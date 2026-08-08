package hooks

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/google/uuid"

	gen "github.com/speakeasy-api/gram/server/gen/hooks"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/risk"
)

// blockEffectShadowMCPScanner is ingestUserScopedShadowMCPScanner with a real
// policy UUID: request-link minting validates risk_policy_id as a UUID, and
// these tests need the link (and therefore the effect) to actually mint.
type blockEffectShadowMCPScanner struct {
	ingestUserScopedShadowMCPScanner
	policyID string
}

func (s blockEffectShadowMCPScanner) LookupShadowMCPBlockingPolicy(_ context.Context, _ string, _ uuid.UUID, userID string) (*risk.ShadowMCPPolicy, error) {
	if userID != s.ingestUserScopedShadowMCPScanner.userID {
		return nil, nil
	}
	return &risk.ShadowMCPPolicy{ID: s.policyID, Name: "shadow-mcp-block"}, nil
}

func shadowMCPDenyPayload(sessionID, toolCallID string) *gen.IngestPayload {
	toolName := "mcp__local_server__search"
	serverIdentity := "local-server"
	payload := canonicalIngestPayload("custom-adapter", "tool.requested", sessionID)
	payload.Data = &gen.HookIngestData{
		ToolCall: &gen.HookToolCallData{
			ID:    &toolCallID,
			Name:  &toolName,
			Input: map[string]any{"query": "secret"},
		},
		Mcp: &gen.HookMCPData{
			ServerIdentity: &serverIdentity,
		},
	}
	return payload
}

// A shadow-MCP deny that minted a request link must also carry the structured
// "block" effect so the hooks binary can hand the device agent something it
// can act on without parsing prose.
func TestIngest_ShadowMCPDenyCarriesBlockEffect(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestHooksService(t)
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	require.NotNil(t, authCtx.ProjectID)

	ti.service.riskScanner = blockEffectShadowMCPScanner{
		ingestUserScopedShadowMCPScanner: ingestUserScopedShadowMCPScanner{userID: authCtx.UserID},
		policyID:                         uuid.NewString(),
	}

	result, err := ti.service.Ingest(ctx, shadowMCPDenyPayload("block-effect-shadow-deny", "call-effect-1"))
	require.NoError(t, err)
	require.NotNil(t, result)
	require.Equal(t, "deny", result.Decision)

	effect := requireEffectMap(t, result.Effects, "block")
	require.Equal(t, blockEffectContractVersion, effect["v"])
	require.Equal(t, "shadow_mcp", effect["category"])
	require.Equal(t, true, effect["requestable"])

	token, ok := effect["request_token"].(string)
	require.True(t, ok)
	require.True(t, strings.HasPrefix(token, "rpbr2."), "token must be the short cache-backed format")

	requestURL, ok := effect["request_url"].(string)
	require.True(t, ok)
	require.Contains(t, requestURL, "/risk-policy-bypass/request#request_token="+token,
		"URL and token must reference the same request state")
	require.NotNil(t, result.Message)
	require.Contains(t, *result.Message, requestURL, "effect must mirror the link in the prose, not mint a second one")

	expiresAt, err := time.Parse(time.RFC3339, effect["request_expires_at"].(string))
	require.NoError(t, err)
	require.WithinDuration(t, time.Now().Add(shadowMCPApprovalRequestTokenTTL), expiresAt, time.Minute)

	require.Equal(t, "shadow-mcp-block", effect["policy_name"])
	require.Equal(t, "search", effect["tool_name"])
	require.NotEmpty(t, effect["server_name"])
	require.NotContains(t, effect, "server_url", "identity-only evidence carries no URL")

	blockURL, ok := effect["block_url"].(string)
	require.True(t, ok, "first delivery must carry the durable block-row URL")
	require.Contains(t, *result.Message, blockURL)
}

// An allow must never carry the effect.
func TestIngest_AllowCarriesNoBlockEffect(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestHooksService(t)

	result, err := ti.service.Ingest(ctx, canonicalIngestPayload("claude", "session.started", "block-effect-allow"))
	require.NoError(t, err)
	require.NotNil(t, result)
	require.Equal(t, "allow", result.Decision)
	require.NotContains(t, result.Effects, "block")
}

// A scan-based deny (PII, prompt policy, …) is not requestable in v1 and must
// not carry the effect — its absence is the "not requestable" signal.
func TestIngest_ScanDenyCarriesNoBlockEffect(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestHooksService(t)

	prompt := "scan deny prompt"
	payload := canonicalIngestPayload("claude", "prompt.submitted", "block-effect-scan-deny")
	payload.Data = &gen.HookIngestData{
		Prompt: &gen.HookPromptData{Text: &prompt},
	}
	ti.service.riskScanner = &stubResultScanner{result: &risk.ScanResult{
		Action:      "block",
		PolicyID:    uuid.NewString(),
		PolicyName:  "pii boundary policy",
		Description: "blocked by deterministic test scanner",
	}}

	result, err := ti.service.Ingest(ctx, payload)
	require.NoError(t, err)
	require.NotNil(t, result)
	require.Equal(t, "deny", result.Decision)
	require.NotContains(t, result.Effects, "block")
}

// A shadow-MCP deny whose link minting is unavailable (no site URL) has
// nothing for the user to request — deny stands, no effect.
func TestIngest_ShadowMCPDenyWithoutLinkCarriesNoBlockEffect(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestHooksService(t)
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)

	ti.service.riskScanner = blockEffectShadowMCPScanner{
		ingestUserScopedShadowMCPScanner: ingestUserScopedShadowMCPScanner{userID: authCtx.UserID},
		policyID:                         uuid.NewString(),
	}
	ti.service.siteURL = nil

	result, err := ti.service.Ingest(ctx, shadowMCPDenyPayload("block-effect-no-link", "call-effect-2"))
	require.NoError(t, err)
	require.NotNil(t, result)
	require.Equal(t, "deny", result.Decision)
	require.NotContains(t, result.Effects, "block")
}

// A duplicate delivery re-mints the request link (the prose re-sends it too)
// but never a second block row, so the effect exists without block_url.
func TestIngest_DuplicateDeliveryBlockEffectOmitsBlockURL(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestHooksService(t)
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)

	ti.service.riskScanner = blockEffectShadowMCPScanner{
		ingestUserScopedShadowMCPScanner: ingestUserScopedShadowMCPScanner{userID: authCtx.UserID},
		policyID:                         uuid.NewString(),
	}

	idempotencyKey := "block-effect-dup-" + uuid.NewString()
	payload := shadowMCPDenyPayload("block-effect-dup", "call-effect-dup")
	payload.IdempotencyKey = &idempotencyKey

	first, err := ti.service.Ingest(ctx, payload)
	require.NoError(t, err)
	require.Equal(t, "deny", first.Decision)
	require.Contains(t, requireEffectMap(t, first.Effects, "block"), "block_url")

	retry, err := ti.service.Ingest(ctx, payload)
	require.NoError(t, err)
	require.Equal(t, "deny", retry.Decision)
	retryEffect := requireEffectMap(t, retry.Effects, "block")
	require.NotContains(t, retryEffect, "block_url",
		"a duplicate delivery must not reference a block row it never minted")
	require.Contains(t, retryEffect["request_token"], "rpbr2.")
}
