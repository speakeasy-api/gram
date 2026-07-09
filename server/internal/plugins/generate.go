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
	// OrgID pins the browser login flow to the generating organization: the
	// dashboard refuses to mint a hooks key when the active session org
	// differs, so a plugin from org A cannot cache a key for org B.
	OrgID string
	// Base server URL (e.g. https://app.getgram.ai).
	ServerURL string
	// APIKey is the plaintext consumer-scoped Gram API key to inject into
	// MCP server configs. If empty, configs will use placeholder variables.
	APIKey string
	// HooksAPIKey controls whether the observability plugin is emitted. Runtime
	// hook senders authenticate with explicit env credentials or a local cached
	// hooks key; they do not embed this publish-time key.
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

// DogfoodPluginFiles renders the dogfood plugins (plugin-claude,
// plugin-cursor) from the same generators that publish customer plugins, so
// dogfood senders exercise the exact scripts -- canonical payloads,
// /rpc/hooks.ingest, ratchet and org-key recovery -- customers run. The
// configuration is fully environment-driven: no org, no baked key, no
// project; every value resolves from GRAM_HOOKS_* variables at run time.
// The rendered manifests carry org-derived names that make no sense without
// an org, so manifests are dropped from the returned map; consumers that
// need one (cmd/export-hook-plugin for `claude --plugin-dir`) supply their
// own. Nothing is checked in: tests and the local-dev task render into temp
// directories on demand.
func DogfoodPluginFiles() (map[string][]byte, error) {
	cfg := GenerateConfig{
		OrgName:           "",
		OrgEmail:          "",
		OrgID:             "",
		ServerURL:         "https://app.getgram.ai",
		APIKey:            "",
		HooksAPIKey:       "",
		ProjectSlug:       "",
		IsDefaultProject:  true,
		Version:           "",
		MarketplaceName:   "",
		ObservabilityMode: false,
	}
	files := make(map[string][]byte)
	if err := generateClaudeObservabilityPluginInDir(files, "plugin-claude", cfg); err != nil {
		return nil, fmt.Errorf("generate dogfood claude plugin: %w", err)
	}
	if err := generateCursorObservabilityPluginInDir(files, "plugin-cursor", "plugin-cursor", cfg); err != nil {
		return nil, fmt.Errorf("generate dogfood cursor plugin: %w", err)
	}
	for p := range files {
		if strings.Contains(p, ".claude-plugin/") || strings.Contains(p, ".cursor-plugin/") {
			delete(files, p)
		}
	}
	return files, nil
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
// fingerprint pass can't observe.
const pluginGeneratorVersion = "9"

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
			DisplayName: cfg.OrgName + " Observability",
			Source:      "./" + claudeObservability,
			Description: "Required: Speakeasy observability hooks for " + cfg.OrgName + ".",
		})
		cursorObservability := CursorObservabilitySlug(cfg)
		cursorPlugins = append(cursorPlugins, marketplaceEntry{
			Name:        cursorObservability,
			DisplayName: "", // Cursor carries the display name in its own plugin.json.
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
			DisplayName: p.Name,
			Source:      "./" + p.Slug,
			Description: p.Description,
		})
		cursorPlugins = append(cursorPlugins, marketplaceEntry{
			Name:        p.Slug + "-cursor",
			DisplayName: "", // Cursor carries the display name in its own plugin.json.
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

	// Assign every server its Codex key before building entries. Already-valid
	// display names reserve their exact key first, so an invalid name that
	// sanitizes into the same key can never steal it — "Team Slack" must not
	// displace a server literally named "Team_Slack".
	keys := make([]string, len(p.Servers))
	taken := make(map[string]bool, len(p.Servers))
	for i, s := range p.Servers {
		if name := codexMCPServerName(s.DisplayName); name == s.DisplayName && !taken[name] {
			keys[i] = name
			taken[name] = true
		}
	}
	for i, s := range p.Servers {
		if keys[i] != "" {
			continue
		}
		base := codexMCPServerName(s.DisplayName)
		key := base
		for n := 2; taken[key] && n <= maxCodexServerRenameSuffix; n++ {
			key = fmt.Sprintf("%s_%d", base, n)
		}
		if taken[key] {
			// Rename attempts exhausted; drop the server rather than
			// overwrite another entry.
			continue
		}
		keys[i] = key
		taken[key] = true
	}

	mcpServers := make(map[string]codexMCPServer, len(p.Servers))
	for i, s := range p.Servers {
		if keys[i] == "" {
			continue
		}

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

		mcpServers[keys[i]] = entry
	}
	mcpJSON, err := marshalJSON(codexMCPConfig{MCPServers: mcpServers})
	if err != nil {
		return fmt.Errorf("marshal .mcp.json: %w", err)
	}
	files[path.Join(subdir, ".mcp.json")] = mcpJSON

	return nil
}

// maxCodexServerRenameSuffix bounds the collision-rename attempts for
// sanitized Codex server keys: a colliding name tries _2 through _6 before
// the server is dropped from the config.
const maxCodexServerRenameSuffix = 6

// codexMCPServerName converts a human display name (e.g. "Team Slack") into a
// Codex-safe MCP server key. Codex validates the keys of .mcp.json against
// ^[a-zA-Z0-9_-]+$ at MCP client startup and refuses to start clients whose
// names contain spaces, parentheses, or other punctuation. Each run of
// disallowed characters collapses into a single underscore; leading and
// trailing runs are dropped.
func codexMCPServerName(displayName string) string {
	var b strings.Builder
	b.Grow(len(displayName))
	pendingSep := false
	for _, r := range displayName {
		valid := (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9') || r == '_' || r == '-'
		if !valid {
			pendingSep = true
			continue
		}
		if pendingSep && b.Len() > 0 {
			b.WriteByte('_')
		}
		pendingSep = false
		b.WriteRune(r)
	}
	if b.Len() == 0 {
		return "mcp-server"
	}
	return b.String()
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
		DisplayName: cfg.OrgName + " Observability",
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
	// SessionStart first runs a blocking auth preflight so fresh installs fail
	// closed until explicit or cached hook credentials exist. The unified hook
	// path sends all provider events through hook.sh; provider-specific MCP
	// enrichment stays local to the hook sender and is not persisted as a
	// server-side inventory cache.
	hookEvents := make(map[string][]claudeHookMatcher, len(ClaudeObservabilityHookEvents))
	for _, event := range ClaudeObservabilityHookEvents {
		hooks := []claudeHookCommand{{Type: "command", Command: `bash "$CLAUDE_PLUGIN_ROOT/hooks/hook.sh"`, Async: claudeHookAsyncFlag(event, cfg.ObservabilityMode), Timeout: nil}}
		if event == "SessionStart" {
			f := false
			// Claude's default 60s hook timeout is too short for the
			// interactive browser login the preflight can run; the login's
			// 240s inner wait must finish before the hook is killed.
			preflightTimeout := 300
			hooks = append([]claudeHookCommand{{Type: "command", Command: `bash "$CLAUDE_PLUGIN_ROOT/hooks/auth_preflight.sh"`, Async: &f, Timeout: &preflightTimeout}}, hooks...)
		}
		hookEvents[event] = []claudeHookMatcher{
			{Matcher: "", Hooks: hooks},
		}
	}
	hooksJSON, err := marshalJSON(claudeHooksConfig{Hooks: hookEvents})
	if err != nil {
		return fmt.Errorf("marshal hooks.json: %w", err)
	}
	files[path.Join(subdir, "hooks/hooks.json")] = hooksJSON

	files[path.Join(subdir, "hooks/identity.sh")] = renderDeviceAgentIdentityScript()
	files[path.Join(subdir, "hooks/http.sh")] = renderSharedHTTPScript()
	files[path.Join(subdir, "hooks/auth.sh")] = renderSharedAuthScript()
	files[path.Join(subdir, "hooks/login.sh")] = renderLoginScript(cfg)
	files[path.Join(subdir, "hooks/auth_preflight.sh")] = renderAuthPreflightScript(cfg)
	files[path.Join(subdir, "hooks/hook.sh")] = renderHookScript(cfg, "claude")

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
	authPreflightTimeout := 330
	// Cursor fails hooks open by default: a crashed or timed-out hook allows
	// the action unless the entry opts into failClosed. Decision-capable
	// events must not silently allow when the sender exits 2 (established
	// machine with broken auth, unreachable server). The never-authenticated
	// ratchet is unaffected — that path exits 0 with a pass-through body.
	// ObservabilityMode is documented as fully non-blocking, so no entry
	// (preflight included) may fail closed there.
	var hookFailClosed *bool
	if !cfg.ObservabilityMode {
		enforced := true
		hookFailClosed = &enforced
	}
	hookEvents := make(map[string][]cursorHookCommand, len(CursorObservabilityHookEvents)+1)
	hookEvents["sessionStart"] = []cursorHookCommand{
		{
			Command:    `bash "$CURSOR_PLUGIN_ROOT/hooks/auth_preflight.sh"`,
			Matcher:    "",
			Timeout:    &authPreflightTimeout,
			FailClosed: hookFailClosed,
		},
		{
			Command:    `bash "$CURSOR_PLUGIN_ROOT/hooks/hook.sh"`,
			Matcher:    "",
			Timeout:    nil,
			FailClosed: nil,
		},
	}
	cursorBlockingEvents := map[string]bool{
		"beforeSubmitPrompt": true,
		"preToolUse":         true,
		"beforeMCPExecution": true,
	}
	for _, event := range CursorObservabilityHookEvents {
		var failClosed *bool
		if cursorBlockingEvents[event] {
			failClosed = hookFailClosed
		}
		hookEvents[event] = []cursorHookCommand{{
			Command:    `bash "$CURSOR_PLUGIN_ROOT/hooks/hook.sh"`,
			Matcher:    "",
			Timeout:    nil,
			FailClosed: failClosed,
		}}
	}
	hooksJSON, err := marshalJSON(cursorHooksConfig{Version: 1, Hooks: hookEvents})
	if err != nil {
		return fmt.Errorf("marshal hooks.json: %w", err)
	}
	files[path.Join(subdir, "hooks/hooks.json")] = hooksJSON

	files[path.Join(subdir, "hooks/identity.sh")] = renderDeviceAgentIdentityScript()
	files[path.Join(subdir, "hooks/http.sh")] = renderSharedHTTPScript()
	files[path.Join(subdir, "hooks/auth.sh")] = renderSharedAuthScript()
	files[path.Join(subdir, "hooks/login.sh")] = renderLoginScript(cfg)
	files[path.Join(subdir, "hooks/auth_preflight.sh")] = renderAuthPreflightScript(cfg)
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
		hooks := []codexHookCommand{{Type: "command", Command: codexHookCommandString(marketplace, plugin, codexHookScriptName(event))}}
		if event == "SessionStart" {
			hooks = append([]codexHookCommand{{Type: "command", Command: codexHookCommandString(marketplace, plugin, "auth_preflight.sh")}}, hooks...)
		}
		hookEvents[event] = []codexMatcherGroup{{
			Matcher: "",
			Hooks:   hooks,
		}}
	}
	hooksJSON, err := marshalJSON(codexHooksConfig{Hooks: hookEvents})
	if err != nil {
		return fmt.Errorf("marshal hooks.json: %w", err)
	}
	files[path.Join(subdir, "hooks/hooks.json")] = hooksJSON

	files[path.Join(subdir, "hooks/identity.sh")] = renderDeviceAgentIdentityScript()
	files[path.Join(subdir, "hooks/http.sh")] = renderSharedHTTPScript()
	files[path.Join(subdir, "hooks/auth.sh")] = renderSharedAuthScript()
	files[path.Join(subdir, "hooks/login.sh")] = renderLoginScript(cfg)
	files[path.Join(subdir, "hooks/auth_preflight.sh")] = renderAuthPreflightScript(cfg)
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
#
# Resolve the local user's email via a device agent and stamp it onto the hook
# payload as user_email. Best-effort by design: every failure path leaves the
# payload unchanged so a hook is never blocked on identity resolution. Set
# GRAM_HOOKS_DEBUG=1 to surface why attribution was skipped — "my hooks show no
# user_email" is a common support question and is otherwise invisible here.
gram_hooks_identity_debug() {
  if [ -n "${GRAM_HOOKS_DEBUG:-}" ]; then
    printf 'gram-hooks(identity): %s\n' "$1" >&2
  fi
}

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
      [ -n "$command" ] && gram_hooks_identity_debug "device agent '$command' not found on PATH; trying next"
      IFS=,
      continue
    fi

    tmp="$(mktemp "${TMPDIR:-/tmp}/gram-device-agent-identity.XXXXXX")" || {
      gram_hooks_identity_debug "mktemp failed while running device agent '$command'; trying next"
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
      gram_hooks_identity_debug "device agent '$command identity' timed out after ${timeout_tenths}00ms; killed and trying next"
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
      gram_hooks_identity_debug "resolved user_email via '$command identity'"
      break
    fi
    gram_hooks_identity_debug "device agent '$command identity' returned no parseable email; trying next"
    IFS=,
  done
  IFS="$old_ifs"

  if [ -z "$email" ]; then
    gram_hooks_identity_debug "no user_email resolved from device agent(s) [$commands]; sending payload without attribution (server may fall back to OTEL session metadata)"
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
      gram_hooks_identity_debug "payload is not a JSON object; cannot stamp user_email, sending unchanged"
      printf '%s' "$payload"
      ;;
  esac
}
`)
}

func renderHookRuntimeSourceSnippet() string {
	return `script_dir="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
# shellcheck source=/dev/null
. "$script_dir/http.sh"
# shellcheck source=/dev/null
. "$script_dir/auth.sh"
`
}

// orgHintAssignment renders the gram_hooks_org_hint line for generated
// scripts. A baked org id is authoritative: the pin exists so cached
// credentials minted for another organization are never trusted, and an
// ambient GRAM_HOOKS_ORG_ID must not repoint a published plugin at a
// different org. Only the env-driven dogfood render (no baked org) reads the
// environment.
func orgHintAssignment(cfg GenerateConfig) string {
	if cfg.OrgID != "" {
		// Single-quote for the shell: %q would double-quote, and bash expands
		// $ and backticks inside double quotes.
		return "gram_hooks_org_hint='" + strings.ReplaceAll(cfg.OrgID, "'", `'\''`) + "'"
	}
	return `gram_hooks_org_hint="${GRAM_HOOKS_ORG_ID:-}"`
}

func renderAuthPreflightScript(cfg GenerateConfig) []byte {
	// In observability mode nothing may block or stall session start: auth
	// failures exit 0, and the interactive browser login (which can wait
	// minutes for the redirect) never runs — the in-session nudge and
	// hooks/login.sh remain the interactive paths.
	failureExit := "2"
	interactive := "1"
	if cfg.ObservabilityMode {
		failureExit = "0"
		interactive = "0"
	}
	return fmt.Appendf(nil, `#!/usr/bin/env bash
# Generated by Speakeasy. Do not edit — overwritten on every publish.
#
# Blocking auth preflight: fresh installs wait here until explicit or cached
# hook credentials are available, then later hook senders can reuse them.

set -u

server_url="${GRAM_HOOKS_SERVER_URL:-%s}"
project_slug="${GRAM_HOOKS_PROJECT_SLUG:-%s}"
%s

script_dir="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
# shellcheck source=/dev/null
if ! . "$script_dir/auth.sh"; then
  echo "Speakeasy hooks could not load auth helper." >&2
  exit %s
fi

export GRAM_HOOKS_INTERACTIVE=%s

# Never-authenticated machines fail open (prepare_auth returns 3 after
# warning); once credentials have been established, a broken auth state
# exits with the configured failure code from inside prepare_auth (2 blocks
# the session start; observability mode passes 0 so nothing ever blocks).
gram_hooks_prepare_auth "$server_url" "$project_slug" %s || true
exit 0
`, cfg.ServerURL, cfg.ProjectSlug, orgHintAssignment(cfg), failureExit, interactive, failureExit)
}

// renderLoginScript emits hooks/login.sh: the standalone interactive login
// entry point. Users (or a coding agent acting on the unauthenticated-session
// nudge) run it directly to open the dashboard browser flow and cache a
// hooks-scoped API key for this machine.
func renderLoginScript(cfg GenerateConfig) []byte {
	return fmt.Appendf(nil, `#!/usr/bin/env bash
# Generated by Speakeasy. Do not edit — overwritten on every publish.
#
# Interactive login for Speakeasy observability hooks. Opens a browser to the
# Gram dashboard, waits for the localhost callback, and caches the minted
# hooks API key for this machine. Safe to re-run: exits 0 immediately when
# already authenticated. Pass --force to discard cached credentials first.

set -u

server_url="${GRAM_HOOKS_SERVER_URL:-%s}"
project_slug="${GRAM_HOOKS_PROJECT_SLUG:-%s}"
%s

script_dir="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
# shellcheck source=/dev/null
if ! . "$script_dir/auth.sh"; then
  echo "Speakeasy hooks could not load auth helper." >&2
  exit 1
fi

export GRAM_HOOKS_INTERACTIVE=1
export GRAM_HOOKS_LOGIN_FORCE=1

if [ "${1:-}" = "--force" ]; then
  gram_hooks_forget_auth
elif gram_hooks_read_auth "$server_url" 2>/dev/null; then
  echo "Speakeasy hooks already authenticated for ${server_url} (project ${GRAM_HOOKS_CACHED_PROJECT:-unset}). Re-run with --force to re-authenticate."
  exit 0
fi

if ! gram_hooks_login "$server_url" "$project_slug"; then
  echo "Speakeasy hooks login failed. Alternatively set GRAM_HOOKS_API_KEY and GRAM_HOOKS_PROJECT_SLUG in your environment." >&2
  exit 1
fi

if ! gram_hooks_read_auth "$server_url" 2>/dev/null; then
  echo "Speakeasy hooks login completed but credentials could not be read back." >&2
  exit 1
fi

echo "Speakeasy hooks authenticated (project ${GRAM_HOOKS_CACHED_PROJECT:-unset})."
exit 0
`, cfg.ServerURL, cfg.ProjectSlug, orgHintAssignment(cfg))
}

// renderSharedAuthScript emits hooks/auth.sh: local device authentication for
// hook senders. Hook runtimes cannot assume python/node/jq are installed, so
// this helper only uses shell, curl's config file support, and POSIX utilities.
// Keep it in sync with the checked-in hooks/plugin-*/hooks/auth.sh fixtures.
func renderSharedAuthScript() []byte {
	return []byte(`#!/usr/bin/env bash
# Shared local authentication helper for Gram hook senders.

gram_hooks_auth_file() {
  if [ -n "${GRAM_HOOKS_AUTH_FILE:-}" ]; then
    printf '%s' "$GRAM_HOOKS_AUTH_FILE"
    return 0
  fi
  local config_home="${XDG_CONFIG_HOME:-$HOME/.config}"
  printf '%s/gram/hooks-auth.env' "$config_home"
}

gram_hooks_auth_value() {
  local path="$1"
  local key="$2"
  sed -n "s/^${key}=//p" "$path" 2>/dev/null | sed -n '1p'
}

gram_hooks_read_auth() {
  local server_url="$1"
  local path
  path="$(gram_hooks_auth_file)"
  if [ ! -r "$path" ]; then
    return 1
  fi
  GRAM_HOOKS_CACHED_SERVER_URL="$(gram_hooks_auth_value "$path" "server_url")"
  GRAM_HOOKS_CACHED_API_KEY="$(gram_hooks_auth_value "$path" "api_key")"
  GRAM_HOOKS_CACHED_PROJECT="$(gram_hooks_auth_value "$path" "project")"
  GRAM_HOOKS_CACHED_EMAIL="$(gram_hooks_auth_value "$path" "email")"
  GRAM_HOOKS_CACHED_ORG="$(gram_hooks_auth_value "$path" "org")"
  [ "$GRAM_HOOKS_CACHED_SERVER_URL" = "$server_url" ] || return 1
  [ -n "$GRAM_HOOKS_CACHED_API_KEY" ] || return 1
  # A cache minted for another organization must not authenticate this
  # plugin: with shared project slugs like "default", its events would land
  # in — and enforce policies from — the wrong org. Caches from before org
  # stamping carry no org and stay usable.
  if [ -n "${gram_hooks_org_hint:-}" ] && [ -n "$GRAM_HOOKS_CACHED_ORG" ] &&
    [ "$GRAM_HOOKS_CACHED_ORG" != "${gram_hooks_org_hint:-}" ]; then
    return 1
  fi
}

gram_hooks_write_auth() {
  local server_url="$1"
  local api_key="$2"
  local project="$3"
  local email="${4:-}"
  local org="${5:-}"
  local path
  path="$(gram_hooks_auth_file)"
  mkdir -p "$(dirname "$path")" || return 1
  chmod 700 "$(dirname "$path")" 2>/dev/null || true
  local tmp="${path}.tmp.$$"
  local old_umask
  old_umask="$(umask)"
  umask 077
  {
    printf 'server_url=%s\n' "$server_url"
    printf 'api_key=%s\n' "$api_key"
    printf 'project=%s\n' "$project"
    printf 'email=%s\n' "$email"
    printf 'org=%s\n' "$org"
  } >"$tmp" || {
    rm -f "$tmp"
    umask "$old_umask"
    return 1
  }
  umask "$old_umask"
  mv "$tmp" "$path" || return 1
  gram_hooks_mark_auth_established
  gram_hooks_clear_reauth_needed
}

gram_hooks_forget_auth() {
  local path
  path="$(gram_hooks_auth_file)"
  rm -f "$path"
}

gram_hooks_mark_reauth_needed() {
  : >"$(gram_hooks_auth_file).reauth-needed" 2>/dev/null || true
}

gram_hooks_reauth_needed() {
  [ -e "$(gram_hooks_auth_file).reauth-needed" ]
}

gram_hooks_clear_reauth_needed() {
  rm -f "$(gram_hooks_auth_file).reauth-needed" 2>/dev/null || true
}

# gram_hooks_auth_established reports whether this machine has EVER cached
# hook credentials — the fail-closed ratchet: before the first successful
# auth, blocking hook paths warn and fail open; afterwards they fail closed.
# The marker survives gram_hooks_forget_auth so a forgotten or invalidated
# key cannot silently disable enforcement.
gram_hooks_auth_established() {
  [ -e "$(gram_hooks_auth_file).established" ] && return 0
  [ -r "$(gram_hooks_auth_file)" ]
}

gram_hooks_mark_auth_established() {
  : >"$(gram_hooks_auth_file).established" 2>/dev/null || true
}

gram_hooks_env_key_source() {
  if [ -n "${GRAM_HOOKS_API_KEY:-}" ]; then
    printf 'GRAM_HOOKS_API_KEY'
    return 0
  fi
  return 1
}

gram_hooks_env_key_rejected_message() {
  local source
  source="$(gram_hooks_env_key_source)" || return 1
  printf 'Speakeasy hooks rejected the API key configured in %s. Update or unset %s, then run hooks/login.sh to reconnect hooks.' "$source" "$source"
}

gram_hooks_manual_auth_instructions() {
  local server_url="$1"
  local project_hint="$2"
  echo "Speakeasy hooks need a Gram hooks API key before events can be recorded." >&2
  echo "Set GRAM_HOOKS_API_KEY and GRAM_HOOKS_PROJECT_SLUG, or cache a key by sourcing hooks/auth.sh and running:" >&2
  echo "  gram_hooks_write_auth '$server_url' '<hooks-api-key>' '${project_hint}' '<email>'" >&2
}

# gram_hooks_urldecode decodes URL-encoded values (+ as space, %XX escapes).
# Literal backslashes are routed through %5C so printf %b cannot interpret
# them as escape sequences.
gram_hooks_urldecode() {
  local data="${1//+/ }"
  data="${data//\\/%5C}"
  printf '%b' "${data//%/\\x}"
}

# gram_hooks_nc_listen_styles orders candidate nc invocation styles by a
# usage-text sniff: host_port = BSD/OpenBSD (nc -l 127.0.0.1 PORT), dash_p_local
# and dash_p = GNU/busybox (-p PORT, loopback-bound when -s is accepted). The
# sniff only ranks; each style is verified with a live HTTP self-probe before
# the browser opens.
gram_hooks_nc_listen_styles() {
  local help_text
  help_text="$(nc -h 2>&1 || true)"
  case "$help_text" in
    *--local-port* | *"-p PORT"*) printf 'dash_p_local dash_p host_port' ;;
    *) printf 'host_port dash_p_local dash_p' ;;
  esac
}

gram_hooks_nc_listen() {
  case "$1" in
    dash_p_local) nc -l -p "$2" -s 127.0.0.1 2>/dev/null ;;
    dash_p) nc -l -p "$2" 2>/dev/null ;;
    *) nc -l 127.0.0.1 "$2" 2>/dev/null ;;
  esac
}

gram_hooks_login_http_response() {
  local status="$1"
  local body="$2"
  local reason="OK"
  if [ "$status" = "204" ]; then
    reason="No Content"
  elif [ "$status" = "403" ]; then
    reason="Forbidden"
  fi
  printf 'HTTP/1.1 %s %s\r\nContent-Type: text/html; charset=utf-8\r\nContent-Length: %s\r\nConnection: close\r\n\r\n%s' \
    "$status" "$reason" "${#body}" "$body"
}

gram_hooks_login_success_html() {
  printf '<!doctype html><html><head><title>Speakeasy hooks connected</title></head><body style="font-family:sans-serif;text-align:center;padding-top:4rem"><h1>Authentication successful</h1><p>Speakeasy hooks are connected. You can close this tab.</p><script>window.close()</script></body></html>'
}

# gram_hooks_login_handle_request reads one HTTP request from stdin (the nc
# pipe), captures the credential fields into a file, and writes the response
# to stdout (piped back to the client through the fifo). Dashboards honoring
# callback_method=post deliver the credentials as a form POST body so the API
# key never appears in a URL; older dashboards send them in the /callback
# query string, and both shapes are accepted. The probe path echoes a
# per-attempt marker so the readiness check can tell this listener apart from
# whatever else answers on the port; other requests without api_key (favicon)
# get a 204 so the serve loop keeps waiting for the dashboard's real
# response. The callback must echo the unguessable state token minted for
# this attempt — anyone on this machine can reach the listener, and without
# the token a racing local process could inject its own key and reroute
# telemetry to an attacker-controlled project.
gram_hooks_login_handle_request() {
  local dir="$1"
  local state="$2"
  local probe="$3"
  local request_line="" line="" path_query="" method="" content_length=""
  # This read is what waits for the browser to hit the callback. nc has already
  # accepted a connection (or is about to on a reused socket), but the countdown
  # starts the instant the serve loop iteration begins — before the user has
  # clicked through the dashboard, minted the key, and been redirected back,
  # which routinely takes far longer than a few seconds. A short timeout here
  # fires first, the handler returns with no HTTP response, and the browser
  # renders ERR_EMPTY_RESPONSE on the reused connection. Wait the full login
  # window so the callback is actually caught. Once the first line arrives the
  # rest of the request follows immediately, so the header/body reads below stay
  # short.
  IFS= read -r -t "${GRAM_HOOKS_LOGIN_TIMEOUT_SECONDS:-240}" request_line || request_line=""
  request_line="${request_line%$'\r'}"
  if [ -z "$request_line" ]; then
    return 0
  fi
  method="${request_line%% *}"
  while IFS= read -r -t 10 line; do
    line="${line%$'\r'}"
    if [ -z "$line" ]; then
      break
    fi
    case "$line" in
      [Cc][Oo][Nn][Tt][Ee][Nn][Tt]-[Ll][Ee][Nn][Gg][Tt][Hh]:*)
        content_length="${line#*:}"
        content_length="${content_length//[[:space:]]/}"
        # A malformed length must be rejected, not digit-stripped into a
        # different valid number that would truncate or garble the body.
        case "$content_length" in
          '' | *[!0-9]*) content_length="" ;;
        esac
        ;;
    esac
  done
  path_query="${request_line#* }"
  path_query="${path_query%% *}"
  case "$path_query" in
    /callback\?*)
      local cb_query="${path_query#*\?}" creds=""
      if [ "$method" = "POST" ]; then
        # read -n, not -N: macOS ships bash 3.2, which lacks -N, and -n reads
        # the exact count anyway because the urlencoded body is newline-free
        # ASCII. The length-of-length guard keeps oversized values out of the
        # arithmetic tests, and the size cap plus timeout keep a hostile
        # local client from ballooning or stalling the capture.
        if [ -n "$content_length" ] && [ "${#content_length}" -le 5 ] && [ "$content_length" -gt 0 ] && [ "$content_length" -le 16384 ]; then
          IFS= read -r -t 10 -n "$content_length" creds || true
        fi
      else
        creds="$cb_query"
      fi
      case "$creds" in
        *api_key=*)
          case "&${cb_query}&" in
            *"&state=${state}&"*)
              printf '%s' "$creds" >"$dir/query.tmp"
              mv "$dir/query.tmp" "$dir/query"
              gram_hooks_login_http_response 200 "$(gram_hooks_login_success_html)"
              ;;
            *)
              gram_hooks_login_http_response 403 ""
              ;;
          esac
          ;;
        *)
          gram_hooks_login_http_response 204 ""
          ;;
      esac
      ;;
    /gram-probe*)
      gram_hooks_login_http_response 200 "gram-hooks-probe-ok:${probe}"
      ;;
    *)
      gram_hooks_login_http_response 204 ""
      ;;
  esac
}

# gram_hooks_login_serve accepts connections one at a time until the callback
# query is captured, a stop file appears, or the request budget runs out. The
# fifo's read (nc stdin) and write (handler stdout) ends open symmetrically
# within each pipeline, and the handler exiting closes the write end — that
# EOF is what makes netcat flavors without socket-close exit (busybox) finish
# the cycle so the next iteration can listen again. A failed nc bind degrades
# to a fast, bounded loop that the probe below detects instead of a hung
# orphan process.
gram_hooks_login_serve() {
  local style="$1"
  local dir="$2"
  local port="$3"
  local state="$4"
  local probe="$5"
  local requests=0
  while [ "$requests" -lt 32 ] && [ ! -e "$dir/stop" ] && [ ! -s "$dir/query" ]; do
    gram_hooks_nc_listen "$style" "$port" <"$dir/fifo" | gram_hooks_login_handle_request "$dir" "$state" "$probe" >"$dir/fifo"
    requests=$((requests + 1))
  done
}

# The probe must see this attempt's marker, not just any HTTP response: if the
# random port is already bound by another local service, nc's bind fails while
# a bare connectivity check against that service would still succeed — and the
# browser would then deliver the freshly minted key to the wrong process.
gram_hooks_login_probe() {
  local port="$1"
  local probe="$2"
  local i=0 body=""
  while [ "$i" -lt 3 ]; do
    i=$((i + 1))
    body="$(curl -s --max-time 2 "http://127.0.0.1:${port}/gram-probe" 2>/dev/null)" || body=""
    case "$body" in
      *"gram-hooks-probe-ok:${probe}"*)
        return 0
        ;;
    esac
    sleep 1
  done
  return 1
}

# gram_hooks_login_stop_server unblocks a listening nc with a loopback poke so
# the serve loop can observe the stop file, then reaps the background job.
gram_hooks_login_stop_server() {
  local port="$1"
  local pid="$2"
  local dir="$3"
  : >"$dir/stop" 2>/dev/null || true
  curl -s -o /dev/null --max-time 1 "http://127.0.0.1:${port}/gram-stop" 2>/dev/null || true
  if [ -n "$pid" ]; then
    kill "$pid" 2>/dev/null || true
    wait "$pid" 2>/dev/null || true
  fi
}

gram_hooks_open_browser() {
  local url="$1"
  case "$(uname -s 2>/dev/null)" in
    Darwin)
      if command -v open >/dev/null 2>&1; then
        open "$url" 2>/dev/null && return 0
      fi
      ;;
    *)
      if command -v xdg-open >/dev/null 2>&1; then
        xdg-open "$url" >/dev/null 2>&1 && return 0
      fi
      ;;
  esac
  return 1
}

gram_hooks_cleanup_login() {
  if [ -n "${GRAM_HOOKS_LOGIN_TMPDIR:-}" ]; then
    : >"$GRAM_HOOKS_LOGIN_TMPDIR/stop" 2>/dev/null || true
  fi
  if [ -n "${GRAM_HOOKS_LOGIN_PORT:-}" ]; then
    curl -s -o /dev/null --max-time 1 "http://127.0.0.1:${GRAM_HOOKS_LOGIN_PORT}/gram-stop" 2>/dev/null || true
  fi
  if [ -n "${GRAM_HOOKS_LOGIN_SERVER_PID:-}" ]; then
    kill "$GRAM_HOOKS_LOGIN_SERVER_PID" 2>/dev/null || true
  fi
  if [ -n "${GRAM_HOOKS_LOGIN_TMPDIR:-}" ]; then
    rm -rf "$GRAM_HOOKS_LOGIN_TMPDIR"
  fi
}

# gram_hooks_login mints a hooks-scoped API key via the dashboard browser flow:
# start a one-shot localhost listener, open the dashboard with cli_callback_url
# pointing at it, wait for the api_key redirect, and cache the result with
# gram_hooks_write_auth. Only interactive entry points run this —
# auth_preflight.sh and login.sh export GRAM_HOOKS_INTERACTIVE=1; per-event
# hook senders never block on a browser.
gram_hooks_login() {
  local server_url="$1"
  local project_hint="$2"

  if [ "${GRAM_HOOKS_DISABLE_LOCAL_AUTH:-}" = "1" ]; then
    return 1
  fi
  if [ "${GRAM_HOOKS_INTERACTIVE:-}" != "1" ]; then
    gram_hooks_manual_auth_instructions "$server_url" "$project_hint"
    return 1
  fi
  if [ -n "${CI:-}" ] || [ -n "${SSH_CONNECTION:-}" ] || [ -n "${SSH_TTY:-}" ]; then
    echo "Speakeasy hooks: no local browser available for login. Set GRAM_HOOKS_API_KEY and GRAM_HOOKS_PROJECT_SLUG instead." >&2
    return 1
  fi
  case "$(uname -s 2>/dev/null)" in
    Darwin) ;;
    *)
      if [ -z "${DISPLAY:-}${WAYLAND_DISPLAY:-}" ]; then
        echo "Speakeasy hooks: no graphical session detected; skipping browser login. Set GRAM_HOOKS_API_KEY and GRAM_HOOKS_PROJECT_SLUG instead." >&2
        return 1
      fi
      ;;
  esac
  local dep
  for dep in nc mkfifo curl date; do
    if ! command -v "$dep" >/dev/null 2>&1; then
      echo "Speakeasy hooks: this machine is missing '$dep' for browser login." >&2
      gram_hooks_manual_auth_instructions "$server_url" "$project_hint"
      return 1
    fi
  done

  # A dismissed or failed browser attempt is not retried for a cooldown period
  # (login.sh sets GRAM_HOOKS_LOGIN_FORCE=1 to bypass), so an unattended
  # machine is not spammed with browser tabs on every session start.
  local now last attempt_marker
  attempt_marker="$(gram_hooks_auth_file).login-attempt"
  now="$(date +%s)"
  if [ "${GRAM_HOOKS_LOGIN_FORCE:-}" != "1" ] && [ -r "$attempt_marker" ]; then
    last="$(cat "$attempt_marker" 2>/dev/null)"
    if [ -n "$last" ] && [ "$((now - last))" -lt "${GRAM_HOOKS_LOGIN_COOLDOWN_SECONDS:-21600}" ] 2>/dev/null; then
      echo "Speakeasy hooks: browser login was attempted recently; run the plugin's hooks/login.sh to retry now." >&2
      return 1
    fi
  fi
  mkdir -p "$(dirname "$attempt_marker")" 2>/dev/null || true
  printf '%s' "$now" >"$attempt_marker" 2>/dev/null || true

  # Unguessable per-attempt token: the dashboard echoes it back on the
  # callback and the listener rejects anything without it, so a local
  # process racing the redirect cannot inject its own credentials. Without a
  # cryptographic source the guard would be enumerable, so browser login is
  # refused entirely rather than run with a guessable token.
  local state
  state="$(od -An -N16 -tx1 /dev/urandom 2>/dev/null | tr -d ' \n')"
  if [ -z "$state" ]; then
    state="$(openssl rand -hex 16 2>/dev/null)"
  fi
  if [ -z "$state" ]; then
    echo "Speakeasy hooks: no secure random source for browser login on this machine." >&2
    gram_hooks_manual_auth_instructions "$server_url" "$project_hint"
    return 1
  fi
  # Separate marker for the readiness probe: it is echoed to anyone who asks,
  # so it must never be the state token.
  local probe
  probe="$(od -An -N8 -tx1 /dev/urandom 2>/dev/null | tr -d ' \n')"
  if [ -z "$probe" ]; then
    probe="$(openssl rand -hex 8 2>/dev/null)"
  fi

  GRAM_HOOKS_LOGIN_TMPDIR="$(mktemp -d "${TMPDIR:-/tmp}/gram-hooks-login.XXXXXX")" || return 1
  local dir="$GRAM_HOOKS_LOGIN_TMPDIR"
  if ! mkfifo "$dir/fifo"; then
    rm -rf "$dir"
    GRAM_HOOKS_LOGIN_TMPDIR=""
    return 1
  fi

  local style port tries started=""
  for style in $(gram_hooks_nc_listen_styles); do
    tries=0
    while [ "$tries" -lt 2 ]; do
      tries=$((tries + 1))
      port=$(( (${RANDOM:-17} % 45000) + 20000 ))
      rm -f "$dir/query" "$dir/stop"
      gram_hooks_login_serve "$style" "$dir" "$port" "$state" "$probe" &
      GRAM_HOOKS_LOGIN_SERVER_PID=$!
      GRAM_HOOKS_LOGIN_PORT="$port"
      if gram_hooks_login_probe "$port" "$probe"; then
        started=1
        break 2
      fi
      gram_hooks_login_stop_server "$port" "$GRAM_HOOKS_LOGIN_SERVER_PID" "$dir"
      GRAM_HOOKS_LOGIN_SERVER_PID=""
      GRAM_HOOKS_LOGIN_PORT=""
    done
  done
  if [ -z "$started" ]; then
    rm -rf "$dir"
    GRAM_HOOKS_LOGIN_TMPDIR=""
    echo "Speakeasy hooks: could not start a localhost login listener." >&2
    gram_hooks_manual_auth_instructions "$server_url" "$project_hint"
    return 1
  fi

  # The callback URL carries the state token as its own query parameter; the
  # dashboard preserves it whether it appends the credentials to the query
  # string (legacy) or, per callback_method=post, delivers them as a form
  # body that keeps the API key out of URLs.
  local auth_url="${server_url%/}/?from_cli=true&cli_callback_url=http%3A%2F%2F127.0.0.1%3A${port}%2Fcallback%3Fstate%3D${state}&key_scope=hooks&callback_method=post"
  # Project slugs are URL-safe by construction; anything else would need
  # percent-encoding, so it is dropped rather than corrupt the query string.
  case "$project_hint" in
    "" | *[!A-Za-z0-9._-]*) ;;
    *) auth_url="${auth_url}&project=${project_hint}" ;;
  esac
  # Pin the mint to the plugin's organization (callers set gram_hooks_org_hint
  # from the generated config): in a multi-org browser session the dashboard
  # refuses to mint a key when the active org differs, instead of silently
  # binding this machine's telemetry to whichever org happens to be active.
  case "${gram_hooks_org_hint:-}" in
    "" | *[!A-Za-z0-9._-]*) ;;
    *) auth_url="${auth_url}&organization_id=${gram_hooks_org_hint}" ;;
  esac
  echo "Speakeasy hooks: opening your browser to connect observability hooks." >&2
  echo "If nothing opens, visit: $auth_url" >&2
  # Hand the opener a 0600 file:// redirect instead of the URL itself:
  # process arguments are world-readable (ps, /proc/<pid>/cmdline), and the
  # state token must not leak to other local users who can also reach the
  # loopback listener. The stderr copy above is same-user-only and is the
  # manual completion path, which needs the token to work.
  local launch_url="$auth_url"
  local escaped_url="${auth_url//&/&amp;}"
  if printf '<!doctype html><meta http-equiv="refresh" content="0;url=%s"><title>Speakeasy sign-in</title><a href="%s">Continue to Speakeasy sign-in</a>\n' "$escaped_url" "$escaped_url" >"$dir/open.html" 2>/dev/null; then
    chmod 600 "$dir/open.html" 2>/dev/null || true
    launch_url="$dir/open.html"
  fi
  gram_hooks_open_browser "$launch_url" || true

  local waited=0
  local wait_limit="${GRAM_HOOKS_LOGIN_TIMEOUT_SECONDS:-240}"
  while [ "$waited" -lt "$wait_limit" ] && [ ! -s "$dir/query" ]; do
    sleep 1
    waited=$((waited + 1))
  done

  gram_hooks_login_stop_server "$port" "$GRAM_HOOKS_LOGIN_SERVER_PID" "$dir"
  GRAM_HOOKS_LOGIN_SERVER_PID=""
  GRAM_HOOKS_LOGIN_PORT=""

  local query=""
  if [ -r "$dir/query" ]; then
    query="$(cat "$dir/query" 2>/dev/null)"
  fi
  rm -rf "$dir"
  GRAM_HOOKS_LOGIN_TMPDIR=""
  if [ -z "$query" ]; then
    echo "Speakeasy hooks: browser login did not complete. Run the plugin's hooks/login.sh to try again." >&2
    return 1
  fi

  local api_key="" project="" email="" org="" pair pairs
  IFS='&' read -r -a pairs <<<"$query"
  for pair in "${pairs[@]}"; do
    case "$pair" in
      api_key=*) api_key="$(gram_hooks_urldecode "${pair#api_key=}")" ;;
      project=*) project="$(gram_hooks_urldecode "${pair#project=}")" ;;
      email=*) email="$(gram_hooks_urldecode "${pair#email=}")" ;;
      organization_id=*) org="$(gram_hooks_urldecode "${pair#organization_id=}")" ;;
    esac
  done
  if [ -z "$api_key" ]; then
    echo "Speakeasy hooks: login callback did not include an API key." >&2
    return 1
  fi
  if [ -z "$project" ]; then
    project="$project_hint"
  fi
  # Older dashboards omit organization_id on the callback; the login URL
  # pinned the mint to the plugin's org, so fall back to that hint.
  if [ -z "$org" ]; then
    org="${gram_hooks_org_hint:-}"
  fi
  if ! gram_hooks_write_auth "$server_url" "$api_key" "$project" "$email" "$org"; then
    echo "Speakeasy hooks: could not cache the new hooks API key." >&2
    return 1
  fi
  rm -f "$attempt_marker" 2>/dev/null || true
  echo "Speakeasy hooks: connected${email:+ as $email} (project ${project:-unset})." >&2
  return 0
}

gram_hooks_write_curl_config() {
  local api_key="$1"
  local project="$2"
  gram_hooks_cleanup_auth_config
  auth_config=""
  auth_config_arg=()
  auth_config=$(mktemp "${TMPDIR:-/tmp}/gram-hooks-curl.XXXXXX") || return 1
  chmod 600 "$auth_config" || true
  # curl config quoted strings treat backslash and double quote specially,
  # and the config file is line-oriented; escape the metacharacters and strip
  # CR/LF so a hostile or corrupted cached value cannot break out of the
  # header directive or inject additional config lines.
  api_key="${api_key//\\/\\\\}"
  api_key="${api_key//\"/\\\"}"
  api_key="${api_key//$'\n'/}"
  api_key="${api_key//$'\r'/}"
  project="${project//\\/\\\\}"
  project="${project//\"/\\\"}"
  project="${project//$'\n'/}"
  project="${project//$'\r'/}"
  printf 'header = "Gram-Key: %s"\n' "$api_key" >"$auth_config"
  printf 'header = "Gram-Project: %s"\n' "$project" >>"$auth_config"
  auth_config_arg=(--config "$auth_config")
}

gram_hooks_cleanup_auth_config() {
  if [ -n "${auth_config:-}" ]; then
    rm -f "$auth_config"
  fi
}
# Installed at source time: scripts sourcing this library must not set their
# own EXIT trap, or it would be overwritten here.
trap 'gram_hooks_cleanup_auth_config; gram_hooks_cleanup_login' EXIT

gram_hooks_prepare_auth() {
  local server_url="$1"
  local project_hint="$2"
  local failure_exit="$3"
  local force="${4:-}"
  local api_key project email

  # Refuse to send credentials over plaintext HTTP; only loopback hosts
  # (local dev servers) are exempt. Same ratchet as auth failures: machines
  # that never authenticated fail open (return 3 also skips the network
  # entirely, so no key can leak), established machines fail closed.
  case "$server_url" in
    https://*) ;;
    http://127.0.0.1 | http://127.0.0.1[:/]* | http://localhost | http://localhost[:/]* | http://\[::1\] | http://\[::1\][:/]*) ;;
    *)
      echo "Speakeasy hooks refused insecure Gram server URL '$server_url'; use https:// (or an http://localhost dev server)." >&2
      if gram_hooks_auth_established; then
        exit "$failure_exit"
      fi
      return 3
      ;;
  esac

  api_key=""
  project=""
  email=""
  if [ "$force" != "force" ]; then
    api_key="${GRAM_HOOKS_API_KEY:-}"
    project="${GRAM_HOOKS_PROJECT_SLUG:-}"
  fi

  if [ -z "$api_key" ]; then
    GRAM_HOOKS_CACHED_API_KEY=""
    GRAM_HOOKS_CACHED_PROJECT=""
    GRAM_HOOKS_CACHED_EMAIL=""
    if [ "$force" != "force" ] && ! gram_hooks_read_auth "$server_url" 2>/dev/null; then
      # gram_hooks_read_auth populates the CACHED_* fields before validating
      # them; a cache rejected for a server or organization mismatch must not
      # leak into the send path below.
      GRAM_HOOKS_CACHED_API_KEY=""
      GRAM_HOOKS_CACHED_PROJECT=""
      GRAM_HOOKS_CACHED_EMAIL=""
    fi
    if [ -z "${GRAM_HOOKS_CACHED_API_KEY:-}" ]; then
      if ! gram_hooks_login "$server_url" "$project_hint"; then
        if [ "${GRAM_HOOKS_INTERACTIVE:-}" != "1" ] && gram_hooks_reauth_needed; then
          GRAM_HTTP_CODE=""
          GRAM_HTTP_BODY='{"message":"Speakeasy hooks need to reconnect. Run hooks/login.sh to reconnect hooks."}'
          return 79
        fi
        if gram_hooks_auth_established; then
          echo "Speakeasy hooks could not authenticate with Gram. Run the plugin's hooks/login.sh to reconnect, or set GRAM_HOOKS_API_KEY." >&2
          exit "$failure_exit"
        fi
        echo "Speakeasy hooks are not connected on this machine yet; events are not being recorded. Run the plugin's hooks/login.sh to connect." >&2
        return 3
      fi
      if ! gram_hooks_read_auth "$server_url" 2>/dev/null; then
        echo "Speakeasy hooks could not read Gram authentication after login." >&2
        exit "$failure_exit"
      fi
    fi
    api_key="${GRAM_HOOKS_CACHED_API_KEY:-}"
    # An explicit env project selection outranks the project the key was
    # cached with; the cached project only outranks the baked default, so a
    # login-time project choice survives sends but GRAM_HOOKS_PROJECT_SLUG
    # still switches projects without forcing a re-login.
    project="${GRAM_HOOKS_PROJECT_SLUG:-${GRAM_HOOKS_CACHED_PROJECT:-}}"
    email="${GRAM_HOOKS_CACHED_EMAIL:-}"
  fi

  if [ -z "$project" ]; then
    project="$project_hint"
  fi
  if [ -z "$api_key" ] || [ -z "$project" ]; then
    echo "Speakeasy hooks are missing Gram authentication or project selection." >&2
    exit "$failure_exit"
  fi

  if ! gram_hooks_write_curl_config "$api_key" "$project"; then
    echo "Speakeasy hooks could not prepare Gram authentication." >&2
    exit "$failure_exit"
  fi

  if [ -n "$email" ]; then
    export GRAM_HOOKS_AUTH_EMAIL="$email"
  fi
}

gram_hooks_post_authenticated() {
  local server_url="$1"
  local payload="$2"
  local max_time="$3"
  local project_hint="$4"
  local failure_exit="$5"
  shift 5

  # Return 78 when this machine has never authenticated (ratchet fail-open):
  # callers emit a pass-through response instead of blocking. Return 79 when a
  # rejected cached key was cleared and the caller should surface reconnect.
  # Other established-auth failures still fail closed by exiting from within.
  gram_hooks_prepare_auth "$server_url" "$project_hint" "$failure_exit"
  local prepare_status=$?
  if [ "$prepare_status" -ne 0 ]; then
    GRAM_HTTP_CODE=""
    if [ "$prepare_status" -ne 79 ]; then
      GRAM_HTTP_BODY=""
    fi
    if [ "$prepare_status" -eq 79 ]; then
      return 79
    fi
    return 78
  fi
  gram_http_post "${server_url}/rpc/hooks.ingest" "$payload" "$max_time" \
    "$@" \
    ${auth_config_arg[@]+"${auth_config_arg[@]}"}
  local first_status="$GRAM_HTTP_CODE"
  # Retry through the browser-login cache only when the rejected credentials
  # came from it. Explicit GRAM_HOOKS_API_KEY values take
  # precedence over the cache on every send, so a re-login can never replace
  # them: a rejected configured key must fall through to the caller's non-2xx
  # handling (fail closed) rather than wipe the cache and downgrade to the
  # never-authenticated pass-through.
  if { [ "$first_status" = "401" ] || [ "$first_status" = "403" ]; } \
    && [ -z "${GRAM_HOOKS_API_KEY:-}" ] \
    && [ "${GRAM_HOOKS_DISABLE_LOCAL_AUTH:-}" != "1" ]; then
    gram_hooks_forget_auth
    gram_hooks_mark_reauth_needed
    if [ "${GRAM_HOOKS_INTERACTIVE:-}" != "1" ]; then
      GRAM_HTTP_CODE="$first_status"
      return 79
    fi
    if ! gram_hooks_prepare_auth "$server_url" "$project_hint" "$failure_exit" force; then
      GRAM_HTTP_CODE="$first_status"
      return 78
    fi
    gram_http_post "${server_url}/rpc/hooks.ingest" "$payload" "$max_time" \
      "$@" \
      ${auth_config_arg[@]+"${auth_config_arg[@]}"}
  fi
}
`)
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

// renderHookScript produces the bash wrapper that forwards hook event JSON
// from stdin to the unified Gram endpoint. The shared auth helper supplies
// Gram-Key + Gram-Project from explicit env credentials or a per-device cached
// hooks key.
//
// The script captures the HTTP status code and response body separately so
// it can forward the body to stdout (for PreToolUse deny decisions) while
// still exiting with code 2 on 4xx/5xx to signal a block to Claude.
func renderHookScript(cfg GenerateConfig, platform string) []byte {
	projectSlug := cfg.ProjectSlug
	// In observability mode the plugin must never block: server deny decisions
	// are swallowed and transport failures exit 0 instead of 2.
	nonblocking := ""
	if cfg.ObservabilityMode {
		nonblocking = "1"
	}
	cursorMCPEnrichment := ""
	if platform == "cursor" {
		cursorMCPEnrichment = renderCursorMCPEnrichmentSnippet()
	}
	claudeMCPEnrichment := ""
	if platform == "claude" {
		claudeMCPEnrichment = renderClaudeMCPEnrichmentSnippet()
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

# Send a hook event to Speakeasy. The server is the sole authority on whether to block:
#   HTTP 2xx -> allow (exit 0, no stdout — Codex allow = empty stdout).
#   HTTP 4xx/5xx -> block (exit 2). Server message relayed to stderr.
# The script never makes the allow/deny decision — only the server does.

set -u

server_url="${GRAM_HOOKS_SERVER_URL:-%s}"
project_slug="${GRAM_HOOKS_PROJECT_SLUG:-%s}"
%s
gram_hooks_nonblocking="%s"
# In observability mode auth-state failures must not block either: prepare_auth
# exits with this value on an established machine whose credentials broke.
gram_hooks_failure_exit=2
if [ -n "$gram_hooks_nonblocking" ]; then
  gram_hooks_failure_exit=0
fi
provider_payload=$(cat)

%s

%s

hook_hostname=$(hostname 2>/dev/null || true)
native_event="$(gram_hooks_native_event_name "$provider_payload")"
payload="$(gram_hooks_build_canonical_payload "$provider_payload" "$hook_hostname")"

# gram_http_post (http.sh) retries transient resets so a single reset no
# longer blocks the tool call; the server still decides allow/block.
gram_hooks_post_authenticated "$server_url" "$payload" 10 "$project_slug" "$gram_hooks_failure_exit"
post_status=$?
# 78 = never-authenticated ratchet fail-open: allow (empty stdout) instead of
# blocking a machine that has no way to hold credentials yet.
if [ "$post_status" -eq 78 ]; then
  exit 0
fi

http_code="$GRAM_HTTP_CODE"
body="$GRAM_HTTP_BODY"

# curl returns 000 on connection failure — treat as block so an unreachable
# Speakeasy server cannot silently bypass blocking policies.
if [ "$http_code" -ge 200 ] 2>/dev/null && [ "$http_code" -lt 300 ] 2>/dev/null; then
  decision="$(gram_hooks_json_string_value "$body" "decision")"
  reason="$(gram_hooks_decision_message "$body")"
  if [ "$decision" = "deny" ] && [ -z "$gram_hooks_nonblocking" ]; then
    echo "${reason:-Speakeasy blocked this Codex hook}" >&2
    exit 2
  fi
  exit 0
fi

reason="$(gram_hooks_json_string_value "$body" "message")"
echo "${reason:-Speakeasy hook returned HTTP ${http_code}}" >&2
if [ -n "$gram_hooks_nonblocking" ]; then
  exit 0
fi
exit 2
`, cfg.ServerURL, projectSlug, orgHintAssignment(cfg), nonblocking, renderHookRuntimeSourceSnippet(), renderHookPayloadNormalizationSnippet("codex"))
	}

	return fmt.Appendf(nil, `#!/usr/bin/env bash
# Generated by Speakeasy. Do not edit — overwritten on every publish.

# Send a hook event to Speakeasy. The server is the sole authority on whether to block:
#   HTTP 2xx -> proceed (exit 0). A deny decision in the body is relayed as an
#               explicit block; anything else emits an empty response so the
#               client's own permission flow still runs (never a forced allow).
#   HTTP 4xx/5xx -> block (exit 2). Server message relayed to stderr.
# The script never makes the allow/deny decision — only the server does.

set -u

server_url="${GRAM_HOOKS_SERVER_URL:-%s}"
project_slug="${GRAM_HOOKS_PROJECT_SLUG:-%s}"
%s
gram_hooks_nonblocking="%s"
# In observability mode auth-state failures must not block either: prepare_auth
# exits with this value on an established machine whose credentials broke.
gram_hooks_failure_exit=2
if [ -n "$gram_hooks_nonblocking" ]; then
  gram_hooks_failure_exit=0
fi

%s
provider_payload=$(cat)
if type gram_hooks_enrich_cursor_mcp_payload >/dev/null 2>&1; then
  provider_payload="$(gram_hooks_enrich_cursor_mcp_payload "$provider_payload")"
fi
if type gram_hooks_enrich_claude_mcp_payload >/dev/null 2>&1; then
  provider_payload="$(gram_hooks_enrich_claude_mcp_payload "$provider_payload")"
fi

%s

# gram_hooks_emit_login_nudge injects a once-per-session UserPromptSubmit
# additionalContext telling Claude the hooks are unauthenticated and where the
# login helper lives, so it can offer to run it for the user.
gram_hooks_emit_login_nudge() {
  local payload="$1"
  local plugin_hooks_dir="$2"
  local session_id marker context escaped
  session_id="$(gram_hooks_json_string_value "$payload" "session_id")"
  # The session id comes from the provider payload; sanitize it before using
  # it in a filesystem path so a crafted value cannot escape TMPDIR.
  session_id="$(printf '%%s' "$session_id" | tr -c 'A-Za-z0-9_.-' '_')"
  marker="${TMPDIR:-/tmp}/gram-hooks-login-nudge-${session_id:-$(date +%%Y%%m%%d)}"
  if [ -e "$marker" ]; then
    return 0
  fi
  : >"$marker" 2>/dev/null || true
  context="Notice from the Speakeasy observability plugin installed in this workspace: this machine has not completed sign-in, so the telemetry this workspace expects is not being recorded. Handle the user's current request as normal, then you MUST directly ask the user whether to connect Speakeasy hooks now — put it to them as an explicit question (use your ask-user tool if you have one); this is the user's decision, so do not resolve it yourself and do not silently drop this notice. If they agree, run: bash \"${plugin_hooks_dir}/login.sh\" (opens a browser sign-in and waits for it to complete). Never run it without their explicit consent. If they decline, do not raise this again during this session."
  escaped="$(printf '%%s' "$context" | gram_hooks_json_escape_string)"
  printf '{"hookSpecificOutput":{"hookEventName":"UserPromptSubmit","additionalContext":"%%s"}}' "$escaped"
}

hook_hostname=$(hostname 2>/dev/null || true)
native_event="$(gram_hooks_native_event_name "$provider_payload")"
if [ "%s" = "cursor" ] && [ "$native_event" != "beforeSubmitPrompt" ]; then
  gram_hooks_cursor_backfill_prompt_if_missing "$provider_payload" "$hook_hostname" "$server_url" "$project_slug"
  # A denied backfilled prompt would have blocked at beforeSubmitPrompt had
  # that delivery not been missed; relay the deny on this turn's decision
  # event instead of letting the turn keep executing. A failed backfill also
  # blocks: it was the turn's only prompt-policy check, so proceeding would
  # skip prompt blocking exactly on the recovery path.
  if [ -z "$gram_hooks_nonblocking" ]; then
    case "$native_event" in
      preToolUse | beforeMCPExecution)
        # A deny stashed by a backfill on an earlier non-decision event is
        # consumed here; the take always runs so a deny already relayed
        # in-process does not leave a stale stash behind.
        pending_deny="$(gram_hooks_cursor_take_pending_prompt_deny "$provider_payload" "$server_url" "$project_slug")" || pending_deny=""
        if [ "${GRAM_HOOKS_BACKFILL_DECISION:-}" != "deny" ] && [ -n "$pending_deny" ]; then
          GRAM_HOOKS_BACKFILL_DECISION="deny"
          GRAM_HOOKS_BACKFILL_BODY="$pending_deny"
        fi
        if [ "${GRAM_HOOKS_BACKFILL_DECISION:-}" = "deny" ]; then
          gram_hooks_provider_response "cursor" "$native_event" "$GRAM_HOOKS_BACKFILL_BODY"
          exit 0
        fi
        if [ "${GRAM_HOOKS_BACKFILL_STATUS:-}" = "failed" ]; then
          gram_hooks_provider_response "cursor" "$native_event" '{"decision":"deny","message":"Speakeasy could not verify this turn'"'"'s prompt against policy, so the tool call was blocked. Retry in a moment."}'
          exit 0
        fi
        ;;
    esac
  fi
fi
if [ "%s" = "cursor" ]; then
  # Cursor also fires the generic pre/post/failure hooks around MCP calls;
  # the dedicated before/afterMCPExecution events carry the same call, so
  # the generic echoes are skipped to avoid duplicate telemetry and
  # duplicate chat tool rows under the same synthetic tool id.
  case "$native_event" in
    preToolUse | postToolUse | postToolUseFailure)
      cursor_tool_name="$(gram_hooks_json_string_value "$provider_payload" "tool_name")"
      case "$cursor_tool_name" in
        MCP:*)
          gram_hooks_provider_response "%s" "$native_event" '{}'
          exit 0
          ;;
      esac
      ;;
  esac
fi
payload="$(gram_hooks_build_canonical_payload "$provider_payload" "$hook_hostname")"

# gram_http_post (http.sh) retries transient resets so a single reset no
# longer blocks the tool call; the server still decides allow/block.
gram_hooks_post_authenticated "$server_url" "$payload" 10 "$project_slug" "$gram_hooks_failure_exit"
post_status=$?
# 78 = never-authenticated ratchet fail-open: emit a pass-through response
# instead of blocking a machine that has no way to hold credentials yet. On
# Claude prompt submission, additionally nudge the agent to offer login.
if [ "$post_status" -eq 78 ]; then
  if [ "%s" = "claude" ] && [ "$native_event" = "UserPromptSubmit" ]; then
    gram_hooks_emit_login_nudge "$provider_payload" "$script_dir"
  else
    gram_hooks_provider_response "%s" "$native_event" '{}'
  fi
  exit 0
fi

http_code="$GRAM_HTTP_CODE"
body="$GRAM_HTTP_BODY"

if [ "$post_status" -eq 79 ] && [ "%s" = "claude" ] && [ "$native_event" = "UserPromptSubmit" ]; then
  gram_hooks_emit_login_nudge "$provider_payload" "$script_dir"
  exit 0
fi

# curl returns 000 on connection failure — treat as block so an unreachable
# Speakeasy server cannot silently bypass blocking policies.
if [ "$http_code" -ge 200 ] 2>/dev/null && [ "$http_code" -lt 300 ] 2>/dev/null; then
  if [ "%s" = "cursor" ] && [ "$native_event" = "beforeSubmitPrompt" ]; then
    gram_hooks_cursor_mark_prompt_submitted "$provider_payload" "$server_url" "$project_slug"
  fi
  if [ -n "$gram_hooks_nonblocking" ]; then
    body='{}'
  fi
  gram_hooks_provider_response "%s" "$native_event" "$body"
  exit 0
fi

reason="$(gram_hooks_json_string_value "$body" "message")"
if { [ "$http_code" = "401" ] || [ "$http_code" = "403" ]; } &&
  type gram_hooks_env_key_rejected_message >/dev/null 2>&1 &&
  env_reason="$(gram_hooks_env_key_rejected_message)"; then
  if [ -n "$reason" ]; then
    reason="${env_reason} Server response: ${reason}"
  else
    reason="$env_reason"
  fi
fi
echo "${reason:-Speakeasy hook returned HTTP ${http_code}}" >&2
if [ -n "$gram_hooks_nonblocking" ]; then
  gram_hooks_provider_response "%s" "$native_event" '{}'
  exit 0
fi
exit 2
`, cfg.ServerURL, projectSlug, orgHintAssignment(cfg), nonblocking, renderHookRuntimeSourceSnippet()+cursorMCPEnrichment+claudeMCPEnrichment, renderHookPayloadNormalizationSnippet(platform), platform, platform, platform, platform, platform, platform, platform, platform, platform)
}

func renderClaudeMCPEnrichmentSnippet() string {
	return `

gram_hooks_sanitize_claude_mcp_name() {
  local s="$1"
  s="${s// /_}"
  s="${s//(/}"
  s="${s//)/}"
  while [[ "$s" == *"__"* ]]; do
    s="${s//__/_}"
  done
  while [[ "$s" == _* ]]; do
    s="${s#_}"
  done
  while [[ "$s" == *_ ]]; do
    s="${s%_}"
  done
  printf '%s' "$s"
}

gram_hooks_enrich_claude_mcp_payload() {
  local input="$1"
  if ! command -v jq >/dev/null 2>&1; then
    printf '%s' "$input"
    return
  fi

  local event
  event=$(printf '%s' "$input" | jq -r '.hook_event_name // .event_name // .event // empty' 2>/dev/null) || {
    printf '%s' "$input"
    return
  }
  case "$event" in
    PreToolUse|PostToolUse|PostToolUseFailure) ;;
    *)
      printf '%s' "$input"
      return
      ;;
  esac

  local existing_url
  existing_url=$(printf '%s' "$input" | jq -r '(.url // .mcp_server_url // "") | select(type == "string")' 2>/dev/null) || true
  if [ -n "$existing_url" ]; then
    printf '%s' "$input"
    return
  fi

  local tool_name server_identity
  tool_name=$(printf '%s' "$input" | jq -r '.tool_name // empty | select(type == "string")' 2>/dev/null) || true
  case "$tool_name" in
    mcp__*__*) ;;
    *)
      printf '%s' "$input"
      return
      ;;
  esac
  server_identity="${tool_name#mcp__}"
  server_identity="${server_identity%%__*}"
  if [ -z "$server_identity" ]; then
    printf '%s' "$input"
    return
  fi

  # Marketplace installs put the sanctioned Gram MCP URLs in sibling feature
  # plugins' .mcp.json, not in the observability plugin running this hook, so
  # the sibling configs must be scanned too — otherwise plugin-prefixed calls
  # lose their URL evidence and Shadow MCP treats them as non-Gram-hosted.
  local plugin_root=""
  if [ -n "${CLAUDE_PLUGIN_ROOT:-}" ]; then
    plugin_root="$CLAUDE_PLUGIN_ROOT"
  else
    plugin_root="$(cd "$script_dir/.." 2>/dev/null && pwd)"
  fi
  local candidates=("${plugin_root}/.mcp.json")
  local sibling
  for sibling in "$(dirname "$plugin_root")"/*/.mcp.json; do
    [ "$sibling" = "${plugin_root}/.mcp.json" ] && continue
    candidates+=("$sibling")
  done

  local matched_name=""
  local matched_url=""
  local ambiguous=0
  local mcp_file rows name url prefix file_plugin_prefix
  for mcp_file in "${candidates[@]}"; do
    [ -f "$mcp_file" ] || continue
    file_plugin_prefix="plugin_$(gram_hooks_sanitize_claude_mcp_name "$(basename "$(dirname "$mcp_file")")")_"
    rows=$(jq -r '.mcpServers // {} | to_entries[] | [.key, (.value.url // "")] | @tsv' "$mcp_file" 2>/dev/null) || continue
    while IFS=$'\t' read -r name url; do
      [ -n "$name" ] && [ -n "$url" ] || continue
      prefix="$(gram_hooks_sanitize_claude_mcp_name "$name")"
      if [ "$prefix" != "$server_identity" ] && [ "${file_plugin_prefix}${prefix}" != "$server_identity" ]; then
        continue
      fi
      if [ -z "$matched_url" ]; then
        matched_name="$name"
        matched_url="$url"
        continue
      fi
      if [ "$matched_url" != "$url" ]; then
        ambiguous=1
        break
      fi
    done <<< "$rows"
    [ "$ambiguous" -eq 0 ] || break
  done

  # Cowork/cmux sessions name MCP tools by connector UUID, which never
  # matches a .mcp.json display name. The run's connector config maps
  # UUID -> URL: CLAUDE_PROJECT_DIR is .../local_<rid>/outputs and the config
  # sits one directory up as .../local_<rid>.json, falling back to the newest
  # sibling when the per-run file has not been written yet. Without this
  # lookup, UUID-prefixed Gram-hosted calls arrive with no URL evidence and
  # Shadow MCP blocks the customer's own tools.
  if [ -z "$matched_url" ] && [ "$ambiguous" -eq 0 ] && [ -n "${CLAUDE_PROJECT_DIR:-}" ]; then
    local cowork_json="" candidate_local_dir cowork_parent sibling_json connector_uuid
    candidate_local_dir="$(dirname "$CLAUDE_PROJECT_DIR")"
    if [ -f "${candidate_local_dir}.json" ]; then
      cowork_json="${candidate_local_dir}.json"
    else
      cowork_parent="$(dirname "$candidate_local_dir")"
      if [ -d "$cowork_parent" ]; then
        sibling_json="$(ls -t "$cowork_parent"/local_*.json 2>/dev/null | head -1)"
        if [ -n "$sibling_json" ] && [ -f "$sibling_json" ]; then
          cowork_json="$sibling_json"
        fi
      fi
    fi
    if [ -n "$cowork_json" ]; then
      rows=$(jq -r '
        (.remoteMcpServersConfig // [])[]
        | [
            (.uuid // .connectorUuid // .connector_uuid // .id // .connectorId // .connector_id // ""),
            (.name // .displayName // .display_name // ""),
            (.url // .serverUrl // .server_url // "")
          ]
        | @tsv' "$cowork_json" 2>/dev/null) || rows=""
      while IFS=$'\t' read -r connector_uuid name url; do
        [ -n "$url" ] || continue
        if [ "$connector_uuid" != "$server_identity" ] &&
          [ "$(gram_hooks_sanitize_claude_mcp_name "$name")" != "$server_identity" ]; then
          continue
        fi
        if [ -z "$matched_url" ]; then
          matched_name="${name:-$connector_uuid}"
          matched_url="$url"
          continue
        fi
        if [ "$matched_url" != "$url" ]; then
          ambiguous=1
          break
        fi
      done <<< "$rows"
    fi
  fi

  if [ -z "$matched_url" ] || [ "$ambiguous" -ne 0 ]; then
    printf '%s' "$input"
    return
  fi

  printf '%s' "$input" | jq -c --arg name "$matched_name" --arg identity "$server_identity" --arg url "$matched_url" \
    '. + {mcp_server_name: $name, server_identity: $identity, url: $url, mcp_server_url: $url}' 2>/dev/null || printf '%s' "$input"
}
`
}

func renderCursorMCPEnrichmentSnippet() string {
	return `

gram_hooks_stat_uid_mode() {
  local path="$1"
  if stat -c '%u %a' "$path" >/dev/null 2>&1; then
    stat -c '%u %a' "$path"
    return
  fi
  stat -f '%u %Lp' "$path" 2>/dev/null
}

gram_hooks_trusted_cursor_manifest() {
  local path="$1"
  [ -n "$path" ] && [ -f "$path" ] || return 1
  [ ! -L "$path" ] || return 1

  local dir
  dir=$(dirname "$path")
  [ -d "$dir" ] && [ ! -L "$dir" ] || return 1

  local uid file_stat dir_stat file_uid file_mode dir_uid dir_mode
  uid=$(id -u 2>/dev/null) || return 1
  file_stat=$(gram_hooks_stat_uid_mode "$path") || return 1
  dir_stat=$(gram_hooks_stat_uid_mode "$dir") || return 1

  file_uid="${file_stat%% *}"
  file_mode="${file_stat#* }"
  dir_uid="${dir_stat%% *}"
  dir_mode="${dir_stat#* }"

  [ "$file_uid" = "$uid" ] && [ "$dir_uid" = "$uid" ] || return 1
  [ -n "$file_mode" ] && [ -n "$dir_mode" ] || return 1
  [ $((8#$file_mode & 022)) -eq 0 ] || return 1
  [ $((8#$dir_mode & 022)) -eq 0 ] || return 1
}

gram_hooks_enrich_cursor_mcp_payload() {
  local input="$1"
  if ! command -v jq >/dev/null 2>&1; then
    printf '%s' "$input"
    return
  fi

  local event
  event=$(printf '%s' "$input" | jq -r '.hook_event_name // .event_name // .event // empty' 2>/dev/null) || {
    printf '%s' "$input"
    return
  }
  case "$event" in
    beforeMCPExecution|afterMCPExecution) ;;
    *)
      printf '%s' "$input"
      return
      ;;
  esac

  local existing_url
  existing_url=$(printf '%s' "$input" | jq -r '(.url // .mcp_server_url // "") | select(type == "string")' 2>/dev/null) || true
  if [ -n "$existing_url" ]; then
    printf '%s' "$input"
    return
  fi

  local server_name
  server_name=$(printf '%s' "$input" | jq -r '(.mcp_server_name // .command // "") | select(type == "string")' 2>/dev/null) || true
  if [ -z "$server_name" ]; then
    printf '%s' "$input"
    return
  fi

  local roots=()
  if [ -n "${CURSOR_PLUGIN_ROOT:-}" ]; then
    roots+=("$(dirname "$CURSOR_PLUGIN_ROOT")")
  fi
  if [ -n "${script_dir:-}" ]; then
    roots+=("$(dirname "$(dirname "$script_dir")")")
  fi
  roots+=("${HOME}/.cursor/plugins/local")

  local matched_url=""
  local ambiguous=0
  local root mcp_file url
  for root in "${roots[@]}"; do
    for mcp_file in "$root"/*/mcp.json; do
      gram_hooks_trusted_cursor_manifest "$mcp_file" || continue
      url=$(jq -r --arg name "$server_name" '.mcpServers[$name].url // empty | select(type == "string")' "$mcp_file" 2>/dev/null) || continue
      [ -n "$url" ] || continue
      if [ -z "$matched_url" ]; then
        matched_url="$url"
        continue
      fi
      if [ "$matched_url" != "$url" ]; then
        ambiguous=1
        break
      fi
    done
    [ "$ambiguous" -eq 0 ] || break
  done

  if [ -z "$matched_url" ] || [ "$ambiguous" -ne 0 ]; then
    printf '%s' "$input"
    return
  fi

  printf '%s' "$input" | jq -c --arg url "$matched_url" '. + {url: $url, mcp_server_url: $url}' 2>/dev/null || printf '%s' "$input"
}
`
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
func computeCodexHookHash(event, command string) (string, error) {
	eventSnake := codexEventSnakeCase(event)
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
	approvals := make([]codexHookApproval, 0, len(CodexObservabilityHookEvents)+1)
	for _, event := range CodexObservabilityHookEvents {
		snake := codexEventSnakeCase(event)
		scripts := []string{codexHookScriptName(event)}
		if event == "SessionStart" {
			scripts = append([]string{"auth_preflight.sh"}, scripts...)
		}
		for hookIndex, script := range scripts {
			command := codexHookCommandString(marketplace, plugin, script)
			hash, err := computeCodexHookHash(event, command)
			if err != nil {
				return nil, fmt.Errorf("compute hash for %s hook %d: %w", event, hookIndex, err)
			}
			approvals = append(approvals, codexHookApproval{
				StateKey:    fmt.Sprintf(`%s@%s:hooks/hooks.json:%s:0:%d`, plugin, marketplace, snake, hookIndex),
				TrustedHash: hash,
			})
		}
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

def table_body_bounds(text, table_header):
    m = re.search(r'(?m)^' + re.escape(table_header) + r'(?:\s*(?:#.*)?)?(?:\n|$)', text)
    if not m:
        return None
    start = m.end()
    m2 = re.search(r'(?m)^\[', text[start:])
    end = start + m2.start() if m2 else len(text)
    return start, end

def ensure_table_entry(text, table_header, key, value):
    bounds = table_body_bounds(text, table_header)
    if bounds is None:
        return text.rstrip('\n') + '\n\n' + table_header + '\n' + key + ' = ' + value + '\n'
    if re.search(r'(?m)^' + re.escape(key) + r'\s*=', text[bounds[0]:bounds[1]]):
        return text
    prefix = text[:bounds[0]]
    if not prefix.endswith('\n'):
        prefix += '\n'
    return prefix + key + ' = ' + value + '\n' + text[bounds[0]:]

def has_table_header(text, header):
    return table_body_bounds(text, header) is not None

`)

	// Python literals — embedded at generation time, not expanded by bash.
	fmt.Fprintf(&b, "PLUGIN_KEY = %q\n", plugin)
	fmt.Fprintf(&b, "MARKETPLACE_KEY = %q\n\n", marketplace)

	b.WriteString(`content = strip_root_dotted_key(content, "features.hooks")
content = strip_root_dotted_key(content, "features.plugin_hooks")
content = ensure_table_entry(content, "[features]", "hooks", "true")
content = ensure_table_entry(content, "[features]", "plugin_hooks", "true")

# Qualified [hooks.state."…"] sections do not require a bare parent header.
if not has_table_header(content, "[hooks.state]") and not re.search(r'(?m)^\[hooks\.state\.', content):
    content = content.rstrip('\n') + '\n\n[hooks.state]\n'

for state_key, trusted_hash in [
`)

	for _, a := range approvals {
		fmt.Fprintf(&b, "    (%q, %q),\n", a.StateKey, a.TrustedHash)
	}

	b.WriteString(`]:
    section = f'[hooks.state."{state_key}"]'
    if not has_table_header(content, section):
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
		DisplayName: p.Name,
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
	DisplayName string `json:"displayName,omitempty"`
	Source      string `json:"source"`
	Description string `json:"description"`
}

type pluginAuthor struct {
	Name string `json:"name"`
	URL  string `json:"url"`
}

type claudePluginMeta struct {
	Name        string                     `json:"name"`
	DisplayName string                     `json:"displayName,omitempty"`
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
	// Timeout in seconds before Claude kills the hook command. nil uses
	// Claude's default (60s), which is too short for the interactive browser
	// login the SessionStart preflight can run.
	Timeout *int `json:"timeout,omitempty"`
}

type cursorHooksConfig struct {
	Version int                            `json:"version"`
	Hooks   map[string][]cursorHookCommand `json:"hooks"`
}

type cursorHookCommand struct {
	Command    string `json:"command"`
	Matcher    string `json:"matcher,omitempty"`
	Timeout    *int   `json:"timeout,omitempty"`
	FailClosed *bool  `json:"failClosed,omitempty"`
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
	// Newline-terminate: these bytes land on disk as files, and the checked-in
	// dogfood mirrors are formatted by hk (oxfmt), which enforces a trailing
	// newline -- the render must match byte-for-byte.
	return append(b, '\n'), nil
}
