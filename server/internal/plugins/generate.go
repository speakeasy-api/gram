package plugins

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"maps"
	"path"
	"slices"
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
	// hooks key, then fall back to this org-wide key embedded in speakeasy.json.
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
	// Version is the per-publish epoch (unix seconds) mixed into generated
	// plugin.json versions (see pluginManifestVersion and hooksManifestVersion)
	// so platform marketplaces (Claude Code, Cursor, Codex) treat regenerated
	// manifests as new and refresh installed copies. Empty pins deterministic
	// defaults for tests, fingerprints, and the CI render diff.
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
	// BrowserLogin lets the generated plugin mint per-user hooks keys via the
	// interactive dashboard browser flow (localhost callback token exchange).
	// Off by default: the runtime authenticates only through explicit
	// GRAM_HOOKS_* env credentials, a previously cached key, or the baked
	// org-wide key instead of opening a browser.
	BrowserLogin bool
	// HooksOrgName overrides the org name used to derive the observability
	// plugin directory slugs (Claude/Cursor/Codex). The publish path sets it to
	// the published hooks config's org name when it carries the hooks subtree
	// verbatim, so the always-regenerated shared marketplace manifests and
	// README keep referencing the carried directories even after an org rename.
	// Empty falls back to OrgName.
	HooksOrgName string
	// InstallFailOpen selects the org's binary install-failure policy. When
	// set, a bootstrap distribution failure (unreachable download origin,
	// missing tooling, lock-wait timeout, checksum mismatch) exits 0 so the
	// provider treats the hook as passed; off keeps the fail-closed default
	// where decision-capable hooks fail per provider semantics. It only covers
	// installation — a verified installed binary always runs, and a checksum
	// mismatch never executes the artifact under either policy.
	InstallFailOpen bool
}

// PublishedHooksFiles renders the complete hooks (observability) subtree the
// publish path rolls out — the Claude, Cursor, and Codex plugins with their
// manifests — under a pinned configuration, across every hook-mode and
// browser-login combination a publish can emit. CI compares
// this output between a PR's merge base and head to decide whether a
// hooksGeneratorVersion bump is required, so it must cover everything a
// publish emits: a Codex-only or manifest-only generator change has to
// surface here or it would never roll out to connected repos. Org-derived
// fields are fixed sentinels so only generator changes register, never data.
func PublishedHooksFiles() (map[string][]byte, error) {
	cfg := GenerateConfig{
		OrgName:           "Hooks Check",
		OrgEmail:          "hooks-check@example.com",
		OrgID:             "org-hooks-check",
		ServerURL:         "https://app.getgram.ai",
		APIKey:            fingerprintAPIKeySentinel,
		HooksAPIKey:       fingerprintHooksKeySentinel,
		ProjectSlug:       "hooks-check",
		IsDefaultProject:  true,
		Version:           "",
		MarketplaceName:   "",
		HooksOrgName:      "",
		ObservabilityMode: false,
		BrowserLogin:      false,
		InstallFailOpen:   false,
	}
	out := make(map[string][]byte)
	for _, mode := range []struct {
		prefix          string
		observability   bool
		browserLogin    bool
		installFailOpen bool
	}{
		{"default", false, false, false},
		{"observability-mode", true, false, false},
		{"browser-login", false, true, false},
		{"observability-mode-browser-login", true, true, false},
		{"install-fail-open", false, false, true},
	} {
		cfg.ObservabilityMode = mode.observability
		cfg.BrowserLogin = mode.browserLogin
		cfg.InstallFailOpen = mode.installFailOpen
		files, err := generateHooksFiles(cfg)
		if err != nil {
			return nil, fmt.Errorf("generate hooks files (%s): %w", mode.prefix, err)
		}
		for p, b := range files {
			out[path.Join(mode.prefix, p)] = b
		}
	}
	return out, nil
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
		HooksOrgName:      "",
		ObservabilityMode: false,
		// The dogfood harness is how the browser flow itself gets exercised
		// locally, so it stays on here regardless of the publish default.
		BrowserLogin:    true,
		InstallFailOpen: false,
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

// pluginManifestVersion returns the version to stamp into generated MCP
// plugin.json files: 0.1.<publish epoch>, which stays strictly above the
// historical 0.1.0 manifests already in users' caches so a re-publish is
// always seen as newer and triggers a refresh. The "0.1.0" fallback exists so
// package tests don't need to construct a version just to exercise unrelated
// assertions.
func pluginManifestVersion(cfg GenerateConfig) string {
	if cfg.Version == "" {
		return "0.1.0"
	}
	return "0.1." + cfg.Version
}

// hooksManifestVersion is the version stamped into the observability
// plugin.json on all three platforms: 0.<hooksGeneratorVersion>.<publish
// epoch>. The hooks subtree is only regenerated when its content changes — a
// hooksGeneratorVersion bump or an org-level hooks settings flip — and carried
// byte-identical otherwise, so the epoch patch component moves exactly when
// new hooks content publishes and installed copies refresh on settings-only
// republishes too. The minor component (9+) sorts strictly above the
// historical epoch-based 0.1.<epoch> hooks manifests already in users'
// caches, so any regenerated manifest is always seen as newer. Empty
// cfg.Version pins the deterministic 0.<hooksGeneratorVersion>.0 for tests
// and the CI render diff.
func hooksManifestVersion(cfg GenerateConfig) string {
	return "0." + hooksGeneratorVersion + "." + conv.Default(cfg.Version, "0")
}

// HooksConfig is the hook-output-affecting slice of GenerateConfig, captured at
// publish time (persisted as plugin_github_connections.published_hooks_config)
// so a later publish can tell whether the observability (hooks) subtree must be
// regenerated. hooksGeneratorVersion alone can't catch these: a marketplace
// rename or an observability-mode toggle changes the generated hook commands
// while leaving the version untouched, so without this snapshot the publish path
// carries a stale hooks subtree. It deliberately excludes per-publish secrets
// (HooksAPIKey, APIKey) and the manifest version (tracked separately via
// published_hooks_version) — only fields that change generated hook *content*
// belong here. Fields are the resolved/effective values that actually appear in
// output: the resolved marketplace name folds in the override, org name, project
// slug, and default-project flag, while OrgName/OrgEmail/ProjectSlug also appear
// directly in plugin metadata and runtime configuration. ServerURL/OrgID are
// written to the generated runtime configuration.
type HooksConfig struct {
	MarketplaceName string `json:"marketplace_name"`
	// OrgName's tag is also read by naming.PublishedHooksOrgName, which every
	// surface uses to name the published observability plugin after a rename.
	OrgName           string                       `json:"org_name"`
	OrgEmail          string                       `json:"org_email"`
	ProjectSlug       string                       `json:"project_slug"`
	ServerURL         string                       `json:"server_url"`
	OrgID             string                       `json:"org_id"`
	ObservabilityMode bool                         `json:"observability_mode"`
	BrowserLogin      bool                         `json:"browser_login"`
	InstallFailOpen   bool                         `json:"install_fail_open"`
	BinaryVersion     string                       `json:"binary_version"`
	BinaryTargets     map[string]hooksBinaryTarget `json:"binary_targets"`
}

// hooksConfigSnapshot extracts the hook-output-affecting config from cfg. The
// marketplace name is resolved (override or org default) so the snapshot records
// exactly what was baked into the generated hooks.
func hooksConfigSnapshot(cfg GenerateConfig) HooksConfig {
	return HooksConfig{
		MarketplaceName:   resolveMarketplaceName(cfg),
		OrgName:           cfg.OrgName,
		OrgEmail:          cfg.OrgEmail,
		ProjectSlug:       cfg.ProjectSlug,
		ServerURL:         cfg.ServerURL,
		OrgID:             cfg.OrgID,
		ObservabilityMode: cfg.ObservabilityMode,
		BrowserLogin:      cfg.BrowserLogin,
		InstallFailOpen:   cfg.InstallFailOpen,
		BinaryVersion:     hooksBinaryVersion,
		BinaryTargets:     hooksServedTargets(cfg.ServerURL),
	}
}

// marshalHooksConfig serializes a HooksConfig to the JSON stored in
// published_hooks_config. Go marshals struct fields in declaration order, so the
// bytes are deterministic for a given value.
func marshalHooksConfig(c HooksConfig) ([]byte, error) {
	b, err := json.Marshal(c)
	if err != nil {
		return nil, fmt.Errorf("marshal hooks config: %w", err)
	}
	return b, nil
}

// hooksConfigHash returns a stable sha256 over a HooksConfig, used to compare a
// freshly-computed snapshot against the one last published. Hashing the value
// (rather than raw stored bytes) normalizes away Postgres JSONB reserialization,
// which does not preserve key order or whitespace.
func hooksConfigHash(c HooksConfig) string {
	b, _ := json.Marshal(c)
	sum := sha256.Sum256(b)
	return "sha256:" + hex.EncodeToString(sum[:])
}

// storedHooksConfigHash decodes a published_hooks_config value and hashes it for
// comparison against a live snapshot. An empty (backfill) or unparseable value
// hashes to "" so the caller treats it as changed and regenerates once.
func storedHooksConfigHash(stored []byte) string {
	if len(stored) == 0 {
		return ""
	}
	var c HooksConfig
	if err := json.Unmarshal(stored, &c); err != nil {
		return ""
	}
	return hooksConfigHash(c)
}

// mcpGeneratorVersion is mixed into the MCP fingerprint (see MCPFingerprint).
// Bump it to force the automated rollout to republish every connected project's
// MCP plugins on the next run, even when a project's generated MCP output is
// byte-identical — for generator changes that alter MCP behaviour in ways the
// placeholder fingerprint pass can't observe.
const mcpGeneratorVersion = "9"

// hooksGeneratorVersion is the sole rollout signal for the observability (hooks)
// plugin. It is stamped into the hooks plugin.json version (see
// hooksManifestVersion) rather than folded into a content hash, so bumping it is
// the only thing that publishes a new hooks plugin to connected repos — an
// MCP-only publish leaves the existing hooks subtree untouched. Bump it for ANY
// change to hooks generation, including behaviour a fingerprint couldn't observe
// (e.g. the observability_mode toggle's effect on emitted hooks).
//
// The Plugin Generate Check CI workflow requires the relevant one of these two
// constants to change whenever generate.go does.
const hooksGeneratorVersion = "15"

// Fixed, non-empty sentinels substituted for the per-publish API keys when
// computing a fingerprint. They must be non-empty: an empty HooksAPIKey omits
// the observability plugin entirely (see GenerateConfig.HooksAPIKey), which
// would make the fingerprint blind to it. Constant values keep the generated
// bytes stable across publishes while the real keys rotate.
const (
	fingerprintAPIKeySentinel   = "gram_fingerprint_api_key"
	fingerprintHooksKeySentinel = "gram_fingerprint_hooks_key"
)

// mcpSharedFingerprintKey is the reserved key under which MCPFingerprints stores
// the hash of the shared marketplace/README files. Plugin slugs are kebab-case
// and never collide with it. When per-plugin publishing lands, shared files will
// be assembled per plugin and this reserved entry can be reworked away.
const mcpSharedFingerprintKey = "__shared__"

// MCPFingerprints returns per-plugin content fingerprints of the MCP (feature)
// plugins that would be generated for the given plugins — a map of plugin slug ->
// stable hash, plus a reserved mcpSharedFingerprintKey entry covering the shared
// marketplace/README files. It deliberately excludes the observability (hooks)
// subtree: that plugin rolls out on its own manual hooksGeneratorVersion signal,
// so MCP-content and hooks changes move independently.
//
// Fingerprints are stored per plugin (rather than as one aggregate) so a future
// per-plugin publish flow can decide independently which plugins have unpublished
// changes without a schema migration; today the rollout treats the MCP component
// as changed when any entry differs. Per-publish fields (manifest version, the
// injected MCP API key) are normalized out so the same MCP configuration and
// mcpGeneratorVersion always produce the same fingerprints.
func MCPFingerprints(plugins []PluginInfo, cfg GenerateConfig) (map[string]string, error) {
	cfg.Version = ""
	cfg.APIKey = fingerprintAPIKeySentinel
	// HooksAPIKey affects only the shared files here (whether the observability
	// entry is listed in marketplace.json). Published repos always carry a hooks
	// key, so pin a stable non-empty sentinel to keep that entry in the hash.
	cfg.HooksAPIKey = fingerprintHooksKeySentinel

	out := make(map[string]string, len(plugins)+1)
	for _, p := range plugins {
		files, err := generateMCPFiles([]PluginInfo{p}, cfg)
		if err != nil {
			return nil, fmt.Errorf("generate mcp files for plugin %s fingerprint: %w", p.Slug, err)
		}
		out[p.Slug] = hashFiles(mcpGeneratorVersion, files)
	}

	shared, err := generateSharedFiles(plugins, cfg)
	if err != nil {
		return nil, fmt.Errorf("generate shared files for fingerprint: %w", err)
	}
	out[mcpSharedFingerprintKey] = hashFiles(mcpGeneratorVersion, shared)

	return out, nil
}

// hashFiles returns a stable sha256 over a salt plus the sorted (path, content)
// pairs. A NUL after each component disambiguates boundaries so no concatenation
// of distinct (path, content) sequences can collide.
func hashFiles(salt string, files map[string][]byte) string {
	paths := make([]string, 0, len(files))
	for p := range files {
		paths = append(paths, p)
	}
	sort.Strings(paths)

	h := sha256.New()
	_, _ = h.Write([]byte(salt))
	_, _ = h.Write([]byte{0})
	for _, p := range paths {
		_, _ = h.Write([]byte(p))
		_, _ = h.Write([]byte{0})
		_, _ = h.Write(files[p])
		_, _ = h.Write([]byte{0})
	}
	return "sha256:" + hex.EncodeToString(h.Sum(nil))
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
// SessionStart is also blocking so a cold install completes while the
// session-start payload waits on stdin, and the plugin's first event is
// never lost.
//
// When observabilityMode is set, every event is forced async so the plugin
// can only observe and report — no hook can deny or delay a tool call, and a
// cold binary install happens in the background of the same invocation. This
// is the low-risk path for orgs that cannot tolerate hook errors or brief
// server unavailability disrupting the user, and it accepts the async-Stop
// dispatch gap above as part of that trade.
func claudeHookAsyncFlag(event string, observabilityMode bool) *bool {
	if observabilityMode {
		t := true
		return &t
	}
	switch event {
	case "UserPromptSubmit", "PreToolUse", "Stop", "SessionStart":
		f := false
		return &f
	default:
		t := true
		return &t
	}
}

// ClaudeObservabilityHookEvents are the Claude Code hook events the
// observability plugin registers against. Names match Claude's hooks.json
// schema. Claude only invokes the hooks runtime for events listed here, so any event
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
	"SubagentStop",
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

// GeneratePluginPackages produces the complete file map for a plugin
// distribution repository containing Claude Code, Cursor, and Codex plugins.
// Used for GitHub push. It is the union of the three independently-generatable
// components: the hooks (observability) subtree, the MCP (feature) plugin
// subtree, and the shared marketplace/README files. The publish path generates
// and pushes these components separately (see publishProject) so a change to one
// axis does not churn the other; this helper is the all-at-once composition used
// for first publishes, fingerprinting, and tests.
func GeneratePluginPackages(plugins []PluginInfo, cfg GenerateConfig) (map[string][]byte, error) {
	hooks, err := generateHooksFiles(cfg)
	if err != nil {
		return nil, err
	}
	mcp, err := generateMCPFiles(plugins, cfg)
	if err != nil {
		return nil, err
	}
	shared, err := generateSharedFiles(plugins, cfg)
	if err != nil {
		return nil, err
	}

	files := make(map[string][]byte, len(hooks)+len(mcp)+len(shared))
	maps.Copy(files, hooks)
	maps.Copy(files, mcp)
	maps.Copy(files, shared)
	return files, nil
}

// generateHooksFiles produces the per-org observability (hooks) plugin subtree
// for Claude, Cursor, and Codex. Returns an empty map when no hooks key is
// configured (cfg.HooksAPIKey == ""), which omits the observability plugin —
// typically only in tests that don't exercise the publish flow. This subtree
// rolls out independently of the MCP subtree on the hooksGeneratorVersion signal.
func generateHooksFiles(cfg GenerateConfig) (map[string][]byte, error) {
	files := make(map[string][]byte)
	if cfg.HooksAPIKey == "" {
		return files, nil
	}
	if err := generateClaudeObservabilityPlugin(files, cfg); err != nil {
		return nil, fmt.Errorf("generate claude observability plugin: %w", err)
	}
	if err := generateCursorObservabilityPlugin(files, cfg); err != nil {
		return nil, fmt.Errorf("generate cursor observability plugin: %w", err)
	}
	if err := generateCodexObservabilityPlugin(files, cfg); err != nil {
		return nil, fmt.Errorf("generate codex observability plugin: %w", err)
	}
	return files, nil
}

// writeHooksRuntimeFiles emits the files every observability plugin carries:
// the deployment-identity speakeasy.json and the pinned-binary bootstrap
// script. Only Codex additionally ships the PowerShell bootstrapper (via
// commandWindows); Claude and Cursor invoke hooks through bash on every
// platform.
func writeHooksRuntimeFiles(files map[string][]byte, subdir string, cfg GenerateConfig) error {
	configJSON, err := renderHooksConfig(cfg)
	if err != nil {
		return err
	}
	files[path.Join(subdir, "speakeasy.json")] = configJSON
	files[path.Join(subdir, "hooks/bootstrap.sh")] = renderHooksBootstrap(cfg)
	return nil
}

// mcpFilePaths returns the sorted set of repo paths the MCP (feature) plugin
// subtree occupies for the given plugins and config, used to carry the MCP files
// verbatim from the existing repo when only the hooks component changed.
func mcpFilePaths(plugins []PluginInfo, cfg GenerateConfig) ([]string, error) {
	cfg.APIKey = fingerprintAPIKeySentinel
	files, err := generateMCPFiles(plugins, cfg)
	if err != nil {
		return nil, fmt.Errorf("enumerate mcp file paths: %w", err)
	}
	return slices.Sorted(maps.Keys(files)), nil
}

// generateMCPFiles produces the feature (MCP) plugin subtree — one plugin per
// PluginInfo — for Claude, Cursor, and Codex.
func generateMCPFiles(plugins []PluginInfo, cfg GenerateConfig) (map[string][]byte, error) {
	files := make(map[string][]byte)
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
	}
	return files, nil
}

// generateSharedFiles produces the files that reference both the hooks and MCP
// plugins: the per-platform marketplace.json manifests and the README. They
// embed no per-publish secrets or versions, so they are deterministic given the
// plugin set and org config and are regenerated on every publish. The
// observability entry is listed first (so it's the first thing team admins see)
// and only when a hooks key is configured, matching generateHooksFiles.
func generateSharedFiles(plugins []PluginInfo, cfg GenerateConfig) (map[string][]byte, error) {
	files := make(map[string][]byte)

	claudePlugins := make([]marketplaceEntry, 0)
	cursorPlugins := make([]marketplaceEntry, 0)
	codexPlugins := make([]codexMarketplaceEntry, 0)

	if cfg.HooksAPIKey != "" {
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
// plugin's directory name and marketplace identifier from the org slug
// (HooksOrgName when the publish path carried an older subtree, see
// GenerateConfig). Namespacing per-org avoids collisions with user plugins
// that legitimately use slug "observability".
// Exported because tests need to predict the published path.
func ClaudeObservabilitySlug(cfg GenerateConfig) string {
	return naming.ObservabilitySlug(conv.Default(cfg.HooksOrgName, cfg.OrgName))
}
func CursorObservabilitySlug(cfg GenerateConfig) string {
	return conv.ToSlug(conv.Default(cfg.HooksOrgName, cfg.OrgName)) + "-observability-cursor"
}
func CodexObservabilitySlug(cfg GenerateConfig) string {
	return conv.ToSlug(conv.Default(cfg.HooksOrgName, cfg.OrgName)) + "-observability-codex"
}

// hooksSubtreePrefixes returns the repo directory prefixes the hooks
// (observability) subtree occupies for a given org name — every hooks
// generator version to date has kept each platform's observability plugin
// under these org-derived directories, so the publish path can carry the
// subtree verbatim by prefix without knowing which generator version produced
// it. The rollout gate depends on that layout independence to hold orgs on
// their published version.
func hooksSubtreePrefixes(orgName string) []string {
	return []string{
		naming.ObservabilitySlug(orgName) + "/",
		cursorPluginRoot + "/" + conv.ToSlug(orgName) + "-observability-cursor/",
		conv.ToSlug(orgName) + "-observability-codex/",
	}
}

// generateClaudeObservabilityPlugin emits the per-org observability plugin
// containing Gram hooks for Claude Code. The generated runtime configuration
// carries the org's hooks-scoped API key so no per-machine setup is required.
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
		Version:     hooksManifestVersion(cfg),
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
	// hooks manifest at the plugin root, Claude registers the plugin silently
	// but never wires the hooks up.
	//
	hookEvents := make(map[string][]claudeHookMatcher, len(ClaudeObservabilityHookEvents))
	for _, event := range ClaudeObservabilityHookEvents {
		timeoutSeconds := 60
		var timeout *int
		if event == "SessionStart" {
			timeoutSeconds = 300
			timeout = &timeoutSeconds
		}
		hooks := []claudeHookCommand{{Type: "command", Command: hooksBootstrapCommand(`$CLAUDE_PLUGIN_ROOT`, "claude-code", timeoutSeconds, false), Async: claudeHookAsyncFlag(event, cfg.ObservabilityMode), Timeout: timeout}}
		hookEvents[event] = []claudeHookMatcher{
			{Matcher: "", Hooks: hooks},
		}
	}
	hooksJSON, err := marshalJSON(claudeHooksConfig{Hooks: hookEvents})
	if err != nil {
		return fmt.Errorf("marshal hooks.json: %w", err)
	}
	files[path.Join(subdir, "hooks/hooks.json")] = hooksJSON

	if err := writeHooksRuntimeFiles(files, subdir, cfg); err != nil {
		return err
	}
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
		Version:     hooksManifestVersion(cfg),
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
	sessionStartTimeout := 330
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
			Command:    hooksBootstrapCommand(`$CURSOR_PLUGIN_ROOT`, "cursor", sessionStartTimeout, false),
			Matcher:    "",
			Timeout:    &sessionStartTimeout,
			FailClosed: hookFailClosed,
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
			Command:    hooksBootstrapCommand(`$CURSOR_PLUGIN_ROOT`, "cursor", 60, false),
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

	if err := writeHooksRuntimeFiles(files, subdir, cfg); err != nil {
		return err
	}
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
		Version:     hooksManifestVersion(cfg),
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

	hookEvents := make(map[string][]codexMatcherGroup, len(CodexObservabilityHookEvents))
	for _, event := range CodexObservabilityHookEvents {
		timeoutSeconds, async := codexHookParams(event, cfg.ObservabilityMode)
		hooks := []codexHookCommand{{
			Type:           "command",
			Command:        codexHookCommandString(timeoutSeconds, async),
			CommandWindows: codexHookCommandStringWindows(timeoutSeconds, async),
		}}
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

	if err := writeHooksRuntimeFiles(files, subdir, cfg); err != nil {
		return err
	}
	files[path.Join(subdir, "hooks/bootstrap.ps1")] = renderHooksPowerShellBootstrap(cfg)

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

// codexHookApproval is a single [hooks.state] entry that pre-approves a Codex
// hook event without requiring the user to click through Settings → Hooks.
// Codex hashes the platform-effective command — on Windows commandWindows
// replaces command before hashing (discovery.rs at the pinned commit) — so an
// approval carries one hash per platform and the install script picks by OS.
type codexHookApproval struct {
	StateKey           string
	TrustedHash        string
	TrustedHashWindows string
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
// The command keeps ${PLUGIN_ROOT} literal while hashing; Codex expands plugin
// variables only after trust verification.
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

// codexHookParams returns the timeout and relay --async flag for a Codex hook
// event. The command string embeds both, and Codex trust-hashes the command,
// so the hooks.json generator and the precomputed approvals must derive them
// from this single source. In observability mode every event runs async so no
// hook (or cold binary install) can delay the agent.
func codexHookParams(event string, observabilityMode bool) (timeoutSeconds int, async bool) {
	async = observabilityMode || event == "PostToolUse" || event == "Stop"
	timeoutSeconds = 60
	if event == "SessionStart" {
		timeoutSeconds = 330
	}
	return timeoutSeconds, async
}

// computeCodexHookApprovals returns pre-computed [hooks.state] entries for all
// Codex observability hook events for a given marketplace and plugin name.
func computeCodexHookApprovals(marketplace, plugin string, observabilityMode bool) ([]codexHookApproval, error) {
	approvals := make([]codexHookApproval, 0, len(CodexObservabilityHookEvents))
	for _, event := range CodexObservabilityHookEvents {
		snake := codexEventSnakeCase(event)
		timeoutSeconds, async := codexHookParams(event, observabilityMode)
		hash, err := computeCodexHookHash(event, codexHookCommandString(timeoutSeconds, async))
		if err != nil {
			return nil, fmt.Errorf("compute hash for %s hook: %w", event, err)
		}
		windowsHash, err := computeCodexHookHash(event, codexHookCommandStringWindows(timeoutSeconds, async))
		if err != nil {
			return nil, fmt.Errorf("compute windows hash for %s hook: %w", event, err)
		}
		approvals = append(approvals, codexHookApproval{
			StateKey:           fmt.Sprintf(`%s@%s:hooks/hooks.json:%s:0:0`, plugin, marketplace, snake),
			TrustedHash:        hash,
			TrustedHashWindows: windowsHash,
		})
	}
	return approvals, nil
}

// codexHookCommandString / codexHookCommandStringWindows are the single source
// for the command strings emitted into the Codex hooks.json (command /
// commandWindows) AND hashed into the precomputed approvals — Codex hashes the
// command string verbatim, so any drift between the two call sites silently
// untrusts every hook.
func codexHookCommandString(timeoutSeconds int, async bool) string {
	return hooksBootstrapCommand(`${PLUGIN_ROOT}`, "codex", timeoutSeconds, async)
}

func codexHookCommandStringWindows(timeoutSeconds int, async bool) string {
	return hooksPowerShellCommand(`${PLUGIN_ROOT}`, "codex", timeoutSeconds, async)
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

	approvals, err := computeCodexHookApprovals(marketplace, plugin, cfg.ObservabilityMode)
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
	// the managed-install and app-bundle locations.
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
		b.WriteString(`# ── 1. Register marketplace & install plugin ────────────────────────────────
echo "→ Registering Speakeasy marketplace..."
if [ -n "${CODEX_BIN}" ]; then
  # add is idempotent (no-ops if already registered); upgrade pulls any new commits.
  "${CODEX_BIN}" plugin marketplace add "${MARKETPLACE_URL}" || true
  "${CODEX_BIN}" plugin marketplace upgrade "${MARKETPLACE_KEY}"
  # Registering the marketplace only makes the plugin available; it must also
  # be installed. plugin add is idempotent.
  "${CODEX_BIN}" plugin add "${PLUGIN_KEY}@${MARKETPLACE_KEY}"
else
  echo "  ⚠  'codex' executable not found."
  echo "     Run manually: codex plugin marketplace add '${MARKETPLACE_URL}'"
  echo "     Then:         codex plugin add '${PLUGIN_KEY}@${MARKETPLACE_KEY}'"
fi

`)
	} else {
		b.WriteString(`# ── 1. Register marketplace & install plugin (local ZIP install) ────────────
SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
echo "→ Registering Speakeasy marketplace from ${SCRIPT_DIR}..."
if [ -n "${CODEX_BIN}" ]; then
  "${CODEX_BIN}" plugin marketplace add "${SCRIPT_DIR}" || true
  # Registering the marketplace only makes the plugin available; it must also
  # be installed. plugin add is idempotent.
  "${CODEX_BIN}" plugin add "${PLUGIN_KEY}@${MARKETPLACE_KEY}"
else
  echo "  ⚠  'codex' executable not found."
  echo "     Run manually: codex plugin marketplace add '${SCRIPT_DIR}'"
  echo "     Then:         codex plugin add '${PLUGIN_KEY}@${MARKETPLACE_KEY}'"
fi

`)
	}

	// Step 2: patch ~/.codex/config.toml via embedded Python (always available
	// on macOS, where Codex runs). Uses a quoted heredoc (<<'PYTHON') so bash
	// does not expand $PLUGIN_KEY etc. inside the Python source.
	b.WriteString(`# ── 2. Patch ~/.codex/config.toml ────────────────────────────────────────────
echo "→ Configuring ~/.codex/config.toml..."
# Codex hashes the platform-effective hook command (commandWindows replaces
# command on Windows before hashing), so the trusted hash to install differs
# per platform. The quoted heredoc hides bash variables from Python; pass the
# platform through the environment.
case "$(uname -s)" in
  MINGW*|MSYS*|CYGWIN*) SPEAKEASY_HOOKS_OS=windows ;;
  *) SPEAKEASY_HOOKS_OS=unix ;;
esac
export SPEAKEASY_HOOKS_OS
python3 - <<'PYTHON'
import os, re

WINDOWS = os.environ.get("SPEAKEASY_HOOKS_OS") == "windows"

config_path = os.path.expanduser("~/.codex/config.toml")
os.makedirs(os.path.dirname(config_path), exist_ok=True)
content = open(config_path).read() if os.path.exists(config_path) else ""

# TOML permits leading whitespace before keys and table headers, and Codex's
# own config writer emits indented entries — every line anchor below must
# tolerate it or a re-install inserts duplicates into an indented config,
# which fails to parse and takes Codex down entirely.

# A root-level dotted key implicitly defines its parent table and conflicts
# with an explicit [table] header elsewhere in the file. Only the region
# before the first table header is touched.
def strip_root_dotted_key(text, key):
    m = re.search(r'(?m)^[ \t]*\[', text)
    root, rest = (text[:m.start()], text[m.start():]) if m else (text, "")
    root = re.sub(r'(?m)^[ \t]*' + re.escape(key) + r'\s*=.*\n?', '', root)
    return root + rest

def table_body_bounds(text, table_header):
    m = re.search(r'(?m)^[ \t]*' + re.escape(table_header) + r'(?:\s*(?:#.*)?)?(?:\n|$)', text)
    if not m:
        return None
    start = m.end()
    m2 = re.search(r'(?m)^[ \t]*\[', text[start:])
    end = start + m2.start() if m2 else len(text)
    return start, end

def ensure_table_entry(text, table_header, key, value):
    bounds = table_body_bounds(text, table_header)
    if bounds is None:
        return text.rstrip('\n') + '\n\n' + table_header + '\n' + key + ' = ' + value + '\n'
    if re.search(r'(?m)^[ \t]*' + re.escape(key) + r'\s*=', text[bounds[0]:bounds[1]]):
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
if not has_table_header(content, "[hooks.state]") and not re.search(r'(?m)^[ \t]*\[hooks\.state\.', content):
    content = content.rstrip('\n') + '\n\n[hooks.state]\n'

for state_key, trusted_hash, trusted_hash_windows in [
`)

	for _, a := range approvals {
		fmt.Fprintf(&b, "    (%q, %q, %q),\n", a.StateKey, a.TrustedHash, a.TrustedHashWindows)
	}

	b.WriteString(`]:
    if WINDOWS:
        trusted_hash = trusted_hash_windows
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

// commandWindows is supported by Codex hook_config.rs at
// 5bed6447998c754d154dbd796517310b8f04d4ce. On Windows it replaces command
// before execution. Codex substitutes plugin variables only in the ${KEY}
// textual form (discovery.rs), so commandWindows must use ${PLUGIN_ROOT},
// never PowerShell-only $env: expansion. Trust hashing happens after that
// replacement (discovery.rs normalizes command_windows into command before
// hashing), so Windows machines verify against a hash of the commandWindows
// string — precomputed approvals carry both hashes and the install script
// selects by platform.
type codexHookCommand struct {
	Type           string `json:"type"`
	Command        string `json:"command"`
	CommandWindows string `json:"commandWindows,omitempty"`
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
