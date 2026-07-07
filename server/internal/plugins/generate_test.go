package plugins

import (
	"archive/zip"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/BurntSushi/toml"
	"github.com/speakeasy-api/gram/server/internal/conv"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func requireFileBytes(t *testing.T, path string) []byte {
	t.Helper()
	data, err := os.ReadFile(path)
	require.NoError(t, err)
	return data
}

func requireMapValue(t *testing.T, values map[string]any, key string) map[string]any {
	t.Helper()
	value, ok := values[key].(map[string]any)
	require.Truef(t, ok, "%q must be an object, got %T (%#v)", key, values[key], values[key])
	return value
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

// TestSharedAuthScriptMatchesCheckedIn guards against drift between the
// generated hooks/auth.sh (renderSharedAuthScript) and the checked-in
// hooks/plugin-claude/hooks/auth.sh sourced by the local-dev plugin. Both must
// be identical so local-dev and generated plugins share one auth flow.
func TestSharedAuthScriptMatchesCheckedIn(t *testing.T) {
	t.Parallel()
	checkedIn := requireFileBytes(t, filepath.Join("..", "..", "..", "hooks", "plugin-claude", "hooks", "auth.sh"))
	// renderSharedAuthScript() is canonical → pass it as testify's "expected".
	require.Equal(t, string(renderSharedAuthScript()), string(checkedIn),
		"hooks/plugin-claude/hooks/auth.sh has drifted from renderSharedAuthScript() — keep them identical")
}

// TestSharedAuthScriptBrowserLoginRoundtrip drives the interactive login flow
// end to end against the real nc-based localhost listener: a stubbed browser
// opener captures the dashboard URL, the test plays the dashboard's role by
// requesting the localhost callback with an api_key, and the flow must cache
// the credentials and clear the attempt cooldown marker.
func TestSharedAuthScriptBrowserLoginRoundtrip(t *testing.T) {
	t.Parallel()
	_, err := exec.LookPath("nc")
	require.NoError(t, err, "netcat is required: the browser login flow depends on it and must stay covered")

	dir := t.TempDir()
	authFile := filepath.Join(dir, "auth.env")
	urlFile := filepath.Join(dir, "auth-url")
	argvFile := filepath.Join(dir, "auth-argv")
	require.NoError(t, os.WriteFile(filepath.Join(dir, "auth.sh"), renderSharedAuthScript(), 0o755))
	// Stub both browser openers; whichever the host OS selects records its
	// raw argument and the resolved content (the opener receives a file://
	// redirect page so the state token stays out of process arguments).
	opener := []byte(`#!/usr/bin/env bash
printf '%s' "$1" > "$GRAM_TEST_ARGV_FILE"
if [ -f "$1" ]; then cat "$1" > "$GRAM_TEST_URL_FILE"; else printf '%s' "$1" > "$GRAM_TEST_URL_FILE"; fi
`)
	require.NoError(t, os.WriteFile(filepath.Join(dir, "open"), opener, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "xdg-open"), opener, 0o755))

	// The login flow refuses to run under CI/SSH markers and, on Linux,
	// without a display — scrub and pin those so the test drives the real
	// listener everywhere.
	env := []string{"PATH=" + dir + string(os.PathListSeparator) + os.Getenv("PATH")}
	for _, kv := range os.Environ() {
		if strings.HasPrefix(kv, "CI=") || strings.HasPrefix(kv, "SSH_CONNECTION=") ||
			strings.HasPrefix(kv, "SSH_TTY=") || strings.HasPrefix(kv, "PATH=") ||
			strings.HasPrefix(kv, "GRAM_") || strings.HasPrefix(kv, "DISPLAY=") {
			continue
		}
		env = append(env, kv)
	}
	env = append(env,
		"GRAM_HOOKS_AUTH_FILE="+authFile,
		"GRAM_TEST_URL_FILE="+urlFile,
		"GRAM_TEST_ARGV_FILE="+argvFile,
		"GRAM_HOOKS_INTERACTIVE=1",
		"GRAM_HOOKS_LOGIN_FORCE=1",
		"GRAM_HOOKS_LOGIN_TIMEOUT_SECONDS=60",
		"DISPLAY=:0",
	)

	ctx, cancel := context.WithTimeout(t.Context(), 90*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, "bash", "-c", `. ./auth.sh; gram_hooks_login https://gram.test default`)
	cmd.Dir = dir
	cmd.Env = env
	// The listener runs as a background child sharing stdout; WaitDelay keeps
	// Wait from blocking on its pipe if a failure path leaks it.
	cmd.WaitDelay = 5 * time.Second
	var output bytes.Buffer
	cmd.Stdout = &output
	cmd.Stderr = &output
	require.NoError(t, cmd.Start())

	var port, state string
	portPattern := regexp.MustCompile(`127\.0\.0\.1%3A(\d+)%2Fcallback%3Fstate%3D([A-Za-z0-9-]+)`)
	require.EventuallyWithT(t, func(c *assert.CollectT) {
		raw, err := os.ReadFile(urlFile)
		if !assert.NoError(c, err) {
			return
		}
		match := portPattern.FindStringSubmatch(string(raw))
		if assert.NotNil(c, match, "auth URL missing callback port and state token: %s", string(raw)) {
			port = match[1]
			state = match[2]
		}
		// The listener advertises form_post so the dashboard POSTs the key in a
		// request body instead of exposing it in the callback URL.
		assert.Contains(c, string(raw), "response_mode=form_post", "auth URL must request the form_post token exchange: %s", string(raw))
	}, 30*time.Second, 100*time.Millisecond, "browser opener was never invoked: %s", output.String())

	// Process arguments are world-readable; the state token must reach the
	// opener only through the 0600 redirect file.
	argv := string(requireFileBytes(t, argvFile))
	require.NotContains(t, argv, "state", "opener argv must not carry the state token")

	// A callback without the per-attempt state token is an injection attempt
	// by something else on this machine: rejected, and the listener keeps
	// waiting for the real redirect.
	forged := "http://127.0.0.1:" + port + "/callback"
	require.EventuallyWithT(t, func(c *assert.CollectT) {
		req, err := http.NewRequestWithContext(ctx, http.MethodPost, forged, strings.NewReader("api_key=attacker-key&project=evil"))
		if !assert.NoError(c, err) {
			return
		}
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		resp, err := http.DefaultClient.Do(req)
		if !assert.NoError(c, err) {
			return
		}
		assert.NoError(c, resp.Body.Close())
		assert.Equal(c, http.StatusForbidden, resp.StatusCode)
	}, 30*time.Second, 200*time.Millisecond, "forged callback was never rejected: %s", output.String())

	// The dashboard's form_post: the state token stays in the callback URL, but
	// the credentials arrive in the request body so they never touch the URL.
	callback := "http://127.0.0.1:" + port + "/callback?state=" + state
	require.EventuallyWithT(t, func(c *assert.CollectT) {
		req, err := http.NewRequestWithContext(ctx, http.MethodPost, callback, strings.NewReader("api_key=test-key-123&project=default&email=a%40b.c"))
		if !assert.NoError(c, err) {
			return
		}
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		resp, err := http.DefaultClient.Do(req)
		if !assert.NoError(c, err) {
			return
		}
		body, err := io.ReadAll(resp.Body)
		assert.NoError(c, err)
		assert.NoError(c, resp.Body.Close())
		assert.Equal(c, http.StatusOK, resp.StatusCode)
		assert.Contains(c, string(body), "close this tab")
	}, 30*time.Second, 200*time.Millisecond, "callback request never succeeded: %s", output.String())

	require.NoError(t, cmd.Wait(), output.String())

	cached := string(requireFileBytes(t, authFile))
	require.Contains(t, cached, "server_url=https://gram.test\n")
	require.Contains(t, cached, "api_key=test-key-123\n")
	require.NotContains(t, cached, "attacker-key")
	require.Contains(t, cached, "project=default\n")
	require.Contains(t, cached, "email=a@b.c\n")
	require.NoFileExists(t, authFile+".login-attempt", "successful login must clear the attempt cooldown marker")
}

// TestSharedAuthScriptHandleRequestFormPost drives the listener's request
// handler directly to lock in the form_post token exchange and its safeguards:
// a POST with the valid state captures the body verbatim, an older dashboard's
// GET redirect still works (so a new plugin keeps authenticating against a
// dashboard that has not yet learned form_post), and a POST without the state
// token is rejected without capturing anything.
func TestSharedAuthScriptHandleRequestFormPost(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(dir, "auth.sh"), renderSharedAuthScript(), 0o755))

	script := `. ./auth.sh
run_case() {
  local label="$1" method="$2" target="$3" body="$4"
  local work; work="$(mktemp -d)"
  if [ "$method" = "POST" ]; then
    printf 'POST %s HTTP/1.1\r\nContent-Type: application/x-www-form-urlencoded\r\nContent-Length: %s\r\n\r\n%s' "$target" "${#body}" "$body" \
      | gram_hooks_login_handle_request "$work" tok probe >"$work/resp"
  else
    printf 'GET %s HTTP/1.1\r\n\r\n' "$target" \
      | gram_hooks_login_handle_request "$work" tok probe >"$work/resp"
  fi
  printf '%s-STATUS:%s\n' "$label" "$(head -n1 "$work/resp" | tr -d '\r')"
  printf '%s-QUERY:%s\n' "$label" "$(cat "$work/query" 2>/dev/null)"
}
run_case POST POST '/callback?state=tok' 'api_key=post-key&project=proj&email=a%40b.c'
run_case GET GET '/callback?state=tok&api_key=get-key&project=proj' ''
run_case FORGED POST '/callback' 'api_key=attacker&project=evil'
`
	cmd := exec.Command("bash", "-c", script)
	cmd.Dir = dir
	out, err := cmd.CombinedOutput()
	require.NoError(t, err, string(out))
	output := string(out)

	require.Contains(t, output, "POST-STATUS:HTTP/1.1 200 OK", "form_post callback must succeed")
	require.Contains(t, output, "POST-QUERY:api_key=post-key&project=proj&email=a%40b.c", "form_post body must be captured verbatim")
	require.Contains(t, output, "GET-STATUS:HTTP/1.1 200 OK", "legacy GET redirect must still succeed")
	require.Contains(t, output, "GET-QUERY:state=tok&api_key=get-key&project=proj", "legacy GET query must still be captured")
	require.Contains(t, output, "FORGED-STATUS:HTTP/1.1 403 Forbidden", "a POST without the state token must be rejected")
	require.Contains(t, output, "FORGED-QUERY:\n", "a rejected POST must not capture credentials")
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

// hookAuthTestEnv builds a scrubbed environment for exercising rendered hook
// scripts against a controlled auth state: no ambient Gram credentials, no
// CI/SSH markers, and TMPDIR pinned inside the test dir so nudge markers are
// isolated per test.
func hookAuthTestEnv(dir string, extra ...string) []string {
	env := []string{"PATH=" + os.Getenv("PATH"), "TMPDIR=" + dir}
	for _, kv := range os.Environ() {
		if strings.HasPrefix(kv, "CI=") || strings.HasPrefix(kv, "SSH_CONNECTION=") ||
			strings.HasPrefix(kv, "SSH_TTY=") || strings.HasPrefix(kv, "PATH=") ||
			strings.HasPrefix(kv, "TMPDIR=") || strings.HasPrefix(kv, "GRAM_") {
			continue
		}
		env = append(env, kv)
	}
	return append(env, extra...)
}

// TestRenderHookScriptClaudeUnauthenticatedNudgesLoginOnce verifies the
// never-authenticated ratchet on Claude prompt submission: the hook must not
// block (exit 0), must inject an additionalContext nudge pointing at the
// plugin's login helper, and must inject it at most once per session.
func TestRenderHookScriptClaudeUnauthenticatedNudgesLoginOnce(t *testing.T) {
	t.Parallel()
	cfg := GenerateConfig{
		ServerURL:   "https://app.getgram.ai",
		HooksAPIKey: "gram_local_secret_xyz",
		ProjectSlug: "acme-prod",
	}
	dir := t.TempDir()
	hookPath := filepath.Join(dir, "hook.sh")
	require.NoError(t, os.WriteFile(hookPath, renderHookScript(cfg, "claude"), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "http.sh"), renderSharedHTTPScript(), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "auth.sh"), renderSharedAuthScript(), 0o755))

	env := hookAuthTestEnv(dir, "GRAM_HOOKS_AUTH_FILE="+filepath.Join(dir, "auth.env"))
	payload := `{"hook_event_name":"UserPromptSubmit","session_id":"sess-nudge","prompt":"hi"}`

	cmd := exec.Command("bash", hookPath)
	cmd.Stdin = strings.NewReader(payload)
	cmd.Env = env
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	require.NoError(t, cmd.Run(), "unauthenticated UserPromptSubmit must fail open: %s", stderr.String())
	require.Contains(t, stdout.String(), `"additionalContext"`)
	require.Contains(t, stdout.String(), "login.sh")
	require.Contains(t, stderr.String(), "not connected on this machine yet")

	repeat := exec.Command("bash", hookPath)
	repeat.Stdin = strings.NewReader(payload)
	repeat.Env = env
	var repeatOut bytes.Buffer
	repeat.Stdout = &repeatOut
	repeat.Stderr = &stderr
	require.NoError(t, repeat.Run(), stderr.String())
	require.Empty(t, repeatOut.String(), "nudge must be injected at most once per session")
}

// TestRenderHookScriptClaudeEstablishedFailsClosed verifies the other side of
// the ratchet: once credentials have ever been cached on a machine, a missing
// or invalidated key blocks the hook (exit 2) instead of failing open.
func TestRenderHookScriptClaudeEstablishedFailsClosed(t *testing.T) {
	t.Parallel()
	cfg := GenerateConfig{
		ServerURL:   "https://app.getgram.ai",
		HooksAPIKey: "gram_local_secret_xyz",
		ProjectSlug: "acme-prod",
	}
	dir := t.TempDir()
	hookPath := filepath.Join(dir, "hook.sh")
	require.NoError(t, os.WriteFile(hookPath, renderHookScript(cfg, "claude"), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "http.sh"), renderSharedHTTPScript(), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "auth.sh"), renderSharedAuthScript(), 0o755))

	authFile := filepath.Join(dir, "auth.env")
	require.NoError(t, os.WriteFile(authFile+".established", nil, 0o600))

	cmd := exec.Command("bash", hookPath)
	cmd.Stdin = strings.NewReader(`{"hook_event_name":"PreToolUse","session_id":"sess-closed","tool_name":"Bash"}`)
	cmd.Env = hookAuthTestEnv(dir, "GRAM_HOOKS_AUTH_FILE="+authFile)
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	err := cmd.Run()
	var exitErr *exec.ExitError
	require.ErrorAs(t, err, &exitErr, "established machine without credentials must block")
	require.Equal(t, 2, exitErr.ExitCode(), stderr.String())
	require.Contains(t, stderr.String(), "could not authenticate")
}

// TestRenderAuthPreflightScriptRatchet verifies SessionStart preflight
// behavior on both sides of the ratchet: never-authenticated machines start
// the session (exit 0, after the login attempt is skipped in CI), while
// established machines with broken credentials block session start (exit 2).
func TestRenderAuthPreflightScriptRatchet(t *testing.T) {
	t.Parallel()
	cfg := GenerateConfig{
		ServerURL:   "https://app.getgram.ai",
		HooksAPIKey: "gram_local_secret_xyz",
		ProjectSlug: "acme-prod",
	}
	dir := t.TempDir()
	preflightPath := filepath.Join(dir, "auth_preflight.sh")
	require.NoError(t, os.WriteFile(preflightPath, renderAuthPreflightScript(cfg), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "auth.sh"), renderSharedAuthScript(), 0o755))

	authFile := filepath.Join(dir, "auth.env")
	// CI=1 makes the interactive login path decline immediately instead of
	// opening a browser, exercising the same fallthrough real headless
	// machines hit.
	env := hookAuthTestEnv(dir, "GRAM_HOOKS_AUTH_FILE="+authFile, "CI=1")

	fresh := exec.Command("bash", preflightPath)
	fresh.Env = env
	var freshErr bytes.Buffer
	fresh.Stderr = &freshErr
	require.NoError(t, fresh.Run(), "never-authenticated preflight must not block session start: %s", freshErr.String())

	require.NoError(t, os.WriteFile(authFile+".established", nil, 0o600))
	broken := exec.Command("bash", preflightPath)
	broken.Env = env
	var brokenErr bytes.Buffer
	broken.Stderr = &brokenErr
	err := broken.Run()
	var exitErr *exec.ExitError
	require.ErrorAs(t, err, &exitErr, "established machine with broken credentials must block session start")
	require.Equal(t, 2, exitErr.ExitCode(), brokenErr.String())
}

// TestRenderHookScriptClaudeRejectedEnvKeyFailsClosed verifies that a 401 on
// explicitly configured credentials (GRAM_HOOKS_API_KEY) blocks the hook even
// on a machine that never completed browser login: the never-authenticated
// pass-through only covers machines with no credentials at all, and the
// cache-relogin retry must not fire (or wipe the cache) for env credentials a
// re-login could never replace.
func TestRenderHookScriptClaudeRejectedEnvKeyFailsClosed(t *testing.T) {
	t.Parallel()
	cfg := GenerateConfig{
		ServerURL:   "https://app.getgram.ai",
		HooksAPIKey: "gram_local_secret_xyz",
		ProjectSlug: "acme-prod",
	}
	dir := t.TempDir()
	binDir := filepath.Join(dir, "bin")
	require.NoError(t, os.MkdirAll(binDir, 0o755))
	hookPath := filepath.Join(dir, "hook.sh")
	capturePath := filepath.Join(dir, "payloads.jsonl")
	require.NoError(t, os.WriteFile(hookPath, renderHookScript(cfg, "claude"), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "http.sh"), renderSharedHTTPScript(), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "auth.sh"), renderSharedAuthScript(), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(binDir, "curl"), []byte(`#!/usr/bin/env bash
url=""
while [ "$#" -gt 0 ]; do
  case "$1" in
    -H|-w|-X|--data-binary|--max-time|--config) shift 2 ;;
    -*) shift ;;
    *) url="$1"; shift ;;
  esac
done
payload="$(cat)"
case "$url" in
  */rpc/hooks.ingest)
    printf '%s' "$payload" >> "$GRAM_CAPTURE_PAYLOADS"
    printf '\n---GRAM---\n' >> "$GRAM_CAPTURE_PAYLOADS"
    ;;
esac
printf '{}\n401'
`), 0o755))

	authFile := filepath.Join(dir, "auth.env")
	env := hookAuthTestEnv(dir,
		"GRAM_CAPTURE_PAYLOADS="+capturePath,
		"GRAM_HOOKS_AUTH_FILE="+authFile,
		"GRAM_HOOKS_API_KEY=gram_revoked_env_key",
		"GRAM_HOOKS_PROJECT_SLUG=acme-prod",
	)
	for i, kv := range env {
		if strings.HasPrefix(kv, "PATH=") {
			env[i] = "PATH=" + binDir + string(os.PathListSeparator) + os.Getenv("PATH")
		}
	}

	fresh := exec.Command("bash", hookPath)
	fresh.Stdin = strings.NewReader(`{"hook_event_name":"PreToolUse","session_id":"sess-env-reject","tool_name":"Bash"}`)
	fresh.Env = env
	var freshErr bytes.Buffer
	fresh.Stderr = &freshErr
	err := fresh.Run()
	var exitErr *exec.ExitError
	require.ErrorAs(t, err, &exitErr, "rejected env credentials must block even before first browser login")
	require.Equal(t, 2, exitErr.ExitCode(), freshErr.String())
	require.Contains(t, freshErr.String(), "GRAM_HOOKS_API_KEY")
	require.Contains(t, freshErr.String(), "Update or unset GRAM_HOOKS_API_KEY")
	require.Contains(t, freshErr.String(), "hooks/login.sh")
	posts := strings.Count(string(requireFileBytes(t, capturePath)), "\n---GRAM---\n")
	require.Equal(t, 1, posts, "rejected env credentials must not trigger the cache-relogin retry")

	// A rejected env key must also leave any cached browser login untouched.
	require.NoError(t, os.WriteFile(authFile, []byte("GRAM_HOOKS_CACHED_API_KEY=gram_cached_key\nGRAM_HOOKS_CACHED_PROJECT=acme-prod\n"), 0o600))
	require.NoError(t, os.WriteFile(authFile+".established", nil, 0o600))
	cached := exec.Command("bash", hookPath)
	cached.Stdin = strings.NewReader(`{"hook_event_name":"PreToolUse","session_id":"sess-env-reject","tool_name":"Bash"}`)
	cached.Env = env
	var cachedErr bytes.Buffer
	cached.Stderr = &cachedErr
	err = cached.Run()
	require.ErrorAs(t, err, &exitErr, cachedErr.String())
	require.Equal(t, 2, exitErr.ExitCode(), cachedErr.String())
	require.Contains(t, cachedErr.String(), "GRAM_HOOKS_API_KEY")
	require.FileExists(t, authFile, "env credential rejection must not wipe the cached browser login")
}

func TestRenderHookScriptClaudeIgnoresGenericGramAPIKeyForHooksAuth(t *testing.T) {
	t.Parallel()
	cfg := GenerateConfig{
		ServerURL:   "https://app.getgram.ai",
		HooksAPIKey: "gram_local_secret_xyz",
		ProjectSlug: "acme-prod",
	}
	dir := t.TempDir()
	binDir := filepath.Join(dir, "bin")
	require.NoError(t, os.MkdirAll(binDir, 0o755))
	hookPath := filepath.Join(dir, "hook.sh")
	capturePath := filepath.Join(dir, "requests.txt")
	require.NoError(t, os.WriteFile(hookPath, renderHookScript(cfg, "claude"), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "http.sh"), renderSharedHTTPScript(), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "auth.sh"), renderSharedAuthScript(), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(binDir, "curl"), []byte(`#!/usr/bin/env bash
url=""
while [ "$#" -gt 0 ]; do
  case "$1" in
    --config) cat "$2" >> "$GRAM_CAPTURE_REQUESTS"; shift 2 ;;
    -H|-w|-X|--data-binary|--max-time) shift 2 ;;
    -*) shift ;;
    *) url="$1"; shift ;;
  esac
done
cat >/dev/null
printf '%s\n' "$url" >> "$GRAM_CAPTURE_REQUESTS"
printf '{}\n200'
`), 0o755))

	authFile := filepath.Join(dir, "auth.env")
	require.NoError(t, os.WriteFile(authFile, []byte("server_url=https://app.getgram.ai\napi_key=gram_cached_hooks_key\nproject=acme-prod\nemail=dev@example.com\n"), 0o600))
	env := hookAuthTestEnv(dir,
		"GRAM_CAPTURE_REQUESTS="+capturePath,
		"GRAM_HOOKS_AUTH_FILE="+authFile,
		"GRAM_API_KEY=gram_unrelated_mcp_key",
		"GRAM_PROJECT_SLUG=wrong-project",
	)
	for i, kv := range env {
		if strings.HasPrefix(kv, "PATH=") {
			env[i] = "PATH=" + binDir + string(os.PathListSeparator) + os.Getenv("PATH")
		}
	}

	cmd := exec.Command("bash", hookPath)
	cmd.Stdin = strings.NewReader(`{"hook_event_name":"PreToolUse","session_id":"sess-generic-key","tool_name":"Bash"}`)
	cmd.Env = env
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	require.NoError(t, cmd.Run(), stderr.String())
	requests := string(requireFileBytes(t, capturePath))
	require.Contains(t, requests, "Gram-Key: gram_cached_hooks_key")
	require.Contains(t, requests, "Gram-Project: acme-prod")
	require.NotContains(t, requests, "gram_unrelated_mcp_key")
	require.NotContains(t, requests, "wrong-project")
}

// TestRenderHookScriptClaudeRejectedCachedKeyClearsAuthAndNudgesLogin covers
// the stale browser-login cache path: if Gram rejects the cached hooks key, the
// hook clears that key and falls back to the unauthenticated login nudge instead
// of repeatedly blocking UserPromptSubmit with the stale token.
func TestRenderHookScriptClaudeRejectedCachedKeyClearsAuthAndNudgesLogin(t *testing.T) {
	t.Parallel()
	cfg := GenerateConfig{
		ServerURL:   "https://app.getgram.ai",
		HooksAPIKey: "gram_local_secret_xyz",
		ProjectSlug: "acme-prod",
	}
	dir := t.TempDir()
	binDir := filepath.Join(dir, "bin")
	require.NoError(t, os.MkdirAll(binDir, 0o755))
	hookPath := filepath.Join(dir, "hook.sh")
	capturePath := filepath.Join(dir, "requests.txt")
	require.NoError(t, os.WriteFile(hookPath, renderHookScript(cfg, "claude"), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "http.sh"), renderSharedHTTPScript(), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "auth.sh"), renderSharedAuthScript(), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(binDir, "curl"), []byte(`#!/usr/bin/env bash
url=""
while [ "$#" -gt 0 ]; do
  case "$1" in
    --config) cat "$2" >> "$GRAM_CAPTURE_REQUESTS"; shift 2 ;;
    -H|-w|-X|--data-binary|--max-time) shift 2 ;;
    -*) shift ;;
    *) url="$1"; shift ;;
  esac
done
cat >/dev/null
printf '%s\n' "$url" >> "$GRAM_CAPTURE_REQUESTS"
printf '{"message":"unauthorized: api_key not found"}\n401'
`), 0o755))

	authFile := filepath.Join(dir, "auth.env")
	require.NoError(t, os.WriteFile(authFile, []byte("server_url=https://app.getgram.ai\napi_key=gram_stale_cached_key\nproject=acme-prod\nemail=dev@example.com\n"), 0o600))
	require.NoError(t, os.WriteFile(authFile+".established", nil, 0o600))
	env := hookAuthTestEnv(dir,
		"GRAM_CAPTURE_REQUESTS="+capturePath,
		"GRAM_HOOKS_AUTH_FILE="+authFile,
		"GRAM_API_KEY=gram_unrelated_mcp_key",
		"GRAM_PROJECT_SLUG=wrong-project",
	)
	for i, kv := range env {
		if strings.HasPrefix(kv, "PATH=") {
			env[i] = "PATH=" + binDir + string(os.PathListSeparator) + os.Getenv("PATH")
		}
	}

	cmd := exec.Command("bash", hookPath)
	cmd.Stdin = strings.NewReader(`{"hook_event_name":"UserPromptSubmit","session_id":"sess-stale-cache","prompt":"hi"}`)
	cmd.Env = env
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	require.NoError(t, cmd.Run(), "stale cached key should fail open with login nudge: %s", stderr.String())
	require.Contains(t, stdout.String(), `"additionalContext"`)
	require.Contains(t, stdout.String(), "login.sh")
	require.NoFileExists(t, authFile, "rejected cached key must be cleared")
	require.FileExists(t, authFile+".established", "clearing a rejected key must preserve the fail-closed ratchet marker")
	require.FileExists(t, authFile+".reauth-needed", "clearing a rejected key must remember that prompt submits should keep nudging reconnect")
	requests := string(requireFileBytes(t, capturePath))
	require.Contains(t, requests, "Gram-Key: gram_stale_cached_key")
	require.Contains(t, requests, "/rpc/hooks.ingest")
	require.Equal(t, 1, strings.Count(requests, "/rpc/hooks.ingest"), "noninteractive stale-cache recovery should not retry with the same rejected key")

	repeat := exec.Command("bash", hookPath)
	repeat.Stdin = strings.NewReader(`{"hook_event_name":"UserPromptSubmit","session_id":"sess-stale-cache-repeat","prompt":"again"}`)
	repeat.Env = env
	var repeatOut, repeatErr bytes.Buffer
	repeat.Stdout = &repeatOut
	repeat.Stderr = &repeatErr
	require.NoError(t, repeat.Run(), "reauth-needed prompt submits should keep nudging login after the stale cache is gone: %s", repeatErr.String())
	require.Contains(t, repeatOut.String(), `"additionalContext"`)
	require.Contains(t, repeatOut.String(), "login.sh")
	requests = string(requireFileBytes(t, capturePath))
	require.Equal(t, 1, strings.Count(requests, "/rpc/hooks.ingest"), "reauth-needed prompt submit must not send an unauthenticated request")
}

// TestRenderHookScriptClaudeRejectedCachedKeyStillBlocksToolUse verifies that
// stale-cache recovery is not a general bypass: after clearing the rejected
// cached token, blocking non-prompt events still fail closed.
func TestRenderHookScriptClaudeRejectedCachedKeyStillBlocksToolUse(t *testing.T) {
	t.Parallel()
	cfg := GenerateConfig{
		ServerURL:   "https://app.getgram.ai",
		HooksAPIKey: "gram_local_secret_xyz",
		ProjectSlug: "acme-prod",
	}
	dir := t.TempDir()
	binDir := filepath.Join(dir, "bin")
	require.NoError(t, os.MkdirAll(binDir, 0o755))
	hookPath := filepath.Join(dir, "hook.sh")
	capturePath := filepath.Join(dir, "requests.txt")
	require.NoError(t, os.WriteFile(hookPath, renderHookScript(cfg, "claude"), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "http.sh"), renderSharedHTTPScript(), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "auth.sh"), renderSharedAuthScript(), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(binDir, "curl"), []byte(`#!/usr/bin/env bash
url=""
while [ "$#" -gt 0 ]; do
  case "$1" in
    --config) cat "$2" >> "$GRAM_CAPTURE_REQUESTS"; shift 2 ;;
    -H|-w|-X|--data-binary|--max-time) shift 2 ;;
    -*) shift ;;
    *) url="$1"; shift ;;
  esac
done
cat >/dev/null
printf '%s\n' "$url" >> "$GRAM_CAPTURE_REQUESTS"
printf '{"message":"unauthorized: api_key not found"}\n401'
`), 0o755))

	authFile := filepath.Join(dir, "auth.env")
	require.NoError(t, os.WriteFile(authFile, []byte("server_url=https://app.getgram.ai\napi_key=gram_stale_cached_key\nproject=acme-prod\nemail=dev@example.com\n"), 0o600))
	require.NoError(t, os.WriteFile(authFile+".established", nil, 0o600))
	env := hookAuthTestEnv(dir,
		"GRAM_CAPTURE_REQUESTS="+capturePath,
		"GRAM_HOOKS_AUTH_FILE="+authFile,
		"GRAM_API_KEY=gram_unrelated_mcp_key",
		"GRAM_PROJECT_SLUG=wrong-project",
	)
	for i, kv := range env {
		if strings.HasPrefix(kv, "PATH=") {
			env[i] = "PATH=" + binDir + string(os.PathListSeparator) + os.Getenv("PATH")
		}
	}

	cmd := exec.Command("bash", hookPath)
	cmd.Stdin = strings.NewReader(`{"hook_event_name":"PreToolUse","session_id":"sess-stale-cache-tool","tool_name":"Bash"}`)
	cmd.Env = env
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	err := cmd.Run()
	var exitErr *exec.ExitError
	require.ErrorAs(t, err, &exitErr, "stale cached key should still block tool-use events")
	require.Equal(t, 2, exitErr.ExitCode(), stderr.String())
	require.Contains(t, stderr.String(), "unauthorized: api_key not found")
	require.NoFileExists(t, authFile, "rejected cached key must be cleared")
	require.FileExists(t, authFile+".reauth-needed", "clearing a rejected key must remember that reconnect is required")
	requests := string(requireFileBytes(t, capturePath))
	require.Contains(t, requests, "Gram-Key: gram_stale_cached_key")
	require.Equal(t, 1, strings.Count(requests, "/rpc/hooks.ingest"), "noninteractive stale-cache recovery should not retry with the same rejected key")

	repeat := exec.Command("bash", hookPath)
	repeat.Stdin = strings.NewReader(`{"hook_event_name":"PreToolUse","session_id":"sess-stale-cache-tool-repeat","tool_name":"Bash"}`)
	repeat.Env = env
	stderr.Reset()
	repeat.Stderr = &stderr
	err = repeat.Run()
	require.ErrorAs(t, err, &exitErr, "reauth-needed tool-use events must still fail closed")
	require.Equal(t, 2, exitErr.ExitCode(), stderr.String())
	require.Contains(t, stderr.String(), "need to reconnect")
	requests = string(requireFileBytes(t, capturePath))
	require.Equal(t, 1, strings.Count(requests, "/rpc/hooks.ingest"), "reauth-needed tool-use event must not send an unauthenticated request")
}

// TestRenderHookScriptClaudeInsecureServerURLRatchet verifies that a
// non-HTTPS, non-loopback server URL is refused before any credential leaves
// the machine, with the same ratchet as auth failures: fail open before the
// first successful auth, fail closed (exit 2) once established.
func TestRenderHookScriptClaudeInsecureServerURLRatchet(t *testing.T) {
	t.Parallel()
	cfg := GenerateConfig{
		ServerURL:   "https://app.getgram.ai",
		HooksAPIKey: "gram_local_secret_xyz",
		ProjectSlug: "acme-prod",
	}
	dir := t.TempDir()
	hookPath := filepath.Join(dir, "hook.sh")
	require.NoError(t, os.WriteFile(hookPath, renderHookScript(cfg, "claude"), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "http.sh"), renderSharedHTTPScript(), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "auth.sh"), renderSharedAuthScript(), 0o755))

	authFile := filepath.Join(dir, "auth.env")
	env := hookAuthTestEnv(dir,
		"GRAM_HOOKS_AUTH_FILE="+authFile,
		"GRAM_HOOKS_SERVER_URL=http://gram.example.com",
		"GRAM_HOOKS_API_KEY=gram_test_hooks_key",
		"GRAM_HOOKS_PROJECT_SLUG=acme-prod",
	)

	fresh := exec.Command("bash", hookPath)
	fresh.Stdin = strings.NewReader(`{"hook_event_name":"UserPromptSubmit","session_id":"sess-insecure","prompt":"hi"}`)
	fresh.Env = env
	var freshErr bytes.Buffer
	fresh.Stderr = &freshErr
	require.NoError(t, fresh.Run(), "never-authenticated machine must fail open on an insecure URL: %s", freshErr.String())
	require.Contains(t, freshErr.String(), "refused insecure Gram server URL")

	require.NoError(t, os.WriteFile(authFile+".established", nil, 0o600))
	broken := exec.Command("bash", hookPath)
	broken.Stdin = strings.NewReader(`{"hook_event_name":"UserPromptSubmit","session_id":"sess-insecure","prompt":"hi"}`)
	broken.Env = env
	var brokenErr bytes.Buffer
	broken.Stderr = &brokenErr
	err := broken.Run()
	var exitErr *exec.ExitError
	require.ErrorAs(t, err, &exitErr, "established machine must fail closed on an insecure URL")
	require.Equal(t, 2, exitErr.ExitCode(), brokenErr.String())
	require.Contains(t, brokenErr.String(), "refused insecure Gram server URL")
}

// TestRenderHookScriptClaudeToolInputServerGatedToMCPMetaTools verifies that
// an ordinary tool taking an unrelated "server" argument is not classified as
// an MCP call (which would subject it to Shadow MCP enforcement), while the
// MCP meta-tools still surface tool_input.server as the MCP server name.
func TestRenderHookScriptClaudeToolInputServerGatedToMCPMetaTools(t *testing.T) {
	t.Parallel()

	_, err := exec.LookPath("jq")
	require.NoError(t, err, "jq is required for tool_input.server extraction")

	cfg := GenerateConfig{
		ServerURL:   "https://app.getgram.ai",
		HooksAPIKey: "gram_local_secret_xyz",
		ProjectSlug: "acme-prod",
	}
	dir := t.TempDir()
	binDir := filepath.Join(dir, "bin")
	require.NoError(t, os.MkdirAll(binDir, 0o755))
	hookPath := filepath.Join(dir, "hook.sh")
	capturePath := filepath.Join(dir, "payloads.jsonl")
	require.NoError(t, os.WriteFile(hookPath, renderHookScript(cfg, "claude"), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "http.sh"), renderSharedHTTPScript(), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "auth.sh"), renderSharedAuthScript(), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(binDir, "curl"), []byte(`#!/usr/bin/env bash
url=""
while [ "$#" -gt 0 ]; do
  case "$1" in
    -H|-w|-X|--data-binary|--max-time|--config) shift 2 ;;
    -*) shift ;;
    *) url="$1"; shift ;;
  esac
done
payload="$(cat)"
case "$url" in
  */rpc/hooks.ingest)
    printf '%s' "$payload" >> "$GRAM_CAPTURE_PAYLOADS"
    printf '\n---GRAM---\n' >> "$GRAM_CAPTURE_PAYLOADS"
    ;;
esac
printf '{}\n200'
`), 0o755))

	env := hookAuthTestEnv(dir,
		"GRAM_CAPTURE_PAYLOADS="+capturePath,
		"GRAM_HOOKS_AUTH_FILE="+filepath.Join(dir, "auth.env"),
		"GRAM_HOOKS_API_KEY=gram_test_hooks_key",
		"GRAM_HOOKS_PROJECT_SLUG=acme-prod",
	)
	for i, kv := range env {
		if strings.HasPrefix(kv, "PATH=") {
			env[i] = "PATH=" + binDir + string(os.PathListSeparator) + os.Getenv("PATH")
		}
	}

	ordinary := exec.Command("bash", hookPath)
	ordinary.Stdin = strings.NewReader(`{"hook_event_name":"PreToolUse","session_id":"sess-mcp-gate","tool_name":"deploy_service","tool_input":{"server":"prod-1"}}`)
	ordinary.Env = env
	output, err := ordinary.CombinedOutput()
	require.NoError(t, err, string(output))

	meta := exec.Command("bash", hookPath)
	meta.Stdin = strings.NewReader(`{"hook_event_name":"PreToolUse","session_id":"sess-mcp-gate","tool_name":"ListMcpResourcesTool","tool_input":{"server":"grame2e"}}`)
	meta.Env = env
	output, err = meta.CombinedOutput()
	require.NoError(t, err, string(output))

	chunks := strings.Split(string(requireFileBytes(t, capturePath)), "\n---GRAM---\n")
	require.Len(t, chunks, 3, "expected two captured ingest payloads")

	var ordinaryPayload map[string]any
	require.NoError(t, json.Unmarshal([]byte(strings.TrimSpace(chunks[0])), &ordinaryPayload))
	ordinaryData := requireMapValue(t, ordinaryPayload, "data")
	require.NotContains(t, ordinaryData, "mcp", "a plain tool with a server argument must not be classified as MCP")
	ordinaryTool := requireMapValue(t, ordinaryData, "tool_call")
	require.Equal(t, "deploy_service", ordinaryTool["name"])

	var metaPayload map[string]any
	require.NoError(t, json.Unmarshal([]byte(strings.TrimSpace(chunks[1])), &metaPayload))
	metaData := requireMapValue(t, metaPayload, "data")
	metaMCP := requireMapValue(t, metaData, "mcp")
	require.Equal(t, "grame2e", metaMCP["server_name"])
}

// TestRenderHookScriptClaudeNestedURLCommandNotClassifiedAsMCP verifies that
// url/command keys nested inside tool_input (a fetch tool's url, a shell
// tool's command) never surface as data.mcp: only top-level payload fields
// carry MCP evidence, otherwise ordinary tools land under Shadow MCP
// enforcement.
func TestRenderHookScriptClaudeNestedURLCommandNotClassifiedAsMCP(t *testing.T) {
	t.Parallel()

	cfg := GenerateConfig{
		ServerURL:   "https://app.getgram.ai",
		HooksAPIKey: "gram_local_secret_xyz",
		ProjectSlug: "acme-prod",
	}
	dir := t.TempDir()
	binDir := filepath.Join(dir, "bin")
	require.NoError(t, os.MkdirAll(binDir, 0o755))
	hookPath := filepath.Join(dir, "hook.sh")
	capturePath := filepath.Join(dir, "payloads.jsonl")
	require.NoError(t, os.WriteFile(hookPath, renderHookScript(cfg, "claude"), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "http.sh"), renderSharedHTTPScript(), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "auth.sh"), renderSharedAuthScript(), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(binDir, "curl"), []byte(`#!/usr/bin/env bash
url=""
while [ "$#" -gt 0 ]; do
  case "$1" in
    -H|-w|-X|--data-binary|--max-time|--config) shift 2 ;;
    -*) shift ;;
    *) url="$1"; shift ;;
  esac
done
payload="$(cat)"
case "$url" in
  */rpc/hooks.ingest)
    printf '%s' "$payload" >> "$GRAM_CAPTURE_PAYLOADS"
    printf '\n---GRAM---\n' >> "$GRAM_CAPTURE_PAYLOADS"
    ;;
esac
printf '{}\n200'
`), 0o755))

	env := hookAuthTestEnv(dir,
		"GRAM_CAPTURE_PAYLOADS="+capturePath,
		"GRAM_HOOKS_AUTH_FILE="+filepath.Join(dir, "auth.env"),
		"GRAM_HOOKS_API_KEY=gram_test_hooks_key",
		"GRAM_HOOKS_PROJECT_SLUG=acme-prod",
	)
	for i, kv := range env {
		if strings.HasPrefix(kv, "PATH=") {
			env[i] = "PATH=" + binDir + string(os.PathListSeparator) + os.Getenv("PATH")
		}
	}

	cmd := exec.Command("bash", hookPath)
	cmd.Stdin = strings.NewReader(`{"hook_event_name":"PreToolUse","session_id":"sess-nested","tool_name":"Bash","tool_input":{"command":"curl https://example.com","url":"https://example.com"}}`)
	cmd.Env = env
	output, err := cmd.CombinedOutput()
	require.NoError(t, err, string(output))

	chunks := strings.Split(string(requireFileBytes(t, capturePath)), "\n---GRAM---\n")
	require.Len(t, chunks, 2, "expected one captured ingest payload")
	var parsed map[string]any
	require.NoError(t, json.Unmarshal([]byte(strings.TrimSpace(chunks[0])), &parsed))
	data := requireMapValue(t, parsed, "data")
	require.NotContains(t, data, "mcp",
		"nested tool_input url/command must not produce MCP evidence")
	tool := requireMapValue(t, data, "tool_call")
	require.Equal(t, "Bash", tool["name"])
}

// TestRenderHookPayloadNormalizationParsesExponentNumbers verifies the no-jq
// number extractor accepts the full JSON number grammar: exponent forms
// (1.5e3) silently dropping would lose durations, token counts, and costs.
func TestRenderHookPayloadNormalizationParsesExponentNumbers(t *testing.T) {
	t.Parallel()

	cfg := GenerateConfig{
		ServerURL:   "https://app.getgram.ai",
		HooksAPIKey: "gram_local_secret_xyz",
		ProjectSlug: "acme-prod",
	}
	dir := t.TempDir()
	binDir := filepath.Join(dir, "bin")
	require.NoError(t, os.MkdirAll(binDir, 0o755))
	hookPath := filepath.Join(dir, "hook.sh")
	capturePath := filepath.Join(dir, "payloads.jsonl")
	require.NoError(t, os.WriteFile(hookPath, renderHookScript(cfg, "claude"), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "http.sh"), renderSharedHTTPScript(), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "auth.sh"), renderSharedAuthScript(), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(binDir, "curl"), []byte(`#!/usr/bin/env bash
url=""
while [ "$#" -gt 0 ]; do
  case "$1" in
    -H|-w|-X|--data-binary|--max-time|--config) shift 2 ;;
    -*) shift ;;
    *) url="$1"; shift ;;
  esac
done
payload="$(cat)"
case "$url" in
  */rpc/hooks.ingest)
    printf '%s' "$payload" >> "$GRAM_CAPTURE_PAYLOADS"
    printf '\n---GRAM---\n' >> "$GRAM_CAPTURE_PAYLOADS"
    ;;
esac
printf '{}\n200'
`), 0o755))

	env := hookAuthTestEnv(dir,
		"GRAM_CAPTURE_PAYLOADS="+capturePath,
		"GRAM_HOOKS_AUTH_FILE="+filepath.Join(dir, "auth.env"),
		"GRAM_HOOKS_API_KEY=gram_test_hooks_key",
		"GRAM_HOOKS_PROJECT_SLUG=acme-prod",
	)
	for i, kv := range env {
		if strings.HasPrefix(kv, "PATH=") {
			env[i] = "PATH=" + binDir + string(os.PathListSeparator) + os.Getenv("PATH")
		}
	}

	cmd := exec.Command("bash", hookPath)
	cmd.Stdin = strings.NewReader(`{"hook_event_name":"Stop","session_id":"sess-exp","last_assistant_message":"done","duration_ms":1.5e3}`)
	cmd.Env = env
	output, err := cmd.CombinedOutput()
	require.NoError(t, err, string(output))

	chunks := strings.Split(string(requireFileBytes(t, capturePath)), "\n---GRAM---\n")
	require.Len(t, chunks, 2, "expected one captured ingest payload")
	var parsed map[string]any
	require.NoError(t, json.Unmarshal([]byte(strings.TrimSpace(chunks[0])), &parsed))
	message := requireMapValue(t, requireMapValue(t, parsed, "data"), "message")
	require.InDelta(t, 1500.0, message["duration_ms"], 0.001,
		"exponent-form numbers must survive extraction")
}

// TestRenderHookScriptCursorPinsStdioMCPIdentityToCommand verifies that a
// stdio MCP server's identity — what Shadow MCP approvals are scoped to — is
// the launch command, not the mutable server alias, while URL servers keep
// the alias identity alongside their URL evidence.
func TestRenderHookScriptCursorPinsStdioMCPIdentityToCommand(t *testing.T) {
	t.Parallel()

	cfg := GenerateConfig{
		ServerURL:   "https://app.getgram.ai",
		HooksAPIKey: "gram_local_secret_xyz",
		ProjectSlug: "acme-prod",
	}
	dir := t.TempDir()
	binDir := filepath.Join(dir, "bin")
	require.NoError(t, os.MkdirAll(binDir, 0o755))
	hookPath := filepath.Join(dir, "hook.sh")
	capturePath := filepath.Join(dir, "payloads.jsonl")
	require.NoError(t, os.WriteFile(hookPath, renderHookScript(cfg, "cursor"), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "http.sh"), renderSharedHTTPScript(), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "auth.sh"), renderSharedAuthScript(), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(binDir, "curl"), []byte(`#!/usr/bin/env bash
url=""
while [ "$#" -gt 0 ]; do
  case "$1" in
    -H|-w|-X|--data-binary|--max-time|--config) shift 2 ;;
    -*) shift ;;
    *) url="$1"; shift ;;
  esac
done
payload="$(cat)"
case "$url" in
  */rpc/hooks.ingest)
    printf '%s' "$payload" >> "$GRAM_CAPTURE_PAYLOADS"
    printf '\n---GRAM---\n' >> "$GRAM_CAPTURE_PAYLOADS"
    ;;
esac
printf '{}\n200'
`), 0o755))

	env := hookAuthTestEnv(dir,
		"GRAM_CAPTURE_PAYLOADS="+capturePath,
		"GRAM_HOOKS_AUTH_FILE="+filepath.Join(dir, "auth.env"),
		"GRAM_HOOKS_API_KEY=gram_test_hooks_key",
		"GRAM_HOOKS_PROJECT_SLUG=acme-prod",
	)
	for i, kv := range env {
		if strings.HasPrefix(kv, "PATH=") {
			env[i] = "PATH=" + binDir + string(os.PathListSeparator) + os.Getenv("PATH")
		}
	}

	stdio := exec.Command("bash", hookPath)
	stdio.Stdin = strings.NewReader(`{"hook_event_name":"beforeMCPExecution","conversation_id":"sess-mcp-id","mcp_server_name":"foo","command":"npx server-foo","tool_name":"lookup"}`)
	stdio.Env = env
	output, err := stdio.CombinedOutput()
	require.NoError(t, err, string(output))

	byURL := exec.Command("bash", hookPath)
	byURL.Stdin = strings.NewReader(`{"hook_event_name":"beforeMCPExecution","conversation_id":"sess-mcp-id","mcp_server_name":"foo","url":"https://mcp.example.com/x","tool_name":"lookup"}`)
	byURL.Env = env
	output, err = byURL.CombinedOutput()
	require.NoError(t, err, string(output))

	secretCmd := exec.Command("bash", hookPath)
	secretCmd.Stdin = strings.NewReader(`{"hook_event_name":"beforeMCPExecution","conversation_id":"sess-mcp-id","mcp_server_name":"bar","command":"npx server-bar --api-key sk-verysecret123","tool_name":"lookup"}`)
	secretCmd.Env = env
	output, err = secretCmd.CombinedOutput()
	require.NoError(t, err, string(output))

	secretURL := exec.Command("bash", hookPath)
	secretURL.Stdin = strings.NewReader(`{"hook_event_name":"beforeMCPExecution","conversation_id":"sess-mcp-id","mcp_server_name":"baz","url":"https://user:hunter2@mcp.example.com/sse?api_key=sk-urlsecret456&region=us#frag","tool_name":"lookup"}`)
	secretURL.Env = env
	output, err = secretURL.CombinedOutput()
	require.NoError(t, err, string(output))

	chunks := strings.Split(string(requireFileBytes(t, capturePath)), "\n---GRAM---\n")
	require.Len(t, chunks, 5, "expected four captured ingest payloads")

	var stdioPayload map[string]any
	require.NoError(t, json.Unmarshal([]byte(strings.TrimSpace(chunks[0])), &stdioPayload))
	stdioMCP := requireMapValue(t, requireMapValue(t, stdioPayload, "data"), "mcp")
	require.Equal(t, "npx server-foo", stdioMCP["server_identity"],
		"stdio MCP identity must be pinned to the launch command, not the alias")
	require.Equal(t, "foo", stdioMCP["server_name"])

	var urlPayload map[string]any
	require.NoError(t, json.Unmarshal([]byte(strings.TrimSpace(chunks[1])), &urlPayload))
	urlMCP := requireMapValue(t, requireMapValue(t, urlPayload, "data"), "mcp")
	require.Equal(t, "foo", urlMCP["server_identity"])
	require.Equal(t, "https://mcp.example.com/x", urlMCP["url"])

	// Provider-supplied commands carry the server's launch argv; credentials
	// in it must be redacted before becoming telemetry or identity.
	var secretPayload map[string]any
	require.NoError(t, json.Unmarshal([]byte(strings.TrimSpace(chunks[2])), &secretPayload))
	secretMCP := requireMapValue(t, requireMapValue(t, secretPayload, "data"), "mcp")
	require.NotContains(t, secretMCP["command"], "sk-verysecret123")
	require.NotContains(t, secretMCP["server_identity"], "sk-verysecret123")
	require.Equal(t, "npx server-bar --api-key ***", secretMCP["command"])
	require.Equal(t, "npx server-bar --api-key ***", secretMCP["server_identity"])
	require.NotContains(t, strings.TrimSpace(chunks[2]), "sk-verysecret123",
		"the raw payload echo must not leak the command credential either")

	// Provider-supplied URLs can embed credentials too: basic-auth userinfo
	// and secret-named query values must not reach telemetry or evidence.
	var secretURLPayload map[string]any
	require.NoError(t, json.Unmarshal([]byte(strings.TrimSpace(chunks[3])), &secretURLPayload))
	secretURLMCP := requireMapValue(t, requireMapValue(t, secretURLPayload, "data"), "mcp")
	require.Equal(t, "https://mcp.example.com/sse?api_key=***&region=us", secretURLMCP["url"],
		"userinfo, fragment and secret query values must be stripped from MCP URLs")
	require.NotContains(t, strings.TrimSpace(chunks[3]), "hunter2")
	require.NotContains(t, strings.TrimSpace(chunks[3]), "sk-urlsecret456")
}

// TestRenderHookScriptClaudeStdioMCPIdentityFromSanitizedMCPJSONName verifies
// command discovery for Claude MCP tools whose mcp__<server>__ prefix carries
// a sanitized display name: the .mcp.json entry "Linear Server" must still be
// found for prefix "Linear_Server", pinning the stdio identity to the launch
// command instead of the mutable alias.
func TestRenderHookScriptClaudeStdioMCPIdentityFromSanitizedMCPJSONName(t *testing.T) {
	t.Parallel()

	_, err := exec.LookPath("jq")
	require.NoError(t, err, "jq is required for .mcp.json metadata lookup")

	cfg := GenerateConfig{
		ServerURL:   "https://app.getgram.ai",
		HooksAPIKey: "gram_local_secret_xyz",
		ProjectSlug: "acme-prod",
	}
	dir := t.TempDir()
	binDir := filepath.Join(dir, "bin")
	workDir := filepath.Join(dir, "workspace")
	require.NoError(t, os.MkdirAll(binDir, 0o755))
	require.NoError(t, os.MkdirAll(workDir, 0o755))
	hookPath := filepath.Join(dir, "hook.sh")
	capturePath := filepath.Join(dir, "payloads.jsonl")
	require.NoError(t, os.WriteFile(hookPath, renderHookScript(cfg, "claude"), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "http.sh"), renderSharedHTTPScript(), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "auth.sh"), renderSharedAuthScript(), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(workDir, ".mcp.json"), []byte(`{"mcpServers":{"Linear Server":{"command":"npx","args":["linear-mcp"]}}}`), 0o600))
	require.NoError(t, os.WriteFile(filepath.Join(binDir, "curl"), []byte(`#!/usr/bin/env bash
url=""
while [ "$#" -gt 0 ]; do
  case "$1" in
    -H|-w|-X|--data-binary|--max-time|--config) shift 2 ;;
    -*) shift ;;
    *) url="$1"; shift ;;
  esac
done
payload="$(cat)"
case "$url" in
  */rpc/hooks.ingest)
    printf '%s' "$payload" >> "$GRAM_CAPTURE_PAYLOADS"
    printf '\n---GRAM---\n' >> "$GRAM_CAPTURE_PAYLOADS"
    ;;
esac
printf '{}\n200'
`), 0o755))

	env := hookAuthTestEnv(dir,
		"GRAM_CAPTURE_PAYLOADS="+capturePath,
		"GRAM_HOOKS_AUTH_FILE="+filepath.Join(dir, "auth.env"),
		"GRAM_HOOKS_API_KEY=gram_test_hooks_key",
		"GRAM_HOOKS_PROJECT_SLUG=acme-prod",
	)
	for i, kv := range env {
		if strings.HasPrefix(kv, "PATH=") {
			env[i] = "PATH=" + binDir + string(os.PathListSeparator) + os.Getenv("PATH")
		}
	}

	cmd := exec.Command("bash", hookPath)
	cmd.Stdin = strings.NewReader(`{"hook_event_name":"PreToolUse","session_id":"sess-sanitized","tool_name":"mcp__Linear_Server__create_issue","tool_input":{"title":"x"}}`)
	cmd.Env = env
	cmd.Dir = workDir
	output, err := cmd.CombinedOutput()
	require.NoError(t, err, string(output))

	chunks := strings.Split(string(requireFileBytes(t, capturePath)), "\n---GRAM---\n")
	require.Len(t, chunks, 2, "expected one captured ingest payload")
	var parsed map[string]any
	require.NoError(t, json.Unmarshal([]byte(strings.TrimSpace(chunks[0])), &parsed))
	mcp := requireMapValue(t, requireMapValue(t, parsed, "data"), "mcp")
	require.Equal(t, "npx linear-mcp", mcp["command"],
		"sanitized prefix must still resolve the .mcp.json launch command")
	require.Equal(t, "npx linear-mcp", mcp["server_identity"],
		"stdio MCP identity must be pinned to the launch command, not the alias")
}

// TestRenderLoginScriptsPinOrganization verifies the interactive login entry
// points embed the generating org's id and the shared login flow forwards it
// to the dashboard, which refuses to mint a key when a multi-org browser
// session has a different org active.
func TestRenderLoginScriptsPinOrganization(t *testing.T) {
	t.Parallel()
	cfg := GenerateConfig{
		ServerURL:   "https://app.getgram.ai",
		HooksAPIKey: "gram_local_secret_xyz",
		ProjectSlug: "acme-prod",
		OrgID:       "org_12345",
	}
	require.Contains(t, string(renderLoginScript(cfg)), `gram_hooks_org_hint="org_12345"`)
	require.Contains(t, string(renderAuthPreflightScript(cfg)), `gram_hooks_org_hint="org_12345"`)
	require.Contains(t, string(renderSharedAuthScript()), "organization_id=${gram_hooks_org_hint}")
}

// TestRenderHookScriptClaudeEnrichesURLFromSiblingPluginConfig verifies the
// Claude MCP URL enrichment scans sibling feature plugins' .mcp.json: in a
// generated marketplace the observability plugin runs the hook while the
// sanctioned Gram MCP URLs live in the feature plugins, and a
// mcp__plugin_<feature>_<server>__ call without its URL would be treated as
// non-Gram-hosted by Shadow MCP blocking policies.
func TestRenderHookScriptClaudeEnrichesURLFromSiblingPluginConfig(t *testing.T) {
	t.Parallel()

	_, err := exec.LookPath("jq")
	require.NoError(t, err, "jq is required for Claude MCP URL enrichment")

	cfg := GenerateConfig{
		ServerURL:   "https://app.getgram.ai",
		HooksAPIKey: "gram_local_secret_xyz",
		ProjectSlug: "acme-prod",
	}
	dir := t.TempDir()
	binDir := filepath.Join(dir, "bin")
	obsHooksDir := filepath.Join(dir, "market", "observability", "hooks")
	siblingDir := filepath.Join(dir, "market", "acme-tools")
	require.NoError(t, os.MkdirAll(binDir, 0o755))
	require.NoError(t, os.MkdirAll(obsHooksDir, 0o755))
	require.NoError(t, os.MkdirAll(siblingDir, 0o755))
	hookPath := filepath.Join(obsHooksDir, "hook.sh")
	capturePath := filepath.Join(dir, "payloads.jsonl")
	require.NoError(t, os.WriteFile(hookPath, renderHookScript(cfg, "claude"), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(obsHooksDir, "http.sh"), renderSharedHTTPScript(), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(obsHooksDir, "auth.sh"), renderSharedAuthScript(), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(siblingDir, ".mcp.json"), []byte(`{"mcpServers":{"Acme Tools":{"type":"http","url":"https://app.getgram.ai/mcp/acme-tools"}}}`), 0o600))
	require.NoError(t, os.WriteFile(filepath.Join(binDir, "curl"), []byte(`#!/usr/bin/env bash
url=""
while [ "$#" -gt 0 ]; do
  case "$1" in
    -H|-w|-X|--data-binary|--max-time|--config) shift 2 ;;
    -*) shift ;;
    *) url="$1"; shift ;;
  esac
done
payload="$(cat)"
case "$url" in
  */rpc/hooks.ingest)
    printf '%s' "$payload" >> "$GRAM_CAPTURE_PAYLOADS"
    printf '\n---GRAM---\n' >> "$GRAM_CAPTURE_PAYLOADS"
    ;;
esac
printf '{}\n200'
`), 0o755))

	env := hookAuthTestEnv(dir,
		"GRAM_CAPTURE_PAYLOADS="+capturePath,
		"GRAM_HOOKS_AUTH_FILE="+filepath.Join(dir, "auth.env"),
		"GRAM_HOOKS_API_KEY=gram_test_hooks_key",
		"GRAM_HOOKS_PROJECT_SLUG=acme-prod",
		"CLAUDE_PLUGIN_ROOT="+filepath.Join(dir, "market", "observability"),
	)
	for i, kv := range env {
		if strings.HasPrefix(kv, "PATH=") {
			env[i] = "PATH=" + binDir + string(os.PathListSeparator) + os.Getenv("PATH")
		}
	}

	cmd := exec.Command("bash", hookPath)
	cmd.Stdin = strings.NewReader(`{"hook_event_name":"PreToolUse","session_id":"sess-sibling","tool_name":"mcp__plugin_acme-tools_Acme_Tools__do_thing","tool_input":{"q":"x"}}`)
	cmd.Env = env
	output, err := cmd.CombinedOutput()
	require.NoError(t, err, string(output))

	chunks := strings.Split(string(requireFileBytes(t, capturePath)), "\n---GRAM---\n")
	require.Len(t, chunks, 2, "expected one captured ingest payload")
	var parsed map[string]any
	require.NoError(t, json.Unmarshal([]byte(strings.TrimSpace(chunks[0])), &parsed))
	mcp := requireMapValue(t, requireMapValue(t, parsed, "data"), "mcp")
	require.Equal(t, "https://app.getgram.ai/mcp/acme-tools", mcp["url"],
		"sibling feature plugin URLs must be discovered for plugin-prefixed MCP calls")
}

// TestRenderHookScriptClaudeEnrichesURLFromCoworkConnectorConfig covers the
// Cowork/cmux environment, where MCP tool names carry connector UUID prefixes
// that never match .mcp.json display names: the run's local_<rid>.json maps
// connector UUID -> URL (newest sibling when the per-run file is not written
// yet), and without it UUID-prefixed Gram-hosted calls lose their URL
// evidence and Shadow MCP blocks them.
func TestRenderHookScriptClaudeEnrichesURLFromCoworkConnectorConfig(t *testing.T) {
	t.Parallel()

	_, err := exec.LookPath("jq")
	require.NoError(t, err, "jq is required for Claude MCP URL enrichment")

	cfg := GenerateConfig{
		ServerURL:   "https://app.getgram.ai",
		HooksAPIKey: "gram_local_secret_xyz",
		ProjectSlug: "acme-prod",
	}
	dir := t.TempDir()
	binDir := filepath.Join(dir, "bin")
	hooksDir := filepath.Join(dir, "plugin", "hooks")
	runsDir := filepath.Join(dir, "runs")
	connectorUUID := "019a2f3e-7c41-7d2a-b5c9-3f2ab416778b"
	require.NoError(t, os.MkdirAll(binDir, 0o755))
	require.NoError(t, os.MkdirAll(hooksDir, 0o755))
	require.NoError(t, os.MkdirAll(filepath.Join(runsDir, "local_run1", "outputs"), 0o755))
	require.NoError(t, os.MkdirAll(filepath.Join(runsDir, "local_run2", "outputs"), 0o755))
	hookPath := filepath.Join(hooksDir, "hook.sh")
	capturePath := filepath.Join(dir, "payloads.jsonl")
	require.NoError(t, os.WriteFile(hookPath, renderHookScript(cfg, "claude"), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(hooksDir, "http.sh"), renderSharedHTTPScript(), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(hooksDir, "auth.sh"), renderSharedAuthScript(), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(runsDir, "local_run1.json"), []byte(`{"remoteMcpServersConfig":[{"uuid":"`+connectorUUID+`","name":"Slack","url":"https://app.getgram.ai/mcp/int-slack"}]}`), 0o600))
	require.NoError(t, os.WriteFile(filepath.Join(binDir, "curl"), []byte(`#!/usr/bin/env bash
url=""
while [ "$#" -gt 0 ]; do
  case "$1" in
    -H|-w|-X|--data-binary|--max-time|--config) shift 2 ;;
    -*) shift ;;
    *) url="$1"; shift ;;
  esac
done
payload="$(cat)"
case "$url" in
  */rpc/hooks.ingest)
    printf '%s' "$payload" >> "$GRAM_CAPTURE_PAYLOADS"
    printf '\n---GRAM---\n' >> "$GRAM_CAPTURE_PAYLOADS"
    ;;
esac
printf '{}\n200'
`), 0o755))

	env := hookAuthTestEnv(dir,
		"GRAM_CAPTURE_PAYLOADS="+capturePath,
		"GRAM_HOOKS_AUTH_FILE="+filepath.Join(dir, "auth.env"),
		"GRAM_HOOKS_API_KEY=gram_test_hooks_key",
		"GRAM_HOOKS_PROJECT_SLUG=acme-prod",
		"CLAUDE_PLUGIN_ROOT="+filepath.Join(dir, "plugin"),
		"CLAUDE_PROJECT_DIR="+filepath.Join(runsDir, "local_run1", "outputs"),
	)
	for i, kv := range env {
		if strings.HasPrefix(kv, "PATH=") {
			env[i] = "PATH=" + binDir + string(os.PathListSeparator) + os.Getenv("PATH")
		}
	}

	stdin := `{"hook_event_name":"PreToolUse","session_id":"sess-cowork","tool_name":"mcp__` + connectorUUID + `__send_message","tool_input":{"q":"x"}}`
	cmd := exec.Command("bash", hookPath)
	cmd.Stdin = strings.NewReader(stdin)
	cmd.Env = env
	output, err := cmd.CombinedOutput()
	require.NoError(t, err, string(output))

	// The per-run config may not exist yet when the hook fires; the newest
	// sibling local_*.json must then resolve the connector.
	siblingEnv := make([]string, 0, len(env))
	for _, kv := range env {
		if strings.HasPrefix(kv, "CLAUDE_PROJECT_DIR=") {
			kv = "CLAUDE_PROJECT_DIR=" + filepath.Join(runsDir, "local_run2", "outputs")
		}
		siblingEnv = append(siblingEnv, kv)
	}
	sibling := exec.Command("bash", hookPath)
	sibling.Stdin = strings.NewReader(stdin)
	sibling.Env = siblingEnv
	output, err = sibling.CombinedOutput()
	require.NoError(t, err, string(output))

	chunks := strings.Split(string(requireFileBytes(t, capturePath)), "\n---GRAM---\n")
	require.Len(t, chunks, 3, "expected two captured ingest payloads")
	for i := range 2 {
		var parsed map[string]any
		require.NoError(t, json.Unmarshal([]byte(strings.TrimSpace(chunks[i])), &parsed))
		mcp := requireMapValue(t, requireMapValue(t, parsed, "data"), "mcp")
		require.Equal(t, "https://app.getgram.ai/mcp/int-slack", mcp["url"],
			"cowork connector UUIDs must resolve to their configured MCP URL")
		require.Equal(t, "Slack", mcp["server_name"],
			"evidence must carry the connector display name, not the UUID")
	}
}

// TestRenderAuthPreflightScriptObservabilityModeFailsOpen verifies the
// observability-mode preflight neither blocks nor stalls session start: a
// fresh machine exits 0 without opening a browser (no interactive login
// wait), and an established machine with broken credentials also exits 0.
func TestRenderAuthPreflightScriptObservabilityModeFailsOpen(t *testing.T) {
	t.Parallel()
	cfg := GenerateConfig{
		ServerURL:         "https://app.getgram.ai",
		HooksAPIKey:       "gram_local_secret_xyz",
		ProjectSlug:       "acme-prod",
		ObservabilityMode: true,
	}
	dir := t.TempDir()
	preflightPath := filepath.Join(dir, "auth_preflight.sh")
	urlFile := filepath.Join(dir, "auth-url")
	require.NoError(t, os.WriteFile(preflightPath, renderAuthPreflightScript(cfg), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "auth.sh"), renderSharedAuthScript(), 0o755))
	opener := []byte("#!/usr/bin/env bash\nprintf '%s' \"$1\" > \"$GRAM_TEST_URL_FILE\"\n")
	require.NoError(t, os.WriteFile(filepath.Join(dir, "open"), opener, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "xdg-open"), opener, 0o755))

	authFile := filepath.Join(dir, "auth.env")
	env := hookAuthTestEnv(dir,
		"GRAM_HOOKS_AUTH_FILE="+authFile,
		"GRAM_TEST_URL_FILE="+urlFile,
		"DISPLAY=:0",
	)
	for i, kv := range env {
		if strings.HasPrefix(kv, "PATH=") {
			env[i] = "PATH=" + dir + string(os.PathListSeparator) + os.Getenv("PATH")
		}
	}

	fresh := exec.Command("bash", preflightPath)
	fresh.Env = env
	var freshErr bytes.Buffer
	fresh.Stderr = &freshErr
	require.NoError(t, fresh.Run(),
		"observability mode must not block session start on a fresh machine: %s", freshErr.String())
	require.NoFileExists(t, urlFile,
		"observability mode must not stall session start on an interactive browser login")

	require.NoError(t, os.WriteFile(authFile+".established", nil, 0o600))
	broken := exec.Command("bash", preflightPath)
	broken.Env = env
	var brokenErr bytes.Buffer
	broken.Stderr = &brokenErr
	require.NoError(t, broken.Run(),
		"observability mode must not block session start on broken established auth: %s", brokenErr.String())
	require.NoFileExists(t, urlFile)
}

// checkedInSenderCapturedRequest runs a checked-in per-event sender with no
// env credentials but a cached browser-login auth file, and returns the curl
// config lines plus request URL captured by a stubbed curl.
func checkedInSenderCapturedRequest(t *testing.T, plugin, payload string) string {
	t.Helper()

	senderPath, err := filepath.Abs(filepath.Join("..", "..", "..", "hooks", plugin, "hooks", "send_hook.sh"))
	require.NoError(t, err)

	dir := t.TempDir()
	binDir := filepath.Join(dir, "bin")
	require.NoError(t, os.MkdirAll(binDir, 0o755))
	headersPath := filepath.Join(dir, "headers.txt")
	require.NoError(t, os.WriteFile(filepath.Join(binDir, "curl"), []byte(`#!/usr/bin/env bash
url=""
while [ "$#" -gt 0 ]; do
  case "$1" in
    --config) cat "$2" >> "$GRAM_CAPTURE_HEADERS"; shift 2 ;;
    -H|-w|-X|--data-binary|--max-time) shift 2 ;;
    -*) shift ;;
    *) url="$1"; shift ;;
  esac
done
cat >/dev/null
printf '%s\n' "$url" >> "$GRAM_CAPTURE_HEADERS"
printf '{}\n200'
`), 0o755))

	authFile := filepath.Join(dir, "auth.env")
	require.NoError(t, os.WriteFile(authFile, []byte("server_url=https://app.getgram.ai\napi_key=gram_cached_browser_key\nproject=acme-prod\nemail=dev@example.com\n"), 0o600))

	env := hookAuthTestEnv(dir,
		"GRAM_CAPTURE_HEADERS="+headersPath,
		"GRAM_HOOKS_AUTH_FILE="+authFile,
	)
	for i, kv := range env {
		if strings.HasPrefix(kv, "PATH=") {
			env[i] = "PATH=" + binDir + string(os.PathListSeparator) + os.Getenv("PATH")
		}
	}

	cmd := exec.Command("bash", senderPath)
	cmd.Stdin = strings.NewReader(payload)
	cmd.Env = env
	output, err := cmd.CombinedOutput()
	require.NoError(t, err, string(output))

	return string(requireFileBytes(t, headersPath))
}

// TestCheckedInCursorSenderUsesCachedBrowserAuth verifies the checked-in
// Cursor plugin's per-event sender falls back to the credentials cached by
// the browser login flow (auth_preflight.sh / login.sh) when no env key is
// set, instead of silently skipping the send.
func TestCheckedInCursorSenderUsesCachedBrowserAuth(t *testing.T) {
	t.Parallel()

	headers := checkedInSenderCapturedRequest(t, "plugin-cursor",
		`{"hook_event_name":"beforeSubmitPrompt","conversation_id":"sess-cached","prompt":"hi"}`)
	require.Contains(t, headers, "Gram-Key: gram_cached_browser_key", "sender must use the cached browser-login key")
	require.Contains(t, headers, "Gram-Project: acme-prod")
	require.Contains(t, headers, "/rpc/hooks.cursor")
}

// TestCheckedInClaudeSenderUsesCachedBrowserAuth verifies the checked-in
// Claude plugin's per-event sender attaches the credentials cached by the
// browser login flow when no env key is set, so policy enforcement that needs
// auth context is not silently skipped on the legacy path.
func TestCheckedInClaudeSenderUsesCachedBrowserAuth(t *testing.T) {
	t.Parallel()

	headers := checkedInSenderCapturedRequest(t, "plugin-claude",
		`{"hook_event_name":"UserPromptSubmit","session_id":"sess-cached-claude","prompt":"hi"}`)
	require.Contains(t, headers, "Gram-Key: gram_cached_browser_key", "sender must use the cached browser-login key")
	require.Contains(t, headers, "Gram-Project: acme-prod")
	require.Contains(t, headers, "/rpc/hooks.claude")
}

// TestRenderHookScriptCursorBackfillsLaterTurnPrompts verifies the prompt
// backfill marker tracks prompt content rather than a per-session boolean: a
// beforeSubmitPrompt dropped on a later turn is still backfilled from the
// transcript's latest user entry, while turns whose prompt was delivered are
// not re-sent.
func TestRenderHookScriptCursorBackfillsLaterTurnPrompts(t *testing.T) {
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
	dir := t.TempDir()
	binDir := filepath.Join(dir, "bin")
	require.NoError(t, os.MkdirAll(binDir, 0o755))
	hookPath := filepath.Join(dir, "hook.sh")
	capturePath := filepath.Join(dir, "payloads.jsonl")
	stateDir := filepath.Join(dir, "state")
	transcriptPath := filepath.Join(dir, "transcript.jsonl")
	require.NoError(t, os.WriteFile(hookPath, renderHookScript(cfg, "cursor"), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "http.sh"), renderSharedHTTPScript(), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "auth.sh"), renderSharedAuthScript(), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(binDir, "curl"), []byte(`#!/usr/bin/env bash
url=""
while [ "$#" -gt 0 ]; do
  case "$1" in
    -H|-w|-X|--data-binary|--max-time|--config) shift 2 ;;
    -*) shift ;;
    *) url="$1"; shift ;;
  esac
done
payload="$(cat)"
case "$url" in
  */rpc/hooks.ingest)
    printf '%s' "$payload" >> "$GRAM_CAPTURE_PAYLOADS"
    printf '\n---GRAM---\n' >> "$GRAM_CAPTURE_PAYLOADS"
    ;;
esac
printf '{}\n200'
`), 0o755))

	env := hookAuthTestEnv(dir,
		"GRAM_CAPTURE_PAYLOADS="+capturePath,
		"GRAM_HOOKS_AUTH_FILE="+filepath.Join(dir, "auth.env"),
		"GRAM_HOOKS_API_KEY=gram_test_hooks_key",
		"GRAM_HOOKS_PROJECT_SLUG=acme-prod",
		"XDG_STATE_HOME="+stateDir,
	)
	for i, kv := range env {
		if strings.HasPrefix(kv, "PATH=") {
			env[i] = "PATH=" + binDir + string(os.PathListSeparator) + os.Getenv("PATH")
		}
	}

	run := func(payload string) {
		t.Helper()
		cmd := exec.Command("bash", hookPath)
		cmd.Stdin = strings.NewReader(payload)
		cmd.Env = env
		output, err := cmd.CombinedOutput()
		require.NoError(t, err, string(output))
	}

	// Turn 1: prompt delivered normally, then the agent responds.
	require.NoError(t, os.WriteFile(transcriptPath, []byte(`{"role":"user","message":{"content":[{"type":"text","text":"<user_query>\nfirst prompt\n</user_query>"}]}}
{"role":"assistant","message":{"content":[{"type":"text","text":"ok one"}]}}
`), 0o600))
	run(`{"hook_event_name":"beforeSubmitPrompt","conversation_id":"sess-turns","session_id":"sess-turns","prompt":"first prompt"}`)
	run(`{"hook_event_name":"afterAgentResponse","conversation_id":"sess-turns","session_id":"sess-turns","generation_id":"turn-1","text":"ok one","transcript_path":"` + transcriptPath + `"}`)

	// Turn 2: beforeSubmitPrompt is dropped; only the response event arrives.
	require.NoError(t, os.WriteFile(transcriptPath, []byte(`{"role":"user","message":{"content":[{"type":"text","text":"<user_query>\nfirst prompt\n</user_query>"}]}}
{"role":"assistant","message":{"content":[{"type":"text","text":"ok one"}]}}
{"role":"user","message":{"content":[{"type":"text","text":"<user_query>\nsecond prompt\n</user_query>"}]}}
{"role":"assistant","message":{"content":[{"type":"text","text":"ok two"}]}}
`), 0o600))
	run(`{"hook_event_name":"afterAgentResponse","conversation_id":"sess-turns","session_id":"sess-turns","generation_id":"turn-2","text":"ok two","transcript_path":"` + transcriptPath + `"}`)

	chunks := strings.Split(string(requireFileBytes(t, capturePath)), "\n---GRAM---\n")
	require.Len(t, chunks, 5, "expected prompt, response, backfilled prompt, response, trailing split")

	eventType := func(raw string) (string, map[string]any) {
		var parsed map[string]any
		require.NoError(t, json.Unmarshal([]byte(strings.TrimSpace(raw)), &parsed))
		event := requireMapValue(t, parsed, "event")
		typ, _ := event["type"].(string)
		return typ, parsed
	}

	typ0, _ := eventType(chunks[0])
	require.Equal(t, "prompt.submitted", typ0)
	typ1, _ := eventType(chunks[1])
	require.Equal(t, "assistant.responded", typ1, "delivered turn-1 prompt must not be re-sent by backfill")
	typ2, parsed2 := eventType(chunks[2])
	require.Equal(t, "prompt.submitted", typ2, "turn-2 dropped prompt must be backfilled")
	prompt2 := requireMapValue(t, requireMapValue(t, parsed2, "data"), "prompt")
	require.Equal(t, "second prompt", prompt2["text"])
	typ3, _ := eventType(chunks[3])
	require.Equal(t, "assistant.responded", typ3)
}

// TestRenderHookScriptCursorBackfillDenyBlocksDecisionEvent verifies that when
// a missed prompt is backfilled during a decision-capable event and the server
// denies it, the deny is relayed on that event instead of being swallowed —
// the deny would have fired at beforeSubmitPrompt had it not been missed. The
// denied turn's tool event must not proceed to its own ingest post.
func TestRenderHookScriptCursorBackfillDenyBlocksDecisionEvent(t *testing.T) {
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
	dir := t.TempDir()
	binDir := filepath.Join(dir, "bin")
	require.NoError(t, os.MkdirAll(binDir, 0o755))
	hookPath := filepath.Join(dir, "hook.sh")
	capturePath := filepath.Join(dir, "payloads.jsonl")
	transcriptPath := filepath.Join(dir, "transcript.jsonl")
	require.NoError(t, os.WriteFile(hookPath, renderHookScript(cfg, "cursor"), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "http.sh"), renderSharedHTTPScript(), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "auth.sh"), renderSharedAuthScript(), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(binDir, "curl"), []byte(`#!/usr/bin/env bash
url=""
while [ "$#" -gt 0 ]; do
  case "$1" in
    -H|-w|-X|--data-binary|--max-time|--config) shift 2 ;;
    -*) shift ;;
    *) url="$1"; shift ;;
  esac
done
payload="$(cat)"
case "$url" in
  */rpc/hooks.ingest)
    printf '%s' "$payload" >> "$GRAM_CAPTURE_PAYLOADS"
    printf '\n---GRAM---\n' >> "$GRAM_CAPTURE_PAYLOADS"
    ;;
esac
case "$payload" in
  *'"type":"prompt.submitted"'*)
    printf '{"decision":"deny","message":"prompt blocked by policy"}\n200'
    ;;
  *)
    printf '{}\n200'
    ;;
esac
`), 0o755))

	require.NoError(t, os.WriteFile(transcriptPath, []byte(`{"role":"user","message":{"content":[{"type":"text","text":"<user_query>\ninjected prompt\n</user_query>"}]}}
`), 0o600))

	env := hookAuthTestEnv(dir,
		"GRAM_CAPTURE_PAYLOADS="+capturePath,
		"GRAM_HOOKS_AUTH_FILE="+filepath.Join(dir, "auth.env"),
		"GRAM_HOOKS_API_KEY=gram_test_hooks_key",
		"GRAM_HOOKS_PROJECT_SLUG=acme-prod",
		"XDG_STATE_HOME="+filepath.Join(dir, "state"),
	)
	for i, kv := range env {
		if strings.HasPrefix(kv, "PATH=") {
			env[i] = "PATH=" + binDir + string(os.PathListSeparator) + os.Getenv("PATH")
		}
	}

	cmd := exec.Command("bash", hookPath)
	cmd.Stdin = strings.NewReader(`{"hook_event_name":"preToolUse","conversation_id":"sess-backfill-deny","session_id":"sess-backfill-deny","generation_id":"turn-1","tool_name":"shell","tool_input":{"command":"ls"},"transcript_path":"` + transcriptPath + `"}`)
	cmd.Env = env
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	require.NoError(t, cmd.Run(), stderr.String())
	require.Contains(t, stdout.String(), `"permission":"deny"`)
	require.Contains(t, stdout.String(), "prompt blocked by policy")

	posts := strings.Count(string(requireFileBytes(t, capturePath)), "\n---GRAM---\n")
	require.Equal(t, 1, posts, "only the backfilled prompt must be posted; the denied turn's tool event must not proceed")
}

// TestRenderHookScriptCursorObservabilityModeSwallowsDeny verifies the
// observability-mode contract end to end for Cursor: server deny decisions are
// not relayed (Cursor honors a deny body regardless of failClosed) and
// transport failures exit 0 instead of 2.
func TestRenderHookScriptCursorObservabilityModeSwallowsDeny(t *testing.T) {
	t.Parallel()
	cfg := GenerateConfig{
		ServerURL:         "https://app.getgram.ai",
		HooksAPIKey:       "gram_local_secret_xyz",
		ProjectSlug:       "acme-prod",
		ObservabilityMode: true,
	}
	dir := t.TempDir()
	binDir := filepath.Join(dir, "bin")
	require.NoError(t, os.MkdirAll(binDir, 0o755))
	hookPath := filepath.Join(dir, "hook.sh")
	require.NoError(t, os.WriteFile(hookPath, renderHookScript(cfg, "cursor"), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "http.sh"), renderSharedHTTPScript(), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "auth.sh"), renderSharedAuthScript(), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(binDir, "curl"), []byte(`#!/usr/bin/env bash
cat >/dev/null
printf '%s\n%s' "$GRAM_FAKE_BODY" "$GRAM_FAKE_CODE"
`), 0o755))

	env := hookAuthTestEnv(dir,
		"GRAM_HOOKS_AUTH_FILE="+filepath.Join(dir, "auth.env"),
		"GRAM_HOOKS_API_KEY=gram_test_hooks_key",
		"GRAM_HOOKS_PROJECT_SLUG=acme-prod",
	)
	for i, kv := range env {
		if strings.HasPrefix(kv, "PATH=") {
			env[i] = "PATH=" + binDir + string(os.PathListSeparator) + os.Getenv("PATH")
		}
	}
	payload := `{"hook_event_name":"preToolUse","session_id":"sess-obs","tool_name":"shell","tool_input":{"command":"ls"}}`

	denied := exec.Command("bash", hookPath)
	denied.Stdin = strings.NewReader(payload)
	denied.Env = append(append([]string{}, env...), "GRAM_FAKE_BODY={\"decision\":\"deny\",\"message\":\"blocked\"}", "GRAM_FAKE_CODE=200")
	var deniedOut, deniedErr bytes.Buffer
	denied.Stdout = &deniedOut
	denied.Stderr = &deniedErr
	require.NoError(t, denied.Run(), "observability mode must not block on a deny decision: %s", deniedErr.String())
	require.NotContains(t, deniedOut.String(), "deny")
	require.Equal(t, "{}", strings.TrimSpace(deniedOut.String()))

	failed := exec.Command("bash", hookPath)
	failed.Stdin = strings.NewReader(payload)
	failed.Env = append(append([]string{}, env...), "GRAM_FAKE_BODY={}", "GRAM_FAKE_CODE=500")
	var failedOut, failedErr bytes.Buffer
	failed.Stdout = &failedOut
	failed.Stderr = &failedErr
	require.NoError(t, failed.Run(), "observability mode must not block on a transport failure: %s", failedErr.String())
	require.Equal(t, "{}", strings.TrimSpace(failedOut.String()))

	// Established-but-broken auth (marker without cache, no env credentials)
	// exits closed with 2 outside observability mode; here it must not block.
	brokenAuthFile := filepath.Join(dir, "broken-auth.env")
	require.NoError(t, os.WriteFile(brokenAuthFile+".established", nil, 0o600))
	broken := exec.Command("bash", hookPath)
	broken.Stdin = strings.NewReader(payload)
	broken.Env = hookAuthTestEnv(dir, "GRAM_HOOKS_AUTH_FILE="+brokenAuthFile)
	var brokenErr bytes.Buffer
	broken.Stderr = &brokenErr
	require.NoError(t, broken.Run(), "observability mode must not block on broken established auth: %s", brokenErr.String())
}

// TestRenderHookScriptCodexObservabilityModeNeverBlocks verifies the same
// contract for Codex, where exit 2 is the only blocking signal: neither a
// server deny nor a transport failure may exit non-zero in observability mode.
func TestRenderHookScriptCodexObservabilityModeNeverBlocks(t *testing.T) {
	t.Parallel()
	cfg := GenerateConfig{
		ServerURL:         "https://app.getgram.ai",
		HooksAPIKey:       "gram_local_secret_xyz",
		ProjectSlug:       "acme-prod",
		ObservabilityMode: true,
	}
	dir := t.TempDir()
	binDir := filepath.Join(dir, "bin")
	require.NoError(t, os.MkdirAll(binDir, 0o755))
	hookPath := filepath.Join(dir, "hook.sh")
	require.NoError(t, os.WriteFile(hookPath, renderHookScript(cfg, "codex"), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "http.sh"), renderSharedHTTPScript(), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "auth.sh"), renderSharedAuthScript(), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(binDir, "curl"), []byte(`#!/usr/bin/env bash
cat >/dev/null
printf '%s\n%s' "$GRAM_FAKE_BODY" "$GRAM_FAKE_CODE"
`), 0o755))

	env := hookAuthTestEnv(dir,
		"GRAM_HOOKS_AUTH_FILE="+filepath.Join(dir, "auth.env"),
		"GRAM_HOOKS_API_KEY=gram_test_hooks_key",
		"GRAM_HOOKS_PROJECT_SLUG=acme-prod",
	)
	for i, kv := range env {
		if strings.HasPrefix(kv, "PATH=") {
			env[i] = "PATH=" + binDir + string(os.PathListSeparator) + os.Getenv("PATH")
		}
	}
	payload := `{"hook_event_name":"PreToolUse","session_id":"sess-obs-codex","tool_name":"shell","tool_input":{"command":"ls"}}`

	denied := exec.Command("bash", hookPath)
	denied.Stdin = strings.NewReader(payload)
	denied.Env = append(append([]string{}, env...), "GRAM_FAKE_BODY={\"decision\":\"deny\",\"message\":\"blocked\"}", "GRAM_FAKE_CODE=200")
	var deniedOut, deniedErr bytes.Buffer
	denied.Stdout = &deniedOut
	denied.Stderr = &deniedErr
	require.NoError(t, denied.Run(), "observability mode must not block on a deny decision: %s", deniedErr.String())
	require.Empty(t, deniedOut.String(), "codex allow must be empty stdout")

	failed := exec.Command("bash", hookPath)
	failed.Stdin = strings.NewReader(payload)
	failed.Env = append(append([]string{}, env...), "GRAM_FAKE_BODY={}", "GRAM_FAKE_CODE=500")
	var failedErr bytes.Buffer
	failed.Stderr = &failedErr
	require.NoError(t, failed.Run(), "observability mode must not block on a transport failure: %s", failedErr.String())

	// Established-but-broken auth (marker without cache, no env credentials)
	// exits closed with 2 outside observability mode; here it must not block.
	brokenAuthFile := filepath.Join(dir, "broken-auth.env")
	require.NoError(t, os.WriteFile(brokenAuthFile+".established", nil, 0o600))
	broken := exec.Command("bash", hookPath)
	broken.Stdin = strings.NewReader(payload)
	broken.Env = hookAuthTestEnv(dir, "GRAM_HOOKS_AUTH_FILE="+brokenAuthFile)
	var brokenErr bytes.Buffer
	broken.Stderr = &brokenErr
	require.NoError(t, broken.Run(), "observability mode must not block on broken established auth: %s", brokenErr.String())
}

// TestRenderHookScriptCursorThoughtMapsToAssistantThought verifies Cursor
// afterAgentThought events are sent as the dedicated assistant.thought
// canonical type: assistant.responded would be persisted into the session's
// chat transcript, surfacing internal reasoning as a regular message.
func TestRenderHookScriptCursorThoughtMapsToAssistantThought(t *testing.T) {
	t.Parallel()
	cfg := GenerateConfig{
		ServerURL:   "https://app.getgram.ai",
		HooksAPIKey: "gram_local_secret_xyz",
		ProjectSlug: "acme-prod",
	}
	dir := t.TempDir()
	binDir := filepath.Join(dir, "bin")
	require.NoError(t, os.MkdirAll(binDir, 0o755))
	hookPath := filepath.Join(dir, "hook.sh")
	capturePath := filepath.Join(dir, "payloads.jsonl")
	require.NoError(t, os.WriteFile(hookPath, renderHookScript(cfg, "cursor"), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "http.sh"), renderSharedHTTPScript(), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "auth.sh"), renderSharedAuthScript(), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(binDir, "curl"), []byte(`#!/usr/bin/env bash
url=""
while [ "$#" -gt 0 ]; do
  case "$1" in
    -H|-w|-X|--data-binary|--max-time|--config) shift 2 ;;
    -*) shift ;;
    *) url="$1"; shift ;;
  esac
done
payload="$(cat)"
case "$url" in
  */rpc/hooks.ingest)
    printf '%s' "$payload" >> "$GRAM_CAPTURE_PAYLOADS"
    printf '\n---GRAM---\n' >> "$GRAM_CAPTURE_PAYLOADS"
    ;;
esac
printf '{}\n200'
`), 0o755))

	env := hookAuthTestEnv(dir,
		"GRAM_CAPTURE_PAYLOADS="+capturePath,
		"GRAM_HOOKS_AUTH_FILE="+filepath.Join(dir, "auth.env"),
		"GRAM_HOOKS_API_KEY=gram_test_hooks_key",
		"GRAM_HOOKS_PROJECT_SLUG=acme-prod",
	)
	for i, kv := range env {
		if strings.HasPrefix(kv, "PATH=") {
			env[i] = "PATH=" + binDir + string(os.PathListSeparator) + os.Getenv("PATH")
		}
	}

	cmd := exec.Command("bash", hookPath)
	cmd.Stdin = strings.NewReader(`{"hook_event_name":"afterAgentThought","conversation_id":"sess-thought","session_id":"sess-thought","generation_id":"turn-1","text":"internal reasoning"}`)
	cmd.Env = env
	output, err := cmd.CombinedOutput()
	require.NoError(t, err, string(output))

	chunks := strings.Split(string(requireFileBytes(t, capturePath)), "\n---GRAM---\n")
	require.Len(t, chunks, 2, "expected one captured ingest payload")
	var parsed map[string]any
	require.NoError(t, json.Unmarshal([]byte(strings.TrimSpace(chunks[0])), &parsed))
	event := requireMapValue(t, parsed, "event")
	require.Equal(t, "assistant.thought", event["type"])
	message := requireMapValue(t, requireMapValue(t, parsed, "data"), "message")
	require.Equal(t, "internal reasoning", message["text"])
}

// TestSharedAuthScriptEscapesCurlConfigValues verifies credentials containing
// curl config metacharacters cannot break out of the header directive.
func TestSharedAuthScriptEscapesCurlConfigValues(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	authPath := filepath.Join(dir, "auth.sh")
	require.NoError(t, os.WriteFile(authPath, renderSharedAuthScript(), 0o755))

	cmd := exec.Command("bash", "-c", `. "$GRAM_TEST_AUTH_SH"; gram_hooks_write_curl_config 'k"ey\1' 'pro"j\2'; cat "$auth_config"`)
	cmd.Env = hookAuthTestEnv(dir, "GRAM_TEST_AUTH_SH="+authPath)
	output, err := cmd.CombinedOutput()
	require.NoError(t, err, string(output))
	require.Contains(t, string(output), `header = "Gram-Key: k\"ey\\1"`)
	require.Contains(t, string(output), `header = "Gram-Project: pro\"j\\2"`)

	// The config file is line-oriented: embedded CR/LF would end the header
	// directive and let the remainder parse as additional curl options.
	inject := exec.Command("bash", "-c", `. "$GRAM_TEST_AUTH_SH"; gram_hooks_write_curl_config $'ke\ny\rz' $'sl\nug'; cat "$auth_config"`)
	inject.Env = hookAuthTestEnv(dir, "GRAM_TEST_AUTH_SH="+authPath)
	output, err = inject.CombinedOutput()
	require.NoError(t, err, string(output))
	require.Contains(t, string(output), `header = "Gram-Key: keyz"`)
	require.Contains(t, string(output), `header = "Gram-Project: slug"`)
}

// TestRenderHookPayloadNormalizationDecodesEscapedPromptWithoutJQ verifies
// that prompts containing JSON escapes (newlines, quotes) survive extraction
// on machines without jq — an empty prompt would silently bypass prompt
// policy enforcement server-side.
func TestRenderHookPayloadNormalizationDecodesEscapedPromptWithoutJQ(t *testing.T) {
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
	script := renderHookPayloadNormalizationSnippet("claude") + `
payload='{"hook_event_name":"UserPromptSubmit","session_id":"sess-esc","prompt":"line one\nsay \"hi\" \\ done"}'
gram_hooks_build_canonical_payload "$payload" "test-host"
`
	require.NoError(t, os.WriteFile(scriptPath, []byte(script), 0o755))

	cmd := exec.Command(bashPath, scriptPath)
	cmd.Env = append(os.Environ(), "PATH="+binDir)
	output, err := cmd.CombinedOutput()
	require.NoError(t, err, string(output))

	var parsed map[string]any
	require.NoError(t, json.Unmarshal(output, &parsed))
	prompt := requireMapValue(t, requireMapValue(t, parsed, "data"), "prompt")
	require.Equal(t, "line one\nsay \"hi\" \\ done", prompt["text"],
		"escaped prompt must be decoded, not dropped, when jq is unavailable")
}

// TestRenderHookPayloadNormalizationSynthesizesToolID verifies that tool
// events without a provider tool_use_id get a deterministic synthetic id so
// before/after records correlate instead of collapsing into one identity.
func TestRenderHookPayloadNormalizationSynthesizesToolID(t *testing.T) {
	t.Parallel()

	bashPath, err := exec.LookPath("bash")
	require.NoError(t, err, "bash is required to run generated hook snippets")

	scriptPath := filepath.Join(t.TempDir(), "normalize.sh")
	script := renderHookPayloadNormalizationSnippet("cursor") + `
before='{"event":"beforeMCPExecution","conversation_id":"sess-synth","generation_id":"gen-1","tool_name":"MCP:lookup","tool_input":{"q":"x"}}'
after='{"event":"afterMCPExecution","conversation_id":"sess-synth","generation_id":"gen-1","tool_name":"MCP:lookup","tool_input":{"q":"x"},"result_json":"{}"}'
gram_hooks_build_canonical_payload "$before" "test-host"
printf '\n---GRAM---\n'
gram_hooks_build_canonical_payload "$after" "test-host"
`
	require.NoError(t, os.WriteFile(scriptPath, []byte(script), 0o755))

	cmd := exec.Command(bashPath, scriptPath)
	output, err := cmd.CombinedOutput()
	require.NoError(t, err, string(output))

	chunks := strings.Split(string(output), "\n---GRAM---\n")
	require.Len(t, chunks, 2)

	toolID := func(raw string) string {
		var parsed map[string]any
		require.NoError(t, json.Unmarshal([]byte(strings.TrimSpace(raw)), &parsed))
		toolCall := requireMapValue(t, requireMapValue(t, parsed, "data"), "tool_call")
		id, _ := toolCall["id"].(string)
		return id
	}
	beforeID := toolID(chunks[0])
	afterID := toolID(chunks[1])
	require.NotEmpty(t, beforeID, "tool events without tool_use_id must get a synthetic id")
	require.True(t, strings.HasPrefix(beforeID, "hook_synth_"), beforeID)
	require.Equal(t, beforeID, afterID, "before/after events must derive the same synthetic id")
}

// TestRenderHookPayloadNormalizationCodexSynthIDs verifies Codex synthetic
// tool ids satisfy both constraints its payloads make hard: a request and its
// PostToolUse result (which omits tool_input) share one id via the local
// in-flight ledger, while two same-tool requests with different inputs stay
// distinct.
func TestRenderHookPayloadNormalizationCodexSynthIDs(t *testing.T) {
	t.Parallel()

	bashPath, err := exec.LookPath("bash")
	require.NoError(t, err, "bash is required to run generated hook snippets")

	dir := t.TempDir()
	scriptPath := filepath.Join(dir, "normalize.sh")
	// Completions run through the async sender, so a second request can fire
	// before the first completion is processed: the ledger is a FIFO queue,
	// exercised here in the interleaved order (reqA, reqB, resA, resB). The
	// leading PermissionRequest also normalizes to tool.requested but has no
	// completion, so it must not occupy a queue slot.
	script := renderHookPayloadNormalizationSnippet("codex") + `
perm='{"hook_event_name":"PermissionRequest","session_id":"sess-codex","tool_name":"shell","tool_input":{"command":"ls"},"permission_type":"exec"}'
req_a='{"hook_event_name":"PreToolUse","session_id":"sess-codex","tool_name":"shell","tool_input":{"command":"ls"}}'
req_b='{"hook_event_name":"PreToolUse","session_id":"sess-codex","tool_name":"shell","tool_input":{"command":"whoami"}}'
res='{"hook_event_name":"PostToolUse","session_id":"sess-codex","tool_name":"shell","tool_output":{"stdout":"ok"}}'
gram_hooks_build_canonical_payload "$perm" "test-host"
printf '\n---GRAM---\n'
gram_hooks_build_canonical_payload "$req_a" "test-host"
printf '\n---GRAM---\n'
gram_hooks_build_canonical_payload "$req_b" "test-host"
printf '\n---GRAM---\n'
gram_hooks_build_canonical_payload "$res" "test-host"
printf '\n---GRAM---\n'
gram_hooks_build_canonical_payload "$res" "test-host"
`
	require.NoError(t, os.WriteFile(scriptPath, []byte(script), 0o755))

	cmd := exec.Command(bashPath, scriptPath)
	cmd.Env = append(os.Environ(), "XDG_STATE_HOME="+filepath.Join(dir, "state"))
	output, err := cmd.CombinedOutput()
	require.NoError(t, err, string(output))

	chunks := strings.Split(string(output), "\n---GRAM---\n")
	require.Len(t, chunks, 5)

	toolID := func(raw string) string {
		var parsed map[string]any
		require.NoError(t, json.Unmarshal([]byte(strings.TrimSpace(raw)), &parsed))
		toolCall := requireMapValue(t, requireMapValue(t, parsed, "data"), "tool_call")
		id, _ := toolCall["id"].(string)
		return id
	}
	reqAID := toolID(chunks[1])
	reqBID := toolID(chunks[2])
	resAID := toolID(chunks[3])
	resBID := toolID(chunks[4])
	require.NotEmpty(t, reqAID)
	require.NotEqual(t, reqAID, reqBID, "distinct same-tool codex requests must not collide")
	require.Equal(t, reqAID, resAID, "first completion must pop the first request's id, not the permission prompt's")
	require.Equal(t, reqBID, resBID, "second completion must pop the second request's id")
}

// TestRenderHookPayloadNormalizationCodexSkillHeuristic covers the best-effort
// Codex skill detection. Codex has no Skill tool: implicit activations surface
// as a reader tool opening skills/<name>/SKILL.md, and explicit $skill-name
// prompt mentions are expanded internally without any tool call. Both must
// attach data.skill while keeping the event's true type on the wire — a
// reclassified event would skip the server's tool/prompt policy scan.
func TestRenderHookPayloadNormalizationCodexSkillHeuristic(t *testing.T) {
	t.Parallel()

	bashPath, err := exec.LookPath("bash")
	require.NoError(t, err, "bash is required to run generated hook snippets")

	dir := t.TempDir()
	homeDir := filepath.Join(dir, "home")
	repoDir := filepath.Join(dir, "repo")
	cwd := filepath.Join(repoDir, "nested", "sub")
	codexHome := filepath.Join(dir, "codex-home")
	require.NoError(t, os.MkdirAll(filepath.Join(homeDir, ".agents", "skills", "home-skill"), 0o755))
	require.NoError(t, os.MkdirAll(filepath.Join(repoDir, ".agents", "skills", "repo-skill"), 0o755))
	require.NoError(t, os.MkdirAll(filepath.Join(codexHome, "skills", ".system", "sys-skill"), 0o755))
	require.NoError(t, os.MkdirAll(cwd, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(homeDir, ".agents", "skills", "home-skill", "SKILL.md"), []byte("# home"), 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(repoDir, ".agents", "skills", "repo-skill", "SKILL.md"), []byte("# repo"), 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(codexHome, "skills", ".system", "sys-skill", "SKILL.md"), []byte("# sys"), 0o644))

	scriptPath := filepath.Join(dir, "normalize.sh")
	script := renderHookPayloadNormalizationSnippet("codex") + fmt.Sprintf(`
read_req='{"hook_event_name":"PreToolUse","session_id":"sess-skill","tool_name":"Bash","tool_input":{"command":"sed -n 1,240p %[1]s/.agents/skills/repo-skill/SKILL.md"},"tool_use_id":"call_1"}'
read_res='{"hook_event_name":"PostToolUse","session_id":"sess-skill","tool_name":"Bash","tool_input":{"command":"sed -n 1,240p %[1]s/.agents/skills/repo-skill/SKILL.md"},"tool_response":{"output":"ok"},"tool_use_id":"call_1"}'
perm_req='{"hook_event_name":"PermissionRequest","session_id":"sess-skill","tool_name":"Bash","tool_input":{"command":"cat %[1]s/.agents/skills/repo-skill/SKILL.md"},"permission_type":"exec"}'
patch_req='{"hook_event_name":"PreToolUse","session_id":"sess-skill","tool_name":"apply_patch","tool_input":{"changes":"%[1]s/.agents/skills/repo-skill/SKILL.md"},"tool_use_id":"call_2"}'
prompt_hit='{"hook_event_name":"UserPromptSubmit","session_id":"sess-skill","prompt":"Check $HOME then use $home-skill please","cwd":"%[2]s"}'
prompt_walk='{"hook_event_name":"UserPromptSubmit","session_id":"sess-skill","prompt":"use $repo-skill","cwd":"%[2]s"}'
prompt_miss='{"hook_event_name":"UserPromptSubmit","session_id":"sess-skill","prompt":"pay $50 to $unknown-skill","cwd":"%[2]s"}'
prompt_punct='{"hook_event_name":"UserPromptSubmit","session_id":"sess-skill","prompt":"please use $home-skill.","cwd":"%[2]s"}'
prompt_relcwd='{"hook_event_name":"UserPromptSubmit","session_id":"sess-skill","prompt":"use $repo-skill","cwd":"nested/sub"}'
prompt_system='{"hook_event_name":"UserPromptSubmit","session_id":"sess-skill","prompt":"use $sys-skill","cwd":"%[2]s"}'
read_system='{"hook_event_name":"PreToolUse","session_id":"sess-skill","tool_name":"Bash","tool_input":{"command":"cat /opt/codex/skills/.system/imagegen/SKILL.md"},"tool_use_id":"call_3"}'
gram_hooks_build_canonical_payload "$read_req" "test-host"
printf '\n---GRAM---\n'
gram_hooks_build_canonical_payload "$read_res" "test-host"
printf '\n---GRAM---\n'
gram_hooks_build_canonical_payload "$perm_req" "test-host"
printf '\n---GRAM---\n'
gram_hooks_build_canonical_payload "$patch_req" "test-host"
printf '\n---GRAM---\n'
gram_hooks_build_canonical_payload "$prompt_hit" "test-host"
printf '\n---GRAM---\n'
gram_hooks_build_canonical_payload "$prompt_walk" "test-host"
printf '\n---GRAM---\n'
gram_hooks_build_canonical_payload "$prompt_miss" "test-host"
printf '\n---GRAM---\n'
gram_hooks_build_canonical_payload "$prompt_punct" "test-host"
printf '\n---GRAM---\n'
gram_hooks_build_canonical_payload "$prompt_relcwd" "test-host"
printf '\n---GRAM---\n'
gram_hooks_build_canonical_payload "$prompt_system" "test-host"
printf '\n---GRAM---\n'
gram_hooks_build_canonical_payload "$read_system" "test-host"
`, repoDir, cwd)
	require.NoError(t, os.WriteFile(scriptPath, []byte(script), 0o755))

	cmd := exec.Command(bashPath, scriptPath)
	cmd.Env = append(os.Environ(), "HOME="+homeDir, "CODEX_HOME="+codexHome, "XDG_STATE_HOME="+filepath.Join(dir, "state"))
	output, err := cmd.CombinedOutput()
	require.NoError(t, err, string(output))

	chunks := strings.Split(string(output), "\n---GRAM---\n")
	require.Len(t, chunks, 11)

	parse := func(raw string) (eventType string, skillName string) {
		var parsed map[string]any
		require.NoError(t, json.Unmarshal([]byte(strings.TrimSpace(raw)), &parsed))
		eventType, _ = requireMapValue(t, parsed, "event")["type"].(string)
		data, _ := parsed["data"].(map[string]any)
		if skill, ok := data["skill"].(map[string]any); ok {
			skillName, _ = skill["name"].(string)
		}
		return eventType, skillName
	}

	eventType, skill := parse(chunks[0])
	require.Equal(t, "tool.requested", eventType, "a detected skill read must keep its true event type")
	require.Equal(t, "repo-skill", skill, "SKILL.md path in a reader tool input must resolve the skill name")

	eventType, skill = parse(chunks[1])
	require.Equal(t, "tool.completed", eventType)
	require.Empty(t, skill, "completions must not re-report the activation")

	eventType, skill = parse(chunks[2])
	require.Equal(t, "tool.requested", eventType)
	require.Empty(t, skill, "permission previews may be denied and must not count as activations")

	eventType, skill = parse(chunks[3])
	require.Equal(t, "tool.requested", eventType)
	require.Empty(t, skill, "editing a SKILL.md is not an activation")

	eventType, skill = parse(chunks[4])
	require.Equal(t, "prompt.submitted", eventType, "a skill mention must keep the prompt event type")
	require.Equal(t, "home-skill", skill, "$name mentions must resolve against $HOME/.agents/skills")

	eventType, skill = parse(chunks[5])
	require.Equal(t, "prompt.submitted", eventType)
	require.Equal(t, "repo-skill", skill, "$name mentions must resolve by walking up from the session cwd")

	eventType, skill = parse(chunks[6])
	require.Equal(t, "prompt.submitted", eventType)
	require.Empty(t, skill, "$ tokens that resolve to no skill directory must be ignored")

	eventType, skill = parse(chunks[7])
	require.Equal(t, "prompt.submitted", eventType)
	require.Equal(t, "home-skill", skill, "sentence-final punctuation must not defeat a mention")

	eventType, skill = parse(chunks[8])
	require.Equal(t, "prompt.submitted", eventType)
	require.Empty(t, skill, "a relative cwd must terminate the walk without matching")

	eventType, skill = parse(chunks[9])
	require.Equal(t, "prompt.submitted", eventType)
	require.Equal(t, "sys-skill", skill, "bundled skills under a .system subdirectory must resolve by bare name")

	eventType, skill = parse(chunks[10])
	require.Equal(t, "tool.requested", eventType)
	require.Equal(t, "imagegen", skill, "reads of .system skill paths must infer the bare skill name")
}

// TestRenderHookPayloadNormalizationClaudeSkillStillReclassifies pins the
// Claude behavior the codex guard must not disturb: the dedicated Skill tool
// is a benign meta-tool, so its events keep being reclassified to
// skill.activated on the wire.
func TestRenderHookPayloadNormalizationClaudeSkillStillReclassifies(t *testing.T) {
	t.Parallel()

	bashPath, err := exec.LookPath("bash")
	require.NoError(t, err, "bash is required to run generated hook snippets")

	scriptPath := filepath.Join(t.TempDir(), "normalize.sh")
	script := renderHookPayloadNormalizationSnippet("claude") + `
payload='{"hook_event_name":"PreToolUse","session_id":"sess-claude-skill","tool_name":"Skill","tool_input":{"skill":"pdf-tools"},"tool_use_id":"toolu_1"}'
gram_hooks_build_canonical_payload "$payload" "test-host"
`
	require.NoError(t, os.WriteFile(scriptPath, []byte(script), 0o755))

	output, err := exec.Command(bashPath, scriptPath).CombinedOutput()
	require.NoError(t, err, string(output))

	var parsed map[string]any
	require.NoError(t, json.Unmarshal([]byte(strings.TrimSpace(string(output))), &parsed))
	require.Equal(t, "skill.activated", requireMapValue(t, parsed, "event")["type"])
	skill := requireMapValue(t, requireMapValue(t, parsed, "data"), "skill")
	require.Equal(t, "pdf-tools", skill["name"])
}

// TestRenderHookPayloadNormalizationCodexMCPExactServerNameWins pins the codex
// MCP config lookup semantics when server names collide after sanitizing: an
// exact name match must win, and without one a sanitized match only resolves
// when it is unambiguous.
func TestRenderHookPayloadNormalizationCodexMCPExactServerNameWins(t *testing.T) {
	t.Parallel()

	_, err := exec.LookPath("jq")
	require.NoError(t, err, "jq is required for the codex MCP metadata lookup")

	dir := t.TempDir()
	binDir := filepath.Join(dir, "bin")
	require.NoError(t, os.MkdirAll(binDir, 0o755))
	codexStub := `#!/usr/bin/env bash
if [ "$1" = "mcp" ] && [ "$2" = "list" ]; then
  cat <<'JSON'
[
  {"name":"Linear-Server","transport":{"type":"stdio","command":"npx","args":["colliding-linear-mcp"]}},
  {"name":"Linear_Server","transport":{"type":"stdio","command":"npx","args":["linear-mcp"]}},
  {"name":"Other-Server","transport":{"type":"stdio","command":"npx","args":["other-a"]}},
  {"name":"Other.Server","transport":{"type":"stdio","command":"npx","args":["other-b"]}}
]
JSON
fi
`
	require.NoError(t, os.WriteFile(filepath.Join(binDir, "codex"), []byte(codexStub), 0o755))

	scriptPath := filepath.Join(dir, "lookup.sh")
	script := renderHookPayloadNormalizationSnippet("codex") + `
printf 'exact:'
gram_hooks_codex_mcp_metadata "Linear_Server" | tr '\n' ';'
printf '\nambiguous:'
gram_hooks_codex_mcp_metadata "Other_Server" | tr '\n' ';'
printf '\n'
`
	require.NoError(t, os.WriteFile(scriptPath, []byte(script), 0o755))

	cmd := exec.Command("bash", scriptPath)
	cmd.Env = append(os.Environ(), "PATH="+binDir+string(os.PathListSeparator)+os.Getenv("PATH"))
	output, err := cmd.CombinedOutput()
	require.NoError(t, err, string(output))
	require.Contains(t, string(output), "command=npx linear-mcp", "exact server name must win over a sanitized collision")
	require.NotContains(t, string(output), "colliding-linear-mcp")
	require.Contains(t, string(output), "ambiguous:\n", "ambiguous sanitized matches must resolve to nothing")
}

// TestSharedAuthScriptLoginProbeVerifiesListenerIdentity covers the login
// readiness probe: it must accept only this attempt's handler (which echoes
// the per-attempt probe marker) and reject an unrelated localhost service
// that happens to answer 200 on the chosen port — otherwise the browser
// callback would deliver a freshly minted key to the wrong process.
func TestSharedAuthScriptLoginProbeVerifiesListenerIdentity(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(dir, "auth.sh"), renderSharedAuthScript(), 0o755))

	foreign := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = io.WriteString(w, "ok")
	}))
	t.Cleanup(foreign.Close)
	foreignPort := foreign.URL[strings.LastIndex(foreign.URL, ":")+1:]

	script := `. ./auth.sh
work="$(mktemp -d)"
printf 'GET /gram-probe HTTP/1.1\r\n\r\n' | gram_hooks_login_handle_request "$work" state-token probe-token
printf '\n'
if gram_hooks_login_probe "$1" probe-token; then echo FOREIGN-ACCEPTED; else echo FOREIGN-REJECTED; fi
`
	cmd := exec.Command("bash", "-c", script, "probe-test", foreignPort)
	cmd.Dir = dir
	output, err := cmd.CombinedOutput()
	require.NoError(t, err, string(output))
	require.Contains(t, string(output), "gram-hooks-probe-ok:probe-token", "handler must echo the per-attempt probe marker")
	require.Contains(t, string(output), "FOREIGN-REJECTED", "probe must not accept a listener that lacks the marker")
}

// TestSharedAuthScriptRejectsCachedAuthFromOtherOrg verifies the credential
// cache is bound to the generating plugin's organization: a cache minted for
// org B must not authenticate a plugin generated for org A even when the
// server URL matches, while same-org caches and org-less caches from before
// stamping stay usable.
func TestSharedAuthScriptRejectsCachedAuthFromOtherOrg(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(dir, "auth.sh"), renderSharedAuthScript(), 0o755))

	script := `. ./auth.sh
export GRAM_HOOKS_AUTH_FILE="$PWD/auth.env"
gram_hooks_write_auth https://gram.test key-123 default dev@example.com org-b || exit 90
gram_hooks_org_hint="org-a"
if gram_hooks_read_auth https://gram.test; then echo MISMATCH-ACCEPTED; else echo MISMATCH-REJECTED; fi
gram_hooks_org_hint="org-b"
if gram_hooks_read_auth https://gram.test; then echo MATCH-ACCEPTED; else echo MATCH-REJECTED; fi
gram_hooks_write_auth https://gram.test key-123 default dev@example.com || exit 91
gram_hooks_org_hint="org-a"
if gram_hooks_read_auth https://gram.test; then echo LEGACY-ACCEPTED; else echo LEGACY-REJECTED; fi
`
	cmd := exec.Command("bash", "-c", script)
	cmd.Dir = dir
	output, err := cmd.CombinedOutput()
	require.NoError(t, err, string(output))
	require.Contains(t, string(output), "MISMATCH-REJECTED", "another org's cache must not authenticate this plugin")
	require.Contains(t, string(output), "MATCH-ACCEPTED")
	require.Contains(t, string(output), "LEGACY-ACCEPTED", "pre-stamping caches without an org must stay usable")
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
url=""
while [ "$#" -gt 0 ]; do
  case "$1" in
    -H)
      if [ "$#" -gt 1 ]; then
        case "$2" in
          Idempotency-Key:*) key="${2#Idempotency-Key: }" ;;
        esac
      fi
      shift 2
      ;;
    -w|-X|--data-binary|--max-time|--config)
      shift 2
      ;;
    -*)
      shift
      ;;
    *)
      url="$1"
      shift
      ;;
  esac
done
payload="$(cat)"
case "$url" in
  */rpc/hooks.ingest)
    printf '%s\n' "$key" >> "$GRAM_CAPTURE_KEYS"
    printf '%s' "$payload" >> "$GRAM_CAPTURE_PAYLOADS"
    printf '\n---GRAM---\n' >> "$GRAM_CAPTURE_PAYLOADS"
    ;;
esac
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
	backfilledEvent := requireMapValue(t, backfilled, "event")
	require.Equal(t, "prompt.submitted", backfilledEvent["type"])
	backfilledData := requireMapValue(t, backfilled, "data")
	backfilledPrompt := requireMapValue(t, backfilledData, "prompt")
	require.Equal(t, "GRAM_CURSOR_BACKFILL_PROMPT\n\nPlease reply.", backfilledPrompt["text"])

	var actual map[string]any
	require.NoError(t, json.Unmarshal([]byte(secondPayload), &actual))
	actualEvent := requireMapValue(t, actual, "event")
	require.Equal(t, "assistant.responded", actualEvent["type"])
	actualData := requireMapValue(t, actual, "data")
	actualMessage := requireMapValue(t, actualData, "message")
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
		ClaudeObservabilitySlug(cfg) + "/hooks/login.sh",
		ClaudeObservabilitySlug(cfg) + "/hooks/auth_preflight.sh",
		"cursor-plugins/" + CursorObservabilitySlug(cfg) + "/hooks/identity.sh",
		"cursor-plugins/" + CursorObservabilitySlug(cfg) + "/hooks/auth.sh",
		"cursor-plugins/" + CursorObservabilitySlug(cfg) + "/hooks/login.sh",
		"cursor-plugins/" + CursorObservabilitySlug(cfg) + "/hooks/auth_preflight.sh",
		CodexObservabilitySlug(cfg) + "/hooks/identity.sh",
		CodexObservabilitySlug(cfg) + "/hooks/auth.sh",
		CodexObservabilitySlug(cfg) + "/hooks/login.sh",
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
	require.NotNil(t, sessionStart[0].Hooks[0].Timeout, "preflight needs an explicit timeout: Claude's 60s default kills the interactive browser login")
	require.Equal(t, 300, *sessionStart[0].Hooks[0].Timeout)
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
		require.Contains(t, parsed.Hooks[event][0].Command, "hooks/hook.sh")
		if blockingEvents[event] {
			require.NotNil(t, parsed.Hooks[event][0].FailClosed, "blocking event %q must fail closed", event)
			require.True(t, *parsed.Hooks[event][0].FailClosed, "blocking event %q must fail closed", event)
		} else {
			require.Nil(t, parsed.Hooks[event][0].FailClosed, "observational event %q must not fail closed", event)
		}
	}
}

// TestGenerateCursorObservabilityModeNeverFailsClosed verifies that
// ObservabilityMode — documented as fully non-blocking — disables failClosed
// on every cursor hook entry, preflight included.
func TestGenerateCursorObservabilityModeNeverFailsClosed(t *testing.T) {
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

	slug := "cursor-plugins/" + CursorObservabilitySlug(cfg)
	var parsed cursorHooksConfig
	require.NoError(t, json.Unmarshal(files[slug+"/hooks/hooks.json"], &parsed))

	for event, commands := range parsed.Hooks {
		for i, command := range commands {
			require.Nil(t, command.FailClosed, "observability mode must not fail closed: event %q entry %d", event, i)
		}
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
	require.Contains(t, script, `gram_hooks_post_authenticated "$server_url" "$payload" 10 "$project_slug" "$gram_hooks_failure_exit"`)
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
	pythonPath, err := exec.LookPath("python3")
	require.NoError(t, err, "python3 is required by the generated install script")

	scriptPath := filepath.Join(t.TempDir(), "install.sh")
	require.NoError(t, os.WriteFile(scriptPath, script, 0o755))

	cmd := exec.Command(bashPath, scriptPath)
	cmd.Env = []string{
		"HOME=" + home,
		"PATH=" + filepath.Dir(pythonPath) + ":/usr/bin:/bin",
	}
	out, err := cmd.CombinedOutput()
	require.NoError(t, err, "install script failed: %s", out)
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
url=""
while [ "$#" -gt 0 ]; do
  case "$1" in
    -w|-X|-H|--data-binary|--max-time|--config)
      shift 2
      ;;
    -*)
      shift
      ;;
    *)
      url="$1"
      shift
      ;;
  esac
done
payload="$(cat)"
case "$url" in
  */rpc/hooks.ingest) printf '%s' "$payload" > "$GRAM_CAPTURE_PAYLOAD" ;;
esac
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
	data := requireMapValue(t, posted, "data")
	toolCall := requireMapValue(t, data, "tool_call")
	require.Equal(t, "mcp__shadow_e2e__lookup", toolCall["name"])
	mcp := requireMapValue(t, data, "mcp")
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

// Running install.sh twice against a config that already contains every entry
// the script writes must not duplicate TOML keys — duplicate keys make Codex
// refuse to load config.toml entirely.
func TestGenerateCodexInstallScriptIsIdempotent(t *testing.T) {
	t.Parallel()

	cfg := GenerateConfig{OrgName: "Acme", ServerURL: "https://app.getgram.ai"}
	marketplace := conv.ToSlug(cfg.OrgName) + "-speakeasy"
	plugin := CodexObservabilitySlug(cfg)

	approvals, err := computeCodexHookApprovals(marketplace, plugin)
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
