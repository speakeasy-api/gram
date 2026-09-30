package productmetrics

import (
	"context"
	"os"
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

// seededBenchmarkClickhouse uses an explicitly selected synthetic benchmark DB.
// The caller owns seeding and retention. Ordinary tests never touch this database.
func seededBenchmarkClickhouse(t *testing.T) clickhouse.Conn {
	t.Helper()
	dsn := os.Getenv("PRODUCT_METRICS_BENCH_DSN")
	if dsn == "" {
		t.Skip("set PRODUCT_METRICS_BENCH_DSN to a seeded synthetic database")
	}
	opts, err := clickhouse.ParseDSN(dsn)
	require.NoError(t, err)
	conn, err := clickhouse.Open(opts)
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, conn.Close()) })
	require.NoError(t, conn.Ping(t.Context()))
	return conn
}
