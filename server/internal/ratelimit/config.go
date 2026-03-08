package ratelimit

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/speakeasy-api/gram/server/internal/attr"
	"github.com/speakeasy-api/gram/server/internal/cache"
	"github.com/speakeasy-api/gram/server/internal/ratelimit/repo"
)

// rateLimitEntry is a cached rate limit override value.
// A RequestsPerMinute of 0 means no override exists (cached negative result).
type rateLimitEntry struct {
	AttributeType     string `json:"attribute_type"`
	AttributeValue    string `json:"attribute_value"`
	RequestsPerMinute int    `json:"requests_per_minute"`
}

var _ cache.CacheableObject[rateLimitEntry] = (*rateLimitEntry)(nil)

func (r rateLimitEntry) CacheKey() string {
	return rateLimitCacheKey(r.AttributeType, r.AttributeValue)
}

func rateLimitCacheKey(attributeType, attributeValue string) string {
	return fmt.Sprintf("platform_rate_limit:%s:%s", attributeType, attributeValue)
}

func (r rateLimitEntry) TTL() time.Duration {
	return 1 * time.Minute
}

func (r rateLimitEntry) AdditionalCacheKeys() []string {
	return []string{}
}

// ConfigLoader loads rate limit overrides from the platform_rate_limits table,
// caching results in Redis to avoid repeated database lookups.
type ConfigLoader struct {
	logger *slog.Logger
	repo   *repo.Queries
	cache  cache.TypedCacheObject[rateLimitEntry]
}

// NewConfigLoader creates a ConfigLoader backed by the given database pool and cache.
func NewConfigLoader(logger *slog.Logger, db *pgxpool.Pool, cacheImpl cache.Cache) *ConfigLoader {
	return &ConfigLoader{
		logger: logger.With(attr.SlogComponent("ratelimit-config")),
		repo:   repo.New(db),
		cache:  cache.NewTypedObjectCache[rateLimitEntry](logger.With(attr.SlogCacheNamespace("ratelimit")), cacheImpl, cache.SuffixNone),
	}
}

// GetLimit returns the rate limit override for the given attribute type and value.
// Returns 0 with a nil error if no override exists, signaling the caller to use the default.
func (cl *ConfigLoader) GetLimit(ctx context.Context, attributeType, attributeValue string) (int, error) {
	cacheKey := rateLimitCacheKey(attributeType, attributeValue)

	// Check cache first.
	cached, err := cl.cache.Get(ctx, cacheKey)
	if err == nil {
		return cached.RequestsPerMinute, nil
	}

	// Cache miss — query the database.
	row, err := cl.repo.GetPlatformRateLimit(ctx, repo.GetPlatformRateLimitParams{
		AttributeType:  attributeType,
		AttributeValue: attributeValue,
	})
	if err != nil && !errors.Is(err, pgx.ErrNoRows) {
		return 0, fmt.Errorf("query platform_rate_limits for %s/%s: %w", attributeType, attributeValue, err)
	}

	// Build the cache entry. A zero value means "no override" (cached negative).
	entry := rateLimitEntry{
		AttributeType:     attributeType,
		AttributeValue:    attributeValue,
		RequestsPerMinute: 0,
	}
	if err == nil {
		entry.RequestsPerMinute = int(row.RequestsPerMinute)
	}

	// Store in cache (best-effort).
	if storeErr := cl.cache.Store(ctx, entry); storeErr != nil {
		cl.logger.WarnContext(ctx, "cache store rate limit entry",
			attr.SlogError(storeErr),
		)
	}

	return entry.RequestsPerMinute, nil
}
