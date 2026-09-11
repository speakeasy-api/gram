package chrepo_test

import (
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/metering/chrepo"
)

func TestInsertReadingsPreservesUsageAndSeparateAdjustments(t *testing.T) {
	t.Parallel()
	conn := newTestClickhouse(t)
	queries := chrepo.New(conn)
	organizationID := "org-" + uuid.NewString()
	projectID := uuid.New()
	readingID := uuid.New()
	correctionID := uuid.New()
	producedAt := time.Now().UTC().Add(-3 * time.Second)
	adjustmentProducedAt := producedAt.Add(time.Second)
	firstInsertedAt := producedAt.Add(2 * time.Second)
	secondInsertedAt := firstInsertedAt.Add(time.Second)
	firstOccurredAt := time.Date(2026, time.January, 15, 1, 2, 3, 4, time.UTC)
	secondOccurredAt := time.Date(2026, time.February, 15, 1, 2, 3, 4, time.UTC)

	first := chrepo.ReadingRow{
		ID:                readingID,
		OrganizationID:    organizationID,
		ProjectID:         projectID,
		MeterID:           "gram.agent_session.storage",
		OperationID:       "chat_message:" + uuid.NewString(),
		Unit:              "stokens",
		MeasurementMethod: "tiktoken_o200k_base",
		Value:             10,
		OccurredAt:        firstOccurredAt,
		ProducedAt:        producedAt,
		InsertedAt:        firstInsertedAt,
		CorrectsReadingID: nil,
		Attributes:        map[string]string{"codec": "tiktoken_o200k_base", "source": "first"},
	}
	correction := chrepo.ReadingRow{
		ID:                correctionID,
		OrganizationID:    organizationID,
		ProjectID:         projectID,
		MeterID:           "gram.agent_session.storage",
		OperationID:       first.OperationID + ":correction",
		Unit:              "stokens",
		MeasurementMethod: "tiktoken_o200k_base",
		Value:             -4,
		OccurredAt:        secondOccurredAt,
		ProducedAt:        adjustmentProducedAt,
		InsertedAt:        secondInsertedAt,
		CorrectsReadingID: &readingID,
		Attributes:        map[string]string{"codec": "tiktoken_o200k_base"},
	}

	require.NoError(t, queries.InsertReadings(t.Context(), nil))
	require.NoError(t, queries.InsertReadings(t.Context(), []chrepo.ReadingRow{first}))
	require.NoError(t, queries.InsertReadings(t.Context(), []chrepo.ReadingRow{correction}))

	var (
		operationID, unit, measurementMethod, corrects string
		value                                          int64
		occurredAt, storedProducedAt                   time.Time
		storedInsertedAt                               time.Time
		attributes                                     map[string]string
	)
	err := conn.QueryRow(t.Context(), `
		SELECT operation_id, toString(unit), toString(measurement_method), value,
		       occurred_at, produced_at, inserted_at, ifNull(toString(corrects_reading_id), ''), attributes
		FROM billing_meter_readings_by_time
		WHERE organization_id = ? AND project_id = ? AND meter_id = ? AND id = ?
	`, organizationID, projectID, first.MeterID, readingID).Scan(
		&operationID,
		&unit,
		&measurementMethod,
		&value,
		&occurredAt,
		&storedProducedAt,
		&storedInsertedAt,
		&corrects,
		&attributes,
	)
	require.NoError(t, err)
	require.Equal(t, first.OperationID, operationID)
	require.Equal(t, "stokens", unit)
	require.Equal(t, "tiktoken_o200k_base", measurementMethod)
	require.Equal(t, int64(10), value)
	require.Equal(t, firstOccurredAt, occurredAt)
	require.Equal(t, producedAt, storedProducedAt)
	require.Equal(t, firstInsertedAt, storedInsertedAt)
	require.Empty(t, corrects)
	require.Equal(t, "first", attributes["source"])

	var usageCount uint64
	var usageValue int64
	require.NoError(t, conn.QueryRow(t.Context(), `
		SELECT count(), sum(value)
		FROM billing_meter_readings_by_time
		WHERE organization_id = ? AND meter_id = ? AND reading_kind = 'usage'
		  AND occurred_at >= ? AND occurred_at < ?
	`, organizationID, first.MeterID,
		time.Date(2026, time.January, 1, 0, 0, 0, 0, time.UTC),
		time.Date(2026, time.February, 1, 0, 0, 0, 0, time.UTC),
	).Scan(&usageCount, &usageValue))
	require.Equal(t, uint64(1), usageCount)
	require.Equal(t, int64(10), usageValue)

	var oldLedgerCount uint64
	require.NoError(t, conn.QueryRow(t.Context(), `
		SELECT count() FROM billing_meter_readings
		WHERE organization_id = ?
	`, organizationID).Scan(&oldLedgerCount))
	require.Zero(t, oldLedgerCount)

	var storedCorrectionID string
	var storedCorrectionValue int64
	require.NoError(t, conn.QueryRow(t.Context(), `
		SELECT toString(corrects_reading_id), value
		FROM billing_meter_readings_by_time
		WHERE organization_id = ? AND project_id = ? AND id = ? AND reading_kind = 'adjustment'
	`, organizationID, projectID, correctionID).Scan(&storedCorrectionID, &storedCorrectionValue))
	require.Equal(t, readingID.String(), storedCorrectionID)
	require.Equal(t, int64(-4), storedCorrectionValue)
}

func TestInsertReadingsAcceptsBandwidthMeasurements(t *testing.T) {
	t.Parallel()
	conn := newTestClickhouse(t)
	queries := chrepo.New(conn)
	row := chrepo.ReadingRow{
		ID:                uuid.New(),
		OrganizationID:    "org-" + uuid.NewString(),
		ProjectID:         uuid.New(),
		MeterID:           "gram.mcp.bandwidth.egress",
		OperationID:       "mcp-http-exchange:" + uuid.NewString(),
		Unit:              "bytes",
		MeasurementMethod: "http_body_bytes",
		Value:             8192,
		OccurredAt:        time.Now().UTC(),
		ProducedAt:        time.Now().UTC(),
		InsertedAt:        time.Now().UTC(),
		Attributes:        map[string]string{},
	}

	require.NoError(t, queries.InsertReadings(t.Context(), []chrepo.ReadingRow{row}))

	var unit, measurementMethod string
	var value int64
	require.NoError(t, conn.QueryRow(t.Context(), `
		SELECT toString(unit), toString(measurement_method), value
		FROM billing_meter_readings_by_time
		WHERE organization_id = ? AND project_id = ? AND id = ?
	`, row.OrganizationID, row.ProjectID, row.ID).Scan(&unit, &measurementMethod, &value))
	require.Equal(t, "bytes", unit)
	require.Equal(t, "http_body_bytes", measurementMethod)
	require.Equal(t, int64(8192), value)
}
