package demoseed

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestWaitForClickHouseTelemetryDeleteRetriesUntilNoRowsRemain(t *testing.T) {
	t.Parallel()

	counts := []uint64{7, 2, 0}
	calls := 0

	err := waitForClickHouseDelete(
		t.Context(),
		time.Second,
		time.Millisecond,
		func(context.Context) (uint64, error) {
			count := counts[calls]
			calls++
			return count, nil
		},
	)

	require.NoError(t, err)
	require.Equal(t, len(counts), calls)
}

func TestWaitForClickHouseTelemetryDeleteReturnsQueryError(t *testing.T) {
	t.Parallel()

	queryErr := errors.New("query unavailable")

	err := waitForClickHouseDelete(
		t.Context(),
		time.Second,
		time.Millisecond,
		func(context.Context) (uint64, error) {
			return 0, queryErr
		},
	)

	require.ErrorIs(t, err, queryErr)
}

func TestWaitForClickHouseTelemetryDeleteTimesOut(t *testing.T) {
	t.Parallel()

	err := waitForClickHouseDelete(
		t.Context(),
		20*time.Millisecond,
		time.Millisecond,
		func(context.Context) (uint64, error) {
			return 3, nil
		},
	)

	require.ErrorIs(t, err, context.DeadlineExceeded)
}
