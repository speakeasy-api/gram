package hooks

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/gen/hooks"
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

	// Use a bare context without auth. The handler returns a shaped JSON
	// deny (permission=deny + user_message) instead of an error so the
	// Cursor CLI surfaces the reason to the user.
	ctx := t.Context()
	result, err := ti.service.Cursor(ctx, &hooks.CursorPayload{
		HookEventName: "preToolUse",
	})
	require.NoError(t, err)
	require.NotNil(t, result)
	require.NotNil(t, result.Permission)
	assert.Equal(t, "deny", *result.Permission)
	require.NotNil(t, result.UserMessage)
	assert.Contains(t, *result.UserMessage, "unauthorized")
}

func TestCursor_BeforeMCPExecution_ReturnsAllow(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestHooksService(t)

	toolName := "list_issues"
	toolUseID := "toolu_mcp_123"
	conversationID := "conv-mcp-abc"
	serverURL := "https://mcp.linear.app/sse"

	result, err := ti.service.Cursor(ctx, &hooks.CursorPayload{
		HookEventName:  "beforeMCPExecution",
		ToolName:       &toolName,
		ToolUseID:      &toolUseID,
		ConversationID: &conversationID,
		URL:            &serverURL,
	})
	require.NoError(t, err)
	require.NotNil(t, result)
	require.NotNil(t, result.Permission)
	assert.Equal(t, "allow", *result.Permission)
}

func TestCursor_BeforeMCPExecution_ShadowMCPBlockIncludesRequestLink(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestHooksService(t)
	ti.service.riskScanner = stubBlockingShadowMCPScanner{}

	toolName := "list_issues"
	toolUseID := "toolu_mcp_blocked"
	conversationID := "conv-mcp-blocked"
	serverURL := "https://mcp.linear.app/sse"

	result, err := ti.service.Cursor(ctx, &hooks.CursorPayload{
		HookEventName:  "beforeMCPExecution",
		ToolName:       &toolName,
		ToolUseID:      &toolUseID,
		ConversationID: &conversationID,
		URL:            &serverURL,
		ToolInput:      map[string]any{"foo": "bar"},
	})
	require.NoError(t, err)
	require.NotNil(t, result)
	require.NotNil(t, result.Permission)
	assert.Equal(t, "deny", *result.Permission)
	require.NotNil(t, result.UserMessage)
	assert.Contains(t, *result.UserMessage, "Request access:\nhttps://app.example.test/shadow-mcp/request#request_token=smar1.",
		"shadow-MCP deny messages should include a signed approval request link")
	assert.Contains(t, *result.UserMessage, shadowMCPApprovalRequestPrompt)
	require.NotNil(t, result.AgentMessage)
	assert.Equal(t, *result.UserMessage, *result.AgentMessage)
}

func TestCursor_BeforeSubmitPrompt_ReturnsEmpty(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestHooksService(t)

	prompt := "How do I fix this bug?"
	conversationID := "conv-prompt-123"
	userEmail := "dev@example.com"

	result, err := ti.service.Cursor(ctx, &hooks.CursorPayload{
		HookEventName:  "beforeSubmitPrompt",
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

	conversationID := "conv-stop-123"
	userEmail := "dev@example.com"
	status := "completed"
	inputTokens := 38950
	outputTokens := 500

	result, err := ti.service.Cursor(ctx, &hooks.CursorPayload{
		HookEventName:  "stop",
		ConversationID: &conversationID,
		UserEmail:      &userEmail,
		Status:         &status,
		InputTokens:    &inputTokens,
		OutputTokens:   &outputTokens,
	})
	require.NoError(t, err)
	require.NotNil(t, result)
	require.Nil(t, result.Permission)
}

func TestCursor_AfterAgentResponse_WithTokens_DoesNotError(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestHooksService(t)

	conversationID := "conv-aar-tokens"
	userEmail := "dev@example.com"
	text := "here is my response"
	model := "claude-sonnet-4-5"
	inputTokens := 182306
	outputTokens := 981
	cacheReadTokens := 143523
	cacheWriteTokens := 38773

	result, err := ti.service.Cursor(ctx, &hooks.CursorPayload{
		HookEventName:    "afterAgentResponse",
		ConversationID:   &conversationID,
		UserEmail:        &userEmail,
		Text:             &text,
		Model:            &model,
		InputTokens:      &inputTokens,
		OutputTokens:     &outputTokens,
		CacheReadTokens:  &cacheReadTokens,
		CacheWriteTokens: &cacheWriteTokens,
	})
	require.NoError(t, err)
	require.NotNil(t, result)
}

func TestCursor_PersistEventRouting_AllEventTypes(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestHooksService(t)

	conversationID := "conv-routing-test"
	userEmail := "dev@example.com"
	toolName := "Edit"
	toolUseID := "toolu_routing_test"

	// All event types should route through persistCursorEventToPG without error
	events := []struct {
		name    string
		payload *hooks.CursorPayload
	}{
		{
			name: "beforeSubmitPrompt",
			payload: &hooks.CursorPayload{
				HookEventName:  "beforeSubmitPrompt",
				ConversationID: &conversationID,
				UserEmail:      &userEmail,
				Prompt:         new("test prompt"),
			},
		},
		{
			name: "preToolUse",
			payload: &hooks.CursorPayload{
				HookEventName:  "preToolUse",
				ConversationID: &conversationID,
				UserEmail:      &userEmail,
				ToolName:       &toolName,
				ToolUseID:      &toolUseID,
			},
		},
		{
			name: "postToolUse",
			payload: &hooks.CursorPayload{
				HookEventName:  "postToolUse",
				ConversationID: &conversationID,
				UserEmail:      &userEmail,
				ToolName:       &toolName,
				ToolUseID:      &toolUseID,
				ToolResponse:   "success",
			},
		},
		{
			name: "postToolUseFailure",
			payload: &hooks.CursorPayload{
				HookEventName:  "postToolUseFailure",
				ConversationID: &conversationID,
				UserEmail:      &userEmail,
				ToolName:       &toolName,
				ToolUseID:      &toolUseID,
				Error:          "something failed",
			},
		},
		{
			name: "stop",
			payload: &hooks.CursorPayload{
				HookEventName:  "stop",
				ConversationID: &conversationID,
				UserEmail:      &userEmail,
			},
		},
	}

	for _, e := range events {
		result, err := ti.service.Cursor(ctx, e.payload)
		require.NoError(t, err, "event %s should not error", e.name)
		require.NotNil(t, result, "event %s should return a result", e.name)
	}
}
