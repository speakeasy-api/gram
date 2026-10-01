package productmetrics

import (
	"context"
	"testing"

	"github.com/ClickHouse/clickhouse-go/v2"
	"github.com/speakeasy-api/gram/server/internal/testenv"
	"github.com/stretchr/testify/require"
)

func newTestClickhouse(t *testing.T) clickhouse.Conn {
	t.Helper()
	container, factory, err := testenv.NewTestClickhouse(t.Context())
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, container.Terminate(context.Background())) })
	conn, err := factory(t)
	require.NoError(t, err)
	require.NoError(t, conn.Exec(t.Context(), "SYSTEM STOP MERGES product_metric_sums_1m"))
	require.NoError(t, conn.Exec(t.Context(), "SYSTEM STOP MERGES product_metric_histograms_1m"))
	return conn
}
