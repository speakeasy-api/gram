//go:build productmetrics_bench

package productmetrics

import (
	"github.com/ClickHouse/clickhouse-go/v2"
	"github.com/stretchr/testify/require"
	"os"
	"testing"
)

// seededBenchmarkClickhouse uses an explicitly selected synthetic benchmark DB.
// The caller owns seeding and retention. Ordinary tests never touch this database.
func seededBenchmarkClickhouse(t *testing.T) clickhouse.Conn {
	t.Helper()
	dsn := os.Getenv("PRODUCT_METRICS_BENCH_DSN")
	require.NotEmpty(t, dsn, "set PRODUCT_METRICS_BENCH_DSN to a seeded synthetic database")
	opts, err := clickhouse.ParseDSN(dsn)
	require.NoError(t, err)
	conn, err := clickhouse.Open(opts)
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, conn.Close()) })
	require.NoError(t, conn.Ping(t.Context()))
	return conn
}
