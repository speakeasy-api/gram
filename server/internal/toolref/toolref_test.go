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

// TestClaudeMCPServerPrefix is calibrated against the real prefixes observed in
// a live Claude Code session for the `claude mcp list` entries named here. The
// convention is undocumented, so these cases are the specification.
func TestClaudeMCPServerPrefix(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name   string
		source string
		plugin string
		raw    string
		want   string
	}{
		{"claude.ai simple", toolref.ClaudeMCPSourceClaudeAI, "", "Slack", "claude_ai_Slack"},
		{"claude.ai with parens", toolref.ClaudeMCPSourceClaudeAI, "", "Linear (Speakeasy)", "claude_ai_Linear_Speakeasy"},
		{"claude.ai multi-word + parens", toolref.ClaudeMCPSourceClaudeAI, "", "HubSpot (Speakeasy MCP Platform)", "claude_ai_HubSpot_Speakeasy_MCP_Platform"},
		{"claude.ai parens with multi-word inner", toolref.ClaudeMCPSourceClaudeAI, "", "Speakeasy MCP Server (Read only)", "claude_ai_Speakeasy_MCP_Server_Read_only"},
		{"claude.ai with hyphens", toolref.ClaudeMCPSourceClaudeAI, "", "la-growth-machine", "claude_ai_la-growth-machine"},
		{"plugin double name", toolref.ClaudeMCPSourcePlugin, "slack", "slack", "plugin_slack_slack"},
		{"plugin distinct name", toolref.ClaudeMCPSourcePlugin, "github", "octocat-mcp", "plugin_github_octocat-mcp"},
		{"plugin hyphenated slug and spaced display name", toolref.ClaudeMCPSourcePlugin, "acme-tools", "External Acme Chat", "plugin_acme-tools_External_Acme_Chat"},
		{"local plain", "local", "", "gram", "gram"},
		{"local with hyphen", "local", "", "notion-local", "notion-local"},
	}
	for _, tc := range cases {
		require.Equal(t, tc.want, toolref.ClaudeMCPServerPrefix(tc.source, tc.plugin, tc.raw), tc.name)
	}
}

func TestSanitizeClaudeMCPName(t *testing.T) {
	t.Parallel()
	require.Equal(t, "External_Acme_Chat", toolref.SanitizeClaudeMCPName("  External  Acme Chat "))
	require.Equal(t, "Linear_Speakeasy", toolref.SanitizeClaudeMCPName("Linear (Speakeasy)"))
	require.Empty(t, toolref.SanitizeClaudeMCPName("()"))
}
