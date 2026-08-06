package telemetry_test

import (
	"context"
	"encoding/json"
	"sort"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	gen "github.com/speakeasy-api/gram/server/gen/telemetry"
	"github.com/speakeasy-api/gram/server/internal/authz"
	"github.com/speakeasy-api/gram/server/internal/authztest"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/conv"
	"github.com/speakeasy-api/gram/server/internal/telemetry/semantic"
	"github.com/speakeasy-api/gram/server/internal/testenv"
)

// TestQuerySemantic_ParityWithLegacyPath runs the same telemetry.query
// payloads through the production (semantic) Service.Query and the retained
// legacy repo path, asserting identical results. Phase 1 proved the layer
// reproduces the dashboard queries before the rewiring; now the roles are
// inverted and the legacy mirror keeps the rewired endpoint honest.
func TestQuerySemantic_ParityWithLegacyPath(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestLogsService(t)

	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	projectID := authCtx.ProjectID.String()

	ctx = authztest.WithExactGrants(t, ctx, authz.Grant{
		Scope:    authz.ScopeOrgRead,
		Selector: authz.NewSelector(authz.ScopeOrgRead, authCtx.ActiveOrganizationID),
	})

	now := time.Date(2026, time.July, 14, 1, 0, 0, 0, time.UTC)
	ts := now.Add(-10 * time.Minute)

	// Realistic mixed-surface slice: Claude api_request rows (with tool
	// results and MCP/skill attribution), a Cursor usage row, and a Codex
	// usage row across two departments, several users, models, and roles.
	claudeChat := uuid.NewString()
	insertAttributeClaudeAPIRequestLog(t, ctx, projectID, ts, claudeChat, 0.25, 15, 5, 2, 3, "opus", "a@x.com", "Engineering", []string{"admin", "dev"}, "main", "git-skill", "generalPurpose", "github", "search")
	insertAttributeClaudeAPIRequestLog(t, ctx, projectID, ts, uuid.NewString(), 0.50, 50, 10, 0, 0, "sonnet", "c@x.com", "Sales", nil, "main", "", "", "", "")
	insertAttributeUsageLog(t, ctx, projectID, ts, uuid.NewString(), 0.10, 5, "opus", "cursor", "b@x.com", "Engineering", []string{"dev"})
	insertAttributeUsageLog(t, ctx, projectID, ts, uuid.NewString(), 0.05, 7, "gpt-5-codex", "codex", "d@x.com", "Sales", nil)

	// Claude tool calls (deduped by tool_use_id) and a completed Cursor hook
	// tool call.
	bashCallID := uuid.NewString()
	insertAttributeClaudeToolResultLog(t, ctx, projectID, ts, claudeChat, bashCallID, "Bash", "a@x.com", "Engineering")
	insertAttributeClaudeToolResultLog(t, ctx, projectID, ts, claudeChat, bashCallID, "Bash", "a@x.com", "Engineering")
	insertAttributeClaudeToolResultLog(t, ctx, projectID, ts, claudeChat, uuid.NewString(), "Read", "a@x.com", "Engineering")
	cursorCallID := uuid.NewString()
	insertAttributeHookToolLog(t, ctx, projectID, ts, "cursor", "Grep", "PreToolUse", cursorCallID, "b@x.com", "Engineering")
	insertAttributeHookToolLog(t, ctx, projectID, ts, "cursor", "Grep", "PostToolUse", cursorCallID, "b@x.com", "Engineering")

	testenv.FlushClickHouseAsyncInserts(t, ti.chConn)

	from := now.Add(-1 * time.Hour).Format(time.RFC3339)
	to := now.Add(1 * time.Hour).Format(time.RFC3339)

	// Wait until the aggregate MV has materialized every seeded usage row
	// (four distinct users in the email breakdown).
	require.Eventually(t, func() bool {
		res, err := ti.service.Query(ctx, &gen.QueryPayload{
			From:    from,
			To:      to,
			GroupBy: conv.PtrEmpty("email"),
			TopN:    10,
			SortBy:  "total_cost",
		})
		return err == nil && res != nil && len(res.Table) == 4
	}, 10*time.Second, 200*time.Millisecond)

	payloads := []struct {
		name    string
		payload *gen.QueryPayload
	}{
		{
			// The cost explorer's demo query: group by user.
			name: "group_by email",
			payload: &gen.QueryPayload{
				From: from, To: to,
				GroupBy:            conv.PtrEmpty("email"),
				GranularitySeconds: conv.PtrEmpty(int64(3600)),
				TopN:               10,
				SortBy:             "total_cost",
			},
		},
		{
			name: "group_by role (array dimension)",
			payload: &gen.QueryPayload{
				From: from, To: to,
				GroupBy:            conv.PtrEmpty("role"),
				GranularitySeconds: conv.PtrEmpty(int64(3600)),
				TopN:               10,
				SortBy:             "total_cost",
			},
		},
		{
			name: "no group_by",
			payload: &gen.QueryPayload{
				From: from, To: to,
				GranularitySeconds: conv.PtrEmpty(int64(3600)),
				TopN:               10,
				SortBy:             "total_cost",
			},
		},
		{
			name: "drilldown: department filter + group_by email",
			payload: &gen.QueryPayload{
				From: from, To: to,
				GroupBy:            conv.PtrEmpty("email"),
				Filters:            []*gen.QueryFilter{{Dimension: "department_name", Values: []string{"Engineering"}}},
				GranularitySeconds: conv.PtrEmpty(int64(3600)),
				TopN:               10,
				SortBy:             "total_cost",
			},
		},
		{
			name: "array dimension filter",
			payload: &gen.QueryPayload{
				From: from, To: to,
				GroupBy:            conv.PtrEmpty("department_name"),
				Filters:            []*gen.QueryFilter{{Dimension: "role", Values: []string{"dev"}}},
				GranularitySeconds: conv.PtrEmpty(int64(3600)),
				TopN:               10,
				SortBy:             "total_tokens",
			},
		},
	}

	for _, tc := range payloads {
		legacy, err := ti.service.QueryLegacyForTest(ctx, tc.payload)
		require.NoError(t, err, "legacy path failed for %s", tc.name)
		require.NotEmpty(t, legacy.Table, "legacy path returned no rows for %s — parity would be vacuous", tc.name)

		viaSemantic, err := ti.service.Query(ctx, tc.payload)
		require.NoError(t, err, "semantic path failed for %s", tc.name)

		// Deep equality on the result structs — strictly stronger than the
		// byte-identical-JSON contract (it also distinguishes nil from
		// empty) — after normalizing the one legitimately nondeterministic
		// part (dimension_values array order; see sortDimensionValues).
		sortDimensionValues(legacy)
		sortDimensionValues(viaSemantic)
		require.Equal(t, legacy, viaSemantic,
			"semantic result diverged from the legacy path for %s", tc.name)
	}
}

// sortDimensionValues sorts each table row's dimension_values lists in place.
// They are groupUniqArray aggregations, whose element order ClickHouse does
// not define — the legacy path isn't stable against itself run-to-run, so
// parity can only be asserted on the sets, not the order.
func sortDimensionValues(res *gen.QueryResult) {
	for _, row := range res.Table {
		for _, values := range row.DimensionValues {
			sort.Strings(values)
		}
	}
}

// insertSemanticProviderUsageLog inserts a generic usage-metrics row with an
// explicit gram_urn and gram.event.source, so the raw binding's observed
// population split (hook-written vs Admin-API-polled rows) is exercisable.
func insertSemanticProviderUsageLog(t *testing.T, ctx context.Context, projectID string, timestamp time.Time, chatID, gramURN, eventSource string, inputTokens int, cost float64, chargedCents int) {
	t.Helper()

	conn, err := infra.NewClickhouseClient(t)
	require.NoError(t, err)

	id, err := uuid.NewV7()
	require.NoError(t, err)

	attributes := map[string]any{
		"gen_ai.conversation.id":    chatID,
		"gen_ai.usage.input_tokens": inputTokens,
		"gen_ai.usage.cost":         cost,
		"gen_ai.response.model":     "some-model",
		"gram.hook.source":          "cursor",
		"gram.event.source":         eventSource,
		"gram.resource.urn":         gramURN,
	}
	if chargedCents > 0 {
		attributes["cursor.charged_cents"] = chargedCents
	}
	attrsJSON, err := json.Marshal(attributes)
	require.NoError(t, err)

	err = conn.Exec(ctx, `
		INSERT INTO telemetry_logs (
			id, time_unix_nano, observed_time_unix_nano, severity_text, body,
			trace_id, span_id, attributes, resource_attributes,
			gram_project_id, gram_urn, service_name
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
	`, id.String(), timestamp.UnixNano(), timestamp.UnixNano(), "INFO", "usage metrics",
		nil, nil, string(attrsJSON), "{}",
		projectID, gramURN, "gram-server")
	require.NoError(t, err)
}

// TestQuerySemantic_RawBindingServesObservedPopulationOnly seeds one workspace
// observed through both the Cursor hook and the Admin-API poller plus a
// provider-settled Claude Chat row, and asserts the raw telemetry_logs
// binding's row_filter admits only the hook-written (locally observed) row.
// The legacy sessionSourceRowPredicate unions both populations; the semantic
// raw binding deliberately splits them (provider_reports serves the settled
// complement).
func TestQuerySemantic_RawBindingServesObservedPopulationOnly(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestLogsService(t)

	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	projectID := authCtx.ProjectID.String()

	now := time.Date(2026, time.July, 14, 1, 0, 0, 0, time.UTC)
	ts := now.Add(-10 * time.Minute)

	hookChat := uuid.NewString()
	apiChat := uuid.NewString()
	settledChat := uuid.NewString()
	chatgptChat := uuid.NewString()
	codexChat := uuid.NewString()
	insertSemanticProviderUsageLog(t, ctx, projectID, ts, hookChat, "cursor:usage:metrics", "hook", 10, 0.10, 0)
	insertSemanticProviderUsageLog(t, ctx, projectID, ts, apiChat, "cursor:usage:metrics", "api", 999, 9.99, 0)
	insertSemanticProviderUsageLog(t, ctx, projectID, ts, settledChat, "claude_chat:usage:metrics", "api", 555, 5.55, 0)
	insertSemanticProviderUsageLog(t, ctx, projectID, ts, chatgptChat, "chatgpt:usage:metrics", "api", 111, 1.11, 0)
	insertSemanticProviderUsageLog(t, ctx, projectID, ts, codexChat, "codex:usage:metrics", "api", 222, 2.22, 0)

	testenv.FlushClickHouseAsyncInserts(t, ti.chConn)

	def, err := semantic.Load()
	require.NoError(t, err)

	// A session filter is only served by the raw telemetry_logs binding.
	plan, err := semantic.Plan(def, semantic.Query{
		Model:                  "usage",
		Measures:               []string{"cost_usd", "tokens_total"},
		GroupBy:                "",
		Filters:                []semantic.Filter{{Dimension: "session", Values: []string{hookChat, apiChat, settledChat, chatgptChat, codexChat}}},
		TimeStart:              now.Add(-1 * time.Hour).UnixNano(),
		TimeEnd:                now.Add(1 * time.Hour).UnixNano(),
		GranularitySeconds:     0,
		Scope:                  semantic.Scope{ProjectIDs: []string{projectID}},
		Sort:                   nil,
		IncludeDimensionValues: false,
	})
	require.NoError(t, err)
	require.Equal(t, "telemetry_logs", plan.Binding.Source)

	rows, err := semantic.Execute(ctx, ti.chConn, plan)
	require.NoError(t, err)
	require.Len(t, rows, 1)
	require.InDelta(t, 0.10, rows[0].Measures["cost_usd"].Float64, 1e-9,
		"only the hook-written cursor row is locally observed; API-polled cursor/codex, Claude Chat, and ChatGPT rows are provider-reported")
	require.EqualValues(t, 10, rows[0].Measures["tokens_total"].Int64)
}

// TestQuerySemantic_ProviderReportsServeSettledPopulationOnly is the
// complement of the observed-population test above: provider_reports'
// row_filter must count ONLY provider-settled rows (Admin-API-polled cursor
// usage and Claude Chat usage) and exclude the hook-written row for the same
// workspace.
func TestQuerySemantic_ProviderReportsServeSettledPopulationOnly(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestLogsService(t)

	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	projectID := authCtx.ProjectID.String()

	now := time.Date(2026, time.July, 14, 1, 0, 0, 0, time.UTC)
	ts := now.Add(-10 * time.Minute)

	insertSemanticProviderUsageLog(t, ctx, projectID, ts, uuid.NewString(), "cursor:usage:metrics", "hook", 10, 0.10, 0)
	insertSemanticProviderUsageLog(t, ctx, projectID, ts, uuid.NewString(), "cursor:usage:metrics", "api", 999, 9.99, 250)
	insertSemanticProviderUsageLog(t, ctx, projectID, ts, uuid.NewString(), "claude_chat:usage:metrics", "api", 555, 5.55, 0)
	insertSemanticProviderUsageLog(t, ctx, projectID, ts, uuid.NewString(), "chatgpt:usage:metrics", "api", 111, 1.11, 0)
	insertSemanticProviderUsageLog(t, ctx, projectID, ts, uuid.NewString(), "codex:usage:metrics", "api", 222, 2.22, 0)

	testenv.FlushClickHouseAsyncInserts(t, ti.chConn)

	def, err := semantic.Load()
	require.NoError(t, err)

	plan, err := semantic.Plan(def, semantic.Query{
		Model:                  "provider_reports",
		Measures:               []string{"tokens_total", "cost_usd", "charged_usd"},
		GroupBy:                "",
		Filters:                nil,
		TimeStart:              now.Add(-1 * time.Hour).UnixNano(),
		TimeEnd:                now.Add(1 * time.Hour).UnixNano(),
		GranularitySeconds:     0,
		Scope:                  semantic.Scope{ProjectIDs: []string{projectID}},
		Sort:                   nil,
		IncludeDimensionValues: false,
	})
	require.NoError(t, err)
	require.Equal(t, "telemetry_logs", plan.Binding.Source)

	rows, err := semantic.Execute(ctx, ti.chConn, plan)
	require.NoError(t, err)
	require.Len(t, rows, 1)
	require.EqualValues(t, 999+555+111+222, rows[0].Measures["tokens_total"].Int64,
		"settled tokens are the API-polled cursor/codex rows plus the Claude Chat and ChatGPT rows; the hook row is excluded")
	require.InDelta(t, 9.99+5.55+1.11+2.22, rows[0].Measures["cost_usd"].Float64, 1e-9)
	require.InDelta(t, 2.50, rows[0].Measures["charged_usd"].Float64, 1e-9,
		"charged_usd converts cursor.charged_cents on the API-polled row")
}

// parityLogSeed is one fully-attributed Claude api_request row for the parity
// matrix: every public telemetry.query dimension carries a value so each
// group_by axis has real groups to compare across the two paths.
type parityLogSeed struct {
	chatID        string
	cost          float64
	inputTokens   int
	outputTokens  int
	cacheRead     int
	cacheCreation int
	model         string
	email         string
	hostname      string
	department    string
	jobTitle      string
	employeeType  string
	division      string
	costCenter    string
	billingMode   string
	roles         []string
	groups        []string
	querySource   string
	skillName     string
	agentName     string
	mcpServerName string
	mcpToolName   string
}

func insertParityClaudeAPIRequestLog(t *testing.T, ctx context.Context, projectID string, timestamp time.Time, seed parityLogSeed) {
	t.Helper()

	conn, err := infra.NewClickhouseClient(t)
	require.NoError(t, err)

	id, err := uuid.NewV7()
	require.NoError(t, err)

	attributes := map[string]any{
		"gen_ai.conversation.id":           seed.chatID,
		"prompt.id":                        uuid.NewString(),
		"event.name":                       "api_request",
		"input_tokens":                     seed.inputTokens,
		"output_tokens":                    seed.outputTokens,
		"cache_read_tokens":                seed.cacheRead,
		"cache_creation_tokens":            seed.cacheCreation,
		"cost_usd":                         seed.cost,
		"model":                            seed.model,
		"gen_ai.request.model":             seed.model,
		"gram.hook.source":                 "claude-code",
		"gram.hook.hostname":               seed.hostname,
		"gram.provider":                    "anthropic",
		"gram.account_type":                "team",
		"gram.billing_mode":                seed.billingMode,
		"user.email":                       seed.email,
		"user.attributes.department_name":  seed.department,
		"user.attributes.job_title":        seed.jobTitle,
		"user.attributes.employee_type":    seed.employeeType,
		"user.attributes.division_name":    seed.division,
		"user.attributes.cost_center_name": seed.costCenter,
		"query_source":                     seed.querySource,
		"skill.name":                       seed.skillName,
		"agent.name":                       seed.agentName,
		"mcp_server.name":                  seed.mcpServerName,
		"mcp_tool.name":                    seed.mcpToolName,
	}
	if seed.roles != nil {
		attributes["user.roles"] = seed.roles
	}
	if seed.groups != nil {
		attributes["user.groups"] = seed.groups
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

// TestQuery_SemanticParityMatrix seeds one richly-attributed slice and runs
// the rewired (semantic) Service.Query against the retained legacy repo path
// for every public group_by dimension (minus skill_version, which stays on
// its own path) plus rollup, drilldown, array-filter, sort, and granularity
// special cases, asserting identical results throughout.
//
// The per-row costs are powers of two so every group's cost sum is unique —
// ORDER BY total_cost DESC never has ties, keeping row order deterministic
// across the two textually different SQL statements.
func TestQuery_SemanticParityMatrix(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestLogsService(t)

	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	projectID := authCtx.ProjectID.String()

	ctx = authztest.WithExactGrants(t, ctx, authz.Grant{
		Scope:    authz.ScopeOrgRead,
		Selector: authz.NewSelector(authz.ScopeOrgRead, authCtx.ActiveOrganizationID),
	})

	now := time.Date(2026, time.July, 14, 1, 0, 0, 0, time.UTC)
	ts := now.Add(-10 * time.Minute)

	claudeChatA := uuid.NewString()
	seeds := []parityLogSeed{
		{
			chatID: claudeChatA, cost: 0.01, inputTokens: 100, outputTokens: 10, cacheRead: 5, cacheCreation: 1,
			model: "opus", email: "a@x.com", hostname: "host-1", department: "Engineering", jobTitle: "SWE",
			employeeType: "full_time", division: "R&D", costCenter: "CC-1", billingMode: "metered",
			roles: []string{"admin", "dev"}, groups: []string{"g1"},
			querySource: "qs-a", skillName: "sk-a", agentName: "ag-a", mcpServerName: "ms-a", mcpToolName: "mt-a",
		},
		{
			chatID: uuid.NewString(), cost: 0.02, inputTokens: 200, outputTokens: 20, cacheRead: 6, cacheCreation: 2,
			model: "sonnet", email: "b@x.com", hostname: "host-2", department: "Engineering", jobTitle: "SRE",
			employeeType: "contractor", division: "R&D", costCenter: "CC-2", billingMode: "metered",
			roles: []string{"dev"}, groups: []string{"g1", "g2"},
			querySource: "qs-b", skillName: "sk-b", agentName: "ag-b", mcpServerName: "ms-b", mcpToolName: "mt-b",
		},
		{
			chatID: uuid.NewString(), cost: 0.04, inputTokens: 400, outputTokens: 40, cacheRead: 7, cacheCreation: 3,
			model: "opus", email: "c@x.com", hostname: "host-3", department: "Sales", jobTitle: "AE",
			employeeType: "full_time", division: "GTM", costCenter: "CC-3", billingMode: "flat_rate",
			roles: nil, groups: nil,
			querySource: "qs-c", skillName: "", agentName: "", mcpServerName: "", mcpToolName: "",
		},
		{
			chatID: uuid.NewString(), cost: 0.08, inputTokens: 800, outputTokens: 80, cacheRead: 8, cacheCreation: 4,
			model: "haiku", email: "d@x.com", hostname: "host-4", department: "Sales", jobTitle: "SDR",
			employeeType: "intern", division: "GTM", costCenter: "CC-4", billingMode: "flat_rate",
			roles: []string{"ops"}, groups: []string{"g3"},
			querySource: "", skillName: "sk-d", agentName: "", mcpServerName: "", mcpToolName: "",
		},
	}
	for _, seed := range seeds {
		insertParityClaudeAPIRequestLog(t, ctx, projectID, ts, seed)
	}
	// Non-Claude surfaces: a Cursor and a Codex usage row (their provider /
	// account_type / billing_mode / attribution dims are unset — the ''
	// bucket).
	insertAttributeUsageLog(t, ctx, projectID, ts, uuid.NewString(), 0.16, 500, "cursor-model", "cursor", "e@x.com", "Support", nil)
	insertAttributeUsageLog(t, ctx, projectID, ts, uuid.NewString(), 0.32, 700, "gpt-5-codex", "codex", "f@x.com", "Support", nil)

	// Tool calls: two distinct Claude calls (a@x) and one completed Cursor
	// hook call (e@x) — distinct per-surface counts for the sort_by case.
	insertAttributeClaudeToolResultLog(t, ctx, projectID, ts, claudeChatA, uuid.NewString(), "Bash", "a@x.com", "Engineering")
	insertAttributeClaudeToolResultLog(t, ctx, projectID, ts, claudeChatA, uuid.NewString(), "Read", "a@x.com", "Engineering")
	cursorCallID := uuid.NewString()
	insertAttributeHookToolLog(t, ctx, projectID, ts, "cursor", "Grep", "PostToolUse", cursorCallID, "e@x.com", "Support")

	testenv.FlushClickHouseAsyncInserts(t, ti.chConn)

	from := now.Add(-1 * time.Hour).Format(time.RFC3339)
	to := now.Add(1 * time.Hour).Format(time.RFC3339)

	// Wait until the aggregate MV has materialized every seeded row: six
	// distinct users and all three tool calls.
	require.Eventually(t, func() bool {
		res, err := ti.service.Query(ctx, &gen.QueryPayload{
			From:    from,
			To:      to,
			GroupBy: conv.PtrEmpty("email"),
			TopN:    10,
			SortBy:  "total_cost",
		})
		if err != nil || res == nil || len(res.Table) != 6 {
			return false
		}
		var toolCalls int64
		for _, row := range res.Table {
			toolCalls += row.Measures.TotalToolCalls
		}
		return toolCalls == 3
	}, 10*time.Second, 200*time.Millisecond)

	// Every public group_by dimension except skill_version (kept on the
	// legacy raw+mappings path). Mirrors queryDimensions in
	// server/design/telemetry/design.go.
	groupByDims := []string{
		"department_name", "job_title", "employee_type", "division_name",
		"cost_center_name", "email", "hostname", "model", "hook_source",
		"account_type", "provider", "billing_mode", "query_source",
		"skill_name", "agent_name", "mcp_server_name", "mcp_tool_name",
		"role", "group", "project_id",
	}

	type matrixCase struct {
		name    string
		payload *gen.QueryPayload
	}
	cases := make([]matrixCase, 0, len(groupByDims)+6)
	for _, dim := range groupByDims {
		cases = append(cases, matrixCase{
			name: "group_by " + dim,
			payload: &gen.QueryPayload{
				From: from, To: to,
				GroupBy:            conv.PtrEmpty(dim),
				GranularitySeconds: conv.PtrEmpty(int64(3600)),
				TopN:               10,
				SortBy:             "total_cost",
			},
		})
	}
	cases = append(cases,
		matrixCase{
			name: "top_n=2 rollup into Other",
			payload: &gen.QueryPayload{
				From: from, To: to,
				GroupBy:            conv.PtrEmpty("email"),
				GranularitySeconds: conv.PtrEmpty(int64(3600)),
				TopN:               2,
				SortBy:             "total_cost",
			},
		},
		matrixCase{
			name: "multi-filter drilldown: department + model",
			payload: &gen.QueryPayload{
				From: from, To: to,
				GroupBy: conv.PtrEmpty("email"),
				Filters: []*gen.QueryFilter{
					{Dimension: "department_name", Values: []string{"Engineering"}},
					{Dimension: "model", Values: []string{"opus"}},
				},
				GranularitySeconds: conv.PtrEmpty(int64(3600)),
				TopN:               10,
				SortBy:             "total_cost",
			},
		},
		matrixCase{
			name: "array-dim filter including the '' unset bucket",
			payload: &gen.QueryPayload{
				From: from, To: to,
				GroupBy:            conv.PtrEmpty("department_name"),
				Filters:            []*gen.QueryFilter{{Dimension: "role", Values: []string{"dev", ""}}},
				GranularitySeconds: conv.PtrEmpty(int64(3600)),
				TopN:               10,
				SortBy:             "total_cost",
			},
		},
		matrixCase{
			name: "sort_by total_tool_calls",
			payload: &gen.QueryPayload{
				From: from, To: to,
				GroupBy:            conv.PtrEmpty("hook_source"),
				GranularitySeconds: conv.PtrEmpty(int64(3600)),
				TopN:               10,
				SortBy:             "total_tool_calls",
			},
		},
		matrixCase{
			name: "sort_by cache_creation_input_tokens",
			payload: &gen.QueryPayload{
				From: from, To: to,
				GroupBy:            conv.PtrEmpty("email"),
				Filters:            []*gen.QueryFilter{{Dimension: "hook_source", Values: []string{"claude-code"}}},
				GranularitySeconds: conv.PtrEmpty(int64(3600)),
				TopN:               10,
				SortBy:             "cache_creation_input_tokens",
			},
		},
		matrixCase{
			name: "custom granularity_seconds",
			payload: &gen.QueryPayload{
				From: from, To: to,
				GroupBy:            conv.PtrEmpty("email"),
				GranularitySeconds: conv.PtrEmpty(int64(7200)),
				TopN:               10,
				SortBy:             "total_cost",
			},
		},
	)

	for _, tc := range cases {
		legacy, err := ti.service.QueryLegacyForTest(ctx, tc.payload)
		require.NoError(t, err, "legacy path failed for %s", tc.name)
		require.NotEmpty(t, legacy.Table, "legacy path returned no rows for %s — parity would be vacuous", tc.name)

		rewired, err := ti.service.Query(ctx, tc.payload)
		require.NoError(t, err, "rewired Query failed for %s", tc.name)
		sortDimensionValues(legacy)
		sortDimensionValues(rewired)
		require.Equal(t, legacy, rewired, "rewired Query diverged from the legacy path for %s", tc.name)
	}
}
