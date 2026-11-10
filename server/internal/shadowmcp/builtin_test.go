package shadowmcp_test

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/shadowmcp"
)

func TestIsAgentBuiltinServer(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name       string
		serverName string
		url        string
		command    string
		want       bool
	}{
		{name: "claude in chrome without transport", serverName: "claude-in-chrome", want: true},
		{name: "desktop app task tools", serverName: "ccd_turn", want: true},
		{name: "desktop app session tools", serverName: "ccd_session", want: true},
		{name: "desktop app pull request tools", serverName: "ccd_pr", want: true},
		{name: "desktop app browser pane", serverName: "Claude_Browser", want: true},
		{name: "desktop app iOS simulator", serverName: "Claude_Code_iOS_Simulator", want: true},
		{name: "computer use", serverName: "computer-use", want: true},
		{name: "surrounding whitespace", serverName: " claude-in-chrome ", want: true},
		{name: "built-in name with stdio command", serverName: "claude-in-chrome", command: "npx some-mcp", want: false},
		{name: "built-in prefix with stdio command", serverName: "ccd_turn", command: "node server.js", want: false},
		{name: "built-in name with remote URL", serverName: "Claude_Browser", url: "https://mcp.example.test/mcp", want: false},
		{name: "built-in name with Speakeasy-hosted URL", serverName: "computer-use", url: "https://app.getgram.ai/mcp/team", want: false},
		{name: "bare reserved prefix", serverName: "ccd_", want: false},
		{name: "different case", serverName: "claude_browser", want: false},
		{name: "third-party server without transport", serverName: "linear", want: false},
		{name: "empty name", serverName: "", want: false},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			require.Equal(t, tc.want, shadowmcp.IsAgentBuiltinServer(tc.serverName, tc.url, tc.command))
		})
	}
}
