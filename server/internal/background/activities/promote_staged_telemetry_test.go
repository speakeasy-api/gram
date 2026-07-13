package activities_test

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/background/activities"
	"github.com/speakeasy-api/gram/server/internal/cache"
	"github.com/speakeasy-api/gram/server/internal/telemetry"
	telemetryrepo "github.com/speakeasy-api/gram/server/internal/telemetry/repo"
	"github.com/speakeasy-api/gram/server/internal/testenv"
)

type stagedRowFixture struct {
	id        string
	projectID uuid.UUID
	sessionID string
	requestID string
	observed  time.Time
}

func insertStagedRowForTest(t *testing.T, ctx context.Context, queries *telemetryrepo.Queries, fx stagedRowFixture) {
	t.Helper()

	attrs, err := json.Marshal(map[string]any{
		"event.name":             "api_request",
		"session.id":             fx.sessionID,
		"gen_ai.conversation.id": fx.sessionID,
		"prompt.id":              "prompt-1",
		"request_id":             fx.requestID,
		"mcp_server.name":        "custom",
		"mcp_tool.name":          "custom",
		"cost_usd":               0.0042,
		"gram.project.id":        fx.projectID.String(),
		"gram.event.source":      "hook",
	})
	require.NoError(t, err)

	sessionID := fx.sessionID
	require.NoError(t, queries.InsertTelemetryLogsStaging(ctx, []telemetryrepo.InsertTelemetryLogParams{{
		ID:                   fx.id,
		TimeUnixNano:         fx.observed.UnixNano(),
		ObservedTimeUnixNano: fx.observed.UnixNano(),
		SeverityText:         nil,
		Body:                 "claude_code.api_request",
		TraceID:              nil,
		SpanID:               nil,
		Attributes:           string(attrs),
		ResourceAttributes:   "{}",
		GramProjectID:        fx.projectID.String(),
		GramDeploymentID:     nil,
		GramFunctionID:       nil,
		GramURN:              "claude-code:otel:logs",
		ServiceName:          "claude-code",
		ServiceVersion:       nil,
		GramChatID:           &sessionID,
	}}))
}

func newPromoteStagedTelemetryHarness(t *testing.T) (context.Context, *activities.PromoteStagedTelemetry, *telemetryrepo.Queries, cache.Cache) {
	t.Helper()

	ctx := t.Context()
	chConn, err := infra.NewClickhouseClient(t)
	require.NoError(t, err)
	redisClient, err := infra.NewRedisClient(t, 0)
	require.NoError(t, err)
	cacheAdapter := cache.NewRedisCacheAdapter(redisClient)

	act := activities.NewPromoteStagedTelemetry(testenv.NewLogger(t), chConn, cacheAdapter)
	return ctx, act, telemetryrepo.New(chConn), cacheAdapter
}

func listPromotedLogs(t *testing.T, ctx context.Context, queries *telemetryrepo.Queries, projectID uuid.UUID, since time.Time) []telemetryrepo.TelemetryLog {
	t.Helper()

	logs, err := queries.ListTelemetryLogs(ctx, telemetryrepo.ListTelemetryLogsParams{
		GramProjectID: projectID.String(),
		TimeStart:     since.Add(-2 * time.Hour).UnixNano(),
		TimeEnd:       time.Now().Add(time.Minute).UnixNano(),
		GramURNs:      []string{"claude-code:otel:logs"},
		SortOrder:     "desc",
		Cursor:        "",
		Limit:         10,
	})
	require.NoError(t, err)
	return logs
}

func TestPromoteStagedTelemetry_RewritesWithTuple(t *testing.T) {
	t.Parallel()

	ctx, act, queries, cacheAdapter := newPromoteStagedTelemetryHarness(t)

	projectID := uuid.New()
	sessionID := "promo-session-rewrite"
	rowID := uuid.NewString()
	observed := time.Now().UTC().Add(-time.Minute)

	insertStagedRowForTest(t, ctx, queries, stagedRowFixture{
		id:        rowID,
		projectID: projectID,
		sessionID: sessionID,
		requestID: "req_rewrite_1",
		observed:  observed,
	})
	require.NoError(t, cacheAdapter.Set(ctx,
		telemetry.MCPAttributionTupleKey(projectID.String(), "req_rewrite_1"),
		telemetry.MCPAttributionTuple{Server: "workos-public", Tool: "whoami"},
		telemetry.MCPAttributionTupleTTL,
	))

	// The staged insert may not be visible to the first pass; re-run the
	// (idempotent) pass until one reports the promotion.
	var promotedPass *activities.PromoteStagedTelemetryResult
	require.EventuallyWithT(t, func(collect *assert.CollectT) {
		result, err := act.Do(ctx, activities.PromoteStagedTelemetryArgs{ProjectID: projectID, SessionID: sessionID})
		assert.NoError(collect, err)
		if err == nil && result != nil && result.Promoted > 0 {
			promotedPass = result
		}
		assert.NotNil(collect, promotedPass)
	}, 5*time.Second, 50*time.Millisecond)
	require.Equal(t, 1, promotedPass.Promoted)
	require.Equal(t, 1, promotedPass.Rewritten)
	require.Equal(t, 0, promotedPass.Remaining)

	var logs []telemetryrepo.TelemetryLog
	require.EventuallyWithT(t, func(collect *assert.CollectT) {
		logs = listPromotedLogs(t, ctx, queries, projectID, observed)
		assert.Len(collect, logs, 1)
	}, 3*time.Second, 50*time.Millisecond)
	require.Equal(t, rowID, logs[0].ID)
	require.Contains(t, logs[0].Attributes, "workos-public")
	require.Contains(t, logs[0].Attributes, "whoami")
	require.NotContains(t, logs[0].Attributes, `"custom"`)

	require.EventuallyWithT(t, func(collect *assert.CollectT) {
		staged, err := queries.ListStagedTelemetryLogs(ctx, projectID.String(), sessionID)
		assert.NoError(collect, err)
		assert.Empty(collect, staged)
	}, 3*time.Second, 50*time.Millisecond)
}

func TestPromoteStagedTelemetry_LeavesFreshRowsAwaitingTuple(t *testing.T) {
	t.Parallel()

	ctx, act, queries, _ := newPromoteStagedTelemetryHarness(t)

	projectID := uuid.New()
	sessionID := "promo-session-waiting"
	observed := time.Now().UTC().Add(-time.Minute)

	insertStagedRowForTest(t, ctx, queries, stagedRowFixture{
		id:        uuid.NewString(),
		projectID: projectID,
		sessionID: sessionID,
		requestID: "req_waiting_1",
		observed:  observed,
	})

	var result *activities.PromoteStagedTelemetryResult
	require.EventuallyWithT(t, func(collect *assert.CollectT) {
		var err error
		result, err = act.Do(ctx, activities.PromoteStagedTelemetryArgs{ProjectID: projectID, SessionID: sessionID})
		assert.NoError(collect, err)
		if result != nil {
			assert.Equal(collect, 1, result.Remaining)
		}
	}, 3*time.Second, 50*time.Millisecond)
	require.Equal(t, 0, result.Promoted)

	logs := listPromotedLogs(t, ctx, queries, projectID, observed)
	require.Empty(t, logs, "row awaiting its tuple must not be promoted")

	staged, err := queries.ListStagedTelemetryLogs(ctx, projectID.String(), sessionID)
	require.NoError(t, err)
	require.Len(t, staged, 1)
}

func TestPromoteStagedTelemetry_PromotesVerbatimAfterTimeout(t *testing.T) {
	t.Parallel()

	ctx, act, queries, _ := newPromoteStagedTelemetryHarness(t)

	projectID := uuid.New()
	sessionID := "promo-session-timeout"
	rowID := uuid.NewString()
	// Older than the 30-minute timeout: promotes verbatim, still "custom".
	observed := time.Now().UTC().Add(-45 * time.Minute)

	insertStagedRowForTest(t, ctx, queries, stagedRowFixture{
		id:        rowID,
		projectID: projectID,
		sessionID: sessionID,
		requestID: "req_timeout_1",
		observed:  observed,
	})

	var result *activities.PromoteStagedTelemetryResult
	require.EventuallyWithT(t, func(collect *assert.CollectT) {
		var err error
		result, err = act.Do(ctx, activities.PromoteStagedTelemetryArgs{ProjectID: projectID, SessionID: sessionID})
		assert.NoError(collect, err)
		if result != nil {
			assert.Equal(collect, 1, result.Promoted)
		}
	}, 3*time.Second, 50*time.Millisecond)
	require.Equal(t, 0, result.Rewritten)

	var logs []telemetryrepo.TelemetryLog
	require.EventuallyWithT(t, func(collect *assert.CollectT) {
		logs = listPromotedLogs(t, ctx, queries, projectID, observed)
		assert.Len(collect, logs, 1)
	}, 3*time.Second, 50*time.Millisecond)
	require.Equal(t, rowID, logs[0].ID)
	require.Contains(t, logs[0].Attributes, `"custom"`)
}

func TestPromoteStagedTelemetry_DedupSkipsAlreadyPromotedRows(t *testing.T) {
	t.Parallel()

	ctx, act, queries, cacheAdapter := newPromoteStagedTelemetryHarness(t)

	projectID := uuid.New()
	sessionID := "promo-session-dedup"
	rowID := uuid.NewString()
	observed := time.Now().UTC().Add(-time.Minute)

	insertStagedRowForTest(t, ctx, queries, stagedRowFixture{
		id:        rowID,
		projectID: projectID,
		sessionID: sessionID,
		requestID: "req_dedup_1",
		observed:  observed,
	})
	require.NoError(t, cacheAdapter.Set(ctx,
		telemetry.MCPAttributionTupleKey(projectID.String(), "req_dedup_1"),
		telemetry.MCPAttributionTuple{Server: "workos-public", Tool: "whoami"},
		telemetry.MCPAttributionTupleTTL,
	))

	// Simulate a crash between insert and delete: the row already exists in
	// telemetry_logs under the same id when the pass retries.
	attrs := `{"event.name":"api_request","request_id":"req_dedup_1","mcp_server.name":"workos-public","mcp_tool.name":"whoami","gen_ai.conversation.id":"` + sessionID + `"}`
	gramChatID := sessionID
	require.NoError(t, queries.InsertTelemetryLogs(ctx, []telemetryrepo.InsertTelemetryLogParams{{
		ID:                   rowID,
		TimeUnixNano:         observed.UnixNano(),
		ObservedTimeUnixNano: observed.UnixNano(),
		SeverityText:         nil,
		Body:                 "claude_code.api_request",
		TraceID:              nil,
		SpanID:               nil,
		Attributes:           attrs,
		ResourceAttributes:   "{}",
		GramProjectID:        projectID.String(),
		GramDeploymentID:     nil,
		GramFunctionID:       nil,
		GramURN:              "claude-code:otel:logs",
		ServiceName:          "claude-code",
		ServiceVersion:       nil,
		GramChatID:           &gramChatID,
	}}))

	// Wait for the staged insert to become visible first — otherwise a pass
	// that sees nothing would satisfy the assertions below vacuously and the
	// row would never be processed.
	require.EventuallyWithT(t, func(collect *assert.CollectT) {
		staged, err := queries.ListStagedTelemetryLogs(ctx, projectID.String(), sessionID)
		assert.NoError(collect, err)
		assert.Len(collect, staged, 1)
	}, 5*time.Second, 50*time.Millisecond)

	// Run the (idempotent) pass until staging drains. The dedup guard skips
	// the insert, so Promoted stays 0 — the row was promoted by the earlier
	// (simulated) crashed pass. Generous budget: a pass that reaches the
	// delete blocks on a ClickHouse lightweight-delete mutation, so one
	// iteration can take seconds.
	require.EventuallyWithT(t, func(collect *assert.CollectT) {
		result, err := act.Do(ctx, activities.PromoteStagedTelemetryArgs{ProjectID: projectID, SessionID: sessionID})
		assert.NoError(collect, err)
		if err == nil && result != nil {
			assert.Equal(collect, 0, result.Promoted, "dedup guard must not count skipped rows as promoted")
		}
		staged, err := queries.ListStagedTelemetryLogs(ctx, projectID.String(), sessionID)
		assert.NoError(collect, err)
		assert.Empty(collect, staged)
	}, 20*time.Second, 50*time.Millisecond)

	// Exactly one copy in telemetry_logs (no double insert)…
	require.EventuallyWithT(t, func(collect *assert.CollectT) {
		existing, err := queries.ListExistingTelemetryLogIDs(ctx, projectID.String(),
			[]string{rowID}, observed.Add(-time.Hour).UnixNano(), observed.Add(time.Hour).UnixNano())
		assert.NoError(collect, err)
		assert.Len(collect, existing, 1)
	}, 3*time.Second, 50*time.Millisecond)

	// …and staging finished its delete.
	require.EventuallyWithT(t, func(collect *assert.CollectT) {
		staged, err := queries.ListStagedTelemetryLogs(ctx, projectID.String(), sessionID)
		assert.NoError(collect, err)
		assert.Empty(collect, staged)
	}, 3*time.Second, 50*time.Millisecond)
}
