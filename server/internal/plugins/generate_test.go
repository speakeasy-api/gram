package plugins

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestGeneratePluginPackagesProducesExpectedFiles(t *testing.T) {
	t.Parallel()
	plugins := []PluginInfo{
		{
			Name:        "Engineering Tools",
			Slug:        "engineering-tools",
			Description: "MCP servers for the engineering team",
			Servers: []PluginServerInfo{
				{
					DisplayName: "crm-tools",
					Policy:      "required",
					MCPURL:      "https://app.getgram.ai/mcp/acme-abc12",
					UseGramAuth: true,
				},
				{
					DisplayName: "external-analytics",
					Policy:      "optional",
					MCPURL:      "https://analytics.example.com/mcp",
					UseGramAuth: false,
				},
			},
		},
	}

	cfg := GenerateConfig{
		OrgName:   "Acme Corp",
		OrgEmail:  "admin@acme.com",
		ServerURL: "https://app.getgram.ai",
	}

	files, err := GeneratePluginPackages(plugins, cfg)
	require.NoError(t, err)

	// Verify expected file paths exist.
	expectedPaths := []string{
		".claude-plugin/marketplace.json",
		"engineering-tools/.claude-plugin/plugin.json",
		"engineering-tools/.mcp.json",
		"engineering-tools-cursor/.cursor-plugin/plugin.json",
		"engineering-tools-cursor/mcp.json",
	}
	for _, p := range expectedPaths {
		_, ok := files[p]
		require.True(t, ok, "missing file: %s", p)
	}
}

func TestGenerateClaudeMCPConfigHasCorrectAuthHeaders(t *testing.T) {
	t.Parallel()
	plugins := []PluginInfo{
		{
			Name:        "Test",
			Slug:        "test",
			Description: "",
			Servers: []PluginServerInfo{
				{DisplayName: "gram-server", Policy: "", MCPURL: "https://app.getgram.ai/mcp/test", UseGramAuth: true},
				{DisplayName: "external", Policy: "", MCPURL: "https://ext.example.com", UseGramAuth: false},
			},
		},
	}

	files, err := GeneratePluginPackages(plugins, GenerateConfig{
		OrgName:   "Test Org",
		OrgEmail:  "test@test.com",
		ServerURL: "https://app.getgram.ai",
	})
	require.NoError(t, err)

	var mcpConfig claudeMCPConfig
	err = json.Unmarshal(files["test/.mcp.json"], &mcpConfig)
	require.NoError(t, err)

	// Gram-hosted server should have auth header.
	gramServer := mcpConfig.MCPServers["gram-server"]
	require.Equal(t, "Bearer ${user_config.GRAM_API_KEY}", gramServer.Headers["Authorization"])

	// External server should not have auth header.
	extServer := mcpConfig.MCPServers["external"]
	require.Empty(t, extServer.Headers)
}

func TestGenerateCursorMCPConfigUsesEnvSyntax(t *testing.T) {
	t.Parallel()
	plugins := []PluginInfo{
		{
			Name:        "Test",
			Slug:        "test",
			Description: "",
			Servers: []PluginServerInfo{
				{DisplayName: "gram-server", Policy: "", MCPURL: "https://app.getgram.ai/mcp/test", UseGramAuth: true},
			},
		},
	}

	files, err := GeneratePluginPackages(plugins, GenerateConfig{
		OrgName:   "Test Org",
		OrgEmail:  "test@test.com",
		ServerURL: "https://app.getgram.ai",
	})
	require.NoError(t, err)

	var mcpConfig cursorMCPConfig
	err = json.Unmarshal(files["test-cursor/mcp.json"], &mcpConfig)
	require.NoError(t, err)

	gramServer := mcpConfig.MCPServers["gram-server"]
	require.Equal(t, "Bearer ${env:GRAM_API_KEY}", gramServer.Headers["Authorization"])
}

func TestGenerateMarketplaceManifest(t *testing.T) {
	t.Parallel()
	plugins := []PluginInfo{
		{Name: "A", Slug: "a", Description: "First plugin", Servers: nil},
		{Name: "B", Slug: "b", Description: "Second plugin", Servers: nil},
	}

	files, err := GeneratePluginPackages(plugins, GenerateConfig{
		OrgName:   "Acme",
		OrgEmail:  "admin@acme.com",
		ServerURL: "https://app.getgram.ai",
	})
	require.NoError(t, err)

	var manifest marketplaceManifest
	err = json.Unmarshal(files[".claude-plugin/marketplace.json"], &manifest)
	require.NoError(t, err)

	require.Equal(t, "Acme-gram", manifest.Name)
	require.Equal(t, "Acme", manifest.Owner.Name)
	require.Len(t, manifest.Plugins, 2)
	require.Equal(t, "./a", manifest.Plugins[0].Source)
	require.Equal(t, "./b", manifest.Plugins[1].Source)
}

func TestResolveServerMCPURL(t *testing.T) {
	t.Parallel()
	toolsetSlug := "acme-abc12"
	url, useAuth := ResolveServerMCPURL("https://app.getgram.ai", &toolsetSlug, nil, nil)
	require.Equal(t, "https://app.getgram.ai/mcp/acme-abc12", url)
	require.True(t, useAuth)

	extURL := "https://ext.example.com/mcp"
	url, useAuth = ResolveServerMCPURL("https://app.getgram.ai", nil, nil, &extURL)
	require.Equal(t, "https://ext.example.com/mcp", url)
	require.False(t, useAuth)
}
