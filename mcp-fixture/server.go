// Package mcpfixture is a throwaway MCP server for local development. It
// gives the local demo seed upstream servers that actually work:
//
//   - /mcp/oauth: an MCP endpoint protected by the fixture's own OAuth 2.1
//     authorization server. Signing in means picking (or typing) an email.
//   - /mcp/shared: an MCP endpoint that accepts the checked-in shared token
//     (fixtureauth.SharedToken), or a caller's own fixture access token.
//   - /acme-api: a stub of the demo seed's "Acme Internal API" OpenAPI
//     document, so the seeded generated servers' tool calls return data.
//
// The authorization server implements authorization-server metadata
// (RFC 8414), protected-resource metadata (RFC 9728), dynamic client
// registration (RFC 7591), client ID metadata documents, authorization code
// with PKCE S256, refresh token rotation, resource indicators (RFC 8707) and
// token revocation (RFC 7009). Tokens are HS256 JWTs signed with a key
// checked into fixtureauth, so they survive restarts and the seed can mint
// them. State that must not survive (used refresh tokens, revocations, notes)
// lives in memory.
package mcpfixture

import (
	"context"
	"crypto/sha256"
	"embed"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"html/template"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/speakeasy-api/gram/mcp-fixture/fixtureauth"
)

//go:embed templates/*.html
var templateFS embed.FS

var signInPage = template.Must(template.ParseFS(templateFS, "templates/signin.html"))

// Config configures a fixture server.
type Config struct {
	// BaseURL is the fixture's external URL, e.g. https://localhost:41234. It
	// is the authorization server's issuer.
	BaseURL string
	// AccessTokenTTL is how long access tokens live. Short values make expiry
	// and refresh visible.
	AccessTokenTTL time.Duration
	// SuggestedEmails are offered on the sign-in page.
	SuggestedEmails []string
	// HTTPClient fetches client ID metadata documents.
	HTTPClient *http.Client
	Logger     *slog.Logger
}

// Server is the fixture. Build it with New and serve Handler.
type Server struct {
	cfg     Config
	issuer  string
	mu      sync.Mutex
	revoked map[string]time.Time // token jti -> expiry, for used refresh tokens and revocations
	notes   map[string][]string  // identity -> notes
}

// New returns a fixture server.
func New(cfg Config) *Server {
	if cfg.AccessTokenTTL <= 0 {
		cfg.AccessTokenTTL = time.Hour
	}
	if cfg.HTTPClient == nil {
		cfg.HTTPClient = &http.Client{Timeout: 10 * time.Second}
	}
	// A client metadata document is served at its client_id URL; never follow
	// a redirect somewhere else.
	noRedirect := *cfg.HTTPClient
	noRedirect.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }
	cfg.HTTPClient = &noRedirect
	if cfg.Logger == nil {
		cfg.Logger = slog.New(slog.NewTextHandler(io.Discard, nil))
	}
	return &Server{
		cfg:     cfg,
		issuer:  strings.TrimRight(cfg.BaseURL, "/"),
		revoked: map[string]time.Time{},
		notes:   map[string][]string{},
	}
}

// Handler serves every fixture route.
func (s *Server) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, _ *http.Request) { _, _ = io.WriteString(w, "ok") })

	// Authorization server.
	mux.HandleFunc("GET /.well-known/oauth-authorization-server", s.authorizationServerMetadata)
	mux.HandleFunc("GET /.well-known/openid-configuration", s.authorizationServerMetadata)
	mux.HandleFunc("GET /.well-known/oauth-protected-resource", s.protectedResourceMetadata)
	mux.HandleFunc("GET /.well-known/oauth-protected-resource"+fixtureauth.PathOAuthMCP, s.protectedResourceMetadata)
	mux.HandleFunc("POST /oauth/register", s.register)
	mux.HandleFunc("GET /oauth/authorize", s.authorizeGet)
	mux.HandleFunc("POST /oauth/authorize", s.authorizePost)
	mux.HandleFunc("POST /oauth/token", s.token)
	mux.HandleFunc("POST /oauth/revoke", s.revoke)

	// MCP endpoints.
	mux.Handle(fixtureauth.PathOAuthMCP, s.mcpHandler(s.authenticateOAuth))
	mux.Handle(fixtureauth.PathSharedMCP, s.mcpHandler(s.authenticateShared))

	// Demo API stub.
	mux.Handle(fixtureauth.PathAPI+"/", http.StripPrefix(fixtureauth.PathAPI, apiStub()))

	return s.logRequests(mux)
}

func (s *Server) logRequests(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		rec := &statusRecorder{ResponseWriter: w, status: http.StatusOK}
		next.ServeHTTP(rec, r)
		s.cfg.Logger.InfoContext(r.Context(), "request", "method", r.Method, "path", r.URL.Path, "status", rec.status)
	})
}

type statusRecorder struct {
	http.ResponseWriter
	status int
}

func (r *statusRecorder) WriteHeader(code int) {
	r.status = code
	r.ResponseWriter.WriteHeader(code)
}

func (r *statusRecorder) Flush() {
	if f, ok := r.ResponseWriter.(http.Flusher); ok {
		f.Flush()
	}
}

// ---------------------------------------------------------------------------
// Metadata

func (s *Server) authorizationServerMetadata(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, map[string]any{
		"issuer":                                     s.issuer,
		"authorization_endpoint":                     s.issuer + "/oauth/authorize",
		"token_endpoint":                             s.issuer + "/oauth/token",
		"registration_endpoint":                      s.issuer + "/oauth/register",
		"revocation_endpoint":                        s.issuer + "/oauth/revoke",
		"scopes_supported":                           fixtureauth.DefaultScopes,
		"response_types_supported":                   []string{"code"},
		"grant_types_supported":                      []string{"authorization_code", "refresh_token"},
		"token_endpoint_auth_methods_supported":      []string{"none"},
		"revocation_endpoint_auth_methods_supported": []string{"none"},
		"code_challenge_methods_supported":           []string{"S256"},
		"client_id_metadata_document_supported":      true,
		"service_documentation":                      s.issuer + "/healthz",
	})
}

func (s *Server) protectedResourceMetadata(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, map[string]any{
		"resource":                 fixtureauth.OAuthResource(s.issuer),
		"resource_name":            "Speakeasy MCP fixture (per-user OAuth)",
		"authorization_servers":    []string{s.issuer},
		"scopes_supported":         fixtureauth.DefaultScopes,
		"bearer_methods_supported": []string{"header"},
	})
}

// ---------------------------------------------------------------------------
// Clients: dynamic registration (stateless) and metadata documents

type client struct {
	ID           string
	Name         string
	RedirectURIs []string
}

func (s *Server) register(w http.ResponseWriter, r *http.Request) {
	var req struct {
		RedirectURIs            []string `json:"redirect_uris"`
		ClientName              string   `json:"client_name"`
		TokenEndpointAuthMethod string   `json:"token_endpoint_auth_method"`
		GrantTypes              []string `json:"grant_types"`
		ResponseTypes           []string `json:"response_types"`
		Scope                   string   `json:"scope"`
	}
	if err := json.NewDecoder(io.LimitReader(r.Body, 1<<16)).Decode(&req); err != nil {
		oauthError(w, http.StatusBadRequest, "invalid_client_metadata", "body is not JSON")
		return
	}
	if len(req.RedirectURIs) == 0 {
		oauthError(w, http.StatusBadRequest, "invalid_redirect_uri", "redirect_uris is required")
		return
	}
	for _, u := range req.RedirectURIs {
		if err := checkRedirectURI(u); err != nil {
			oauthError(w, http.StatusBadRequest, "invalid_redirect_uri", err.Error())
			return
		}
	}
	if req.TokenEndpointAuthMethod != "" && req.TokenEndpointAuthMethod != "none" {
		oauthError(w, http.StatusBadRequest, "invalid_client_metadata", "only token_endpoint_auth_method none is supported")
		return
	}
	now := time.Now()
	// The client id is a signed token describing the client, so registrations
	// survive a fixture restart without storage.
	clientID, err := fixtureauth.Sign(fixtureauth.Claims{
		RegisteredClaims: jwt.RegisteredClaims{
			Issuer:    s.issuer,
			Subject:   "client",
			IssuedAt:  jwt.NewNumericDate(now),
			ExpiresAt: jwt.NewNumericDate(now.Add(10 * 365 * 24 * time.Hour)),
			ID:        fixtureauth.NewID(),
		},
		Kind:         fixtureauth.KindClient,
		RedirectURIs: req.RedirectURIs,
		ClientName:   req.ClientName,
	})
	if err != nil {
		oauthError(w, http.StatusInternalServerError, "server_error", err.Error())
		return
	}
	writeJSON(w, http.StatusCreated, map[string]any{
		"client_id":                  clientID,
		"client_id_issued_at":        now.Unix(),
		"client_name":                req.ClientName,
		"redirect_uris":              req.RedirectURIs,
		"token_endpoint_auth_method": "none",
		"grant_types":                []string{"authorization_code", "refresh_token"},
		"response_types":             []string{"code"},
		"scope":                      strings.Join(fixtureauth.DefaultScopes, " "),
	})
}

// resolveClient accepts a dynamically registered client id or the URL of a
// client ID metadata document.
func (s *Server) resolveClient(ctx context.Context, clientID string) (client, error) {
	if strings.HasPrefix(clientID, "https://") || strings.HasPrefix(clientID, "http://") {
		if err := checkMetadataDocumentURL(clientID); err != nil {
			return client{}, err
		}
		return s.fetchClientMetadataDocument(ctx, clientID)
	}
	c, err := fixtureauth.Parse(clientID, fixtureauth.KindClient)
	if err != nil {
		return client{}, fmt.Errorf("unknown client: %w", err)
	}
	if c.Issuer != s.issuer {
		return client{}, errors.New("client was registered with another fixture")
	}
	return client{ID: clientID, Name: c.ClientName, RedirectURIs: c.RedirectURIs}, nil
}

func (s *Server) fetchClientMetadataDocument(ctx context.Context, docURL string) (client, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, docURL, nil)
	if err != nil {
		return client{}, fmt.Errorf("client metadata document url: %w", err)
	}
	req.Header.Set("Accept", "application/json")
	resp, err := s.cfg.HTTPClient.Do(req)
	if err != nil {
		return client{}, fmt.Errorf("fetch client metadata document: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		return client{}, fmt.Errorf("fetch client metadata document: status %d", resp.StatusCode)
	}
	var doc struct {
		ClientID     string   `json:"client_id"`
		ClientName   string   `json:"client_name"`
		RedirectURIs []string `json:"redirect_uris"`
	}
	if err := json.NewDecoder(io.LimitReader(resp.Body, 1<<16)).Decode(&doc); err != nil {
		return client{}, fmt.Errorf("decode client metadata document: %w", err)
	}
	if doc.ClientID != docURL {
		return client{}, errors.New("client metadata document client_id does not match its URL")
	}
	for _, u := range doc.RedirectURIs {
		if err := checkRedirectURI(u); err != nil {
			return client{}, fmt.Errorf("client metadata document: %w", err)
		}
	}
	return client{ID: docURL, Name: doc.ClientName, RedirectURIs: doc.RedirectURIs}, nil
}

// ---------------------------------------------------------------------------
// Authorization endpoint: a sign-in page where you pick an email

type authorizeRequest struct {
	ClientID            string
	RedirectURI         string
	State               string
	Scope               string
	Resource            string
	CodeChallenge       string
	CodeChallengeMethod string
}

func readAuthorizeRequest(v url.Values) authorizeRequest {
	return authorizeRequest{
		ClientID:            v.Get("client_id"),
		RedirectURI:         v.Get("redirect_uri"),
		State:               v.Get("state"),
		Scope:               v.Get("scope"),
		Resource:            v.Get("resource"),
		CodeChallenge:       v.Get("code_challenge"),
		CodeChallengeMethod: v.Get("code_challenge_method"),
	}
}

// validate checks everything that can be checked before redirecting. A bad
// client or redirect URI must not redirect, so it is reported as a page.
func (s *Server) validate(ctx context.Context, ar authorizeRequest) (client, string, error) {
	c, err := s.resolveClient(ctx, ar.ClientID)
	if err != nil {
		return client{}, "", err
	}
	if !slices.Contains(c.RedirectURIs, ar.RedirectURI) {
		return client{}, "", fmt.Errorf("redirect_uri %q is not registered for this client", ar.RedirectURI)
	}
	if ar.CodeChallenge == "" || ar.CodeChallengeMethod != "S256" {
		return c, "PKCE with code_challenge_method=S256 is required", nil
	}
	if ar.Resource != "" && ar.Resource != fixtureauth.OAuthResource(s.issuer) {
		return c, "unknown resource " + ar.Resource, nil
	}
	for sc := range strings.FieldsSeq(ar.Scope) {
		if !slices.Contains(fixtureauth.DefaultScopes, sc) {
			return c, "unknown scope " + sc, nil
		}
	}
	return c, "", nil
}

func (s *Server) authorizeGet(w http.ResponseWriter, r *http.Request) {
	ar := readAuthorizeRequest(r.URL.Query())
	c, redirectErr, err := s.validate(r.Context(), ar)
	if err != nil {
		http.Error(w, "Sign-in request rejected: "+err.Error(), http.StatusBadRequest)
		return
	}
	if redirectErr != "" {
		redirectWithError(w, r, ar, "invalid_request", redirectErr)
		return
	}
	if r.URL.Query().Get("response_type") != "code" {
		redirectWithError(w, r, ar, "unsupported_response_type", "only response_type=code is supported")
		return
	}
	scope := ar.Scope
	if scope == "" {
		scope = strings.Join(fixtureauth.DefaultScopes, " ")
	}
	name := c.Name
	if name == "" {
		name = "An MCP client"
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	_ = signInPage.Execute(w, map[string]any{
		"ClientName": name,
		"Scopes":     strings.Fields(scope),
		"Suggested":  s.cfg.SuggestedEmails,
		"Request":    ar,
	})
}

func (s *Server) authorizePost(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseForm(); err != nil {
		http.Error(w, "bad form", http.StatusBadRequest)
		return
	}
	ar := readAuthorizeRequest(r.PostForm)
	_, redirectErr, err := s.validate(r.Context(), ar)
	if err != nil {
		http.Error(w, "Sign-in request rejected: "+err.Error(), http.StatusBadRequest)
		return
	}
	if redirectErr != "" {
		redirectWithError(w, r, ar, "invalid_request", redirectErr)
		return
	}
	if r.PostForm.Get("action") == "deny" {
		redirectWithError(w, r, ar, "access_denied", "the user denied the request")
		return
	}
	email := strings.TrimSpace(r.PostForm.Get("email"))
	if email == "" || !strings.Contains(email, "@") {
		http.Error(w, "Enter an email address to sign in as.", http.StatusBadRequest)
		return
	}
	scope := ar.Scope
	if scope == "" {
		scope = strings.Join(fixtureauth.DefaultScopes, " ")
	}
	now := time.Now()
	code, err := fixtureauth.Sign(fixtureauth.Claims{
		RegisteredClaims: jwt.RegisteredClaims{
			Issuer:    s.issuer,
			Subject:   email,
			IssuedAt:  jwt.NewNumericDate(now),
			ExpiresAt: jwt.NewNumericDate(now.Add(5 * time.Minute)),
			ID:        fixtureauth.NewID(),
		},
		Kind:          fixtureauth.KindCode,
		Scope:         scope,
		ClientID:      ar.ClientID,
		RedirectURI:   ar.RedirectURI,
		CodeChallenge: ar.CodeChallenge,
		Resource:      ar.Resource,
	})
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	target, _ := url.Parse(ar.RedirectURI)
	q := target.Query()
	q.Set("code", code)
	q.Set("iss", s.issuer)
	if ar.State != "" {
		q.Set("state", ar.State)
	}
	target.RawQuery = q.Encode()
	http.Redirect(w, r, target.String(), http.StatusFound)
}

func redirectWithError(w http.ResponseWriter, r *http.Request, ar authorizeRequest, code, desc string) {
	target, err := url.Parse(ar.RedirectURI)
	if err != nil {
		http.Error(w, desc, http.StatusBadRequest)
		return
	}
	q := target.Query()
	q.Set("error", code)
	q.Set("error_description", desc)
	if ar.State != "" {
		q.Set("state", ar.State)
	}
	target.RawQuery = q.Encode()
	http.Redirect(w, r, target.String(), http.StatusFound)
}

// ---------------------------------------------------------------------------
// Token endpoint

func (s *Server) token(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseForm(); err != nil {
		oauthError(w, http.StatusBadRequest, "invalid_request", "bad form")
		return
	}
	switch r.PostForm.Get("grant_type") {
	case "authorization_code":
		s.exchangeCode(w, r)
	case "refresh_token":
		s.refresh(w, r)
	default:
		oauthError(w, http.StatusBadRequest, "unsupported_grant_type", "use authorization_code or refresh_token")
	}
}

func (s *Server) exchangeCode(w http.ResponseWriter, r *http.Request) {
	f := r.PostForm
	code, err := fixtureauth.Parse(f.Get("code"), fixtureauth.KindCode)
	if err != nil || code.Issuer != s.issuer {
		oauthError(w, http.StatusBadRequest, "invalid_grant", "code is invalid or expired")
		return
	}
	if !s.useOnce(code.ID, code.ExpiresAt.Time) {
		oauthError(w, http.StatusBadRequest, "invalid_grant", "code was already used")
		return
	}
	if f.Get("client_id") != code.ClientID {
		oauthError(w, http.StatusBadRequest, "invalid_grant", "code was issued to another client")
		return
	}
	if f.Get("redirect_uri") != code.RedirectURI {
		oauthError(w, http.StatusBadRequest, "invalid_grant", "redirect_uri does not match the authorization request")
		return
	}
	if !verifyPKCE(f.Get("code_verifier"), code.CodeChallenge) {
		oauthError(w, http.StatusBadRequest, "invalid_grant", "code_verifier does not match code_challenge")
		return
	}
	if res := f.Get("resource"); res != "" && code.Resource != "" && res != code.Resource {
		oauthError(w, http.StatusBadRequest, "invalid_target", "resource does not match the authorization request")
		return
	}
	s.issue(w, code.Email(), code.ClientID, code.Scopes())
}

func (s *Server) refresh(w http.ResponseWriter, r *http.Request) {
	f := r.PostForm
	rt, err := fixtureauth.Parse(f.Get("refresh_token"), fixtureauth.KindRefresh)
	if err != nil || rt.Issuer != s.issuer {
		oauthError(w, http.StatusBadRequest, "invalid_grant", "refresh token is invalid or expired")
		return
	}
	if cid := f.Get("client_id"); cid != "" && cid != rt.ClientID {
		oauthError(w, http.StatusBadRequest, "invalid_grant", "refresh token was issued to another client")
		return
	}
	// Check the requested scopes before spending the token, so a bad request
	// can be retried.
	scopes := rt.Scopes()
	if requested := strings.Fields(f.Get("scope")); len(requested) > 0 {
		for _, sc := range requested {
			if !slices.Contains(scopes, sc) {
				oauthError(w, http.StatusBadRequest, "invalid_scope", "scope "+sc+" was not granted")
				return
			}
		}
		scopes = requested
	}
	// Rotation: every refresh token works once.
	if !s.useOnce(rt.ID, rt.ExpiresAt.Time) {
		oauthError(w, http.StatusBadRequest, "invalid_grant", "refresh token was already used or revoked")
		return
	}
	s.issue(w, rt.Email(), rt.ClientID, scopes)
}

func (s *Server) issue(w http.ResponseWriter, email, clientID string, scopes []string) {
	pair, err := fixtureauth.IssueTokens(s.issuer, fixtureauth.OAuthResource(s.issuer), clientID, email, scopes, s.cfg.AccessTokenTTL, time.Now())
	if err != nil {
		oauthError(w, http.StatusInternalServerError, "server_error", err.Error())
		return
	}
	w.Header().Set("Cache-Control", "no-store")
	writeJSON(w, http.StatusOK, map[string]any{
		"access_token":  pair.AccessToken,
		"token_type":    "Bearer",
		"expires_in":    int(s.cfg.AccessTokenTTL.Seconds()),
		"refresh_token": pair.RefreshToken,
		"scope":         pair.Scope,
		"resource":      fixtureauth.OAuthResource(s.issuer),
	})
}

func (s *Server) revoke(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseForm(); err != nil {
		oauthError(w, http.StatusBadRequest, "invalid_request", "bad form")
		return
	}
	tok := r.PostForm.Get("token")
	for _, kind := range []string{fixtureauth.KindRefresh, fixtureauth.KindAccess} {
		if c, err := fixtureauth.Parse(tok, kind); err == nil {
			s.useOnce(c.ID, c.ExpiresAt.Time)
		}
	}
	// RFC 7009: respond 200 whether or not the token was known.
	w.WriteHeader(http.StatusOK)
}

// useOnce records jti as spent and reports whether it was unspent.
func (s *Server) useOnce(jti string, exp time.Time) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	now := time.Now()
	for id, e := range s.revoked {
		if e.Before(now) {
			delete(s.revoked, id)
		}
	}
	if _, spent := s.revoked[jti]; spent {
		return false
	}
	s.revoked[jti] = exp
	return true
}

func (s *Server) isRevoked(jti string) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	_, ok := s.revoked[jti]
	return ok
}

func verifyPKCE(verifier, challenge string) bool {
	if verifier == "" || challenge == "" {
		return false
	}
	sum := sha256.Sum256([]byte(verifier))
	return base64.RawURLEncoding.EncodeToString(sum[:]) == challenge
}

// ---------------------------------------------------------------------------
// MCP endpoints

type identity struct {
	Name      string
	Auth      string
	Scopes    []string
	ExpiresAt *time.Time
}

type authenticator func(*http.Request) (identity, *authFailure)

type authFailure struct {
	status    int
	challenge string
	message   string
}

func bearer(r *http.Request) string {
	h := r.Header.Get("Authorization")
	if len(h) > 7 && strings.EqualFold(h[:7], "bearer ") {
		return strings.TrimSpace(h[7:])
	}
	return ""
}

func (s *Server) accessIdentity(tok string) (identity, error) {
	c, err := fixtureauth.Parse(tok, fixtureauth.KindAccess)
	if err != nil {
		return identity{}, err
	}
	if c.Issuer != s.issuer {
		return identity{}, errors.New("token was issued by another fixture")
	}
	if !slices.Contains(c.Audience, fixtureauth.OAuthResource(s.issuer)) {
		return identity{}, errors.New("token audience is not this server")
	}
	if s.isRevoked(c.ID) {
		return identity{}, errors.New("token was revoked")
	}
	exp := c.ExpiresAt.Time
	return identity{Name: c.Email(), Auth: "oauth", Scopes: c.Scopes(), ExpiresAt: &exp}, nil
}

func (s *Server) authenticateOAuth(r *http.Request) (identity, *authFailure) {
	challenge := fmt.Sprintf(`Bearer resource_metadata=%q, scope=%q`,
		s.issuer+"/.well-known/oauth-protected-resource"+fixtureauth.PathOAuthMCP,
		strings.Join(fixtureauth.DefaultScopes, " "))
	tok := bearer(r)
	if tok == "" {
		return identity{}, &authFailure{status: http.StatusUnauthorized, challenge: challenge, message: "sign in required"}
	}
	// Like many real servers, the OAuth endpoint also takes a service
	// account key: the shared token.
	if tok == fixtureauth.SharedToken {
		return identity{Name: fixtureauth.SharedIdentity, Auth: "shared-token", Scopes: fixtureauth.DefaultScopes}, nil
	}
	id, err := s.accessIdentity(tok)
	if err != nil {
		return identity{}, &authFailure{status: http.StatusUnauthorized, challenge: challenge + `, error="invalid_token"`, message: err.Error()}
	}
	return id, nil
}

func (s *Server) authenticateShared(r *http.Request) (identity, *authFailure) {
	tok := bearer(r)
	if tok == fixtureauth.SharedToken {
		return identity{Name: fixtureauth.SharedIdentity, Auth: "shared-token", Scopes: fixtureauth.DefaultScopes}, nil
	}
	if tok != "" {
		if id, err := s.accessIdentity(tok); err == nil {
			id.Auth = "own-token"
			return id, nil
		}
	}
	return identity{}, &authFailure{
		status:    http.StatusUnauthorized,
		challenge: `Bearer realm="mcp-fixture-shared"`,
		message:   "send the shared token or your own fixture access token as a bearer token",
	}
}

func (s *Server) mcpHandler(auth authenticator) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		id, fail := auth(r)
		if fail != nil {
			w.Header().Set("WWW-Authenticate", fail.challenge)
			http.Error(w, fail.message, fail.status)
			return
		}
		h := mcp.NewStreamableHTTPHandler(func(*http.Request) *mcp.Server { return s.mcpServer(id) },
			&mcp.StreamableHTTPOptions{Stateless: true, JSONResponse: true})
		h.ServeHTTP(w, r)
	})
}

type whoamiOut struct {
	Identity  string   `json:"identity"`
	Auth      string   `json:"auth"`
	Scopes    []string `json:"scopes"`
	ExpiresAt string   `json:"expires_at,omitempty"`
}

type echoIn struct {
	Text string `json:"text" jsonschema:"Text to send back"`
}

type echoOut struct {
	Text string `json:"text"`
}

type noteIn struct {
	Text string `json:"text" jsonschema:"The note to save"`
}

type notesOut struct {
	Identity string   `json:"identity"`
	Notes    []string `json:"notes"`
}

func (s *Server) mcpServer(id identity) *mcp.Server {
	srv := mcp.NewServer(&mcp.Implementation{Name: "speakeasy-mcp-fixture", Title: "Speakeasy MCP fixture", Version: "1.0.0"}, nil)
	mcp.AddTool(srv, &mcp.Tool{
		Name:        "whoami",
		Description: "Show who you are signed in as and how you authenticated.",
		Annotations: &mcp.ToolAnnotations{ReadOnlyHint: true},
	}, func(context.Context, *mcp.CallToolRequest, struct{}) (*mcp.CallToolResult, whoamiOut, error) {
		out := whoamiOut{Identity: id.Name, Auth: id.Auth, Scopes: id.Scopes}
		if id.ExpiresAt != nil {
			out.ExpiresAt = id.ExpiresAt.UTC().Format(time.RFC3339)
		}
		return nil, out, nil
	})
	mcp.AddTool(srv, &mcp.Tool{
		Name:        "echo",
		Description: "Send text back unchanged.",
		Annotations: &mcp.ToolAnnotations{ReadOnlyHint: true},
	}, func(_ context.Context, _ *mcp.CallToolRequest, in echoIn) (*mcp.CallToolResult, echoOut, error) {
		return nil, echoOut{Text: in.Text}, nil
	})
	mcp.AddTool(srv, &mcp.Tool{
		Name:        "list_notes",
		Description: "List the notes you saved. Each identity has its own notes.",
		Annotations: &mcp.ToolAnnotations{ReadOnlyHint: true},
	}, func(context.Context, *mcp.CallToolRequest, struct{}) (*mcp.CallToolResult, notesOut, error) {
		if !fixtureauth.HasScope(id.Scopes, fixtureauth.ScopeNotesRead) {
			return nil, notesOut{}, errors.New("this token lacks the notes:read scope")
		}
		return nil, notesOut{Identity: id.Name, Notes: s.listNotes(id.Name)}, nil
	})
	mcp.AddTool(srv, &mcp.Tool{
		Name:        "add_note",
		Description: "Save a note for the signed-in identity. Notes live in memory until the fixture restarts.",
	}, func(_ context.Context, _ *mcp.CallToolRequest, in noteIn) (*mcp.CallToolResult, notesOut, error) {
		if !fixtureauth.HasScope(id.Scopes, fixtureauth.ScopeNotesWrite) {
			return nil, notesOut{}, errors.New("this token lacks the notes:write scope")
		}
		if strings.TrimSpace(in.Text) == "" {
			return nil, notesOut{}, errors.New("text is required")
		}
		return nil, notesOut{Identity: id.Name, Notes: s.addNote(id.Name, in.Text)}, nil
	})
	return srv
}

func (s *Server) listNotes(who string) []string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]string{}, s.notes[who]...)
}

func (s *Server) addNote(who, text string) []string {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.notes[who] = append(s.notes[who], text)
	return append([]string{}, s.notes[who]...)
}

// ---------------------------------------------------------------------------
// Helpers

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

// checkRedirectURI accepts an absolute URL without a fragment.
func checkRedirectURI(raw string) error {
	u, err := url.Parse(raw)
	if err != nil || !u.IsAbs() || u.Host == "" {
		return fmt.Errorf("redirect_uri %q must be an absolute URL", raw)
	}
	if u.Fragment != "" {
		return fmt.Errorf("redirect_uri %q must not have a fragment", raw)
	}
	return nil
}

// checkMetadataDocumentURL accepts https anywhere, and plain http only for an
// exact loopback host.
func checkMetadataDocumentURL(raw string) error {
	u, err := url.Parse(raw)
	if err != nil || u.Host == "" {
		return fmt.Errorf("client_id %q is not a URL", raw)
	}
	if u.Scheme == "https" {
		return nil
	}
	if u.Scheme == "http" && slices.Contains([]string{"localhost", "127.0.0.1", "::1"}, u.Hostname()) {
		return nil
	}
	return fmt.Errorf("client_id %q must be an https URL", raw)
}

func decodeSegment(seg string) ([]byte, error) {
	b, err := base64.RawURLEncoding.DecodeString(seg)
	if err != nil {
		return nil, fmt.Errorf("decode token segment: %w", err)
	}
	return b, nil
}

func oauthError(w http.ResponseWriter, status int, code, desc string) {
	writeJSON(w, status, map[string]string{"error": code, "error_description": desc})
}
