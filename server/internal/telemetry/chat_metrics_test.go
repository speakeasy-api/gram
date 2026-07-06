package telemetry_test

import (
	"context"
	"encoding/json"
	"math"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/telemetry/repo"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestGetChatMetricsByIDs_UsesMaterializedChatID(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestLogsService(t)

	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	projectID := authCtx.ProjectID.String()
	chatID := uuid.NewString()
	now := time.Now().UTC()

	insertChatCompletionMetricLog(t, ctx, projectID, chatID, now, 12, 8, 20, 0.42)

	require.EventuallyWithT(t, func(c *assert.CollectT) {
		got, err := ti.chClient.GetChatMetricsByIDs(ctx, repo.GetChatMetricsByIDsParams{
			GramProjectID: projectID,
			ChatIDs:       []string{chatID},
		})
		require.NoError(c, err)
		row, ok := got[chatID]
		require.True(c, ok)
		require.Equal(c, int64(12), row.TotalInputTokens)
		require.Equal(c, int64(8), row.TotalOutputTokens)
		require.Equal(c, int64(20), row.TotalTokens)
		require.Less(c, math.Abs(row.TotalCost-0.42), 1e-9)
	}, 10*time.Second, 200*time.Millisecond)
}

func insertChatCompletionMetricLog(t *testing.T, ctx context.Context, projectID, chatID string, timestamp time.Time, inputTokens, outputTokens, totalTokens int, cost float64) {
	t.Helper()

	conn, err := infra.NewClickhouseClient(t)
	require.NoError(t, err)

	id, err := uuid.NewV7()
	require.NoError(t, err)

	attributes := map[string]any{
		"gen_ai.conversation.id":     chatID,
		"gen_ai.operation.name":      "chat",
		"gen_ai.response.id":         uuid.NewString(),
		"gen_ai.response.model":      "openai/gpt-5.4",
		"gen_ai.usage.input_tokens":  inputTokens,
		"gen_ai.usage.output_tokens": outputTokens,
		"gen_ai.usage.total_tokens":  totalTokens,
		"gen_ai.usage.cost":          cost,
		"gram.hook.source":           "assistants",
		"gram.resource.urn":          "assistants:chat:completion",
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
		projectID, "assistants:chat:completion", "gram-server")
	require.NoError(t, err)
}
