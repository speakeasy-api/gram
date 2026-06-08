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

func newTestCursorAgentEventSource(t *testing.T) *agentevents.Source[*gen.CursorPayload] {
	t.Helper()

	source, err := newTestCursorAgentEventSourceWithError()
	require.NoError(t, err)
	return source
}

func newTestCursorAgentEventSourceWithError() (*agentevents.Source[*gen.CursorPayload], error) {
	sourceRegistry := agentevents.NewSourceRegistry()
	source, err := agentevents.RegisterSource[*gen.CursorPayload](sourceRegistry, cursoragent.Agent)
	if err != nil {
		return nil, err
	}

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
	resolver := func(field types.Field, resolve agentevents.FieldResolver[*gen.CursorPayload]) agentevents.Resolver[*gen.CursorPayload] {
		return agentevents.Resolver[*gen.CursorPayload]{Field: field, Resolve: resolve}
	}
	commonResolvers := []agentevents.Resolver[*gen.CursorPayload]{
		resolver(types.FieldEventType, cursoragent.GetEventType),
		resolver(types.FieldHookSource, cursoragent.GetHookSource),
		resolver(types.FieldHookHostname, cursoragent.GetHookHostname),
		resolver(types.FieldBlockReason, cursoragent.GetBlockReason),
		resolver(types.FieldModel, cursoragent.GetModel),
	}

	if err := source.Register(commonResolvers...); err != nil {
		return nil, fmt.Errorf("register cursor agent event resolvers: %w", err)
	}
	if err := source.RegisterFor(usageEventTypes,
		resolver(types.FieldUsageInputTokens, cursoragent.GetUsageInputTokens),
		resolver(types.FieldUsageOutputTokens, cursoragent.GetUsageOutputTokens),
		resolver(types.FieldUsageCacheReadTokens, cursoragent.GetUsageCacheReadTokens),
		resolver(types.FieldUsageCacheWriteTokens, cursoragent.GetUsageCacheWriteTokens),
	); err != nil {
		return nil, fmt.Errorf("register cursor agent event resolvers: %w", err)
	}
	if err := source.RegisterFor(scanEventTypes,
		resolver(types.FieldScannableText, cursoragent.GetScannableText),
		resolver(types.FieldScanMessageType, cursoragent.GetScanMessageType),
	); err != nil {
		return nil, fmt.Errorf("register cursor agent event resolvers: %w", err)
	}
	if err := source.RegisterFor(toolEventTypes,
		resolver(types.FieldToolName, cursoragent.GetToolName),
		resolver(types.FieldToolDisplayName, cursoragent.GetToolDisplayName),
		resolver(types.FieldToolSource, cursoragent.GetToolSource),
		resolver(types.FieldToolInput, cursoragent.GetToolInput),
		resolver(types.FieldToolCallID, cursoragent.GetToolCallID),
	); err != nil {
		return nil, fmt.Errorf("register cursor agent event resolvers: %w", err)
	}
	if err := source.RegisterFor(toolResultEventTypes,
		resolver(types.FieldToolOutput, cursoragent.GetToolOutput),
		resolver(types.FieldError, cursoragent.GetError),
	); err != nil {
		return nil, fmt.Errorf("register cursor agent event resolvers: %w", err)
	}
	if err := source.RegisterFor([]types.EventType{types.UserPromptSubmit},
		resolver(types.FieldPrompt, cursoragent.GetPrompt),
	); err != nil {
		return nil, fmt.Errorf("register cursor agent event resolvers: %w", err)
	}
	if err := source.RegisterFor([]types.EventType{types.AssistantResponseComplete},
		resolver(types.FieldAssistantText, cursoragent.GetAssistantText),
	); err != nil {
		return nil, fmt.Errorf("register cursor agent event resolvers: %w", err)
	}
	return source, nil
}
