package chrepo_test

import (
	"context"
	"testing"
	"time"

	"github.com/ClickHouse/clickhouse-go/v2"
	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/metering/chrepo"
	"github.com/speakeasy-api/gram/server/internal/testenv"
)

const usageSummaryRebuildTimeout = 30 * time.Second

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
	ctx, cancel := context.WithTimeout(t.Context(), usageSummaryRebuildTimeout)
	defer cancel()
	require.NoError(t, chrepo.New(conn).RebuildUsageSummaries(ctx, time.Now().UTC()))

	var publishedBefore time.Time
	require.NoError(t, conn.QueryRow(ctx, `
		SELECT published_before
		FROM billing_meter_daily_summaries
		WHERE organization_id = '' AND is_publication = 1
	`).Scan(&publishedBefore))
	return publishedBefore
}
