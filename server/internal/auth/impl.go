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
	"strings"
	"time"

	redisCache "github.com/go-redis/cache/v9"
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
	"github.com/speakeasy-api/gram/server/internal/audit"
	"github.com/speakeasy-api/gram/server/internal/auth/identity"
	"github.com/speakeasy-api/gram/server/internal/auth/orgslug"
	"github.com/speakeasy-api/gram/server/internal/auth/sessions"
	"github.com/speakeasy-api/gram/server/internal/authz"
	"github.com/speakeasy-api/gram/server/internal/billing"
	"github.com/speakeasy-api/gram/server/internal/cache"
	"github.com/speakeasy-api/gram/server/internal/constants"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/conv"
	envRepo "github.com/speakeasy-api/gram/server/internal/environments/repo"
	"github.com/speakeasy-api/gram/server/internal/middleware"
	"github.com/speakeasy-api/gram/server/internal/o11y"
	"github.com/speakeasy-api/gram/server/internal/oops"
	orgRepo "github.com/speakeasy-api/gram/server/internal/organizations/repo"
	projectsRepo "github.com/speakeasy-api/gram/server/internal/projects/repo"
	"github.com/speakeasy-api/gram/server/internal/thirdparty/posthog"
	trialsRepo "github.com/speakeasy-api/gram/server/internal/trials/repo"
	"github.com/speakeasy-api/gram/server/internal/trialemails"
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
	authzProvisioner    *authz.Provisioner
	trialBundleSeeder   EnterpriseTrialBundleSeeder
	auditLogger         *audit.Logger
	trialNotifier       trialemails.Notifier
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
	authzProvisioner *authz.Provisioner,
	trialBundleSeeder EnterpriseTrialBundleSeeder,
	auditLogger *audit.Logger,
	trialNotifier trialemails.Notifier,
) *Service {
	logger = logger.With(attr.SlogComponent("auth"))
	if trialNotifier == nil {
		trialNotifier = trialemails.NoopNotifier{}
	}

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
		authzProvisioner:    authzProvisioner,
		trialBundleSeeder:   trialBundleSeeder,
		auditLogger:         auditLogger,
		trialNotifier:       trialNotifier,
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

	srv.Mount(mux, server)
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
		return ctx, oops.E(oops.CodeUnexpected, err, "load access grants").LogError(ctx, s.logger)
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

	// Consume the signup intent on every path, not only the zero-org one, so a
	// returning user who happens to carry one does not leave a key behind for
	// its full TTL. Absence is normal — an ordinary login has none.
	var intent *signupIntent
	if state := decodeStateParam(payload); state != nil && state.Nonce != "" {
		var stored signupIntent
		err := s.nonceStore.GetAndDelete(ctx, signupIntentKey(state.Nonce), &stored)
		switch {
		case err == nil:
			intent = &stored
		case errors.Is(err, redisCache.ErrCacheMiss):
			// Ordinary login. Nothing to do.
		default:
			// Swallow so a cache blip cannot fail an otherwise valid login, but
			// say so: without this line a signup silently degrades into landing
			// on the register page with nothing explaining why.
			s.logger.WarnContext(ctx, "failed to read signup intent", attr.SlogError(err))
		}
	}

	idpUser, err := s.identity.ExchangeCodeForTokens(ctx, payload.Code)
	if err != nil {
		return redirectWithError(authErrCodeLookup, err)
	}

	userID, err := s.identity.UpsertUserFromIDP(ctx, idpUser)
	if err != nil {
		return redirectWithError(authErrInit, err)
	}

	if idpUser.Sub != "" {
		if err := s.identity.SyncMembershipsFromWorkOS(ctx, userID, idpUser.Sub); err != nil {
			return redirectWithError(authErrInit, err)
		}
	}

	userInfo, _, err := s.identity.GetUserInfo(ctx, userID)
	if err != nil {
		return redirectWithError(authErrInit, err)
	}

	sessionID, err := sessions.NewSessionID()
	if err != nil {
		return redirectWithError(authErrInit, err)
	}
	session := sessions.Session{
		SessionID:            sessionID,
		UserID:               userID,
		ActiveOrganizationID: "",
		WorkOSSessionID:      idpUser.WorkOSSessionID,
	}

	if len(userInfo.Organizations) == 0 {
		// Signup: the user typed a company name before authenticating. Create
		// the org now so they land in the product instead of a second form.
		// A user-typed name beats a generated one, so this wins over the
		// assistants disposition below.
		if intent != nil && intent.OrgName != "" {
			org, err := s.provisionOrgForUser(ctx, userID, intent.OrgName, orgProvisionOptions{
				Whitelisted:    true,
				ProvisionTrial: true,
			})
			if err != nil {
				return s.redirectSignupError(ctx, err)
			}

			session.ActiveOrganizationID = org.ID
			if err := s.sessions.StoreSession(ctx, session); err != nil {
				return s.redirectSignupError(ctx, err)
			}

			if err := s.trialNotifier.TrialStarted(ctx, org.ID); err != nil {
				s.logger.ErrorContext(ctx, "failed to notify trial started", attr.SlogError(err), attr.SlogOrganizationID(org.ID), attr.SlogUserID(userID))
			}

			s.captureSignupTelemetry(ctx, userInfo.Email, intent.OrgName, org)

			return &gen.CallbackResult{
				Location:      s.callbackRedirectURL(ctx, payload),
				SessionToken:  session.SessionID,
				SessionCookie: session.SessionID,
			}, nil
		}

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
	if inviteeEmail := conv.NormalizeEmail(userInfo.Email); inviteeEmail != "" {
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
	}

	if err := s.sessions.InvalidateUserInfoCache(ctx, gramUserID); err != nil {
		return fmt.Errorf("invalidate user info cache: %w", err)
	}
	return nil
}

func (s *Service) Login(ctx context.Context, payload *gen.LoginPayload) (res *gen.LoginResult, err error) {
	// A company name means this login started from the sign-up page. Validate
	// before anything is minted or stored, so a rejected name leaves no orphan
	// nonce behind — and so a bad name fails before the identity-provider hop
	// rather than after it. Only when the parameter is present: a malformed
	// value must never be able to block an ordinary login.
	orgName := strings.TrimSpace(conv.PtrValOr(payload.OrgName, ""))
	if orgName != "" {
		if err := validateOrgName(orgName); err != nil {
			return nil, err
		}
	}

	// An email means the sign-up page collected one to pre-fill on the identity
	// provider's screen. Validated at the same gate and for the same reasons,
	// and likewise only when present.
	//
	// Deliberately never stored: nothing server-side reads it after the
	// redirect, and stashing an unverified address alongside the company name
	// would invite a later reader to treat it as identity.
	email := strings.TrimSpace(conv.PtrValOr(payload.Email, ""))
	if email != "" {
		if err := validateSignupEmail(email); err != nil {
			return nil, err
		}
	}

	callbackURL := s.buildCallbackURL(ctx)

	nonce, err := generateNonce()
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "error generating login nonce").LogError(ctx, s.logger)
	}
	// Store the nonce binding (cookie value set by middleware) so the
	// callback can verify the same browser that started login finishes it.
	binding := nonceBindingFromContext(ctx)
	if err := s.nonceStore.Set(ctx, nonceKey(nonce), binding, nonceTTL); err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "error storing login nonce").LogError(ctx, s.logger)
	}

	// Stash the name against the nonce so the callback can create the org
	// inline. It arrives as a query param on this request and goes no further:
	// it is not encoded into the state param, so it does not travel through the
	// identity provider or come back on the redirect.
	if orgName != "" {
		if err := s.nonceStore.Set(ctx, signupIntentKey(nonce), signupIntent{OrgName: orgName}, nonceTTL); err != nil {
			return nil, oops.E(oops.CodeUnexpected, err, "error storing signup intent").LogError(ctx, s.logger)
		}
	}

	state := encodeStateParam(payload, nonce)

	// The company name is what marks a login as having begun on /sign-up, so it
	// alone selects AuthKit's sign-up screen. Keeping one signal authoritative
	// beats a second flag that can drift out of sync with it. The email only
	// fills the field in, so a sign-up without one still lands on the right
	// screen.
	screenHint := ""
	if orgName != "" {
		screenHint = "sign-up"
	}

	authURL, err := s.identity.BuildAuthorizationURL(ctx, identity.AuthorizationURLParams{
		CallbackURL:     callbackURL,
		State:           state,
		Scope:           "",
		ScopesSupported: nil,
		LoginHint:       email,
		ScreenHint:      screenHint,
	})
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "error building authorization URL").LogError(ctx, s.logger)
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
		return nil, oops.E(oops.CodeUnexpected, err, "error getting user info").LogError(ctx, s.logger)
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
		return nil, oops.E(oops.CodeUnexpected, err, "error upserting organization metadata").LogError(ctx, s.logger)
	}

	existingSession, err := s.sessions.GetSession(ctx, *authCtx.SessionID)
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "error loading existing session").LogError(ctx, s.logger)
	}
	existingSession.ActiveOrganizationID = authCtx.ActiveOrganizationID
	if err := s.sessions.UpdateSession(ctx, existingSession); err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "error updating auth session").LogError(ctx, s.logger)
	}

	return &gen.SwitchScopesResult{
		SessionToken:  *authCtx.SessionID,
		SessionCookie: *authCtx.SessionID,
	}, nil
}

// EnterDemo points the current session at the shared read-only demo
// organization. Any authenticated user may enter — the demo org has no
// membership rows, so downstream request auth and grant resolution rely on
// carve-outs keyed off constants.DemoOrganizationID.
func (s *Service) EnterDemo(ctx context.Context, payload *gen.EnterDemoPayload) (res *gen.EnterDemoResult, err error) {
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	if !ok || authCtx == nil || authCtx.SessionID == nil {
		return nil, oops.C(oops.CodeUnauthorized)
	}

	orgMetadata, err := s.orgRepo.GetOrganizationMetadata(ctx, constants.DemoOrganizationID)
	switch {
	case errors.Is(err, pgx.ErrNoRows):
		return nil, oops.E(oops.CodeNotFound, err, "demo organization is not available").LogError(ctx, s.logger)
	case err != nil:
		return nil, oops.E(oops.CodeUnexpected, err, "error loading demo organization").LogError(ctx, s.logger)
	}
	if orgMetadata.DisabledAt.Valid {
		return nil, oops.E(oops.CodeNotFound, nil, "demo organization is not available")
	}

	existingSession, err := s.sessions.GetSession(ctx, *authCtx.SessionID)
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "error loading existing session").LogError(ctx, s.logger)
	}
	existingSession.ActiveOrganizationID = constants.DemoOrganizationID
	if err := s.sessions.UpdateSession(ctx, existingSession); err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "error updating auth session").LogError(ctx, s.logger)
	}

	return &gen.EnterDemoResult{
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
		return nil, oops.E(oops.CodeUnexpected, err, "error invalidating user").LogError(ctx, s.logger)
	}

	if err := s.sessions.ClearSession(ctx, sessions.Session{
		SessionID:            *authCtx.SessionID,
		ActiveOrganizationID: authCtx.ActiveOrganizationID,
		UserID:               authCtx.UserID,
		WorkOSSessionID:      "",
	}); err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "error clearing session").LogError(ctx, s.logger)
	}
	return &gen.LogoutResult{SessionCookie: ""}, nil
}

func (s *Service) Info(ctx context.Context, payload *gen.InfoPayload) (res *gen.InfoResult, err error) {
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	if !ok || authCtx == nil || authCtx.SessionID == nil {
		return nil, oops.C(oops.CodeUnauthorized)
	}

	trial := loadActiveTrial(
		ctx,
		authCtx.ActiveOrganizationID,
		trialsRepo.New(s.db).GetActiveTrial,
		s.logger,
	)

	userInfo, _, err := s.sessions.GetUserInfo(ctx, authCtx.UserID)
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "error getting user info").LogError(ctx, s.logger)
	}

	// Sessions in the shared demo org: append the demo org alongside real
	// memberships (there is no membership row for it), so the dashboard can
	// resolve the active org AND still offer the user's own orgs to exit to.
	// A failed lookup is an error, not a silent omission — without the entry
	// the dashboard cannot resolve the active org or offer an exit.
	if authCtx.ActiveOrganizationID == constants.DemoOrganizationID {
		orgMeta, err := s.orgRepo.GetOrganizationMetadata(ctx, constants.DemoOrganizationID)
		if err != nil {
			return nil, oops.E(oops.CodeUnexpected, err, "error loading demo organization").LogError(ctx, s.logger)
		}
		userInfo.Organizations = append(userInfo.Organizations, sessions.Organization{
			ID:                 orgMeta.ID,
			Name:               orgMeta.Name,
			Slug:               orgMeta.Slug,
			WorkosID:           conv.FromPGText[string](orgMeta.WorkosID),
			UserWorkspaceSlugs: nil,
			SSOEnabled:         orgMeta.SsoEnabled.Bool,
			SCIMEnabled:        orgMeta.ScimEnabled.Bool,
		})
	} else if userInfo.Admin {
		// For admins overriding into a foreign org (one not in their own membership list),
		// return only that org to avoid overloaded returns. When admins are in one of their
		// own orgs, return all real memberships so the org-switcher works normally.
		inOwnOrg := false
		for _, org := range userInfo.Organizations {
			if org.ID == authCtx.ActiveOrganizationID {
				inOwnOrg = true
				break
			}
		}
		if !inOwnOrg {
			orgMeta, err := s.orgRepo.GetOrganizationMetadata(ctx, authCtx.ActiveOrganizationID)
			if err == nil {
				userInfo.Organizations = []sessions.Organization{{
					ID:                 orgMeta.ID,
					Name:               orgMeta.Name,
					Slug:               orgMeta.Slug,
					WorkosID:           conv.FromPGText[string](orgMeta.WorkosID),
					UserWorkspaceSlugs: nil,
					SSOEnabled:         orgMeta.SsoEnabled.Bool,
					SCIMEnabled:        orgMeta.ScimEnabled.Bool,
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
			SsoEnabled:         &org.SSOEnabled,
			ScimEnabled:        &org.SCIMEnabled,
		})
	}

	return &gen.InfoResult{
		SessionToken:          *authCtx.SessionID,
		SessionCookie:         *authCtx.SessionID,
		ActiveOrganizationID:  authCtx.ActiveOrganizationID,
		GramAccountType:       authCtx.AccountType,
		HasActiveSubscription: authCtx.HasActiveSubscription,
		Whitelisted:           authCtx.Whitelisted,
		Trial:                 trial,
		UserID:                userInfo.UserID,
		UserEmail:             userInfo.Email,
		UserSignature:         userInfo.UserPylonSignature,
		UserDisplayName:       userInfo.DisplayName,
		UserPhotoURL:          userInfo.PhotoURL,
		IsAdmin:               userInfo.Admin,
		Organizations:         organizations,
	}, nil
}

func loadActiveTrial(
	ctx context.Context,
	organizationID string,
	getTrial func(context.Context, string) (trialsRepo.GetActiveTrialRow, error),
	logger *slog.Logger,
) *gen.Trial {
	trial, err := getTrial(ctx, organizationID)
	switch {
	case errors.Is(err, pgx.ErrNoRows):
		return nil
	case err != nil:
		logger.ErrorContext(
			ctx,
			"error loading active trial; continuing without trial status",
			attr.SlogError(err),
			attr.SlogOrganizationID(organizationID),
		)
		return nil
	default:
		return &gen.Trial{
			StartedAt: conv.FromPGTimestamptz(trial.CreatedAt),
			EndsAt:    conv.FromPGTimestamptz(trial.EndsAt),
		}
	}
}

// orgProvisionOptions carries the per-caller choices for a new organization.
// The choices are a struct rather than positional booleans because every call
// site then names what it asks for, and exhaustruct forces a new field to be
// answered everywhere instead of defaulting to false.
type orgProvisionOptions struct {
	// Whitelisted clears the dashboard's book-a-demo gate for the organization.
	Whitelisted bool

	// ProvisionTrial arms a 14-day enterprise trial in the same transaction
	// that creates the organization.
	ProvisionTrial bool
}

// provisionOrgForUser creates an organization and attaches a user to it as the
// first member. Shared by the three paths that create orgs: self-serve register,
// signup with a company name carried through login, and assistants
// auto-provisioning.
//
// Session mutation is deliberately left to the caller. Register updates an
// already-stored session; the callback-side callers assign
// ActiveOrganizationID on an in-memory session before storing it once.
func (s *Service) provisionOrgForUser(ctx context.Context, userID, orgName string, opts orgProvisionOptions) (orgRepo.OrganizationMetadatum, error) {
	var empty orgRepo.OrganizationMetadatum

	slug, err := orgslug.FindUnique(ctx, s.orgRepo, orgslug.Slugify(orgName))
	if err != nil {
		return empty, fmt.Errorf("find unique slug: %w", err)
	}

	// Provision the WorkOS org first, then derive a deterministic UUIDv5 org ID from the WorkOS ID.
	provisionedOrg, err := s.identity.ProvisionOrgInWorkOS(ctx, orgName, userID)
	if err != nil {
		return empty, fmt.Errorf("provision org in WorkOS: %w", err)
	}

	// Metadata, the user relationship, the admin grant, and any trial all land
	// in one transaction, so a failure part-way cannot leave an organization a
	// user can authenticate into without access control.
	org, err := s.persistProvisionedOrganization(ctx, provisionedOrg, orgName, slug, userID, opts)
	if err != nil {
		return empty, fmt.Errorf("persist provisioned organization: %w", err)
	}

	if err := s.sessions.InvalidateUserInfoCache(ctx, userID); err != nil {
		return empty, fmt.Errorf("invalidate user info cache: %w", err)
	}

	return org, nil
}

func (s *Service) Register(ctx context.Context, payload *gen.RegisterPayload) (err error) {
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	if !ok || authCtx == nil || authCtx.SessionID == nil {
		return oops.C(oops.CodeUnauthorized)
	}

	if authCtx.ActiveOrganizationID != "" {
		return oops.E(oops.CodeInvalid, errors.New("user already has an active organization"), "user already has an active organization")
	}

	if err := validateOrgName(payload.OrgName); err != nil {
		return err
	}

	org, err := s.provisionOrgForUser(ctx, authCtx.UserID, payload.OrgName, orgProvisionOptions{
		Whitelisted:    true,
		ProvisionTrial: true,
	})
	if err != nil {
		return oops.E(oops.CodeUnexpected, err, "error creating organization").LogError(ctx, s.logger)
	}

	existingSession, err := s.sessions.GetSession(ctx, *authCtx.SessionID)
	if err != nil {
		return oops.E(oops.CodeUnexpected, err, "error loading existing session").LogError(ctx, s.logger)
	}
	existingSession.ActiveOrganizationID = org.ID
	if err := s.sessions.UpdateSession(ctx, existingSession); err != nil {
		return oops.E(oops.CodeUnexpected, err, "error storing session").LogError(ctx, s.logger)
	}

	return nil
}

func (s *Service) autoProvisionForAssistants(ctx context.Context, userInfo *sessions.CachedUserInfo, session *sessions.Session) (string, error) {
	orgName := generateLegibleOrgName()

	// Assistants is a live product for users who never asked for a trial, so a
	// trial armed here would strip their entitlements two weeks later.
	org, err := s.provisionOrgForUser(ctx, userInfo.UserID, orgName, orgProvisionOptions{
		Whitelisted:    true,
		ProvisionTrial: false,
	})
	if err != nil {
		return "", err
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

// persistProvisionedOrganization writes the Gram-side records for an
// organization that already exists in WorkOS, and returns the row as it stood
// before any trial arming. A trial armed under opts.ProvisionTrial joins the
// same transaction, so a failure leaves neither behind.
func (s *Service) persistProvisionedOrganization(
	ctx context.Context,
	provisionedOrg identity.ProvisionedOrganization,
	orgName string,
	slug string,
	userID string,
	opts orgProvisionOptions,
) (orgRepo.OrganizationMetadatum, error) {
	tx, err := s.db.Begin(ctx)
	if err != nil {
		return orgRepo.OrganizationMetadatum{}, fmt.Errorf("begin organization provisioning transaction: %w", err)
	}
	defer o11y.NoLogDefer(func() error { return tx.Rollback(ctx) })

	queries := orgRepo.New(tx)
	org, err := queries.UpsertOrganizationMetadata(ctx, orgRepo.UpsertOrganizationMetadataParams{
		ID:          provisionedOrg.GramOrganizationID,
		Name:        orgName,
		Slug:        slug,
		WorkosID:    pgtype.Text{String: provisionedOrg.WorkOSOrganizationID, Valid: provisionedOrg.WorkOSOrganizationID != ""},
		Whitelisted: pgtype.Bool{Bool: opts.Whitelisted, Valid: true},
	})
	if err != nil {
		return orgRepo.OrganizationMetadatum{}, fmt.Errorf("create organization metadata: %w", err)
	}

	if _, err := queries.UpsertOrganizationUserRelationship(ctx, orgRepo.UpsertOrganizationUserRelationshipParams{
		OrganizationID: org.ID,
		UserID:         conv.ToPGText(userID),
	}); err != nil {
		return orgRepo.OrganizationMetadatum{}, fmt.Errorf("create organization user relationship: %w", err)
	}

	if err := s.authzProvisioner.ProvisionOrganizationAdminTx(ctx, tx, org.ID, authz.InitialOrganizationAdmin{
		UserID:             userID,
		WorkOSUserID:       provisionedOrg.WorkOSUserID,
		WorkOSMembershipID: provisionedOrg.WorkOSMembershipID,
	}); err != nil {
		return orgRepo.OrganizationMetadatum{}, fmt.Errorf("provision organization access: %w", err)
	}

	if opts.ProvisionTrial {
		if err := s.armEnterpriseTrialTx(ctx, tx, org, userID); err != nil {
			return orgRepo.OrganizationMetadatum{}, fmt.Errorf("arm enterprise trial: %w", err)
		}
	}

	if err := tx.Commit(ctx); err != nil {
		return orgRepo.OrganizationMetadatum{}, fmt.Errorf("commit organization provisioning transaction: %w", err)
	}

	return org, nil
}

// redirectSignupError sends a failed signup back to the page it started on.
// The user is authenticated by this point but has no org, so /sign-up renders
// the error and a retry: hitting the CTA again is a fast bounce through the
// identity provider, since the browser still holds a live IDP session.
//
// Only the immediate redirect is origin-aware. Any later visit to the app hits
// the blanket zero-org redirect in the dashboard's app layout and lands on
// /register — making that split persist would mean storing the flow origin on
// the session, which is not worth it for this path.
func (s *Service) redirectSignupError(ctx context.Context, err error) (*gen.CallbackResult, error) {
	s.logger.ErrorContext(ctx, "signup provisioning failed", attr.SlogError(err), attr.SlogReason(string(authErrInit)))

	base := strings.TrimRight(s.cfg.SignInRedirectURL, "/")
	return &gen.CallbackResult{
		Location:      fmt.Sprintf("%s/sign-up?signin_error=%s", base, authErrInit),
		SessionToken:  "",
		SessionCookie: "",
	}, nil
}

// captureSignupTelemetry records a self-serve signup. new_org_created otherwise
// only fires client-side from the register page, which a signup never renders —
// without this the existing funnel would flatline for signup users.
//
// Reuses the onboarding_event + action shape that the register page and the
// onboarding flows already emit, so existing charts stay continuous. is_gram and
// start_time are stamped by CaptureEvent; organization_id and organization_slug
// are passed by hand because that auto-population reads the auth context and the
// callback has none.
//
// created_via is the flow tag. Not "source" (already means the OpenAPI spec's
// origin on spec_uploaded) and not "disposition" (the assistants flow token,
// which is also a URL param and a callback branch — reusing it would imply
// ?disposition=signup is a supported entry point, which it is not).
//
// Telemetry failures are logged, never returned: the org exists and the user is
// authenticated, so a dropped analytics event must not fail the request.
func (s *Service) captureSignupTelemetry(ctx context.Context, email, orgName string, org orgRepo.OrganizationMetadatum) {
	if err := s.posthog.CaptureEvent(ctx, "onboarding_event", email, map[string]any{
		"action":            "new_org_created",
		"created_via":       "signup",
		"company_name":      orgName,
		"organization_id":   org.ID,
		"organization_slug": org.Slug,
	}); err != nil {
		s.logger.ErrorContext(ctx, "failed to capture signup onboarding_event", attr.SlogError(err), attr.SlogOrganizationID(org.ID))
	}

	// Person property, so the cohort is durable rather than only queryable at
	// the moment of the event. This is what PostHog cohorts, flags, and surveys
	// target.
	if err := s.posthog.IdentifyUser(ctx, email, map[string]any{
		"created_via": "signup",
	}); err != nil {
		s.logger.ErrorContext(ctx, "failed to set signup created_via person property", attr.SlogError(err), attr.SlogOrganizationID(org.ID))
	}
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
		return nil, oops.E(oops.CodeUnexpected, err, "error listing projects").LogError(ctx, s.logger)
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
			return nil, oops.E(oops.CodeUnexpected, err, "error creating default environment").LogError(ctx, s.logger)
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
		return empty, oops.E(oops.CodeUnexpected, err, "error creating default project").LogError(ctx, s.logger)
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

// signupIntent is what the user supplied on the sign-up page before
// authenticating. It lives only for the duration of the identity-provider round
// trip: if the user abandons the flow it expires with the nonce and nothing was
// created.
//
// Deliberately growable. Adding fields later (campaign attribution, plan) is
// additive — this is short-lived, so there is no migration and no back-compat
// window.
//
// No serialization tag, deliberately. The cache marshals with msgpack, which
// keys by the Go field name and ignores `json` tags entirely, so a
// `json:"org_name"` tag here would be inert and would misdescribe the wire
// format: the stored key is "OrgName". Producer and consumer both use this
// struct, so the round trip is symmetric either way.
type signupIntent struct {
	OrgName string
}

// signupIntentKey namespaces the intent separately from the nonce binding.
// Folding it into the binding value would leave a nonceTTL-long window after
// deploy where in-flight nonces are the old shape and fail to decode.
func signupIntentKey(nonce string) string {
	return "auth:signup_intent:" + nonce
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

	return returnAddress + "/rpc/auth.callback"
}

// callbackRedirectURL determines the redirect location after authentication. It
// only allows relative URLs to prevent open redirect attacks (see relativeURL).
// If no redirect is found, fall back to SignInRedirectURL.
func (s *Service) callbackRedirectURL(
	ctx context.Context,
	payload *gen.CallbackPayload,
) string {
	var location string

	if state := decodeStateParam(payload); state != nil {
		location = relativeURL(state.FinalDestinationURL)
	}

	if location != "" {
		msg := fmt.Sprintf("Found destination URL in state: '%s'", location)
		s.logger.InfoContext(ctx, msg)

		return location
	}

	return s.cfg.SignInRedirectURL
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
