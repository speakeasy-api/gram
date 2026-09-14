package mcp

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/remotesessions"
)

func TestRemoteSessionRecheckLeaseCoversQueuedBatch(t *testing.T) {
	t.Parallel()
	waves := (int(remoteSessionRecheckBatch) + remoteSessionRecheckSlots - 1) / remoteSessionRecheckSlots
	budget := time.Duration(waves)*RemoteSessionRecheckProbeBudgetCap + RemoteSessionRecheckLeaseReleaseBudget
	require.Greater(t, remotesessions.RecheckLease(time.Nanosecond), budget, "the shortest lease must cover every queued probe, not just one probe")
}

func TestRemoteSessionShutdownDoesNotAdmitCancelledProbe(t *testing.T) {
	t.Parallel()
	r := newRemoteSessionRecheck(time.Hour, nil, nil)
	require.True(t, r.acquireSlot(t.Context()))
	<-r.slots

	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	require.False(t, r.acquireSlot(ctx), "an already cancelled context must not acquire a slot")
	require.Empty(t, r.slots)
}

func TestRemoteSessionShutdownReleasesSlotCancelledAfterAcquisition(t *testing.T) {
	t.Parallel()
	r := newRemoteSessionRecheck(time.Hour, nil, nil)
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()

	checks := 0
	// Inject cancellation at the post-send Err check. The context stays live
	// through the entry check and select, so the slot-send branch must run.
	controlled := remoteSessionErrHookContext{Context: ctx, beforeErr: func() {
		checks++
		if checks == 2 {
			require.Len(t, r.slots, 1, "cancellation must happen after acquiring the slot")
			cancel()
		}
	}}
	require.False(t, r.acquireSlot(controlled), "cancellation after acquisition must refuse admission")
	require.Equal(t, 2, checks, "must reach the post-send cancellation check")
	require.ErrorIs(t, ctx.Err(), context.Canceled)
	require.Empty(t, r.slots, "refused admission must release the acquired slot")
}

// remoteSessionErrHookContext injects a cancellation at a deterministic checkpoint.
type remoteSessionErrHookContext struct {
	context.Context
	beforeErr func()
}

func (c remoteSessionErrHookContext) Err() error {
	c.beforeErr()
	return c.Context.Err()
}

func TestRemoteSessionShutdownClosesBothAdmissionGatesBeforeDraining(t *testing.T) {
	t.Parallel()
	s := new(Service)
	s.autoVerifications = newAutoVerifications()
	s.remoteSessionRecheck = newRemoteSessionRecheck(time.Hour, nil, nil)
	release := make(chan struct{})
	t.Cleanup(func() {
		close(release)
		s.remoteSessionRecheck.wg.Wait()
		s.autoVerifications.wg.Wait()
	})
	require.True(t, s.remoteSessionRecheck.admit(func() { <-release }))
	require.True(t, s.autoVerifications.admit(func() { <-release }))

	ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
	defer cancel()
	done := make(chan error, 1)
	go func() { done <- s.Shutdown(ctx) }()
	select {
	case <-s.remoteSessionRecheck.stop:
	case <-ctx.Done():
		t.Fatal("shutdown did not close the recheck admission gate")
	}
	require.False(t, s.remoteSessionRecheck.admit(func() {}))
	require.False(t, s.autoVerifications.admit(func() {}), "automatic verification must stop before rechecks finish draining")
	select {
	case err := <-done:
		t.Fatalf("shutdown returned before probes drained: %v", err)
	default:
	}

	// Both groups are still busy: an expired drain must report both errors.
	cancel()
	err := <-done
	require.ErrorIs(t, err, context.Canceled)
	require.ErrorContains(t, err, "drain remote session re-checks")
	require.ErrorContains(t, err, "drain automatic verifications")
}
