package plugins

import (
	"archive/zip"
	"bytes"
	"crypto/sha256"
	"encoding/base64"
	"encoding/binary"
	"encoding/json"
	"fmt"
	"maps"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"slices"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
	"unicode/utf16"

	"github.com/BurntSushi/toml"
	"github.com/speakeasy-api/gram/server/internal/conv"
	"github.com/stretchr/testify/require"
)

func requireFileBytes(t *testing.T, path string) []byte {
	t.Helper()
	data, err := os.ReadFile(path)
	require.NoError(t, err)
	return data
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

func TestGeneratePluginPackagesIncludesPlatformMCPOnlyWhenEnabled(t *testing.T) {
	t.Parallel()
	cfg := GenerateConfig{
		OrgName:            "Acme Corp",
		OrgEmail:           "admin@example.com",
		ServerURL:          "https://app.getgram.ai///",
		APIKey:             "gram_consumer_secret",
		HooksAPIKey:        "gram_hooks_secret",
		ProjectSlug:        "tenant-project-sentinel",
		PlatformMCPEnabled: true,
	}

	files, err := GeneratePluginPackages(fingerprintTestPlugins(), cfg)
	require.NoError(t, err)

	var meta claudePluginMeta
	require.NoError(t, json.Unmarshal(files["platform-mcp/.claude-plugin/plugin.json"], &meta))
	require.Equal(t, platformMCPPluginName, meta.Name)
	require.Equal(t, platformMCPDisplayName, meta.DisplayName)
	require.Equal(t, platformMCPDescription, meta.Description)
	require.Equal(t, "Speakeasy", meta.Author.Name)
	require.Nil(t, meta.UserConfig, "Platform MCP must not request tenant credentials")

	var mcpConfig claudeMCPConfig
	require.NoError(t, json.Unmarshal(files["platform-mcp/.mcp.json"], &mcpConfig))
	require.Equal(t, map[string]claudeMCPServer{
		platformMCPServerName: {
			Type: "http",
			URL:  "https://app.getgram.ai/platform-mcp",
		},
	}, mcpConfig.MCPServers)

	platformSkills, err := loadPlatformMCPSkills()
	require.NoError(t, err)
	require.NotEmpty(t, platformSkills)
	for name, skill := range platformSkills {
		paths := []string{
			"platform-mcp/skills/" + name + "/SKILL.md",
			"cursor-plugins/platform-mcp-cursor/skills/" + name + "/SKILL.md",
			"platform-mcp-codex/skills/" + name + "/SKILL.md",
			"opencode-plugins/platform-mcp/" + platformMCPPluginName + "/skills/" + name + "/SKILL.md",
			platformMCPAgentPluginRoot + "/skills/" + name + "/SKILL.md",
		}
		for _, packagePath := range paths {
			require.Equal(t, skill, files[packagePath], "missing or changed Platform MCP skill %q at %s", name, packagePath)
		}
		require.NotContains(t, string(skill), "Gram", "distributed skill %q exposes the internal codename", name)
	}

	claudeSkill := files["platform-mcp/skills/add-mcp-from-catalog/SKILL.md"]
	require.Contains(t, string(claudeSkill), "register_catalog_mcp")
	require.Contains(t, string(claudeSkill), "add-mcp-from-remote-url", "the catalogue skill routes raw URLs to the remote URL workflow")
	require.Contains(t, string(claudeSkill), "get_mcp_readiness")
	require.Contains(t, string(claudeSkill), "attach_platform_mcp_identity_provider")
	require.Contains(t, string(claudeSkill), "send_platform_mcp_feedback")
	require.NotContains(t, string(claudeSkill), "register_platform_mcp_for_project", "the shipped skill must name only canonical tools")
	require.NotContains(t, string(claudeSkill), "add_platform_mcp_to_default_plugin", "exact-plugin distribution remains rollout-gated")
	require.Contains(t, string(claudeSkill), "For a managed project assistant, call `get_mcp_readiness` without `force`", "assistants must not be instructed to force connection-bound readiness probes")

	remoteURLSkill := files["platform-mcp/skills/add-mcp-from-remote-url/SKILL.md"]
	require.Contains(t, string(remoteURLSkill), "inspect_mcp_candidate")
	require.Contains(t, string(remoteURLSkill), "register_remote_mcp")
	require.Contains(t, string(remoteURLSkill), "get_mcp_readiness")
	require.Contains(t, string(remoteURLSkill), "attach_platform_mcp_identity_provider")
	require.NotContains(t, string(remoteURLSkill), "get_platform_mcp_onboarding_status", "the shipped skill must name only canonical tools")
	require.NotContains(t, string(remoteURLSkill), "add_platform_mcp_to_default_plugin", "exact-plugin distribution remains rollout-gated")
	require.Contains(t, string(remoteURLSkill), "For an external Platform MCP client, call `get_mcp_readiness`", "managed assistants must stop after their dashboard handoff")
	require.NotContains(t, string(remoteURLSkill), "probe_remote_mcp", "the shipped skill must name only our registered tools")
	require.NotContains(t, string(remoteURLSkill), "register_remote_mcp_for_project", "the shipped skill must name only our registered tools")

	var agentManifest agentPluginManifest
	require.NoError(t, json.Unmarshal(files[platformMCPAgentPluginRoot+"/plugin.json"], &agentManifest))
	require.Equal(t, platformMCPPluginName, agentManifest.Name)
	require.Equal(t, "Speakeasy", agentManifest.Author.Name)
	var agentMCP agentMCPConfig
	require.NoError(t, json.Unmarshal(files[platformMCPAgentPluginRoot+"/mcp.json"], &agentMCP))
	require.Equal(t, "https://app.getgram.ai/platform-mcp", agentMCP.MCPServers[platformMCPServerName].URL)
	require.Equal(t, claudeSkill, files[platformMCPAgentPluginRoot+"/skills/add-mcp-from-catalog/SKILL.md"])
	require.Equal(t, remoteURLSkill, files[platformMCPAgentPluginRoot+"/skills/add-mcp-from-remote-url/SKILL.md"])

	platformPrefixes := []string{
		"platform-mcp/",
		"cursor-plugins/platform-mcp-cursor/",
		"platform-mcp-codex/",
		"opencode-plugins/platform-mcp/",
		"agent-plugins/" + platformMCPPluginName + "/",
	}
	for path, content := range files {
		if slices.ContainsFunc(platformPrefixes, func(prefix string) bool { return strings.HasPrefix(path, prefix) }) {
			require.NotContains(t, string(content), cfg.APIKey)
			require.NotContains(t, string(content), cfg.HooksAPIKey)
			require.NotContains(t, string(content), cfg.ProjectSlug)
			require.NotContains(t, string(content), cfg.OrgName)
			require.NotContains(t, string(content), cfg.OrgEmail)
			require.NotContains(t, path, "hooks/")
			require.NotContains(t, string(content), skillFeedbackMCPServerName)
		}
	}

	require.Contains(t, string(files["README.md"]), "## "+platformMCPDisplayName)
	require.Contains(t, string(files["README.md"]), "`"+platformMCPPluginName+"`")
	require.NotContains(t, strings.ToLower(string(files["README.md"])), "aicp")

	var claude marketplaceManifest
	require.NoError(t, json.Unmarshal(files[".claude-plugin/marketplace.json"], &claude))
	require.Len(t, claude.Plugins, len(fingerprintTestPlugins())+2)
	require.Equal(t, "acme-corp-observability", claude.Plugins[0].Name)
	require.Equal(t, marketplaceEntry{
		Name:        platformMCPPluginName,
		DisplayName: platformMCPDisplayName,
		Source:      "./platform-mcp",
		Description: platformMCPDescription,
	}, claude.Plugins[1])

	var cursor marketplaceManifest
	require.NoError(t, json.Unmarshal(files[".cursor-plugin/marketplace.json"], &cursor))
	require.Contains(t, cursor.Plugins, marketplaceEntry{
		Name:        "platform-mcp-cursor",
		Source:      "platform-mcp-cursor",
		Description: platformMCPDescription,
	})

	var codex codexMarketplaceManifest
	require.NoError(t, json.Unmarshal(files[".agents/plugins/marketplace.json"], &codex))
	require.Contains(t, codex.Plugins, codexMarketplaceEntry{
		Name: "platform-mcp-codex",
		Source: codexMarketplaceSource{
			Source: "local",
			Path:   "./platform-mcp-codex",
		},
		Policy: codexMarketplacePolicy{Installation: "AVAILABLE", Authentication: "ON_USE"},
	})

	cfg.PlatformMCPEnabled = false
	withoutPlatform, err := GeneratePluginPackages(fingerprintTestPlugins(), cfg)
	require.NoError(t, err)
	for path := range withoutPlatform {
		require.NotContains(t, path, platformMCPPluginRoot)
		require.NotContains(t, path, platformMCPPluginName)
	}
}

func TestCarryPlatformMCPSubtreeAcceptsB1AndFullNativeLayouts(t *testing.T) {
	t.Parallel()

	cfg := GenerateConfig{ServerURL: "https://app.getgram.ai", Version: "42"}
	allFiles, err := generatePlatformMCPFiles(cfg)
	require.NoError(t, err)

	b1Files := make(map[string][]byte)
	for filePath, content := range allFiles {
		if strings.HasPrefix(filePath, platformMCPPluginRoot+"/") || strings.HasPrefix(filePath, platformMCPAgentPluginRoot+"/") {
			b1Files[filePath] = content
		}
	}
	carried := make(map[string][]byte)
	intact, nativeClientsAvailable := carryPlatformMCPSubtree(carried, b1Files)
	require.True(t, intact)
	require.False(t, nativeClientsAvailable)
	require.Equal(t, b1Files, carried)

	carried = make(map[string][]byte)
	intact, nativeClientsAvailable = carryPlatformMCPSubtree(carried, allFiles)
	require.True(t, intact)
	require.True(t, nativeClientsAvailable)
	require.Equal(t, allFiles, carried)

	partialNative := maps.Clone(allFiles)
	delete(partialNative, platformMCPCodexPluginRoot+"/.mcp.json")
	carried = make(map[string][]byte)
	intact, nativeClientsAvailable = carryPlatformMCPSubtree(carried, partialNative)
	require.True(t, intact)
	require.False(t, nativeClientsAvailable)
	require.NotContains(t, carried, platformMCPCursorPluginRoot+"/.cursor-plugin/plugin.json")
	require.NotContains(t, carried, platformMCPOpenCodePluginRoot+"/plugin/"+platformMCPPluginName+".ts")
}

func TestCarryPlatformMCPSubtreeAcceptsLegacyAgentPluginLayout(t *testing.T) {
	t.Parallel()

	cfg := GenerateConfig{ServerURL: "https://app.getgram.ai", Version: "42"}
	current, err := generatePlatformMCPFiles(cfg)
	require.NoError(t, err)

	legacy := remapPlatformMCPFilesToLegacyLayout(current)
	require.NotContains(t, legacy, platformMCPAgentPluginRoot+"/plugin.json")
	require.Contains(t, legacy, platformMCPLegacyAgentPluginRoot+"/plugin.json")
	require.Contains(t, legacy, platformMCPOpenCodePluginRoot+"/plugin/"+platformMCPLegacyPluginName+".ts")

	carried := make(map[string][]byte)
	intact, nativeClientsAvailable := carryPlatformMCPSubtree(carried, legacy)
	require.True(t, intact)
	require.True(t, nativeClientsAvailable)
	require.Equal(t, legacy, carried)

	b1Legacy := make(map[string][]byte)
	for filePath, content := range legacy {
		if strings.HasPrefix(filePath, platformMCPPluginRoot+"/") || strings.HasPrefix(filePath, platformMCPLegacyAgentPluginRoot+"/") {
			b1Legacy[filePath] = content
		}
	}
	carried = make(map[string][]byte)
	intact, nativeClientsAvailable = carryPlatformMCPSubtree(carried, b1Legacy)
	require.True(t, intact)
	require.False(t, nativeClientsAvailable)
	require.Equal(t, b1Legacy, carried)

	// A B1-only repair of this layout must hash the Agent Plugin bytes that
	// were actually preserved, not only the Claude package.
	withAgent := platformMCPFingerprintFromFiles(b1Legacy)
	withoutAgent := maps.Clone(b1Legacy)
	delete(withoutAgent, platformMCPLegacyAgentPluginRoot+"/plugin.json")
	require.NotEqual(t, withAgent, platformMCPFingerprintFromFiles(withoutAgent))
}

// remapPlatformMCPFilesToLegacyLayout rewrites only the Agent Plugin directory
// and the OpenCode identifier so already-published repositories can be
// reconstructed in tests. Claude, Cursor, Codex, and the OpenCode package root
// stay on their current paths.
func remapPlatformMCPFilesToLegacyLayout(files map[string][]byte) map[string][]byte {
	legacy := make(map[string][]byte, len(files))
	for filePath, content := range files {
		legacy[remapPlatformMCPPathToLegacyLayout(filePath)] = content
	}
	return legacy
}

func remapPlatformMCPPathToLegacyLayout(filePath string) string {
	currentAgentPrefix := platformMCPAgentPluginRoot + "/"
	legacyAgentPrefix := platformMCPLegacyAgentPluginRoot + "/"
	if rest, ok := strings.CutPrefix(filePath, currentAgentPrefix); ok {
		return legacyAgentPrefix + rest
	}

	currentOpenCodePlugin := platformMCPOpenCodePluginRoot + "/plugin/" + platformMCPPluginName + ".ts"
	if filePath == currentOpenCodePlugin {
		return platformMCPOpenCodePluginRoot + "/plugin/" + platformMCPLegacyPluginName + ".ts"
	}

	currentOpenCodeIDPrefix := platformMCPOpenCodePluginRoot + "/" + platformMCPPluginName + "/"
	legacyOpenCodeIDPrefix := platformMCPOpenCodePluginRoot + "/" + platformMCPLegacyPluginName + "/"
	if rest, ok := strings.CutPrefix(filePath, currentOpenCodeIDPrefix); ok {
		return legacyOpenCodeIDPrefix + rest
	}
	return filePath
}

func TestGeneratePlatformMCPPluginPackageDirectDownloadsShareDefinition(t *testing.T) {
	t.Parallel()

	packages := make(map[string]map[string][]byte)
	for _, platform := range []string{"claude", "cursor", "codex", "opencode", "agent-plugin"} {
		files, err := GeneratePlatformMCPPluginPackage("https://app.getgram.ai/", "42", platform)
		require.NoError(t, err, platform)
		packages[platform] = files
	}

	platformSkills, err := loadPlatformMCPSkills()
	require.NoError(t, err)
	for name, skill := range platformSkills {
		paths := map[string]string{
			"claude":       "skills/" + name + "/SKILL.md",
			"cursor":       "skills/" + name + "/SKILL.md",
			"codex":        "skills/" + name + "/SKILL.md",
			"opencode":     platformMCPPluginName + "/skills/" + name + "/SKILL.md",
			"agent-plugin": "skills/" + name + "/SKILL.md",
		}
		for platform, packagePath := range paths {
			require.Equal(t, skill, packages[platform][packagePath], "missing or changed %s direct-download skill %q", platform, name)
		}
	}
	for platform, files := range packages {
		for path, content := range files {
			require.NotContains(t, path, "hooks/", platform)
			require.NotContains(t, string(content), "GRAM_API_KEY", platform)
			require.NotContains(t, string(content), skillFeedbackMCPServerName, platform)
		}
	}

	_, err = GeneratePlatformMCPPluginPackage("https://app.getgram.ai", "42", "unsupported")
	require.ErrorContains(t, err, "unsupported Platform MCP platform")
}

func TestGeneratePluginPackagesRejectsInvalidPlatformMCPServerURL(t *testing.T) {
	t.Parallel()

	for _, serverURL := range []string{
		"",
		"localhost:8080",
		"https:///missing-host",
		"ftp://example.com",
		"https://user:password@example.com",
		"https://example.com?query=value",
		"https://example.com#fragment",
	} {
		_, err := GeneratePluginPackages(fingerprintTestPlugins(), GenerateConfig{
			OrgName:            "Acme Corp",
			ServerURL:          serverURL,
			PlatformMCPEnabled: true,
		})
		require.ErrorContains(t, err, "invalid Platform MCP server URL", serverURL)
	}
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
		"opencode-plugins/engineering-tools/plugin/engineering-tools.ts",
		"opencode-plugins/engineering-tools/engineering-tools/mcp.json",
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

// TestGenerateUnproxiedServerNeverGetsGramCredential guards against
// reintroducing the leak fixed alongside this test: an unproxied server's
// MCPURL points straight at the vendor, so no format may attach a Gram
// credential (static header, env-header, or bearer-token-env-var) to it —
// checked across all four generated formats, and with cfg.APIKey both set
// and unset, since the leak only reproduced with a baked key present.
func TestGenerateUnproxiedServerNeverGetsGramCredential(t *testing.T) {
	t.Parallel()

	for _, cfg := range []GenerateConfig{
		{OrgName: "Test Org", ServerURL: "https://app.getgram.ai", APIKey: "gram_live_leaked_key"},
		{OrgName: "Test Org", ServerURL: "https://app.getgram.ai"},
	} {
		plugins := []PluginInfo{
			{
				Name: "Test",
				Slug: "test",
				Servers: []PluginServerInfo{
					{DisplayName: "vendor-widget", MCPURL: "https://vendor.example.com/mcp", IsUnproxied: true},
				},
			},
		}

		files, err := GeneratePluginPackages(plugins, cfg)
		require.NoError(t, err)

		var claudeConfig claudeMCPConfig
		require.NoError(t, json.Unmarshal(files["test/.mcp.json"], &claudeConfig))
		claudeServer := claudeConfig.MCPServers["vendor-widget"]
		require.Equal(t, "https://vendor.example.com/mcp", claudeServer.URL)
		require.Empty(t, claudeServer.Headers, "Claude must not attach a Gram credential to an unproxied server")

		var cursorConfig cursorMCPConfig
		require.NoError(t, json.Unmarshal(files["cursor-plugins/test-cursor/mcp.json"], &cursorConfig))
		cursorServer := cursorConfig.MCPServers["vendor-widget"]
		require.Equal(t, "https://vendor.example.com/mcp", cursorServer.URL)
		require.Empty(t, cursorServer.Headers, "Cursor must not attach a Gram credential to an unproxied server")

		var codexConfig codexMCPConfig
		require.NoError(t, json.Unmarshal(files["test-codex/.mcp.json"], &codexConfig))
		codexServer := codexConfig.MCPServers["vendor-widget"]
		require.Equal(t, "https://vendor.example.com/mcp", codexServer.URL)
		require.Empty(t, codexServer.HTTPHeaders, "Codex must not attach a Gram credential to an unproxied server")
		require.Empty(t, codexServer.BearerTokenEnvVar, "Codex must not set a bearer_token_env_var for an unproxied server")

		var opencodeConfig opencodeMCPConfig
		require.NoError(t, json.Unmarshal(files["opencode-plugins/test/test/mcp.json"], &opencodeConfig))
		opencodeServer := opencodeConfig.MCP["vendor-widget"]
		require.Equal(t, "https://vendor.example.com/mcp", opencodeServer.URL)
		require.Empty(t, opencodeServer.Headers, "OpenCode must not attach a Gram credential to an unproxied server")

		require.Equal(t, "ON_USE", codexAuthPolicy(plugins[0], cfg),
			"an all-unproxied plugin needs no install-time secret prompt")
	}
}

// TestGenerateClaudeUnproxiedDoesNotForcePrompt mirrors
// TestGenerateClaudeMixedOAuthAndHTTPServers: a private HTTP server still
// forces the GRAM_API_KEY prompt, but an unproxied server standing alone
// must not — needsGramKeyPrompt has the same IsOAuth/IsPublic-only gap the
// header-attachment branches had.
func TestGenerateClaudeUnproxiedDoesNotForcePrompt(t *testing.T) {
	t.Parallel()
	plugins := []PluginInfo{
		{
			Name: "Test",
			Slug: "test",
			Servers: []PluginServerInfo{
				{DisplayName: "vendor-widget", MCPURL: "https://vendor.example.com/mcp", IsUnproxied: true},
			},
		},
	}

	files, err := GeneratePluginPackages(plugins, GenerateConfig{
		OrgName:   "Test Org",
		ServerURL: "https://app.getgram.ai",
	})
	require.NoError(t, err)

	var pluginMeta claudePluginMeta
	err = json.Unmarshal(files["test/.claude-plugin/plugin.json"], &pluginMeta)
	require.NoError(t, err)
	require.NotContains(t, pluginMeta.UserConfig, "GRAM_API_KEY",
		"a plugin with only an unproxied server needs no Gram API key prompt")
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

// Codex validates .mcp.json server names against ^[a-zA-Z0-9_-]+$ at MCP
// client startup, so human display names must be sanitized into valid keys.
func TestGenerateCodexMCPServerNamesSanitized(t *testing.T) {
	t.Parallel()
	plugins := []PluginInfo{
		{
			Name: "Test",
			Slug: "test",
			Servers: []PluginServerInfo{
				{DisplayName: "Team Slack", MCPURL: "https://app.getgram.ai/mcp/team-slack"},
				{DisplayName: "Slack (Remote)", MCPURL: "https://app.getgram.ai/mcp/slack-remote"},
				{DisplayName: "already_valid-1", MCPURL: "https://app.getgram.ai/mcp/valid"},
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
	require.Len(t, mcpConfig.MCPServers, 3)

	codexNamePattern := regexp.MustCompile(`^[a-zA-Z0-9_-]+$`)
	for name := range mcpConfig.MCPServers {
		require.Regexp(t, codexNamePattern, name, "Codex rejects MCP server names outside its allowed pattern")
	}

	require.Equal(t, "https://app.getgram.ai/mcp/team-slack", mcpConfig.MCPServers["Team_Slack"].URL)
	require.Equal(t, "https://app.getgram.ai/mcp/slack-remote", mcpConfig.MCPServers["Slack_Remote"].URL)
	require.Equal(t, "https://app.getgram.ai/mcp/valid", mcpConfig.MCPServers["already_valid-1"].URL, "already-valid names must pass through unchanged")
}

// Display names that differ only in punctuation sanitize to the same key;
// later servers must get a numeric suffix instead of overwriting earlier ones.
func TestGenerateCodexMCPServerNameCollisionsDeduped(t *testing.T) {
	t.Parallel()
	plugins := []PluginInfo{
		{
			Name: "Test",
			Slug: "test",
			Servers: []PluginServerInfo{
				{DisplayName: "Notes App", MCPURL: "https://app.getgram.ai/mcp/notes-one"},
				{DisplayName: "Notes (App)", MCPURL: "https://app.getgram.ai/mcp/notes-two"},
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
	require.Len(t, mcpConfig.MCPServers, 2, "colliding names must not overwrite each other")

	require.Equal(t, "https://app.getgram.ai/mcp/notes-one", mcpConfig.MCPServers["Notes_App"].URL)
	require.Equal(t, "https://app.getgram.ai/mcp/notes-two", mcpConfig.MCPServers["Notes_App_2"].URL)
}

// An already-valid display name must keep its exact key even when an
// earlier-sorted invalid name sanitizes to the same key — the invalid name
// takes the suffix, not the valid one.
func TestGenerateCodexMCPServerValidNamesReservedOverSanitized(t *testing.T) {
	t.Parallel()
	plugins := []PluginInfo{
		{
			Name: "Test",
			Slug: "test",
			Servers: []PluginServerInfo{
				{DisplayName: "Team Slack", MCPURL: "https://app.getgram.ai/mcp/spaced"},
				{DisplayName: "Team_Slack", MCPURL: "https://app.getgram.ai/mcp/literal"},
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
	require.Len(t, mcpConfig.MCPServers, 2)

	require.Equal(t, "https://app.getgram.ai/mcp/literal", mcpConfig.MCPServers["Team_Slack"].URL, "valid name must keep its exact key")
	require.Equal(t, "https://app.getgram.ai/mcp/spaced", mcpConfig.MCPServers["Team_Slack_2"].URL, "sanitized name takes the suffix")
}

// Collision renames are bounded (_2 through _6); servers beyond that are
// dropped instead of overwriting an earlier entry.
func TestGenerateCodexMCPServerRenameAttemptsBounded(t *testing.T) {
	t.Parallel()
	servers := make([]PluginServerInfo, 8)
	for i := range servers {
		servers[i] = PluginServerInfo{
			DisplayName: "Dup Server",
			MCPURL:      fmt.Sprintf("https://app.getgram.ai/mcp/dup-%d", i),
		}
	}
	plugins := []PluginInfo{{Name: "Test", Slug: "test", Servers: servers}}

	files, err := GeneratePluginPackages(plugins, GenerateConfig{
		OrgName:   "Test Org",
		ServerURL: "https://app.getgram.ai",
	})
	require.NoError(t, err)

	var mcpConfig codexMCPConfig
	err = json.Unmarshal(files["test-codex/.mcp.json"], &mcpConfig)
	require.NoError(t, err)

	require.Len(t, mcpConfig.MCPServers, 6, "base key plus renames _2.._6, remaining collisions dropped")
	require.Equal(t, "https://app.getgram.ai/mcp/dup-0", mcpConfig.MCPServers["Dup_Server"].URL)
	require.Equal(t, "https://app.getgram.ai/mcp/dup-5", mcpConfig.MCPServers["Dup_Server_6"].URL)
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
	require.Equal(t, &marketplaceMetadata{PluginRoot: cursorPluginRoot}, cursorManifest.Metadata)
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

func TestGenerateObservabilityPluginsIncludeBootstrapLayout(t *testing.T) {
	t.Parallel()
	cfg := GenerateConfig{
		OrgName:      "Acme",
		OrgID:        "org_123",
		ServerURL:    "https://app.getgram.ai",
		HooksAPIKey:  "gram_local_secret_xyz",
		ProjectSlug:  "acme-prod",
		BrowserLogin: true,
	}
	files, err := GeneratePluginPackages(nil, cfg)
	require.NoError(t, err)

	for _, root := range []string{
		ClaudeObservabilitySlug(cfg),
		"cursor-plugins/" + CursorObservabilitySlug(cfg),
		CodexObservabilitySlug(cfg),
	} {
		require.JSONEq(t, `{
  "server_url": "https://app.getgram.ai",
  "project": "acme-prod",
  "org": "org_123",
  "hooks_api_key": "gram_local_secret_xyz",
  "browser_login": true
}`, string(files[root+"/speakeasy.json"]), "%s/speakeasy.json", root)
		require.NotEmpty(t, files[root+"/hooks/bootstrap.sh"], "%s/hooks/bootstrap.sh", root)
		require.NotContains(t, string(files[root+"/hooks/bootstrap.sh"]), cfg.HooksAPIKey)
		require.NotContains(t, string(files[root+"/hooks/hooks.json"]), cfg.HooksAPIKey)
	}

	// Only Codex invokes the PowerShell bootstrapper (via commandWindows);
	// Claude and Cursor run hooks through bash on every platform.
	require.NotEmpty(t, files[CodexObservabilitySlug(cfg)+"/hooks/bootstrap.ps1"])
	require.NotContains(t, files, ClaudeObservabilitySlug(cfg)+"/hooks/bootstrap.ps1")
	require.NotContains(t, files, "cursor-plugins/"+CursorObservabilitySlug(cfg)+"/hooks/bootstrap.ps1")
}

// Claude only invokes events listed in hooks.json. The Claude() handler in
// server/internal/hooks/claude_hooks.go records PostToolUseFailure,
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

	root := ClaudeObservabilitySlug(cfg)
	hooksJSON := files[root+"/hooks/hooks.json"]
	require.NotNil(t, hooksJSON, "claude observability hooks/hooks.json missing")

	var parsed claudeHooksConfig
	require.NoError(t, json.Unmarshal(hooksJSON, &parsed))

	for _, event := range ClaudeObservabilityHookEvents {
		matchers, ok := parsed.Hooks[event]
		require.True(t, ok, "event %q must be registered in hooks.json or Claude will silently drop it", event)
		require.Len(t, matchers, 1)
		require.Len(t, matchers[0].Hooks, 1)

		timeoutSeconds := 60
		if event == "SessionStart" {
			timeoutSeconds = 300
			require.NotNil(t, matchers[0].Hooks[0].Timeout)
			require.Equal(t, 300, *matchers[0].Hooks[0].Timeout)
		} else {
			require.Nil(t, matchers[0].Hooks[0].Timeout)
		}
		require.Equal(t,
			fmt.Sprintf(`bash "$CLAUDE_PLUGIN_ROOT/hooks/bootstrap.sh" --config="$CLAUDE_PLUGIN_ROOT/speakeasy.json" agenthooks run --provider=claude-code --timeout=%ds`, timeoutSeconds),
			matchers[0].Hooks[0].Command,
		)
		blocking := event == "UserPromptSubmit" || event == "PreToolUse" || event == "Stop" || event == "SessionStart"
		require.NotNil(t, matchers[0].Hooks[0].Async)
		require.Equal(t, !blocking, *matchers[0].Hooks[0].Async,
			"decision-capable and dispatch-fragile events block; the rest are fire-and-forget")
	}
}

func TestGenerateCursorObservabilityPluginRegistersBootstrapCommands(t *testing.T) {
	t.Parallel()
	cfg := GenerateConfig{
		OrgName:     "Acme",
		ServerURL:   "https://app.getgram.ai",
		HooksAPIKey: "gram_local_secret_xyz",
		ProjectSlug: "acme-prod",
	}
	files, err := GeneratePluginPackages(nil, cfg)
	require.NoError(t, err)

	root := "cursor-plugins/" + CursorObservabilitySlug(cfg)

	var parsed cursorHooksConfig
	require.NoError(t, json.Unmarshal(files[root+"/hooks/hooks.json"], &parsed))

	sessionStart, ok := parsed.Hooks["sessionStart"]
	require.True(t, ok, "Cursor sessionStart must be registered")
	require.Len(t, sessionStart, 1)
	require.Equal(t, `bash "$CURSOR_PLUGIN_ROOT/hooks/bootstrap.sh" --config="$CURSOR_PLUGIN_ROOT/speakeasy.json" agenthooks run --provider=cursor --timeout=330s`, sessionStart[0].Command)
	require.NotNil(t, sessionStart[0].Timeout)
	require.Equal(t, 330, *sessionStart[0].Timeout)
	require.NotNil(t, sessionStart[0].FailClosed)
	require.True(t, *sessionStart[0].FailClosed, "Cursor sessionStart must fail closed")

	// Cursor fails hooks open by default on command error/timeout; the
	// decision-capable events must opt into failClosed or an established
	// machine with broken auth (or an unreachable server) silently allows.
	blockingEvents := map[string]bool{
		"beforeSubmitPrompt": true,
		"preToolUse":         true,
		"beforeMCPExecution": true,
	}
	for _, event := range CursorObservabilityHookEvents {
		require.Contains(t, parsed.Hooks, event, "event %q must be registered", event)
		require.Len(t, parsed.Hooks[event], 1)
		require.Equal(t, `bash "$CURSOR_PLUGIN_ROOT/hooks/bootstrap.sh" --config="$CURSOR_PLUGIN_ROOT/speakeasy.json" agenthooks run --provider=cursor --timeout=60s`, parsed.Hooks[event][0].Command)
		if blockingEvents[event] {
			require.NotNil(t, parsed.Hooks[event][0].FailClosed, "blocking event %q must fail closed", event)
			require.True(t, *parsed.Hooks[event][0].FailClosed, "blocking event %q must fail closed", event)
		} else {
			require.Nil(t, parsed.Hooks[event][0].FailClosed, "observational event %q must not fail closed", event)
		}
	}
}

func TestGenerateCodexObservabilityPluginHooksJSONIncludesBootstrapCommands(t *testing.T) {
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
		groups, ok := parsed.Hooks[event]
		require.True(t, ok, "event %q must be registered in hooks.json or Codex will silently drop it", event)
		require.Len(t, groups, 1)
		require.Len(t, groups[0].Hooks, 1)

		async := event == "PostToolUse" || event == "SessionEnd" || event == "Stop"
		timeoutSeconds := 60
		switch event {
		case "SessionStart":
			timeoutSeconds = 330
		case "SessionEnd":
			timeoutSeconds = 3
		}
		require.Equal(t, codexHooksBootstrapCommand(timeoutSeconds, async, cfg.InstallFailOpen), groups[0].Hooks[0].Command)
		require.Contains(t, groups[0].Hooks[0].Command, fmt.Sprintf(`agenthooks run --provider=codex --timeout=%ds`, timeoutSeconds))
		require.Contains(t, groups[0].Hooks[0].Command, `r="$PLUGIN_ROOT"`, "the shell must expand Codex's runtime environment safely")
		require.Equal(t, codexHooksPowerShellCommand(timeoutSeconds, async, cfg.InstallFailOpen), groups[0].Hooks[0].CommandWindows)
		if event == "SessionEnd" {
			require.Equal(t, 3, groups[0].Hooks[0].Timeout)
		} else {
			require.Zero(t, groups[0].Hooks[0].Timeout)
		}
		// The Unix command wraps the script in `bash -c '...'`, so --async is
		// the last thing inside the quotes rather than the last thing on the line.
		require.Equal(t, async, strings.HasSuffix(groups[0].Hooks[0].Command, " --async'"))
		require.Equal(t, async, strings.Contains(decodePowerShellCommand(t, groups[0].Hooks[0].CommandWindows), " --async"))
	}
}

func decodePowerShellCommand(t *testing.T, command string) string {
	t.Helper()
	const prefix = `powershell.exe -NoLogo -NoProfile -ExecutionPolicy Bypass -EncodedCommand `
	require.True(t, strings.HasPrefix(command, prefix))
	encoded, err := base64.StdEncoding.DecodeString(strings.TrimPrefix(command, prefix))
	require.NoError(t, err)
	require.Zero(t, len(encoded)%2)
	units := make([]uint16, len(encoded)/2)
	for i := range units {
		units[i] = binary.LittleEndian.Uint16(encoded[2*i:])
	}
	return string(utf16.Decode(units))
}

func TestComputeCodexHookApprovalsIncludesSingleSessionStartCommand(t *testing.T) {
	t.Parallel()
	cfg := GenerateConfig{OrgName: "Acme", ServerURL: "https://app.getgram.ai"}
	marketplace := conv.ToSlug(cfg.OrgName) + "-speakeasy"
	plugin := CodexObservabilitySlug(cfg)

	approvals, err := computeCodexHookApprovals(marketplace, plugin, false)
	require.NoError(t, err)

	sessionStartPrefix := plugin + "@" + marketplace + ":hooks/hooks.json:session_start:0:"
	var sessionStartApprovals []codexHookApproval
	for _, approval := range approvals {
		if strings.HasPrefix(approval.StateKey, sessionStartPrefix) {
			sessionStartApprovals = append(sessionStartApprovals, approval)
		}
	}
	require.Len(t, sessionStartApprovals, 1, "SessionStart has one bootstrap command to approve")
	require.Equal(t, sessionStartPrefix+"0", sessionStartApprovals[0].StateKey)
	require.NotEmpty(t, sessionStartApprovals[0].TrustedHash)
}

func TestComputeCodexHookApprovalsIncludesSessionEndTrustState(t *testing.T) {
	t.Parallel()
	cfg := GenerateConfig{OrgName: "Acme", ServerURL: "https://app.getgram.ai"}
	marketplace := conv.ToSlug(cfg.OrgName) + "-speakeasy"
	plugin := CodexObservabilitySlug(cfg)

	approvals, err := computeCodexHookApprovals(marketplace, plugin, false)
	require.NoError(t, err)

	prefix := plugin + "@" + marketplace + ":hooks/hooks.json:session_end:0:"
	var sessionEndApprovals []codexHookApproval
	for _, approval := range approvals {
		if strings.HasPrefix(approval.StateKey, prefix) {
			sessionEndApprovals = append(sessionEndApprovals, approval)
		}
	}
	require.Len(t, sessionEndApprovals, 1)
	require.Equal(t, prefix+"0", sessionEndApprovals[0].StateKey)
	require.NotEmpty(t, sessionEndApprovals[0].TrustedHash)
}

// Codex hashes the bytes of its own serde_json serialization, which leaves <, >
// and & unescaped. Go's json.Marshal escapes them, so a command containing any
// of the three would hash to a value no Codex install computes and every hook
// would be discarded as modified — silently, with no output at all.
func TestComputeCodexHookHashDoesNotEscapeShellRedirections(t *testing.T) {
	t.Parallel()
	const command = `bash -c 'a 2>/dev/null && b >&2'`

	hash, err := computeCodexHookHash("SessionStart", command)
	require.NoError(t, err)

	canonical := fmt.Sprintf(
		`{"event_name":"session_start","hooks":[{"async":false,"command":%s,"timeout":600,"type":"command"}],"matcher":""}`,
		mustJSONStringUnescaped(t, command),
	)
	require.Equal(t, fmt.Sprintf("sha256:%x", sha256.Sum256([]byte(canonical))), hash)
	require.NotContains(t, canonical, `\u003e`)
}

// The trust identity is an external contract: Codex only runs a hook whose
// trusted_hash in config.toml equals the hash it recomputes from hooks.json, and
// nothing re-approves an install whose hash drifted. These digests were verified
// against codex-cli 0.147.0 by installing the generated plugin and confirming
// SessionStart and UserPromptSubmit were invoked without bypassing hook trust.
// Any change to the hook commands or hashed identity must be re-verified before
// these expectations are updated.
func TestComputeCodexHookApprovalsMatchHashesCodexAccepts(t *testing.T) {
	t.Parallel()
	cfg := GenerateConfig{OrgName: "Acme Inc", ServerURL: "https://app.example.com"}
	marketplace := conv.ToSlug(cfg.OrgName) + "-speakeasy"
	plugin := CodexObservabilitySlug(cfg)

	approvals, err := computeCodexHookApprovals(marketplace, plugin, false)
	require.NoError(t, err)

	want := map[string]string{
		"session_start":      "sha256:01aa9026f9191e84ba6d993afb74f7d0415e84714b6abe9663e732bfdf341677",
		"session_end":        "sha256:a9a6220fdd0aebc83248da0b91f500784fb48e19437d97f92c493cd264c96d05",
		"pre_tool_use":       "sha256:f1d673b42ef2ce2d16a93d2509fe08cbce64942de1ba4fab0eabe94e0bc39cbd",
		"permission_request": "sha256:ea2d44b06d6c36971d5fb9121aaedc64b1e49a65e854e284f0c6f164f60fa897",
		"post_tool_use":      "sha256:d0e7d5c001f156a748ad7471930012a6c9d3d726ceffe9d17e893a5fa743ef97",
		"user_prompt_submit": "sha256:4e18decc3bc5d57114130d3b2bb80b7c8999cc654e92a5590b489762a98878c9",
		"stop":               "sha256:a2ff79157b9cdf19f1342772f4d25ab0cc58e141d9426681c5708f00feeb3a4b",
	}
	got := make(map[string]string, len(approvals))
	for _, approval := range approvals {
		event := strings.TrimSuffix(strings.TrimPrefix(approval.StateKey, plugin+"@"+marketplace+":hooks/hooks.json:"), ":0:0")
		got[event] = approval.TrustedHash
	}
	require.Equal(t, want, got)
}

func mustJSONStringUnescaped(t *testing.T, s string) string {
	t.Helper()
	encoded, err := marshalUnescapedJSON(s)
	require.NoError(t, err)
	return string(encoded)
}

// runCodexInstallScript executes the generated install script under an
// isolated HOME containing a stub codex at ~/.local/bin (off PATH), so binary
// probing never reaches a real install on the host. The stub appends its
// arguments to the returned call log.
func runCodexInstallScript(t *testing.T, script []byte, existingConfig string) (home string, callLog string) {
	t.Helper()

	home = t.TempDir()
	callLog = filepath.Join(home, "codex-calls.log")
	stub := "#!/bin/sh\nprintf '%s\\n' \"$*\" >> \"" + callLog + "\"\n"
	require.NoError(t, os.MkdirAll(filepath.Join(home, ".local", "bin"), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(home, ".local", "bin", "codex"), []byte(stub), 0o755))

	if existingConfig != "" {
		require.NoError(t, os.MkdirAll(filepath.Join(home, ".codex"), 0o755))
		require.NoError(t, os.WriteFile(filepath.Join(home, ".codex", "config.toml"), []byte(existingConfig), 0o644))
	}

	execCodexInstallScript(t, script, home)

	return home, callLog
}

func execCodexInstallScript(t *testing.T, script []byte, home string) {
	t.Helper()

	bashPath, err := exec.LookPath("bash")
	require.NoError(t, err, "bash is required to run the generated install script")

	scriptDir := t.TempDir()
	scriptPath := filepath.Join(scriptDir, "install.sh")
	require.NoError(t, os.WriteFile(scriptPath, script, 0o755))

	cmd := exec.Command(bashPath, scriptPath)
	// Run from outside the repository so nothing the script executes can pick
	// up repo-local configuration by walking up from the working directory.
	cmd.Dir = scriptDir
	cmd.Env = []string{
		"HOME=" + home,
		"PATH=" + realPythonBinDir(t) + ":/usr/bin:/bin",
	}
	out, err := cmd.CombinedOutput()
	require.NoError(t, err, "install script failed: %s", out)
}

// realPythonBinDir returns a directory whose only entry is a python3 symlink
// pointing at the real interpreter. exec.LookPath can resolve python3 to a
// version-manager shim (e.g. mise), and putting the shim directory on the
// subprocess PATH breaks the test's isolation twice over: the directory holds
// shims for every managed tool (including codex, defeating the stub), and each
// shim re-execs the manager, which under the scrubbed HOME cannot see the
// user's per-machine trust state and refuses to run at all. Asking the
// interpreter for sys.executable pierces any shim.
func realPythonBinDir(t *testing.T) string {
	t.Helper()

	pythonPath, err := exec.LookPath("python3")
	require.NoError(t, err, "python3 is required by the generated install script")
	out, err := exec.Command(pythonPath, "-c", "import sys; print(sys.executable)").Output()
	require.NoError(t, err, "resolve the real python3 interpreter")
	realPython := strings.TrimSpace(string(out))
	require.NotEmpty(t, realPython, "sys.executable must not be empty")

	dir := t.TempDir()
	require.NoError(t, os.Symlink(realPython, filepath.Join(dir, "python3")))
	return dir
}

func seededCodexInstallConfig(plugin, marketplace string, approvals []codexHookApproval) string {
	var b strings.Builder
	b.WriteString("[features]\nhooks = true\nplugin_hooks = true\njs_repl = true\n\n")
	b.WriteString("[hooks.state]\n\n")
	for _, approval := range approvals {
		fmt.Fprintf(&b, "[hooks.state.%q]\nenabled = true\ntrusted_hash = %q\n\n", approval.StateKey, approval.TrustedHash)
	}
	fmt.Fprintf(&b, "[plugins.%q]\nenabled = true\n", plugin+"@"+marketplace)
	return b.String()
}

func runCodexInstallScriptTimes(t *testing.T, script []byte, existingConfig string, times int) string {
	t.Helper()
	require.Positive(t, times)

	home, _ := runCodexInstallScript(t, script, existingConfig)
	for range times - 1 {
		execCodexInstallScript(t, script, home)
	}

	return string(requireFileBytes(t, filepath.Join(home, ".codex", "config.toml")))
}

func countTableHeaderLines(config, header string) int {
	pattern := regexp.MustCompile(`(?m)^` + regexp.QuoteMeta(header) + `(?:\s*(?:#.*)?)?\s*$`)
	return len(pattern.FindAllStringIndex(config, -1))
}

func countTableKeyLines(config, tableHeader, key string) int {
	bounds := regexp.MustCompile(`(?m)^` + regexp.QuoteMeta(tableHeader) + `(?:\s*(?:#.*)?)?\n`).FindStringIndex(config)
	if bounds == nil {
		return 0
	}
	body := config[bounds[1]:]
	if next := regexp.MustCompile(`(?m)^\[`).FindStringIndex(body); next != nil {
		body = body[:next[0]]
	}
	pattern := regexp.MustCompile(`(?m)^` + regexp.QuoteMeta(key) + `\s*=`)
	return len(pattern.FindAllStringIndex(body, -1))
}

func hooksBootstrapArchive(t *testing.T, binary []byte) []byte {
	t.Helper()
	var buf bytes.Buffer
	require.NoError(t, writePluginZip(&buf, map[string][]byte{"speakeasy-hooks": binary}))
	return buf.Bytes()
}

func currentHooksBootstrapTarget(t *testing.T) string {
	t.Helper()
	require.Contains(t, []string{"darwin", "linux"}, runtime.GOOS, "Unix bootstrap execution test")
	arch := runtime.GOARCH
	require.Contains(t, []string{"amd64", "arm64"}, arch, "unsupported test architecture")
	return runtime.GOOS + "-" + arch
}

func runHooksBootstrap(t *testing.T, script []byte, cache, stdin string, args ...string) ([]byte, error) {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, "bootstrap.sh")
	if err := os.WriteFile(path, script, 0o755); err != nil {
		return nil, fmt.Errorf("write hooks bootstrap: %w", err)
	}
	cmd := exec.Command("bash", append([]string{path}, args...)...)
	cmd.Stdin = strings.NewReader(stdin)
	cmd.Env = append(os.Environ(), "GRAM_HOOKS_HOME="+cache)
	out, err := cmd.CombinedOutput()
	if err != nil {
		return out, fmt.Errorf("run hooks bootstrap: %w", err)
	}
	return out, nil
}

func TestHooksBootstrapColdAndWarmPathsPreserveInput(t *testing.T) {
	t.Parallel()
	target := currentHooksBootstrapTarget(t)
	archive := hooksBootstrapArchive(t, []byte("#!/bin/sh\nprintf 'args:%s\\n' \"$*\"\ncat\n"))
	sum := sha256.Sum256(archive)
	var downloads atomic.Int64
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		downloads.Add(1)
		_, _ = w.Write(archive)
	}))

	script := renderHooksBootstrapForRelease("test-version", map[string]hooksBinaryTarget{
		target: {URL: server.URL + "/hooks.zip", SHA256: fmt.Sprintf("%x", sum)},
	}, false, "releases.test")
	cache := t.TempDir()
	first, err := runHooksBootstrap(t, script, cache, "cold-input", "--first", "value")
	require.NoError(t, err, string(first))
	require.Equal(t, "args:--first value\ncold-input", string(first))

	server.Close()
	second, err := runHooksBootstrap(t, script, cache, "warm-input", "--second")
	require.NoError(t, err, string(second))
	require.Equal(t, "args:--second\nwarm-input", string(second))
	require.Equal(t, int64(1), downloads.Load())

	// The PowerShell bootstrapper writes the marker into the same cache; a
	// missing trailing newline or a CRLF ending must not invalidate it (the
	// server is closed, so a re-download would fail).
	markerPath := filepath.Join(cache, "test-version", target, "archive.sha256")
	markerContent, err := os.ReadFile(markerPath)
	require.NoError(t, err)
	sha := strings.TrimSpace(string(markerContent))
	for _, variant := range []string{sha, sha + "\r\n"} {
		require.NoError(t, os.WriteFile(markerPath, []byte(variant), 0o644))
		out, err := runHooksBootstrap(t, script, cache, "still-warm")
		require.NoError(t, err, string(out))
		require.Equal(t, "args:\nstill-warm", string(out))
	}
}

func TestCodexBootstrapPersistsCompletePayloadBeforeRelayExecution(t *testing.T) {
	t.Parallel()
	target := currentHooksBootstrapTarget(t)
	archive := hooksBootstrapArchive(t, []byte("#!/bin/sh\nprintf 'args:%s\\n' \"$*\"\n"))
	sum := sha256.Sum256(archive)
	var downloads atomic.Int64
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		downloads.Add(1)
		_, _ = w.Write(archive)
	}))

	script := renderHooksBootstrapForRelease("test-version", map[string]hooksBinaryTarget{
		target: {URL: server.URL + "/hooks.zip", SHA256: fmt.Sprintf("%x", sum)},
	}, false, "releases.test")
	base := t.TempDir()
	root := filepath.Join(base, "plugins", "cache", "acme-speakeasy", "acme-observability-codex", "0.29.100")
	require.NoError(t, os.MkdirAll(filepath.Join(root, "hooks"), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(root, "hooks", "bootstrap.sh"), script, 0o755))
	config := []byte("{\"deployment\":\"test\"}\n")
	require.NoError(t, os.WriteFile(filepath.Join(root, "speakeasy.json"), config, 0o600))
	data := filepath.Join(base, "plugins", "data", "acme-observability-codex-acme-speakeasy")
	cache := filepath.Join(base, "hooks-binary-cache")
	command := codexHookCommandString(60, false, false)

	stdout, stderr, code := codexHookCommandProbe(t, command, root, data, "GRAM_HOOKS_HOME="+cache)
	require.Equal(t, 0, code, "stderr: %s", stderr)
	stable := filepath.Join(data, filepath.FromSlash(codexHooksStablePayloadSubdir))
	currentGeneration := strings.TrimSpace(string(requireFileBytes(t, filepath.Join(stable, ".unix-current"))))
	stableGeneration := filepath.Join(stable, "generations", currentGeneration)
	stableConfig := filepath.Join(stableGeneration, "speakeasy.json")
	require.Contains(t, stdout, "--config="+stableConfig,
		"the first relay execution must no longer depend on the disposable config")
	require.Equal(t, script, requireFileBytes(t, filepath.Join(stableGeneration, "hooks", "bootstrap.sh")))
	require.Equal(t, config, requireFileBytes(t, stableConfig))
	configInfo, err := os.Stat(stableConfig)
	require.NoError(t, err)
	require.Equal(t, os.FileMode(0o600), configInfo.Mode().Perm())

	refreshedScript := bytes.Replace(script, []byte("set -eu\n"), []byte("set -eu\nprintf 'refreshed-bootstrap\\n' >&2\n"), 1)
	refreshedConfig := []byte("{\"deployment\":\"refreshed\"}\n")
	require.NoError(t, os.WriteFile(filepath.Join(root, "hooks", "bootstrap.sh"), refreshedScript, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(root, "speakeasy.json"), refreshedConfig, 0o600))
	persistLock := filepath.Join(stable, ".persist-unix.lock")
	require.NoError(t, os.Mkdir(persistLock, 0o700))
	require.NoError(t, os.WriteFile(filepath.Join(persistLock, "pid"), fmt.Appendf(nil, "%d\n", os.Getpid()), 0o600))
	old := time.Now().Add(-10 * time.Minute)
	require.NoError(t, os.Chtimes(persistLock, old, old))
	stdout, stderr, code = codexHookCommandProbe(t, command, root, data, "GRAM_HOOKS_HOME="+cache)
	require.Equal(t, 0, code, "stderr: %s", stderr)
	require.Contains(t, stderr, "refreshed-bootstrap",
		"the hook that refreshes the bundle must immediately execute the new bootstrap")
	currentGeneration = strings.TrimSpace(string(requireFileBytes(t, filepath.Join(stable, ".unix-current"))))
	stableGeneration = filepath.Join(stable, "generations", currentGeneration)
	stableConfig = filepath.Join(stableGeneration, "speakeasy.json")
	require.Contains(t, stdout, "--config="+stableConfig,
		"the refreshed bootstrap must execute with the config from its immutable generation")
	require.Equal(t, refreshedScript, requireFileBytes(t, filepath.Join(stableGeneration, "hooks", "bootstrap.sh")))
	require.Equal(t, refreshedConfig, requireFileBytes(t, stableConfig))
	require.NoDirExists(t, persistLock, "an expired lock must be reclaimed even if its PID was reused")

	newestScript := bytes.Replace(script, []byte("set -eu\n"), []byte("set -eu\nprintf 'newest-bootstrap\\n' >&2\n"), 1)
	newestConfig := []byte("{\"deployment\":\"newest\"}\n")
	require.NoError(t, os.WriteFile(filepath.Join(root, "hooks", "bootstrap.sh"), newestScript, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(root, "speakeasy.json"), newestConfig, 0o600))
	require.NoError(t, os.Mkdir(persistLock, 0o700))
	require.NoError(t, os.WriteFile(filepath.Join(persistLock, "pid"), []byte("99999999\n"), 0o600))
	stdout, stderr, code = codexHookCommandProbe(t, command, root, data, "GRAM_HOOKS_HOME="+cache)
	require.Equal(t, 0, code, "stderr: %s", stderr)
	currentGeneration = strings.TrimSpace(string(requireFileBytes(t, filepath.Join(stable, ".unix-current"))))
	stableGeneration = filepath.Join(stable, "generations", currentGeneration)
	stableConfig = filepath.Join(stableGeneration, "speakeasy.json")
	require.Contains(t, stdout, "--config="+stableConfig)
	require.Equal(t, newestScript, requireFileBytes(t, filepath.Join(stableGeneration, "hooks", "bootstrap.sh")))
	require.Equal(t, newestConfig, requireFileBytes(t, stableConfig))
	require.NoDirExists(t, persistLock, "an old ownerless persistence lock must be reclaimed")

	require.NoError(t, os.RemoveAll(filepath.Dir(filepath.Dir(root))))
	server.Close()
	stdout, stderr, code = codexHookCommandProbe(t, command, root, data, "GRAM_HOOKS_HOME="+cache)
	require.Equal(t, 0, code, "stderr: %s", stderr)
	require.Contains(t, stdout, "--config="+stableConfig)
	require.Equal(t, int64(1), downloads.Load(), "stable fallback must reuse the verified binary cache")
}

func TestHooksBootstrapChecksumMismatchNeverExecutes(t *testing.T) {
	t.Parallel()
	target := currentHooksBootstrapTarget(t)
	marker := filepath.Join(t.TempDir(), "executed")
	archive := hooksBootstrapArchive(t, fmt.Appendf(nil, "#!/bin/sh\ntouch %q\n", marker))
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write(archive)
	}))
	defer server.Close()

	script := renderHooksBootstrapForRelease("bad-checksum", map[string]hooksBinaryTarget{
		target: {URL: server.URL + "/hooks.zip", SHA256: strings.Repeat("0", 64)},
	}, false, "releases.test")
	out, err := runHooksBootstrap(t, script, t.TempDir(), "payload")
	require.Error(t, err)
	require.Contains(t, string(out), "checksum mismatch")
	require.NoFileExists(t, marker)
}

func TestHooksBootstrapInstallFailOpenExitsZeroWithoutExecuting(t *testing.T) {
	t.Parallel()
	target := currentHooksBootstrapTarget(t)
	marker := filepath.Join(t.TempDir(), "executed")
	archive := hooksBootstrapArchive(t, fmt.Appendf(nil, "#!/bin/sh\ntouch %q\n", marker))
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write(archive)
	}))
	defer server.Close()

	script := renderHooksBootstrapForRelease("fail-open", map[string]hooksBinaryTarget{
		target: {URL: server.URL + "/hooks.zip", SHA256: strings.Repeat("0", 64)},
	}, true, "releases.test")
	out, err := runHooksBootstrap(t, script, t.TempDir(), "payload")
	require.NoError(t, err, string(out))
	require.Contains(t, string(out), "checksum mismatch")
	require.NoFileExists(t, marker)
}

func TestHooksBootstrapBakesInstallFailurePolicy(t *testing.T) {
	t.Parallel()
	// The baked exit code is the publish-time snapshot of hooks_fail_open: a
	// cold install has no binary (and no cached org settings) to consult, so
	// only the bootstrap exit code can honor the org's outage tolerance.
	require.Contains(t, string(renderHooksBootstrap(GenerateConfig{InstallFailOpen: true})), "install_failure_exit=0")
	require.Contains(t, string(renderHooksPowerShellBootstrap(GenerateConfig{InstallFailOpen: true})), "$InstallFailureExit = 0")
	require.Contains(t, string(renderHooksBootstrap(GenerateConfig{})), "install_failure_exit=1")
	require.Contains(t, string(renderHooksPowerShellBootstrap(GenerateConfig{})), "$InstallFailureExit = 1")
}

func TestHooksPowerShellBootstrapRecoversStalePersistenceLock(t *testing.T) {
	t.Parallel()
	script := string(renderHooksPowerShellBootstrap(GenerateConfig{}))
	require.Contains(t, script, `function Enter-PersistenceLock`)
	require.Contains(t, script, `Get-Process -Id $OwnerPID`)
	require.Contains(t, script, `Move-Item -LiteralPath $LockPath -Destination $Reap`)
}

func TestCarryHooksSubtreeIsLayoutIndependent(t *testing.T) {
	t.Parallel()
	prefixes := hooksSubtreePrefixes("Acme")
	published := map[string][]byte{
		prefixes[0] + "hooks/hook.sh":                []byte("v14 claude"),
		prefixes[0] + ".claude-plugin/plugin.json":   []byte("{}"),
		prefixes[1] + "hooks/hook.sh":                []byte("v14 cursor"),
		prefixes[2] + "hooks/hook.sh":                []byte("v14 codex"),
		prefixes[3] + "plugin/agenthooks.ts":         []byte("v14 opencode"),
		"some-mcp-plugin/.claude-plugin/plugin.json": []byte("{}"),
	}

	dst := map[string][]byte{}
	carriedOrg, carried := carryHooksSubtree(dst, published, []byte(`{"org_name":"Acme"}`), "Renamed Since Publish")
	require.True(t, carried)
	require.Equal(t, "Acme", carriedOrg)
	require.Len(t, dst, 5)
	require.Equal(t, []byte("v14 claude"), dst[prefixes[0]+"hooks/hook.sh"])
	require.NotContains(t, dst, "some-mcp-plugin/.claude-plugin/plugin.json")

	// The regenerated shared manifests must reference the carried directories
	// (published under the old org name), not the renamed org's paths.
	sharedCfg := GenerateConfig{
		OrgName:      "Renamed Since Publish",
		HooksAPIKey:  "gram_hooks_test",
		HooksOrgName: carriedOrg,
	}
	shared, err := generateSharedFiles(nil, sharedCfg)
	require.NoError(t, err)
	require.Contains(t, string(shared[".claude-plugin/marketplace.json"]), `"./`+ClaudeObservabilitySlug(sharedCfg)+`"`)
	require.True(t, strings.HasPrefix(ClaudeObservabilitySlug(sharedCfg)+"/", prefixes[0]))

	// A platform subtree missing from the published repo means the component
	// is not recoverable and the caller must regenerate.
	delete(published, prefixes[2]+"hooks/hook.sh")
	_, carried = carryHooksSubtree(map[string][]byte{}, published, []byte(`{"org_name":"Acme"}`), "Acme")
	require.False(t, carried)
}

func TestHooksBootstrapConcurrentColdInvocationsDownloadOnce(t *testing.T) {
	t.Parallel()
	target := currentHooksBootstrapTarget(t)
	archive := hooksBootstrapArchive(t, []byte("#!/bin/sh\ncat\n"))
	sum := sha256.Sum256(archive)
	var downloads atomic.Int64
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		downloads.Add(1)
		_, _ = w.Write(archive)
	}))
	defer server.Close()

	script := renderHooksBootstrapForRelease("concurrent", map[string]hooksBinaryTarget{
		target: {URL: server.URL + "/hooks.zip", SHA256: fmt.Sprintf("%x", sum)},
	}, false, "releases.test")
	cache := t.TempDir()
	var wg sync.WaitGroup
	errs := make(chan error, 2)
	for range 2 {
		wg.Go(func() {
			out, err := runHooksBootstrap(t, script, cache, "same-event")
			if err == nil && string(out) != "same-event" {
				err = fmt.Errorf("unexpected output %q", out)
			}
			errs <- err
		})
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		require.NoError(t, err)
	}
	require.Equal(t, int64(1), downloads.Load())
}

func TestHooksBootstrapRecoversStaleInstallLock(t *testing.T) {
	t.Parallel()
	target := currentHooksBootstrapTarget(t)
	archive := hooksBootstrapArchive(t, []byte("#!/bin/sh\ncat\n"))
	sum := sha256.Sum256(archive)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write(archive)
	}))
	defer server.Close()

	cache := t.TempDir()
	lock := filepath.Join(cache, "stale-lock", target+".lock")
	require.NoError(t, os.MkdirAll(lock, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(lock, "pid"), []byte("99999999\n"), 0o644))
	script := renderHooksBootstrapForRelease("stale-lock", map[string]hooksBinaryTarget{
		target: {URL: server.URL + "/hooks.zip", SHA256: fmt.Sprintf("%x", sum)},
	}, false, "releases.test")
	out, err := runHooksBootstrap(t, script, cache, "event")
	require.NoError(t, err, string(out))
	require.Equal(t, "event", string(out))
}

func TestHooksBootstrapChecksumChangeInvalidatesWarmCache(t *testing.T) {
	t.Parallel()
	target := currentHooksBootstrapTarget(t)
	firstArchive := hooksBootstrapArchive(t, []byte("#!/bin/sh\nprintf first\n"))
	firstSum := sha256.Sum256(firstArchive)
	firstServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write(firstArchive)
	}))
	defer firstServer.Close()
	secondArchive := hooksBootstrapArchive(t, []byte("#!/bin/sh\nprintf second\n"))
	secondSum := sha256.Sum256(secondArchive)
	secondServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write(secondArchive)
	}))
	defer secondServer.Close()

	cache := t.TempDir()
	firstScript := renderHooksBootstrapForRelease("same-version", map[string]hooksBinaryTarget{
		target: {URL: firstServer.URL + "/hooks.zip", SHA256: fmt.Sprintf("%x", firstSum)},
	}, false, "releases.test")
	out, err := runHooksBootstrap(t, firstScript, cache, "")
	require.NoError(t, err, string(out))
	require.Equal(t, "first", string(out))

	secondScript := renderHooksBootstrapForRelease("same-version", map[string]hooksBinaryTarget{
		target: {URL: secondServer.URL + "/hooks.zip", SHA256: fmt.Sprintf("%x", secondSum)},
	}, false, "releases.test")
	out, err = runHooksBootstrap(t, secondScript, cache, "")
	require.NoError(t, err, string(out))
	require.Equal(t, "second", string(out))
}

func TestHooksBootstrapEmbedsPinnedReleaseMetadata(t *testing.T) {
	t.Parallel()
	require.Len(t, hooksBinaryTargets, 6)
	cfg := GenerateConfig{ServerURL: "https://app.getgram.ai"}
	script := string(renderHooksBootstrap(cfg))
	served := hooksServedTargets(cfg.ServerURL)
	for target, asset := range served {
		require.Contains(t, script, target)
		require.Equal(t, "https://app.getgram.ai/hooks/releases/"+hooksBinaryVersion+"/speakeasy-hooks_"+strings.ReplaceAll(target, "-", "_")+".zip", asset.URL)
		require.Len(t, asset.SHA256, 64)
		require.Contains(t, script, asset.URL)
		require.Contains(t, script, asset.SHA256)
		// Upstream (server-side fetch) stays pinned to the immutable GitHub
		// release; only the client-facing URLs point at the Gram domain.
		require.Contains(t, hooksBinaryTargets[target].URL, "https://github.com/speakeasy-api/gram/releases/download/hooks%40"+hooksBinaryVersion+"/")
		require.Equal(t, hooksBinaryTargets[target].SHA256, asset.SHA256)
	}
	// Cold-install failures must steer administrators toward the org's own
	// server domain — the whole point of serving artifacts from it is that
	// GitHub may be unreachable (sandboxed harnesses).
	require.Contains(t, script, "allow downloads from app.getgram.ai")
	require.NotContains(t, script, "github.com")
	require.Contains(t, string(renderHooksPowerShellBootstrap(cfg)), "allow downloads from app.getgram.ai")
	require.NotContains(t, string(renderHooksPowerShellBootstrap(cfg)), "github.com")
}

func TestHooksConfigHashTracksBinaryReleaseMetadata(t *testing.T) {
	t.Parallel()
	current := hooksConfigSnapshot(GenerateConfig{OrgName: "Acme"})

	changedVersion := current
	changedVersion.BinaryVersion = "0.2.0"
	require.NotEqual(t, hooksConfigHash(current), hooksConfigHash(changedVersion))

	changedTargets := current
	changedTargets.BinaryTargets = make(map[string]hooksBinaryTarget, len(current.BinaryTargets))
	maps.Copy(changedTargets.BinaryTargets, current.BinaryTargets)
	asset := changedTargets.BinaryTargets["linux-amd64"]
	asset.SHA256 = strings.Repeat("f", 64)
	changedTargets.BinaryTargets["linux-amd64"] = asset
	require.NotEqual(t, hooksConfigHash(current), hooksConfigHash(changedTargets))
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
	for _, platform := range []string{"claude", "cursor", "codex", "opencode"} {
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

// OpenCode has no hooks.json or plugin manifest — the shim under plugin/ is the
// whole hook registration — so a regression there is silent. Pin that the
// package ships the shim wired to serve mode and the sibling speakeasy.json.
func TestGenerateOpenCodeObservabilityPluginPackage(t *testing.T) {
	t.Parallel()
	cfg := GenerateConfig{
		OrgName:     "Acme",
		ServerURL:   "https://app.getgram.ai",
		HooksAPIKey: "gram_local_secret_xyz",
	}
	files, err := GenerateObservabilityPluginPackage(cfg, "opencode")
	require.NoError(t, err)

	shim, ok := files["plugin/agenthooks.ts"]
	require.True(t, ok, "opencode package must ship plugin/agenthooks.ts")
	require.Contains(t, string(shim), "--provider=opencode")
	require.Contains(t, string(shim), "speakeasy.json")
	require.Contains(t, string(shim), "bootstrap.sh")
	require.Contains(t, string(shim), `"x-gram-agent-provider": "opencode"`)
	require.Contains(t, string(shim), `"x-gram-agent-turn-id": messageID`)

	_, ok = files["speakeasy.json"]
	require.True(t, ok, "opencode package must ship speakeasy.json alongside the shim")
	_, ok = files["hooks/bootstrap.sh"]
	require.True(t, ok, "opencode package must ship the hooks bootstrapper the shim spawns")
}

// An upgraded install already carries [hooks.state] entries whose trusted_hash
// was computed against the previous hook command. When the command changes
// (for example, when a bootstrap argument changes) the installer must
// rewrite those hashes in place, otherwise Codex flags the hooks as modified
// and silently stops running telemetry until the user re-approves them.
func TestGenerateCodexInstallScriptRefreshesStaleTrustedHashes(t *testing.T) {
	t.Parallel()

	cfg := GenerateConfig{OrgName: "Acme", ServerURL: "https://app.getgram.ai"}
	marketplace := conv.ToSlug(cfg.OrgName) + "-speakeasy"
	plugin := CodexObservabilitySlug(cfg)

	approvals, err := computeCodexHookApprovals(marketplace, plugin, false)
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

// The unified ChatGPT desktop app (Chat + Work + Codex modes) that OpenAI
// merged the standalone Codex app into on 2026-07-09 ships the codex CLI at a
// different bundle path. Without this probe the script silently degrades to
// "codex executable not found" manual instructions on every machine that only
// has the post-merge app — the common case now that the legacy app is frozen.
// find_codex walks a candidate list in order when no codex is on PATH. Run
// the script against two seeded candidates and observe which binary it
// actually invokes, so this covers the lookup and its precedence rather than
// the generated text.
func TestGenerateCodexInstallScriptResolvesCodexByProbeOrder(t *testing.T) {
	t.Parallel()

	cfg := GenerateConfig{OrgName: "Acme", ServerURL: "https://app.getgram.ai"}
	script, err := GenerateCodexInstallScript("https://example.com/gram-marketplace", cfg)
	require.NoError(t, err)

	home := t.TempDir()
	callLog := filepath.Join(home, "codex-calls.log")
	for marker, dir := range map[string]string{
		// Candidate 1: the standalone package install.
		"standalone-wins": filepath.Join(home, ".codex", "packages", "standalone", "current", "bin"),
		// Candidate 2: ~/.local/bin, which must lose to the earlier match.
		"local-bin-ran": filepath.Join(home, ".local", "bin"),
	} {
		require.NoError(t, os.MkdirAll(dir, 0o755))
		stub := "#!/bin/sh\nprintf '" + marker + "\\n' >> \"" + callLog + "\"\n"
		require.NoError(t, os.WriteFile(filepath.Join(dir, "codex"), []byte(stub), 0o755))
	}

	execCodexInstallScript(t, script, home)

	calls := string(requireFileBytes(t, callLog))
	require.Contains(t, calls, "standalone-wins", "the first matching candidate must be the one invoked")
	require.NotContains(t, calls, "local-bin-ran", "a later candidate must not run once an earlier one matches")
}

// The /Applications candidates cannot be executed here — a test cannot place
// a bundle under the system Applications directory — so the candidate list is
// read out of the generated script and asserted whole. That pins both which
// paths are probed and the order they are tried in, and a failure prints the
// actual list rather than an index comparison.
//
// ChatGPT.app leads the bundle pair because OpenAI merged the standalone
// Codex app into the unified ChatGPT app, which is where the maintained codex
// CLI now ships (verified by hand against 0.146, DNO-737); the frozen legacy
// bundle stays as a fallback for machines that never migrated. Dropping the
// unified entry silently returns install to "codex executable not found" on
// any post-merge machine.
func TestGenerateCodexInstallScriptProbesCandidatesInOrder(t *testing.T) {
	t.Parallel()

	cfg := GenerateConfig{OrgName: "Acme", ServerURL: "https://app.getgram.ai"}
	script, err := GenerateCodexInstallScript("https://example.com/gram-marketplace", cfg)
	require.NoError(t, err)

	require.Equal(t, []string{
		"${codex_home}/packages/standalone/current/bin/codex",
		"${HOME}/.local/bin/codex",
		"/usr/local/bin/codex",
		"/Applications/ChatGPT.app/Contents/Resources/codex",
		"/Applications/Codex.app/Contents/Resources/codex",
	}, codexProbeCandidates(t, script))
}

// codexProbeCandidates reads find_codex's ordered candidate list out of the
// generated script.
func codexProbeCandidates(t *testing.T, script []byte) []string {
	t.Helper()

	const marker = "for candidate in"
	start := strings.Index(string(script), marker)
	require.Positive(t, start, "generated script must declare a candidate list")
	body := string(script)[start+len(marker):]
	end := strings.Index(body, "; do")
	require.Positive(t, end, "candidate list must terminate with '; do'")

	candidates := make([]string, 0, 8)
	for field := range strings.FieldsSeq(body[:end]) {
		field = strings.Trim(field, "\\\"")
		if field != "" {
			candidates = append(candidates, field)
		}
	}
	return candidates
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

// Running install.sh twice against a config that already contains every entry
// the script writes must not duplicate TOML keys — duplicate keys make Codex
// refuse to load config.toml entirely.
func TestGenerateCodexInstallScriptIsIdempotent(t *testing.T) {
	t.Parallel()

	cfg := GenerateConfig{OrgName: "Acme", ServerURL: "https://app.getgram.ai"}
	marketplace := conv.ToSlug(cfg.OrgName) + "-speakeasy"
	plugin := CodexObservabilitySlug(cfg)

	approvals, err := computeCodexHookApprovals(marketplace, plugin, false)
	require.NoError(t, err)
	require.NotEmpty(t, approvals)

	script, err := GenerateCodexInstallScript("https://example.com/gram-marketplace", cfg)
	require.NoError(t, err)

	seeded := seededCodexInstallConfig(plugin, marketplace, approvals)
	patched := runCodexInstallScriptTimes(t, script, seeded, 2)

	var decoded map[string]any
	_, err = toml.Decode(patched, &decoded)
	require.NoError(t, err, "patched config.toml must remain valid TOML without duplicate keys")

	require.Equal(t, 1, countTableHeaderLines(patched, "[features]"))
	require.Equal(t, 1, countTableKeyLines(patched, "[features]", "hooks"))
	require.Equal(t, 1, countTableKeyLines(patched, "[features]", "plugin_hooks"))
	require.Equal(t, 1, countTableHeaderLines(patched, "[hooks.state]"))
	require.Equal(t, 1, countTableHeaderLines(patched, fmt.Sprintf(`[plugins."%s@%s"]`, plugin, marketplace)))
	require.Equal(t, 1, countTableKeyLines(patched, fmt.Sprintf(`[plugins."%s@%s"]`, plugin, marketplace), "enabled"))

	for _, approval := range approvals {
		section := fmt.Sprintf(`[hooks.state."%s"]`, approval.StateKey)
		require.Equal(t, 1, countTableHeaderLines(patched, section), "hook approval section %q must appear exactly once", section)
		require.Equal(t, 1, countTableKeyLines(patched, section, "enabled"))
		require.Equal(t, 1, countTableKeyLines(patched, section, "trusted_hash"))
		require.Contains(t, patched, approval.TrustedHash)
	}

	require.Contains(t, patched, "js_repl = true", "pre-existing table entries must be preserved")
}

// A table header sitting at EOF without a trailing newline is still an
// existing table — the script must insert its entries under that header
// instead of appending a duplicate one, which would make Codex refuse to
// load config.toml entirely.
func TestGenerateCodexInstallScriptReusesTableHeaderAtEOFWithoutNewline(t *testing.T) {
	t.Parallel()

	cfg := GenerateConfig{OrgName: "Acme", ServerURL: "https://app.getgram.ai"}
	script, err := GenerateCodexInstallScript("https://example.com/gram-marketplace", cfg)
	require.NoError(t, err)

	patched := runCodexInstallScriptTimes(t, script, "js_repl = true\n\n[features]", 2)

	var decoded map[string]any
	_, err = toml.Decode(patched, &decoded)
	require.NoError(t, err, "patched config.toml must remain valid TOML without duplicate tables")

	require.Equal(t, 1, countTableHeaderLines(patched, "[features]"))
	require.Equal(t, 1, countTableKeyLines(patched, "[features]", "hooks"))
	require.Equal(t, 1, countTableKeyLines(patched, "[features]", "plugin_hooks"))
	require.Contains(t, patched, "js_repl = true", "pre-existing root-level entries must be preserved")
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

// Bootstrap and install scripts in ZIPs must carry the execute bit, otherwise
// extracting the archive leaves them unrunnable. Mirrors the GitHub publish
// path's mode 100755 in thirdparty/github/repo.go.
func TestWritePluginZipMakesShellScriptsExecutable(t *testing.T) {
	t.Parallel()
	files := map[string][]byte{
		"hooks/bootstrap.sh":         []byte("#!/usr/bin/env bash\necho hi\n"),
		"install.sh":                 []byte("#!/usr/bin/env bash\necho hi\n"),
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

	require.Equal(t, uint32(0o755), modes["hooks/bootstrap.sh"], "bootstrap.sh must be executable after unzip")
	require.Equal(t, uint32(0o755), modes["install.sh"], "install.sh must be executable after unzip")
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
		Version:     "1747087200",
	}

	files, err := GeneratePluginPackages(plugins, cfg)
	require.NoError(t, err)

	readVersion := func(p string) string {
		raw, ok := files[p]
		require.True(t, ok, "missing manifest: %s", p)
		var meta struct {
			Version string `json:"version"`
		}
		require.NoError(t, json.Unmarshal(raw, &meta), "parse %s", p)
		return meta.Version
	}

	// Every per-plugin (MCP) plugin.json across all three platforms carries the
	// per-publish cfg.Version so platform marketplaces refresh on republish.
	mcpManifestPaths := []string{
		"engineering-tools/.claude-plugin/plugin.json",
		"cursor-plugins/engineering-tools-cursor/.cursor-plugin/plugin.json",
		"engineering-tools-codex/.codex-plugin/plugin.json",
	}
	for _, p := range mcpManifestPaths {
		require.Equal(t, "0.1.1747087200", readVersion(p), "%s did not pick up cfg.Version", p)
	}

	// The observability (hooks) plugin.json carries the hooks generator version
	// as its minor component and the same publish epoch as its patch. Stability
	// across MCP-only publishes comes from the publish path carrying the hooks
	// subtree verbatim, not from deterministic rendering.
	observabilityManifestPaths := []string{
		"acme-observability/.claude-plugin/plugin.json",
		"cursor-plugins/acme-observability-cursor/.cursor-plugin/plugin.json",
		"acme-observability-codex/.codex-plugin/plugin.json",
	}
	for _, p := range observabilityManifestPaths {
		require.Equal(t, hooksManifestVersion(cfg), readVersion(p), "%s must use the hooks manifest version", p)
		require.Equal(t, "0."+hooksGeneratorVersion+".1747087200", readVersion(p),
			"%s must carry the generator version and the publish epoch", p)
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
	first.Version = "100"
	firstFiles, err := GeneratePluginPackages(plugins, first)
	require.NoError(t, err)

	second := base
	second.Version = "200"
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
	require.Equal(t, "0.1.42", pluginManifestVersion(GenerateConfig{Version: "42"}))
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

func TestMCPFingerprintsIsStableAcrossCalls(t *testing.T) {
	t.Parallel()
	cfg := GenerateConfig{OrgName: "Acme Corp", ServerURL: "https://app.getgram.ai", ProjectSlug: "acme"}

	first, err := MCPFingerprints(fingerprintTestPlugins(), cfg)
	require.NoError(t, err)
	// One entry per plugin plus the reserved shared entry.
	require.Contains(t, first, "engineering-tools")
	require.Contains(t, first, mcpSharedFingerprintKey)
	require.True(t, strings.HasPrefix(first["engineering-tools"], "sha256:"))

	second, err := MCPFingerprints(fingerprintTestPlugins(), cfg)
	require.NoError(t, err)

	require.Equal(t, first, second, "same plugins + config must produce the same fingerprints")
}

func TestMCPFingerprintsIsolatesPlatformMCP(t *testing.T) {
	t.Parallel()
	cfg := GenerateConfig{OrgName: "Acme Corp", ServerURL: "https://app.getgram.ai", ProjectSlug: "default"}

	withoutPlatform, err := MCPFingerprints(fingerprintTestPlugins(), cfg)
	require.NoError(t, err)

	cfg.PlatformMCPEnabled = true
	withPlatform, err := MCPFingerprints(fingerprintTestPlugins(), cfg)
	require.NoError(t, err)

	require.Equal(t, withoutPlatform["engineering-tools"], withPlatform["engineering-tools"], "Platform MCP must not churn customer plugin fingerprints")
	require.NotEqual(t, withoutPlatform[mcpSharedFingerprintKey], withPlatform[mcpSharedFingerprintKey], "shared marketplace files list the Platform package and must change with it")
	require.Contains(t, withPlatform, mcpPlatformFingerprintKey)
	require.True(t, strings.HasPrefix(withPlatform[mcpPlatformFingerprintKey], "sha256:"))
}

func TestMCPFingerprintsIgnoresPerPublishFields(t *testing.T) {
	t.Parallel()
	plugins := fingerprintTestPlugins()
	plugins[0].Skills = []PluginSkillInfo{{Name: "release-notes", Content: "v1"}}

	base, err := MCPFingerprints(plugins, GenerateConfig{
		OrgName:     "Acme Corp",
		ServerURL:   "https://app.getgram.ai",
		ProjectSlug: "acme",
	})
	require.NoError(t, err)

	// Version and the injected API keys vary on every publish; the fingerprints
	// normalize them so they must not change the result.
	withNoise, err := MCPFingerprints(plugins, GenerateConfig{
		OrgName:     "Acme Corp",
		ServerURL:   "https://app.getgram.ai",
		ProjectSlug: "acme",
		Version:     "1750000000",
		APIKey:      "gram_live_realkey",
		HooksAPIKey: "gram_live_realhookskey",
	})
	require.NoError(t, err)

	require.Equal(t, base, withNoise, "manifest version and API keys must not affect the fingerprints")
}

// A change to one plugin must change only that plugin's fingerprint, leaving the
// others untouched — the property a per-plugin publish flow relies on.
func TestMCPFingerprintsIsolatesChangePerPlugin(t *testing.T) {
	t.Parallel()
	cfg := GenerateConfig{OrgName: "Acme Corp", ServerURL: "https://app.getgram.ai"}

	plugins := []PluginInfo{
		{Name: "Plugin A", Slug: "plugin-a", Description: "A", Servers: []PluginServerInfo{{DisplayName: "a1", MCPURL: "https://app.getgram.ai/mcp/a1"}}},
		{Name: "Plugin B", Slug: "plugin-b", Description: "B", Servers: []PluginServerInfo{{DisplayName: "b1", MCPURL: "https://app.getgram.ai/mcp/b1"}}},
	}

	base, err := MCPFingerprints(plugins, cfg)
	require.NoError(t, err)

	// Add a server to plugin A only.
	changed := []PluginInfo{
		{Name: "Plugin A", Slug: "plugin-a", Description: "A", Servers: []PluginServerInfo{
			{DisplayName: "a1", MCPURL: "https://app.getgram.ai/mcp/a1"},
			{DisplayName: "a2", MCPURL: "https://app.getgram.ai/mcp/a2"},
		}},
		{Name: "Plugin B", Slug: "plugin-b", Description: "B", Servers: []PluginServerInfo{{DisplayName: "b1", MCPURL: "https://app.getgram.ai/mcp/b1"}}},
	}
	changedFP, err := MCPFingerprints(changed, cfg)
	require.NoError(t, err)

	require.NotEqual(t, base["plugin-a"], changedFP["plugin-a"], "changed plugin's fingerprint must differ")
	require.Equal(t, base["plugin-b"], changedFP["plugin-b"], "untouched plugin's fingerprint must be stable")
}

func TestGenerateMCPFilesEmitsDistributedSkills(t *testing.T) {
	t.Parallel()
	cfg := GenerateConfig{
		OrgName:     "Acme Corp",
		ServerURL:   "https://app.getgram.ai///",
		HooksAPIKey: "gram_hooks_feedback",
		ProjectSlug: "acme",
	}
	content := "---\nname: release-notes\ndescription: d\n---\n\nbody\n"
	plugins := []PluginInfo{{
		Name:        "Engineering Tools",
		Slug:        "engineering-tools",
		Description: "d",
		Servers: []PluginServerInfo{
			{DisplayName: "crm-tools", MCPURL: "https://app.getgram.ai/mcp/acme-abc12"},
		},
		Skills: []PluginSkillInfo{
			{Name: "release-notes", Content: content},
			// Not a valid normalized skill name — must never become a path.
			{Name: "../escape", Content: "nope"},
		},
	}}

	files, err := generateMCPFiles(plugins, cfg)
	require.NoError(t, err)

	require.Equal(t, []byte(content), files["engineering-tools/skills/release-notes/SKILL.md"])
	require.Equal(t, []byte(content), files[cursorPluginRoot+"/engineering-tools-cursor/skills/release-notes/SKILL.md"])
	require.Equal(t, []byte(content), files["engineering-tools-codex/skills/release-notes/SKILL.md"])
	for p := range files {
		require.NotContains(t, p, "escape", "invalid skill names must be dropped, not emitted as paths")
	}

	var claude claudeMCPConfig
	require.NoError(t, json.Unmarshal(files["engineering-tools/.mcp.json"], &claude))
	require.Equal(t, claudeMCPServer{
		Type:    "stdio",
		Command: "bash",
		Args:    skillFeedbackMCPArgs("${CLAUDE_PLUGIN_ROOT}"),
		URL:     "",
		Headers: nil,
	}, claude.MCPServers[skillFeedbackMCPServerName])

	var cursor cursorMCPConfig
	require.NoError(t, json.Unmarshal(files[cursorPluginRoot+"/engineering-tools-cursor/mcp.json"], &cursor))
	require.Equal(t, cursorMCPServer{
		Command: "bash",
		Args:    skillFeedbackMCPArgs("${CURSOR_PLUGIN_ROOT}"),
		URL:     "",
		Headers: nil,
	}, cursor.MCPServers[skillFeedbackMCPServerName])

	var codex codexMCPConfig
	require.NoError(t, json.Unmarshal(files["engineering-tools-codex/.mcp.json"], &codex))
	require.Equal(t, codexMCPServer{
		Command:           "bash",
		Args:              codexSkillFeedbackMCPArgs("engineering-tools-codex", cfg),
		URL:               "",
		BearerTokenEnvVar: "",
		HTTPHeaders:       nil,
		EnvHTTPHeaders:    nil,
	}, codex.MCPServers[skillFeedbackMCPServerName])
	require.Contains(t, codex.MCPServers[skillFeedbackMCPServerName].Args[1], "${CODEX_HOME:-$HOME/.codex}/plugins/cache/")
	feedbackJSON, err := json.Marshal(codex.MCPServers[skillFeedbackMCPServerName])
	require.NoError(t, err)
	require.NotContains(t, string(feedbackJSON), "url")
	require.NotContains(t, string(feedbackJSON), "bearer_token_env_var")
	require.NotContains(t, string(feedbackJSON), "http_headers")

	// The stdio server rides the plugin-local bootstrap script and deployment
	// identity, so skill-carrying feature plugins ship both.
	for _, subdir := range []string{"engineering-tools", cursorPluginRoot + "/engineering-tools-cursor", "engineering-tools-codex"} {
		require.Contains(t, files, subdir+"/hooks/bootstrap.sh")
		require.Contains(t, string(files[subdir+"/speakeasy.json"]), "gram_hooks_feedback")
	}
}

func TestGenerateMCPFilesOmitsSkillFeedbackWithoutSkill(t *testing.T) {
	t.Parallel()
	withoutSkill := PluginInfo{
		Name:   "Engineering Tools",
		Slug:   "engineering-tools",
		Skills: []PluginSkillInfo{{Name: "../escape", Content: "invalid"}},
	}
	files, err := generateMCPFiles([]PluginInfo{withoutSkill}, GenerateConfig{
		ServerURL:   "https://app.getgram.ai",
		HooksAPIKey: "gram_hooks_feedback",
		ProjectSlug: "acme",
	})
	require.NoError(t, err)
	for p, content := range files {
		require.NotContains(t, string(content), skillFeedbackMCPServerName)
		require.NotContains(t, p, "speakeasy.json", "plugins without skills must not carry the hooks runtime")
	}
}

// The ZIP download path generates with no hooks key. The stdio server still
// ships — the binary falls back to cached or browser-login credentials — so
// downloaded packages keep the feedback loop.
func TestGenerateSinglePluginPackageBundlesSkillFeedbackWithoutKey(t *testing.T) {
	t.Parallel()
	withSkill := PluginInfo{
		Name:   "Engineering Tools",
		Slug:   "engineering-tools",
		Skills: []PluginSkillInfo{{Name: "release-notes", Content: "v1"}},
	}
	for _, platform := range []string{"claude", "cursor", "codex"} {
		files, err := GenerateSinglePluginPackage(withSkill, GenerateConfig{
			ServerURL:   "https://app.getgram.ai",
			ProjectSlug: "acme",
		}, platform)
		require.NoError(t, err)
		mcpPath := ".mcp.json"
		if platform == "cursor" {
			mcpPath = "mcp.json"
		}
		require.Contains(t, string(files[mcpPath]), skillFeedbackMCPServerName, platform)
		require.Contains(t, files, "hooks/bootstrap.sh", platform)
		require.Contains(t, files, "speakeasy.json", platform)
	}
}

func TestGenerateMCPFilesPreservesSkillFeedbackServerCollision(t *testing.T) {
	t.Parallel()
	plugin := PluginInfo{
		Name: "Engineering Tools",
		Slug: "engineering-tools",
		Servers: []PluginServerInfo{{
			DisplayName: skillFeedbackMCPServerName,
			MCPURL:      "https://user.example.com/mcp",
		}},
		Skills: []PluginSkillInfo{{Name: "release-notes", Content: "v1"}},
	}
	files, err := generateMCPFiles([]PluginInfo{plugin}, GenerateConfig{
		ServerURL:   "https://app.getgram.ai",
		APIKey:      "gram_consumer_key",
		HooksAPIKey: "gram_hooks_feedback",
		ProjectSlug: "acme",
	})
	require.NoError(t, err)

	var claude claudeMCPConfig
	require.NoError(t, json.Unmarshal(files["engineering-tools/.mcp.json"], &claude))
	require.Equal(t, "https://user.example.com/mcp", claude.MCPServers[skillFeedbackMCPServerName].URL)
	require.Equal(t, "Bearer gram_consumer_key", claude.MCPServers[skillFeedbackMCPServerName].Headers["Authorization"])

	var cursor cursorMCPConfig
	require.NoError(t, json.Unmarshal(files[cursorPluginRoot+"/engineering-tools-cursor/mcp.json"], &cursor))
	require.Equal(t, "https://user.example.com/mcp", cursor.MCPServers[skillFeedbackMCPServerName].URL)
	require.Equal(t, "Bearer gram_consumer_key", cursor.MCPServers[skillFeedbackMCPServerName].Headers["Authorization"])

	var codex codexMCPConfig
	require.NoError(t, json.Unmarshal(files["engineering-tools-codex/.mcp.json"], &codex))
	require.Equal(t, "https://user.example.com/mcp", codex.MCPServers[skillFeedbackMCPServerName].URL)
	require.Equal(t, "Bearer gram_consumer_key", codex.MCPServers[skillFeedbackMCPServerName].HTTPHeaders["Authorization"])
}

func TestGenerateCodexPluginPreservesNormalizedSkillFeedbackServerCollision(t *testing.T) {
	t.Parallel()
	plugin := PluginInfo{
		Name: "Engineering Tools",
		Slug: "engineering-tools",
		Servers: []PluginServerInfo{{
			DisplayName: "!" + skillFeedbackMCPServerName,
			MCPURL:      "https://user.example.com/mcp",
		}},
		Skills: []PluginSkillInfo{{Name: "release-notes", Content: "v1"}},
	}
	files, err := GenerateSinglePluginPackage(plugin, GenerateConfig{
		ServerURL:   "https://app.getgram.ai",
		APIKey:      "gram_consumer_key",
		HooksAPIKey: "gram_hooks_feedback",
		ProjectSlug: "acme",
	}, "codex")
	require.NoError(t, err)

	var config codexMCPConfig
	require.NoError(t, json.Unmarshal(files[".mcp.json"], &config))
	require.Equal(t, "https://user.example.com/mcp", config.MCPServers[skillFeedbackMCPServerName].URL)
}

// Distributing a skill (or changing its resolved content) must move the
// carrying plugin's fingerprint and only that one — the signal the publish
// freshness check and the auto-sync rollout key on.
func TestMCPFingerprintsChangeWithDistributedSkills(t *testing.T) {
	t.Parallel()
	cfg := GenerateConfig{OrgName: "Acme Corp", ServerURL: "https://app.getgram.ai"}
	makePlugins := func(skillContent string) []PluginInfo {
		a := PluginInfo{Name: "Plugin A", Slug: "plugin-a", Description: "A", Servers: []PluginServerInfo{{DisplayName: "a1", MCPURL: "https://app.getgram.ai/mcp/a1"}}}
		if skillContent != "" {
			a.Skills = []PluginSkillInfo{{Name: "release-notes", Content: skillContent}}
		}
		b := PluginInfo{Name: "Plugin B", Slug: "plugin-b", Description: "B", Servers: []PluginServerInfo{{DisplayName: "b1", MCPURL: "https://app.getgram.ai/mcp/b1"}}}
		return []PluginInfo{a, b}
	}

	base, err := MCPFingerprints(makePlugins(""), cfg)
	require.NoError(t, err)
	withSkill, err := MCPFingerprints(makePlugins("v1"), cfg)
	require.NoError(t, err)
	withNewVersion, err := MCPFingerprints(makePlugins("v2"), cfg)
	require.NoError(t, err)

	require.NotEqual(t, base["plugin-a"], withSkill["plugin-a"], "distributing a skill must change the plugin's fingerprint")
	require.NotEqual(t, withSkill["plugin-a"], withNewVersion["plugin-a"], "a new resolved skill version must change the plugin's fingerprint")
	require.Equal(t, base["plugin-b"], withSkill["plugin-b"], "plugins not carrying the skill must be untouched")
}
