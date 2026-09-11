package chrepo_test

import (
	"context"
	"testing"
	"time"

	"github.com/ClickHouse/clickhouse-go/v2"
	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/testenv"
)

const usageSummaryRefreshTimeout = 30 * time.Second

func newTestClickhouse(t *testing.T) clickhouse.Conn {
	t.Helper()
	container, factory, err := testenv.NewTestClickhouse(t.Context())
	require.NoError(t, err)
	t.Cleanup(func() {
		require.NoError(t, container.Terminate(context.Background()))
	})
	conn, err := factory(t)
	require.NoError(t, err)
	// Keep redeliveries unmerged so reads exercise FINAL, not background convergence.
	require.NoError(t, conn.Exec(t.Context(), "SYSTEM STOP MERGES billing_meter_readings_by_time"))
	return conn
}

func refreshUsageSummary(t *testing.T, conn clickhouse.Conn) time.Time {
	t.Helper()
	ctx, cancel := context.WithTimeout(t.Context(), usageSummaryRefreshTimeout)
	defer cancel()
	require.NoError(t, conn.Exec(ctx, "SYSTEM START VIEW billing_meter_daily_summary_refresh"))
	require.NoError(t, conn.Exec(ctx, "SYSTEM REFRESH VIEW billing_meter_daily_summary_refresh"))
	require.NoError(t, conn.Exec(ctx, "SYSTEM WAIT VIEW billing_meter_daily_summary_refresh"))
	require.NoError(t, conn.Exec(ctx, "SYSTEM STOP VIEW billing_meter_daily_summary_refresh"))

	var publishedBefore time.Time
	require.NoError(t, conn.QueryRow(ctx, `
		SELECT published_before
		FROM billing_meter_daily_summaries
		WHERE organization_id = '' AND is_publication = 1
	`).Scan(&publishedBefore))
	return publishedBefore
}

func stopUsageSummaryRefresh(t *testing.T, conn clickhouse.Conn) {
	t.Helper()
	ctx, cancel := context.WithTimeout(t.Context(), usageSummaryRefreshTimeout)
	defer cancel()
	require.NoError(t, conn.Exec(ctx, "SYSTEM STOP VIEW billing_meter_daily_summary_refresh"))
	if err := conn.Exec(ctx, "SYSTEM WAIT VIEW billing_meter_daily_summary_refresh"); err != nil {
		var refreshError *clickhouse.Exception
		require.ErrorAs(t, err, &refreshError)
		require.EqualValues(t, 730, refreshError.Code)
	}
}
