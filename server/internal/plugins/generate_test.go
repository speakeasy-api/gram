package plugins

import (
	"encoding/json"
	"strings"
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
	err = json.Unmarshal(files["test-cursor/mcp.json"], &mcpConfig)
	require.NoError(t, err)

	gramServer := mcpConfig.MCPServers["gram-server"]
	require.Equal(t, "Bearer ${env:GRAM_API_KEY}", gramServer.Headers["Authorization"])
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

	require.Equal(t, "acme-gram", claudeManifest.Name)
	require.Equal(t, "Acme", claudeManifest.Owner.Name)
	require.Len(t, claudeManifest.Plugins, 2)
	require.Equal(t, "./a", claudeManifest.Plugins[0].Source)
	require.Equal(t, "./b", claudeManifest.Plugins[1].Source)

	var cursorManifest marketplaceManifest
	err = json.Unmarshal(files[".cursor-plugin/marketplace.json"], &cursorManifest)
	require.NoError(t, err)

	require.Equal(t, "acme-gram", cursorManifest.Name)
	require.Len(t, cursorManifest.Plugins, 2)
	require.Equal(t, "./a-cursor", cursorManifest.Plugins[0].Source)
	require.Equal(t, "./b-cursor", cursorManifest.Plugins[1].Source)
}

func TestRenderHookScriptClaudeUsesGramKeyAndProjectHeaders(t *testing.T) {
	t.Parallel()
	// Claude's hook endpoint accepts Gram-Key + Gram-Project as optional
	// headers (design.go:116). When supplied, the handler attributes hooks
	// via the auth context; when absent, it falls back to OTEL session
	// metadata. The script always sends them so plugin installs work.
	cfg := GenerateConfig{
		ServerURL:   "https://app.getgram.ai",
		HooksAPIKey: "gram_local_secret_xyz",
		ProjectSlug: "acme-prod",
	}
	script := string(renderHookScript(cfg, "claude"))

	require.Contains(t, script, "https://app.getgram.ai/rpc/hooks.claude")
	require.NotContains(t, script, "/hooks/claude", "must not use the legacy /hooks/<platform> path")
	require.Contains(t, script, "Gram-Key: gram_local_secret_xyz")
	require.Contains(t, script, "Gram-Project: acme-prod")
	require.NotContains(t, script, "Authorization", "endpoint reads Gram-Key, not Authorization")
}

func TestRenderHookScriptCursorUsesGramKeyAndProjectHeaders(t *testing.T) {
	t.Parallel()
	// Cursor's hook endpoint reads Gram-Key + Gram-Project per
	// server/gen/http/hooks/server/encode_decode.go:261.
	cfg := GenerateConfig{
		ServerURL:   "https://app.getgram.ai",
		HooksAPIKey: "gram_local_secret_xyz",
		ProjectSlug: "acme-prod",
	}
	script := string(renderHookScript(cfg, "cursor"))

	require.Contains(t, script, "https://app.getgram.ai/rpc/hooks.cursor")
	require.NotContains(t, script, "/hooks/cursor", "must not use the legacy /hooks/<platform> path")
	require.Contains(t, script, `Gram-Key: gram_local_secret_xyz`, "cursor reads Gram-Key, not Authorization")
	require.NotContains(t, script, "Authorization", "cursor endpoint does not read Authorization")
	require.Contains(t, script, `Gram-Project: acme-prod`, "cursor requires the project header per design")
}

func TestRenderHookScriptCursorOmitsProjectHeaderWhenSlugMissing(t *testing.T) {
	t.Parallel()
	// Defensive: if generateConfig is ever called without a slug, we should
	// emit a script that's at least syntactically valid rather than embed an
	// empty header.
	cfg := GenerateConfig{
		ServerURL:   "https://app.getgram.ai",
		HooksAPIKey: "gram_local_secret_xyz",
	}
	script := string(renderHookScript(cfg, "cursor"))

	require.Contains(t, script, "Gram-Key: gram_local_secret_xyz", "key still emitted without a slug")
	require.NotContains(t, script, "Gram-Project")
}
