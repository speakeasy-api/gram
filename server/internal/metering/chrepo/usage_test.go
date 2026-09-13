package chrepo_test

import (
	"math"
	"math/big"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/metering"
	"github.com/speakeasy-api/gram/server/internal/metering/chrepo"
)

func TestGetUsageCountsDeliveriesScopesTenantAndConservesExactTotals(t *testing.T) {
	t.Parallel()
	conn := newTestClickhouse(t)
	queries := chrepo.New(conn)
	organizationID := "org-" + uuid.NewString()
	from := time.Date(2026, time.January, 31, 0, 0, 0, 0, time.UTC)
	to := from.AddDate(0, 0, 2)

	rows := make([]chrepo.ReadingRow, 0, 12)
	for index, model := range []string{"largest", "m1", "m2", "m3", "m4", "m5", "m6", "m7"} {
		value := int64(10 - index)
		if model == "largest" {
			value = math.MaxInt64
		}
		rows = append(rows, meterUsageReading(organizationID, metering.MeterAgentSessionStorage, value, from.Add(time.Duration(index+1)*time.Minute), nil, map[string]string{
			metering.AttributeModel: model,
		}))
	}
	secondLargest := meterUsageReading(organizationID, metering.MeterAgentSessionStorage, math.MaxInt64, from.AddDate(0, 0, 1).Add(time.Hour), nil, map[string]string{
		metering.AttributeModel: "largest",
	})
	rows = append(rows, secondLargest)
	redelivery := secondLargest
	redelivery.InsertedAt = to.Add(time.Hour)
	rows = append(rows, meterUsageReading("other-"+uuid.NewString(), metering.MeterAgentSessionStorage, math.MaxInt64, from.Add(time.Hour), nil, map[string]string{
		metering.AttributeModel: "other-tenant",
	}))
	require.NoError(t, queries.InsertReadings(t.Context(), rows))
	// Deliver the old reading in a separate, unmerged block after this window.
	require.NoError(t, queries.InsertReadings(t.Context(), []chrepo.ReadingRow{redelivery}))

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
	require.Equal(t, "27670116110564327421", largest.String())
	require.Equal(t, "27670116110564327463", total.String())
	require.True(t, remainderSeen)
}

func TestGetUsageKeepsAdjustmentsSeparateAndNormalizesUnsetSets(t *testing.T) {
	t.Parallel()
	conn := newTestClickhouse(t)
	queries := chrepo.New(conn)
	organizationID := "org-" + uuid.NewString()
	from := time.Date(2026, time.March, 1, 0, 0, 0, 0, time.UTC)
	to := from.AddDate(0, 0, 2)
	originalID := uuid.New()

	usage := meterUsageReading(organizationID, metering.MeterAgentSessionStorage, 500, from.Add(time.Hour), nil, map[string]string{})
	negative := meterUsageReading(organizationID, metering.MeterAgentSessionStorage, -100, from.Add(2*time.Hour), &originalID, map[string]string{})
	positive := meterUsageReading(organizationID, metering.MeterAgentSessionStorage, 100, from.Add(25*time.Hour), &originalID, map[string]string{})
	sortedGroups := meterUsageReading(organizationID, metering.MeterAgentSessionStorage, 25, from.Add(3*time.Hour), nil, map[string]string{
		metering.AttributeBillingUserDirectoryGroups: `["Support","Engineering","Support"]`,
	})
	require.NoError(t, queries.InsertReadings(t.Context(), []chrepo.ReadingRow{usage, negative, positive, sortedGroups}))

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

func TestGetUsageRejectsMixedMeasurementOutsideTopSix(t *testing.T) {
	t.Parallel()
	conn := newTestClickhouse(t)
	queries := chrepo.New(conn)
	organizationID := "org-" + uuid.NewString()
	from := time.Date(2026, time.April, 1, 0, 0, 0, 0, time.UTC)
	rows := make([]chrepo.ReadingRow, 0, 8)
	for index := range 7 {
		rows = append(rows, meterUsageReading(organizationID, metering.MeterAgentSessionStorage, int64(100-index), from.Add(time.Duration(index+1)*time.Minute), nil, map[string]string{
			metering.AttributeModel: string(rune('a' + index)),
		}))
	}
	incompatible := meterUsageReading(organizationID, metering.MeterAgentSessionStorage, 1, from.Add(time.Hour), nil, map[string]string{
		metering.AttributeModel: "loser",
	})
	incompatible.MeasurementMethod = "incompatible"
	rows = append(rows, incompatible)
	require.NoError(t, queries.InsertReadings(t.Context(), rows))

	_, err := queries.GetUsage(t.Context(), chrepo.UsageParams{
		OrganizationID: organizationID,
		Selection:      storageUsageSelection(t, "model"),
		From:           from,
		To:             from.AddDate(0, 0, 1),
		ReadingKind:    chrepo.ReadingKindUsage,
	})
	require.ErrorIs(t, err, chrepo.ErrMixedMeasurement)
}

func TestGetUsageRanksAdjustmentsByAbsoluteNetAndPreservesZeroNetActivity(t *testing.T) {
	t.Parallel()
	conn := newTestClickhouse(t)
	queries := chrepo.New(conn)
	organizationID := "org-" + uuid.NewString()
	from := time.Date(2026, time.May, 1, 0, 0, 0, 0, time.UTC)
	originalID := uuid.New()

	rows := make([]chrepo.ReadingRow, 0, 12)
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
	rows = append(rows,
		meterUsageReading(organizationID, metering.MeterAgentSessionStorage, 1000, from.Add(7*time.Hour), &originalID, map[string]string{metering.AttributeModel: "noisy"}),
		meterUsageReading(organizationID, metering.MeterAgentSessionStorage, -1000, from.Add(8*time.Hour), &originalID, map[string]string{metering.AttributeModel: "noisy"}),
		meterUsageReading(organizationID, metering.MeterAgentSessionStorage, 5, from.Add(9*time.Hour), &originalID, map[string]string{metering.AttributeModel: "omitted"}),
		meterUsageReading(organizationID, metering.MeterAgentSessionStorage, -5, from.Add(10*time.Hour), &originalID, map[string]string{metering.AttributeModel: "omitted"}),
	)
	require.NoError(t, queries.InsertReadings(t.Context(), rows))

	result, err := queries.GetUsage(t.Context(), chrepo.UsageParams{
		OrganizationID: organizationID,
		Selection:      storageUsageSelection(t, "model"),
		From:           from,
		To:             from.AddDate(0, 0, 1),
		ReadingKind:    chrepo.ReadingKindAdjustment,
	})
	require.NoError(t, err)
	require.Len(t, result.Rows, 7)
	require.Equal(t, "remainder", result.Rows[0].Kind)
	require.Equal(t, "0", result.Rows[0].Total)
}

func TestGetUsageRanksAcrossTheWholePeriod(t *testing.T) {
	t.Parallel()
	conn := newTestClickhouse(t)
	queries := chrepo.New(conn)
	organizationID := "org-" + uuid.NewString()
	from := time.Date(2026, time.June, 1, 0, 0, 0, 0, time.UTC)
	rows := make([]chrepo.ReadingRow, 0, 15)
	for _, model := range []string{"a0", "a1", "a2", "a3", "a4", "a5", "b0", "b1", "b2", "b3", "b4", "b5"} {
		day := from
		if model[0] == 'b' {
			day = day.AddDate(0, 0, 1)
		}
		rows = append(rows, meterUsageReading(organizationID, metering.MeterAgentSessionStorage, 100, day.Add(time.Hour), nil, map[string]string{metering.AttributeModel: model}))
	}
	for day := range 2 {
		rows = append(rows, meterUsageReading(organizationID, metering.MeterAgentSessionStorage, 99, from.AddDate(0, 0, day).Add(2*time.Hour), nil, map[string]string{metering.AttributeModel: "period"}))
	}
	rows = append(rows, meterUsageReading(organizationID, metering.MeterAgentSessionStorage, 500, from.AddDate(0, 0, 1).Add(3*time.Hour), nil, map[string]string{metering.AttributeModel: "live"}))
	require.NoError(t, queries.InsertReadings(t.Context(), rows))

	result, err := queries.GetUsage(t.Context(), chrepo.UsageParams{
		OrganizationID: organizationID,
		Selection:      storageUsageSelection(t, "model"),
		From:           from,
		To:             from.AddDate(0, 0, 2),
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

func TestGetUsageReturnsEmptyWithoutCoverageMarkers(t *testing.T) {
	t.Parallel()
	conn := newTestClickhouse(t)
	from := time.Date(2026, time.July, 1, 0, 0, 0, 0, time.UTC)

	result, err := chrepo.New(conn).GetUsage(t.Context(), chrepo.UsageParams{
		OrganizationID: "org-" + uuid.NewString(),
		Selection:      storageUsageSelection(t, "total"),
		From:           from,
		To:             from.AddDate(0, 0, 1),
		ReadingKind:    chrepo.ReadingKindUsage,
	})
	require.NoError(t, err)
	require.Empty(t, result.Rows)
	require.Equal(t, "stokens", result.Unit)
	require.Equal(t, "tiktoken_o200k_base", result.MeasurementMethod)
}

func storageUsageSelection(t *testing.T, breakdown string) chrepo.UsageSelection {
	t.Helper()
	_, selection, err := metering.ResolveUsageSelection(metering.UsageFamilyAgentSessionStorage, breakdown)
	require.NoError(t, err)
	return selection
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
