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
	"github.com/speakeasy-api/gram/server/internal/audit"
	"github.com/speakeasy-api/gram/server/internal/auth/orgslug"
	"github.com/speakeasy-api/gram/server/internal/authz"
	"github.com/speakeasy-api/gram/server/internal/background/activities/keybillinglock"
	"github.com/speakeasy-api/gram/server/internal/cache"
	"github.com/speakeasy-api/gram/server/internal/constants"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/conv"
	"github.com/speakeasy-api/gram/server/internal/encryption"
	"github.com/speakeasy-api/gram/server/internal/middleware"
	"github.com/speakeasy-api/gram/server/internal/o11y"
	"github.com/speakeasy-api/gram/server/internal/oops"
	"github.com/speakeasy-api/gram/server/internal/organizations/orgprovision"
	orgRepo "github.com/speakeasy-api/gram/server/internal/organizations/repo"
	"github.com/speakeasy-api/gram/server/internal/productfeatures"
	"github.com/speakeasy-api/gram/server/internal/supporthandoff"
	"github.com/speakeasy-api/gram/server/internal/thirdparty/openrouter"
	orrepo "github.com/speakeasy-api/gram/server/internal/thirdparty/openrouter/repo"
	"github.com/speakeasy-api/gram/server/internal/trialemails"
	trialsRepo "github.com/speakeasy-api/gram/server/internal/trials/repo"
	"github.com/speakeasy-api/gram/server/internal/urn"
)

type Service struct {
	tracer               trace.Tracer
	logger               *slog.Logger
	db                   *pgxpool.Pool
	verifier             *Verifier
	loginStates          cache.TypedCacheObject[LoginState]
	oidc                 *OIDCClient
	sessions             *SessionStore
	allowedOrigins       []string
	dashboardURL         *url.URL
	supportHandoffIssuer supportHandoffIssuer

	// workos creates organizations in the identity provider. Deployments with
	// no WorkOS configuration get orgprovision.Unavailable, whose failure
	// CreateOrganization reports rather than working around.
	workos orgprovision.WorkOSOrganizationCreator

	openRouter      TrialKeyReviver
	productFeatures *productfeatures.Client

	audit *audit.Logger

	trial trialemails.Notifier
}

// TrialKeyReviver is the OpenRouter surface a trial re-arm needs.
type TrialKeyReviver interface {
	RefreshAPIKeyLimit(ctx context.Context, orgID string, keyType openrouter.KeyType, limit *int) (int, error)
	ReinstateAPIKeyLimit(ctx context.Context, orgID string, keyType openrouter.KeyType, limit *int) (int, error)
	ReinstateAPIKeyLimitWithDB(ctx context.Context, db openrouter.DBTX, orgID string, keyType openrouter.KeyType, limit *int) (int, error)
}

// ErrKeyRevivalUnavailable reports a deployment that cannot reach OpenRouter.
var ErrKeyRevivalUnavailable = errors.New("no usable OpenRouter configuration")

const keyBillingLockWaitTimeout = 5 * time.Second

// TrialKeysUnavailable lets the admin server boot without OpenRouter.
type TrialKeysUnavailable struct{}

func (TrialKeysUnavailable) RefreshAPIKeyLimit(context.Context, string, openrouter.KeyType, *int) (int, error) {
	return 0, ErrKeyRevivalUnavailable
}

func (TrialKeysUnavailable) ReinstateAPIKeyLimit(context.Context, string, openrouter.KeyType, *int) (int, error) {
	return 0, ErrKeyRevivalUnavailable
}

func (TrialKeysUnavailable) ReinstateAPIKeyLimitWithDB(context.Context, openrouter.DBTX, string, openrouter.KeyType, *int) (int, error) {
	return 0, ErrKeyRevivalUnavailable
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
	workosClient orgprovision.WorkOSOrganizationCreator,
	openRouter TrialKeyReviver,
	trialNotifier trialemails.Notifier,
	productFeatures *productfeatures.Client,
	dashboardURL *url.URL,
) *Service {
	logger = logger.With(attr.SlogComponent("admin"))

	if trialNotifier == nil {
		trialNotifier = trialemails.NoopNotifier{}
	}

	adminCache := cache.NewRedisCacheAdapter(redisClient)
	sessionStore := NewSessionStore(
		cache.NewTypedObjectCache[Session](
			logger.With(attr.SlogCacheNamespace("admin_session")),
			adminCache,
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
		verifier:       NewVerifier(logger, sessionStore, oidcClient, adminCache),
		allowedOrigins: allowedOrigins,
		dashboardURL:   dashboardURL,
		supportHandoffIssuer: supporthandoff.NewIssuer(
			supporthandoff.NewStore(adminCache),
		),
		workos:          workosClient,
		openRouter:      openRouter,
		productFeatures: productFeatures,
		audit:           audit.NewLogger(),
		loginStates: cache.NewTypedObjectCache[LoginState](
			logger.With(attr.SlogCacheNamespace("admin_login_state")),
			adminCache,
			cache.SuffixNone,
		),
		trial: trialNotifier,
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
	mux.Handle(
		http.MethodPost,
		"/admin/organization.open-dashboard",
		oops.ErrHandle(service.logger, service.handleOpenOrganizationInDashboard).ServeHTTP,
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

	// The organization is resolved to its id first so that both project
	// addresses below are checked against the same value: the caller names it
	// the way the URL does, by id or slug, and a project row carries only the id.
	var orgID string
	if payload.OrganizationIDOrSlug != nil {
		var err error
		orgID, err = queries.AdminResolveOrganizationID(ctx, *payload.OrganizationIDOrSlug)
		switch {
		case errors.Is(err, pgx.ErrNoRows):
			return nil, oops.C(oops.CodeNotFound)
		case err != nil:
			return nil, oops.E(oops.CodeUnexpected, err, "resolve organization").LogError(ctx, s.logger)
		}
	}

	// A slug is resolved to one id before the detail is read, never counted
	// against directly: slugs are unique only within an organization, and the
	// detail query prices every row it matches. See AdminResolveProjectIDBySlug.
	id, err := uuid.Parse(payload.IDOrSlug)
	if err != nil {
		if orgID != "" {
			id, err = queries.AdminResolveProjectIDBySlugInOrganization(ctx, repo.AdminResolveProjectIDBySlugInOrganizationParams{
				OrganizationID: orgID,
				Slug:           payload.IDOrSlug,
			})
		} else {
			id, err = queries.AdminResolveProjectIDBySlug(ctx, payload.IDOrSlug)
		}
		switch {
		case errors.Is(err, pgx.ErrNoRows):
			return nil, oops.C(oops.CodeNotFound)
		case err != nil:
			return nil, oops.E(oops.CodeUnexpected, err, "resolve project slug").LogError(ctx, s.logger)
		}
	}

	row, err := queries.AdminGetProjectDetailByID(ctx, id)
	switch {
	case errors.Is(err, pgx.ErrNoRows):
		return nil, oops.C(oops.CodeNotFound)
	case err != nil:
		return nil, oops.E(oops.CodeUnexpected, err, "lookup project detail by id").LogError(ctx, s.logger)
	}

	// An id addresses a project on its own, so the organization is checked after
	// the read rather than inside it. Duplicating the six-subquery detail read
	// for one comparison would cost more than the comparison saves.
	if orgID != "" && row.OrganizationID != orgID {
		return nil, oops.C(oops.CodeNotFound)
	}

	return adminProjectDetailFromIDRow(row), nil
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

// listOrganizationsFilters resolves the two set filters the SQL takes from the
// four the payload offers. The scalar account_type and the include_disabled
// boolean predate the sets and stay live, because this endpoint keeps serving
// the dashboard that is on main until AGE-3207 retires them.
//
// Unknown values pass straight through to match nothing. An organization can
// carry an account type from outside the list the dashboard knows, and an
// operator pasting a colleague's URL is owed an empty table rather than a 422.
func listOrganizationsFilters(payload *gen.ListOrganizationsPayload) (accountTypes []string, disabledStates []string) {
	// Union, not override: a caller supplying both asks for both.
	accountTypes = payload.AccountTypes
	if payload.AccountType != nil {
		accountTypes = append(append([]string{}, accountTypes...), *payload.AccountType)
	}

	// disabled_states overrides the boolean outright. The boolean only picks the
	// fallback, and these two literals are the arms of the CASE in both queries.
	disabledStates = payload.DisabledStates
	if len(disabledStates) == 0 {
		disabledStates = []string{"active"}
		if conv.PtrValOr(payload.IncludeDisabled, false) {
			disabledStates = append(disabledStates, "disabled")
		}
	}

	return accountTypes, disabledStates
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

	accountTypes, disabledStates := listOrganizationsFilters(payload)

	// Trimmed once for both queries. A pasted id commonly arrives with the
	// newline that ended the line it was copied from, and no arm matches through
	// it; the id arms because they compare exactly, the name and slug arms
	// because the whitespace lands inside the pattern.
	searchTerm := conv.PtrToPGTextTrimmed(payload.Q)

	rows, err := queries.AdminListOrganizations(ctx, repo.AdminListOrganizationsParams{
		Q:              searchTerm,
		AccountTypes:   accountTypes,
		TrialStates:    payload.TrialStates,
		DisabledStates: disabledStates,
		AfterID:        afterID,
		SortBy:         sortBy,
		SortDir:        sortDir,
		PageOffset:     pageOffset,
		PageLimit:      fetchLimit,
	})
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "list organizations").LogError(ctx, s.logger)
	}

	// Counted separately from the page. A page past the end holds no row to carry
	// a count, and in cursor mode the page query has already discarded everything
	// before the cursor.
	total, err := queries.AdminCountOrganizations(ctx, repo.AdminCountOrganizationsParams{
		Q:              searchTerm,
		AccountTypes:   accountTypes,
		TrialStates:    payload.TrialStates,
		DisabledStates: disabledStates,
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
	// into a negative offset, which Postgres rejects. Do not add one to this
	// ceiling. limit is a variable, so at limit 1 that addition would itself
	// overflow to MinInt64, the guard would fire on every page, and every page
	// would come back empty. That was the bug this replaced.
	if maxPage := math.MaxInt64 / int64(limit); n > maxPage {
		n = maxPage
	}

	return (n - 1) * int64(limit)
}

// GetOrganizationStats counts the whole platform. It takes no filters, so the
// figures stay put while an operator narrows the list below them.
func (s *Service) GetOrganizationStats(ctx context.Context, payload *gen.GetOrganizationStatsPayload) (*gen.AdminOrganizationStats, error) {
	row, err := repo.New(s.db).AdminGetOrganizationStats(ctx)
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "organization stats").LogError(ctx, s.logger)
	}

	return &gen.AdminOrganizationStats{
		Total:             row.Total,
		CreatedLast7Days:  row.CreatedLast7Days,
		TrialsEndingSoon:  row.TrialsEndingSoon,
		Disabled:          row.Disabled,
		DisabledLast7Days: row.DisabledLast7Days,
	}, nil
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
			ID:             row.ID.String(),
			Name:           row.Name,
			Slug:           row.Slug,
			McpServerCount: int(row.McpServerCount),
			CreatedAt:      row.CreatedAt.Time.Format(time.RFC3339),
			UpdatedAt:      row.UpdatedAt.Time.Format(time.RFC3339),
		}
	}

	return &gen.AdminListOrganizationProjectsResult{Projects: projects}, nil
}

func (s *Service) UpdateOrganization(ctx context.Context, payload *gen.UpdateOrganizationPayload) (*gen.AdminOrganization, error) {
	if payload.AccountType == nil && payload.Whitelisted == nil {
		return nil, oops.E(oops.CodeBadRequest, nil, "at least one of account_type or whitelisted must be supplied")
	}
	// See ExtendTrial: the design bounds this too, but generated validation only
	// runs at the HTTP boundary.
	if payload.AccountType != nil && !constants.IsAccountType(*payload.AccountType) {
		return nil, oops.E(oops.CodeInvalid, nil, "account_type must be one of %s, got %q", strings.Join(constants.AccountTypes, ", "), *payload.AccountType)
	}

	queries := repo.New(s.db)
	if err := queries.AdminUpdateOrganization(ctx, repo.AdminUpdateOrganizationParams{
		ID:          payload.ID,
		AccountType: conv.PtrToPGText(payload.AccountType),
		Whitelisted: conv.PtrToPGBool(payload.Whitelisted),
	}); err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "update organization").LogError(ctx, s.logger)
	}

	// Setting account type is how a trial becomes a signed contract. Stop
	// pending trial reminders; trialActive stays true otherwise.
	if payload.AccountType != nil {
		if err := s.trial.TrialInactive(ctx, payload.ID); err != nil {
			s.logger.ErrorContext(ctx, "failed to notify trial inactive", attr.SlogError(err), attr.SlogOrganizationID(payload.ID))
		}
	}

	return s.readOrganizationAfterWrite(ctx, payload.ID, "fetch organization after update")
}

func (s *Service) BulkUpdateAccountType(ctx context.Context, payload *gen.BulkUpdateAccountTypePayload) (*gen.AdminBulkUpdateAccountTypeResult, error) {
	if !constants.IsAccountType(payload.AccountType) {
		return nil, oops.E(oops.CodeInvalid, nil, "account_type must be one of %s, got %q", strings.Join(constants.AccountTypes, ", "), payload.AccountType)
	}

	updated, err := repo.New(s.db).AdminBulkUpdateAccountType(ctx, repo.AdminBulkUpdateAccountTypeParams{
		AccountType: payload.AccountType,
		Ids:         payload.Ids,
	})
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "bulk update account type").LogError(ctx, s.logger)
	}

	written := make(map[string]struct{}, len(updated))
	for _, id := range updated {
		written[id] = struct{}{}
	}

	// Naming the ids that matched nothing is the only way the operator can tell
	// the batch fell short.
	missing := make([]string, 0, len(payload.Ids))
	seen := make(map[string]struct{}, len(payload.Ids))
	for _, id := range payload.Ids {
		if _, ok := written[id]; ok {
			continue
		}
		if _, ok := seen[id]; ok {
			continue
		}
		seen[id] = struct{}{}
		missing = append(missing, id)
	}

	if updated == nil {
		updated = []string{}
	}

	return &gen.AdminBulkUpdateAccountTypeResult{
		UpdatedIds: updated,
		MissingIds: missing,
	}, nil
}

func (s *Service) DisableOrganization(ctx context.Context, payload *gen.DisableOrganizationPayload) (*gen.AdminOrganization, error) {
	rows, err := repo.New(s.db).AdminDisableOrganization(ctx, payload.ID)
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "disable organization").LogError(ctx, s.logger)
	}
	if rows == 0 {
		return nil, oops.C(oops.CodeNotFound)
	}

	return s.readOrganizationAfterWrite(ctx, payload.ID, "fetch organization after disable")
}

func (s *Service) EnableOrganization(ctx context.Context, payload *gen.EnableOrganizationPayload) (*gen.AdminOrganization, error) {
	rows, err := repo.New(s.db).AdminEnableOrganization(ctx, payload.ID)
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "enable organization").LogError(ctx, s.logger)
	}
	if rows == 0 {
		return nil, oops.C(oops.CodeNotFound)
	}

	return s.readOrganizationAfterWrite(ctx, payload.ID, "fetch organization after enable")
}

func (s *Service) ExtendTrial(ctx context.Context, payload *gen.ExtendTrialPayload) (*gen.AdminOrganization, error) {
	// The design bounds this too, but that validation is generated into the
	// request decoder and only runs at the HTTP boundary. Repeating it here is
	// what stops a negative day count from shortening a trial through an
	// endpoint named extend if a future caller reaches the service another way.
	//
	// The check must stay on the wide payload.Days and the int32 narrowing must
	// stay below it. Narrowing first would let 1<<32 + 1 truncate to 1 and
	// quietly extend by a day, and a non-HTTP caller is the only one who can
	// pass a day count that large at all. TestExtendTrial_DayCountBounds pins
	// this order.
	if payload.Days < constants.MinTrialExtensionDays || payload.Days > constants.MaxTrialExtensionDays {
		return nil, oops.E(oops.CodeInvalid, nil, "days must be between %d and %d", constants.MinTrialExtensionDays, constants.MaxTrialExtensionDays)
	}

	logger := s.logger.With(attr.SlogOrganizationID(payload.ID))

	tx, err := s.db.Begin(ctx)
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "begin trial extension transaction").LogError(ctx, logger)
	}
	defer o11y.NoLogDefer(func() error { return tx.Rollback(ctx) })

	extended, err := trialsRepo.New(tx).ExtendTrial(ctx, trialsRepo.ExtendTrialParams{
		OrganizationID: payload.ID,
		ExtendByDays:   int32(payload.Days),
	})
	switch {
	case errors.Is(err, pgx.ErrNoRows):
		// rejectTrialChange reads on the pool, so this connection goes back
		// before it asks for a second one. The deferred rollback is idempotent.
		_ = tx.Rollback(ctx)
		return nil, s.rejectTrialChange(ctx, logger, payload.ID,
			"look up organization after unextended trial",
			"organization has no running enterprise trial to extend")
	case err != nil:
		return nil, oops.E(oops.CodeUnexpected, err, "extend trial").LogError(ctx, logger)
	}

	// The customer's feed names the organization, not only its id, and extend
	// writes nothing on organization_metadata to get those columns back from.
	organization, err := repo.New(tx).AdminGetOrganization(ctx, repo.AdminGetOrganizationParams{
		ID:        payload.ID,
		AllowSlug: false,
	})
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "read organization for trial extension").LogError(ctx, logger)
	}

	actor, operatorEmail := adminActor(ctx)
	if err := s.audit.LogOrganizationEnterpriseTrialExtended(ctx, tx, audit.LogOrganizationEnterpriseTrialExtendedEvent{
		OrganizationID: payload.ID,
		Actor:          actor,
		// The customer reads this feed, so the entry carries the team label
		// rather than the operator's email. adminActor says why.
		ActorDisplayName:    conv.PtrEmpty(audit.SpeakeasyTeamActorLabel),
		ActorSlug:           nil,
		OrganizationName:    organization.Name,
		OrganizationSlug:    organization.Slug,
		ExtendedByDays:      payload.Days,
		PreviousTrialEndsAt: extended.PreviousEndsAt.Time,
		TrialEndsAt:         extended.EndsAt.Time,
	}); err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "log trial extension").LogError(ctx, logger)
	}

	if err := tx.Commit(ctx); err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "commit trial extension").LogError(ctx, logger)
	}

	// Speakeasy-only, and the only place the email meets the entry's subject.
	logger.InfoContext(ctx, "extended enterprise trial",
		attr.SlogAuthUserEmail(conv.PtrValOr(operatorEmail, "unknown")),
	)

	return s.readOrganizationAfterWrite(ctx, payload.ID, "fetch organization after trial extension")
}

// rejectTrialChange turns a trial write that touched no row into the error the
// operator should act on. There are two causes and only the second is a
// conflict: the organization does not exist at all, or it exists and its trial
// is in the wrong state. Disable and enable both answer not-found for an id that
// matches nothing, so an operator who pastes one bad id must not be told to go
// and look at a trial by this endpoint alone.
//
// The organization existing means the trial is what blocks the write: converted,
// demoted, expired, or never granted. Which one is not the operator's business,
// and arming a trial that was never granted is the auth flow's job rather than
// this endpoint's. failed_precondition would read better than conflict, but the
// admin service does not declare it, so it would leave as a 500.
//
// The lookup is on the failure path only, and runs on the pool because the
// caller's transaction is already rolled back.
func (s *Service) rejectTrialChange(ctx context.Context, logger *slog.Logger, organizationID string, lookupContext string, conflictMessage string) error {
	_, lookupErr := repo.New(s.db).AdminGetOrganization(ctx, repo.AdminGetOrganizationParams{
		ID:        organizationID,
		AllowSlug: false,
	})
	switch {
	case errors.Is(lookupErr, pgx.ErrNoRows):
		return oops.C(oops.CodeNotFound)
	case lookupErr != nil:
		// Falling through to the conflict here would report a trial state we
		// never managed to read.
		return oops.E(oops.CodeUnexpected, lookupErr, "%s", lookupContext).LogError(ctx, logger)
	}

	return oops.E(oops.CodeConflict, nil, "%s", conflictMessage)
}

// CreateOrganization creates an organization in WorkOS and then in Gram.
//
// The WorkOS create happens before the transaction opens, because it is the one
// step that cannot be rolled back. Everything Gram stores is written inside a
// single transaction afterwards, so a failure below leaves no organization row,
// no role grants and no entitlements from this call.
//
// That is not the same as leaving nothing. Wherever the WorkOS webhook is
// configured, organization.created arrives about ten seconds later and the sync
// activity writes the organization row and its role grants anyway, without the
// default entitlements this handler would have seeded. AGE-3213 covers that gap.
// Retrying the create is still the right move: the derived ID makes the retry
// land on that row rather than beside it.
func (s *Service) CreateOrganization(ctx context.Context, payload *gen.CreateOrganizationPayload) (*gen.AdminOrganization, error) {
	name, err := orgprovision.ValidateName(payload.Name)
	if err != nil {
		return nil, err
	}

	created, err := orgprovision.CreateInWorkOS(ctx, s.workos, name)
	switch {
	case errors.Is(err, orgprovision.ErrUnavailable):
		// CodeInvalid and not CodeInvariantViolation, which reads like the
		// better fit and is not. server/design/shared/errors.go maps
		// invariant_violation to 500, and the admin app trusts a response body
		// only below 500, so the explanation below would never reach the
		// operator. oops.CodeMap disagrees and maps it to 422; the Goa HTTP
		// layer does not read that map.
		return nil, oops.E(oops.CodeInvalid, err, "this server has no WorkOS configuration, so it cannot create organizations")
	case err != nil:
		return nil, oops.E(oops.CodeGatewayError, err, "create organization in WorkOS").LogError(ctx, s.logger)
	}

	logger := s.logger.With(
		attr.SlogOrganizationID(created.GramOrganizationID),
		attr.SlogWorkOSOrganizationID(created.WorkOSOrganizationID),
	)

	tx, err := s.db.Begin(ctx)
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "begin organization creation transaction").LogError(ctx, logger)
	}
	defer o11y.NoLogDefer(func() error { return tx.Rollback(ctx) })

	queries := orgRepo.New(tx)

	// A row can already exist under the derived ID: WorkOS fires
	// organization.created the moment the call above returns, so the sync
	// activity can win the race to insert it. That row's slug is already in the
	// organization's URL, and re-deriving one would find the base taken by that
	// very row and write a suffixed variant over it.
	uniqueSlug := ""
	existing, err := queries.GetOrganizationMetadata(ctx, created.GramOrganizationID)
	switch {
	case err == nil:
		uniqueSlug = existing.Slug
	case errors.Is(err, pgx.ErrNoRows):
		// Seeded with the WorkOS organization ID rather than randomness so a
		// retry of the same create competes for one advisory-lock key instead
		// of two.
		base, baseErr := orgslug.StableBase(name, created.WorkOSOrganizationID)
		if baseErr != nil {
			return nil, oops.E(oops.CodeUnexpected, baseErr, "derive organization slug").LogError(ctx, logger)
		}
		if lockErr := queries.LockOrganizationSlug(ctx, base); lockErr != nil {
			return nil, oops.E(oops.CodeUnexpected, lockErr, "lock organization slug").LogError(ctx, logger)
		}

		// Read again now that the lock is held. The read above was taken before
		// it, and under READ COMMITTED the sync activity can hold the lock,
		// insert this very row under slug base, and commit in the gap. Deciding
		// from the stale read would then hand back a suffixed variant, and the
		// upsert below writes slug unconditionally, so it would overwrite a slug
		// already serving in the organization's URL.
		afterLock, reReadErr := queries.GetOrganizationMetadata(ctx, created.GramOrganizationID)
		switch {
		case reReadErr == nil:
			uniqueSlug = afterLock.Slug
		case errors.Is(reReadErr, pgx.ErrNoRows):
			found, findErr := orgslug.FindUnique(ctx, queries, base)
			if findErr != nil {
				return nil, oops.E(oops.CodeUnexpected, findErr, "find unique organization slug").LogError(ctx, logger)
			}
			uniqueSlug = found
		default:
			return nil, oops.E(oops.CodeUnexpected, reReadErr, "look up organization after slug lock").LogError(ctx, logger)
		}
	default:
		return nil, oops.E(oops.CodeUnexpected, err, "look up organization before create").LogError(ctx, logger)
	}

	// Keyed on the derived ID with ON CONFLICT (id) DO UPDATE, so a webhook that
	// already created this organization is updated rather than duplicated. The
	// FromWorkOS variants of this query are for the sync path and would insert a
	// second row here.
	org, err := queries.UpsertOrganizationMetadata(ctx, orgRepo.UpsertOrganizationMetadataParams{
		ID:   created.GramOrganizationID,
		Name: name,
		Slug: uniqueSlug,
		// The unique index over workos_id is what turns a diverging ID
		// derivation into a loud failure instead of a duplicate.
		WorkosID: conv.ToPGText(created.WorkOSOrganizationID),
		// On insert this changes nothing: the query already coalesces a null
		// whitelisted to FALSE. It is a statement of intent at the call site
		// rather than the mechanism, and it does carry weight on the conflict
		// arm, where a null would preserve whatever the existing row holds and
		// FALSE states that an operator creating an organization is not
		// whitelisting it.
		Whitelisted: pgtype.Bool{Bool: false, Valid: true},
	})
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "create organization metadata").LogError(ctx, logger)
	}

	if err := authz.SeedSystemRoleGrantsTx(ctx, tx, org.ID); err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "provision organization access defaults").LogError(ctx, logger)
	}

	if err := productfeatures.SeedOrganizationDefaultsTx(ctx, tx, org.ID); err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "seed organization default entitlements").LogError(ctx, logger)
	}

	if err := tx.Commit(ctx); err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "commit organization creation transaction").LogError(ctx, logger)
	}

	return s.readOrganizationAfterWrite(ctx, org.ID, "fetch organization after create")
}

// RearmTrial puts a demoted enterprise trial back on.
//
// The keys come back up before the restore commits, deliberately not mirroring
// the demotion's ordering: a partial failure must leave the organization
// demoted with live keys, never a running trial with dead ones.
func (s *Service) RearmTrial(ctx context.Context, payload *gen.RearmTrialPayload) (*gen.AdminOrganization, error) {
	// Defence in depth against a non-HTTP caller: the design's bounds are
	// generated into the request decoder alone. Keep this on the wide
	// payload.Days, above the int32 narrowing, or 1<<32 + 1 truncates into range.
	if payload.Days < constants.MinTrialRearmDays || payload.Days > constants.MaxTrialRearmDays {
		return nil, oops.E(oops.CodeInvalid, nil, "days must be between %d and %d", constants.MinTrialRearmDays, constants.MaxTrialRearmDays)
	}

	logger := s.logger.With(attr.SlogOrganizationID(payload.ID))
	lockedKeys := make(map[openrouter.KeyType]*pgxpool.Conn, len(openrouter.AllKeyTypes))
	var result *gen.AdminOrganization
	err := s.withTrialKeyBillingLocks(ctx, logger, payload.ID, openrouter.AllKeyTypes, lockedKeys, func() error {
		var lockedErr error
		result, lockedErr = s.rearmTrialLocked(ctx, logger, payload, lockedKeys)
		return lockedErr
	})
	if err == nil {
		return result, nil
	}

	var shareable *oops.ShareableError
	if errors.As(err, &shareable) {
		return nil, shareable
	}
	if errors.Is(err, keybillinglock.ErrAcquireTimeout) {
		return nil, oops.E(oops.CodeUnavailable, err, "another billing operation is in progress; retry shortly").LogWarn(ctx, logger)
	}
	return nil, oops.E(oops.CodeUnexpected, err, "lock inference keys for trial re-arm").LogError(ctx, logger)
}

func (s *Service) withTrialKeyBillingLocks(
	ctx context.Context,
	logger *slog.Logger,
	organizationID string,
	keyTypes []openrouter.KeyType,
	locked map[openrouter.KeyType]*pgxpool.Conn,
	operation func() error,
) error {
	if len(keyTypes) == 0 {
		return operation()
	}

	keyType := keyTypes[0]
	err := keybillinglock.WithAcquireTimeout(ctx, logger, s.db, organizationID, keyType, keyBillingLockWaitTimeout, func(conn *pgxpool.Conn) error {
		locked[keyType] = conn
		defer delete(locked, keyType)
		return s.withTrialKeyBillingLocks(ctx, logger, organizationID, keyTypes[1:], locked, operation)
	})
	if err != nil {
		return fmt.Errorf("hold OpenRouter %s key billing lock for trial re-arm: %w", keyType, err)
	}
	return nil
}

func (s *Service) rearmTrialLocked(
	ctx context.Context,
	logger *slog.Logger,
	payload *gen.RearmTrialPayload,
	lockedKeys map[openrouter.KeyType]*pgxpool.Conn,
) (*gen.AdminOrganization, error) {

	tx, err := s.db.Begin(ctx)
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "begin trial re-arm transaction").LogError(ctx, logger)
	}
	defer o11y.NoLogDefer(func() error { return tx.Rollback(ctx) })

	trials := trialsRepo.New(tx)

	rearmed, err := trials.RearmTrial(ctx, trialsRepo.RearmTrialParams{
		OrganizationID: payload.ID,
		RearmForDays:   conv.SafeInt32(payload.Days),
	})
	switch {
	case errors.Is(err, pgx.ErrNoRows):
		// rejectTrialChange reads on the pool, so this connection goes back
		// before it asks for a second one. The deferred rollback is idempotent.
		_ = tx.Rollback(ctx)
		return nil, s.rejectTrialChange(ctx, logger, payload.ID,
			"look up organization after unrearmed trial",
			"organization has no demoted enterprise trial to re-arm")
	case err != nil:
		return nil, oops.E(oops.CodeUnexpected, err, "re-arm trial").LogError(ctx, logger)
	}

	uncapped, err := s.reviveTrialKeys(ctx, logger, lockedKeys, payload.ID)
	if err != nil {
		return nil, err
	}

	organization, err := trials.RestoreOrganizationFromTrial(ctx, trialsRepo.RestoreOrganizationFromTrialParams{
		OrganizationID: payload.ID,
		AccountType:    rearmed.Tier,
	})
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "restore organization from trial").LogError(ctx, logger)
	}

	if err := productfeatures.SetTrialRuntimeFeaturesTx(ctx, tx, payload.ID, true); err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "restore trial runtime features").LogError(ctx, logger)
	}

	actor, operatorEmail := adminActor(ctx)
	if err := s.audit.LogOrganizationEnterpriseTrialRearmed(ctx, tx, audit.LogOrganizationEnterpriseTrialRearmedEvent{
		OrganizationID: payload.ID,
		Actor:          actor,
		// The customer reads this feed, so the entry carries the team label
		// rather than the operator's email. adminActor says why.
		ActorDisplayName: conv.PtrEmpty(audit.SpeakeasyTeamActorLabel),
		ActorSlug:        nil,
		OrganizationName: organization.Name,
		OrganizationSlug: organization.Slug,
		AccountType:      rearmed.Tier,
		TrialEndsAt:      rearmed.EndsAt.Time,
	}); err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "log trial re-arm").LogError(ctx, logger)
	}

	if err := tx.Commit(ctx); err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "commit trial re-arm").LogError(ctx, logger)
	}

	s.recapRevivedKeys(ctx, logger, lockedKeys, payload.ID, uncapped)
	for _, feature := range productfeatures.TrialRuntimeFeatures {
		s.productFeatures.UpdateFeatureCache(ctx, payload.ID, feature, true)
	}

	// Speakeasy-only, and the only place the email meets the entry's subject.
	logger.InfoContext(ctx, "re-armed enterprise trial",
		attr.SlogAuthUserEmail(conv.PtrValOr(operatorEmail, "unknown")),
	)

	return s.readOrganizationAfterWrite(ctx, payload.ID, "fetch organization after trial re-arm")
}

// reviveTrialKeys brings every platform key the organization holds back up, and
// returns the types revived at a pre-commit ceiling, for recapRevivedKeys.
//
// Shaped like openrouterkeys.EnableKey rather than the demotion's blind loop:
// DisableAPIKey no-ops on a missing key row, RefreshAPIKeyLimit errors on one.
//
// The caller holds both per-key session locks through commit. Key revival uses
// those locked sessions but stays outside the organization transaction, so a
// later rollback leaves the org demoted with live keys instead of exposing an
// admitted trial whose keys are still disabled.
func (s *Service) reviveTrialKeys(ctx context.Context, logger *slog.Logger, lockedKeys map[openrouter.KeyType]*pgxpool.Conn, organizationID string) ([]openrouter.KeyType, error) {
	var uncapped []openrouter.KeyType

	for _, keyType := range openrouter.AllKeyTypes {
		conn := lockedKeys[keyType]
		if conn == nil {
			return nil, oops.E(oops.CodeUnexpected, nil, "missing openrouter %s key lock", keyType).LogError(ctx, logger)
		}
		row, err := orrepo.New(conn).GetOpenRouterAPIKey(ctx, orrepo.GetOpenRouterAPIKeyParams{
			OrganizationID: organizationID,
			KeyType:        string(keyType),
		})
		switch {
		case errors.Is(err, pgx.ErrNoRows):
			continue
		case err != nil:
			return nil, oops.E(oops.CodeUnexpected, err, "read openrouter %s key", keyType).LogError(ctx, logger)
		case !row.Disabled:
			continue
		}

		// The ceiling recorded on the row, not the policy default: a trial
		// key is minted well below that default. A zero resolves from the
		// pre-commit free-tier projection and is corrected after commit.
		if row.MonthlyCredits == 0 {
			uncapped = append(uncapped, keyType)
		}
		limit := conv.PtrEmpty(int(row.MonthlyCredits))
		_, err = s.openRouter.ReinstateAPIKeyLimitWithDB(ctx, conn, organizationID, keyType, limit)
		switch {
		case errors.Is(err, ErrKeyRevivalUnavailable):
			return nil, oops.E(oops.CodeInvalid, err, "this server cannot revive model provider keys: it is missing either the OpenRouter provisioning key or a usable encryption key. The server log says which at startup")
		case err != nil:
			return nil, oops.E(oops.CodeGatewayError, err, "revive openrouter %s key", keyType).LogError(ctx, logger)
		}
	}

	return uncapped, nil
}

// recapRevivedKeys puts the trial's own ceiling on the keys reviveTrialKeys
// could only revive at the free-tier one. It must run after the commit, because
// a nil limit resolves from the now-committed organization tier.
//
// A failure is logged and swallowed: the re-arm is already durable, so an error
// here would report an armed trial as unarmed.
func (s *Service) recapRevivedKeys(ctx context.Context, logger *slog.Logger, lockedKeys map[openrouter.KeyType]*pgxpool.Conn, organizationID string, keyTypes []openrouter.KeyType) {
	for _, keyType := range keyTypes {
		conn := lockedKeys[keyType]
		if conn == nil {
			logger.ErrorContext(ctx, "re-armed trial key kept the free-tier allowance: missing key lock",
				attr.SlogOpenRouterKeyType(string(keyType)),
			)
			continue
		}
		if _, err := s.openRouter.ReinstateAPIKeyLimitWithDB(ctx, conn, organizationID, keyType, nil); err != nil {
			logger.ErrorContext(ctx, "re-armed trial key kept the free-tier allowance: refresh it from the platform admin key page",
				attr.SlogError(err),
				attr.SlogOpenRouterKeyType(string(keyType)),
			)
		}
	}
}

// adminActor identifies the operator behind an admin-app write. An admin session
// carries an OIDC subject rather than a Gram user id, and a call without one
// records the system actor the demotion sweeper uses.
//
// The returned email is for the structured log only, never the entry's display
// name: these entries surface in the customer's own feed, and auditapi's
// read-side mask cannot recognise an OIDC subject as staff.
func adminActor(ctx context.Context) (actor urn.Principal, operatorEmail *string) {
	authCtx, ok := contextvalues.GetAdminAuthContext(ctx)
	if !ok || authCtx == nil || authCtx.OIDCSubject == "" {
		return urn.NewPrincipal(urn.PrincipalTypeUser, "system"), nil
	}

	return urn.NewPrincipal(urn.PrincipalTypeUser, authCtx.OIDCSubject), conv.PtrEmpty(authCtx.Email)
}

// readOrganizationAfterWrite returns the organization a write just landed on.
// The read is keyed on id alone because every admin write is, so resolving a
// slug here could return a different organization than the one written.
func (s *Service) readOrganizationAfterWrite(ctx context.Context, id string, errMsg string) (*gen.AdminOrganization, error) {
	row, err := repo.New(s.db).AdminGetOrganization(ctx, repo.AdminGetOrganizationParams{
		ID:        id,
		AllowSlug: false,
	})
	switch {
	case errors.Is(err, pgx.ErrNoRows):
		return nil, oops.C(oops.CodeNotFound)
	case err != nil:
		return nil, oops.E(oops.CodeUnexpected, err, "%s", errMsg).LogError(ctx, s.logger)
	}

	return adminOrganizationFromGetRow(row), nil
}

func (s *Service) GetOrganization(ctx context.Context, payload *gen.GetOrganizationPayload) (*gen.AdminOrganization, error) {
	row, err := repo.New(s.db).AdminGetOrganization(ctx, repo.AdminGetOrganizationParams{
		ID:        payload.IDOrSlug,
		AllowSlug: true,
	})
	switch {
	case errors.Is(err, pgx.ErrNoRows):
		return nil, oops.C(oops.CodeNotFound)
	case err != nil:
		return nil, oops.E(oops.CodeUnexpected, err, "lookup organization by id or slug").LogError(ctx, s.logger)
	}
	return adminOrganizationFromGetRow(row), nil
}

func adminOrganizationFromGetRow(row repo.AdminGetOrganizationRow) *gen.AdminOrganization {
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
