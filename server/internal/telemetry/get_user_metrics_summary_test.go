package telemetry_test

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/google/uuid"
	gen "github.com/speakeasy-api/gram/server/gen/telemetry"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/stretchr/testify/assert"
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

	from := now.Add(-1 * time.Hour).Format(time.RFC3339)
	to := now.Add(1 * time.Hour).Format(time.RFC3339)

	require.EventuallyWithT(t, func(c *assert.CollectT) {
		res, err := ti.service.GetUserMetricsSummary(ctx, &gen.GetUserMetricsSummaryPayload{
			From:   from,
			To:     to,
			UserID: &userID,
		})
		if !assert.NoError(c, err) {
			return
		}
		if !assert.NotNil(c, res) || !assert.NotNil(c, res.Metrics) {
			return
		}

		m := res.Metrics

		// Token metrics (sum of chat completions for target user only)
		assert.Equal(c, int64(300), m.TotalInputTokens)  // 100 + 200
		assert.Equal(c, int64(150), m.TotalOutputTokens) // 50 + 100
		assert.Equal(c, int64(450), m.TotalTokens)       // 150 + 300

		// Chat request metrics
		assert.Equal(c, int64(2), m.TotalChatRequests)
		assert.Greater(c, m.AvgChatDurationMs, float64(0))

		// Resolution status
		assert.Equal(c, int64(1), m.FinishReasonStop)      // 1 "stop"
		assert.Equal(c, int64(1), m.FinishReasonToolCalls) // 1 "tool_calls"

		// Tool call metrics (only target user's tools)
		assert.Equal(c, int64(2), m.TotalToolCalls)
		assert.Equal(c, int64(1), m.ToolCallSuccess) // 1 with status 200
		assert.Equal(c, int64(1), m.ToolCallFailure) // 1 with status 500

		// Cardinality
		assert.Equal(c, int64(1), m.TotalChats)        // 1 distinct chat ID for target user
		assert.Equal(c, int64(1), m.DistinctModels)    // gpt-4 only
		assert.Equal(c, int64(1), m.DistinctProviders) // openai only

		// Model breakdown
		if assert.Len(c, m.Models, 1) {
			assert.Equal(c, "gpt-4", m.Models[0].Name)
			assert.Equal(c, int64(2), m.Models[0].Count)
		}

		// Tool breakdown
		if assert.Len(c, m.Tools, 2) {
			toolStats := make(map[string]*gen.ToolUsage)
			for _, tool := range m.Tools {
				toolStats[tool.Urn] = tool
			}
			assert.Equal(c, int64(1), toolStats["tools:http:petstore:listPets"].Count)
			assert.Equal(c, int64(1), toolStats["tools:http:petstore:listPets"].SuccessCount)
			assert.Equal(c, int64(0), toolStats["tools:http:petstore:listPets"].FailureCount)
			assert.Equal(c, int64(1), toolStats["tools:http:petstore:getPet"].Count)
			assert.Equal(c, int64(0), toolStats["tools:http:petstore:getPet"].SuccessCount)
			assert.Equal(c, int64(1), toolStats["tools:http:petstore:getPet"].FailureCount)
		}
	}, 10*time.Second, 200*time.Millisecond)
}

func TestGetUserMetricsSummary_FallsBackToUserEmail(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestLogsService(t)

	authCtx, _ := contextvalues.GetAuthContext(ctx)
	projectID := authCtx.ProjectID.String()
	deploymentID := uuid.New().String()

	now := time.Now().UTC()
	email := "metrics-unmatched-" + uuid.New().String() + "@example.com"
	insertPollingLogWithEmail(t, ctx, projectID, deploymentID, now.Add(-10*time.Minute), email, 100, 50, 1.25)

	from := now.Add(-1 * time.Hour).Format(time.RFC3339)
	to := now.Add(1 * time.Hour).Format(time.RFC3339)

	require.EventuallyWithT(t, func(c *assert.CollectT) {
		res, err := ti.service.GetUserMetricsSummary(ctx, &gen.GetUserMetricsSummaryPayload{
			From:   from,
			To:     to,
			UserID: &email,
		})
		if !assert.NoError(c, err) {
			return
		}
		if !assert.NotNil(c, res) || !assert.NotNil(c, res.Metrics) {
			return
		}
		assert.Equal(c, int64(100), res.Metrics.TotalInputTokens)
		assert.Equal(c, int64(50), res.Metrics.TotalOutputTokens)
	}, 10*time.Second, 200*time.Millisecond)
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

	from := now.Add(-1 * time.Hour).Format(time.RFC3339)
	to := now.Add(1 * time.Hour).Format(time.RFC3339)

	require.EventuallyWithT(t, func(c *assert.CollectT) {
		res, err := ti.service.GetUserMetricsSummary(ctx, &gen.GetUserMetricsSummaryPayload{
			From:           from,
			To:             to,
			ExternalUserID: &externalUserID,
		})
		if !assert.NoError(c, err) {
			return
		}
		if !assert.NotNil(c, res) || !assert.NotNil(c, res.Metrics) {
			return
		}

		m := res.Metrics

		// Token metrics (only for target external user)
		assert.Equal(c, int64(100), m.TotalInputTokens)
		assert.Equal(c, int64(50), m.TotalOutputTokens)
		assert.Equal(c, int64(150), m.TotalTokens)

		// Chat request metrics
		assert.Equal(c, int64(1), m.TotalChatRequests)

		// Tool call metrics
		assert.Equal(c, int64(1), m.TotalToolCalls)
		assert.Equal(c, int64(1), m.ToolCallSuccess)
		assert.Equal(c, int64(0), m.ToolCallFailure)

		// Cardinality
		assert.Equal(c, int64(1), m.TotalChats)
		assert.Equal(c, int64(1), m.DistinctModels)
		assert.Equal(c, int64(1), m.DistinctProviders)

		// Model breakdown
		if assert.Len(c, m.Models, 1) {
			assert.Equal(c, "claude-3", m.Models[0].Name)
		}
	}, 10*time.Second, 200*time.Millisecond)
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

	from := now.Add(-1 * time.Hour).Format(time.RFC3339)
	to := now.Add(1 * time.Hour).Format(time.RFC3339)

	require.EventuallyWithT(t, func(c *assert.CollectT) {
		res, err := ti.service.GetUserMetricsSummary(ctx, &gen.GetUserMetricsSummaryPayload{
			From:   from,
			To:     to,
			UserID: &userID,
		})
		if !assert.NoError(c, err) {
			return
		}
		if !assert.NotNil(c, res) || !assert.NotNil(c, res.Metrics) {
			return
		}

		m := res.Metrics
		assert.Equal(c, int64(2), m.TotalToolCalls)
		assert.Equal(c, int64(2), m.ToolCallSuccess)
		assert.Equal(c, int64(0), m.ToolCallFailure)

		// Chat-related metrics should be zero
		assert.Equal(c, int64(0), m.TotalChatRequests)
		assert.Equal(c, int64(0), m.TotalChats)
		assert.Equal(c, int64(0), m.TotalInputTokens)
		assert.Equal(c, int64(0), m.TotalOutputTokens)
		assert.Equal(c, int64(0), m.TotalTokens)
		assert.Empty(c, m.Models)

		// But tools should be present
		assert.Len(c, m.Tools, 2)
	}, 10*time.Second, 200*time.Millisecond)
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

	from := now.Add(-1 * time.Hour).Format(time.RFC3339)
	to := now.Add(1 * time.Hour).Format(time.RFC3339)

	require.EventuallyWithT(t, func(c *assert.CollectT) {
		res, err := ti.service.GetUserMetricsSummary(ctx, &gen.GetUserMetricsSummaryPayload{
			From:   from,
			To:     to,
			UserID: &userID,
		})
		if !assert.NoError(c, err) {
			return
		}
		if !assert.NotNil(c, res) || !assert.NotNil(c, res.Metrics) {
			return
		}

		m := res.Metrics
		assert.Equal(c, int64(2), m.TotalChatRequests)
		assert.Equal(c, int64(1), m.TotalChats) // 1 unique chat ID
		assert.Equal(c, int64(300), m.TotalInputTokens)
		assert.Equal(c, int64(150), m.TotalOutputTokens)
		assert.Equal(c, int64(450), m.TotalTokens)

		// Tool metrics should be zero
		assert.Equal(c, int64(0), m.TotalToolCalls)
		assert.Equal(c, int64(0), m.ToolCallSuccess)
		assert.Equal(c, int64(0), m.ToolCallFailure)
		assert.Empty(c, m.Tools)

		// Models should be present
		if assert.Len(c, m.Models, 1) {
			assert.Equal(c, "gpt-4", m.Models[0].Name)
			assert.Equal(c, int64(2), m.Models[0].Count)
		}
	}, 10*time.Second, 200*time.Millisecond)
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
		"gen_ai.response.id":             uuid.New().String(),
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
