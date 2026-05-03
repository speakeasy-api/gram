// Package oauth2 implements the dev-idp's oauth2 mode (idp-design.md §7.4):
// an OAuth 2.0 authorization server with optional PKCE (honored when
// present), no DCR, and OIDC compliance. Backs the legacy
// remote_session_issuer shape used in migration-testing milestones.
//
// Differences from the sibling oauth21 mode:
//   - No /register endpoint — DCR is not part of OAuth 2.0.
//   - PKCE on /authorize is optional. When omitted, /token honors the
//     legacy "no code_verifier required" path.
//   - Refresh tokens do not rotate. OAuth 2.0 doesn't mandate it; tests
//     that need rotation should target oauth2-1.
//
// Shared with oauth21: the keystore + JWKS surface, the auth_codes /
// tokens schema, the per-mode currentUser pointer (idp-design.md §3),
// and the permissive client-authentication posture (§5.2).
package oauth2

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
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

	"github.com/golang-jwt/jwt/v5"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"
	"go.opentelemetry.io/otel/trace"

	"github.com/speakeasy-api/gram/server/internal/attr"
	"github.com/speakeasy-api/gram/server/internal/devidp/database/repo"
	"github.com/speakeasy-api/gram/server/internal/devidp/keystore"
)

// Mode is the discriminator persisted on auth_codes / tokens /
// current_users rows owned by this handler.
const Mode = "oauth2"

// Prefix is the URL prefix the dev-idp listener mounts the handler under.
const Prefix = "/oauth2"

const (
	authCodeLifetime     = 5 * time.Minute
	accessTokenLifetime  = 1 * time.Hour
	refreshTokenLifetime = 30 * 24 * time.Hour
	idTokenLifetime      = 1 * time.Hour

	// maxFormBodyBytes caps /token and /revoke request bodies. OAuth form
	// payloads are small.
	maxFormBodyBytes = 64 << 10
)

// Config carries the static configuration for the oauth2 mode.
type Config struct {
	// ExternalURL is the dev-idp's externally reachable base URL (no
	// trailing slash, no mode prefix). Used to build absolute URLs in
	// discovery documents and as the `iss` claim on issued id_tokens.
	ExternalURL string
}

// Handler serves the oauth2 mode's HTTP routes.
type Handler struct {
	cfg      Config
	tracer   trace.Tracer
	logger   *slog.Logger
	db       *pgxpool.Pool
	keystore *keystore.Keystore
}

func NewHandler(cfg Config, ks *keystore.Keystore, logger *slog.Logger, tracerProvider trace.TracerProvider, db *pgxpool.Pool) *Handler {
	return &Handler{
		cfg:      cfg,
		tracer:   tracerProvider.Tracer("github.com/speakeasy-api/gram/server/internal/devidp/modes/oauth2"),
		logger:   logger.With(attr.SlogComponent("devidp." + Mode)),
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
	mux.HandleFunc("GET /authorize", h.handleAuthorize)
	mux.HandleFunc("POST /token", h.handleToken)
	mux.HandleFunc("GET /userinfo", h.handleUserinfo)
	mux.HandleFunc("POST /revoke", h.handleRevoke)
	return mux
}

func (h *Handler) issuer() string {
	return strings.TrimRight(h.cfg.ExternalURL, "/") + Prefix
}

// =============================================================================
// Discovery
// =============================================================================

type asMetadata struct {
	Issuer                            string   `json:"issuer"`
	AuthorizationEndpoint             string   `json:"authorization_endpoint"`
	TokenEndpoint                     string   `json:"token_endpoint"`
	UserinfoEndpoint                  string   `json:"userinfo_endpoint"`
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
// /authorize — PKCE optional (honored when present)
// =============================================================================

func (h *Handler) handleAuthorize(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	q := r.URL.Query()

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
	// PKCE is optional in oauth2: a request without code_challenge is
	// accepted (legacy 2.0 path). When supplied, only S256 is honored —
	// `plain` is rejected because a verifier check that's just string
	// equality adds no security value.
	if codeChallenge != "" && codeChallengeMethod != "" && codeChallengeMethod != "S256" {
		oauthError(w, http.StatusBadRequest, "invalid_request", "code_challenge_method must be S256 when supplied")
		return
	}
	if codeChallenge != "" && codeChallengeMethod == "" {
		// RFC 7636 §4.3 default: "plain" when omitted. The dev-idp doesn't
		// honor "plain" (see above) so default to S256 for the caller.
		codeChallengeMethod = "S256"
	}

	target, err := url.Parse(redirectURI)
	if err != nil {
		oauthError(w, http.StatusBadRequest, "invalid_request", "redirect_uri is not a valid URL")
		return
	}

	userID, err := h.resolveCurrentUserID(ctx)
	if err != nil {
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
		CodeChallenge:       pgtype.Text{String: codeChallenge, Valid: codeChallenge != ""},
		CodeChallengeMethod: pgtype.Text{String: codeChallengeMethod, Valid: codeChallenge != ""},
		Scope:               pgtype.Text{String: scope, Valid: scope != ""},
		ExpiresAt:           pgtype.Timestamptz{Time: time.Now().Add(authCodeLifetime), Valid: true, InfinityModifier: pgtype.Finite},
	}); err != nil {
		h.logger.ErrorContext(ctx, "create auth code", attr.SlogError(err))
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
// /token — authorization_code (PKCE-conditional) + refresh_token (no rotation)
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
	if code == "" {
		oauthError(w, http.StatusBadRequest, "invalid_request", "code is required")
		return
	}

	queries := repo.New(h.db)
	stored, err := queries.ConsumeAuthCode(ctx, repo.ConsumeAuthCodeParams{Code: code, Mode: Mode})
	if err != nil {
		oauthError(w, http.StatusBadRequest, "invalid_grant", "auth code is unknown, consumed, or expired")
		return
	}

	// PKCE is conditional on whether /authorize stored a challenge. When
	// present, the verifier MUST match. When absent, the verifier (if
	// any) is ignored — the legacy 2.0 path.
	if stored.CodeChallenge.Valid {
		if verifier == "" {
			oauthError(w, http.StatusBadRequest, "invalid_grant", "code_verifier is required for codes minted with PKCE")
			return
		}
		if !validatePKCES256(verifier, stored.CodeChallenge.String) {
			oauthError(w, http.StatusBadRequest, "invalid_grant", "PKCE verifier does not match challenge")
			return
		}
	}

	if clientID != "" && clientID != stored.ClientID {
		oauthError(w, http.StatusBadRequest, "invalid_grant", "client_id does not match the auth code")
		return
	}

	scope := pgTextOrEmpty(stored.Scope)
	tokens, err := h.issueTokenSet(ctx, queries, stored.UserID, stored.ClientID, scope)
	if err != nil {
		h.logger.ErrorContext(ctx, "issue token set", attr.SlogError(err))
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

	// OAuth 2.0 (vs 2.1) doesn't mandate refresh-token rotation. We mint
	// a new access token but reuse the refresh token so the caller can
	// keep using it.
	scope := pgTextOrEmpty(stored.Scope)
	access := randomHex(32)
	if _, err := queries.CreateToken(ctx, repo.CreateTokenParams{
		Token:     access,
		Mode:      Mode,
		UserID:    stored.UserID,
		ClientID:  stored.ClientID,
		Kind:      "access_token",
		Scope:     pgtype.Text{String: scope, Valid: scope != ""},
		ExpiresAt: pgtype.Timestamptz{Time: time.Now().Add(accessTokenLifetime), Valid: true, InfinityModifier: pgtype.Finite},
	}); err != nil {
		h.logger.ErrorContext(ctx, "issue access_token on refresh", attr.SlogError(err))
		oauthError(w, http.StatusInternalServerError, "server_error", "failed to issue access token")
		return
	}

	resp := tokenResponse{
		AccessToken:  access,
		TokenType:    "Bearer",
		ExpiresIn:    int(accessTokenLifetime.Seconds()),
		RefreshToken: refreshToken, // reused, not rotated
		Scope:        scope,
		IDToken:      "",
	}
	writeJSON(w, http.StatusOK, resp)
}

func (h *Handler) issueTokenSet(ctx context.Context, queries *repo.Queries, userID uuid.UUID, clientID, scope string) (tokenResponse, error) {
	access := randomHex(32)
	refresh := randomHex(32)

	if _, err := queries.CreateToken(ctx, repo.CreateTokenParams{
		Token:     access,
		Mode:      Mode,
		UserID:    userID,
		ClientID:  clientID,
		Kind:      "access_token",
		Scope:     pgtype.Text{String: scope, Valid: scope != ""},
		ExpiresAt: pgtype.Timestamptz{Time: time.Now().Add(accessTokenLifetime), Valid: true, InfinityModifier: pgtype.Finite},
	}); err != nil {
		return tokenResponse{}, fmt.Errorf("insert access_token: %w", err)
	}
	if _, err := queries.CreateToken(ctx, repo.CreateTokenParams{
		Token:     refresh,
		Mode:      Mode,
		UserID:    userID,
		ClientID:  clientID,
		Kind:      "refresh_token",
		Scope:     pgtype.Text{String: scope, Valid: scope != ""},
		ExpiresAt: pgtype.Timestamptz{Time: time.Now().Add(refreshTokenLifetime), Valid: true, InfinityModifier: pgtype.Finite},
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
			Scope:     pgtype.Text{String: scope, Valid: true},
			ExpiresAt: pgtype.Timestamptz{Time: time.Now().Add(idTokenLifetime), Valid: true, InfinityModifier: pgtype.Finite},
		}); err != nil {
			return tokenResponse{}, fmt.Errorf("insert id_token: %w", err)
		}
		resp.IDToken = idToken
	}

	return resp, nil
}

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
		w.Header().Set("WWW-Authenticate", `Bearer realm="oauth2", error="invalid_token"`)
		oauthError(w, http.StatusUnauthorized, "invalid_token", "missing bearer token")
		return
	}

	queries := repo.New(h.db)
	stored, err := queries.GetActiveToken(ctx, repo.GetActiveTokenParams{Token: bearer, Mode: Mode})
	if err != nil || stored.Kind != "access_token" {
		w.Header().Set("WWW-Authenticate", `Bearer realm="oauth2", error="invalid_token"`)
		oauthError(w, http.StatusUnauthorized, "invalid_token", "bearer is unknown, revoked, expired, or not an access token")
		return
	}

	user, err := queries.GetUser(ctx, stored.UserID)
	if err != nil {
		h.logger.ErrorContext(ctx, "look up user for userinfo", attr.SlogError(err))
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
		h.logger.WarnContext(ctx, "revoke parse form", attr.SlogError(err))
		w.WriteHeader(http.StatusOK)
		return
	}
	token := r.Form.Get("token")
	if token != "" {
		if err := repo.New(h.db).RevokeToken(ctx, repo.RevokeTokenParams{Token: token, Mode: Mode}); err != nil {
			h.logger.WarnContext(ctx, "revoke token", attr.SlogError(err))
		}
	}
	w.WriteHeader(http.StatusOK)
}

// =============================================================================
// Helpers
// =============================================================================

var (
	errCurrentUserNotSet  = errors.New("no currentUser set for oauth2 mode (call /rpc/devIdp.setCurrentUser)")
	errCurrentUserMissing = errors.New("currentUser pointer references a missing user row")
)

func (h *Handler) resolveCurrentUserID(ctx context.Context) (uuid.UUID, error) {
	queries := repo.New(h.db)
	pointer, err := queries.GetCurrentUserPointer(ctx, Mode)
	if errors.Is(err, pgx.ErrNoRows) {
		return uuid.Nil, errCurrentUserNotSet
	}
	if err != nil {
		return uuid.Nil, fmt.Errorf("read currentUser pointer: %w", err)
	}
	id, err := uuid.Parse(pointer.SubjectRef)
	if err != nil {
		return uuid.Nil, fmt.Errorf("parse currentUser subject_ref: %w", err)
	}
	if _, err := queries.GetUser(ctx, id); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return uuid.Nil, errCurrentUserMissing
		}
		return uuid.Nil, fmt.Errorf("look up currentUser: %w", err)
	}
	return id, nil
}

func validatePKCES256(verifier, challenge string) bool {
	digest := sha256.Sum256([]byte(verifier))
	computed := base64.RawURLEncoding.EncodeToString(digest[:])
	return subtle.ConstantTimeCompare([]byte(computed), []byte(challenge)) == 1
}

func scopeContains(scope, want string) bool {
	for _, s := range strings.Fields(scope) {
		if s == want {
			return true
		}
	}
	return false
}

func bearerToken(r *http.Request) string {
	h := r.Header.Get("Authorization")
	if !strings.HasPrefix(h, "Bearer ") {
		return ""
	}
	return strings.TrimSpace(strings.TrimPrefix(h, "Bearer "))
}

func pgTextOrEmpty(t pgtype.Text) string {
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

func oauthError(w http.ResponseWriter, status int, code, description string) {
	writeJSON(w, status, map[string]string{
		"error":             code,
		"error_description": description,
	})
}
