package chrepo

import (
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
	"google.golang.org/protobuf/proto"

	sigintv1 "github.com/speakeasy-api/gram/infra/gen/gram/sigint/v1"
	"github.com/speakeasy-api/gram/server/internal/sigint/readings"
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
		FROM sigint_readings_v2 FINAL
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
		FROM sigint_readings_v2 FINAL
		WHERE organization_id = ? AND project_id = ? AND sensor_id = ?
		SETTINGS max_execution_time = 10, max_rows_to_read = 100000
	`, first.OrganizationID, first.ProjectID, first.SensorID).Scan(&nulls, &zeroes))
	require.Equal(t, uint64(1), nulls)
	require.Equal(t, uint64(1), zeroes)
}

// landedStatus reads the converged row for a logical reading.
func landedStatus(t *testing.T, queries *Queries, row ReadingRow) (rows uint64, status, reason, resultType string, probabilities uint64) {
	t.Helper()
	require.NoError(t, queries.conn.QueryRow(t.Context(), `
		SELECT count() OVER (), status, failure_reason, result_type, toUInt64(length(probabilities))
		FROM sigint_readings_v2 FINAL
		WHERE organization_id = ? AND project_id = ? AND sensor_id = ? AND id = ?
		LIMIT 1 SETTINGS max_execution_time = 10, max_rows_to_read = 100000
	`, row.OrganizationID, row.ProjectID, row.SensorID, row.ID).Scan(&rows, &status, &reason, &resultType, &probabilities))
	return rows, status, reason, resultType, probabilities
}

func TestLandingLaterAnswerReplacesEarlierFailure(t *testing.T) {
	t.Parallel()
	queries := New(testConnection(t))
	now := time.Now().UTC()
	at := now.Truncate(time.Hour).Add(-3 * time.Hour)
	reading := testReading(t, at)
	reading.SetEvaluatedAt(at.Add(time.Minute).Format(time.RFC3339Nano))
	failure := testOutcome(t, reading, at.Add(time.Second), sigintv1.Outcome_STATUS_FAILED, "classifier_rejected")
	answered, err := ParseReading(reading, now)
	require.NoError(t, err)
	failed, err := ParseOutcome(failure, now)
	require.NoError(t, err)
	// The stale failure arrives after the newer answer; evaluation time decides.
	require.NoError(t, queries.InsertReadings(t.Context(), []ReadingRow{answered}))
	require.NoError(t, queries.InsertReadings(t.Context(), []ReadingRow{failed}))
	rows, status, reason, resultType, probabilities := landedStatus(t, queries, answered)
	require.Equal(t, uint64(1), rows)
	require.Equal(t, StatusAnswered, status)
	require.Empty(t, reason)
	require.Equal(t, "multi_label", resultType)
	require.Equal(t, uint64(2), probabilities)
}

func TestLandingLaterFailureReplacesEarlierAnswer(t *testing.T) {
	t.Parallel()
	queries := New(testConnection(t))
	now := time.Now().UTC()
	at := now.Truncate(time.Hour).Add(-3 * time.Hour)
	reading := testReading(t, at)
	failure := testOutcome(t, reading, at.Add(time.Minute), sigintv1.Outcome_STATUS_FAILED, "content_too_large")
	answered, err := ParseReading(reading, now)
	require.NoError(t, err)
	failed, err := ParseOutcome(failure, now)
	require.NoError(t, err)
	require.NoError(t, queries.InsertReadings(t.Context(), []ReadingRow{failed}))
	require.NoError(t, queries.InsertReadings(t.Context(), []ReadingRow{answered}))
	rows, status, reason, resultType, probabilities := landedStatus(t, queries, answered)
	require.Equal(t, uint64(1), rows)
	require.Equal(t, StatusFailed, status)
	require.Equal(t, "content_too_large", reason)
	require.Empty(t, resultType)
	require.Zero(t, probabilities)
}

func TestLandingAcceptsEveryOutcomeStatus(t *testing.T) {
	t.Parallel()
	queries := New(testConnection(t))
	now := time.Now().UTC()
	at := now.Truncate(time.Hour).Add(-time.Hour)
	var rows []ReadingRow
	for _, tc := range []struct {
		status sigintv1.Outcome_Status
		reason string
	}{
		{sigintv1.Outcome_STATUS_NOT_APPLICABLE, ""},
		{sigintv1.Outcome_STATUS_DRAFT, ""},
		{sigintv1.Outcome_STATUS_FAILED, "match_compile"},
	} {
		o := testOutcome(t, testReading(t, at), at, tc.status, tc.reason)
		if tc.status == sigintv1.Outcome_STATUS_DRAFT {
			o.SetDefinitionHash("")
		}
		row, err := ParseOutcome(o, now)
		require.NoError(t, err)
		rows = append(rows, row)
	}
	// Every outcome status lands, with its reason and an empty result.
	require.NoError(t, queries.InsertReadings(t.Context(), rows))
	for _, row := range rows {
		landed, status, _, _, _ := landedStatus(t, queries, row)
		require.Equal(t, uint64(1), landed)
		require.Equal(t, row.Status, status)
	}
}

func TestLandingKeepsManualRunsIndependentOfStreaming(t *testing.T) {
	t.Parallel()
	conn := testConnection(t)
	queries := New(conn)
	now := time.Now().UTC()
	base := testReading(t, now.AddDate(-3, 0, 0))
	runA, runB := uuid.New(), uuid.New()
	var messages []*sigintv1.Reading
	var attempts []ReadingRow
	for _, execution := range []readings.Context{
		{TriggerKind: readings.TriggerStream, RequestedAt: now.Add(-3 * time.Hour)},
		{TriggerKind: readings.TriggerManual, RunID: runA, RequestedAt: now.Add(-2 * time.Hour)},
		{TriggerKind: readings.TriggerManual, RunID: runB, RequestedAt: now.Add(-time.Hour)},
	} {
		m := proto.CloneOf(base)
		m.SetId(execution.ReadingID(m.GetOrganizationId(), m.GetProjectId(), m.GetEvent().GetKind(), m.GetEvent().GetId(), m.GetSensorId()).String())
		m.SetTriggerKind(string(execution.TriggerKind))
		m.SetEvaluationRequestedAt(execution.RequestedAt.Format(time.RFC3339Nano))
		m.SetEvaluatedAt(execution.RequestedAt.Add(time.Second).Format(time.RFC3339Nano))
		if execution.RunID != uuid.Nil {
			m.SetRunId(execution.RunID.String())
		}
		row, err := ParseReading(m, now)
		require.NoError(t, err)
		require.Equal(t, string(execution.TriggerKind), row.TriggerKind)
		require.Equal(t, execution.RunID, row.RunID)
		require.Equal(t, execution.RequestedAt, row.EvaluationRequestedAt)
		messages = append(messages, m)
		attempts = append(attempts, row)
	}
	failure := testOutcome(t, messages[1], now, sigintv1.Outcome_STATUS_FAILED, "classifier_rejected")
	failed, err := ParseOutcome(failure, now)
	require.NoError(t, err)
	require.Equal(t, attempts[1].RunID, failed.RunID)
	require.Equal(t, attempts[1].EvaluationRequestedAt, failed.EvaluationRequestedAt)
	// A later manual failure arrives first, followed by duplicate/stale answers.
	require.NoError(t, queries.InsertReadings(t.Context(), []ReadingRow{failed, failed}))
	require.NoError(t, queries.InsertReadings(t.Context(), append(attempts, attempts[1])))
	for i, row := range attempts {
		rows, status, _, _, _ := landedStatus(t, queries, row)
		require.Equal(t, uint64(1), rows)
		if i == 1 {
			require.Equal(t, StatusFailed, status)
		} else {
			require.Equal(t, StatusAnswered, status)
		}
		var trigger string
		var run uuid.UUID
		var requested time.Time
		require.NoError(t, conn.QueryRow(t.Context(), `
			SELECT trigger_kind, run_id, evaluation_requested_at FROM sigint_readings_v2 FINAL
			WHERE organization_id = ? AND project_id = ? AND trigger_kind = ? AND run_id = ? AND sensor_id = ? AND id = ?
			LIMIT 1 SETTINGS max_execution_time = 10, max_rows_to_read = 100000
		`, row.OrganizationID, row.ProjectID, row.TriggerKind, row.RunID, row.SensorID, row.ID).Scan(&trigger, &run, &requested))
		require.Equal(t, row.TriggerKind, trigger)
		require.Equal(t, row.RunID, run)
		require.Equal(t, row.EvaluationRequestedAt, requested)
	}
}
