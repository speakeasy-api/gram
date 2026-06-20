package plugins

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"path"
	"sort"
	"strings"
	"unicode/utf8"

	"github.com/speakeasy-api/gram/server/internal/conv"
	"github.com/speakeasy-api/gram/server/internal/plugins/naming"
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
	// IsOAuth indicates the toolset uses OAuth (proxy or external). OAuth servers are emitted
	// as stdio mcp-remote entries instead of HTTP-with-headers entries.
	IsOAuth bool
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
	// requires it via the Gram-Project header (Claude's does not), and it scopes
	// the default marketplace name for non-default projects.
	ProjectSlug string
	// IsDefaultProject reports whether this is the org's default project (its
	// oldest, by id ASC). The default project keeps the bare org-derived
	// marketplace name; non-default projects get a project-scoped one. Must be
	// resolved identically to the device-agent endpoint (see naming.MarketplaceName).
	IsDefaultProject bool
	// Version is stamped into every plugin.json. Callers should bump this on
	// every publish so platform marketplaces (Claude Code, Cursor, Codex) treat
	// the manifest as new and refresh installed copies. Empty falls back to
	// the static default, preserving test ergonomics.
	Version string
	// MarketplaceName is the identifier users type into Claude Code or Codex
	// (e.g. `<plugin>@<marketplace>`) and the `name` field in the generated
	// marketplace.json. Empty falls back to DefaultMarketplaceName.
	MarketplaceName string
	// ObservabilityMode makes the generated hook plugin fully non-blocking when
	// set: every Claude hook event is emitted async so the plugin can only
	// observe and report, never deny or delay a tool call. It is the low-risk
	// path for POC rollouts in orgs that cannot tolerate hook errors or brief
	// server unavailability disrupting the user.
	ObservabilityMode bool
}

// DefaultMarketplaceName returns the marketplace identifier used when no
// per-project override is configured: the slugified org name with a
// "-speakeasy" suffix. Shows up as the `name` field in the generated
// Claude/Cursor/Codex marketplace.json and as the marketplace half of
// `<plugin>@<marketplace>` install strings. Suffixing by org keeps the
// default unique across customers so Claude Code installs from two Gram
// orgs don't collide on the marketplace identifier.
//
// Delegates to naming.MarketplaceName so the publish path and the device-agent
// endpoint stay on the identical formula — the cross-surface contract that
// package documents. A per-project override (resolveMarketplaceName) layers on
// top of this default. The default project keeps the bare org-derived name;
// non-default projects are scoped by their slug so an org's projects don't
// collide on a single marketplace identifier.
func DefaultMarketplaceName(orgName, projectSlug string, isDefaultProject bool) string {
	return naming.MarketplaceName(orgName, projectSlug, isDefaultProject)
}

func resolveMarketplaceName(cfg GenerateConfig) string {
	return conv.Default(cfg.MarketplaceName, DefaultMarketplaceName(cfg.OrgName, cfg.ProjectSlug, cfg.IsDefaultProject))
}

// pluginManifestVersion returns the version to stamp into generated
// plugin.json files. Callers set cfg.Version to a per-publish value; the
// "0.1.0" fallback exists so package tests don't need to construct a version
// just to exercise unrelated assertions.
func pluginManifestVersion(cfg GenerateConfig) string {
	return conv.Default(cfg.Version, "0.1.0")
}

// pluginGeneratorVersion is mixed into every plugin fingerprint. Bump it to
// force the automated rollout to republish every connected project on the next
// run, even when an individual project's generated output is byte-identical —
// for generator changes that alter behaviour in ways the placeholder
// fingerprint pass can't observe. The Plugin Generate Check CI workflow
// requires this to change whenever generate.go does.
const pluginGeneratorVersion = "5"

// Fixed, non-empty sentinels substituted for the per-publish API keys when
// computing a fingerprint. They must be non-empty: an empty HooksAPIKey omits
// the observability plugin entirely (see GenerateConfig.HooksAPIKey), which
// would make the fingerprint blind to it. Constant values keep the generated
// bytes stable across publishes while the real keys rotate.
const (
	fingerprintAPIKeySentinel   = "gram_fingerprint_api_key"
	fingerprintHooksKeySentinel = "gram_fingerprint_hooks_key"
)

// PluginFingerprint returns a stable content hash of the packages that would be
// generated for the given plugins. It normalizes the per-publish fields
// (manifest version and injected API keys) so two publishes with the same
// plugin configuration and generator version produce the same fingerprint,
// while any change to the plugin set, project/org config, or generator output
// changes it. The automated rollout uses this to skip no-op republishes.
func PluginFingerprint(plugins []PluginInfo, cfg GenerateConfig) (string, error) {
	cfg.Version = ""
	cfg.APIKey = fingerprintAPIKeySentinel
	cfg.HooksAPIKey = fingerprintHooksKeySentinel

	files, err := GeneratePluginPackages(plugins, cfg)
	if err != nil {
		return "", fmt.Errorf("generate plugin packages for fingerprint: %w", err)
	}

	paths := make([]string, 0, len(files))
	for p := range files {
		paths = append(paths, p)
	}
	sort.Strings(paths)

	h := sha256.New()
	// A NUL after each component disambiguates boundaries so no concatenation
	// of distinct (path, content) sequences can collide.
	_, _ = h.Write([]byte(pluginGeneratorVersion))
	_, _ = h.Write([]byte{0})
	for _, p := range paths {
		_, _ = h.Write([]byte(p))
		_, _ = h.Write([]byte{0})
		_, _ = h.Write(files[p])
		_, _ = h.Write([]byte{0})
	}

	return "sha256:" + hex.EncodeToString(h.Sum(nil)), nil
}

// claudeHookAsyncFlag returns the async flag for a Claude hook event.
// PreToolUse and UserPromptSubmit are blocking so Claude waits for the
// deny/allow decision. Stop must also be blocking: when async=true,
// Cowork (Claude Code) appears to skip dispatching the Stop hook entirely
// — an apparent bug on the client side. Marking it synchronous is the
// only reliable way to get Stop events to fire. All other events
// (including ConfigChange, which has no allow/deny decision to honor)
// return true for fire-and-forget telemetry so Claude is not held up while
// the MCP inventory is re-synced mid-session.
//
// When observabilityMode is set, every event is forced async so the plugin
// can only observe and report — no hook can deny or delay a tool call. This
// is the low-risk path for POC rollouts in orgs that cannot tolerate hook
// errors or brief server unavailability disrupting the user.
func claudeHookAsyncFlag(event string, observabilityMode bool) *bool {
	if observabilityMode {
		t := true
		return &t
	}
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
	"ConfigChange",
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

// cursorPluginRoot is the subdirectory under which all Cursor plugins are
// grouped in a published repo. Declared via marketplace.json's metadata.pluginRoot
// so plugin sources can be referenced by bare name relative to this root.
const cursorPluginRoot = "cursor-plugins"

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
			Source:      cursorObservability,
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
			Source:      p.Slug + "-cursor",
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
		Name:     marketplaceName,
		Owner:    owner,
		Metadata: nil,
		Plugins:  claudePlugins,
	})
	if err != nil {
		return nil, fmt.Errorf("marshal claude marketplace.json: %w", err)
	}
	files[".claude-plugin/marketplace.json"] = claudeManifest

	cursorManifest, err := marshalJSON(marketplaceManifest{
		Name:     marketplaceName,
		Owner:    owner,
		Metadata: &marketplaceMetadata{PluginRoot: cursorPluginRoot},
		Plugins:  cursorPlugins,
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
	name := p.Slug + "-cursor"
	return generateCursorPluginInDir(files, path.Join(cursorPluginRoot, name), name, p, cfg)
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

		if s.IsOAuth {
			// OAuth servers handle identity at the HTTP layer — no auth credential needed.
		} else if s.IsPublic {
			if len(s.EnvConfigs) > 0 {
				entry.EnvHTTPHeaders = make(map[string]string, len(s.EnvConfigs))
				for _, ec := range s.EnvConfigs {
					entry.EnvHTTPHeaders[ec.DisplayName] = ec.VariableName
				}
			}
		} else if cfg.APIKey != "" {
			entry.HTTPHeaders = map[string]string{"Authorization": "Bearer " + cfg.APIKey}
		} else {
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
	return naming.ObservabilitySlug(cfg.OrgName)
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
	// SessionStart and ConfigChange are routed to a separate script
	// (mcp_inventory.sh) that enriches the payload with an MCP server
	// inventory before forwarding to Gram, so the server can re-sync its
	// cached inventory whenever Claude (re)loads the session or a settings
	// file changes mid-session. The inventory is sourced from cmux's per-run
	// local_<rid>.json in cowork environments, falling back to
	// `claude mcp list` output when run under stock Claude Code. All other
	// events use the standard hook.sh.
	hookEvents := make(map[string][]claudeHookMatcher, len(ClaudeObservabilityHookEvents))
	for _, event := range ClaudeObservabilityHookEvents {
		script := "hook.sh"
		if event == "SessionStart" || event == "ConfigChange" {
			script = "mcp_inventory.sh"
		}
		command := `bash "$CLAUDE_PLUGIN_ROOT/hooks/` + script + `"`
		hookEvents[event] = []claudeHookMatcher{
			{Matcher: "", Hooks: []claudeHookCommand{{Type: "command", Command: command, Async: claudeHookAsyncFlag(event, cfg.ObservabilityMode)}}},
		}
	}
	hooksJSON, err := marshalJSON(claudeHooksConfig{Hooks: hookEvents})
	if err != nil {
		return fmt.Errorf("marshal hooks.json: %w", err)
	}
	files[path.Join(subdir, "hooks/hooks.json")] = hooksJSON

	files[path.Join(subdir, "hooks/identity.sh")] = renderDeviceAgentIdentityScript()
	files[path.Join(subdir, "hooks/http.sh")] = renderSharedHTTPScript()
	files[path.Join(subdir, "hooks/hook.sh")] = renderHookScript(cfg, "claude")
	if !cfg.ObservabilityMode {
		files[path.Join(subdir, "hooks/breaker.sh")] = renderBreakerScript(name)
	}
	files[path.Join(subdir, "hooks/mcp_inventory.sh")] = renderClaudeMCPInventoryScript(cfg)

	return nil
}

// generateCursorObservabilityPlugin emits the per-org observability plugin
// for Cursor. Same shape as the Claude variant but uses Cursor's hook event
// names + script destination URL.
func generateCursorObservabilityPlugin(files map[string][]byte, cfg GenerateConfig) error {
	name := CursorObservabilitySlug(cfg)
	return generateCursorObservabilityPluginInDir(files, path.Join(cursorPluginRoot, name), name, cfg)
}

// generateCursorObservabilityPluginFlat emits the same files at the root
// (no subdir) for direct ZIP installation.
func generateCursorObservabilityPluginFlat(files map[string][]byte, cfg GenerateConfig) error {
	return generateCursorObservabilityPluginInDir(files, "", CursorObservabilitySlug(cfg), cfg)
}

func generateCursorObservabilityPluginInDir(files map[string][]byte, subdir, name string, cfg GenerateConfig) error {
	pluginJSON, err := marshalJSON(cursorPluginMeta{
		Name:        name,
		DisplayName: "Observability (Cursor)",
		Description: "Speakeasy observability hooks for " + cfg.OrgName + ". Install this plugin to forward tool events to your team's Speakeasy dashboard.",
		Version:     pluginManifestVersion(cfg),
		Author:      cursorAuthor{Name: cfg.OrgName, Email: cfg.OrgEmail},
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

	files[path.Join(subdir, "hooks/identity.sh")] = renderDeviceAgentIdentityScript()
	files[path.Join(subdir, "hooks/http.sh")] = renderSharedHTTPScript()
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
	hookEvents := make(map[string][]codexMatcherGroup, len(CodexObservabilityHookEvents))
	for _, event := range CodexObservabilityHookEvents {
		hookEvents[event] = []codexMatcherGroup{{
			Matcher: "",
			Hooks:   []codexHookCommand{{Type: "command", Command: codexHookCommandString(marketplace, plugin, codexHookScriptName(event))}},
		}}
	}
	hooksJSON, err := marshalJSON(codexHooksConfig{Hooks: hookEvents})
	if err != nil {
		return fmt.Errorf("marshal hooks.json: %w", err)
	}
	files[path.Join(subdir, "hooks/hooks.json")] = hooksJSON

	files[path.Join(subdir, "hooks/identity.sh")] = renderDeviceAgentIdentityScript()
	files[path.Join(subdir, "hooks/http.sh")] = renderSharedHTTPScript()
	files[path.Join(subdir, "hooks/hook.sh")] = renderHookScript(cfg, "codex")
	files[path.Join(subdir, "hooks/hook_async.sh")] = renderCodexAsyncHookScript()

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

func renderDeviceAgentIdentityScript() []byte {
	return []byte(`#!/usr/bin/env bash
# Generated by Speakeasy. Do not edit - overwritten on every publish.

gram_enrich_identity_payload() {
  local payload="$1"
  local email=""
  local commands="${GRAM_DEVICE_AGENT_COMMANDS:-speakeasyd}"
  local timeout_tenths="${GRAM_DEVICE_AGENT_TIMEOUT_TENTHS:-15}"
  local old_ifs="$IFS"
  local command output tmp pid elapsed prefix trimmed

  IFS=,
  for command in $commands; do
    IFS="$old_ifs"
    command="${command#"${command%%[![:space:]]*}"}"
    command="${command%"${command##*[![:space:]]}"}"
    if [ -z "$command" ] || ! command -v "$command" >/dev/null 2>&1; then
      IFS=,
      continue
    fi

    tmp="$(mktemp "${TMPDIR:-/tmp}/gram-device-agent-identity.XXXXXX")" || {
      IFS=,
      continue
    }
    ("$command" identity >"$tmp" 2>/dev/null) &
    pid=$!
    elapsed=0
    while kill -0 "$pid" >/dev/null 2>&1 && [ "$elapsed" -lt "$timeout_tenths" ]; do
      sleep 0.1
      elapsed=$((elapsed + 1))
    done
    if kill -0 "$pid" >/dev/null 2>&1; then
      kill "$pid" >/dev/null 2>&1 || true
      wait "$pid" >/dev/null 2>&1 || true
      rm -f "$tmp"
      IFS=,
      continue
    fi
    wait "$pid" >/dev/null 2>&1 || true
    output=$(cat "$tmp" 2>/dev/null || true)
    rm -f "$tmp"

    if [[ "$output" =~ ^[[:space:]]*([A-Za-z0-9._%+\-]+@[A-Za-z0-9.\-]+\.[A-Za-z]{2,})[[:space:]]*$ ]]; then
      email="${BASH_REMATCH[1]}"
    elif [[ "$output" =~ \"(email|user_email|userEmail|mail|preferred_username)\"[[:space:]]*:[[:space:]]*\"([A-Za-z0-9._%+\-]+@[A-Za-z0-9.\-]+\.[A-Za-z]{2,})\" ]]; then
      email="${BASH_REMATCH[2]}"
    fi
    if [ -n "$email" ]; then
      break
    fi
    IFS=,
  done
  IFS="$old_ifs"

  if [ -z "$email" ]; then
    printf '%s' "$payload"
    return
  fi

  trimmed=$(printf '%s' "$payload" | sed 's/[[:space:]]*$//')
  case "$trimmed" in
    \{*\})
      if [[ "$trimmed" =~ \"user_email\"[[:space:]]*: ]]; then
        printf '%s' "$trimmed" | sed -E 's|"user_email"[[:space:]]*:[[:space:]]*"[^"]*"|"user_email":"'"$email"'"|g'
        return
      fi
      prefix="${trimmed%\}}"
      if [[ "$prefix" =~ ^\{[[:space:]]*$ ]]; then
        printf '{"user_email":"%s"}' "$email"
      else
        printf '%s,"user_email":"%s"}' "$prefix" "$email"
      fi
      ;;
    *)
      printf '%s' "$payload"
      ;;
  esac
}
`)
}

func renderIdentitySourceSnippet() string {
	return `script_dir="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
if [ -f "$script_dir/identity.sh" ]; then
  # shellcheck source=/dev/null
  . "$script_dir/identity.sh"
fi
# shellcheck source=/dev/null
. "$script_dir/http.sh"
`
}

// renderSharedHTTPScript emits hooks/http.sh: the retryable transport sourced
// by every generated hook script. It must stay byte-for-byte identical to the
// checked-in hooks/plugin-claude/hooks/http.sh so local-dev and generated
// plugins behave the same. gram_http_post retries transient connection resets
// and 5xx with backoff while reusing one Idempotency-Key across attempts, so
// the server (which de-duplicates on that key) stores a redelivery once.
func renderSharedHTTPScript() []byte {
	return []byte(`# Shared retryable HTTP helper for Gram hook senders.
#
# Sourced (not executed) by every plugin's send/hook scripts so all plugins
# share one transport with identical retry and idempotency behavior.
#
# Why retries are safe: the server de-duplicates on the per-invocation
# Idempotency-Key header (see gram_http_post), so re-sending the same request
# after a transient reset stores the event exactly once.
#
# Usage:
#   . "$script_dir/http.sh"
#   gram_http_post "$url" "$payload" max_time [extra curl args...]
#   # then read $GRAM_HTTP_CODE (e.g. "200", "000") and $GRAM_HTTP_BODY.
#
# gram_http_post returns 0 once curl produced a definitive HTTP status (any
# code, including 4xx/5xx — the caller decides allow/block from $GRAM_HTTP_CODE)
# and non-zero only when every attempt failed to reach the server.

GRAM_HTTP_MAX_ATTEMPTS="${GRAM_HTTP_MAX_ATTEMPTS:-4}"
GRAM_HTTP_BACKOFF_BASE="${GRAM_HTTP_BACKOFF_BASE:-1}"

# gram_new_idempotency_token emits one token per shell invocation, captured
# once and reused across retries so every attempt of the same logical delivery
# carries the same token.
gram_new_idempotency_token() {
  if [ -n "${GRAM_IDEMPOTENCY_TOKEN:-}" ]; then
    printf '%s' "$GRAM_IDEMPOTENCY_TOKEN"
    return 0
  fi
  if command -v uuidgen >/dev/null 2>&1; then
    GRAM_IDEMPOTENCY_TOKEN=$(uuidgen | tr '[:upper:]' '[:lower:]')
  elif [ -r /proc/sys/kernel/random/uuid ]; then
    GRAM_IDEMPOTENCY_TOKEN=$(cat /proc/sys/kernel/random/uuid)
  else
    GRAM_IDEMPOTENCY_TOKEN="$(date +%s)-$$-${RANDOM:-0}${RANDOM:-0}"
  fi
  printf '%s' "$GRAM_IDEMPOTENCY_TOKEN"
}

# _gram_http_is_transient returns 0 for a transient failure worth retrying: a
# connection-level error (no response) or a 5xx. A clean 2xx/3xx/4xx is a
# definitive answer and must NOT be retried.
_gram_http_is_transient() {
  local curl_exit="$1"
  local http_code="$2"
  case "$curl_exit" in
    # 6 DNS, 7 connect, 28 timeout, 35 TLS handshake, 52 empty reply,
    # 55 send error, 56 recv error (the connection-reset class).
    6 | 7 | 28 | 35 | 52 | 55 | 56) return 0 ;;
  esac
  if [ "$curl_exit" -ne 0 ] && { [ -z "$http_code" ] || [ "$http_code" = "000" ]; }; then
    return 0
  fi
  if [ "$http_code" -ge 500 ] 2>/dev/null; then
    return 0
  fi
  return 1
}

# gram_http_post POSTs $2 to $1 with a per-attempt timeout of $3 seconds,
# retrying transient failures with backoff. Remaining args pass verbatim to
# curl. It always adds Content-Type and the reused Idempotency-Key header.
gram_http_post() {
  local _url="$1"
  local _payload="$2"
  local _max_time="$3"
  shift 3

  local _token
  _token=$(gram_new_idempotency_token)

  local attempt=1
  local response curl_exit
  GRAM_HTTP_CODE="000"
  GRAM_HTTP_BODY=""
  while [ "$attempt" -le "$GRAM_HTTP_MAX_ATTEMPTS" ]; do
    response=$(printf '%s' "$_payload" | curl -s -w "\n%{http_code}" -X POST \
      -H "Content-Type: application/json" \
      -H "Idempotency-Key: ${_token}" \
      "$@" \
      --data-binary @- \
      --max-time "$_max_time" \
      "$_url")
    curl_exit=$?

    GRAM_HTTP_CODE=$(printf '%s' "$response" | tail -1)
    GRAM_HTTP_BODY=$(printf '%s' "$response" | sed '$d')

    if ! _gram_http_is_transient "$curl_exit" "$GRAM_HTTP_CODE"; then
      # Definitive: a 2xx/3xx/4xx (success) or a non-transient curl error
      # (bad usage/URL — retrying won't help). Distinguish via the code.
      if [ "$curl_exit" -eq 0 ]; then
        return 0
      fi
      return 1
    fi

    if [ "$attempt" -lt "$GRAM_HTTP_MAX_ATTEMPTS" ]; then
      sleep "$((GRAM_HTTP_BACKOFF_BASE * attempt))"
    fi
    attempt=$((attempt + 1))
  done

  # Exhausted retries. A real HTTP status (e.g. a persistent 5xx) → report
  # success so the caller can act on the code; otherwise the server was
  # unreachable.
  if [ "$GRAM_HTTP_CODE" != "000" ] && [ -n "$GRAM_HTTP_CODE" ]; then
    return 0
  fi
  return 1
}
`)
}

func renderBreakerScript(pluginName string) []byte {
	script := `#!/usr/bin/env bash
# Filesystem-backed circuit breaker for Gram hook senders.
#
# Sourced (not executed) by generated Claude hook.sh when Observability Mode is
# off. The breaker tracks repeated Gram outages and lets the hook fail open
# while periodically probing for recovery.
#
# Usage:
#   . "$script_dir/breaker.sh"
#   gram_breaker_before || exit $?
#   command args...
#   gram_breaker_after $?
#
# Configuration:
#   GRAM_BREAKER_OPEN_EXIT_CODE: exit code returned by gram_breaker_before while
#     the circuit is open. Default: 75.
#   GRAM_BREAKER_NAME: breaker identity used to derive state and lock filenames.
#     Default: __GRAM_BREAKER_DEFAULT_NAME__.
#   GRAM_BREAKER_THRESHOLD: Gram outage failures required within the error
#     window before opening the circuit. Default: 5.
#   GRAM_BREAKER_ERROR_WINDOW: seconds over which failures are counted.
#     Default: 60.
#   GRAM_BREAKER_COOLDOWN: seconds to wait before allowing a half-open recovery
#     probe. Default: 60.
#   GRAM_BREAKER_LOCK_STALE_AFTER: maximum trusted age, in seconds, for lock and
#     half-open ownership. Default: 12 (the hook HTTP timeout plus 2 seconds).
#   GRAM_BREAKER_LOCK_WAIT: seconds to wait between lock acquisition attempts.
#     Default: 0.005.
#   GRAM_BREAKER_DIR: directory for breaker state and lock directories. Default:
#     ${CLAUDE_PLUGIN_DATA:-/tmp}/circuit-breakers.

GRAM_BREAKER_OPEN_EXIT_CODE="${GRAM_BREAKER_OPEN_EXIT_CODE:-75}"
GRAM_BREAKER_NAME="${GRAM_BREAKER_NAME:-__GRAM_BREAKER_DEFAULT_NAME__}"
GRAM_BREAKER_THRESHOLD="${GRAM_BREAKER_THRESHOLD:-5}"
GRAM_BREAKER_ERROR_WINDOW="${GRAM_BREAKER_ERROR_WINDOW:-60}"
GRAM_BREAKER_COOLDOWN="${GRAM_BREAKER_COOLDOWN:-60}"
GRAM_BREAKER_LOCK_STALE_AFTER="${GRAM_BREAKER_LOCK_STALE_AFTER:-12}"
GRAM_BREAKER_LOCK_WAIT="${GRAM_BREAKER_LOCK_WAIT:-0.005}"
GRAM_BREAKER_DIR="${GRAM_BREAKER_DIR:-${CLAUDE_PLUGIN_DATA:-/tmp}/circuit-breakers}"

_gram_breaker_is_int() {
  case "${1:-}" in
    '' | *[!0-9]*) return 1 ;;
    *) return 0 ;;
  esac
}

_gram_breaker_pid_running() {
  local pid="$1"
  _gram_breaker_is_int "$pid" || return 1
  [ "$pid" -gt 0 ] || return 1
  kill -0 "$pid" >/dev/null 2>&1
}

_gram_breaker_unlock() {
  local owner_pid=""
  if [ -r "$GRAM_BREAKER_LOCK_OWNER_FILE" ]; then
    read -r owner_pid <"$GRAM_BREAKER_LOCK_OWNER_FILE" || true
  fi
  if [ "$owner_pid" = "$$" ]; then
    rm -f "$GRAM_BREAKER_LOCK_OWNER_FILE" 2>/dev/null || true
    rmdir "$GRAM_BREAKER_LOCK_DIR" 2>/dev/null || true
  fi
}

_gram_breaker_lock() {
  local owner_pid now mtime

  while ! mkdir "$GRAM_BREAKER_LOCK_DIR" 2>/dev/null; do
    mtime="$(stat -f %m "$GRAM_BREAKER_LOCK_DIR" 2>/dev/null ||
      stat -c %Y "$GRAM_BREAKER_LOCK_DIR" 2>/dev/null)" || {
      sleep "$GRAM_BREAKER_LOCK_WAIT"
      continue
    }
    if _gram_breaker_is_int "$mtime"; then
      now="$(date +%s)"
      if [ $((now - mtime)) -ge "$GRAM_BREAKER_LOCK_STALE_AFTER" ]; then
        rm -f "$GRAM_BREAKER_LOCK_OWNER_FILE" 2>/dev/null || true
        rmdir "$GRAM_BREAKER_LOCK_DIR" 2>/dev/null || true
        sleep "$GRAM_BREAKER_LOCK_WAIT"
        continue
      fi
    fi

    owner_pid=""
    if [ -r "$GRAM_BREAKER_LOCK_OWNER_FILE" ]; then
      read -r owner_pid <"$GRAM_BREAKER_LOCK_OWNER_FILE" || true
      if _gram_breaker_pid_running "$owner_pid"; then
        sleep "$GRAM_BREAKER_LOCK_WAIT"
        continue
      fi
      if _gram_breaker_is_int "$owner_pid" && [ "$owner_pid" -gt 0 ]; then
        rm -f "$GRAM_BREAKER_LOCK_OWNER_FILE" 2>/dev/null || true
        rmdir "$GRAM_BREAKER_LOCK_DIR" 2>/dev/null || true
      fi
    fi
    sleep "$GRAM_BREAKER_LOCK_WAIT"
  done

  if ! printf '%s\n' "$$" >"$GRAM_BREAKER_LOCK_OWNER_FILE"; then
    rm -f "$GRAM_BREAKER_LOCK_OWNER_FILE" 2>/dev/null || true
    rmdir "$GRAM_BREAKER_LOCK_DIR" 2>/dev/null || true
    return 1
  fi
}

_gram_breaker_read_state() {
  local state_file_line

  GRAM_BREAKER_STATE="closed"
  GRAM_BREAKER_FAILURES=0
  GRAM_BREAKER_WINDOW_STARTED_AT=0
  GRAM_BREAKER_OPENED_AT=0
  GRAM_BREAKER_OWNER_PID=0

  if [ -r "$GRAM_BREAKER_STATE_FILE" ]; then
    read -r state_file_line <"$GRAM_BREAKER_STATE_FILE" || true
    set -- $state_file_line
    if [ "$#" -eq 4 ]; then
      # Older generated breakers wrote: state failures opened_at owner_pid.
      GRAM_BREAKER_STATE="$1"
      GRAM_BREAKER_FAILURES="$2"
      GRAM_BREAKER_WINDOW_STARTED_AT=0
      GRAM_BREAKER_OPENED_AT="$3"
      GRAM_BREAKER_OWNER_PID="$4"
    elif [ "$#" -ge 5 ]; then
      GRAM_BREAKER_STATE="$1"
      GRAM_BREAKER_FAILURES="$2"
      GRAM_BREAKER_WINDOW_STARTED_AT="$3"
      GRAM_BREAKER_OPENED_AT="$4"
      GRAM_BREAKER_OWNER_PID="$5"
    fi
  fi

  case "$GRAM_BREAKER_STATE" in
    closed | open | half_open) ;;
    *) GRAM_BREAKER_STATE="closed" ;;
  esac
  if ! _gram_breaker_is_int "$GRAM_BREAKER_FAILURES"; then
    GRAM_BREAKER_FAILURES=0
  fi
  if ! _gram_breaker_is_int "$GRAM_BREAKER_WINDOW_STARTED_AT"; then
    GRAM_BREAKER_WINDOW_STARTED_AT=0
  fi
  if ! _gram_breaker_is_int "$GRAM_BREAKER_OPENED_AT"; then
    GRAM_BREAKER_OPENED_AT=0
  fi
  if ! _gram_breaker_is_int "$GRAM_BREAKER_OWNER_PID"; then
    GRAM_BREAKER_OWNER_PID=0
  fi
}

_gram_breaker_write_state() {
  local state="$1"
  local failures="$2"
  local window_started_at="$3"
  local opened_at="$4"
  local owner_pid="${5:-0}"
  local tmp

  if ! _gram_breaker_is_int "$window_started_at"; then
    window_started_at=0
  fi
  if ! _gram_breaker_is_int "$owner_pid"; then
    owner_pid=0
  fi

  tmp="$(mktemp "${GRAM_BREAKER_DIR}/.${GRAM_BREAKER_SAFE_NAME}.XXXXXX")" || return 1
  if printf '%s %s %s %s %s\n' "$state" "$failures" "$window_started_at" "$opened_at" "$owner_pid" >"$tmp"; then
    mv "$tmp" "$GRAM_BREAKER_STATE_FILE"
    return $?
  fi
  rm -f "$tmp"
  return 1
}

_gram_breaker_prepare() {
  GRAM_BREAKER_SAFE_NAME="${GRAM_BREAKER_NAME//[!A-Za-z0-9_.-]/_}"
  if [ -z "$GRAM_BREAKER_SAFE_NAME" ]; then
    GRAM_BREAKER_SAFE_NAME="default"
  fi

  mkdir -p "$GRAM_BREAKER_DIR" 2>/dev/null || return 1
  [ -d "$GRAM_BREAKER_DIR" ] && [ -w "$GRAM_BREAKER_DIR" ] || return 1
  GRAM_BREAKER_STATE_FILE="${GRAM_BREAKER_DIR}/${GRAM_BREAKER_SAFE_NAME}.state"
  GRAM_BREAKER_LOCK_DIR="${GRAM_BREAKER_DIR}/${GRAM_BREAKER_SAFE_NAME}.lockdir"
  GRAM_BREAKER_LOCK_OWNER_FILE="${GRAM_BREAKER_LOCK_DIR}/owner.pid"
}

gram_breaker_before() {
  _gram_breaker_prepare || return 2

  local now elapsed

  GRAM_BREAKER_RUN_MODE=""
  GRAM_BREAKER_RUN_PID=""
  _gram_breaker_lock || return 1
  _gram_breaker_read_state

  case "$GRAM_BREAKER_STATE" in
    closed)
      GRAM_BREAKER_RUN_MODE="closed"
      _gram_breaker_unlock
      return 0
      ;;
    open)
      now="$(date +%s)"
      elapsed=$((now - GRAM_BREAKER_OPENED_AT))
      if [ "$elapsed" -ge "$GRAM_BREAKER_COOLDOWN" ]; then
        GRAM_BREAKER_RUN_MODE="half_open"
        GRAM_BREAKER_RUN_PID="$$"
        _gram_breaker_write_state "half_open" "$GRAM_BREAKER_FAILURES" "$GRAM_BREAKER_WINDOW_STARTED_AT" "$now" "$GRAM_BREAKER_RUN_PID" || {
          _gram_breaker_unlock
          return 1
        }
        _gram_breaker_unlock
        return 0
      fi
      _gram_breaker_unlock
      return "$GRAM_BREAKER_OPEN_EXIT_CODE"
      ;;
    half_open)
      now="$(date +%s)"
      elapsed=$((now - GRAM_BREAKER_OPENED_AT))
      if [ "$elapsed" -ge "$GRAM_BREAKER_LOCK_STALE_AFTER" ] || ! _gram_breaker_pid_running "$GRAM_BREAKER_OWNER_PID"; then
        GRAM_BREAKER_RUN_MODE="half_open"
        GRAM_BREAKER_RUN_PID="$$"
        _gram_breaker_write_state "half_open" "$GRAM_BREAKER_FAILURES" "$GRAM_BREAKER_WINDOW_STARTED_AT" "$now" "$GRAM_BREAKER_RUN_PID" || {
          _gram_breaker_unlock
          return 1
        }
        _gram_breaker_unlock
        return 0
      fi
      _gram_breaker_unlock
      return "$GRAM_BREAKER_OPEN_EXIT_CODE"
      ;;
  esac

  _gram_breaker_unlock
  return 1
}

gram_breaker_after() {
  local command_status="$1"
  local now failures

  GRAM_BREAKER_AFTER_DECISION="unchanged"
  GRAM_BREAKER_AFTER_REASON=""

  _gram_breaker_prepare || return 2
  _gram_breaker_lock || return 1
  _gram_breaker_read_state

  case "${GRAM_BREAKER_RUN_MODE:-}" in
    half_open)
      if [ "$GRAM_BREAKER_STATE" != "half_open" ] || [ "$GRAM_BREAKER_OWNER_PID" != "${GRAM_BREAKER_RUN_PID:-$$}" ]; then
        GRAM_BREAKER_AFTER_DECISION="allow"
        GRAM_BREAKER_AFTER_REASON="state_changed"
        _gram_breaker_unlock
        return 0
      fi
      if [ "$command_status" -eq 0 ]; then
        _gram_breaker_write_state "closed" 0 0 0 0 || {
          _gram_breaker_unlock
          return 1
        }
        GRAM_BREAKER_AFTER_DECISION="allow"
        GRAM_BREAKER_AFTER_REASON="success_closed"
      else
        now="$(date +%s)"
        _gram_breaker_write_state "open" "$GRAM_BREAKER_THRESHOLD" 0 "$now" 0 || {
          _gram_breaker_unlock
          return 1
        }
        GRAM_BREAKER_AFTER_DECISION="allow"
        GRAM_BREAKER_AFTER_REASON="half_open_failed"
      fi
      ;;
    closed)
      if [ "$GRAM_BREAKER_STATE" != "closed" ]; then
        case "$GRAM_BREAKER_STATE" in
          open | half_open)
            GRAM_BREAKER_AFTER_DECISION="allow"
            GRAM_BREAKER_AFTER_REASON="state_changed"
            ;;
        esac
        _gram_breaker_unlock
        return 0
      fi
      if [ "$command_status" -eq 0 ]; then
        _gram_breaker_write_state "closed" 0 0 0 0 || {
          _gram_breaker_unlock
          return 1
        }
        GRAM_BREAKER_AFTER_DECISION="allow"
        GRAM_BREAKER_AFTER_REASON="success_reset"
      else
        now="$(date +%s)"
        if [ "$GRAM_BREAKER_WINDOW_STARTED_AT" -le 0 ] ||
          [ $((now - GRAM_BREAKER_WINDOW_STARTED_AT)) -gt "$GRAM_BREAKER_ERROR_WINDOW" ]; then
          failures=1
          GRAM_BREAKER_WINDOW_STARTED_AT="$now"
        else
          failures=$((GRAM_BREAKER_FAILURES + 1))
        fi
        if [ "$failures" -ge "$GRAM_BREAKER_THRESHOLD" ]; then
          _gram_breaker_write_state "open" "$failures" "$GRAM_BREAKER_WINDOW_STARTED_AT" "$now" 0 || {
            _gram_breaker_unlock
            return 1
          }
          GRAM_BREAKER_AFTER_DECISION="allow"
          GRAM_BREAKER_AFTER_REASON="threshold_opened"
        else
          _gram_breaker_write_state "closed" "$failures" "$GRAM_BREAKER_WINDOW_STARTED_AT" "$GRAM_BREAKER_OPENED_AT" 0 || {
            _gram_breaker_unlock
            return 1
          }
          GRAM_BREAKER_AFTER_DECISION="block"
          GRAM_BREAKER_AFTER_REASON="failure_recorded"
        fi
      fi
      ;;
  esac

  _gram_breaker_unlock
  return 0
}
`
	return []byte(strings.ReplaceAll(script, "__GRAM_BREAKER_DEFAULT_NAME__", shellDoubleQuoteValue(pluginName)))
}

func renderCurlAuthConfigSnippet(cfg GenerateConfig, failureExit int) string {
	var config strings.Builder
	fmt.Fprintf(&config, "header = \"Gram-Key: %s\"\n", curlConfigQuote(cfg.HooksAPIKey))
	if cfg.ProjectSlug != "" {
		fmt.Fprintf(&config, "header = \"Gram-Project: %s\"\n", curlConfigQuote(cfg.ProjectSlug))
	}

	return fmt.Sprintf(`auth_config=""
auth_config_arg=()
cleanup_auth_config() {
  if [ -n "$auth_config" ]; then
    rm -f "$auth_config"
  fi
}
trap cleanup_auth_config EXIT
auth_config=$(mktemp "${TMPDIR:-/tmp}/gram-hooks-curl.XXXXXX") || exit %d
chmod 600 "$auth_config" || true
cat >"$auth_config" <<'GRAM_HOOKS_CURL_CONFIG'
%sGRAM_HOOKS_CURL_CONFIG
auth_config_arg=(--config "$auth_config")
`, failureExit, config.String())
}

func curlConfigQuote(value string) string {
	return strings.NewReplacer(`\`, `\\`, `"`, `\"`).Replace(value)
}

func shellDoubleQuoteValue(value string) string {
	return strings.NewReplacer(`\`, `\\`, `"`, `\"`, "$", `\$`, "`", "\\`").Replace(value)
}

func renderOptimisticClaudeHookScript(cfg GenerateConfig, keyPrefix, authConfigSnippet string) []byte {
	script := `#!/usr/bin/env bash
# Generated by Speakeasy. Do not edit - overwritten on every publish.
# Key prefix: __KEY_PREFIX__ (correlate with the dashboard's API Keys page).
#
# Send a Claude hook event to Speakeasy. When reachable, the server is the sole
# authority on whether to block:
#   HTTP 2xx -> allow (exit 0). Body forwarded to stdout.
#   HTTP 4xx -> block (exit 2). Server message relayed to stderr.
#   HTTP 000/5xx -> allow with degradation context; counted by the breaker.
# If Speakeasy is unreachable or repeatedly returns 5xx, fail open with context
# so Claude can continue while the circuit breaker probes for recovery.

set -u

server_url="${GRAM_HOOKS_SERVER_URL:-__SERVER_URL__}"

__AUTH_CONFIG_SNIPPET__
__IDENTITY_SOURCE_SNIPPET__# shellcheck source=/dev/null
. "$script_dir/breaker.sh"

payload=$(cat)
if type gram_enrich_identity_payload >/dev/null 2>&1; then
  payload=$(gram_enrich_identity_payload "$payload")
fi

hook_hostname=$(hostname 2>/dev/null || true)
hook_hostname_header=()
if [ -n "$hook_hostname" ]; then
  hook_hostname_header=(-H "X-Gram-Hook-Hostname: ${hook_hostname}")
fi

gram_json_escape() {
  local s="${1:-}"
  s=${s//\\/\\\\}
  s=${s//\"/\\\"}
  s=${s//$'\n'/\\n}
  s=${s//$'\r'/\\r}
  s=${s//$'\t'/\\t}
  printf '%s' "$s"
}

gram_emit_error_message() {
  local reason="${1:-}"
  local system_message="${2:-}"
  local escaped_message

  if [ -z "$reason" ]; then
    reason="${system_message:-Speakeasy hook returned an error}"
  fi
  if [ -z "$system_message" ]; then
    system_message="$reason"
  fi

  escaped_message="$(gram_json_escape "$system_message")"
  printf '{"systemMessage":"%s"}\n' "$escaped_message"
  printf '%s\n' "$reason" >&2
}

GRAM_HTTP_CODE="000"
GRAM_HTTP_BODY="{}"
gram_breaker_before
breaker_status=$?
if [ "$breaker_status" -eq "$GRAM_BREAKER_OPEN_EXIT_CODE" ]; then
  gram_emit_error_message \
    "Gram hook degraded" \
    "Gram hook degraded. Gram is temporarily unavailable, so this hook is allowing the request without policy evaluation. Continue normally."
  exit 0
fi
if [ "$breaker_status" -ne 0 ]; then
  gram_emit_error_message \
    "Gram hook degraded" \
    "Gram hook degraded. Gram is temporarily unavailable, so this hook is allowing the request without policy evaluation. Continue normally."
  exit 0
fi

# gram_http_post (http.sh) retries transient resets so a single reset no
# longer blocks the tool call; the server still decides allow/block.
gram_http_post "${server_url}/rpc/hooks.claude" "$payload" 10 \
  ${auth_config_arg[@]+"${auth_config_arg[@]}"} \
  ${hook_hostname_header[@]+"${hook_hostname_header[@]}"}

http_code="$GRAM_HTTP_CODE"
body="$GRAM_HTTP_BODY"

# curl returns 000 on connection failure. Treat 000/5xx as a Gram outage:
# allow the request and let the circuit breaker decide when to probe again.
if [ "$http_code" = "000" ] || [ "$http_code" -ge 500 ] 2>/dev/null; then
  if ! gram_breaker_after 1; then
    gram_emit_error_message \
      "Speakeasy hook returned HTTP ${http_code}" \
      "Speakeasy hook returned HTTP ${http_code}"
    exit 2
  fi
  case "$GRAM_BREAKER_AFTER_DECISION" in
    allow)
      gram_emit_error_message \
        "Gram hook degraded" \
        "Gram hook degraded. Gram is temporarily unavailable, so this hook is allowing the request without policy evaluation. Continue normally."
      exit 0
      ;;
  esac
  gram_emit_error_message \
    "Speakeasy hook returned HTTP ${http_code}" \
    "Speakeasy hook returned HTTP ${http_code}"
  exit 2
fi

gram_breaker_after 0 || true

if [ "$http_code" -ge 200 ] 2>/dev/null && [ "$http_code" -lt 400 ] 2>/dev/null; then
  echo "$body"
  exit 0
fi

gram_emit_error_message \
  "Speakeasy hook returned HTTP ${http_code}" \
  "Speakeasy hook returned HTTP ${http_code}"
exit 2
`
	replacements := map[string]string{
		"__KEY_PREFIX__":              keyPrefix,
		"__SERVER_URL__":              cfg.ServerURL,
		"__AUTH_CONFIG_SNIPPET__":     authConfigSnippet,
		"__IDENTITY_SOURCE_SNIPPET__": renderIdentitySourceSnippet(),
	}
	for old, new := range replacements {
		script = strings.ReplaceAll(script, old, new)
	}
	return []byte(script)
}

// renderHookScript produces the bash wrapper that forwards hook event JSON
// from stdin to the appropriate Gram endpoint. Generated observability plugins
// bake Gram-Key + Gram-Project into a protected curl config file; the
// checked-in hooks/plugin-* scripts used for local development read equivalent
// values from environment. Both paths send the same headers:
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

	authConfigSnippet := renderCurlAuthConfigSnippet(cfg, 2)

	// %%{http_code} → %{http_code} in the emitted script (curl write-out format).
	// %%s           → %s           in the emitted script (printf format).
	//
	// Claude reads hookSpecificOutput.permissionDecision from stdout on 2xx,
	// so the body is echoed unconditionally for the claude platform.
	// Codex treats any stdout as a structured response and rejects unknown JSON,
	// so for codex we suppress stdout on 2xx (empty stdout = allow).
	// Both platforms treat exit 2 as a block; the reason goes to stderr.
	if platform == "claude" && !cfg.ObservabilityMode {
		return renderOptimisticClaudeHookScript(cfg, keyPrefix, authConfigSnippet)
	}

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
%s
payload=$(cat)

%s
if type gram_enrich_identity_payload >/dev/null 2>&1; then
  payload=$(gram_enrich_identity_payload "$payload")
fi

if command -v python3 >/dev/null 2>&1; then
	payload=$(printf '%%s' "$payload" | python3 -c '
import base64
import json
import os
import re
import shutil
import subprocess
import sys
import urllib.parse

payload = sys.stdin.read()
try:
    data = json.loads(payload)
except Exception:
    print(payload, end="")
    raise SystemExit

if data.get("hook_event_name") == "SessionStart" and not data.get("user_email"):
    email = ""
    codex_home = os.environ.get("CODEX_HOME") or os.path.join(os.path.expanduser("~"), ".codex")
    auth_path = os.path.join(codex_home, "auth.json")
    try:
        with open(auth_path, encoding="utf-8") as f:
            token = (json.load(f).get("tokens") or {}).get("id_token") or ""
        parts = token.split(".")
        if len(parts) >= 2:
            body = parts[1] + "=" * (-len(parts[1]) %% 4)
            claims = json.loads(base64.urlsafe_b64decode(body.encode("ascii")))
            email = str(claims.get("email") or "").strip()
    except Exception:
        email = ""

    if email:
        data["user_email"] = email

# SessionStart only: collect the configured MCP server inventory so Gram can
# apply shadow-MCP policy and visibility to servers it is not proxying. The
# gate is a real JSON field check, so the blocking PreToolUse path never pays
# for a codex invocation; SessionStart itself is routed through hook_async.sh
# so the latency is invisible to Codex. The timeout caps wall time in case
# the codex CLI misbehaves.
#
# Only the fields Gram consumes are shipped — the raw transport object also
# carries env vars and HTTP headers, which often contain credentials and
# must never leave the machine. Stdio launch args are kept for server
# identity (e.g. npx package names) but credential-shaped values are
# redacted: values following a secret-named flag (including the short -H
# header alias), inline flag=value pairs, header-shaped values whose name
# suggests credentials, and well-known token prefixes.
secret_flag = re.compile(r"(key|token|secret|password|passwd|auth|credential|header)", re.I)
secret_value = re.compile(r"^(sk-|pk-|ghp_|gho_|github_pat_|xox[a-z]-|ya29\.|AKIA|eyJ)")
secret_header = re.compile(r"^(authorization|proxy-authorization|cookie|[a-z0-9_-]*(key|token|secret|auth)[a-z0-9_-]*)\s*:", re.I)

def redact_args(args):
    if not isinstance(args, list):
        return None
    out = []
    redact_next = False
    for a in args:
        if not isinstance(a, str):
            continue
        if redact_next:
            out.append("[REDACTED]")
            redact_next = False
        elif "=" in a and secret_flag.search(a.split("=", 1)[0]):
            out.append(a.split("=", 1)[0] + "=[REDACTED]")
        elif a == "-H":
            out.append(a)
            redact_next = True
        elif a.startswith("-H") and secret_header.match(a[2:]):
            out.append("-H" + a[2:].split(":", 1)[0] + ": [REDACTED]")
        elif a.startswith("-") and secret_flag.search(a):
            out.append(a)
            redact_next = True
        elif secret_header.match(a):
            out.append(a.split(":", 1)[0] + ": [REDACTED]")
        elif secret_value.match(a):
            out.append("[REDACTED]")
        else:
            out.append(a)
    return out

# URLs are their own credential channel: strip userinfo and the fragment
# (OAuth-style #access_token=... never identifies a server) and redact
# secret-named query parameters while preserving scheme/host/path, which
# the server needs for provenance checks.
def redact_url(url):
    if not isinstance(url, str) or not url:
        return url
    try:
        parts = urllib.parse.urlsplit(url)
        netloc = parts.netloc.rsplit("@", 1)[-1]
        pairs = []
        for k, v in urllib.parse.parse_qsl(parts.query, keep_blank_values=True):
            if secret_flag.search(k):
                v = "[REDACTED]"
            pairs.append((k, v))
        query = urllib.parse.urlencode(pairs)
        return urllib.parse.urlunsplit((parts.scheme, netloc, parts.path, query, ""))
    except Exception:
        return url

# PATH lookup alone misses real installs: hooks fired by the Codex desktop
# app inherit the minimal GUI environment, and desktop-only users never have
# codex on PATH at all — the app references its bundled binary by absolute
# path. Probe the managed-install and app-bundle locations directly; paths
# absent on this platform simply fail the probe. NB: this comment is inside
# a bash single-quoted heredoc — apostrophes here break the script.
def find_codex():
    found = shutil.which("codex")
    if found:
        return found
    home = os.path.expanduser("~")
    codex_home = os.environ.get("CODEX_HOME") or os.path.join(home, ".codex")
    candidates = [
        os.path.join(codex_home, "packages", "standalone", "current", "bin", "codex"),
        os.path.join(home, ".local", "bin", "codex"),
        "/usr/local/bin/codex",
        "/Applications/Codex.app/Contents/Resources/codex",
    ]
    for candidate in candidates:
        if os.path.isfile(candidate) and os.access(candidate, os.X_OK):
            return candidate
    return None

codex_bin = find_codex() if data.get("hook_event_name") == "SessionStart" else None
if codex_bin:
    try:
        out = subprocess.run(
            [codex_bin, "mcp", "list", "--json"],
            stdin=subprocess.DEVNULL,
            capture_output=True,
            timeout=15,
        ).stdout
        inventory = json.loads(out)
    except Exception:
        inventory = None
    if isinstance(inventory, list):
        slim = []
        for item in inventory:
            if not isinstance(item, dict):
                continue
            transport = item.get("transport")
            if not isinstance(transport, dict):
                transport = {}
            slim.append({
                "name": item.get("name"),
                "enabled": item.get("enabled"),
                "auth_status": item.get("auth_status"),
                "transport": {
                    "type": transport.get("type"),
                    "url": redact_url(transport.get("url")),
                    "command": transport.get("command"),
                    "args": redact_args(transport.get("args")),
                },
            })
        additional = data.get("additional_data")
        if not isinstance(additional, dict):
            additional = {}
        additional["mcp_inventory_codex"] = slim
        data["additional_data"] = additional

print(json.dumps(data, separators=(",", ":")), end="")
' 2>/dev/null) || true
fi

hook_hostname=$(hostname 2>/dev/null || true)
hook_hostname_header=()
if [ -n "$hook_hostname" ]; then
  hook_hostname_header=(-H "X-Gram-Hook-Hostname: ${hook_hostname}")
fi

# gram_http_post (http.sh) retries transient resets so a single reset no
# longer blocks the tool call; the server still decides allow/block.
gram_http_post "${server_url}/rpc/hooks.codex" "$payload" 10 \
  ${auth_config_arg[@]+"${auth_config_arg[@]}"} \
  ${hook_hostname_header[@]+"${hook_hostname_header[@]}"}

http_code="$GRAM_HTTP_CODE"
body="$GRAM_HTTP_BODY"

# curl returns 000 on connection failure — treat as block so an unreachable
# Speakeasy server cannot silently bypass blocking policies.
if [ "$http_code" -ge 200 ] 2>/dev/null && [ "$http_code" -lt 400 ] 2>/dev/null; then
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
`, keyPrefix, cfg.ServerURL, authConfigSnippet, renderIdentitySourceSnippet())
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

%s
%s
payload=$(cat)
if type gram_enrich_identity_payload >/dev/null 2>&1; then
  payload=$(gram_enrich_identity_payload "$payload")
fi

hook_hostname=$(hostname 2>/dev/null || true)
hook_hostname_header=()
if [ -n "$hook_hostname" ]; then
  hook_hostname_header=(-H "X-Gram-Hook-Hostname: ${hook_hostname}")
fi

# gram_http_post (http.sh) retries transient resets so a single reset no
# longer blocks the tool call; the server still decides allow/block.
gram_http_post "${server_url}/rpc/hooks.%s" "$payload" 10 \
  ${auth_config_arg[@]+"${auth_config_arg[@]}"} \
  ${hook_hostname_header[@]+"${hook_hostname_header[@]}"}

http_code="$GRAM_HTTP_CODE"
body="$GRAM_HTTP_BODY"

echo "$body"

# curl returns 000 on connection failure — treat as block so an unreachable
# Speakeasy server cannot silently bypass blocking policies.
if [ "$http_code" -ge 200 ] 2>/dev/null && [ "$http_code" -lt 400 ] 2>/dev/null; then
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
`, keyPrefix, cfg.ServerURL, authConfigSnippet, renderIdentitySourceSnippet(), platform)
}

func renderCodexAsyncHookScript() []byte {
	return []byte(`#!/usr/bin/env bash
# Generated by Gram. Do not edit - overwritten on every publish.
#
# Codex does not support hooks.json async=true yet. This wrapper keeps
# telemetry-only events off the critical path by copying stdin before the
# parent hook exits, then forwarding the payload in a background process.

set -u

tmp="$(mktemp "${TMPDIR:-/tmp}/gram-codex-hook.XXXXXX")" || exit 0
if ! cat > "$tmp"; then
  rm -f "$tmp"
  exit 0
fi

script_dir="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"

(
  bash "$script_dir/hook.sh" < "$tmp" >/dev/null 2>&1
  rm -f "$tmp"
) >/dev/null 2>&1 &

exit 0
`)
}

// renderClaudeMCPInventoryScript produces the Claude hook script registered
// against SessionStart and ConfigChange. It enriches the payload with an MCP
// server inventory before forwarding to Gram, picking the source by what the
// sandbox can see:
//
//   - cowork: when cmux's per-run config file (local_<rid>.json) is reachable
//     via CLAUDE_PROJECT_DIR/.., we ship its remoteMcpServersConfig array
//     verbatim as `additional_data.mcp_inventory_cowork`. This is the only
//     host-side spot where the connector UUID is paired with the MCP URL.
//   - Claude Code (default): shell out to `claude mcp list` and ship its raw
//     output as `additional_data.mcp_inventory_claude_code`.
//
// The script is fire-and-forget: neither SessionStart nor ConfigChange has
// an allow/deny decision to honor, so we always exit 0 and discard the
// response body to keep latency invisible to Claude. Both events run async
// so Claude is never held up while the inventory is gathered.
//
// Auth headers match renderHookScript so server-side attribution works:
// Gram-Key always, Gram-Project when ProjectSlug is set.
func renderClaudeMCPInventoryScript(cfg GenerateConfig) []byte {
	keyPrefix := cfg.HooksAPIKey
	if len(keyPrefix) > 12 {
		keyPrefix = keyPrefix[:12]
	}

	authConfigSnippet := renderCurlAuthConfigSnippet(cfg, 0)

	return fmt.Appendf(nil, `#!/usr/bin/env bash
# Generated by Speakeasy. Do not edit — overwritten on every publish.
# Key prefix: %s (correlate with the dashboard's API Keys page).
#
# MCP inventory hook: enriches the payload with the active MCP server list
# and forwards it to Gram. Registered against both SessionStart and
# ConfigChange so the server re-syncs its cached inventory whenever Claude
# (re)loads the session or a settings file changes mid-session. Neither
# event has an allow/deny decision to honor, so we always exit 0 and
# fire-and-forget.
#
# Two execution environments are supported:
#   - cowork: detected by the presence of cmux's per-run local_<rid>.json
#     config file. We extract its remoteMcpServersConfig (connector UUID +
#     URL pairs) and ship them as mcp_inventory_cowork.
#   - Claude Code (default): shell out to `+"`claude mcp list`"+` and forward
#     the human-readable output as mcp_inventory_claude_code.

set -u

server_url="${GRAM_HOOKS_SERVER_URL:-%s}"

%s
hook_hostname=$(hostname 2>/dev/null || true)
hook_hostname_header=()
if [ -n "$hook_hostname" ]; then
  hook_hostname_header=(-H "X-Gram-Hook-Hostname: ${hook_hostname}")
fi

payload=$(cat)
%s
if type gram_enrich_identity_payload >/dev/null 2>&1; then
  payload=$(gram_enrich_identity_payload "$payload")
fi

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
  # cmux's field naming has drifted across versions (snake_case vs
  # camelCase, `+"`uuid`"+` vs `+"`id`"+` for the connector identifier) so we try
  # multiple candidates per slot and keep the first non-null. This is
  # the field that becomes the `+"`mcp__<server>__tool`"+` prefix server-side,
  # so getting it wrong silently shows users a UUID instead of "Slack".
  inv=$(jq -c '
    [
      (.remoteMcpServersConfig // [])[]
      | {
          connector_uuid: (.uuid // .connectorUuid // .connector_uuid // .id // .connectorId // .connector_id // null),
          name:           (.name // .displayName // .display_name // null),
          url:            (.url // .serverUrl // .server_url // null),
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

# Fire-and-forget through the shared helper (http.sh) so a transient reset
# retries instead of dropping the inventory. The result is ignored.
gram_http_post "${server_url}/rpc/hooks.claude" "$enriched" 30 \
  ${auth_config_arg[@]+"${auth_config_arg[@]}"} \
  ${hook_hostname_header[@]+"${hook_hostname_header[@]}"} >/dev/null 2>&1 || true

exit 0
`, keyPrefix, cfg.ServerURL, authConfigSnippet, renderIdentitySourceSnippet())
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
func computeCodexHookHash(event, marketplace, plugin string) (string, error) {
	eventSnake := codexEventSnakeCase(event)
	command := codexHookCommandString(marketplace, plugin, codexHookScriptName(event))
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
		hash, err := computeCodexHookHash(event, marketplace, plugin)
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

func codexHookScriptName(event string) string {
	switch event {
	case "SessionStart", "PostToolUse", "Stop":
		return "hook_async.sh"
	default:
		return "hook.sh"
	}
}

func codexHookCommandString(marketplace, plugin, script string) string {
	return fmt.Sprintf(`bash "$HOME/.codex/.tmp/marketplaces/%s/%s/hooks/%s"`, marketplace, plugin, script)
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

	// Desktop-only and MDM-deployed machines run without codex on PATH; probe
	// the managed-install and app-bundle locations. Candidate list mirrors
	// find_codex in the generated hook script.
	b.WriteString(`find_codex() {
  if command -v codex >/dev/null 2>&1; then
    command -v codex
    return 0
  fi
  local codex_home="${CODEX_HOME:-$HOME/.codex}"
  local candidate
  for candidate in \
    "${codex_home}/packages/standalone/current/bin/codex" \
    "${HOME}/.local/bin/codex" \
    /usr/local/bin/codex \
    "/Applications/Codex.app/Contents/Resources/codex"; do
    if [ -f "${candidate}" ] && [ -x "${candidate}" ]; then
      printf '%s\n' "${candidate}"
      return 0
    fi
  done
  return 1
}
CODEX_BIN="$(find_codex || true)"

`)

	// Step 1: marketplace registration differs for remote vs local ZIP installs.
	if marketplaceURL != "" {
		fmt.Fprintf(&b, "MARKETPLACE_URL=%q\n\n", marketplaceURL)
		b.WriteString(`# ── 1. Register & sync marketplace ──────────────────────────────────────────
echo "→ Registering Speakeasy marketplace..."
if [ -n "${CODEX_BIN}" ]; then
  # add is idempotent (no-ops if already registered); upgrade pulls any new commits.
  "${CODEX_BIN}" plugin marketplace add "${MARKETPLACE_URL}" || true
  "${CODEX_BIN}" plugin marketplace upgrade "${MARKETPLACE_KEY}"
else
  echo "  ⚠  'codex' executable not found."
  echo "     Run manually: codex plugin marketplace add '${MARKETPLACE_URL}'"
fi

`)
	} else {
		b.WriteString(`# ── 1. Register marketplace (local ZIP install) ──────────────────────────────
SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
echo "→ Registering Speakeasy marketplace from ${SCRIPT_DIR}..."
if [ -n "${CODEX_BIN}" ]; then
  "${CODEX_BIN}" plugin marketplace add "${SCRIPT_DIR}"
else
  echo "  ⚠  'codex' executable not found."
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

# A root-level dotted key implicitly defines its parent table and conflicts
# with an explicit [table] header elsewhere in the file. Only the region
# before the first table header is touched.
def strip_root_dotted_key(text, key):
    m = re.search(r'(?m)^\[', text)
    root, rest = (text[:m.start()], text[m.start():]) if m else (text, "")
    root = re.sub(r'(?m)^' + re.escape(key) + r'\s*=.*\n?', '', root)
    return root + rest

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

	b.WriteString(`content = strip_root_dotted_key(content, "features.hooks")
content = strip_root_dotted_key(content, "features.plugin_hooks")
content = ensure_table_entry(content, "[features]", "hooks", "true")
content = ensure_table_entry(content, "[features]", "plugin_hooks", "true")

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
    else:
        # The hook command (and therefore its trusted_hash) changes between
        # plugin versions. Refresh the hash on this Gram-managed entry so an
        # upgraded install does not get flagged as modified/untrusted.
        content = re.sub(
            re.escape(section) + r'([^\[]*?trusted_hash\s*=\s*")[^"]*(")',
            lambda m: section + m.group(1) + trusted_hash + m.group(2),
            content,
            count=1,
        )

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
		if !s.IsPublic && !s.IsOAuth && cfg.APIKey == "" {
			needsGramKeyPrompt = true
		}
		// Public non-OAuth servers may need user-provided env vars.
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
		var headers map[string]string

		if s.IsOAuth {
			// OAuth servers handle identity at the HTTP layer — no Authorization header needed.
		} else if s.IsPublic {
			headers = make(map[string]string)
			for _, ec := range s.EnvConfigs {
				headers[ec.DisplayName] = "${user_config." + ec.VariableName + "}"
			}
		} else if cfg.APIKey != "" {
			headers = map[string]string{"Authorization": "Bearer " + cfg.APIKey}
		} else {
			headers = map[string]string{"Authorization": "Bearer ${user_config.GRAM_API_KEY}"}
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
		Author:      cursorAuthor{Name: cfg.OrgName, Email: ""},
		Homepage:    "https://getgram.ai",
	})
	if err != nil {
		return fmt.Errorf("marshal plugin.json: %w", err)
	}
	files[path.Join(subdir, ".cursor-plugin/plugin.json")] = pluginJSON

	mcpServers := make(map[string]cursorMCPServer)
	for _, s := range p.Servers {
		var headers map[string]string

		if s.IsOAuth {
			// OAuth servers handle identity at the HTTP layer — no Authorization header needed.
		} else if s.IsPublic {
			headers = make(map[string]string)
			for _, ec := range s.EnvConfigs {
				headers[ec.DisplayName] = "${env:" + ec.VariableName + "}"
			}
		} else if cfg.APIKey != "" {
			headers = map[string]string{"Authorization": "Bearer " + cfg.APIKey}
		} else {
			headers = map[string]string{"Authorization": "Bearer ${env:GRAM_API_KEY}"}
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
	Name     string               `json:"name"`
	Owner    marketplaceOwner     `json:"owner"`
	Metadata *marketplaceMetadata `json:"metadata,omitempty"`
	Plugins  []marketplaceEntry   `json:"plugins"`
}

// marketplaceMetadata carries optional marketplace-level settings. Cursor uses
// pluginRoot to declare the subdirectory that contains all plugins, letting
// plugin sources be referenced by bare name relative to that root.
type marketplaceMetadata struct {
	PluginRoot string `json:"pluginRoot,omitempty"`
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
	Author      cursorAuthor `json:"author"`
	Homepage    string       `json:"homepage"`
}

// cursorAuthor matches Cursor's documented plugin author schema (name + optional
// email). Cursor does not recognize a url sub-field — the author URL belongs in
// the top-level homepage/repository fields instead.
type cursorAuthor struct {
	Name  string `json:"name"`
	Email string `json:"email,omitempty"`
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
