package relay

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/speakeasy-api/agenthooks"
	"github.com/speakeasy-api/agenthooks/agenthookstest"
	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/hooks/sdk/models/components"
)

// claudePluginFixture lays out a throwaway Claude Code config root with a
// plugin registry, settings, and plugin directories under the cache.
type claudePluginFixture struct {
	t       *testing.T
	root    string
	plugins map[string][]claudePluginInstall
}

func newClaudePluginFixture(t *testing.T) *claudePluginFixture {
	t.Helper()
	root := filepath.Join(t.TempDir(), ".claude")
	t.Setenv("CLAUDE_CONFIG_DIR", root)
	t.Setenv("CLAUDE_PROJECT_DIR", "")
	return &claudePluginFixture{t: t, root: root, plugins: map[string][]claudePluginInstall{}}
}

// install writes the plugin's files into the cache and records the install.
func (f *claudePluginFixture) install(key, scope, projectPath string, files map[string]string) string {
	f.t.Helper()
	dir := filepath.Join(f.root, "plugins", "cache", key, scope, filepath.Base(projectPath))
	for name, body := range files {
		path := filepath.Join(dir, name)
		require.NoError(f.t, os.MkdirAll(filepath.Dir(path), 0o700))
		require.NoError(f.t, os.WriteFile(path, []byte(body), 0o600))
	}
	f.plugins[key] = append(f.plugins[key], claudePluginInstall{Scope: scope, ProjectPath: projectPath, InstallPath: dir})
	f.writeJSON(filepath.Join(f.root, "plugins", "installed_plugins.json"), map[string]any{"version": 2, "plugins": f.plugins})
	return dir
}

func (f *claudePluginFixture) settings(path string, enabled map[string]bool) {
	f.t.Helper()
	f.writeJSON(path, map[string]any{"enabledPlugins": enabled})
}

func (f *claudePluginFixture) writeJSON(path string, v any) {
	f.t.Helper()
	data, err := json.Marshal(v)
	require.NoError(f.t, err)
	require.NoError(f.t, os.MkdirAll(filepath.Dir(path), 0o700))
	require.NoError(f.t, os.WriteFile(path, data, 0o600))
}

const engineeringMCPJSON = `{"mcpServers":{
	"Datadog":{"type":"http","url":"https://chat.speakeasy.com/mcp/datadog"},
	"Internal Slack":{"type":"http","url":"https://ai.speakeasy.com/mcp/team-internal-slack"},
	"incident.io":{"type":"http","url":"https://ai.speakeasy.com/mcp/team-incident"},
	"Vendor":{"type":"http","url":"https://mcp.vendor.example/mcp"},
	"Local":{"command":"${CLAUDE_PLUGIN_ROOT}/bin/server","args":["--root","${CLAUDE_PLUGIN_ROOT}"]},
	"Templated":{"type":"http","url":"${MCP_HOST}/mcp"}
}}`

func claudeMCPEvent() *agenthooks.Event {
	return &agenthooks.Event{Provider: agenthooks.ProviderClaudeCode, Session: agenthooks.SessionInfo{CWD: "/work/repo"}}
}

func TestResolveClaudePluginMCPFromPluginMCPJSON(t *testing.T) {
	f := newClaudePluginFixture(t)
	dir := f.install("engineering@speakeasy-marketplace", "user", "", map[string]string{".mcp.json": engineeringMCPJSON})
	f.settings(filepath.Join(f.root, "settings.json"), map[string]bool{"engineering@speakeasy-marketplace": true})

	cases := []struct {
		tool    string
		url     string
		command string
	}{
		{tool: "mcp__plugin_engineering_Datadog__search_logs", url: "https://chat.speakeasy.com/mcp/datadog"},
		{tool: "mcp__plugin_engineering_Internal_Slack__send", url: "https://ai.speakeasy.com/mcp/team-internal-slack"},
		{tool: "mcp__plugin_engineering_incident_io__list", url: "https://ai.speakeasy.com/mcp/team-incident"},
		{tool: "mcp__plugin_engineering_Vendor__call", url: "https://mcp.vendor.example/mcp"},
		{tool: "mcp__plugin_engineering_Local__run", command: dir + "/bin/server --root " + dir},
	}
	for _, tc := range cases {
		t.Run(tc.tool, func(t *testing.T) {
			got := resolveClaudePluginMCP(claudeMCPEvent(), tc.tool, &agenthooks.MCPCall{Server: "plugin_engineering", Tool: "x"})
			require.Equal(t, tc.url, got.URL)
			require.Equal(t, tc.command, got.Command)
			require.True(t, got.FromConfig)
			require.Equal(t, "plugin_engineering", got.Server, "the server name is left as the codec reported it")
		})
	}
}

func TestResolveClaudePluginMCPLeavesUnresolvableCallsAlone(t *testing.T) {
	f := newClaudePluginFixture(t)
	f.install("engineering@speakeasy-marketplace", "user", "", map[string]string{".mcp.json": engineeringMCPJSON})
	f.install("disabled@speakeasy-marketplace", "user", "", map[string]string{".mcp.json": `{"mcpServers":{"Linear":{"url":"https://chat.speakeasy.com/mcp/linear"}}}`})
	f.install("twin@one", "user", "", map[string]string{".mcp.json": `{"mcpServers":{"Linear":{"url":"https://chat.speakeasy.com/mcp/linear"}}}`})
	f.install("twin@two", "user", "", map[string]string{".mcp.json": `{"mcpServers":{"Linear":{"url":"https://mcp.vendor.example/linear"}}}`})
	f.settings(filepath.Join(f.root, "settings.json"), map[string]bool{"disabled@speakeasy-marketplace": false})

	for name, tool := range map[string]string{
		"unknown server":                "mcp__plugin_engineering_Missing__call",
		"unknown plugin":                "mcp__plugin_other_Datadog__call",
		"url with unexpanded variables": "mcp__plugin_engineering_Templated__call",
		"disabled plugin":               "mcp__plugin_disabled_Linear__call",
		"same prefix, different urls":   "mcp__plugin_twin_Linear__call",
		"not a plugin server":           "mcp__Datadog__call",
	} {
		t.Run(name, func(t *testing.T) {
			in := &agenthooks.MCPCall{Server: "s", Tool: "call"}
			require.Same(t, in, resolveClaudePluginMCP(claudeMCPEvent(), tool, in))
		})
	}

	t.Run("other providers", func(t *testing.T) {
		in := &agenthooks.MCPCall{Server: "plugin_engineering_Datadog", Tool: "call"}
		event := &agenthooks.Event{Provider: agenthooks.ProviderCodex}
		require.Same(t, in, resolveClaudePluginMCP(event, "mcp__plugin_engineering_Datadog__call", in))
	})

	t.Run("transport already known", func(t *testing.T) {
		in := &agenthooks.MCPCall{Server: "plugin_engineering_Datadog", Tool: "call", URL: "https://mcp.vendor.example/other"}
		require.Same(t, in, resolveClaudePluginMCP(claudeMCPEvent(), "mcp__plugin_engineering_Datadog__call", in))
	})
}

func TestResolveClaudePluginMCPFromManifest(t *testing.T) {
	f := newClaudePluginFixture(t)
	f.install("inline@market", "user", "", map[string]string{
		".claude-plugin/plugin.json": `{"name":"inline","mcpServers":{"Docs":{"url":"https://chat.speakeasy.com/mcp/docs"}}}`,
	})
	f.install("pointer@market", "user", "", map[string]string{
		".claude-plugin/plugin.json": `{"name":"pointer","mcpServers":["./config/mcp.json","../../escape.json"]}`,
		"config/mcp.json":            `{"Search":{"type":"sse","url":"https://mcp.vendor.example/sse"}}`,
	})

	got := resolveClaudePluginMCP(claudeMCPEvent(), "mcp__plugin_inline_Docs__read", &agenthooks.MCPCall{Server: "plugin_inline_Docs"})
	require.Equal(t, "https://chat.speakeasy.com/mcp/docs", got.URL)

	got = resolveClaudePluginMCP(claudeMCPEvent(), "mcp__plugin_pointer_Search__query", &agenthooks.MCPCall{Server: "plugin_pointer_Search"})
	require.Equal(t, "https://mcp.vendor.example/sse", got.URL)
}

func TestResolveClaudePluginMCPPrefersProjectInstall(t *testing.T) {
	f := newClaudePluginFixture(t)
	project := filepath.Join(t.TempDir(), "repo")
	f.install("tools@market", "user", "", map[string]string{".mcp.json": `{"mcpServers":{"API":{"url":"https://mcp.vendor.example/user"}}}`})
	f.install("tools@market", "project", project, map[string]string{".mcp.json": `{"mcpServers":{"API":{"url":"https://chat.speakeasy.com/mcp/project"}}}`})

	event := &agenthooks.Event{Provider: agenthooks.ProviderClaudeCode, Session: agenthooks.SessionInfo{CWD: filepath.Join(project, "sub")}}
	got := resolveClaudePluginMCP(event, "mcp__plugin_tools_API__get", &agenthooks.MCPCall{Server: "plugin_tools_API"})
	require.Equal(t, "https://chat.speakeasy.com/mcp/project", got.URL)

	event.Session.CWD = t.TempDir()
	got = resolveClaudePluginMCP(event, "mcp__plugin_tools_API__get", &agenthooks.MCPCall{Server: "plugin_tools_API"})
	require.Equal(t, "https://mcp.vendor.example/user", got.URL)

	t.Setenv("CLAUDE_PROJECT_DIR", project)
	got = resolveClaudePluginMCP(event, "mcp__plugin_tools_API__get", &agenthooks.MCPCall{Server: "plugin_tools_API"})
	require.Equal(t, "https://chat.speakeasy.com/mcp/project", got.URL, "CLAUDE_PROJECT_DIR names the project when the cwd has moved")
}

// TestRunnerSendsClaudePluginMCPURL drives a real PreToolUse through the
// runner with `claude mcp list` unavailable, the case in which agenthooks
// leaves plugin MCP calls without transport.
func TestRunnerSendsClaudePluginMCPURL(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	t.Setenv("TMPDIR", t.TempDir())
	t.Setenv("XDG_CACHE_HOME", t.TempDir())
	t.Setenv("PATH", t.TempDir())
	f := newClaudePluginFixture(t)
	f.install("engineering@speakeasy-marketplace", "user", "", map[string]string{".mcp.json": engineeringMCPJSON})

	fs := newFakeServer(t, nil)
	cfg := authedConfig(t, fs.URL)
	t.Setenv("SPEAKEASY_AI_DEVICE_AGENT_COMMANDS", "speakeasy-hooks-test-missing-device-agent")
	unsetClaudeLaunch(t)
	payload := []byte(`{"session_id":"plugin-mcp-session","hook_event_name":"PreToolUse","cwd":"/work/repo","tool_name":"mcp__plugin_engineering_Datadog__search_logs","tool_input":{},"tool_use_id":"tool-1"}`)

	res := agenthookstest.Invoke(t, NewRunner(cfg), agenthooks.ProviderClaudeCode, payload, "--variant=cli")

	require.Equal(t, 0, res.ExitCode)
	tool := fs.last()
	require.Equal(t, components.TypeToolRequested, tool.Event.Type)
	require.NotNil(t, tool.Data)
	require.NotNil(t, tool.Data.Mcp)
	require.NotNil(t, tool.Data.Mcp.URL)
	require.Equal(t, "https://chat.speakeasy.com/mcp/datadog", *tool.Data.Mcp.URL)
	require.NotNil(t, tool.Data.Mcp.ServerName)
	require.Equal(t, "plugin_engineering_Datadog", *tool.Data.Mcp.ServerName)
}
