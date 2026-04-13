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
