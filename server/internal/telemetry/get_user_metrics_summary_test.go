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

func TestGetUserMetricsSummary_LogsDisabled(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestLogsService(t)
	ctx = switchOrganizationInCtx(t, ctx, ti.disabledLogsOrgID)

	now := time.Now().UTC()
	from := now.Add(-1 * time.Hour).Format(time.RFC3339)
	to := now.Add(1 * time.Hour).Format(time.RFC3339)

	userID := "test-user-123"
	_, err := ti.service.GetUserMetricsSummary(ctx, &gen.GetUserMetricsSummaryPayload{
		From:   from,
		To:     to,
		UserID: &userID,
	})

	require.Error(t, err)
	require.Contains(t, err.Error(), "logs are not enabled")
}

func TestGetUserMetricsSummary_MissingUserID(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestLogsService(t)

	now := time.Now().UTC()
	from := now.Add(-1 * time.Hour).Format(time.RFC3339)
	to := now.Add(1 * time.Hour).Format(time.RFC3339)

	// Neither user_id nor external_user_id provided
	_, err := ti.service.GetUserMetricsSummary(ctx, &gen.GetUserMetricsSummaryPayload{
		From: from,
		To:   to,
	})

	require.Error(t, err)
	require.Contains(t, err.Error(), "either user_id or external_user_id is required")
}

func TestGetUserMetricsSummary_BothUserIDsProvided(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestLogsService(t)

	now := time.Now().UTC()
	from := now.Add(-1 * time.Hour).Format(time.RFC3339)
	to := now.Add(1 * time.Hour).Format(time.RFC3339)

	userID := "test-user-123"
	externalUserID := "external-user-456"

	// Both user_id and external_user_id provided
	_, err := ti.service.GetUserMetricsSummary(ctx, &gen.GetUserMetricsSummaryPayload{
		From:           from,
		To:             to,
		UserID:         &userID,
		ExternalUserID: &externalUserID,
	})

	require.Error(t, err)
	require.Contains(t, err.Error(), "only one of user_id or external_user_id can be provided")
}

func TestGetUserMetricsSummary_Empty(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestLogsService(t)

	now := time.Now().UTC()
	from := now.Add(-1 * time.Hour).Format(time.RFC3339)
	to := now.Add(1 * time.Hour).Format(time.RFC3339)

	userID := "nonexistent-user-" + uuid.New().String()
	result, err := ti.service.GetUserMetricsSummary(ctx, &gen.GetUserMetricsSummaryPayload{
		From:   from,
		To:     to,
		UserID: &userID,
	})

	require.NoError(t, err)
	require.NotNil(t, result)
	require.True(t, result.Enabled)
	require.NotNil(t, result.Metrics)
	require.Equal(t, int64(0), result.Metrics.TotalChatRequests)
	require.Equal(t, int64(0), result.Metrics.TotalToolCalls)
}

func TestGetUserMetricsSummary_WithUserID(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestLogsService(t)

	authCtx, _ := contextvalues.GetAuthContext(ctx)
	projectID := authCtx.ProjectID.String()
	deploymentID := uuid.New().String()

	now := time.Now().UTC()
	chatID1 := uuid.New().String()
	chatID2 := uuid.New().String()
	userID := "test-user-" + uuid.New().String()
	otherUserID := "other-user-" + uuid.New().String()

	// Insert chat completion logs for target user
	insertChatCompletionLogWithUser(t, ctx, projectID, deploymentID, now.Add(-10*time.Minute), chatID1, 100, 50, 150, 1.5, "stop", "gpt-4", "openai", userID, "")
	insertChatCompletionLogWithUser(t, ctx, projectID, deploymentID, now.Add(-9*time.Minute), chatID1, 200, 100, 300, 2.0, "tool_calls", "gpt-4", "openai", userID, "")

	// Insert chat completion logs for different user (should not be included)
	insertChatCompletionLogWithUser(t, ctx, projectID, deploymentID, now.Add(-8*time.Minute), chatID2, 500, 250, 750, 3.0, "stop", "claude-3", "anthropic", otherUserID, "")

	// Insert tool call logs for target user
	insertToolCallLogWithUser(t, ctx, projectID, deploymentID, now.Add(-7*time.Minute), "tools:http:petstore:listPets", 200, 0.5, userID, "")
	insertToolCallLogWithUser(t, ctx, projectID, deploymentID, now.Add(-6*time.Minute), "tools:http:petstore:getPet", 500, 1.0, userID, "")

	// Insert tool call for different user (should not be included)
	insertToolCallLogWithUser(t, ctx, projectID, deploymentID, now.Add(-5*time.Minute), "tools:http:weather:forecast", 200, 0.3, otherUserID, "")

	// Wait for ClickHouse eventual consistency
	time.Sleep(200 * time.Millisecond)

	from := now.Add(-1 * time.Hour).Format(time.RFC3339)
	to := now.Add(1 * time.Hour).Format(time.RFC3339)

	result, err := ti.service.GetUserMetricsSummary(ctx, &gen.GetUserMetricsSummaryPayload{
		From:   from,
		To:     to,
		UserID: &userID,
	})

	require.NoError(t, err)
	require.NotNil(t, result)
	require.True(t, result.Enabled)

	m := result.Metrics
	require.NotNil(t, m)

	// Token metrics (sum of chat completions for target user only)
	require.Equal(t, int64(300), m.TotalInputTokens)  // 100 + 200
	require.Equal(t, int64(150), m.TotalOutputTokens) // 50 + 100
	require.Equal(t, int64(450), m.TotalTokens)       // 150 + 300

	// Chat request metrics
	require.Equal(t, int64(2), m.TotalChatRequests)
	require.Greater(t, m.AvgChatDurationMs, float64(0))

	// Resolution status
	require.Equal(t, int64(1), m.FinishReasonStop)      // 1 "stop"
	require.Equal(t, int64(1), m.FinishReasonToolCalls) // 1 "tool_calls"

	// Tool call metrics (only target user's tools)
	require.Equal(t, int64(2), m.TotalToolCalls)
	require.Equal(t, int64(1), m.ToolCallSuccess) // 1 with status 200
	require.Equal(t, int64(1), m.ToolCallFailure) // 1 with status 500

	// Cardinality
	require.Equal(t, int64(1), m.TotalChats)        // 1 distinct chat ID for target user
	require.Equal(t, int64(1), m.DistinctModels)    // gpt-4 only
	require.Equal(t, int64(1), m.DistinctProviders) // openai only

	// Model breakdown
	require.Len(t, m.Models, 1)
	require.Equal(t, "gpt-4", m.Models[0].Name)
	require.Equal(t, int64(2), m.Models[0].Count)

	// Tool breakdown
	require.Len(t, m.Tools, 2)
	toolStats := make(map[string]*gen.ToolUsage)
	for _, tool := range m.Tools {
		toolStats[tool.Urn] = tool
	}
	require.Equal(t, int64(1), toolStats["tools:http:petstore:listPets"].Count)
	require.Equal(t, int64(1), toolStats["tools:http:petstore:listPets"].SuccessCount)
	require.Equal(t, int64(0), toolStats["tools:http:petstore:listPets"].FailureCount)
	require.Equal(t, int64(1), toolStats["tools:http:petstore:getPet"].Count)
	require.Equal(t, int64(0), toolStats["tools:http:petstore:getPet"].SuccessCount)
	require.Equal(t, int64(1), toolStats["tools:http:petstore:getPet"].FailureCount)
}

func TestGetUserMetricsSummary_WithExternalUserID(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestLogsService(t)

	authCtx, _ := contextvalues.GetAuthContext(ctx)
	projectID := authCtx.ProjectID.String()
	deploymentID := uuid.New().String()

	now := time.Now().UTC()
	chatID := uuid.New().String()
	externalUserID := "external-user-" + uuid.New().String()
	otherExternalUserID := "other-external-" + uuid.New().String()

	// Insert chat completion logs for target external user
	insertChatCompletionLogWithUser(t, ctx, projectID, deploymentID, now.Add(-10*time.Minute), chatID, 100, 50, 150, 1.5, "stop", "claude-3", "anthropic", "", externalUserID)

	// Insert chat completion logs for different external user (should not be included)
	insertChatCompletionLogWithUser(t, ctx, projectID, deploymentID, now.Add(-9*time.Minute), uuid.New().String(), 500, 250, 750, 3.0, "stop", "gpt-4", "openai", "", otherExternalUserID)

	// Insert tool call logs for target external user
	insertToolCallLogWithUser(t, ctx, projectID, deploymentID, now.Add(-8*time.Minute), "tools:http:api:call", 200, 0.5, "", externalUserID)

	// Wait for ClickHouse eventual consistency
	time.Sleep(200 * time.Millisecond)

	from := now.Add(-1 * time.Hour).Format(time.RFC3339)
	to := now.Add(1 * time.Hour).Format(time.RFC3339)

	result, err := ti.service.GetUserMetricsSummary(ctx, &gen.GetUserMetricsSummaryPayload{
		From:           from,
		To:             to,
		ExternalUserID: &externalUserID,
	})

	require.NoError(t, err)
	require.NotNil(t, result)
	require.True(t, result.Enabled)

	m := result.Metrics
	require.NotNil(t, m)

	// Token metrics (only for target external user)
	require.Equal(t, int64(100), m.TotalInputTokens)
	require.Equal(t, int64(50), m.TotalOutputTokens)
	require.Equal(t, int64(150), m.TotalTokens)

	// Chat request metrics
	require.Equal(t, int64(1), m.TotalChatRequests)

	// Tool call metrics
	require.Equal(t, int64(1), m.TotalToolCalls)
	require.Equal(t, int64(1), m.ToolCallSuccess)
	require.Equal(t, int64(0), m.ToolCallFailure)

	// Cardinality
	require.Equal(t, int64(1), m.TotalChats)
	require.Equal(t, int64(1), m.DistinctModels)
	require.Equal(t, int64(1), m.DistinctProviders)

	// Model breakdown
	require.Len(t, m.Models, 1)
	require.Equal(t, "claude-3", m.Models[0].Name)
}

func TestGetUserMetricsSummary_OnlyToolCalls(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestLogsService(t)

	authCtx, _ := contextvalues.GetAuthContext(ctx)
	projectID := authCtx.ProjectID.String()
	deploymentID := uuid.New().String()

	now := time.Now().UTC()
	userID := "tool-only-user-" + uuid.New().String()

	// Insert only tool calls, no chat completions
	insertToolCallLogWithUser(t, ctx, projectID, deploymentID, now.Add(-10*time.Minute), "tools:http:petstore:listPets", 200, 0.5, userID, "")
	insertToolCallLogWithUser(t, ctx, projectID, deploymentID, now.Add(-9*time.Minute), "tools:http:petstore:getPet", 200, 0.3, userID, "")

	time.Sleep(200 * time.Millisecond)

	from := now.Add(-1 * time.Hour).Format(time.RFC3339)
	to := now.Add(1 * time.Hour).Format(time.RFC3339)

	result, err := ti.service.GetUserMetricsSummary(ctx, &gen.GetUserMetricsSummaryPayload{
		From:   from,
		To:     to,
		UserID: &userID,
	})

	require.NoError(t, err)
	require.NotNil(t, result)

	m := result.Metrics
	require.Equal(t, int64(2), m.TotalToolCalls)
	require.Equal(t, int64(2), m.ToolCallSuccess)
	require.Equal(t, int64(0), m.ToolCallFailure)

	// Chat-related metrics should be zero
	require.Equal(t, int64(0), m.TotalChatRequests)
	require.Equal(t, int64(0), m.TotalChats)
	require.Equal(t, int64(0), m.TotalInputTokens)
	require.Equal(t, int64(0), m.TotalOutputTokens)
	require.Equal(t, int64(0), m.TotalTokens)
	require.Empty(t, m.Models)

	// But tools should be present
	require.Len(t, m.Tools, 2)
}

func TestGetUserMetricsSummary_OnlyChatCompletions(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestLogsService(t)

	authCtx, _ := contextvalues.GetAuthContext(ctx)
	projectID := authCtx.ProjectID.String()
	deploymentID := uuid.New().String()

	now := time.Now().UTC()
	chatID := uuid.New().String()
	userID := "chat-only-user-" + uuid.New().String()

	// Insert only chat completions, no tool calls
	insertChatCompletionLogWithUser(t, ctx, projectID, deploymentID, now.Add(-10*time.Minute), chatID, 100, 50, 150, 1.5, "stop", "gpt-4", "openai", userID, "")
	insertChatCompletionLogWithUser(t, ctx, projectID, deploymentID, now.Add(-9*time.Minute), chatID, 200, 100, 300, 2.0, "stop", "gpt-4", "openai", userID, "")

	time.Sleep(200 * time.Millisecond)

	from := now.Add(-1 * time.Hour).Format(time.RFC3339)
	to := now.Add(1 * time.Hour).Format(time.RFC3339)

	result, err := ti.service.GetUserMetricsSummary(ctx, &gen.GetUserMetricsSummaryPayload{
		From:   from,
		To:     to,
		UserID: &userID,
	})

	require.NoError(t, err)
	require.NotNil(t, result)

	m := result.Metrics
	require.Equal(t, int64(2), m.TotalChatRequests)
	require.Equal(t, int64(1), m.TotalChats) // 1 unique chat ID
	require.Equal(t, int64(300), m.TotalInputTokens)
	require.Equal(t, int64(150), m.TotalOutputTokens)
	require.Equal(t, int64(450), m.TotalTokens)

	// Tool metrics should be zero
	require.Equal(t, int64(0), m.TotalToolCalls)
	require.Equal(t, int64(0), m.ToolCallSuccess)
	require.Equal(t, int64(0), m.ToolCallFailure)
	require.Empty(t, m.Tools)

	// Models should be present
	require.Len(t, m.Models, 1)
	require.Equal(t, "gpt-4", m.Models[0].Name)
	require.Equal(t, int64(2), m.Models[0].Count)
}

// insertChatCompletionLogWithUser inserts a chat completion log with user identification attributes.
func insertChatCompletionLogWithUser(t *testing.T, ctx context.Context, projectID, deploymentID string, timestamp time.Time, chatID string, inputTokens, outputTokens, totalTokens int, durationSec float64, finishReason, model, provider, userID, externalUserID string) {
	t.Helper()

	conn, err := infra.NewClickhouseClient(t)
	require.NoError(t, err)

	id, err := uuid.NewV7()
	require.NoError(t, err)

	attributes := map[string]any{
		"gen_ai.conversation.id":         chatID,
		"gen_ai.conversation.duration":   durationSec,
		"gen_ai.response.finish_reasons": "['" + finishReason + "']",
		"gen_ai.usage.input_tokens":      inputTokens,
		"gen_ai.usage.output_tokens":     outputTokens,
		"gen_ai.usage.total_tokens":      totalTokens,
		"gen_ai.response.model":          model,
		"gen_ai.provider.name":           provider,
		"gram.resource.urn":              "agents:chat:completion",
	}

	if userID != "" {
		attributes["user.id"] = userID
	}
	if externalUserID != "" {
		attributes["gram.external_user.id"] = externalUserID
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

// insertToolCallLogWithUser inserts a tool call log with user identification attributes.
func insertToolCallLogWithUser(t *testing.T, ctx context.Context, projectID, deploymentID string, timestamp time.Time, toolURN string, statusCode int32, durationSec float64, userID, externalUserID string) {
	t.Helper()

	conn, err := infra.NewClickhouseClient(t)
	require.NoError(t, err)

	id, err := uuid.NewV7()
	require.NoError(t, err)

	attributes := map[string]any{
		"gram.tool.urn":                toolURN,
		"http.server.request.duration": durationSec,
		"http.response.status_code":    statusCode,
	}

	if userID != "" {
		attributes["user.id"] = userID
	}
	if externalUserID != "" {
		attributes["gram.external_user.id"] = externalUserID
	}

	attrsJSON, err := json.Marshal(attributes)
	require.NoError(t, err)

	err = conn.Exec(ctx, `
		INSERT INTO telemetry_logs (
			id, time_unix_nano, observed_time_unix_nano, severity_text, body,
			trace_id, span_id, attributes, resource_attributes,
			gram_project_id, gram_deployment_id, gram_urn, service_name
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
	`, id.String(), timestamp.UnixNano(), timestamp.UnixNano(), "INFO", "tool call",
		nil, nil, string(attrsJSON), "{}",
		projectID, deploymentID, toolURN, "gram-tools")
	require.NoError(t, err)
}
