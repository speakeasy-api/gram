// Package mockidp implements a Speakeasy identity provider bridge for local
// development and testing. It provides the /v1/speakeasy_provider/* endpoints
// that the Gram server calls during authentication.
//
// Two modes are supported:
//   - OIDC mode (local dev): authenticates against a real OIDC provider (e.g.
//     WorkOS AuthKit dev environment). Enabled when OIDC_ISSUER, OIDC_CLIENT_ID,
//     and OIDC_CLIENT_SECRET are set.
//   - Mock mode (tests): auto-logs in a hardcoded test user with no external
//     dependencies. Used by NewConfig().
package mockidp

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"net/url"
	"os"
	"regexp"
	"strings"
	"sync"
	"time"
)

// Constants exported for use in tests.
const (
	MockSecretKey = "test-secret"
	MockUserID    = "test-user-1"
	MockUserEmail = "dev@example.com"
	MockOrgID     = "550e8400-e29b-41d4-a716-446655440000"
	MockOrgName   = "Local Dev Org"
	MockOrgSlug   = "local-dev-org"
)

// Config holds the IDP configuration.
type Config struct {
	SecretKey    string
	User         UserConfig
	Organization OrgConfig
	Oidc         OidcConfig
}

// UserConfig holds the mock user configuration (mock mode only).
type UserConfig struct {
	ID           string
	Email        string
	DisplayName  string
	PhotoURL     *string
	GithubHandle *string
	Admin        bool
	Whitelisted  bool
}

// OrgConfig holds the mock organization configuration (mock mode only).
type OrgConfig struct {
	ID          string
	Name        string
	Slug        string
	AccountType string
	// WorkOSID is the WorkOS organization id included in /validate (json workos_id).
	// Nil means omit the field so Gram's syncWorkOSIDs skips SetOrgWorkosID for that org.
	WorkOSID *string
}

// NewConfig returns a Config with hardcoded test defaults (no env var lookup).
// Use this in tests for deterministic behavior. Always runs in mock mode.
func NewConfig() Config {
	mockOrgWorkOSID := MockOrgID
	return Config{
		SecretKey: MockSecretKey,
		User: UserConfig{
			ID:          MockUserID,
			Email:       MockUserEmail,
			DisplayName: "Dev User",
			Admin:       true,
			Whitelisted: true,
		},
		Organization: OrgConfig{
			ID:          MockOrgID,
			Name:        MockOrgName,
			Slug:        MockOrgSlug,
			AccountType: "free",
			WorkOSID:    &mockOrgWorkOSID,
		},
	}
}

// DefaultConfig returns a Config populated from environment variables.
// When OIDC env vars are set, OIDC mode is enabled and the mock user/org
// config is ignored (real identity comes from the OIDC provider).
func DefaultConfig() Config {
	orgID := envStr("MOCK_IDP_ORG_ID", "550e8400-e29b-41d4-a716-446655440000")
	cfg := Config{
		SecretKey: envStr("SPEAKEASY_SECRET_KEY", MockSecretKey),
		User: UserConfig{
			ID:          envStr("MOCK_IDP_USER_ID", "dev-user-1"),
			Email:       envStr("MOCK_IDP_USER_EMAIL", "dev@example.com"),
			DisplayName: envStr("MOCK_IDP_USER_DISPLAY_NAME", "Dev User"),
			Admin:       envBool("MOCK_IDP_USER_ADMIN", true),
			Whitelisted: envBool("MOCK_IDP_USER_WHITELISTED", true),
		},
		Organization: OrgConfig{
			ID:          orgID,
			Name:        envStr("MOCK_IDP_ORG_NAME", "Local Dev Org"),
			Slug:        envStr("MOCK_IDP_ORG_SLUG", "local-dev-org"),
			AccountType: envStr("MOCK_IDP_ORG_ACCOUNT_TYPE", "free"),
			WorkOSID:    &orgID,
		},
		Oidc: OidcConfig{
			Issuer:       envStrNoUnset("OIDC_ISSUER"),
			ClientID:     envStrNoUnset("OIDC_CLIENT_ID"),
			ClientSecret: envStrNoUnset("OIDC_CLIENT_SECRET"),
			ExternalURL:  envStr("OIDC_EXTERNAL_URL", ""),
			GramSiteURL:  envStr("GRAM_SITE_URL", "https://localhost:5173"),
		},
	}
	if v := os.Getenv("MOCK_IDP_USER_PHOTO_URL"); v != "" {
		cfg.User.PhotoURL = &v
	}
	if v := os.Getenv("MOCK_IDP_USER_GITHUB_HANDLE"); v != "" {
		cfg.User.GithubHandle = &v
	}
	return cfg
}

// Handler returns an http.Handler implementing all /v1/speakeasy_provider/*
// endpoints. In OIDC mode, login redirects to the real OIDC provider and an
// additional /oidc/callback endpoint is registered.
func Handler(cfg Config) http.Handler {
	s := &server{
		cfg:                cfg,
		authCodes:          make(map[string]authCodeEntry),
		tokens:             make(map[string]tokenEntry),
		userAdditionalOrgs: make(map[string][]organization),
		oidcPendingLogins:  make(map[string]pendingOidcLogin),
		oidcAuthCodes:      make(map[string]*oidcSession),
		oidcTokens:         make(map[string]*oidcSession),
		pendingLogouts:     make(map[string]pendingLogout),
	}
	mux := http.NewServeMux()

	// Browser-facing endpoints (no secret key)
	mux.HandleFunc("GET /v1/speakeasy_provider/login", s.handleLogin)
	if cfg.Oidc.IsOidcMode() {
		mux.HandleFunc("GET /v1/speakeasy_provider/oidc/callback", s.handleOidcCallback)
		mux.HandleFunc("GET /v1/speakeasy_provider/logout/callback", s.handleLogoutCallback)
		mux.HandleFunc("GET /v1/auth/callback", s.handleInviteAuthCallback)
	}

	// Server-to-server endpoints (require secret key)
	mux.HandleFunc("POST /v1/speakeasy_provider/exchange", s.withAuth(s.handleExchange))
	mux.HandleFunc("GET /v1/speakeasy_provider/validate", s.withAuth(s.handleValidate))
	mux.HandleFunc("POST /v1/speakeasy_provider/revoke", s.withAuth(s.handleRevoke))
	mux.HandleFunc("POST /v1/speakeasy_provider/register", s.withAuth(s.handleRegister))
	return mux
}

// oidcSession wraps a user session with the WorkOS session ID for logout.
type oidcSession struct {
	*userSession
	workosSessionID string
}

// --- JSON types ---

type user struct {
	ID           string  `json:"id"`
	Email        string  `json:"email"`
	DisplayName  string  `json:"display_name"`
	PhotoURL     *string `json:"photo_url"`
	GithubHandle *string `json:"github_handle"`
	Admin        bool    `json:"admin"`
	CreatedAt    string  `json:"created_at"`
	UpdatedAt    string  `json:"updated_at"`
	Whitelisted  bool    `json:"whitelisted"`
}

type organization struct {
	ID                 string   `json:"id"`
	Name               string   `json:"name"`
	Slug               string   `json:"slug"`
	CreatedAt          string   `json:"created_at"`
	UpdatedAt          string   `json:"updated_at"`
	AccountType        string   `json:"account_type"`
	SSOConnectionID    *string  `json:"sso_connection_id"`
	WorkOSID           *string  `json:"workos_id,omitempty"`
	UserWorkspaceSlugs []string `json:"user_workspaces_slugs"`
}

type validateResponse struct {
	User          user           `json:"user"`
	Organizations []organization `json:"organizations"`
}

// --- internal server ---

type authCodeEntry struct {
	userID    string
	createdAt time.Time
}

type tokenEntry struct {
	userID    string
	createdAt time.Time
}

type pendingOidcLogin struct {
	returnURL    string
	gramState    string
	codeVerifier string
}

// pendingLogout holds the original login parameters while the user is
// redirected through WorkOS logout to clear their AuthKit session cookie.
// Because WorkOS strips query parameters from the return_to URL, we store
// the parameters server-side keyed by a short-lived token.
type pendingLogout struct {
	returnURL string
	state     string
}

type server struct {
	cfg Config

	mu sync.Mutex

	// Mock mode stores
	authCodes          map[string]authCodeEntry
	tokens             map[string]tokenEntry
	userAdditionalOrgs map[string][]organization

	// OIDC mode stores
	oidcPendingLogins   map[string]pendingOidcLogin
	oidcAuthCodes       map[string]*oidcSession
	oidcTokens          map[string]*oidcSession
	lastWorkosSessionID string // stored on revoke, used on next login to clear AuthKit session
	pendingLogouts      map[string]pendingLogout
}

func (s *server) isOidc() bool {
	return s.cfg.Oidc.IsOidcMode()
}

func (s *server) mode() string {
	if s.isOidc() {
		return "oidc"
	}
	return "mock"
}

// withAuth wraps a handler with the secret-key auth middleware.
func (s *server) withAuth(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		key := r.Header.Get("speakeasy-auth-provider-key")
		if key != s.cfg.SecretKey {
			log.Printf("[auth] REJECTED: invalid provider key for %s (got %s)", r.URL.Path, key)
			writeJSON(w, http.StatusUnauthorized, map[string]string{
				"error": "Unauthorized: invalid or missing provider key",
			})
			return
		}
		next(w, r)
	}
}

// ---------------------------------------------------------------------------
// Login
// ---------------------------------------------------------------------------

func (s *server) handleLogin(w http.ResponseWriter, r *http.Request) {
	returnURL := r.URL.Query().Get("return_url")
	state := r.URL.Query().Get("state")

	log.Printf("[login] [%s] return_url=%s state=%s", s.mode(), returnURL, state)

	if returnURL == "" {
		log.Printf("[login] [%s] REJECTED: missing return_url", s.mode())
		writeJSON(w, http.StatusBadRequest, map[string]string{
			"error": "Missing return_url parameter",
		})
		return
	}

	if !s.isOidc() {
		// Mock mode: auto-login with configured test user
		code := s.generateAuthCode(s.cfg.User.ID)
		target, err := url.Parse(returnURL)
		if err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "Invalid return_url"})
			return
		}
		q := target.Query()
		q.Set("code", code)
		if state != "" {
			q.Set("state", state)
		}
		target.RawQuery = q.Encode()
		log.Printf("[login] [mock] auto-login → redirecting to %s", target.Host)
		http.Redirect(w, r, target.String(), http.StatusFound)
		return
	}

	// OIDC mode: if there's a previous WorkOS session to clear, redirect
	// through WorkOS logout first to clear the AuthKit cookie in the browser.
	// This ensures the user gets a fresh login screen after signing out.
	s.mu.Lock()
	prevSessionID := s.lastWorkosSessionID
	s.lastWorkosSessionID = ""
	s.mu.Unlock()

	if prevSessionID != "" {
		// Redirect through WorkOS session logout to clear the AuthKit cookie.
		// WorkOS strips query parameters from the return_to URL, so we store
		// the original login params server-side and retrieve them when we
		// come back via the /logout/callback endpoint.
		s.mu.Lock()
		s.pendingLogouts["latest"] = pendingLogout{
			returnURL: returnURL,
			state:     state,
		}
		s.mu.Unlock()

		continueURL := s.cfg.Oidc.ExternalURL + "/v1/speakeasy_provider/logout/callback"
		logoutURL := fmt.Sprintf("https://api.workos.com/user_management/sessions/logout?session_id=%s&return_to=%s",
			prevSessionID,
			url.QueryEscape(continueURL),
		)
		log.Printf("[login] [oidc] clearing previous WorkOS session %s", prevSessionID)
		http.Redirect(w, r, logoutURL, http.StatusFound)
		return
	}

	// Proceed to WorkOS authorize.
	oidcState := randomID()
	codeVerifier := generateCodeVerifier()

	s.mu.Lock()
	s.oidcPendingLogins[oidcState] = pendingOidcLogin{
		returnURL:    returnURL,
		gramState:    state,
		codeVerifier: codeVerifier,
	}
	s.mu.Unlock()

	authorizeURL, err := buildAuthorizeURL(r.Context(), s.cfg.Oidc, oidcState, codeVerifier)
	if err != nil {
		log.Printf("[login] [oidc] failed to build authorize URL: %v", err)
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "Failed to build OIDC authorize URL"})
		return
	}

	log.Printf("[login] [oidc] redirecting to OIDC provider: %s", authorizeURL)
	http.Redirect(w, r, authorizeURL, http.StatusFound)
}

// ---------------------------------------------------------------------------
// Logout Callback (browser-facing, no secret key)
// ---------------------------------------------------------------------------

// handleLogoutCallback is called by WorkOS after clearing the AuthKit session.
// It restores the original login parameters and redirects to handleLogin to
// continue the OIDC authorize flow with a fresh session.
func (s *server) handleLogoutCallback(w http.ResponseWriter, r *http.Request) {
	s.mu.Lock()
	pending, ok := s.pendingLogouts["latest"]
	delete(s.pendingLogouts, "latest")
	s.mu.Unlock()

	if !ok || pending.returnURL == "" {
		log.Printf("[logout/callback] no pending logout found → 400")
		writeJSON(w, http.StatusBadRequest, map[string]string{
			"error": "No pending logout session",
		})
		return
	}

	// Redirect back to the login endpoint with the original parameters.
	loginURL := fmt.Sprintf("%s/v1/speakeasy_provider/login?return_url=%s&state=%s",
		s.cfg.Oidc.ExternalURL,
		url.QueryEscape(pending.returnURL),
		url.QueryEscape(pending.state),
	)
	log.Printf("[logout/callback] WorkOS session cleared, continuing login flow")
	http.Redirect(w, r, loginURL, http.StatusFound)
}

// ---------------------------------------------------------------------------
// OIDC Callback (browser-facing, no secret key)
// ---------------------------------------------------------------------------

func (s *server) handleOidcCallback(w http.ResponseWriter, r *http.Request) {
	code := r.URL.Query().Get("code")
	oidcState := r.URL.Query().Get("state")
	oidcError := r.URL.Query().Get("error")

	if oidcError != "" {
		desc := r.URL.Query().Get("error_description")
		if desc == "" {
			desc = oidcError
		}
		log.Printf("[oidc/callback] provider returned error: %s", desc)
		http.Error(w, "OIDC error: "+desc, http.StatusBadRequest)
		return
	}

	if code == "" || oidcState == "" {
		log.Printf("[oidc/callback] missing code or state → 400")
		http.Error(w, "missing code or state", http.StatusBadRequest)
		return
	}

	// Look up the pending login
	s.mu.Lock()
	pending, ok := s.oidcPendingLogins[oidcState]
	if ok {
		delete(s.oidcPendingLogins, oidcState)
	}
	s.mu.Unlock()

	if !ok {
		log.Printf("[oidc/callback] unknown/expired state: %s → 400", oidcState)
		http.Error(w, "unknown or expired OIDC state", http.StatusBadRequest)
		return
	}

	// Exchange code with WorkOS
	log.Printf("[oidc/callback] exchanging code with WorkOS...")
	authResp, err := exchangeOidcCode(r.Context(), s.cfg.Oidc, code, pending.codeVerifier)
	if err != nil {
		log.Printf("[oidc/callback] token exchange failed: %v", err)
		http.Error(w, "OIDC token exchange failed", http.StatusInternalServerError)
		return
	}

	// Build claims from the WorkOS authenticate response.
	// WorkOS returns user data directly, so no separate userinfo call needed.
	displayName := strings.TrimSpace(authResp.User.FirstName + " " + authResp.User.LastName)
	if displayName == "" {
		displayName = authResp.User.Email
	}
	claims := &OidcClaims{
		Sub:     authResp.User.ID,
		Email:   authResp.User.Email,
		Name:    displayName,
		Picture: authResp.User.ProfilePictureURL,
		OrgID:   authResp.OrganizationID,
	}
	// If we have an org ID, fetch the org name via the API key (client secret)
	if authResp.OrganizationID != "" {
		if orgName, err := fetchWorkOSOrgName(r.Context(), s.cfg.Oidc.ClientSecret, authResp.OrganizationID); err == nil {
			claims.OrgName = orgName
		} else {
			log.Printf("[oidc/callback] failed to fetch org name: %v (using org ID as name)", err)
			claims.OrgName = authResp.OrganizationID
		}
	}
	log.Printf("[oidc/callback] authenticated sub=%s email=%s org=%s", claims.Sub, claims.Email, claims.OrgID)

	userSess := mapClaimsToSession(claims)
	userSess.User.Admin = s.cfg.User.Admin

	// Extract the WorkOS session ID from the access token (JWT) for logout.
	var workosSessionID string
	if sid, err := extractJWTClaim(authResp.AccessToken, "sid"); err == nil {
		workosSessionID = sid
		log.Printf("[oidc/callback] extracted WorkOS session_id=%s", workosSessionID)
	}

	sess := &oidcSession{
		userSession:     userSess,
		workosSessionID: workosSessionID,
	}

	orgNames := make([]string, 0, len(sess.Organizations))
	for _, o := range sess.Organizations {
		orgNames = append(orgNames, o.Name)
	}
	log.Printf("[oidc/callback] authenticated %s orgs=[%s] → redirecting to Gram",
		sess.User.Email, strings.Join(orgNames, ", "))

	// Create a local auth code that maps to this session
	ourCode := s.createOidcAuthCode(sess)

	target, err := url.Parse(pending.returnURL)
	if err != nil {
		http.Error(w, "invalid return URL", http.StatusInternalServerError)
		return
	}
	q := target.Query()
	q.Set("code", ourCode)
	if pending.gramState != "" {
		q.Set("state", pending.gramState)
	}
	target.RawQuery = q.Encode()

	http.Redirect(w, r, target.String(), http.StatusFound)
}

// ---------------------------------------------------------------------------
// Invite Auth Callback (WorkOS invite acceptance redirect)
// ---------------------------------------------------------------------------

// handleInviteAuthCallback handles the redirect from WorkOS after a user
// accepts an organization invitation. WorkOS redirects to /v1/auth/callback
// with a one-time authorization code. We exchange it, create a local session,
// and redirect to the Gram server's auth callback to complete login.
func (s *server) handleInviteAuthCallback(w http.ResponseWriter, r *http.Request) {
	code := r.URL.Query().Get("code")
	if code == "" {
		log.Printf("[invite/callback] missing code parameter")
		http.Error(w, "missing code parameter", http.StatusBadRequest)
		return
	}

	log.Printf("[invite/callback] received WorkOS invite callback, exchanging code...")

	// Exchange the code with WorkOS. No PKCE verifier because this flow
	// was initiated by WorkOS (invite acceptance), not by the mock IDP.
	authResp, err := exchangeOidcCode(r.Context(), s.cfg.Oidc, code, "")
	if err != nil {
		log.Printf("[invite/callback] code exchange failed: %v — redirecting to app for manual login", err)
		http.Redirect(w, r, s.cfg.Oidc.GramSiteURL, http.StatusFound)
		return
	}

	// Build claims from WorkOS response (same as OIDC callback).
	displayName := strings.TrimSpace(authResp.User.FirstName + " " + authResp.User.LastName)
	if displayName == "" {
		displayName = authResp.User.Email
	}
	claims := &OidcClaims{
		Sub:     authResp.User.ID,
		Email:   authResp.User.Email,
		Name:    displayName,
		Picture: authResp.User.ProfilePictureURL,
		OrgID:   authResp.OrganizationID,
	}
	if authResp.OrganizationID != "" {
		if orgName, err := fetchWorkOSOrgName(r.Context(), s.cfg.Oidc.ClientSecret, authResp.OrganizationID); err == nil {
			claims.OrgName = orgName
		} else {
			log.Printf("[invite/callback] failed to fetch org name: %v (using org ID)", err)
			claims.OrgName = authResp.OrganizationID
		}
	}
	log.Printf("[invite/callback] authenticated sub=%s email=%s org=%s", claims.Sub, claims.Email, claims.OrgID)

	userSess := mapClaimsToSession(claims)
	userSess.User.Admin = s.cfg.User.Admin
	var workosSessionID string
	if sid, err := extractJWTClaim(authResp.AccessToken, "sid"); err == nil {
		workosSessionID = sid
		log.Printf("[invite/callback] extracted WorkOS session_id=%s", workosSessionID)
	}

	sess := &oidcSession{
		userSession:     userSess,
		workosSessionID: workosSessionID,
	}

	ourCode := s.createOidcAuthCode(sess)

	// Redirect to the Gram server's auth callback to complete the login.
	target, err := url.Parse(strings.TrimRight(s.cfg.Oidc.GramSiteURL, "/") + "/rpc/auth.callback")
	if err != nil {
		log.Printf("[invite/callback] invalid GramSiteURL: %v", err)
		http.Error(w, "invalid GRAM_SITE_URL configuration", http.StatusInternalServerError)
		return
	}
	q := target.Query()
	q.Set("code", ourCode)
	target.RawQuery = q.Encode()

	log.Printf("[invite/callback] redirecting to Gram auth callback → %s", target.String())
	http.Redirect(w, r, target.String(), http.StatusFound)
}

// ---------------------------------------------------------------------------
// Exchange
// ---------------------------------------------------------------------------

func (s *server) handleExchange(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Code string `json:"code"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "Invalid request body"})
		return
	}
	log.Printf("[exchange] [%s] code=%s", s.mode(), body.Code)

	if body.Code == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "Missing code in request body"})
		return
	}

	if !s.isOidc() {
		userID := s.validateAuthCode(body.Code)
		if userID == "" {
			// Accept unknown codes gracefully — tests call exchange directly.
			userID = s.cfg.User.ID
		}
		token := s.generateToken(userID)
		log.Printf("[exchange] [mock] issued token for userId=%s", userID)
		writeJSON(w, http.StatusOK, map[string]string{"id_token": token})
		return
	}

	// OIDC mode
	session := s.consumeOidcAuthCode(body.Code)
	if session == nil {
		log.Printf("[exchange] [oidc] invalid/expired code → 400")
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid or expired code"})
		return
	}

	token := s.createOidcToken(session)
	log.Printf("[exchange] [oidc] issued token for %s", session.User.Email)
	writeJSON(w, http.StatusOK, map[string]string{"id_token": token})
}

// ---------------------------------------------------------------------------
// Validate
// ---------------------------------------------------------------------------

func (s *server) handleValidate(w http.ResponseWriter, r *http.Request) {
	idToken := r.Header.Get("speakeasy-auth-provider-id-token")
	log.Printf("[validate] [%s] token=%s", s.mode(), idToken)

	if idToken == "" {
		writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "Missing token header"})
		return
	}

	if !s.isOidc() {
		userID := s.validateToken(idToken)
		if userID == "" {
			writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "Invalid or expired token"})
			return
		}
		u := s.buildUser()
		orgs := s.allOrgsForUser(userID)
		log.Printf("[validate] [mock] → %s orgs=%d", u.Email, len(orgs))
		writeJSON(w, http.StatusOK, validateResponse{User: u, Organizations: orgs})
		return
	}

	// OIDC mode
	session := s.getOidcSession(idToken)
	if session == nil {
		writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "Invalid or expired token"})
		return
	}

	// Snapshot orgs under the lock to avoid a race with handleRegister writes.
	s.mu.Lock()
	orgs := make([]organization, len(session.Organizations))
	copy(orgs, session.Organizations)
	s.mu.Unlock()

	orgNames := make([]string, 0, len(orgs))
	for _, o := range orgs {
		orgNames = append(orgNames, o.Name)
	}
	log.Printf("[validate] [oidc] → %s orgs=[%s]", session.User.Email, strings.Join(orgNames, ", "))
	writeJSON(w, http.StatusOK, validateResponse{
		User:          session.User,
		Organizations: orgs,
	})
}

// ---------------------------------------------------------------------------
// Revoke
// ---------------------------------------------------------------------------

func (s *server) handleRevoke(w http.ResponseWriter, r *http.Request) {
	idToken := r.Header.Get("speakeasy-auth-provider-id-token")
	log.Printf("[revoke] [%s] token=%s", s.mode(), idToken)

	if idToken != "" {
		if s.isOidc() {
			s.revokeOidcToken(idToken)
		} else {
			s.revokeToken(idToken)
		}
	}

	log.Printf("[revoke] [%s] OK", s.mode())
	writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
}

// ---------------------------------------------------------------------------
// Register
// ---------------------------------------------------------------------------

func (s *server) handleRegister(w http.ResponseWriter, r *http.Request) {
	idToken := r.Header.Get("speakeasy-auth-provider-id-token")
	log.Printf("[register] [%s] token=%s", s.mode(), idToken)

	if idToken == "" {
		writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "Missing token header"})
		return
	}

	var body struct {
		OrganizationName string `json:"organization_name"`
		AccountType      string `json:"account_type"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "Invalid request body"})
		return
	}

	if body.OrganizationName == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "Missing organization_name"})
		return
	}

	now := time.Now().UTC().Format(time.RFC3339)
	accountType := body.AccountType
	if accountType == "" {
		accountType = "free"
	}
	slug := slugify(body.OrganizationName)

	newOrg := organization{
		ID:                 randomID(),
		Name:               body.OrganizationName,
		Slug:               slug,
		CreatedAt:          now,
		UpdatedAt:          now,
		AccountType:        accountType,
		SSOConnectionID:    nil,
		WorkOSID:           nil,
		UserWorkspaceSlugs: []string{slug},
	}

	if !s.isOidc() {
		userID := s.validateToken(idToken)
		if userID == "" {
			writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "Invalid or expired token"})
			return
		}

		s.mu.Lock()
		s.userAdditionalOrgs[userID] = append(s.userAdditionalOrgs[userID], newOrg)
		s.mu.Unlock()

		u := s.buildUser()
		orgs := s.allOrgsForUser(userID)
		log.Printf("[register] [mock] created org %q for %s", body.OrganizationName, u.Email)
		writeJSON(w, http.StatusOK, validateResponse{User: u, Organizations: orgs})
		return
	}

	// OIDC mode
	session := s.getOidcSession(idToken)
	if session == nil {
		writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "Invalid or expired token"})
		return
	}

	// Persist the new org to the session so subsequent /validate calls include it.
	// Capture the slice under the lock to avoid a race with concurrent /validate reads.
	s.mu.Lock()
	session.Organizations = append(session.Organizations, newOrg)
	orgs := session.Organizations
	s.mu.Unlock()

	log.Printf("[register] [oidc] created org %q for %s", body.OrganizationName, session.User.Email)
	writeJSON(w, http.StatusOK, validateResponse{
		User:          session.User,
		Organizations: orgs,
	})
}

// ---------------------------------------------------------------------------
// Mock mode store helpers
// ---------------------------------------------------------------------------

func (s *server) generateAuthCode(userID string) string {
	code := randomID()
	s.mu.Lock()
	s.authCodes[code] = authCodeEntry{userID: userID, createdAt: time.Now()}
	s.mu.Unlock()
	return code
}

func (s *server) validateAuthCode(code string) string {
	s.mu.Lock()
	defer s.mu.Unlock()
	entry, ok := s.authCodes[code]
	if !ok {
		return ""
	}
	delete(s.authCodes, code)
	return entry.userID
}

func (s *server) generateToken(userID string) string {
	token := randomID()
	s.mu.Lock()
	s.tokens[token] = tokenEntry{userID: userID, createdAt: time.Now()}
	s.mu.Unlock()
	return token
}

func (s *server) validateToken(token string) string {
	s.mu.Lock()
	defer s.mu.Unlock()
	entry, ok := s.tokens[token]
	if !ok {
		return ""
	}
	return entry.userID
}

func (s *server) revokeToken(token string) {
	s.mu.Lock()
	delete(s.tokens, token)
	s.mu.Unlock()
}

// ---------------------------------------------------------------------------
// OIDC mode store helpers
// ---------------------------------------------------------------------------

func (s *server) createOidcAuthCode(session *oidcSession) string {
	code := randomID()
	s.mu.Lock()
	s.oidcAuthCodes[code] = session
	s.mu.Unlock()
	return code
}

func (s *server) consumeOidcAuthCode(code string) *oidcSession {
	s.mu.Lock()
	defer s.mu.Unlock()
	session, ok := s.oidcAuthCodes[code]
	if !ok {
		return nil
	}
	delete(s.oidcAuthCodes, code)
	return session
}

func (s *server) createOidcToken(session *oidcSession) string {
	token := randomID()
	s.mu.Lock()
	s.oidcTokens[token] = session
	s.mu.Unlock()
	return token
}

func (s *server) getOidcSession(token string) *oidcSession {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.oidcTokens[token]
}

func (s *server) revokeOidcToken(token string) {
	s.mu.Lock()
	session, ok := s.oidcTokens[token]
	delete(s.oidcTokens, token)
	// Store the session ID so the next login can redirect through WorkOS
	// logout to clear the AuthKit session cookie in the browser.
	if ok && session.workosSessionID != "" {
		s.lastWorkosSessionID = session.workosSessionID
		log.Printf("[revoke] [oidc] stored WorkOS session_id=%s for logout on next login", session.workosSessionID)
	}
	s.mu.Unlock()
}

// ---------------------------------------------------------------------------
// Mock mode data helpers
// ---------------------------------------------------------------------------

func (s *server) buildUser() user {
	const fixedTime = "2024-01-01T00:00:00Z"
	return user{
		ID:           s.cfg.User.ID,
		Email:        s.cfg.User.Email,
		DisplayName:  s.cfg.User.DisplayName,
		PhotoURL:     s.cfg.User.PhotoURL,
		GithubHandle: s.cfg.User.GithubHandle,
		Admin:        s.cfg.User.Admin,
		CreatedAt:    fixedTime,
		UpdatedAt:    fixedTime,
		Whitelisted:  s.cfg.User.Whitelisted,
	}
}

func (s *server) buildOrg() organization {
	const fixedTime = "2024-01-01T00:00:00Z"
	return organization{
		ID:                 s.cfg.Organization.ID,
		Name:               s.cfg.Organization.Name,
		Slug:               s.cfg.Organization.Slug,
		CreatedAt:          fixedTime,
		UpdatedAt:          fixedTime,
		AccountType:        s.cfg.Organization.AccountType,
		SSOConnectionID:    nil,
		WorkOSID:           s.cfg.Organization.WorkOSID,
		UserWorkspaceSlugs: []string{s.cfg.Organization.Slug},
	}
}

func (s *server) allOrgsForUser(userID string) []organization {
	base := []organization{s.buildOrg()}
	s.mu.Lock()
	additional := s.userAdditionalOrgs[userID]
	s.mu.Unlock()
	return append(base, additional...)
}

// ---------------------------------------------------------------------------
// Utility functions
// ---------------------------------------------------------------------------

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(v) //nolint:errcheck
}

func randomID() string {
	b := make([]byte, 16)
	_, _ = rand.Read(b)
	return hex.EncodeToString(b)
}

var nonAlphanumeric = regexp.MustCompile(`[^a-z0-9]+`)
var leadingTrailingDash = regexp.MustCompile(`^-+|-+$`)

func slugify(name string) string {
	s := strings.ToLower(name)
	s = nonAlphanumeric.ReplaceAllString(s, "-")
	s = leadingTrailingDash.ReplaceAllString(s, "")
	return s
}

func envStr(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}

// envStrNoUnset returns the env var value, treating "unset" as empty.
func envStrNoUnset(key string) string {
	v := os.Getenv(key)
	if v == "unset" {
		return ""
	}
	return v
}

func envBool(key string, fallback bool) bool {
	v := os.Getenv(key)
	if v == "" {
		return fallback
	}
	return v == "true" || v == "1"
}

// LogConfig prints the server configuration to stdout.
func LogConfig(cfg Config, port int) {
	if cfg.Oidc.IsOidcMode() {
		fmt.Printf("Speakeasy IDP Bridge (OIDC mode) running on port %d\n", port)
		fmt.Printf("OIDC Issuer: %s\n", cfg.Oidc.Issuer)
		fmt.Printf("Client ID:   %s\n", cfg.Oidc.ClientID)
	} else {
		fmt.Printf("Speakeasy IDP Bridge (mock mode) running on port %d\n", port)
		fmt.Printf("User: %s <%s> (admin=%v)\n", cfg.User.DisplayName, cfg.User.Email, cfg.User.Admin)
		fmt.Printf("Org:  %s (%s)\n", cfg.Organization.Name, cfg.Organization.Slug)
	}
	fmt.Println("Endpoints:")
	fmt.Println("  GET  /v1/speakeasy_provider/login")
	if cfg.Oidc.IsOidcMode() {
		fmt.Println("  GET  /v1/speakeasy_provider/oidc/callback")
		fmt.Println("  GET  /v1/speakeasy_provider/logout/callback")
		fmt.Println("  GET  /v1/auth/callback  (WorkOS invite acceptance)")
	}
	fmt.Println("  POST /v1/speakeasy_provider/exchange")
	fmt.Println("  GET  /v1/speakeasy_provider/validate")
	fmt.Println("  POST /v1/speakeasy_provider/revoke")
	fmt.Println("  POST /v1/speakeasy_provider/register")
}
