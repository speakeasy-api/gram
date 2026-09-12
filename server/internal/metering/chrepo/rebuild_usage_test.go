package chrepo_test

import (
	"testing"
	"time"

	"github.com/ClickHouse/clickhouse-go/v2"
	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/metering"
	"github.com/speakeasy-api/gram/server/internal/metering/chrepo"
)

func TestRebuildUsageSummariesConservesDuplicatesAndRepairsLateArrivals(t *testing.T) {
	t.Parallel()
	conn := newTestClickhouse(t)
	queries := chrepo.New(conn)
	day := time.Now().UTC().AddDate(0, 0, -3).Truncate(24 * time.Hour)
	organizationID := "org-" + uuid.NewString()

	first := meterUsageReading(organizationID, metering.MeterAgentSessionStorage, 17, day.Add(time.Hour), nil, map[string]string{
		metering.AttributeModel: "claude-sonnet",
	})
	retry := first
	retry.InsertedAt = retry.InsertedAt.Add(time.Second)
	require.NoError(t, queries.InsertReadings(t.Context(), []chrepo.ReadingRow{first, retry}))

	snapshotAt := day.AddDate(0, 0, 2).Add(123456789 * time.Nanosecond)
	require.NoError(t, queries.RebuildUsageSummaries(t.Context(), snapshotAt))
	requireUsageSummaryTotal(t, conn, organizationID, day, "17", 1)
	requireUsageSummarySnapshot(t, conn, snapshotAt)

	late := meterUsageReading(organizationID, metering.MeterAgentSessionStorage, 5, day.Add(2*time.Hour), nil, map[string]string{
		metering.AttributeModel: "claude-opus",
	})
	require.NoError(t, queries.InsertReadings(t.Context(), []chrepo.ReadingRow{late}))
	secondSnapshotAt := snapshotAt.Add(time.Nanosecond)
	require.NoError(t, queries.RebuildUsageSummaries(t.Context(), secondSnapshotAt))
	requireUsageSummaryTotal(t, conn, organizationID, day, "22", 2)
	requireUsageSummarySnapshot(t, conn, secondSnapshotAt)

	// Rebuilding the same full history is replacement, not append.
	thirdSnapshotAt := secondSnapshotAt.Add(time.Nanosecond)
	require.NoError(t, queries.RebuildUsageSummaries(t.Context(), thirdSnapshotAt))
	requireUsageSummaryTotal(t, conn, organizationID, day, "22", 2)
	requireUsageSummarySnapshot(t, conn, thirdSnapshotAt)

	for _, table := range []string{
		"billing_meter_daily_summaries_staging",
		"billing_meter_daily_summary_parts",
		"billing_meter_daily_summary_attempt",
	} {
		var count uint64
		require.NoError(t, conn.QueryRow(t.Context(), "SELECT count() FROM "+table).Scan(&count))
		require.Zero(t, count)
	}
}

func TestRebuildUsageSummariesFailureRetainsPublishedGeneration(t *testing.T) {
	t.Parallel()
	conn := newTestClickhouse(t)
	queries := chrepo.New(conn)
	day := time.Now().UTC().AddDate(0, 0, -3).Truncate(24 * time.Hour)
	organizationID := "org-" + uuid.NewString()
	require.NoError(t, queries.InsertReadings(t.Context(), []chrepo.ReadingRow{
		meterUsageReading(organizationID, metering.MeterAgentSessionStorage, 31, day.Add(time.Hour), nil, map[string]string{}),
	}))
	snapshotAt := day.AddDate(0, 0, 2)
	require.NoError(t, queries.RebuildUsageSummaries(t.Context(), snapshotAt))
	requireUsageSummaryTotal(t, conn, organizationID, day, "31", 1)

	require.NoError(t, conn.Exec(t.Context(), "ALTER TABLE billing_meter_daily_summary_attempt ADD CONSTRAINT usage_summary_test_failure CHECK quantity = 0"))
	err := queries.RebuildUsageSummaries(t.Context(), snapshotAt.Add(time.Hour))
	require.Error(t, err)
	requireUsageSummaryTotal(t, conn, organizationID, day, "31", 1)

	var publishedSnapshot time.Time
	require.NoError(t, conn.QueryRow(t.Context(), `
		SELECT snapshot_at
		FROM billing_meter_daily_summaries
		WHERE organization_id = '' AND is_publication = 1
	`).Scan(&publishedSnapshot))
	require.Equal(t, snapshotAt, publishedSnapshot)
}

func requireUsageSummaryTotal(t *testing.T, conn clickhouse.Conn, organizationID string, day time.Time, quantity string, readingCount uint64) {
	t.Helper()
	var actualQuantity string
	var actualCount uint64
	require.NoError(t, conn.QueryRow(t.Context(), `
		SELECT toString(sum(quantity)), sum(reading_count)
		FROM billing_meter_daily_summaries
		WHERE organization_id = ? AND family = 'agent_session_storage'
			AND reading_kind = 'usage' AND facet = 'total' AND day = ?
	`, organizationID, day).Scan(&actualQuantity, &actualCount))
	require.Equal(t, quantity, actualQuantity)
	require.Equal(t, readingCount, actualCount)
}

func requireUsageSummarySnapshot(t *testing.T, conn clickhouse.Conn, expected time.Time) {
	t.Helper()
	var snapshotAt time.Time
	require.NoError(t, conn.QueryRow(t.Context(), `
		SELECT snapshot_at
		FROM billing_meter_daily_summaries
		WHERE organization_id = '' AND is_publication = 1
	`).Scan(&snapshotAt))
	require.Equal(t, expected, snapshotAt)
}
