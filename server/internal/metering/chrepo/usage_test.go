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

	_, err := queries.GetUsage(t.Context(), chrepo.UsageParams{
		OrganizationID: organizationID,
		Selection:      storageUsageSelection(t, "total"),
		From:           from,
		To:             from.Add(24 * time.Hour),
		ReadingKind:    chrepo.ReadingKindUsage,
	})
	require.ErrorIs(t, err, chrepo.ErrMixedMeasurement)
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
