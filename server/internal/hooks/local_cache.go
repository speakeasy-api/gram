package hooks

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	mockidp "github.com/speakeasy-api/gram/mock-speakeasy-idp"
	"github.com/speakeasy-api/gram/server/internal/cache"
	projectsRepo "github.com/speakeasy-api/gram/server/internal/projects/repo"
)

// localSessionCache wraps a cache and short-circuits session metadata lookups
// in local development to avoid requiring OTEL export setup.
type localSessionCache struct {
	cache.Cache
	db *pgxpool.Pool
}

// NewLocalSessionCache creates a cache wrapper that handles session metadata
// lookups locally without requiring OTEL telemetry export.
func NewLocalSessionCache(underlying cache.Cache, db *pgxpool.Pool) cache.Cache {
	return &localSessionCache{
		Cache: underlying,
		db:    db,
	}
}

// Get for session cache keys: try the underlying cache first so OTEL-validated
// sessions win, then fall back to hardcoded local dev defaults pinned to the
// first project in the dev org. Non-session keys always go to the underlying
// cache.
func (c *localSessionCache) Get(ctx context.Context, key string, value any) error {
	if !strings.HasPrefix(key, "session:") {
		if err := c.Cache.Get(ctx, key, value); err != nil {
			return fmt.Errorf("get from cache: %w", err)
		}
		return nil
	}

	if err := c.Cache.Get(ctx, key, value); err == nil {
		return nil
	}

	// Underlying cache miss — fall back to local dev defaults so OTEL setup
	// is not required for ad-hoc hook testing.
	config := mockidp.DefaultConfig()
	projectsRepo := projectsRepo.New(c.db)
	projects, err := projectsRepo.ListProjectsByOrganization(ctx, config.Organization.ID)
	if err != nil || len(projects) == 0 {
		return fmt.Errorf("get project: %w", err)
	}
	projectID := projects[0].ID.String()

	// Extract sessionID from key (format: "session:metadata:{sessionID}",
	// see sessionCacheKey in cache.go).
	sessionID := strings.TrimPrefix(key, "session:metadata:")

	metadata := SessionMetadata{
		SessionID:   sessionID,
		ServiceName: "claude-code",
		UserEmail:   config.User.Email,
		ClaudeOrgID: config.Organization.ID,
		GramOrgID:   config.Organization.ID,
		ProjectID:   projectID,
	}

	if dest, ok := value.(*SessionMetadata); ok {
		*dest = metadata
		return nil
	}
	return fmt.Errorf("expected *SessionMetadata, got %T", value)
}

// Set always delegates to the underlying cache so explicitly seeded sessions
// (and OTEL-validated ones) survive across processes. The local dev fallback
// in Get only fires on cache miss.
func (c *localSessionCache) Set(ctx context.Context, key string, value any, ttl time.Duration) error {
	if err := c.Cache.Set(ctx, key, value, ttl); err != nil {
		return fmt.Errorf("set in cache: %w", err)
	}
	return nil
}
