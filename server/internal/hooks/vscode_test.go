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

func TestVSCodeCopilot_PreToolUse_ReturnsEmpty(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestHooksService(t)

	toolName := "runTerminalCommand"
	toolUseID := "tool_vscode_123"
	sessionID := "vsc-session-abc"

	result, err := ti.service.VscodeCopilot(ctx, &hooks.VscodeCopilotPayload{
		HookEventName: "PreToolUse",
		ToolName:      &toolName,
		ToolUseID:     &toolUseID,
		SessionID:     &sessionID,
		ToolInput:     map[string]any{"command": "ls"},
	})
	require.NoError(t, err)
	require.NotNil(t, result)
	// No risk policy configured in test env, so no deny decision is rendered.
	assert.Nil(t, result.HookSpecificOutput)
}

func TestVSCodeCopilot_PostToolUse_ReturnsEmpty(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestHooksService(t)

	toolName := "editFiles"
	toolUseID := "tool_vscode_456"

	result, err := ti.service.VscodeCopilot(ctx, &hooks.VscodeCopilotPayload{
		HookEventName: "PostToolUse",
		ToolName:      &toolName,
		ToolUseID:     &toolUseID,
		ToolResponse:  map[string]any{"ok": true},
	})
	require.NoError(t, err)
	require.NotNil(t, result)
	assert.Nil(t, result.HookSpecificOutput)
}

func TestVSCodeCopilot_UnknownEvent_DoesNotError(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestHooksService(t)

	result, err := ti.service.VscodeCopilot(ctx, &hooks.VscodeCopilotPayload{
		HookEventName: "SubagentStart",
	})
	require.NoError(t, err)
	require.NotNil(t, result)
	assert.Nil(t, result.HookSpecificOutput)
}

func TestVSCodeCopilot_RequiresAuth(t *testing.T) {
	t.Parallel()
	_, ti := newTestHooksService(t)

	ctx := t.Context()
	_, err := ti.service.VscodeCopilot(ctx, &hooks.VscodeCopilotPayload{
		HookEventName: "PreToolUse",
	})
	require.Error(t, err)
}

func TestBuildVSCodeTelemetryAttributes_BasicFields(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestHooksService(t)

	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)

	toolName := "runTerminalCommand"
	toolUseID := "tool_vscode_attr"
	userEmail := "alice@example.com"
	userEmailSource := "env"
	sessionID := "vsc-session-1"

	attrs := ti.service.buildVSCodeTelemetryAttributes(ctx, &hooks.VscodeCopilotPayload{
		HookEventName:        "PreToolUse",
		ToolName:             &toolName,
		ToolUseID:            &toolUseID,
		UserEmailInput:       &userEmail,
		UserEmailSourceInput: &userEmailSource,
		SessionID:            &sessionID,
	}, authCtx.ActiveOrganizationID, authCtx.ProjectID.String())

	assert.Equal(t, string(telemetry.EventSourceHook), attrs[attr.EventSourceKey])
	assert.Equal(t, "runTerminalCommand", attrs[attr.ToolNameKey])
	assert.Equal(t, "PreToolUse", attrs[attr.HookEventKey])
	assert.Equal(t, "copilot", attrs[attr.HookSourceKey])
	assert.Equal(t, "alice@example.com", attrs[attr.UserEmailKey])
	assert.Equal(t, "env", attrs[attr.UserEmailSourceKey])
	assert.Equal(t, "vsc-session-1", attrs[attr.GenAIConversationIDKey])
	assert.Equal(t, "tool_vscode_attr", attrs[attr.GenAIToolCallIDKey])
	assert.Equal(t, hashToolCallIDToTraceID("tool_vscode_attr"), attrs[attr.TraceIDKey])
}

func TestBuildVSCodeTelemetryAttributes_MCPToolParsing(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestHooksService(t)

	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)

	toolName := "mcp__linear__list_issues"

	attrs := ti.service.buildVSCodeTelemetryAttributes(ctx, &hooks.VscodeCopilotPayload{
		HookEventName: "PostToolUse",
		ToolName:      &toolName,
	}, authCtx.ActiveOrganizationID, authCtx.ProjectID.String())

	assert.Equal(t, "list_issues", attrs[attr.ToolNameKey])
	assert.Equal(t, "linear", attrs[attr.ToolCallSourceKey])
}

func TestBuildVSCodeTelemetryAttributes_UserPromptSubmitSetsLogBody(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestHooksService(t)

	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)

	prompt := "Refactor this function"

	attrs := ti.service.buildVSCodeTelemetryAttributes(ctx, &hooks.VscodeCopilotPayload{
		HookEventName: "UserPromptSubmit",
		Prompt:        &prompt,
	}, authCtx.ActiveOrganizationID, authCtx.ProjectID.String())

	assert.Equal(t, "Refactor this function", attrs[attr.LogBodyKey])
}

func TestBuildVSCodeTelemetryAttributes_SubagentFields(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestHooksService(t)

	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)

	agentID := "agent-42"
	agentType := "test-runner"

	attrs := ti.service.buildVSCodeTelemetryAttributes(ctx, &hooks.VscodeCopilotPayload{
		HookEventName: "SubagentStart",
		AgentID:       &agentID,
		AgentType:     &agentType,
	}, authCtx.ActiveOrganizationID, authCtx.ProjectID.String())

	assert.Equal(t, "agent-42", attrs[attr.HookAgentIDKey])
	assert.Equal(t, "test-runner", attrs[attr.HookAgentTypeKey])
}

func TestBuildVSCodeTelemetryAttributes_ToolInputStringified(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestHooksService(t)

	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)

	toolName := "createFile"
	input := map[string]any{"path": "/tmp/x.go", "content": "package main"}

	attrs := ti.service.buildVSCodeTelemetryAttributes(ctx, &hooks.VscodeCopilotPayload{
		HookEventName: "PreToolUse",
		ToolName:      &toolName,
		ToolInput:     input,
	}, authCtx.ActiveOrganizationID, authCtx.ProjectID.String())

	val, ok := attrs[attr.GenAIToolCallArgumentsKey].(string)
	require.True(t, ok, "ToolInput should be stored as a string")
	assert.Contains(t, val, "/tmp/x.go")
	assert.Contains(t, val, "package main")
}

func TestBuildVSCodeTelemetryAttributes_NilUserEmail(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestHooksService(t)

	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)

	attrs := ti.service.buildVSCodeTelemetryAttributes(ctx, &hooks.VscodeCopilotPayload{
		HookEventName:  "PreToolUse",
		UserEmailInput: nil,
	}, authCtx.ActiveOrganizationID, authCtx.ProjectID.String())

	assert.Empty(t, attrs[attr.UserEmailKey])
	_, hasSource := attrs[attr.UserEmailSourceKey]
	assert.False(t, hasSource, "UserEmailSourceKey should not be set when source input is empty")
}
