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

func TestRenderWarnUserReason_AppendsAckLink(t *testing.T) {
	t.Parallel()
	sr := &risk.ScanResult{Action: "warn", PolicyName: "p", RuleID: "r", Entity: "e"}
	got := renderWarnUserReason(sr, "https://example.test/ack#ack_token=rpak1.xyz")
	assert.Contains(t, got, renderWarnBody(sr))
	assert.Contains(t, got, "open this link and acknowledge")
	assert.Contains(t, got, "https://example.test/ack#ack_token=rpak1.xyz")
}

func TestRenderWarnAgentReason_HasNoLinkAndAssertsAuthority(t *testing.T) {
	t.Parallel()
	sr := &risk.ScanResult{Action: "warn", PolicyName: "secrets", RuleID: "r", Entity: "e"}
	got := renderWarnAgentReason(sr)
	assert.Contains(t, got, "secrets", "names the policy")
	assert.Contains(t, got, "not tool output", "frames it as a policy decision, not tool output")
	assert.NotContains(t, got, "http", "the model-facing reason must carry no URL")
	assert.NotContains(t, got, "ack_token")
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

// --- Handler-level tests (use the shared infra-backed service) ---

// A warn (challenge) on a tool call must DENY with an out-of-band
// acknowledgement link — unified with Cursor/Codex on the link flow, not a
// native permissionDecision "ask" (which `--dangerously-skip-permissions`
// bypasses). When no ack link can be built it falls back to a hard block;
// either way the permission decision is "deny", never "ask" or "allow".
func TestClaude_PreToolUse_Warn_DeniesWithChallenge(t *testing.T) {
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
	toolUseID := "toolu_warn_deny"
	userEmail := "warn-deny@example.com"

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
	assert.Equal(t, "deny", *output.PermissionDecision, "warn must deny (challenge/link), never native ask")
	require.NotNil(t, output.PermissionDecisionReason)
	assert.Contains(t, *output.PermissionDecisionReason, "danger", "the matched policy is surfaced")
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
