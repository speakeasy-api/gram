package hooks

import (
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	gen "github.com/speakeasy-api/gram/server/gen/hooks"
	"github.com/speakeasy-api/gram/server/internal/risk"
	"github.com/speakeasy-api/gram/server/internal/scanners/llmanalyzer"
)

// analysisUnavailableResult is the ScanResult the scanner returns when the
// LLM analyzer lane fails closed for a policy with the given action.
func analysisUnavailableResult(action string) *risk.ScanResult {
	userMessage := "You tried to leak a secret."
	return &risk.ScanResult{
		Action:           action,
		PolicyID:         uuid.NewString(),
		PolicyName:       "secrets policy",
		Source:           llmanalyzer.Source,
		RuleID:           llmanalyzer.RuleDeadLetter,
		Description:      "Risk analysis unavailable: deadline",
		UserMessage:      &userMessage,
		DeadLetterReason: "deadline",
	}
}

const analysisUnavailableCopy = `Risk analysis is temporarily unavailable; this action was denied by policy "secrets policy".`

func requirePlainUnavailableDeny(t *testing.T, reason string) {
	t.Helper()
	require.Contains(t, reason, analysisUnavailableCopy)
	require.NotContains(t, reason, "You tried to leak a secret.", "the policy's user_message describes a violation that did not happen")
	require.NotContains(t, reason, "risk-policy-challenge/acknowledge", "an outage is not acknowledgeable")
	require.NotContains(t, reason, "ack_token=")
}

func TestRenderUserBlockReason_AnalysisUnavailableIgnoresUserMessage(t *testing.T) {
	t.Parallel()
	require.Equal(t, analysisUnavailableCopy, renderUserBlockReason(analysisUnavailableResult("block"), "audit reason"))

	// A real match still honours the policy's user_message.
	matched := analysisUnavailableResult("block")
	matched.DeadLetterReason = ""
	matched.RuleID = llmanalyzer.RuleSecret
	require.Equal(t, "You tried to leak a secret.", renderUserBlockReason(matched, "audit reason"))
}

func TestClaude_PreToolUse_WarnUnavailable_DeniesWithoutChallenge(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestHooksService(t)
	scanner := &stubResultScanner{result: analysisUnavailableResult("warn")}
	ti.service.riskScanner = scanner

	sessionID := uuid.NewString()
	toolName := "Bash"
	toolUseID := "toolu_llm_unavailable"
	userEmail := "llm-unavailable@example.com"

	result, err := ti.service.Claude(ctx, &gen.ClaudePayload{
		HookEventName: "PreToolUse",
		SessionID:     &sessionID,
		UserEmail:     &userEmail,
		ToolName:      &toolName,
		ToolUseID:     &toolUseID,
		ToolInput:     map[string]any{"command": "cat ~/.aws/credentials"},
	})
	require.NoError(t, err)
	require.NotNil(t, result)

	output, ok := result.HookSpecificOutput.(*HookSpecificOutput)
	require.True(t, ok)
	require.NotNil(t, output.PermissionDecision)
	require.Equal(t, "deny", *output.PermissionDecision)
	require.NotNil(t, output.PermissionDecisionReason)
	requirePlainUnavailableDeny(t, *output.PermissionDecisionReason)
	require.NotNil(t, result.SystemMessage)
	requirePlainUnavailableDeny(t, *result.SystemMessage)
	require.False(t, scanner.recordedChallenge, "no challenge row is minted for an outage")
}

func TestClaude_UserPromptSubmit_WarnUnavailable_Blocks(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestHooksService(t)
	ti.service.riskScanner = &stubResultScanner{result: analysisUnavailableResult("warn")}

	sessionID := uuid.NewString()
	prompt := "here is my access key"

	// A warn challenge passes prompts through to the follow-on tool call; a
	// fail-closed sentinel has no tool call to defer to and must deny here.
	result, err := ti.service.Claude(ctx, &gen.ClaudePayload{
		HookEventName: "UserPromptSubmit",
		SessionID:     &sessionID,
		Prompt:        &prompt,
	})
	require.NoError(t, err)
	require.NotNil(t, result)
	require.NotNil(t, result.Decision)
	require.Equal(t, "block", *result.Decision)
	require.NotNil(t, result.Reason)
	requirePlainUnavailableDeny(t, *result.Reason)
}

func TestCursor_PreToolUse_BlockUnavailable_DeniesWithReason(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestHooksService(t)
	ti.service.riskScanner = &stubResultScanner{result: analysisUnavailableResult("block")}

	toolName := "Shell"
	toolUseID := "toolu_cursor_llm_unavailable"
	userEmail := "llm-unavailable@example.com"
	conversationID := "conv-llm-unavailable"

	result, err := ti.service.Cursor(ctx, &gen.CursorPayload{
		HookEventName:  "preToolUse",
		ToolName:       &toolName,
		ToolUseID:      &toolUseID,
		ToolInput:      map[string]any{"command": "cat ~/.aws/credentials"},
		UserEmail:      &userEmail,
		ConversationID: &conversationID,
	})
	require.NoError(t, err)
	require.NotNil(t, result)
	require.NotNil(t, result.Permission)
	require.Equal(t, "deny", *result.Permission)
	require.NotNil(t, result.UserMessage)
	requirePlainUnavailableDeny(t, *result.UserMessage)
	require.NotNil(t, result.AgentMessage)
	requirePlainUnavailableDeny(t, *result.AgentMessage)
}

func TestCursor_PreToolUse_WarnUnavailable_DeniesWithoutChallenge(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestHooksService(t)
	scanner := &stubResultScanner{result: analysisUnavailableResult("warn")}
	ti.service.riskScanner = scanner

	toolName := "Shell"
	toolUseID := "toolu_cursor_llm_unavailable_warn"
	userEmail := "llm-unavailable@example.com"
	conversationID := "conv-llm-unavailable-warn"

	result, err := ti.service.Cursor(ctx, &gen.CursorPayload{
		HookEventName:  "preToolUse",
		ToolName:       &toolName,
		ToolUseID:      &toolUseID,
		ToolInput:      map[string]any{"command": "cat ~/.aws/credentials"},
		UserEmail:      &userEmail,
		ConversationID: &conversationID,
	})
	require.NoError(t, err)
	require.NotNil(t, result)
	require.NotNil(t, result.Permission)
	require.Equal(t, "deny", *result.Permission)
	require.NotNil(t, result.UserMessage)
	requirePlainUnavailableDeny(t, *result.UserMessage)
	require.False(t, scanner.recordedChallenge)
}

func TestCodex_PreToolUse_WarnUnavailable_DeniesWithoutChallenge(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestHooksService(t)
	scanner := &stubResultScanner{result: analysisUnavailableResult("warn")}
	ti.service.riskScanner = scanner

	sessionID := "codex-session-llm-unavailable"
	toolName := "shell"
	userEmail := "llm-unavailable@example.com"

	result, err := ti.service.Codex(ctx, &gen.CodexPayload{
		HookEventName: "PreToolUse",
		SessionID:     &sessionID,
		UserEmail:     &userEmail,
		ToolName:      &toolName,
		ToolInput:     map[string]any{"command": "cat ~/.aws/credentials"},
	})
	require.NoError(t, err)
	require.NotNil(t, result)
	require.NotNil(t, result.Decision)
	require.Equal(t, "deny", *result.Decision)
	require.NotNil(t, result.Reason)
	requirePlainUnavailableDeny(t, *result.Reason)
	require.False(t, scanner.recordedChallenge)
}

func TestCodex_PreToolUse_BlockUnavailable_DeniesWithReason(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestHooksService(t)
	ti.service.riskScanner = &stubResultScanner{result: analysisUnavailableResult("block")}

	sessionID := "codex-session-llm-unavailable-block"
	toolName := "shell"
	userEmail := "llm-unavailable@example.com"

	result, err := ti.service.Codex(ctx, &gen.CodexPayload{
		HookEventName: "PreToolUse",
		SessionID:     &sessionID,
		UserEmail:     &userEmail,
		ToolName:      &toolName,
		ToolInput:     map[string]any{"command": "cat ~/.aws/credentials"},
	})
	require.NoError(t, err)
	require.NotNil(t, result)
	require.NotNil(t, result.Decision)
	require.Equal(t, "deny", *result.Decision)
	require.NotNil(t, result.Reason)
	requirePlainUnavailableDeny(t, *result.Reason)
}

func TestIngest_CanonicalToolWarnUnavailable_DeniesWithoutAckLink(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestHooksService(t)
	scanner := &stubResultScanner{result: analysisUnavailableResult("warn")}
	ti.service.riskScanner = scanner

	result, err := ti.service.Ingest(ctx, canonicalToolRequest("opencode", "canonical-llm-unavailable-warn"))
	require.NoError(t, err)
	require.NotNil(t, result)
	require.Equal(t, "deny", result.Decision)
	require.NotNil(t, result.Message)
	requirePlainUnavailableDeny(t, *result.Message)
	require.False(t, scanner.recordedChallenge)
}

func TestIngest_CanonicalToolBlockUnavailable_DeniesWithReason(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestHooksService(t)
	ti.service.riskScanner = &stubResultScanner{result: analysisUnavailableResult("block")}

	result, err := ti.service.Ingest(ctx, canonicalToolRequest("claude", "canonical-llm-unavailable-block"))
	require.NoError(t, err)
	require.NotNil(t, result)
	require.Equal(t, "deny", result.Decision)
	require.NotNil(t, result.Message)
	requirePlainUnavailableDeny(t, *result.Message)
}
