package chrepo

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/o11y"
)

func TestRunScopedReadingsIsolateRunsAndReplaceCompleteAttempts(t *testing.T) {
	t.Parallel()
	ctx := t.Context()
	conn := testConnection(t)
	now := time.Now().UTC()
	requested := time.Date(now.Year(), now.Month()-1, 15, 12, 0, 0, 0, time.UTC)
	row, err := ParseReading(testReading(t, now.AddDate(-3, 0, 0)), now)
	require.NoError(t, err)
	runA, runB := uuid.New(), uuid.New()
	batch, err := conn.PrepareBatch(ctx, `INSERT INTO sigint_readings_v2 (
		id, evaluation_attempt_id, organization_id, project_id, trigger_kind, run_id,
		evaluation_requested_at, sensor_id, sensor_slug, event_kind, event_id,
		event_occurred_at, evaluated_at, definition_hash, status, failure_reason,
		result_type, probabilities
	)`)
	require.NoError(t, err)
	defer o11y.NoLogDefer(batch.Abort)

	// Reuse the ID deliberately to exercise the table's scope columns independently
	// of the producer's run-scoped hash. Newer failures arrive before stale answers.
	for _, attempt := range []struct {
		trigger string
		run     uuid.UUID
		at      time.Time
		status  string
	}{
		{trigger: "stream", run: uuid.Nil, at: requested.Add(time.Second), status: StatusAnswered},
		{trigger: "manual", run: runA, at: now, status: StatusFailed},
		{trigger: "manual", run: runB, at: requested.Add(time.Second), status: StatusAnswered},
		{trigger: "manual", run: runA, at: now, status: StatusFailed},
		{trigger: "manual", run: runA, at: requested.Add(time.Second), status: StatusAnswered},
	} {
		reason, resultType, probabilities := "", row.ResultType, row.Probabilities
		if attempt.status == StatusFailed {
			reason, resultType, probabilities = "classifier_rejected", "", nil
		}
		require.NoError(t, batch.Append(
			row.ID, uuid.New(), row.OrganizationID, row.ProjectID, attempt.trigger, attempt.run,
			requested, row.SensorID, row.SensorSlug, row.EventKind, row.EventID,
			row.EventOccurredAt, attempt.at, row.DefinitionHash, attempt.status, reason,
			resultType, probabilities,
		))
	}
	require.NoError(t, batch.Send())

	rows, err := conn.Query(ctx, `
		SELECT trigger_kind, run_id, status, failure_reason, result_type,
			toUInt64(length(probabilities)), evaluation_requested_at, _partition_id
		FROM sigint_readings_v2 FINAL
		WHERE organization_id = ? AND project_id = ? AND sensor_id = ?
		LIMIT 10 SETTINGS max_execution_time = 10, max_rows_to_read = 100000
	`, row.OrganizationID, row.ProjectID, row.SensorID)
	require.NoError(t, err)
	defer o11y.NoLogDefer(rows.Close)
	landed := make(map[string]string)
	for rows.Next() {
		var trigger, status, reason, resultType, partition string
		var run uuid.UUID
		var probabilities uint64
		var requestTime time.Time
		require.NoError(t, rows.Scan(&trigger, &run, &status, &reason, &resultType, &probabilities, &requestTime, &partition))
		key := trigger + "/" + run.String()
		require.NotContains(t, landed, key)
		landed[key] = status
		require.Equal(t, requested, requestTime)
		require.Equal(t, requested.Format("200601"), partition, "cross-month retries retain their request partition")
		if status == StatusFailed {
			require.Equal(t, "classifier_rejected", reason)
			require.Empty(t, resultType)
			require.Zero(t, probabilities)
		} else {
			require.Empty(t, reason)
			require.Equal(t, row.ResultType, resultType)
			require.Equal(t, uint64(len(row.Probabilities)), probabilities)
		}
	}
	require.NoError(t, rows.Err())
	require.Equal(t, map[string]string{
		"stream/" + uuid.Nil.String(): StatusAnswered,
		"manual/" + runA.String():     StatusFailed,
		"manual/" + runB.String():     StatusAnswered,
	}, landed)
}

func TestRunScopedReadingsRetainByRequestTimeForEveryTrigger(t *testing.T) {
	t.Parallel()
	ctx := t.Context()
	conn := testConnection(t)
	// Isolate TTL materialization from the other tests sharing the source table.
	table := "sigint_readings_v2_ttl_" + strings.ReplaceAll(uuid.NewString(), "-", "")
	require.NoError(t, conn.Exec(ctx, "CREATE TABLE "+table+" AS sigint_readings_v2"))
	t.Cleanup(func() {
		require.NoError(t, conn.Exec(context.Background(), "DROP TABLE "+table))
	})
	now := time.Now().UTC()
	row, err := ParseReading(testReading(t, now.AddDate(-4, 0, 0)), now)
	require.NoError(t, err)
	for _, trigger := range []string{"stream", "manual"} {
		for _, requested := range []time.Time{now, now.AddDate(-3, 0, 0)} {
			run := uuid.Nil
			if trigger == "manual" {
				run = uuid.New()
			}
			require.NoError(t, conn.Exec(ctx, `INSERT INTO `+table+` (
				id, evaluation_attempt_id, organization_id, project_id, trigger_kind,
				run_id, evaluation_requested_at, sensor_id, sensor_slug, event_kind,
				event_id, event_occurred_at, evaluated_at, definition_hash, status, failure_reason
			) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
				uuid.New(), uuid.New(), row.OrganizationID, row.ProjectID, trigger,
				run, requested, row.SensorID, row.SensorSlug, row.EventKind,
				row.EventID, row.EventOccurredAt, now, row.DefinitionHash, StatusFailed, "classifier_rejected",
			))
		}
	}

	// Expire old requests synchronously: recent attempts cannot extend their TTL,
	// while recent requests survive even though the evaluated message is old.
	require.NoError(t, conn.Exec(ctx, "ALTER TABLE "+table+" MATERIALIZE TTL SETTINGS mutations_sync = 1"))
	var rows []struct {
		TriggerKind string `ch:"trigger_kind"`
	}
	require.NoError(t, conn.Select(ctx, &rows, `
		SELECT trigger_kind FROM `+table+` FINAL
		WHERE organization_id = ? AND project_id = ?
		ORDER BY trigger_kind
		LIMIT 10 SETTINGS max_execution_time = 10, max_rows_to_read = 100000
	`, row.OrganizationID, row.ProjectID))
	require.Len(t, rows, 2)
	require.Equal(t, "manual", rows[0].TriggerKind)
	require.Equal(t, "stream", rows[1].TriggerKind)
}
