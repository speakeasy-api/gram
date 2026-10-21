package mcp

import (
	"testing"

	"github.com/stretchr/testify/require"
)

// The script interpolates values into a file that runs on someone's machine,
// so neither the URL nor the key may end the quoting and start a command.
func TestAgentInstallScriptQuotesInterpolatedValues(t *testing.T) {
	t.Parallel()
	script := agentInstallScript("https://example.test/agent-mcp/x", `key'; touch pwned #`)
	require.Contains(t, script, `GRAM_AGENT_KEY='key'\''; touch pwned #'`)
	// The injected command must stay inside the quoted word, never become a
	// statement of its own.
	require.NotContains(t, script, "\ntouch pwned")
}
