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
		usageURN = "claude-code:api_request"
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
		projectID, "claude-code:api_request", "claude-code")
	require.NoError(t, err)
}

func insertAttributeAssistantChatCompletionLog(t *testing.T, ctx context.Context, projectID string, timestamp time.Time, chatID string, cost float64, totalTokens int, model, email, department string, roles []string) {
	t.Helper()

	conn, err := infra.NewClickhouseClient(t)
	require.NoError(t, err)

	id, err := uuid.NewV7()
	require.NoError(t, err)

	attributes := map[string]any{
		"gen_ai.conversation.id":          chatID,
		"gen_ai.operation.name":           "chat",
		"gen_ai.usage.input_tokens":       totalTokens,
		"gen_ai.usage.total_tokens":       totalTokens,
		"gen_ai.usage.cost":               cost,
		"gen_ai.response.model":           model,
		"gram.hook.source":                "assistants",
		"gram.resource.urn":               "assistants:chat:completion",
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
	`, id.String(), timestamp.UnixNano(), timestamp.UnixNano(), "INFO", "assistant chat completion",
		nil, nil, string(attrsJSON), "{}",
		projectID, "assistants:chat:completion", "gram-server")
	require.NoError(t, err)
}

// insertAttributeHookToolLog inserts an agent-hook telemetry row. Agents emit a
// PreToolUse and a PostToolUse (or PostToolUseFailure) row per tool call, each
// carrying gram.tool.name; these capture every tool used in a session, Gram and
// non-Gram alike. They carry no gen_ai.usage.* attributes. The MV must count
// them (POC-209) while only firing once per call (on the completion event).
func insertAttributeHookToolLog(t *testing.T, ctx context.Context, projectID string, timestamp time.Time, toolName, hookEvent, email, department string) {
	t.Helper()

	conn, err := infra.NewClickhouseClient(t)
	require.NoError(t, err)

	id, err := uuid.NewV7()
	require.NoError(t, err)

	// Hook tool rows are not usage-metrics rows; their gram_urn is the session
	// hook URN, distinct from the *:usage:metrics aggregate rows.
	gramURN := "claude-code:hook:" + hookEvent
	attributes := map[string]any{
		"gram.tool.name":                  toolName,
		"gram.hook.event":                 hookEvent,
		"gram.hook.source":                "claude-code",
		"user.email":                      email,
		"user.attributes.department_name": department,
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

// TestQuery_CountsToolCalls is the POC-209 regression: the cost page reported 0
// tool calls because the attribute_metrics_summaries MV's row filter kept only
// `*:usage` rows, excluding the hook tool-call rows the count is sourced from.
// The count must reflect all tools used in a session (Gram and non-Gram), fire
// once per call (PostToolUse/PostToolUseFailure, not the matching PreToolUse),
// exclude provider self-names, and leave token/cost sourced from api_request rows.
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

	// One Claude api_request row carries cost/tokens.
	insertAttributeClaudeAPIRequestLog(t, ctx, projectID, ts, uuid.NewString(), 0.25, 15, 0, 0, 0, "opus", "a@x.com", "Engineering", nil, "main", "", "", "", "")

	// One Bash call: PreToolUse + PostToolUse. Must count once, not twice.
	insertAttributeHookToolLog(t, ctx, projectID, ts, "Bash", "PreToolUse", "a@x.com", "Engineering")
	insertAttributeHookToolLog(t, ctx, projectID, ts, "Bash", "PostToolUse", "a@x.com", "Engineering")
	// A non-Gram MCP tool that failed: counts.
	insertAttributeHookToolLog(t, ctx, projectID, ts, "mcp__github__search", "PostToolUseFailure", "a@x.com", "Engineering")
	// A second successful call: counts.
	insertAttributeHookToolLog(t, ctx, projectID, ts, "Read", "PostToolUse", "a@x.com", "Engineering")
	// Provider self-name and bare PreToolUse must NOT count.
	insertAttributeHookToolLog(t, ctx, projectID, ts, "claude-code", "PostToolUse", "a@x.com", "Engineering")
	insertAttributeHookToolLog(t, ctx, projectID, ts, "Grep", "PreToolUse", "a@x.com", "Engineering")

	from := now.Add(-1 * time.Hour).Format(time.RFC3339)
	to := now.Add(1 * time.Hour).Format(time.RFC3339)

	// Org total (no group_by): three counted calls (Bash, github search, Read).
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
		return totalResult.Table[0].Measures.TotalToolCalls == 3
	}, 10*time.Second, 200*time.Millisecond)

	require.NotNil(t, totalResult, "expected an aggregate row with tool calls")
	require.EqualValues(t, 3, totalResult.Table[0].Measures.TotalToolCalls)
	// Hook tool rows carry no token/cost measures, so admitting them must not
	// inflate cost from the single api_request row.
	require.InDelta(t, 0.25, totalResult.Table[0].Measures.TotalCost, 1e-9)
}

func TestQuery_IncludesCostBearingAssistantChatCompletions(t *testing.T) {
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
	insertAttributeAssistantChatCompletionLog(t, ctx, projectID, ts, uuid.NewString(), 0.42, 25, "openai/gpt-5.4", "assistant@example.com", "Engineering", []string{"dev"})

	from := now.Add(-1 * time.Hour).Format(time.RFC3339)
	to := now.Add(1 * time.Hour).Format(time.RFC3339)

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
		return res.Table[0].Measures.TotalCost == 0.42
	}, 10*time.Second, 200*time.Millisecond)

	require.Equal(t, "assistants", result.Table[0].GroupValue)
	require.InDelta(t, 0.42, result.Table[0].Measures.TotalCost, 1e-9)
	require.Equal(t, int64(25), result.Table[0].Measures.TotalInputTokens)
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

func TestQueryTumDetails_CountsOnlyGramManagedCompletions(t *testing.T) {
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

	// A completion billed as tokens under management (runs through Gram's
	// server) next to a Claude Code fleet api_request observed via OTEL.
	// Fleet telemetry is not billed, so it must not appear anywhere in the
	// billing details — not in totals, points, or any breakdown.
	insertAttributeAssistantChatCompletionLog(t, ctx, projectID, ts, uuid.NewString(), 0.42, 1000, "anthropic/claude-4.6", "assistant@example.com", "Engineering", nil)
	insertAttributeUsageLog(t, ctx, projectID, ts, uuid.NewString(), 1.5, 999999, "claude-4.6", "claude-code", "fleet@example.com", "Engineering", nil)

	from := now.Add(-1 * time.Hour).Format(time.RFC3339)
	to := now.Add(1 * time.Hour).Format(time.RFC3339)

	// Wait until BOTH rows have materialized in the analytics aggregate (the
	// unfiltered query sees the fleet row) so the exclusion assertions below
	// cannot pass vacuously against a half-ingested view.
	require.Eventually(t, func() bool {
		res, err := ti.service.Query(ctx, &gen.QueryPayload{
			From:    from,
			To:      to,
			GroupBy: conv.PtrEmpty("hook_source"),
			TopN:    10,
			SortBy:  "total_tokens",
		})
		return err == nil && res != nil && len(res.Table) == 2
	}, 10*time.Second, 200*time.Millisecond)

	result, err := ti.service.QueryTumDetails(ctx, &gen.QueryTumDetailsPayload{
		SessionToken: nil,
		From:         from,
		To:           to,
		ProjectID:    nil,
	})
	require.NoError(t, err)
	require.NotNil(t, result.Totals)

	require.Equal(t, int64(1000), result.Totals.TotalTokens, "totals must only count the billed completion")

	var pointSum int64
	for _, p := range result.Points {
		pointSum += p.TotalTokens
	}
	require.Equal(t, int64(1000), pointSum, "daily points must only count the billed completion")

	for _, b := range result.Breakdowns {
		for _, row := range b.Rows {
			require.LessOrEqual(t, row.TotalTokens, int64(1000),
				"breakdown %s row %q leaked fleet tokens", b.Key, row.Value)
			require.NotEqual(t, "fleet@example.com", row.Value,
				"fleet row leaked into breakdown %s", b.Key)
		}
	}

	sourceRows := map[string]int64{}
	for _, b := range result.Breakdowns {
		if b.Key != "hook_source" {
			continue
		}
		for _, row := range b.Rows {
			sourceRows[row.Value] = row.TotalTokens
		}
	}
	require.Equal(t, map[string]int64{"assistants": 1000}, sourceRows,
		"the source breakdown holds exactly the Gram surface")
}
