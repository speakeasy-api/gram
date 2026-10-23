package admission

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/aivendors"
)

// TestCatalogMatchesTheCatalogAsShipped pins the admission catalog against the
// list it carried before it was derived from the aivendors registry.
//
// Document identity and attribution are pinned; database ordering is not part
// of admission. Pattern specificity is tested independently.
func TestCatalogMatchesTheCatalogAsShipped(t *testing.T) {
	t.Parallel()

	expected := []Preset{
		{ProductID: "claude-code", Pattern: false, VendorKey: "anthropic", URL: "https://claude.ai/oauth/claude-code-client-metadata", DisplayName: "Anthropic (Claude Code)", Enabled: true},
		{ProductID: "claude", Pattern: false, VendorKey: "anthropic", URL: "https://claude.ai/oauth/mcp-oauth-client-metadata", DisplayName: "Anthropic (Claude)", Enabled: true},
		{ProductID: "vscode", Pattern: false, VendorKey: "microsoft", URL: "https://vscode.dev/oauth/client-metadata.json", DisplayName: "Visual Studio Code", Enabled: true},
		{ProductID: "vscode", Pattern: false, VendorKey: "microsoft", URL: "https://insiders.vscode.dev/oauth/client-metadata.json", DisplayName: "Visual Studio Code (Insiders)", Enabled: true},
		{ProductID: "zed", Pattern: false, VendorKey: "zed", URL: "https://zed.dev/oauth/client-metadata.json", DisplayName: "Zed", Enabled: true},
		{ProductID: "goose", Pattern: false, VendorKey: "block", URL: "https://goose-docs.ai/oauth/client-metadata.json", DisplayName: "Goose", Enabled: true},
		{ProductID: "chatgpt", Pattern: true, VendorKey: "openai", URL: "https://chatgpt.com/oauth/*/client.json", DisplayName: "ChatGPT (connectors)", Enabled: true},
		{ProductID: "chatgpt", Pattern: false, VendorKey: "openai", URL: "https://chatgpt.com/oauth/client.json", DisplayName: "ChatGPT", Enabled: true},
		{ProductID: "codex", Pattern: true, VendorKey: "openai", URL: "https://chatgpt.com/oauth/codex/*/client.json", DisplayName: "Codex CLI", Enabled: true},
		{ProductID: "codex", Pattern: false, VendorKey: "openai", URL: "https://chatgpt.com/oauth/codex/client.json", DisplayName: "Codex CLI (stable document)", Enabled: true},
		{ProductID: "notion", Pattern: false, VendorKey: "notion", URL: "https://www.notion.so/oauth/mcp-client-metadata.json", DisplayName: "Notion", Enabled: true},
		{ProductID: "notion", Pattern: false, VendorKey: "notion", URL: "https://app.notion.com/oauth/mcp-client-metadata.json", DisplayName: "Notion (app.notion.com)", Enabled: true},
		{ProductID: "mcpjam", Pattern: false, VendorKey: "mcpjam", URL: "https://www.mcpjam.com/.well-known/oauth/client-metadata.json", DisplayName: "MCPJam Inspector", Enabled: true},
		{ProductID: "factory-droid", Pattern: false, VendorKey: "factory", URL: "https://api.factory.ai/mcp/oauth-client", DisplayName: "Factory Droid", Enabled: true},
		{ProductID: "toolhive", Pattern: false, VendorKey: "stacklok", URL: "https://toolhive.dev/oauth/client-metadata.json", DisplayName: "ToolHive", Enabled: true},
		{ProductID: "hermes-agent", Pattern: false, VendorKey: "nousresearch", URL: "https://nousresearch.github.io/hermes-agent/docs/oauth/client-metadata.json", DisplayName: "Hermes Agent", Enabled: true},
		{ProductID: "skydive", Pattern: false, VendorKey: "skydive", URL: "https://www.skydive.com/api/v1/external-oauth/client-metadata", DisplayName: "Skydive", Enabled: true},
		{ProductID: "opencode", Pattern: false, VendorKey: "opencode", URL: "https://opencode.ai/oauth/opencode/client.json", DisplayName: "opencode", Enabled: true},
		{ProductID: "github-copilot-cli", Pattern: false, VendorKey: "github", URL: "https://github.com/copilot/cli/client-metadata.json", DisplayName: "GitHub Copilot CLI", Enabled: true},
		{ProductID: "conductor", Pattern: false, VendorKey: "conductor", URL: "https://api.conductor.build/oauth/client-metadata.json", DisplayName: "Conductor", Enabled: true},
		{ProductID: "vercel-connect", Pattern: true, VendorKey: "vercel", URL: "https://connect.vercel.com/connectors/*", DisplayName: "Vercel Connect (connectors)", Enabled: true},
		{ProductID: "linear", Pattern: false, VendorKey: "linear", URL: "https://linear.app/.well-known/oauth-client-metadata/mcp.json", DisplayName: "Linear", Enabled: true},
		{ProductID: "lovable", Pattern: false, VendorKey: "lovable", URL: "https://api.lovable.dev/.well-known/oauth-client", DisplayName: "Lovable", Enabled: true},
		{ProductID: "antigravity", Pattern: false, VendorKey: "google", URL: "https://antigravity.google/oauth/client-metadata.json", DisplayName: "Google Antigravity", Enabled: true},
	}

	require.ElementsMatch(t, expected, aivendors.NewSnapshot(aivendors.Products()).Catalog())
}
