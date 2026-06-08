package cursor_test

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	gen "github.com/speakeasy-api/gram/server/gen/hooks"
	"github.com/speakeasy-api/gram/server/internal/agentevents"
	cursoragent "github.com/speakeasy-api/gram/server/internal/agentevents/cursor"
	"github.com/speakeasy-api/gram/server/internal/agentevents/types"
	"github.com/speakeasy-api/gram/server/internal/message"
)

func TestParseEventType(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		raw      string
		expected types.EventType
		ok       bool
	}{
		{raw: "beforeSubmitPrompt", expected: types.UserPromptSubmit, ok: true},
		{raw: "afterAgentResponse", expected: types.AssistantResponseComplete, ok: true},
		{raw: "preToolUse", expected: types.ToolCallStarted, ok: true},
		{raw: "beforeMCPExecution", expected: types.MCPToolCallStarted, ok: true},
		{raw: "postToolUse", expected: types.ToolCallCompleted, ok: true},
		{raw: "afterMCPExecution", expected: types.MCPToolCallCompleted, ok: true},
		{raw: "postToolUseFailure", expected: types.ToolCallFailed, ok: true},
		{raw: "stop", expected: types.SessionEnded, ok: true},
		{raw: "afterAgentThought", ok: false},
		{raw: "unknown", ok: false},
	}

	for _, tt := range testCases {
		eventType, ok := cursoragent.ParseEventType(tt.raw)
		require.Equal(t, tt.ok, ok, tt.raw)
		assert.Equal(t, tt.expected, eventType)
	}
}

func TestCursorProviderResolvesToolFields(t *testing.T) {
	t.Parallel()

	source := newCursorSource(t)
	toolName := "MCP:list_issues"
	serverURL := "https://mcp.linear.app/sse"
	payload := &gen.CursorPayload{
		HookEventName: "beforeMCPExecution",
		ToolName:      &toolName,
		URL:           &serverURL,
		ToolInput:     map[string]any{"limit": 10},
	}
	ev := source.NewEvent(testContext(), payload)

	eventType, ok, err := ev.EventType()
	require.NoError(t, err)
	require.True(t, ok)
	assert.Equal(t, types.MCPToolCallStarted, eventType)

	resolvedToolName, ok, err := ev.String(types.FieldToolName)
	require.NoError(t, err)
	require.True(t, ok)
	assert.Equal(t, "list_issues", resolvedToolName)

	toolSource, ok, err := ev.String(types.FieldToolSource)
	require.NoError(t, err)
	require.True(t, ok)
	assert.Equal(t, "mcp.linear.app", toolSource)

	scannable, ok, err := ev.String(types.FieldScannableText)
	require.NoError(t, err)
	require.True(t, ok)
	assert.JSONEq(t, `{"limit":10}`, scannable)

	scanType, ok, err := ev.Any(types.FieldScanMessageType)
	require.NoError(t, err)
	require.True(t, ok)
	assert.Equal(t, message.ToolRequest, scanType)
}

func newCursorSource(t *testing.T) *agentevents.Source[*gen.CursorPayload] {
	t.Helper()

	registry := agentevents.NewSourceRegistry()
	source, err := agentevents.RegisterSource[*gen.CursorPayload](registry, cursoragent.Agent)
	require.NoError(t, err)

	resolver := func(field types.Field, resolve agentevents.FieldResolver[*gen.CursorPayload]) agentevents.Resolver[*gen.CursorPayload] {
		return agentevents.Resolver[*gen.CursorPayload]{Field: field, Resolve: resolve}
	}
	require.NoError(t, source.Register(
		resolver(types.FieldEventType, cursoragent.GetEventType),
		resolver(types.FieldHookSource, cursoragent.GetHookSource),
		resolver(types.FieldHookHostname, cursoragent.GetHookHostname),
		resolver(types.FieldBlockReason, cursoragent.GetBlockReason),
		resolver(types.FieldModel, cursoragent.GetModel),
		resolver(types.FieldToolName, cursoragent.GetToolName),
		resolver(types.FieldToolDisplayName, cursoragent.GetToolDisplayName),
		resolver(types.FieldToolSource, cursoragent.GetToolSource),
		resolver(types.FieldToolInput, cursoragent.GetToolInput),
		resolver(types.FieldToolOutput, cursoragent.GetToolOutput),
		resolver(types.FieldToolCallID, cursoragent.GetToolCallID),
		resolver(types.FieldError, cursoragent.GetError),
		resolver(types.FieldUsageInputTokens, cursoragent.GetUsageInputTokens),
		resolver(types.FieldUsageOutputTokens, cursoragent.GetUsageOutputTokens),
		resolver(types.FieldUsageCacheReadTokens, cursoragent.GetUsageCacheReadTokens),
		resolver(types.FieldUsageCacheWriteTokens, cursoragent.GetUsageCacheWriteTokens),
		resolver(types.FieldScannableText, cursoragent.GetScannableText),
		resolver(types.FieldScanMessageType, cursoragent.GetScanMessageType),
		resolver(types.FieldPrompt, cursoragent.GetPrompt),
		resolver(types.FieldAssistantText, cursoragent.GetAssistantText),
	))
	return source
}

func testContext() agentevents.EventContext {
	return agentevents.EventContext{
		OrgID:          "org",
		ProjectID:      "22222222-2222-2222-2222-222222222222",
		UserID:         "user",
		UserEmail:      "dev@example.com",
		ConversationID: "conversation",
		Timestamp:      time.Unix(123, 0),
	}
}
