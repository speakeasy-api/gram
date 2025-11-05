package productfeatures

import (
	"context"
	"log/slog"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/redis/go-redis/v9"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/trace"

	"github.com/speakeasy-api/gram/server/internal/attr"
	"github.com/speakeasy-api/gram/server/internal/cache"
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

func NewClient(logger *slog.Logger, db *pgxpool.Pool, redisClient *redis.Client) *Client {
	logger = logger.With(attr.SlogComponent("productfeatures"))

	return &Client{
		tracer:       otel.Tracer("github.com/speakeasy-api/gram/server/internal/productfeatures"),
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
