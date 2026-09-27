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
		{VendorKey: "anthropic", URL: "https://claude.ai/oauth/claude-code-client-metadata", DisplayName: "Anthropic (Claude Code)", Enabled: true},
		{VendorKey: "anthropic", URL: "https://claude.ai/oauth/mcp-oauth-client-metadata", DisplayName: "Anthropic (Claude)", Enabled: true},
		{VendorKey: "microsoft", URL: "https://vscode.dev/oauth/client-metadata.json", DisplayName: "Visual Studio Code", Enabled: true},
		{VendorKey: "microsoft", URL: "https://insiders.vscode.dev/oauth/client-metadata.json", DisplayName: "Visual Studio Code (Insiders)", Enabled: true},
		{VendorKey: "zed", URL: "https://zed.dev/oauth/client-metadata.json", DisplayName: "Zed", Enabled: true},
		{VendorKey: "block", URL: "https://goose-docs.ai/oauth/client-metadata.json", DisplayName: "Goose", Enabled: true},
		{VendorKey: "openai", URL: "https://chatgpt.com/oauth/*/client.json", DisplayName: "ChatGPT (connectors)", Enabled: true},
		{VendorKey: "openai", URL: "https://chatgpt.com/oauth/client.json", DisplayName: "ChatGPT", Enabled: true},
		{VendorKey: "openai", URL: "https://chatgpt.com/oauth/codex/*/client.json", DisplayName: "Codex CLI", Enabled: true},
		{VendorKey: "openai", URL: "https://chatgpt.com/oauth/codex/client.json", DisplayName: "Codex CLI (stable document)", Enabled: true},
		{VendorKey: "notion", URL: "https://www.notion.so/oauth/mcp-client-metadata.json", DisplayName: "Notion", Enabled: true},
		{VendorKey: "notion", URL: "https://app.notion.com/oauth/mcp-client-metadata.json", DisplayName: "Notion (app.notion.com)", Enabled: true},
		{VendorKey: "mcpjam", URL: "https://www.mcpjam.com/.well-known/oauth/client-metadata.json", DisplayName: "MCPJam Inspector", Enabled: true},
		{VendorKey: "factory", URL: "https://api.factory.ai/mcp/oauth-client", DisplayName: "Factory Droid", Enabled: true},
		{VendorKey: "stacklok", URL: "https://toolhive.dev/oauth/client-metadata.json", DisplayName: "ToolHive", Enabled: true},
		{VendorKey: "nousresearch", URL: "https://nousresearch.github.io/hermes-agent/docs/oauth/client-metadata.json", DisplayName: "Hermes Agent", Enabled: true},
		{VendorKey: "skydive", URL: "https://www.skydive.com/api/v1/external-oauth/client-metadata", DisplayName: "Skydive", Enabled: true},
		{VendorKey: "opencode", URL: "https://opencode.ai/oauth/opencode/client.json", DisplayName: "opencode", Enabled: true},
		{VendorKey: "github", URL: "https://github.com/copilot/cli/client-metadata.json", DisplayName: "GitHub Copilot CLI", Enabled: true},
	}

	require.Equal(t, expected, Catalog())
}
