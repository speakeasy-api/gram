package hooks

import (
	"fmt"
	"testing"

	"github.com/stretchr/testify/require"

	gen "github.com/speakeasy-api/gram/server/gen/hooks"
	"github.com/speakeasy-api/gram/server/internal/agentevents"
	cursoragent "github.com/speakeasy-api/gram/server/internal/agentevents/cursor"
	"github.com/speakeasy-api/gram/server/internal/agentevents/types"
)

func newTestCursorAgentEventSource(t *testing.T) *agentevents.Mux {
	t.Helper()

	source, err := newTestCursorAgentEventSourceWithError()
	require.NoError(t, err)
	return source
}

func newTestCursorAgentEventSourceWithError() (*agentevents.Mux, error) {
	mux := agentevents.NewMux()
	agent, err := newTestCursorAgentWithError()
	if err != nil {
		return nil, err
	}
	if err := mux.Register(agent, nil); err != nil {
		return nil, err
	}
	return mux, nil
}

func newTestCursorAgentWithError() (*agentevents.Agent[*gen.CursorPayload], error) {
	usageEventTypes := []types.EventType{
		types.AssistantResponseComplete,
		types.SessionEnded,
	}
	scanEventTypes := []types.EventType{
		types.UserPromptSubmit,
		types.ToolCallStarted,
		types.MCPToolCallStarted,
	}
	toolEventTypes := []types.EventType{
		types.ToolCallStarted,
		types.ToolCallCompleted,
		types.ToolCallFailed,
		types.MCPToolCallStarted,
		types.MCPToolCallCompleted,
	}
	toolResultEventTypes := []types.EventType{
		types.ToolCallCompleted,
		types.ToolCallFailed,
		types.MCPToolCallCompleted,
	}

	agent, err := agentevents.NewAgent[*gen.CursorPayload](cursoragent.Agent)
	if err != nil {
		return nil, err
	}
	agent, err = agent.Builder().
		Register(
			agentevents.Resolve[*gen.CursorPayload, types.EventType](types.FieldEventType, cursoragent.GetEventType),
			agentevents.Resolve[*gen.CursorPayload, string](types.FieldHookSource, cursoragent.GetHookSource),
			agentevents.Resolve[*gen.CursorPayload, string](types.FieldHookHostname, cursoragent.GetHookHostname),
			agentevents.Resolve[*gen.CursorPayload, string](types.FieldBlockReason, cursoragent.GetBlockReason),
			agentevents.Resolve[*gen.CursorPayload, string](types.FieldModel, cursoragent.GetModel),
		).
		RegisterFor(usageEventTypes,
			agentevents.Resolve[*gen.CursorPayload, int](types.FieldUsageInputTokens, cursoragent.GetUsageInputTokens),
			agentevents.Resolve[*gen.CursorPayload, int](types.FieldUsageOutputTokens, cursoragent.GetUsageOutputTokens),
			agentevents.Resolve[*gen.CursorPayload, int](types.FieldUsageCacheReadTokens, cursoragent.GetUsageCacheReadTokens),
			agentevents.Resolve[*gen.CursorPayload, int](types.FieldUsageCacheWriteTokens, cursoragent.GetUsageCacheWriteTokens),
		).
		RegisterFor(scanEventTypes,
			agentevents.Resolve[*gen.CursorPayload, string](types.FieldScannableText, cursoragent.GetScannableText),
			agentevents.Resolve[*gen.CursorPayload, any](types.FieldScanMessageType, cursoragent.GetScanMessageType),
		).
		RegisterFor(toolEventTypes,
			agentevents.Resolve[*gen.CursorPayload, string](types.FieldToolName, cursoragent.GetToolName),
			agentevents.Resolve[*gen.CursorPayload, string](types.FieldToolDisplayName, cursoragent.GetToolDisplayName),
			agentevents.Resolve[*gen.CursorPayload, string](types.FieldToolSource, cursoragent.GetToolSource),
			agentevents.Resolve[*gen.CursorPayload, any](types.FieldToolInput, cursoragent.GetToolInput),
			agentevents.Resolve[*gen.CursorPayload, string](types.FieldToolCallID, cursoragent.GetToolCallID),
		).
		RegisterFor(toolResultEventTypes,
			agentevents.Resolve[*gen.CursorPayload, any](types.FieldToolOutput, cursoragent.GetToolOutput),
			agentevents.Resolve[*gen.CursorPayload, any](types.FieldError, cursoragent.GetError),
		).
		RegisterFor([]types.EventType{types.UserPromptSubmit},
			agentevents.Resolve[*gen.CursorPayload, string](types.FieldPrompt, cursoragent.GetPrompt),
		).
		RegisterFor([]types.EventType{types.AssistantResponseComplete},
			agentevents.Resolve[*gen.CursorPayload, string](types.FieldAssistantText, cursoragent.GetAssistantText),
		).
		Build()
	if err != nil {
		return nil, fmt.Errorf("register cursor agent event resolvers: %w", err)
	}
	return agent, nil
}
