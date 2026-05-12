package productfeatures

import (
	"context"
	"log/slog"
	"slices"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/redis/go-redis/v9"
	"go.opentelemetry.io/otel/trace"

	"github.com/speakeasy-api/gram/server/internal/attr"
	"github.com/speakeasy-api/gram/server/internal/cache"
	"github.com/speakeasy-api/gram/server/internal/oops"
	"github.com/speakeasy-api/gram/server/internal/productfeatures/repo"
)

type Client struct {
	tracer          trace.Tracer
	logger          *slog.Logger
	db              *pgxpool.Pool
	repo            *repo.Queries
	featureCache    cache.TypedCacheObject[FeatureCache]
	exclusionsCache cache.TypedCacheObject[SessionCaptureExclusionsCache]
}

func NewClient(logger *slog.Logger, tracerProvider trace.TracerProvider, db *pgxpool.Pool, redisClient *redis.Client) *Client {
	logger = logger.With(attr.SlogComponent("productfeatures"))

	cacheAdapter := cache.NewRedisCacheAdapter(redisClient)

	return &Client{
		tracer:          tracerProvider.Tracer("github.com/speakeasy-api/gram/server/internal/productfeatures"),
		logger:          logger,
		db:              db,
		repo:            repo.New(db),
		featureCache:    cache.NewTypedObjectCache[FeatureCache](logger.With(attr.SlogCacheNamespace("productfeature")), cacheAdapter, cache.SuffixNone),
		exclusionsCache: cache.NewTypedObjectCache[SessionCaptureExclusionsCache](logger.With(attr.SlogCacheNamespace("session_capture_exclusions")), cacheAdapter, cache.SuffixNone),
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
	if err != nil {
		return false, oops.E(
			oops.CodeUnexpected,
			err,
			"failed to get organization feature flag %q",
			string(feature),
		).Log(ctx, c.logger, attr.SlogOrganizationID(organizationID))
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

// IsUserSessionCaptureExcluded reports whether a given Gram user has been
// explicitly excluded from session capture by their organization admin. Empty
// userID short-circuits to false because anonymous hook traffic has no
// principal to match against the exclusion list.
func (c *Client) IsUserSessionCaptureExcluded(ctx context.Context, organizationID string, userID string) (bool, error) {
	if organizationID == "" || userID == "" {
		return false, nil
	}

	if cached, err := c.exclusionsCache.Get(ctx, SessionCaptureExclusionsCacheKey(organizationID)); err == nil {
		if slices.Contains(cached.UserIDs, userID) {
			return true, nil
		}
		return false, nil
	}

	userIDs, err := c.repo.ListSessionCaptureExclusions(ctx, organizationID)
	if err != nil {
		return false, oops.E(
			oops.CodeUnexpected,
			err,
			"failed to list session capture exclusions",
		).Log(ctx, c.logger, attr.SlogOrganizationID(organizationID))
	}

	cacheEntry := SessionCaptureExclusionsCache{
		OrganizationID: organizationID,
		UserIDs:        userIDs,
	}
	if cacheErr := c.exclusionsCache.Store(ctx, cacheEntry); cacheErr != nil {
		c.logger.WarnContext(ctx, "failed to cache session capture exclusions",
			attr.SlogError(cacheErr),
			attr.SlogOrganizationID(organizationID),
		)
	}

	if slices.Contains(userIDs, userID) {
		return true, nil
	}
	return false, nil
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
