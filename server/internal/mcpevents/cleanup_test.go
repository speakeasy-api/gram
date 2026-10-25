package mcpevents

import (
	"context"
	"errors"
	"github.com/stretchr/testify/require"
	"testing"
)

func TestCleanupBatchesBoundsSaturatedSweeps(t *testing.T) {
	t.Parallel()
	calls := 0
	err := cleanupBatches(t.Context(), func(context.Context) (int64, error) { calls++; return cleanupBatchSize, nil })
	require.ErrorContains(t, err, "budget exhausted")
	require.Equal(t, cleanupMaxBatches, calls)
}

func TestCleanupBatchesStopsOnDatabaseFailure(t *testing.T) {
	t.Parallel()
	failure := errors.New("database unavailable")
	err := cleanupBatches(t.Context(), func(context.Context) (int64, error) { return 0, failure })
	require.ErrorIs(t, err, failure)
}
