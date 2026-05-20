package plugins

import (
	"crypto/sha256"
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
	// Version is stamped into every plugin.json. Callers should bump this on
	// every publish so platform marketplaces (Claude Code, Cursor, Codex) treat
	// the manifest as new and refresh installed copies. Empty falls back to
	// the static default, preserving test ergonomics.
	Version string
	// MarketplaceName is the identifier users type into Claude Code or Codex
	// (e.g. `<plugin>@<marketplace>`) and the `name` field in the generated
	// marketplace.json. Empty falls back to DefaultMarketplaceName.
	MarketplaceName string
}

// DefaultMarketplaceName is the marketplace identifier used when no
// per-project override is configured. Shows up as the `name` field in the
// generated Claude/Cursor/Codex marketplace.json and as the marketplace half
// of `<plugin>@<marketplace>` install strings.
const DefaultMarketplaceName = "speakeasy"

func resolveMarketplaceName(cfg GenerateConfig) string {
	return conv.Default(cfg.MarketplaceName, DefaultMarketplaceName)
}

// pluginManifestVersion returns the version to stamp into generated
// plugin.json files. Callers set cfg.Version to a per-publish value; the
// "0.1.0" fallback exists so package tests don't need to construct a version
// just to exercise unrelated assertions.
func pluginManifestVersion(cfg GenerateConfig) string {
	return conv.Default(cfg.Version, "0.1.0")
}

// claudeHookAsyncFlag returns the async flag for a Claude hook event.
// PreToolUse and UserPromptSubmit are blocking so Claude waits for the
// deny/allow decision. Stop must also be blocking: when async=true,
// Cowork (Claude Code) appears to skip dispatching the Stop hook entirely
// — an apparent bug on the client side. Marking it synchronous is the
// only reliable way to get Stop events to fire. All other events return
// true for fire-and-forget telemetry so Claude is not held up.
func claudeHookAsyncFlag(event string) *bool {
	switch event {
	case "UserPromptSubmit", "PreToolUse", "Stop":
		f := false
		return &f
	default:
		t := true
		return &t
	}
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

	claudePlugins := make([]marketplaceEntry, 0)
	cursorPlugins := make([]marketplaceEntry, 0)
	codexPlugins := make([]codexMarketplaceEntry, 0)

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
		if err := generateCodexObservabilityPlugin(files, cfg); err != nil {
			return nil, fmt.Errorf("generate codex observability plugin: %w", err)
		}
		claudeObservability := ClaudeObservabilitySlug(cfg)
		claudePlugins = append(claudePlugins, marketplaceEntry{
			Name:        claudeObservability,
			Source:      "./" + claudeObservability,
			Description: "Required: Speakeasy observability hooks for " + cfg.OrgName + ".",
		})
		cursorObservability := CursorObservabilitySlug(cfg)
		cursorPlugins = append(cursorPlugins, marketplaceEntry{
			Name:        cursorObservability,
			Source:      "./" + cursorObservability,
			Description: "Required: Speakeasy observability hooks for " + cfg.OrgName + ".",
		})
		codexObservability := CodexObservabilitySlug(cfg)
		codexPlugins = append(codexPlugins, codexMarketplaceEntry{
			Name: codexObservability,
			Source: codexMarketplaceSource{
				Source: "local",
				Path:   "./" + codexObservability,
			},
			Policy: codexMarketplacePolicy{
				Installation:   "INSTALLED_BY_DEFAULT",
				Authentication: "ON_USE",
			},
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
	marketplaceName := resolveMarketplaceName(cfg)

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
	b.WriteString("This repository contains plugin packages managed by [Speakeasy](https://getgram.ai). ")
	b.WriteString("Each plugin bundles MCP servers for distribution via Claude Code, Cursor, and Codex marketplaces.\n\n")
	b.WriteString("## How this repo works\n\n")
	b.WriteString("- **Read-only access.** Collaborators are granted pull permission only. You can clone and inspect the repository, but you cannot push to it.\n")
	b.WriteString("- **Auto-managed by Speakeasy.** Each publish from the Speakeasy dashboard overwrites this repository's contents. Any manual edits, new branches, or local commits will be discarded on the next publish — make changes in Speakeasy instead.\n\n")

	if cfg.HooksAPIKey != "" {
		fmt.Fprintf(&b, "> **Required:** install the `%s` plugin alongside any feature plugins to enable Speakeasy observability. Without it, your team will install MCP servers but tool events will not be reported to your Speakeasy dashboard.\n\n", ClaudeObservabilitySlug(cfg))
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
		fmt.Fprintf(&b, "```json\n{\n  \"plugins\": {\n    \"required\": [\"%s@%s\"]\n  }\n}\n```\n", obs, resolveMarketplaceName(cfg))
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
		Version:     pluginManifestVersion(cfg),
		Description: p.Description,
		MCPServers:  "./.mcp.json",
		Hooks:       "",
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
func CodexObservabilitySlug(cfg GenerateConfig) string {
	return conv.ToSlug(cfg.OrgName) + "-observability-codex"
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
		Description: "Speakeasy observability hooks for " + cfg.OrgName + ". Install this plugin to forward tool events to your team's Speakeasy dashboard.",
		Version:     pluginManifestVersion(cfg),
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
	//
	// SessionStart is routed to a separate script (session_start.sh) that
	// enriches the payload with an MCP server inventory before forwarding to
	// Gram. The inventory is sourced from cmux's per-run local_<rid>.json in
	// cowork environments, falling back to `claude mcp list` output when run
	// under stock Claude Code. All other events use the standard hook.sh.
	hookEvents := make(map[string][]claudeHookMatcher, len(ClaudeObservabilityHookEvents))
	for _, event := range ClaudeObservabilityHookEvents {
		script := "hook.sh"
		if event == "SessionStart" {
			script = "session_start.sh"
		}
		hookEvents[event] = []claudeHookMatcher{
			{Matcher: "", Hooks: []claudeHookCommand{{Type: "command", Command: `bash "$CLAUDE_PLUGIN_ROOT/hooks/` + script + `"`, Async: claudeHookAsyncFlag(event)}}},
		}
	}
	hooksJSON, err := marshalJSON(claudeHooksConfig{Hooks: hookEvents})
	if err != nil {
		return fmt.Errorf("marshal hooks.json: %w", err)
	}
	files[path.Join(subdir, "hooks/hooks.json")] = hooksJSON

	files[path.Join(subdir, "hooks/hook.sh")] = renderHookScript(cfg, "claude")
	files[path.Join(subdir, "hooks/session_start.sh")] = renderClaudeSessionStartScript(cfg)

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
		Description: "Speakeasy observability hooks for " + cfg.OrgName + ". Install this plugin to forward tool events to your team's Speakeasy dashboard.",
		Version:     pluginManifestVersion(cfg),
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

// generateCodexObservabilityPlugin emits the per-org observability plugin
// for Codex. Same shape as the Claude/Cursor variants but uses Codex's hook
// event names and script destination URL.
func generateCodexObservabilityPlugin(files map[string][]byte, cfg GenerateConfig) error {
	return generateCodexObservabilityPluginInDir(files, CodexObservabilitySlug(cfg), cfg)
}

// generateCodexObservabilityPluginFlat emits the same files at the root
// (no subdir) for direct ZIP installation.
func generateCodexObservabilityPluginFlat(files map[string][]byte, cfg GenerateConfig) error {
	if err := generateCodexObservabilityPluginInDir(files, "", cfg); err != nil {
		return err
	}
	// Add a marketplace manifest at the root so the extracted directory can be
	// used directly: `codex plugin marketplace add <path>`. Codex requires a
	// marketplace root (containing .agents/plugins/marketplace.json), not a bare
	// plugin root. path "." points back to the plugin at the ZIP root.
	marketplaceJSON, err := marshalJSON(codexMarketplaceManifest{
		Name:      resolveMarketplaceName(cfg),
		Interface: codexInterface{DisplayName: cfg.OrgName + " Plugins", ShortDescription: ""},
		Plugins: []codexMarketplaceEntry{{
			Name: CodexObservabilitySlug(cfg),
			Source: codexMarketplaceSource{
				Source: "local",
				Path:   ".",
			},
			Policy: codexMarketplacePolicy{
				Installation:   "INSTALLED_BY_DEFAULT",
				Authentication: "ON_USE",
			},
		}},
	})
	if err != nil {
		return fmt.Errorf("marshal marketplace.json: %w", err)
	}
	files[".agents/plugins/marketplace.json"] = marketplaceJSON

	// Bundle install.sh at the root so users can run `bash install.sh` after
	// extracting the ZIP. The local variant uses the script directory as the
	// marketplace source so no remote URL is needed.
	installScript, err := GenerateCodexInstallScript("", cfg)
	if err != nil {
		return fmt.Errorf("generate install script: %w", err)
	}
	files["install.sh"] = installScript

	return nil
}

func generateCodexObservabilityPluginInDir(files map[string][]byte, subdir string, cfg GenerateConfig) error {
	name := subdir
	if name == "" {
		name = CodexObservabilitySlug(cfg)
	}
	pluginJSON, err := marshalJSON(codexPluginMeta{
		Name:        name,
		Version:     pluginManifestVersion(cfg),
		Description: "Speakeasy observability hooks for " + cfg.OrgName + ". Install this plugin to forward tool events to your team's Speakeasy dashboard.",
		MCPServers:  "",
		Hooks:       "./hooks/hooks.json",
		Interface: &codexInterface{
			DisplayName:      "Observability (Codex)",
			ShortDescription: "Speakeasy observability hooks",
		},
	})
	if err != nil {
		return fmt.Errorf("marshal plugin.json: %w", err)
	}
	files[path.Join(subdir, ".codex-plugin/plugin.json")] = pluginJSON

	marketplace := resolveMarketplaceName(cfg)
	plugin := CodexObservabilitySlug(cfg)
	hookCmd := fmt.Sprintf(`bash "$HOME/.codex/.tmp/marketplaces/%s/%s/hooks/hook.sh"`, marketplace, plugin)
	hookEvents := make(map[string][]codexMatcherGroup, len(CodexObservabilityHookEvents))
	for _, event := range CodexObservabilityHookEvents {
		hookEvents[event] = []codexMatcherGroup{{
			Matcher: "",
			Hooks:   []codexHookCommand{{Type: "command", Command: hookCmd}},
		}}
	}
	hooksJSON, err := marshalJSON(codexHooksConfig{Hooks: hookEvents})
	if err != nil {
		return fmt.Errorf("marshal hooks.json: %w", err)
	}
	files[path.Join(subdir, "hooks/hooks.json")] = hooksJSON

	files[path.Join(subdir, "hooks/hook.sh")] = renderHookScript(cfg, "codex")

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
	case "codex":
		if err := generateCodexObservabilityPluginFlat(files, cfg); err != nil {
			return nil, fmt.Errorf("generate codex observability plugin: %w", err)
		}
	default:
		return nil, fmt.Errorf("unsupported platform: %s", platform)
	}
	return files, nil
}

// renderHookScript produces the bash wrapper that forwards hook event JSON
// from stdin to the appropriate Gram endpoint. Both platforms send
// Gram-Key + Gram-Project headers:
//   - Cursor (design.go:129) requires them via Security(ByKey, ProjectSlug).
//   - Claude (design.go:116) accepts them as optional headers; the handler
//     uses them for plugin-driven org/project attribution when present and
//     falls back to OTEL-seeded Redis session metadata when absent.
//
// The script captures the HTTP status code and response body separately so
// it can forward the body to stdout (for PreToolUse deny decisions) while
// still exiting with code 2 on 4xx/5xx to signal a block to Claude.
func renderHookScript(cfg GenerateConfig, platform string) []byte {
	keyPrefix := cfg.HooksAPIKey
	if len(keyPrefix) > 12 {
		keyPrefix = keyPrefix[:12]
	}

	authHeaders := fmt.Sprintf("  -H \"Gram-Key: %s\" \\\n", cfg.HooksAPIKey)
	if cfg.ProjectSlug != "" {
		authHeaders += fmt.Sprintf("  -H \"Gram-Project: %s\" \\\n", cfg.ProjectSlug)
	}

	// %%{http_code} → %{http_code} in the emitted script (curl write-out format).
	// %%s           → %s           in the emitted script (printf format).
	//
	// Claude reads hookSpecificOutput.permissionDecision from stdout on 2xx,
	// so the body is echoed unconditionally for the claude platform.
	// Codex treats any stdout as a structured response and rejects unknown JSON,
	// so for codex we suppress stdout on 2xx (empty stdout = allow).
	// Both platforms treat exit 2 as a block; the reason goes to stderr.
	if platform == "codex" {
		return fmt.Appendf(nil, `#!/usr/bin/env bash
# Generated by Speakeasy. Do not edit — overwritten on every publish.
# Key prefix: %s (correlate with the dashboard's API Keys page).

# Send a hook event to Speakeasy. The server is the sole authority on whether to block:
#   HTTP 2xx -> allow (exit 0, no stdout — Codex allow = empty stdout).
#   HTTP 4xx/5xx -> block (exit 2). Server message relayed to stderr.
# The script never makes the allow/deny decision — only the server does.

set -u

server_url="${GRAM_HOOKS_SERVER_URL:-%s}"

response=$(curl -s -w "\n%%{http_code}" -X POST \
%s  -H "Content-Type: application/json" \
  -d @- \
  --max-time 10 \
  "${server_url}/rpc/hooks.codex")

http_code=$(echo "$response" | tail -1)
body=$(echo "$response" | sed '$d')

# curl returns 000 on connection failure — treat as block so an unreachable
# Speakeasy server cannot silently bypass blocking policies.
if [ "$http_code" -ge 200 ] && [ "$http_code" -lt 400 ]; then
  exit 0
fi

reason=""
if command -v python3 >/dev/null 2>&1; then
  reason=$(printf '%%s' "$body" | python3 -c "
import json, sys
try:
    print(json.loads(sys.stdin.read()).get('message', ''), end='')
except Exception:
    pass
" 2>/dev/null) || true
fi

echo "${reason:-Speakeasy hook returned HTTP ${http_code}}" >&2
exit 2
`, keyPrefix, cfg.ServerURL, authHeaders)
	}

	return fmt.Appendf(nil, `#!/usr/bin/env bash
# Generated by Speakeasy. Do not edit — overwritten on every publish.
# Key prefix: %s (correlate with the dashboard's API Keys page).

# Send a hook event to Speakeasy. The server is the sole authority on whether to block:
#   HTTP 2xx -> allow (exit 0). Body forwarded to stdout; for PreToolUse,
#               Claude reads hookSpecificOutput.permissionDecision from it.
#   HTTP 4xx/5xx -> block (exit 2). Server message relayed to stderr.
# The script never makes the allow/deny decision — only the server does.

set -u

server_url="${GRAM_HOOKS_SERVER_URL:-%s}"

response=$(curl -s -w "\n%%{http_code}" -X POST \
%s  -H "Content-Type: application/json" \
  -d @- \
  --max-time 10 \
  "${server_url}/rpc/hooks.%s")

http_code=$(echo "$response" | tail -1)
body=$(echo "$response" | sed '$d')

echo "$body"

# curl returns 000 on connection failure — treat as block so an unreachable
# Speakeasy server cannot silently bypass blocking policies.
if [ "$http_code" -ge 200 ] && [ "$http_code" -lt 400 ]; then
  exit 0
fi

reason=""
if command -v python3 >/dev/null 2>&1; then
  reason=$(printf '%%s' "$body" | python3 -c "
import json, sys
try:
    print(json.loads(sys.stdin.read()).get('message', ''), end='')
except Exception:
    pass
" 2>/dev/null) || true
fi

echo "${reason:-Speakeasy hook returned HTTP ${http_code}}" >&2
exit 2
`, keyPrefix, cfg.ServerURL, authHeaders, platform)
}

// renderClaudeSessionStartScript produces the SessionStart-specific Claude
// hook script. It enriches the payload with an MCP server inventory before
// forwarding to Gram, picking the source by what the sandbox can see:
//
//   - cowork: when cmux's per-run config file (local_<rid>.json) is reachable
//     via CLAUDE_PROJECT_DIR/.., we ship its remoteMcpServersConfig array
//     verbatim as `additional_data.mcp_inventory_cowork`. This is the only
//     host-side spot where the connector UUID is paired with the MCP URL.
//   - Claude Code (default): shell out to `claude mcp list` and ship its raw
//     output as `additional_data.mcp_inventory_claude_code`.
//
// The script is fire-and-forget (async=true in hooks.json): SessionStart has
// no allow/deny decision to honor, so we always exit 0 and discard the
// response body to keep latency invisible to Claude.
//
// Auth headers match renderHookScript so server-side attribution works:
// Gram-Key always, Gram-Project when ProjectSlug is set.
func renderClaudeSessionStartScript(cfg GenerateConfig) []byte {
	keyPrefix := cfg.HooksAPIKey
	if len(keyPrefix) > 12 {
		keyPrefix = keyPrefix[:12]
	}

	authHeaders := fmt.Sprintf("  -H \"Gram-Key: %s\" \\\n", cfg.HooksAPIKey)
	if cfg.ProjectSlug != "" {
		authHeaders += fmt.Sprintf("  -H \"Gram-Project: %s\" \\\n", cfg.ProjectSlug)
	}

	return fmt.Appendf(nil, `#!/usr/bin/env bash
# Generated by Speakeasy. Do not edit — overwritten on every publish.
# Key prefix: %s (correlate with the dashboard's API Keys page).
#
# SessionStart-specific hook: enriches the payload with the active MCP
# server list and forwards it to Speakeasy. Runs async, so we fire-and-forget —
# SessionStart has no allow/deny decision to honor.
#
# Two execution environments are supported:
#   - cowork: detected by the presence of cmux's per-run local_<rid>.json
#     config file. We extract its remoteMcpServersConfig (connector UUID +
#     URL pairs) and ship them as mcp_inventory_cowork.
#   - Claude Code (default): shell out to `+"`claude mcp list`"+` and forward
#     the human-readable output as mcp_inventory_claude_code.

set -u

server_url="${GRAM_HOOKS_SERVER_URL:-%s}"

payload=$(cat)

mcp_inventory_claude_code=""
mcp_inventory_cowork="null"

# Locate cmux's per-run config file. CLAUDE_PROJECT_DIR is
# .../local_<rid>/outputs; the config sits one directory up as
# .../local_<rid>.json and lists the remote MCP connectors with their
# connector UUIDs. That's the only spot on the host filesystem where the
# UUID <-> URL pairing exists, so when we find it we ship it verbatim.
local_run_json=""

if [ -n "${CLAUDE_PROJECT_DIR:-}" ]; then
  candidate_local_dir=$(dirname "$CLAUDE_PROJECT_DIR")
  candidate_local_json="${candidate_local_dir}.json"
  if [ -f "$candidate_local_json" ]; then
    local_run_json="$candidate_local_json"
  else
    # SessionStart often fires before cmux writes the per-run config
    # file. Fall back to the most-recent sibling local_*.json — the
    # remoteMcpServersConfig block is account/org-scoped and identical
    # across runs in the same subid directory, so any sibling is good
    # enough for the UUID <-> URL mapping we care about.
    parent_dir=$(dirname "$candidate_local_dir")
    if [ -d "$parent_dir" ]; then
      sibling=$(ls -t "$parent_dir"/local_*.json 2>/dev/null | head -1)
      if [ -n "$sibling" ] && [ -f "$sibling" ]; then
        local_run_json="$sibling"
      fi
    fi
  fi
fi

if [ -n "$local_run_json" ] && command -v jq >/dev/null 2>&1; then
  # Extract the connector UUID + URL pairs we actually care about.
  # `+"`tools`"+` is dropped — it can be huge and we don't need it here.
  inv=$(jq -c '
    [
      (.remoteMcpServersConfig // [])[]
      | {
          connector_uuid: .uuid,
          name:           .name,
          url:            .url,
          source:         "claude.ai"
        }
    ]
  ' "$local_run_json" 2>/dev/null)
  [ -n "$inv" ] && mcp_inventory_cowork="$inv"
elif command -v claude >/dev/null 2>&1; then
  # Claude Code: `+"`claude mcp list`"+` health-checks every server, which can
  # take seconds for stdio servers. Hard-cap wall time so a misbehaving
  # server can't keep this hook alive forever; since the hook is async
  # the latency is invisible to Claude anyway. macOS doesn't ship GNU
  # `+"`timeout`"+` — prefer it, fall back to coreutils' `+"`gtimeout`"+`, then to
  # no timeout at all rather than failing.
  if command -v timeout >/dev/null 2>&1; then
    mcp_inventory_claude_code=$(timeout 15 claude mcp list 2>&1 || true)
  elif command -v gtimeout >/dev/null 2>&1; then
    mcp_inventory_claude_code=$(gtimeout 15 claude mcp list 2>&1 || true)
  else
    mcp_inventory_claude_code=$(claude mcp list 2>&1 || true)
  fi
fi

enriched=$(MCP_CC="$mcp_inventory_claude_code" \
           MCP_CW="$mcp_inventory_cowork" \
           PAYLOAD="$payload" \
           python3 -c '
import json, os, sys
try:
    p = json.loads(os.environ["PAYLOAD"])
except Exception:
    sys.exit(1)
ad = p.get("additional_data") or {}
cc = os.environ.get("MCP_CC", "")
if cc:
    ad["mcp_inventory_claude_code"] = cc
try:
    cw = json.loads(os.environ.get("MCP_CW", "null"))
except Exception:
    cw = None
if cw is not None:
    ad["mcp_inventory_cowork"] = cw
p["additional_data"] = ad
print(json.dumps(p))
') || enriched="$payload"

curl -s -o /dev/null -X POST \
%s  -H "Content-Type: application/json" \
  -d "$enriched" \
  --max-time 30 \
  "${server_url}/rpc/hooks.claude" >/dev/null 2>&1 || true

exit 0
`, keyPrefix, cfg.ServerURL, authHeaders)
}

// codexHookApproval is a single [hooks.state] entry that pre-approves a Codex
// hook event without requiring the user to click through Settings → Hooks.
type codexHookApproval struct {
	StateKey    string
	TrustedHash string
}

// codexEventSnakeCase maps PascalCase Codex hook event names to the snake_case
// form that Codex stores in [hooks.state] config keys.
func codexEventSnakeCase(event string) string {
	switch event {
	case "SessionStart":
		return "session_start"
	case "PreToolUse":
		return "pre_tool_use"
	case "PermissionRequest":
		return "permission_request"
	case "PostToolUse":
		return "post_tool_use"
	case "UserPromptSubmit":
		return "user_prompt_submit"
	case "Stop":
		return "stop"
	default:
		return strings.ToLower(event)
	}
}

// computeCodexHookHash returns the sha256:hex trusted_hash that Codex expects
// for a single hook entry. Codex's canonical JSON varies by event:
//
//   - SessionStart, PreToolUse, PermissionRequest, PostToolUse:
//     sha256(canonical_json({event_name, hooks:[{async, command, timeout, type}], matcher:""}))
//   - UserPromptSubmit, Stop:
//     sha256(canonical_json({event_name, hooks:[{async, command, timeout, type}]}))
//     (no matcher field — these events predate the matcher-group schema in fingerprint.rs)
//
// The command uses the literal string "$HOME" (not expanded), so the hash is
// deterministic for a given marketplace + plugin name pair.
func computeCodexHookHash(eventSnake, marketplace, plugin string) (string, error) {
	command := fmt.Sprintf(`bash "$HOME/.codex/.tmp/marketplaces/%s/%s/hooks/hook.sh"`, marketplace, plugin)
	hook := map[string]any{
		"async":   false,
		"command": command,
		"timeout": 600,
		"type":    "command",
	}
	// json.Marshal on map[string]any sorts keys alphabetically, matching
	// Codex's canonical JSON implementation in fingerprint.rs.
	canonical := map[string]any{
		"event_name": eventSnake,
		"hooks":      []map[string]any{hook},
	}
	// UserPromptSubmit and Stop use a canonical without the matcher field;
	// the older four events include matcher: "".
	switch eventSnake {
	case "user_prompt_submit", "stop":
		// no matcher field
	default:
		canonical["matcher"] = ""
	}
	data, err := json.Marshal(canonical)
	if err != nil {
		return "", fmt.Errorf("marshal canonical JSON: %w", err)
	}
	sum := sha256.Sum256(data)
	return fmt.Sprintf("sha256:%x", sum), nil
}

// computeCodexHookApprovals returns pre-computed [hooks.state] entries for all
// Codex observability hook events for a given marketplace and plugin name.
func computeCodexHookApprovals(marketplace, plugin string) ([]codexHookApproval, error) {
	approvals := make([]codexHookApproval, 0, len(CodexObservabilityHookEvents))
	for _, event := range CodexObservabilityHookEvents {
		snake := codexEventSnakeCase(event)
		hash, err := computeCodexHookHash(snake, marketplace, plugin)
		if err != nil {
			return nil, fmt.Errorf("compute hash for %s: %w", event, err)
		}
		approvals = append(approvals, codexHookApproval{
			StateKey:    fmt.Sprintf(`%s@%s:hooks/hooks.json:%s:0:0`, plugin, marketplace, snake),
			TrustedHash: hash,
		})
	}
	return approvals, nil
}

// GenerateCodexInstallScript produces a bash install script that:
//   - Registers the Gram marketplace with the Codex CLI
//   - Patches ~/.codex/config.toml with feature flags and plugin entry
//   - Pre-approves all hook events so users skip the manual Settings → Hooks step
//
// When marketplaceURL is empty the script uses the directory it was run from as
// the marketplace source (suitable for the ZIP-bundled install.sh). When
// marketplaceURL is non-empty the script registers the remote URL instead.
func GenerateCodexInstallScript(marketplaceURL string, cfg GenerateConfig) ([]byte, error) {
	marketplace := resolveMarketplaceName(cfg)
	plugin := CodexObservabilitySlug(cfg)

	approvals, err := computeCodexHookApprovals(marketplace, plugin)
	if err != nil {
		return nil, fmt.Errorf("compute hook approvals: %w", err)
	}

	return renderCodexInstallScript(marketplaceURL, marketplace, plugin, approvals), nil
}

func renderCodexInstallScript(marketplaceURL, marketplace, plugin string, approvals []codexHookApproval) []byte {
	var b strings.Builder

	fmt.Fprintf(&b, "#!/usr/bin/env bash\n")
	fmt.Fprintf(&b, "# Speakeasy Codex Observability Plugin — Install Script\n")
	fmt.Fprintf(&b, "# Marketplace: %s\n\n", marketplace)
	b.WriteString("set -euo pipefail\n\n")
	fmt.Fprintf(&b, "MARKETPLACE_KEY=%q\n", marketplace)
	fmt.Fprintf(&b, "PLUGIN_KEY=%q\n\n", plugin)

	// Step 1: marketplace registration differs for remote vs local ZIP installs.
	if marketplaceURL != "" {
		fmt.Fprintf(&b, "MARKETPLACE_URL=%q\n\n", marketplaceURL)
		b.WriteString(`# ── 1. Register & sync marketplace ──────────────────────────────────────────
echo "→ Registering Speakeasy marketplace..."
if command -v codex >/dev/null 2>&1; then
  # add is idempotent (no-ops if already registered); upgrade pulls any new commits.
  codex plugin marketplace add "${MARKETPLACE_URL}" || true
  codex plugin marketplace upgrade "${MARKETPLACE_KEY}"
else
  echo "  ⚠  'codex' not found in PATH."
  echo "     Run manually: codex plugin marketplace add '${MARKETPLACE_URL}'"
fi

`)
	} else {
		b.WriteString(`# ── 1. Register marketplace (local ZIP install) ──────────────────────────────
SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
echo "→ Registering Speakeasy marketplace from ${SCRIPT_DIR}..."
if command -v codex >/dev/null 2>&1; then
  codex plugin marketplace add "${SCRIPT_DIR}"
else
  echo "  ⚠  'codex' not found in PATH."
  echo "     Run manually: codex plugin marketplace add '${SCRIPT_DIR}'"
fi

`)
	}

	// Step 2: patch ~/.codex/config.toml via embedded Python (always available
	// on macOS, where Codex runs). Uses a quoted heredoc (<<'PYTHON') so bash
	// does not expand $PLUGIN_KEY etc. inside the Python source.
	b.WriteString(`# ── 2. Patch ~/.codex/config.toml ────────────────────────────────────────────
echo "→ Configuring ~/.codex/config.toml..."
python3 - <<'PYTHON'
import os, re

config_path = os.path.expanduser("~/.codex/config.toml")
os.makedirs(os.path.dirname(config_path), exist_ok=True)
content = open(config_path).read() if os.path.exists(config_path) else ""

def ensure_dotted_key(text, key, value):
    if re.search(r'(?m)^' + re.escape(key) + r'\s*=', text):
        return text
    # Insert before the first table header so the key stays at root scope.
    # Appending after a [table] section would silently nest it inside that table.
    m = re.search(r'(?m)^\[', text)
    if m:
        before = text[:m.start()].rstrip('\n')
        return before + '\n' + key + ' = ' + value + '\n\n' + text[m.start():]
    return text.rstrip('\n') + '\n' + key + ' = ' + value + '\n'

def ensure_table_entry(text, table_header, key, value):
    if re.search(r'(?m)^' + re.escape(table_header) + r'\s*\n(?:[^\[]*\n)*' + re.escape(key) + r'\s*=', text):
        return text
    if table_header not in text:
        return text.rstrip('\n') + '\n\n' + table_header + '\n' + key + ' = ' + value + '\n'
    idx = text.index(table_header) + len(table_header)
    return text[:idx] + '\n' + key + ' = ' + value + text[idx:]

`)

	// Python literals — embedded at generation time, not expanded by bash.
	fmt.Fprintf(&b, "PLUGIN_KEY = %q\n", plugin)
	fmt.Fprintf(&b, "MARKETPLACE_KEY = %q\n\n", marketplace)

	b.WriteString(`content = ensure_dotted_key(content, "features.hooks", "true")
content = ensure_dotted_key(content, "features.plugin_hooks", "true")

if not re.search(r'(?m)^\[hooks\.state\]', content):
    content = content.rstrip('\n') + '\n\n[hooks.state]\n'

for state_key, trusted_hash in [
`)

	for _, a := range approvals {
		fmt.Fprintf(&b, "    (%q, %q),\n", a.StateKey, a.TrustedHash)
	}

	b.WriteString(`]:
    section = f'[hooks.state."{state_key}"]'
    if section not in content:
        entry = f'\n{section}\nenabled = true\ntrusted_hash = "{trusted_hash}"\n'
        content = content.rstrip('\n') + '\n' + entry

content = ensure_table_entry(content, f'[plugins."{PLUGIN_KEY}@{MARKETPLACE_KEY}"]', "enabled", "true")

with open(config_path, 'w') as f:
    f.write(content)
print("  ✓ Config updated.")
PYTHON

echo ""
echo "✓ Speakeasy observability plugin installed. Restart Codex to activate."
`)

	return []byte(b.String())
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
			Description: "Your Speakeasy API key for authenticating MCP server connections",
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
		Version:     pluginManifestVersion(cfg),
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
		Version:     pluginManifestVersion(cfg),
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
	// Async controls whether Claude waits for the hook before proceeding.
	// nil omits the field. false = blocking (PreToolUse, UserPromptSubmit);
	// true = fire-and-forget (Stop, PostToolUse, etc.).
	Async *bool `json:"async,omitempty"`
}

type cursorHooksConfig struct {
	Version int                            `json:"version"`
	Hooks   map[string][]cursorHookCommand `json:"hooks"`
}

type cursorHookCommand struct {
	Command string `json:"command"`
}

type codexHooksConfig struct {
	Hooks map[string][]codexMatcherGroup `json:"hooks"`
}

type codexMatcherGroup struct {
	Matcher string             `json:"matcher"`
	Hooks   []codexHookCommand `json:"hooks"`
}

type codexHookCommand struct {
	Type    string `json:"type"`
	Command string `json:"command"`
}

// CodexObservabilityHookEvents are Codex's hook event names. Codex uses
// PascalCase names and has a PermissionRequest event that Claude/Cursor lack.
var CodexObservabilityHookEvents = []string{
	"SessionStart",
	"PreToolUse",
	"PermissionRequest",
	"PostToolUse",
	"UserPromptSubmit",
	"Stop",
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
	MCPServers  string          `json:"mcpServers,omitempty"`
	Hooks       string          `json:"hooks,omitempty"`
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
