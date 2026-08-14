package admin

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"math"
	"net/http"
	"net/url"
	"strings"
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
	tracer         trace.Tracer
	logger         *slog.Logger
	db             *pgxpool.Pool
	verifier       *Verifier
	loginStates    cache.TypedCacheObject[LoginState]
	oidc           *OIDCClient
	sessions       *SessionStore
	allowedOrigins []string
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
	allowedOrigins []string,
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
		tracer:         tracerProvider.Tracer("github.com/speakeasy-api/gram/server/internal/admin"),
		logger:         logger,
		db:             db,
		oidc:           oidcClient,
		sessions:       sessionStore,
		verifier:       NewVerifier(logger, sessionStore, oidcClient),
		allowedOrigins: allowedOrigins,
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

	// See sessionInfo in session_handler.go for why this one route is hand
	// written rather than generated from the Goa design.
	mux.Handle(
		http.MethodGet,
		"/admin/session.get",
		oops.ErrHandle(service.logger, service.handleGetSession).ServeHTTP,
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
		return nil, oops.E(oops.CodeUnexpected, err, "failed to generate oauth state").LogError(ctx, logger)
	}
	verifier, err := randomString(32)
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "failed to generate pkce verifier").LogError(ctx, logger)
	}
	challenge := pkceChallenge(verifier)

	returnTo := sanitizeReturnTo(conv.PtrValOrEmpty(payload.ReturnTo, ""), "/", s.allowedOrigins)
	prompt := conv.PtrValOrEmpty(payload.Prompt, "")

	rec := LoginState{
		State:        state,
		CodeVerifier: verifier,
		ReturnTo:     returnTo,
		CreatedAt:    time.Now().UTC(),
	}
	if err := s.loginStates.Store(ctx, rec); err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "failed to persist login state").LogError(ctx, logger)
	}

	return &gen.LoginResult{
		Location:    s.oidc.AuthCodeURL(state, challenge, prompt),
		StateCookie: state,
	}, nil
}

func (s *Service) Callback(ctx context.Context, payload *gen.CallbackPayload) (res *gen.CallbackResult, err error) {
	logger := s.logger

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
		return nil, oops.E(oops.CodeBadRequest, err, "unknown or expired login state").LogError(ctx, logger)
	}

	if err := s.loginStates.DeleteByKey(ctx, LoginStateCacheKey(payload.StateParam)); err != nil {
		s.logger.WarnContext(ctx, "delete login state", attr.SlogError(err))
	}

	// Handle OAuth errors returned by the provider (e.g. error=login_required when
	// prompt=none was used). Fall back to an interactive login preserving return_to.
	if oauthErr := conv.PtrValOrEmpty(payload.Error, ""); oauthErr != "" {
		s.logger.InfoContext(ctx, fmt.Sprintf("oauth provider returned %q, falling back to interactive login", oauthErr))
		return &gen.CallbackResult{
			Location:  fmt.Sprintf("/admin/auth.login?return_to=%s", url.QueryEscape(rec.ReturnTo)),
			SessionID: "",
		}, nil
	}

	if conv.PtrValOrEmpty(payload.Code, "") == "" {
		return nil, oops.E(oops.CodeBadRequest, nil, "missing code parameter")
	}

	tok, err := s.oidc.Exchange(ctx, conv.PtrValOrEmpty(payload.Code, ""), rec.CodeVerifier)
	if err != nil {
		return nil, oops.E(oops.CodeUnauthorized, err, "oauth code exchange failed").LogError(ctx, logger)
	}

	idToken, err := ExtractIDToken(tok)
	if err != nil {
		return nil, oops.E(oops.CodeUnauthorized, err, "oidc id_token missing").LogError(ctx, logger)
	}

	identity, err := s.oidc.VerifyIDToken(ctx, idToken)
	switch {
	case errors.Is(err, ErrAdminDomainNotAllowed):
		return nil, oops.E(oops.CodeForbidden, err, "oidc account is not authorized for admin access").LogError(ctx, logger)
	case err != nil:
		return nil, oops.E(oops.CodeUnauthorized, err, "oidc id_token verification failed").LogError(ctx, logger)
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
		return nil, oops.E(oops.CodeUnexpected, err, "failed to persist admin session").LogError(ctx, logger)
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
			return oops.E(oops.CodeUnexpected, err, "failed to delete admin session").LogError(ctx, s.logger)
		}
	}

	// Also honor a context-populated session id in case the cookie is already
	// absent but a token is still present elsewhere (e.g. an admin revoking a
	// session identified by a foreign cookie).
	if tok, ok := contextvalues.GetAdminSessionTokenFromContext(ctx); ok && tok != "" {
		if err := s.sessions.Delete(ctx, tok); err != nil {
			return oops.E(oops.CodeUnexpected, err, "failed to delete injected admin session").LogError(ctx, s.logger)
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
			return nil, oops.E(oops.CodeUnexpected, err, "lookup project detail by id").LogError(ctx, s.logger)
		}
		return adminProjectDetailFromIDRow(row), nil
	}

	row, err := queries.AdminGetProjectDetailBySlug(ctx, payload.IDOrSlug)
	switch {
	case errors.Is(err, pgx.ErrNoRows):
		return nil, oops.C(oops.CodeNotFound)
	case err != nil:
		return nil, oops.E(oops.CodeUnexpected, err, "lookup project detail by slug").LogError(ctx, s.logger)
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

// The columns the ORDER BY ladder in AdminListOrganizations knows. The opaque
// ids are absent on purpose: an order built from them tells an operator nothing.
//
// This map cannot widen what the ladder accepts. The ladder matches these seven
// literals and nothing else, so an unrecognised sort key collapses to the
// tiebreaker with or without the check here. It is defense in depth, and the one
// place a reader can see the accepted set without reading the SQL.
var listOrganizationsSortColumns = map[string]bool{
	"name":          true,
	"slug":          true,
	"account_type":  true,
	"member_count":  true,
	"created_at":    true,
	"disabled_at":   true,
	"trial_ends_at": true,
}

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

	// Sort or page selects offset paging. Without either, the request keeps the
	// cursor walk the shipped dashboard still depends on.
	offsetMode := payload.Sort != nil || payload.Page != nil

	// An unknown sort key falls back to the default order instead of failing: the
	// value comes from a URL operators paste to each other, and a typo should not
	// break the page.
	sortBy := ""
	if payload.Sort != nil {
		if key := strings.ToLower(*payload.Sort); listOrganizationsSortColumns[key] {
			sortBy = key
		}
	}
	sortDir := "asc"
	if payload.Direction != nil && strings.EqualFold(*payload.Direction, "desc") {
		sortDir = "desc"
	}

	var afterID pgtype.Text
	var pageOffset int64
	fetchLimit := limit
	if offsetMode {
		pageOffset = listOrganizationsOffset(payload.Page, limit)
	} else {
		afterID = conv.PtrToPGText(payload.Cursor)
		// Over-fetch one row to learn whether a next page exists.
		fetchLimit = limit + 1
	}

	rows, err := queries.AdminListOrganizations(ctx, repo.AdminListOrganizationsParams{
		Q:               conv.PtrToPGText(payload.Q),
		AccountType:     conv.PtrToPGText(payload.AccountType),
		IncludeDisabled: conv.PtrValOr(payload.IncludeDisabled, false),
		AfterID:         afterID,
		SortBy:          sortBy,
		SortDir:         sortDir,
		PageOffset:      pageOffset,
		PageLimit:       fetchLimit,
	})
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "list organizations").LogError(ctx, s.logger)
	}

	// Counted separately from the page. A page past the end holds no row to carry
	// a count, and in cursor mode the page query has already discarded everything
	// before the cursor.
	total, err := queries.AdminCountOrganizations(ctx, repo.AdminCountOrganizationsParams{
		Q:               conv.PtrToPGText(payload.Q),
		AccountType:     conv.PtrToPGText(payload.AccountType),
		IncludeDisabled: conv.PtrValOr(payload.IncludeDisabled, false),
	})
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "count organizations").LogError(ctx, s.logger)
	}

	var nextCursor *string
	if !offsetMode && len(rows) > int(limit) {
		rows = rows[:limit]
		id := rows[limit-1].ID
		nextCursor = &id
	}

	orgs := make([]*gen.AdminOrganization, len(rows))
	for i := range rows {
		orgs[i] = adminOrganizationFromRow(rows[i])
	}

	return &gen.AdminListOrganizationsResult{
		Organizations: orgs,
		NextCursor:    nextCursor,
		Total:         total,
	}, nil
}

// listOrganizationsOffset turns a 1-based page number into a row offset, with
// pages below 1 clamping to the first page.
func listOrganizationsOffset(page *int, limit int32) int64 {
	n := int64(1)
	if page != nil && *page > 1 {
		n = int64(*page)
	}

	// Cap the page so that a hand-typed page number cannot overflow the multiply
	// into a negative offset, which Postgres rejects. The ceiling carries no +1:
	// limit is not a constant, so at limit 1 the addition overflows to MinInt64
	// and the guard fires on every page.
	if maxPage := math.MaxInt64 / int64(limit); n > maxPage {
		n = maxPage
	}

	return (n - 1) * int64(limit)
}

func (s *Service) ListOrganizationMembers(ctx context.Context, payload *gen.ListOrganizationMembersPayload) (*gen.AdminListOrganizationMembersResult, error) {
	rows, err := repo.New(s.db).AdminListOrganizationMembers(ctx, payload.OrganizationID)
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "list members for organization").LogError(ctx, s.logger)
	}

	members := make([]*gen.AdminOrganizationMember, len(rows))
	for i, row := range rows {
		var lastLogin *string
		if row.LastLogin.Valid {
			s := row.LastLogin.Time.Format(time.RFC3339)
			lastLogin = &s
		}
		members[i] = &gen.AdminOrganizationMember{
			ID:          row.ID,
			Email:       row.Email,
			DisplayName: row.DisplayName,
			LastLogin:   lastLogin,
			CreatedAt:   row.CreatedAt.Time.Format(time.RFC3339),
			UpdatedAt:   row.UpdatedAt.Time.Format(time.RFC3339),
		}
	}

	return &gen.AdminListOrganizationMembersResult{Members: members}, nil
}

func (s *Service) ListOrganizationProjects(ctx context.Context, payload *gen.ListOrganizationProjectsPayload) (*gen.AdminListOrganizationProjectsResult, error) {
	rows, err := repo.New(s.db).AdminListProjectsForOrganization(ctx, payload.OrganizationID)
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "list projects for organization").LogError(ctx, s.logger)
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
		return nil, oops.E(oops.CodeUnexpected, err, "update organization").LogError(ctx, s.logger)
	}

	row, err := queries.AdminGetOrganizationByIDOrSlug(ctx, payload.ID)
	switch {
	case errors.Is(err, pgx.ErrNoRows):
		return nil, oops.C(oops.CodeNotFound)
	case err != nil:
		return nil, oops.E(oops.CodeUnexpected, err, "fetch organization after update").LogError(ctx, s.logger)
	}

	return adminOrganizationFromGetRow(row), nil
}

func (s *Service) GetOrganization(ctx context.Context, payload *gen.GetOrganizationPayload) (*gen.AdminOrganization, error) {
	row, err := repo.New(s.db).AdminGetOrganizationByIDOrSlug(ctx, payload.IDOrSlug)
	switch {
	case errors.Is(err, pgx.ErrNoRows):
		return nil, oops.C(oops.CodeNotFound)
	case err != nil:
		return nil, oops.E(oops.CodeUnexpected, err, "lookup organization by id or slug").LogError(ctx, s.logger)
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
		TrialState:         &row.TrialState,
		TrialEndsAt:        pgTimestampPtr(row.TrialEndsAt),
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
		TrialState:         &row.TrialState,
		TrialEndsAt:        pgTimestampPtr(row.TrialEndsAt),
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
