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
	for _, c := range []struct {
		name    string
		err     error
		overCap bool
	}{
		{"too many rows", &clickhouse.Exception{Code: 158}, true},
		{"timeout", &clickhouse.Exception{Code: 159}, true},
		{"memory limit", &clickhouse.Exception{Code: 241}, true},
		{"too many bytes", &clickhouse.Exception{Code: 307}, true},
		{"too many result rows", &clickhouse.Exception{Code: 396}, true},
		{"unknown table", &clickhouse.Exception{Code: 60}, false},
		{"network", errors.New("network"), false},
	} {
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()
			if c.overCap {
				require.ErrorIs(t, readFailed("query", c.err), ErrReadTooLarge)
			} else {
				require.NotErrorIs(t, readFailed("query", c.err), ErrReadTooLarge)
			}
		})
	}
}

// The driver replaces a context's max_execution_time, so each failing read caps its time in the query.
func TestFailingReads_CapTheirTimeInTheQuery(t *testing.T) {
	t.Parallel()
	for _, query := range []string{servedQuery, answersQuery, servedByFamilyQuery} {
		require.True(t, strings.HasSuffix(query, " SETTINGS max_execution_time = 30"), query)
	}
}
