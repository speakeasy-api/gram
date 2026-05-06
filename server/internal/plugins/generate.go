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
	// observability plugin's hook script. If empty, the observability plugin
	// is omitted.
	HooksAPIKey string
	// ProjectSlug is the publishing project's slug. The Cursor hooks endpoint
	// requires it via the Gram-Project header (Claude's does not).
	ProjectSlug string
}

// ClaudeObservabilityHookEvents are the Claude Code hook events the
// observability plugin registers against. Names match Claude's hooks.json
// schema. Claude only invokes hook.sh for events listed here, so any event
// the Claude() handler in server/internal/hooks/claude_hooks.go expects to
// record must appear in this list — otherwise it is silently dropped on
// the client side.
var ClaudeObservabilityHookEvents = []string{
	"PreToolUse",
	"PostToolUse",
	"PostToolUseFailure",
	"SessionStart",
	"SessionEnd",
	"UserPromptSubmit",
	"Stop",
	"Notification",
}

// CursorObservabilityHookEvents are Cursor's native hook event names (per
// server/design/hooks/design.go:58). Cursor uses different event names
// than Claude, not a lowercased mirror, so the two lists are maintained
// separately.
var CursorObservabilityHookEvents = []string{
	"beforeSubmitPrompt",
	"stop",
	"afterAgentResponse",
	"afterAgentThought",
	"preToolUse",
	"postToolUse",
	"postToolUseFailure",
	"beforeMCPExecution",
	"afterMCPExecution",
}

// GeneratePluginPackages produces the complete file map for a plugin distribution
// repository containing Claude Code, Cursor, and Codex plugins. Used for GitHub push.
func GeneratePluginPackages(plugins []PluginInfo, cfg GenerateConfig) (map[string][]byte, error) {
	files := make(map[string][]byte)

	var claudePlugins []marketplaceEntry
	var cursorPlugins []marketplaceEntry
	var codexPlugins []codexMarketplaceEntry

	// Observability plugin ships first in the marketplace so it's the first
	// thing team admins see. Skipped when no hooks key is configured —
	// typically only in tests that don't exercise the publish flow.
	if cfg.HooksAPIKey != "" {
		if err := generateClaudeObservabilityPlugin(files, cfg); err != nil {
			return nil, fmt.Errorf("generate claude observability plugin: %w", err)
		}
		if err := generateCursorObservabilityPlugin(files, cfg); err != nil {
			return nil, fmt.Errorf("generate cursor observability plugin: %w", err)
		}
		claudeObservability := ClaudeObservabilitySlug(cfg)
		claudePlugins = append(claudePlugins, marketplaceEntry{
			Name:        claudeObservability,
			Source:      "./" + claudeObservability,
			Description: "Required: Gram observability hooks for " + cfg.OrgName + ".",
		})
		cursorObservability := CursorObservabilitySlug(cfg)
		cursorPlugins = append(cursorPlugins, marketplaceEntry{
			Name:        cursorObservability,
			Source:      "./" + cursorObservability,
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
		if err := generateCodexPlugin(files, p, cfg); err != nil {
			return nil, fmt.Errorf("generate codex plugin %s: %w", p.Slug, err)
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
		codexPlugins = append(codexPlugins, codexMarketplaceEntry{
			Name: p.Slug + "-codex",
			Source: codexMarketplaceSource{
				Source: "local",
				Path:   "./" + p.Slug + "-codex",
			},
			Policy: codexMarketplacePolicy{
				Installation:   "AVAILABLE",
				Authentication: codexAuthPolicy(p, cfg),
			},
		})
	}

	owner := marketplaceOwner{Name: cfg.OrgName, Email: cfg.OrgEmail}
	marketplaceName := conv.ToSlug(cfg.OrgName) + "-gram"

	claudeManifest, err := marshalJSON(marketplaceManifest{
		Name:    marketplaceName,
		Owner:   owner,
		Plugins: claudePlugins,
	})
	if err != nil {
		return nil, fmt.Errorf("marshal claude marketplace.json: %w", err)
	}
	files[".claude-plugin/marketplace.json"] = claudeManifest

	cursorManifest, err := marshalJSON(marketplaceManifest{
		Name:    marketplaceName,
		Owner:   owner,
		Plugins: cursorPlugins,
	})
	if err != nil {
		return nil, fmt.Errorf("marshal cursor marketplace.json: %w", err)
	}
	files[".cursor-plugin/marketplace.json"] = cursorManifest

	codexManifest, err := marshalJSON(codexMarketplaceManifest{
		Name:      marketplaceName,
		Interface: codexInterface{DisplayName: cfg.OrgName + " Plugins", ShortDescription: ""},
		Plugins:   codexPlugins,
	})
	if err != nil {
		return nil, fmt.Errorf("marshal codex marketplace.json: %w", err)
	}
	files[".agents/plugins/marketplace.json"] = codexManifest

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
	b.WriteString("Each plugin bundles MCP servers for distribution via Claude Code, Cursor, and Codex marketplaces.\n\n")
	b.WriteString("## How this repo works\n\n")
	b.WriteString("- **Read-only access.** Collaborators are granted pull permission only. You can clone and inspect the repository, but you cannot push to it.\n")
	b.WriteString("- **Auto-managed by Gram.** Each publish from the Gram dashboard overwrites this repository's contents. Any manual edits, new branches, or local commits will be discarded on the next publish — make changes in Gram instead.\n\n")

	if cfg.HooksAPIKey != "" {
		fmt.Fprintf(&b, "> **Required:** install the `%s` plugin alongside any feature plugins to enable Gram observability. Without it, your team will install MCP servers but tool events will not be reported to your Gram dashboard.\n\n", ClaudeObservabilitySlug(cfg))
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
		obs := ClaudeObservabilitySlug(cfg)
		fmt.Fprintf(&b, "\nMark the `%s` plugin as required so observability is on by default for all team members:\n\n", obs)
		fmt.Fprintf(&b, "```json\n{\n  \"plugins\": {\n    \"required\": [\"%s@%s-gram\"]\n  }\n}\n```\n", obs, conv.ToSlug(cfg.OrgName))
	}
	b.WriteString("\n### Cursor\n\n")
	b.WriteString("1. Open your team's [Cursor dashboard](https://cursor.com/dashboard)\n")
	b.WriteString("2. Navigate to **Settings → Plugins → Import**\n")
	b.WriteString("3. Paste this repository's URL to import the marketplace\n")
	b.WriteString("4. Plugins will be available to team members\n")
	if cfg.HooksAPIKey != "" {
		fmt.Fprintf(&b, "\nIn Cursor's team marketplace settings, mark the `%s` plugin as required so observability is on by default for all team members.\n", CursorObservabilitySlug(cfg))
	}

	b.WriteString("\n### Codex\n\n")
	b.WriteString("Add this repository as a plugin marketplace from the Codex CLI:\n\n")
	b.WriteString("```\ncodex plugin marketplace add <this-repo-url>\n```\n\n")
	b.WriteString("Then list available plugins with `codex /plugins` and install the ones you want.\n")
	b.WriteString("Plugins that need authentication will prompt for any required environment variables on install.\n")

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
	case "codex":
		if err := generateCodexPluginFlat(files, plugin, cfg); err != nil {
			return nil, fmt.Errorf("generate codex plugin: %w", err)
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

func generateCodexPluginFlat(files map[string][]byte, p PluginInfo, cfg GenerateConfig) error {
	return generateCodexPluginInDir(files, "", p.Slug, p, cfg)
}

func generateClaudePlugin(files map[string][]byte, p PluginInfo, cfg GenerateConfig) error {
	return generateClaudePluginInDir(files, p.Slug, p, cfg)
}

func generateCursorPlugin(files map[string][]byte, p PluginInfo, cfg GenerateConfig) error {
	return generateCursorPluginInDir(files, p.Slug+"-cursor", p.Slug+"-cursor", p, cfg)
}

func generateCodexPlugin(files map[string][]byte, p PluginInfo, cfg GenerateConfig) error {
	name := p.Slug + "-codex"
	return generateCodexPluginInDir(files, name, name, p, cfg)
}

// codexAuthPolicy picks ON_INSTALL when the user will be prompted for a
// secret (public server env vars or a Gram API key the published config
// can't bake in) and ON_USE when nothing needs to be collected. A baked-in
// APIKey plus all-public-no-env servers means the plugin is install-silent.
func codexAuthPolicy(p PluginInfo, cfg GenerateConfig) string {
	for _, s := range p.Servers {
		if s.IsPublic {
			if len(s.EnvConfigs) > 0 {
				return "ON_INSTALL"
			}
			continue
		}
		if cfg.APIKey == "" {
			return "ON_INSTALL"
		}
	}
	return "ON_USE"
}

func generateCodexPluginInDir(files map[string][]byte, subdir, name string, p PluginInfo, cfg GenerateConfig) error {
	pluginJSON, err := marshalJSON(codexPluginMeta{
		Name:        name,
		Version:     "0.1.0",
		Description: p.Description,
		MCPServers:  "./.mcp.json",
		Interface: &codexInterface{
			DisplayName:      p.Name,
			ShortDescription: p.Description,
		},
	})
	if err != nil {
		return fmt.Errorf("marshal plugin.json: %w", err)
	}
	files[path.Join(subdir, ".codex-plugin/plugin.json")] = pluginJSON

	mcpServers := make(map[string]codexMCPServer)
	for _, s := range p.Servers {
		entry := codexMCPServer{
			URL:               s.MCPURL,
			BearerTokenEnvVar: "",
			HTTPHeaders:       nil,
			EnvHTTPHeaders:    nil,
		}

		if s.IsPublic {
			// User provides each variable in their shell env; Codex
			// substitutes the value into the named header at runtime.
			if len(s.EnvConfigs) > 0 {
				entry.EnvHTTPHeaders = make(map[string]string, len(s.EnvConfigs))
				for _, ec := range s.EnvConfigs {
					entry.EnvHTTPHeaders[ec.DisplayName] = ec.VariableName
				}
			}
		} else if cfg.APIKey != "" {
			// Private server: bake the Gram-issued key directly into the
			// published config. Repo is private, so this matches the Cursor/Claude pattern.
			entry.HTTPHeaders = map[string]string{"Authorization": "Bearer " + cfg.APIKey}
		} else {
			// Private server, no key available: ask Codex to read GRAM_API_KEY
			// from the user's environment at startup.
			entry.BearerTokenEnvVar = "GRAM_API_KEY"
		}

		mcpServers[s.DisplayName] = entry
	}
	mcpJSON, err := marshalJSON(codexMCPConfig{MCPServers: mcpServers})
	if err != nil {
		return fmt.Errorf("marshal .mcp.json: %w", err)
	}
	files[path.Join(subdir, ".mcp.json")] = mcpJSON

	return nil
}

// ClaudeObservabilitySlug / CursorObservabilitySlug derive the observability
// plugin's directory name and marketplace identifier from the org slug.
// Namespacing per-org avoids collisions with user plugins that legitimately
// use slug "observability".
// Exported because tests need to predict the published path.
func ClaudeObservabilitySlug(cfg GenerateConfig) string {
	return conv.ToSlug(cfg.OrgName) + "-observability"
}
func CursorObservabilitySlug(cfg GenerateConfig) string {
	return conv.ToSlug(cfg.OrgName) + "-observability-cursor"
}

// generateClaudeObservabilityPlugin emits the per-org observability plugin
// containing Gram hooks for Claude Code. The hook script bakes in the org's
// hooks-scoped API key so no per-machine credential setup is required.
func generateClaudeObservabilityPlugin(files map[string][]byte, cfg GenerateConfig) error {
	return generateClaudeObservabilityPluginInDir(files, ClaudeObservabilitySlug(cfg), cfg)
}

// generateClaudeObservabilityPluginFlat emits the same files at the root
// (no subdir) for direct ZIP installation via `claude --plugin-dir`.
func generateClaudeObservabilityPluginFlat(files map[string][]byte, cfg GenerateConfig) error {
	return generateClaudeObservabilityPluginInDir(files, "", cfg)
}

func generateClaudeObservabilityPluginInDir(files map[string][]byte, subdir string, cfg GenerateConfig) error {
	name := subdir
	if name == "" {
		name = ClaudeObservabilitySlug(cfg)
	}
	pluginJSON, err := marshalJSON(claudePluginMeta{
		Name:        name,
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

	// Claude Code's plugin reference (https://code.claude.com/docs/en/plugins-reference)
	// requires hooks at hooks/hooks.json, not at the plugin root. With the
	// flat layout (hooks.json + hook.sh at root) Claude registers the plugin
	// silently but never wires the hooks up.
	matchers := []claudeHookMatcher{
		{Matcher: "*", Hooks: []claudeHookCommand{{Type: "command", Command: `bash "$CLAUDE_PLUGIN_ROOT/hooks/hook.sh"`}}},
	}
	hookEvents := make(map[string][]claudeHookMatcher, len(ClaudeObservabilityHookEvents))
	for _, event := range ClaudeObservabilityHookEvents {
		hookEvents[event] = matchers
	}
	hooksJSON, err := marshalJSON(claudeHooksConfig{Hooks: hookEvents})
	if err != nil {
		return fmt.Errorf("marshal hooks.json: %w", err)
	}
	files[path.Join(subdir, "hooks/hooks.json")] = hooksJSON

	files[path.Join(subdir, "hooks/hook.sh")] = renderHookScript(cfg, "claude")

	return nil
}

// generateCursorObservabilityPlugin emits the per-org observability plugin
// for Cursor. Same shape as the Claude variant but uses Cursor's hook event
// names + script destination URL.
func generateCursorObservabilityPlugin(files map[string][]byte, cfg GenerateConfig) error {
	return generateCursorObservabilityPluginInDir(files, CursorObservabilitySlug(cfg), cfg)
}

// generateCursorObservabilityPluginFlat emits the same files at the root
// (no subdir) for direct ZIP installation.
func generateCursorObservabilityPluginFlat(files map[string][]byte, cfg GenerateConfig) error {
	return generateCursorObservabilityPluginInDir(files, "", cfg)
}

func generateCursorObservabilityPluginInDir(files map[string][]byte, subdir string, cfg GenerateConfig) error {
	name := subdir
	if name == "" {
		name = CursorObservabilitySlug(cfg)
	}
	pluginJSON, err := marshalJSON(cursorPluginMeta{
		Name:        name,
		DisplayName: "Observability (Cursor)",
		Description: "Gram observability hooks for " + cfg.OrgName + ". Install this plugin to forward tool events to your team's Gram dashboard.",
		Version:     "0.1.0",
		Author:      pluginAuthor{Name: cfg.OrgName, URL: "https://getgram.ai"},
		Homepage:    "https://getgram.ai",
	})
	if err != nil {
		return fmt.Errorf("marshal plugin.json: %w", err)
	}
	files[path.Join(subdir, ".cursor-plugin/plugin.json")] = pluginJSON

	// Same hooks/ subdirectory layout as the Claude side. Cursor follows the
	// same convention as Claude Code; flat-layout plugins register but their
	// hooks never fire.
	hookEvents := make(map[string][]cursorHookCommand, len(CursorObservabilityHookEvents))
	for _, event := range CursorObservabilityHookEvents {
		hookEvents[event] = []cursorHookCommand{{Command: `bash "$CURSOR_PLUGIN_ROOT/hooks/hook.sh"`}}
	}
	hooksJSON, err := marshalJSON(cursorHooksConfig{Version: 1, Hooks: hookEvents})
	if err != nil {
		return fmt.Errorf("marshal hooks.json: %w", err)
	}
	files[path.Join(subdir, "hooks/hooks.json")] = hooksJSON

	files[path.Join(subdir, "hooks/hook.sh")] = renderHookScript(cfg, "cursor")

	return nil
}

// GenerateObservabilityPluginPackage produces the file map for a single
// observability plugin for direct ZIP installation (no <org>-observability/
// subdir). Minting a fresh hooks key is the caller's responsibility — this
// just renders files using cfg.HooksAPIKey.
func GenerateObservabilityPluginPackage(cfg GenerateConfig, platform string) (map[string][]byte, error) {
	files := make(map[string][]byte)
	switch platform {
	case "claude":
		if err := generateClaudeObservabilityPluginFlat(files, cfg); err != nil {
			return nil, fmt.Errorf("generate claude observability plugin: %w", err)
		}
	case "cursor":
		if err := generateCursorObservabilityPluginFlat(files, cfg); err != nil {
			return nil, fmt.Errorf("generate cursor observability plugin: %w", err)
		}
	default:
		return nil, fmt.Errorf("unsupported platform: %s", platform)
	}
	return files, nil
}

// renderHookScript produces the bash wrapper that forwards hook event JSON
// from stdin to the appropriate Gram endpoint. Both platforms now send
// Gram-Key + Gram-Project headers:
//   - Cursor (design.go:129) requires them via Security(ByKey, ProjectSlug).
//   - Claude (design.go:116) accepts them as optional headers; the handler
//     uses them for plugin-driven org/project attribution when present and
//     falls back to OTEL-seeded Redis session metadata when absent.
func renderHookScript(cfg GenerateConfig, platform string) []byte {
	endpoint := fmt.Sprintf("%s/rpc/hooks.%s", cfg.ServerURL, platform)
	keyPrefix := cfg.HooksAPIKey
	if len(keyPrefix) > 12 {
		keyPrefix = keyPrefix[:12]
	}

	authHeaders := fmt.Sprintf("  -H \"Gram-Key: %s\" \\\n", cfg.HooksAPIKey)
	if cfg.ProjectSlug != "" {
		authHeaders += fmt.Sprintf("  -H \"Gram-Project: %s\" \\\n", cfg.ProjectSlug)
	}

	return fmt.Appendf(nil, `#!/usr/bin/env bash
# Generated by Gram. Do not edit — overwritten on every publish.
# Key prefix: %s (correlate with the dashboard's API Keys page).
exec curl -sS -X POST \
%s  -H "Content-Type: application/json" \
  --data-binary @- \
  %q
`, keyPrefix, authHeaders, endpoint)
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

// Codex types — schema verified against openai/codex @
// f802f0a3911655ac0e2876fceedf8ad833431df3 (2026-04-24):
//   codex-rs/core-plugins/src/manifest.rs  — plugin manifest (rename_all = "camelCase")
//   codex-rs/core-plugins/src/loader.rs    — .mcp.json wrapper format
//   codex-rs/config/src/mcp_types.rs       — server transport (untagged; url selects streamable_http)
// Note: MCP server entry fields are snake_case; plugin manifest fields are camelCase.
// Refresh this pin when Codex's plugin support moves out of preview.

type codexPluginMeta struct {
	Name        string          `json:"name"`
	Version     string          `json:"version"`
	Description string          `json:"description,omitempty"`
	MCPServers  string          `json:"mcpServers"`
	Interface   *codexInterface `json:"interface,omitempty"`
}

type codexInterface struct {
	DisplayName      string `json:"displayName"`
	ShortDescription string `json:"shortDescription,omitempty"`
}

type codexMCPConfig struct {
	MCPServers map[string]codexMCPServer `json:"mcpServers"`
}

type codexMCPServer struct {
	URL               string            `json:"url"`
	BearerTokenEnvVar string            `json:"bearer_token_env_var,omitempty"`
	HTTPHeaders       map[string]string `json:"http_headers,omitempty"`
	EnvHTTPHeaders    map[string]string `json:"env_http_headers,omitempty"`
}

type codexMarketplaceManifest struct {
	Name      string                  `json:"name"`
	Interface codexInterface          `json:"interface"`
	Plugins   []codexMarketplaceEntry `json:"plugins"`
}

type codexMarketplaceEntry struct {
	Name   string                 `json:"name"`
	Source codexMarketplaceSource `json:"source"`
	Policy codexMarketplacePolicy `json:"policy"`
}

type codexMarketplaceSource struct {
	Source string `json:"source"`
	Path   string `json:"path"`
}

type codexMarketplacePolicy struct {
	Installation   string `json:"installation"`
	Authentication string `json:"authentication"`
}

func marshalJSON(v any) ([]byte, error) {
	b, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return nil, fmt.Errorf("marshal JSON: %w", err)
	}
	return b, nil
}
