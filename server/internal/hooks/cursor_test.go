package hooks

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/gen/hooks"
	"github.com/speakeasy-api/gram/server/internal/attr"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/telemetry"
)

func TestCursor_PreToolUse_ReturnsAllow(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestHooksService(t)

	toolName := "Edit"
	toolUseID := "toolu_cursor_123"
	userEmail := "dev@example.com"
	conversationID := "conv-abc"

	result, err := ti.service.Cursor(ctx, &hooks.CursorPayload{
		HookEventName:  "preToolUse",
		ToolName:       &toolName,
		ToolUseID:      &toolUseID,
		UserEmail:      &userEmail,
		ConversationID: &conversationID,
	})
	require.NoError(t, err)
	require.NotNil(t, result)
	require.NotNil(t, result.Permission)
	assert.Equal(t, "allow", *result.Permission)
}

func TestCursor_PostToolUse_ReturnsEmpty(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestHooksService(t)

	toolName := "Read"
	toolUseID := "toolu_cursor_456"

	result, err := ti.service.Cursor(ctx, &hooks.CursorPayload{
		HookEventName: "postToolUse",
		ToolName:      &toolName,
		ToolUseID:     &toolUseID,
	})
	require.NoError(t, err)
	require.NotNil(t, result)
	assert.Nil(t, result.Permission)
}

func TestCursor_PostToolUseFailure_ReturnsEmpty(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestHooksService(t)

	toolName := "Bash"
	toolUseID := "toolu_cursor_789"
	isInterrupt := true

	result, err := ti.service.Cursor(ctx, &hooks.CursorPayload{
		HookEventName: "postToolUseFailure",
		ToolName:      &toolName,
		ToolUseID:     &toolUseID,
		IsInterrupt:   &isInterrupt,
		Error:         "command timed out",
	})
	require.NoError(t, err)
	require.NotNil(t, result)
	assert.Nil(t, result.Permission)
}

func TestCursor_UnknownEvent_ReturnsEmpty(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestHooksService(t)

	result, err := ti.service.Cursor(ctx, &hooks.CursorPayload{
		HookEventName: "someNewEvent",
	})
	require.NoError(t, err)
	require.NotNil(t, result)
	assert.Nil(t, result.Permission)
}

func TestCursor_RequiresAuth(t *testing.T) {
	t.Parallel()
	_, ti := newTestHooksService(t)

	// Use a bare context without auth
	ctx := t.Context()
	_, err := ti.service.Cursor(ctx, &hooks.CursorPayload{
		HookEventName: "preToolUse",
	})
	require.Error(t, err)
}

func TestBuildCursorTelemetryAttributes_BasicFields(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestHooksService(t)

	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)

	toolName := "Edit"
	toolUseID := "toolu_cursor_attr"
	userEmail := "dev@example.com"
	conversationID := "conv-123"

	attrs := ti.service.buildCursorTelemetryAttributes(ctx, &hooks.CursorPayload{
		HookEventName:  "preToolUse",
		ToolName:       &toolName,
		ToolUseID:      &toolUseID,
		UserEmail:      &userEmail,
		ConversationID: &conversationID,
	}, authCtx.ActiveOrganizationID, authCtx.ProjectID.String())

	assert.Equal(t, string(telemetry.EventSourceHook), attrs[attr.EventSourceKey])
	assert.Equal(t, "Edit", attrs[attr.ToolNameKey])
	assert.Equal(t, "PreToolUse", attrs[attr.HookEventKey])
	assert.Equal(t, "cursor", attrs[attr.HookSourceKey])
	assert.Equal(t, "dev@example.com", attrs[attr.UserEmailKey])
	assert.Equal(t, "conv-123", attrs[attr.GenAIConversationIDKey])
	assert.Equal(t, "toolu_cursor_attr", attrs[attr.GenAIToolCallIDKey])
	// Trace ID should be hashed from toolUseID
	assert.Equal(t, hashToolCallIDToTraceID("toolu_cursor_attr"), attrs[attr.TraceIDKey])
}

func TestBuildCursorTelemetryAttributes_MCPToolParsing(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestHooksService(t)

	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)

	toolName := "mcp__linear__list_issues"

	attrs := ti.service.buildCursorTelemetryAttributes(ctx, &hooks.CursorPayload{
		HookEventName: "postToolUse",
		ToolName:      &toolName,
	}, authCtx.ActiveOrganizationID, authCtx.ProjectID.String())

	assert.Equal(t, "list_issues", attrs[attr.ToolNameKey])
	assert.Equal(t, "linear", attrs[attr.ToolCallSourceKey])
}

func TestBuildCursorTelemetryAttributes_NilUserEmail(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestHooksService(t)

	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)

	attrs := ti.service.buildCursorTelemetryAttributes(ctx, &hooks.CursorPayload{
		HookEventName: "preToolUse",
		UserEmail:     nil,
	}, authCtx.ActiveOrganizationID, authCtx.ProjectID.String())

	assert.Empty(t, attrs[attr.UserEmailKey])
}

func TestCursor_UserPromptSubmit_ReturnsEmpty(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestHooksService(t)

	prompt := "How do I fix this bug?"
	conversationID := "conv-prompt-123"
	userEmail := "dev@example.com"

	result, err := ti.service.Cursor(ctx, &hooks.CursorPayload{
		HookEventName:  "userPromptSubmit",
		Prompt:         &prompt,
		ConversationID: &conversationID,
		UserEmail:      &userEmail,
	})
	require.NoError(t, err)
	require.NotNil(t, result)
	require.Nil(t, result.Permission)
}

func TestCursor_Stop_ReturnsEmpty(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestHooksService(t)

	lastMessage := "Here's the fix for your bug..."
	conversationID := "conv-stop-123"
	userEmail := "dev@example.com"
	model := "claude-sonnet-4-6"

	result, err := ti.service.Cursor(ctx, &hooks.CursorPayload{
		HookEventName:        "stop",
		LastAssistantMessage: &lastMessage,
		ConversationID:       &conversationID,
		UserEmail:            &userEmail,
		Model:                &model,
	})
	require.NoError(t, err)
	require.NotNil(t, result)
	require.Nil(t, result.Permission)
}

func TestBuildCursorTelemetryAttributes_UserPromptSubmitNormalization(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestHooksService(t)

	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)

	prompt := "Fix the bug"
	attrs := ti.service.buildCursorTelemetryAttributes(ctx, &hooks.CursorPayload{
		HookEventName: "userPromptSubmit",
		Prompt:        &prompt,
	}, authCtx.ActiveOrganizationID, authCtx.ProjectID.String())

	require.Equal(t, "UserPromptSubmit", attrs[attr.HookEventKey])
}

func TestBuildCursorTelemetryAttributes_StopNormalization(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestHooksService(t)

	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)

	lastMessage := "Here is the result"
	attrs := ti.service.buildCursorTelemetryAttributes(ctx, &hooks.CursorPayload{
		HookEventName:        "stop",
		LastAssistantMessage: &lastMessage,
	}, authCtx.ActiveOrganizationID, authCtx.ProjectID.String())

	require.Equal(t, "Stop", attrs[attr.HookEventKey])
}

func TestIsCursorConversationEvent(t *testing.T) {
	t.Parallel()
	require.True(t, isCursorConversationEvent("userPromptSubmit"))
	require.True(t, isCursorConversationEvent("stop"))
	require.False(t, isCursorConversationEvent("preToolUse"))
	require.False(t, isCursorConversationEvent("postToolUse"))
	require.False(t, isCursorConversationEvent("postToolUseFailure"))
	require.False(t, isCursorConversationEvent("someNewEvent"))
}

func TestBuildCursorTelemetryAttributes_ToolInputStringified(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestHooksService(t)

	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)

	toolName := "Write"
	input := map[string]any{"file_path": "/tmp/test.go", "content": "package main"}

	attrs := ti.service.buildCursorTelemetryAttributes(ctx, &hooks.CursorPayload{
		HookEventName: "preToolUse",
		ToolName:      &toolName,
		ToolInput:     input,
	}, authCtx.ActiveOrganizationID, authCtx.ProjectID.String())

	// Should be stringified JSON, not a nested object
	val, ok := attrs[attr.GenAIToolCallArgumentsKey].(string)
	require.True(t, ok, "ToolInput should be stored as a string")
	assert.Contains(t, val, "file_path")
	assert.Contains(t, val, "/tmp/test.go")
}
