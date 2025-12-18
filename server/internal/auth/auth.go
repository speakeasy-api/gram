package auth

import (
	"context"
	"database/sql"
	"errors"
	"log/slog"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/speakeasy-api/gram/server/internal/attr"
	"github.com/speakeasy-api/gram/server/internal/auth/constants"
	"github.com/speakeasy-api/gram/server/internal/auth/repo"
	"github.com/speakeasy-api/gram/server/internal/auth/sessions"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/oops"
	"goa.design/goa/v3/security"
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
		keys:     NewKeyAuth(db, logger, sessions.Billing()),
		sessions: sessions,
		repo:     repo.New(db),
	}
}

func (s *Auth) Authorize(ctx context.Context, key string, schema *security.APIKeyScheme) (context.Context, error) {
	if schema == nil {
		panic("Goa has not passed a schema") // TODO: figure something out here
	}

	switch schema.Name {
	case constants.KeySecurityScheme:
		return s.keys.KeyBasedAuth(ctx, key, schema.RequiredScopes)
	case constants.SessionSecurityScheme:
		return s.sessions.Authenticate(ctx, key, false)
	case constants.ProjectSlugSecuritySchema:
		return s.checkProjectAccess(ctx, s.logger, key)
	default:
		return ctx, oops.E(oops.CodeUnauthorized, nil, "unsupported security scheme")
	}
}

func (s *Auth) checkProjectAccess(ctx context.Context, logger *slog.Logger, projectSlug string) (context.Context, error) {
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	if !ok {
		return ctx, oops.E(oops.CodeUnauthorized, nil, "no session found")
	}

	projects, err := s.repo.ListProjectsByOrganization(ctx, authCtx.ActiveOrganizationID)
	switch {
	case errors.Is(err, sql.ErrNoRows):
		return ctx, oops.E(oops.CodeForbidden, nil, "no projects found")
	case err != nil:
		return ctx, oops.E(oops.CodeUnexpected, err, "error checking project access").Log(ctx, logger, attr.SlogOrganizationID(authCtx.ActiveOrganizationID))
	}

	if projectSlug == "" && len(projects) == 1 {
		projectSlug = projects[0].Slug
	}

	if projectSlug == "" {
		return ctx, oops.E(oops.CodeBadRequest, nil, "empty project slug")
	}

	hasProjectAccess := false
	for _, project := range projects {
		if project.Slug == projectSlug {
			authCtx.ProjectID = &project.ID // This is important
			authCtx.ProjectSlug = &projectSlug
			hasProjectAccess = true
			break
		}
	}

	logger = logger.With(attr.SlogProjectSlug(projectSlug), attr.SlogOrganizationID(authCtx.ActiveOrganizationID))

	if !hasProjectAccess {
		return ctx, oops.C(oops.CodeForbidden).Log(ctx, logger)
	}

	ctx = contextvalues.SetAuthContext(ctx, authCtx)
	return ctx, nil
}

func (s *Auth) CheckProjectAccess(ctx context.Context, logger *slog.Logger, projectID uuid.UUID) error {
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	if !ok {
		return oops.E(oops.CodeUnauthorized, nil, "no session found")
	}

	id, err := s.repo.PokeProjectByID(ctx, repo.PokeProjectByIDParams{
		OrganizationID: authCtx.ActiveOrganizationID,
		ProjectID:      projectID,
	})
	switch {
	case errors.Is(err, sql.ErrNoRows):
		return oops.C(oops.CodeForbidden).Log(ctx, logger, attr.SlogOrganizationID(authCtx.ActiveOrganizationID))
	case err != nil:
		return oops.E(oops.CodeUnexpected, err, "error checking project access").Log(ctx, logger, attr.SlogOrganizationID(authCtx.ActiveOrganizationID))
	}

	if id == uuid.Nil {
		err := errors.New("check project access by id: database returned nil project id")
		return oops.E(oops.CodeForbidden, err, "%s", oops.CodeForbidden.UserMessage()).Log(ctx, logger, attr.SlogOrganizationID(authCtx.ActiveOrganizationID))
	}

	return nil
}
