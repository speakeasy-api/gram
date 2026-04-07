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

// Get intercepts session cache keys and provides hardcoded local dev values.
func (c *localSessionCache) Get(ctx context.Context, key string, value any) error {
	// Check if this is a session cache key
	if strings.HasPrefix(key, "session:") {
		// Short-circuit with hardcoded test values for local development
		config := mockidp.DefaultConfig()
		projectsRepo := projectsRepo.New(c.db)
		projects, err := projectsRepo.ListProjectsByOrganization(ctx, config.Organization.ID)
		if err != nil || len(projects) == 0 {
			return fmt.Errorf("get project: %w", err)
		}
		projectID := projects[0].ID.String()

		// Extract sessionID from key (format: "session:{sessionID}")
		sessionID := strings.TrimPrefix(key, "session:")

		metadata := SessionMetadata{
			SessionID:   sessionID,
			ServiceName: "claude-code",
			UserEmail:   config.User.Email,
			ClaudeOrgID: config.Organization.ID,
			GramOrgID:   config.Organization.ID,
			ProjectID:   projectID,
		}

		// Type assert and populate the output
		if dest, ok := value.(*SessionMetadata); ok {
			*dest = metadata
			return nil
		}
		return fmt.Errorf("expected *SessionMetadata, got %T", value)
	}

	// Not a session key, delegate to underlying cache
	if err := c.Cache.Get(ctx, key, value); err != nil {
		return fmt.Errorf("get from cache: %w", err)
	}
	return nil
}

// Set intercepts session cache keys and no-ops them (they're computed on Get).
func (c *localSessionCache) Set(ctx context.Context, key string, value any, ttl time.Duration) error {
	// If it's a session key in local dev, we don't need to store it
	// since we compute it on-the-fly in Get()
	if strings.HasPrefix(key, "session:") {
		return nil
	}

	// Not a session key, delegate to underlying cache
	if err := c.Cache.Set(ctx, key, value, ttl); err != nil {
		return fmt.Errorf("set in cache: %w", err)
	}
	return nil
}
