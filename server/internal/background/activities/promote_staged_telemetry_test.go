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
	orgID     string
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
		"gram.org.id":            fx.orgID,
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
	// The tuple is keyed by org, not project — the hooks key that writes it
	// can resolve a different project than the OTEL exporter that staged the
	// row. An org id distinct from the project id proves the join no longer
	// depends on the two credentials agreeing on a project.
	orgID := "org-" + uuid.NewString()
	sessionID := "promo-session-rewrite"
	rowID := uuid.NewString()
	observed := time.Now().UTC().Add(-time.Minute)

	insertStagedRowForTest(t, ctx, queries, stagedRowFixture{
		id:        rowID,
		projectID: projectID,
		orgID:     orgID,
		sessionID: sessionID,
		requestID: "req_rewrite_1",
		observed:  observed,
	})
	require.NoError(t, cacheAdapter.Set(ctx,
		telemetry.MCPAttributionTupleKey(orgID, "req_rewrite_1"),
		telemetry.MCPAttributionTuple{Server: "workos-public", Tool: "whoami"},
		telemetry.MCPAttributionTupleTTL,
	))

	// The staged insert may not be visible to the first pass; re-run the
	// (idempotent) pass until one reports the promotion.
	var promotedPass *activities.PromoteStagedTelemetryResult
	require.EventuallyWithT(t, func(collect *assert.CollectT) {
		result, err := act.Do(ctx, activities.PromoteStagedTelemetryArgs{ProjectID: projectID})
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
		staged, err := queries.ListStagedTelemetryLogs(ctx, projectID.String())
		assert.NoError(collect, err)
		assert.Empty(collect, staged)
	}, 3*time.Second, 50*time.Millisecond)
}

// TestPromoteStagedTelemetry_IgnoresTupleFromAnotherOrg pins the tuple key's
// tenant isolation: a tuple submitted under one org must never rewrite a
// staged row belonging to another org, even when the (client-controlled)
// request id collides. This is the org-scoped successor to the original
// project-scoped defense.
func TestPromoteStagedTelemetry_IgnoresTupleFromAnotherOrg(t *testing.T) {
	t.Parallel()

	ctx, act, queries, cacheAdapter := newPromoteStagedTelemetryHarness(t)

	projectID := uuid.New()
	sessionID := "promo-session-cross-org"
	observed := time.Now().UTC().Add(-time.Minute)

	insertStagedRowForTest(t, ctx, queries, stagedRowFixture{
		id:        uuid.NewString(),
		projectID: projectID,
		orgID:     "org-" + uuid.NewString(),
		sessionID: sessionID,
		requestID: "req_cross_org_1",
		observed:  observed,
	})
	require.NoError(t, cacheAdapter.Set(ctx,
		telemetry.MCPAttributionTupleKey("org-"+uuid.NewString(), "req_cross_org_1"),
		telemetry.MCPAttributionTuple{Server: "attacker-server", Tool: "attacker-tool"},
		telemetry.MCPAttributionTupleTTL,
	))

	var result *activities.PromoteStagedTelemetryResult
	require.EventuallyWithT(t, func(collect *assert.CollectT) {
		var err error
		result, err = act.Do(ctx, activities.PromoteStagedTelemetryArgs{ProjectID: projectID})
		assert.NoError(collect, err)
		if result != nil {
			assert.Equal(collect, 1, result.Remaining)
		}
	}, 3*time.Second, 50*time.Millisecond)
	require.Equal(t, 0, result.Promoted, "another org's tuple must not promote the row")

	require.Empty(t, listPromotedLogs(t, ctx, queries, projectID, observed),
		"another org's tuple must never be applied to this org's staged row")
}

// TestPromoteStagedTelemetry_ScopesTupleMemoizationByOrg pins the in-pass
// memoization to the Redis key's (org, request id) scope. A row staged before
// the org_id column existed carries an empty org; processed first, its nil
// lookup must not be memoized under the request id alone, or a later row with
// the same request id and a populated org would never see its tuple and would
// eventually promote verbatim.
func TestPromoteStagedTelemetry_ScopesTupleMemoizationByOrg(t *testing.T) {
	t.Parallel()

	ctx, act, queries, cacheAdapter := newPromoteStagedTelemetryHarness(t)

	projectID := uuid.New()
	orgID := "org-" + uuid.NewString()
	sessionID := "promo-session-memo"
	requestID := "req_memo_1"
	orglessRowID := uuid.NewString()
	orgRowID := uuid.NewString()
	observed := time.Now().UTC().Add(-time.Minute)

	// Older row first in the pass (rows are ordered by observed time): the
	// empty-org copy must be scanned before the populated-org copy.
	insertStagedRowForTest(t, ctx, queries, stagedRowFixture{
		id:        orglessRowID,
		projectID: projectID,
		orgID:     "",
		sessionID: sessionID,
		requestID: requestID,
		observed:  observed,
	})
	insertStagedRowForTest(t, ctx, queries, stagedRowFixture{
		id:        orgRowID,
		projectID: projectID,
		orgID:     orgID,
		sessionID: sessionID,
		requestID: requestID,
		observed:  observed.Add(time.Second),
	})
	require.NoError(t, cacheAdapter.Set(ctx,
		telemetry.MCPAttributionTupleKey(orgID, requestID),
		telemetry.MCPAttributionTuple{Server: "workos-public", Tool: "whoami"},
		telemetry.MCPAttributionTupleTTL,
	))

	// The org-scoped row must promote rewritten; the org-less row has no
	// reachable tuple and stays awaiting the timeout.
	var promotedPass *activities.PromoteStagedTelemetryResult
	require.EventuallyWithT(t, func(collect *assert.CollectT) {
		result, err := act.Do(ctx, activities.PromoteStagedTelemetryArgs{ProjectID: projectID})
		assert.NoError(collect, err)
		if err == nil && result != nil && result.Promoted > 0 {
			promotedPass = result
		}
		assert.NotNil(collect, promotedPass)
	}, 5*time.Second, 50*time.Millisecond)
	require.Equal(t, 1, promotedPass.Promoted)
	require.Equal(t, 1, promotedPass.Rewritten)
	require.Equal(t, 1, promotedPass.Remaining)

	var logs []telemetryrepo.TelemetryLog
	require.EventuallyWithT(t, func(collect *assert.CollectT) {
		logs = listPromotedLogs(t, ctx, queries, projectID, observed)
		assert.Len(collect, logs, 1)
	}, 3*time.Second, 50*time.Millisecond)
	require.Equal(t, orgRowID, logs[0].ID)
	require.Contains(t, logs[0].Attributes, "workos-public")

	staged, err := queries.ListStagedTelemetryLogs(ctx, projectID.String())
	require.NoError(t, err)
	require.Len(t, staged, 1)
	require.Equal(t, orglessRowID, staged[0].ID)
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
		orgID:     "org-" + uuid.NewString(),
		sessionID: sessionID,
		requestID: "req_waiting_1",
		observed:  observed,
	})

	var result *activities.PromoteStagedTelemetryResult
	require.EventuallyWithT(t, func(collect *assert.CollectT) {
		var err error
		result, err = act.Do(ctx, activities.PromoteStagedTelemetryArgs{ProjectID: projectID})
		assert.NoError(collect, err)
		if result != nil {
			assert.Equal(collect, 1, result.Remaining)
		}
	}, 3*time.Second, 50*time.Millisecond)
	require.Equal(t, 0, result.Promoted)

	logs := listPromotedLogs(t, ctx, queries, projectID, observed)
	require.Empty(t, logs, "row awaiting its tuple must not be promoted")

	staged, err := queries.ListStagedTelemetryLogs(ctx, projectID.String())
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
		orgID:     "org-" + uuid.NewString(),
		sessionID: sessionID,
		requestID: "req_timeout_1",
		observed:  observed,
	})

	var result *activities.PromoteStagedTelemetryResult
	require.EventuallyWithT(t, func(collect *assert.CollectT) {
		var err error
		result, err = act.Do(ctx, activities.PromoteStagedTelemetryArgs{ProjectID: projectID})
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

func TestPromoteStagedTelemetry_DefersRowClaimedByConcurrentAttempt(t *testing.T) {
	t.Parallel()

	ctx, act, queries, cacheAdapter := newPromoteStagedTelemetryHarness(t)

	projectID := uuid.New()
	orgID := "org-" + uuid.NewString()
	sessionID := "promo-session-claim"
	rowID := uuid.NewString()
	observed := time.Now().UTC().Add(-time.Minute)

	insertStagedRowForTest(t, ctx, queries, stagedRowFixture{
		id:        rowID,
		projectID: projectID,
		orgID:     orgID,
		sessionID: sessionID,
		requestID: "req_claim_1",
		observed:  observed,
	})
	require.NoError(t, cacheAdapter.Set(ctx,
		telemetry.MCPAttributionTupleKey(orgID, "req_claim_1"),
		telemetry.MCPAttributionTuple{Server: "workos-public", Tool: "whoami"},
		telemetry.MCPAttributionTupleTTL,
	))

	// Simulate a concurrent (e.g. timed-out) attempt already holding this
	// row's promotion claim, with an insert that may still be in flight.
	claimKey := telemetry.MCPPromotionClaimKey(projectID.String(), rowID)
	won, err := cacheAdapter.Add(ctx, claimKey, time.Minute)
	require.NoError(t, err)
	require.True(t, won, "test must be the one holding the claim")

	// Wait for the staged row to be visible, then a pass must DEFER it (the
	// claim is held) rather than insert a second copy.
	require.EventuallyWithT(t, func(collect *assert.CollectT) {
		staged, err := queries.ListStagedTelemetryLogs(ctx, projectID.String())
		assert.NoError(collect, err)
		assert.Len(collect, staged, 1)
	}, 5*time.Second, 50*time.Millisecond)

	result, err := act.Do(ctx, activities.PromoteStagedTelemetryArgs{ProjectID: projectID})
	require.NoError(t, err)
	require.Equal(t, 0, result.Promoted, "a claimed row must not be promoted by a losing attempt")
	require.Equal(t, 0, result.Deduped)
	require.GreaterOrEqual(t, result.Remaining, 1, "the deferred row must surface as Remaining")

	require.Empty(t, listPromotedLogs(t, ctx, queries, projectID, observed),
		"a claimed row must not be inserted into telemetry_logs")
	staged, err := queries.ListStagedTelemetryLogs(ctx, projectID.String())
	require.NoError(t, err)
	require.Len(t, staged, 1, "a deferred row must stay in staging")

	// The winner finishing (or the claim's TTL lapsing) frees the row: release
	// the claim and a later pass promotes it exactly once.
	require.NoError(t, cacheAdapter.Delete(ctx, claimKey))

	var promoted *activities.PromoteStagedTelemetryResult
	require.EventuallyWithT(t, func(collect *assert.CollectT) {
		result, err := act.Do(ctx, activities.PromoteStagedTelemetryArgs{ProjectID: projectID})
		assert.NoError(collect, err)
		if err == nil && result != nil && result.Promoted > 0 {
			promoted = result
		}
		assert.NotNil(collect, promoted)
	}, 5*time.Second, 50*time.Millisecond)
	require.Equal(t, 1, promoted.Promoted)
	require.Equal(t, 1, promoted.Rewritten)

	require.EventuallyWithT(t, func(collect *assert.CollectT) {
		staged, err := queries.ListStagedTelemetryLogs(ctx, projectID.String())
		assert.NoError(collect, err)
		assert.Empty(collect, staged)
	}, 3*time.Second, 50*time.Millisecond)
}

func TestPromoteStagedTelemetry_DedupSkipsAlreadyPromotedRows(t *testing.T) {
	t.Parallel()

	ctx, act, queries, cacheAdapter := newPromoteStagedTelemetryHarness(t)

	projectID := uuid.New()
	orgID := "org-" + uuid.NewString()
	sessionID := "promo-session-dedup"
	rowID := uuid.NewString()
	observed := time.Now().UTC().Add(-time.Minute)

	insertStagedRowForTest(t, ctx, queries, stagedRowFixture{
		id:        rowID,
		projectID: projectID,
		orgID:     orgID,
		sessionID: sessionID,
		requestID: "req_dedup_1",
		observed:  observed,
	})
	require.NoError(t, cacheAdapter.Set(ctx,
		telemetry.MCPAttributionTupleKey(orgID, "req_dedup_1"),
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
		staged, err := queries.ListStagedTelemetryLogs(ctx, projectID.String())
		assert.NoError(collect, err)
		assert.Len(collect, staged, 1)
	}, 5*time.Second, 50*time.Millisecond)

	// Also wait for the simulated crashed-promotion row to be visible in
	// telemetry_logs. Otherwise a pass whose existence guard runs before the
	// pre-insert is visible would treat the row as new and promote it (a second
	// insert) instead of taking the dedup path, so the drain loop would report
	// Promoted rather than Deduped.
	require.EventuallyWithT(t, func(collect *assert.CollectT) {
		existing, err := queries.ListExistingTelemetryLogIDs(ctx, projectID.String(),
			[]string{rowID}, observed.Add(-time.Hour).UnixNano(), observed.Add(time.Hour).UnixNano())
		assert.NoError(collect, err)
		assert.Len(collect, existing, 1)
	}, 5*time.Second, 50*time.Millisecond)

	// Run the (idempotent) pass until staging drains. The dedup guard skips
	// the insert, so Promoted stays 0 — the row was promoted by the earlier
	// (simulated) crashed pass — but the cleanup must surface as Deduped so
	// the workflow drain loop counts it as progress. Generous budget: a pass
	// that reaches the delete blocks on a ClickHouse lightweight-delete
	// mutation, so one iteration can take seconds.
	sawDeduped := false
	require.EventuallyWithT(t, func(collect *assert.CollectT) {
		result, err := act.Do(ctx, activities.PromoteStagedTelemetryArgs{ProjectID: projectID})
		assert.NoError(collect, err)
		if err == nil && result != nil {
			assert.Equal(collect, 0, result.Promoted, "dedup guard must not count skipped rows as promoted")
			if result.Deduped > 0 {
				sawDeduped = true
			}
		}
		staged, err := queries.ListStagedTelemetryLogs(ctx, projectID.String())
		assert.NoError(collect, err)
		assert.Empty(collect, staged)
	}, 20*time.Second, 50*time.Millisecond)
	require.True(t, sawDeduped, "the pass that cleaned up the crashed promotion must report it as Deduped")

	// Exactly one copy in telemetry_logs (no double insert)…
	require.EventuallyWithT(t, func(collect *assert.CollectT) {
		existing, err := queries.ListExistingTelemetryLogIDs(ctx, projectID.String(),
			[]string{rowID}, observed.Add(-time.Hour).UnixNano(), observed.Add(time.Hour).UnixNano())
		assert.NoError(collect, err)
		assert.Len(collect, existing, 1)
	}, 3*time.Second, 50*time.Millisecond)

	// …and staging finished its delete.
	require.EventuallyWithT(t, func(collect *assert.CollectT) {
		staged, err := queries.ListStagedTelemetryLogs(ctx, projectID.String())
		assert.NoError(collect, err)
		assert.Empty(collect, staged)
	}, 3*time.Second, 50*time.Millisecond)
}
