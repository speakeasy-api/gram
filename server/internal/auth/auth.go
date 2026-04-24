package auth

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"log/slog"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"goa.design/goa/v3/security"

	"github.com/speakeasy-api/gram/server/internal/attr"
	"github.com/speakeasy-api/gram/server/internal/auth/repo"
	"github.com/speakeasy-api/gram/server/internal/auth/sessions"
	"github.com/speakeasy-api/gram/server/internal/authz"
	"github.com/speakeasy-api/gram/server/internal/constants"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/oops"
	"github.com/speakeasy-api/gram/server/internal/wide"
)

type Auth struct {
	logger   *slog.Logger
	db       *pgxpool.Pool
	sessions *sessions.Manager
	keys     *ByKey
	repo     *repo.Queries
	authz    *authz.Engine
}

func New(logger *slog.Logger, db *pgxpool.Pool, sessions *sessions.Manager, authzEngine *authz.Engine) *Auth {
	logger = logger.With(attr.SlogComponent("authorizer"))
	return &Auth{
		logger:   logger,
		db:       db,
		keys:     NewKeyAuth(db, logger, sessions.Billing()),
		sessions: sessions,
		repo:     repo.New(db),
		authz:    authzEngine,
	}
}

func (s *Auth) Authorize(ctx context.Context, key string, scheme *security.APIKeyScheme) (context.Context, error) {
	if scheme == nil {
		panic("Goa has not passed a schema") // TODO: figure something out here
	}

	var err error

	switch scheme.Name {
	case constants.KeySecurityScheme:
		ctx, err = s.keys.KeyBasedAuth(ctx, key, scheme.RequiredScopes)
		s.logAuthContext(ctx, err, scheme.Name, attr.SlogRequestAuthAPIKeyScheme, attr.SlogRequestAuthSchemeAPIKeyError)
	case constants.SessionSecurityScheme:
		ctx, err = s.sessions.Authenticate(ctx, key)
		s.logAuthContext(ctx, err, scheme.Name, attr.SlogRequestAuthSessionScheme, attr.SlogRequestAuthSchemeSessionError)
	case constants.ProjectSlugSecuritySchema:
		ctx, err = s.checkProjectAccess(ctx, s.logger, key)
		s.logAuthContext(ctx, err, scheme.Name, attr.SlogRequestAuthProjectScheme, attr.SlogRequestAuthSchemeProjectSlugError)
	default:
		err = oops.E(oops.CodeUnauthorized, nil, "unsupported security scheme")
	}
	if err != nil {
		return ctx, err
	}
	ctx, err = s.authz.PrepareContext(ctx)
	if err != nil {
		return ctx, oops.E(oops.CodeUnexpected, err, "load access grants").Log(ctx, s.logger)
	}

	return ctx, nil
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

func (s *Auth) logAuthContext(
	ctx context.Context,
	err error,
	scheme string,
	schemeAttr func(matched bool) slog.Attr,
	errAttr func(v string) slog.Attr,
) {
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	if !ok {
		return
	}

	attrs := []slog.Attr{
		schemeAttr(err == nil),
		attr.SlogRequestAuthOrganizationID(authCtx.ActiveOrganizationID),
		attr.SlogRequestAuthOrganizationSlug(authCtx.OrganizationSlug),
		attr.SlogRequestAuthAccountType(authCtx.AccountType),
	}
	if err != nil {
		attrs = append(attrs, errAttr(err.Error()))
	}
	if authCtx.UserID != "" {
		attrs = append(attrs, attr.SlogRequestAuthUserID(authCtx.UserID))
	}
	if authCtx.ExternalUserID != "" {
		attrs = append(attrs, attr.SlogRequestAuthUserExternalID(authCtx.ExternalUserID))
	}
	if authCtx.Email != nil {
		attrs = append(attrs, attr.SlogRequestAuthUserEmail(*authCtx.Email))
	}
	if authCtx.APIKeyID != "" {
		attrs = append(attrs, attr.SlogRequestAuthAPIKeyID(authCtx.APIKeyID))
	}
	if authCtx.SessionID != nil {
		attrs = append(attrs, attr.SlogRequestAuthSessionID(*authCtx.SessionID))
	}
	if authCtx.ProjectID != nil {
		attrs = append(attrs, attr.SlogRequestAuthProjectID(authCtx.ProjectID.String()))
	}
	if authCtx.ProjectSlug != nil {
		attrs = append(attrs, attr.SlogRequestAuthProjectSlug(*authCtx.ProjectSlug))
	}

	wide.Push(ctx, attrs...)

	if err != nil {
		s.logger.LogAttrs(ctx, slog.LevelError, fmt.Sprintf("auth scheme check failed (%s)", scheme), attrs...)
	} else {
		s.logger.LogAttrs(ctx, slog.LevelInfo, fmt.Sprintf("auth scheme check passed (%s)", scheme), attrs...)
	}
}
