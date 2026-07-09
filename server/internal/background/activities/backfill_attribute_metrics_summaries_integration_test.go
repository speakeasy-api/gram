package activities_test

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/ClickHouse/clickhouse-go/v2"
	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/background/activities"
	telemetryrepo "github.com/speakeasy-api/gram/server/internal/telemetry/repo"
	"github.com/speakeasy-api/gram/server/internal/testenv"
)

// TestBackfillAttributeMetricsSummaries_FullLifecycle drives the real
// activities against ClickHouse through the whole staging flow: raw Claude
// logs (including pre-cutoff history and legacy hook_source spellings) are
// replayed through the Null-engine feed, validated, archived, and swapped
// into the live table. All operations are tenant-scoped, so the shared test
// database is safe.
func TestBackfillAttributeMetricsSummaries_FullLifecycle(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	logger := testenv.NewLogger(t)

	chConn, err := infra.NewClickhouseClient(t)
	require.NoError(t, err)

	projectID := uuid.NewString()
	boundary := time.Now().UTC().Truncate(time.Hour)

	// Raw history for the tenant. The live MV only ingests rows at/after its
	// 2026-06-20 cutoff, so preCutoff exists only in raw logs until the
	// backfill extends live history backwards. postCutoff rows flow through
	// the live MV at insert time as well.
	preCutoff := time.Date(2026, time.June, 10, 12, 30, 0, 0, time.UTC)
	postCutoff := time.Date(2026, time.June, 25, 9, 15, 0, 0, time.UTC)

	// Legacy spelling: the row is admitted as Claude by provenance (the OTEL
	// URN) while hook_source passes through as the stamped "claude".
	insertBackfillClaudeAPIRequestLog(t, ctx, chConn, projectID, preCutoff, "session-pre", 0.25, 100, "claude")
	// Empty hook_source: provenance still admits the row; the dimension stays
	// empty.
	insertBackfillClaudeAPIRequestLog(t, ctx, chConn, projectID, postCutoff, "session-post", 0.50, 200, "")
	// Non-Claude row: classified as cursor by its usage URN.
	insertBackfillCursorUsageLog(t, ctx, chConn, projectID, postCutoff.Add(5*time.Minute), "session-cursor", 0.10, 40)
	// Claude tool call via OTEL tool_result, re-emitted with the same
	// tool_use_id: total_tool_calls counts both rows, unique_tool_calls dedups
	// to one call.
	toolUseID := uuid.NewString()
	insertBackfillClaudeToolResultLog(t, ctx, chConn, projectID, postCutoff.Add(10*time.Minute), "session-post", toolUseID, "Bash")
	insertBackfillClaudeToolResultLog(t, ctx, chConn, projectID, postCutoff.Add(11*time.Minute), "session-post", toolUseID, "Bash")

	// A stale live aggregate inside the rebuild window (e.g. produced by old
	// MV logic under the legacy "claude" bucket). The commit must delete it
	// and the archive must preserve it.
	staleBucket := postCutoff.Truncate(time.Hour)
	insertBackfillStaleLiveSummary(t, ctx, chConn, projectID, staleBucket, "claude", 999.0)

	// Inserts become visible to reads with a small delay in the test
	// container; wait for the fixture rows (raw + MV-ingested + stale) before
	// running the activities. Production backfills read history ingested long
	// ago, so the activities themselves need no such wait.
	require.EventuallyWithT(t, func(c *assert.CollectT) {
		var rawRows uint64
		assert.NoError(c, chConn.QueryRow(ctx,
			"SELECT count() FROM telemetry_logs WHERE gram_project_id = ?", projectID,
		).Scan(&rawRows))
		assert.Equal(c, uint64(5), rawRows)

		var liveBuckets uint64
		assert.NoError(c, chConn.QueryRow(ctx,
			"SELECT count() FROM attribute_metrics_summaries WHERE gram_project_id = ?", projectID,
		).Scan(&liveBuckets))
		assert.NotZero(c, liveBuckets)
	}, 15*time.Second, 250*time.Millisecond)

	act := activities.NewBackfillAttributeMetricsSummaries(logger, chConn)

	prep, err := act.Prepare(ctx, activities.PrepareAttributeMetricsBackfillParams{
		ProjectID:        projectID,
		BoundaryUnixNano: boundary.UnixNano(),
	})
	require.NoError(t, err)
	require.Equal(t, uint64(5), prep.RawRowCount)
	require.Equal(t, preCutoff.UnixNano(), prep.MinTimeUnixNano)

	// Stage the whole window in one chunk; chunking is a workflow concern.
	require.NoError(t, act.StageChunk(ctx, activities.StageAttributeMetricsBackfillChunkParams{
		ProjectID:    projectID,
		FromUnixNano: prep.MinTimeUnixNano,
		ToUnixNano:   boundary.UnixNano(),
	}))

	// Poll for the same read-visibility delay as above on the staged rows.
	var report *activities.ValidateAttributeMetricsBackfillResult
	require.EventuallyWithT(t, func(c *assert.CollectT) {
		r, err := act.Validate(ctx, activities.ValidateAttributeMetricsBackfillParams{
			ProjectID:        projectID,
			BoundaryUnixNano: boundary.UnixNano(),
		})
		if !assert.NoError(c, err) {
			return
		}
		report = r
		assert.InDelta(c, 0.85, r.Staging.TotalCost, 1e-9)
	}, 15*time.Second, 250*time.Millisecond)
	require.NotZero(t, report.Staging.RowCount)
	// 100+20 (pre-cutoff Claude) + 200+20 (post-cutoff Claude) + 40 (cursor).
	require.Equal(t, int64(380), report.Staging.TotalTokens)
	// Two tool_result rows staged, but they share one tool_use_id: the legacy
	// row count sees both, the deduped count collapses them to one call.
	require.Equal(t, uint64(2), report.Staging.TotalToolCalls)
	require.Equal(t, uint64(1), report.Staging.UniqueToolCalls)
	require.Equal(t, uint64(3), report.Staging.TotalChats)
	require.Equal(t, preCutoff.Truncate(time.Hour).Unix(), report.Staging.MinTimeBucketUnixSec)
	// Live already holds the MV-ingested post-cutoff rows plus the stale row.
	require.InDelta(t, 999.0+0.60, report.Live.TotalCost, 1e-9)

	runID := uuid.NewString()
	archive, err := act.Archive(ctx, activities.ArchiveAttributeMetricsBackfillParams{
		ProjectID:        projectID,
		BoundaryUnixNano: boundary.UnixNano(),
		BackfillRunID:    runID,
	})
	require.NoError(t, err)
	require.Equal(t, preCutoff.Truncate(time.Hour).Unix(), archive.DeleteWindowStartUnixSec)

	commit, err := act.Commit(ctx, activities.CommitAttributeMetricsBackfillParams{
		ProjectID:        projectID,
		BoundaryUnixNano: boundary.UnixNano(),
	})
	require.NoError(t, err)
	require.Equal(t, preCutoff.Truncate(time.Hour).Unix(), commit.DeleteWindowStartUnixSec)

	// The live table now reflects only the rebuild: pre-cutoff history
	// restored, the stale 999.0 aggregate replaced, hook_source preserved as
	// stamped on the raw rows.
	require.EventuallyWithT(t, func(c *assert.CollectT) {
		costBySource := queryBackfillLiveCostByHookSource(t, ctx, chConn, projectID)
		assert.InDelta(c, 0.25, costBySource["claude"], 1e-9)
		assert.InDelta(c, 0.50, costBySource[""], 1e-9)
		assert.InDelta(c, 0.10, costBySource["cursor"], 1e-9)

		var preCutoffRows uint64
		assert.NoError(c, chConn.QueryRow(ctx,
			"SELECT count() FROM attribute_metrics_summaries WHERE gram_project_id = ? AND time_bucket = ?",
			projectID, preCutoff.Truncate(time.Hour),
		).Scan(&preCutoffRows))
		assert.NotZero(c, preCutoffRows, "backfill should extend live history before the MV cutoff")
	}, 15*time.Second, 250*time.Millisecond)

	// The archive preserved the replaced rows (including the stale one) under
	// this run id.
	require.EventuallyWithT(t, func(c *assert.CollectT) {
		var archivedStale uint64
		assert.NoError(c, chConn.QueryRow(ctx,
			"SELECT count() FROM attribute_metrics_summaries_backfill_archive WHERE backfill_run_id = ? AND hook_source = 'claude'",
			runID,
		).Scan(&archivedStale))
		assert.Equal(c, uint64(1), archivedStale)
	}, 15*time.Second, 250*time.Millisecond)

	require.NoError(t, act.Cleanup(ctx, activities.CleanupAttributeMetricsBackfillParams{ProjectID: projectID}))
	var stagingRows uint64
	require.NoError(t, chConn.QueryRow(ctx,
		"SELECT count() FROM attribute_metrics_summaries_backfill WHERE gram_project_id = ?",
		projectID,
	).Scan(&stagingRows))
	require.Zero(t, stagingRows)
}

// TestBackfillAttributeMetricsSummaries_CommitRequiresStagedRows guards the
// raw-retention clamp: with nothing staged, Archive and Commit must refuse to
// touch the live table rather than derive an unbounded delete window.
func TestBackfillAttributeMetricsSummaries_CommitRequiresStagedRows(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	logger := testenv.NewLogger(t)

	chConn, err := infra.NewClickhouseClient(t)
	require.NoError(t, err)

	act := activities.NewBackfillAttributeMetricsSummaries(logger, chConn)
	projectID := uuid.NewString()
	boundary := time.Now().UTC().Truncate(time.Hour)

	_, err = act.Archive(ctx, activities.ArchiveAttributeMetricsBackfillParams{
		ProjectID:        projectID,
		BoundaryUnixNano: boundary.UnixNano(),
		BackfillRunID:    uuid.NewString(),
	})
	require.ErrorContains(t, err, "staging table has no rows")

	_, err = act.Commit(ctx, activities.CommitAttributeMetricsBackfillParams{
		ProjectID:        projectID,
		BoundaryUnixNano: boundary.UnixNano(),
	})
	require.ErrorContains(t, err, "staging table has no rows")
}

func insertBackfillClaudeAPIRequestLog(t *testing.T, ctx context.Context, chConn clickhouse.Conn, projectID string, timestamp time.Time, sessionID string, cost float64, inputTokens int, hookSource string) {
	t.Helper()

	attributes := map[string]any{
		"event.name":             "api_request",
		"prompt.id":              uuid.NewString(),
		"gen_ai.conversation.id": sessionID,
		"input_tokens":           inputTokens,
		"output_tokens":          20,
		"cache_read_tokens":      0,
		"cache_creation_tokens":  0,
		"cost_usd":               cost,
		"model":                  "claude-sonnet-4-5",
		"user.email":             "user@example.com",
	}
	if hookSource != "" {
		attributes["gram.hook.source"] = hookSource
	}
	insertBackfillTelemetryLog(t, ctx, chConn, projectID, timestamp, "claude_code.api_request", "claude-code:otel:logs", "claude-code", sessionID, attributes)
}

func insertBackfillCursorUsageLog(t *testing.T, ctx context.Context, chConn clickhouse.Conn, projectID string, timestamp time.Time, sessionID string, cost float64, totalTokens int) {
	t.Helper()

	attributes := map[string]any{
		"gen_ai.conversation.id":    sessionID,
		"gen_ai.usage.input_tokens": totalTokens,
		"gen_ai.usage.total_tokens": totalTokens,
		"gen_ai.usage.cost":         cost,
		"gen_ai.response.model":     "gpt-5",
		"gram.hook.source":          "cursor",
		"user.email":                "user@example.com",
	}
	insertBackfillTelemetryLog(t, ctx, chConn, projectID, timestamp, "cursor usage", "cursor:usage:metrics", "gram-server", sessionID, attributes)
}

func insertBackfillClaudeToolResultLog(t *testing.T, ctx context.Context, chConn clickhouse.Conn, projectID string, timestamp time.Time, sessionID string, toolUseID string, toolName string) {
	t.Helper()

	attributes := map[string]any{
		"event.name":             "tool_result",
		"tool_use_id":            toolUseID,
		"tool_name":              toolName,
		"success":                "true",
		"gen_ai.conversation.id": sessionID,
		"user.email":             "user@example.com",
	}
	insertBackfillTelemetryLog(t, ctx, chConn, projectID, timestamp, "claude_code.tool_result", "claude-code:otel:logs", "claude-code", sessionID, attributes)
}

func insertBackfillTelemetryLog(t *testing.T, ctx context.Context, chConn clickhouse.Conn, projectID string, timestamp time.Time, body string, gramURN string, serviceName string, sessionID string, attributes map[string]any) {
	t.Helper()

	attrsJSON, err := json.Marshal(attributes)
	require.NoError(t, err)

	err = telemetryrepo.New(chConn).InsertTelemetryLog(ctx, telemetryrepo.InsertTelemetryLogParams{
		ID:                   uuid.NewString(),
		TimeUnixNano:         timestamp.UnixNano(),
		ObservedTimeUnixNano: timestamp.UnixNano(),
		SeverityText:         nil,
		Body:                 body,
		TraceID:              nil,
		SpanID:               nil,
		Attributes:           string(attrsJSON),
		ResourceAttributes:   "{}",
		GramProjectID:        projectID,
		GramDeploymentID:     nil,
		GramFunctionID:       nil,
		GramURN:              gramURN,
		ServiceName:          serviceName,
		ServiceVersion:       nil,
		GramChatID:           &sessionID,
	})
	require.NoError(t, err)
}

// insertBackfillStaleLiveSummary plants a live aggregate row directly (as old
// MV logic would have written it) so the test can prove the commit replaces
// stale data.
func insertBackfillStaleLiveSummary(t *testing.T, ctx context.Context, chConn clickhouse.Conn, projectID string, bucket time.Time, hookSource string, cost float64) {
	t.Helper()

	err := chConn.Exec(ctx,
		`INSERT INTO attribute_metrics_summaries (
			gram_project_id, time_bucket,
			department_name, job_title, employee_type, division_name, cost_center_name, user_email,
			model, hook_source, roles, groups,
			total_chats, total_input_tokens, total_output_tokens, total_tokens,
			cache_read_input_tokens, cache_creation_input_tokens, total_cost, total_tool_calls,
			account_type, provider, billing_mode,
			query_source, skill_name, agent_name, mcp_server_name, mcp_tool_name
		)
		SELECT
			toUUID(?), toDateTime(?, 'UTC'),
			'', '', '', '', '', 'user@example.com',
			'claude-sonnet-4-5', ?, [], [],
			uniqExactIfState('stale-chat', toUInt8(1)),
			sumIfState(toInt64(1), toUInt8(1)),
			sumIfState(toInt64(1), toUInt8(1)),
			sumIfState(toInt64(2), toUInt8(1)),
			sumIfState(toInt64(0), toUInt8(1)),
			sumIfState(toInt64(0), toUInt8(1)),
			sumIfState(toFloat64(?), toUInt8(1)),
			countIfState(toUInt8(0)),
			'', '', '',
			'', '', '', '', ''
		FROM system.one`,
		projectID, bucket.Unix(), hookSource, cost,
	)
	require.NoError(t, err)
}

func queryBackfillLiveCostByHookSource(t *testing.T, ctx context.Context, chConn clickhouse.Conn, projectID string) map[string]float64 {
	t.Helper()

	rows, err := chConn.Query(ctx,
		"SELECT hook_source, sumIfMerge(total_cost) FROM attribute_metrics_summaries WHERE gram_project_id = ? GROUP BY hook_source",
		projectID,
	)
	require.NoError(t, err)
	defer func() { require.NoError(t, rows.Close()) }()

	out := make(map[string]float64)
	for rows.Next() {
		var source string
		var cost float64
		require.NoError(t, rows.Scan(&source, &cost))
		out[source] = cost
	}
	require.NoError(t, rows.Err())
	return out
}
