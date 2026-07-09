package telemetry_test

import (
	"context"
	"encoding/json"
	"strconv"
	"testing"
	"time"

	"github.com/google/uuid"
	gen "github.com/speakeasy-api/gram/server/gen/telemetry"
	"github.com/speakeasy-api/gram/server/internal/authz"
	"github.com/speakeasy-api/gram/server/internal/authztest"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/conv"
	projectsrepo "github.com/speakeasy-api/gram/server/internal/projects/repo"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// insertAttributeUsageLog inserts a usage-metrics telemetry row carrying the
// user/request attributes the attribute_metrics_summaries MV breaks down by.
func insertAttributeUsageLog(t *testing.T, ctx context.Context, projectID string, timestamp time.Time, chatID string, cost float64, totalTokens int, model, provider, email, department string, roles []string) {
	t.Helper()

	conn, err := infra.NewClickhouseClient(t)
	require.NoError(t, err)

	id, err := uuid.NewV7()
	require.NoError(t, err)

	serviceName := "gram-server"
	usageURN := provider + ":usage:metrics"
	attributes := map[string]any{
		"gen_ai.conversation.id":          chatID,
		"gen_ai.usage.input_tokens":       totalTokens,
		"gen_ai.usage.total_tokens":       totalTokens,
		"gen_ai.usage.cost":               cost,
		"gen_ai.response.model":           model,
		"gram.hook.source":                provider,
		"gram.resource.urn":               usageURN,
		"user.email":                      email,
		"user.attributes.department_name": department,
	}
	if provider == "claude-code" {
		serviceName = "claude-code"
		usageURN = "claude-code:otel:logs"
		attributes = map[string]any{
			"event.name":                      "api_request",
			"prompt.id":                       uuid.NewString(),
			"gen_ai.conversation.id":          chatID,
			"input_tokens":                    totalTokens,
			"output_tokens":                   0,
			"cache_read_tokens":               0,
			"cache_creation_tokens":           0,
			"cost_usd":                        cost,
			"model":                           model,
			"gram.hook.source":                provider,
			"gram.resource.urn":               usageURN,
			"user.email":                      email,
			"user.attributes.department_name": department,
		}
	}
	if roles != nil {
		attributes["user.roles"] = roles
	}

	attrsJSON, err := json.Marshal(attributes)
	require.NoError(t, err)

	err = conn.Exec(ctx, `
		INSERT INTO telemetry_logs (
			id, time_unix_nano, observed_time_unix_nano, severity_text, body,
			trace_id, span_id, attributes, resource_attributes,
			gram_project_id, gram_urn, service_name
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
	`, id.String(), timestamp.UnixNano(), timestamp.UnixNano(), "INFO", "chat completion",
		nil, nil, string(attrsJSON), "{}",
		projectID, usageURN, serviceName)
	require.NoError(t, err)
}

// insertAttributeClaudeAPIRequestLog inserts the Claude Code api_request row that
// now carries Claude token/cost attribution for attribute_metrics_summaries.
func insertAttributeClaudeAPIRequestLog(t *testing.T, ctx context.Context, projectID string, timestamp time.Time, chatID string, cost float64, inputTokens, outputTokens, cacheReadTokens, cacheCreationTokens int, model, email, department string, roles []string, querySource, skillName, agentName, mcpServerName, mcpToolName string) {
	t.Helper()

	conn, err := infra.NewClickhouseClient(t)
	require.NoError(t, err)

	id, err := uuid.NewV7()
	require.NoError(t, err)

	attributes := map[string]any{
		"gen_ai.conversation.id":          chatID,
		"prompt.id":                       uuid.NewString(),
		"event.name":                      "api_request",
		"input_tokens":                    inputTokens,
		"output_tokens":                   outputTokens,
		"cache_read_tokens":               cacheReadTokens,
		"cache_creation_tokens":           cacheCreationTokens,
		"cost_usd":                        cost,
		"model":                           model,
		"gen_ai.request.model":            model,
		"gram.hook.source":                "claude-code",
		"gram.provider":                   "anthropic",
		"gram.account_type":               "team",
		"user.email":                      email,
		"user.attributes.department_name": department,
		"query_source":                    querySource,
		"skill.name":                      skillName,
		"agent.name":                      agentName,
		"mcp_server.name":                 mcpServerName,
		"mcp_tool.name":                   mcpToolName,
	}
	if roles != nil {
		attributes["user.roles"] = roles
	}

	attrsJSON, err := json.Marshal(attributes)
	require.NoError(t, err)

	err = conn.Exec(ctx, `
		INSERT INTO telemetry_logs (
			id, time_unix_nano, observed_time_unix_nano, severity_text, body,
			trace_id, span_id, attributes, resource_attributes,
			gram_project_id, gram_urn, service_name
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
	`, id.String(), timestamp.UnixNano(), timestamp.UnixNano(), "INFO", "claude_code.api_request",
		nil, nil, string(attrsJSON), "{}",
		projectID, "claude-code:otel:logs", "claude-code")
	require.NoError(t, err)
}

// insertAttributeGramCompletionLog inserts a gram-server LLM completion row
// tagged with the given usage source (e.g. "playground", "assistants").
func insertAttributeGramCompletionLog(t *testing.T, ctx context.Context, projectID string, timestamp time.Time, chatID string, cost float64, totalTokens int, model, hookSource, email, department string, roles []string) {
	t.Helper()

	conn, err := infra.NewClickhouseClient(t)
	require.NoError(t, err)

	id, err := uuid.NewV7()
	require.NoError(t, err)

	urn := "chat:completion"
	if hookSource == "assistants" {
		urn = "assistants:chat:completion"
	}
	attributes := map[string]any{
		"gen_ai.conversation.id":          chatID,
		"gen_ai.operation.name":           "chat",
		"gen_ai.usage.input_tokens":       totalTokens,
		"gen_ai.usage.total_tokens":       totalTokens,
		"gen_ai.usage.cost":               cost,
		"gen_ai.response.model":           model,
		"gram.hook.source":                hookSource,
		"gram.resource.urn":               urn,
		"user.email":                      email,
		"user.attributes.department_name": department,
	}
	if roles != nil {
		attributes["user.roles"] = roles
	}

	attrsJSON, err := json.Marshal(attributes)
	require.NoError(t, err)

	err = conn.Exec(ctx, `
		INSERT INTO telemetry_logs (
			id, time_unix_nano, observed_time_unix_nano, severity_text, body,
			trace_id, span_id, attributes, resource_attributes,
			gram_project_id, gram_urn, service_name
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
	`, id.String(), timestamp.UnixNano(), timestamp.UnixNano(), "INFO", "gram chat completion",
		nil, nil, string(attrsJSON), "{}",
		projectID, urn, "gram-server")
	require.NoError(t, err)
}

// insertAttributeHookToolLog inserts an agent-hook telemetry row. Agents emit a
// PreToolUse and a PostToolUse (or PostToolUseFailure) row per tool call, each
// carrying gram.tool.name. The MV counts these only for the hook-based surfaces
// (codex, cursor) and only on the completion event; Claude tool calls come from
// its OTEL tool_result rows instead.
func insertAttributeHookToolLog(t *testing.T, ctx context.Context, projectID string, timestamp time.Time, hookSource, toolName, hookEvent, callID, email, department string) {
	t.Helper()

	conn, err := infra.NewClickhouseClient(t)
	require.NoError(t, err)

	id, err := uuid.NewV7()
	require.NoError(t, err)

	// Hook tool rows are not usage-metrics rows; their gram_urn is the session
	// hook URN, distinct from the *:usage:metrics aggregate rows.
	gramURN := hookSource + ":hook:" + hookEvent
	attributes := map[string]any{
		"gram.tool.name":                  toolName,
		"gram.hook.event":                 hookEvent,
		"gram.hook.source":                hookSource,
		"user.email":                      email,
		"user.attributes.department_name": department,
	}
	if callID != "" {
		attributes["gen_ai.tool.call.id"] = callID
	}

	attrsJSON, err := json.Marshal(attributes)
	require.NoError(t, err)

	err = conn.Exec(ctx, `
		INSERT INTO telemetry_logs (
			id, time_unix_nano, observed_time_unix_nano, severity_text, body,
			trace_id, span_id, attributes, resource_attributes,
			gram_project_id, gram_urn, service_name
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
	`, id.String(), timestamp.UnixNano(), timestamp.UnixNano(), "INFO", "tool hook",
		nil, nil, string(attrsJSON), "{}",
		projectID, gramURN, "gram-server")
	require.NoError(t, err)
}

// insertAttributeClaudeToolResultLog inserts a Claude OTEL tool_result row —
// one per completed tool call, carrying tool_use_id. This is the sole source
// of Claude tool-call counts; re-emitted rows with the same tool_use_id must
// dedup to one call via unique_tool_calls.
func insertAttributeClaudeToolResultLog(t *testing.T, ctx context.Context, projectID string, timestamp time.Time, chatID, toolUseID, toolName, email, department string) {
	t.Helper()

	conn, err := infra.NewClickhouseClient(t)
	require.NoError(t, err)

	id, err := uuid.NewV7()
	require.NoError(t, err)

	attributes := map[string]any{
		"gen_ai.conversation.id":          chatID,
		"event.name":                      "tool_result",
		"tool_use_id":                     toolUseID,
		"tool_name":                       toolName,
		"success":                         "true",
		"user.email":                      email,
		"user.attributes.department_name": department,
	}

	attrsJSON, err := json.Marshal(attributes)
	require.NoError(t, err)

	err = conn.Exec(ctx, `
		INSERT INTO telemetry_logs (
			id, time_unix_nano, observed_time_unix_nano, severity_text, body,
			trace_id, span_id, attributes, resource_attributes,
			gram_project_id, gram_urn, service_name, gram_chat_id
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
	`, id.String(), timestamp.UnixNano(), timestamp.UnixNano(), "INFO", "claude_code.tool_result",
		nil, nil, string(attrsJSON), "{}",
		projectID, "claude-code:otel:logs", "claude-code", chatID)
	require.NoError(t, err)
}

// insertAttributePreDedupSummaryRow plants an attribute_metrics_summaries row
// directly, shaped like rows written before the unique_tool_calls column
// existed: total_tool_calls carries a counted state while unique_tool_calls is
// omitted and defaults to the empty state (exactly what ALTER TABLE ADD COLUMN
// leaves in existing parts).
func insertAttributePreDedupSummaryRow(t *testing.T, ctx context.Context, projectID string, bucket time.Time, toolCalls int, cost float64) {
	t.Helper()

	conn, err := infra.NewClickhouseClient(t)
	require.NoError(t, err)

	err = conn.Exec(ctx, `
		INSERT INTO attribute_metrics_summaries (
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
			'', '', '', '', '', 'legacy@example.com',
			'opus', 'claude-code', [], [],
			uniqExactIfState('legacy-chat', toUInt8(number = 0)),
			sumIfState(toInt64(10), toUInt8(number = 0)),
			sumIfState(toInt64(5), toUInt8(number = 0)),
			sumIfState(toInt64(15), toUInt8(number = 0)),
			sumIfState(toInt64(0), toUInt8(number = 0)),
			sumIfState(toInt64(0), toUInt8(number = 0)),
			sumIfState(toFloat64(?), toUInt8(number = 0)),
			countIfState(toUInt8(1)),
			'', '', '',
			'', '', '', '', ''
		FROM numbers(?)`,
		projectID, bucket.Unix(), cost, toolCalls,
	)
	require.NoError(t, err)
}

func tableCostByGroup(rows []*gen.QueryRow) map[string]float64 {
	out := make(map[string]float64, len(rows))
	for _, r := range rows {
		out[r.GroupValue] = r.Measures.TotalCost
	}
	return out
}

func rowByGroup(t *testing.T, rows []*gen.QueryRow, group string) *gen.QueryRow {
	t.Helper()
	for _, r := range rows {
		if r.GroupValue == group {
			return r
		}
	}
	t.Fatalf("row for group %q not found", group)
	return nil
}

func TestQuery_GroupByDimensionsAndDrilldown(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestLogsService(t)

	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	projectID := authCtx.ProjectID.String()

	// Org-scoped read grant for telemetry.query.
	ctx = authztest.WithExactGrants(t, ctx, authz.Grant{
		Scope:    authz.ScopeOrgRead,
		Selector: authz.NewSelector(authz.ScopeOrgRead, authCtx.ActiveOrganizationID),
	})

	now := time.Date(2026, time.June, 20, 1, 0, 0, 0, time.UTC)
	ts := now.Add(-10 * time.Minute)

	// Engineering: admin+dev ($0.25) and dev ($0.10). Sales: no roles ($0.50).
	insertAttributeClaudeAPIRequestLog(t, ctx, projectID, ts, uuid.NewString(), 0.25, 15, 0, 0, 0, "opus", "a@x.com", "Engineering", []string{"admin", "dev"}, "main", "", "", "", "")
	insertAttributeUsageLog(t, ctx, projectID, ts, uuid.NewString(), 0.10, 5, "opus", "cursor", "b@x.com", "Engineering", []string{"dev"})
	insertAttributeClaudeAPIRequestLog(t, ctx, projectID, ts, uuid.NewString(), 0.50, 50, 0, 0, 0, "sonnet", "c@x.com", "Sales", nil, "main", "", "", "", "")

	from := now.Add(-1 * time.Hour).Format(time.RFC3339)
	to := now.Add(1 * time.Hour).Format(time.RFC3339)

	// Group by department (eventual consistency on the MV).
	var deptResult *gen.QueryResult
	require.Eventually(t, func() bool {
		res, err := ti.service.Query(ctx, &gen.QueryPayload{
			From:    from,
			To:      to,
			GroupBy: conv.PtrEmpty("department_name"),
			TopN:    10,
			SortBy:  "total_cost",
		})
		if err != nil || res == nil {
			return false
		}
		deptResult = res
		return len(res.Table) == 2
	}, 10*time.Second, 200*time.Millisecond)

	require.Equal(t, "department_name", deptResult.GroupBy)
	require.Equal(t, int64(3600), deptResult.IntervalSeconds)

	// Ordered by cost desc: Sales ($0.50) before Engineering ($0.35).
	require.Equal(t, "Sales", deptResult.Table[0].GroupValue)
	require.Equal(t, "Engineering", deptResult.Table[1].GroupValue)
	deptCost := tableCostByGroup(deptResult.Table)
	require.InDelta(t, 0.50, deptCost["Sales"], 1e-9)
	require.InDelta(t, 0.35, deptCost["Engineering"], 1e-9)

	// One timeseries series per table row, gap-filled.
	require.Len(t, deptResult.Timeseries, 2)
	for _, s := range deptResult.Timeseries {
		require.NotEmpty(t, s.Points)
	}

	// dimension_values: each group carries the distinct values of every other
	// allowlisted dimension observed within it. Engineering had two users
	// (a@x.com on opus/claude-code with roles admin+dev, b@x.com on opus/cursor
	// with role dev). The group_by dimension (department_name) is absent.
	eng := rowByGroup(t, deptResult.Table, "Engineering")
	require.NotContains(t, eng.DimensionValues, "department_name", "group_by dimension must be excluded")
	require.ElementsMatch(t, []string{"a@x.com", "b@x.com"}, eng.DimensionValues["email"])
	require.ElementsMatch(t, []string{"opus"}, eng.DimensionValues["model"])
	require.ElementsMatch(t, []string{"claude-code", "cursor"}, eng.DimensionValues["hook_source"])
	require.ElementsMatch(t, []string{"admin", "dev"}, eng.DimensionValues["role"])
	// Unset dimensions are present as keys with empty (filtered) lists.
	require.Empty(t, eng.DimensionValues["job_title"])
	// billing_mode is the exception: unclassified rows surface as "" so a scope
	// mixing metered and unclassified spend can never read as confidently metered.
	require.ElementsMatch(t, []string{""}, eng.DimensionValues["billing_mode"])

	// Sales had a single role-less user; its email surfaces and role is empty.
	sales := rowByGroup(t, deptResult.Table, "Sales")
	require.ElementsMatch(t, []string{"c@x.com"}, sales.DimensionValues["email"])
	require.Empty(t, sales.DimensionValues["role"])

	// Group by role: dev gets both Engineering rows ($0.35), admin one ($0.25),
	// and Sales' role-less spend surfaces under the empty-string group ($0.50).
	roleResult, err := ti.service.Query(ctx, &gen.QueryPayload{
		From:    from,
		To:      to,
		GroupBy: conv.PtrEmpty("role"),
		TopN:    10,
		SortBy:  "total_cost",
	})
	require.NoError(t, err)
	roleCost := tableCostByGroup(roleResult.Table)
	require.InDelta(t, 0.35, roleCost["dev"], 1e-9)
	require.InDelta(t, 0.25, roleCost["admin"], 1e-9)
	require.InDelta(t, 0.50, roleCost[""], 1e-9)

	// Drill-down: filter department=Engineering, group by role. Only Engineering
	// rows count, so dev $0.35 and admin $0.25 (no role-less Sales spend).
	drillResult, err := ti.service.Query(ctx, &gen.QueryPayload{
		From:    from,
		To:      to,
		GroupBy: conv.PtrEmpty("role"),
		Filters: []*gen.QueryFilter{{Dimension: "department_name", Values: []string{"Engineering"}}},
		TopN:    10,
		SortBy:  "total_cost",
	})
	require.NoError(t, err)
	drillCost := tableCostByGroup(drillResult.Table)
	require.InDelta(t, 0.35, drillCost["dev"], 1e-9)
	require.InDelta(t, 0.25, drillCost["admin"], 1e-9)
	require.NotContains(t, drillCost, "", "role-less Sales spend must be excluded by the department filter")

	// No group_by: a single aggregate row over the whole org slice ($0.85).
	totalResult, err := ti.service.Query(ctx, &gen.QueryPayload{
		From:   from,
		To:     to,
		TopN:   10,
		SortBy: "total_cost",
	})
	require.NoError(t, err)
	require.Len(t, totalResult.Table, 1)
	require.Empty(t, totalResult.Table[0].GroupValue)
	require.InDelta(t, 0.85, totalResult.Table[0].Measures.TotalCost, 1e-9)
	require.Len(t, totalResult.Timeseries, 1)
}

func TestQuery_DefaultSortByAndTopN(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestLogsService(t)

	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	projectID := authCtx.ProjectID.String()

	ctx = authztest.WithExactGrants(t, ctx, authz.Grant{
		Scope:    authz.ScopeOrgRead,
		Selector: authz.NewSelector(authz.ScopeOrgRead, authCtx.ActiveOrganizationID),
	})

	now := time.Date(2026, time.June, 20, 1, 0, 0, 0, time.UTC)
	ts := now.Add(-10 * time.Minute)
	for i := range 12 {
		dept := "D" + strconv.Itoa(i+1)
		cost := float64(12 - i)
		insertAttributeClaudeAPIRequestLog(t, ctx, projectID, ts, uuid.NewString(), cost, 1, 0, 0, 0, "m", dept+"@x.com", dept, nil, "main", "", "", "", "")
	}

	from := now.Add(-1 * time.Hour).Format(time.RFC3339)
	to := now.Add(1 * time.Hour).Format(time.RFC3339)

	var res *gen.QueryResult
	require.Eventually(t, func() bool {
		r, err := ti.service.Query(ctx, &gen.QueryPayload{
			From:    from,
			To:      to,
			GroupBy: conv.PtrEmpty("department_name"),
		})
		if err != nil || r == nil {
			return false
		}
		res = r
		return len(r.Table) == 11
	}, 10*time.Second, 200*time.Millisecond)

	require.Equal(t, "D1", res.Table[0].GroupValue, "default sort_by should rank by total_cost")
	require.Equal(t, "D10", res.Table[9].GroupValue)
	require.Equal(t, "Other", res.Table[10].GroupValue, "default top_n should keep 10 groups and roll up the rest")
	require.InDelta(t, 3.0, res.Table[10].Measures.TotalCost, 1e-9)
}

// TestQuery_CountsToolCalls covers the provenance-first tool counting: Claude
// tool calls come only from OTEL tool_result rows deduped by tool_use_id;
// Codex/Cursor tool calls come from completed hook rows; Claude hook rows and
// PreToolUse companions never count; token/cost stays sourced from api_request
// rows.
func TestQuery_CountsToolCalls(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestLogsService(t)

	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	projectID := authCtx.ProjectID.String()

	ctx = authztest.WithExactGrants(t, ctx, authz.Grant{
		Scope:    authz.ScopeOrgRead,
		Selector: authz.NewSelector(authz.ScopeOrgRead, authCtx.ActiveOrganizationID),
	})

	now := time.Date(2026, time.June, 20, 1, 0, 0, 0, time.UTC)
	ts := now.Add(-10 * time.Minute)
	chatID := uuid.NewString()

	// One Claude api_request row carries cost/tokens.
	insertAttributeClaudeAPIRequestLog(t, ctx, projectID, ts, chatID, 0.25, 15, 0, 0, 0, "opus", "a@x.com", "Engineering", nil, "main", "", "", "", "")

	// Claude tool calls: three distinct tool_use_ids; the Bash result is
	// re-emitted with the same tool_use_id and must dedup to one call.
	bashCallID := uuid.NewString()
	insertAttributeClaudeToolResultLog(t, ctx, projectID, ts, chatID, bashCallID, "Bash", "a@x.com", "Engineering")
	insertAttributeClaudeToolResultLog(t, ctx, projectID, ts, chatID, bashCallID, "Bash", "a@x.com", "Engineering")
	insertAttributeClaudeToolResultLog(t, ctx, projectID, ts, chatID, uuid.NewString(), "mcp__github__search", "a@x.com", "Engineering")
	insertAttributeClaudeToolResultLog(t, ctx, projectID, ts, chatID, uuid.NewString(), "Read", "a@x.com", "Engineering")

	// Cursor tool call: PreToolUse + PostToolUse hook rows count once.
	cursorCallID := uuid.NewString()
	insertAttributeHookToolLog(t, ctx, projectID, ts, "cursor", "Grep", "PreToolUse", cursorCallID, "a@x.com", "Engineering")
	insertAttributeHookToolLog(t, ctx, projectID, ts, "cursor", "Grep", "PostToolUse", cursorCallID, "a@x.com", "Engineering")
	// Claude hook rows must NOT count: their calls are already counted via the
	// OTEL tool_result rows.
	insertAttributeHookToolLog(t, ctx, projectID, ts, "claude-code", "Bash", "PostToolUse", "", "a@x.com", "Engineering")
	// Provider self-name rows must NOT count — they are not tool calls.
	insertAttributeHookToolLog(t, ctx, projectID, ts, "cursor", "cursor", "PostToolUse", "", "a@x.com", "Engineering")

	from := now.Add(-1 * time.Hour).Format(time.RFC3339)
	to := now.Add(1 * time.Hour).Format(time.RFC3339)

	// Org total (no group_by): four counted calls — Bash (deduped), github
	// search, Read, and the Cursor Grep call.
	var totalResult *gen.QueryResult
	require.Eventually(t, func() bool {
		res, err := ti.service.Query(ctx, &gen.QueryPayload{
			From:   from,
			To:     to,
			TopN:   10,
			SortBy: "total_tool_calls",
		})
		if err != nil || res == nil || len(res.Table) != 1 {
			return false
		}
		totalResult = res
		return totalResult.Table[0].Measures.TotalToolCalls == 4
	}, 10*time.Second, 200*time.Millisecond)

	require.NotNil(t, totalResult, "expected an aggregate row with tool calls")
	require.EqualValues(t, 4, totalResult.Table[0].Measures.TotalToolCalls)
	// Tool rows carry no token/cost measures, so admitting them must not
	// inflate cost from the single api_request row.
	require.InDelta(t, 0.25, totalResult.Table[0].Measures.TotalCost, 1e-9)
}

// TestQuery_FallsBackToRowCountedToolCalls covers the expand-contract window:
// summary rows written before the unique_tool_calls column existed only carry
// the legacy total_tool_calls count (unique_tool_calls is the empty default
// state), and the reader must fall back to it instead of reporting zero until
// the backfiller rebuilds those buckets.
func TestQuery_FallsBackToRowCountedToolCalls(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestLogsService(t)

	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	projectID := authCtx.ProjectID.String()

	ctx = authztest.WithExactGrants(t, ctx, authz.Grant{
		Scope:    authz.ScopeOrgRead,
		Selector: authz.NewSelector(authz.ScopeOrgRead, authCtx.ActiveOrganizationID),
	})

	now := time.Date(2026, time.June, 20, 1, 0, 0, 0, time.UTC)
	insertAttributePreDedupSummaryRow(t, ctx, projectID, now.Add(-1*time.Hour), 3, 0.75)

	from := now.Add(-2 * time.Hour).Format(time.RFC3339)
	to := now.Add(1 * time.Hour).Format(time.RFC3339)

	var result *gen.QueryResult
	require.Eventually(t, func() bool {
		res, err := ti.service.Query(ctx, &gen.QueryPayload{
			From:   from,
			To:     to,
			TopN:   10,
			SortBy: "total_tool_calls",
		})
		if err != nil || res == nil || len(res.Table) != 1 {
			return false
		}
		result = res
		return res.Table[0].Measures.TotalToolCalls == 3
	}, 10*time.Second, 200*time.Millisecond)

	require.EqualValues(t, 3, result.Table[0].Measures.TotalToolCalls)
	require.InDelta(t, 0.75, result.Table[0].Measures.TotalCost, 1e-9)
}

// TestQuery_ExcludesAssistantChatCompletions guards the provenance rule: the
// aggregate covers the three agent surfaces only, so Gram-hosted assistant
// chat completions never reach attribute_metrics_summaries even when they
// carry cost.
func TestQuery_ExcludesAssistantChatCompletions(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestLogsService(t)

	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	projectID := authCtx.ProjectID.String()

	ctx = authztest.WithExactGrants(t, ctx, authz.Grant{
		Scope:    authz.ScopeOrgRead,
		Selector: authz.NewSelector(authz.ScopeOrgRead, authCtx.ActiveOrganizationID),
	})

	now := time.Date(2026, time.June, 20, 1, 0, 0, 0, time.UTC)
	ts := now.Add(-10 * time.Minute)
	insertAttributeGramCompletionLog(t, ctx, projectID, ts, uuid.NewString(), 0.42, 25, "openai/gpt-5.4", "assistants", "assistant@example.com", "Engineering", []string{"dev"})
	insertAttributeClaudeAPIRequestLog(t, ctx, projectID, ts, uuid.NewString(), 0.25, 15, 0, 0, 0, "opus", "claude@example.com", "Engineering", nil, "main", "", "", "", "")

	from := now.Add(-1 * time.Hour).Format(time.RFC3339)
	to := now.Add(1 * time.Hour).Format(time.RFC3339)

	// Once the Claude row is aggregated, the assistants row must not be: it is
	// excluded by the MV's provenance WHERE clause, not by timing.
	var result *gen.QueryResult
	require.Eventually(t, func() bool {
		res, err := ti.service.Query(ctx, &gen.QueryPayload{
			From:    from,
			To:      to,
			GroupBy: conv.PtrEmpty("hook_source"),
			TopN:    10,
			SortBy:  "total_cost",
		})
		if err != nil || res == nil || len(res.Table) != 1 {
			return false
		}
		result = res
		return res.Table[0].GroupValue == "claude-code"
	}, 10*time.Second, 200*time.Millisecond)

	require.Len(t, result.Table, 1)
	require.Equal(t, "claude-code", result.Table[0].GroupValue)
	require.InDelta(t, 0.25, result.Table[0].Measures.TotalCost, 1e-9)
}

func TestQuery_AttributesClaudeAPIRequestByMCPAndSkill(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestLogsService(t)

	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	projectID := authCtx.ProjectID.String()

	ctx = authztest.WithExactGrants(t, ctx, authz.Grant{
		Scope:    authz.ScopeOrgRead,
		Selector: authz.NewSelector(authz.ScopeOrgRead, authCtx.ActiveOrganizationID),
	})

	now := time.Date(2026, time.June, 20, 1, 0, 0, 0, time.UTC)
	ts := now.Add(-10 * time.Minute)
	chatID := uuid.NewString()

	insertAttributeClaudeAPIRequestLog(t, ctx, projectID, ts, chatID, 0.40, 10, 2, 3, 5, "opus", "a@x.com", "Engineering", []string{"dev"}, "main", "git-skill", "generalPurpose", "github", "search")

	from := now.Add(-1 * time.Hour).Format(time.RFC3339)
	to := now.Add(1 * time.Hour).Format(time.RFC3339)

	var byServer *gen.QueryResult
	require.Eventually(t, func() bool {
		res, err := ti.service.Query(ctx, &gen.QueryPayload{
			From:    from,
			To:      to,
			GroupBy: conv.PtrEmpty("mcp_server_name"),
			TopN:    10,
			SortBy:  "cache_creation_input_tokens",
		})
		if err != nil || res == nil || len(res.Table) != 1 {
			return false
		}
		byServer = res
		return res.Table[0].GroupValue == "github"
	}, 10*time.Second, 200*time.Millisecond)

	row := byServer.Table[0]
	require.Equal(t, "github", row.GroupValue)
	require.InDelta(t, 0.40, row.Measures.TotalCost, 1e-9)
	require.Equal(t, int64(10), row.Measures.TotalInputTokens)
	require.Equal(t, int64(2), row.Measures.TotalOutputTokens)
	require.Equal(t, int64(20), row.Measures.TotalTokens)
	require.Equal(t, int64(3), row.Measures.CacheReadInputTokens)
	require.Equal(t, int64(5), row.Measures.CacheCreationInputTokens)
	require.ElementsMatch(t, []string{"git-skill"}, row.DimensionValues["skill_name"])

	bySkill, err := ti.service.Query(ctx, &gen.QueryPayload{
		From:    from,
		To:      to,
		GroupBy: conv.PtrEmpty("skill_name"),
		TopN:    10,
		SortBy:  "total_cost",
	})
	require.NoError(t, err)
	require.Len(t, bySkill.Table, 1)
	require.Equal(t, "git-skill", bySkill.Table[0].GroupValue)
	require.InDelta(t, 0.40, bySkill.Table[0].Measures.TotalCost, 1e-9)
}

func TestQuery_TopNRollupIntoOther(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestLogsService(t)

	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	projectID := authCtx.ProjectID.String()

	ctx = authztest.WithExactGrants(t, ctx, authz.Grant{
		Scope:    authz.ScopeOrgRead,
		Selector: authz.NewSelector(authz.ScopeOrgRead, authCtx.ActiveOrganizationID),
	})

	now := time.Date(2026, time.June, 20, 1, 0, 0, 0, time.UTC)
	ts := now.Add(-10 * time.Minute)

	// Four departments with distinct costs; top_n=2 keeps the two priciest and
	// folds the rest into "Other".
	insertAttributeClaudeAPIRequestLog(t, ctx, projectID, ts, uuid.NewString(), 4.0, 1, 0, 0, 0, "m", "a@x.com", "D1", nil, "main", "", "", "", "")
	insertAttributeClaudeAPIRequestLog(t, ctx, projectID, ts, uuid.NewString(), 3.0, 1, 0, 0, 0, "m", "b@x.com", "D2", nil, "main", "", "", "", "")
	insertAttributeClaudeAPIRequestLog(t, ctx, projectID, ts, uuid.NewString(), 2.0, 1, 0, 0, 0, "m", "c@x.com", "D3", nil, "main", "", "", "", "")
	insertAttributeClaudeAPIRequestLog(t, ctx, projectID, ts, uuid.NewString(), 1.0, 1, 0, 0, 0, "m", "d@x.com", "D4", nil, "main", "", "", "", "")

	from := now.Add(-1 * time.Hour).Format(time.RFC3339)
	to := now.Add(1 * time.Hour).Format(time.RFC3339)

	var res *gen.QueryResult
	require.Eventually(t, func() bool {
		r, err := ti.service.Query(ctx, &gen.QueryPayload{
			From:    from,
			To:      to,
			GroupBy: conv.PtrEmpty("department_name"),
			TopN:    2,
			SortBy:  "total_cost",
		})
		if err != nil || r == nil {
			return false
		}
		res = r
		// 4 distinct departments visible before rollup.
		var total float64
		for _, row := range r.Table {
			total += row.Measures.TotalCost
		}
		return total >= 9.99 && len(r.Table) == 3
	}, 10*time.Second, 200*time.Millisecond)

	require.Equal(t, "D1", res.Table[0].GroupValue)
	require.Equal(t, "D2", res.Table[1].GroupValue)
	require.Equal(t, "Other", res.Table[2].GroupValue)
	require.InDelta(t, 3.0, res.Table[2].Measures.TotalCost, 1e-9) // D3 + D4

	// Timeseries has a matching Other series.
	require.Len(t, res.Timeseries, 3)
	var hasOther bool
	for _, s := range res.Timeseries {
		if s.GroupValue == "Other" {
			hasOther = true
		}
	}
	require.True(t, hasOther)
}

// insertChatEvidenceRow inserts a chat event row without token attributes —
// the stored-session evidence the billed queries qualify chats on.
func insertChatEvidenceRow(t *testing.T, ctx context.Context, projectID string, timestamp time.Time, chatID string) {
	t.Helper()

	conn, err := infra.NewClickhouseClient(t)
	require.NoError(t, err)

	id, err := uuid.NewV7()
	require.NoError(t, err)

	attributes := map[string]any{"gen_ai.conversation.id": chatID}
	attrsJSON, err := json.Marshal(attributes)
	require.NoError(t, err)

	err = conn.Exec(ctx, `
		INSERT INTO telemetry_logs (
			id, time_unix_nano, observed_time_unix_nano, severity_text, body,
			trace_id, span_id, attributes, resource_attributes,
			gram_project_id, gram_urn, service_name
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
	`, id.String(), timestamp.UnixNano(), timestamp.UnixNano(), "INFO", "chat message",
		nil, nil, string(attrsJSON), "{}",
		projectID, "chat:message", "gram-server")
	require.NoError(t, err)
}

func TestQueryTumDetails_CountsOnlyBilledCompletions(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestLogsService(t)

	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	projectID := authCtx.ProjectID.String()

	ctx = authztest.WithExactGrants(t, ctx, authz.Grant{
		Scope:    authz.ScopeOrgRead,
		Selector: authz.NewSelector(authz.ScopeOrgRead, authCtx.ActiveOrganizationID),
	})

	now := time.Date(2026, time.June, 20, 1, 0, 0, 0, time.UTC)
	ts := now.Add(-10 * time.Minute)

	// A billed completion (playground, a registered usage source) with stored
	// evidence, next to two populations that must not appear anywhere in the
	// billing details: an assistants completion (Speakeasy covers assistants
	// inference until BYOK, so the surface is deliberately unregistered) and
	// a Claude Code fleet api_request observed via OTEL (agent-native token
	// attributes, never part of the billed population).
	playgroundChat := uuid.NewString()
	assistantsChat := uuid.NewString()
	insertAttributeGramCompletionLog(t, ctx, projectID, ts, playgroundChat, 0.42, 1000, "anthropic/claude-4.6", "playground", "user@example.com", "Engineering", []string{"dev"})
	insertChatEvidenceRow(t, ctx, projectID, ts, playgroundChat)
	insertAttributeGramCompletionLog(t, ctx, projectID, ts, assistantsChat, 0.13, 555, "openai/gpt-5.4", "assistants", "assistant@example.com", "Engineering", nil)
	insertChatEvidenceRow(t, ctx, projectID, ts, assistantsChat)
	insertAttributeClaudeAPIRequestLog(t, ctx, projectID, ts, uuid.NewString(), 1.5, 999999, 0, 0, 0, "claude-4.6", "fleet@example.com", "Engineering", nil, "main", "", "", "", "")

	from := now.Add(-1 * time.Hour).Format(time.RFC3339)
	to := now.Add(1 * time.Hour).Format(time.RFC3339)

	// Wait until BOTH gram completions materialized in the billing aggregate
	// (the assistants chat included) so the exclusion assertions below cannot
	// pass vacuously against a half-ingested view.
	chConn, err := infra.NewClickhouseClient(t)
	require.NoError(t, err)
	require.Eventually(t, func() bool {
		row := chConn.QueryRow(ctx,
			"SELECT count(DISTINCT chat_id) FROM tum_breakdown_summaries WHERE gram_project_id = ? AND chat_id IN (?, ?)",
			projectID, playgroundChat, assistantsChat)
		var chats uint64
		if err := row.Scan(&chats); err != nil {
			return false
		}
		return chats == 2
	}, 10*time.Second, 200*time.Millisecond)

	var result *gen.TumDetailsResult
	require.Eventually(t, func() bool {
		res, resErr := ti.service.QueryTumDetails(ctx, &gen.QueryTumDetailsPayload{
			SessionToken: nil,
			From:         from,
			To:           to,
			ProjectID:    nil,
		})
		if resErr != nil || res.Totals == nil {
			return false
		}
		result = res
		// The stored-evidence rows qualify the chats asynchronously too.
		return res.Totals.TotalTokens == 1000
	}, 10*time.Second, 200*time.Millisecond)

	require.Equal(t, int64(1000), result.Totals.InputTokens, "the fixture reports all tokens as input")
	require.Equal(t, int64(0), result.Totals.OutputTokens)

	var pointSum int64
	for _, p := range result.Points {
		pointSum += p.TotalTokens
	}
	require.Equal(t, int64(1000), pointSum, "daily points must only count the billed completion")

	rowsByKey := map[string]map[string]int64{}
	for _, b := range result.Breakdowns {
		rowsByKey[b.Key] = map[string]int64{}
		for _, row := range b.Rows {
			rowsByKey[b.Key][row.Value] = row.TotalTokens
		}
	}
	require.Equal(t, map[string]int64{"playground": 1000}, rowsByKey["hook_source"],
		"the source breakdown holds exactly the billed surface")
	require.Equal(t, map[string]int64{"anthropic/claude-4.6": 1000}, rowsByKey["model"])
	require.Equal(t, map[string]int64{"user@example.com": 1000}, rowsByKey["email"])
	require.Equal(t, map[string]int64{"dev": 1000}, rowsByKey["role"])
	// The fixture carries no division attribute; the tokens land on the ''
	// row (labeled by the frontend).
	require.Equal(t, map[string]int64{"": 1000}, rowsByKey["division_name"])
}

func TestQueryTumDetails_IncludesDeletedProjects(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestLogsService(t)

	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	projectID := authCtx.ProjectID.String()

	ctx = authztest.WithExactGrants(t, ctx, authz.Grant{
		Scope:    authz.ScopeOrgRead,
		Selector: authz.NewSelector(authz.ScopeOrgRead, authCtx.ActiveOrganizationID),
	})

	// A second project that gets soft-deleted after recording usage: the
	// tokens were consumed while it was live, so billing — the card AND the
	// breakdowns — must keep counting them.
	doomed, err := projectsrepo.New(ti.conn).CreateProject(ctx, projectsrepo.CreateProjectParams{
		Name:           "Doomed Project",
		Slug:           "doomed-" + uuid.NewString()[:8],
		OrganizationID: authCtx.ActiveOrganizationID,
	})
	require.NoError(t, err)

	now := time.Date(2026, time.June, 20, 1, 0, 0, 0, time.UTC)
	ts := now.Add(-10 * time.Minute)

	liveChat := uuid.NewString()
	doomedChat := uuid.NewString()
	insertAttributeGramCompletionLog(t, ctx, projectID, ts, liveChat, 0.42, 1000, "anthropic/claude-4.6", "playground", "user@example.com", "Engineering", nil)
	insertChatEvidenceRow(t, ctx, projectID, ts, liveChat)
	insertAttributeGramCompletionLog(t, ctx, doomed.ID.String(), ts, doomedChat, 0.2, 250, "anthropic/claude-4.6", "playground", "user@example.com", "Engineering", nil)
	insertChatEvidenceRow(t, ctx, doomed.ID.String(), ts, doomedChat)

	_, err = projectsrepo.New(ti.conn).DeleteProject(ctx, doomed.ID)
	require.NoError(t, err)

	from := now.Add(-1 * time.Hour).Format(time.RFC3339)
	to := now.Add(1 * time.Hour).Format(time.RFC3339)

	require.EventuallyWithT(t, func(c *assert.CollectT) {
		res, resErr := ti.service.QueryTumDetails(ctx, &gen.QueryTumDetailsPayload{
			SessionToken: nil,
			From:         from,
			To:           to,
			ProjectID:    nil,
		})
		if !assert.NoError(c, resErr) || !assert.NotNil(c, res.Totals) {
			return
		}
		assert.Equal(c, int64(1250), res.Totals.TotalTokens,
			"the deleted project's usage must still count toward the billing breakdowns")
	}, 10*time.Second, 200*time.Millisecond)
}
