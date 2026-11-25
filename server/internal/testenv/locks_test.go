package testenv

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/oops"
	"github.com/speakeasy-api/gram/server/internal/testenv/testrepo"
)

func TestLockHelpers(t *testing.T) {
	t.Parallel()
	container, clone, err := NewTestPostgres(t.Context())
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, container.Terminate(context.Background())) })
	db, err := clone(t, "lock_helpers")
	require.NoError(t, err)
	ctx := t.Context()
	holder := BeginTx(t, ctx, db)
	const key = int64(3435)
	require.NoError(t, testrepo.New(holder).AcquireTestLockFixture(ctx, key))

	// A failed probe must be a server lock error even through service wrapping.
	const probeTimeout = 50 * time.Millisecond
	probe := BeginTx(t, ctx, db)
	SetLockTimeout(t, ctx, probe, probeTimeout)
	err = testrepo.New(probe).AcquireTestLockFixture(ctx, key)
	RequireLockNotAvailable(t, oops.E(oops.CodeUnexpected, fmt.Errorf("probe: %w", err), "lock probe"))
	require.NoError(t, probe.Rollback(ctx))

	pool := NewLockTimeoutPool(t, db, probeTimeout)
	err = testrepo.New(pool).AcquireTestLockFixture(ctx, key)
	RequireLockNotAvailable(t, err)
	// Copying pool settings must not change the original pool.
	require.NotEqual(t, pool.Config().ConnConfig.RuntimeParams["lock_timeout"], db.Config().ConnConfig.RuntimeParams["lock_timeout"])

	waiter1 := BeginTx(t, ctx, db)
	waiter2 := BeginTx(t, ctx, db)
	// waiter2 will wait on waiter1, which itself waits on holder: recursion is required.
	require.NoError(t, testrepo.New(waiter1).AcquireTestLockFixture(ctx, key+1))
	waitCtx, cancel := context.WithCancel(ctx)
	done := make(chan error, 2)
	// Cancel outstanding statements before transaction/pool cleanup on failure.
	var workers sync.WaitGroup
	t.Cleanup(func() { cancel(); workers.Wait() })
	workers.Go(func() { done <- testrepo.New(waiter1).AcquireTestLockFixture(waitCtx, key) })
	WaitForQueryBlockedBy(t, ctx, db, BackendPID(holder), "%AcquireTestLockFixture%")
	unrelated := BeginTx(t, ctx, db)
	unrelatedPID := BackendPID(unrelated)
	require.NoError(t, unrelated.Rollback(ctx))
	count, err := testrepo.New(db).CountBackendsBlockedByFixture(ctx, int32(unrelatedPID))
	require.NoError(t, err)
	require.Zero(t, count)
	matched, err := testrepo.New(db).IsQueryBlockedByFixture(ctx, testrepo.IsQueryBlockedByFixtureParams{HolderPid: int32(unrelatedPID), QueryPattern: "%AcquireTestLockFixture%"})
	require.NoError(t, err)
	require.False(t, matched)
	workers.Go(func() { done <- testrepo.New(waiter2).AcquireTestLockFixture(waitCtx, key+1) })
	WaitForBackendsBlockedBy(t, ctx, db, BackendPID(holder), 2)
	matched, err = testrepo.New(db).IsQueryBlockedByFixture(ctx, testrepo.IsQueryBlockedByFixtureParams{HolderPid: int32(BackendPID(holder)), QueryPattern: "%not_the_waiting_query%"})
	require.NoError(t, err)
	require.False(t, matched)
	cancel()
	for range 2 {
		require.Error(t, <-done)
	}
	require.NoError(t, holder.Rollback(ctx))
	require.NoError(t, testrepo.New(pool).AcquireTestLockFixture(ctx, key))
}

func TestWaitForLockCancellation(t *testing.T) {
	t.Parallel()
	ctx, cancel := context.WithCancel(t.Context())
	calls := 0
	err := waitForLock(ctx, func(context.Context) (bool, error) {
		calls++
		cancel()
		return false, nil
	})
	require.ErrorIs(t, err, context.Canceled)
	require.Equal(t, 1, calls)
}

func TestWaitForLockQueryError(t *testing.T) {
	t.Parallel()
	expected := errors.New("observer unavailable")
	err := waitForLock(t.Context(), func(context.Context) (bool, error) { return false, expected })
	require.ErrorIs(t, err, expected)
}
