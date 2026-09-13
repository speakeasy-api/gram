package admission

import (
	"testing"

	"github.com/stretchr/testify/require"
)

// TestCatalogMatchesTheCatalogAsShipped pins the admission catalog against the
// list it carried before it was derived from the aivendors registry.
//
// Order is part of the contract, not decoration: wildcard entries are matched
// in order, so a reordering can change which entry a client_id is attributed
// to. A change here should be a deliberate catalog edit — adding a vendor, or
// pulling one — never a side effect of restructuring the registry.
func TestCatalogMatchesTheCatalogAsShipped(t *testing.T) {
	t.Parallel()

	expected := []Preset{
		{VendorKey: "anthropic", URL: "https://claude.ai/oauth/claude-code-client-metadata", DisplayName: "Anthropic (Claude Code)", DisplayOnly: false, Enabled: true},
		{VendorKey: "anthropic", URL: "https://claude.ai/oauth/mcp-oauth-client-metadata", DisplayName: "Anthropic (Claude)", DisplayOnly: false, Enabled: true},
		{VendorKey: "microsoft", URL: "https://vscode.dev/oauth/client-metadata.json", DisplayName: "Visual Studio Code", DisplayOnly: false, Enabled: true},
		{VendorKey: "microsoft", URL: "https://insiders.vscode.dev/oauth/client-metadata.json", DisplayName: "Visual Studio Code (Insiders)", DisplayOnly: false, Enabled: true},
		{VendorKey: "zed", URL: "https://zed.dev/oauth/client-metadata.json", DisplayName: "Zed", DisplayOnly: false, Enabled: true},
		{VendorKey: "block", URL: "https://goose-docs.ai/oauth/client-metadata.json", DisplayName: "Goose", DisplayOnly: false, Enabled: true},
		{VendorKey: "openai", URL: "https://chatgpt.com/oauth/*/client.json", DisplayName: "ChatGPT (connectors)", DisplayOnly: false, Enabled: true},
		{VendorKey: "openai", URL: "https://chatgpt.com/oauth/client.json", DisplayName: "ChatGPT", DisplayOnly: false, Enabled: true},
		{VendorKey: "openai", URL: "https://chatgpt.com/oauth/codex/*/client.json", DisplayName: "Codex CLI", DisplayOnly: false, Enabled: true},
		{VendorKey: "openai", URL: "https://chatgpt.com/oauth/codex/client.json", DisplayName: "Codex CLI (stable document)", DisplayOnly: true, Enabled: true},
		{VendorKey: "notion", URL: "https://www.notion.so/oauth/mcp-client-metadata.json", DisplayName: "Notion", DisplayOnly: false, Enabled: true},
		{VendorKey: "notion", URL: "https://app.notion.com/oauth/mcp-client-metadata.json", DisplayName: "Notion (app.notion.com)", DisplayOnly: false, Enabled: true},
		{VendorKey: "mcpjam", URL: "https://www.mcpjam.com/.well-known/oauth/client-metadata.json", DisplayName: "MCPJam Inspector", DisplayOnly: false, Enabled: true},
		{VendorKey: "factory", URL: "https://api.factory.ai/mcp/oauth-client", DisplayName: "Factory Droid", DisplayOnly: false, Enabled: true},
		{VendorKey: "stacklok", URL: "https://toolhive.dev/oauth/client-metadata.json", DisplayName: "ToolHive", DisplayOnly: false, Enabled: true},
		{VendorKey: "nousresearch", URL: "https://nousresearch.github.io/hermes-agent/docs/oauth/client-metadata.json", DisplayName: "Hermes Agent", DisplayOnly: false, Enabled: true},
		{VendorKey: "skydive", URL: "https://www.skydive.com/api/v1/external-oauth/client-metadata", DisplayName: "Skydive", DisplayOnly: false, Enabled: true},
		{VendorKey: "opencode", URL: "https://opencode.ai/oauth/opencode/client.json", DisplayName: "opencode", DisplayOnly: false, Enabled: true},
	}

	require.Equal(t, expected, Catalog())
}
