package plugins

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"maps"
	"path"
	"slices"
	"sort"
	"strings"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/speakeasy-api/gram/server/internal/attr"
	"github.com/speakeasy-api/gram/server/internal/oops"
	"github.com/speakeasy-api/gram/server/internal/plugins/repo"
	projectsrepo "github.com/speakeasy-api/gram/server/internal/projects/repo"
	ghclient "github.com/speakeasy-api/gram/server/internal/thirdparty/github"
)

// pluginHasUnsyncedChangesMessage refuses turning auto sync off while the
// plugin's live state differs from what was last published. Holding the plugin
// from then on would strand those changes with nothing on screen saying so.
const pluginHasUnsyncedChangesMessage = "This plugin has changes that haven't been synced yet. Sync changes first, then turn off auto sync."

// ErrPluginHasUnsyncedChanges is the cause of the refusal carrying
// pluginHasUnsyncedChangesMessage, so a caller outside the management API can
// tell it apart from a failed check.
var ErrPluginHasUnsyncedChanges = errors.New("plugin has unsynced changes")

// autoSyncOffWithOtherChangesMessage refuses turning auto sync off in the same
// update that edits the plugin: the edit itself would be an unsynced change.
const autoSyncOffWithOtherChangesMessage = "Turn off auto sync on its own, without other changes to the plugin."

// pausedPluginPlan is how an automatic publish treats the project's plugins
// once those whose auto sync is off ("paused") are accounted for.
type pausedPluginPlan struct {
	// Active are the plugins generated from their live state, sorted by slug.
	// A paused plugin with nothing unsynced is active: regenerating it
	// produces what is already published.
	Active []PluginInfo

	// Held are paused plugins with unsynced changes. The publish copies the
	// files they were last published with and keeps their stored fingerprint.
	Held []heldPlugin

	// Omitted are slugs of paused plugins that were never published. The
	// publish leaves them out until someone syncs by hand.
	Omitted []string
}

// heldPlugin is a paused plugin an automatic publish keeps exactly as it was
// last published.
type heldPlugin struct {
	Slug string

	// Fingerprint is the plugin's stored published MCP fingerprint.
	Fingerprint string

	// Files are the plugin's published files, copied from the repo.
	Files map[string][]byte

	// Entry is the plugin's marketplace manifest entry, rebuilt from Files.
	Entry sharedPluginEntry
}

// allPluginsActive is the plan for a publish that includes every plugin's
// pending changes: the manual sync, and any project with no paused plugin.
func allPluginsActive(live []PluginInfo) pausedPluginPlan {
	return pausedPluginPlan{Active: live, Held: nil, Omitted: nil}
}

// Slugs lists every plugin this publish puts in the repo.
func (p pausedPluginPlan) Slugs() []string {
	slugs := make([]string, 0, len(p.Active)+len(p.Held))
	for _, info := range p.Active {
		slugs = append(slugs, info.Slug)
	}
	for _, held := range p.Held {
		slugs = append(slugs, held.Slug)
	}
	sort.Strings(slugs)
	return slugs
}

// planPausedPlugins splits the project's live plugins for an automatic
// publish. A paused plugin with a stored fingerprint whose live state has moved
// on is held: its published files are copied from the repo. One never
// published is omitted. When a held plugin's files can't be read from the repo,
// it falls back to its live state for this publish, as the MCP carry does,
// because leaving it out would remove it from the marketplace.
//
// fingerprintCfg must be the publish's generation config: the live
// fingerprints computed here are compared against what earlier publishes
// stored under the same config.
func (s *Service) planPausedPlugins(
	ctx context.Context,
	projectID uuid.UUID,
	repoOwner, repoName string,
	live []PluginInfo,
	published map[string]string,
	fingerprintCfg GenerateConfig,
	includeObservability bool,
) (pausedPluginPlan, error) {
	plan, pending, _, err := s.classifyPausedPlugins(ctx, projectID, live, published, fingerprintCfg, includeObservability)
	if err != nil || len(pending) == 0 {
		return plan, err
	}
	return s.holdPendingPlugins(ctx, projectID, repoOwner, repoName, live, published, plan, pending, false)
}

// classifyPausedPlugins is the part of planPausedPlugins that needs no repo
// files: it returns the plan without the plugins still to hold, which come
// back as pending, and the slugs of every paused plugin.
func (s *Service) classifyPausedPlugins(
	ctx context.Context,
	projectID uuid.UUID,
	live []PluginInfo,
	published map[string]string,
	fingerprintCfg GenerateConfig,
	includeObservability bool,
) (pausedPluginPlan, map[string]PluginInfo, map[string]bool, error) {
	pausedSlugs, err := s.repo.ListAutoSyncPausedPluginSlugs(ctx, projectID)
	if err != nil {
		return pausedPluginPlan{}, nil, nil, oops.E(oops.CodeUnexpected, err, "list plugins with auto sync off").LogError(ctx, s.logger)
	}
	paused := make(map[string]bool, len(pausedSlugs))
	for _, slug := range pausedSlugs {
		paused[slug] = true
	}
	if len(paused) == 0 {
		return allPluginsActive(live), nil, paused, nil
	}

	plan := pausedPluginPlan{Active: make([]PluginInfo, 0, len(live)), Held: nil, Omitted: nil}
	pending := make(map[string]PluginInfo)
	for _, info := range live {
		if !paused[info.Slug] {
			plan.Active = append(plan.Active, info)
			continue
		}
		stored, ok := published[info.Slug]
		if !ok {
			plan.Omitted = append(plan.Omitted, info.Slug)
			continue
		}
		current, err := PluginMCPFingerprint(info, fingerprintCfg, includeObservability)
		if err != nil {
			return pausedPluginPlan{}, nil, nil, oops.E(oops.CodeUnexpected, err, "compute paused plugin fingerprint").LogError(ctx, s.logger)
		}
		if current == stored {
			plan.Active = append(plan.Active, info)
			continue
		}
		pending[info.Slug] = info
	}
	return plan, pending, paused, nil
}

// holdPendingPlugins finishes planPausedPlugins: it reads each pending
// plugin's published files from the repo and holds it, or falls back to its
// live state when its files are unreadable or it shares a directory with
// another plugin. entriesOnly reads just the files its manifest entry comes
// from, for a caller that never pushes the held files.
func (s *Service) holdPendingPlugins(
	ctx context.Context,
	projectID uuid.UUID,
	repoOwner, repoName string,
	live []PluginInfo,
	published map[string]string,
	plan pausedPluginPlan,
	pending map[string]PluginInfo,
	entriesOnly bool,
) (pausedPluginPlan, error) {
	if s.github == nil {
		return pausedPluginPlan{}, oops.E(oops.CodeUnavailable, nil, "github publishing is not configured").LogError(ctx, s.logger)
	}
	owners := ownersOfPlugins(live)
	existing, err := s.github.Client.GetRepoFiles(ctx, s.github.InstallationID, repoOwner, repoName, "main", func(filePath string) bool {
		owner, ok := owners.ownerOf(filePath)
		_, held := pending[owner]
		return ok && held && (!entriesOnly || slices.Contains(heldEntryPaths(owner), filePath))
	})
	switch {
	case errors.Is(err, ghclient.ErrRepoNotFound):
		existing = nil
	case err != nil:
		return pausedPluginPlan{}, oops.E(oops.CodeGatewayError, err, "get published files of plugins with auto sync off").LogError(ctx, s.logger)
	}

	for _, slug := range slices.Sorted(maps.Keys(pending)) {
		if slices.ContainsFunc(pluginSubtreePrefixes(slug), func(prefix string) bool { return owners[prefix] != slug }) {
			s.logger.WarnContext(ctx, "a plugin with auto sync off shares a package directory with another plugin; publishing its current state instead",
				attr.SlogProjectID(projectID.String()),
			)
			plan.Active = append(plan.Active, pending[slug])
			continue
		}
		files := make(map[string][]byte)
		for filePath, content := range existing {
			if owner, ok := owners.ownerOf(filePath); ok && owner == slug {
				files[filePath] = content
			}
		}
		entry, err := heldPluginEntry(slug, files)
		if err != nil {
			s.logger.WarnContext(ctx, "published files of a plugin with auto sync off are unreadable; publishing its current state instead",
				attr.SlogProjectID(projectID.String()),
				attr.SlogError(err),
			)
			plan.Active = append(plan.Active, pending[slug])
			continue
		}
		plan.Held = append(plan.Held, heldPlugin{Slug: slug, Fingerprint: published[slug], Files: files, Entry: entry})
	}
	sort.SliceStable(plan.Active, func(i, j int) bool { return plan.Active[i].Slug < plan.Active[j].Slug })
	return plan, nil
}

// pluginPrefixOwners maps each plugin directory prefix in the repo to the slug
// that owns it.
type pluginPrefixOwners map[string]string

// ownersOfPlugins assigns each plugin directory to its plugin. The metadata
// core refuses a slug whose directories another plugin uses, but plugins
// created before that check or outside it (role distribution names plugins
// after roles) may share one: plugin foo's Codex package and plugin
// foo-codex's Claude package are then both foo-codex/. Such a directory
// belongs to neither, since both write into it, so neither can be held and
// both publish their live state.
func ownersOfPlugins(plugins []PluginInfo) pluginPrefixOwners {
	owners := make(pluginPrefixOwners)
	shared := make(map[string]bool)
	for _, p := range plugins {
		for _, prefix := range pluginSubtreePrefixes(p.Slug) {
			if _, claimed := owners[prefix]; claimed {
				shared[prefix] = true
			}
			owners[prefix] = p.Slug
		}
	}
	for prefix := range shared {
		delete(owners, prefix)
	}
	return owners
}

// ownerOf returns the plugin whose directory holds filePath. The longest
// matching prefix wins, so a slug that happens to equal another plugin's
// directory name (say, cursor-plugins) never claims that plugin's files.
func (o pluginPrefixOwners) ownerOf(filePath string) (string, bool) {
	best := ""
	for prefix := range o {
		if strings.HasPrefix(filePath, prefix) && len(prefix) > len(best) {
			best = prefix
		}
	}
	if best == "" {
		return "", false
	}
	return o[best], true
}

// heldEntryPaths are the published files heldPluginEntry reads or requires.
func heldEntryPaths(slug string) []string {
	return []string{
		path.Join(slug, ".claude-plugin/plugin.json"),
		path.Join(slug, ".mcp.json"),
		path.Join(cursorPluginRoot, slug+"-cursor", ".cursor-plugin/plugin.json"),
		path.Join(cursorPluginRoot, slug+"-cursor", "mcp.json"),
		path.Join(slug+"-codex", ".codex-plugin/plugin.json"),
		path.Join(slug+"-codex", ".mcp.json"),
		path.Join(agentPluginRoot, slug, "plugin.json"),
	}
}

// heldPluginEntry rebuilds a plugin's marketplace manifest entry from the files
// it was last published with, so the regenerated shared manifests list it as
// published. It fails when a file the manifests point at, or that the entry is
// read from, is missing or malformed.
func heldPluginEntry(slug string, files map[string][]byte) (sharedPluginEntry, error) {
	var claudeMeta struct {
		Name        string `json:"name"`
		DisplayName string `json:"displayName"`
		Description string `json:"description"`
	}
	if err := decodeHeldFile(files, path.Join(slug, ".claude-plugin/plugin.json"), &claudeMeta); err != nil {
		return sharedPluginEntry{}, err
	}
	if claudeMeta.Name != slug || claudeMeta.DisplayName == "" {
		return sharedPluginEntry{}, fmt.Errorf("published Claude plugin.json for %s does not name the plugin", slug)
	}

	var claudeMCP struct {
		MCPServers map[string]json.RawMessage `json:"mcpServers"`
	}
	if err := decodeHeldFile(files, path.Join(slug, ".mcp.json"), &claudeMCP); err != nil {
		return sharedPluginEntry{}, err
	}

	// The Cursor and Codex manifests point at these packages, so a copy
	// without them would list a plugin that cannot install.
	for _, required := range []string{
		path.Join(cursorPluginRoot, slug+"-cursor", ".cursor-plugin/plugin.json"),
		path.Join(cursorPluginRoot, slug+"-cursor", "mcp.json"),
		path.Join(slug+"-codex", ".codex-plugin/plugin.json"),
	} {
		if _, ok := files[required]; !ok {
			return sharedPluginEntry{}, fmt.Errorf("published file %s is missing", required)
		}
	}

	// codexAuthPolicy asks for ON_INSTALL when the Codex package collects a
	// credential, which shows as env_http_headers (a public server's env vars)
	// or bearer_token_env_var (a private server with no API key baked in). It
	// doesn't skip OAuth servers, whose entries carry neither field, so a held
	// plugin whose only such server is a public OAuth one with env vars lists
	// as ON_USE.
	var codexMCP codexMCPConfig
	if err := decodeHeldFile(files, path.Join(slug+"-codex", ".mcp.json"), &codexMCP); err != nil {
		return sharedPluginEntry{}, err
	}
	codexAuthentication := "ON_USE"
	for _, server := range codexMCP.MCPServers {
		if server.BearerTokenEnvVar != "" || len(server.EnvHTTPHeaders) > 0 {
			codexAuthentication = "ON_INSTALL"
			break
		}
	}

	_, agentPackage := files[path.Join(agentPluginRoot, slug, "plugin.json")]

	return sharedPluginEntry{
		Slug:                slug,
		Name:                claudeMeta.DisplayName,
		Description:         claudeMeta.Description,
		ServerCount:         len(claudeMCP.MCPServers),
		CodexAuthentication: codexAuthentication,
		AgentPluginsPackage: agentPackage,
	}, nil
}

func decodeHeldFile(files map[string][]byte, filePath string, dst any) error {
	content, ok := files[filePath]
	if !ok {
		return fmt.Errorf("published file %s is missing", filePath)
	}
	if err := json.Unmarshal(content, dst); err != nil {
		return fmt.Errorf("parse published file %s: %w", filePath, err)
	}
	return nil
}

// sharedPublishEntries lists the marketplace manifest entries of every plugin
// a publish puts in the repo. Held plugins keep the entries they were last
// published with and take their place in slug order.
func sharedPublishEntries(active []PluginInfo, held []heldPlugin, cfg GenerateConfig) []sharedPluginEntry {
	entries := make([]sharedPluginEntry, 0, len(active)+len(held))
	for _, p := range active {
		entries = append(entries, sharedPluginEntryFor(p, cfg))
	}
	if len(held) == 0 {
		return entries
	}
	for _, h := range held {
		entries = append(entries, h.Entry)
	}
	sort.SliceStable(entries, func(i, j int) bool { return entries[i].Slug < entries[j].Slug })
	return entries
}

// publishMCPFingerprints returns the content fingerprints a publish records
// for its MCP (feature) plugins: a map of plugin slug to stable hash, plus a
// reserved mcpSharedFingerprintKey entry covering the shared marketplace and
// README files this publish would push. Active plugins are fingerprinted from
// their live state; held plugins keep their stored fingerprint. It excludes the
// observability (hooks) subtree, which rolls out on its own
// hooksGeneratorVersion signal.
//
// includeObservability must mirror the project's observability setting: the
// shared manifests list the observability entry only when it is on. Per-publish
// fields (manifest version and injected API keys) are normalized out, so the
// same MCP configuration and mcpGeneratorVersion always fingerprint the same.
func publishMCPFingerprints(active []PluginInfo, held []heldPlugin, cfg GenerateConfig, includeObservability bool) (map[string]string, error) {
	cfg = fingerprintConfig(cfg, includeObservability)

	out := make(map[string]string, len(active)+len(held)+1)
	for _, p := range active {
		fingerprint, err := pluginMCPFingerprint(p, cfg)
		if err != nil {
			return nil, err
		}
		out[p.Slug] = fingerprint
	}
	for _, h := range held {
		out[h.Slug] = h.Fingerprint
	}
	shared, err := generateSharedFilesFromEntries(sharedPublishEntries(active, held, cfg), cfg)
	if err != nil {
		return nil, fmt.Errorf("generate shared files for fingerprint: %w", err)
	}
	out[mcpSharedFingerprintKey] = hashFiles(mcpGeneratorVersion, shared)
	return out, nil
}

// requirePluginSyncedForPause refuses turning a plugin's auto sync off while
// its live state differs from what was last published. A plugin that was
// never published, or a project with no marketplace yet, has nothing to
// compare against, so turning auto sync off is always allowed there. The live
// fingerprint is computed with the same configuration publishProject uses.
// It reads the plugin's live state in tx. lockContent first takes the package
// content lock exclusively, which the check that decides must do so no change
// commits unseen before its write; it waits only once there is something to
// compare.
func (s *Service) requirePluginSyncedForPause(ctx context.Context, tx pgx.Tx, plugin repo.Plugin, lockContent bool) error {
	conn, err := s.repo.WithTx(tx).GetGitHubConnection(ctx, plugin.ProjectID)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil
	}
	if err != nil {
		return oops.E(oops.CodeUnexpected, err, "get github connection").LogError(ctx, s.logger)
	}
	stored, ok := decodeMCPFingerprints(conn.PublishedMcpFingerprints)[plugin.Slug]
	if !ok {
		// Only a successful publish moves the connection's updated_at, and
		// every publish includes each plugin whose auto sync is on. A plugin
		// that already existed then but is missing under its current slug was
		// renamed since, so its published files sit under the old slug: once
		// paused, it would drop out of the marketplace. Otherwise it was never
		// published and has nothing to strand.
		if conn.UpdatedAt.Valid && plugin.CreatedAt.Valid && plugin.CreatedAt.Time.Before(conn.UpdatedAt.Time) {
			return oops.E(oops.CodeFailedPrecondition, ErrPluginHasUnsyncedChanges, "%s", pluginHasUnsyncedChangesMessage)
		}
		return nil
	}

	project, err := projectsrepo.New(tx).GetProjectWithOrganizationMetadata(ctx, plugin.ProjectID)
	if err != nil {
		return oops.E(oops.CodeUnexpected, err, "get project").LogError(ctx, s.logger)
	}
	serverURL := s.organizationServerURL(project.DefaultHost)
	observabilityEnabled, err := s.projectObservabilityEnabled(ctx, plugin.ProjectID)
	if err != nil {
		return oops.E(oops.CodeUnexpected, err, "read observability plugin setting").LogError(ctx, s.logger)
	}
	cfg := s.generateConfig(ctx, project.ID, project.Slug, project.ProjectSlug, plugin.ProjectID, serverURL, conn.PublishedHooksConfig)

	if lockContent {
		if err := s.repo.WithTx(tx).LockPluginPackageContent(ctx, plugin.ProjectID.String()); err != nil {
			return oops.E(oops.CodeUnexpected, err, "lock plugin package content").LogError(ctx, s.logger)
		}
	}
	infos, err := s.resolvePluginInfosInTx(ctx, tx, project.ID, plugin.ProjectID, serverURL, plugin.ID)
	if err != nil {
		return err
	}
	idx := slices.IndexFunc(infos, func(info PluginInfo) bool { return info.Slug == plugin.Slug })
	if idx < 0 {
		return oops.C(oops.CodeNotFound)
	}
	current, err := PluginMCPFingerprint(infos[idx], cfg, observabilityEnabled)
	if err != nil {
		return oops.E(oops.CodeUnexpected, err, "compute plugin fingerprint").LogError(ctx, s.logger)
	}
	if current != stored {
		return oops.E(oops.CodeFailedPrecondition, ErrPluginHasUnsyncedChanges, "%s", pluginHasUnsyncedChangesMessage)
	}
	return nil
}
