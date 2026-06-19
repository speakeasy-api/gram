package toolref_test

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/toolref"
)

func TestAttributeTool(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name     string
		in       string
		server   string
		function string
		isMCP    bool
	}{
		{"claude code mcp", "mcp__github__create_issue", "github", "create_issue", true},
		{"nested server name", "mcp__claude_ai_Linear__list_issues", "claude_ai_Linear", "list_issues", true},
		{"cursor MCP prefix", "MCP:slack:send_message", "slack:send_message", "slack:send_message", true},
		{"native tool", "Bash", "", "", false},
		{"malformed mcp without function", "mcp__github__", "", "", false},
		{"malformed mcp without server", "mcp____create_issue", "", "", false},
		{"bare cursor prefix", "MCP:", "", "", false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			server, function, isMCP := toolref.AttributeTool(tc.in)
			require.Equal(t, tc.isMCP, isMCP)
			require.Equal(t, tc.server, server)
			require.Equal(t, tc.function, function)
		})
	}
}

// MCPServerOf feeds the `match` column on shadow_mcp findings from the batch
// scanner. Cover the format variants the parser actually sees in chat messages
// so a future tool-name format slip is caught at unit-test time rather than as
// a malformed Recent Findings row.
func TestMCPServerOf(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name string
		in   string
		want string
	}{
		{"claude code mcp tool", "mcp__mise__run_task", "mise"},
		{"claude code nested server name", "mcp__claude_ai_Linear_Speakeasy__list_issues", "claude_ai_Linear_Speakeasy"},
		{"cursor MCP prefix", "MCP:slack:send_message", "slack:send_message"},
		{"native tool", "Bash", ""},
		{"malformed mcp without server", "mcp__", ""},
		{"malformed mcp without tool", "mcp__server__", "server"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			require.Equal(t, tc.want, toolref.MCPServerOf(tc.in))
		})
	}
}
