package chrepo_test

import (
	"context"
	"testing"

	"github.com/ClickHouse/clickhouse-go/v2"
	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/testenv"
)

func newTestClickhouse(t *testing.T) clickhouse.Conn {
	t.Helper()
	container, factory, err := testenv.NewTestClickhouse(t.Context())
	require.NoError(t, err)
	t.Cleanup(func() {
		require.NoError(t, container.Terminate(context.Background()))
	})
	conn, err := factory(t)
	require.NoError(t, err)
	// Keep summary parts unmerged so reads must aggregate their increments.
	require.NoError(t, conn.Exec(t.Context(), "SYSTEM STOP MERGES billing_meter_daily_summaries"))
	return conn
}
