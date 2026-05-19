package admin

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/redis/go-redis/v9"
	"go.opentelemetry.io/otel/trace"
	goahttp "goa.design/goa/v3/http"
	"goa.design/goa/v3/security"

	gen "github.com/speakeasy-api/gram/server/gen/admin"
	srv "github.com/speakeasy-api/gram/server/gen/http/admin/server"
	"github.com/speakeasy-api/gram/server/internal/admin/repo"
	"github.com/speakeasy-api/gram/server/internal/attr"
	"github.com/speakeasy-api/gram/server/internal/cache"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/conv"
	"github.com/speakeasy-api/gram/server/internal/encryption"
	"github.com/speakeasy-api/gram/server/internal/middleware"
	"github.com/speakeasy-api/gram/server/internal/oops"
)

type Service struct {
	tracer      trace.Tracer
	logger      *slog.Logger
	db          *pgxpool.Pool
	verifier    *Verifier
	loginStates cache.TypedCacheObject[LoginState]
	oidc        *OIDCClient
	sessions    *SessionStore
}

var _ gen.Service = (*Service)(nil)
var _ gen.Auther = (*Service)(nil)

func NewService(
	logger *slog.Logger,
	tracerProvider trace.TracerProvider,
	db *pgxpool.Pool,
	redisClient *redis.Client,
	oidcClient *OIDCClient,
	encryptionClient *encryption.Client,
) *Service {
	logger = logger.With(attr.SlogComponent("admin"))

	sessionStore := NewSessionStore(
		cache.NewTypedObjectCache[Session](
			logger.With(attr.SlogCacheNamespace("admin_session")),
			cache.NewRedisCacheAdapter(redisClient),
			cache.SuffixNone,
		),
		encryptionClient,
	)

	return &Service{
		tracer:   tracerProvider.Tracer("github.com/speakeasy-api/gram/server/internal/admin"),
		logger:   logger,
		db:       db,
		oidc:     oidcClient,
		sessions: sessionStore,
		verifier: NewVerifier(logger, sessionStore, oidcClient),
		loginStates: cache.NewTypedObjectCache[LoginState](
			logger.With(attr.SlogCacheNamespace("admin_login_state")),
			cache.NewRedisCacheAdapter(redisClient),
			cache.SuffixNone,
		),
	}
}

func Attach(mux goahttp.Muxer, service *Service) {
	endpoints := gen.NewEndpoints(service)
	endpoints.Use(middleware.MapErrors())
	endpoints.Use(middleware.TraceMethods(service.tracer))
	srv.Mount(
		mux,
		srv.New(endpoints, mux, goahttp.RequestDecoder, goahttp.ResponseEncoder, nil, nil),
	)
}

func (s *Service) APIKeyAuth(ctx context.Context, key string, schema *security.APIKeyScheme) (context.Context, error) {
	ctx, err := s.verifier.Authorize(ctx, key, schema)
	if err != nil {
		return ctx, fmt.Errorf("admin auth: %w", err)
	}
	return ctx, nil
}

func (s *Service) Login(ctx context.Context, payload *gen.LoginPayload) (res *gen.LoginResult, err error) {
	logger := s.logger

	state, err := randomString(32)
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "failed to generate oauth state").Log(ctx, logger)
	}
	verifier, err := randomString(32)
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "failed to generate pkce verifier").Log(ctx, logger)
	}
	challenge := pkceChallenge(verifier)

	returnTo := sanitizeReturnTo(conv.PtrValOrEmpty(payload.ReturnTo, ""), "/")

	rec := LoginState{
		State:        state,
		CodeVerifier: verifier,
		ReturnTo:     returnTo,
		CreatedAt:    time.Now().UTC(),
	}
	if err := s.loginStates.Store(ctx, rec); err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "failed to persist login state").Log(ctx, logger)
	}

	return &gen.LoginResult{
		Location:    s.oidc.AuthCodeURL(state, challenge),
		StateCookie: state,
	}, nil
}

func (s *Service) Callback(ctx context.Context, payload *gen.CallbackPayload) (res *gen.CallbackResult, err error) {
	logger := s.logger

	if payload.Code == "" {
		return nil, oops.E(oops.CodeBadRequest, nil, "missing code parameter")
	}
	if payload.StateParam == "" {
		return nil, oops.E(oops.CodeInvalid, nil, "missing state parameter")
	}

	// Verify the state cookie matches the state query param to prevent login CSRF.
	// The cookie is set by /admin/auth.login and must echo back the same random value.
	stateCookie := conv.PtrValOrEmpty(payload.StateCookie, "")
	if stateCookie == "" {
		return nil, oops.E(oops.CodeBadRequest, nil, "state cookie missing")
	}
	if stateCookie != payload.StateParam {
		return nil, oops.E(oops.CodeBadRequest, nil, "state cookie does not match state parameter")
	}

	rec, err := s.loginStates.Get(ctx, LoginStateCacheKey(payload.StateParam))
	if err != nil {
		return nil, oops.E(oops.CodeBadRequest, err, "unknown or expired login state").Log(ctx, logger)
	}

	if err := s.loginStates.DeleteByKey(ctx, LoginStateCacheKey(payload.StateParam)); err != nil {
		s.logger.WarnContext(ctx, "delete login state", attr.SlogError(err))
	}

	tok, err := s.oidc.Exchange(ctx, payload.Code, rec.CodeVerifier)
	if err != nil {
		return nil, oops.E(oops.CodeUnauthorized, err, "oauth code exchange failed").Log(ctx, logger)
	}

	idToken, err := ExtractIDToken(tok)
	if err != nil {
		return nil, oops.E(oops.CodeUnauthorized, err, "oidc id_token missing").Log(ctx, logger)
	}

	identity, err := s.oidc.VerifyIDToken(ctx, idToken)
	switch {
	case errors.Is(err, ErrAdminDomainNotAllowed):
		return nil, oops.E(oops.CodeForbidden, err, "oidc account is not authorized for admin access").Log(ctx, logger)
	case err != nil:
		return nil, oops.E(oops.CodeUnauthorized, err, "oidc id_token verification failed").Log(ctx, logger)
	}

	sessionID, err := s.sessions.Store(ctx, StoreParams{
		Email:        identity.Email,
		Name:         identity.Name,
		OIDCSubject:  identity.OIDCSubject,
		HD:           identity.HD,
		AccessToken:  tok.AccessToken,
		RefreshToken: tok.RefreshToken,
		ExpiresAt:    tok.Expiry,
	})
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "failed to persist admin session").Log(ctx, logger)
	}

	s.logger.InfoContext(ctx, "admin session created", attr.SlogAuthUserEmail(identity.Email))

	return &gen.CallbackResult{
		Location:  rec.ReturnTo,
		SessionID: sessionID,
	}, nil
}

func (s *Service) Logout(ctx context.Context, payload *gen.LogoutPayload) error {
	sessionID := conv.PtrValOrEmpty(payload.SessionID, "")
	if sessionID != "" {
		if err := s.sessions.Delete(ctx, sessionID); err != nil {
			return oops.E(oops.CodeUnexpected, err, "failed to delete admin session").Log(ctx, s.logger)
		}
	}

	// Also honor a context-populated session id in case the cookie is already
	// absent but a token is still present elsewhere (e.g. an admin revoking a
	// session identified by a foreign cookie).
	if tok, ok := contextvalues.GetAdminSessionTokenFromContext(ctx); ok && tok != "" {
		if err := s.sessions.Delete(ctx, tok); err != nil {
			return oops.E(oops.CodeUnexpected, err, "failed to delete injected admin session").Log(ctx, s.logger)
		}
	}

	return nil
}

func (s *Service) GetProject(ctx context.Context, payload *gen.GetProjectPayload) (*gen.AdminProjectDetail, error) {
	queries := repo.New(s.db)

	if id, err := uuid.Parse(payload.IDOrSlug); err == nil {
		row, err := queries.AdminGetProjectDetailByID(ctx, id)
		switch {
		case errors.Is(err, pgx.ErrNoRows):
			return nil, oops.C(oops.CodeNotFound)
		case err != nil:
			return nil, oops.E(oops.CodeUnexpected, err, "lookup project detail by id").Log(ctx, s.logger)
		}
		return adminProjectDetailFromIDRow(row), nil
	}

	row, err := queries.AdminGetProjectDetailBySlug(ctx, payload.IDOrSlug)
	switch {
	case errors.Is(err, pgx.ErrNoRows):
		return nil, oops.C(oops.CodeNotFound)
	case err != nil:
		return nil, oops.E(oops.CodeUnexpected, err, "lookup project detail by slug").Log(ctx, s.logger)
	}
	return adminProjectDetailFromSlugRow(row), nil
}

func adminProjectDetailFromIDRow(row repo.AdminGetProjectDetailByIDRow) *gen.AdminProjectDetail {
	logo := uuidPtr(row.LogoAssetID)
	runner := conv.FromPGText[string](row.FunctionsRunnerVersion)
	return &gen.AdminProjectDetail{
		ID:                     row.ID.String(),
		Name:                   row.Name,
		Slug:                   row.Slug,
		OrganizationID:         row.OrganizationID,
		LogoAssetID:            logo,
		FunctionsRunnerVersion: runner,
		ToolsetCount:           int(row.ToolsetCount),
		DeploymentCount:        int(row.DeploymentCount),
		HTTPToolCount:          int(row.HttpToolCount),
		EnvironmentCount:       int(row.EnvironmentCount),
		APIKeyCount:            int(row.ApiKeyCount),
		AssistantCount:         int(row.AssistantCount),
		CreatedAt:              row.CreatedAt.Time.Format(time.RFC3339),
		UpdatedAt:              row.UpdatedAt.Time.Format(time.RFC3339),
	}
}

func adminProjectDetailFromSlugRow(row repo.AdminGetProjectDetailBySlugRow) *gen.AdminProjectDetail {
	logo := uuidPtr(row.LogoAssetID)
	runner := conv.FromPGText[string](row.FunctionsRunnerVersion)
	return &gen.AdminProjectDetail{
		ID:                     row.ID.String(),
		Name:                   row.Name,
		Slug:                   row.Slug,
		OrganizationID:         row.OrganizationID,
		LogoAssetID:            logo,
		FunctionsRunnerVersion: runner,
		ToolsetCount:           int(row.ToolsetCount),
		DeploymentCount:        int(row.DeploymentCount),
		HTTPToolCount:          int(row.HttpToolCount),
		EnvironmentCount:       int(row.EnvironmentCount),
		APIKeyCount:            int(row.ApiKeyCount),
		AssistantCount:         int(row.AssistantCount),
		CreatedAt:              row.CreatedAt.Time.Format(time.RFC3339),
		UpdatedAt:              row.UpdatedAt.Time.Format(time.RFC3339),
	}
}

func uuidPtr(u uuid.NullUUID) *string {
	if !u.Valid {
		return nil
	}
	s := u.UUID.String()
	return &s
}

const (
	listOrganizationsDefaultLimit = 50
	listOrganizationsMaxLimit     = 100
)

func (s *Service) ListOrganizations(ctx context.Context, payload *gen.ListOrganizationsPayload) (*gen.AdminListOrganizationsResult, error) {
	queries := repo.New(s.db)

	limit := int32(listOrganizationsDefaultLimit)
	if payload.Limit != nil {
		l := *payload.Limit
		if l < 1 {
			l = listOrganizationsDefaultLimit
		}
		if l > listOrganizationsMaxLimit {
			l = listOrganizationsMaxLimit
		}
		limit = int32(l)
	}

	rows, err := queries.AdminListOrganizations(ctx, repo.AdminListOrganizationsParams{
		Q:               conv.PtrToPGText(payload.Q),
		AccountType:     conv.PtrToPGText(payload.AccountType),
		IncludeDisabled: conv.PtrValOr(payload.IncludeDisabled, false),
		AfterID:         conv.PtrToPGText(payload.Cursor),
		PageLimit:       limit,
	})
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "list organizations").Log(ctx, s.logger)
	}

	orgs := make([]*gen.AdminOrganization, len(rows))
	for i := range rows {
		orgs[i] = adminOrganizationFromRow(rows[i])
	}

	var nextCursor *string
	if len(rows) == int(limit) && len(rows) > 0 {
		id := rows[len(rows)-1].ID
		nextCursor = &id
	}

	return &gen.AdminListOrganizationsResult{
		Organizations: orgs,
		NextCursor:    nextCursor,
	}, nil
}

func (s *Service) ListOrganizationProjects(ctx context.Context, payload *gen.ListOrganizationProjectsPayload) (*gen.AdminListOrganizationProjectsResult, error) {
	rows, err := repo.New(s.db).AdminListProjectsForOrganization(ctx, payload.OrganizationID)
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "list projects for organization").Log(ctx, s.logger)
	}

	projects := make([]*gen.AdminProject, len(rows))
	for i, row := range rows {
		projects[i] = &gen.AdminProject{
			ID:        row.ID.String(),
			Name:      row.Name,
			Slug:      row.Slug,
			CreatedAt: row.CreatedAt.Time.Format(time.RFC3339),
			UpdatedAt: row.UpdatedAt.Time.Format(time.RFC3339),
		}
	}

	return &gen.AdminListOrganizationProjectsResult{Projects: projects}, nil
}

func (s *Service) UpdateOrganization(ctx context.Context, payload *gen.UpdateOrganizationPayload) (*gen.AdminOrganization, error) {
	if payload.AccountType == nil && payload.Whitelisted == nil {
		return nil, oops.E(oops.CodeBadRequest, nil, "at least one of account_type or whitelisted must be supplied")
	}

	queries := repo.New(s.db)
	if err := queries.AdminUpdateOrganization(ctx, repo.AdminUpdateOrganizationParams{
		ID:          payload.ID,
		AccountType: conv.PtrToPGText(payload.AccountType),
		Whitelisted: conv.PtrToPGBool(payload.Whitelisted),
	}); err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "update organization").Log(ctx, s.logger)
	}

	row, err := queries.AdminGetOrganizationByIDOrSlug(ctx, payload.ID)
	switch {
	case errors.Is(err, pgx.ErrNoRows):
		return nil, oops.C(oops.CodeNotFound)
	case err != nil:
		return nil, oops.E(oops.CodeUnexpected, err, "fetch organization after update").Log(ctx, s.logger)
	}
	return adminOrganizationFromGetRow(row), nil
}

func (s *Service) GetOrganization(ctx context.Context, payload *gen.GetOrganizationPayload) (*gen.AdminOrganization, error) {
	row, err := repo.New(s.db).AdminGetOrganizationByIDOrSlug(ctx, payload.IDOrSlug)
	switch {
	case errors.Is(err, pgx.ErrNoRows):
		return nil, oops.C(oops.CodeNotFound)
	case err != nil:
		return nil, oops.E(oops.CodeUnexpected, err, "lookup organization by id or slug").Log(ctx, s.logger)
	}
	return adminOrganizationFromGetRow(row), nil
}

func adminOrganizationFromGetRow(row repo.AdminGetOrganizationByIDOrSlugRow) *gen.AdminOrganization {
	return &gen.AdminOrganization{
		ID:                 row.ID,
		Name:               row.Name,
		Slug:               row.Slug,
		AccountType:        row.AccountType,
		WorkosID:           conv.FromPGText[string](row.WorkosID),
		Whitelisted:        row.Whitelisted,
		DisabledAt:         pgTimestampPtr(row.DisabledAt),
		FreeTrialStartedAt: pgTimestampPtr(row.FreeTrialStartedAt),
		FreeTrialEndsAt:    pgTimestampPtr(row.FreeTrialEndsAt),
		MemberCount:        int(row.MemberCount),
		CreatedAt:          row.CreatedAt.Time.Format(time.RFC3339),
		UpdatedAt:          row.UpdatedAt.Time.Format(time.RFC3339),
	}
}

func adminOrganizationFromRow(row repo.AdminListOrganizationsRow) *gen.AdminOrganization {
	return &gen.AdminOrganization{
		ID:                 row.ID,
		Name:               row.Name,
		Slug:               row.Slug,
		AccountType:        row.AccountType,
		WorkosID:           conv.FromPGText[string](row.WorkosID),
		Whitelisted:        row.Whitelisted,
		DisabledAt:         pgTimestampPtr(row.DisabledAt),
		FreeTrialStartedAt: pgTimestampPtr(row.FreeTrialStartedAt),
		FreeTrialEndsAt:    pgTimestampPtr(row.FreeTrialEndsAt),
		MemberCount:        int(row.MemberCount),
		CreatedAt:          row.CreatedAt.Time.Format(time.RFC3339),
		UpdatedAt:          row.UpdatedAt.Time.Format(time.RFC3339),
	}
}

func pgTimestampPtr(t pgtype.Timestamptz) *string {
	if !t.Valid {
		return nil
	}
	s := t.Time.Format(time.RFC3339)
	return &s
}
