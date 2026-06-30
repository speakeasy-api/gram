package productfeatures

import (
	"context"
	"errors"
	"fmt"
	"log/slog"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/redis/go-redis/v9"
	"go.opentelemetry.io/otel/trace"

	"github.com/speakeasy-api/gram/server/internal/attr"
	"github.com/speakeasy-api/gram/server/internal/authz"
	"github.com/speakeasy-api/gram/server/internal/cache"
	"github.com/speakeasy-api/gram/server/internal/o11y"
	"github.com/speakeasy-api/gram/server/internal/oops"
	"github.com/speakeasy-api/gram/server/internal/productfeatures/repo"
)

type Client struct {
	tracer       trace.Tracer
	logger       *slog.Logger
	db           *pgxpool.Pool
	repo         *repo.Queries
	featureCache cache.TypedCacheObject[FeatureCache]
}

func NewClient(logger *slog.Logger, tracerProvider trace.TracerProvider, db *pgxpool.Pool, redisClient *redis.Client) *Client {
	logger = logger.With(attr.SlogComponent("productfeatures"))

	return &Client{
		tracer:       tracerProvider.Tracer("github.com/speakeasy-api/gram/server/internal/productfeatures"),
		logger:       logger,
		db:           db,
		repo:         repo.New(db),
		featureCache: cache.NewTypedObjectCache[FeatureCache](logger.With(attr.SlogCacheNamespace("productfeature")), cache.NewRedisCacheAdapter(redisClient), cache.SuffixNone),
	}
}

func (c *Client) IsFeatureEnabled(ctx context.Context, organizationID string, feature Feature) (bool, error) {
	if cached, err := c.featureCache.Get(ctx, FeatureCacheKey(organizationID, feature)); err == nil {
		return cached.Enabled, nil
	}

	res, err := c.repo.IsFeatureEnabled(ctx, repo.IsFeatureEnabledParams{
		OrganizationID: organizationID,
		FeatureName:    string(feature),
	})
	switch {
	case errors.Is(err, context.Canceled):
		// Do not cache results if the context was canceled, as this likely
		// indicates a timeout or shutdown in progress. Caching in this case
		// could lead to incorrect feature flag states being stored.
		return false, nil
	case errors.Is(err, pgx.ErrNoRows):
		// If there is no row, the feature is not enabled. Cache this result to
		// avoid hitting the database repeatedly for missing features.
		res = false
	case err != nil:
		return false, oops.E(
			oops.CodeUnexpected,
			err,
			"failed to get organization feature flag %q",
			string(feature),
		).LogError(ctx, c.logger, attr.SlogOrganizationID(organizationID))
	}

	cacheEntry := FeatureCache{
		OrganizationID: organizationID,
		Feature:        feature,
		Enabled:        res,
	}

	if cacheErr := c.featureCache.Store(ctx, cacheEntry); cacheErr != nil {
		c.logger.WarnContext(ctx, "failed to cache feature flag state",
			attr.SlogError(cacheErr),
			attr.SlogOrganizationID(organizationID),
			attr.SlogProductFeatureName(string(feature)),
		)
	}

	return res, nil
}

// PlatformFeatureCheck adapts IsFeatureEnabled to the
// platformtools.FeatureChecker signature so it can gate platform-tool
// dispatch. Errors degrade to "disabled" so a transient lookup failure does
// not silently grant access; the underlying error is logged for ops.
func (c *Client) PlatformFeatureCheck(ctx context.Context, organizationID string, feature string) bool {
	enabled, err := c.IsFeatureEnabled(ctx, organizationID, Feature(feature))
	if err != nil {
		c.logger.ErrorContext(ctx, "platform tool feature check failed",
			attr.SlogError(err),
			attr.SlogOrganizationID(organizationID),
			attr.SlogProductFeatureName(feature),
		)
		return false
	}
	return enabled
}

// UpdateFeatureCache stores the given enabled state for the feature directly
// into the cache. Call this after writing the feature flag to the database
// from a code path that bypasses this client, so the cache stays consistent.
func (c *Client) UpdateFeatureCache(ctx context.Context, organizationID string, feature Feature, enabled bool) {
	cacheEntry := FeatureCache{
		OrganizationID: organizationID,
		Feature:        feature,
		Enabled:        enabled,
	}
	if err := c.featureCache.Store(ctx, cacheEntry); err != nil {
		c.logger.WarnContext(ctx, "failed to update feature flag cache",
			attr.SlogError(err),
			attr.SlogOrganizationID(organizationID),
			attr.SlogProductFeatureName(string(feature)),
		)
	}
}

// EnableRBACTx seeds the built-in system-role grants and turns on the org-level
// RBAC feature flag within the caller's transaction. It is the single source of
// truth for "enable RBAC for an org" and is idempotent: an already-enabled org
// re-runs cleanly. It deliberately does not touch the feature cache —
// transaction-based callers (e.g. provisioning an org from a WorkOS webhook)
// create brand-new orgs for which nothing is cached yet. Callers holding a
// *Client should prefer Client.EnableRBAC, which wraps this in a transaction and
// refreshes the cache.
func EnableRBACTx(ctx context.Context, dbtx repo.DBTX, organizationID string) error {
	if err := authz.SeedSystemRoleGrantsTx(ctx, dbtx, organizationID); err != nil {
		return fmt.Errorf("seed system role grants: %w", err)
	}

	// EnableFeature upserts on the partial unique index (org, feature) WHERE
	// deleted IS FALSE, so re-enabling an already-enabled org is a clean no-op
	// rather than a UniqueViolation that would poison the transaction.
	if _, err := repo.New(dbtx).EnableFeature(ctx, repo.EnableFeatureParams{
		OrganizationID: organizationID,
		FeatureName:    string(FeatureRBAC),
	}); err != nil {
		return fmt.Errorf("enable RBAC feature flag: %w", err)
	}

	return nil
}

// EnableRBAC enables RBAC for an organization: it seeds the built-in system-role
// grants and turns on the RBAC feature flag atomically, then refreshes the
// feature cache so the engine observes the change without waiting for the cache
// TTL. Idempotent — safe to call on every org creation and to re-run from the
// super-admin tool.
func (c *Client) EnableRBAC(ctx context.Context, organizationID string) error {
	tx, err := c.db.Begin(ctx)
	if err != nil {
		return fmt.Errorf("begin enable RBAC transaction: %w", err)
	}
	defer o11y.NoLogDefer(func() error { return tx.Rollback(ctx) })

	if err := EnableRBACTx(ctx, tx, organizationID); err != nil {
		return err
	}

	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("commit enable RBAC transaction: %w", err)
	}

	c.UpdateFeatureCache(ctx, organizationID, FeatureRBAC, true)
	return nil
}
