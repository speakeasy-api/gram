package otel

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"github.com/jackc/pgx/v5"
	"golang.org/x/sync/singleflight"

	"github.com/speakeasy-api/gram/server/internal/cache"
	"github.com/speakeasy-api/gram/server/internal/database"
	organizationsrepo "github.com/speakeasy-api/gram/server/internal/organizations/repo"
)

const (
	logRelayOrganizationSlugCacheTTL = 5 * time.Minute
	logRelayOrganizationSlugTimeout  = time.Second
)

type logRelayOrganizationSlugResolver interface {
	OrganizationSlug(ctx context.Context, organizationID string) (string, error)
}

type postgresLogRelayOrganizationSlugResolver struct {
	db    database.DBTX
	cache cache.TypedCacheObject[cachedLogRelayOrganizationSlug]
	loads singleflight.Group
}

func newPostgresLogRelayOrganizationSlugResolver(
	logger *slog.Logger,
	db database.DBTX,
	cacheImpl cache.Cache,
) *postgresLogRelayOrganizationSlugResolver {
	return &postgresLogRelayOrganizationSlugResolver{
		db: db,
		cache: cache.NewTypedObjectCache[cachedLogRelayOrganizationSlug](
			logger,
			cacheImpl,
			cache.SuffixNone,
		),
		loads: singleflight.Group{},
	}
}

func (r *postgresLogRelayOrganizationSlugResolver) OrganizationSlug(
	ctx context.Context,
	organizationID string,
) (string, error) {
	if organizationID == "" {
		return "", nil
	}

	lookupCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), logRelayOrganizationSlugTimeout)
	defer cancel()

	cacheKey := logRelayOrganizationSlugCacheKey(organizationID)
	if cached, err := r.cache.Get(lookupCtx, cacheKey); err == nil {
		return cached.Slug, nil
	}

	results := r.loads.DoChan(cacheKey, func() (any, error) {
		if cached, err := r.cache.Get(lookupCtx, cacheKey); err == nil {
			return cached.Slug, nil
		}

		slug := ""
		organization, err := organizationsrepo.New(r.db).GetOrganizationMetadata(lookupCtx, organizationID)
		switch {
		case errors.Is(err, pgx.ErrNoRows):
		case err != nil:
			return "", fmt.Errorf("get organization metadata: %w", err)
		default:
			slug = organization.Slug
		}

		if err := r.cache.Store(lookupCtx, cachedLogRelayOrganizationSlug{
			OrganizationID: organizationID,
			Slug:           slug,
		}); err != nil {
			return "", fmt.Errorf("cache organization slug: %w", err)
		}
		return slug, nil
	})

	var result singleflight.Result
	select {
	case result = <-results:
	case <-lookupCtx.Done():
		select {
		case result = <-results:
		default:
			return "", fmt.Errorf("resolve organization slug: %w", lookupCtx.Err())
		}
	}

	slug, ok := result.Val.(string)
	if !ok {
		return "", fmt.Errorf("resolve organization slug: unexpected result type %T", result.Val)
	}
	return slug, result.Err
}

type cachedLogRelayOrganizationSlug struct {
	OrganizationID string `json:"organization_id"`
	Slug           string `json:"slug"`
}

var _ cache.CacheableObject[cachedLogRelayOrganizationSlug] = (*cachedLogRelayOrganizationSlug)(nil)

func (c cachedLogRelayOrganizationSlug) CacheKey() string {
	return logRelayOrganizationSlugCacheKey(c.OrganizationID)
}

func (cachedLogRelayOrganizationSlug) TTL() time.Duration {
	return logRelayOrganizationSlugCacheTTL
}

func logRelayOrganizationSlugCacheKey(organizationID string) string {
	return "otelLogRelayOrganizationSlug:v1:" + organizationID
}
