package plugins

import (
	"encoding/json"
	"fmt"
	"path"
	"strings"
	"unicode/utf8"

	"github.com/speakeasy-api/gram/server/internal/conv"
)

// ServerEnvConfig represents a user-facing environment variable required by a server.
type ServerEnvConfig struct {
	VariableName string
	DisplayName  string // Shown to the user in Claude's userConfig prompt
}

// PluginServerInfo contains the resolved information for a single MCP server.
type PluginServerInfo struct {
	DisplayName string
	Policy      string
	// Resolved MCP URL (e.g. https://app.getgram.ai/mcp/{slug}).
	MCPURL string
	// IsPublic indicates whether the toolset is publicly accessible (no Gram API key needed).
	IsPublic bool
	// EnvConfigs are user-facing environment variables for public servers.
	EnvConfigs []ServerEnvConfig
}

// PluginInfo contains the data needed to generate packages for a single plugin.
type PluginInfo struct {
	Name        string
	Slug        string
	Description string
	Servers     []PluginServerInfo
}

// GenerateConfig holds org-level configuration for package generation.
type GenerateConfig struct {
	OrgName  string
	OrgEmail string
	// Base server URL (e.g. https://app.getgram.ai).
	ServerURL string
	// APIKey is the plaintext consumer-scoped Gram API key to inject into
	// MCP server configs. If empty, configs will use placeholder variables.
	APIKey string
	// HooksAPIKey is the plaintext hooks-scoped Gram API key embedded in the
	// base plugin's hook script. If empty, the base plugin is omitted.
	HooksAPIKey string
	// ProjectSlug is the publishing project's slug. The Cursor hooks endpoint
	// requires it via the Gram-Project header (Claude's does not).
	ProjectSlug string
}

// BaseHookEvents lists every hook event Gram captures from Claude Code and
// Cursor. The base plugin registers its hook script against all of them so a
// single install gives the org full observability.
var BaseHookEvents = []string{
	"PreToolUse",
	"PostToolUse",
	"SessionStart",
	"UserPromptSubmit",
	"Stop",
}

// GeneratePluginPackages produces the complete file map for a plugin distribution
// repository containing both Claude Code and Cursor plugins. Used for GitHub push.
func GeneratePluginPackages(plugins []PluginInfo, cfg GenerateConfig) (map[string][]byte, error) {
	files := make(map[string][]byte)

	var claudePlugins []marketplaceEntry
	var cursorPlugins []marketplaceEntry

	// Base plugin (observability hooks) ships first in the marketplace so it's
	// the first thing team admins see. Skipped when no hooks key is configured
	// — typically only in tests that don't exercise the publish flow.
	if cfg.HooksAPIKey != "" {
		if err := generateClaudeBasePlugin(files, cfg); err != nil {
			return nil, fmt.Errorf("generate claude base plugin: %w", err)
		}
		if err := generateCursorBasePlugin(files, cfg); err != nil {
			return nil, fmt.Errorf("generate cursor base plugin: %w", err)
		}
		claudePlugins = append(claudePlugins, marketplaceEntry{
			Name:        "base",
			Source:      "./base",
			Description: "Required: Gram observability hooks for " + cfg.OrgName + ".",
		})
		cursorPlugins = append(cursorPlugins, marketplaceEntry{
			Name:        "base-cursor",
			Source:      "./base-cursor",
			Description: "Required: Gram observability hooks for " + cfg.OrgName + ".",
		})
	}

	for _, p := range plugins {
		if err := generateClaudePlugin(files, p, cfg); err != nil {
			return nil, fmt.Errorf("generate claude plugin %s: %w", p.Slug, err)
		}
		if err := generateCursorPlugin(files, p, cfg); err != nil {
			return nil, fmt.Errorf("generate cursor plugin %s: %w", p.Slug, err)
		}
		claudePlugins = append(claudePlugins, marketplaceEntry{
			Name:        p.Slug,
			Source:      "./" + p.Slug,
			Description: p.Description,
		})
		cursorPlugins = append(cursorPlugins, marketplaceEntry{
			Name:        p.Slug + "-cursor",
			Source:      "./" + p.Slug + "-cursor",
			Description: p.Description,
		})
	}

	owner := marketplaceOwner{Name: cfg.OrgName, Email: cfg.OrgEmail}

	claudeManifest, err := marshalJSON(marketplaceManifest{
		Name:    conv.ToSlug(cfg.OrgName) + "-gram",
		Owner:   owner,
		Plugins: claudePlugins,
	})
	if err != nil {
		return nil, fmt.Errorf("marshal claude marketplace.json: %w", err)
	}
	files[".claude-plugin/marketplace.json"] = claudeManifest

	cursorManifest, err := marshalJSON(marketplaceManifest{
		Name:    conv.ToSlug(cfg.OrgName) + "-gram",
		Owner:   owner,
		Plugins: cursorPlugins,
	})
	if err != nil {
		return nil, fmt.Errorf("marshal cursor marketplace.json: %w", err)
	}
	files[".cursor-plugin/marketplace.json"] = cursorManifest

	files["README.md"] = generateReadme(plugins, cfg)

	return files, nil
}

// escapeMarkdownCell sanitizes user-controlled text for inclusion in a single
// Markdown table cell: collapses line breaks, escapes pipes that would otherwise
// split the row, and caps excessively long values.
func escapeMarkdownCell(s string) string {
	s = strings.ReplaceAll(s, "\r\n", " ")
	s = strings.ReplaceAll(s, "\n", " ")
	s = strings.ReplaceAll(s, "\r", " ")
	s = strings.ReplaceAll(s, "|", `\|`)

	const maxRunes = 200
	if utf8.RuneCountInString(s) > maxRunes {
		runes := []rune(s)
		s = string(runes[:maxRunes]) + "…"
	}
	return s
}

func generateReadme(plugins []PluginInfo, cfg GenerateConfig) []byte {
	var b strings.Builder

	b.WriteString("# " + cfg.OrgName + " Plugins\n\n")
	b.WriteString("This repository contains plugin packages managed by [Gram](https://getgram.ai). ")
	b.WriteString("Each plugin bundles MCP servers for distribution via Claude Code and Cursor marketplaces.\n\n")
	b.WriteString("## How this repo works\n\n")
	b.WriteString("- **Read-only access.** Collaborators are granted pull permission only. You can clone and inspect the repository, but you cannot push to it.\n")
	b.WriteString("- **Auto-managed by Gram.** Each publish from the Gram dashboard overwrites this repository's contents. Any manual edits, new branches, or local commits will be discarded on the next publish — make changes in Gram instead.\n\n")

	if cfg.HooksAPIKey != "" {
		b.WriteString("> **Required:** install the `base` plugin alongside any feature plugins to enable Gram observability. Without it, your team will install MCP servers but tool events will not be reported to your Gram dashboard.\n\n")
	}

	if len(plugins) > 0 {
		b.WriteString("## Plugins\n\n")
		b.WriteString("| Plugin | Description | Servers |\n")
		b.WriteString("|--------|-------------|--------:|\n")
		for _, p := range plugins {
			desc := strings.TrimSpace(p.Description)
			if desc == "" {
				desc = "—"
			}
			fmt.Fprintf(&b, "| %s | %s | %d |\n", escapeMarkdownCell(p.Name), escapeMarkdownCell(desc), len(p.Servers))
		}
		b.WriteString("\n")
	}

	b.WriteString("## Installation\n\n")
	b.WriteString("### Claude Code\n\n")
	b.WriteString("1. Go to your organization's [Claude admin console](https://claude.ai)\n")
	b.WriteString("2. Navigate to **Settings → Plugin Marketplaces**\n")
	b.WriteString("3. Click **Add Marketplace** and paste this repository's URL\n")
	b.WriteString("4. Plugins will be automatically available to members of your organization\n")
	if cfg.HooksAPIKey != "" {
		b.WriteString("\nMark the `base` plugin as required so observability is on by default for all team members:\n\n")
		b.WriteString("```json\n{\n  \"plugins\": {\n    \"required\": [\"base@" + conv.ToSlug(cfg.OrgName) + "-gram\"]\n  }\n}\n```\n")
	}
	b.WriteString("\n### Cursor\n\n")
	b.WriteString("1. Open your team's [Cursor dashboard](https://cursor.com/dashboard)\n")
	b.WriteString("2. Navigate to **Settings → Plugins → Import**\n")
	b.WriteString("3. Paste this repository's URL to import the marketplace\n")
	b.WriteString("4. Plugins will be available to team members\n")
	if cfg.HooksAPIKey != "" {
		b.WriteString("\nIn Cursor's team marketplace settings, mark the `base-cursor` plugin as required so observability is on by default for all team members.\n")
	}

	return []byte(b.String())
}

// GenerateSinglePluginPackage produces files for a single plugin with files at
// the root level (no subdirectory prefix). Used for per-plugin ZIP downloads
// that can be installed directly via `claude --plugin-dir`.
func GenerateSinglePluginPackage(plugin PluginInfo, cfg GenerateConfig, platform string) (map[string][]byte, error) {
	files := make(map[string][]byte)

	switch platform {
	case "claude":
		if err := generateClaudePluginFlat(files, plugin, cfg); err != nil {
			return nil, fmt.Errorf("generate claude plugin: %w", err)
		}
	case "cursor":
		if err := generateCursorPluginFlat(files, plugin, cfg); err != nil {
			return nil, fmt.Errorf("generate cursor plugin: %w", err)
		}
	default:
		return nil, fmt.Errorf("unsupported platform: %s", platform)
	}

	return files, nil
}

func generateClaudePluginFlat(files map[string][]byte, p PluginInfo, cfg GenerateConfig) error {
	return generateClaudePluginInDir(files, "", p, cfg)
}

func generateCursorPluginFlat(files map[string][]byte, p PluginInfo, cfg GenerateConfig) error {
	return generateCursorPluginInDir(files, "", p.Slug, p, cfg)
}

func generateClaudePlugin(files map[string][]byte, p PluginInfo, cfg GenerateConfig) error {
	return generateClaudePluginInDir(files, p.Slug, p, cfg)
}

func generateCursorPlugin(files map[string][]byte, p PluginInfo, cfg GenerateConfig) error {
	return generateCursorPluginInDir(files, p.Slug+"-cursor", p.Slug+"-cursor", p, cfg)
}

// generateClaudeBasePlugin emits the per-org "base" plugin containing Gram
// observability hooks for Claude Code. The hook script bakes in the org's
// hooks-scoped API key so no per-machine credential setup is required.
func generateClaudeBasePlugin(files map[string][]byte, cfg GenerateConfig) error {
	const subdir = "base"
	pluginJSON, err := marshalJSON(claudePluginMeta{
		Name:        subdir,
		Description: "Gram observability hooks for " + cfg.OrgName + ". Install this plugin to forward tool events to your team's Gram dashboard.",
		Version:     "0.1.0",
		Author:      pluginAuthor{Name: cfg.OrgName, URL: "https://getgram.ai"},
		Homepage:    "https://getgram.ai",
		UserConfig:  nil,
	})
	if err != nil {
		return fmt.Errorf("marshal plugin.json: %w", err)
	}
	files[path.Join(subdir, ".claude-plugin/plugin.json")] = pluginJSON

	matchers := []claudeHookMatcher{
		{Matcher: "*", Hooks: []claudeHookCommand{{Type: "command", Command: "./hook.sh"}}},
	}
	hookEvents := make(map[string][]claudeHookMatcher, len(BaseHookEvents))
	for _, event := range BaseHookEvents {
		hookEvents[event] = matchers
	}
	hooksJSON, err := marshalJSON(claudeHooksConfig{Hooks: hookEvents})
	if err != nil {
		return fmt.Errorf("marshal hooks.json: %w", err)
	}
	files[path.Join(subdir, "hooks.json")] = hooksJSON

	files[path.Join(subdir, "hook.sh")] = renderHookScript(cfg, "claude")

	return nil
}

// generateCursorBasePlugin emits the per-org "base" plugin for Cursor. Same
// shape as the Claude variant but uses Cursor's hook event names + script
// destination URL.
func generateCursorBasePlugin(files map[string][]byte, cfg GenerateConfig) error {
	const subdir = "base-cursor"
	pluginJSON, err := marshalJSON(cursorPluginMeta{
		Name:        subdir,
		DisplayName: "Base (Cursor)",
		Description: "Gram observability hooks for " + cfg.OrgName + ". Install this plugin to forward tool events to your team's Gram dashboard.",
		Version:     "0.1.0",
		Author:      pluginAuthor{Name: cfg.OrgName, URL: "https://getgram.ai"},
		Homepage:    "https://getgram.ai",
	})
	if err != nil {
		return fmt.Errorf("marshal plugin.json: %w", err)
	}
	files[path.Join(subdir, ".cursor-plugin/plugin.json")] = pluginJSON

	hookEvents := make(map[string][]cursorHookCommand, len(BaseHookEvents))
	for _, event := range BaseHookEvents {
		hookEvents[cursorHookEventName(event)] = []cursorHookCommand{{Command: "./hook.sh"}}
	}
	hooksJSON, err := marshalJSON(cursorHooksConfig{Version: 1, Hooks: hookEvents})
	if err != nil {
		return fmt.Errorf("marshal hooks.json: %w", err)
	}
	files[path.Join(subdir, "hooks.json")] = hooksJSON

	files[path.Join(subdir, "hook.sh")] = renderHookScript(cfg, "cursor")

	return nil
}

// renderHookScript produces the bash wrapper that forwards hook event JSON
// from stdin to the appropriate Gram endpoint with the embedded API key.
// Cursor's endpoint additionally requires the Gram-Project header; Claude's
// does not declare a Security() block on the design side, so the project
// header is only emitted for the cursor variant.
func renderHookScript(cfg GenerateConfig, platform string) []byte {
	endpoint := fmt.Sprintf("%s/rpc/hooks.%s", cfg.ServerURL, platform)
	keyPrefix := cfg.HooksAPIKey
	if len(keyPrefix) > 12 {
		keyPrefix = keyPrefix[:12]
	}
	projectHeader := ""
	if platform == "cursor" && cfg.ProjectSlug != "" {
		projectHeader = fmt.Sprintf("  -H \"Gram-Project: %s\" \\\n", cfg.ProjectSlug)
	}
	return fmt.Appendf(nil, `#!/usr/bin/env bash
# Generated by Gram. Do not edit — overwritten on every publish.
# Key prefix: %s (correlate with the dashboard's API Keys page).
exec curl -sS -X POST \
  -H "Authorization: Bearer %s" \
  -H "Content-Type: application/json" \
%s  --data-binary @- \
  %q
`, keyPrefix, cfg.HooksAPIKey, projectHeader, endpoint)
}

func generateClaudePluginInDir(files map[string][]byte, subdir string, p PluginInfo, cfg GenerateConfig) error {
	// Collect userConfig entries across all servers that need user-provided values.
	userConfig := make(map[string]userConfigEntry)

	// Determine if any private server needs a Gram API key prompt.
	needsGramKeyPrompt := false
	for _, s := range p.Servers {
		if !s.IsPublic && cfg.APIKey == "" {
			needsGramKeyPrompt = true
		}
		// Public servers may need user-provided env vars.
		for _, ec := range s.EnvConfigs {
			userConfig[ec.VariableName] = userConfigEntry{
				Description: ec.DisplayName,
				Sensitive:   true,
			}
		}
	}

	if needsGramKeyPrompt {
		userConfig["GRAM_API_KEY"] = userConfigEntry{
			Description: "Your Gram API key for authenticating MCP server connections",
			Sensitive:   true,
		}
	}

	var userConfigPtr map[string]userConfigEntry
	if len(userConfig) > 0 {
		userConfigPtr = userConfig
	}

	pluginJSON, err := marshalJSON(claudePluginMeta{
		Name:        p.Slug,
		Description: p.Description,
		Version:     "0.1.0",
		Author:      pluginAuthor{Name: cfg.OrgName, URL: "https://getgram.ai"},
		Homepage:    "https://getgram.ai",
		UserConfig:  userConfigPtr,
	})
	if err != nil {
		return fmt.Errorf("marshal plugin.json: %w", err)
	}
	files[path.Join(subdir, ".claude-plugin/plugin.json")] = pluginJSON

	mcpServers := make(map[string]claudeMCPServer)
	for _, s := range p.Servers {
		headers := make(map[string]string)

		if s.IsPublic {
			// Public servers use env config variables for auth.
			for _, ec := range s.EnvConfigs {
				headers[ec.DisplayName] = "${user_config." + ec.VariableName + "}"
			}
		} else if cfg.APIKey != "" {
			// Private server with injected key.
			headers["Authorization"] = "Bearer " + cfg.APIKey
		} else {
			// Private server without key — prompt user.
			headers["Authorization"] = "Bearer ${user_config.GRAM_API_KEY}"
		}

		mcpServers[s.DisplayName] = claudeMCPServer{
			Type:    "http",
			URL:     s.MCPURL,
			Headers: headers,
		}
	}
	mcpJSON, err := marshalJSON(claudeMCPConfig{MCPServers: mcpServers})
	if err != nil {
		return fmt.Errorf("marshal .mcp.json: %w", err)
	}
	files[path.Join(subdir, ".mcp.json")] = mcpJSON

	return nil
}

func generateCursorPluginInDir(files map[string][]byte, subdir, name string, p PluginInfo, cfg GenerateConfig) error {
	displayName := p.Name
	if subdir != "" {
		displayName = p.Name + " (Cursor)"
	}

	pluginJSON, err := marshalJSON(cursorPluginMeta{
		Name:        name,
		DisplayName: displayName,
		Description: p.Description,
		Version:     "0.1.0",
		Author:      pluginAuthor{Name: cfg.OrgName, URL: "https://getgram.ai"},
		Homepage:    "https://getgram.ai",
	})
	if err != nil {
		return fmt.Errorf("marshal plugin.json: %w", err)
	}
	files[path.Join(subdir, ".cursor-plugin/plugin.json")] = pluginJSON

	mcpServers := make(map[string]cursorMCPServer)
	for _, s := range p.Servers {
		headers := make(map[string]string)

		if s.IsPublic {
			for _, ec := range s.EnvConfigs {
				headers[ec.DisplayName] = "${env:" + ec.VariableName + "}"
			}
		} else if cfg.APIKey != "" {
			headers["Authorization"] = "Bearer " + cfg.APIKey
		} else {
			headers["Authorization"] = "Bearer ${env:GRAM_API_KEY}"
		}

		mcpServers[s.DisplayName] = cursorMCPServer{
			URL:     s.MCPURL,
			Headers: headers,
		}
	}
	mcpJSON, err := marshalJSON(cursorMCPConfig{MCPServers: mcpServers})
	if err != nil {
		return fmt.Errorf("marshal mcp.json: %w", err)
	}
	files[path.Join(subdir, "mcp.json")] = mcpJSON

	return nil
}

// --- JSON types ---

type marketplaceManifest struct {
	Name    string             `json:"name"`
	Owner   marketplaceOwner   `json:"owner"`
	Plugins []marketplaceEntry `json:"plugins"`
}

type marketplaceOwner struct {
	Name  string `json:"name"`
	Email string `json:"email,omitempty"`
}

type marketplaceEntry struct {
	Name        string `json:"name"`
	Source      string `json:"source"`
	Description string `json:"description"`
}

type pluginAuthor struct {
	Name string `json:"name"`
	URL  string `json:"url"`
}

type claudePluginMeta struct {
	Name        string                     `json:"name"`
	Description string                     `json:"description"`
	Version     string                     `json:"version"`
	Author      pluginAuthor               `json:"author"`
	Homepage    string                     `json:"homepage"`
	UserConfig  map[string]userConfigEntry `json:"userConfig,omitempty"`
}

type userConfigEntry struct {
	Description string `json:"description"`
	Sensitive   bool   `json:"sensitive"`
}

type claudeMCPConfig struct {
	MCPServers map[string]claudeMCPServer `json:"mcpServers"`
}

type claudeMCPServer struct {
	Type    string            `json:"type"`
	URL     string            `json:"url"`
	Headers map[string]string `json:"headers,omitempty"`
}

type cursorPluginMeta struct {
	Name        string       `json:"name"`
	DisplayName string       `json:"displayName"`
	Description string       `json:"description"`
	Version     string       `json:"version"`
	Author      pluginAuthor `json:"author"`
	Homepage    string       `json:"homepage"`
}

type cursorMCPConfig struct {
	MCPServers map[string]cursorMCPServer `json:"mcpServers"`
}

type cursorMCPServer struct {
	URL     string            `json:"url"`
	Headers map[string]string `json:"headers,omitempty"`
}

// Hook config types — Claude uses PascalCase event names + a matcher
// wrapper, Cursor uses camelCase event names + a flatter shape.

type claudeHooksConfig struct {
	Hooks map[string][]claudeHookMatcher `json:"hooks"`
}

type claudeHookMatcher struct {
	Matcher string              `json:"matcher,omitempty"`
	Hooks   []claudeHookCommand `json:"hooks"`
}

type claudeHookCommand struct {
	Type    string `json:"type"`
	Command string `json:"command"`
}

type cursorHooksConfig struct {
	Version int                            `json:"version"`
	Hooks   map[string][]cursorHookCommand `json:"hooks"`
}

type cursorHookCommand struct {
	Command string `json:"command"`
}

// cursorHookEventName converts Gram's PascalCase event names to Cursor's
// camelCase (e.g. "PreToolUse" -> "preToolUse").
func cursorHookEventName(event string) string {
	if event == "" {
		return event
	}
	return strings.ToLower(event[:1]) + event[1:]
}

func marshalJSON(v any) ([]byte, error) {
	b, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return nil, fmt.Errorf("marshal JSON: %w", err)
	}
	return b, nil
}
