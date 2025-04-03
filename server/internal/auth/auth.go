package auth

import (
	"context"
	"errors"
	"fmt"
	"log/slog"

	"github.com/jackc/pgx/v5/pgxpool"
	dsecurity "github.com/speakeasy-api/gram/design/security"
	"github.com/speakeasy-api/gram/internal/auth/repo"
	"github.com/speakeasy-api/gram/internal/auth/sessions"
	"github.com/speakeasy-api/gram/internal/contextvalues"
	"goa.design/goa/v3/security"
)

var (
	ErrAuthCheckFailed    = errors.New("check failed")
	ErrAuthNoSession      = errors.New("no session found")
	ErrAuthInvalidOrgID   = errors.New("invalid session organization")
	ErrAuthInvalidProject = errors.New("invalid project")
	ErrAuthAccessDenied   = errors.New("access denied")
)

type Auth struct {
	logger   *slog.Logger
	db       *pgxpool.Pool
	sessions *sessions.Sessions
	keys     *ByKey
	repo     *repo.Queries
}

func New(logger *slog.Logger, db *pgxpool.Pool) *Auth {
	return &Auth{
		logger:   logger,
		db:       db,
		keys:     NewKeyAuth(db),
		sessions: sessions.NewSessionAuth(logger),
		repo:     repo.New(db),
	}
}

func (s *Auth) Authorize(ctx context.Context, key string, schema *security.APIKeyScheme) (context.Context, error) {
	if schema == nil {
		panic("GOA has not passed a scheme") // TODO: figure something out here
	}

	switch schema.Name {
	case dsecurity.KeySecurityScheme:
		return s.keys.KeyBasedAuth(ctx, key, schema.Scopes)
	case dsecurity.SessionSecurityScheme:
		return s.sessions.SessionAuth(ctx, key)
	case dsecurity.ProjectSlugSecuritySchema:
		return s.checkProjectAccess(ctx, s.logger, s.db, key)
	default:
		return ctx, errors.New("unsupported security scheme")
	}
}

func (s *Auth) checkProjectAccess(ctx context.Context, logger *slog.Logger, db *pgxpool.Pool, projectSlug string) (context.Context, error) {
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	if !ok {
		return ctx, fmt.Errorf("project access: %w", ErrAuthNoSession)
	}

	projects, err := s.repo.ListProjectsByOrganization(ctx, authCtx.ActiveOrganizationID)
	if err != nil {
		logger.ErrorContext(ctx, "project access lookup error", slog.String("org_id", authCtx.ActiveOrganizationID), slog.String("error", err.Error()))
		return ctx, fmt.Errorf("project access: %w: %w", ErrAuthCheckFailed, err)
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
		logger.ErrorContext(ctx, "project access lookup error", slog.String("project_slug", projectSlug), slog.String("org_id", authCtx.ActiveOrganizationID))
		return ctx, fmt.Errorf("project access: %w", ErrAuthCheckFailed)
	}

	ctx = contextvalues.SetAuthContext(ctx, authCtx)
	return ctx, nil
}
