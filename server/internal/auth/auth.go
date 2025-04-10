package auth

import (
	"context"
	"errors"
	"fmt"
	"log/slog"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/speakeasy-api/gram/internal/auth/repo"
	"github.com/speakeasy-api/gram/internal/auth/sessions"
	"github.com/speakeasy-api/gram/internal/contextvalues"
	"github.com/speakeasy-api/gram/internal/oops"
	"goa.design/goa/v3/security"
)

var (
	ErrAuthCheckFailed    = oops.New("check failed")
	ErrAuthNoSession      = oops.New("no session found")
	ErrAuthInvalidOrgID   = oops.New("invalid session organization")
	ErrAuthInvalidProject = oops.New("invalid project")
	ErrAuthAccessDenied   = oops.New("access denied")
)

type Auth struct {
	logger   *slog.Logger
	db       *pgxpool.Pool
	sessions *sessions.Manager
	keys     *ByKey
	repo     *repo.Queries
}

func New(logger *slog.Logger, db *pgxpool.Pool, sessions *sessions.Manager) *Auth {
	return &Auth{
		logger:   logger,
		db:       db,
		keys:     NewKeyAuth(db),
		sessions: sessions,
		repo:     repo.New(db),
	}
}

func (s *Auth) Authorize(ctx context.Context, key string, schema *security.APIKeyScheme) (context.Context, error) {
	if schema == nil {
		panic("Goa has not passed a schema") // TODO: figure something out here
	}

	switch schema.Name {
	case KeySecurityScheme:
		return s.keys.KeyBasedAuth(ctx, key, schema.RequiredScopes)
	case SessionSecurityScheme:
		return s.sessions.Authenticate(ctx, key, false)
	case ProjectSlugSecuritySchema:
		return s.checkProjectAccess(ctx, s.logger, key)
	default:
		return ctx, errors.New("unsupported security scheme")
	}
}

func (s *Auth) checkProjectAccess(ctx context.Context, logger *slog.Logger, projectSlug string) (context.Context, error) {
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	if !ok {
		return ctx, fmt.Errorf("project access: %w", ErrAuthNoSession)
	}

	projects, err := s.repo.ListProjectsByOrganization(ctx, authCtx.ActiveOrganizationID)
	if err != nil {
		return ctx, oops.E(ErrAuthCheckFailed.Wrap(err), "error checking project access", "database error for project access lookup").Log(ctx, logger, slog.String("org_id", authCtx.ActiveOrganizationID))
	}

	if len(projects) == 0 {
		return ctx, fmt.Errorf("project access: %w: no projects found", ErrAuthAccessDenied)
	}

	if projectSlug == "" && len(projects) == 1 {
		projectSlug = projects[0].Slug
	}

	if projectSlug == "" {
		return ctx, fmt.Errorf("project access: %w: empty slug", ErrAuthInvalidProject)
	}

	hasProjectAccess := false
	for _, project := range projects {
		if project.Slug == projectSlug {
			authCtx.ProjectID = &project.ID // This is important
			hasProjectAccess = true
			break
		}
	}

	if !hasProjectAccess {
		return ctx, oops.E(ErrAuthCheckFailed, "project access lookup error", "project slug not found").Log(ctx, logger, slog.String("project_slug", projectSlug), slog.String("org_id", authCtx.ActiveOrganizationID))
	}

	ctx = contextvalues.SetAuthContext(ctx, authCtx)
	return ctx, nil
}
