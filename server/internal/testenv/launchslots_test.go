//go:build unix

package testenv

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

// slotWaitTimeout bounds how long a test waits to prove a slot is taken. The
// poll interval is 100ms, so this covers several attempts.
const slotWaitTimeout = 500 * time.Millisecond

func TestAcquireLaunchSlotInBlocksWhenAllSlotsAreHeld(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()

	first, err := acquireLaunchSlotIn(t.Context(), dir, 2)
	require.NoError(t, err)
	second, err := acquireLaunchSlotIn(t.Context(), dir, 2)
	require.NoError(t, err)

	ctx, cancel := context.WithTimeout(t.Context(), slotWaitTimeout)
	defer cancel()
	_, err = acquireLaunchSlotIn(ctx, dir, 2)
	require.ErrorIs(t, err, context.DeadlineExceeded)

	first()

	third, err := acquireLaunchSlotIn(t.Context(), dir, 2)
	require.NoError(t, err)

	second()
	third()
}

func TestMaxConcurrentLaunchesRejectsInvalidValues(t *testing.T) {
	t.Setenv(maxConcurrentLaunchesEnv, "-1")
	_, err := maxConcurrentLaunches()
	require.Error(t, err)

	t.Setenv(maxConcurrentLaunchesEnv, "four")
	_, err = maxConcurrentLaunches()
	require.Error(t, err)

	t.Setenv(maxConcurrentLaunchesEnv, "")
	n, err := maxConcurrentLaunches()
	require.NoError(t, err)
	require.Zero(t, n)
}
