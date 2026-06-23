package telemetry_test

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/google/uuid"
	gen "github.com/speakeasy-api/gram/server/gen/telemetry"
	"github.com/speakeasy-api/gram/server/internal/authz"
	"github.com/speakeasy-api/gram/server/internal/authztest"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/conv"
	"github.com/speakeasy-api/gram/server/internal/telemetry/repo"
	"github.com/stretchr/testify/require"
)

func TestGetClaudeTurnUsageByChatIDs_MultipleTurns(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestLogsService(t)
	authCtx, _ := contextvalues.GetAuthContext(ctx)
	projectID := authCtx.ProjectID.String()
	deploymentID := uuid.New().String()
	chatID := uuid.New().String()
	now := time.Now().UTC()

	insertClaudeOTELLog(t, ctx, claudeOTELLogParams{
		projectID: projectID, deploymentID: deploymentID, chatID: chatID,
		timestamp: now, promptID: "prompt-1", eventName: "user_prompt",
	})
	insertClaudeOTELLog(t, ctx, claudeOTELLogParams{
		projectID: projectID, deploymentID: deploymentID, chatID: chatID,
		timestamp: now.Add(time.Second), promptID: "prompt-1", eventName: "api_request",
		inputTokens: 10, outputTokens: 5, cacheReadTokens: 2, cacheCreationTokens: 3,
		costUSD: 0.001, costMicros: 1000, model: "claude-sonnet-4-6", querySource: "sdk",
	})
	insertClaudeOTELLog(t, ctx, claudeOTELLogParams{
		projectID: projectID, deploymentID: deploymentID, chatID: chatID,
		timestamp: now.Add(10 * time.Second), promptID: "prompt-2", eventName: "user_prompt",
	})
	insertClaudeOTELLog(t, ctx, claudeOTELLogParams{
		projectID: projectID, deploymentID: deploymentID, chatID: chatID,
		timestamp: now.Add(11 * time.Second), promptID: "prompt-2", eventName: "api_request",
		inputTokens: 20, outputTokens: 7, cacheReadTokens: 4, cacheCreationTokens: 1,
		costUSD: 0.0025, costMicros: 2500, model: "claude-haiku-4-5-20251001", querySource: "generate_session_title",
	})

	got := requireClaudeTurnUsageEventually(ctx, t, ti, repo.GetClaudeTurnUsageByChatIDsParams{
		GramProjectID: projectID,
		ChatIDs:       []string{chatID},
	}, chatID, 2)
	require.Equal(t, "prompt-1", got[chatID][0].PromptID)
	require.Equal(t, int64(20), got[chatID][0].TotalTokens)
	require.Equal(t, []string{"claude-sonnet-4-6"}, got[chatID][0].Models)
	require.Equal(t, []string{"sdk"}, got[chatID][0].QuerySources)
	require.Equal(t, "prompt-2", got[chatID][1].PromptID)
	require.Equal(t, int64(32), got[chatID][1].TotalTokens)
	require.Equal(t, []string{"claude-haiku-4-5-20251001"}, got[chatID][1].Models)
	require.Equal(t, []string{"generate_session_title"}, got[chatID][1].QuerySources)
}

func TestGetClaudeTurnUsageByChatIDs_MultipleAPIRequestsInTurn(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestLogsService(t)
	authCtx, _ := contextvalues.GetAuthContext(ctx)
	projectID := authCtx.ProjectID.String()
	deploymentID := uuid.New().String()
	chatID := uuid.New().String()
	now := time.Now().UTC()

	insertClaudeOTELLog(t, ctx, claudeOTELLogParams{
		projectID: projectID, deploymentID: deploymentID, chatID: chatID,
		timestamp: now, promptID: "prompt-1", eventName: "api_request",
		inputTokens: 3, outputTokens: 129, cacheReadTokens: 18658, cacheCreationTokens: 7688,
		costUSD: 0.0363714, costMicros: 36371, model: "claude-sonnet-4-6", querySource: "sdk",
	})
	insertClaudeOTELLog(t, ctx, claudeOTELLogParams{
		projectID: projectID, deploymentID: deploymentID, chatID: chatID,
		timestamp: now.Add(time.Second), promptID: "prompt-1", eventName: "api_request",
		inputTokens: 1, outputTokens: 56, cacheReadTokens: 26346, cacheCreationTokens: 186,
		costUSD: 0.0094443, costMicros: 9444, model: "claude-sonnet-4-6", querySource: "sdk",
	})

	got := requireClaudeTurnUsageEventually(ctx, t, ti, repo.GetClaudeTurnUsageByChatIDsParams{
		GramProjectID: projectID,
		ChatIDs:       []string{chatID},
	}, chatID, 1)

	turn := got[chatID][0]
	require.Equal(t, uint64(2), turn.RequestCount)
	require.Equal(t, int64(4), turn.InputTokens)
	require.Equal(t, int64(185), turn.OutputTokens)
	require.Equal(t, int64(45004), turn.CacheReadTokens)
	require.Equal(t, int64(7874), turn.CacheCreationTokens)
	require.Equal(t, int64(53067), turn.TotalTokens)
	require.InDelta(t, 0.0458157, turn.CostUSD, 0.0000001)
	require.Equal(t, int64(45815), turn.CostMicros)
	require.Equal(t, []string{"claude-sonnet-4-6"}, turn.Models)
	require.Equal(t, []string{"sdk"}, turn.QuerySources)
}

func TestGetClaudeTurnUsageByChatIDs_NoCostBearingRequest(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestLogsService(t)
	authCtx, _ := contextvalues.GetAuthContext(ctx)
	projectID := authCtx.ProjectID.String()
	deploymentID := uuid.New().String()
	chatID := uuid.New().String()

	insertClaudeOTELLog(t, ctx, claudeOTELLogParams{
		projectID: projectID, deploymentID: deploymentID, chatID: chatID,
		timestamp: time.Now().UTC(), promptID: "prompt-without-request", eventName: "tool_result",
	})

	got := requireClaudeTurnUsageEventually(ctx, t, ti, repo.GetClaudeTurnUsageByChatIDsParams{
		GramProjectID: projectID,
		ChatIDs:       []string{chatID},
	}, chatID, 1)

	turn := got[chatID][0]
	require.Equal(t, "prompt-without-request", turn.PromptID)
	require.Equal(t, uint64(0), turn.RequestCount)
	require.Equal(t, int64(0), turn.TotalTokens)
	require.Zero(t, turn.CostUSD)
	require.Empty(t, turn.Models)
	require.Empty(t, turn.QuerySources)
}

func TestGetClaudeTurnUsageByChatIDs_NoOTELData(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestLogsService(t)
	authCtx, _ := contextvalues.GetAuthContext(ctx)
	projectID := authCtx.ProjectID.String()
	chatID := uuid.New().String()

	got, err := ti.chClient.GetClaudeTurnUsageByChatIDs(ctx, repo.GetClaudeTurnUsageByChatIDsParams{
		GramProjectID: projectID,
		ChatIDs:       []string{chatID},
	})
	require.NoError(t, err)
	require.Contains(t, got, chatID)
	require.Empty(t, got[chatID])
}

func TestGetClaudeToolUsageByChatIDs(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestLogsService(t)
	authCtx, _ := contextvalues.GetAuthContext(ctx)
	projectID := authCtx.ProjectID.String()
	deploymentID := uuid.New().String()
	chatID := uuid.New().String()
	now := time.Now().UTC()

	insertClaudeOTELLog(t, ctx, claudeOTELLogParams{
		projectID: projectID, deploymentID: deploymentID, chatID: chatID,
		timestamp: now, promptID: "prompt-1", eventName: "tool_result",
		toolUseID: "toolu_1", toolName: "Bash", toolInputSizeBytes: 256, toolResultSizeBytes: 1024,
	})
	insertClaudeOTELLog(t, ctx, claudeOTELLogParams{
		projectID: projectID, deploymentID: deploymentID, chatID: chatID,
		timestamp: now.Add(time.Second), promptID: "prompt-1", eventName: "tool_result",
		toolUseID: "toolu_2", toolName: "WebFetch", toolInputSizeBytes: 512, toolResultSizeBytes: 4096,
	})

	got := requireClaudeToolUsageEventually(ctx, t, ti, repo.GetClaudeTurnUsageByChatIDsParams{
		GramProjectID: projectID,
		ChatIDs:       []string{chatID},
	}, chatID, 2)

	require.Equal(t, "toolu_1", got[chatID][0].ToolUseID)
	require.Equal(t, "prompt-1", got[chatID][0].PromptID)
	require.Equal(t, "Bash", got[chatID][0].ToolName)
	require.Equal(t, int64(256), got[chatID][0].InputSizeBytes)
	require.Equal(t, int64(1024), got[chatID][0].ResultSizeBytes)

	require.Equal(t, "toolu_2", got[chatID][1].ToolUseID)
	require.Equal(t, "WebFetch", got[chatID][1].ToolName)
	require.Equal(t, int64(512), got[chatID][1].InputSizeBytes)
	require.Equal(t, int64(4096), got[chatID][1].ResultSizeBytes)
}

func TestQueryChatTurns_GroupByAttributionAndFilter(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestLogsService(t)
	authCtx, _ := contextvalues.GetAuthContext(ctx)
	projectID := authCtx.ProjectID.String()
	deploymentID := uuid.New().String()
	chatID := uuid.New().String()
	otherChatID := uuid.New().String()
	now := time.Date(2026, time.June, 23, 19, 0, 0, 0, time.UTC)

	ctx = authztest.WithExactGrants(t, ctx, authz.Grant{
		Scope:    authz.ScopeOrgRead,
		Selector: authz.NewSelector(authz.ScopeOrgRead, authCtx.ActiveOrganizationID),
	})

	insertClaudeOTELLog(t, ctx, claudeOTELLogParams{
		projectID: projectID, deploymentID: deploymentID, chatID: chatID,
		timestamp: now, promptID: "prompt-1", eventName: "api_request",
		inputTokens: 10, outputTokens: 3, cacheReadTokens: 50, cacheCreationTokens: 100,
		costUSD: 0.01, costMicros: 10000, model: "claude-sonnet-4-6", querySource: "main",
		mcpServerName: "filesystem", mcpToolName: "read_file", departmentName: "Engineering", roles: []string{"admin", "dev"},
	})
	insertClaudeOTELLog(t, ctx, claudeOTELLogParams{
		projectID: projectID, deploymentID: deploymentID, chatID: chatID,
		timestamp: now.Add(5 * time.Minute), promptID: "prompt-2", eventName: "api_request",
		inputTokens: 4, outputTokens: 2, cacheReadTokens: 10, cacheCreationTokens: 25,
		costUSD: 0.0025, costMicros: 2500, model: "claude-sonnet-4-6", querySource: "main",
		mcpServerName: "slack", mcpToolName: "post_message", departmentName: "Engineering", roles: []string{"dev"},
	})
	insertClaudeOTELLog(t, ctx, claudeOTELLogParams{
		projectID: projectID, deploymentID: deploymentID, chatID: otherChatID,
		timestamp: now.Add(10 * time.Minute), promptID: "prompt-1", eventName: "api_request",
		inputTokens: 20, outputTokens: 5, cacheReadTokens: 100, cacheCreationTokens: 999,
		costUSD: 0.09, costMicros: 90000, model: "claude-opus-4-1", querySource: "main",
		mcpServerName: "filesystem", mcpToolName: "read_file", departmentName: "Support", roles: []string{"support"},
	})

	from := now.Add(-time.Hour).Format(time.RFC3339)
	to := now.Add(time.Hour).Format(time.RFC3339)

	var serverResult *gen.QueryChatTurnsResult
	require.Eventually(t, func() bool {
		res, err := ti.service.QueryChatTurns(ctx, &gen.QueryChatTurnsPayload{
			From:    from,
			To:      to,
			GroupBy: conv.PtrEmpty("mcp_server_name"),
			TopN:    10,
			SortBy:  "cache_creation_tokens",
		})
		if err != nil || res == nil {
			return false
		}
		serverResult = res
		return len(res.Table) == 2
	}, 10*time.Second, 200*time.Millisecond)

	require.Equal(t, "mcp_server_name", serverResult.GroupBy)
	require.Len(t, serverResult.Timeseries, 2)
	serverTokens := chatTurnCacheCreationByGroup(serverResult.Table)
	require.Equal(t, int64(1099), serverTokens["filesystem"])
	require.Equal(t, int64(25), serverTokens["slack"])

	toolResult, err := ti.service.QueryChatTurns(ctx, &gen.QueryChatTurnsPayload{
		From:    from,
		To:      to,
		GroupBy: conv.PtrEmpty("mcp_tool_name"),
		Filters: []*gen.ChatTurnQueryFilter{
			{Dimension: "chat_id", Values: []string{chatID}},
			{Dimension: "role", Values: []string{"dev"}},
		},
		TopN:   10,
		SortBy: "cache_creation_tokens",
	})
	require.NoError(t, err)

	require.Len(t, toolResult.Table, 2)
	toolTokens := chatTurnCacheCreationByGroup(toolResult.Table)
	require.Equal(t, int64(100), toolTokens["read_file"])
	require.Equal(t, int64(25), toolTokens["post_message"])

	readFile := chatTurnRowByGroup(t, toolResult.Table, "read_file")
	require.ElementsMatch(t, []string{chatID}, readFile.DimensionValues["chat_id"])
	require.ElementsMatch(t, []string{"prompt-1"}, readFile.DimensionValues["turn_id"])
	require.ElementsMatch(t, []string{"filesystem"}, readFile.DimensionValues["mcp_server_name"])
	require.Equal(t, int64(163), readFile.Measures.TotalTokens)

	postMessage := chatTurnRowByGroup(t, toolResult.Table, "post_message")
	require.Equal(t, int64(41), postMessage.Measures.TotalTokens)
}

func chatTurnCacheCreationByGroup(rows []*gen.ChatTurnQueryRow) map[string]int64 {
	out := make(map[string]int64, len(rows))
	for _, r := range rows {
		out[r.GroupValue] = r.Measures.CacheCreationTokens
	}
	return out
}

func chatTurnRowByGroup(t *testing.T, rows []*gen.ChatTurnQueryRow, group string) *gen.ChatTurnQueryRow {
	t.Helper()
	for _, r := range rows {
		if r.GroupValue == group {
			return r
		}
	}
	t.Fatalf("chat turn row for group %q not found", group)
	return nil
}

func requireClaudeTurnUsageEventually(
	ctx context.Context,
	t *testing.T,
	ti *testInstance,
	params repo.GetClaudeTurnUsageByChatIDsParams,
	chatID string,
	expectedRows int,
) map[string][]repo.ClaudeTurnUsageRow {
	t.Helper()

	var got map[string][]repo.ClaudeTurnUsageRow
	require.Eventually(t, func() bool {
		var err error
		got, err = ti.chClient.GetClaudeTurnUsageByChatIDs(ctx, params)
		return err == nil && len(got[chatID]) == expectedRows
	}, 2*time.Second, 50*time.Millisecond)

	return got
}

func requireClaudeToolUsageEventually(
	ctx context.Context,
	t *testing.T,
	ti *testInstance,
	params repo.GetClaudeTurnUsageByChatIDsParams,
	chatID string,
	expectedRows int,
) map[string][]repo.ClaudeToolUsageRow {
	t.Helper()

	var got map[string][]repo.ClaudeToolUsageRow
	require.Eventually(t, func() bool {
		var err error
		got, err = ti.chClient.GetClaudeToolUsageByChatIDs(ctx, params)
		return err == nil && len(got[chatID]) == expectedRows
	}, 2*time.Second, 50*time.Millisecond)

	return got
}

type claudeOTELLogParams struct {
	projectID           string
	deploymentID        string
	chatID              string
	timestamp           time.Time
	promptID            string
	eventName           string
	inputTokens         int64
	outputTokens        int64
	cacheReadTokens     int64
	cacheCreationTokens int64
	costUSD             float64
	costMicros          int64
	model               string
	querySource         string
	skillName           string
	agentName           string
	mcpServerName       string
	mcpToolName         string
	departmentName      string
	roles               []string
	toolUseID           string
	toolName            string
	toolInputSizeBytes  int64
	toolResultSizeBytes int64
}

func insertClaudeOTELLog(t *testing.T, ctx context.Context, p claudeOTELLogParams) {
	t.Helper()

	conn, err := infra.NewClickhouseClient(t)
	require.NoError(t, err)

	id, err := uuid.NewV7()
	require.NoError(t, err)

	attributes := map[string]any{
		"event.name":             p.eventName,
		"gen_ai.conversation.id": p.chatID,
		"prompt.id":              p.promptID,
		"session.id":             p.chatID,
		"user.email":             "claude-user@example.com",
		"user.id":                "claude-user",
		"organization.id":        uuid.New().String(),
	}
	if p.inputTokens != 0 {
		attributes["input_tokens"] = p.inputTokens
	}
	if p.outputTokens != 0 {
		attributes["output_tokens"] = p.outputTokens
	}
	if p.cacheReadTokens != 0 {
		attributes["cache_read_tokens"] = p.cacheReadTokens
	}
	if p.cacheCreationTokens != 0 {
		attributes["cache_creation_tokens"] = p.cacheCreationTokens
	}
	if p.costUSD != 0 {
		attributes["cost_usd"] = p.costUSD
	}
	if p.costMicros != 0 {
		attributes["cost_usd_micros"] = p.costMicros
	}
	if p.model != "" {
		attributes["model"] = p.model
	}
	if p.querySource != "" {
		attributes["query_source"] = p.querySource
	}
	if p.skillName != "" {
		attributes["skill.name"] = p.skillName
	}
	if p.agentName != "" {
		attributes["agent.name"] = p.agentName
	}
	if p.mcpServerName != "" {
		attributes["mcp_server.name"] = p.mcpServerName
	}
	if p.mcpToolName != "" {
		attributes["mcp_tool.name"] = p.mcpToolName
	}
	if p.departmentName != "" {
		attributes["user.attributes.department_name"] = p.departmentName
	}
	if p.roles != nil {
		attributes["user.roles"] = p.roles
	}
	if p.toolUseID != "" {
		attributes["tool_use_id"] = p.toolUseID
	}
	if p.toolName != "" {
		attributes["tool_name"] = p.toolName
	}
	if p.toolInputSizeBytes != 0 {
		attributes["tool_input_size_bytes"] = p.toolInputSizeBytes
	}
	if p.toolResultSizeBytes != 0 {
		attributes["tool_result_size_bytes"] = p.toolResultSizeBytes
	}

	attrsJSON, err := json.Marshal(attributes)
	require.NoError(t, err)
	resourceAttrsJSON, err := json.Marshal(map[string]any{"service.name": "claude-code"})
	require.NoError(t, err)

	err = conn.Exec(ctx, `
		INSERT INTO telemetry_logs (
			id, time_unix_nano, observed_time_unix_nano, severity_text, body,
			trace_id, span_id, attributes, resource_attributes,
			gram_project_id, gram_deployment_id, gram_urn, service_name,
			gram_chat_id
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
	`, id.String(), p.timestamp.UnixNano(), p.timestamp.UnixNano(), "INFO", "claude_code."+p.eventName,
		nil, nil, string(attrsJSON), string(resourceAttrsJSON),
		p.projectID, p.deploymentID, "claude-code:otel:"+p.eventName, "claude-code",
		p.chatID)
	require.NoError(t, err)
}
