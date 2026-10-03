package relay

import (
	"net/http"
	"testing"

	"github.com/speakeasy-api/agenthooks"
	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/hooks/sdk/models/components"
)

// hangingServer answers no ingest request until the test ends, so every
// gating exchange runs out of the binary's own gate budget. Unlike a refused
// connection, the control plane is up and merely slower than the budget: the
// shape of a slow policy scan.
func hangingServer(t *testing.T) *fakeServer {
	t.Helper()
	release := make(chan struct{})
	fs := newFakeServer(t, func(components.IngestRequestBody) (int, decision) {
		<-release
		return http.StatusOK, decision{Decision: "allow", Reason: "", Message: ""}
	})
	// Registered after newFakeServer so it runs first: the server's Close
	// waits for in-flight handlers, which are parked on release.
	t.Cleanup(func() { close(release) })
	return fs
}

// requireNoTimeoutBlock asserts a gate that ran out of budget under a
// fail-open posture neither blocks nor surfaces the transport status: the
// binary's own deadline is not a server verdict, so "HTTP 0" must never reach
// the user.
func requireNoTimeoutBlock(t *testing.T, stdout []byte, stderr string) {
	t.Helper()
	require.NotContains(t, string(stdout), "HTTP 0")
	require.NotContains(t, stderr, "HTTP 0")
	require.NotContains(t, string(stdout), `"permissionDecision":"deny"`)
	require.NotContains(t, string(stdout), `"decision":"block"`)
}

// TestGateTimeoutFailsOpenWithCachedSetting: a PreToolUse whose verdict
// exchange exceeds the gate budget is let through when the org's cached
// posture is fail-open.
func TestGateTimeoutFailsOpenWithCachedSetting(t *testing.T) {
	fs := hangingServer(t)
	cfg := authedConfig(t, fs.URL)
	writeOrgSettings(cfg, true)

	res := invoke(t, cfg, agenthooks.ProviderClaudeCode, "claude/pre_tool_use.json")

	require.Equal(t, 0, res.ExitCode)
	requireNoTimeoutBlock(t, res.Stdout, res.Stderr)
}

// TestPromptGateTimeoutFailsOpenWithCachedSetting covers the other gating
// event users hit on every turn: UserPromptSubmit past the gate budget.
func TestPromptGateTimeoutFailsOpenWithCachedSetting(t *testing.T) {
	fs := hangingServer(t)
	cfg := authedConfig(t, fs.URL)
	writeOrgSettings(cfg, true)

	res := invoke(t, cfg, agenthooks.ProviderClaudeCode, "claude/user_prompt_submit.json")

	require.Equal(t, 0, res.ExitCode)
	requireNoTimeoutBlock(t, res.Stdout, res.Stderr)
}

// TestGateTimeoutBlocksWhenCachedFailClosed is the control: with a
// fail-closed posture the same timeout blocks, which proves the hanging
// server really exhausts the budget rather than answering in time.
func TestGateTimeoutBlocksWhenCachedFailClosed(t *testing.T) {
	fs := hangingServer(t)
	cfg := authedConfig(t, fs.URL)
	writeOrgSettings(cfg, false)

	res := invoke(t, cfg, agenthooks.ProviderClaudeCode, "claude/pre_tool_use.json")

	require.Contains(t, string(res.Stdout), `"permissionDecision":"deny"`)
}
