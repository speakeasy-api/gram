package growthsignals

import (
	"context"
	"errors"
	"fmt"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	orgrepo "github.com/speakeasy-api/gram/server/internal/organizations/repo"
	projectsrepo "github.com/speakeasy-api/gram/server/internal/projects/repo"
	usersrepo "github.com/speakeasy-api/gram/server/internal/users/repo"
)

// DatabaseEnricher resolves ids against Gram's own tables, behind a per-id TTL
// cache.
//
// The events this serves arrive in bursts — one organization's audit log is
// mostly one organization — so caching per id rather than loading a set keeps
// the work proportional to who is actually active. A row that no longer exists
// is cached as readily as one that does, because a deleted project is a
// permanent answer for the length of a TTL rather than a reason to retry on
// every event.
type DatabaseEnricher struct {
	organizations *lookupCache[string, OrganizationDetails]
	projects      *lookupCache[uuid.UUID, ProjectDetails]
	userEmails    *lookupCache[string, string]
}

var _ Enricher = (*DatabaseEnricher)(nil)

func NewDatabaseEnricher(db *pgxpool.Pool) *DatabaseEnricher {
	return &DatabaseEnricher{
		organizations: newLookupCache(func(ctx context.Context, organizationID string) (OrganizationDetails, error) {
			organization, err := orgrepo.New(db).GetOrganizationMetadata(ctx, organizationID)
			switch {
			case errors.Is(err, pgx.ErrNoRows):
				return OrganizationDetails{Slug: "", Name: ""}, nil
			case err != nil:
				return OrganizationDetails{Slug: "", Name: ""}, fmt.Errorf("get organization metadata: %w", err)
			}

			return OrganizationDetails{Slug: organization.Slug, Name: organization.Name}, nil
		}),
		projects: newLookupCache(func(ctx context.Context, projectID uuid.UUID) (ProjectDetails, error) {
			project, err := projectsrepo.New(db).GetProjectByID(ctx, projectID)
			switch {
			case errors.Is(err, pgx.ErrNoRows):
				return ProjectDetails{Slug: "", Name: ""}, nil
			case err != nil:
				return ProjectDetails{Slug: "", Name: ""}, fmt.Errorf("get project by id: %w", err)
			}

			return ProjectDetails{Slug: project.Slug, Name: project.Name}, nil
		}),
		userEmails: newLookupCache(func(ctx context.Context, userID string) (string, error) {
			user, err := usersrepo.New(db).GetUser(ctx, userID)
			switch {
			case errors.Is(err, pgx.ErrNoRows):
				return "", nil
			case err != nil:
				return "", fmt.Errorf("get user: %w", err)
			}

			return user.Email, nil
		}),
	}
}

func (e *DatabaseEnricher) Organization(ctx context.Context, organizationID string) (OrganizationDetails, error) {
	return e.organizations.resolve(ctx, organizationID)
}

func (e *DatabaseEnricher) Project(ctx context.Context, projectID uuid.UUID) (ProjectDetails, error) {
	return e.projects.resolve(ctx, projectID)
}

func (e *DatabaseEnricher) UserEmail(ctx context.Context, userID string) (string, error) {
	return e.userEmails.resolve(ctx, userID)
}
