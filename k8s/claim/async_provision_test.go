package claim

import (
	"context"
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/arschles/assert"
	"github.com/deis/steward-framework"
	"github.com/deis/steward-framework/fake"
	"github.com/deis/steward-framework/k8s"
	"github.com/deis/steward-framework/k8s/claim/state"
	"github.com/pborman/uuid"
)

func TestPollProvisionState(t *testing.T) {
	const (
		timeout = 1 * time.Second
	)
	ctx, cancelFn := context.WithCancel(context.Background())
	defer cancelFn()

	serviceID := uuid.New()
	planID := uuid.New()
	operation := "testOperation"
	instanceID := uuid.New()

	var curStateMut sync.RWMutex
	curState := framework.LastOperationStateInProgress

	lastOpGetter := &fake.LastOperationGetter{
		Ret: func() *framework.GetLastOperationResponse {
			curStateMut.RLock()
			defer curStateMut.RUnlock()
			return &framework.GetLastOperationResponse{State: curState.String()}
		},
	}

	claimCh := make(chan state.Update)
	go func() {
		finalState := pollProvisionState(
			ctx,
			serviceID,
			planID,
			operation,
			instanceID,
			lastOpGetter,
			claimCh,
		)
		assert.Equal(t, finalState, framework.LastOperationStateSucceeded, "final state")
	}()

	/////
	// expect a provisioning-async first. after we receive it, the last op getter will get another provisioning-async and wait to send it. we then change the current state, receive the second provisioning-async and then expect the channel to not receive anymore. the final success state is received in the return value of pollProvisionState, and it's checked in the above goroutine
	/////

	assert.NoErr(t, acceptStatus(claimCh, k8s.StatusProvisioningAsync))

	curStateMut.Lock()
	curState = framework.LastOperationStateSucceeded
	curStateMut.Unlock()

	assert.NoErr(t, acceptStatus(claimCh, k8s.StatusProvisioningAsync))

	select {
	case update := <-claimCh:
		t.Fatalf("received %s on claim channel, expected nothing", update)
	case <-time.After(timeout):
	}
}

func acceptStatus(claimCh <-chan state.Update, expected k8s.ServicePlanClaimStatus) error {
	const timeout = 31 * time.Second
	select {
	case update := <-claimCh:
		if update.Status() != expected {
			return fmt.Errorf("expected status %s, got %s", expected, update.Status())
		}
		return nil
	case <-time.After(timeout):
		return fmt.Errorf("no status update after %s", timeout)
	}
}
