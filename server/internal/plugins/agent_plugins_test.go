package plugins

import (
	"archive/zip"
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"io"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestAgentPluginClassifier(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name       string
		plugin     PluginInfo
		compatible bool
	}{
		{name: "empty", plugin: PluginInfo{Name: "Empty", Slug: "empty", Description: "", Servers: nil, Skills: nil, AgentPluginsV1Issues: nil}, compatible: true},
		{name: "skills only", plugin: PluginInfo{Name: "Skills", Slug: "skills", Description: "", Servers: nil, Skills: []PluginSkillInfo{{Name: "review", Content: "content"}}, AgentPluginsV1Issues: nil}, compatible: true},
		{name: "public remote", plugin: PluginInfo{Name: "Public", Slug: "public", Description: "", Servers: []PluginServerInfo{{DisplayName: "public", Policy: "required", MCPURL: "https://example.com/mcp", IsPublic: true, IsOAuth: false, IsUnproxied: false, EnvConfigs: nil}}, Skills: nil, AgentPluginsV1Issues: nil}, compatible: true},
		{name: "oauth remote", plugin: PluginInfo{Name: "OAuth", Slug: "oauth", Description: "", Servers: []PluginServerInfo{{DisplayName: "oauth", Policy: "required", MCPURL: "https://example.com/mcp", IsPublic: false, IsOAuth: true, IsUnproxied: false, EnvConfigs: nil}}, Skills: nil, AgentPluginsV1Issues: nil}, compatible: true},
		{name: "loopback http", plugin: PluginInfo{Name: "Local", Slug: "local", Description: "", Servers: []PluginServerInfo{{DisplayName: "local", Policy: "required", MCPURL: "http://127.0.0.1:8080/mcp", IsPublic: true, IsOAuth: false, IsUnproxied: false, EnvConfigs: nil}}, Skills: nil, AgentPluginsV1Issues: nil}, compatible: true},
		{name: "unproxied https", plugin: PluginInfo{Name: "External", Slug: "external", Description: "", Servers: []PluginServerInfo{{DisplayName: "external", Policy: "required", MCPURL: "https://example.com/Authorization", IsPublic: false, IsOAuth: false, IsUnproxied: true, EnvConfigs: nil}}, Skills: nil, AgentPluginsV1Issues: nil}, compatible: true},
		{name: "private remote", plugin: PluginInfo{Name: "Private", Slug: "private", Description: "", Servers: []PluginServerInfo{{DisplayName: "private", Policy: "required", MCPURL: "https://example.com/mcp", IsPublic: false, IsOAuth: false, IsUnproxied: false, EnvConfigs: nil}}, Skills: nil, AgentPluginsV1Issues: nil}, compatible: false},
		{name: "environment header", plugin: PluginInfo{Name: "Header", Slug: "header", Description: "", Servers: []PluginServerInfo{{DisplayName: "header", Policy: "required", MCPURL: "https://example.com/mcp", IsPublic: true, IsOAuth: false, IsUnproxied: false, EnvConfigs: []ServerEnvConfig{{VariableName: "TOKEN", DisplayName: "X-Token"}}}}, Skills: nil, AgentPluginsV1Issues: nil}, compatible: false},
		{name: "non portable url", plugin: PluginInfo{Name: "HTTP", Slug: "http", Description: "", Servers: []PluginServerInfo{{DisplayName: "http", Policy: "required", MCPURL: "http://example.com/mcp", IsPublic: true, IsOAuth: false, IsUnproxied: false, EnvConfigs: nil}}, Skills: nil, AgentPluginsV1Issues: nil}, compatible: false},
		{name: "unresolved attachment", plugin: PluginInfo{Name: "Broken", Slug: "broken", Description: "", Servers: nil, Skills: nil, AgentPluginsV1Issues: []string{"mcp_server_disabled"}}, compatible: false},
	}

	for _, test := range tests {
		require.Equal(t, test.compatible, classifyAgentPlugin(test.plugin).Compatible, test.name)
	}
}

func TestCompileAgentPluginUsesPinnedSchemasAndKeylessMode(t *testing.T) {
	t.Parallel()

	pluginSchemaHash := sha256.Sum256(agentPluginSchemaJSON)
	mcpSchemaHash := sha256.Sum256(agentMCPSchemaJSON)
	require.Equal(t, "0a4aad95ce337878ad38802ebf0daa3fde76abe3f65400c86bcbb1ec0b3ab883", hex.EncodeToString(pluginSchemaHash[:]))
	require.Equal(t, "6539175bfcdf43085855183e86da40ea94b166547a72b47ae9a0a390516d3acb", hex.EncodeToString(mcpSchemaHash[:]))

	plugin := PluginInfo{
		Name:                 "Review Tools",
		Slug:                 "review-tools",
		Description:          "Review code",
		Servers:              []PluginServerInfo{{DisplayName: "reviews", Policy: "required", MCPURL: "https://example.com/mcp", IsPublic: true, IsOAuth: false, IsUnproxied: false, EnvConfigs: nil}},
		Skills:               []PluginSkillInfo{{Name: "review", Content: "---\nname: review\ndescription: Review code.\n---\n"}},
		AgentPluginsV1Issues: nil,
	}
	cfg := GenerateConfig{OrgName: "Example", OrgEmail: "", OrgID: "org-example", ServerURL: "https://app.getgram.ai", APIKey: "consumer-secret", HooksAPIKey: "hooks-secret", ProjectSlug: "default", IsDefaultProject: true, Version: "1", PlatformMCPEnabled: false, MarketplaceName: "", BrowserLogin: false, HooksOrgName: "", InstallFailOpen: false}

	files, err := compileAgentPlugin(plugin, cfg, agentPluginCredentialsKeyless)
	require.NoError(t, err)
	require.Contains(t, files, "plugin.json")
	require.Contains(t, files, "mcp.json")
	require.NotContains(t, files, ".cursor-plugin/plugin.json")
	require.NotContains(t, files, ".codex-plugin/plugin.json")
	require.NotContains(t, files, ".mcp.json")
	require.Contains(t, files, "skills/review/SKILL.md")
	require.Contains(t, files, "hooks/bootstrap.sh")
	require.Contains(t, files, "speakeasy.json")
	require.NotContains(t, string(files["speakeasy.json"]), "hooks-secret")

	var mcp agentMCPConfig
	require.NoError(t, json.Unmarshal(files["mcp.json"], &mcp))
	require.NoError(t, validateAgentPluginDocument(agentMCPSchemaID, mcp))
	require.Empty(t, mcp.MCPServers["reviews"].Headers)
	for _, forbidden := range []string{"consumer-secret", "hooks-secret", "Authorization", "${env:", "bearer_token_env_var", "env_http_headers"} {
		require.NotContains(t, string(files["mcp.json"]), forbidden)
	}
	feedback := mcp.MCPServers[skillFeedbackMCPServerName]
	require.Equal(t, "bash", feedback.Command)
	require.Contains(t, feedback.Args[1], `${PLUGIN_ROOT:-${CURSOR_PLUGIN_ROOT}}`)

	published, err := compileAgentPlugin(plugin, cfg, agentPluginCredentialsPackage)
	require.NoError(t, err)
	require.Contains(t, published, ".cursor-plugin/plugin.json")
	require.Contains(t, published, ".codex-plugin/plugin.json")
	require.Contains(t, published, ".mcp.json")
	for path, content := range published {
		require.NotContains(t, string(content), cfg.APIKey, path)
		require.NotContains(t, string(content), cfg.HooksAPIKey, path)
	}
}

func TestAgentPluginMarketplaceKeepsNativeSources(t *testing.T) {
	t.Parallel()

	cfg := GenerateConfig{OrgName: "Example", OrgEmail: "", OrgID: "", ServerURL: "https://app.getgram.ai", APIKey: "", HooksAPIKey: "", ProjectSlug: "default", IsDefaultProject: true, Version: "", PlatformMCPEnabled: false, MarketplaceName: "", BrowserLogin: false, HooksOrgName: "", InstallFailOpen: false}
	compatible := PluginInfo{Name: "Tools", Slug: "tools", Description: "", Servers: []PluginServerInfo{{DisplayName: "tools", Policy: "required", MCPURL: "https://example.com/mcp", IsPublic: true, IsOAuth: false, IsUnproxied: false, EnvConfigs: nil}}, Skills: nil, AgentPluginsV1Issues: nil}
	incompatible := compatible
	incompatible.Servers = append([]PluginServerInfo(nil), compatible.Servers...)
	incompatible.Servers[0].IsPublic = false

	sharedFiles, err := generateSharedFiles([]PluginInfo{compatible}, cfg)
	require.NoError(t, err)
	legacyFiles, err := generateSharedFiles([]PluginInfo{incompatible}, cfg)
	require.NoError(t, err)

	var sharedCursor, legacyCursor marketplaceManifest
	require.NoError(t, json.Unmarshal(sharedFiles[".cursor-plugin/marketplace.json"], &sharedCursor))
	require.NoError(t, json.Unmarshal(legacyFiles[".cursor-plugin/marketplace.json"], &legacyCursor))
	require.Equal(t, legacyCursor.Plugins[0].Name, sharedCursor.Plugins[0].Name)
	require.Equal(t, "tools-cursor", sharedCursor.Plugins[0].Source)
	require.Equal(t, "tools-cursor", legacyCursor.Plugins[0].Source)

	var sharedCodex, legacyCodex codexMarketplaceManifest
	require.NoError(t, json.Unmarshal(sharedFiles[".agents/plugins/marketplace.json"], &sharedCodex))
	require.NoError(t, json.Unmarshal(legacyFiles[".agents/plugins/marketplace.json"], &legacyCodex))
	require.Equal(t, legacyCodex.Plugins[0].Name, sharedCodex.Plugins[0].Name)
	require.Equal(t, "./tools-codex", sharedCodex.Plugins[0].Source.Path)
	require.Equal(t, "./tools-codex", legacyCodex.Plugins[0].Source.Path)

	// Copilot serves feature plugins only through the Agent Plugins package, so
	// its manifest — unlike the three native ones above — does track
	// compatibility: an incompatible plugin is omitted rather than pointed at a
	// directory generateMCPFiles never wrote.
	var sharedCopilot, legacyCopilot marketplaceManifest
	require.NoError(t, json.Unmarshal(sharedFiles["marketplace.json"], &sharedCopilot))
	require.NoError(t, json.Unmarshal(legacyFiles["marketplace.json"], &legacyCopilot))
	require.Equal(t, "./agent-plugins/tools", sharedCopilot.Plugins[0].Source)
	require.Empty(t, legacyCopilot.Plugins)

	sharedFingerprint, err := MCPFingerprints([]PluginInfo{compatible}, cfg)
	require.NoError(t, err)
	legacyFingerprint, err := MCPFingerprints([]PluginInfo{incompatible}, cfg)
	require.NoError(t, err)
	require.NotEqual(t, sharedFingerprint["tools"], legacyFingerprint["tools"])
	// The shared fingerprint follows the Copilot manifest, so flipping a plugin's
	// portability republishes the catalog — which is the point: the entry has to
	// appear or disappear for Copilot to see the change.
	require.NotEqual(t, sharedFingerprint[mcpSharedFingerprintKey], legacyFingerprint[mcpSharedFingerprintKey])
}

func TestAgentPluginZipIsDeterministicAndExecutable(t *testing.T) {
	t.Parallel()

	files := map[string][]byte{"z.txt": []byte("z"), "hooks/bootstrap.sh": []byte("#!/bin/sh\n"), "a.txt": []byte("a")}
	var first, second bytes.Buffer
	require.NoError(t, writePluginZip(&first, files))
	require.NoError(t, writePluginZip(&second, files))
	require.Equal(t, first.Bytes(), second.Bytes())

	zr, err := zip.NewReader(bytes.NewReader(first.Bytes()), int64(first.Len()))
	require.NoError(t, err)
	require.Equal(t, []string{"a.txt", "hooks/bootstrap.sh", "z.txt"}, []string{zr.File[0].Name, zr.File[1].Name, zr.File[2].Name})
	require.Equal(t, "-rwxr-xr-x", zr.File[1].Mode().String())
	rc, err := zr.File[1].Open()
	require.NoError(t, err)
	_, err = io.ReadAll(rc)
	require.NoError(t, err)
	require.NoError(t, rc.Close())
}

func TestCompileAgentPluginFailsClosed(t *testing.T) {
	t.Parallel()

	_, err := compileAgentPlugin(PluginInfo{Name: "Broken", Slug: "broken", Description: "", Servers: nil, Skills: nil, AgentPluginsV1Issues: []string{"backing unresolved"}}, GenerateConfig{}, agentPluginCredentialsKeyless)
	require.Error(t, err)
	require.ErrorIs(t, err, ErrAgentPluginsV1Incompatible)
	require.Contains(t, err.Error(), "backing unresolved")
	require.NotContains(t, err.Error(), "secret")
}
