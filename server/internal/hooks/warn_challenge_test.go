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
	shadowPolicy      *risk.ShadowMCPPolicy
	acknowledged      bool
	recordedChallenge bool
}

func (s *stubResultScanner) ScanForEnforcement(_ context.Context, _ string, _ uuid.UUID, _ string, _ string, _ string, _ string) (*risk.ScanResult, error) {
	return s.result, nil
}

func (s *stubResultScanner) LookupShadowMCPBlockingPolicy(_ context.Context, _ string, _ uuid.UUID, _ string) (*risk.ShadowMCPPolicy, error) {
	return s.shadowPolicy, nil
}

func (s *stubResultScanner) HasEnabledShadowMCPPolicy(_ context.Context, _ uuid.UUID) (bool, error) {
	return false, nil
}

func (s *stubResultScanner) HasAcknowledgedChallenge(_ context.Context, _ uuid.UUID, _, _, _, _ string) bool {
	return s.acknowledged
}

func (s *stubResultScanner) RecordPolicyChallenge(_ context.Context, _ string, _ uuid.UUID, _, _, _, _, _, _, _ string) {
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

// When no acknowledgement link can be built (missing site URL / cache / user),
// a warn must fail SAFE to a hard block — never a native ask, never allow, and
// with no dangling half-challenge. Guards the security-critical fallback path.
func TestClaude_PreToolUse_Warn_FallsBackToBlockWhenNoLink(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestHooksService(t)
	// Force warnDenyReason to return ok=false: no site URL means no ack link.
	ti.service.siteURL = nil
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
	toolUseID := "toolu_warn_fallback"
	userEmail := "warn-fallback@example.com"

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
	assert.Equal(t, "deny", *output.PermissionDecision, "no-link warn must hard-deny, never ask or allow")
	require.NotNil(t, output.PermissionDecisionReason)
	// Fell through to the plain block path, not the challenge path: no ack link.
	assert.NotContains(t, *output.PermissionDecisionReason, "risk-policy-challenge/acknowledge", "fallback block must not carry an ack link")
	assert.NotContains(t, *output.PermissionDecisionReason, "ack_token=")
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

// canonicalToolRequest builds a plain (non-MCP, non-permission) tool.requested
// payload that routes through scanToolRequestForEnforcement, for the given adapter.
func canonicalToolRequest(adapter, sessionID string) *gen.IngestPayload {
	toolName := "bash"
	toolCallID := "call-" + sessionID
	payload := canonicalIngestPayload(adapter, "tool.requested", sessionID)
	payload.Data = &gen.HookIngestData{
		ToolCall: &gen.HookToolCallData{
			ID:    &toolCallID,
			Name:  &toolName,
			Input: map[string]any{"command": "rm -rf /tmp/warn"},
		},
	}
	return payload
}

// A block on the canonical path hard-denies for every adapter (unchanged).
func TestIngest_Opencode_Block_Denies(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestHooksService(t)
	ti.service.riskScanner = &stubResultScanner{result: &risk.ScanResult{
		Action:      "block",
		PolicyID:    uuid.NewString(),
		PolicyName:  "secret policy",
		Description: "leaked credential",
	}}

	result, err := ti.service.Ingest(ctx, canonicalToolRequest("opencode", "canonical-opencode-block"))
	require.NoError(t, err)
	require.NotNil(t, result)
	assert.Equal(t, "deny", result.Decision)
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

func TestIngest_CanonicalWarnChallengesEveryAdapterAndEvent(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestHooksService(t)
	adapters := []string{"claude", "cursor", "codex", "opencode"}
	eventKinds := []string{"prompt", "tool", "mcp", "permission"}

	for _, adapter := range adapters {
		for _, eventKind := range eventKinds {
			scanner := &stubResultScanner{result: &risk.ScanResult{
				Action:          "warn",
				PolicyID:        uuid.NewString(),
				PolicyName:      "danger",
				Description:     "requires review",
				RuleID:          "dangerous-operation",
				Entity:          "destructive",
				MatchedValue:    "rm -rf /tmp/warn",
				CallFingerprint: "canonical-warn-fingerprint",
			}}
			ti.service.riskScanner = scanner
			payload := canonicalWarnTestPayload(t, adapter, eventKind, adapter+"-"+eventKind+"-"+uuid.NewString())

			result, err := ti.service.Ingest(ctx, payload)
			require.NoErrorf(t, err, "%s %s warn request", adapter, eventKind)
			require.NotNilf(t, result, "%s %s warn request", adapter, eventKind)
			require.Equalf(t, "deny", result.Decision, "%s %s warn request", adapter, eventKind)
			require.NotNilf(t, result.Message, "%s %s warn request", adapter, eventKind)
			require.Containsf(t, *result.Message, "risk-policy-challenge/acknowledge", "%s %s warn request", adapter, eventKind)
			require.Containsf(t, *result.Message, "ack_token=", "%s %s warn request", adapter, eventKind)
			require.Truef(t, scanner.recordedChallenge, "%s %s warn request", adapter, eventKind)

			scanner.acknowledged = true
			scanner.recordedChallenge = false

			retryResult, err := ti.service.Ingest(ctx, payload)
			require.NoErrorf(t, err, "%s %s acknowledged retry", adapter, eventKind)
			require.NotNilf(t, retryResult, "%s %s acknowledged retry", adapter, eventKind)
			require.Equalf(t, "allow", retryResult.Decision, "%s %s acknowledged retry", adapter, eventKind)
			require.Nilf(t, retryResult.Message, "%s %s acknowledged retry", adapter, eventKind)
			require.Falsef(t, scanner.recordedChallenge, "%s %s acknowledged retry", adapter, eventKind)
		}
	}
}

// An acknowledged permission warn clears only that risk challenge — it must
// still flow into the shadow-MCP guard, never let the tool call skip it. This
// is the regression guard for the fix where an acknowledged permission warn
// returned early instead of falling through: with the bug the guard is skipped
// and the call is allowed; with the fix the unapproved MCP server is denied.
func TestIngest_CanonicalAcknowledgedPermissionWarn_StillRunsShadowMCPGuard(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestHooksService(t)
	ti.service.riskScanner = &stubResultScanner{
		acknowledged: true,
		result: &risk.ScanResult{
			Action:          "warn",
			PolicyID:        uuid.NewString(),
			PolicyName:      "danger",
			Description:     "requires review",
			RuleID:          "dangerous-operation",
			Entity:          "destructive",
			MatchedValue:    "rm -rf /tmp/warn",
			CallFingerprint: "canonical-perm-warn-shadow",
		},
		// A shadow-MCP blocking policy is enabled; with no bypass grant and a
		// non-Gram-hosted server the guard must deny.
		shadowPolicy: &risk.ShadowMCPPolicy{ID: uuid.NewString(), Name: "shadow guard"},
	}

	// tool.requested for an MCP tool that also carries a permission type, so the
	// permission block runs first and control must still reach the MCP guard.
	toolName := "mcp__filesystem__remove"
	permissionType := "exec"
	serverIdentity := "filesystem"
	payload := canonicalIngestPayload("claude", "tool.requested", "perm-warn-shadow-"+uuid.NewString())
	payload.Data = &gen.HookIngestData{
		ToolCall: &gen.HookToolCallData{
			Name:           &toolName,
			Input:          map[string]any{"command": "rm -rf /tmp/warn"},
			PermissionType: &permissionType,
		},
		Mcp: &gen.HookMCPData{ServerIdentity: &serverIdentity},
	}

	result, err := ti.service.Ingest(ctx, payload)
	require.NoError(t, err)
	require.NotNil(t, result)
	require.Equal(t, "deny", result.Decision, "acknowledged permission warn must not bypass the shadow-MCP guard")
	require.NotNil(t, result.Message)
	require.Contains(t, *result.Message, "shadow guard")
}

func TestIngest_CanonicalWarnFallsBackToBlockWithoutAckLink(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestHooksService(t)
	ti.service.siteURL = nil
	ti.service.riskScanner = &stubResultScanner{result: &risk.ScanResult{
		Action:          "warn",
		PolicyID:        uuid.NewString(),
		PolicyName:      "danger",
		Description:     "requires review",
		RuleID:          "dangerous-operation",
		Entity:          "destructive",
		MatchedValue:    "rm -rf /tmp/warn",
		CallFingerprint: "canonical-warn-fallback",
	}}
	payload := canonicalWarnTestPayload(t, "opencode", "prompt", "warn-fallback-"+uuid.NewString())

	result, err := ti.service.Ingest(ctx, payload)
	require.NoError(t, err)
	require.NotNil(t, result)
	require.Equal(t, "deny", result.Decision)
	require.NotNil(t, result.Message)
	require.NotContains(t, *result.Message, "risk-policy-challenge/acknowledge")
	require.NotContains(t, *result.Message, "ack_token=")
}

func TestIngest_CanonicalBlockBehaviorUnchanged(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestHooksService(t)
	ti.service.riskScanner = &stubResultScanner{result: &risk.ScanResult{
		Action:      "block",
		PolicyID:    uuid.NewString(),
		PolicyName:  "hard block",
		Description: "must not run",
	}}
	payload := canonicalWarnTestPayload(t, "opencode", "prompt", "block-regression-"+uuid.NewString())

	result, err := ti.service.Ingest(ctx, payload)
	require.NoError(t, err)
	require.NotNil(t, result)
	require.Equal(t, "deny", result.Decision)
	require.NotNil(t, result.Message)
	require.Contains(t, *result.Message, "hard block")
	require.NotContains(t, *result.Message, "ack_token=")
}

func canonicalWarnTestPayload(t *testing.T, adapter, eventKind, sessionID string) *gen.IngestPayload {
	t.Helper()

	payload := canonicalIngestPayload(adapter, "tool.requested", sessionID)
	toolName := "Bash"
	toolInput := map[string]any{"command": "rm -rf /tmp/warn"}

	switch eventKind {
	case "prompt":
		prompt := "remove everything under /tmp/warn"
		payload.Event.Type = "prompt.submitted"
		payload.Data = &gen.HookIngestData{
			Prompt: &gen.HookPromptData{Text: &prompt},
		}
	case "tool":
		payload.Data = &gen.HookIngestData{
			ToolCall: &gen.HookToolCallData{
				Name:  &toolName,
				Input: toolInput,
			},
		}
	case "mcp":
		toolName = "mcp__filesystem__remove"
		serverIdentity := "filesystem"
		payload.Data = &gen.HookIngestData{
			ToolCall: &gen.HookToolCallData{
				Name:  &toolName,
				Input: toolInput,
			},
			Mcp: &gen.HookMCPData{ServerIdentity: &serverIdentity},
		}
	case "permission":
		permissionType := "exec"
		payload.Data = &gen.HookIngestData{
			ToolCall: &gen.HookToolCallData{
				Name:           &toolName,
				Input:          toolInput,
				PermissionType: &permissionType,
			},
		}
	default:
		require.FailNow(t, "unsupported canonical warn event kind", eventKind)
	}

	return payload
}
