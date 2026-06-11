package hooks

import (
	"testing"

	"github.com/stretchr/testify/require"

	gen "github.com/speakeasy-api/gram/server/gen/hooks"
	"github.com/speakeasy-api/gram/server/internal/attr"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
)

func TestCodex_PreToolUse_ShadowMCPBlockWithIdentityEvidenceIncludesRequestLink(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestHooksService(t)
	ti.service.riskScanner = stubBlockingShadowMCPScanner{}

	sessionID := "codex-session-blocked"
	toolName := "mcp__gram__do_thing"

	result, err := ti.service.Codex(ctx, &gen.CodexPayload{
		HookEventName: "PreToolUse",
		SessionID:     &sessionID,
		ToolName:      &toolName,
		ToolInput:     map[string]any{"foo": "bar"},
	})
	require.NoError(t, err)
	require.NotNil(t, result)
	require.NotNil(t, result.Decision)
	require.Equal(t, "deny", *result.Decision)
	require.NotNil(t, result.Reason)
	require.Contains(t, *result.Reason, "Request access:")
	require.Contains(t, *result.Reason, "/risk-policy-bypass/request#request_token=rpbr1.")
	require.Contains(t, *result.Reason, shadowMCPApprovalRequestPrompt)
}

func TestBuildCodexTelemetryAttributes_UsesPayloadUserEmail(t *testing.T) {
	t.Parallel()
	_, ti := newTestHooksService(t)

	email := "dev@example.com"
	payload := &gen.CodexPayload{
		HookEventName: "PreToolUse",
		UserEmail:     &email,
	}
	metadata := &SessionMetadata{
		SessionID:   "",
		ServiceName: "Codex",
		UserEmail:   email,
		UserID:      "",
		ClaudeOrgID: "",
		GramOrgID:   "org-id",
		ProjectID:   "project-id",
	}

	attrs := ti.service.buildCodexTelemetryAttributes(t.Context(), payload, metadata)
	require.Equal(t, email, attrs[attr.UserEmailKey])
}

func TestCodexSessionMetadata_CachesSessionStartEmail(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestHooksService(t)

	sessionID := "codex-session-with-email"
	email := "dev@example.com"
	_, err := ti.service.Codex(ctx, &gen.CodexPayload{
		HookEventName: "SessionStart",
		SessionID:     &sessionID,
		UserEmail:     &email,
	})
	require.NoError(t, err)

	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)

	metadata := ti.service.codexSessionMetadata(ctx, &gen.CodexPayload{
		HookEventName: "PreToolUse",
		SessionID:     &sessionID,
	}, authCtx.ActiveOrganizationID, authCtx.ProjectID.String())
	require.Equal(t, email, metadata.UserEmail)
}
