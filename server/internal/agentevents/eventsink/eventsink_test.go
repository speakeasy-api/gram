package eventsink_test

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	gen "github.com/speakeasy-api/gram/server/gen/hooks"
	"github.com/speakeasy-api/gram/server/internal/agentevents"
	cursoragent "github.com/speakeasy-api/gram/server/internal/agentevents/cursor"
	"github.com/speakeasy-api/gram/server/internal/agentevents/eventsink"
	"github.com/speakeasy-api/gram/server/internal/agentevents/types"
	"github.com/speakeasy-api/gram/server/internal/attr"
)

const (
	testProjectID = "22222222-2222-2222-2222-222222222222"
	testSource    = "Cursor"
)

func TestBuildChatMessagesPrompt(t *testing.T) {
	t.Parallel()

	source := newCursorSource(t)
	prompt := "Fix the tests"
	payload := &gen.CursorPayload{
		HookEventName: "beforeSubmitPrompt",
		Prompt:        &prompt,
	}
	ev := source.NewEvent(testContext(), payload)

	messages, err := eventsink.BuildChatMessages(ev, testChatID(), testSource)
	require.NoError(t, err)
	require.Len(t, messages, 1)
	assert.Equal(t, "user", messages[0].Params.Role)
	assert.Equal(t, prompt, messages[0].Params.Content)
	assert.Equal(t, testSource, messages[0].Params.Source.String)
	assert.False(t, messages[0].ScheduleTitle)
}

func TestBuildAssistantResponseTelemetryAndChat(t *testing.T) {
	t.Parallel()

	source := newCursorSource(t)
	text := "Done"
	model := "claude-sonnet"
	inputTokens := 120
	outputTokens := 25
	cacheReadTokens := 90
	payload := &gen.CursorPayload{
		HookEventName:   "afterAgentResponse",
		Text:            &text,
		Model:           &model,
		InputTokens:     &inputTokens,
		OutputTokens:    &outputTokens,
		CacheReadTokens: &cacheReadTokens,
	}
	ev := source.NewEvent(testContext(), payload)

	logs, err := eventsink.BuildTelemetryLogs(ev, testSource)
	require.NoError(t, err)
	require.Len(t, logs, 1)
	assert.Equal(t, "Cursor usage metrics", logs[0].Attributes[attr.LogBodyKey])
	assert.Equal(t, "cursor:usage:metrics", logs[0].Attributes[attr.ResourceURNKey])
	assert.Equal(t, inputTokens, logs[0].Attributes[attr.GenAIUsageInputTokensKey])
	assert.Equal(t, outputTokens, logs[0].Attributes[attr.GenAIUsageOutputTokensKey])
	assert.Equal(t, cacheReadTokens, logs[0].Attributes[attr.GenAIUsageCacheReadInputTokensKey])

	messages, err := eventsink.BuildChatMessages(ev, testChatID(), testSource)
	require.NoError(t, err)
	require.Len(t, messages, 1)
	assert.Equal(t, "assistant", messages[0].Params.Role)
	assert.Equal(t, text, messages[0].Params.Content)
	assert.Equal(t, model, messages[0].Params.Model.String)
	assert.True(t, messages[0].ScheduleTitle)
}

func TestBuildStopTelemetry(t *testing.T) {
	t.Parallel()

	source := newCursorSource(t)
	model := "gpt-5.5-extra-high"
	inputTokens := 358187
	outputTokens := 1415
	cacheReadTokens := 345600
	cacheWriteTokens := 0
	payload := &gen.CursorPayload{
		HookEventName:    "stop",
		Model:            &model,
		InputTokens:      &inputTokens,
		OutputTokens:     &outputTokens,
		CacheReadTokens:  &cacheReadTokens,
		CacheWriteTokens: &cacheWriteTokens,
	}
	ev := source.NewEvent(testContext(), payload)

	logs, err := eventsink.BuildTelemetryLogs(ev, testSource)
	require.NoError(t, err)
	require.Len(t, logs, 1)
	attrs := logs[0].Attributes
	assert.Equal(t, string(types.SessionEnded), attrs[attr.HookEventKey])
	assert.Equal(t, model, attrs[attr.GenAIResponseModelKey])
	assert.Equal(t, inputTokens, attrs[attr.GenAIUsageInputTokensKey])
	assert.Equal(t, outputTokens, attrs[attr.GenAIUsageOutputTokensKey])
	assert.Equal(t, cacheReadTokens, attrs[attr.GenAIUsageCacheReadInputTokensKey])
	assert.NotContains(t, attrs, attr.GenAIUsageCacheCreationInputTokensKey)
	assert.NotContains(t, attrs, attr.ToolNameKey)
	assert.NotContains(t, attrs, attr.GenAIToolCallIDKey)
	assert.Equal(t, "cursor", logs[0].ToolInfo.Name)
}

func TestBuildToolEvents(t *testing.T) {
	t.Parallel()

	source := newCursorSource(t)
	toolName := "mcp__linear__list_issues"
	toolUseID := "toolu_cursor"
	payload := &gen.CursorPayload{
		HookEventName: "preToolUse",
		ToolName:      &toolName,
		ToolUseID:     &toolUseID,
		ToolInput:     map[string]any{"assignee": "me"},
	}
	ev := source.NewEvent(testContext(), payload).WithBlockReason("blocked by policy")

	logs, err := eventsink.BuildTelemetryLogs(ev, testSource)
	require.NoError(t, err)
	require.Len(t, logs, 1)
	attrs := logs[0].Attributes
	assert.Equal(t, "list_issues", attrs[attr.ToolNameKey])
	assert.Equal(t, "linear", attrs[attr.ToolCallSourceKey])
	assert.Equal(t, toolUseID, attrs[attr.GenAIToolCallIDKey])
	assert.Equal(t, "blocked by policy", attrs[attr.HookBlockReasonKey])

	messages, err := eventsink.BuildChatMessages(ev, testChatID(), testSource)
	require.NoError(t, err)
	require.Len(t, messages, 1)
	assert.Equal(t, "assistant", messages[0].Params.Role)
	assert.Equal(t, "tool_calls", messages[0].Params.FinishReason.String)

	var toolCalls []map[string]any
	require.NoError(t, json.Unmarshal(messages[0].Params.ToolCalls, &toolCalls))
	require.Len(t, toolCalls, 1)
	assert.Equal(t, toolUseID, toolCalls[0]["id"])
	assert.Equal(t, "function", toolCalls[0]["type"])
	fn, ok := toolCalls[0]["function"].(map[string]any)
	require.True(t, ok)
	assert.Equal(t, "list_issues", fn["name"])
	arguments, ok := fn["arguments"].(string)
	require.True(t, ok)
	assert.JSONEq(t, `{"assignee":"me"}`, arguments)
}

func testChatID() uuid.UUID {
	return uuid.MustParse("33333333-3333-3333-3333-333333333333")
}

func testContext() agentevents.EventContext {
	return agentevents.EventContext{
		OrgID:          "org",
		ProjectID:      testProjectID,
		UserID:         "user",
		UserEmail:      "dev@example.com",
		ConversationID: "conversation",
		Timestamp:      time.Unix(123, 0),
	}
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
