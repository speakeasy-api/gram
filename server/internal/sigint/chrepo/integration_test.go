package chrepo

import (
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
	"google.golang.org/protobuf/proto"

	sigintv1 "github.com/speakeasy-api/gram/infra/gen/gram/sigint/v1"
)

func TestLandingReplacesCompleteAttemptsByEvaluationTime(t *testing.T) {
	t.Parallel()
	conn := testConnection(t)
	queries := New(conn)
	now := time.Now().UTC()
	at := now.Truncate(time.Hour).Add(-2*time.Hour + 15*time.Second + 123*time.Nanosecond)
	first := testReading(t, at)
	first.SetAttributes(map[string]string{"quoted':\\\n世界": "value':\\\n", "empty": ""})
	first.SetInput(sigintv1.Reading_Input_builder{Strategy: new("sliding_window"), RequestedMessageCount: new(int32(5)), MessageCount: new(int32(5))}.Build())
	latest := proto.CloneOf(first)
	latest.SetEvaluationAttemptId(uuid.NewString())
	latest.SetEvaluatedAt(at.Add(2 * time.Second).Format(time.RFC3339Nano))
	latest.SetDefinitionHash("example-definition-v2")
	latest.GetMultiLabel().SetSignals(latest.GetMultiLabel().GetSignals()[:1])
	latest.GetMultiLabel().GetSignals()[0].SetProbability(0.8)
	latest.SetSensorSlug("renamed-sensor")
	latest.SetInput(sigintv1.Reading_Input_builder{Strategy: new("sliding_window"), RequestedMessageCount: new(int32(5)), MessageCount: new(int32(2)), Truncated: new(true)}.Build())
	oldRow, err := ParseReading(first, now)
	require.NoError(t, err)
	newRow, err := ParseReading(latest, now)
	require.NoError(t, err)
	// Older deliveries arrive last and twice. Source time, not delivery time,
	// pins their partition, so replacement still converges on the latest attempt.
	require.NoError(t, queries.InsertReadings(t.Context(), []ReadingRow{newRow, oldRow, oldRow}))
	var rows, length uint64
	var slug, definition, status string
	var supplied uint8
	var truncated bool
	var labels map[string]string
	require.NoError(t, conn.QueryRow(t.Context(), `
		SELECT count() OVER (), toUInt64(length(probabilities)), sensor_slug, definition_hash, status,
			input_message_count, input_truncated, attributes
		FROM sigint_readings FINAL
		WHERE organization_id = ? AND project_id = ? AND sensor_id = ? AND id = ?
		LIMIT 1 SETTINGS max_execution_time = 10, max_rows_to_read = 100000
	`, newRow.OrganizationID, newRow.ProjectID, newRow.SensorID, newRow.ID).Scan(&rows, &length, &slug, &definition, &status, &supplied, &truncated, &labels))
	require.Equal(t, uint64(1), rows)
	require.Equal(t, uint64(1), length)
	require.Equal(t, "renamed-sensor", slug)
	require.Equal(t, "example-definition-v2", definition)
	require.Equal(t, StatusAnswered, status)
	require.Equal(t, uint8(2), supplied)
	require.True(t, truncated)
	require.Equal(t, first.GetAttributes(), labels)
}

func TestLandingKeepsAbsentAndZeroConfidenceDistinct(t *testing.T) {
	t.Parallel()
	conn := testConnection(t)
	queries := New(conn)
	now := time.Now().UTC()
	at := now.Truncate(time.Hour).Add(-time.Hour + time.Minute)
	absent := testReading(t, at)
	p := absent.GetMultiLabel().GetSignals()
	absent.SetChoice(sigintv1.Reading_Choice_builder{SelectedSignalId: new(p[1].GetSignalId()), SelectedSignalSlug: new(p[1].GetSignalSlug()), Distribution: p}.Build())
	present := proto.CloneOf(absent)
	present.SetId(uuid.NewString())
	present.SetEvaluationAttemptId(uuid.NewString())
	present.GetEvent().SetId("another-event")
	present.GetChoice().SetConfidence(0)
	first, err := ParseReading(absent, now)
	require.NoError(t, err)
	second, err := ParseReading(present, now)
	require.NoError(t, err)
	require.NoError(t, queries.InsertReadings(t.Context(), []ReadingRow{first, second}))
	var nulls, zeroes uint64
	require.NoError(t, conn.QueryRow(t.Context(), `
		SELECT countIf(confidence IS NULL), countIf(confidence = 0)
		FROM sigint_readings FINAL
		WHERE organization_id = ? AND project_id = ? AND sensor_id = ?
		SETTINGS max_execution_time = 10, max_rows_to_read = 100000
	`, first.OrganizationID, first.ProjectID, first.SensorID).Scan(&nulls, &zeroes))
	require.Equal(t, uint64(1), nulls)
	require.Equal(t, uint64(1), zeroes)
}
