package openrouter

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/speakeasy-api/gram/server/internal/attr"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestChatClient_EmitGenAITelemetryUsesStableURN(t *testing.T) {
	t.Parallel()

	telemetryLogger := &mockTelemetryLogger{}
	client := &ChatClient{telemetryLogger: telemetryLogger}
	chatID := uuid.NewString()
	cost := 0.0125

	client.emitGenAITelemetry(
		context.Background(),
		nil,
		"org-id",
		uuid.NewString(),
		chatID,
		"user-id",
		"",
		"",
		"api-key-id",
		"assistant",
		CompletionResponse{
			StartTime: time.Now().Add(-time.Second),
			Model:     "openai/gpt-5.4",
			Usage: Usage{
				PromptTokens:     10,
				CompletionTokens: 5,
				TotalTokens:      15,
				Cost:             &cost,
			},
		},
	)

	telemetryLogger.mu.Lock()
	defer telemetryLogger.mu.Unlock()
	require.Len(t, telemetryLogger.logs, 1)
	log := telemetryLogger.logs[0]
	assert.Equal(t, chatID, log.ToolInfo.ID)
	assert.Equal(t, "assistants:chat:completion", log.ToolInfo.URN)
	assert.Equal(t, "assistants:chat:completion", log.Attributes[attr.ResourceURNKey])
	assert.Equal(t, "assistants", log.Attributes[attr.HookSourceKey])
	assert.Equal(t, chatID, log.Attributes[attr.GenAIConversationIDKey])
	assert.InDelta(t, cost, log.Attributes[attr.GenAIUsageCostKey], 1e-9)
}
