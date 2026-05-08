package plugins

import (
	"archive/zip"
	"bytes"
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
		".agents/plugins/marketplace.json",
		"engineering-tools/.claude-plugin/plugin.json",
		"engineering-tools/.mcp.json",
		"engineering-tools-cursor/.cursor-plugin/plugin.json",
		"engineering-tools-cursor/mcp.json",
		"engineering-tools-codex/.codex-plugin/plugin.json",
		"engineering-tools-codex/.mcp.json",
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

	var parsed claudeHooksConfig
	require.NoError(t, json.Unmarshal(hooksJSON, &parsed))

	for _, event := range ClaudeObservabilityHookEvents {
		require.Contains(t, parsed.Hooks, event, "event %q must be registered in hooks.json or Claude will silently drop it", event)
	}
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
	require.Equal(t, uint32(0o644), modes[".claude-plugin/plugin.json"], "non-script files keep default mode")
	require.Equal(t, uint32(0o644), modes["README.md"], "non-script files keep default mode")
}
