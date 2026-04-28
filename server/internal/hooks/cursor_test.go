package hooks

import (
	"testing"

	"github.com/google/uuid"
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

func TestBuildCursorTelemetryAttributes_BeforeMCPExecution_ToolSourceFromURL(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestHooksService(t)

	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)

	toolName := "list_issues"
	serverURL := "https://mcp.linear.app/sse"

	attrs := ti.service.buildCursorTelemetryAttributes(ctx, &hooks.CursorPayload{
		HookEventName: "beforeMCPExecution",
		ToolName:      &toolName,
		URL:           &serverURL,
	}, authCtx.ActiveOrganizationID, authCtx.ProjectID.String())

	assert.Equal(t, "BeforeMCPExecution", attrs[attr.HookEventKey])
	assert.Equal(t, "list_issues", attrs[attr.ToolNameKey])
	assert.Equal(t, "mcp.linear.app", attrs[attr.ToolCallSourceKey])
}

func TestBuildCursorTelemetryAttributes_BeforeMCPExecution_StripsMCPPrefix(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestHooksService(t)

	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)

	toolName := "MCP:list_issues"
	cmd := "npx -y @linear/mcp"

	attrs := ti.service.buildCursorTelemetryAttributes(ctx, &hooks.CursorPayload{
		HookEventName: "beforeMCPExecution",
		ToolName:      &toolName,
		Command:       &cmd,
	}, authCtx.ActiveOrganizationID, authCtx.ProjectID.String())

	assert.Equal(t, "list_issues", attrs[attr.ToolNameKey])
	assert.Equal(t, cmd, attrs[attr.ToolCallSourceKey])
}

func TestBuildCursorTelemetryAttributes_MCPBeforeAfter_ShareTraceID(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestHooksService(t)

	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)

	conversationID := "conv-trace-1"
	generationID := "gen-trace-1"
	toolName := "list_issues"
	serverURL := "https://mcp.linear.app/sse"
	toolInput := map[string]any{"assignee": "me", "state": "started", "limit": 100}
	resultJSON := `{"content":[],"isError":false}`

	before := ti.service.buildCursorTelemetryAttributes(ctx, &hooks.CursorPayload{
		HookEventName:  "beforeMCPExecution",
		ConversationID: &conversationID,
		GenerationID:   &generationID,
		ToolName:       &toolName,
		ToolInput:      toolInput,
		URL:            &serverURL,
	}, authCtx.ActiveOrganizationID, authCtx.ProjectID.String())

	after := ti.service.buildCursorTelemetryAttributes(ctx, &hooks.CursorPayload{
		HookEventName:  "afterMCPExecution",
		ConversationID: &conversationID,
		GenerationID:   &generationID,
		ToolName:       &toolName,
		ToolInput:      toolInput,
		URL:            &serverURL,
		ResultJSON:     &resultJSON,
	}, authCtx.ActiveOrganizationID, authCtx.ProjectID.String())

	beforeTrace, ok := before[attr.TraceIDKey].(string)
	require.True(t, ok)
	require.NotEmpty(t, beforeTrace)
	afterTrace, ok := after[attr.TraceIDKey].(string)
	require.True(t, ok)
	assert.Equal(t, beforeTrace, afterTrace, "before/after MCP events should share a trace_id")

	beforeCallID, ok := before[attr.GenAIToolCallIDKey].(string)
	require.True(t, ok)
	afterCallID, ok := after[attr.GenAIToolCallIDKey].(string)
	require.True(t, ok)
	assert.Equal(t, beforeCallID, afterCallID, "before/after MCP events should share a synthetic tool_call_id")
	assert.NotEmpty(t, beforeCallID)
}

func TestBuildCursorTelemetryAttributes_AfterMCPExecution_ResultJSON(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestHooksService(t)

	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)

	toolName := "list_issues"
	serverURL := "https://mcp.linear.app/sse"
	resultJSON := `{"issues":[{"id":"ABC-1"}]}`

	attrs := ti.service.buildCursorTelemetryAttributes(ctx, &hooks.CursorPayload{
		HookEventName: "afterMCPExecution",
		ToolName:      &toolName,
		URL:           &serverURL,
		ResultJSON:    &resultJSON,
	}, authCtx.ActiveOrganizationID, authCtx.ProjectID.String())

	assert.Equal(t, "AfterMCPExecution", attrs[attr.HookEventKey])
	assert.Equal(t, "mcp.linear.app", attrs[attr.ToolCallSourceKey])
	got, ok := attrs[attr.GenAIToolCallResultKey].(string)
	require.True(t, ok, "tool call result should be a string")
	assert.JSONEq(t, resultJSON, got)
}

func TestBuildCursorTelemetryAttributes_AfterMCPExecution_IsErrorSetsHookError(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestHooksService(t)

	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)

	toolName := "list_issues"
	resultJSON := `{"content":[{"type":"text","text":"oops"}],"isError":true}`

	attrs := ti.service.buildCursorTelemetryAttributes(ctx, &hooks.CursorPayload{
		HookEventName: "afterMCPExecution",
		ToolName:      &toolName,
		ResultJSON:    &resultJSON,
	}, authCtx.ActiveOrganizationID, authCtx.ProjectID.String())

	got, ok := attrs[attr.HookErrorKey].(string)
	require.True(t, ok, "gram.hook.error should be set when isError=true")
	assert.JSONEq(t, resultJSON, got)
}

func TestBuildCursorTelemetryAttributes_AfterMCPExecution_IsErrorFalseNoHookError(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestHooksService(t)

	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)

	toolName := "list_issues"
	resultJSON := `{"content":[],"isError":false}`

	attrs := ti.service.buildCursorTelemetryAttributes(ctx, &hooks.CursorPayload{
		HookEventName: "afterMCPExecution",
		ToolName:      &toolName,
		ResultJSON:    &resultJSON,
	}, authCtx.ActiveOrganizationID, authCtx.ProjectID.String())

	_, ok = attrs[attr.HookErrorKey]
	assert.False(t, ok, "gram.hook.error should not be set when isError=false")
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

func TestBuildCursorTelemetryAttributes_BeforeSubmitPromptNormalization(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestHooksService(t)

	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)

	prompt := "Fix the bug"
	attrs := ti.service.buildCursorTelemetryAttributes(ctx, &hooks.CursorPayload{
		HookEventName: "beforeSubmitPrompt",
		Prompt:        &prompt,
	}, authCtx.ActiveOrganizationID, authCtx.ProjectID.String())

	require.Equal(t, "BeforeSubmitPrompt", attrs[attr.HookEventKey])
	// Prompt should be stored as LogBody for beforeSubmitPrompt
	require.Equal(t, "Fix the bug", attrs[attr.LogBodyKey])
	// ToolNameKey should be empty for conversation events
	require.Empty(t, attrs[attr.ToolNameKey])
}

func TestBuildCursorTelemetryAttributes_StopNormalization(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestHooksService(t)

	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)

	status := "completed"
	inputTokens := 38950
	outputTokens := 500
	attrs := ti.service.buildCursorTelemetryAttributes(ctx, &hooks.CursorPayload{
		HookEventName: "stop",
		Status:        &status,
		InputTokens:   &inputTokens,
		OutputTokens:  &outputTokens,
	}, authCtx.ActiveOrganizationID, authCtx.ProjectID.String())

	require.Equal(t, "Stop", attrs[attr.HookEventKey])
	// Token usage should be captured from stop events
	require.Equal(t, 38950, attrs[attr.GenAIUsageInputTokensKey])
	require.Equal(t, 500, attrs[attr.GenAIUsageOutputTokensKey])
	// ToolNameKey should be empty for non-tool events
	require.Empty(t, attrs[attr.ToolNameKey])
	// LogBody should NOT contain prompt text (stop has no prompt)
	require.Equal(t, "Hook: Stop", attrs[attr.LogBodyKey])
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

func TestBuildCursorTelemetryAttributes_SkillsMetadata(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestHooksService(t)

	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)

	skillID := uuid.NewString()
	skillVersionID := uuid.NewString()

	attrs := ti.service.buildCursorTelemetryAttributes(ctx, &hooks.CursorPayload{
		HookEventName: "preToolUse",
		AdditionalData: map[string]any{
			"skills": []any{
				map[string]any{
					"name":              "golang",
					"scope":             "project",
					"discovery_root":    "project_agents",
					"source_type":       "local_filesystem",
					"skill_id":          skillID,
					"skill_version_id":  skillVersionID,
					"resolution_status": "resolved",
				},
			},
		},
	}, authCtx.ActiveOrganizationID, authCtx.ProjectID.String())

	require.Equal(t, "golang", attrs[attr.SkillNameKey])
	require.Equal(t, "project", attrs[attr.SkillScopeKey])
	require.Equal(t, "project_agents", attrs[attr.SkillDiscoveryRootKey])
	require.Equal(t, "local_filesystem", attrs[attr.SkillSourceTypeKey])
	require.Equal(t, skillID, attrs[attr.SkillIDKey])
	require.Equal(t, skillVersionID, attrs[attr.SkillVersionIDKey])
	require.Equal(t, "resolved", attrs[attr.SkillResolutionStatusKey])
}

func TestBuildCursorTelemetryAttributes_InvalidSkillsMetadataIgnored(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestHooksService(t)

	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)

	attrs := ti.service.buildCursorTelemetryAttributes(ctx, &hooks.CursorPayload{
		HookEventName: "preToolUse",
		AdditionalData: map[string]any{
			"skills": "invalid",
		},
	}, authCtx.ActiveOrganizationID, authCtx.ProjectID.String())

	_, hasSkillName := attrs[attr.SkillNameKey]
	_, hasSkillScope := attrs[attr.SkillScopeKey]
	_, hasSkillResolutionStatus := attrs[attr.SkillResolutionStatusKey]
	require.False(t, hasSkillName)
	require.False(t, hasSkillScope)
	require.False(t, hasSkillResolutionStatus)
}

func TestBuildCursorTelemetryAttributes_NullSkillsIgnored(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestHooksService(t)

	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)

	attrs := ti.service.buildCursorTelemetryAttributes(ctx, &hooks.CursorPayload{
		HookEventName: "preToolUse",
		AdditionalData: map[string]any{
			"skills": nil,
		},
	}, authCtx.ActiveOrganizationID, authCtx.ProjectID.String())

	_, hasSkillName := attrs[attr.SkillNameKey]
	_, hasSkillScope := attrs[attr.SkillScopeKey]
	require.False(t, hasSkillName)
	require.False(t, hasSkillScope)
}

func TestBuildCursorTelemetryAttributes_EmptySkillsSliceIgnored(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestHooksService(t)

	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)

	attrs := ti.service.buildCursorTelemetryAttributes(ctx, &hooks.CursorPayload{
		HookEventName: "preToolUse",
		AdditionalData: map[string]any{
			"skills": []any{},
		},
	}, authCtx.ActiveOrganizationID, authCtx.ProjectID.String())

	_, hasSkillName := attrs[attr.SkillNameKey]
	require.False(t, hasSkillName)
}

func TestBuildCursorTelemetryAttributes_SkillsMixedInvalidAndValid(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestHooksService(t)

	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)

	attrs := ti.service.buildCursorTelemetryAttributes(ctx, &hooks.CursorPayload{
		HookEventName: "preToolUse",
		AdditionalData: map[string]any{
			"skills": []any{
				"notamap",
				map[string]any{
					"name": "golang",
				},
			},
		},
	}, authCtx.ActiveOrganizationID, authCtx.ProjectID.String())

	require.Equal(t, "golang", attrs[attr.SkillNameKey])
}

func TestBuildCursorTelemetryAttributes_SkillsPartialFields(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestHooksService(t)

	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)

	attrs := ti.service.buildCursorTelemetryAttributes(ctx, &hooks.CursorPayload{
		HookEventName: "preToolUse",
		AdditionalData: map[string]any{
			"skills": []any{
				map[string]any{
					"name": "golang",
				},
			},
		},
	}, authCtx.ActiveOrganizationID, authCtx.ProjectID.String())

	require.Equal(t, "golang", attrs[attr.SkillNameKey])
	_, hasScope := attrs[attr.SkillScopeKey]
	_, hasDiscoveryRoot := attrs[attr.SkillDiscoveryRootKey]
	_, hasSourceType := attrs[attr.SkillSourceTypeKey]
	_, hasSkillID := attrs[attr.SkillIDKey]
	_, hasSkillVersionID := attrs[attr.SkillVersionIDKey]
	_, hasResolutionStatus := attrs[attr.SkillResolutionStatusKey]
	require.False(t, hasScope)
	require.False(t, hasDiscoveryRoot)
	require.False(t, hasSourceType)
	require.False(t, hasSkillID)
	require.False(t, hasSkillVersionID)
	require.False(t, hasResolutionStatus)
}
