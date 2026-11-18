package shadowmcp_test

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/shadowmcp"
)

func TestClassifyServer(t *testing.T) {
	t.Parallel()

	const connector = "1c55c87d-ac7c-4dfc-95f1-69e84cb5f92c"
	tests := []struct {
		name  string
		facts shadowmcp.ServerFacts
		want  shadowmcp.Classification
	}{
		{
			name:  "known URL is judged by the URL",
			facts: shadowmcp.ServerFacts{Name: "claude-in-chrome", URL: "https://mcp.example.test/mcp"},
			want:  shadowmcp.Classification{Kind: shadowmcp.ServerKindURL},
		},
		{
			name:  "connector UUID with a known URL is judged by the URL",
			facts: shadowmcp.ServerFacts{Name: connector, URL: "https://mcp.example.test/mcp"},
			want:  shadowmcp.Classification{Kind: shadowmcp.ServerKindURL},
		},
		{
			name:  "connector UUID without a URL",
			facts: shadowmcp.ServerFacts{Name: connector, InventoryLoaded: true},
			want:  shadowmcp.Classification{Kind: shadowmcp.ServerKindConnector},
		},
		{
			name:  "connector UUID without any inventory",
			facts: shadowmcp.ServerFacts{Name: connector},
			want:  shadowmcp.Classification{Kind: shadowmcp.ServerKindConnector},
		},
		{
			name:  "user launch command",
			facts: shadowmcp.ServerFacts{Name: "claude-in-chrome", Command: "npx some-mcp"},
			want:  shadowmcp.Classification{Kind: shadowmcp.ServerKindCommand},
		},
		{
			name:  "vendor bundle launch command",
			facts: shadowmcp.ServerFacts{Name: "computer_use", Command: "/Applications/ChatGPT.app/Contents/Resources/cua_node/bin/node_repl"},
			want:  shadowmcp.Classification{Kind: shadowmcp.ServerKindBuiltin, Evidence: shadowmcp.BuiltinEvidenceVendorBundle},
		},
		{
			name:  "listed in the inventory without transport",
			facts: shadowmcp.ServerFacts{Name: "linear", Installed: true, InventoryLoaded: true},
			want:  shadowmcp.Classification{Kind: shadowmcp.ServerKindInstalled},
		},
		{
			name:  "listed built-in name is still installed",
			facts: shadowmcp.ServerFacts{Name: "claude-in-chrome", Installed: true, InventoryLoaded: true},
			want:  shadowmcp.Classification{Kind: shadowmcp.ServerKindInstalled},
		},
		{
			name:  "cursor plugin identifier",
			facts: shadowmcp.ServerFacts{Name: "Linear", CursorServerIdentifier: "plugin-acme-Linear"},
			want:  shadowmcp.Classification{Kind: shadowmcp.ServerKindInstalled},
		},
		{
			name:  "cursor user identifier",
			facts: shadowmcp.ServerFacts{Name: "extension-Tracker", CursorServerIdentifier: "user-vendor.ext-Tracker"},
			want:  shadowmcp.Classification{Kind: shadowmcp.ServerKindInstalled},
		},
		{
			name:  "cursor built-in identifier",
			facts: shadowmcp.ServerFacts{Name: "cursor-ide-browser", CursorServerIdentifier: "cursor-ide-browser"},
			want:  shadowmcp.Classification{Kind: shadowmcp.ServerKindBuiltin, Evidence: shadowmcp.BuiltinEvidenceCursorBuiltin},
		},
		{
			name:  "known name not in the inventory",
			facts: shadowmcp.ServerFacts{Name: "ccd_turn", InventoryLoaded: true},
			want:  shadowmcp.Classification{Kind: shadowmcp.ServerKindBuiltin, Evidence: shadowmcp.BuiltinEvidenceKnownName},
		},
		{
			name:  "known name without any inventory",
			facts: shadowmcp.ServerFacts{Name: "claude-in-chrome"},
			want:  shadowmcp.Classification{Kind: shadowmcp.ServerKindBuiltin, Evidence: shadowmcp.BuiltinEvidenceKnownName},
		},
		{
			name:  "unlisted name not in the inventory",
			facts: shadowmcp.ServerFacts{Name: "ccd_new_tool", InventoryLoaded: true},
			want:  shadowmcp.Classification{Kind: shadowmcp.ServerKindBuiltin, Evidence: shadowmcp.BuiltinEvidenceNotInInventory},
		},
		{
			name:  "unlisted name without any inventory",
			facts: shadowmcp.ServerFacts{Name: "ccd_new_tool"},
			want:  shadowmcp.Classification{Kind: shadowmcp.ServerKindBuiltin, Evidence: shadowmcp.BuiltinEvidenceUnverified},
		},
		{
			name:  "claude.ai connector prefix without an inventory",
			facts: shadowmcp.ServerFacts{Name: "claude_ai_Linear"},
			want:  shadowmcp.Classification{Kind: shadowmcp.ServerKindInstalled},
		},
		{
			name:  "plugin server prefix missing from the inventory",
			facts: shadowmcp.ServerFacts{Name: "plugin_slack_slack", InventoryLoaded: true},
			want:  shadowmcp.Classification{Kind: shadowmcp.ServerKindInstalled},
		},
		{
			name:  "claude.ai connector prefix with a resolved URL is judged by the URL",
			facts: shadowmcp.ServerFacts{Name: "claude_ai_Linear", URL: "https://mcp.example.test/mcp", InventoryLoaded: true},
			want:  shadowmcp.Classification{Kind: shadowmcp.ServerKindURL},
		},
		{
			name:  "no server name",
			facts: shadowmcp.ServerFacts{Name: "  ", InventoryLoaded: true},
			want:  shadowmcp.Classification{Kind: shadowmcp.ServerKindUnidentified},
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got := shadowmcp.ClassifyServer(tc.facts)
			require.Equal(t, tc.want, got)
			require.Equal(t, tc.want.Kind == shadowmcp.ServerKindBuiltin, got.IsBuiltin())
		})
	}
}

func TestIsKnownAgentBuiltinServerName(t *testing.T) {
	t.Parallel()

	for _, name := range []string{
		"claude-in-chrome", "Claude_Browser", "Claude_Code_iOS_Simulator", "computer-use",
		"ccd_turn", "ccd_session", "ccd_session_mgmt", "ccd_pr", "ccd_view", "ccd_window",
		"ccd_sidebar", "ccd_settings", "ccd_host", "ccd_directory", "ccd_connectors",
	} {
		require.True(t, shadowmcp.IsKnownAgentBuiltinServerName(name), name)
	}
	for _, name := range []string{"", "Claude-In-Chrome", "ccd_custom", "linear"} {
		require.False(t, shadowmcp.IsKnownAgentBuiltinServerName(name), name)
	}
}

func TestParseBuiltinEvidence(t *testing.T) {
	t.Parallel()

	for _, evidence := range []shadowmcp.BuiltinEvidence{
		shadowmcp.BuiltinEvidenceVendorBundle,
		shadowmcp.BuiltinEvidenceCursorBuiltin,
		shadowmcp.BuiltinEvidenceKnownName,
		shadowmcp.BuiltinEvidenceNotInInventory,
		shadowmcp.BuiltinEvidenceUnverified,
	} {
		got, ok := shadowmcp.ParseBuiltinEvidence(string(evidence))
		require.True(t, ok, evidence)
		require.Equal(t, evidence, got)
	}
	for _, value := range []string{"", "claude-in-chrome", "builtin"} {
		_, ok := shadowmcp.ParseBuiltinEvidence(value)
		require.False(t, ok, value)
	}
}

func TestIsConnectorUUID(t *testing.T) {
	t.Parallel()

	require.True(t, shadowmcp.IsConnectorUUID("1c55c87d-ac7c-4dfc-95f1-69e84cb5f92c"))
	require.True(t, shadowmcp.IsConnectorUUID("1C55C87D-AC7C-4DFC-95F1-69E84CB5F92C"))
	require.False(t, shadowmcp.IsConnectorUUID("1c55c87dac7c4dfc95f169e84cb5f92c"))
	require.False(t, shadowmcp.IsConnectorUUID("claude_ai_1c55c87d-ac7c-4dfc-95f1-69e84cb5f92c"))
	require.False(t, shadowmcp.IsConnectorUUID("claude-in-chrome"))
}

func TestCursorServerIdentifiers(t *testing.T) {
	t.Parallel()

	require.True(t, shadowmcp.IsCursorBuiltinIdentifier("cursor-ide-browser"))
	require.False(t, shadowmcp.IsCursorBuiltinIdentifier("cursor-"))
	require.True(t, shadowmcp.IsCursorUserInstalledIdentifier("plugin-acme-Linear"))
	require.True(t, shadowmcp.IsCursorUserInstalledIdentifier("user-vendor.ext-Tracker"))
	require.False(t, shadowmcp.IsCursorUserInstalledIdentifier("user-"))
	require.False(t, shadowmcp.IsCursorServerIdentifier("Linear"))
}

func TestHasInstalledServerNamePrefix(t *testing.T) {
	t.Parallel()

	for _, name := range []string{"claude_ai_Linear", "claude_ai_Slack_Remote", "plugin_slack_slack", "plugin_kitchen-sink_Linear"} {
		require.True(t, shadowmcp.HasInstalledServerNamePrefix(name), name)
	}

	// Desktop and CLI built-ins never carry either prefix, so none of them can
	// be mistaken for an installed server.
	builtins := []string{
		"terminal", "visualize", "mcp-registry", "scheduled-tasks",
		"ccd_turn", "ccd_session", "ccd_session_mgmt", "ccd_pr", "ccd_view", "ccd_window",
		"ccd_sidebar", "ccd_settings", "ccd_host", "ccd_directory", "ccd_connectors", "ccd_new_tool",
		"Claude_Browser", "Claude_Code_iOS_Simulator", "Claude_Preview",
		"claude-in-chrome", "computer-use",
	}
	for _, name := range builtins {
		require.False(t, shadowmcp.HasInstalledServerNamePrefix(name), name)
		require.True(t, shadowmcp.ClassifyServer(shadowmcp.ServerFacts{Name: name}).IsBuiltin(), name)
		require.True(t, shadowmcp.ClassifyServer(shadowmcp.ServerFacts{Name: name, InventoryLoaded: true}).IsBuiltin(), name)
	}

	for _, name := range []string{"", "claude_ai_", "plugin_", "Claude_AI_Linear", "Plugin_slack_slack", "claude_ai", "plugins"} {
		require.False(t, shadowmcp.HasInstalledServerNamePrefix(name), name)
	}
}
