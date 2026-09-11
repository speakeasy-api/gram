package chrepo_test

import (
	"math"
	"math/big"
	"testing"
	"time"

	"github.com/ClickHouse/clickhouse-go/v2"
	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/metering"
	"github.com/speakeasy-api/gram/server/internal/metering/chrepo"
)

func TestGetUsageDeduplicatesRanksAndPreservesExactTotals(t *testing.T) {
	t.Parallel()
	conn := newTestClickhouse(t)
	queries := chrepo.New(conn)
	organizationID := "org-" + uuid.NewString()
	from := time.Date(2026, time.January, 31, 12, 0, 0, 0, time.UTC)
	to := time.Date(2026, time.February, 2, 12, 0, 0, 0, time.UTC)

	rows := make([]chrepo.ReadingRow, 0, 10)
	for index, model := range []string{"largest", "m1", "m2", "m3", "m4", "m5", "m6", "m7"} {
		value := int64(10 - index)
		if model == "largest" {
			value = math.MaxInt64
		}
		rows = append(rows, meterUsageReading(organizationID, metering.MeterAgentSessionStorage, value, from.Add(time.Duration(index)*time.Minute), nil, map[string]string{
			metering.AttributeModel: model,
		}))
	}
	secondLargest := meterUsageReading(organizationID, metering.MeterAgentSessionStorage, math.MaxInt64, time.Date(2026, time.February, 1, 1, 0, 0, 0, time.UTC), nil, map[string]string{
		metering.AttributeModel: "largest",
	})
	rows = append(rows, secondLargest)
	retry := secondLargest
	retry.InsertedAt = retry.InsertedAt.Add(time.Second)
	rows = append(rows, retry)
	require.NoError(t, queries.InsertReadings(t.Context(), rows))
	refreshUsageSummary(t, conn)

	result, err := queries.GetUsage(t.Context(), chrepo.UsageParams{
		OrganizationID: organizationID,
		Selection:      storageUsageSelection(t, "model"),
		From:           from,
		To:             to,
		ReadingKind:    chrepo.ReadingKindUsage,
	})
	require.NoError(t, err)
	require.Equal(t, "stokens", result.Unit)
	require.Equal(t, "tiktoken_o200k_base", result.MeasurementMethod)
	require.LessOrEqual(t, len(result.Rows), 14)

	total := new(big.Int)
	largest := new(big.Int)
	remainderSeen := false
	for _, row := range result.Rows {
		quantity, ok := new(big.Int).SetString(row.Total, 10)
		require.True(t, ok)
		total.Add(total, quantity)
		if row.Key == "largest" {
			largest.Add(largest, quantity)
		}
		if row.Kind == "remainder" {
			remainderSeen = true
			require.Empty(t, row.Key)
			require.Equal(t, "Other", row.Label)
		}
	}
	require.Equal(t, "18446744073709551614", largest.String())
	require.Equal(t, "18446744073709551656", total.String())
	require.True(t, remainderSeen)
}

func TestGetUsageKeepsAdjustmentsSeparateAndNormalizesUnsetSets(t *testing.T) {
	t.Parallel()
	conn := newTestClickhouse(t)
	queries := chrepo.New(conn)
	organizationID := "org-" + uuid.NewString()
	from := time.Date(2026, time.March, 1, 0, 0, 0, 0, time.UTC)
	to := from.Add(48 * time.Hour)
	originalID := uuid.New()

	usage := meterUsageReading(organizationID, metering.MeterAgentSessionStorage, 500, from.Add(time.Hour), nil, map[string]string{})
	negative := meterUsageReading(organizationID, metering.MeterAgentSessionStorage, -100, from.Add(2*time.Hour), &originalID, map[string]string{})
	positive := meterUsageReading(organizationID, metering.MeterAgentSessionStorage, 100, from.Add(25*time.Hour), &originalID, map[string]string{})
	sortedGroups := meterUsageReading(organizationID, metering.MeterAgentSessionStorage, 25, from.Add(3*time.Hour), nil, map[string]string{
		metering.AttributeBillingUserDirectoryGroups: `["Support","Engineering","Support"]`,
	})
	require.NoError(t, queries.InsertReadings(t.Context(), []chrepo.ReadingRow{usage, negative, positive, sortedGroups}))
	refreshUsageSummary(t, conn)

	adjustments, err := queries.GetUsage(t.Context(), chrepo.UsageParams{
		OrganizationID: organizationID,
		Selection:      storageUsageSelection(t, "model"),
		From:           from,
		To:             to,
		ReadingKind:    chrepo.ReadingKindAdjustment,
	})
	require.NoError(t, err)
	require.Len(t, adjustments.Rows, 2)
	require.Equal(t, "unset", adjustments.Rows[0].Kind)
	require.Equal(t, "(unset)", adjustments.Rows[0].Label)
	require.Equal(t, "-100", adjustments.Rows[0].Total)
	require.Equal(t, "100", adjustments.Rows[1].Total)

	groups, err := queries.GetUsage(t.Context(), chrepo.UsageParams{
		OrganizationID: organizationID,
		Selection:      storageUsageSelection(t, "directory_group_set"),
		From:           from,
		To:             to,
		ReadingKind:    chrepo.ReadingKindUsage,
	})
	require.NoError(t, err)
	require.Len(t, groups.Rows, 2)
	require.Equal(t, "unset", groups.Rows[0].Kind)
	require.Equal(t, "value", groups.Rows[1].Kind)
	require.Equal(t, `["Engineering","Support"]`, groups.Rows[1].Key)
	require.Equal(t, "Engineering, Support", groups.Rows[1].Label)
}

func TestGetUsageRejectsMixedMeasurementMethods(t *testing.T) {
	t.Parallel()
	conn := newTestClickhouse(t)
	queries := chrepo.New(conn)
	organizationID := "org-" + uuid.NewString()
	from := time.Date(2026, time.April, 1, 0, 0, 0, 0, time.UTC)
	row := meterUsageReading(organizationID, metering.MeterAgentSessionStorage, 1, from.Add(time.Hour), nil, map[string]string{})
	row.MeasurementMethod = "incompatible"
	require.NoError(t, queries.InsertReadings(t.Context(), []chrepo.ReadingRow{row}))
	refreshUsageSummary(t, conn)

	_, err := queries.GetUsage(t.Context(), chrepo.UsageParams{
		OrganizationID: organizationID,
		Selection:      storageUsageSelection(t, "total"),
		From:           from,
		To:             from.Add(24 * time.Hour),
		ReadingKind:    chrepo.ReadingKindUsage,
	})
	require.ErrorIs(t, err, chrepo.ErrMixedMeasurement)
}

func TestGetUsagePreservesZeroNetOmittedActivity(t *testing.T) {
	t.Parallel()
	conn := newTestClickhouse(t)
	queries := chrepo.New(conn)
	organizationID := "org-" + uuid.NewString()
	from := time.Date(2026, time.May, 1, 0, 0, 0, 0, time.UTC)
	originalID := uuid.New()

	rows := make([]chrepo.ReadingRow, 0, 9)
	for index, model := range []string{"m1", "m2", "m3", "m4", "m5", "m6"} {
		rows = append(rows, meterUsageReading(
			organizationID,
			metering.MeterAgentSessionStorage,
			int64(100-index),
			from.Add(time.Duration(index+1)*time.Hour),
			&originalID,
			map[string]string{metering.AttributeModel: model},
		))
	}
	rows = append(
		rows,
		meterUsageReading(organizationID, metering.MeterAgentSessionStorage, 5, from.Add(7*time.Hour), &originalID, map[string]string{metering.AttributeModel: "omitted"}),
		meterUsageReading(organizationID, metering.MeterAgentSessionStorage, -5, from.Add(8*time.Hour), &originalID, map[string]string{metering.AttributeModel: "omitted"}),
	)
	require.NoError(t, queries.InsertReadings(t.Context(), rows))
	refreshUsageSummary(t, conn)

	result, err := queries.GetUsage(t.Context(), chrepo.UsageParams{
		OrganizationID: organizationID,
		Selection:      storageUsageSelection(t, "model"),
		From:           from,
		To:             from.Add(24 * time.Hour),
		ReadingKind:    chrepo.ReadingKindAdjustment,
	})
	require.NoError(t, err)
	require.Len(t, result.Rows, 7)
	require.Equal(t, "remainder", result.Rows[0].Kind)
	require.Equal(t, "0", result.Rows[0].Total)
}

func TestGetUsageRanksPeriodWinnersWithTheLiveTail(t *testing.T) {
	t.Parallel()
	conn := newTestClickhouse(t)
	queries := chrepo.New(conn)
	organizationID := "org-" + uuid.NewString()
	publicationBoundary := time.Now().UTC().Add(-time.Hour).Truncate(24 * time.Hour)
	from := publicationBoundary.AddDate(0, 0, -5)
	rows := make([]chrepo.ReadingRow, 0, 14)
	for _, model := range []string{"a0", "a1", "a2", "a3", "a4", "a5", "b0", "b1", "b2", "b3", "b4", "b5"} {
		day := from
		if model[0] == 'b' {
			day = day.AddDate(0, 0, 1)
		}
		rows = append(rows, meterUsageReading(organizationID, metering.MeterAgentSessionStorage, 100, day.Add(time.Hour), nil, map[string]string{metering.AttributeModel: model}))
	}
	for day := range 2 {
		// Seventh on each day, but first across the two complete days.
		rows = append(rows, meterUsageReading(organizationID, metering.MeterAgentSessionStorage, 99, from.AddDate(0, 0, day).Add(time.Hour), nil, map[string]string{metering.AttributeModel: "period"}))
	}
	require.NoError(t, queries.InsertReadings(t.Context(), rows))
	publishedBefore := refreshUsageSummary(t, conn)
	require.NoError(t, queries.InsertReadings(t.Context(), []chrepo.ReadingRow{
		meterUsageReading(organizationID, metering.MeterAgentSessionStorage, 500, publishedBefore.Add(time.Minute), nil, map[string]string{metering.AttributeModel: "live"}),
	}))

	result, err := queries.GetUsage(t.Context(), chrepo.UsageParams{
		OrganizationID: organizationID,
		Selection:      storageUsageSelection(t, "model"),
		From:           from,
		To:             publishedBefore.Add(time.Hour),
		ReadingKind:    chrepo.ReadingKindUsage,
	})
	require.NoError(t, err)
	totals := make(map[string]*big.Int)
	for _, row := range result.Rows {
		identity := row.Kind + ":" + row.Key
		if totals[identity] == nil {
			totals[identity] = new(big.Int)
		}
		value, ok := new(big.Int).SetString(row.Total, 10)
		require.True(t, ok)
		totals[identity].Add(totals[identity], value)
	}
	exact := make(map[string]string, len(totals))
	for identity, total := range totals {
		exact[identity] = total.String()
	}
	require.Equal(t, map[string]string{
		"value:live": "500", "value:period": "198",
		"value:a0": "100", "value:a1": "100", "value:a2": "100", "value:a3": "100",
		"remainder:": "800",
	}, exact)
}

func TestGetUsageRejectsAbsentPublication(t *testing.T) {
	t.Parallel()
	conn := newTestClickhouse(t)
	prepareMissingUsagePublication(t, conn)

	publishedBefore := time.Now().UTC().Add(-time.Hour).Truncate(24 * time.Hour)
	_, err := chrepo.New(conn).GetUsage(t.Context(), chrepo.UsageParams{
		OrganizationID: "org-" + uuid.NewString(),
		Selection:      storageUsageSelection(t, "total"),
		From:           publishedBefore.AddDate(0, 0, -1),
		To:             publishedBefore,
		ReadingKind:    chrepo.ReadingKindUsage,
	})
	require.ErrorIs(t, err, chrepo.ErrUsageSummaryUnavailable)
}

func TestGetUsageRejectsStalePublication(t *testing.T) {
	t.Parallel()
	conn := newTestClickhouse(t)
	prepareMissingUsagePublication(t, conn)

	publishedBefore := time.Now().UTC().Add(-time.Hour).Truncate(24*time.Hour).AddDate(0, 0, -4)
	require.NoError(t, conn.Exec(t.Context(), `
		INSERT INTO billing_meter_daily_summaries
		VALUES ('', '', '', '', ?, '', '', '', '', '', toInt128(0), toUInt64(0), toUInt8(1), ?, now64(9))
	`, publishedBefore, publishedBefore))
	_, err := chrepo.New(conn).GetUsage(t.Context(), chrepo.UsageParams{
		OrganizationID: "org-" + uuid.NewString(),
		Selection:      storageUsageSelection(t, "total"),
		From:           publishedBefore,
		To:             time.Now().UTC(),
		ReadingKind:    chrepo.ReadingKindUsage,
	})
	require.ErrorIs(t, err, chrepo.ErrUsageSummaryUnavailable)
}

func storageUsageSelection(t *testing.T, breakdown string) chrepo.UsageSelection {
	t.Helper()
	_, selection, err := metering.ResolveUsageSelection(metering.UsageFamilyAgentSessionStorage, breakdown)
	require.NoError(t, err)
	return selection
}

func prepareMissingUsagePublication(t *testing.T, conn clickhouse.Conn) {
	t.Helper()
	stopUsageSummaryRefresh(t, conn)
	require.NoError(t, conn.Exec(t.Context(), "TRUNCATE TABLE billing_meter_daily_summaries"))
}

func meterUsageReading(organizationID string, meterID metering.MeterID, value int64, occurredAt time.Time, corrects *uuid.UUID, attributes map[string]string) chrepo.ReadingRow {
	unit := "stokens"
	method := "tiktoken_o200k_base"
	if meterID == metering.MeterMCPBandwidthIngress || meterID == metering.MeterMCPBandwidthEgress {
		unit = "bytes"
		method = "http_body_bytes"
	}
	return chrepo.ReadingRow{
		ID:                uuid.New(),
		OrganizationID:    organizationID,
		ProjectID:         uuid.New(),
		MeterID:           string(meterID),
		OperationID:       "usage-test:" + uuid.NewString(),
		Unit:              unit,
		MeasurementMethod: method,
		Value:             value,
		OccurredAt:        occurredAt,
		ProducedAt:        occurredAt.Add(time.Second),
		InsertedAt:        occurredAt.Add(2 * time.Second),
		CorrectsReadingID: corrects,
		Attributes:        attributes,
	}
}
