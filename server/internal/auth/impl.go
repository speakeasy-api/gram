package auth

import (
	"context"
	"crypto/rand"
	"crypto/subtle"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgerrcode"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"
	"go.opentelemetry.io/otel/trace"
	goahttp "goa.design/goa/v3/http"
	"goa.design/goa/v3/security"

	gen "github.com/speakeasy-api/gram/server/gen/auth"
	srv "github.com/speakeasy-api/gram/server/gen/http/auth/server"
	"github.com/speakeasy-api/gram/server/gen/types"
	"github.com/speakeasy-api/gram/server/internal/attr"
	"github.com/speakeasy-api/gram/server/internal/auth/identity"
	"github.com/speakeasy-api/gram/server/internal/auth/orgslug"
	"github.com/speakeasy-api/gram/server/internal/auth/sessions"
	"github.com/speakeasy-api/gram/server/internal/authz"
	"github.com/speakeasy-api/gram/server/internal/billing"
	"github.com/speakeasy-api/gram/server/internal/cache"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/conv"
	envRepo "github.com/speakeasy-api/gram/server/internal/environments/repo"
	"github.com/speakeasy-api/gram/server/internal/middleware"
	"github.com/speakeasy-api/gram/server/internal/oops"
	orgRepo "github.com/speakeasy-api/gram/server/internal/organizations/repo"
	projectsRepo "github.com/speakeasy-api/gram/server/internal/projects/repo"
	"github.com/speakeasy-api/gram/server/internal/thirdparty/posthog"
)

const dispositionAssistants = "assistants"

// nonceTTL is the maximum time a login nonce is valid. Nonces are one-time-use
// and deleted on consumption; this TTL is a safety net for abandoned flows.
const nonceTTL = 10 * time.Minute

// nonceBindingCookie is the name of the HttpOnly cookie that binds a login
// nonce to the browser session that initiated the OAuth flow. This prevents
// login CSRF where an attacker crafts a callback URL that logs the victim
// into the attacker's account.
const nonceBindingCookie = "gram_auth_nonce"

type nonceBindingKey struct{}

// withNonceBinding stores a nonce binding value in the context.
func withNonceBinding(ctx context.Context, binding string) context.Context {
	return context.WithValue(ctx, nonceBindingKey{}, binding)
}

// nonceBindingFromContext retrieves the nonce binding value from the context.
func nonceBindingFromContext(ctx context.Context) string {
	v, _ := ctx.Value(nonceBindingKey{}).(string)
	return v
}

// TestNonceBindingContext injects a nonce binding value into the context.
// Exported for use in tests only.
func TestNonceBindingContext(ctx context.Context, binding string) context.Context {
	return withNonceBinding(ctx, binding)
}

type authErr string

const (
	authErrCodeLookup authErr = "lookup_error"
	authErrInit       authErr = "init_error"
)

type AuthConfigurations struct {
	IDPBaseURL        string
	GramServerURL     string
	SignInRedirectURL string
	SetupSiteURL      string // Origin of the enterprise setup subdomain (e.g. https://setup.getgram.ai)
	CookieDomain      string // Domain attribute for session cookies (e.g. "getgram.ai", "localhost")
	Environment       string
}

// Service for gram dashboard authentication endpoints

type AssistantsSubscriptionCancelScheduler interface {
	ScheduleCancelAssistantsSubscription(ctx context.Context, subscriptionID string) error
}

type Service struct {
	tracer              trace.Tracer
	logger              *slog.Logger
	db                  *pgxpool.Pool
	sessions            *sessions.Manager
	identity            *identity.Resolver
	cfg                 AuthConfigurations
	authz               *authz.Engine
	billing             billing.Repository
	cancelSubsScheduler AssistantsSubscriptionCancelScheduler
	posthog             *posthog.Posthog
	nonceStore          cache.Cache
	projectsRepo        *projectsRepo.Queries
	envRepo             *envRepo.Queries
	orgRepo             *orgRepo.Queries
}

var _ gen.Service = (*Service)(nil)

func NewService(
	logger *slog.Logger,
	tracerProvider trace.TracerProvider,
	db *pgxpool.Pool,
	sessions *sessions.Manager,
	identityResolver *identity.Resolver,
	cfg AuthConfigurations,
	authzEngine *authz.Engine,
	billingRepo billing.Repository,
	cancelSubsScheduler AssistantsSubscriptionCancelScheduler,
	posthogClient *posthog.Posthog,
	nonceStore cache.Cache,
) *Service {
	logger = logger.With(attr.SlogComponent("auth"))

	return &Service{
		tracer:              tracerProvider.Tracer("github.com/speakeasy-api/gram/server/internal/auth"),
		logger:              logger,
		db:                  db,
		sessions:            sessions,
		identity:            identityResolver,
		cfg:                 cfg,
		authz:               authzEngine,
		billing:             billingRepo,
		cancelSubsScheduler: cancelSubsScheduler,
		posthog:             posthogClient,
		nonceStore:          nonceStore,
		projectsRepo:        projectsRepo.New(db),
		envRepo:             envRepo.New(db),
		orgRepo:             orgRepo.New(db),
	}
}

func FormSignInRedirectURL(siteURL string) string {
	return siteURL
}

func Attach(mux goahttp.Muxer, service *Service) {
	endpoints := gen.NewEndpoints(service)
	endpoints.Use(middleware.MapErrors())
	endpoints.Use(middleware.TraceMethods(service.tracer))
	server := srv.New(endpoints, mux, goahttp.RequestDecoder, goahttp.ResponseEncoder, nil, nil)

	// Wrap Login handler: generate a random binding token, set it as an
	// HttpOnly cookie, and inject it into the context so Login() can store
	// it alongside the nonce in Redis.
	server.Login = loginNonceBindingMiddleware(service.cfg.Environment)(server.Login)

	// Wrap Callback handler: read the binding cookie and inject it into the
	// context so validateAuthNonce() can verify it.
	server.Callback = callbackNonceBindingMiddleware(server.Callback)

	// If a cookie domain is configured, rewrite gram_session cookies to
	// include Domain= so they're shared across subdomains (e.g. app.getgram.ai
	// and setup.getgram.ai both under getgram.ai).
	if service.cfg.CookieDomain != "" {
		server.Login = cookieDomainMiddleware(service.cfg.CookieDomain)(server.Login)
		server.Callback = cookieDomainMiddleware(service.cfg.CookieDomain)(server.Callback)
		server.SwitchScopes = cookieDomainMiddleware(service.cfg.CookieDomain)(server.SwitchScopes)
		server.Logout = cookieDomainMiddleware(service.cfg.CookieDomain)(server.Logout)
		server.Info = cookieDomainMiddleware(service.cfg.CookieDomain)(server.Info)
	}

	srv.Mount(mux, server)
}

// cookieDomainMiddleware rewrites Set-Cookie headers for auth cookies
// (gram_session, gram_auth_nonce) to include the specified Domain attribute.
// This is needed because the Goa generated code and nonce middleware set
// cookies without a Domain, making them host-only.
func cookieDomainMiddleware(domain string) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			wrapped := &cookieDomainWriter{ResponseWriter: w, domain: domain}
			next.ServeHTTP(wrapped, r)
		})
	}
}

type cookieDomainWriter struct {
	http.ResponseWriter
	domain string
}

func (w *cookieDomainWriter) WriteHeader(statusCode int) {
	headers := w.ResponseWriter.Header()
	cookies := headers.Values("Set-Cookie")
	if len(cookies) > 0 {
		headers.Del("Set-Cookie")
		for _, c := range cookies {
			if strings.HasPrefix(c, "gram_session=") {
				c += "; Domain=" + w.domain
			}
			headers.Add("Set-Cookie", c)
		}
	}
	w.ResponseWriter.WriteHeader(statusCode)
}

// loginNonceBindingMiddleware generates a random nonce-binding token, sets it
// as an HttpOnly/SameSite cookie, and stores it in the request context.
func loginNonceBindingMiddleware(env string) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			binding, err := generateNonce()
			if err != nil {
				http.Error(w, "internal error", http.StatusInternalServerError)
				return
			}

			//nolint:exhaustruct // we only desire these fields and dont want to accidentally change behavior with some unexpected zero value
			http.SetCookie(w, &http.Cookie{
				Name:     nonceBindingCookie,
				Value:    binding,
				MaxAge:   int(nonceTTL.Seconds()),
				Path:     "/",
				Secure:   env != "local",
				HttpOnly: true,
				SameSite: http.SameSiteLaxMode,
			})

			ctx := withNonceBinding(r.Context(), binding)
			next.ServeHTTP(w, r.WithContext(ctx))
		})
	}
}

// callbackNonceBindingMiddleware reads the nonce-binding cookie and injects its
// value into the request context for validateAuthNonce to verify.
func callbackNonceBindingMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var binding string
		if c, err := r.Cookie(nonceBindingCookie); err == nil {
			binding = c.Value
		}

		// Clear the cookie regardless of outcome — it's single-use.
		//nolint:exhaustruct // we only desire these fields and dont want to accidentally change behavior with some unexpected zero value
		http.SetCookie(w, &http.Cookie{
			Name:     nonceBindingCookie,
			Value:    "",
			MaxAge:   -1,
			Path:     "/",
			HttpOnly: true,
			SameSite: http.SameSiteLaxMode,
		})

		ctx := withNonceBinding(r.Context(), binding)
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}

func (s *Service) APIKeyAuth(ctx context.Context, key string, schema *security.APIKeyScheme) (context.Context, error) {
	ctx, err := s.sessions.Authenticate(ctx, key)
	if err != nil {
		return ctx, err
	}
	ctx, err = s.authz.PrepareContext(ctx)
	if err != nil {
		return ctx, oops.E(oops.CodeUnexpected, err, "load access grants").Log(ctx, s.logger)
	}
	return ctx, nil
}

func (s *Service) Callback(ctx context.Context, payload *gen.CallbackPayload) (res *gen.CallbackResult, err error) {
	redirectWithError := func(code authErr, err error) (*gen.CallbackResult, error) {
		s.logger.ErrorContext(ctx, "signin error", attr.SlogError(err), attr.SlogReason(string(code)))
		return &gen.CallbackResult{
			Location:      fmt.Sprintf("%s?signin_error=%s", s.cfg.SignInRedirectURL, err.Error()),
			SessionToken:  "",
			SessionCookie: "",
		}, nil
	}

	if payload.Code == "" {
		return redirectWithError(authErrCodeLookup, errors.New("code is required"))
	}

	if err := s.validateAuthNonce(ctx, payload); err != nil {
		return redirectWithError(authErrCodeLookup, err)
	}

	idpUser, err := s.identity.ExchangeCodeForTokens(ctx, payload.Code)
	if err != nil {
		return redirectWithError(authErrCodeLookup, err)
	}

	userID, err := s.identity.UpsertUserFromIDP(ctx, idpUser)
	if err != nil {
		return redirectWithError(authErrInit, err)
	}

	if idpUser.OrganizationID != "" {
		if err := s.identity.SyncMembershipsFromWorkOS(ctx, userID, idpUser.Sub); err != nil {
			return redirectWithError(authErrInit, err)
		}
	}

	userInfo, _, err := s.identity.GetUserInfo(ctx, userID)
	if err != nil {
		return redirectWithError(authErrInit, err)
	}

	sessionID := uuid.New().String()
	session := sessions.Session{
		SessionID:            sessionID,
		UserID:               userID,
		ActiveOrganizationID: "",
		WorkOSSessionID:      idpUser.WorkOSSessionID,
	}

	if len(userInfo.Organizations) == 0 {
		if dispositionFromState(payload) == dispositionAssistants {
			location, err := s.autoProvisionForAssistants(ctx, userInfo, &session)
			if err != nil {
				return redirectWithError(authErrInit, err)
			}
			return &gen.CallbackResult{
				Location:      location,
				SessionToken:  session.SessionID,
				SessionCookie: session.SessionID,
			}, nil
		}

		if err := s.sessions.StoreSession(ctx, session); err != nil {
			return redirectWithError(authErrInit, err)
		}

		return &gen.CallbackResult{
			Location:      s.callbackRedirectURL(ctx, payload),
			SessionToken:  session.SessionID,
			SessionCookie: session.SessionID,
		}, nil
	}

	// Default to the first org; overridden by the priority chain below.
	activeOrgID := userInfo.Organizations[0].ID
	activeOrgSelected := false

	// Priority 1: admin override header — look up org from DB since
	// the admin may not be a member of the target org.
	if userInfo.Admin {
		if adminOverride, _ := contextvalues.GetAdminOverrideFromContext(ctx); adminOverride != "" {
			orgMeta, err := s.orgRepo.GetOrganizationMetadataBySlug(ctx, adminOverride)
			if err == nil {
				activeOrgID = orgMeta.ID
				activeOrgSelected = true
			}
		}
	}

	// Priority 2: org slug from the state param (explicit destination URL).
	// First try the user's own orgs; for admins, fall back to a DB lookup
	// since they may access orgs they aren't a member of.
	if !activeOrgSelected {
		if org, ok := activeOrganizationFromState(payload, userInfo.Organizations); ok {
			activeOrgID = org.ID
			activeOrgSelected = true
		} else if userInfo.Admin {
			if slug := organizationSlugFromState(payload); slug != "" {
				if orgMeta, err := s.orgRepo.GetOrganizationMetadataBySlug(ctx, slug); err == nil {
					activeOrgID = orgMeta.ID
					activeOrgSelected = true
				}
			}
		}
	}

	// Priority 3: org the user selected in the IDP auth flow (WorkOS AuthKit).
	if !activeOrgSelected && idpUser.OrganizationID != "" {
		if org, ok := activeOrganizationFromWorkOSID(idpUser.OrganizationID, userInfo.Organizations); ok {
			activeOrgID = org.ID
		}
	}

	orgMetadata, err := s.orgRepo.GetOrganizationMetadata(ctx, activeOrgID)
	if err != nil {
		return redirectWithError(authErrInit, err)
	}

	if orgMetadata.DisabledAt.Valid {
		return redirectWithError(authErrInit, errors.New("this organization is disabled, please reach out to support@speakeasy.com for more information"))
	}

	session.ActiveOrganizationID = activeOrgID
	if err := s.sessions.StoreSession(ctx, session); err != nil {
		return redirectWithError(authErrInit, err)
	}
	if inviteeEmail := strings.ToLower(strings.TrimSpace(userInfo.Email)); inviteeEmail != "" {
		if err := s.acceptPendingInvitationForMember(ctx, activeOrgID, inviteeEmail, userID, idpUser.Sub); err != nil {
			s.logger.WarnContext(ctx, "failed to accept pending invite after login",
				attr.SlogError(err),
				attr.SlogOrganizationID(activeOrgID),
				attr.SlogAuthUserEmail(inviteeEmail),
			)
		}
	}

	return &gen.CallbackResult{
		Location:      s.callbackRedirectURL(ctx, payload),
		SessionToken:  session.SessionID,
		SessionCookie: session.SessionID,
	}, nil
}

func (s *Service) acceptPendingInvitationForMember(ctx context.Context, organizationID, inviteeEmail, gramUserID, workosUserID string) error {
	invite, err := s.orgRepo.AcceptPendingInvitationForMember(ctx, orgRepo.AcceptPendingInvitationForMemberParams{
		OrganizationID: organizationID,
		Email:          inviteeEmail,
	})
	switch {
	case errors.Is(err, pgx.ErrNoRows):
		return nil
	case err != nil:
		return fmt.Errorf("accept pending invitation: %w", err)
	}

	if invite.RoleSlug.Valid {
		if workosUserID == "" {
			return errors.New("cannot sync invite role without WorkOS user id")
		}

		orgMetadata, err := s.orgRepo.GetOrganizationMetadata(ctx, invite.OrganizationID)
		if err != nil {
			return fmt.Errorf("get invite organization metadata: %w", err)
		}
		var workosMembershipID string
		if orgMetadata.WorkosID.Valid && orgMetadata.WorkosID.String != "" {
			membershipID, err := s.identity.UpdateOrganizationMembershipRole(ctx, workosUserID, orgMetadata.WorkosID.String, invite.RoleSlug.String)
			if err != nil {
				return fmt.Errorf("update WorkOS membership role: %w", err)
			}
			workosMembershipID = membershipID
		}

		if err := s.orgRepo.SyncUserOrganizationRoleAssignments(ctx, orgRepo.SyncUserOrganizationRoleAssignmentsParams{
			OrganizationID:     invite.OrganizationID,
			WorkosUserID:       workosUserID,
			WorkosRoleSlugs:    []string{invite.RoleSlug.String},
			UserID:             conv.ToPGText(gramUserID),
			WorkosMembershipID: conv.ToPGText(workosMembershipID),
			WorkosUpdatedAt:    pgtype.Timestamptz{Time: time.Now(), InfinityModifier: pgtype.Finite, Valid: true},
			WorkosLastEventID:  pgtype.Text{String: "", Valid: false},
		}); err != nil {
			return fmt.Errorf("sync invite role assignments: %w", err)
		}
		s.authz.InvalidateRoleCache(ctx, gramUserID, invite.OrganizationID)
	}

	if err := s.sessions.InvalidateUserInfoCache(ctx, gramUserID); err != nil {
		return fmt.Errorf("invalidate user info cache: %w", err)
	}
	return nil
}

func (s *Service) Login(ctx context.Context, payload *gen.LoginPayload) (res *gen.LoginResult, err error) {
	callbackURL := s.buildCallbackURL(ctx)

	nonce, err := generateNonce()
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "error generating login nonce").Log(ctx, s.logger)
	}
	// Store the nonce binding (cookie value set by middleware) so the
	// callback can verify the same browser that started login finishes it.
	binding := nonceBindingFromContext(ctx)
	if err := s.nonceStore.Set(ctx, nonceKey(nonce), binding, nonceTTL); err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "error storing login nonce").Log(ctx, s.logger)
	}

	state := encodeStateParam(payload, nonce)

	authURL, err := s.identity.BuildAuthorizationURL(ctx, identity.AuthorizationURLParams{
		CallbackURL:     callbackURL,
		State:           state,
		Scope:           "",
		ScopesSupported: nil,
	})
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "error building authorization URL").Log(ctx, s.logger)
	}

	return &gen.LoginResult{
		Location: authURL.String(),
	}, nil
}

// organizationSlugFromState extracts the org slug from the state param's
// final destination URL. Returns "" if the state is missing or has no org slug.
func organizationSlugFromState(payload *gen.CallbackPayload) string {
	state := decodeStateParam(payload)
	if state == nil {
		return ""
	}
	return organizationSlugFromDestinationURL(state.FinalDestinationURL)
}

func activeOrganizationFromState(payload *gen.CallbackPayload, organizations []sessions.Organization) (sessions.Organization, bool) {
	var empty sessions.Organization

	orgSlug := organizationSlugFromState(payload)
	if orgSlug == "" {
		return empty, false
	}

	for _, org := range organizations {
		if org.Slug == orgSlug {
			return org, true
		}
	}

	return empty, false
}

func activeOrganizationFromWorkOSID(workosOrgID string, organizations []sessions.Organization) (sessions.Organization, bool) {
	var empty sessions.Organization

	for _, org := range organizations {
		if org.WorkosID != nil && *org.WorkosID == workosOrgID {
			return org, true
		}
	}

	return empty, false
}

func organizationSlugFromDestinationURL(destinationURL string) string {
	location := relativeURL(destinationURL)
	if location == "" {
		return ""
	}

	parsed, err := url.Parse(location)
	if err != nil {
		return ""
	}

	path := strings.Trim(parsed.Path, "/")
	if path == "" {
		return ""
	}

	orgSlug, _, _ := strings.Cut(path, "/")
	return orgSlug
}

func (s *Service) SwitchScopes(ctx context.Context, payload *gen.SwitchScopesPayload) (res *gen.SwitchScopesResult, err error) {
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	if !ok || authCtx == nil || authCtx.SessionID == nil {
		return nil, oops.C(oops.CodeUnauthorized)
	}

	userInfo, _, err := s.sessions.GetUserInfo(ctx, authCtx.UserID)
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "error getting user info").Log(ctx, s.logger)
	}

	selectedOrg := authCtx.ActiveOrganizationID
	if payload.OrganizationID != nil {
		selectedOrg = *payload.OrganizationID
	}

	var selected sessions.Organization
	orgFound := false
	for _, org := range userInfo.Organizations {
		if org.ID == selectedOrg {
			selected = org
			orgFound = true
			break
		}
	}
	if !orgFound {
		return nil, oops.E(oops.CodeInvalid, nil, "organization not found in user info")
	}
	authCtx.ActiveOrganizationID = selectedOrg

	if _, err := s.orgRepo.UpsertOrganizationMetadata(ctx, orgRepo.UpsertOrganizationMetadataParams{
		ID:          selected.ID,
		Name:        selected.Name,
		Slug:        selected.Slug,
		WorkosID:    conv.PtrToPGText(selected.WorkosID),
		Whitelisted: pgtype.Bool{Bool: false, Valid: false},
	}); err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "error upserting organization metadata").Log(ctx, s.logger)
	}

	existingSession, err := s.sessions.GetSession(ctx, *authCtx.SessionID)
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "error loading existing session").Log(ctx, s.logger)
	}
	existingSession.ActiveOrganizationID = authCtx.ActiveOrganizationID
	if err := s.sessions.UpdateSession(ctx, existingSession); err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "error updating auth session").Log(ctx, s.logger)
	}

	return &gen.SwitchScopesResult{
		SessionToken:  *authCtx.SessionID,
		SessionCookie: *authCtx.SessionID,
	}, nil
}

func (s *Service) Logout(ctx context.Context, payload *gen.LogoutPayload) (res *gen.LogoutResult, err error) {
	// Clears cookie and invalidates session
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	if !ok || authCtx == nil || authCtx.SessionID == nil {
		return nil, oops.C(oops.CodeUnauthorized)
	}

	if err := s.sessions.InvalidateUserInfoCache(ctx, authCtx.UserID); err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "error invalidating user").Log(ctx, s.logger)
	}

	if err := s.sessions.ClearSession(ctx, sessions.Session{
		SessionID:            *authCtx.SessionID,
		ActiveOrganizationID: authCtx.ActiveOrganizationID,
		UserID:               authCtx.UserID,
		WorkOSSessionID:      "",
	}); err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "error clearing session").Log(ctx, s.logger)
	}
	return &gen.LogoutResult{SessionCookie: ""}, nil
}

func (s *Service) Info(ctx context.Context, payload *gen.InfoPayload) (res *gen.InfoResult, err error) {
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	if !ok || authCtx == nil || authCtx.SessionID == nil {
		return nil, oops.C(oops.CodeUnauthorized)
	}

	userInfo, _, err := s.sessions.GetUserInfo(ctx, authCtx.UserID)
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "error getting user info").Log(ctx, s.logger)
	}

	// For admins we only return the active organization to avoid overloaded returns.
	// The active org may not be in the admin's membership list (admin override),
	// so fall back to a DB lookup.
	if userInfo.Admin {
		found := false
		for _, org := range userInfo.Organizations {
			if org.ID == authCtx.ActiveOrganizationID {
				userInfo.Organizations = []sessions.Organization{org}
				found = true
				break
			}
		}
		if !found {
			orgMeta, err := s.orgRepo.GetOrganizationMetadata(ctx, authCtx.ActiveOrganizationID)
			if err == nil {
				userInfo.Organizations = []sessions.Organization{{
					ID:                 orgMeta.ID,
					Name:               orgMeta.Name,
					Slug:               orgMeta.Slug,
					WorkosID:           conv.FromPGText[string](orgMeta.WorkosID),
					UserWorkspaceSlugs: nil,
				}}
			}
		}
	}

	// Fully unpack the userInfo object
	organizations := make([]*gen.OrganizationEntry, 0, len(userInfo.Organizations))
	for _, org := range userInfo.Organizations {
		// TODO: Not the cleanest but a temporary measue while in POC phase.
		// This may actually be bettter executed from elsewhere
		projectRows, err := s.getProjectsOrSetupDefaults(ctx, org.ID)
		if err != nil {
			return nil, err
		}
		// Build the full list of project IDs, then filter to only those the
		// user is allowed to see (mirrors the projects.List endpoint).
		projectIDs := make([]string, 0, len(projectRows))
		for _, p := range projectRows {
			projectIDs = append(projectIDs, p.ID.String())
		}

		allowedIDs := projectIDs
		if len(projectIDs) > 0 && org.ID == authCtx.ActiveOrganizationID {
			checks := make([]authz.Check, len(projectIDs))
			for i, id := range projectIDs {
				checks[i] = authz.Check{Scope: authz.ScopeProjectRead, ResourceID: id, ResourceKind: "", Dimensions: nil}
			}
			allowedIDs, err = s.authz.Filter(ctx, checks)
			if err != nil {
				return nil, err
			}
		}

		allowed := make(map[string]struct{}, len(allowedIDs))
		for _, id := range allowedIDs {
			allowed[id] = struct{}{}
		}

		orgProjects := make([]*gen.ProjectEntry, 0, len(allowedIDs))
		for _, project := range projectRows {
			if _, ok := allowed[project.ID.String()]; !ok {
				continue
			}
			orgProjects = append(orgProjects, &gen.ProjectEntry{
				ID:   project.ID.String(),
				Name: project.Name,
				Slug: types.Slug(project.Slug),
			})
		}

		organizations = append(organizations, &gen.OrganizationEntry{
			ID:                 org.ID,
			Name:               org.Name,
			Slug:               org.Slug,
			UserWorkspaceSlugs: org.UserWorkspaceSlugs,
			Projects:           orgProjects,
		})
	}

	return &gen.InfoResult{
		SessionToken:          *authCtx.SessionID,
		SessionCookie:         *authCtx.SessionID,
		ActiveOrganizationID:  authCtx.ActiveOrganizationID,
		GramAccountType:       authCtx.AccountType,
		HasActiveSubscription: authCtx.HasActiveSubscription,
		Whitelisted:           authCtx.Whitelisted,
		UserID:                userInfo.UserID,
		UserEmail:             userInfo.Email,
		UserSignature:         userInfo.UserPylonSignature,
		UserDisplayName:       userInfo.DisplayName,
		UserPhotoURL:          userInfo.PhotoURL,
		IsAdmin:               userInfo.Admin,
		Organizations:         organizations,
	}, nil
}

func (s *Service) Register(ctx context.Context, payload *gen.RegisterPayload) (err error) {
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	if !ok || authCtx == nil || authCtx.SessionID == nil {
		return oops.C(oops.CodeUnauthorized)
	}

	if authCtx.ActiveOrganizationID != "" {
		return oops.E(oops.CodeInvalid, errors.New("user already has an active organization"), "user already has an active organization")
	}

	if payload.OrgName == "" {
		return oops.E(oops.CodeInvalid, errors.New("org name is required"), "org name is required")
	}

	if !validOrgNameRegex.MatchString(payload.OrgName) {
		return oops.E(oops.CodeInvalid, errors.New("organization name contains invalid characters"), "organization name contains invalid characters")
	}

	slug, err := orgslug.FindUnique(ctx, s.orgRepo, orgslug.Slugify(payload.OrgName))
	if err != nil {
		return oops.E(oops.CodeUnexpected, err, "error finding unique slug").Log(ctx, s.logger)
	}

	gramOrgID := "org_" + uuid.New().String()

	// Provision the WorkOS org first so it exists before we create the Gram org.
	workosOrgID, err := s.identity.ProvisionOrgInWorkOS(ctx, gramOrgID, payload.OrgName, authCtx.UserID)
	if err != nil {
		return oops.E(oops.CodeUnexpected, err, "error provisioning organization in WorkOS").Log(ctx, s.logger)
	}

	org, err := s.orgRepo.UpsertOrganizationMetadata(ctx, orgRepo.UpsertOrganizationMetadataParams{
		ID:          gramOrgID,
		Name:        payload.OrgName,
		Slug:        slug,
		WorkosID:    pgtype.Text{String: workosOrgID, Valid: workosOrgID != ""},
		Whitelisted: pgtype.Bool{Bool: false, Valid: true},
	})
	if err != nil {
		return oops.E(oops.CodeUnexpected, err, "error creating organization").Log(ctx, s.logger)
	}

	if _, err := s.orgRepo.UpsertOrganizationUserRelationship(ctx, orgRepo.UpsertOrganizationUserRelationshipParams{
		OrganizationID: org.ID,
		UserID:         conv.ToPGText(authCtx.UserID),
	}); err != nil {
		return oops.E(oops.CodeUnexpected, err, "error creating organization user relationship").Log(ctx, s.logger)
	}

	if err := s.sessions.InvalidateUserInfoCache(ctx, authCtx.UserID); err != nil {
		return oops.E(oops.CodeUnexpected, err, "error invalidating user info cache").Log(ctx, s.logger)
	}

	existingSession, err := s.sessions.GetSession(ctx, *authCtx.SessionID)
	if err != nil {
		return oops.E(oops.CodeUnexpected, err, "error loading existing session").Log(ctx, s.logger)
	}
	existingSession.ActiveOrganizationID = org.ID
	if err := s.sessions.UpdateSession(ctx, existingSession); err != nil {
		return oops.E(oops.CodeUnexpected, err, "error storing session").Log(ctx, s.logger)
	}

	return nil
}

func (s *Service) autoProvisionForAssistants(ctx context.Context, userInfo *sessions.CachedUserInfo, session *sessions.Session) (string, error) {
	orgName := generateLegibleOrgName()

	slug, err := orgslug.FindUnique(ctx, s.orgRepo, orgslug.Slugify(orgName))
	if err != nil {
		return "", fmt.Errorf("find unique slug: %w", err)
	}

	gramOrgID := "org_" + uuid.New().String()

	// Provision the WorkOS org first so it exists before we create the Gram org.
	workosOrgID, err := s.identity.ProvisionOrgInWorkOS(ctx, gramOrgID, orgName, userInfo.UserID)
	if err != nil {
		return "", fmt.Errorf("provision org in WorkOS: %w", err)
	}

	org, err := s.orgRepo.UpsertOrganizationMetadata(ctx, orgRepo.UpsertOrganizationMetadataParams{
		ID:          gramOrgID,
		Name:        orgName,
		Slug:        slug,
		WorkosID:    pgtype.Text{String: workosOrgID, Valid: workosOrgID != ""},
		Whitelisted: pgtype.Bool{Bool: true, Valid: true},
	})
	if err != nil {
		return "", fmt.Errorf("create organization: %w", err)
	}

	if _, err := s.orgRepo.UpsertOrganizationUserRelationship(ctx, orgRepo.UpsertOrganizationUserRelationshipParams{
		OrganizationID: org.ID,
		UserID:         conv.ToPGText(userInfo.UserID),
	}); err != nil {
		return "", fmt.Errorf("create org-user relationship: %w", err)
	}

	if invalidationErr := s.sessions.InvalidateUserInfoCache(ctx, userInfo.UserID); invalidationErr != nil {
		return "", fmt.Errorf("invalidate user info cache: %w", invalidationErr)
	}

	projects, err := s.getProjectsOrSetupDefaults(ctx, org.ID)
	if err != nil {
		return "", fmt.Errorf("setup default project: %w", err)
	}
	if len(projects) == 0 {
		return "", errors.New("default project missing after setup")
	}

	subID, benefitErr := s.billing.AttachAssistantsBenefit(ctx, org.ID, userInfo.Email)
	if benefitErr != nil {
		s.logger.ErrorContext(ctx, "failed to attach assistants benefit", attr.SlogError(benefitErr), attr.SlogOrganizationID(org.ID))
	} else if subID != "" {
		if err := s.cancelSubsScheduler.ScheduleCancelAssistantsSubscription(ctx, subID); err != nil {
			s.logger.ErrorContext(ctx, "failed to schedule assistants subscription cancel-at-period-end", attr.SlogError(err), attr.SlogOrganizationID(org.ID))
		}
	}

	session.ActiveOrganizationID = org.ID
	if err := s.sessions.StoreSession(ctx, *session); err != nil {
		return "", fmt.Errorf("store session: %w", err)
	}

	if err := s.posthog.IdentifyUser(ctx, userInfo.Email, map[string]any{
		"disposition": dispositionAssistants,
	}); err != nil {
		s.logger.ErrorContext(ctx, "failed to set assistants disposition person property", attr.SlogError(err), attr.SlogOrganizationID(org.ID))
	}

	if err := s.posthog.CaptureEvent(ctx, "gram_assistants_signup", userInfo.Email, map[string]any{
		"email":                       userInfo.Email,
		"organization_id":             org.ID,
		"organization_slug":           org.Slug,
		"disposition":                 dispositionAssistants,
		"has_assistants_subscription": subID != "",
	}); err != nil {
		s.logger.ErrorContext(ctx, "failed to capture gram_assistants_signup event", attr.SlogError(err), attr.SlogOrganizationID(org.ID))
	}

	return fmt.Sprintf("/%s/projects/%s/assistants/new?disposition=%s", org.Slug, projects[0].Slug, dispositionAssistants), nil
}

func dispositionFromState(payload *gen.CallbackPayload) string {
	state := decodeStateParam(payload)
	if state == nil {
		return ""
	}
	parsed, err := url.Parse(relativeURL(state.FinalDestinationURL))
	if err != nil {
		return ""
	}
	return parsed.Query().Get("disposition")
}

func (s *Service) getProjectsOrSetupDefaults(ctx context.Context, organizationID string) ([]projectsRepo.Project, error) {
	projects, err := s.projectsRepo.ListProjectsByOrganization(ctx, organizationID)
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "error listing projects").Log(ctx, s.logger)
	}

	if len(projects) == 0 {
		project, err := s.createDefaultProject(ctx, organizationID)
		if err != nil {
			return nil, err
		}

		_, err = s.envRepo.CreateEnvironment(ctx, envRepo.CreateEnvironmentParams{
			OrganizationID: organizationID,
			ProjectID:      project.ID,
			Name:           "Default",
			Slug:           "default",
			Description:    conv.ToPGText("Default project for organization"),
		})
		if err != nil {
			return nil, oops.E(oops.CodeUnexpected, err, "error creating default environment").Log(ctx, s.logger)
		}

		projects = append(projects, project)
	}

	return projects, nil
}

func (s *Service) createDefaultProject(ctx context.Context, organizationID string) (projectsRepo.Project, error) {
	project, err := s.projectsRepo.CreateProject(ctx, projectsRepo.CreateProjectParams{
		OrganizationID: organizationID,
		Name:           "Default",
		Slug:           "default",
	})
	var empty projectsRepo.Project
	if err != nil {
		var pgErr *pgconn.PgError
		if errors.As(err, &pgErr) && pgErr.Code == pgerrcode.UniqueViolation {
			return empty, oops.E(oops.CodeConflict, nil, "project already exists")
		}
		return empty, oops.E(oops.CodeUnexpected, err, "error creating default project").Log(ctx, s.logger)
	}

	return project, nil
}

type loginState struct {
	FinalDestinationURL string `json:"final_destination_url"`
	Nonce               string `json:"nonce,omitempty"`
}

func encodeStateParam(payload *gen.LoginPayload, nonce string) string {
	state := loginState{
		FinalDestinationURL: conv.PtrValOr(payload.Redirect, ""),
		Nonce:               nonce,
	}

	jsonBytes, err := json.Marshal(state)
	if err != nil {
		return ""
	}

	return base64.RawURLEncoding.EncodeToString(jsonBytes)
}

func decodeStateParam(payload *gen.CallbackPayload) *loginState {
	if payload == nil {
		return nil
	}

	rawB64 := conv.PtrValOr(payload.State, "")
	if rawB64 == "" {
		return nil
	}

	rawJSON, err := base64.RawURLEncoding.DecodeString(rawB64)
	if err != nil {
		return nil
	}

	var state *loginState
	err = json.Unmarshal(rawJSON, &state)
	if err != nil {
		return nil
	}

	return state
}

// generateNonce produces a cryptographically random 16-byte hex string for use
// as the OAuth state nonce.
func generateNonce() (string, error) {
	b := make([]byte, 16)
	if _, err := rand.Read(b); err != nil {
		return "", fmt.Errorf("generate nonce: %w", err)
	}
	return hex.EncodeToString(b), nil
}

func nonceKey(nonce string) string {
	return "auth:login_nonce:" + nonce
}

// validateAuthNonce validates that the OAuth callback was initiated by a Login call
// that Gram controls, preventing CSRF attacks where an attacker crafts a
// callback URL with a stolen authorization code. Without this, the state param
// is caller-controlled base64 JSON with no server-side binding — an attacker
// can forge it freely.
//
// The nonce is stored in Redis during Login with a short TTL and consumed
// (deleted) here atomically so each nonce is single-use. The stored value is
// the nonce-binding cookie that was set on the browser during Login — this
// ties the nonce to the specific browser session, preventing login CSRF.
func (s *Service) validateAuthNonce(ctx context.Context, payload *gen.CallbackPayload) error {
	state := decodeStateParam(payload)
	if state == nil || state.Nonce == "" {
		return errors.New("missing or invalid state parameter")
	}

	key := nonceKey(state.Nonce)
	var storedBinding string
	if err := s.nonceStore.GetAndDelete(ctx, key, &storedBinding); err != nil {
		return errors.New("invalid or expired login nonce")
	}

	// Verify the cookie binding matches what was stored during Login.
	// This ensures the same browser that started the login flow is
	// completing it, preventing login CSRF attacks.
	cookieBinding := nonceBindingFromContext(ctx)
	if cookieBinding == "" || subtle.ConstantTimeCompare([]byte(storedBinding), []byte(cookieBinding)) != 1 {
		return errors.New("login session mismatch: nonce not bound to this browser")
	}

	return nil
}

// buildCallbackURL constructs the OIDC redirect_uri that the IDP will send the
// user back to after authentication. Must match what Login passes to
// BuildAuthorizationURL.
func (s *Service) buildCallbackURL(ctx context.Context) string {
	returnAddress := strings.TrimRight(s.cfg.GramServerURL, "/")
	if s.cfg.Environment == "local" {
		returnAddress = strings.TrimRight(s.cfg.SignInRedirectURL, "/")
	}

	if requestCtx, ok := contextvalues.GetRequestContext(ctx); ok && requestCtx != nil && strings.Contains(requestCtx.Host, "speakeasyapi.vercel.app") && s.cfg.Environment == "dev" {
		returnAddress = "https://" + requestCtx.Host
	}

	// In local dev, if the login request came from the setup subdomain
	// (detected via Referer host, since the Vite proxy rewrites Host),
	// route the IDP callback through the setup origin so the nonce-binding
	// cookie is scoped correctly.
	if s.cfg.Environment == "local" && s.cfg.SetupSiteURL != "" {
		if requestCtx, ok := contextvalues.GetRequestContext(ctx); ok && requestCtx != nil {
			setupURL, err := url.Parse(s.cfg.SetupSiteURL)
			if err == nil && requestCtx.RefererHost == setupURL.Host {
				returnAddress = strings.TrimRight(s.cfg.SetupSiteURL, "/")
			}
		}
	}

	return returnAddress + "/rpc/auth.callback"
}

// validOrgNameRegex allows alphanumeric characters, spaces, hyphens, and underscores.
var validOrgNameRegex = regexp.MustCompile(`^[a-zA-Z0-9\s-_]+$`)

// callbackRedirectURL determines the redirect location after authentication. It
// only allows relative URLs to prevent open redirect attacks (see relativeURL),
// with the exception of the trusted setup subdomain origin (SetupSiteURL).
// If no redirect is found, fall back to SignInRedirectURL.
func (s *Service) callbackRedirectURL(
	ctx context.Context,
	payload *gen.CallbackPayload,
) string {
	if state := decodeStateParam(payload); state != nil {
		// Allow absolute redirects to the trusted setup subdomain.
		if s.cfg.SetupSiteURL != "" && isTrustedSetupRedirect(state.FinalDestinationURL, s.cfg.SetupSiteURL) {
			s.logger.InfoContext(ctx, fmt.Sprintf("Redirecting to setup domain: '%s'", state.FinalDestinationURL))
			return state.FinalDestinationURL
		}

		if location := relativeURL(state.FinalDestinationURL); location != "" {
			s.logger.InfoContext(ctx, fmt.Sprintf("Found destination URL in state: '%s'", location))
			return location
		}
	}

	return s.cfg.SignInRedirectURL
}

// isTrustedSetupRedirect returns true if destURL is an absolute URL whose
// origin (scheme + host) matches the trusted setup site URL.
func isTrustedSetupRedirect(destURL, setupSiteURL string) bool {
	dest, err := url.Parse(destURL)
	if err != nil || dest.Host == "" {
		return false
	}
	trusted, err := url.Parse(setupSiteURL)
	if err != nil || trusted.Host == "" {
		return false
	}
	return dest.Scheme == trusted.Scheme && dest.Host == trusted.Host
}

// relativeURL converts any URL to a safe relative URL by extracting only the
// path, query, and fragment components.
//
// Examples:
//   - "/dashboard" → "/dashboard"
//   - "/projects?id=123#section" → "/projects?id=123#section"
//   - "http://localhost:3000/dashboard" → "/dashboard"
//   - "https://evil-site.com/phishing" → "/phishing"
//   - "//evil.com/phish" → ""
//   - "invalid:///" → ""
func relativeURL(urlStr string) string {
	parsed, err := url.Parse(urlStr)
	if err != nil {
		return ""
	}

	isRelative := parsed.Host == "" && parsed.Scheme == ""
	if isRelative {
		return urlStr
	}

	rel := parsed.Path
	if parsed.RawQuery != "" {
		rel += "?" + parsed.RawQuery
	}
	if parsed.Fragment != "" {
		rel += "#" + parsed.Fragment
	}

	return rel
}
