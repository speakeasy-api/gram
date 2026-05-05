// Package oauth21 implements the dev-idp's oauth2-1 mode (idp-design.md
// §7.3): an OAuth 2.1 authorization server with PKCE-required (S256),
// stateless DCR, and OIDC compliance. Backs `remote_session_issuer` rows
// used in remote-session tests.
//
// Identity resolution is non-interactive (idp-design.md §3) — every
// /authorize call resolves the per-mode currentUser and
// immediately redirects with the issued code. Client authentication is
// permissive (idp-design.md §5.2) — every client_id / client_secret /
// redirect_uri is accepted as-is.
package oauth21

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"database/sql"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"net/url"
	"slices"
	"strings"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"github.com/google/uuid"
	"go.opentelemetry.io/otel/trace"

	"github.com/speakeasy-api/gram/dev-idp/internal/database/repo"
	"github.com/speakeasy-api/gram/dev-idp/internal/defaultuser"
	"github.com/speakeasy-api/gram/dev-idp/internal/keystore"
)

// Mode is the discriminator persisted on auth_codes / tokens /
// current_users rows owned by this handler.
const Mode = "oauth2-1"

// Prefix is the URL prefix the dev-idp listener mounts the handler under.
const Prefix = "/oauth2-1"

const (
	authCodeLifetime     = 5 * time.Minute
	accessTokenLifetime  = 1 * time.Hour
	refreshTokenLifetime = 30 * 24 * time.Hour
	idTokenLifetime      = 1 * time.Hour

	// maxFormBodyBytes caps /token and /revoke request bodies. OAuth form
	// payloads are small; 64 KiB is comfortably above any legitimate
	// request and below any DoS-relevant size.
	maxFormBodyBytes = 64 << 10
)

// Config carries the static configuration for the oauth2-1 mode.
type Config struct {
	// ExternalURL is the dev-idp's externally reachable base URL (no
	// trailing slash, no mode prefix). Used to build absolute URLs in
	// discovery documents and as the `iss` claim on issued id_tokens.
	ExternalURL string
}

// Handler serves the oauth2-1 mode's HTTP routes.
type Handler struct {
	cfg      Config
	tracer   trace.Tracer
	logger   *slog.Logger
	db       *sql.DB
	keystore *keystore.Keystore
}

func NewHandler(cfg Config, ks *keystore.Keystore, logger *slog.Logger, tracerProvider trace.TracerProvider, db *sql.DB) *Handler {
	return &Handler{
		cfg:      cfg,
		tracer:   tracerProvider.Tracer("github.com/speakeasy-api/gram/dev-idp/internal/modes/oauth21"),
		logger:   logger.With(slog.String("component", "devidp."+Mode)),
		db:       db,
		keystore: ks,
	}
}

// Handler returns the http.Handler that should be mounted under `Prefix`
// (use http.StripPrefix). All registered paths are relative to that
// prefix.
func (h *Handler) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /.well-known/oauth-authorization-server", h.handleASMetadata)
	mux.HandleFunc("GET /.well-known/openid-configuration", h.handleOIDCDiscovery)
	mux.Handle("GET /.well-known/jwks.json", h.keystore.JWKSHandler())
	mux.HandleFunc("POST /register", h.handleRegister)
	mux.HandleFunc("GET /authorize", h.handleAuthorize)
	mux.HandleFunc("POST /token", h.handleToken)
	mux.HandleFunc("GET /userinfo", h.handleUserinfo)
	mux.HandleFunc("POST /revoke", h.handleRevoke)
	return mux
}

// issuer is the absolute URL identifying this AS in OAuth/OIDC metadata
// and in id_token `iss` claims.
func (h *Handler) issuer() string {
	return strings.TrimRight(h.cfg.ExternalURL, "/") + Prefix
}

// =============================================================================
// Discovery — RFC 8414 (oauth-authorization-server) + OIDC Discovery 1.0
// =============================================================================

type asMetadata struct {
	Issuer                            string   `json:"issuer"`
	AuthorizationEndpoint             string   `json:"authorization_endpoint"`
	TokenEndpoint                     string   `json:"token_endpoint"`
	UserinfoEndpoint                  string   `json:"userinfo_endpoint"`
	RegistrationEndpoint              string   `json:"registration_endpoint"`
	RevocationEndpoint                string   `json:"revocation_endpoint"`
	JwksURI                           string   `json:"jwks_uri"`
	ResponseTypesSupported            []string `json:"response_types_supported"`
	GrantTypesSupported               []string `json:"grant_types_supported"`
	CodeChallengeMethodsSupported     []string `json:"code_challenge_methods_supported"`
	TokenEndpointAuthMethodsSupported []string `json:"token_endpoint_auth_methods_supported"`
	ScopesSupported                   []string `json:"scopes_supported"`
}

type oidcMetadata struct {
	asMetadata
	SubjectTypesSupported            []string `json:"subject_types_supported"`
	IDTokenSigningAlgValuesSupported []string `json:"id_token_signing_alg_values_supported"`
	ClaimsSupported                  []string `json:"claims_supported"`
}

func (h *Handler) baseMetadata() asMetadata {
	iss := h.issuer()
	return asMetadata{
		Issuer:                            iss,
		AuthorizationEndpoint:             iss + "/authorize",
		TokenEndpoint:                     iss + "/token",
		UserinfoEndpoint:                  iss + "/userinfo",
		RegistrationEndpoint:              iss + "/register",
		RevocationEndpoint:                iss + "/revoke",
		JwksURI:                           iss + "/.well-known/jwks.json",
		ResponseTypesSupported:            []string{"code"},
		GrantTypesSupported:               []string{"authorization_code", "refresh_token"},
		CodeChallengeMethodsSupported:     []string{"S256"},
		TokenEndpointAuthMethodsSupported: []string{"client_secret_basic", "client_secret_post", "none"},
		ScopesSupported:                   []string{"openid", "email", "profile"},
	}
}

func (h *Handler) handleASMetadata(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, h.baseMetadata())
}

func (h *Handler) handleOIDCDiscovery(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, oidcMetadata{
		asMetadata:                       h.baseMetadata(),
		SubjectTypesSupported:            []string{"public"},
		IDTokenSigningAlgValuesSupported: []string{h.keystore.SigningAlg()},
		ClaimsSupported:                  []string{"sub", "iss", "aud", "exp", "iat", "email", "name", "picture"},
	})
}

// =============================================================================
// /register — RFC 7591 stateless DCR (idp-design.md §5.2)
// =============================================================================

type dcrResponse struct {
	ClientID                string   `json:"client_id"`
	ClientSecret            string   `json:"client_secret"`
	ClientIDIssuedAt        int64    `json:"client_id_issued_at"`
	ClientSecretExpiresAt   int64    `json:"client_secret_expires_at"`
	RedirectURIs            []string `json:"redirect_uris,omitempty"`
	GrantTypes              []string `json:"grant_types,omitempty"`
	ResponseTypes           []string `json:"response_types,omitempty"`
	TokenEndpointAuthMethod string   `json:"token_endpoint_auth_method,omitempty"`
}

func (h *Handler) handleRegister(w http.ResponseWriter, r *http.Request) {
	// Decode whatever the caller sent so we can echo redirect_uris /
	// grant_types / response_types back. None of it is persisted (§5.2).
	var body struct {
		RedirectURIs            []string `json:"redirect_uris"`
		GrantTypes              []string `json:"grant_types"`
		ResponseTypes           []string `json:"response_types"`
		TokenEndpointAuthMethod string   `json:"token_endpoint_auth_method"`
	}
	_ = json.NewDecoder(r.Body).Decode(&body)

	writeJSON(w, http.StatusCreated, dcrResponse{
		ClientID:                "client_" + randomHex(16),
		ClientSecret:            "secret_" + randomHex(32),
		ClientIDIssuedAt:        time.Now().Unix(),
		ClientSecretExpiresAt:   0, // 0 = never expires (RFC 7591)
		RedirectURIs:            body.RedirectURIs,
		GrantTypes:              body.GrantTypes,
		ResponseTypes:           body.ResponseTypes,
		TokenEndpointAuthMethod: body.TokenEndpointAuthMethod,
	})
}

// =============================================================================
// /authorize — PKCE-required (S256). Auto-passes for currentUser.
// =============================================================================

func (h *Handler) handleAuthorize(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	q := r.URL.Query()

	h.logger.InfoContext(ctx, "auth flow initiated",
		slog.String("event", "devidp.mode.used"),
		slog.String("http.route", r.URL.Path),
	)

	responseType := q.Get("response_type")
	clientID := q.Get("client_id")
	redirectURI := q.Get("redirect_uri")
	scope := q.Get("scope")
	state := q.Get("state")
	codeChallenge := q.Get("code_challenge")
	codeChallengeMethod := q.Get("code_challenge_method")

	if responseType != "code" {
		oauthError(w, http.StatusBadRequest, "unsupported_response_type", "only response_type=code is supported")
		return
	}
	if clientID == "" || redirectURI == "" {
		oauthError(w, http.StatusBadRequest, "invalid_request", "client_id and redirect_uri are required")
		return
	}
	if codeChallenge == "" {
		oauthError(w, http.StatusBadRequest, "invalid_request", "code_challenge is required (PKCE is mandatory in oauth2-1)")
		return
	}
	if codeChallengeMethod != "S256" {
		oauthError(w, http.StatusBadRequest, "invalid_request", "code_challenge_method must be S256")
		return
	}

	target, err := url.Parse(redirectURI)
	if err != nil {
		oauthError(w, http.StatusBadRequest, "invalid_request", "redirect_uri is not a valid URL")
		return
	}

	userID, err := h.resolveCurrentUserID(ctx)
	if err != nil {
		// PKCE flows can't redirect on auth-resolution failure (no client
		// to send the user back to in a meaningful state). Surface the
		// failure as a JSON error the test/dashboard can read directly.
		oauthError(w, http.StatusBadRequest, "access_denied", err.Error())
		return
	}

	code := randomHex(16)
	if _, err := repo.New(h.db).CreateAuthCode(ctx, repo.CreateAuthCodeParams{
		Code:                code,
		Mode:                Mode,
		UserID:              userID,
		ClientID:            clientID,
		RedirectUri:         redirectURI,
		CodeChallenge:       sql.NullString{String: codeChallenge, Valid: true},
		CodeChallengeMethod: sql.NullString{String: codeChallengeMethod, Valid: true},
		Scope:               sql.NullString{String: scope, Valid: scope != ""},
		ExpiresAt:           time.Now().Add(authCodeLifetime),
	}); err != nil {
		h.logger.ErrorContext(ctx, "create auth code", slog.Any("error", err))
		oauthError(w, http.StatusInternalServerError, "server_error", "failed to issue auth code")
		return
	}

	rq := target.Query()
	rq.Set("code", code)
	if state != "" {
		rq.Set("state", state)
	}
	target.RawQuery = rq.Encode()
	http.Redirect(w, r, target.String(), http.StatusFound)
}

// =============================================================================
// /token — authorization_code + refresh_token grants
// =============================================================================

type tokenResponse struct {
	AccessToken  string `json:"access_token"`
	TokenType    string `json:"token_type"`
	ExpiresIn    int    `json:"expires_in"`
	RefreshToken string `json:"refresh_token,omitempty"`
	Scope        string `json:"scope,omitempty"`
	IDToken      string `json:"id_token,omitempty"`
}

func (h *Handler) handleToken(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	r.Body = http.MaxBytesReader(w, r.Body, maxFormBodyBytes)
	if err := r.ParseForm(); err != nil {
		oauthError(w, http.StatusBadRequest, "invalid_request", "failed to parse form")
		return
	}

	switch r.Form.Get("grant_type") {
	case "authorization_code":
		h.handleAuthorizationCodeGrant(ctx, w, r)
	case "refresh_token":
		h.handleRefreshTokenGrant(ctx, w, r)
	case "":
		oauthError(w, http.StatusBadRequest, "invalid_request", "grant_type is required")
	default:
		oauthError(w, http.StatusBadRequest, "unsupported_grant_type", "only authorization_code and refresh_token are supported")
	}
}

func (h *Handler) handleAuthorizationCodeGrant(ctx context.Context, w http.ResponseWriter, r *http.Request) {
	code := r.Form.Get("code")
	verifier := r.Form.Get("code_verifier")
	clientID := r.Form.Get("client_id")
	if code == "" || verifier == "" {
		oauthError(w, http.StatusBadRequest, "invalid_request", "code and code_verifier are required")
		return
	}

	queries := repo.New(h.db)
	stored, err := queries.ConsumeAuthCode(ctx, repo.ConsumeAuthCodeParams{Code: code, Mode: Mode})
	if err != nil {
		// Includes ErrNoRows (unknown / consumed / expired). Don't leak which.
		oauthError(w, http.StatusBadRequest, "invalid_grant", "auth code is unknown, consumed, or expired")
		return
	}

	if !stored.CodeChallenge.Valid || !validatePKCES256(verifier, stored.CodeChallenge.String) {
		oauthError(w, http.StatusBadRequest, "invalid_grant", "PKCE verifier does not match challenge")
		return
	}

	// Per §5.2 client_id is recorded for inspection only. We cross-check
	// it to give the caller a useful error if they typo it on /token vs
	// /authorize, but only when the caller bothered to send it.
	if clientID != "" && clientID != stored.ClientID {
		oauthError(w, http.StatusBadRequest, "invalid_grant", "client_id does not match the auth code")
		return
	}

	scope := pgTextOrEmpty(stored.Scope)
	tokens, err := h.issueTokenSet(ctx, queries, stored.UserID, stored.ClientID, scope)
	if err != nil {
		h.logger.ErrorContext(ctx, "issue token set", slog.Any("error", err))
		oauthError(w, http.StatusInternalServerError, "server_error", "failed to issue tokens")
		return
	}
	writeJSON(w, http.StatusOK, tokens)
}

func (h *Handler) handleRefreshTokenGrant(ctx context.Context, w http.ResponseWriter, r *http.Request) {
	refreshToken := r.Form.Get("refresh_token")
	if refreshToken == "" {
		oauthError(w, http.StatusBadRequest, "invalid_request", "refresh_token is required")
		return
	}

	queries := repo.New(h.db)
	stored, err := queries.GetActiveToken(ctx, repo.GetActiveTokenParams{Token: refreshToken, Mode: Mode})
	if err != nil {
		oauthError(w, http.StatusBadRequest, "invalid_grant", "refresh token is unknown, revoked, or expired")
		return
	}
	if stored.Kind != "refresh_token" {
		oauthError(w, http.StatusBadRequest, "invalid_grant", "presented token is not a refresh token")
		return
	}

	// OAuth 2.1 recommends rotating refresh tokens on use. Revoke the
	// presented token; issueTokenSet mints a fresh pair.
	if err := queries.RevokeToken(ctx, repo.RevokeTokenParams{
		Ts:    sql.NullTime{Time: time.Now(), Valid: true},
		Token: refreshToken,
		Mode:  Mode,
	}); err != nil {
		h.logger.ErrorContext(ctx, "revoke rotated refresh token", slog.Any("error", err))
		oauthError(w, http.StatusInternalServerError, "server_error", "failed to rotate refresh token")
		return
	}

	tokens, err := h.issueTokenSet(ctx, queries, stored.UserID, stored.ClientID, pgTextOrEmpty(stored.Scope))
	if err != nil {
		h.logger.ErrorContext(ctx, "issue token set on refresh", slog.Any("error", err))
		oauthError(w, http.StatusInternalServerError, "server_error", "failed to issue tokens")
		return
	}
	writeJSON(w, http.StatusOK, tokens)
}

// issueTokenSet writes opaque access + refresh rows to the tokens table
// and, when the scope contains "openid", signs an id_token JWT and writes
// a row for it too (so the dashboard can show "this id_token was issued").
func (h *Handler) issueTokenSet(ctx context.Context, queries *repo.Queries, userID uuid.UUID, clientID, scope string) (tokenResponse, error) {
	access := randomHex(32)
	refresh := randomHex(32)

	if _, err := queries.CreateToken(ctx, repo.CreateTokenParams{
		Token:     access,
		Mode:      Mode,
		UserID:    userID,
		ClientID:  clientID,
		Kind:      "access_token",
		Scope:     sql.NullString{String: scope, Valid: scope != ""},
		ExpiresAt: time.Now().Add(accessTokenLifetime),
	}); err != nil {
		return tokenResponse{}, fmt.Errorf("insert access_token: %w", err)
	}
	if _, err := queries.CreateToken(ctx, repo.CreateTokenParams{
		Token:     refresh,
		Mode:      Mode,
		UserID:    userID,
		ClientID:  clientID,
		Kind:      "refresh_token",
		Scope:     sql.NullString{String: scope, Valid: scope != ""},
		ExpiresAt: time.Now().Add(refreshTokenLifetime),
	}); err != nil {
		return tokenResponse{}, fmt.Errorf("insert refresh_token: %w", err)
	}

	resp := tokenResponse{
		AccessToken:  access,
		TokenType:    "Bearer",
		ExpiresIn:    int(accessTokenLifetime.Seconds()),
		RefreshToken: refresh,
		Scope:        scope,
		IDToken:      "",
	}

	if scopeContains(scope, "openid") {
		idToken, err := h.signIDToken(ctx, queries, userID, clientID)
		if err != nil {
			return tokenResponse{}, err
		}
		if _, err := queries.CreateToken(ctx, repo.CreateTokenParams{
			Token:     idToken,
			Mode:      Mode,
			UserID:    userID,
			ClientID:  clientID,
			Kind:      "id_token",
			Scope:     sql.NullString{String: scope, Valid: true},
			ExpiresAt: time.Now().Add(idTokenLifetime),
		}); err != nil {
			return tokenResponse{}, fmt.Errorf("insert id_token: %w", err)
		}
		resp.IDToken = idToken
	}

	return resp, nil
}

// idTokenClaims carries the OIDC standard claim set the dev-idp emits.
// The dev-idp does not gate `email` / `name` / `picture` on a `profile`
// or `email` scope (idp-design.md §7.3 says tests shouldn't have to wire
// up scope-conditional claims).
type idTokenClaims struct {
	Email   string `json:"email,omitempty"`
	Name    string `json:"name,omitempty"`
	Picture string `json:"picture,omitempty"`
	jwt.RegisteredClaims
}

func (h *Handler) signIDToken(ctx context.Context, queries *repo.Queries, userID uuid.UUID, clientID string) (string, error) {
	user, err := queries.GetUser(ctx, userID)
	if err != nil {
		return "", fmt.Errorf("look up user for id_token: %w", err)
	}

	now := time.Now()
	claims := idTokenClaims{
		Email:   user.Email,
		Name:    user.DisplayName,
		Picture: pgTextOrEmpty(user.PhotoUrl),
		RegisteredClaims: jwt.RegisteredClaims{
			ID:        uuid.New().String(),
			Issuer:    h.issuer(),
			Subject:   userID.String(),
			Audience:  jwt.ClaimStrings{clientID},
			IssuedAt:  jwt.NewNumericDate(now),
			ExpiresAt: jwt.NewNumericDate(now.Add(idTokenLifetime)),
			NotBefore: nil,
		},
	}

	token := jwt.NewWithClaims(jwt.SigningMethodRS256, claims)
	token.Header["kid"] = h.keystore.KID()
	signed, err := token.SignedString(h.keystore.PrivateKey())
	if err != nil {
		return "", fmt.Errorf("sign id_token: %w", err)
	}
	return signed, nil
}

// =============================================================================
// /userinfo
// =============================================================================

type userinfoResponse struct {
	Sub     string `json:"sub"`
	Email   string `json:"email,omitempty"`
	Name    string `json:"name,omitempty"`
	Picture string `json:"picture,omitempty"`
}

func (h *Handler) handleUserinfo(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	bearer := bearerToken(r)
	if bearer == "" {
		w.Header().Set("WWW-Authenticate", `Bearer realm="oauth2-1", error="invalid_token"`)
		oauthError(w, http.StatusUnauthorized, "invalid_token", "missing bearer token")
		return
	}

	queries := repo.New(h.db)
	stored, err := queries.GetActiveToken(ctx, repo.GetActiveTokenParams{Token: bearer, Mode: Mode})
	if err != nil || stored.Kind != "access_token" {
		w.Header().Set("WWW-Authenticate", `Bearer realm="oauth2-1", error="invalid_token"`)
		oauthError(w, http.StatusUnauthorized, "invalid_token", "bearer is unknown, revoked, expired, or not an access token")
		return
	}

	user, err := queries.GetUser(ctx, stored.UserID)
	if err != nil {
		h.logger.ErrorContext(ctx, "look up user for userinfo", slog.Any("error", err))
		oauthError(w, http.StatusInternalServerError, "server_error", "failed to load user")
		return
	}

	writeJSON(w, http.StatusOK, userinfoResponse{
		Sub:     user.ID.String(),
		Email:   user.Email,
		Name:    user.DisplayName,
		Picture: pgTextOrEmpty(user.PhotoUrl),
	})
}

// =============================================================================
// /revoke — RFC 7009
// =============================================================================

func (h *Handler) handleRevoke(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	r.Body = http.MaxBytesReader(w, r.Body, maxFormBodyBytes)
	if err := r.ParseForm(); err != nil {
		// RFC 7009 still wants 200 here; the AS is "best effort." Log and
		// return success.
		h.logger.WarnContext(ctx, "revoke parse form", slog.Any("error", err))
		w.WriteHeader(http.StatusOK)
		return
	}
	token := r.Form.Get("token")
	if token != "" {
		if err := repo.New(h.db).RevokeToken(ctx, repo.RevokeTokenParams{
			Ts:    sql.NullTime{Time: time.Now(), Valid: true},
			Token: token,
			Mode:  Mode,
		}); err != nil {
			h.logger.WarnContext(ctx, "revoke token", slog.Any("error", err))
		}
	}
	w.WriteHeader(http.StatusOK)
}

// =============================================================================
// Helpers
// =============================================================================

var errCurrentUserMissing = errors.New("currentUser references a missing user row")

func (h *Handler) resolveCurrentUserID(ctx context.Context) (uuid.UUID, error) {
	queries := repo.New(h.db)
	row, err := queries.GetCurrentUser(ctx, Mode)
	if errors.Is(err, sql.ErrNoRows) {
		// First touch on this mode: bootstrap from git committer.
		uid, berr := defaultuser.BootstrapLocalUser(ctx, h.db, Mode)
		if berr != nil {
			return uuid.Nil, fmt.Errorf("bootstrap default currentUser: %w", berr)
		}
		return uid, nil
	}
	if err != nil {
		return uuid.Nil, fmt.Errorf("read currentUser: %w", err)
	}
	id, err := uuid.Parse(row.SubjectRef)
	if err != nil {
		return uuid.Nil, fmt.Errorf("parse currentUser subject_ref: %w", err)
	}
	if _, err := queries.GetUser(ctx, id); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return uuid.Nil, errCurrentUserMissing
		}
		return uuid.Nil, fmt.Errorf("look up currentUser: %w", err)
	}
	return id, nil
}

// validatePKCES256 returns true when the SHA-256 hash of `verifier`,
// base64url-encoded without padding, equals `challenge`.
func validatePKCES256(verifier, challenge string) bool {
	digest := sha256.Sum256([]byte(verifier))
	computed := base64.RawURLEncoding.EncodeToString(digest[:])
	return subtle.ConstantTimeCompare([]byte(computed), []byte(challenge)) == 1
}

func scopeContains(scope, want string) bool {
	return slices.Contains(strings.Fields(scope), want)
}

func bearerToken(r *http.Request) string {
	h := r.Header.Get("Authorization")
	if !strings.HasPrefix(h, "Bearer ") {
		return ""
	}
	return strings.TrimSpace(strings.TrimPrefix(h, "Bearer "))
}

func pgTextOrEmpty(t sql.NullString) string {
	if !t.Valid {
		return ""
	}
	return t.String
}

func randomHex(nBytes int) string {
	b := make([]byte, nBytes)
	_, _ = rand.Read(b)
	return hex.EncodeToString(b)
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

// oauthError writes an OAuth-shaped error envelope (RFC 6749 §5.2).
func oauthError(w http.ResponseWriter, status int, code, description string) {
	writeJSON(w, status, map[string]string{
		"error":             code,
		"error_description": description,
	})
}
