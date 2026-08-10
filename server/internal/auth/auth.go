package auth

import (
	"context"
	"errors"
	"fmt"
	"log/slog"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"goa.design/goa/v3/security"

	"github.com/speakeasy-api/gram/server/internal/attr"
	"github.com/speakeasy-api/gram/server/internal/auth/repo"
	"github.com/speakeasy-api/gram/server/internal/auth/sessions"
	"github.com/speakeasy-api/gram/server/internal/authz"
	"github.com/speakeasy-api/gram/server/internal/constants"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/oops"
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
	defer func() {
		s.logAuthContext(ctx, err, scheme.Name)
	}()

	switch scheme.Name {
	case constants.KeySecurityScheme:
		ctx, err = s.keys.KeyBasedAuth(ctx, key, scheme.RequiredScopes)
	case constants.SessionSecurityScheme:
		ctx, err = s.sessions.Authenticate(ctx, key)
	case constants.ProjectSlugSecuritySchema:
		ctx, err = s.checkProjectAccess(ctx, s.logger, key)
	default:
		err = oops.E(oops.CodeUnauthorized, nil, "unsupported security scheme")
	}
	if err != nil {
		return ctx, err
	}
	ctx, err = s.authz.PrepareContext(ctx)
	if err != nil {
		return ctx, oops.E(oops.CodeUnexpected, err, "load access grants").LogError(ctx, s.logger)
	}

	// After resolving Gram-Project, require the caller holds project:read on
	// that project. When RBAC is off (or the caller is an API key), Require is
	// a no-op and org/project-bound key scoping above remains authoritative.
	if scheme.Name == constants.ProjectSlugSecuritySchema {
		if err := s.requireResolvedProjectAccess(ctx); err != nil {
			return ctx, err
		}
	}

	return ctx, nil
}

// requireResolvedProjectAccess ensures the authenticated principal is granted
// project:read on the project selected via the Gram-Project header. This closes
// the gap where checkProjectAccess only verified the project belonged to the
// caller's organization (AIS-425).
func (s *Auth) requireResolvedProjectAccess(ctx context.Context) error {
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	if !ok || authCtx == nil || authCtx.ProjectID == nil {
		return oops.E(oops.CodeUnauthorized, nil, "no session found")
	}

	return s.authz.Require(ctx, authz.Check{
		Scope:        authz.ScopeProjectRead,
		ResourceKind: "",
		ResourceID:   authCtx.ProjectID.String(),
		Dimensions:   nil,
	})
}

func (s *Auth) checkProjectAccess(ctx context.Context, logger *slog.Logger, projectSlug string) (context.Context, error) {
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	if !ok {
		return ctx, oops.E(oops.CodeUnauthorized, nil, "no session found")
	}
	boundProjectID := authCtx.ProjectID

	projects, err := s.repo.ListProjectsByOrganization(ctx, authCtx.ActiveOrganizationID)
	switch {
	case errors.Is(err, pgx.ErrNoRows):
		return ctx, oops.E(oops.CodeForbidden, nil, "no projects found")
	case err != nil:
		return ctx, oops.E(oops.CodeUnexpected, err, "error checking project access").LogError(ctx, logger, attr.SlogOrganizationID(authCtx.ActiveOrganizationID))
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
			if boundProjectID != nil && project.ID != *boundProjectID {
				return ctx, oops.C(oops.CodeForbidden)
			}
			authCtx.ProjectID = &project.ID // This is important
			authCtx.ProjectSlug = &projectSlug
			hasProjectAccess = true
			break
		}
	}

	if !hasProjectAccess {
		return ctx, oops.C(oops.CodeForbidden)
	}
	if IsLiteLLMAPIKeyName(authCtx.APIKeyName) && authCtx.APIKeyID != "" {
		keyID, parseErr := uuid.Parse(authCtx.APIKeyID)
		if parseErr != nil {
			logger.WarnContext(ctx, "failed to parse LiteLLM API key ID",
				attr.SlogError(parseErr),
				attr.SlogAPIKeyID(authCtx.APIKeyID),
			)
		} else if touchErr := s.keys.keyDB.UpdateAPIKeyLastAccessedAt(ctx, keyID); touchErr != nil {
			logger.WarnContext(ctx, "failed to update LiteLLM API key last accessed at",
				attr.SlogError(touchErr),
				attr.SlogAPIKeyID(authCtx.APIKeyID),
				attr.SlogOrganizationID(authCtx.ActiveOrganizationID),
			)
		}
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
	case errors.Is(err, pgx.ErrNoRows):
		return oops.C(oops.CodeForbidden).LogWarn(ctx, logger, attr.SlogOrganizationID(authCtx.ActiveOrganizationID))
	case err != nil:
		return oops.E(oops.CodeUnexpected, err, "error checking project access").LogError(ctx, logger, attr.SlogOrganizationID(authCtx.ActiveOrganizationID))
	}

	if id == uuid.Nil {
		err := errors.New("check project access by id: database returned nil project id")
		return oops.E(oops.CodeForbidden, err, "%s", oops.CodeForbidden.UserMessage()).LogError(ctx, logger, attr.SlogOrganizationID(authCtx.ActiveOrganizationID))
	}

	return nil
}

func (s *Auth) logAuthContext(ctx context.Context, err error, scheme string) {
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	if !ok {
		return
	}

	attrs := []any{
		attr.SlogAuthScheme(scheme),
		attr.SlogAuthOrganizationID(authCtx.ActiveOrganizationID),
		attr.SlogAuthOrganizationSlug(authCtx.OrganizationSlug),
		attr.SlogAuthAccountType(authCtx.AccountType),
	}
	if err != nil {
		attrs = append(attrs, attr.SlogError(err))
	}
	if authCtx.UserID != "" {
		attrs = append(attrs, attr.SlogAuthUserID(authCtx.UserID))
	}
	if authCtx.ExternalUserID != "" {
		attrs = append(attrs, attr.SlogAuthUserExternalID(authCtx.ExternalUserID))
	}
	if authCtx.Email != nil {
		attrs = append(attrs, attr.SlogAuthUserEmail(*authCtx.Email))
	}
	if authCtx.APIKeyID != "" {
		attrs = append(attrs, attr.SlogAuthAPIKeyID(authCtx.APIKeyID))
	}
	if authCtx.SessionID != nil {
		attrs = append(attrs, attr.SlogAuthSessionID(*authCtx.SessionID))
	}
	if authCtx.ProjectID != nil {
		attrs = append(attrs, attr.SlogAuthProjectID(authCtx.ProjectID.String()))
	}
	if authCtx.ProjectSlug != nil {
		attrs = append(attrs, attr.SlogAuthProjectSlug(*authCtx.ProjectSlug))
	}

	if err != nil {
		s.logger.WarnContext(ctx, fmt.Sprintf("auth scheme check failed (%s)", scheme), attrs...)
	} else {
		s.logger.InfoContext(ctx, fmt.Sprintf("auth scheme check passed (%s)", scheme), attrs...)
	}
}
