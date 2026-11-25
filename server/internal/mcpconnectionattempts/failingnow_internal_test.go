package mcpconnectionattempts

import (
	"errors"
	"strings"
	"testing"

	"github.com/ClickHouse/clickhouse-go/v2"
	"github.com/stretchr/testify/require"
)

func TestReadFailed_MarksAReadOverItsLimits(t *testing.T) {
	t.Parallel()
	require.ErrorIs(t, readFailed("query", &clickhouse.Exception{Code: 396}), ErrReadTooLarge, "too many result rows")
	require.ErrorIs(t, readFailed("query", &clickhouse.Exception{Code: 159}), ErrReadTooLarge, "timeout")
	require.NotErrorIs(t, readFailed("query", &clickhouse.Exception{Code: 60}), ErrReadTooLarge)
	require.NotErrorIs(t, readFailed("query", errors.New("network")), ErrReadTooLarge)
}

// The driver replaces a context's max_execution_time, so each failing read caps its time in the query.
func TestFailingReads_CapTheirTimeInTheQuery(t *testing.T) {
	t.Parallel()
	for _, query := range []string{servedQuery, answersQuery, servedByFamilyQuery} {
		require.True(t, strings.HasSuffix(query, " SETTINGS max_execution_time = 30"), query)
	}
}
