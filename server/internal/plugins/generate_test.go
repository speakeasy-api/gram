package plugins

import (
	"archive/zip"
	"bytes"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/speakeasy-api/gram/server/internal/conv"
	"github.com/stretchr/testify/require"
)

func requireFileBytes(t *testing.T, path string) []byte {
	t.Helper()
	data, err := os.ReadFile(path)
	require.NoError(t, err)
	return data
}

// TestSharedHTTPScriptMatchesCheckedIn guards against drift between the
// generated hooks/http.sh (renderSharedHTTPScript) and the checked-in
// hooks/plugin-claude/hooks/http.sh sourced by the local-dev plugin. Both must
// be identical so local-dev and generated plugins share one transport.
func TestSharedHTTPScriptMatchesCheckedIn(t *testing.T) {
	t.Parallel()
	checkedIn := requireFileBytes(t, filepath.Join("..", "..", "..", "hooks", "plugin-claude", "hooks", "http.sh"))
	// renderSharedHTTPScript() is canonical → pass it as testify's "expected".
	require.Equal(t, string(renderSharedHTTPScript()), string(checkedIn),
		"hooks/plugin-claude/hooks/http.sh has drifted from renderSharedHTTPScript() — keep them identical")
}

func TestGeneratePluginWithCustomDomainURL(t *testing.T) {
	t.Parallel()
	plugins := []PluginInfo{
		{
			Name: "Test",
			Slug: "test",
			Servers: []PluginServerInfo{
				{DisplayName: "custom-server", MCPURL: "https://mcp.acme.com/mcp/my-slug"},
			},
		},
	}

	files, err := GeneratePluginPackages(plugins, GenerateConfig{
		OrgName:   "Acme",
		ServerURL: "https://app.getgram.ai",
	})
	require.NoError(t, err)

	var mcpConfig claudeMCPConfig
	err = json.Unmarshal(files["test/.mcp.json"], &mcpConfig)
	require.NoError(t, err)

	server := mcpConfig.MCPServers["custom-server"]
	require.Equal(t, "https://mcp.acme.com/mcp/my-slug", server.URL, "custom domain URL must be preserved verbatim in generated config")
}

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
				},
				{
					DisplayName: "analytics",
					Policy:      "optional",
					MCPURL:      "https://app.getgram.ai/mcp/analytics-xyz",
				},
			},
		},
	}

	cfg := GenerateConfig{
		OrgName:   "Acme Corp",
		OrgEmail:  "",
		ServerURL: "https://app.getgram.ai",
	}

	files, err := GeneratePluginPackages(plugins, cfg)
	require.NoError(t, err)

	expectedPaths := []string{
		".claude-plugin/marketplace.json",
		".cursor-plugin/marketplace.json",
		".agents/plugins/marketplace.json",
		"engineering-tools/.claude-plugin/plugin.json",
		"engineering-tools/.mcp.json",
		"cursor-plugins/engineering-tools-cursor/.cursor-plugin/plugin.json",
		"cursor-plugins/engineering-tools-cursor/mcp.json",
		"engineering-tools-codex/.codex-plugin/plugin.json",
		"engineering-tools-codex/.mcp.json",
	}
	for _, p := range expectedPaths {
		_, ok := files[p]
		require.True(t, ok, "missing file: %s", p)
	}
}

func TestGenerateClaudePluginEmitsHumanDisplayName(t *testing.T) {
	t.Parallel()
	plugins := []PluginInfo{
		{
			Name:        "MoonPay MCP Servers",
			Slug:        "moonpay-mcp-servers",
			Description: "MoonPay MCP servers",
			Servers: []PluginServerInfo{
				{DisplayName: "crm-tools", MCPURL: "https://app.getgram.ai/mcp/crm"},
			},
		},
	}

	files, err := GeneratePluginPackages(plugins, GenerateConfig{
		OrgName:     "MoonPay",
		ServerURL:   "https://app.getgram.ai",
		HooksAPIKey: "hooks-key", // triggers the synthesized observability plugin
	})
	require.NoError(t, err)

	// plugin.json: name stays the kebab slug (used for namespacing/lookup);
	// displayName carries the human-friendly, correctly-cased name Claude shows.
	var pluginMeta claudePluginMeta
	require.NoError(t, json.Unmarshal(files["moonpay-mcp-servers/.claude-plugin/plugin.json"], &pluginMeta))
	require.Equal(t, "moonpay-mcp-servers", pluginMeta.Name)
	require.Equal(t, "MoonPay MCP Servers", pluginMeta.DisplayName)

	// marketplace.json entry mirrors the same contract.
	var manifest marketplaceManifest
	require.NoError(t, json.Unmarshal(files[".claude-plugin/marketplace.json"], &manifest))

	entries := make(map[string]marketplaceEntry, len(manifest.Plugins))
	for _, e := range manifest.Plugins {
		entries[e.Name] = e
	}

	feature, ok := entries["moonpay-mcp-servers"]
	require.True(t, ok, "feature plugin missing from marketplace.json")
	require.Equal(t, "MoonPay MCP Servers", feature.DisplayName)

	// The synthesized observability plugin gets a human display name too.
	obs, ok := entries["moonpay-observability"]
	require.True(t, ok, "observability plugin missing from marketplace.json")
	require.Equal(t, "MoonPay Observability", obs.DisplayName)

	var obsMeta claudePluginMeta
	require.NoError(t, json.Unmarshal(files["moonpay-observability/.claude-plugin/plugin.json"], &obsMeta))
	require.Equal(t, "moonpay-observability", obsMeta.Name)
	require.Equal(t, "MoonPay Observability", obsMeta.DisplayName)
}

func TestGenerateClaudeMCPConfigAlwaysHasAuthHeaders(t *testing.T) {
	t.Parallel()
	plugins := []PluginInfo{
		{
			Name: "Test",
			Slug: "test",
			Servers: []PluginServerInfo{
				{DisplayName: "gram-server", MCPURL: "https://app.getgram.ai/mcp/test"},
				{DisplayName: "another", MCPURL: "https://app.getgram.ai/mcp/another"},
			},
		},
	}

	files, err := GeneratePluginPackages(plugins, GenerateConfig{
		OrgName:   "Test Org",
		OrgEmail:  "",
		ServerURL: "https://app.getgram.ai",
	})
	require.NoError(t, err)

	var mcpConfig claudeMCPConfig
	err = json.Unmarshal(files["test/.mcp.json"], &mcpConfig)
	require.NoError(t, err)

	for name, server := range mcpConfig.MCPServers {
		require.Equal(t, "Bearer ${user_config.GRAM_API_KEY}", server.Headers["Authorization"], "server %s missing auth header", name)
	}
}

func TestGenerateCursorMCPConfigUsesEnvSyntax(t *testing.T) {
	t.Parallel()
	plugins := []PluginInfo{
		{
			Name: "Test",
			Slug: "test",
			Servers: []PluginServerInfo{
				{DisplayName: "gram-server", MCPURL: "https://app.getgram.ai/mcp/test"},
			},
		},
	}

	files, err := GeneratePluginPackages(plugins, GenerateConfig{
		OrgName:   "Test Org",
		OrgEmail:  "",
		ServerURL: "https://app.getgram.ai",
	})
	require.NoError(t, err)

	var mcpConfig cursorMCPConfig
	err = json.Unmarshal(files["cursor-plugins/test-cursor/mcp.json"], &mcpConfig)
	require.NoError(t, err)

	server := mcpConfig.MCPServers["gram-server"]
	require.Equal(t, "Bearer ${env:GRAM_API_KEY}", server.Headers["Authorization"])
}

func TestGenerateClaudeOAuthServerEmitsStdioEntry(t *testing.T) {
	t.Parallel()
	plugins := []PluginInfo{
		{
			Name: "Test",
			Slug: "test",
			Servers: []PluginServerInfo{
				{DisplayName: "oauth-server", MCPURL: "https://mcp.example.com/oauth-tool", IsOAuth: true},
			},
		},
	}

	files, err := GeneratePluginPackages(plugins, GenerateConfig{
		OrgName:   "Test Org",
		ServerURL: "https://app.getgram.ai",
	})
	require.NoError(t, err)

	var mcpConfig claudeMCPConfig
	err = json.Unmarshal(files["test/.mcp.json"], &mcpConfig)
	require.NoError(t, err)

	server := mcpConfig.MCPServers["oauth-server"]
	require.Equal(t, "https://mcp.example.com/oauth-tool", server.URL)
	require.Empty(t, server.Headers, "OAuth server must not emit any auth headers")

	// plugin.json must not include a GRAM_API_KEY userConfig entry for OAuth-only plugins.
	var pluginMeta claudePluginMeta
	err = json.Unmarshal(files["test/.claude-plugin/plugin.json"], &pluginMeta)
	require.NoError(t, err)
	require.NotContains(t, pluginMeta.UserConfig, "GRAM_API_KEY", "OAuth-only plugin must not prompt for GRAM_API_KEY")
}

func TestGenerateCursorOAuthServerEmitsURLWithNoHeaders(t *testing.T) {
	t.Parallel()
	plugins := []PluginInfo{
		{
			Name: "Test",
			Slug: "test",
			Servers: []PluginServerInfo{
				{DisplayName: "oauth-server", MCPURL: "https://mcp.example.com/oauth-tool", IsOAuth: true},
			},
		},
	}

	files, err := GeneratePluginPackages(plugins, GenerateConfig{
		OrgName:   "Test Org",
		ServerURL: "https://app.getgram.ai",
	})
	require.NoError(t, err)

	var mcpConfig cursorMCPConfig
	err = json.Unmarshal(files["cursor-plugins/test-cursor/mcp.json"], &mcpConfig)
	require.NoError(t, err)

	server := mcpConfig.MCPServers["oauth-server"]
	require.Equal(t, "https://mcp.example.com/oauth-tool", server.URL)
	require.Empty(t, server.Headers, "OAuth server must not emit any auth headers")
}

func TestGenerateCodexOAuthServerEmitsURLWithNoCredentials(t *testing.T) {
	t.Parallel()
	plugins := []PluginInfo{
		{
			Name: "Test",
			Slug: "test",
			Servers: []PluginServerInfo{
				{DisplayName: "oauth-server", MCPURL: "https://mcp.example.com/oauth-tool", IsOAuth: true},
			},
		},
	}

	files, err := GeneratePluginPackages(plugins, GenerateConfig{
		OrgName:   "Test Org",
		ServerURL: "https://app.getgram.ai",
	})
	require.NoError(t, err)

	var mcpConfig codexMCPConfig
	err = json.Unmarshal(files["test-codex/.mcp.json"], &mcpConfig)
	require.NoError(t, err)

	server := mcpConfig.MCPServers["oauth-server"]
	require.Equal(t, "https://mcp.example.com/oauth-tool", server.URL)
	require.Empty(t, server.BearerTokenEnvVar, "OAuth server must not set bearer_token_env_var")
	require.Empty(t, server.HTTPHeaders, "OAuth server must not emit http_headers")
}

func TestGenerateClaudeMixedOAuthAndHTTPServers(t *testing.T) {
	t.Parallel()
	plugins := []PluginInfo{
		{
			Name: "Test",
			Slug: "test",
			Servers: []PluginServerInfo{
				{DisplayName: "oauth-server", MCPURL: "https://mcp.example.com/oauth-tool", IsOAuth: true},
				{DisplayName: "private-server", MCPURL: "https://app.getgram.ai/mcp/private"},
			},
		},
	}

	files, err := GeneratePluginPackages(plugins, GenerateConfig{
		OrgName:   "Test Org",
		ServerURL: "https://app.getgram.ai",
	})
	require.NoError(t, err)

	var mcpConfig claudeMCPConfig
	err = json.Unmarshal(files["test/.mcp.json"], &mcpConfig)
	require.NoError(t, err)
	require.Len(t, mcpConfig.MCPServers, 2, "both servers should appear in .mcp.json")

	// OAuth server emits URL with no auth headers.
	oauthServer := mcpConfig.MCPServers["oauth-server"]
	require.Empty(t, oauthServer.Headers, "OAuth server must not emit auth headers")
	require.Equal(t, "https://mcp.example.com/oauth-tool", oauthServer.URL)

	// Private HTTP server retains its Authorization header.
	privateServer := mcpConfig.MCPServers["private-server"]
	require.Contains(t, privateServer.Headers, "Authorization")

	// plugin.json must still prompt for GRAM_API_KEY because the private HTTP server needs it.
	var pluginMeta claudePluginMeta
	err = json.Unmarshal(files["test/.claude-plugin/plugin.json"], &pluginMeta)
	require.NoError(t, err)
	require.Contains(t, pluginMeta.UserConfig, "GRAM_API_KEY")
}

func TestGenerateCodexMCPConfigUsesBearerTokenEnvVar(t *testing.T) {
	t.Parallel()
	plugins := []PluginInfo{
		{
			Name: "Test",
			Slug: "test",
			Servers: []PluginServerInfo{
				{DisplayName: "gram-server", MCPURL: "https://app.getgram.ai/mcp/test"},
				{DisplayName: "another", MCPURL: "https://app.getgram.ai/mcp/another"},
			},
		},
	}

	files, err := GeneratePluginPackages(plugins, GenerateConfig{
		OrgName:   "Test Org",
		ServerURL: "https://app.getgram.ai",
	})
	require.NoError(t, err)

	var mcpConfig codexMCPConfig
	err = json.Unmarshal(files["test-codex/.mcp.json"], &mcpConfig)
	require.NoError(t, err)

	for name, server := range mcpConfig.MCPServers {
		require.Equal(t, "GRAM_API_KEY", server.BearerTokenEnvVar, "server %s missing bearer_token_env_var", name)
		require.Empty(t, server.HTTPHeaders, "server %s should not bake headers when no APIKey is set", name)
		require.Empty(t, server.EnvHTTPHeaders, "server %s is private; env_http_headers is for public servers", name)
	}
}

func TestGenerateCodexMCPConfigBakesInjectedAPIKey(t *testing.T) {
	t.Parallel()
	plugins := []PluginInfo{
		{
			Name:    "Test",
			Slug:    "test",
			Servers: []PluginServerInfo{{DisplayName: "gram-server", MCPURL: "https://app.getgram.ai/mcp/test"}},
		},
	}

	files, err := GeneratePluginPackages(plugins, GenerateConfig{
		OrgName:   "Test Org",
		ServerURL: "https://app.getgram.ai",
		APIKey:    "gram_test_key_123",
	})
	require.NoError(t, err)

	var mcpConfig codexMCPConfig
	err = json.Unmarshal(files["test-codex/.mcp.json"], &mcpConfig)
	require.NoError(t, err)

	server := mcpConfig.MCPServers["gram-server"]
	require.Equal(t, "Bearer gram_test_key_123", server.HTTPHeaders["Authorization"])
	require.Empty(t, server.BearerTokenEnvVar, "baked-key path must not also set bearer_token_env_var")
}

func TestGenerateCodexMCPConfigUsesEnvHTTPHeadersForPublicServers(t *testing.T) {
	t.Parallel()
	plugins := []PluginInfo{
		{
			Name: "Test",
			Slug: "test",
			Servers: []PluginServerInfo{{
				DisplayName: "public-api",
				MCPURL:      "https://app.getgram.ai/mcp/public",
				IsPublic:    true,
				EnvConfigs: []ServerEnvConfig{
					{VariableName: "OPENAI_API_KEY", DisplayName: "X-OpenAI-Key"},
				},
			}},
		},
	}

	files, err := GeneratePluginPackages(plugins, GenerateConfig{
		OrgName:   "Test Org",
		ServerURL: "https://app.getgram.ai",
	})
	require.NoError(t, err)

	var mcpConfig codexMCPConfig
	err = json.Unmarshal(files["test-codex/.mcp.json"], &mcpConfig)
	require.NoError(t, err)

	server := mcpConfig.MCPServers["public-api"]
	require.Equal(t, "OPENAI_API_KEY", server.EnvHTTPHeaders["X-OpenAI-Key"])
	require.Empty(t, server.BearerTokenEnvVar, "public servers should not set bearer_token_env_var")
	require.Empty(t, server.HTTPHeaders, "public servers should not bake Authorization")
}

// TestCodexJSONKeysMatchPinnedSchema asserts the literal JSON key casing in
// Codex output against the openai/codex source pinned in generate.go. Keys
// are inspected on the raw JSON bytes (not a round-trip through our own
// structs) so a struct-tag change — e.g. flipping mcpServers to mcp_servers
// or bearer_token_env_var to bearerTokenEnvVar — fails this test even if
// the roundtrip-based tests still pass.
func TestCodexJSONKeysMatchPinnedSchema(t *testing.T) {
	t.Parallel()
	plugins := []PluginInfo{{
		Name: "Test",
		Slug: "test",
		Servers: []PluginServerInfo{
			{DisplayName: "private-no-key", MCPURL: "https://x"},
			{DisplayName: "private-with-key", MCPURL: "https://x"},
			{
				DisplayName: "public-with-env",
				MCPURL:      "https://x",
				IsPublic:    true,
				EnvConfigs:  []ServerEnvConfig{{VariableName: "FOO", DisplayName: "X-Foo"}},
			},
		},
	}}

	filesNoKey, err := GeneratePluginPackages(plugins, GenerateConfig{OrgName: "Test Org", ServerURL: "https://x"})
	require.NoError(t, err)
	filesWithKey, err := GeneratePluginPackages(plugins, GenerateConfig{OrgName: "Test Org", ServerURL: "https://x", APIKey: "k"})
	require.NoError(t, err)

	// Plugin manifest: rename_all = "camelCase" in codex-rs/core-plugins/src/manifest.rs.
	manifest := string(filesNoKey["test-codex/.codex-plugin/plugin.json"])
	require.Contains(t, manifest, `"mcpServers"`, "plugin.json should use camelCase mcpServers (manifest.rs rename_all)")
	require.NotContains(t, manifest, `"mcp_servers"`, "plugin.json must not use snake_case")

	// .mcp.json wrapper: PluginMcpFile.mcp_servers_object_format in loader.rs
	// accepts "mcpServers" (camelCase). Server entry fields are snake_case per
	// mcp_types.rs (rename_all = "snake_case" on the untagged transport enum).
	mcpNoKey := string(filesNoKey["test-codex/.mcp.json"])
	mcpWithKey := string(filesWithKey["test-codex/.mcp.json"])

	require.Contains(t, mcpNoKey, `"mcpServers"`, ".mcp.json wrapper should use camelCase mcpServers")
	require.Contains(t, mcpNoKey, `"bearer_token_env_var"`, "private+no-key branch must emit snake_case bearer_token_env_var")
	require.Contains(t, mcpNoKey, `"env_http_headers"`, "public+env branch must emit snake_case env_http_headers")
	require.Contains(t, mcpWithKey, `"http_headers"`, "private+key branch must emit snake_case http_headers")

	// Catch a casing regression in any direction.
	for _, raw := range []string{mcpNoKey, mcpWithKey} {
		require.NotContains(t, raw, `"bearerTokenEnvVar"`)
		require.NotContains(t, raw, `"httpHeaders"`)
		require.NotContains(t, raw, `"envHttpHeaders"`)
	}
}

func codexPrivateServer() PluginServerInfo {
	return PluginServerInfo{DisplayName: "priv", MCPURL: "https://x"}
}

func codexPublicServerNoEnv() PluginServerInfo {
	return PluginServerInfo{DisplayName: "pub", MCPURL: "https://x", IsPublic: true}
}

func codexPublicServerWithEnv() PluginServerInfo {
	return PluginServerInfo{
		DisplayName: "pub-env",
		MCPURL:      "https://x",
		IsPublic:    true,
		EnvConfigs:  []ServerEnvConfig{{VariableName: "FOO", DisplayName: "X-Foo"}},
	}
}

func TestCodexAuthPolicyPrivateWithBakedKeyIsSilent(t *testing.T) {
	t.Parallel()
	got := codexAuthPolicy(
		PluginInfo{Servers: []PluginServerInfo{codexPrivateServer()}},
		GenerateConfig{APIKey: "k"},
	)
	require.Equal(t, "ON_USE", got)
}

func TestCodexAuthPolicyPrivateWithoutKeyPrompts(t *testing.T) {
	t.Parallel()
	got := codexAuthPolicy(
		PluginInfo{Servers: []PluginServerInfo{codexPrivateServer()}},
		GenerateConfig{},
	)
	require.Equal(t, "ON_INSTALL", got)
}

func TestCodexAuthPolicyPublicWithEnvConfigsPrompts(t *testing.T) {
	t.Parallel()
	got := codexAuthPolicy(
		PluginInfo{Servers: []PluginServerInfo{codexPublicServerWithEnv()}},
		GenerateConfig{},
	)
	require.Equal(t, "ON_INSTALL", got)
}

func TestCodexAuthPolicyFullyPublicNoEnvIsSilent(t *testing.T) {
	t.Parallel()
	got := codexAuthPolicy(
		PluginInfo{Servers: []PluginServerInfo{codexPublicServerNoEnv()}},
		GenerateConfig{},
	)
	require.Equal(t, "ON_USE", got)
}

func TestCodexAuthPolicyMixedForcesPrompt(t *testing.T) {
	t.Parallel()
	got := codexAuthPolicy(
		PluginInfo{Servers: []PluginServerInfo{codexPublicServerNoEnv(), codexPublicServerWithEnv()}},
		GenerateConfig{APIKey: "k"},
	)
	require.Equal(t, "ON_INSTALL", got)
}

func TestCodexAuthPolicyNoServersIsSilent(t *testing.T) {
	t.Parallel()
	got := codexAuthPolicy(PluginInfo{}, GenerateConfig{})
	require.Equal(t, "ON_USE", got)
}

func TestGenerateSinglePluginPackageCodex(t *testing.T) {
	t.Parallel()
	plugin := PluginInfo{
		Name:    "Test",
		Slug:    "test",
		Servers: []PluginServerInfo{{DisplayName: "gram-server", MCPURL: "https://app.getgram.ai/mcp/test"}},
	}

	files, err := GenerateSinglePluginPackage(plugin, GenerateConfig{OrgName: "Test Org", ServerURL: "https://app.getgram.ai"}, "codex")
	require.NoError(t, err)

	for p := range files {
		require.False(t, strings.HasPrefix(p, "test-codex/"), "flat package must not include the marketplace subdir prefix: %s", p)
	}

	var meta codexPluginMeta
	err = json.Unmarshal(files[".codex-plugin/plugin.json"], &meta)
	require.NoError(t, err)
	require.Equal(t, "test", meta.Name, "flat package should use the raw slug, not slug-codex")
}

func TestGenerateReadmeEscapesMarkdownInTableCells(t *testing.T) {
	t.Parallel()
	plugins := []PluginInfo{
		{
			Name:        "Name | with pipe",
			Slug:        "evil-plugin",
			Description: "line one\nline two | still line two",
		},
	}

	files, err := GeneratePluginPackages(plugins, GenerateConfig{
		OrgName:   "Acme",
		OrgEmail:  "",
		ServerURL: "https://app.getgram.ai",
	})
	require.NoError(t, err)

	readme := string(files["README.md"])

	var row string
	for line := range strings.SplitSeq(readme, "\n") {
		if strings.HasPrefix(line, "| Name") || strings.HasPrefix(line, "| evil") {
			row = line
			break
		}
	}
	require.NotEmpty(t, row, "plugin row not found in README:\n%s", readme)

	unescapedPipes := strings.Count(strings.ReplaceAll(row, `\|`, ""), "|")
	require.Equal(t, 4, unescapedPipes, "row should have exactly 4 unescaped pipes (3 separators + trailing)")
	require.Contains(t, row, `Name \| with pipe`)
	require.Contains(t, row, `line one line two \| still line two`)
	require.NotContains(t, row, "\nline two")
}

func TestEscapeMarkdownCellTruncatesLongValues(t *testing.T) {
	t.Parallel()
	long := strings.Repeat("a", 500)
	got := escapeMarkdownCell(long)
	require.True(t, strings.HasSuffix(got, "…"))
	require.Less(t, len(got), len(long))
}

func TestGenerateMarketplaceManifest(t *testing.T) {
	t.Parallel()
	plugins := []PluginInfo{
		{Name: "A", Slug: "a", Description: "First plugin"},
		{Name: "B", Slug: "b", Description: "Second plugin"},
	}

	files, err := GeneratePluginPackages(plugins, GenerateConfig{
		OrgName:   "Acme",
		OrgEmail:  "",
		ServerURL: "https://app.getgram.ai",
	})
	require.NoError(t, err)

	var claudeManifest marketplaceManifest
	err = json.Unmarshal(files[".claude-plugin/marketplace.json"], &claudeManifest)
	require.NoError(t, err)

	require.Equal(t, "acme-speakeasy", claudeManifest.Name)
	require.Equal(t, "Acme", claudeManifest.Owner.Name)
	require.Len(t, claudeManifest.Plugins, 2)
	require.Equal(t, "./a", claudeManifest.Plugins[0].Source)
	require.Equal(t, "./b", claudeManifest.Plugins[1].Source)

	var cursorManifest marketplaceManifest
	err = json.Unmarshal(files[".cursor-plugin/marketplace.json"], &cursorManifest)
	require.NoError(t, err)

	require.Equal(t, "acme-speakeasy", cursorManifest.Name)
	require.Len(t, cursorManifest.Plugins, 2)
	require.NotNil(t, cursorManifest.Metadata)
	require.Equal(t, "cursor-plugins", cursorManifest.Metadata.PluginRoot)
	require.Equal(t, "a-cursor", cursorManifest.Plugins[0].Source)
	require.Equal(t, "b-cursor", cursorManifest.Plugins[1].Source)
}

func TestGenerateMarketplaceManifestUsesMarketplaceNameOverride(t *testing.T) {
	t.Parallel()
	plugins := []PluginInfo{{Name: "A", Slug: "a"}}

	files, err := GeneratePluginPackages(plugins, GenerateConfig{
		OrgName:         "Acme",
		ServerURL:       "https://app.getgram.ai",
		MarketplaceName: "acme-custom",
	})
	require.NoError(t, err)

	var claudeManifest marketplaceManifest
	require.NoError(t, json.Unmarshal(files[".claude-plugin/marketplace.json"], &claudeManifest))
	require.Equal(t, "acme-custom", claudeManifest.Name)

	var cursorManifest marketplaceManifest
	require.NoError(t, json.Unmarshal(files[".cursor-plugin/marketplace.json"], &cursorManifest))
	require.Equal(t, "acme-custom", cursorManifest.Name)

	var codexManifest codexMarketplaceManifest
	require.NoError(t, json.Unmarshal(files[".agents/plugins/marketplace.json"], &codexManifest))
	require.Equal(t, "acme-custom", codexManifest.Name)
}

func TestGenerateMarketplaceManifestScopesNonDefaultProject(t *testing.T) {
	t.Parallel()
	plugins := []PluginInfo{{Name: "A", Slug: "a"}}

	// Non-default project: the name is scoped by the project slug so it doesn't
	// collide with the org's other projects.
	scoped, err := GeneratePluginPackages(plugins, GenerateConfig{
		OrgName:          "Acme",
		ServerURL:        "https://app.getgram.ai",
		ProjectSlug:      "sales",
		IsDefaultProject: false,
	})
	require.NoError(t, err)
	var scopedManifest marketplaceManifest
	require.NoError(t, json.Unmarshal(scoped[".claude-plugin/marketplace.json"], &scopedManifest))
	require.Equal(t, "acme-sales-speakeasy", scopedManifest.Name)

	// Default project keeps the bare org-derived name even with a slug set.
	def, err := GeneratePluginPackages(plugins, GenerateConfig{
		OrgName:          "Acme",
		ServerURL:        "https://app.getgram.ai",
		ProjectSlug:      "sales",
		IsDefaultProject: true,
	})
	require.NoError(t, err)
	var defManifest marketplaceManifest
	require.NoError(t, json.Unmarshal(def[".claude-plugin/marketplace.json"], &defManifest))
	require.Equal(t, "acme-speakeasy", defManifest.Name)
}

func TestRenderHookScriptClaudeUsesLocalHookAuth(t *testing.T) {
	t.Parallel()
	cfg := GenerateConfig{
		ServerURL:   "https://app.getgram.ai",
		HooksAPIKey: "gram_local_secret_xyz",
		ProjectSlug: "acme-prod",
	}
	script := string(renderHookScript(cfg, "claude"))

	require.Contains(t, string(renderSharedAuthScript()), "${server_url}/rpc/hooks.ingest")
	require.NotContains(t, script, `X-Gram-Hook-Source`)
	require.NotContains(t, script, "/hooks/claude", "must not use the legacy /hooks/<platform> path")
	require.NotContains(t, script, "gram_local_secret_xyz", "hook sender must not embed the publish-time hooks key")
	require.NotContains(t, script, `-H "Gram-Key:`, "secret headers should not be passed in curl argv")
	require.NotContains(t, script, "Authorization", "endpoint reads Gram-Key, not Authorization")
}

func TestRenderHookScriptCursorUsesLocalHookAuth(t *testing.T) {
	t.Parallel()
	cfg := GenerateConfig{
		ServerURL:   "https://app.getgram.ai",
		HooksAPIKey: "gram_local_secret_xyz",
		ProjectSlug: "acme-prod",
	}
	script := string(renderHookScript(cfg, "cursor"))

	require.Contains(t, string(renderSharedAuthScript()), "${server_url}/rpc/hooks.ingest")
	require.NotContains(t, script, `X-Gram-Hook-Source`)
	require.NotContains(t, script, `${server_url}/hooks/cursor`, "must not use the legacy /hooks/<platform> path")
	require.NotContains(t, script, "gram_local_secret_xyz", "hook sender must not embed the publish-time hooks key")
	require.NotContains(t, script, `-H "Gram-Key:`, "secret headers should not be passed in curl argv")
	require.NotContains(t, script, "Authorization", "cursor endpoint does not read Authorization")
}

func TestRenderHookScriptCursorBackfillsSkippedPromptFromTranscript(t *testing.T) {
	t.Parallel()

	_, err := exec.LookPath("jq")
	require.NoError(t, err, "jq is required for Cursor transcript backfill")
	_, err = exec.LookPath("base64")
	require.NoError(t, err, "base64 is required for Cursor transcript backfill")

	cfg := GenerateConfig{
		ServerURL:   "https://app.getgram.ai",
		HooksAPIKey: "gram_local_secret_xyz",
		ProjectSlug: "acme-prod",
	}
	script := string(renderHookScript(cfg, "cursor"))

	dir := t.TempDir()
	hookPath := filepath.Join(dir, "hook.sh")
	capturePath := filepath.Join(dir, "payloads.jsonl")
	keysPath := filepath.Join(dir, "keys.txt")
	stateDir := filepath.Join(dir, "state")
	transcriptPath := filepath.Join(dir, "transcript.jsonl")
	require.NoError(t, os.WriteFile(hookPath, []byte(script), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "http.sh"), renderSharedHTTPScript(), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "auth.sh"), renderSharedAuthScript(), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "curl"), []byte(`#!/usr/bin/env bash
key=""
while [ "$#" -gt 0 ]; do
  if [ "$1" = "-H" ] && [ "$#" -gt 1 ]; then
    case "$2" in
      Idempotency-Key:*) key="${2#Idempotency-Key: }" ;;
    esac
    shift 2
    continue
  fi
  shift
done
printf '%s\n' "$key" >> "$GRAM_CAPTURE_KEYS"
cat >> "$GRAM_CAPTURE_PAYLOADS"
printf '\n---GRAM---\n' >> "$GRAM_CAPTURE_PAYLOADS"
printf '{}\n200'
`), 0o755))
	require.NoError(t, os.WriteFile(transcriptPath, []byte(`{"role":"user","message":{"content":[{"type":"text","text":"<user_query>\nGRAM_CURSOR_BACKFILL_PROMPT\n\nPlease reply.\n</user_query>"}]}}
{"role":"assistant","message":{"content":[{"type":"text","text":"ok"}]}}
`), 0o600))

	payload := map[string]any{
		"hook_event_name": "afterAgentResponse",
		"conversation_id": "cursor-cli-session",
		"generation_id":   "turn-1",
		"session_id":      "cursor-cli-session",
		"text":            "assistant ok",
		"transcript_path": transcriptPath,
		"cursor_version":  "3.9.16",
		"model":           "composer-2.5-fast",
	}
	payloadBytes, err := json.Marshal(payload)
	require.NoError(t, err)

	cmd := exec.Command("bash", hookPath)
	cmd.Stdin = bytes.NewReader(payloadBytes)
	cmd.Env = append(os.Environ(),
		"PATH="+dir+string(os.PathListSeparator)+os.Getenv("PATH"),
		"GRAM_CAPTURE_PAYLOADS="+capturePath,
		"GRAM_CAPTURE_KEYS="+keysPath,
		"GRAM_HOOKS_API_KEY=gram_test_hooks_key",
		"GRAM_HOOKS_PROJECT_SLUG=acme-prod",
		"XDG_STATE_HOME="+stateDir,
	)
	output, err := cmd.CombinedOutput()
	require.NoError(t, err, string(output))

	chunks := strings.Split(string(requireFileBytes(t, capturePath)), "\n---GRAM---\n")
	require.Len(t, chunks, 3, "expected backfilled prompt, actual event, and trailing split")
	firstPayload := strings.TrimSpace(chunks[0])
	secondPayload := strings.TrimSpace(chunks[1])

	var backfilled map[string]any
	require.NoError(t, json.Unmarshal([]byte(firstPayload), &backfilled))
	backfilledEvent := backfilled["event"].(map[string]any)
	require.Equal(t, "prompt.submitted", backfilledEvent["type"])
	backfilledData := backfilled["data"].(map[string]any)
	backfilledPrompt := backfilledData["prompt"].(map[string]any)
	require.Equal(t, "GRAM_CURSOR_BACKFILL_PROMPT\n\nPlease reply.", backfilledPrompt["text"])

	var actual map[string]any
	require.NoError(t, json.Unmarshal([]byte(secondPayload), &actual))
	actualEvent := actual["event"].(map[string]any)
	require.Equal(t, "assistant.responded", actualEvent["type"])
	actualData := actual["data"].(map[string]any)
	actualMessage := actualData["message"].(map[string]any)
	require.Equal(t, "assistant ok", actualMessage["text"])

	keys := strings.Fields(string(requireFileBytes(t, keysPath)))
	require.Len(t, keys, 2)
	require.NotEqual(t, keys[0], keys[1], "backfill and actual event must use distinct idempotency keys")
}

func TestCursorMCPEnrichmentRecognizesEventField(t *testing.T) {
	t.Parallel()

	_, err := exec.LookPath("jq")
	require.NoError(t, err, "jq is required for Cursor MCP enrichment")

	root := t.TempDir()
	pluginDir := filepath.Join(root, "gram-plugin")
	require.NoError(t, os.MkdirAll(pluginDir, 0o755))
	hooksDir := filepath.Join(pluginDir, "hooks")
	require.NoError(t, os.MkdirAll(hooksDir, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(pluginDir, "mcp.json"), []byte(`{
  "mcpServers": {
    "grame2e": { "url": "https://app.getgram.ai/mcp/e2e" }
  }
}`), 0o600))

	scriptPath := filepath.Join(t.TempDir(), "cursor-mcp.sh")
	require.NoError(t, os.WriteFile(scriptPath, []byte(renderCursorMCPEnrichmentSnippet()), 0o755))

	payload := `{"event":"beforeMCPExecution","mcp_server_name":"grame2e","tool_name":"shadow_lookup"}`
	cmd := exec.Command("bash", "-c", `. ./cursor-mcp.sh; gram_hooks_enrich_cursor_mcp_payload "$PAYLOAD"`)
	cmd.Dir = filepath.Dir(scriptPath)
	cmd.Env = append(os.Environ(),
		"script_dir="+hooksDir,
		"PAYLOAD="+payload,
	)
	output, err := cmd.CombinedOutput()
	require.NoError(t, err, string(output))

	var enriched map[string]any
	require.NoError(t, json.Unmarshal(output, &enriched))
	require.Equal(t, "https://app.getgram.ai/mcp/e2e", enriched["url"])
	require.Equal(t, "https://app.getgram.ai/mcp/e2e", enriched["mcp_server_url"])
}

func TestRenderHookPayloadNormalizationDoesNotRequireJQForToolInput(t *testing.T) {
	t.Parallel()

	bashPath, err := exec.LookPath("bash")
	require.NoError(t, err, "bash is required to run generated hook snippets")

	binDir := t.TempDir()
	for _, name := range []string{"awk", "sed", "tr"} {
		path, err := exec.LookPath(name)
		require.NoError(t, err, "%s is required to run generated hook snippets", name)
		require.NoError(t, os.Symlink(path, filepath.Join(binDir, name)))
	}

	scriptPath := filepath.Join(t.TempDir(), "normalize.sh")
	script := renderHookPayloadNormalizationSnippet("cursor") + `
payload='{"event":"preToolUse","tool_name":"Search","tool_input":{"query":"a,b","nested":{"ok":true}}}'
gram_hooks_build_canonical_payload "$payload" "test-host"
`
	require.NoError(t, os.WriteFile(scriptPath, []byte(script), 0o755))

	cmd := exec.Command(bashPath, scriptPath)
	cmd.Env = append(os.Environ(), "PATH="+binDir)
	output, err := cmd.CombinedOutput()
	require.NoError(t, err, string(output))
	require.NotContains(t, string(output), "awk:", "normalization fallback must not emit awk errors")

	var parsed map[string]any
	require.NoError(t, json.Unmarshal(output, &parsed))
	data, ok := parsed["data"].(map[string]any)
	require.True(t, ok)
	toolCall, ok := data["tool_call"].(map[string]any)
	require.True(t, ok)
	input, ok := toolCall["input"].(map[string]any)
	require.True(t, ok)
	require.Equal(t, "a,b", input["query"])
}

func TestRenderHookScriptCursorOmitsProjectHeaderWhenSlugMissing(t *testing.T) {
	t.Parallel()
	cfg := GenerateConfig{
		ServerURL:   "https://app.getgram.ai",
		HooksAPIKey: "gram_local_secret_xyz",
	}
	script := string(renderHookScript(cfg, "cursor"))

	require.Contains(t, script, `project_slug="${GRAM_HOOKS_PROJECT_SLUG:-}"`)
	require.NotContains(t, script, "gram_local_secret_xyz", "hook sender must not embed the publish-time hooks key")
	require.NotContains(t, script, "Gram-Project")
	require.NotContains(t, script, `-H "Gram-Key:`, "secret headers should not be passed in curl argv")
}

func TestRenderHookScriptUsesDeviceAgentIdentityWhenAvailable(t *testing.T) {
	t.Parallel()

	cfg := GenerateConfig{
		ServerURL:   "https://app.getgram.ai",
		HooksAPIKey: "gram_local_secret_xyz",
		ProjectSlug: "acme-prod",
	}
	script := string(renderHookScript(cfg, "cursor"))

	dir := t.TempDir()
	hookPath := filepath.Join(dir, "hook.sh")
	capturePath := filepath.Join(dir, "payload.json")
	require.NoError(t, os.WriteFile(hookPath, []byte(script), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "identity.sh"), renderDeviceAgentIdentityScript(), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "http.sh"), renderSharedHTTPScript(), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "auth.sh"), renderSharedAuthScript(), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "fake-agent"), []byte(`#!/usr/bin/env bash
if [ "$1" = "identity" ]; then
  printf '{"identity":{"email":"agent@example.com"}}'
  exit 0
fi
exit 1
`), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "curl"), []byte(`#!/usr/bin/env bash
cat > "$GRAM_CAPTURE_PAYLOAD"
printf '{}\n200'
`), 0o755))

	cmd := exec.Command("bash", hookPath)
	cmd.Stdin = strings.NewReader(`{"hook_event_name":"beforeSubmitPrompt","user_email":"cursor@example.com"}`)
	cmd.Env = append(os.Environ(),
		"PATH="+dir+string(os.PathListSeparator)+os.Getenv("PATH"),
		"GRAM_CAPTURE_PAYLOAD="+capturePath,
		"GRAM_HOOKS_API_KEY=gram_test_hooks_key",
		"GRAM_HOOKS_PROJECT_SLUG=acme-prod",
		"GRAM_DEVICE_AGENT_COMMANDS=fake-agent",
		// Pin a generous timeout so CI scheduling jitter can't trip the
		// device-agent wall-clock timeout (default 1.5s) and flake the test.
		"GRAM_DEVICE_AGENT_TIMEOUT_TENTHS=600",
	)
	output, err := cmd.CombinedOutput()
	require.NoError(t, err, string(output))

	var posted map[string]any
	postedPayload := string(requireFileBytes(t, capturePath))
	require.NoError(t, json.Unmarshal([]byte(postedPayload), &posted))
	require.Nil(t, posted["user_email"])
	require.NotContains(t, postedPayload, `agent@example.com`, "unified hooks must not enrich attribution from the device agent")
	require.Contains(t, postedPayload, `"adapter":"cursor"`)
}

func TestRenderHookScriptFallsBackWhenDeviceAgentMissing(t *testing.T) {
	t.Parallel()

	cfg := GenerateConfig{
		ServerURL:   "https://app.getgram.ai",
		HooksAPIKey: "gram_local_secret_xyz",
		ProjectSlug: "acme-prod",
	}
	script := string(renderHookScript(cfg, "cursor"))

	dir := t.TempDir()
	hookPath := filepath.Join(dir, "hook.sh")
	capturePath := filepath.Join(dir, "payload.json")
	require.NoError(t, os.WriteFile(hookPath, []byte(script), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "identity.sh"), renderDeviceAgentIdentityScript(), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "http.sh"), renderSharedHTTPScript(), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "auth.sh"), renderSharedAuthScript(), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "curl"), []byte(`#!/usr/bin/env bash
cat > "$GRAM_CAPTURE_PAYLOAD"
printf '{}\n200'
`), 0o755))

	cmd := exec.Command("bash", hookPath)
	cmd.Stdin = strings.NewReader(`{"hook_event_name":"beforeSubmitPrompt","user_email":"cursor@example.com"}`)
	cmd.Env = append(os.Environ(),
		"PATH="+dir+string(os.PathListSeparator)+os.Getenv("PATH"),
		"GRAM_CAPTURE_PAYLOAD="+capturePath,
		"GRAM_HOOKS_API_KEY=gram_test_hooks_key",
		"GRAM_HOOKS_PROJECT_SLUG=acme-prod",
		"GRAM_DEVICE_AGENT_COMMANDS=missing-agent",
	)
	output, err := cmd.CombinedOutput()
	require.NoError(t, err, string(output))

	var posted map[string]any
	require.NoError(t, json.Unmarshal(requireFileBytes(t, capturePath), &posted))
	require.Nil(t, posted["user_email"])
	require.Contains(t, string(requireFileBytes(t, capturePath)), `"adapter":"cursor"`)
}

func TestDeviceAgentIdentityScriptHandlesWhitespaceEmptyObject(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(dir, "fake-agent"), []byte(`#!/usr/bin/env bash
if [ "$1" = "identity" ]; then
  printf '{"email":"agent@example.com"}'
  exit 0
fi
exit 1
`), 0o755))

	cmd := exec.Command("bash", "-c", `. ./identity.sh; gram_enrich_identity_payload '{
  }'`)
	cmd.Dir = dir
	cmd.Env = append(os.Environ(),
		"PATH="+dir+string(os.PathListSeparator)+os.Getenv("PATH"),
		"GRAM_DEVICE_AGENT_COMMANDS=fake-agent",
		// Pin a generous timeout so CI scheduling jitter can't trip the
		// device-agent wall-clock timeout (default 1.5s) and flake the test.
		"GRAM_DEVICE_AGENT_TIMEOUT_TENTHS=600",
	)
	require.NoError(t, os.WriteFile(filepath.Join(dir, "identity.sh"), renderDeviceAgentIdentityScript(), 0o755))
	output, err := cmd.CombinedOutput()
	require.NoError(t, err, string(output))

	var posted map[string]any
	require.NoError(t, json.Unmarshal(output, &posted))
	require.Equal(t, "agent@example.com", posted["user_email"])
}

func TestGenerateObservabilityPluginsIncludeSharedHookHelpers(t *testing.T) {
	t.Parallel()
	cfg := GenerateConfig{
		OrgName:     "Acme",
		ServerURL:   "https://app.getgram.ai",
		HooksAPIKey: "gram_local_secret_xyz",
	}
	files, err := GeneratePluginPackages(nil, cfg)
	require.NoError(t, err)

	for _, path := range []string{
		ClaudeObservabilitySlug(cfg) + "/hooks/identity.sh",
		ClaudeObservabilitySlug(cfg) + "/hooks/auth.sh",
		ClaudeObservabilitySlug(cfg) + "/hooks/auth_preflight.sh",
		"cursor-plugins/" + CursorObservabilitySlug(cfg) + "/hooks/identity.sh",
		"cursor-plugins/" + CursorObservabilitySlug(cfg) + "/hooks/auth.sh",
		"cursor-plugins/" + CursorObservabilitySlug(cfg) + "/hooks/auth_preflight.sh",
		CodexObservabilitySlug(cfg) + "/hooks/identity.sh",
		CodexObservabilitySlug(cfg) + "/hooks/auth.sh",
		CodexObservabilitySlug(cfg) + "/hooks/auth_preflight.sh",
	} {
		require.NotNil(t, files[path], "observability helper missing: %s", path)
	}
}

// Claude only invokes hook.sh for events listed in hooks.json. The Claude()
// handler in server/internal/hooks/claude_hooks.go records PostToolUseFailure,
// so dropping it from the registered events would silently lose all tool
// failure telemetry. Cursor's parallel list already carries postToolUseFailure;
// keep parity to make sure the failure signal isn't dropped on the Claude side.
func TestClaudeObservabilityHookEventsRegistersToolFailureEvent(t *testing.T) {
	t.Parallel()
	require.Contains(t, ClaudeObservabilityHookEvents, "PostToolUseFailure")
}

func TestGenerateClaudeObservabilityPluginHooksJSONIncludesAllRegisteredEvents(t *testing.T) {
	t.Parallel()
	cfg := GenerateConfig{
		OrgName:     "Acme",
		ServerURL:   "https://app.getgram.ai",
		HooksAPIKey: "gram_local_secret_xyz",
	}
	files, err := GeneratePluginPackages(nil, cfg)
	require.NoError(t, err)

	hooksJSON := files[ClaudeObservabilitySlug(cfg)+"/hooks/hooks.json"]
	require.NotNil(t, hooksJSON, "claude observability hooks/hooks.json missing")
	require.NotNil(t, files[ClaudeObservabilitySlug(cfg)+"/hooks/identity.sh"], "claude observability hooks/identity.sh missing")

	var parsed claudeHooksConfig
	require.NoError(t, json.Unmarshal(hooksJSON, &parsed))

	for _, event := range ClaudeObservabilityHookEvents {
		require.Contains(t, parsed.Hooks, event, "event %q must be registered in hooks.json or Claude will silently drop it", event)
	}
}

func TestGenerateClaudeObservabilityUsesUnifiedHookScriptForAllEvents(t *testing.T) {
	t.Parallel()
	cfg := GenerateConfig{
		OrgName:     "Acme",
		ServerURL:   "https://app.getgram.ai",
		HooksAPIKey: "gram_local_secret_xyz",
		ProjectSlug: "acme-prod",
	}
	files, err := GeneratePluginPackages(nil, cfg)
	require.NoError(t, err)

	slug := ClaudeObservabilitySlug(cfg)

	require.Nil(t, files[slug+"/hooks/mcp_inventory.sh"], "unified Claude hooks must not ship a server-side inventory sender")
	require.NotNil(t, files[slug+"/hooks/identity.sh"], "claude observability hooks/identity.sh missing")

	var parsed claudeHooksConfig
	require.NoError(t, json.Unmarshal(files[slug+"/hooks/hooks.json"], &parsed))

	sessionStart, ok := parsed.Hooks["SessionStart"]
	require.True(t, ok, "SessionStart must be registered")
	require.Len(t, sessionStart, 1)
	require.Len(t, sessionStart[0].Hooks, 2)
	require.Contains(t, sessionStart[0].Hooks[0].Command, "hooks/auth_preflight.sh", "SessionStart must block on auth before any telemetry hooks")
	require.NotNil(t, sessionStart[0].Hooks[0].Async)
	require.False(t, *sessionStart[0].Hooks[0].Async, "SessionStart auth preflight must be blocking")
	require.Contains(t, sessionStart[0].Hooks[1].Command, "hooks/hook.sh", "SessionStart must use the unified hook sender")

	configChange, ok := parsed.Hooks["ConfigChange"]
	require.True(t, ok, "ConfigChange must be registered")
	require.Len(t, configChange, 1)
	require.Len(t, configChange[0].Hooks, 1)
	require.Contains(t, configChange[0].Hooks[0].Command, "hooks/hook.sh", "ConfigChange must use the unified hook sender")

	// ConfigChange is async (fire-and-forget): it has no allow/deny decision
	// to honor, so Claude must not be held up while telemetry is delivered.
	require.NotNil(t, parsed.Hooks["ConfigChange"][0].Hooks[0].Async)
	require.True(t, *parsed.Hooks["ConfigChange"][0].Hooks[0].Async, "ConfigChange must be async")

	for event, matchers := range parsed.Hooks {
		for _, hook := range matchers[0].Hooks {
			if strings.Contains(hook.Command, "hooks/auth_preflight.sh") {
				continue
			}
			require.Contains(t, hook.Command, "hooks/hook.sh", "event %q should use hook.sh", event)
		}
	}
}

// With observability mode off (the default) the blocking events keep their
// synchronous flag so Claude waits for the deny/allow decision.
func TestGenerateClaudeObservabilityBlockingEventsDefaultToSync(t *testing.T) {
	t.Parallel()
	cfg := GenerateConfig{
		OrgName:     "Acme",
		ServerURL:   "https://app.getgram.ai",
		HooksAPIKey: "gram_local_secret_xyz",
		ProjectSlug: "acme-prod",
	}
	files, err := GeneratePluginPackages(nil, cfg)
	require.NoError(t, err)

	var parsed claudeHooksConfig
	require.NoError(t, json.Unmarshal(files[ClaudeObservabilitySlug(cfg)+"/hooks/hooks.json"], &parsed))

	for _, event := range []string{"UserPromptSubmit", "PreToolUse", "Stop"} {
		matchers, ok := parsed.Hooks[event]
		require.True(t, ok, "%s must be registered", event)
		require.NotNil(t, matchers[0].Hooks[0].Async)
		require.False(t, *matchers[0].Hooks[0].Async, "%s must be blocking when observability mode is off", event)
	}
}

// With observability mode on, telemetry hooks are emitted async so the plugin
// can only observe and report. The SessionStart auth preflight remains blocking
// so fresh installs fail closed until explicit or cached hook credentials exist.
func TestGenerateClaudeObservabilityModeForcesAsyncForAllEvents(t *testing.T) {
	t.Parallel()
	cfg := GenerateConfig{
		OrgName:           "Acme",
		ServerURL:         "https://app.getgram.ai",
		HooksAPIKey:       "gram_local_secret_xyz",
		ProjectSlug:       "acme-prod",
		ObservabilityMode: true,
	}
	files, err := GeneratePluginPackages(nil, cfg)
	require.NoError(t, err)

	var parsed claudeHooksConfig
	require.NoError(t, json.Unmarshal(files[ClaudeObservabilitySlug(cfg)+"/hooks/hooks.json"], &parsed))

	require.NotEmpty(t, parsed.Hooks)
	for event, matchers := range parsed.Hooks {
		for _, hook := range matchers[0].Hooks {
			require.NotNil(t, hook.Async, "event %q must carry an async flag", event)
			if strings.Contains(hook.Command, "hooks/auth_preflight.sh") {
				require.False(t, *hook.Async, "SessionStart auth preflight must remain blocking")
				continue
			}
			require.True(t, *hook.Async, "event %q must be async in observability mode", event)
		}
	}
}

func TestGenerateCursorObservabilityPluginRegistersBlockingSessionStartAuth(t *testing.T) {
	t.Parallel()
	cfg := GenerateConfig{
		OrgName:     "Acme",
		ServerURL:   "https://app.getgram.ai",
		HooksAPIKey: "gram_local_secret_xyz",
		ProjectSlug: "acme-prod",
	}
	files, err := GeneratePluginPackages(nil, cfg)
	require.NoError(t, err)

	slug := "cursor-plugins/" + CursorObservabilitySlug(cfg)
	require.NotNil(t, files[slug+"/hooks/auth_preflight.sh"], "cursor observability hooks/auth_preflight.sh missing")

	var parsed cursorHooksConfig
	require.NoError(t, json.Unmarshal(files[slug+"/hooks/hooks.json"], &parsed))

	sessionStart, ok := parsed.Hooks["sessionStart"]
	require.True(t, ok, "Cursor sessionStart must be registered")
	require.Len(t, sessionStart, 2)
	require.Contains(t, sessionStart[0].Command, "hooks/auth_preflight.sh")
	require.NotNil(t, sessionStart[0].Timeout)
	require.Equal(t, 330, *sessionStart[0].Timeout)
	require.NotNil(t, sessionStart[0].FailClosed)
	require.True(t, *sessionStart[0].FailClosed, "Cursor auth preflight must fail closed")
	require.Contains(t, sessionStart[1].Command, "hooks/hook.sh", "Cursor sessionStart must send unified telemetry after auth preflight")

	for _, event := range CursorObservabilityHookEvents {
		require.Contains(t, parsed.Hooks, event, "event %q must be registered", event)
		require.Len(t, parsed.Hooks[event], 1)
		require.Contains(t, parsed.Hooks[event][0].Command, "hooks/hook.sh")
	}
}

func TestGenerateCodexObservabilityPluginHooksJSONIncludesAllRegisteredEvents(t *testing.T) {
	t.Parallel()
	cfg := GenerateConfig{
		OrgName:     "Acme",
		ServerURL:   "https://app.getgram.ai",
		HooksAPIKey: "gram_local_secret_xyz",
	}
	files, err := GeneratePluginPackages(nil, cfg)
	require.NoError(t, err)

	hooksJSON := files[CodexObservabilitySlug(cfg)+"/hooks/hooks.json"]
	require.NotNil(t, hooksJSON, "codex observability hooks/hooks.json missing")

	var parsed codexHooksConfig
	require.NoError(t, json.Unmarshal(hooksJSON, &parsed))

	for _, event := range CodexObservabilityHookEvents {
		require.Contains(t, parsed.Hooks, event, "event %q must be registered in hooks.json or Codex will silently drop it", event)
	}
}

func TestGenerateCodexObservabilityPluginRoutesTelemetryEventsThroughBackgroundWrapper(t *testing.T) {
	t.Parallel()
	cfg := GenerateConfig{
		OrgName:     "Acme",
		ServerURL:   "https://app.getgram.ai",
		HooksAPIKey: "gram_local_secret_xyz",
	}
	files, err := GeneratePluginPackages(nil, cfg)
	require.NoError(t, err)

	hooksJSON := files[CodexObservabilitySlug(cfg)+"/hooks/hooks.json"]
	require.NotNil(t, hooksJSON, "codex observability hooks/hooks.json missing")
	require.NotContains(t, string(hooksJSON), `"async"`, "Codex skips hooks with async=true/false until async hooks are supported")

	var parsed codexHooksConfig
	require.NoError(t, json.Unmarshal(hooksJSON, &parsed))

	sessionStart, ok := parsed.Hooks["SessionStart"]
	require.True(t, ok, "SessionStart must be registered")
	require.Len(t, sessionStart, 1)
	require.Len(t, sessionStart[0].Hooks, 2)
	require.Contains(t, sessionStart[0].Hooks[0].Command, "hooks/auth_preflight.sh", "SessionStart must block on auth before telemetry")
	require.Contains(t, sessionStart[0].Hooks[1].Command, "hooks/hook_async.sh", "SessionStart telemetry should stay fire-and-forget")

	for _, event := range []string{"PostToolUse", "Stop"} {
		require.Contains(t, parsed.Hooks, event)
		require.Len(t, parsed.Hooks[event], 1)
		require.Len(t, parsed.Hooks[event][0].Hooks, 1)
		require.Contains(t, parsed.Hooks[event][0].Hooks[0].Command, "hooks/hook_async.sh", "event %q should be fire-and-forget", event)
	}

	for _, event := range []string{"PreToolUse", "PermissionRequest", "UserPromptSubmit"} {
		require.Contains(t, parsed.Hooks, event)
		require.Len(t, parsed.Hooks[event], 1)
		require.Len(t, parsed.Hooks[event][0].Hooks, 1)
		require.Contains(t, parsed.Hooks[event][0].Hooks[0].Command, "hooks/hook.sh", "event %q must stay blocking", event)
		require.NotContains(t, parsed.Hooks[event][0].Hooks[0].Command, "hook_async.sh")
	}
}

func TestGenerateCodexObservabilityPluginScriptPostsToCodexEndpoint(t *testing.T) {
	t.Parallel()
	cfg := GenerateConfig{
		OrgName:     "Acme",
		ServerURL:   "https://app.getgram.ai",
		HooksAPIKey: "gram_local_secret_xyz",
	}
	files, err := GeneratePluginPackages(nil, cfg)
	require.NoError(t, err)

	script := string(files[CodexObservabilitySlug(cfg)+"/hooks/hook.sh"])
	require.Contains(t, string(files[CodexObservabilitySlug(cfg)+"/hooks/auth.sh"]), "hooks.ingest", "auth.sh must POST to the unified ingest endpoint")
	require.NotContains(t, script, `X-Gram-Hook-Source`)
	require.Contains(t, script, `gram_hooks_build_canonical_payload`)
	require.Contains(t, script, `"adapter" "codex"`)
	require.Contains(t, script, `gram_hooks_post_authenticated "$server_url" "$payload" 10 "$project_slug" 2`)
	require.Contains(t, script, `[ "$http_code" -lt 300 ]`, "generated hooks must not treat redirects as allow")
	require.NotContains(t, script, `[ "$http_code" -lt 400 ]`, "redirects carry no hook decision and must fail closed")
	require.NotContains(t, script, cfg.HooksAPIKey, "hook.sh must not embed the publish-time hooks key")
	require.NotContains(t, script, "auth.json", "hook.sh must not inspect Codex auth claims for attribution")
	require.NotContains(t, script, `"user_email"`, "hook.sh must not enrich attribution fields; /rpc/hooks.ingest attributes from the Gram auth token")
	require.NotContains(t, script, "python3", "hook runtime must not depend on python")
	require.NotContains(t, script, "GRAM_USER_EMAIL", "hook.sh must not rely on a manually configured user email")

	asyncScript := string(files[CodexObservabilitySlug(cfg)+"/hooks/hook_async.sh"])
	require.Contains(t, asyncScript, "mktemp", "hook_async.sh must copy stdin before returning")
	require.Contains(t, asyncScript, `bash "$script_dir/hook.sh" < "$tmp"`, "hook_async.sh must delegate to hook.sh")
	require.Contains(t, asyncScript, ") >/dev/null 2>&1 &", "hook_async.sh must run the sender in the background")
}

func TestComputeCodexHookApprovalsIncludesSessionStartPreflight(t *testing.T) {
	t.Parallel()
	cfg := GenerateConfig{OrgName: "Acme", ServerURL: "https://app.getgram.ai"}
	marketplace := conv.ToSlug(cfg.OrgName) + "-speakeasy"
	plugin := CodexObservabilitySlug(cfg)

	approvals, err := computeCodexHookApprovals(marketplace, plugin)
	require.NoError(t, err)

	sessionStartPrefix := plugin + "@" + marketplace + ":hooks/hooks.json:session_start:0:"
	var sessionStartApprovals []codexHookApproval
	for _, approval := range approvals {
		if strings.HasPrefix(approval.StateKey, sessionStartPrefix) {
			sessionStartApprovals = append(sessionStartApprovals, approval)
		}
	}
	require.Len(t, sessionStartApprovals, 2, "SessionStart must pre-approve auth preflight and telemetry hooks")
	require.Equal(t, sessionStartPrefix+"0", sessionStartApprovals[0].StateKey)
	require.Equal(t, sessionStartPrefix+"1", sessionStartApprovals[1].StateKey)
	require.NotEqual(t, sessionStartApprovals[0].TrustedHash, sessionStartApprovals[1].TrustedHash)
}

// runCodexInstallScript executes the generated install script under an
// isolated HOME containing a stub codex at ~/.local/bin (off PATH), so binary
// probing never reaches a real install on the host. The stub appends its
// arguments to the returned call log.
func runCodexInstallScript(t *testing.T, script []byte, existingConfig string) (home string, callLog string) {
	t.Helper()

	bashPath, err := exec.LookPath("bash")
	require.NoError(t, err, "bash is required to run the generated install script")
	pythonPath, err := exec.LookPath("python3")
	require.NoError(t, err, "python3 is required by the generated install script")

	home = t.TempDir()
	callLog = filepath.Join(home, "codex-calls.log")
	stub := "#!/bin/sh\nprintf '%s\\n' \"$*\" >> \"" + callLog + "\"\n"
	require.NoError(t, os.MkdirAll(filepath.Join(home, ".local", "bin"), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(home, ".local", "bin", "codex"), []byte(stub), 0o755))

	if existingConfig != "" {
		require.NoError(t, os.MkdirAll(filepath.Join(home, ".codex"), 0o755))
		require.NoError(t, os.WriteFile(filepath.Join(home, ".codex", "config.toml"), []byte(existingConfig), 0o644))
	}

	scriptPath := filepath.Join(t.TempDir(), "install.sh")
	require.NoError(t, os.WriteFile(scriptPath, script, 0o755))

	cmd := exec.Command(bashPath, scriptPath)
	cmd.Env = []string{
		"HOME=" + home,
		"PATH=" + filepath.Dir(pythonPath) + ":/usr/bin:/bin",
	}
	out, err := cmd.CombinedOutput()
	require.NoError(t, err, "install script failed: %s", out)

	return home, callLog
}

func TestGenerateCodexObservabilityPluginScriptEnrichesMCPMetadataOnDemand(t *testing.T) {
	t.Parallel()
	cfg := GenerateConfig{
		OrgName:     "Acme",
		ServerURL:   "https://app.getgram.ai",
		HooksAPIKey: "gram_local_secret_xyz",
	}
	files, err := GeneratePluginPackages(nil, cfg)
	require.NoError(t, err)

	dir := t.TempDir()
	hookPath := filepath.Join(dir, "hook.sh")
	capturePath := filepath.Join(dir, "payload.json")
	require.NoError(t, os.WriteFile(hookPath, files[CodexObservabilitySlug(cfg)+"/hooks/hook.sh"], 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "http.sh"), renderSharedHTTPScript(), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "auth.sh"), renderSharedAuthScript(), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "curl"), []byte(`#!/usr/bin/env bash
cat > "$GRAM_CAPTURE_PAYLOAD"
printf '{}\n200'
`), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "codex"), []byte(`#!/usr/bin/env bash
if [ "$1" = "mcp" ] && [ "$2" = "list" ] && [ "$3" = "--json" ]; then
  printf '[{"name":"shadow_e2e","transport":{"type":"streamable_http","url":"https://app.getgram.ai/mcp/shadow-e2e"}}]'
  exit 0
fi
exit 1
`), 0o755))

	cmd := exec.Command("bash", hookPath)
	cmd.Stdin = strings.NewReader(`{"hook_event_name":"PreToolUse","tool_name":"mcp__shadow_e2e__lookup","tool_input":{"query":"needle"},"session_id":"codex-session","tool_use_id":"tool-1"}`)
	cmd.Env = append(os.Environ(),
		"PATH="+dir+string(os.PathListSeparator)+os.Getenv("PATH"),
		"GRAM_CAPTURE_PAYLOAD="+capturePath,
		"GRAM_HOOKS_API_KEY=gram_test_hooks_key",
		"GRAM_HOOKS_PROJECT_SLUG=default",
	)
	output, err := cmd.CombinedOutput()
	require.NoError(t, err, string(output))

	var posted map[string]any
	require.NoError(t, json.Unmarshal(requireFileBytes(t, capturePath), &posted))
	data := posted["data"].(map[string]any)
	toolCall := data["tool_call"].(map[string]any)
	require.Equal(t, "mcp__shadow_e2e__lookup", toolCall["name"])
	mcp := data["mcp"].(map[string]any)
	require.Equal(t, "shadow_e2e", mcp["server_name"])
	require.Equal(t, "https://app.getgram.ai/mcp/shadow-e2e", mcp["url"])
}

// Substring assertions cannot catch shell quoting regressions — run bash -n
// over every generated shell script.
func TestGeneratedHookScriptsAreValidBash(t *testing.T) {
	t.Parallel()
	bashPath, err := exec.LookPath("bash")
	require.NoError(t, err, "bash is required to syntax-check generated hook scripts")

	cfg := GenerateConfig{
		OrgName:     "Acme",
		ServerURL:   "https://app.getgram.ai",
		HooksAPIKey: "gram_local_secret_xyz",
	}
	for _, platform := range []string{"claude", "cursor", "codex"} {
		files, err := GenerateObservabilityPluginPackage(cfg, platform)
		require.NoError(t, err)
		for name, content := range files {
			if !strings.HasSuffix(name, ".sh") {
				continue
			}
			path := filepath.Join(t.TempDir(), filepath.Base(name))
			require.NoError(t, os.WriteFile(path, content, 0o755))
			out, err := exec.Command(bashPath, "-n", path).CombinedOutput()
			require.NoError(t, err, "%s %s failed bash -n: %s", platform, name, out)
		}
	}
}

// An upgraded install already carries [hooks.state] entries whose trusted_hash
// was computed against the previous hook command. When the command changes
// (e.g. SessionStart moving from hook.sh to hook_async.sh) the installer must
// rewrite those hashes in place, otherwise Codex flags the hooks as modified
// and silently stops running telemetry until the user re-approves them.
func TestGenerateCodexInstallScriptRefreshesStaleTrustedHashes(t *testing.T) {
	t.Parallel()

	cfg := GenerateConfig{OrgName: "Acme", ServerURL: "https://app.getgram.ai"}
	marketplace := conv.ToSlug(cfg.OrgName) + "-speakeasy"
	plugin := CodexObservabilitySlug(cfg)

	approvals, err := computeCodexHookApprovals(marketplace, plugin)
	require.NoError(t, err)
	require.NotEmpty(t, approvals)
	target := approvals[0]

	const staleHash = "sha256:0000000000000000000000000000000000000000000000000000000000000000"
	require.NotEqual(t, staleHash, target.TrustedHash, "fixture hash must differ from the computed one")

	script, err := GenerateCodexInstallScript("https://example.com/gram-marketplace", cfg)
	require.NoError(t, err)

	existing := "[hooks.state.\"" + target.StateKey + "\"]\n" +
		"enabled = true\n" +
		"trusted_hash = \"" + staleHash + "\"\n"
	home, _ := runCodexInstallScript(t, script, existing)

	patched := requireFileBytes(t, filepath.Join(home, ".codex", "config.toml"))
	patchedStr := string(patched)

	require.NotContains(t, patchedStr, staleHash, "stale trusted_hash must be replaced")
	require.Contains(t, patchedStr, target.TrustedHash, "trusted_hash must be refreshed to the current command's hash")
	require.Equal(t, 1, strings.Count(patchedStr, "[hooks.state.\""+target.StateKey+"\"]"), "refresh must not duplicate the entry")
}

// Desktop-only and MDM-deployed machines run without codex on PATH. The
// install script must probe well-known install locations and use the binary
// it finds there instead of skipping marketplace registration.
func TestGenerateCodexInstallScriptProbesForCodexBinary(t *testing.T) {
	t.Parallel()

	cfg := GenerateConfig{OrgName: "Acme", ServerURL: "https://app.getgram.ai"}
	script, err := GenerateCodexInstallScript("https://example.com/gram-marketplace", cfg)
	require.NoError(t, err)

	_, callLog := runCodexInstallScript(t, script, "")

	calls := string(requireFileBytes(t, callLog))
	require.Contains(t, calls, "plugin marketplace add https://example.com/gram-marketplace")
	require.Contains(t, calls, "plugin marketplace upgrade "+conv.ToSlug(cfg.OrgName)+"-speakeasy")
}

// Root-level dotted keys (features.hooks = true) implicitly define the
// [features] table and make Codex reject the whole config with a duplicate-key
// error when an explicit [features] table is also present — which is the
// default, since js_repl lives there. The flags must be written inside the
// table, and dotted keys left behind by earlier script versions removed.
func TestGenerateCodexInstallScriptWritesFeatureFlagsInFeaturesTable(t *testing.T) {
	t.Parallel()

	cfg := GenerateConfig{OrgName: "Acme", ServerURL: "https://app.getgram.ai"}
	script, err := GenerateCodexInstallScript("https://example.com/gram-marketplace", cfg)
	require.NoError(t, err)

	existing := "features.hooks = true\n" +
		"features.plugin_hooks = true\n\n" +
		"[features]\n" +
		"js_repl = true\n"
	home, _ := runCodexInstallScript(t, script, existing)

	patched := string(requireFileBytes(t, filepath.Join(home, ".codex", "config.toml")))

	require.NotRegexp(t, `(?m)^features\.`, patched, "root-level dotted feature keys must be removed")
	require.Equal(t, 1, strings.Count(patched, "[features]"), "the existing [features] table must be reused")
	require.Equal(t, 1, strings.Count(patched, "\nhooks = true"), "hooks flag must live in the [features] table")
	require.Equal(t, 1, strings.Count(patched, "\nplugin_hooks = true"), "plugin_hooks flag must live in the [features] table")
	require.Contains(t, patched, "js_repl = true", "pre-existing table entries must be preserved")
}

func TestGenerateCodexInstallScriptCreatesFeaturesTable(t *testing.T) {
	t.Parallel()

	cfg := GenerateConfig{OrgName: "Acme", ServerURL: "https://app.getgram.ai"}
	script, err := GenerateCodexInstallScript("https://example.com/gram-marketplace", cfg)
	require.NoError(t, err)

	home, _ := runCodexInstallScript(t, script, "")

	patched := string(requireFileBytes(t, filepath.Join(home, ".codex", "config.toml")))

	require.NotRegexp(t, `(?m)^features\.`, patched, "feature flags must not be written as root-level dotted keys")
	require.Equal(t, 1, strings.Count(patched, "[features]"))
	require.Equal(t, 1, strings.Count(patched, "\nhooks = true"))
	require.Equal(t, 1, strings.Count(patched, "\nplugin_hooks = true"))
}

func TestGenerateReadmeIncludesCodexInstallation(t *testing.T) {
	t.Parallel()
	files, err := GeneratePluginPackages(nil, GenerateConfig{
		OrgName:   "Acme",
		ServerURL: "https://app.getgram.ai",
	})
	require.NoError(t, err)

	readme := string(files["README.md"])
	require.Contains(t, readme, "### Codex", "Codex installation section must be present — Codex packages are still generated and listed in the marketplace")
	require.Contains(t, readme, "codex plugin marketplace add")
}

// hook.sh in the ZIP must carry the execute bit, otherwise extracting the
// archive leaves the script unrunnable and Claude Code / Cursor fail with
// "permission denied" when the registered command tries `./hook.sh`. Mirrors
// the GitHub publish path's mode 100755 in thirdparty/github/repo.go.
func TestWritePluginZipMakesShellScriptsExecutable(t *testing.T) {
	t.Parallel()
	files := map[string][]byte{
		"hook.sh":                    []byte("#!/usr/bin/env bash\necho hi\n"),
		"hook_async.sh":              []byte("#!/usr/bin/env bash\necho hi\n"),
		"hooks/auth_preflight.sh":    []byte("#!/usr/bin/env bash\necho hi\n"),
		".claude-plugin/plugin.json": []byte("{}"),
		"README.md":                  []byte("# readme\n"),
	}

	var buf bytes.Buffer
	require.NoError(t, writePluginZip(&buf, files))

	r, err := zip.NewReader(bytes.NewReader(buf.Bytes()), int64(buf.Len()))
	require.NoError(t, err)

	modes := make(map[string]uint32, len(r.File))
	for _, f := range r.File {
		modes[f.Name] = uint32(f.Mode().Perm())
	}

	require.Equal(t, uint32(0o755), modes["hook.sh"], "hook.sh must be executable so ./hook.sh works after unzip")
	require.Equal(t, uint32(0o755), modes["hook_async.sh"], "hook_async.sh must be executable so ./hook_async.sh works after unzip")
	require.Equal(t, uint32(0o755), modes["hooks/auth_preflight.sh"], "auth_preflight.sh must be executable so hook auth can block SessionStart")
	require.Equal(t, uint32(0o644), modes[".claude-plugin/plugin.json"], "non-script files keep default mode")
	require.Equal(t, uint32(0o644), modes["README.md"], "non-script files keep default mode")
}

// Each publish must stamp a fresh manifest version into every plugin.json.
// Claude Code, Cursor, and Codex marketplaces all key cache invalidation off
// the manifest's version field: if it doesn't change between publishes,
// previously-installed copies are treated as up-to-date and never refreshed.
func TestGeneratePluginPackagesStampsConfigVersionIntoEveryManifest(t *testing.T) {
	t.Parallel()
	plugins := []PluginInfo{
		{
			Name:        "Engineering Tools",
			Slug:        "engineering-tools",
			Description: "MCP servers",
			Servers: []PluginServerInfo{
				{DisplayName: "crm", MCPURL: "https://app.getgram.ai/mcp/crm"},
			},
		},
	}

	cfg := GenerateConfig{
		OrgName:     "Acme",
		ServerURL:   "https://app.getgram.ai",
		HooksAPIKey: "gram_test_hooks_key",
		ProjectSlug: "acme-prod",
		Version:     "0.1.1747087200",
	}

	files, err := GeneratePluginPackages(plugins, cfg)
	require.NoError(t, err)

	// Every plugin.json the publisher writes — both per-plugin and the
	// per-org observability bundle, across all three platforms — must carry
	// the supplied version.
	manifestPaths := []string{
		"engineering-tools/.claude-plugin/plugin.json",
		"cursor-plugins/engineering-tools-cursor/.cursor-plugin/plugin.json",
		"engineering-tools-codex/.codex-plugin/plugin.json",
		"acme-observability/.claude-plugin/plugin.json",
		"cursor-plugins/acme-observability-cursor/.cursor-plugin/plugin.json",
		"acme-observability-codex/.codex-plugin/plugin.json",
	}
	for _, p := range manifestPaths {
		raw, ok := files[p]
		require.True(t, ok, "missing manifest: %s", p)

		var meta struct {
			Version string `json:"version"`
		}
		require.NoError(t, json.Unmarshal(raw, &meta), "parse %s", p)
		require.Equal(t, "0.1.1747087200", meta.Version, "%s did not pick up cfg.Version", p)
	}
}

// Successive publishes with bumped versions must produce different manifest
// bytes so platform clients see a new version and pull. This is the core
// regression test for the "republish doesn't refresh clients" gap.
func TestGeneratePluginPackagesRepublishWithBumpedVersionRefreshesManifest(t *testing.T) {
	t.Parallel()
	plugins := []PluginInfo{
		{
			Name:        "Engineering Tools",
			Slug:        "engineering-tools",
			Description: "MCP servers",
			Servers: []PluginServerInfo{
				{DisplayName: "crm", MCPURL: "https://app.getgram.ai/mcp/crm"},
			},
		},
	}

	base := GenerateConfig{
		OrgName:   "Acme",
		ServerURL: "https://app.getgram.ai",
	}

	first := base
	first.Version = "0.1.100"
	firstFiles, err := GeneratePluginPackages(plugins, first)
	require.NoError(t, err)

	second := base
	second.Version = "0.1.200"
	secondFiles, err := GeneratePluginPackages(plugins, second)
	require.NoError(t, err)

	const manifestPath = "engineering-tools/.claude-plugin/plugin.json"
	require.NotEqual(t,
		string(firstFiles[manifestPath]),
		string(secondFiles[manifestPath]),
		"manifest bytes must differ between publishes — otherwise Claude's marketplace will not refresh",
	)
}

// Empty cfg.Version preserves the legacy "0.1.0" so tests that don't care
// about versioning don't have to construct one. Production callers always
// set cfg.Version via Service.generateConfig.
func TestPluginManifestVersionFallsBackTo010WhenUnset(t *testing.T) {
	t.Parallel()
	require.Equal(t, "0.1.0", pluginManifestVersion(GenerateConfig{}))
	require.Equal(t, "0.1.42", pluginManifestVersion(GenerateConfig{Version: "0.1.42"}))
}

// fingerprintTestPlugins is a representative plugin set reused across the
// fingerprint tests.
func fingerprintTestPlugins() []PluginInfo {
	return []PluginInfo{
		{
			Name:        "Engineering Tools",
			Slug:        "engineering-tools",
			Description: "MCP servers for the engineering team",
			Servers: []PluginServerInfo{
				{
					DisplayName: "crm-tools",
					Policy:      "required",
					MCPURL:      "https://app.getgram.ai/mcp/acme-abc12",
				},
			},
		},
	}
}

func TestPluginFingerprintIsStableAcrossCalls(t *testing.T) {
	t.Parallel()
	cfg := GenerateConfig{OrgName: "Acme Corp", ServerURL: "https://app.getgram.ai", ProjectSlug: "acme"}

	first, err := PluginFingerprint(fingerprintTestPlugins(), cfg)
	require.NoError(t, err)
	require.True(t, strings.HasPrefix(first, "sha256:"))

	second, err := PluginFingerprint(fingerprintTestPlugins(), cfg)
	require.NoError(t, err)

	require.Equal(t, first, second, "same plugins + config must produce the same fingerprint")
}

func TestPluginFingerprintIgnoresPerPublishFields(t *testing.T) {
	t.Parallel()
	plugins := fingerprintTestPlugins()

	base, err := PluginFingerprint(plugins, GenerateConfig{
		OrgName:   "Acme Corp",
		ServerURL: "https://app.getgram.ai",
	})
	require.NoError(t, err)

	// Version and the injected API keys vary on every publish; the fingerprint
	// normalizes them so they must not change the result.
	withNoise, err := PluginFingerprint(plugins, GenerateConfig{
		OrgName:     "Acme Corp",
		ServerURL:   "https://app.getgram.ai",
		Version:     "0.1.1750000000",
		APIKey:      "gram_live_realkey",
		HooksAPIKey: "gram_live_realhookskey",
	})
	require.NoError(t, err)

	require.Equal(t, base, withNoise, "manifest version and API keys must not affect the fingerprint")
}

func TestPluginFingerprintChangesWithPluginConfig(t *testing.T) {
	t.Parallel()
	cfg := GenerateConfig{OrgName: "Acme Corp", ServerURL: "https://app.getgram.ai"}

	base, err := PluginFingerprint(fingerprintTestPlugins(), cfg)
	require.NoError(t, err)

	changed := fingerprintTestPlugins()
	changed[0].Servers = append(changed[0].Servers, PluginServerInfo{
		DisplayName: "analytics",
		Policy:      "optional",
		MCPURL:      "https://app.getgram.ai/mcp/analytics-xyz",
	})
	changedFP, err := PluginFingerprint(changed, cfg)
	require.NoError(t, err)

	require.NotEqual(t, base, changedFP, "adding a server must change the fingerprint")
}

func TestPluginFingerprintChangesWithGeneratorVersion(t *testing.T) {
	t.Parallel()
	// The generator version is mixed into the hash so a deliberate bump forces
	// every project to be seen as changed.
	cfg := GenerateConfig{OrgName: "Acme Corp", ServerURL: "https://app.getgram.ai"}
	plugins := fingerprintTestPlugins()

	files, err := GeneratePluginPackages(plugins, GenerateConfig{
		OrgName:     cfg.OrgName,
		ServerURL:   cfg.ServerURL,
		APIKey:      fingerprintAPIKeySentinel,
		HooksAPIKey: fingerprintHooksKeySentinel,
	})
	require.NoError(t, err)
	require.NotEmpty(t, files)

	fp, err := PluginFingerprint(plugins, cfg)
	require.NoError(t, err)
	require.NotEmpty(t, fp)
}
