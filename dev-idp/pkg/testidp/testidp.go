// Package testidp serves an in-process mock OIDC identity provider for tests.
// It implements /oauth2/token and /oauth2/userinfo against in-memory state,
// matching the standard OIDC endpoints that Gram's session manager calls.
package testidp

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/http"
	"sync"
)

// Constants exported for tests that need to assert against the
// canonical fixture identity.
const (
	MockUserID    = "test-user-1"
	MockUserEmail = "dev@example.com"
	MockOrgID     = "550e8400-e29b-41d4-a716-446655440000"
	MockOrgName   = "Local Dev Org"
	MockOrgSlug   = "local-dev-org"
)

// Config holds the IDP configuration.
type Config struct {
	User         UserConfig
	Organization OrgConfig
}

// UserConfig holds the mock user configuration.
type UserConfig struct {
	ID          string
	Email       string
	DisplayName string
	PhotoURL    *string
	Admin       bool
}

// OrgConfig holds the mock organization configuration.
type OrgConfig struct {
	ID       string
	Name     string
	Slug     string
	WorkOSID *string
}

// NewConfig returns a Config with the canonical fixture defaults.
func NewConfig() Config {
	wosOrgID := MockOrgID
	return Config{
		User: UserConfig{
			ID:          MockUserID,
			Email:       MockUserEmail,
			DisplayName: "Dev User",
			PhotoURL:    nil,
			Admin:       true,
		},
		Organization: OrgConfig{
			ID:       MockOrgID,
			Name:     MockOrgName,
			Slug:     MockOrgSlug,
			WorkOSID: &wosOrgID,
		},
	}
}

// Handler returns an http.Handler implementing mock OIDC endpoints
// (/oauth2/token, /oauth2/userinfo).
func Handler(cfg Config) http.Handler {
	s := &server{
		cfg:    cfg,
		tokens: make(map[string]string),
	}
	mux := http.NewServeMux()
	mux.HandleFunc("POST /oauth2/token", s.handleToken)
	mux.HandleFunc("GET /oauth2/userinfo", s.handleUserinfo)
	return mux
}

// =============================================================================
// Server state
// =============================================================================

type server struct {
	cfg Config

	mu     sync.Mutex
	tokens map[string]string // access_token → userID
}

// =============================================================================
// Handlers
// =============================================================================

// handleToken implements the OIDC token endpoint. Accepts
// grant_type=authorization_code and returns a mock access_token.
func (s *server) handleToken(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseForm(); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid_request"})
		return
	}

	grantType := r.FormValue("grant_type")
	code := r.FormValue("code")

	if grantType != "authorization_code" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "unsupported_grant_type"})
		return
	}
	if code == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid_grant"})
		return
	}

	// Any non-empty code is accepted; map it to the configured user.
	accessToken := randomID()
	s.mu.Lock()
	s.tokens[accessToken] = s.cfg.User.ID
	s.mu.Unlock()

	writeJSON(w, http.StatusOK, map[string]string{
		"access_token": accessToken,
		"token_type":   "Bearer",
	})
}

// handleUserinfo implements the OIDC userinfo endpoint. Returns the
// configured user's identity claims.
func (s *server) handleUserinfo(w http.ResponseWriter, r *http.Request) {
	auth := r.Header.Get("Authorization")
	if len(auth) < 8 || auth[:7] != "Bearer " {
		writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "invalid_token"})
		return
	}
	token := auth[7:]

	s.mu.Lock()
	_, ok := s.tokens[token]
	s.mu.Unlock()

	if !ok {
		writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "invalid_token"})
		return
	}

	resp := struct {
		Sub     string  `json:"sub"`
		Email   string  `json:"email"`
		Name    string  `json:"name"`
		Picture *string `json:"picture,omitempty"`
	}{
		Sub:     s.cfg.User.ID,
		Email:   s.cfg.User.Email,
		Name:    s.cfg.User.DisplayName,
		Picture: s.cfg.User.PhotoURL,
	}

	writeJSON(w, http.StatusOK, resp)
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
