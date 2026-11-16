package plugins

import (
	"archive/zip"
	"bytes"
	"encoding/json"
	"io"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

// Codex chooses its restricted Agent Plugins loader from the root manifest,
// even when a native Codex manifest is also present.
func TestCodexPackageBoundaries(t *testing.T) {
	t.Parallel()
	cfg := GenerateConfig{OrgName: "Example", ServerURL: "https://app.getgram.ai"}
	plugin := PluginInfo{Name: "Tools", Slug: "tools", Servers: []PluginServerInfo{{DisplayName: "tools", MCPURL: "https://example.com/mcp", IsPublic: true}}}
	org, err := GeneratePluginPackages([]PluginInfo{plugin}, cfg)
	require.NoError(t, err)
	public, err := PublicPlatformMCPFiles(cfg.ServerURL, "1")
	require.NoError(t, err)

	for name, files := range map[string]map[string][]byte{"organization": org, "public": public} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			var marketplace codexMarketplaceManifest
			require.NoError(t, json.Unmarshal(files[".agents/plugins/marketplace.json"], &marketplace))
			root := "tools-codex/"
			portable := "agent-plugins/tools/"
			if name == "public" {
				root = "speakeasy-codex/"
				portable = "agent-plugins/speakeasy/"
			}
			require.Len(t, marketplace.Plugins, 1)
			require.Equal(t, "./"+strings.TrimSuffix(root, "/"), marketplace.Plugins[0].Source.Path)
			assertNativeCodexPackage(t, files, root)
			require.NotContains(t, files, portable+".codex-plugin/plugin.json")
			for file, schema := range map[string]string{"plugin.json": agentPluginSchemaID, "mcp.json": agentMCPSchemaID} {
				var document any
				require.NoError(t, json.Unmarshal(files[portable+file], &document))
				require.NoError(t, validateAgentPluginDocument(schema, document))
			}
		})
	}

	var copilot marketplaceManifest
	require.NoError(t, json.Unmarshal(org["marketplace.json"], &copilot))
	require.Len(t, copilot.Plugins, 1)
	require.Equal(t, "./agent-plugins/tools", copilot.Plugins[0].Source)
	require.Contains(t, org, "agent-plugins/tools/.cursor-plugin/plugin.json")
	require.Equal(t, org["tools-codex/.mcp.json"], org["agent-plugins/tools/.mcp.json"], "retain Cursor's existing native MCP configuration")
	require.Contains(t, org, "cursor-plugins/tools-cursor/.cursor-plugin/plugin.json")

	flat, err := GenerateSinglePluginPackage(plugin, cfg, "codex")
	require.NoError(t, err)
	var archive bytes.Buffer
	require.NoError(t, writePluginZip(&archive, flat))
	reader, err := zip.NewReader(bytes.NewReader(archive.Bytes()), int64(archive.Len()))
	require.NoError(t, err)
	unzipped := make(map[string][]byte)
	for _, file := range reader.File {
		r, err := file.Open()
		require.NoError(t, err)
		unzipped[file.Name], err = io.ReadAll(r)
		require.NoError(t, err)
		require.NoError(t, r.Close())
	}
	assertNativeCodexPackage(t, unzipped, "")
}

func assertNativeCodexPackage(t *testing.T, files map[string][]byte, root string) {
	t.Helper()
	require.Contains(t, files, root+".codex-plugin/plugin.json")
	require.Contains(t, files, root+".mcp.json")
	require.NotContains(t, files, root+"plugin.json")
	for name, content := range files {
		if strings.HasPrefix(name, root) {
			require.NotContains(t, string(content), "https://agent-plugins.org/schemas/", name)
		}
	}
}
