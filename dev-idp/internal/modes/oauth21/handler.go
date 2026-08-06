// Package oauth21 implements the dev-idp's OAuth 2.1 authorization server:
// PKCE (S256), stateless DCR, and OIDC compliance. It backs both the
// `remote_session_issuer` rows used in remote-session tests and the authorize
// leg of dashboard login.
//
// It also doubles as the enterprise IdP for enterprise-managed
// authorization: the token
// endpoint mints ID-JAG grants under the RFC 8693 token-exchange grant (see
// idjag.go). The server that redeems those grants is a different issuer,
// internal/modes/resourceas.
//
// Identity resolution is non-interactive — every /authorize call resolves the
// currentUser and immediately redirects with the issued code. Dynamic client
// registration persists redirect_uris so tests can catch unregistered callback
// URLs; Config.LoginClientID names the one statically provisioned client that
// skips registration.
//
// PKCE is optional rather than mandatory. The dev-idp only ever serves
// localhost, and the first-party login client sends no challenge. A challenge
// that IS supplied is enforced through the token exchange.
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

	"github.com/speakeasy-api/gram/dev-idp/internal/cimd"
	"github.com/speakeasy-api/gram/dev-idp/internal/database/repo"
	"github.com/speakeasy-api/gram/dev-idp/internal/defaultuser"
	"github.com/speakeasy-api/gram/dev-idp/internal/ema"
	"github.com/speakeasy-api/gram/dev-idp/internal/keystore"
)

// Mode is the current_users slot this handler resolves its subject from.
// The local identity slot; the workos surface owns the other one.
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

	// cimdCacheTTL bounds how long a client metadata document is reused. Long
	// enough that a burst of requests is one fetch, short enough that
	// republishing a rotated key takes effect without a restart.
	cimdCacheTTL = 60 * time.Second
)

// Config carries the static configuration for the oauth2-1 mode.
type Config struct {
	// ExternalURL is the dev-idp's externally reachable base URL (no
	// trailing slash, no mode prefix). Used to build absolute URLs in
	// discovery documents and as the `iss` claim on issued id_tokens.
	ExternalURL string

	// LoginClientID is the statically provisioned first-party client the
	// Gram server logs in with (GRAM_IDP_CLIENT_ID). It never goes through
	// dynamic client registration, so /authorize skips the registered-client
	// and redirect_uri allowlist checks for it -- the server's callback URL
	// varies with the local port and scheme. Empty disables the exemption.
	LoginClientID string
}

// Handler serves the oauth2-1 mode's HTTP routes.
type Handler struct {
	cfg      Config
	tracer   trace.Tracer
	logger   *slog.Logger
	db       *sql.DB
	keystore *keystore.Keystore
	// cimd dereferences Client ID Metadata Document client_id URLs: on the
	// authorize leg to learn a client's redirect_uris, and on the mint leg to
	// learn the keys a CIMD client authenticates with.
	cimd *cimd.Resolver
}

func NewHandler(cfg Config, ks *keystore.Keystore, logger *slog.Logger, tracerProvider trace.TracerProvider, db *sql.DB) *Handler {
	return &Handler{
		cfg:      cfg,
		tracer:   tracerProvider.Tracer("github.com/speakeasy-api/gram/dev-idp/internal/modes/oauth21"),
		logger:   logger.With(slog.String("component", "devidp."+Mode)),
		db:       db,
		keystore: ks,
		cimd:     cimd.NewResolver(&http.Client{Timeout: 5 * time.Second}, cimdCacheTTL),
	}
}

// Handler returns the http.Handler that should be mounted under `Prefix`
// (use http.StripPrefix). All registered paths are relative to that
// prefix.
//
// Note: the RFC 8414 .well-known/oauth-authorization-server route is NOT
// mounted here — RFC 8414 places that document at the host root with the
// issuer's path component as a suffix, not under the issuer path. Wire it
// via RegisterRootRoutes on the outer (host-root) mux.
func (h *Handler) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /.well-known/openid-configuration", h.handleOIDCDiscovery)
	mux.Handle("GET /.well-known/jwks.json", h.keystore.JWKSHandler())
	mux.HandleFunc("POST /register", h.handleRegister)
	mux.HandleFunc("GET /authorize", h.handleAuthorize)
	mux.HandleFunc("POST /token", h.handleToken)
	mux.HandleFunc("GET /userinfo", h.handleUserinfo)
	mux.HandleFunc("POST /revoke", h.handleRevoke)
	return mux
}

// RegisterRootRoutes mounts the RFC 8414 .well-known/oauth-authorization-server
// route on the host-root mux. Per RFC 8414 §3, the well-known URI suffix is
// appended to the host and the issuer's path component is appended after,
// so an issuer of "<host>/oauth2-1" exposes its metadata at
// "<host>/.well-known/oauth-authorization-server/oauth2-1".
func (h *Handler) RegisterRootRoutes(mux *http.ServeMux) {
	mux.HandleFunc("GET /.well-known/oauth-authorization-server"+Prefix, h.handleASMetadata)
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
	// ClientIDMetadataDocumentSupported advertises the OAuth CIMD draft
	// (draft-ietf-oauth-client-id-metadata-document): this AS accepts a hosted
	// metadata-document URL as the client_id. The dev-idp dereferences it in
	// handleAuthorize instead of requiring DCR/pre-registration.
	ClientIDMetadataDocumentSupported bool `json:"client_id_metadata_document_supported"`

	// IdentityChainingRequestedTokenTypesSupported advertises which
	// `requested_token_type` values the token-exchange grant honors. Listing
	// the ID-JAG type is how a client learns this IdP can mint cross-app
	// access grants (draft-ietf-oauth-identity-assertion-authz-grant).
	IdentityChainingRequestedTokenTypesSupported []string `json:"identity_chaining_requested_token_types_supported"`
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
		GrantTypesSupported:               []string{"authorization_code", "refresh_token", ema.GrantTypeTokenExchange},
		CodeChallengeMethodsSupported:     []string{"S256"},
		TokenEndpointAuthMethodsSupported: []string{"client_secret_basic", "client_secret_post", "private_key_jwt", "none"},
		ScopesSupported:                   []string{"openid", "email", "profile"},
		ClientIDMetadataDocumentSupported: true,

		IdentityChainingRequestedTokenTypesSupported: []string{ema.TokenTypeIDJAG},
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
	// grant_types / response_types back.
	var body struct {
		RedirectURIs            []string `json:"redirect_uris"`
		GrantTypes              []string `json:"grant_types"`
		ResponseTypes           []string `json:"response_types"`
		TokenEndpointAuthMethod string   `json:"token_endpoint_auth_method"`
		// RotateRefreshTokens is a dev-idp extension to RFC 7591, not a real
		// DCR field. Nil (the common case) means rotate, matching OAuth 2.1.
		// Register with false to emulate an upstream that reuses refresh
		// tokens across /token calls.
		RotateRefreshTokens *bool `json:"rotate_refresh_tokens"`
	}
	_ = json.NewDecoder(r.Body).Decode(&body)

	rotateRefreshTokens := body.RotateRefreshTokens == nil || *body.RotateRefreshTokens

	clientID := "client_" + randomHex(16)
	clientSecret := "secret_" + randomHex(32)
	redirectURIs := body.RedirectURIs
	if redirectURIs == nil {
		redirectURIs = []string{}
	}
	rawRedirectURIs, err := json.Marshal(redirectURIs)
	if err != nil {
		h.logger.ErrorContext(r.Context(), "marshal registered redirect uris", slog.Any("error", err))
		oauthError(w, http.StatusInternalServerError, "server_error", "failed to register client")
		return
	}
	if _, err := repo.New(h.db).CreateOAuthClient(r.Context(), repo.CreateOAuthClientParams{
		ClientID:            clientID,
		ClientSecret:        clientSecret,
		RedirectUris:        string(rawRedirectURIs),
		RotateRefreshTokens: rotateRefreshTokens,
	}); err != nil {
		h.logger.ErrorContext(r.Context(), "create oauth client", slog.Any("error", err))
		oauthError(w, http.StatusInternalServerError, "server_error", "failed to register client")
		return
	}

	writeJSON(w, http.StatusCreated, dcrResponse{
		ClientID:                clientID,
		ClientSecret:            clientSecret,
		ClientIDIssuedAt:        time.Now().Unix(),
		ClientSecretExpiresAt:   0, // 0 = never expires (RFC 7591)
		RedirectURIs:            redirectURIs,
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
	// PKCE is optional: the dev-idp only ever serves localhost traffic, and
	// the first-party login client (see Config.LoginClientID) does not send a
	// challenge. When one IS supplied it is enforced end to end -- only S256
	// is honored, because a `plain` verifier check is just string equality.
	if codeChallenge != "" && codeChallengeMethod != "" && codeChallengeMethod != "S256" {
		oauthError(w, http.StatusBadRequest, "invalid_request", "code_challenge_method must be S256 when supplied")
		return
	}
	if codeChallenge != "" && codeChallengeMethod == "" {
		// RFC 7636 §4.3 defaults the method to "plain" when omitted. The
		// dev-idp doesn't honor "plain" (see above) so default to S256.
		codeChallengeMethod = "S256"
	}

	target, err := url.Parse(redirectURI)
	if err != nil {
		oauthError(w, http.StatusBadRequest, "invalid_request", "redirect_uri is not a valid URL")
		return
	}

	// A CIMD client_id is a URL to a hosted client metadata document;
	// dereference and validate it instead of requiring a pre-registered (DCR)
	// client. Any other client_id resolves against the registered-clients table.
	var allowedRedirectURIs []string
	switch {
	case h.cfg.LoginClientID != "" && clientID == h.cfg.LoginClientID:
		// Statically provisioned first-party client: nothing to look up and
		// no registered redirect_uri to match against. The Gram server's
		// callback host and port vary per worktree, so there is no stable
		// value to pin. This accepts any redirect_uri for this one client id,
		// which in a production AS would be an open redirect -- acceptable
		// only because dev-idp is dev-only, binds to localhost, and resolves
		// identity non-interactively (there is no session to phish; a caller
		// who can reach this endpoint already has local access).
		allowedRedirectURIs = []string{redirectURI}
	case cimd.IsClientID(clientID):
		doc, derr := h.cimd.Document(ctx, clientID)
		switch {
		case errors.Is(derr, cimd.ErrClientIDMismatch):
			oauthError(w, http.StatusBadRequest, "invalid_client", cimd.ErrClientIDMismatch.Error())
			return
		case derr != nil:
			h.logger.WarnContext(ctx, "fetch client metadata document", slog.Any("error", derr))
			oauthError(w, http.StatusBadRequest, "invalid_client", "could not fetch client metadata document")
			return
		}
		allowedRedirectURIs = doc.RedirectURIs
	default:
		client, cerr := repo.New(h.db).GetOAuthClient(ctx, clientID)
		if cerr != nil {
			if errors.Is(cerr, sql.ErrNoRows) {
				oauthError(w, http.StatusBadRequest, "invalid_client", "client_id is not registered")
				return
			}
			h.logger.ErrorContext(ctx, "load oauth client", slog.Any("error", cerr))
			oauthError(w, http.StatusInternalServerError, "server_error", "failed to load client")
			return
		}
		if uerr := json.Unmarshal([]byte(client.RedirectUris), &allowedRedirectURIs); uerr != nil {
			h.logger.ErrorContext(ctx, "decode registered redirect uris", slog.Any("error", uerr))
			oauthError(w, http.StatusInternalServerError, "server_error", "failed to load client")
			return
		}
	}
	if !slices.Contains(allowedRedirectURIs, redirectURI) {
		oauthError(w, http.StatusBadRequest, "invalid_request", "redirect_uri is not registered for this client")
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
		UserID:              userID,
		ClientID:            clientID,
		RedirectUri:         redirectURI,
		CodeChallenge:       sql.NullString{String: codeChallenge, Valid: codeChallenge != ""},
		CodeChallengeMethod: sql.NullString{String: codeChallengeMethod, Valid: codeChallenge != ""},
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
	case ema.GrantTypeTokenExchange:
		h.handleTokenExchangeGrant(ctx, w, r)
	case "":
		oauthError(w, http.StatusBadRequest, "invalid_request", "grant_type is required")
	default:
		oauthError(w, http.StatusBadRequest, "unsupported_grant_type", "only authorization_code, refresh_token, and token-exchange are supported")
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
	stored, err := queries.ConsumeAuthCode(ctx, repo.ConsumeAuthCodeParams{Code: code, Ts: time.Now()})
	if err != nil {
		// Includes ErrNoRows (unknown / consumed / expired). Don't leak which.
		oauthError(w, http.StatusBadRequest, "invalid_grant", "auth code is unknown, consumed, or expired")
		return
	}

	// PKCE is enforced exactly when /authorize accepted a challenge. A code
	// minted with one cannot be redeemed without a matching verifier; a code
	// minted without one never accepts a verifier it can't check.
	if stored.CodeChallenge.Valid {
		if verifier == "" {
			oauthError(w, http.StatusBadRequest, "invalid_request", "code_verifier is required for a code issued with PKCE")
			return
		}
		if !validatePKCES256(verifier, stored.CodeChallenge.String) {
			oauthError(w, http.StatusBadRequest, "invalid_grant", "PKCE verifier does not match challenge")
			return
		}
	}

	// Per §5.2 client_id is recorded for inspection only. We cross-check
	// it to give the caller a useful error if they typo it on /token vs
	// /authorize, but only when the caller bothered to send it.
	if clientID != "" && clientID != stored.ClientID {
		oauthError(w, http.StatusBadRequest, "invalid_grant", "client_id does not match the auth code")
		return
	}

	scope := pgTextOrEmpty(stored.Scope)
	tokens, err := h.issueTokenSet(ctx, queries, stored.UserID, stored.ClientID, scope, "")
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
	stored, err := queries.GetActiveToken(ctx, repo.GetActiveTokenParams{Token: refreshToken, Ts: time.Now()})
	if err != nil {
		oauthError(w, http.StatusBadRequest, "invalid_grant", "refresh token is unknown, revoked, or expired")
		return
	}
	if stored.Kind != "refresh_token" {
		oauthError(w, http.StatusBadRequest, "invalid_grant", "presented token is not a refresh token")
		return
	}

	// OAuth 2.1 recommends rotating refresh tokens on use, which is the
	// default. A client that registered with rotate_refresh_tokens=false
	// keeps its refresh token across calls, emulating upstreams that don't
	// rotate. Unregistered clients (CIMD, the login client) rotate.
	rotate := true
	if client, cerr := queries.GetOAuthClient(ctx, stored.ClientID); cerr == nil {
		rotate = client.RotateRefreshTokens
	} else if !errors.Is(cerr, sql.ErrNoRows) {
		h.logger.ErrorContext(ctx, "load oauth client for refresh", slog.Any("error", cerr))
		oauthError(w, http.StatusInternalServerError, "server_error", "failed to load client")
		return
	}

	reuseRefresh := refreshToken
	if rotate {
		reuseRefresh = ""
		if err := queries.RevokeToken(ctx, repo.RevokeTokenParams{
			Ts:    sql.NullTime{Time: time.Now(), Valid: true},
			Token: refreshToken,
		}); err != nil {
			h.logger.ErrorContext(ctx, "revoke rotated refresh token", slog.Any("error", err))
			oauthError(w, http.StatusInternalServerError, "server_error", "failed to rotate refresh token")
			return
		}
	}

	tokens, err := h.issueTokenSet(ctx, queries, stored.UserID, stored.ClientID, pgTextOrEmpty(stored.Scope), reuseRefresh)
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
//
// reuseRefresh, when non-empty, is handed back verbatim instead of minting a
// new refresh token -- the non-rotating path, where the caller's existing
// token stays live.
func (h *Handler) issueTokenSet(ctx context.Context, queries *repo.Queries, userID uuid.UUID, clientID, scope, reuseRefresh string) (tokenResponse, error) {
	access := randomHex(32)
	refresh := reuseRefresh

	if _, err := queries.CreateToken(ctx, repo.CreateTokenParams{
		Token:     access,
		UserID:    userID,
		ClientID:  clientID,
		Kind:      "access_token",
		Scope:     sql.NullString{String: scope, Valid: scope != ""},
		ExpiresAt: time.Now().Add(accessTokenLifetime),
	}); err != nil {
		return tokenResponse{}, fmt.Errorf("insert access_token: %w", err)
	}
	if refresh == "" {
		refresh = randomHex(32)
		if _, err := queries.CreateToken(ctx, repo.CreateTokenParams{
			Token:     refresh,
			UserID:    userID,
			ClientID:  clientID,
			Kind:      "refresh_token",
			Scope:     sql.NullString{String: scope, Valid: scope != ""},
			ExpiresAt: time.Now().Add(refreshTokenLifetime),
		}); err != nil {
			return tokenResponse{}, fmt.Errorf("insert refresh_token: %w", err)
		}
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
	stored, err := queries.GetActiveToken(ctx, repo.GetActiveTokenParams{Token: bearer, Ts: time.Now()})
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
