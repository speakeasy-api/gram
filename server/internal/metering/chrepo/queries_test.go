package chrepo_test

import (
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/metering/chrepo"
)

func TestInsertReadingsPersistsAndDeduplicatesStableIDs(t *testing.T) {
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
		Value:             10,
		OccurredAt:        firstOccurredAt,
		ProducedAt:        producedAt,
		InsertedAt:        firstInsertedAt,
		CorrectsReadingID: nil,
		Attributes:        map[string]string{"codec": "tiktoken_o200k_base", "source": "first"},
	}
	redelivery := first
	redelivery.OccurredAt = secondOccurredAt
	redelivery.InsertedAt = secondInsertedAt
	redelivery.Attributes = map[string]string{"codec": "tiktoken_o200k_base", "source": "redelivery"}
	correction := chrepo.ReadingRow{
		ID:                correctionID,
		OrganizationID:    organizationID,
		ProjectID:         projectID,
		MeterID:           "gram.agent_session.storage",
		OperationID:       first.OperationID + ":correction",
		Unit:              "stokens",
		Value:             -4,
		OccurredAt:        secondOccurredAt,
		ProducedAt:        adjustmentProducedAt,
		InsertedAt:        secondInsertedAt,
		CorrectsReadingID: &readingID,
		Attributes:        map[string]string{"codec": "tiktoken_o200k_base"},
	}

	require.NoError(t, queries.InsertReadings(t.Context(), nil))
	require.NoError(t, queries.InsertReadings(t.Context(), []chrepo.ReadingRow{first}))
	require.NoError(t, queries.InsertReadings(t.Context(), []chrepo.ReadingRow{redelivery, correction}))

	var (
		operationID, unit, corrects  string
		value                        int64
		occurredAt, storedProducedAt time.Time
		storedInsertedAt             time.Time
		attributes                   map[string]string
	)
	err := conn.QueryRow(t.Context(), `
		SELECT operation_id, toString(unit), value, occurred_at, produced_at, inserted_at,
		       ifNull(toString(corrects_reading_id), ''), attributes
		FROM billing_meter_readings FINAL
		WHERE organization_id = ? AND project_id = ? AND meter_id = ? AND id = ?
	`, organizationID, projectID, first.MeterID, readingID).Scan(
		&operationID,
		&unit,
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
	require.Equal(t, int64(10), value)
	require.Equal(t, secondOccurredAt, occurredAt)
	require.Equal(t, producedAt, storedProducedAt)
	require.Equal(t, secondInsertedAt, storedInsertedAt)
	require.Empty(t, corrects)
	require.Equal(t, "redelivery", attributes["source"])

	var partitionCount uint64
	require.NoError(t, conn.QueryRow(t.Context(), `
		SELECT uniqExact(_partition_id)
		FROM billing_meter_readings
		WHERE organization_id = ? AND project_id = ? AND id = ?
	`, organizationID, projectID, readingID).Scan(&partitionCount))
	require.Equal(t, uint64(1), partitionCount)

	var rowCount uint64
	var net int64
	require.NoError(t, conn.QueryRow(t.Context(), `
		SELECT count(), sum(value)
		FROM billing_meter_readings FINAL
		WHERE organization_id = ? AND project_id = ? AND meter_id = ?
	`, organizationID, projectID, first.MeterID).Scan(&rowCount, &net))
	require.Equal(t, uint64(2), rowCount)
	require.Equal(t, int64(6), net)

	var storedCorrectionID string
	var storedCorrectionValue int64
	require.NoError(t, conn.QueryRow(t.Context(), `
		SELECT toString(corrects_reading_id), value
		FROM billing_meter_readings FINAL
		WHERE organization_id = ? AND project_id = ? AND id = ?
	`, organizationID, projectID, correctionID).Scan(&storedCorrectionID, &storedCorrectionValue))
	require.Equal(t, readingID.String(), storedCorrectionID)
	require.Equal(t, int64(-4), storedCorrectionValue)
}
