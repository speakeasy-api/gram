package mockoidc

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/speakeasy-api/gram/mock-oidc/internal/o11y"
)

type Server struct {
	provider *Provider
	logger   *slog.Logger
}

func NewServer(provider *Provider, logger *slog.Logger) *Server {
	return &Server{provider: provider, logger: logger}
}

func (s *Server) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /healthz", s.healthz)
	mux.HandleFunc("GET /.well-known/openid-configuration", s.discovery)
	mux.HandleFunc("GET /.well-known/oauth-authorization-server", s.discovery)
	mux.HandleFunc("GET /jwks.json", s.jwksHandler)
	mux.HandleFunc("GET /authorize", s.authorizeGet)
	mux.HandleFunc("POST /authorize", s.authorizePost)
	mux.HandleFunc("POST /token", s.tokenHandler)
	mux.HandleFunc("GET /userinfo", s.userinfoHandler)
	mux.HandleFunc("POST /userinfo", s.userinfoHandler)
	return s.logging(mux)
}

func (s *Server) logging(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		rw := &statusRecorder{ResponseWriter: w, status: 200}
		next.ServeHTTP(rw, r)
		s.logger.InfoContext(r.Context(), "http request",
			slog.String("method", r.Method),
			slog.String("path", r.URL.Path),
			slog.Int("status", rw.status),
			slog.Duration("duration", time.Since(start)),
		)
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

func (s *Server) healthz(w http.ResponseWriter, _ *http.Request) {
	w.WriteHeader(http.StatusOK)
}

func (s *Server) discovery(w http.ResponseWriter, _ *http.Request) {
	iss := s.provider.Issuer()
	doc := map[string]any{
		"issuer":                                iss,
		"authorization_endpoint":                iss + "/authorize",
		"token_endpoint":                        iss + "/token",
		"userinfo_endpoint":                     iss + "/userinfo",
		"jwks_uri":                              iss + "/jwks.json",
		"response_types_supported":              []string{"code"},
		"grant_types_supported":                 []string{"authorization_code"},
		"subject_types_supported":               []string{"public"},
		"id_token_signing_alg_values_supported": []string{"RS256"},
		"scopes_supported":                      []string{"openid", "email", "profile"},
		"token_endpoint_auth_methods_supported": []string{"client_secret_basic", "client_secret_post"},
		"claims_supported":                      []string{"sub", "iss", "aud", "exp", "iat", "email", "email_verified", "name", "picture", "hd", "nonce"},
		"code_challenge_methods_supported":      []string{"S256", "plain"},
	}
	writeJSON(w, http.StatusOK, doc)
}

func (s *Server) jwksHandler(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, s.provider.JWKS())
}

type authorizeParams struct {
	ClientID            string
	RedirectURI         string
	ResponseType        string
	Scope               string
	State               string
	Nonce               string
	CodeChallenge       string
	CodeChallengeMethod string
}

func parseAuthorizeParams(values url.Values) authorizeParams {
	return authorizeParams{
		ClientID:            values.Get("client_id"),
		RedirectURI:         values.Get("redirect_uri"),
		ResponseType:        values.Get("response_type"),
		Scope:               values.Get("scope"),
		State:               values.Get("state"),
		Nonce:               values.Get("nonce"),
		CodeChallenge:       values.Get("code_challenge"),
		CodeChallengeMethod: values.Get("code_challenge_method"),
	}
}

func (p authorizeParams) encode() string {
	v := url.Values{}
	v.Set("client_id", p.ClientID)
	v.Set("redirect_uri", p.RedirectURI)
	v.Set("response_type", p.ResponseType)
	if p.Scope != "" {
		v.Set("scope", p.Scope)
	}
	if p.State != "" {
		v.Set("state", p.State)
	}
	if p.Nonce != "" {
		v.Set("nonce", p.Nonce)
	}
	if p.CodeChallenge != "" {
		v.Set("code_challenge", p.CodeChallenge)
	}
	if p.CodeChallengeMethod != "" {
		v.Set("code_challenge_method", p.CodeChallengeMethod)
	}
	return v.Encode()
}

func (s *Server) signState(p authorizeParams) string {
	mac := hmac.New(sha256.New, s.provider.hmacKey)
	mac.Write([]byte(p.encode()))
	sig := base64.RawURLEncoding.EncodeToString(mac.Sum(nil))
	payload := base64.RawURLEncoding.EncodeToString([]byte(p.encode()))
	return payload + "." + sig
}

func (s *Server) verifyState(token string) (authorizeParams, bool) {
	parts := strings.SplitN(token, ".", 2)
	if len(parts) != 2 {
		return authorizeParams{}, false
	}
	payload, err := base64.RawURLEncoding.DecodeString(parts[0])
	if err != nil {
		return authorizeParams{}, false
	}
	sig, err := base64.RawURLEncoding.DecodeString(parts[1])
	if err != nil {
		return authorizeParams{}, false
	}
	mac := hmac.New(sha256.New, s.provider.hmacKey)
	mac.Write(payload)
	if !hmac.Equal(mac.Sum(nil), sig) {
		return authorizeParams{}, false
	}
	values, err := url.ParseQuery(string(payload))
	if err != nil {
		return authorizeParams{}, false
	}
	return parseAuthorizeParams(values), true
}

func (s *Server) authorizeGet(w http.ResponseWriter, r *http.Request) {
	params := parseAuthorizeParams(r.URL.Query())

	client, ok := s.provider.cfg.FindClient(params.ClientID)
	if !ok {
		s.writeAuthError(w, r, "invalid_client", "unknown client_id")
		return
	}
	if !client.AllowsRedirect(params.RedirectURI) {
		s.writeAuthError(w, r, "invalid_request", "redirect_uri not registered")
		return
	}
	if params.ResponseType != "code" {
		s.redirectAuthError(w, r, params, "unsupported_response_type", "only code response_type is supported")
		return
	}
	if params.CodeChallengeMethod != "" && params.CodeChallengeMethod != "S256" && params.CodeChallengeMethod != "plain" {
		s.redirectAuthError(w, r, params, "invalid_request", "unsupported code_challenge_method")
		return
	}

	data := challengeData{
		ClientID:   client.ClientID,
		ClientName: client.Name,
		StateToken: s.signState(params),
		Users:      s.provider.cfg.Provider.Users,
	}

	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	if err := challengeTmpl.Execute(w, data); err != nil {
		s.logger.ErrorContext(r.Context(), "render challenge", o11y.ErrAttr(err))
	}
}

func (s *Server) authorizePost(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseForm(); err != nil {
		http.Error(w, "invalid form", http.StatusBadRequest)
		return
	}
	stateToken := r.PostForm.Get("state_token")
	email := r.PostForm.Get("user_email")

	params, ok := s.verifyState(stateToken)
	if !ok {
		http.Error(w, "invalid state token", http.StatusBadRequest)
		return
	}

	client, ok := s.provider.cfg.FindClient(params.ClientID)
	if !ok {
		s.writeAuthError(w, r, "invalid_client", "unknown client_id")
		return
	}
	if !client.AllowsRedirect(params.RedirectURI) {
		s.writeAuthError(w, r, "invalid_request", "redirect_uri not registered")
		return
	}

	user, ok := s.provider.cfg.FindUser(email)
	if !ok {
		s.redirectAuthError(w, r, params, "access_denied", "unknown user")
		return
	}

	code, err := s.provider.MintCode(&codeEntry{
		clientID:            params.ClientID,
		redirectURI:         params.RedirectURI,
		subject:             user.Subject(),
		user:                user,
		scope:               params.Scope,
		nonce:               params.Nonce,
		codeChallenge:       params.CodeChallenge,
		codeChallengeMethod: params.CodeChallengeMethod,
	})
	if err != nil {
		s.logger.ErrorContext(r.Context(), "mint code", o11y.ErrAttr(err))
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}

	s.logger.InfoContext(r.Context(), "issued auth code",
		slog.String("client_id", params.ClientID),
		slog.String("subject", user.Subject()),
		slog.String("email", user.Email),
	)

	redirect, err := url.Parse(params.RedirectURI)
	if err != nil {
		http.Error(w, "invalid redirect_uri", http.StatusBadRequest)
		return
	}
	q := redirect.Query()
	q.Set("code", code)
	if params.State != "" {
		q.Set("state", params.State)
	}
	redirect.RawQuery = q.Encode()
	http.Redirect(w, r, redirect.String(), http.StatusFound)
}

func (s *Server) tokenHandler(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseForm(); err != nil {
		writeTokenError(w, http.StatusBadRequest, "invalid_request", "invalid form")
		return
	}

	if r.PostForm.Get("grant_type") != "authorization_code" {
		writeTokenError(w, http.StatusBadRequest, "unsupported_grant_type", "only authorization_code is supported")
		return
	}

	clientID, clientSecret, ok := r.BasicAuth()
	if !ok {
		clientID = r.PostForm.Get("client_id")
		clientSecret = r.PostForm.Get("client_secret")
	}

	client, found := s.provider.cfg.FindClient(clientID)
	if !found || client.ClientSecret != clientSecret {
		writeTokenError(w, http.StatusUnauthorized, "invalid_client", "client authentication failed")
		return
	}

	code := r.PostForm.Get("code")
	entry, ok := s.provider.ConsumeCode(code)
	if !ok {
		writeTokenError(w, http.StatusBadRequest, "invalid_grant", "code is invalid or expired")
		return
	}
	if entry.clientID != clientID {
		writeTokenError(w, http.StatusBadRequest, "invalid_grant", "code was issued to another client")
		return
	}
	if entry.redirectURI != r.PostForm.Get("redirect_uri") {
		writeTokenError(w, http.StatusBadRequest, "invalid_grant", "redirect_uri does not match")
		return
	}

	if entry.codeChallenge != "" {
		verifier := r.PostForm.Get("code_verifier")
		if !verifyPKCE(entry.codeChallenge, entry.codeChallengeMethod, verifier) {
			writeTokenError(w, http.StatusBadRequest, "invalid_grant", "pkce verification failed")
			return
		}
	}

	accessToken, accessExpires, err := s.provider.MintAccessToken(entry.user, entry.scope)
	if err != nil {
		s.logger.ErrorContext(r.Context(), "mint access token", o11y.ErrAttr(err))
		writeTokenError(w, http.StatusInternalServerError, "server_error", "could not mint token")
		return
	}

	idToken, err := s.provider.SignIDToken(entry.user, clientID, entry.nonce, accessExpires)
	if err != nil {
		s.logger.ErrorContext(r.Context(), "sign id token", o11y.ErrAttr(err))
		writeTokenError(w, http.StatusInternalServerError, "server_error", "could not sign id token")
		return
	}

	s.logger.InfoContext(r.Context(), "issued tokens",
		slog.String("client_id", clientID),
		slog.String("subject", entry.user.Subject()),
	)

	resp := map[string]any{
		"access_token": accessToken,
		"token_type":   "Bearer",
		"expires_in":   int(time.Until(accessExpires).Seconds()),
		"id_token":     idToken,
	}
	if entry.scope != "" {
		resp["scope"] = entry.scope
	}
	writeJSON(w, http.StatusOK, resp)
}

func (s *Server) userinfoHandler(w http.ResponseWriter, r *http.Request) {
	authHeader := r.Header.Get("Authorization")
	const prefix = "Bearer "
	if !strings.HasPrefix(authHeader, prefix) {
		w.Header().Set("WWW-Authenticate", `Bearer realm="mock-oidc"`)
		w.WriteHeader(http.StatusUnauthorized)
		return
	}
	tok := strings.TrimPrefix(authHeader, prefix)

	entry, ok := s.provider.LookupAccessToken(tok)
	if !ok {
		w.Header().Set("WWW-Authenticate", `Bearer realm="mock-oidc",error="invalid_token"`)
		w.WriteHeader(http.StatusUnauthorized)
		return
	}

	user := entry.user
	resp := map[string]any{
		"sub":            user.Subject(),
		"email":          user.Email,
		"email_verified": user.EmailVerified,
		"name":           user.Name,
	}
	if user.Picture != "" {
		resp["picture"] = user.Picture
	}
	if user.HD != "" {
		resp["hd"] = user.HD
	}
	writeJSON(w, http.StatusOK, resp)
}

func verifyPKCE(challenge, method, verifier string) bool {
	if verifier == "" {
		return false
	}
	switch method {
	case "", "plain":
		return verifier == challenge
	case "S256":
		sum := sha256.Sum256([]byte(verifier))
		return base64.RawURLEncoding.EncodeToString(sum[:]) == challenge
	default:
		return false
	}
}

func (s *Server) writeAuthError(w http.ResponseWriter, _ *http.Request, code, desc string) {
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	w.WriteHeader(http.StatusBadRequest)
	fmt.Fprintf(w, "%s: %s\n", code, desc)
}

func (s *Server) redirectAuthError(w http.ResponseWriter, r *http.Request, p authorizeParams, code, desc string) {
	redirect, err := url.Parse(p.RedirectURI)
	if err != nil {
		s.writeAuthError(w, r, code, desc)
		return
	}
	q := redirect.Query()
	q.Set("error", code)
	q.Set("error_description", desc)
	if p.State != "" {
		q.Set("state", p.State)
	}
	redirect.RawQuery = q.Encode()
	http.Redirect(w, r, redirect.String(), http.StatusFound)
}

func writeTokenError(w http.ResponseWriter, status int, code, desc string) {
	writeJSON(w, status, map[string]string{
		"error":             code,
		"error_description": desc,
	})
}

func writeJSON(w http.ResponseWriter, status int, body any) {
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(body)
}
