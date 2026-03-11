// Package mockidp implements a mock Speakeasy identity provider for local
// development and testing. It provides the /v1/speakeasy_provider/* endpoints
// that the Gram server calls during authentication.
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

// Config holds the mock IDP configuration. All fields have defaults.
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
}

// NewConfig returns a Config with hardcoded test defaults (no env var lookup).
// Use this in tests for deterministic behavior.
func NewConfig() Config {
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
		},
	}
}

// DefaultConfig returns a Config populated from environment variables with
// sensible defaults. Use this for local development servers.
func DefaultConfig() Config {
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
			ID:          envStr("MOCK_IDP_ORG_ID", "550e8400-e29b-41d4-a716-446655440000"),
			Name:        envStr("MOCK_IDP_ORG_NAME", "Local Dev Org"),
			Slug:        envStr("MOCK_IDP_ORG_SLUG", "local-dev-org"),
			AccountType: envStr("MOCK_IDP_ORG_ACCOUNT_TYPE", "free"),
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
// endpoints with auth middleware baked in.
func Handler(cfg Config) http.Handler {
	s := &server{
		cfg:                cfg,
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

type server struct {
	cfg Config

	mu                 sync.Mutex
	authCodes          map[string]authCodeEntry
	tokens             map[string]tokenEntry
	userAdditionalOrgs map[string][]organization
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

func (s *server) handleLogin(w http.ResponseWriter, r *http.Request) {
	returnURL := r.URL.Query().Get("return_url")
	state := r.URL.Query().Get("state")

	log.Printf("[login] return_url=%s state=%s", returnURL, state)

	if returnURL == "" {
		log.Printf("[login] REJECTED: missing return_url")
		writeJSON(w, http.StatusBadRequest, map[string]string{
			"error": "Missing return_url parameter",
		})
		return
	}

	code := s.generateAuthCode(s.cfg.User.ID)

	u, err := url.Parse(returnURL)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{
			"error": "Invalid return_url",
		})
		return
	}
	q := u.Query()
	q.Set("code", code)
	if state != "" {
		q.Set("state", state)
	}
	u.RawQuery = q.Encode()

	log.Printf("[login] OK: generated code=%s, redirecting to %s", code, u.String())
	http.Redirect(w, r, u.String(), http.StatusFound)
}

func (s *server) handleExchange(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Code string `json:"code"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "Invalid request body"})
		return
	}
	log.Printf("[exchange] code=%s", body.Code)

	if body.Code == "" {
		log.Printf("[exchange] REJECTED: missing code")
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "Missing code in request body"})
		return
	}

	userID := s.validateAuthCode(body.Code)
	if userID == "" {
		log.Printf("[exchange] REJECTED: invalid code")
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "Invalid or expired auth code"})
		return
	}

	token := s.generateToken(userID)
	log.Printf("[exchange] OK: userId=%s token=%s", userID, token)
	writeJSON(w, http.StatusOK, map[string]string{"id_token": token})
}

func (s *server) handleValidate(w http.ResponseWriter, r *http.Request) {
	token := r.Header.Get("speakeasy-auth-provider-id-token")
	log.Printf("[validate] token=%s", token)

	if token == "" {
		log.Printf("[validate] REJECTED: missing token header")
		writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "Missing token header"})
		return
	}

	userID := s.validateToken(token)
	if userID == "" {
		log.Printf("[validate] REJECTED: invalid token")
		writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "Invalid or expired token"})
		return
	}

	u := s.buildUser()
	orgs := s.allOrgsForUser(userID)
	log.Printf("[validate] OK: userId=%s email=%s orgs=%d", userID, u.Email, len(orgs))
	writeJSON(w, http.StatusOK, validateResponse{User: u, Organizations: orgs})
}

func (s *server) handleRevoke(w http.ResponseWriter, r *http.Request) {
	token := r.Header.Get("speakeasy-auth-provider-id-token")
	log.Printf("[revoke] token=%s", token)

	if token != "" {
		s.revokeToken(token)
	}

	log.Printf("[revoke] OK")
	writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
}

func (s *server) handleRegister(w http.ResponseWriter, r *http.Request) {
	token := r.Header.Get("speakeasy-auth-provider-id-token")
	log.Printf("[register] token=%s", token)

	if token == "" {
		log.Printf("[register] REJECTED: missing token header")
		writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "Missing token header"})
		return
	}

	userID := s.validateToken(token)
	if userID == "" {
		log.Printf("[register] REJECTED: invalid token")
		writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "Invalid or expired token"})
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
	log.Printf("[register] org_name=%s account_type=%s", body.OrganizationName, body.AccountType)

	if body.OrganizationName == "" {
		log.Printf("[register] REJECTED: missing organization_name")
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "Missing organization_name"})
		return
	}

	now := time.Now().UTC().Format(time.RFC3339)
	slug := slugify(body.OrganizationName)
	accountType := body.AccountType
	if accountType == "" {
		accountType = "free"
	}

	newOrg := organization{
		ID:                 randomID(),
		Name:               body.OrganizationName,
		Slug:               slug,
		CreatedAt:          now,
		UpdatedAt:          now,
		AccountType:        accountType,
		SSOConnectionID:    nil,
		UserWorkspaceSlugs: []string{slug},
	}

	s.mu.Lock()
	s.userAdditionalOrgs[userID] = append(s.userAdditionalOrgs[userID], newOrg)
	s.mu.Unlock()

	u := s.buildUser()
	orgs := s.allOrgsForUser(userID)
	writeJSON(w, http.StatusOK, validateResponse{User: u, Organizations: orgs})
}

// --- store helpers ---

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
	delete(s.authCodes, code) // one-time use
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

// --- data helpers ---

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

// --- utility functions ---

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

func envBool(key string, fallback bool) bool {
	v := os.Getenv(key)
	if v == "" {
		return fallback
	}
	return v == "true" || v == "1"
}

// LogConfig prints the server configuration to stdout, matching the TypeScript
// version's startup output.
func LogConfig(cfg Config, port int) {
	fmt.Printf("Mock Speakeasy IDP running on http://localhost:%d\n", port)
	fmt.Printf("User: %s <%s> (admin=%v)\n", cfg.User.DisplayName, cfg.User.Email, cfg.User.Admin)
	fmt.Printf("Org:  %s (%s)\n", cfg.Organization.Name, cfg.Organization.Slug)
	fmt.Println("Endpoints:")
	fmt.Println("  GET  /v1/speakeasy_provider/login")
	fmt.Println("  POST /v1/speakeasy_provider/exchange")
	fmt.Println("  GET  /v1/speakeasy_provider/validate")
	fmt.Println("  POST /v1/speakeasy_provider/revoke")
	fmt.Println("  POST /v1/speakeasy_provider/register")
}
