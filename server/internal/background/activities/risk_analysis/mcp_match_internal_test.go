package risk_analysis

import "testing"

// mcpServerPrefixOf feeds the `match` column on shadow_mcp findings from the
// batch scanner. Cover the format variants the parser actually sees in chat
// messages so a future tool-name format slip is caught at unit-test time
// rather than as a malformed Recent Findings row.
func TestMCPServerPrefixOf(t *testing.T) {
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
			if got := mcpServerPrefixOf(tc.in); got != tc.want {
				t.Fatalf("mcpServerPrefixOf(%q) = %q, want %q", tc.in, got, tc.want)
			}
		})
	}
}
