package shadowmcp_test

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/shadowmcp"
)

func TestIsAgentBuiltinServer(t *testing.T) {
	t.Parallel()

	noTransport := shadowmcp.ServerProvenance{Known: true, URL: "", Command: ""}

	tests := []struct {
		name       string
		serverName string
		provenance shadowmcp.ServerProvenance
		want       bool
	}{
		{name: "claude in chrome without transport", serverName: "claude-in-chrome", provenance: noTransport, want: true},
		{name: "desktop app task tools", serverName: "ccd_turn", provenance: noTransport, want: true},
		{name: "desktop app session tools", serverName: "ccd_session", provenance: noTransport, want: true},
		{name: "desktop app session management", serverName: "ccd_session_mgmt", provenance: noTransport, want: true},
		{name: "desktop app pull request tools", serverName: "ccd_pr", provenance: noTransport, want: true},
		{name: "desktop app view tools", serverName: "ccd_view", provenance: noTransport, want: true},
		{name: "desktop app window tools", serverName: "ccd_window", provenance: noTransport, want: true},
		{name: "desktop app sidebar tools", serverName: "ccd_sidebar", provenance: noTransport, want: true},
		{name: "desktop app settings tools", serverName: "ccd_settings", provenance: noTransport, want: true},
		{name: "desktop app host tools", serverName: "ccd_host", provenance: noTransport, want: true},
		{name: "desktop app directory tools", serverName: "ccd_directory", provenance: noTransport, want: true},
		{name: "desktop app connector tools", serverName: "ccd_connectors", provenance: noTransport, want: true},
		{name: "desktop app browser pane", serverName: "Claude_Browser", provenance: noTransport, want: true},
		{name: "desktop app iOS simulator", serverName: "Claude_Code_iOS_Simulator", provenance: noTransport, want: true},
		{name: "computer use", serverName: "computer-use", provenance: noTransport, want: true},
		{name: "surrounding whitespace", serverName: " claude-in-chrome ", provenance: noTransport, want: true},
		{name: "unknown provenance", serverName: "claude-in-chrome", provenance: shadowmcp.UnknownServerProvenance(), want: false},
		{name: "unknown provenance for a desktop app tool", serverName: "ccd_turn", provenance: shadowmcp.UnknownServerProvenance(), want: false},
		{name: "built-in name with stdio command", serverName: "claude-in-chrome", provenance: shadowmcp.ServerProvenance{Known: true, URL: "", Command: "npx some-mcp"}, want: false},
		{name: "desktop app name with stdio command", serverName: "ccd_turn", provenance: shadowmcp.ServerProvenance{Known: true, URL: "", Command: "node server.js"}, want: false},
		{name: "built-in name with remote URL", serverName: "Claude_Browser", provenance: shadowmcp.ServerProvenance{Known: true, URL: "https://mcp.example.test/mcp", Command: ""}, want: false},
		{name: "built-in name with Speakeasy-hosted URL", serverName: "computer-use", provenance: shadowmcp.ServerProvenance{Known: true, URL: "https://app.getgram.ai/mcp/team", Command: ""}, want: false},
		{name: "unlisted ccd name", serverName: "ccd_custom", provenance: noTransport, want: false},
		{name: "bare ccd prefix", serverName: "ccd_", provenance: noTransport, want: false},
		{name: "different case", serverName: "claude_browser", provenance: noTransport, want: false},
		{name: "third-party server without transport", serverName: "linear", provenance: noTransport, want: false},
		{name: "empty name", serverName: "", provenance: noTransport, want: false},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			require.Equal(t, tc.want, shadowmcp.IsAgentBuiltinServer(tc.serverName, tc.provenance))
		})
	}
}
