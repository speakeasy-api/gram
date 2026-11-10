package relay

import (
	"encoding/json"
	"io"
	"maps"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/speakeasy-api/agenthooks"
)

// Claude Code names the tools of a plugin-provided MCP server
// mcp__plugin_<plugin>_<server>__<tool>, and the server's transport lives
// only in the plugin's own config under the plugin cache. agenthooks recovers
// it through `claude mcp list`, which health-checks every configured server
// and routinely outlives its probe budget, so a plugin MCP call regularly
// reaches the relay with nothing but the tool-name prefix. Without a URL the
// backend cannot tell a Speakeasy-hosted plugin server from a third-party
// one. The resolver here reads the installed plugin's MCP config straight
// from disk instead: a handful of small file reads, no subprocess.

const (
	// claudePluginMCPToolPrefix opens the name of every tool a Claude Code
	// plugin MCP server exposes.
	claudePluginMCPToolPrefix = "mcp__plugin_"

	// claudePluginMCPToolSeparator separates the server prefix from the tool
	// name in a Claude Code MCP tool name.
	claudePluginMCPToolSeparator = "__"

	// maxClaudePluginConfigBytes bounds every plugin registry, settings and
	// MCP config file read on the tool-call path. Real files are a few KiB;
	// the registry grows with project-scoped installs but stays far below
	// this. The cap keeps a corrupt file from stalling the hook.
	maxClaudePluginConfigBytes = 4 << 20 // 4 MiB

	// claudePluginRegistryVersion is the installed_plugins.json schema whose
	// per-plugin install records this resolver understands.
	claudePluginRegistryVersion = 2
)

// claudePluginInstall is one install record of a plugin in Claude Code's
// installed_plugins.json registry.
type claudePluginInstall struct {
	// Scope is where the plugin was installed: user, project, local or
	// managed.
	Scope string `json:"scope"`

	// ProjectPath is the project a project- or local-scoped install belongs
	// to.
	ProjectPath string `json:"projectPath"`

	// InstallPath is the plugin's directory in the plugin cache.
	InstallPath string `json:"installPath"`
}

// claudePluginMCPServer is one MCP server entry in a plugin's .mcp.json or
// inline plugin.json mcpServers block.
type claudePluginMCPServer struct {
	// Type is the transport: stdio, http or sse.
	Type string `json:"type"`

	// Command is the executable a stdio server runs.
	Command string `json:"command"`

	// Args are the stdio command's arguments.
	Args []string `json:"args"`

	// URL is the endpoint of an http or sse server.
	URL string `json:"url"`
}

// claudePluginMCPTransport is the resolved transport of one plugin MCP server.
type claudePluginMCPTransport struct {
	url     string
	command string
}

// resolveClaudePluginMCP fills in the URL or command of a Claude Code plugin
// MCP call that arrived without transport. It returns mcp unchanged when the
// call is not an unresolved plugin MCP call, or when the installed plugin
// config does not name exactly one transport for it: misattributing a server
// is worse than reporting none.
func resolveClaudePluginMCP(base *agenthooks.Event, toolName string, mcp *agenthooks.MCPCall) *agenthooks.MCPCall {
	if mcp == nil || base.Provider != agenthooks.ProviderClaudeCode || mcp.URL != "" || mcp.Command != "" {
		return mcp
	}
	if !strings.HasPrefix(toolName, claudePluginMCPToolPrefix) {
		return mcp
	}
	transport, ok := lookupClaudePluginMCP(toolName, claudeProjectDir(base.Session.CWD))
	if !ok {
		return mcp
	}
	resolved := *mcp
	resolved.URL = transport.url
	resolved.Command = transport.command
	resolved.FromConfig = true
	return &resolved
}

// claudeProjectDir is the project root Claude Code reads project settings
// from. Claude exports it to hooks; the session cwd is the fallback.
func claudeProjectDir(cwd string) string {
	if dir := strings.TrimSpace(os.Getenv("CLAUDE_PROJECT_DIR")); dir != "" {
		return dir
	}
	return cwd
}

// claudeConfigRoot is Claude Code's config directory: CLAUDE_CONFIG_DIR, or
// ~/.claude.
func claudeConfigRoot() string {
	if dir := strings.TrimSpace(os.Getenv("CLAUDE_CONFIG_DIR")); dir != "" {
		return dir
	}
	home, err := os.UserHomeDir()
	if err != nil || home == "" {
		return ""
	}
	return filepath.Join(home, ".claude")
}

// lookupClaudePluginMCP finds the transport behind a plugin MCP tool name.
// Only plugins whose name matches the tool-name prefix have their MCP config
// read. Plugins disabled in settings are skipped. Two installs or servers
// that claim the same prefix with different transports resolve to nothing.
func lookupClaudePluginMCP(toolName, projectDir string) (claudePluginMCPTransport, bool) {
	rest := strings.TrimPrefix(toolName, claudePluginMCPToolPrefix)
	configRoot := claudeConfigRoot()
	if configRoot == "" {
		return claudePluginMCPTransport{}, false
	}
	installs := readClaudePluginRegistry(filepath.Join(configRoot, "plugins", "installed_plugins.json"))
	if len(installs) == 0 {
		return claudePluginMCPTransport{}, false
	}
	enabled := claudeEnabledPlugins(configRoot, projectDir)

	keys := make([]string, 0, len(installs))
	for key := range installs {
		keys = append(keys, key)
	}
	sort.Strings(keys)

	var (
		found     []claudePluginMCPTransport
		bestLen   int
		ambiguous bool
	)
	for _, key := range keys {
		if on, set := enabled[key]; set && !on {
			continue
		}
		plugin, _, _ := strings.Cut(key, "@")
		pluginPrefix := normalizeClaudeMCPName(plugin) + "_"
		if plugin == "" || !strings.HasPrefix(rest, pluginPrefix) {
			continue
		}
		installPath := pickClaudePluginInstall(installs[key], projectDir)
		if installPath == "" {
			continue
		}
		for name, server := range readClaudePluginMCPServers(installPath) {
			serverPrefix := pluginPrefix + normalizeClaudeMCPName(name)
			if !strings.HasPrefix(rest, serverPrefix+claudePluginMCPToolSeparator) {
				continue
			}
			transport, ok := claudePluginServerTransport(server, installPath)
			switch {
			case len(serverPrefix) > bestLen:
				bestLen, found, ambiguous = len(serverPrefix), nil, false
			case len(serverPrefix) < bestLen:
				continue
			}
			if !ok {
				ambiguous = true
				continue
			}
			found = append(found, transport)
		}
	}
	if ambiguous || len(found) == 0 {
		return claudePluginMCPTransport{}, false
	}
	for _, other := range found[1:] {
		if other != found[0] {
			return claudePluginMCPTransport{}, false
		}
	}
	return found[0], true
}

// normalizeClaudeMCPName mirrors how Claude Code turns a plugin or server
// name into a tool-name segment: every character outside [A-Za-z0-9_-]
// becomes "_" ("Internal Slack" -> "Internal_Slack", "incident.io" ->
// "incident_io").
func normalizeClaudeMCPName(name string) string {
	var b strings.Builder
	b.Grow(len(name))
	for _, r := range name {
		if r == '_' || r == '-' || (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9') {
			b.WriteRune(r)
			continue
		}
		b.WriteByte('_')
	}
	return b.String()
}

// pickClaudePluginInstall chooses the install Claude Code loads for the
// project: the project- or local-scoped install of the closest enclosing
// project, else the user or managed install. Two equally specific installs
// in different directories resolve to none.
func pickClaudePluginInstall(records []claudePluginInstall, projectDir string) string {
	var (
		project, global []string
		longest         int
	)
	for _, record := range records {
		if record.InstallPath == "" {
			continue
		}
		switch record.Scope {
		case "project", "local":
			if !filepath.IsAbs(projectDir) || !pathWithin(projectDir, record.ProjectPath) {
				continue
			}
			depth := len(filepath.Clean(record.ProjectPath))
			switch {
			case depth > longest:
				longest, project = depth, []string{record.InstallPath}
			case depth == longest:
				project = append(project, record.InstallPath)
			}
		case "user", "managed":
			global = append(global, record.InstallPath)
		}
	}
	candidates := global
	if len(project) > 0 {
		candidates = project
	}
	if len(candidates) == 0 {
		return ""
	}
	for _, other := range candidates[1:] {
		if filepath.Clean(other) != filepath.Clean(candidates[0]) {
			return ""
		}
	}
	return candidates[0]
}

// claudePluginServerTransport renders a plugin server entry as the URL or
// command Claude Code connects to. ${CLAUDE_PLUGIN_ROOT} is expanded to the
// install path. A URL that still holds a variable reference is unresolvable
// here: the expanded value decides the real host, so reporting the template
// could misclassify the server.
func claudePluginServerTransport(server claudePluginMCPServer, installPath string) (claudePluginMCPTransport, bool) {
	expand := func(s string) string {
		return strings.ReplaceAll(s, "${CLAUDE_PLUGIN_ROOT}", installPath)
	}
	if u := strings.TrimSpace(expand(server.URL)); u != "" {
		if strings.Contains(u, "${") {
			return claudePluginMCPTransport{}, false
		}
		return claudePluginMCPTransport{url: u, command: ""}, true
	}
	command := strings.TrimSpace(expand(server.Command))
	if command == "" {
		return claudePluginMCPTransport{}, false
	}
	parts := make([]string, 0, len(server.Args)+1)
	parts = append(parts, command)
	for _, arg := range server.Args {
		parts = append(parts, expand(arg))
	}
	return claudePluginMCPTransport{url: "", command: strings.Join(parts, " ")}, true
}

// readClaudePluginRegistry returns the install records of every plugin in
// installed_plugins.json, keyed by "<plugin>@<marketplace>".
func readClaudePluginRegistry(path string) map[string][]claudePluginInstall {
	data, ok := readClaudePluginFile(path)
	if !ok {
		return nil
	}
	var registry struct {
		Version int                              `json:"version"`
		Plugins map[string][]claudePluginInstall `json:"plugins"`
	}
	if json.Unmarshal(data, &registry) != nil || registry.Version != claudePluginRegistryVersion {
		return nil
	}
	return registry.Plugins
}

// claudeEnabledPlugins merges the enabledPlugins maps of the user, project
// and local settings files, later scopes overriding earlier ones.
func claudeEnabledPlugins(configRoot, projectDir string) map[string]bool {
	paths := []string{filepath.Join(configRoot, "settings.json")}
	if filepath.IsAbs(projectDir) {
		paths = append(paths,
			filepath.Join(projectDir, ".claude", "settings.json"),
			filepath.Join(projectDir, ".claude", "settings.local.json"))
	}
	enabled := map[string]bool{}
	for _, path := range paths {
		data, ok := readClaudePluginFile(path)
		if !ok {
			continue
		}
		var settings struct {
			EnabledPlugins map[string]bool `json:"enabledPlugins"`
		}
		if json.Unmarshal(data, &settings) != nil {
			continue
		}
		maps.Copy(enabled, settings.EnabledPlugins)
	}
	return enabled
}

// readClaudePluginMCPServers collects the MCP servers a plugin declares: the
// .mcp.json at its root, plus plugin.json's mcpServers, which may be an
// inline server map, a path to a config file, or a list of either.
func readClaudePluginMCPServers(installPath string) map[string]claudePluginMCPServer {
	servers := map[string]claudePluginMCPServer{}
	mergeClaudePluginMCPFile(servers, installPath, ".mcp.json")

	data, ok := readClaudePluginFile(filepath.Join(installPath, ".claude-plugin", "plugin.json"))
	if !ok {
		return servers
	}
	var manifest struct {
		MCPServers json.RawMessage `json:"mcpServers"`
	}
	if json.Unmarshal(data, &manifest) != nil || len(manifest.MCPServers) == 0 {
		return servers
	}
	mergeClaudePluginMCPRef(servers, installPath, manifest.MCPServers)
	return servers
}

// mergeClaudePluginMCPRef merges one plugin.json mcpServers value: a path
// string, an inline server map, or an array of those.
func mergeClaudePluginMCPRef(servers map[string]claudePluginMCPServer, installPath string, raw json.RawMessage) {
	var path string
	if json.Unmarshal(raw, &path) == nil {
		mergeClaudePluginMCPFile(servers, installPath, path)
		return
	}
	var list []json.RawMessage
	if json.Unmarshal(raw, &list) == nil {
		for _, item := range list {
			mergeClaudePluginMCPRef(servers, installPath, item)
		}
		return
	}
	maps.Copy(servers, parseClaudePluginMCPConfig(raw))
}

// mergeClaudePluginMCPFile merges the servers of an MCP config file inside
// the plugin directory. Paths that escape the plugin directory are ignored.
func mergeClaudePluginMCPFile(servers map[string]claudePluginMCPServer, installPath, rel string) {
	rel = strings.TrimSpace(strings.ReplaceAll(rel, "${CLAUDE_PLUGIN_ROOT}", installPath))
	if rel == "" {
		return
	}
	path := rel
	if !filepath.IsAbs(path) {
		path = filepath.Join(installPath, path)
	}
	if !pathWithin(path, installPath) {
		return
	}
	data, ok := readClaudePluginFile(path)
	if !ok {
		return
	}
	maps.Copy(servers, parseClaudePluginMCPConfig(data))
}

// parseClaudePluginMCPConfig reads an MCP server map written either wrapped,
// {"mcpServers": {...}}, or bare, {"name": {...}}.
func parseClaudePluginMCPConfig(data []byte) map[string]claudePluginMCPServer {
	var wrapped struct {
		MCPServers map[string]claudePluginMCPServer `json:"mcpServers"`
	}
	if json.Unmarshal(data, &wrapped) == nil && wrapped.MCPServers != nil {
		return wrapped.MCPServers
	}
	var bare map[string]claudePluginMCPServer
	if json.Unmarshal(data, &bare) != nil {
		return nil
	}
	return bare
}

// readClaudePluginFile reads a regular file of at most
// maxClaudePluginConfigBytes.
func readClaudePluginFile(path string) ([]byte, bool) {
	file, err := os.Open(path) // #nosec G304 -- Claude Code config paths under the user's own config and plugin cache.
	if err != nil {
		return nil, false
	}
	info, statErr := file.Stat()
	if statErr != nil || !info.Mode().IsRegular() {
		_ = file.Close()
		return nil, false
	}
	data, readErr := io.ReadAll(io.LimitReader(file, maxClaudePluginConfigBytes+1))
	closeErr := file.Close()
	if readErr != nil || closeErr != nil || len(data) > maxClaudePluginConfigBytes {
		return nil, false
	}
	return data, true
}
