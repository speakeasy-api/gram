package localfixture

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"mime"
	"net/http"
	"net/url"
	"slices"
	"sync"
	"time"

	"github.com/speakeasy-api/gram/server/internal/usersessions"
)

const oauthRequestMaxBytes int64 = 64 << 10

type OAuthHTTP struct {
	config        *Config
	clients       map[string]registeredClient
	authorization map[string]authorizationCode
	accessTokens  map[string]issuedToken
	refreshTokens map[string]issuedToken
	mu            sync.Mutex
}

type registeredClient struct {
	redirectURI string
	createdAt   time.Time
}

type authorizationCode struct {
	clientID      string
	redirectURI   string
	codeChallenge string
	issuedAt      time.Time
}

type issuedToken struct {
	clientID         string
	accessToken      string
	refreshToken     string
	accessExpiresAt  time.Time
	refreshExpiresAt time.Time
}

func NewOAuthHTTP(config *Config) *OAuthHTTP {
	return &OAuthHTTP{
		config:        config,
		clients:       make(map[string]registeredClient),
		authorization: make(map[string]authorizationCode),
		accessTokens:  make(map[string]issuedToken),
		refreshTokens: make(map[string]issuedToken),
		mu:            sync.Mutex{},
	}
}

func (s *OAuthHTTP) Handler() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if s == nil || s.config == nil {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Cache-Control", "no-store")
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/.well-known/oauth-authorization-server/"+fixtureOAuthPath:
			s.handleMetadata(w)
		case r.Method == http.MethodPost && r.URL.Path == "/"+fixtureOAuthPath+"/register":
			s.handleRegister(w, r)
		case r.Method == http.MethodGet && r.URL.Path == "/"+fixtureOAuthPath+"/authorize":
			s.handleAuthorize(w, r)
		case r.Method == http.MethodPost && r.URL.Path == "/"+fixtureOAuthPath+"/token":
			s.handleToken(w, r)
		case r.Method == http.MethodPost && r.URL.Path == "/"+fixtureOAuthPath+"/revoke":
			s.handleRevoke(w, r)
		default:
			http.NotFound(w, r)
		}
	})
}

func (s *OAuthHTTP) handleMetadata(w http.ResponseWriter) {
	writeJSON(w, http.StatusOK, map[string]any{
		"issuer":                                s.config.OAuthIssuerURL(),
		"authorization_endpoint":                s.config.OAuthAuthorizationURL(),
		"token_endpoint":                        s.config.OAuthTokenURL(),
		"registration_endpoint":                 s.config.OAuthRegistrationURL(),
		"revocation_endpoint":                   s.config.OAuthRevocationURL(),
		"response_types_supported":              []string{"code"},
		"grant_types_supported":                 []string{"authorization_code", "refresh_token"},
		"token_endpoint_auth_methods_supported": []string{"none"},
		"code_challenge_methods_supported":      []string{"S256"},
		"scopes_supported":                      []string{"tools:read"},
	})
}

func (s *OAuthHTTP) handleRegister(w http.ResponseWriter, r *http.Request) {
	mediaType, _, err := mime.ParseMediaType(r.Header.Get("Content-Type"))
	if err != nil || mediaType != "application/json" {
		writeOAuthError(w, http.StatusUnsupportedMediaType, "invalid_client_metadata", "Content-Type must be application/json")
		return
	}
	r.Body = http.MaxBytesReader(w, r.Body, oauthRequestMaxBytes)
	var request usersessions.RegistrationRequest
	if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
		writeOAuthError(w, http.StatusBadRequest, "invalid_client_metadata", "request body is not valid JSON")
		return
	}
	request.SetDefaults()
	if err := request.Validate(); err != nil {
		writeOAuthError(w, http.StatusBadRequest, "invalid_client_metadata", "request does not match the local fixture contract")
		return
	}
	if request.ClientName != OAuthClientName || len(request.RedirectURIs) != 1 || request.RedirectURIs[0] != s.config.RemoteLoginCallbackURL() || request.TokenEndpointAuthMethod != "none" || !sameStrings(request.GrantTypes, []string{"authorization_code", "refresh_token"}) || !sameStrings(request.ResponseTypes, []string{"code"}) {
		writeOAuthError(w, http.StatusBadRequest, "invalid_client_metadata", "request does not match the local fixture contract")
		return
	}

	clientID, err := opaqueID()
	if err != nil {
		writeOAuthError(w, http.StatusInternalServerError, "server_error", "could not register client")
		return
	}
	s.mu.Lock()
	s.clients[clientID] = registeredClient{redirectURI: request.RedirectURIs[0], createdAt: time.Now().UTC()}
	s.mu.Unlock()
	writeJSON(w, http.StatusCreated, map[string]any{
		"client_id":                  clientID,
		"client_id_issued_at":        time.Now().Unix(),
		"client_name":                request.ClientName,
		"redirect_uris":              request.RedirectURIs,
		"grant_types":                request.GrantTypes,
		"response_types":             request.ResponseTypes,
		"token_endpoint_auth_method": request.TokenEndpointAuthMethod,
	})
}

func (s *OAuthHTTP) handleAuthorize(w http.ResponseWriter, r *http.Request) {
	query := r.URL.Query()
	clientID := query.Get("client_id")
	redirectURI := query.Get("redirect_uri")
	if clientID == "" || redirectURI == "" {
		writeOAuthError(w, http.StatusBadRequest, "invalid_request", "client_id and redirect_uri are required")
		return
	}

	s.mu.Lock()
	client, ok := s.clients[clientID]
	s.mu.Unlock()
	if !ok || client.redirectURI != redirectURI {
		writeOAuthError(w, http.StatusBadRequest, "invalid_request", "client or redirect URI is not registered")
		return
	}
	if query.Get("response_type") != "code" || query.Get("state") == "" || query.Get("scope") != "tools:read" || query.Get("resource") != s.config.RemoteURL() || query.Get("code_challenge_method") != "S256" || query.Get("code_challenge") == "" {
		writeOAuthError(w, http.StatusBadRequest, "invalid_request", "request does not match the local fixture contract")
		return
	}

	code, err := opaqueID()
	if err != nil {
		writeOAuthError(w, http.StatusInternalServerError, "server_error", "could not issue authorization code")
		return
	}
	s.mu.Lock()
	s.authorization[code] = authorizationCode{
		clientID:      clientID,
		redirectURI:   redirectURI,
		codeChallenge: query.Get("code_challenge"),
		issuedAt:      time.Now().UTC(),
	}
	s.mu.Unlock()

	callback, err := url.Parse(redirectURI)
	if err != nil {
		writeOAuthError(w, http.StatusBadRequest, "invalid_request", "redirect URI is invalid")
		return
	}
	values := callback.Query()
	values.Set("code", code)
	values.Set("state", query.Get("state"))
	callback.RawQuery = values.Encode()
	http.Redirect(w, r, callback.String(), http.StatusFound)
}

func (s *OAuthHTTP) handleToken(w http.ResponseWriter, r *http.Request) {
	if r.Header.Get("Content-Type") != "application/x-www-form-urlencoded" {
		writeOAuthError(w, http.StatusUnsupportedMediaType, "invalid_request", "Content-Type must be application/x-www-form-urlencoded")
		return
	}
	r.Body = http.MaxBytesReader(w, r.Body, oauthRequestMaxBytes)
	if err := r.ParseForm(); err != nil {
		writeOAuthError(w, http.StatusBadRequest, "invalid_request", "request body is not valid form data")
		return
	}
	switch r.PostForm.Get("grant_type") {
	case "authorization_code":
		s.exchangeAuthorizationCode(w, r.PostForm)
	case "refresh_token":
		s.exchangeRefreshToken(w, r.PostForm)
	default:
		writeOAuthError(w, http.StatusBadRequest, "unsupported_grant_type", "grant_type is not supported")
	}
}

func (s *OAuthHTTP) exchangeAuthorizationCode(w http.ResponseWriter, form url.Values) {
	code := form.Get("code")
	s.mu.Lock()
	authorization, ok := s.authorization[code]
	delete(s.authorization, code)
	s.mu.Unlock()
	if !ok || time.Since(authorization.issuedAt) > 5*time.Minute || form.Get("client_id") != authorization.clientID || form.Get("redirect_uri") != authorization.redirectURI || !requestedScopeIsAllowed(form) || form.Get("resource") != s.config.RemoteURL() || !matchesS256(form.Get("code_verifier"), authorization.codeChallenge) {
		writeInvalidGrant(w)
		return
	}
	s.issueTokens(w, authorization.clientID)
}

func (s *OAuthHTTP) exchangeRefreshToken(w http.ResponseWriter, form url.Values) {
	refreshToken := form.Get("refresh_token")
	s.mu.Lock()
	token, ok := s.refreshTokens[refreshToken]
	if ok && token.refreshExpiresAt.Before(time.Now()) {
		delete(s.refreshTokens, refreshToken)
		delete(s.accessTokens, token.accessToken)
		ok = false
	}
	if ok && (form.Get("client_id") != token.clientID || !requestedScopeIsAllowed(form) || form.Get("resource") != s.config.RemoteURL()) {
		s.mu.Unlock()
		writeInvalidGrant(w)
		return
	}
	if ok {
		delete(s.refreshTokens, refreshToken)
		delete(s.accessTokens, token.accessToken)
	}
	s.mu.Unlock()
	if !ok {
		writeInvalidGrant(w)
		return
	}
	s.issueTokens(w, token.clientID)
}

func (s *OAuthHTTP) issueTokens(w http.ResponseWriter, clientID string) {
	accessToken, err := opaqueID()
	if err != nil {
		writeOAuthError(w, http.StatusInternalServerError, "server_error", "could not issue access token")
		return
	}
	refreshToken, err := opaqueID()
	if err != nil {
		writeOAuthError(w, http.StatusInternalServerError, "server_error", "could not issue refresh token")
		return
	}
	now := time.Now().UTC()
	token := issuedToken{
		clientID:         clientID,
		accessToken:      accessToken,
		refreshToken:     refreshToken,
		accessExpiresAt:  now.Add(5 * time.Minute),
		refreshExpiresAt: now.Add(time.Hour),
	}
	s.mu.Lock()
	s.accessTokens[accessToken] = token
	s.refreshTokens[refreshToken] = token
	s.mu.Unlock()
	writeJSON(w, http.StatusOK, map[string]any{
		"access_token":  accessToken,
		"expires_in":    300,
		"refresh_token": refreshToken,
		"scope":         "tools:read",
		"token_type":    "Bearer",
	})
}

func (s *OAuthHTTP) HasRegisteredClient(clientID string) bool {
	if s == nil || clientID == "" {
		return false
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	_, ok := s.clients[clientID]
	return ok
}

// HasLiveAccessToken reports whether token is an unrevoked fixture access token.
// It is intentionally the only token-store operation exposed to the fixture MCP
// handler; token values never leave the localfixture package or reach logs.
func (s *OAuthHTTP) HasLiveAccessToken(token string) bool {
	if s == nil || token == "" {
		return false
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	issued, ok := s.accessTokens[token]
	if !ok || !issued.accessExpiresAt.After(time.Now()) {
		if ok {
			delete(s.accessTokens, token)
		}
		return false
	}
	return true
}

func (s *OAuthHTTP) handleRevoke(w http.ResponseWriter, r *http.Request) {
	if r.Header.Get("Content-Type") != "application/x-www-form-urlencoded" {
		writeOAuthError(w, http.StatusUnsupportedMediaType, "invalid_request", "Content-Type must be application/x-www-form-urlencoded")
		return
	}
	r.Body = http.MaxBytesReader(w, r.Body, oauthRequestMaxBytes)
	if err := r.ParseForm(); err != nil {
		writeOAuthError(w, http.StatusBadRequest, "invalid_request", "request body is not valid form data")
		return
	}
	tokenValue := r.PostForm.Get("token")
	s.mu.Lock()
	if token, ok := s.accessTokens[tokenValue]; ok {
		delete(s.accessTokens, token.accessToken)
		delete(s.refreshTokens, token.refreshToken)
	}
	if token, ok := s.refreshTokens[tokenValue]; ok {
		delete(s.accessTokens, token.accessToken)
		delete(s.refreshTokens, token.refreshToken)
	}
	s.mu.Unlock()
	w.WriteHeader(http.StatusOK)
}

func sameStrings(left, right []string) bool {
	return len(left) == len(right) && slices.Equal(left, right)
}

func requestedScopeIsAllowed(form url.Values) bool {
	return form.Get("scope") == "" || form.Get("scope") == "tools:read"
}

func matchesS256(verifier, challenge string) bool {
	if verifier == "" {
		return false
	}
	sum := sha256.Sum256([]byte(verifier))
	return base64.RawURLEncoding.EncodeToString(sum[:]) == challenge
}

func opaqueID() (string, error) {
	bytes := make([]byte, 32)
	if _, err := rand.Read(bytes); err != nil {
		return "", fmt.Errorf("generate fixture opaque ID: %w", err)
	}
	return "local_fixture_" + base64.RawURLEncoding.EncodeToString(bytes), nil
}

func writeInvalidGrant(w http.ResponseWriter) {
	writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid_grant"})
}

func writeOAuthError(w http.ResponseWriter, status int, code, description string) {
	writeJSON(w, status, map[string]string{"error": code, "error_description": description})
}
