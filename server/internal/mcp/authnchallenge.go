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
//   6. HandleClientLoginCallback — GET /mcp/{slug}/client_login_callback
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
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"net/http/httputil"
	"net/url"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5"

	"github.com/speakeasy-api/gram/server/internal/oauth/wellknown"
	"github.com/speakeasy-api/gram/server/internal/oops"
	usersessions_repo "github.com/speakeasy-api/gram/server/internal/usersessions/repo"
)

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
			ResponseTypesSupported:            []string{"code"},
			GrantTypesSupported:               []string{"authorization_code", "refresh_token"},
			TokenEndpointAuthMethodsSupported: []string{"client_secret_basic", "client_secret_post", "none"},
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
