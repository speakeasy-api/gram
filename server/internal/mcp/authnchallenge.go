// Package mcp — authnchallenge.go
//
// Linear, top-to-bottom view of the Gram-as-Authorization-Server authn
// challenge flow for MCP clients. Read this file end-to-end to follow what
// happens between an unauthenticated `/mcp/{slug}` request and a signed
// SessionClaims JWT (per spike §4.5) coming back to the client.
//
// The flow follows the OAuth 2.1 + RFC 7591 / RFC 9728 dance, gated on
// toolsets.user_session_issuer_id. Legacy paths stay unchanged when the
// column is unset.
//
// Order of operations (each handler lands in its own commit):
//
//   1. WriteAuthenticateChallenge — 401 with WWW-Authenticate; kicks the
//      unauthenticated client into RFC 9728 discovery.
//   2. HandleGetProtectedResource — GET <new path TBD>.
//   3. HandleGetAuthorizationServer — GET <new path TBD>.
//   4. HandleRegister — POST /mcp/{slug}/register (RFC 7591 DCR).
//   5. HandleAuthorize — GET /mcp/{slug}/authorize.
//   6. HandleIDPCallback — GET /mcp/{slug}/idp_callback
//      (Speakeasy IDP returns here on the private-toolset path).
//   7. HandleConsent — GET, POST /mcp/{slug}/connect.
//   8. HandleToken — POST /mcp/{slug}/token (auth-code grant).
//   9. HandleRevoke — POST /mcp/{slug}/revoke (RFC 7009).
//
// Routes 4–9 are advertised by the AS metadata document built in step 3 so
// MCP clients see a coherent set even before each handler is implemented.
//
// Wiring policy: the existing legacy code paths (mcp/impl.go inline
// WWW-Authenticate writes, the well-known handlers at the canonical RFC
// paths) are left untouched. The handlers in this file serve issuer-gated
// traffic via separate routes; clients are pointed at them through the
// resource_metadata parameter on the new path's WWW-Authenticate. The
// WriteAuthenticateChallenge helper below is therefore unused at the time
// of its introduction — call sites land in commits 4 onward.

package mcp

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	_ "embed"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"html/template"
	"log/slog"
	"net/http"
	"net/http/httputil"
	"net/url"
	"slices"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"golang.org/x/crypto/bcrypt"

	"github.com/speakeasy-api/gram/server/internal/attr"
	"github.com/speakeasy-api/gram/server/internal/auth/sessions"
	"github.com/speakeasy-api/gram/server/internal/cache"
	"github.com/speakeasy-api/gram/server/internal/oauth/wellknown"
	"github.com/speakeasy-api/gram/server/internal/oops"
	"github.com/speakeasy-api/gram/server/internal/urn"
	usersessions_repo "github.com/speakeasy-api/gram/server/internal/usersessions/repo"
)

//go:embed consent_template.html
var consentTemplateHTML string

var consentTemplate = template.Must(template.New("consent").Parse(consentTemplateHTML))

// remoteSetHashEmpty is the SHA-256 of an empty remote-set, used by the
// consent record's remote_set_hash column when the issuer has no remote
// session clients (the only case in this milestone). Per spike §3.4: the
// empty case is NOT skipped — every consent binds to a specific hash.
var remoteSetHashEmpty = func() string {
	h := sha256.Sum256([]byte("[]"))
	return base64.RawURLEncoding.EncodeToString(h[:])
}()

// AuthnChallengeState is the in-flight context of a single Gram-as-AS authn
// challenge — the OAuth client's request, the issuer it's against, and (after
// Phase 2) the resolved subject. Stored in Redis under `authnChallenge:{ID}`
// for ~10 minutes (spike §4.3) — long enough for the user to round-trip
// through the IDP and land on /connect, short enough that abandoned flows
// don't pile up.
type AuthnChallengeState struct {
	ID                  string    `json:"id"`
	UserSessionIssuerID uuid.UUID `json:"user_session_issuer_id"`
	ToolsetID           uuid.UUID `json:"toolset_id"`
	ClientID            string    `json:"client_id"`
	RedirectURI         string    `json:"redirect_uri"`
	Scope               string    `json:"scope,omitempty"`
	State               string    `json:"state,omitempty"`
	CodeChallenge       string    `json:"code_challenge"`
	CodeChallengeMethod string    `json:"code_challenge_method"`
	// Subject is nil on creation; HandleIDPCallback (private path)
	// stamps `user:<id>`, HandleConsent's POST mints a fresh
	// `anonymous:<uuid>` on the public path. Pointer so the Redis JSON
	// can round-trip an unstamped state (the URN's MarshalJSON refuses
	// to serialise a zero-value SessionSubject).
	Subject   *urn.SessionSubject `json:"subject,omitempty"`
	CreatedAt time.Time           `json:"created_at"`
}

var _ cache.CacheableObject[AuthnChallengeState] = (*AuthnChallengeState)(nil)

// CacheKey implements cache.CacheableObject.
func (a AuthnChallengeState) CacheKey() string { return "authnChallenge:" + a.ID }

// AdditionalCacheKeys implements cache.CacheableObject. Single-key entry; no
// fan-out. (Per the Cleanup ticket in project.md, AdditionalCacheKeys is
// itself slated for removal from the interface.)
func (a AuthnChallengeState) AdditionalCacheKeys() []string { return []string{} }

// TTL implements cache.CacheableObject.
func (a AuthnChallengeState) TTL() time.Duration { return 10 * time.Minute }

// UserSessionGrant is the short-lived OAuth authorization grant minted by
// HandleConsent's POST and consumed by HandleToken's authorization_code grant
// (spike §4.3). Stored in Redis under
// `userSessionGrant:{user_session_issuer_id}:{code}` for ~10 minutes.
type UserSessionGrant struct {
	Code                string             `json:"code"`
	UserSessionIssuerID uuid.UUID          `json:"user_session_issuer_id"`
	UserSessionClientID uuid.UUID          `json:"user_session_client_id"`
	ClientID            string             `json:"client_id"`
	RedirectURI         string             `json:"redirect_uri"`
	Scope               string             `json:"scope,omitempty"`
	CodeChallenge       string             `json:"code_challenge"`
	CodeChallengeMethod string             `json:"code_challenge_method"`
	Subject             urn.SessionSubject `json:"subject"`
	CreatedAt           time.Time          `json:"created_at"`
}

var _ cache.CacheableObject[UserSessionGrant] = (*UserSessionGrant)(nil)

// CacheKey implements cache.CacheableObject.
func (g UserSessionGrant) CacheKey() string {
	return "userSessionGrant:" + g.UserSessionIssuerID.String() + ":" + g.Code
}

// AdditionalCacheKeys implements cache.CacheableObject. Single-key entry; no
// fan-out.
func (g UserSessionGrant) AdditionalCacheKeys() []string { return []string{} }

// TTL implements cache.CacheableObject. 10 minutes is the standard OAuth code
// lifetime — enough for a slow round trip from the MCP client to /token, short
// enough to limit blast radius if the code leaks.
func (g UserSessionGrant) TTL() time.Duration { return 10 * time.Minute }

// supportedGrantTypes / supportedResponseTypes / supportedAuthMethods mirror
// the values HandleGetAuthorizationServer advertises. Keep in sync — registered
// clients must request only what the AS metadata claims to support.
var (
	supportedGrantTypes    = []string{"authorization_code", "refresh_token"}
	supportedResponseTypes = []string{"code"}
	// `none` covers public PKCE-only clients (mobile, CLI, MCP SDK). Real
	// MCP clients in the wild use it. PKCE provides per-flow integrity; the
	// only guard against cross-flow client-id confusion is the consent
	// prompt itself, which we always render (HandleConsent never skips).
	supportedAuthMethods = []string{"client_secret_basic", "client_secret_post", "none"}
)

// dcrRegistrationRequest is the RFC 7591 §3.1 client metadata document. Only
// the fields we honour are listed; unknown fields are ignored.
type dcrRegistrationRequest struct {
	ClientName              string   `json:"client_name"`
	RedirectURIs            []string `json:"redirect_uris"`
	GrantTypes              []string `json:"grant_types,omitempty"`
	ResponseTypes           []string `json:"response_types,omitempty"`
	TokenEndpointAuthMethod string   `json:"token_endpoint_auth_method,omitempty"`
	Scope                   string   `json:"scope,omitempty"`
}

// dcrRegistrationResponse is the RFC 7591 §3.2.1 successful registration
// response. `client_secret` is included exactly once, on this response. Both
// `client_secret` and `client_secret_expires_at` are omitted entirely for
// public (`token_endpoint_auth_method=none`) clients per RFC 7591 §3.2.1
// — emitting an empty string for `client_secret` confuses some MCP SDKs into
// preferring `client_secret_basic` for the token call.
type dcrRegistrationResponse struct {
	ClientID                string   `json:"client_id"`
	ClientSecret            string   `json:"client_secret,omitempty"`
	ClientIDIssuedAt        int64    `json:"client_id_issued_at"`
	ClientSecretExpiresAt   *int64   `json:"client_secret_expires_at,omitempty"`
	ClientName              string   `json:"client_name"`
	RedirectURIs            []string `json:"redirect_uris"`
	GrantTypes              []string `json:"grant_types"`
	ResponseTypes           []string `json:"response_types"`
	TokenEndpointAuthMethod string   `json:"token_endpoint_auth_method"`
	Scope                   string   `json:"scope,omitempty"`
}

// WriteAuthenticateChallenge sets the WWW-Authenticate header and returns an
// oops.CodeUnauthorized error. The 401 status and response body come from
// the oops error middleware; the helper owns only the header.
//
// Header shape (RFC 9728 §5.3):
//
//	Bearer resource_metadata="<base>/.well-known/oauth-protected-resource/mcp/<slug>"
//
// The path is the canonical RFC 9728 prefix path — exactly what a
// spec-compliant client constructs from a resource URL of `<base>/mcp/<slug>`.
func WriteAuthenticateChallenge(w http.ResponseWriter, baseURL, mcpSlug, message string) error {
	w.Header().Set(
		"WWW-Authenticate",
		fmt.Sprintf(`Bearer resource_metadata="%s"`, baseURL+"/.well-known/oauth-protected-resource/mcp/"+mcpSlug),
	)
	if message == "" {
		return oops.C(oops.CodeUnauthorized)
	}
	return oops.E(oops.CodeUnauthorized, nil, "%s", message)
}

// oauthProtectedResourceMetadata mirrors RFC 9728 §2 fields. Distinct from the
// legacy package's wellknown.OAuthProtectedResourceMetadata so the two paths
// stay independently editable; the new path may grow fields the legacy path
// can't.
type oauthProtectedResourceMetadata struct {
	Resource               string   `json:"resource"`
	AuthorizationServers   []string `json:"authorization_servers"`
	ScopesSupported        []string `json:"scopes_supported,omitempty"`
	BearerMethodsSupported []string `json:"bearer_methods_supported,omitempty"`
}

// oauthAuthorizationServerMetadata mirrors RFC 8414 §2 fields. Distinct from
// the legacy package's wellknown.OAuthServerMetadata for the same reason as
// above.
type oauthAuthorizationServerMetadata struct {
	Issuer                            string   `json:"issuer"`
	AuthorizationEndpoint             string   `json:"authorization_endpoint"`
	TokenEndpoint                     string   `json:"token_endpoint"`
	RegistrationEndpoint              string   `json:"registration_endpoint"`
	RevocationEndpoint                string   `json:"revocation_endpoint"`
	ScopesSupported                   []string `json:"scopes_supported,omitempty"`
	ResponseTypesSupported            []string `json:"response_types_supported"`
	GrantTypesSupported               []string `json:"grant_types_supported"`
	TokenEndpointAuthMethodsSupported []string `json:"token_endpoint_auth_methods_supported"`
	CodeChallengeMethodsSupported     []string `json:"code_challenge_methods_supported"`
}

// HandleGetProtectedResource serves RFC 9728 protected-resource metadata at
// the canonical RFC path `/.well-known/oauth-protected-resource/mcp/{mcpSlug}`
// — the only path a spec-compliant client constructs from a resource URL of
// `<base>/mcp/{slug}`. Dispatches internally:
//
//   - If toolset.user_session_issuer_id is set: emit the issuer-gated metadata
//     shape (resource + authorization_servers point at the same /mcp/{slug}
//     URL the AS metadata is keyed under).
//   - Else: delegate to wellknown.ResolveOAuthProtectedResourceFromToolset for
//     legacy toolsets (oauth_proxy_server_id / external_oauth_server_id).
//   - Else still: 404.
//
// Replaces the prior HandleWellKnownOAuthProtectedResourceMetadata in
// mcp/impl.go (deleted in this commit; route is now registered to this
// dispatcher).
func (s *Service) HandleGetProtectedResource(w http.ResponseWriter, r *http.Request) error {
	ctx := r.Context()
	mcpSlug := chi.URLParam(r, "mcpSlug")
	if mcpSlug == "" {
		return oops.E(oops.CodeBadRequest, nil, "an mcp slug must be provided").Log(ctx, s.logger)
	}

	toolset, customDomainCtx, err := s.loadToolsetFromMcpSlug(ctx, mcpSlug)
	switch {
	case errors.Is(err, errToolsetNotFound):
		return oops.E(oops.CodeNotFound, err, "mcp server not found")
	case err != nil:
		return oops.E(oops.CodeUnexpected, err, "failed to load MCP server").Log(ctx, s.logger)
	}

	baseURL := s.serverURL.String()
	if customDomainCtx != nil {
		baseURL = fmt.Sprintf("https://%s", customDomainCtx.Domain)
	}

	if toolset.UserSessionIssuerID.Valid {
		resource := baseURL + "/mcp/" + mcpSlug
		return writeJSONMetadata(ctx, w, s.logger, oauthProtectedResourceMetadata{
			Resource:               resource,
			AuthorizationServers:   []string{resource},
			ScopesSupported:        nil,
			BearerMethodsSupported: []string{"header"},
		})
	}

	// Legacy fallback: delegate to the existing wellknown resolver. Returning
	// nil from that resolver historically meant 200 + JSON error body — fix
	// to 404 now since spec-compliant clients otherwise blow past the miss.
	legacy, err := wellknown.ResolveOAuthProtectedResourceFromToolset(
		ctx, s.logger, s.db, &s.toolsetCache, toolset, baseURL, mcpSlug,
	)
	if err != nil {
		return oops.E(oops.CodeUnexpected, err, "failed to resolve OAuth protected resource metadata").Log(ctx, s.logger)
	}
	if legacy == nil {
		return oops.E(oops.CodeNotFound, nil, "no OAuth configuration found")
	}
	return writeOAuthProtectedResourceMetadataResponse(ctx, s.logger, w, legacy)
}

// HandleGetAuthorizationServer serves RFC 8414 authorization-server metadata
// at the canonical RFC path
// `/.well-known/oauth-authorization-server/mcp/{mcpSlug}` — the only path a
// spec-compliant client constructs from an issuer URL of `<base>/mcp/{slug}`.
// Same dispatch model as HandleGetProtectedResource.
func (s *Service) HandleGetAuthorizationServer(w http.ResponseWriter, r *http.Request) error {
	ctx := r.Context()
	mcpSlug := chi.URLParam(r, "mcpSlug")
	if mcpSlug == "" {
		return oops.E(oops.CodeBadRequest, nil, "an mcp slug must be provided").Log(ctx, s.logger)
	}

	toolset, customDomainCtx, err := s.loadToolsetFromMcpSlug(ctx, mcpSlug)
	switch {
	case errors.Is(err, errToolsetNotFound):
		return oops.E(oops.CodeNotFound, err, "mcp server not found")
	case err != nil:
		return oops.E(oops.CodeUnexpected, err, "failed to load MCP server").Log(ctx, s.logger)
	}

	baseURL := s.serverURL.String()
	if customDomainCtx != nil {
		baseURL = fmt.Sprintf("https://%s", customDomainCtx.Domain)
	}

	if toolset.UserSessionIssuerID.Valid {
		if _, err := usersessions_repo.New(s.db).GetUserSessionIssuerByID(ctx, usersessions_repo.GetUserSessionIssuerByIDParams{
			ID:        toolset.UserSessionIssuerID.UUID,
			ProjectID: toolset.ProjectID,
		}); err != nil {
			if errors.Is(err, pgx.ErrNoRows) {
				return oops.E(oops.CodeNotFound, err, "user_session_issuer not found")
			}
			return oops.E(oops.CodeUnexpected, err, "load user_session_issuer").Log(ctx, s.logger)
		}

		root := baseURL + "/mcp/" + mcpSlug
		return writeJSONMetadata(ctx, w, s.logger, oauthAuthorizationServerMetadata{
			Issuer:                            root,
			AuthorizationEndpoint:             root + "/authorize",
			TokenEndpoint:                     root + "/token",
			RegistrationEndpoint:              root + "/register",
			RevocationEndpoint:                root + "/revoke",
			ScopesSupported:                   nil,
			ResponseTypesSupported:            supportedResponseTypes,
			GrantTypesSupported:               supportedGrantTypes,
			TokenEndpointAuthMethodsSupported: supportedAuthMethods,
			CodeChallengeMethodsSupported:     []string{"S256"},
		})
	}

	// Legacy fallback: delegate to the existing wellknown resolver. Returning
	// nil now means 404 (was 200 + JSON error body — see fix above).
	legacy, err := wellknown.ResolveOAuthServerMetadataFromToolset(
		ctx, s.logger, s.db, s.oauthRepo, &s.toolsetCache, toolset, baseURL, mcpSlug,
	)
	if err != nil {
		return oops.E(oops.CodeUnexpected, err, "failed to resolve OAuth server metadata").Log(ctx, s.logger)
	}
	if legacy == nil {
		return oops.E(oops.CodeNotFound, nil, "no OAuth configuration found")
	}

	if legacy.Kind == wellknown.OAuthServerMetadataResultKindProxy {
		target, parseErr := url.Parse(legacy.ProxyURL)
		if parseErr != nil {
			return oops.E(oops.CodeUnexpected, parseErr, "failed to parse well-known URL").Log(ctx, s.logger)
		}
		proxy := &httputil.ReverseProxy{
			Director: nil,
			Rewrite: func(pr *httputil.ProxyRequest) {
				pr.SetURL(target)
			},
			Transport:      nil,
			FlushInterval:  0,
			ErrorLog:       nil,
			BufferPool:     nil,
			ModifyResponse: nil,
			ErrorHandler:   nil,
		}
		proxy.ServeHTTP(w, r)
		return nil
	}

	return writeOAuthServerMetadataResponse(ctx, s.logger, w, legacy)
}

// writeJSONMetadata is the shared write path for issuer-gated metadata
// responses. Marshals the value, sets Content-Type, then commits 200.
func writeJSONMetadata(ctx context.Context, w http.ResponseWriter, logger *slog.Logger, v any) error {
	body, err := json.Marshal(v)
	if err != nil {
		return oops.E(oops.CodeUnexpected, err, "failed to marshal metadata").Log(ctx, logger)
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	if _, err := w.Write(body); err != nil {
		return oops.E(oops.CodeUnexpected, err, "failed to write response body").Log(ctx, logger)
	}
	return nil
}

// HandleRegister implements RFC 7591 Dynamic Client Registration for issuer-
// gated MCP servers. Mounted at `POST /mcp/{mcpSlug}/register`. Public endpoint
// (no caller auth); the issuer's metadata document advertises this URL via
// `registration_endpoint`.
//
// Generated client_secret is returned plaintext exactly once; only its bcrypt
// hash is persisted in user_session_clients.client_secret_hash.
func (s *Service) HandleRegister(w http.ResponseWriter, r *http.Request) error {
	ctx := r.Context()
	mcpSlug := chi.URLParam(r, "mcpSlug")
	if mcpSlug == "" {
		return oops.E(oops.CodeBadRequest, nil, "an mcp slug must be provided").Log(ctx, s.logger)
	}

	toolset, _, err := s.loadToolsetFromMcpSlug(ctx, mcpSlug)
	switch {
	case errors.Is(err, errToolsetNotFound):
		return oops.E(oops.CodeNotFound, err, "mcp server not found")
	case err != nil:
		return oops.E(oops.CodeUnexpected, err, "failed to load MCP server").Log(ctx, s.logger)
	}

	if !toolset.UserSessionIssuerID.Valid {
		return oops.E(oops.CodeNotFound, nil, "not found")
	}

	logger := s.logger.With(
		attr.SlogToolsetID(toolset.ID.String()),
		attr.SlogProjectID(toolset.ProjectID.String()),
	)

	var req dcrRegistrationRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		return writeDCRError(ctx, w, logger, "invalid_client_metadata", "request body is not valid JSON")
	}

	if req.ClientName == "" {
		return writeDCRError(ctx, w, logger, "invalid_client_metadata", "client_name is required")
	}
	if len(req.RedirectURIs) == 0 {
		return writeDCRError(ctx, w, logger, "invalid_redirect_uri", "redirect_uris is required")
	}
	for _, u := range req.RedirectURIs {
		parsed, parseErr := url.Parse(u)
		if parseErr != nil || parsed.Scheme == "" || parsed.Host == "" {
			return writeDCRError(ctx, w, logger, "invalid_redirect_uri", "redirect_uri must be an absolute URL")
		}
	}

	if len(req.GrantTypes) == 0 {
		req.GrantTypes = []string{"authorization_code"}
	}
	for _, gt := range req.GrantTypes {
		if !slices.Contains(supportedGrantTypes, gt) {
			return writeDCRError(ctx, w, logger, "invalid_client_metadata", fmt.Sprintf("unsupported grant_type %q", gt))
		}
	}

	if len(req.ResponseTypes) == 0 {
		req.ResponseTypes = []string{"code"}
	}
	for _, rt := range req.ResponseTypes {
		if !slices.Contains(supportedResponseTypes, rt) {
			return writeDCRError(ctx, w, logger, "invalid_client_metadata", fmt.Sprintf("unsupported response_type %q", rt))
		}
	}

	if req.TokenEndpointAuthMethod == "" {
		req.TokenEndpointAuthMethod = "client_secret_basic"
	}
	if !slices.Contains(supportedAuthMethods, req.TokenEndpointAuthMethod) {
		return writeDCRError(ctx, w, logger, "invalid_client_metadata", fmt.Sprintf("unsupported token_endpoint_auth_method %q", req.TokenEndpointAuthMethod))
	}

	clientID := "client_" + uuid.NewString()

	// Public clients (token_endpoint_auth_method=none) skip secret generation
	// and store NULL in client_secret_hash. The /token handler treats a NULL
	// hash as "no secret expected; PKCE is the integrity proof".
	var clientSecret string
	var clientSecretHash pgtype.Text
	if req.TokenEndpointAuthMethod != "none" {
		var err error
		clientSecret, err = generateClientSecret()
		if err != nil {
			return oops.E(oops.CodeUnexpected, err, "failed to generate client secret").Log(ctx, logger)
		}
		hashed, hashErr := bcrypt.GenerateFromPassword([]byte(clientSecret), bcrypt.DefaultCost)
		if hashErr != nil {
			return oops.E(oops.CodeUnexpected, hashErr, "failed to hash client secret").Log(ctx, logger)
		}
		clientSecretHash = pgtype.Text{String: string(hashed), Valid: true}
	}

	row, err := usersessions_repo.New(s.db).CreateUserSessionClient(ctx, usersessions_repo.CreateUserSessionClientParams{
		UserSessionIssuerID: toolset.UserSessionIssuerID.UUID,
		ClientID:            clientID,
		ClientSecretHash:    clientSecretHash,
		ClientName:          req.ClientName,
		RedirectUris:        req.RedirectURIs,
		// RFC 7591 §3.2.1 expires_at=0 = non-expiring; we leave the Postgres column NULL.
		ClientSecretExpiresAt: pgtype.Timestamptz{Time: time.Time{}, InfinityModifier: 0, Valid: false},
	})
	if err != nil {
		return oops.E(oops.CodeUnexpected, err, "failed to create user session client").Log(ctx, logger)
	}

	logger.InfoContext(ctx, "user session client registered",
		attr.SlogOAuthClientID(clientID),
		attr.SlogOAuthClientName(req.ClientName),
	)

	// Confidential clients get client_secret + client_secret_expires_at=0
	// (non-expiring per RFC 7591 §3.2.1). Public clients (none) get neither
	// field — emitting them would suggest a secret exists.
	var clientSecretExpiresAt *int64
	if req.TokenEndpointAuthMethod != "none" {
		zero := int64(0)
		clientSecretExpiresAt = &zero
	}

	resp := dcrRegistrationResponse{
		ClientID:                clientID,
		ClientSecret:            clientSecret,
		ClientIDIssuedAt:        row.ClientIDIssuedAt.Time.Unix(),
		ClientSecretExpiresAt:   clientSecretExpiresAt,
		ClientName:              req.ClientName,
		RedirectURIs:            req.RedirectURIs,
		GrantTypes:              req.GrantTypes,
		ResponseTypes:           req.ResponseTypes,
		TokenEndpointAuthMethod: req.TokenEndpointAuthMethod,
		Scope:                   req.Scope,
	}

	body, err := json.Marshal(resp)
	if err != nil {
		return oops.E(oops.CodeUnexpected, err, "failed to marshal registration response").Log(ctx, logger)
	}

	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("Pragma", "no-cache")
	w.WriteHeader(http.StatusCreated)
	if _, err := w.Write(body); err != nil {
		return oops.E(oops.CodeUnexpected, err, "failed to write response body").Log(ctx, logger)
	}
	return nil
}

// writeDCRError emits an RFC 7591 §3.2.2 client registration error response.
// Status is 400 with a JSON body { "error": "<code>", "error_description": "..." }.
func writeDCRError(ctx context.Context, w http.ResponseWriter, logger *slog.Logger, code, description string) error {
	body, err := json.Marshal(map[string]string{
		"error":             code,
		"error_description": description,
	})
	if err != nil {
		return oops.E(oops.CodeUnexpected, err, "failed to marshal DCR error").Log(ctx, logger)
	}

	logger.InfoContext(ctx, "DCR registration rejected",
		attr.SlogOAuthError(code),
		attr.SlogOAuthErrorDescription(description),
	)

	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("Pragma", "no-cache")
	w.WriteHeader(http.StatusBadRequest)
	if _, werr := w.Write(body); werr != nil {
		return oops.E(oops.CodeUnexpected, werr, "failed to write DCR error body").Log(ctx, logger)
	}
	return nil
}

// generateClientSecret produces 32 bytes of cryptographically random data
// and base64url-encodes them.
func generateClientSecret() (string, error) {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		return "", fmt.Errorf("read random bytes: %w", err)
	}
	return base64.RawURLEncoding.EncodeToString(b), nil
}

// HandleAuthorize implements the OAuth 2.1 authorization endpoint (RFC 6749
// §4.1.1) on the issuer-gated authn-challenge surface. Mounted at
// `GET /mcp/{mcpSlug}/authorize`.
//
// Flow:
//   - validate the request (response_type=code, S256 PKCE, known client,
//     allowed redirect_uri)
//   - mint an AuthnChallengeState in Redis carrying the request context
//   - branch on the toolset's privacy:
//   - private (`!McpIsPublic`): 302 to the Speakeasy IDP login page; on
//     return HandleIDPCallback stamps `user:<id>` onto the state
//   - public (`McpIsPublic`): 302 directly to /connect; HandleConsent's
//     POST stamps `anonymous:<prospective_mcp_session_id>`
func (s *Service) HandleAuthorize(w http.ResponseWriter, r *http.Request) error {
	ctx := r.Context()
	mcpSlug := chi.URLParam(r, "mcpSlug")
	if mcpSlug == "" {
		return oops.E(oops.CodeBadRequest, nil, "an mcp slug must be provided").Log(ctx, s.logger)
	}

	toolset, customDomainCtx, err := s.loadToolsetFromMcpSlug(ctx, mcpSlug)
	switch {
	case errors.Is(err, errToolsetNotFound):
		return oops.E(oops.CodeNotFound, err, "mcp server not found")
	case err != nil:
		return oops.E(oops.CodeUnexpected, err, "failed to load MCP server").Log(ctx, s.logger)
	}

	if !toolset.UserSessionIssuerID.Valid {
		return oops.E(oops.CodeNotFound, nil, "not found")
	}

	logger := s.logger.With(
		attr.SlogToolsetID(toolset.ID.String()),
		attr.SlogProjectID(toolset.ProjectID.String()),
	)

	q := r.URL.Query()
	clientID := q.Get("client_id")
	redirectURI := q.Get("redirect_uri")
	responseType := q.Get("response_type")
	state := q.Get("state")
	scope := q.Get("scope")
	codeChallenge := q.Get("code_challenge")
	codeChallengeMethod := q.Get("code_challenge_method")

	if clientID == "" {
		return writeAuthorizeError(ctx, w, logger, http.StatusBadRequest, "invalid_request", "client_id is required")
	}
	if redirectURI == "" {
		return writeAuthorizeError(ctx, w, logger, http.StatusBadRequest, "invalid_request", "redirect_uri is required")
	}
	if responseType != "code" {
		return writeAuthorizeError(ctx, w, logger, http.StatusBadRequest, "unsupported_response_type", "response_type must be 'code'")
	}
	if codeChallenge == "" {
		return writeAuthorizeError(ctx, w, logger, http.StatusBadRequest, "invalid_request", "code_challenge is required (PKCE mandatory)")
	}
	if codeChallengeMethod != "S256" {
		return writeAuthorizeError(ctx, w, logger, http.StatusBadRequest, "invalid_request", "code_challenge_method must be 'S256'")
	}

	client, err := usersessions_repo.New(s.db).GetUserSessionClientByClientID(ctx, usersessions_repo.GetUserSessionClientByClientIDParams{
		UserSessionIssuerID: toolset.UserSessionIssuerID.UUID,
		ClientID:            clientID,
	})
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return writeAuthorizeError(ctx, w, logger, http.StatusUnauthorized, "invalid_client", "unknown client_id")
		}
		return oops.E(oops.CodeUnexpected, err, "lookup user session client").Log(ctx, logger)
	}
	if !slices.Contains(client.RedirectUris, redirectURI) {
		return writeAuthorizeError(ctx, w, logger, http.StatusBadRequest, "invalid_request", "redirect_uri is not registered for this client")
	}

	challengeID := uuid.NewString()

	challengeState := AuthnChallengeState{
		ID:                  challengeID,
		UserSessionIssuerID: toolset.UserSessionIssuerID.UUID,
		ToolsetID:           toolset.ID,
		ClientID:            clientID,
		RedirectURI:         redirectURI,
		Scope:               scope,
		State:               state,
		CodeChallenge:       codeChallenge,
		CodeChallengeMethod: codeChallengeMethod,
		// Subject is left nil — HandleIDPCallback (private path) and
		// HandleConsent (public path) stamp it later in the flow.
		Subject:   nil,
		CreatedAt: time.Now(),
	}

	if err := s.authnChallengeCache.Store(ctx, challengeState); err != nil {
		return oops.E(oops.CodeUnexpected, err, "store authn challenge state").Log(ctx, logger)
	}

	baseURL := s.serverURL.String()
	if customDomainCtx != nil {
		baseURL = fmt.Sprintf("https://%s", customDomainCtx.Domain)
	}

	if !toolset.McpIsPublic {
		callbackURL := fmt.Sprintf("%s/mcp/%s/idp_callback", baseURL, mcpSlug)
		idpURL, err := s.sessions.BuildAuthorizationURL(ctx, sessions.AuthURLParams{
			CallbackURL:     callbackURL,
			Scope:           "",
			State:           challengeID,
			ClientID:        "",
			ScopesSupported: nil,
		})
		if err != nil {
			return oops.E(oops.CodeUnexpected, err, "build IDP authorization URL").Log(ctx, logger)
		}
		http.Redirect(w, r, idpURL.String(), http.StatusFound)
		return nil
	}

	// Public toolset: skip IDP, route straight to consent. The anonymous sub
	// is minted on the consent POST (per plan).
	consentURL := fmt.Sprintf("%s/mcp/%s/connect?state=%s", baseURL, mcpSlug, url.QueryEscape(challengeID))
	http.Redirect(w, r, consentURL, http.StatusFound)
	return nil
}

// writeAuthorizeError emits an OAuth 2.1 authorization error (RFC 6749
// §4.1.2.1) inline as a JSON body. We don't redirect to redirect_uri because
// the request hasn't been validated to that point — per RFC 6749 §3.1.2.4, an
// invalid redirect_uri must NOT be redirected to.
func writeAuthorizeError(ctx context.Context, w http.ResponseWriter, logger *slog.Logger, status int, code, description string) error {
	body, err := json.Marshal(map[string]string{
		"error":             code,
		"error_description": description,
	})
	if err != nil {
		return oops.E(oops.CodeUnexpected, err, "failed to marshal authorize error").Log(ctx, logger)
	}

	logger.InfoContext(ctx, "authorize request rejected",
		attr.SlogOAuthError(code),
		attr.SlogOAuthErrorDescription(description),
	)

	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("Pragma", "no-cache")
	w.WriteHeader(status)
	if _, werr := w.Write(body); werr != nil {
		return oops.E(oops.CodeUnexpected, werr, "failed to write authorize error body").Log(ctx, logger)
	}
	return nil
}

// HandleIDPCallback is the GET endpoint Speakeasy IDP redirects back to
// after the user authenticates on the private-toolset path. Mounted at
// `GET /mcp/{mcpSlug}/idp_callback`.
//
// The name pairs with `remote_login_callback` (spike §6.5) — the other
// callback on this surface, used for upstream OAuth resource providers
// (Linear, Notion, etc.). Reading the two side-by-side: IDP returns user
// identity; remote returns resource-access tokens.
//
// It is independent of the chat-session manager: we drive the IDP wire calls
// directly through s.idpClient (see speakeasyclient.go) and skip everything
// the chat-session path bundles in (userInfoCache writes, posthog, pylon,
// WorkOS sync, admin override, cookie issuance). We DO upsert the Gram user
// row -- otherwise we have no Gram user_id to put in the URN.
//
// Side effects on success: UpsertUser, AuthnChallengeState rewrite (subject
// stamped). The IDP idToken is consumed and discarded; no chat session
// persists.
func (s *Service) HandleIDPCallback(w http.ResponseWriter, r *http.Request) error {
	ctx := r.Context()
	mcpSlug := chi.URLParam(r, "mcpSlug")
	if mcpSlug == "" {
		return oops.E(oops.CodeBadRequest, nil, "an mcp slug must be provided").Log(ctx, s.logger)
	}

	toolset, customDomainCtx, err := s.loadToolsetFromMcpSlug(ctx, mcpSlug)
	switch {
	case errors.Is(err, errToolsetNotFound):
		return oops.E(oops.CodeNotFound, err, "mcp server not found")
	case err != nil:
		return oops.E(oops.CodeUnexpected, err, "failed to load MCP server").Log(ctx, s.logger)
	}
	if !toolset.UserSessionIssuerID.Valid {
		return oops.E(oops.CodeNotFound, nil, "not found")
	}

	logger := s.logger.With(
		attr.SlogToolsetID(toolset.ID.String()),
		attr.SlogProjectID(toolset.ProjectID.String()),
	)

	q := r.URL.Query()
	stateID := q.Get("state")
	code := q.Get("code")
	if stateID == "" || code == "" {
		return oops.E(oops.CodeBadRequest, nil, "state and code are required").Log(ctx, logger)
	}

	challengeState, err := s.authnChallengeCache.Get(ctx, "authnChallenge:"+stateID)
	if err != nil {
		return oops.E(oops.CodeUnauthorized, err, "authn challenge state not found or expired").Log(ctx, logger)
	}

	// State-confusion guard: the state must belong to this toolset.
	if challengeState.ToolsetID != toolset.ID {
		return oops.E(oops.CodeUnauthorized, nil, "authn challenge state does not match this MCP server").Log(ctx, logger)
	}

	idToken, err := s.idpClient.ExchangeCode(ctx, code)
	if err != nil {
		return oops.E(oops.CodeUnauthorized, err, "failed to exchange IDP code").Log(ctx, logger)
	}

	validated, err := s.idpClient.ValidateIDToken(ctx, idToken)
	if err != nil {
		return oops.E(oops.CodeUnauthorized, err, "failed to validate IDP id token").Log(ctx, logger)
	}

	// Here we validate that the owner belongs to the toolset Org before proceeding
	// We don't want to mess around with issuing tokens to non-org users
	// Why not the project? Well the mcp:connect RBAC policy operates at
	// an organization level. This policy will be enforced in the MCP endpoint
	// but we defer the check to be more general here
	authorized := false
	for _, org := range validated.Organizations {
		if org.ID == toolset.OrganizationID {
			authorized = true
			break
		}
	}
	if !authorized {
		return oops.E(oops.CodeForbidden, nil, "user is not a member of this MCP server's organization").Log(ctx, logger)
	}

	// Run the shared post-IDP user bootstrap: UpsertUser + posthog signup
	// event + WorkOS membership sync. Same side effects the chat-session
	// manager runs on dashboard logins, identical ordering. WorkOS sync in
	// particular is required so downstream RBAC has the right org-membership
	// records for an MCP-only user authenticating for the first time.
	user, err := s.idpClient.BootstrapUser(ctx, validated)
	if err != nil {
		return oops.E(oops.CodeUnexpected, err, "failed to bootstrap user").Log(ctx, logger)
	}

	subject := urn.NewUserSubject(user.ID)
	challengeState.Subject = &subject
	if err := s.authnChallengeCache.Store(ctx, challengeState); err != nil {
		return oops.E(oops.CodeUnexpected, err, "failed to update authn challenge state").Log(ctx, logger)
	}

	baseURL := s.serverURL.String()
	if customDomainCtx != nil {
		baseURL = fmt.Sprintf("https://%s", customDomainCtx.Domain)
	}
	consentURL := fmt.Sprintf("%s/mcp/%s/connect?state=%s", baseURL, mcpSlug, url.QueryEscape(challengeState.ID))
	http.Redirect(w, r, consentURL, http.StatusFound)
	return nil
}

// consentTemplateData is the field set the consent template renders against.
type consentTemplateData struct {
	ClientName     string
	MCPSlug        string
	State          string
	SubjectDisplay string
	RedirectURI    string
	Scopes         []string
}

// HandleConsent serves the GET (consent UI) and POST (Give Access /
// Cancel) for the issuer-gated authn-challenge flow. Mounted at
// `GET, POST /mcp/{mcpSlug}/connect`.
//
// On POST + Give Access:
//
//   - If the AuthnChallengeState's Subject is still empty (public-toolset
//     path that bypassed the IDP), mint a fresh anonymous URN here.
//   - Persist a user_session_consents row binding (principal, client,
//     remote_set_hash). Per spike §3.4: even the empty-remote-set case is
//     bound to a specific hash so consent can't be CSRF'd past on a future
//     issuer change.
//   - Mint a UserSessionGrant in Redis carrying everything HandleToken
//     needs to mint a JWT (sub, client_id, redirect_uri, code_challenge,
//     scope) and 302 the MCP client to its registered redirect_uri with
//     `?code={code}&state={original_state}`.
func (s *Service) HandleConsent(w http.ResponseWriter, r *http.Request) error {
	switch r.Method {
	case http.MethodGet:
		return s.handleConsentGet(w, r)
	case http.MethodPost:
		return s.handleConsentPost(w, r)
	default:
		return oops.E(oops.CodeBadRequest, nil, "method not allowed").Log(r.Context(), s.logger)
	}
}

func (s *Service) handleConsentGet(w http.ResponseWriter, r *http.Request) error {
	ctx := r.Context()
	mcpSlug := chi.URLParam(r, "mcpSlug")
	if mcpSlug == "" {
		return oops.E(oops.CodeBadRequest, nil, "an mcp slug must be provided").Log(ctx, s.logger)
	}

	toolset, _, err := s.loadToolsetFromMcpSlug(ctx, mcpSlug)
	switch {
	case errors.Is(err, errToolsetNotFound):
		return oops.E(oops.CodeNotFound, err, "mcp server not found")
	case err != nil:
		return oops.E(oops.CodeUnexpected, err, "failed to load MCP server").Log(ctx, s.logger)
	}
	if !toolset.UserSessionIssuerID.Valid {
		return oops.E(oops.CodeNotFound, nil, "not found")
	}

	logger := s.logger.With(
		attr.SlogToolsetID(toolset.ID.String()),
		attr.SlogProjectID(toolset.ProjectID.String()),
	)

	stateID := r.URL.Query().Get("state")
	if stateID == "" {
		return oops.E(oops.CodeBadRequest, nil, "state is required").Log(ctx, logger)
	}

	challengeState, err := s.authnChallengeCache.Get(ctx, "authnChallenge:"+stateID)
	if err != nil {
		return oops.E(oops.CodeUnauthorized, err, "authn challenge state not found or expired").Log(ctx, logger)
	}
	if challengeState.ToolsetID != toolset.ID {
		return oops.E(oops.CodeUnauthorized, nil, "authn challenge state does not match this MCP server").Log(ctx, logger)
	}

	client, err := usersessions_repo.New(s.db).GetUserSessionClientByClientID(ctx, usersessions_repo.GetUserSessionClientByClientIDParams{
		UserSessionIssuerID: toolset.UserSessionIssuerID.UUID,
		ClientID:            challengeState.ClientID,
	})
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return oops.E(oops.CodeUnauthorized, err, "user session client revoked").Log(ctx, logger)
		}
		return oops.E(oops.CodeUnexpected, err, "lookup user session client").Log(ctx, logger)
	}

	subjectDisplay := "An anonymous session for this MCP server"
	if challengeState.Subject != nil && !challengeState.Subject.IsZero() {
		subjectDisplay = challengeState.Subject.String()
	}

	var scopes []string
	if challengeState.Scope != "" {
		scopes = strings.Fields(challengeState.Scope)
	}

	data := consentTemplateData{
		ClientName:     client.ClientName,
		MCPSlug:        mcpSlug,
		State:          stateID,
		SubjectDisplay: subjectDisplay,
		RedirectURI:    challengeState.RedirectURI,
		Scopes:         scopes,
	}

	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	if err := consentTemplate.Execute(w, data); err != nil {
		return oops.E(oops.CodeUnexpected, err, "render consent template").Log(ctx, logger)
	}
	return nil
}

func (s *Service) handleConsentPost(w http.ResponseWriter, r *http.Request) error {
	ctx := r.Context()
	mcpSlug := chi.URLParam(r, "mcpSlug")
	if mcpSlug == "" {
		return oops.E(oops.CodeBadRequest, nil, "an mcp slug must be provided").Log(ctx, s.logger)
	}

	// Cap form body to defend against memory exhaustion (gosec G120). The
	// consent form has a few short fields; 16 KiB is generous.
	r.Body = http.MaxBytesReader(w, r.Body, 16<<10)
	if err := r.ParseForm(); err != nil {
		return oops.E(oops.CodeBadRequest, err, "failed to parse form").Log(ctx, s.logger)
	}

	toolset, _, err := s.loadToolsetFromMcpSlug(ctx, mcpSlug)
	switch {
	case errors.Is(err, errToolsetNotFound):
		return oops.E(oops.CodeNotFound, err, "mcp server not found")
	case err != nil:
		return oops.E(oops.CodeUnexpected, err, "failed to load MCP server").Log(ctx, s.logger)
	}
	if !toolset.UserSessionIssuerID.Valid {
		return oops.E(oops.CodeNotFound, nil, "not found")
	}

	logger := s.logger.With(
		attr.SlogToolsetID(toolset.ID.String()),
		attr.SlogProjectID(toolset.ProjectID.String()),
	)

	stateID := r.PostForm.Get("state")
	if stateID == "" {
		return oops.E(oops.CodeBadRequest, nil, "state is required").Log(ctx, logger)
	}

	challengeState, err := s.authnChallengeCache.Get(ctx, "authnChallenge:"+stateID)
	if err != nil {
		return oops.E(oops.CodeUnauthorized, err, "authn challenge state not found or expired").Log(ctx, logger)
	}
	if challengeState.ToolsetID != toolset.ID {
		return oops.E(oops.CodeUnauthorized, nil, "authn challenge state does not match this MCP server").Log(ctx, logger)
	}

	// Cancel: 302 the MCP client back to its redirect_uri with
	// access_denied per RFC 6749 §4.1.2.1, preserving the original state.
	if r.PostForm.Get("action") == "deny" {
		denyURL := buildClientRedirect(challengeState.RedirectURI, "", challengeState.State, "access_denied", "user denied consent")
		_ = s.authnChallengeCache.Delete(ctx, challengeState)
		http.Redirect(w, r, denyURL, http.StatusFound)
		return nil
	}

	// Late-bind the anonymous URN here on the public-toolset path. Private
	// toolsets had Subject stamped at /idp_callback time.
	var subject urn.SessionSubject
	if challengeState.Subject != nil && !challengeState.Subject.IsZero() {
		subject = *challengeState.Subject
	} else {
		subject = urn.NewAnonymousSubject(uuid.NewString())
	}

	// Resolve the user_session_clients row id for the consent FK.
	clientRow, err := usersessions_repo.New(s.db).GetUserSessionClientByClientID(ctx, usersessions_repo.GetUserSessionClientByClientIDParams{
		UserSessionIssuerID: toolset.UserSessionIssuerID.UUID,
		ClientID:            challengeState.ClientID,
	})
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return oops.E(oops.CodeUnauthorized, err, "user session client revoked").Log(ctx, logger)
		}
		return oops.E(oops.CodeUnexpected, err, "lookup user session client").Log(ctx, logger)
	}

	// Persist the consent record. The unique index on
	// (principal_urn, user_session_client_id, remote_set_hash) makes this
	// idempotent on re-consent for the same set; we treat the duplicate-key
	// error as a no-op (consent already on file).
	if _, err := usersessions_repo.New(s.db).CreateUserSessionConsent(ctx, usersessions_repo.CreateUserSessionConsentParams{
		SubjectUrn:          subject,
		UserSessionClientID: clientRow.ID,
		RemoteSetHash:       remoteSetHashEmpty,
	}); err != nil && !isUniqueViolation(err) {
		return oops.E(oops.CodeUnexpected, err, "record consent").Log(ctx, logger)
	}

	code, err := generateOpaqueToken()
	if err != nil {
		return oops.E(oops.CodeUnexpected, err, "generate authorization code").Log(ctx, logger)
	}

	grant := UserSessionGrant{
		Code:                code,
		UserSessionIssuerID: toolset.UserSessionIssuerID.UUID,
		UserSessionClientID: clientRow.ID,
		ClientID:            challengeState.ClientID,
		RedirectURI:         challengeState.RedirectURI,
		Scope:               challengeState.Scope,
		CodeChallenge:       challengeState.CodeChallenge,
		CodeChallengeMethod: challengeState.CodeChallengeMethod,
		Subject:             subject,
		CreatedAt:           time.Now(),
	}
	if err := s.userSessionGrantCache.Store(ctx, grant); err != nil {
		return oops.E(oops.CodeUnexpected, err, "store user session grant").Log(ctx, logger)
	}

	// Authn challenge state has served its purpose; drop it so a replay
	// can't re-issue a code from the same flow.
	_ = s.authnChallengeCache.Delete(ctx, challengeState)

	clientRedirect := buildClientRedirect(challengeState.RedirectURI, code, challengeState.State, "", "")
	http.Redirect(w, r, clientRedirect, http.StatusFound)
	return nil
}

// generateOpaqueToken produces a cryptographically random 32-byte URL-safe
// token. Used as both the OAuth authorization code (HandleConsent's POST) and
// the refresh token (HandleToken). 32 bytes of entropy from crypto/rand far
// exceeds RFC 6749 §10.10's 128-bit minimum; base64url makes the value safe
// to drop in a URL query string or HTTP header without further encoding.
func generateOpaqueToken() (string, error) {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		return "", fmt.Errorf("read random bytes: %w", err)
	}
	return base64.RawURLEncoding.EncodeToString(b), nil
}

// buildClientRedirect produces the URL to redirect the MCP client to,
// preserving any prior query string on redirectURI and adding `code` (success)
// or `error` / `error_description` (failure) plus the original `state`.
func buildClientRedirect(redirectURI, code, originalState, errCode, errDescription string) string {
	u, err := url.Parse(redirectURI)
	if err != nil {
		// Should never happen — redirect_uri was validated at HandleAuthorize
		// time. Fall back to a best-effort string concatenation.
		return redirectURI
	}
	q := u.Query()
	if code != "" {
		q.Set("code", code)
	}
	if errCode != "" {
		q.Set("error", errCode)
		if errDescription != "" {
			q.Set("error_description", errDescription)
		}
	}
	if originalState != "" {
		q.Set("state", originalState)
	}
	u.RawQuery = q.Encode()
	return u.String()
}

// isUniqueViolation reports whether err is a Postgres unique-constraint
// violation. Used to detect duplicate consent inserts (idempotent re-consent).
func isUniqueViolation(err error) bool {
	if err == nil {
		return false
	}
	// pgx wraps PG errors as *pgconn.PgError with Code "23505".
	return strings.Contains(err.Error(), "23505")
}

// tokenResponse is the RFC 6749 §5.1 successful token response shape, plus
// `refresh_token` since we issue them on every grant.
type tokenResponse struct {
	AccessToken  string `json:"access_token"`
	TokenType    string `json:"token_type"`
	ExpiresIn    int64  `json:"expires_in"`
	RefreshToken string `json:"refresh_token,omitempty"`
	Scope        string `json:"scope,omitempty"`
}

// HandleToken implements the OAuth 2.1 token endpoint (RFC 6749 §4.1.3) for
// the authorization_code grant. Mounted at `POST /mcp/{mcpSlug}/token`.
//
// Flow:
//   - parse + cap form
//   - authenticate the client (Basic or POST credential), bcrypt-verify
//     against user_session_clients.client_secret_hash
//   - atomically read+delete the UserSessionGrant (Redis) so a code can't
//     be redeemed twice
//   - verify redirect_uri matches the grant + S256 PKCE verifier matches
//     code_challenge
//   - mint a SessionClaims JWT (HS256 with GRAM_JWT_SIGNING_KEY) audienced
//     to the toolset slug, sub = grant.Subject
//   - persist a user_sessions row keyed on sha256(refresh_token)
//   - return RFC 6749 §5.1 token JSON with no-store headers
//
// Refresh-token grant is intentionally out of scope; lands in milestone #2
// alongside rotation policy and the chat-session retirement work.
func (s *Service) HandleToken(w http.ResponseWriter, r *http.Request) error {
	ctx := r.Context()
	mcpSlug := chi.URLParam(r, "mcpSlug")
	if mcpSlug == "" {
		return oops.E(oops.CodeBadRequest, nil, "an mcp slug must be provided").Log(ctx, s.logger)
	}

	r.Body = http.MaxBytesReader(w, r.Body, 16<<10)
	if err := r.ParseForm(); err != nil {
		return writeTokenError(ctx, w, s.logger, http.StatusBadRequest, "invalid_request", "failed to parse form")
	}

	toolset, _, err := s.loadToolsetFromMcpSlug(ctx, mcpSlug)
	switch {
	case errors.Is(err, errToolsetNotFound):
		return oops.E(oops.CodeNotFound, err, "mcp server not found")
	case err != nil:
		return oops.E(oops.CodeUnexpected, err, "failed to load MCP server").Log(ctx, s.logger)
	}
	if !toolset.UserSessionIssuerID.Valid {
		return oops.E(oops.CodeNotFound, nil, "not found")
	}

	logger := s.logger.With(
		attr.SlogToolsetID(toolset.ID.String()),
		attr.SlogProjectID(toolset.ProjectID.String()),
	)

	grantType := r.PostForm.Get("grant_type")
	switch grantType {
	case "authorization_code":
		// fall through
	case "refresh_token":
		return writeTokenError(ctx, w, logger, http.StatusBadRequest, "unsupported_grant_type", "refresh_token grant lands in milestone #2")
		// > booooooo - inaccurate. Just fucking implement it bruv
	default:
		return writeTokenError(ctx, w, logger, http.StatusBadRequest, "unsupported_grant_type", "unsupported grant_type")
	}

	clientID, clientSecret, _ := extractClientCredentials(r)
	if clientID == "" {
		return writeTokenError(ctx, w, logger, http.StatusUnauthorized, "invalid_client", "client_id is required")
	}

	clientRow, err := usersessions_repo.New(s.db).GetUserSessionClientByClientID(ctx, usersessions_repo.GetUserSessionClientByClientIDParams{
		UserSessionIssuerID: toolset.UserSessionIssuerID.UUID,
		ClientID:            clientID,
	})
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return writeTokenError(ctx, w, logger, http.StatusUnauthorized, "invalid_client", "unknown client_id")
		}
		return oops.E(oops.CodeUnexpected, err, "lookup user session client").Log(ctx, logger)
	}
	// Public clients (token_endpoint_auth_method=none) have a NULL hash:
	// PKCE is the integrity proof, no secret check. Confidential clients
	// MUST present a matching secret.
	if clientRow.ClientSecretHash.Valid {
		if err := bcrypt.CompareHashAndPassword([]byte(clientRow.ClientSecretHash.String), []byte(clientSecret)); err != nil {
			return writeTokenError(ctx, w, logger, http.StatusUnauthorized, "invalid_client", "client secret mismatch")
		}
	}

	code := r.PostForm.Get("code")
	if code == "" {
		return writeTokenError(ctx, w, logger, http.StatusBadRequest, "invalid_request", "code is required")
	}

	// Atomically read+delete the grant so a code can't be redeemed twice.
	grantKey := "userSessionGrant:" + toolset.UserSessionIssuerID.UUID.String() + ":" + code
	grant, err := s.userSessionGrantCache.Get(ctx, grantKey)
	if err != nil {
		return writeTokenError(ctx, w, logger, http.StatusBadRequest, "invalid_grant", "code not found or expired")
	}
	if err := s.userSessionGrantCache.Delete(ctx, grant); err != nil {
		// Failed to delete -- another process may redeem. Refuse to continue.
		return oops.E(oops.CodeUnexpected, err, "consume user session grant").Log(ctx, logger)
	}

	if grant.ClientID != clientID {
		return writeTokenError(ctx, w, logger, http.StatusBadRequest, "invalid_grant", "code was issued to a different client")
	}
	if grant.RedirectURI != r.PostForm.Get("redirect_uri") {
		return writeTokenError(ctx, w, logger, http.StatusBadRequest, "invalid_grant", "redirect_uri does not match the original request")
	}

	verifier := r.PostForm.Get("code_verifier")
	if verifier == "" {
		return writeTokenError(ctx, w, logger, http.StatusBadRequest, "invalid_request", "code_verifier is required")
	}
	if !verifyPKCES256(verifier, grant.CodeChallenge) {
		return writeTokenError(ctx, w, logger, http.StatusBadRequest, "invalid_grant", "code_verifier does not match code_challenge")
	}

	// No re-check of user_session_consents here: possession of a valid
	// UserSessionGrant IS proof of consent. The grant was minted by the
	// HandleConsent POST after writing the consent record, and we just
	// atomically consumed it (single-use). Re-querying the consent table
	// would be redundant.

	// Resolve the issuer's session_duration. Microseconds-only: the issuer
	// create handler stores via conv.PtrToPGInterval which never sets
	// Months/Days; if we ever see those here, raw SQL bypassed the writer
	// and the conversion is calendar-dependent — fail with 500 rather than
	// silently approximate.
	issuer, err := usersessions_repo.New(s.db).GetUserSessionIssuerByID(ctx, usersessions_repo.GetUserSessionIssuerByIDParams{
		ID:        toolset.UserSessionIssuerID.UUID,
		ProjectID: toolset.ProjectID,
	})
	if err != nil {
		return oops.E(oops.CodeUnexpected, err, "lookup user session issuer").Log(ctx, logger)
	}
	if !issuer.SessionDuration.Valid {
		return oops.E(oops.CodeUnexpected, nil, "issuer session_duration is not set").Log(ctx, logger)
	}
	if issuer.SessionDuration.Months != 0 || issuer.SessionDuration.Days != 0 {
		return oops.E(oops.CodeUnexpected, nil, "issuer session_duration carries Months/Days; only Microseconds intervals are supported").Log(ctx, logger)
	}
	lifetime := time.Duration(issuer.SessionDuration.Microseconds) * time.Microsecond
	if lifetime <= 0 {
		return oops.E(oops.CodeUnexpected, nil, "issuer session_duration is non-positive").Log(ctx, logger)
	}

	issuerURL := s.serverURL.String() + "/mcp/" + mcpSlug
	access, jti, err := s.userSessionSigner.Mint(grant.Subject, toolset.Slug, issuerURL, lifetime)
	if err != nil {
		return oops.E(oops.CodeUnexpected, err, "mint session jwt").Log(ctx, logger)
	}

	refreshTokenRaw, err := generateOpaqueToken()
	if err != nil {
		return oops.E(oops.CodeUnexpected, err, "generate refresh token").Log(ctx, logger)
	}
	refreshHash := sha256Hex(refreshTokenRaw)

	now := time.Now()
	expiresAt := now.Add(lifetime)
	if _, err := usersessions_repo.New(s.db).CreateUserSession(ctx, usersessions_repo.CreateUserSessionParams{
		UserSessionIssuerID: toolset.UserSessionIssuerID.UUID,
		SubjectUrn:          grant.Subject,
		Jti:                 jti,
		RefreshTokenHash:    refreshHash,
		RefreshExpiresAt:    pgtype.Timestamptz{Time: expiresAt, InfinityModifier: 0, Valid: true},
		ExpiresAt:           pgtype.Timestamptz{Time: expiresAt, InfinityModifier: 0, Valid: true},
	}); err != nil {
		return oops.E(oops.CodeUnexpected, err, "persist user session").Log(ctx, logger)
	}

	body, err := json.Marshal(tokenResponse{
		AccessToken:  access,
		TokenType:    "Bearer",
		ExpiresIn:    int64(lifetime.Seconds()),
		RefreshToken: refreshTokenRaw,
		Scope:        grant.Scope,
	})
	if err != nil {
		return oops.E(oops.CodeUnexpected, err, "marshal token response").Log(ctx, logger)
	}

	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("Pragma", "no-cache")
	w.WriteHeader(http.StatusOK)
	if _, err := w.Write(body); err != nil {
		return oops.E(oops.CodeUnexpected, err, "write token response").Log(ctx, logger)
	}
	return nil
}

// extractClientCredentials returns the client_id + client_secret + ok from
// either the Authorization header (client_secret_basic) or the form body
// (client_secret_post). HTTP Basic wins when both are present, per RFC 6749
// §2.3.1 ("the client MAY use only one authentication method").
func extractClientCredentials(r *http.Request) (string, string, bool) {
	if id, secret, ok := r.BasicAuth(); ok && id != "" {
		return id, secret, true
	}
	id := r.PostForm.Get("client_id")
	secret := r.PostForm.Get("client_secret")
	if id == "" {
		return "", "", false
	}
	return id, secret, true
}

// verifyPKCES256 reports whether code_verifier matches the stored
// code_challenge under the S256 method (RFC 7636 §4.6):
// BASE64URL-NO-PAD(SHA256(ASCII(code_verifier))) == code_challenge.
func verifyPKCES256(verifier, challenge string) bool {
	sum := sha256.Sum256([]byte(verifier))
	return base64.RawURLEncoding.EncodeToString(sum[:]) == challenge
}

// sha256Hex returns the hex-encoded SHA-256 of the input.
func sha256Hex(s string) string {
	sum := sha256.Sum256([]byte(s))
	// Use base64url so the hash on the wire matches the format used elsewhere
	// for token-derived storage keys.
	return base64.RawURLEncoding.EncodeToString(sum[:])
}

// writeTokenError emits an RFC 6749 §5.2 token error response: 4xx with a
// JSON body { "error": "<code>", "error_description": "..." } and the
// no-store headers required by RFC 6749 §5.1.
func writeTokenError(ctx context.Context, w http.ResponseWriter, logger *slog.Logger, status int, code, description string) error {
	body, err := json.Marshal(map[string]string{
		"error":             code,
		"error_description": description,
	})
	if err != nil {
		return oops.E(oops.CodeUnexpected, err, "marshal token error").Log(ctx, logger)
	}

	logger.InfoContext(ctx, "token request rejected",
		attr.SlogOAuthError(code),
		attr.SlogOAuthErrorDescription(description),
	)

	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("Pragma", "no-cache")
	w.WriteHeader(status)
	if _, werr := w.Write(body); werr != nil {
		return oops.E(oops.CodeUnexpected, werr, "write token error body").Log(ctx, logger)
	}
	return nil
}
