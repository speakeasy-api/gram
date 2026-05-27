package hooks

import (
	"testing"

	"github.com/stretchr/testify/require"

	gen "github.com/speakeasy-api/gram/server/gen/hooks"
)

func TestCodex_PreToolUse_ShadowMCPBlockIncludesRequestLink(t *testing.T) {
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
	require.Contains(t, *result.Reason, "Request access:\nhttps://app.example.test/shadow-mcp/request#request_token=smar1.",
		"shadow-MCP deny messages should include a signed approval request link")
	require.Contains(t, *result.Reason, shadowMCPApprovalRequestPrompt)
}
