package hooks

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/speakeasy-api/gram/server/internal/cache"
	"github.com/speakeasy-api/gram/server/internal/conv"
	organizationsRepo "github.com/speakeasy-api/gram/server/internal/organizations/repo"
	projectsRepo "github.com/speakeasy-api/gram/server/internal/projects/repo"
)

// localFallbackEmail is the synthesized email reported on
// session-cache-miss SessionMetadata in local dev. Hooks consume the
// metadata only for routing/scoping; the email is informational.
const localFallbackEmail = "local-hook-testing@example.com"

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
		if metadata, ok := value.(*SessionMetadata); ok {
			if err := c.enrichLocalSessionMetadata(ctx, metadata); err != nil {
				return fmt.Errorf("enrich local session metadata: %w", err)
			}
		}
		return nil
	}

	if dest, ok := value.(*SessionMetadata); ok {
		metadata, err := c.fallbackSessionMetadata(ctx, strings.TrimPrefix(key, "session:metadata:"))
		if err != nil {
			return err
		}
		*dest = metadata
		return nil
	}
	return fmt.Errorf("expected *SessionMetadata, got %T", value)
}

func (c *localSessionCache) fallbackSessionMetadata(ctx context.Context, sessionID string) (SessionMetadata, error) {
	// Underlying cache miss — fall back to whatever project happens to
	// exist so OTEL setup is not required for ad-hoc hook testing. The
	// org id is read off that project; the email is a synthesized
	// placeholder.
	project, err := c.localFallbackProject(ctx, "")
	if err != nil {
		return SessionMetadata{}, err
	}
	orgID := project.OrganizationID
	projectID := project.ID.String()
	userID, userEmail := c.localFallbackUser(ctx, orgID)

	metadata := SessionMetadata{
		SessionID:   sessionID,
		ServiceName: "claude-code",
		UserEmail:   userEmail,
		UserID:      userID,
		ClaudeOrgID: orgID,
		GramOrgID:   orgID,
		ProjectID:   projectID,
	}

	return metadata, nil
}

func (c *localSessionCache) enrichLocalSessionMetadata(ctx context.Context, metadata *SessionMetadata) error {
	if metadata.UserID != "" {
		return nil
	}

	project, err := c.localFallbackProject(ctx, metadata.ProjectID)
	if err != nil {
		return err
	}

	if metadata.ProjectID == "" {
		metadata.ProjectID = project.ID.String()
	}
	if metadata.GramOrgID == "" {
		metadata.GramOrgID = project.OrganizationID
	}
	if metadata.ClaudeOrgID == "" {
		metadata.ClaudeOrgID = metadata.GramOrgID
	}

	userID, userEmail := c.localFallbackUser(ctx, metadata.GramOrgID)
	if userID == "" {
		return nil
	}

	metadata.UserID = userID
	if metadata.UserEmail == "" || metadata.UserEmail == localFallbackEmail {
		metadata.UserEmail = userEmail
	}

	return nil
}

func (c *localSessionCache) localFallbackProject(ctx context.Context, projectID string) (projectsRepo.Project, error) {
	projects := projectsRepo.New(c.db)
	if projectID != "" {
		id, err := uuid.Parse(projectID)
		if err == nil {
			project, err := projects.GetProjectByID(ctx, id)
			if err == nil {
				return project, nil
			}
		}
	}

	project, err := projects.GetFirstProject(ctx)
	if err != nil {
		return projectsRepo.Project{}, fmt.Errorf("get project: %w", err)
	}

	return project, nil
}

func (c *localSessionCache) localFallbackUser(ctx context.Context, orgID string) (string, string) {
	users, err := organizationsRepo.New(c.db).ListOrganizationUsers(ctx, orgID)
	if err != nil || len(users) == 0 {
		return "", localFallbackEmail
	}

	return conv.FromPGTextOrEmpty[string](users[0].UserID), users[0].UserEmail
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
