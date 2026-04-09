package oauthtest

import (
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
)

// AuthorizationServer is an in-memory OAuth 2.1 authorization server for
// integration testing. It implements DCR (RFC 7591), authorization, and token
// exchange endpoints with PKCE (S256) validation.
//
// All authorization requests are auto-approved — there is no interactive consent
// step. Tokens are opaque UUIDs prefixed with "access_" or "refresh_" for easy
// identification in test assertions.
type AuthorizationServer struct {
	// Endpoint URLs (populated after the server starts).
	Issuer                string
	AuthorizationEndpoint string
	TokenEndpoint         string
	RegistrationEndpoint  string

	server *httptest.Server

	mu       sync.Mutex
	clients  map[string]registeredClient
	codes    map[string]authCode
	tokens   map[string]tokenRecord
	settings AuthorizationServerSettings
}

// AuthorizationServerSettings allows tests to customize server behavior.
type AuthorizationServerSettings struct {
	// AccessTokenTTL controls how long issued access tokens are valid.
	// Defaults to 1 hour.
	AccessTokenTTL time.Duration
	// RefreshTokenTTL controls how long issued refresh tokens are valid.
	// Defaults to 24 hours.
	RefreshTokenTTL time.Duration
}

type registeredClient struct {
	ClientID     string
	ClientSecret string
	RedirectURIs []string
}

type authCode struct {
	ClientID      string
	RedirectURI   string
	CodeChallenge string
	Scope         string
	ExpiresAt     time.Time
}

type tokenRecord struct {
	AccessToken  string
	RefreshToken string
	ClientID     string
	Scope        string
	ExpiresAt    time.Time
}

// NewAuthorizationServer starts an in-memory OAuth authorization server and
// registers t.Cleanup to shut it down. The server listens on a random loopback
// port via httptest.NewServer.
func NewAuthorizationServer(t *testing.T, settings ...AuthorizationServerSettings) *AuthorizationServer {
	t.Helper()

	s := &AuthorizationServer{
		clients: make(map[string]registeredClient),
		codes:   make(map[string]authCode),
		tokens:  make(map[string]tokenRecord),
		settings: AuthorizationServerSettings{
			AccessTokenTTL:  time.Hour,
			RefreshTokenTTL: 24 * time.Hour,
		},
	}
	if len(settings) > 0 {
		if settings[0].AccessTokenTTL > 0 {
			s.settings.AccessTokenTTL = settings[0].AccessTokenTTL
		}
		if settings[0].RefreshTokenTTL > 0 {
			s.settings.RefreshTokenTTL = settings[0].RefreshTokenTTL
		}
	}

	mux := http.NewServeMux()
	mux.HandleFunc("POST /register", s.handleRegister)
	mux.HandleFunc("GET /authorize", s.handleAuthorize)
	mux.HandleFunc("POST /token", s.handleToken)

	s.server = httptest.NewServer(mux)
	t.Cleanup(s.server.Close)

	base := s.server.URL
	s.Issuer = base
	s.AuthorizationEndpoint = base + "/authorize"
	s.TokenEndpoint = base + "/token"
	s.RegistrationEndpoint = base + "/register"

	return s
}

// Metadata returns RFC 8414 authorization server metadata as JSON bytes,
// suitable for storing in the external_oauth_server_metadata table.
func (s *AuthorizationServer) Metadata() []byte {
	meta := map[string]any{
		"issuer":                                s.Issuer,
		"authorization_endpoint":                s.AuthorizationEndpoint,
		"token_endpoint":                        s.TokenEndpoint,
		"registration_endpoint":                 s.RegistrationEndpoint,
		"response_types_supported":              []string{"code"},
		"grant_types_supported":                 []string{"authorization_code", "refresh_token"},
		"token_endpoint_auth_methods_supported": []string{"client_secret_post"},
		"code_challenge_methods_supported":      []string{"S256"},
	}
	bs, _ := json.Marshal(meta)
	return bs
}

// SeedRefreshToken pre-registers a refresh token so that a subsequent
// grant_type=refresh_token request for it will succeed. This is useful when
// tests issue Gram-layer tokens via TokenIssuer with an arbitrary upstream
// refresh token string — the AuthorizationServer needs to know about it.
func (s *AuthorizationServer) SeedRefreshToken(refreshToken string, clientID string, scope string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.tokens[refreshToken] = tokenRecord{
		RefreshToken: refreshToken,
		ClientID:     clientID,
		Scope:        scope,
		ExpiresAt:    time.Now().Add(s.settings.RefreshTokenTTL),
	}
}

// LookupAccessToken returns the token record for a given access token, or false
// if it doesn't exist or has expired. Useful for test assertions.
func (s *AuthorizationServer) LookupAccessToken(accessToken string) (tokenRecord, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	rec, ok := s.tokens[accessToken]
	if !ok || time.Now().After(rec.ExpiresAt) {
		return tokenRecord{}, false
	}
	return rec, true
}

// handleRegister implements Dynamic Client Registration (RFC 7591).
func (s *AuthorizationServer) handleRegister(w http.ResponseWriter, r *http.Request) {
	var req struct {
		RedirectURIs            []string `json:"redirect_uris"`
		TokenEndpointAuthMethod string   `json:"token_endpoint_auth_method"`
		GrantTypes              []string `json:"grant_types"`
		ResponseTypes           []string `json:"response_types"`
		ClientName              string   `json:"client_name"`
		Scope                   string   `json:"scope,omitempty"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSONError(w, http.StatusBadRequest, "invalid_request", "malformed JSON body")
		return
	}

	clientID := "client_" + uuid.New().String()[:8]
	clientSecret := "secret_" + uuid.New().String()[:8]

	s.mu.Lock()
	s.clients[clientID] = registeredClient{
		ClientID:     clientID,
		ClientSecret: clientSecret,
		RedirectURIs: req.RedirectURIs,
	}
	s.mu.Unlock()

	now := time.Now()
	resp := map[string]any{
		"client_id":                  clientID,
		"client_secret":              clientSecret,
		"client_id_issued_at":        now.Unix(),
		"client_secret_expires_at":   0,
		"redirect_uris":              req.RedirectURIs,
		"token_endpoint_auth_method": req.TokenEndpointAuthMethod,
		"grant_types":                req.GrantTypes,
		"response_types":             req.ResponseTypes,
		"client_name":                req.ClientName,
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusCreated)
	_ = json.NewEncoder(w).Encode(resp)
}

// handleAuthorize auto-approves authorization requests and redirects with a code.
func (s *AuthorizationServer) handleAuthorize(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	clientID := q.Get("client_id")
	redirectURI := q.Get("redirect_uri")
	state := q.Get("state")
	codeChallenge := q.Get("code_challenge")
	codeChallengeMethod := q.Get("code_challenge_method")
	scope := q.Get("scope")

	if clientID == "" || redirectURI == "" {
		writeJSONError(w, http.StatusBadRequest, "invalid_request", "client_id and redirect_uri are required")
		return
	}

	s.mu.Lock()
	_, clientExists := s.clients[clientID]
	s.mu.Unlock()

	if !clientExists {
		writeJSONError(w, http.StatusUnauthorized, "invalid_client", "unknown client_id")
		return
	}

	if codeChallengeMethod != "" && codeChallengeMethod != "S256" {
		writeJSONError(w, http.StatusBadRequest, "invalid_request", "only S256 code_challenge_method is supported")
		return
	}

	code := "code_" + uuid.New().String()[:16]

	s.mu.Lock()
	s.codes[code] = authCode{
		ClientID:      clientID,
		RedirectURI:   redirectURI,
		CodeChallenge: codeChallenge,
		Scope:         scope,
		ExpiresAt:     time.Now().Add(10 * time.Minute),
	}
	s.mu.Unlock()

	redirectURL, err := url.Parse(redirectURI)
	if err != nil {
		writeJSONError(w, http.StatusBadRequest, "invalid_request", "malformed redirect_uri")
		return
	}
	rq := redirectURL.Query()
	rq.Set("code", code)
	if state != "" {
		rq.Set("state", state)
	}
	redirectURL.RawQuery = rq.Encode()

	http.Redirect(w, r, redirectURL.String(), http.StatusFound)
}

// handleToken exchanges authorization codes or refresh tokens for access tokens.
func (s *AuthorizationServer) handleToken(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseForm(); err != nil {
		writeJSONError(w, http.StatusBadRequest, "invalid_request", "malformed form body")
		return
	}

	grantType := r.FormValue("grant_type")
	switch grantType {
	case "authorization_code":
		s.handleTokenAuthorizationCode(w, r)
	case "refresh_token":
		s.handleTokenRefresh(w, r)
	default:
		writeJSONError(w, http.StatusBadRequest, "unsupported_grant_type",
			fmt.Sprintf("grant_type %q is not supported", grantType))
	}
}

func (s *AuthorizationServer) handleTokenAuthorizationCode(w http.ResponseWriter, r *http.Request) {
	code := r.FormValue("code")
	clientID := r.FormValue("client_id")
	codeVerifier := r.FormValue("code_verifier")

	s.mu.Lock()
	ac, codeExists := s.codes[code]
	if codeExists {
		delete(s.codes, code) // single-use
	}
	s.mu.Unlock()

	if !codeExists || time.Now().After(ac.ExpiresAt) {
		writeJSONError(w, http.StatusBadRequest, "invalid_grant", "authorization code is invalid or expired")
		return
	}

	if ac.ClientID != clientID {
		writeJSONError(w, http.StatusBadRequest, "invalid_grant", "client_id mismatch")
		return
	}

	// Validate PKCE
	if ac.CodeChallenge != "" {
		if codeVerifier == "" {
			writeJSONError(w, http.StatusBadRequest, "invalid_grant", "code_verifier is required")
			return
		}
		hash := sha256.Sum256([]byte(codeVerifier))
		expected := base64.RawURLEncoding.EncodeToString(hash[:])
		if expected != ac.CodeChallenge {
			writeJSONError(w, http.StatusBadRequest, "invalid_grant", "code_verifier does not match code_challenge")
			return
		}
	}

	accessToken := "access_" + uuid.New().String()
	refreshToken := "refresh_" + uuid.New().String()

	s.mu.Lock()
	s.tokens[accessToken] = tokenRecord{
		AccessToken:  accessToken,
		RefreshToken: refreshToken,
		ClientID:     clientID,
		Scope:        ac.Scope,
		ExpiresAt:    time.Now().Add(s.settings.AccessTokenTTL),
	}
	s.tokens[refreshToken] = tokenRecord{
		AccessToken:  accessToken,
		RefreshToken: refreshToken,
		ClientID:     clientID,
		Scope:        ac.Scope,
		ExpiresAt:    time.Now().Add(s.settings.RefreshTokenTTL),
	}
	s.mu.Unlock()

	resp := map[string]any{
		"access_token":  accessToken,
		"token_type":    "Bearer",
		"expires_in":    int(s.settings.AccessTokenTTL.Seconds()),
		"refresh_token": refreshToken,
	}
	if ac.Scope != "" {
		resp["scope"] = ac.Scope
	}

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(resp)
}

func (s *AuthorizationServer) handleTokenRefresh(w http.ResponseWriter, r *http.Request) {
	refreshToken := r.FormValue("refresh_token")
	clientID := r.FormValue("client_id")

	s.mu.Lock()
	rec, exists := s.tokens[refreshToken]
	if exists {
		// Rotate: invalidate old tokens
		delete(s.tokens, rec.AccessToken)
		delete(s.tokens, refreshToken)
	}
	s.mu.Unlock()

	if !exists || time.Now().After(rec.ExpiresAt) {
		writeJSONError(w, http.StatusBadRequest, "invalid_grant", "refresh token is invalid or expired")
		return
	}

	if rec.ClientID != clientID {
		writeJSONError(w, http.StatusBadRequest, "invalid_grant", "client_id mismatch")
		return
	}

	newAccessToken := "access_" + uuid.New().String()
	newRefreshToken := "refresh_" + uuid.New().String()

	s.mu.Lock()
	s.tokens[newAccessToken] = tokenRecord{
		AccessToken:  newAccessToken,
		RefreshToken: newRefreshToken,
		ClientID:     clientID,
		Scope:        rec.Scope,
		ExpiresAt:    time.Now().Add(s.settings.AccessTokenTTL),
	}
	s.tokens[newRefreshToken] = tokenRecord{
		AccessToken:  newAccessToken,
		RefreshToken: newRefreshToken,
		ClientID:     clientID,
		Scope:        rec.Scope,
		ExpiresAt:    time.Now().Add(s.settings.RefreshTokenTTL),
	}
	s.mu.Unlock()

	resp := map[string]any{
		"access_token":  newAccessToken,
		"token_type":    "Bearer",
		"expires_in":    int(s.settings.AccessTokenTTL.Seconds()),
		"refresh_token": newRefreshToken,
	}
	if rec.Scope != "" {
		resp["scope"] = rec.Scope
	}

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(resp)
}

func writeJSONError(w http.ResponseWriter, status int, errorCode string, description string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(map[string]string{
		"error":             errorCode,
		"error_description": description,
	})
}
