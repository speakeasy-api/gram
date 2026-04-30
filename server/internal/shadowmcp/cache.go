package shadowmcp

import (
	"context"
	"log/slog"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/speakeasy-api/gram/server/internal/attr"
	"github.com/speakeasy-api/gram/server/internal/cache"
	"github.com/speakeasy-api/gram/server/internal/mv"
	risk_repo "github.com/speakeasy-api/gram/server/internal/risk/repo"
)

// policyEnabledCacheTTL bounds the staleness window for the
// "shadow_mcp enabled?" lookup. Risk-policy writes invalidate the entry
// directly, so this only matters when an invalidation is missed (e.g. a
// cross-process race or a Redis outage during the write path).
const policyEnabledCacheTTL = 15 * time.Minute

func policyEnabledCacheKey(projectID uuid.UUID) string {
	return "shadow_mcp_policy_enabled:" + projectID.String()
}

// PolicyEnabledCache is the cached "does this project have any enabled
// shadow_mcp risk policy?" answer keyed by project ID. Both true and
// false answers are cached so the common "no shadow_mcp policy" case
// avoids the per-tools/list DB query.
type PolicyEnabledCache struct {
	ProjectID string `json:"project_id"`
	Enabled   bool   `json:"enabled"`
}

var _ cache.CacheableObject[PolicyEnabledCache] = (*PolicyEnabledCache)(nil)

func (p PolicyEnabledCache) CacheKey() string {
	return "shadow_mcp_policy_enabled:" + p.ProjectID
}

func (p PolicyEnabledCache) AdditionalCacheKeys() []string { return nil }

func (p PolicyEnabledCache) TTL() time.Duration { return policyEnabledCacheTTL }

// Client serves "is shadow-MCP enabled for this project?" reads from Redis,
// falling back to the risk_policies table on a miss, and exposes the
// invalidation entry-point used by the risk service after policy writes.
//
// It also owns the toolset cache used by ValidateToolsetCall so every
// shadow_mcp call site (tools/list hot path, hook handlers, batch scanner)
// shares a single cache instance — and the underlying Redis namespace —
// rather than each constructing its own.
type Client struct {
	logger       *slog.Logger
	db           *pgxpool.Pool
	repo         *risk_repo.Queries
	cache        cache.TypedCacheObject[PolicyEnabledCache]
	toolsetCache cache.TypedCacheObject[mv.ToolsetBaseContents]
}

func NewClient(logger *slog.Logger, db *pgxpool.Pool, cacheImpl cache.Cache) *Client {
	logger = logger.With(attr.SlogComponent("shadowmcp"))
	return &Client{
		logger: logger,
		db:     db,
		repo:   risk_repo.New(db),
		cache: cache.NewTypedObjectCache[PolicyEnabledCache](
			logger.With(attr.SlogCacheNamespace("shadow_mcp_policy_enabled")),
			cacheImpl,
			cache.SuffixNone,
		),
		toolsetCache: cache.NewTypedObjectCache[mv.ToolsetBaseContents](
			logger.With(attr.SlogCacheNamespace("toolset")),
			cacheImpl,
			cache.SuffixNone,
		),
	}
}

// IsEnabledForProject reports whether the project has at least one enabled
// shadow_mcp risk policy. Lookup failures (cache or DB) return false so
// schema injection stays off rather than breaking otherwise-valid tool
// calls.
func (c *Client) IsEnabledForProject(ctx context.Context, projectID uuid.UUID) bool {
	if projectID == uuid.Nil {
		return false
	}

	if cached, err := c.cache.Get(ctx, policyEnabledCacheKey(projectID)); err == nil {
		return cached.Enabled
	}

	policies, err := c.repo.ListEnabledShadowMCPPoliciesByProject(ctx, projectID)
	if err != nil {
		c.logger.WarnContext(ctx, "failed to list shadow_mcp policies; defaulting to off",
			attr.SlogError(err),
			attr.SlogProjectID(projectID.String()),
		)
		return false
	}

	enabled := len(policies) > 0
	entry := PolicyEnabledCache{
		ProjectID: projectID.String(),
		Enabled:   enabled,
	}
	if err := c.cache.Store(ctx, entry); err != nil {
		c.logger.WarnContext(ctx, "failed to cache shadow_mcp policy lookup",
			attr.SlogError(err),
			attr.SlogProjectID(projectID.String()),
		)
	}
	return enabled
}

// Invalidate drops the cached shadow_mcp enabled state for the project so
// the next IsEnabledForProject re-reads from the DB. Failures are logged
// but not returned — the entry will expire via TTL.
func (c *Client) Invalidate(ctx context.Context, projectID uuid.UUID) {
	if projectID == uuid.Nil {
		return
	}
	if err := c.cache.DeleteByKey(ctx, policyEnabledCacheKey(projectID)); err != nil {
		c.logger.WarnContext(ctx, "failed to invalidate shadow_mcp policy cache",
			attr.SlogError(err),
			attr.SlogProjectID(projectID.String()),
		)
	}
}
