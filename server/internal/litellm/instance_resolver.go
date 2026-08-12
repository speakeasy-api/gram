package litellm

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"sync"
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
	cache  *lru.LRU[string, uuid.UUID]
	group  singleflight.Group
	mu     sync.Mutex
	// generations invalidates in-flight fills per cache key, so forgetting one
	// key never discards concurrent resolutions for unrelated keys. Entries
	// exist only while a fill is in flight, keeping the map bounded.
	generations map[string]*fillGeneration
	lookup      func(context.Context, repo.GetActiveLiteLLMInstanceIDByAPIKeyParams) (uuid.UUID, error)
}

type fillGeneration struct {
	generation uint64
	inflight   int
}

func NewInstanceResolver(logger *slog.Logger, db *pgxpool.Pool) *InstanceResolver {
	resolver := &InstanceResolver{
		logger:      logger.With(attr.SlogComponent("litellm.instance-resolver")),
		cache:       lru.NewLRU[string, uuid.UUID](instanceResolverCacheSize, nil, instanceResolverCacheTTL),
		group:       singleflight.Group{},
		mu:          sync.Mutex{},
		generations: make(map[string]*fillGeneration),
		lookup:      nil,
	}
	resolver.lookup = func(ctx context.Context, params repo.GetActiveLiteLLMInstanceIDByAPIKeyParams) (uuid.UUID, error) {
		return repo.New(db).GetActiveLiteLLMInstanceIDByAPIKey(ctx, params)
	}
	return resolver
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
		r.mu.Lock()
		gen := r.generations[cacheKey]
		if gen == nil {
			gen = &fillGeneration{generation: 0, inflight: 0}
			r.generations[cacheKey] = gen
		}
		gen.inflight++
		generation := gen.generation
		r.mu.Unlock()
		defer func() {
			r.mu.Lock()
			gen.inflight--
			if gen.inflight == 0 {
				delete(r.generations, cacheKey)
			}
			r.mu.Unlock()
		}()
		instanceID, queryErr := r.lookup(ctx, repo.GetActiveLiteLLMInstanceIDByAPIKeyParams{
			OrganizationID: organizationID,
			ProjectID:      projectUUID,
			ApiKeyID:       keyID,
		})
		if errors.Is(queryErr, pgx.ErrNoRows) {
			instanceID = uuid.Nil
		} else if queryErr != nil {
			return uuid.Nil, fmt.Errorf("get active LiteLLM instance by API key: %w", queryErr)
		}
		r.mu.Lock()
		defer r.mu.Unlock()
		if cached, ok := r.cache.Get(cacheKey); ok {
			return cached, nil
		}
		if generation != gen.generation {
			return uuid.Nil, nil
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
	cacheKey := instanceResolverCacheKey(organizationID, projectID.String(), apiKeyID)
	r.mu.Lock()
	defer r.mu.Unlock()
	if gen, ok := r.generations[cacheKey]; ok {
		gen.generation++
	}
	r.cache.Add(cacheKey, instanceID)
}

func (r *InstanceResolver) Forget(organizationID string, projectID uuid.UUID, apiKeyID string) {
	cacheKey := instanceResolverCacheKey(organizationID, projectID.String(), apiKeyID)
	r.mu.Lock()
	defer r.mu.Unlock()
	if gen, ok := r.generations[cacheKey]; ok {
		gen.generation++
	}
	r.cache.Add(cacheKey, uuid.Nil)
}

func instanceResolverCacheKey(organizationID, projectID, apiKeyID string) string {
	return fmt.Sprintf("%s:%s:%s", organizationID, projectID, apiKeyID)
}
