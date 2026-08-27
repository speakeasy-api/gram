package plugins

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestPublicPlatformMCPFiles(t *testing.T) {
	t.Parallel()

	files, err := PublicPlatformMCPFiles("https://app.getgram.ai", "17")
	require.NoError(t, err)

	for _, expected := range []string{
		".claude-plugin/marketplace.json",
		".cursor-plugin/marketplace.json",
		".agents/plugins/marketplace.json",
		"README.md",
		"LICENSE",
		"speakeasy/.mcp.json",
		"speakeasy/.claude-plugin/plugin.json",
		"cursor-plugins/speakeasy-cursor/.cursor-plugin/plugin.json",
		"speakeasy-codex/.codex-plugin/plugin.json",
	} {
		require.Contains(t, files, expected)
	}

	var claude marketplaceManifest
	require.NoError(t, json.Unmarshal(files[".claude-plugin/marketplace.json"], &claude))
	require.Equal(t, PublicMarketplaceName, claude.Name)
	require.Len(t, claude.Plugins, 1, "the public marketplace advertises only the first-party package")
	require.Equal(t, platformMCPPluginName, claude.Plugins[0].Name)

	var codex codexMarketplaceManifest
	require.NoError(t, json.Unmarshal(files[".agents/plugins/marketplace.json"], &codex))
	require.Equal(t, PublicMarketplaceName, codex.Name)
	require.Len(t, codex.Plugins, 1)

	require.Contains(t, string(files["LICENSE"]), "MIT License")
	require.Contains(t, string(files["LICENSE"]), "Speakeasy Development, Inc.")

	var meta claudePluginMeta
	require.NoError(t, json.Unmarshal(files["speakeasy/.claude-plugin/plugin.json"], &meta))
	require.Equal(t, "0."+platformMCPGeneratorVersion+".17", meta.Version, "the CI counter must reach the manifest so installs refresh")
}

// The public repository is world-readable, so nothing org-derived or secret may
// reach it. Rendering with credentials set on the config must not change the
// output, and no rendered byte may carry a key or an organization identity.
func TestPublicPlatformMCPFilesCarriesNoSecrets(t *testing.T) {
	t.Parallel()

	files, err := PublicPlatformMCPFiles("https://app.getgram.ai", "17")
	require.NoError(t, err)

	for path, content := range files {
		body := string(content)
		for _, forbidden := range []string{"gram_", "GRAM_API_KEY", "Gram-Project", "Authorization"} {
			require.NotContains(t, body, forbidden, "%s must not carry credentials", path)
		}
	}
}

// The published .mcp.json is what every installed client dials, so a plaintext
// deployment URL would reach every install at once.
func TestPublicPlatformMCPFilesRejectsInsecureServerURL(t *testing.T) {
	t.Parallel()

	_, err := PublicPlatformMCPFiles("http://app.getgram.ai", "17")
	require.Error(t, err)
	require.Contains(t, err.Error(), "must be https")

	// url.Parse normalizes the scheme, so an uppercase one is still secure.
	files, err := PublicPlatformMCPFiles("HTTPS://app.getgram.ai", "17")
	require.NoError(t, err)
	require.Contains(t, string(files["speakeasy/.mcp.json"]), "app.getgram.ai/platform-mcp")
}

func TestPublicPlatformMCPFilesRequiresServerURL(t *testing.T) {
	t.Parallel()

	_, err := PublicPlatformMCPFiles("  ", "17")
	require.Error(t, err)

	// https, so it clears the scheme guard above and reaches the shape
	// validation the shared generator applies.
	_, err = PublicPlatformMCPFiles("https:///missing-host", "17")
	require.Error(t, err)
	require.Contains(t, err.Error(), "invalid Platform MCP server URL")
}
