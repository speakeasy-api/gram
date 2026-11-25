package mcpconnectionattempts

import (
	"errors"
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
