package litellm

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"github.com/google/uuid"
	lru "github.com/hashicorp/golang-lru/v2/expirable"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"golang.org/x/sync/singleflight"

	"github.com/speakeasy-api/gram/server/internal/attr"
	"github.com/speakeasy-api/gram/server/internal/litellm/repo"
)

const (
	instanceResolverCacheSize = 10_000
	instanceResolverCacheTTL  = time.Hour
	instanceResolverTimeout   = 250 * time.Millisecond
)

// InstanceResolver maps managed ingestion keys to stable LiteLLM instance IDs
// outside the request path. Nil UUIDs negatively cache unmanaged hooks keys.
type InstanceResolver struct {
	logger *slog.Logger
	db     *pgxpool.Pool
	cache  *lru.LRU[string, uuid.UUID]
	group  singleflight.Group
}

func NewInstanceResolver(logger *slog.Logger, db *pgxpool.Pool) *InstanceResolver {
	return &InstanceResolver{
		logger: logger.With(attr.SlogComponent("litellm.instance-resolver")),
		db:     db,
		cache:  lru.NewLRU[string, uuid.UUID](instanceResolverCacheSize, nil, instanceResolverCacheTTL),
		group:  singleflight.Group{},
	}
}

func (r *InstanceResolver) Resolve(ctx context.Context, organizationID, projectID, apiKeyID string) (uuid.UUID, bool) {
	cacheKey := instanceResolverCacheKey(organizationID, projectID, apiKeyID)
	if instanceID, ok := r.cache.Get(cacheKey); ok {
		return instanceID, instanceID != uuid.Nil
	}
	keyID, err := uuid.Parse(apiKeyID)
	if err != nil {
		return uuid.Nil, false
	}
	projectUUID, err := uuid.Parse(projectID)
	if err != nil {
		return uuid.Nil, false
	}
	value, err, _ := r.group.Do(cacheKey, func() (any, error) {
		if instanceID, ok := r.cache.Get(cacheKey); ok {
			return instanceID, nil
		}
		instanceID, queryErr := repo.New(r.db).GetActiveLiteLLMInstanceIDByAPIKey(ctx, repo.GetActiveLiteLLMInstanceIDByAPIKeyParams{
			OrganizationID: organizationID,
			ProjectID:      projectUUID,
			ApiKeyID:       keyID,
		})
		if errors.Is(queryErr, pgx.ErrNoRows) {
			r.cache.Add(cacheKey, uuid.Nil)
			return uuid.Nil, nil
		}
		if queryErr != nil {
			return uuid.Nil, fmt.Errorf("get active LiteLLM instance by API key: %w", queryErr)
		}
		if cached, ok := r.cache.Get(cacheKey); ok {
			return cached, nil
		}
		r.cache.Add(cacheKey, instanceID)
		return instanceID, nil
	})
	if err != nil {
		r.logger.WarnContext(ctx, "resolve LiteLLM instance for telemetry",
			attr.SlogError(err),
			attr.SlogOrganizationID(organizationID),
			attr.SlogProjectID(projectID),
			attr.SlogAPIKeyID(apiKeyID),
		)
		return uuid.Nil, false
	}
	instanceID, ok := value.(uuid.UUID)
	return instanceID, ok && instanceID != uuid.Nil
}

func (r *InstanceResolver) Remember(organizationID string, projectID uuid.UUID, apiKeyID string, instanceID uuid.UUID) {
	r.cache.Add(instanceResolverCacheKey(organizationID, projectID.String(), apiKeyID), instanceID)
}

func (r *InstanceResolver) Forget(organizationID string, projectID uuid.UUID, apiKeyID string) {
	cacheKey := instanceResolverCacheKey(organizationID, projectID.String(), apiKeyID)
	// Tombstone before and after waiting so an older in-flight fill cannot restore a revoked key.
	r.cache.Add(cacheKey, uuid.Nil)
	_, _, _ = r.group.Do(cacheKey, func() (any, error) {
		return uuid.Nil, nil
	})
	r.cache.Add(cacheKey, uuid.Nil)
}

func instanceResolverCacheKey(organizationID, projectID, apiKeyID string) string {
	return fmt.Sprintf("%s:%s:%s", organizationID, projectID, apiKeyID)
}
