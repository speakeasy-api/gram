package plugins

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"maps"
	"slices"
	"sort"
	"time"

	redisCache "github.com/go-redis/cache/v9"
	"github.com/google/uuid"

	"github.com/speakeasy-api/gram/server/internal/attr"
	"github.com/speakeasy-api/gram/server/internal/conv"
	"github.com/speakeasy-api/gram/server/internal/plugins/repo"
)

// marketplaceFreshness compares a project's published marketplace with what
// an automatic publish would push now. The dashboard and the Platform MCP both
// read it, so a plugin reads as current or not the same way on each.
type marketplaceFreshness struct {
	// Plugins maps each live plugin's slug to whether its published package
	// is current. A plugin whose auto sync is on is current when an automatic
	// publish would push nothing new for it: its own package and the shared
	// manifests, generated with paused plugins held, match what was
	// published. A plugin whose auto sync is off is current when its own
	// package matches what it was last published with; one never published
	// under its slug is not.
	Plugins map[string]bool

	// Paused holds the slugs of plugins whose auto sync is off.
	Paused map[string]bool

	// AutomaticPublishPending reports that an automatic publish would change
	// the marketplace. It stays false while the only pending changes belong to
	// plugins whose auto sync is off.
	AutomaticPublishPending bool
}

// UpToDate reports that nothing waits to publish, automatically or by hand.
func (f marketplaceFreshness) UpToDate() bool {
	if f.AutomaticPublishPending {
		return false
	}
	for _, fresh := range f.Plugins {
		if !fresh {
			return false
		}
	}
	return true
}

// AwaitingManualSync reports that slug's auto sync is off and its own package
// has changes waiting, or was never published: only a manual sync publishes it.
func (f marketplaceFreshness) AwaitingManualSync(slug string) bool {
	return f.Paused[slug] && !f.Plugins[slug]
}

// resolveMarketplaceFreshness plans an automatic publish of infos, the
// project's live plugins, the way publishProject would, and compares it with
// what conn records as published. A paused plugin with changes waiting is
// held, so its manifest entry is read from the repo; it only changes with a
// publish, so it is cached per published state.
func (s *Service) resolveMarketplaceFreshness(
	ctx context.Context,
	organizationID, organizationSlug, projectSlug string,
	projectID uuid.UUID,
	infos []PluginInfo,
	serverURL string,
	conn repo.PluginGithubConnection,
	observabilityEnabled bool,
) (marketplaceFreshness, error) {
	cfg := s.generateConfig(ctx, organizationID, organizationSlug, projectSlug, projectID, serverURL, conn.PublishedHooksConfig)
	published := decodeMCPFingerprints(conn.PublishedMcpFingerprints)
	plan, pending, paused, err := s.classifyPausedPlugins(ctx, projectID, infos, published, cfg, observabilityEnabled)
	if err != nil {
		return marketplaceFreshness{}, err
	}
	if len(pending) > 0 {
		plan = s.holdPendingPluginEntries(ctx, projectID, conn, infos, published, plan, pending)
	}

	// A carried hooks subtree keeps the directory names it was published
	// under, and the shared manifests point at them, as publishProject
	// fingerprints them.
	hooksCurrent := s.hooksPublishCurrent(ctx, organizationID, organizationSlug, conn, cfg, observabilityEnabled)
	fingerprintCfg := cfg
	if observabilityEnabled && hooksCurrent {
		fingerprintCfg.HooksOrgName, _ = hooksCarryPrefixes(conn.PublishedHooksConfig, cfg.OrgName)
	}
	automatic, err := publishMCPFingerprints(plan.Active, plan.Held, fingerprintCfg, observabilityEnabled)
	if err != nil {
		return marketplaceFreshness{}, fmt.Errorf("compute automatic publish fingerprints: %w", err)
	}

	return marketplaceFreshness{
		Plugins:                 pluginFreshness(infos, paused, plan.Held, automatic, published),
		Paused:                  paused,
		AutomaticPublishPending: !maps.Equal(automatic, published) || !hooksCurrent,
	}, nil
}

// pluginFreshness decides each plugin's freshness from automatic, what an
// automatic publish would record, and published, what the last publish did.
// A plugin with auto sync on needs its own and the shared fingerprints to
// match; a paused one only its own, and a held one has changes waiting
// whatever its recorded fingerprint says. A plugin never published under its
// slug has no recorded fingerprint, so it is never fresh.
func pluginFreshness(infos []PluginInfo, paused map[string]bool, held []heldPlugin, automatic, published map[string]string) map[string]bool {
	waiting := make(map[string]bool, len(held))
	for _, h := range held {
		waiting[h.Slug] = true
	}
	publishedShared, hasPublishedShared := published[mcpSharedFingerprintKey]
	sharedCurrent := hasPublishedShared && automatic[mcpSharedFingerprintKey] == publishedShared

	fresh := make(map[string]bool, len(infos))
	for _, info := range infos {
		stored, ok := published[info.Slug]
		current := ok && automatic[info.Slug] == stored && !waiting[info.Slug]
		if !paused[info.Slug] {
			current = current && sharedCurrent
		}
		fresh[info.Slug] = current
	}
	return fresh
}

// hooksPublishCurrent reports whether the published hooks (observability)
// subtree is what a publish would push. It counts only for an org cleared for
// the current generator version: publishProject carries a gated org's
// published subtree verbatim, so for a gated org the published hooks are its
// target, and a hook-affecting config change while gated applies by itself
// once the rollout advances. A project that turned observability off is
// current once no hooks subtree remains published.
func (s *Service) hooksPublishCurrent(ctx context.Context, organizationID, organizationSlug string, conn repo.PluginGithubConnection, cfg GenerateConfig, observabilityEnabled bool) bool {
	if !observabilityEnabled {
		return conv.FromPGTextOrEmpty[string](conn.PublishedHooksVersion) == ""
	}
	if !s.hooksRolloutEligible(ctx, organizationID, organizationSlug) {
		return true
	}
	return conv.FromPGTextOrEmpty[string](conn.PublishedHooksVersion) == hooksGeneratorVersion &&
		storedHooksConfigHash(conn.PublishedHooksConfig) == hooksConfigHash(hooksConfigSnapshot(cfg))
}

// heldEntriesCacheTTL bounds how long the manifest entries of held plugins are
// reused. The cache key moves with every publish, so the bound only matters
// for a repo changed outside Speakeasy.
const heldEntriesCacheTTL = 10 * time.Minute

// heldEntriesFailureTTL bounds how long a failed read of held plugins' files
// is reused, so the next read soon retries it.
const heldEntriesFailureTTL = 30 * time.Second

// holdPendingPluginEntries holds each pending plugin with the manifest entry
// its published files give, keeping no files, as a freshness read needs. The
// entries are cached per published state, since the dashboard polls
// freshness and the files change only with a publish.
func (s *Service) holdPendingPluginEntries(
	ctx context.Context,
	projectID uuid.UUID,
	conn repo.PluginGithubConnection,
	live []PluginInfo,
	published map[string]string,
	plan pausedPluginPlan,
	pending map[string]PluginInfo,
) pausedPluginPlan {
	key := heldEntriesCacheKey(projectID, conn, live, pending, published)
	var entries map[string]*sharedPluginEntry
	if s.cache != nil {
		switch err := s.cache.Get(ctx, key, &entries); {
		case err == nil:
			return applyHeldEntries(plan, pending, published, entries)
		case errors.Is(err, redisCache.ErrCacheMiss):
		default:
			s.logger.WarnContext(ctx, "read held plugin entries cache", attr.SlogError(err))
		}
	}

	// A pending plugin that isn't held fell back to its live state.
	entries = make(map[string]*sharedPluginEntry, len(pending))
	for slug := range pending {
		entries[slug] = nil
	}
	ttl := heldEntriesCacheTTL
	var held pausedPluginPlan
	err := errors.New("github publishing is not configured")
	if s.github != nil {
		held, err = s.holdPendingPlugins(ctx, projectID, conn.RepoOwner, conn.RepoName, live, published, plan, pending, true)
	}
	if err != nil {
		// Freshness degrades rather than fails: every pending plugin counts
		// with its live state, as a publish does when it can't read their
		// files, which can only make plugins read stale. Caching that briefly
		// keeps a polled read from calling GitHub through an outage.
		s.logger.WarnContext(ctx, "read held plugin entries; comparing their live state instead",
			attr.SlogProjectID(projectID.String()),
			attr.SlogError(err),
		)
		ttl = heldEntriesFailureTTL
		held = applyHeldEntries(plan, pending, published, entries)
	} else {
		for _, h := range held.Held {
			entries[h.Slug] = &h.Entry
		}
	}
	if s.cache != nil {
		if err := s.cache.Set(ctx, key, &entries, ttl); err != nil {
			s.logger.WarnContext(ctx, "write held plugin entries cache", attr.SlogError(err))
		}
	}
	return held
}

// applyHeldEntries holds each pending plugin with a cached entry and returns
// the rest, which fell back to their live state, to the active plugins.
func applyHeldEntries(plan pausedPluginPlan, pending map[string]PluginInfo, published map[string]string, entries map[string]*sharedPluginEntry) pausedPluginPlan {
	for _, slug := range slices.Sorted(maps.Keys(pending)) {
		entry := entries[slug]
		if entry == nil {
			plan.Active = append(plan.Active, pending[slug])
			continue
		}
		plan.Held = append(plan.Held, heldPlugin{Slug: slug, Fingerprint: published[slug], Files: nil, Entry: *entry})
	}
	sort.SliceStable(plan.Active, func(i, j int) bool { return plan.Active[i].Slug < plan.Active[j].Slug })
	return plan
}

// heldEntriesCacheKey names one published state of the held plugins: the
// connection's last publish, the live slugs (which decide who owns each
// directory), and each pending plugin's stored fingerprint.
func heldEntriesCacheKey(projectID uuid.UUID, conn repo.PluginGithubConnection, live []PluginInfo, pending map[string]PluginInfo, published map[string]string) string {
	liveSlugs := make([]string, 0, len(live))
	for _, info := range live {
		liveSlugs = append(liveSlugs, info.Slug)
	}
	sort.Strings(liveSlugs)
	held := make(map[string]string, len(pending))
	for slug := range pending {
		held[slug] = published[slug]
	}
	state, _ := json.Marshal(struct {
		PublishedAt int64             `json:"published_at"`
		Live        []string          `json:"live"`
		Held        map[string]string `json:"held"`
	}{PublishedAt: conn.UpdatedAt.Time.UnixNano(), Live: liveSlugs, Held: held})
	digest := sha256.Sum256(state)
	return fmt.Sprintf("plugins:held-entries:%s:%s", projectID, hex.EncodeToString(digest[:]))
}
