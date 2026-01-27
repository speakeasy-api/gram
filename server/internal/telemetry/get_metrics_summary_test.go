package telemetry_test

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/google/uuid"
	gen "github.com/speakeasy-api/gram/server/gen/telemetry"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/stretchr/testify/require"
)

func TestGetMetricsSummary_LogsDisabled(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestLogsService(t)
	ctx = switchOrganizationInCtx(t, ctx)

	now := time.Now().UTC()
	from := now.Add(-1 * time.Hour).Format(time.RFC3339)
	to := now.Add(1 * time.Hour).Format(time.RFC3339)

	_, err := ti.service.GetMetricsSummary(ctx, &gen.GetMetricsSummaryPayload{
		Scope: "project",
		From:  from,
		To:    to,
	})

	require.Error(t, err)
	require.Contains(t, err.Error(), "telemetry is not enabled")
}

func TestGetMetricsSummary_Empty(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestLogsService(t)

	now := time.Now().UTC()
	from := now.Add(-1 * time.Hour).Format(time.RFC3339)
	to := now.Add(1 * time.Hour).Format(time.RFC3339)

	result, err := ti.service.GetMetricsSummary(ctx, &gen.GetMetricsSummaryPayload{
		Scope: "project",
		From:  from,
		To:    to,
	})

	require.NoError(t, err)
	require.NotNil(t, result)
	require.True(t, result.Enabled)
	require.NotNil(t, result.Metrics)
	require.Equal(t, int64(0), result.Metrics.TotalChatRequests)
	require.Equal(t, int64(0), result.Metrics.TotalToolCalls)
}

func TestGetMetricsSummary_InvalidScope(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestLogsService(t)

	now := time.Now().UTC()
	from := now.Add(-1 * time.Hour).Format(time.RFC3339)
	to := now.Add(1 * time.Hour).Format(time.RFC3339)

	_, err := ti.service.GetMetricsSummary(ctx, &gen.GetMetricsSummaryPayload{
		Scope: "invalid",
		From:  from,
		To:    to,
	})

	require.Error(t, err)
	require.Contains(t, err.Error(), "scope must be 'project' or 'chat'")
}

func TestGetMetricsSummary_ChatScopeMissingChatID(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestLogsService(t)

	now := time.Now().UTC()
	from := now.Add(-1 * time.Hour).Format(time.RFC3339)
	to := now.Add(1 * time.Hour).Format(time.RFC3339)

	_, err := ti.service.GetMetricsSummary(ctx, &gen.GetMetricsSummaryPayload{
		Scope: "chat",
		From:  from,
		To:    to,
	})

	require.Error(t, err)
	require.Contains(t, err.Error(), "chat_id is required when scope is 'chat'")
}

func TestGetMetricsSummary_ProjectScope(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestLogsService(t)

	authCtx, _ := contextvalues.GetAuthContext(ctx)
	projectID := authCtx.ProjectID.String()
	deploymentID := uuid.New().String()

	now := time.Now().UTC()
	chatID1 := uuid.New().String()
	chatID2 := uuid.New().String()

	// Insert chat completion logs for chat 1
	insertChatCompletionLog(t, ctx, projectID, deploymentID, now.Add(-10*time.Minute), chatID1, 100, 50, 150, 1.5, "stop", "gpt-4", "openai")
	insertChatCompletionLog(t, ctx, projectID, deploymentID, now.Add(-9*time.Minute), chatID1, 200, 100, 300, 2.0, "tool_calls", "gpt-4", "openai")

	// Insert chat completion logs for chat 2
	insertChatCompletionLog(t, ctx, projectID, deploymentID, now.Add(-8*time.Minute), chatID2, 150, 75, 225, 1.8, "stop", "claude-3", "anthropic")

	// Insert tool call logs
	insertToolCallLog(t, ctx, projectID, deploymentID, now.Add(-7*time.Minute), "tools:http:petstore:listPets", 200, 0.5)
	insertToolCallLog(t, ctx, projectID, deploymentID, now.Add(-6*time.Minute), "tools:http:petstore:getPet", 500, 1.0)
	insertToolCallLog(t, ctx, projectID, deploymentID, now.Add(-5*time.Minute), "tools:http:weather:forecast", 200, 0.3)

	// Wait for ClickHouse eventual consistency
	time.Sleep(200 * time.Millisecond)

	from := now.Add(-1 * time.Hour).Format(time.RFC3339)
	to := now.Add(1 * time.Hour).Format(time.RFC3339)

	result, err := ti.service.GetMetricsSummary(ctx, &gen.GetMetricsSummaryPayload{
		Scope: "project",
		From:  from,
		To:    to,
	})

	require.NoError(t, err)
	require.NotNil(t, result)
	require.True(t, result.Enabled)
	require.Equal(t, gen.MetricsScope("project"), result.Scope)

	m := result.Metrics
	require.NotNil(t, m)

	// Token metrics (sum of all chat completions)
	require.Equal(t, int64(450), m.TotalInputTokens)  // 100 + 200 + 150
	require.Equal(t, int64(225), m.TotalOutputTokens) // 50 + 100 + 75
	require.Equal(t, int64(675), m.TotalTokens)       // 150 + 300 + 225

	// Chat request metrics
	require.Equal(t, int64(3), m.TotalChatRequests)
	require.Greater(t, m.AvgChatDurationMs, float64(0))

	// Resolution status
	require.Equal(t, int64(2), m.FinishReasonStop)      // 2 "stop"
	require.Equal(t, int64(1), m.FinishReasonToolCalls) // 1 "tool_calls"

	// Tool call metrics
	require.Equal(t, int64(3), m.TotalToolCalls)
	require.Equal(t, int64(2), m.ToolCallSuccess) // 2 with status 200
	require.Equal(t, int64(1), m.ToolCallFailure) // 1 with status 500

	// Cardinality (project scope only)
	require.Equal(t, int64(2), m.TotalChats)        // 2 distinct chat IDs
	require.Equal(t, int64(2), m.DistinctModels)    // gpt-4, claude-3
	require.Equal(t, int64(2), m.DistinctProviders) // openai, anthropic

	// Model breakdown
	require.Len(t, m.Models, 2)
	modelCounts := make(map[string]int64)
	for _, model := range m.Models {
		modelCounts[model.Name] = model.Count
	}
	require.Equal(t, int64(2), modelCounts["gpt-4"])   // 2 chat completions with gpt-4
	require.Equal(t, int64(1), modelCounts["claude-3"]) // 1 chat completion with claude-3

	// Tool breakdown
	require.Len(t, m.Tools, 3)
	toolStats := make(map[string]*gen.ToolUsage)
	for _, tool := range m.Tools {
		toolStats[tool.Urn] = tool
	}
	require.Equal(t, int64(1), toolStats["tools:http:petstore:listPets"].Count)
	require.Equal(t, int64(1), toolStats["tools:http:petstore:listPets"].SuccessCount)
	require.Equal(t, int64(0), toolStats["tools:http:petstore:listPets"].FailureCount)
	require.Equal(t, int64(1), toolStats["tools:http:petstore:getPet"].Count)
	require.Equal(t, int64(0), toolStats["tools:http:petstore:getPet"].SuccessCount)
	require.Equal(t, int64(1), toolStats["tools:http:petstore:getPet"].FailureCount) // status 500
	require.Equal(t, int64(1), toolStats["tools:http:weather:forecast"].Count)
	require.Equal(t, int64(1), toolStats["tools:http:weather:forecast"].SuccessCount)
	require.Equal(t, int64(0), toolStats["tools:http:weather:forecast"].FailureCount)
}

func TestGetMetricsSummary_ChatScope(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestLogsService(t)

	authCtx, _ := contextvalues.GetAuthContext(ctx)
	projectID := authCtx.ProjectID.String()
	deploymentID := uuid.New().String()

	now := time.Now().UTC()
	chatID1 := uuid.New().String()
	chatID2 := uuid.New().String()

	// Insert chat completion logs for chat 1
	insertChatCompletionLog(t, ctx, projectID, deploymentID, now.Add(-10*time.Minute), chatID1, 100, 50, 150, 1.5, "stop", "gpt-4", "openai")
	insertChatCompletionLog(t, ctx, projectID, deploymentID, now.Add(-9*time.Minute), chatID1, 200, 100, 300, 2.0, "tool_calls", "gpt-4", "openai")

	// Insert chat completion logs for chat 2 (should not be included)
	insertChatCompletionLog(t, ctx, projectID, deploymentID, now.Add(-8*time.Minute), chatID2, 500, 250, 750, 3.0, "stop", "claude-3", "anthropic")

	// Wait for ClickHouse eventual consistency
	time.Sleep(200 * time.Millisecond)

	from := now.Add(-1 * time.Hour).Format(time.RFC3339)
	to := now.Add(1 * time.Hour).Format(time.RFC3339)

	result, err := ti.service.GetMetricsSummary(ctx, &gen.GetMetricsSummaryPayload{
		Scope:  "chat",
		From:   from,
		To:     to,
		ChatID: &chatID1,
	})

	require.NoError(t, err)
	require.NotNil(t, result)
	require.True(t, result.Enabled)
	require.Equal(t, gen.MetricsScope("chat"), result.Scope)

	m := result.Metrics
	require.NotNil(t, m)

	// Token metrics (only chat 1)
	require.Equal(t, int64(300), m.TotalInputTokens)  // 100 + 200
	require.Equal(t, int64(150), m.TotalOutputTokens) // 50 + 100
	require.Equal(t, int64(450), m.TotalTokens)       // 150 + 300

	// Chat request metrics (only chat 1)
	require.Equal(t, int64(2), m.TotalChatRequests)

	// Resolution status (only chat 1)
	require.Equal(t, int64(1), m.FinishReasonStop)
	require.Equal(t, int64(1), m.FinishReasonToolCalls)

	// Cardinality should be 0 for chat scope
	require.Equal(t, int64(0), m.TotalChats)
	require.Equal(t, int64(0), m.DistinctModels)
	require.Equal(t, int64(0), m.DistinctProviders)
}

func insertChatCompletionLog(t *testing.T, ctx context.Context, projectID, deploymentID string, timestamp time.Time, chatID string, inputTokens, outputTokens, totalTokens int, durationSec float64, finishReason, model, provider string) {
	t.Helper()
	insertChatCompletionLogWithDeployment(t, ctx, projectID, deploymentID, timestamp, chatID, inputTokens, outputTokens, totalTokens, durationSec, finishReason, model, provider)
}

func insertChatCompletionLogWithDeployment(t *testing.T, ctx context.Context, projectID, deploymentID string, timestamp time.Time, chatID string, inputTokens, outputTokens, totalTokens int, durationSec float64, finishReason, model, provider string) {
	t.Helper()

	conn, err := infra.NewClickhouseClient(t)
	require.NoError(t, err)

	id, err := uuid.NewV7()
	require.NoError(t, err)

	attributes := map[string]any{
		"gen_ai.conversation.id":        chatID,
		"gen_ai.conversation.duration":  durationSec,
		"gen_ai.response.finish_reasons": "['" + finishReason + "']",
		"gen_ai.usage.input_tokens":     inputTokens,
		"gen_ai.usage.output_tokens":    outputTokens,
		"gen_ai.usage.total_tokens":     totalTokens,
		"gen_ai.response.model":         model,
		"gen_ai.provider.name":          provider,
		"gram.resource.urn":             "agents:chat:completion",
	}

	attrsJSON, err := json.Marshal(attributes)
	require.NoError(t, err)

	err = conn.Exec(ctx, `
		INSERT INTO telemetry_logs (
			id, time_unix_nano, observed_time_unix_nano, severity_text, body,
			trace_id, span_id, attributes, resource_attributes,
			gram_project_id, gram_deployment_id, gram_urn, service_name
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
	`, id.String(), timestamp.UnixNano(), timestamp.UnixNano(), "INFO", "chat completion",
		nil, nil, string(attrsJSON), "{}",
		projectID, deploymentID, "agents:chat:completion", "gram-agents")
	require.NoError(t, err)
}

func insertToolCallLog(t *testing.T, ctx context.Context, projectID, deploymentID string, timestamp time.Time, toolURN string, statusCode int32, durationSec float64) {
	t.Helper()

	conn, err := infra.NewClickhouseClient(t)
	require.NoError(t, err)

	id, err := uuid.NewV7()
	require.NoError(t, err)

	attributes := map[string]any{
		"gram.tool.urn":                toolURN,
		"http.server.request.duration": durationSec,
	}

	attrsJSON, err := json.Marshal(attributes)
	require.NoError(t, err)

	err = conn.Exec(ctx, `
		INSERT INTO telemetry_logs (
			id, time_unix_nano, observed_time_unix_nano, severity_text, body,
			trace_id, span_id, attributes, resource_attributes,
			gram_project_id, gram_deployment_id, gram_urn, service_name,
			http_response_status_code
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
	`, id.String(), timestamp.UnixNano(), timestamp.UnixNano(), "INFO", "tool call",
		nil, nil, string(attrsJSON), "{}",
		projectID, deploymentID, toolURN, "gram-tools",
		statusCode)
	require.NoError(t, err)
}
