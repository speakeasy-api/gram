// OAuth authorization code exchange handlers for MCP clients. Issuer-gated
// toolsets (toolsets.user_session_issuer_id set) flow through the OAuth 2.1
// + RFC 7591 / RFC 9728 handlers in this file; toolsets without an issuer
// fall through to the legacy paths in wellknown.Resolve*.
//
// Handlers in this file:
//
//   - WriteAuthenticateChallenge — 401 + WWW-Authenticate; points clients
//     at RFC 9728 discovery.
//   - HandleGetProtectedResource — GET /.well-known/oauth-protected-resource/mcp/{slug}.
//   - HandleGetAuthorizationServer — GET /.well-known/oauth-authorization-server/mcp/{slug}.
//   - HandleRegister — POST /mcp/{slug}/register (RFC 7591 DCR).

package mcp

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"mime"
	"net/http"
	"net/http/httputil"
	"net/url"
	"slices"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"golang.org/x/crypto/bcrypt"

	"github.com/speakeasy-api/gram/server/internal/attr"
	"github.com/speakeasy-api/gram/server/internal/oauth/wellknown"
	"github.com/speakeasy-api/gram/server/internal/oops"
	usersessions_repo "github.com/speakeasy-api/gram/server/internal/usersessions/repo"
)

// supportedGrantTypes / supportedResponseTypes / supportedAuthMethods /
// supportedBearerMethods mirror the values HandleGetAuthorizationServer and
// HandleGetProtectedResource advertise. Keep in sync — registered clients
// must request only what the AS metadata claims to support.
var (
	supportedGrantTypes    = []string{"authorization_code", "refresh_token"}
	supportedResponseTypes = []string{"code"}
	// `none` covers public PKCE-only clients (mobile, CLI, MCP SDK). Real
	// MCP clients in the wild use it. PKCE provides per-flow integrity; the
	// only guard against cross-flow client-id confusion is the consent
	// prompt itself, which we always render (HandleConsent never skips).
	supportedAuthMethods          = []string{"client_secret_basic", "client_secret_post", "none"}
	supportedBearerMethods        = []string{"header"}
	supportedCodeChallengeMethods = []string{"S256"}
)

// dcrMaxBodyBytes caps the RFC 7591 §3.1 client metadata document size on
// HandleRegister. The spec doesn't mandate a limit; 64 KiB is well past any
// real document and defends against memory-exhaustion (gosec G120).
const dcrMaxBodyBytes int64 = 64 * 1024

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

// dcrError is an RFC 7591 §3.2.2 client registration error in structured
// form. Carries the wire-protocol error code and human-readable description
// to the response writer without leaking either into the function signatures
// of upstream validators.
type dcrError struct {
	Code        string
	Description string
}

func (e *dcrError) Error() string { return e.Code + ": " + e.Description }

// SetDefaults populates the RFC 7591 §2 defaults for fields the client
// didn't supply. Must be called before Validate so the §2.1 grant/response
// correlation check sees materialized values.
func (r *dcrRegistrationRequest) SetDefaults() {
	if len(r.GrantTypes) == 0 {
		r.GrantTypes = []string{"authorization_code"}
	}
	if len(r.ResponseTypes) == 0 {
		r.ResponseTypes = []string{"code"}
	}
	if r.TokenEndpointAuthMethod == "" {
		r.TokenEndpointAuthMethod = "client_secret_basic"
	}
}

// Validate checks the (defaulted) fields of an RFC 7591 §3.1 client metadata
// document. Returns a *dcrError on a spec-defined rejection. Callers must
// invoke SetDefaults first so grant_types / response_types / auth method
// are populated.
func (r *dcrRegistrationRequest) Validate() error {
	if r.ClientName == "" {
		return &dcrError{Code: "invalid_client_metadata", Description: "client_name is required"}
	}
	if len(r.RedirectURIs) == 0 {
		return &dcrError{Code: "invalid_redirect_uri", Description: "redirect_uris is required"}
	}
	for _, u := range r.RedirectURIs {
		parsed, parseErr := url.Parse(u)
		if parseErr != nil || parsed.Scheme == "" || parsed.Host == "" {
			return &dcrError{Code: "invalid_redirect_uri", Description: "redirect_uri must be an absolute URL"}
		}
	}
	for _, gt := range r.GrantTypes {
		if !slices.Contains(supportedGrantTypes, gt) {
			return &dcrError{Code: "invalid_client_metadata", Description: fmt.Sprintf("unsupported grant_type %q", gt)}
		}
	}
	for _, rt := range r.ResponseTypes {
		if !slices.Contains(supportedResponseTypes, rt) {
			return &dcrError{Code: "invalid_client_metadata", Description: fmt.Sprintf("unsupported response_type %q", rt)}
		}
	}
	if !slices.Contains(supportedAuthMethods, r.TokenEndpointAuthMethod) {
		return &dcrError{Code: "invalid_client_metadata", Description: fmt.Sprintf("unsupported token_endpoint_auth_method %q", r.TokenEndpointAuthMethod)}
	}

	// RFC 7591 §2.1 correlation: response_type "code" requires grant_type
	// "authorization_code" and vice versa.
	hasCodeResponse := slices.Contains(r.ResponseTypes, "code")
	hasAuthCodeGrant := slices.Contains(r.GrantTypes, "authorization_code")
	if hasCodeResponse && !hasAuthCodeGrant {
		return &dcrError{Code: "invalid_client_metadata", Description: `response_type "code" requires grant_type "authorization_code"`}
	}
	if hasAuthCodeGrant && !hasCodeResponse {
		return &dcrError{Code: "invalid_client_metadata", Description: `grant_type "authorization_code" requires response_type "code"`}
	}
	// refresh_token can only follow an initial authorization_code in our
	// supported set; a client registering refresh_token alone has no way
	// to ever obtain one.
	if slices.Contains(r.GrantTypes, "refresh_token") && !hasAuthCodeGrant {
		return &dcrError{Code: "invalid_client_metadata", Description: `grant_type "refresh_token" requires grant_type "authorization_code"`}
	}
	return nil
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
			BearerMethodsSupported: supportedBearerMethods,
		})
	}

	// Legacy fallback: delegate to the existing wellknown resolver. A nil
	// result means the toolset has no OAuth configuration at all — 404.
	legacy, err := wellknown.ResolveOAuthProtectedResourceFromToolset(
		ctx, s.logger, s.db, &s.toolsetCache, toolset, baseURL+"/mcp/"+mcpSlug,
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
			CodeChallengeMethodsSupported:     supportedCodeChallengeMethods,
		})
	}

	// Legacy fallback: delegate to the existing wellknown resolver. A nil
	// result means the toolset has no OAuth configuration at all — 404.
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

	if ct := r.Header.Get("Content-Type"); ct != "" {
		mediaType, _, err := mime.ParseMediaType(ct)
		if err != nil || mediaType != "application/json" {
			return writeDCRError(ctx, w, logger, "invalid_client_metadata", "Content-Type must be application/json")
		}
	}

	r.Body = http.MaxBytesReader(w, r.Body, dcrMaxBodyBytes)

	var req dcrRegistrationRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		var maxBytesErr *http.MaxBytesError
		if errors.As(err, &maxBytesErr) {
			return writeDCRError(ctx, w, logger, "invalid_client_metadata", fmt.Sprintf("request body exceeds %d bytes", dcrMaxBodyBytes))
		}
		return writeDCRError(ctx, w, logger, "invalid_client_metadata", "request body is not valid JSON")
	}

	req.SetDefaults()
	if err := req.Validate(); err != nil {
		var dcrErr *dcrError
		if errors.As(err, &dcrErr) {
			return writeDCRError(ctx, w, logger, dcrErr.Code, dcrErr.Description)
		}
		return oops.E(oops.CodeUnexpected, err, "validate DCR request").Log(ctx, logger)
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
