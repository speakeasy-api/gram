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
	for range 100 {
		require.False(t, r.acquireSlot(ctx), "cancellation must win even when a slot is available")
	}
	require.Empty(t, r.slots)
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
