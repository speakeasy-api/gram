package hooks

import (
	"context"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	gen "github.com/speakeasy-api/gram/server/gen/hooks"
	"github.com/speakeasy-api/gram/server/internal/risk"
)

// stubResultScanner is a RiskScanner that returns a fixed ScanForEnforcement
// result, letting the warn (challenge) branches be exercised deterministically.
type stubResultScanner struct {
	result            *risk.ScanResult
	acknowledged      bool
	recordedChallenge bool
}

func (s *stubResultScanner) ScanForEnforcement(_ context.Context, _ string, _ uuid.UUID, _ string, _ string, _ string, _ string) (*risk.ScanResult, error) {
	return s.result, nil
}

func (s *stubResultScanner) LookupShadowMCPBlockingPolicy(_ context.Context, _ string, _ uuid.UUID, _ string) (*risk.ShadowMCPPolicy, error) {
	return nil, nil
}

func (s *stubResultScanner) HasEnabledShadowMCPPolicy(_ context.Context, _ uuid.UUID) (bool, error) {
	return false, nil
}

func (s *stubResultScanner) HasAcknowledgedChallenge(_ context.Context, _ uuid.UUID, _, _, _ string) bool {
	return s.acknowledged
}

func (s *stubResultScanner) RecordPolicyChallenge(_ context.Context, _ string, _ uuid.UUID, _, _, _, _, _, _ string) {
	s.recordedChallenge = true
}

// --- Pure rendering / response-shape tests (no infra dependency) ---

func TestRenderWarnBody_SubstitutesPlaceholders(t *testing.T) {
	t.Parallel()
	sr := &risk.ScanResult{
		Action:       "warn",
		PolicyName:   "secrets",
		RuleID:       "stripe-key",
		Entity:       "stripe",
		MatchedValue: "sk_live_abc123",
		UserMessage:  new("%{match} is risky (%{entity}/%{policy}/%{rule})"),
	}
	got := renderWarnBody(sr)
	assert.Equal(t, "sk_live_abc123 is risky (stripe/secrets/stripe-key)", got)
}

func TestRenderWarnBody_DefaultIncludesMatchAndEntity(t *testing.T) {
	t.Parallel()
	sr := &risk.ScanResult{
		Action:       "warn",
		PolicyName:   "danger",
		RuleID:       "rmrf",
		Entity:       "destructive",
		MatchedValue: "rm -rf /tmp/warn",
	}
	got := renderWarnBody(sr)
	assert.Contains(t, got, "danger")
	assert.Contains(t, got, "rm -rf /tmp/warn")
	assert.Contains(t, got, "destructive")
}

func TestRenderWarnBody_EmptyMatchFallsBackToRuleID(t *testing.T) {
	t.Parallel()
	// No Entity and no MatchedValue (judge-based match): entity falls back to
	// RuleID and the message omits the (absent) matched value.
	sr := &risk.ScanResult{
		Action:     "warn",
		PolicyName: "prompt-injection",
		RuleID:     "pi-rule",
	}
	got := renderWarnBody(sr)
	assert.Contains(t, got, "prompt-injection")
	assert.Contains(t, got, "pi-rule")
	assert.NotContains(t, got, "%{")
}

func TestRenderWarnReason_AppendsAckLink(t *testing.T) {
	t.Parallel()
	sr := &risk.ScanResult{Action: "warn", PolicyName: "p", RuleID: "r", Entity: "e"}
	got := renderWarnReason(sr, "https://example.test/ack#ack_token=rpak1.xyz")
	assert.Contains(t, got, renderWarnBody(sr))
	assert.Contains(t, got, "Acknowledge to proceed")
	assert.Contains(t, got, "https://example.test/ack#ack_token=rpak1.xyz")
}

func TestTruncateForWarn(t *testing.T) {
	t.Parallel()
	short := strings.Repeat("a", warnMatchMaxLen)
	assert.Equal(t, short, truncateForWarn(short), "at the limit is unchanged")

	long := strings.Repeat("b", warnMatchMaxLen+50)
	got := truncateForWarn(long)
	assert.True(t, strings.HasSuffix(got, "…"))
	assert.Len(t, []rune(strings.TrimSuffix(got, "…")), warnMatchMaxLen)
}

func TestEmphasizeWarn_WrapsWithMarkerAndAmber(t *testing.T) {
	t.Parallel()
	got := emphasizeWarn("watch out")
	assert.True(t, strings.HasPrefix(got, "⚠ "), "leads with a warning marker")
	assert.Contains(t, got, ansiAmberBold)
	assert.Contains(t, got, ansiReset)
	assert.Contains(t, got, "watch out")
}

func TestConstructAskResponse_PreToolUseAsks(t *testing.T) {
	t.Parallel()
	res := constructAskResponse("PreToolUse", "please confirm")
	output, ok := res.HookSpecificOutput.(*HookSpecificOutput)
	require.True(t, ok)
	require.NotNil(t, output.PermissionDecision)
	assert.Equal(t, "ask", *output.PermissionDecision)
	require.NotNil(t, output.PermissionDecisionReason)
	assert.Equal(t, "please confirm", *output.PermissionDecisionReason)
	require.NotNil(t, res.SystemMessage)
	assert.Equal(t, "please confirm", *res.SystemMessage)
	assert.Nil(t, res.Decision, "ask must not set a top-level block decision")
}

func TestConstructAskResponse_NonPreToolUseFallsBackToBlock(t *testing.T) {
	t.Parallel()
	// UserPromptSubmit has no "ask" primitive, so ask degrades to a fail-safe
	// block rather than silently allowing.
	res := constructAskResponse("UserPromptSubmit", "cannot ask here")
	require.NotNil(t, res.Decision)
	assert.Equal(t, "block", *res.Decision)
	require.NotNil(t, res.Reason)
	assert.Equal(t, "cannot ask here", *res.Reason)
}

// --- Handler-level tests (use the shared infra-backed service) ---

// A warn (challenge) on a tool call must defer to Claude Code's native
// confirmation prompt (permissionDecision "ask"), never a hard deny.
func TestClaude_PreToolUse_Warn_AsksInsteadOfBlocking(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestHooksService(t)
	ti.service.riskScanner = &stubResultScanner{result: &risk.ScanResult{
		Action:       "warn",
		PolicyID:     uuid.NewString(),
		PolicyName:   "danger",
		RuleID:       "rmrf",
		Entity:       "destructive",
		MatchedValue: "rm -rf /tmp/warn",
	}}

	sessionID := uuid.NewString()
	toolName := "Bash"
	toolUseID := "toolu_warn_ask"
	userEmail := "warn-ask@example.com"

	result, err := ti.service.Claude(ctx, &gen.ClaudePayload{
		HookEventName: "PreToolUse",
		SessionID:     &sessionID,
		UserEmail:     &userEmail,
		ToolName:      &toolName,
		ToolUseID:     &toolUseID,
		ToolInput:     map[string]any{"command": "rm -rf /tmp/warn/*"},
	})
	require.NoError(t, err)
	require.NotNil(t, result)

	output, ok := result.HookSpecificOutput.(*HookSpecificOutput)
	require.True(t, ok)
	require.NotNil(t, output.PermissionDecision)
	assert.Equal(t, "ask", *output.PermissionDecision, "warn must ask, not deny")
	require.NotNil(t, output.PermissionDecisionReason)
	assert.Contains(t, *output.PermissionDecisionReason, "rm -rf /tmp/warn", "the matched value is shown in the ephemeral warning")
	assert.Contains(t, *output.PermissionDecisionReason, "⚠", "the warning is emphasized")
}

// A warn at prompt submit has no confirmation primitive, so it must pass the
// prompt through (never block) — the follow-on tool call gets challenged. This
// guards the regression where warn hard-blocked at UserPromptSubmit.
func TestClaude_UserPromptSubmit_Warn_PassesThrough(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestHooksService(t)
	ti.service.riskScanner = &stubResultScanner{result: &risk.ScanResult{
		Action:       "warn",
		PolicyID:     uuid.NewString(),
		PolicyName:   "danger",
		RuleID:       "rmrf",
		Entity:       "destructive",
		MatchedValue: "rm -rf",
	}}

	sessionID := uuid.NewString()
	prompt := "clean out everything under /tmp/warn"

	result, err := ti.service.Claude(ctx, &gen.ClaudePayload{
		HookEventName: "UserPromptSubmit",
		SessionID:     &sessionID,
		Prompt:        &prompt,
	})
	require.NoError(t, err)
	require.NotNil(t, result)
	assert.Nil(t, result.Decision, "warn must not block the prompt")
}

// A block at prompt submit still hard-blocks with the reason.
func TestClaude_UserPromptSubmit_Block_Blocks(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestHooksService(t)
	ti.service.riskScanner = &stubResultScanner{result: &risk.ScanResult{
		Action:      "block",
		PolicyID:    uuid.NewString(),
		PolicyName:  "secret policy",
		Description: "leaked credential",
	}}

	sessionID := uuid.NewString()
	prompt := "here is a secret"

	result, err := ti.service.Claude(ctx, &gen.ClaudePayload{
		HookEventName: "UserPromptSubmit",
		SessionID:     &sessionID,
		Prompt:        &prompt,
	})
	require.NoError(t, err)
	require.NotNil(t, result)
	require.NotNil(t, result.Decision)
	assert.Equal(t, "block", *result.Decision)
	require.NotNil(t, result.Reason)
	assert.Contains(t, *result.Reason, "secret policy")
}
