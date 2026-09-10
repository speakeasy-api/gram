package shadowmcp_test

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/shadowmcp"
)

func TestToolNamespaceURL(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		in   string
		want string
	}{
		{name: "it lower-cases and trims the server name", in: "  GitHub ", want: "mcp-tool://github"},
		{name: "it keeps underscores and hyphens", in: "platform_logs-prod", want: "mcp-tool://platform_logs-prod"},
		{name: "it rejects an empty name", in: "   ", want: ""},
		{name: "it rejects whitespace inside the name", in: "my server", want: ""},
		{name: "it rejects path separators", in: "a/b", want: ""},
		{name: "it rejects a port-like colon", in: "srv:8080", want: ""},
		{name: "it rejects userinfo", in: "user@srv", want: ""},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			require.Equal(t, tt.want, shadowmcp.ToolNamespaceURL(tt.in))
		})
	}
}

func TestIsToolNamespaceURL(t *testing.T) {
	t.Parallel()

	require.True(t, shadowmcp.IsToolNamespaceURL("mcp-tool://github"))
	require.True(t, shadowmcp.IsToolNamespaceURL("  MCP-TOOL://github "))
	require.False(t, shadowmcp.IsToolNamespaceURL("https://mcp.example.com/mcp"))
	require.False(t, shadowmcp.IsToolNamespaceURL("npx mcp-remote https://app.getgram.ai/mcp/x"))
	require.False(t, shadowmcp.IsToolNamespaceURL(""))
}

func TestToolNamespaceServer(t *testing.T) {
	t.Parallel()

	require.Equal(t, "github", shadowmcp.ToolNamespaceServer("mcp-tool://github"))
	require.Equal(t, "github", shadowmcp.ToolNamespaceServer("mcp-tool://GitHub"))
	require.Equal(t, "", shadowmcp.ToolNamespaceServer("https://github.com"))
	require.Equal(t, "", shadowmcp.ToolNamespaceServer(""))
}

// The identity must survive every URL-keyed inventory layer unchanged and must
// never read as Gram-hosted, otherwise name-grade rows would be filtered out
// as trusted before they reached the inventory.
func TestToolNamespaceURL_FlowsThroughInventoryLayers(t *testing.T) {
	t.Parallel()

	identity := shadowmcp.ToolNamespaceURL("GitHub")
	require.Equal(t, "mcp-tool://github", identity)

	inv, ok := shadowmcp.CanonicalizeInventoryURL(identity)
	require.True(t, ok)
	require.Equal(t, "mcp-tool://github", inv.CanonicalURL)
	require.Equal(t, "github", inv.URLHost)

	require.False(t, shadowmcp.IsGramHostedMCPURL(identity))
	require.False(t, shadowmcp.IsGramHostedMCPURL(identity, "mcp.customer.example"))

	slug := shadowmcp.ServerSlug(inv.CanonicalURL)
	require.Contains(t, slug, "mcp-tool-github-")
	require.Len(t, slug, len("mcp-tool-github-")+8)
}
