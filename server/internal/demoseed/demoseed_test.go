package demoseed

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestWaitForClickHouseTelemetryDeleteRetriesUntilNoRowsRemain(t *testing.T) {
	t.Parallel()

	counts := []uint64{7, 2, 0}
	calls := 0

	err := waitForClickHouseTelemetryDelete(
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

	err := waitForClickHouseTelemetryDelete(
		t.Context(),
		time.Second,
		time.Millisecond,
		func(context.Context) (uint64, error) {
			return 0, queryErr
		},
	)

	require.ErrorIs(t, err, queryErr)
	require.ErrorContains(t, err, "check demo telemetry delete visibility")
}

func TestWaitForClickHouseTelemetryDeleteTimesOut(t *testing.T) {
	t.Parallel()

	err := waitForClickHouseTelemetryDelete(
		t.Context(),
		20*time.Millisecond,
		time.Millisecond,
		func(context.Context) (uint64, error) {
			return 3, nil
		},
	)

	require.ErrorIs(t, err, context.DeadlineExceeded)
	require.ErrorContains(t, err, "3 rows remaining")
}

func TestClickHouseDeletePreflightPrecedesInserts(t *testing.T) {
	t.Parallel()

	statements := splitStatements(clickhouseSQL)
	lastDelete := -1
	preflight := -1
	firstInsert := -1
	for i, stmt := range statements {
		upperStmt := strings.ToUpper(stmt)
		switch {
		case strings.HasPrefix(upperStmt, "DELETE "):
			lastDelete = i
		case strings.Contains(stmt, "demo seed preflight: telemetry rows remain after scoped deletes"):
			preflight = i
		case firstInsert == -1 && strings.HasPrefix(upperStmt, "INSERT "):
			firstInsert = i
		}
	}

	require.NotEqual(t, -1, lastDelete)
	require.NotEqual(t, -1, preflight)
	require.NotEqual(t, -1, firstInsert)
	require.Less(t, lastDelete, preflight)
	require.Less(t, preflight, firstInsert)
}
