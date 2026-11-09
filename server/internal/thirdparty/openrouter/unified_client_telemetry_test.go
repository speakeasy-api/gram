package openrouter

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/speakeasy-api/gram/server/internal/attr"
	"github.com/speakeasy-api/gram/server/internal/captureredact"
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

func TestCaptureRedactionCompletionTelemetryPreservesExecutionInput(t *testing.T) {
	t.Parallel()
	// Synthetic provider credential, never a live key.
	const token = "ghp_" + "b847c2d93a5e4f6071cb9368d014a2e75f69"
	for _, passive := range []bool{true, false} {
		t.Run(map[bool]string{true: "passive capture", false: "live assistant"}[passive], func(t *testing.T) {
			t.Parallel()
			ctx := t.Context()
			if passive {
				ctx = captureredact.WithPassiveCapture(ctx)
			}
			telemetryLogger := &mockTelemetryLogger{}
			client := &ChatClient{telemetryLogger: telemetryLogger}
			arguments := `{"MISE_GITHUB_TOKEN":"` + token + `"}`
			calls := []ToolCall{{Type: "function", Function: ToolCallFunction{Name: "shell", Arguments: arguments}}}
			client.emitGenAITelemetry(ctx, calls, "synthetic-org", uuid.NewString(), uuid.NewString(), "", "", "", "", "codex", CompletionResponse{StartTime: time.Now()})
			require.Len(t, telemetryLogger.logs, 1)
			value, ok := telemetryLogger.logs[0].Attributes[attr.GenAIToolCallsKey].(string)
			require.True(t, ok)
			if passive {
				require.NotContains(t, value, token)
			} else {
				require.Contains(t, value, token)
			}
			require.Equal(t, arguments, calls[0].Function.Arguments)
		})
	}
}

func TestCaptureRedactionCompletionTelemetryUsesResponseContext(t *testing.T) {
	t.Parallel()
	// Synthetic pair from the detector's format fixtures.
	const accessID = "ASIAZ2XY3WNBQR5TUVWX"
	const secret = "wJalrXUtnFEMIbKp7MDoRZfiCYqTvHgNsQ8xLcWd"
	for _, passive := range []bool{true, false} {
		t.Run(map[bool]string{true: "passive capture", false: "live assistant"}[passive], func(t *testing.T) {
			t.Parallel()
			ctx := t.Context()
			if passive {
				ctx = captureredact.WithPassiveCapture(ctx)
			}
			telemetryLogger := &mockTelemetryLogger{}
			client := &ChatClient{telemetryLogger: telemetryLogger}
			arguments := `{"SecretAccessKey":"` + secret + `"}`
			calls := []ToolCall{{Type: "function", Function: ToolCallFunction{Name: "shell", Arguments: arguments}}}
			response := CompletionResponse{StartTime: time.Now(), Content: "AWS_ACCESS_KEY_ID=" + accessID}
			client.emitGenAITelemetry(ctx, calls, "synthetic-org", uuid.NewString(), uuid.NewString(), "", "", "", "", "codex", response)
			require.Len(t, telemetryLogger.logs, 1)
			value, ok := telemetryLogger.logs[0].Attributes[attr.GenAIToolCallsKey].(string)
			require.True(t, ok)
			if passive {
				require.NotContains(t, value, secret)
			} else {
				require.Contains(t, value, secret)
			}
			require.Equal(t, arguments, calls[0].Function.Arguments)
			require.Equal(t, "AWS_ACCESS_KEY_ID="+accessID, response.Content)
		})
	}
}
