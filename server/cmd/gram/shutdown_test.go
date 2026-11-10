package gram

import (
	"context"
	"errors"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/mcpconnectionattempts"
)

// The recorder's writer must be joined before runShutdown returns and ClickHouse closes.
func TestConnectionRecorderShutdown_EndsBeforeRunShutdown(t *testing.T) {
	t.Parallel()

	require.Less(t, connectionRecorderShutdownTimeout+mcpconnectionattempts.MaxShutdownOverrun, runShutdownTimeout)
}

// The recorder drain runs alongside other shutdown work; only what waits on it is held.
func TestStartShutdown_RunsAlongsideOtherWorkAndWaitReturnsItsResult(t *testing.T) {
	t.Parallel()

	otherRan := make(chan struct{})
	drainErr := errors.New("drain failed")
	wait := startShutdown(t.Context(), func(ctx context.Context) error {
		select {
		case <-otherRan:
			return drainErr
		case <-ctx.Done():
			return ctx.Err()
		}
	})
	close(otherRan)
	require.ErrorIs(t, wait(), drainErr)
}

func TestStartShutdown_NilWaitsForNothing(t *testing.T) {
	t.Parallel()

	require.NoError(t, startShutdown(t.Context(), nil)())
}
