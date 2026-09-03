package auth

import (
	"context"
	"errors"
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
		err = s.requireResolvedProjectAccess(ctx)
		if err != nil {
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

	var schemeAttr slog.Attr
	var errAttr slog.Attr
	switch scheme {
	case constants.KeySecurityScheme:
		schemeAttr = attr.SlogRequestAuthAPIKeyScheme(err == nil)
		if err != nil {
			errAttr = attr.SlogRequestAuthSchemeAPIKeyError(err.Error())
		}
	case constants.SessionSecurityScheme:
		schemeAttr = attr.SlogRequestAuthSessionScheme(err == nil)
		if err != nil {
			errAttr = attr.SlogRequestAuthSchemeSessionError(err.Error())
		}
	case constants.ProjectSlugSecuritySchema:
		schemeAttr = attr.SlogRequestAuthProjectScheme(err == nil)
		if err != nil {
			errAttr = attr.SlogRequestAuthSchemeProjectSlugError(err.Error())
		}
	default:
		return
	}

	attrs := []slog.Attr{schemeAttr}
	if err != nil {
		attrs = append(attrs, errAttr)
	}

	if !wide.Contains(ctx, string(attr.RequestAuthOrganizationIDKey)) {
		attrs = append(
			attrs,
			attr.SlogRequestAuthOrganizationID(authCtx.ActiveOrganizationID),
			attr.SlogRequestAuthOrganizationSlug(authCtx.OrganizationSlug),
			attr.SlogRequestAuthAccountType(authCtx.AccountType),
		)
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
	}
	if authCtx.ProjectID != nil && !wide.Contains(ctx, string(attr.RequestAuthProjectIDKey)) {
		attrs = append(attrs, attr.SlogRequestAuthProjectID(authCtx.ProjectID.String()))
	}
	if authCtx.ProjectSlug != nil && !wide.Contains(ctx, string(attr.RequestAuthProjectSlugKey)) {
		attrs = append(attrs, attr.SlogRequestAuthProjectSlug(*authCtx.ProjectSlug))
	}

	wide.Push(ctx, attrs...)
}
