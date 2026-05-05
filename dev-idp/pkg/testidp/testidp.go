// Package testidp serves an in-process Speakeasy IDP for tests. It's a
// stripped-down successor to the standalone mock-speakeasy-idp binary's
// mock-mode behavior -- drops the OIDC bridge entirely (tests never
// exercised it) and keeps just the /v1/speakeasy_provider/{login,
// exchange,validate,revoke,register} surface against in-memory state.
//
// The dev-idp's `localspeakeasy` mode (dev-idp/internal/modes/localspeakeasy)
// owns the same wire shape against a real SQLite store and is what runs
// in `madprocs` / dev workflows. This package is the public, in-process
// entry point for server-side tests that don't want a process boundary.
package testidp

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"sync"
	"time"
)

// Constants exported for tests that need to assert against the
// canonical fixture identity.
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
}

// UserConfig holds the mock user configuration.
type UserConfig struct {
	ID           string
	Email        string
	DisplayName  string
	PhotoURL     *string
	GithubHandle *string
	Admin        bool
	Whitelisted  bool
}

// OrgConfig holds the mock organization configuration.
type OrgConfig struct {
	ID          string
	Name        string
	Slug        string
	AccountType string
	// WorkOSID is the WorkOS organization id included in /validate (json
	// `workos_id`). Nil omits the field so Gram's syncWorkOSIDs skips
	// SetOrgWorkosID for that org.
	WorkOSID *string
}

// NewConfig returns a Config with the canonical fixture defaults.
func NewConfig() Config {
	wosOrgID := MockOrgID
	return Config{
		SecretKey: MockSecretKey,
		User: UserConfig{
			ID:           MockUserID,
			Email:        MockUserEmail,
			DisplayName:  "Dev User",
			PhotoURL:     nil,
			GithubHandle: nil,
			Admin:        true,
			Whitelisted:  true,
		},
		Organization: OrgConfig{
			ID:          MockOrgID,
			Name:        MockOrgName,
			Slug:        MockOrgSlug,
			AccountType: "free",
			WorkOSID:    &wosOrgID,
		},
	}
}

// Handler returns an http.Handler implementing all mock-mode
// /v1/speakeasy_provider/* endpoints against the given config.
func Handler(cfg Config) http.Handler {
	s := &server{
		cfg:                cfg,
		mu:                 sync.Mutex{},
		authCodes:          make(map[string]authCodeEntry),
		tokens:             make(map[string]tokenEntry),
		userAdditionalOrgs: make(map[string][]organization),
	}
	mux := http.NewServeMux()
	mux.HandleFunc("GET /v1/speakeasy_provider/login", s.handleLogin)
	mux.HandleFunc("POST /v1/speakeasy_provider/exchange", s.withAuth(s.handleExchange))
	mux.HandleFunc("GET /v1/speakeasy_provider/validate", s.withAuth(s.handleValidate))
	mux.HandleFunc("POST /v1/speakeasy_provider/revoke", s.withAuth(s.handleRevoke))
	mux.HandleFunc("POST /v1/speakeasy_provider/register", s.withAuth(s.handleRegister))
	return mux
}

// =============================================================================
// JSON wire types — match the production Speakeasy IDP shape exactly.
// =============================================================================

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
	UserWorkspaceSlugs []string `json:"user_workspaces_slugs"` // typo preserved — wire compat with Gram-side
}

type validateResponse struct {
	User          user           `json:"user"`
	Organizations []organization `json:"organizations"`
}

// =============================================================================
// Server state
// =============================================================================

type authCodeEntry struct {
	userID    string
	createdAt time.Time
}

type tokenEntry struct {
	userID    string
	createdAt time.Time
}

type server struct {
	cfg Config

	mu                 sync.Mutex
	authCodes          map[string]authCodeEntry
	tokens             map[string]tokenEntry
	userAdditionalOrgs map[string][]organization
}

// =============================================================================
// Middleware
// =============================================================================

func (s *server) withAuth(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		key := r.Header.Get("speakeasy-auth-provider-key")
		if key != s.cfg.SecretKey {
			writeJSON(w, http.StatusUnauthorized, map[string]string{
				"error": "Unauthorized: invalid or missing provider key",
			})
			return
		}
		next(w, r)
	}
}

// =============================================================================
// Handlers
// =============================================================================

func (s *server) handleLogin(w http.ResponseWriter, r *http.Request) {
	returnURL := r.URL.Query().Get("return_url")
	state := r.URL.Query().Get("state")
	if returnURL == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{
			"error": "Missing return_url parameter",
		})
		return
	}
	target, err := url.Parse(returnURL)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "Invalid return_url"})
		return
	}
	code := s.generateAuthCode(s.cfg.User.ID)
	q := target.Query()
	q.Set("code", code)
	if state != "" {
		q.Set("state", state)
	}
	target.RawQuery = q.Encode()
	http.Redirect(w, r, target.String(), http.StatusFound)
}

func (s *server) handleExchange(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Code string `json:"code"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "Invalid request body"})
		return
	}
	if body.Code == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "Missing code in request body"})
		return
	}
	userID := s.validateAuthCode(body.Code)
	if userID == "" {
		// Tolerance: tests sometimes /exchange without /login. Fall back
		// to the configured user.
		userID = s.cfg.User.ID
	}
	token := s.generateToken(userID)
	writeJSON(w, http.StatusOK, map[string]string{"id_token": token})
}

func (s *server) handleValidate(w http.ResponseWriter, r *http.Request) {
	idToken := r.Header.Get("speakeasy-auth-provider-id-token")
	if idToken == "" {
		writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "Missing token header"})
		return
	}
	userID := s.validateToken(idToken)
	if userID == "" {
		writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "Invalid or expired token"})
		return
	}
	writeJSON(w, http.StatusOK, validateResponse{User: s.buildUser(), Organizations: s.allOrgsForUser(userID)})
}

func (s *server) handleRevoke(w http.ResponseWriter, r *http.Request) {
	idToken := r.Header.Get("speakeasy-auth-provider-id-token")
	if idToken != "" {
		s.revokeToken(idToken)
	}
	writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
}

func (s *server) handleRegister(w http.ResponseWriter, r *http.Request) {
	idToken := r.Header.Get("speakeasy-auth-provider-id-token")
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
	userID := s.validateToken(idToken)
	if userID == "" {
		writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "Invalid or expired token"})
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

	s.mu.Lock()
	s.userAdditionalOrgs[userID] = append(s.userAdditionalOrgs[userID], newOrg)
	s.mu.Unlock()

	writeJSON(w, http.StatusOK, validateResponse{User: s.buildUser(), Organizations: s.allOrgsForUser(userID)})
}

// =============================================================================
// In-memory store helpers
// =============================================================================

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

// =============================================================================
// Response builders
// =============================================================================

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

// =============================================================================
// Utility
// =============================================================================

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

func randomID() string {
	b := make([]byte, 16)
	if _, err := rand.Read(b); err != nil {
		panic(fmt.Sprintf("crypto/rand failure: %v", err))
	}
	return hex.EncodeToString(b)
}

var (
	nonAlphanumeric     = regexp.MustCompile(`[^a-z0-9]+`)
	leadingTrailingDash = regexp.MustCompile(`^-+|-+$`)
)

func slugify(name string) string {
	s := strings.ToLower(name)
	s = nonAlphanumeric.ReplaceAllString(s, "-")
	s = leadingTrailingDash.ReplaceAllString(s, "")
	return s
}
