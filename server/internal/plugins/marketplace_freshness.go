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

// resolveMarketplaceFreshness plans an automatic publish of infos, the
// project's live plugins, the way publishProject would, and compares it with
// what conn records as published. A paused plugin with changes waiting is
// held, so its published files are read from the repo; they only change with
// a publish, so their manifest entries are cached per published state.
func (s *Service) resolveMarketplaceFreshness(
	ctx context.Context,
	organizationID, organizationSlug string,
	projectID uuid.UUID,
	infos []PluginInfo,
	conn repo.PluginGithubConnection,
	cfg GenerateConfig,
	observabilityEnabled bool,
) (marketplaceFreshness, error) {
	published := decodeMCPFingerprints(conn.PublishedMcpFingerprints)
	plan, pending, paused, err := s.classifyPausedPlugins(ctx, projectID, infos, published, cfg, observabilityEnabled)
	if err != nil {
		return marketplaceFreshness{}, err
	}
	if len(pending) > 0 {
		plan, err = s.holdPendingPluginEntries(ctx, projectID, conn, infos, published, plan, pending)
		if err != nil {
			return marketplaceFreshness{}, err
		}
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
		Plugins:                 pluginFreshness(infos, paused, plan, automatic, published),
		Paused:                  paused,
		AutomaticPublishPending: !maps.Equal(automatic, published) || !hooksCurrent,
	}, nil
}

// pluginFreshness applies marketplaceFreshness.Plugins' rule: automatic is
// what an automatic publish of plan would record, published what the last
// publish did. A held or omitted plugin has changes waiting, whatever its
// recorded fingerprint says.
func pluginFreshness(infos []PluginInfo, paused map[string]bool, plan pausedPluginPlan, automatic, published map[string]string) map[string]bool {
	waiting := make(map[string]bool, len(plan.Held)+len(plan.Omitted))
	for _, held := range plan.Held {
		waiting[held.Slug] = true
	}
	for _, slug := range plan.Omitted {
		waiting[slug] = true
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

// holdPendingPluginEntries is holdPendingPlugins for a read: it keeps only
// each held plugin's manifest entry, cached per published state, since the
// dashboard polls freshness and the files change only with a publish.
func (s *Service) holdPendingPluginEntries(
	ctx context.Context,
	projectID uuid.UUID,
	conn repo.PluginGithubConnection,
	live []PluginInfo,
	published map[string]string,
	plan pausedPluginPlan,
	pending map[string]PluginInfo,
) (pausedPluginPlan, error) {
	key := heldEntriesCacheKey(projectID, conn, live, pending, published)
	var entries map[string]*sharedPluginEntry
	if s.cache != nil {
		switch err := s.cache.Get(ctx, key, &entries); {
		case err == nil:
			return applyHeldEntries(plan, pending, published, entries), nil
		case errors.Is(err, redisCache.ErrCacheMiss):
		default:
			s.logger.WarnContext(ctx, "read held plugin entries cache", attr.SlogError(err))
		}
	}

	unshared := pausedPluginPlan{Active: slices.Clone(plan.Active), Held: nil, Omitted: plan.Omitted}
	held, err := s.holdPendingPlugins(ctx, projectID, conn.RepoOwner, conn.RepoName, live, published, unshared, pending)
	if err != nil {
		return pausedPluginPlan{}, err
	}
	entries = make(map[string]*sharedPluginEntry, len(pending))
	for slug := range pending {
		entries[slug] = nil
	}
	for _, h := range held.Held {
		entries[h.Slug] = &h.Entry
	}
	if s.cache != nil {
		if err := s.cache.Set(ctx, key, &entries, heldEntriesCacheTTL); err != nil {
			s.logger.WarnContext(ctx, "write held plugin entries cache", attr.SlogError(err))
		}
	}
	return applyHeldEntries(plan, pending, published, entries), nil
}

// applyHeldEntries holds each pending plugin with a cached entry and returns
// the rest, whose published files were unreadable, to the active plugins.
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
