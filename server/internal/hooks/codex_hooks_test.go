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

	_, err := ti.service.Codex(ctx, &gen.CodexPayload{
		HookEventName: "PreToolUse",
		SessionID:     &sessionID,
		ToolName:      &toolName,
		ToolInput:     map[string]any{"foo": "bar"},
	})
	require.Error(t, err)
	require.ErrorContains(t, err, "Request access:\nhttps://app.example.test/shadow-mcp/request#request_token=smar1.",
		"shadow-MCP deny messages should include a signed approval request link")
}
