// Well-known metadata handlers for the issuer-gated OAuth surface:
// RFC 9728 protected-resource metadata and RFC 8414 authorization-server
// metadata. Both routes dispatch internally on
// toolsets.user_session_issuer_id — issuer-gated toolsets get the new
// metadata shape, legacy toolsets fall through to wellknown.Resolve*.

package mcp

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"net/http/httputil"
	"net/url"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"

	"github.com/speakeasy-api/gram/server/internal/customdomains"
	"github.com/speakeasy-api/gram/server/internal/oauth/wellknown"
	"github.com/speakeasy-api/gram/server/internal/oops"
	"github.com/speakeasy-api/gram/server/internal/usersessions"
)

// supportedBearerMethods advertises what the MCP resource-server surface
// accepts in the WWW-Authenticate challenge (RFC 9728). The AS-level
// supported sets (grant types, response types, auth methods, code-challenge
// methods) live in the usersessions package — referenced from the AS
// metadata document below.
var supportedBearerMethods = []string{"header"}

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

	var customDomainID uuid.NullUUID
	if domainCtx := customdomains.FromContext(ctx); domainCtx != nil {
		customDomainID = uuid.NullUUID{UUID: domainCtx.DomainID, Valid: true}
	}
	toolset, err := s.loadToolset(ctx, mcpSlug, customDomainID, false)
	switch {
	case errors.Is(err, errToolsetNotFound):
		return oops.E(oops.CodeNotFound, err, "mcp server not found")
	case err != nil:
		return oops.E(oops.CodeUnexpected, err, "failed to load MCP server").Log(ctx, s.logger)
	}

	baseURL := s.BaseURLForRequest(r)

	if toolset.UserSessionIssuerID.Valid {
		endpoint := newResolvedMcpEndpointFromToolset(toolset, "mcp")
		if err := s.RequireUserSessionIssuer(ctx, endpoint); err != nil {
			return err
		}
		return s.ServeGetProtectedResource(w, r, endpoint)
	}

	// Legacy fallback: delegate to the existing wellknown resolver. A nil
	// result means the toolset has no OAuth configuration at all — 404.
	resourceURL, err := url.JoinPath(baseURL, "mcp", mcpSlug)
	if err != nil {
		return oops.E(oops.CodeUnexpected, err, "build legacy resource URL").Log(ctx, s.logger)
	}
	legacy, err := wellknown.ResolveOAuthProtectedResourceFromToolset(
		ctx, s.logger, s.db, &s.toolsetCache, toolset, resourceURL,
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

	var customDomainID uuid.NullUUID
	if domainCtx := customdomains.FromContext(ctx); domainCtx != nil {
		customDomainID = uuid.NullUUID{UUID: domainCtx.DomainID, Valid: true}
	}
	toolset, err := s.loadToolset(ctx, mcpSlug, customDomainID, false)
	switch {
	case errors.Is(err, errToolsetNotFound):
		return oops.E(oops.CodeNotFound, err, "mcp server not found")
	case err != nil:
		return oops.E(oops.CodeUnexpected, err, "failed to load MCP server").Log(ctx, s.logger)
	}

	baseURL := s.BaseURLForRequest(r)

	if toolset.UserSessionIssuerID.Valid {
		endpoint := newResolvedMcpEndpointFromToolset(toolset, "mcp")
		if err := s.RequireUserSessionIssuer(ctx, endpoint); err != nil {
			return err
		}
		return s.ServeGetAuthorizationServer(w, r, endpoint)
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

// ServeGetProtectedResource is the post-resolution entry point for the
// RFC 9728 protected-resource metadata response, shared by /mcp's
// HandleGetProtectedResource (toolset-keyed) and /x/mcp's mcp_endpoint-
// keyed route registration. Emits the issuer-gated metadata shape; the
// legacy non-issuer-gated fallback stays in HandleGetProtectedResource
// because it depends on the toolsets row directly.
func (s *Service) ServeGetProtectedResource(w http.ResponseWriter, r *http.Request, endpoint *ResolvedMcpEndpoint) error {
	ctx := r.Context()
	baseURL := s.BaseURLForRequest(r)
	resource, err := endpoint.RootURL(baseURL)
	if err != nil {
		return oops.E(oops.CodeUnexpected, err, "build resource URL").Log(ctx, s.logger)
	}
	return writeJSONMetadata(ctx, w, s.logger, oauthProtectedResourceMetadata{
		Resource:               resource,
		AuthorizationServers:   []string{resource},
		ScopesSupported:        nil,
		BearerMethodsSupported: supportedBearerMethods,
	})
}

// ServeGetAuthorizationServer is the post-resolution entry point for the
// RFC 8414 authorization-server metadata response, shared by /mcp's
// HandleGetAuthorizationServer (toolset-keyed) and /x/mcp's
// mcp_endpoint-keyed route registration. Emits the issuer-gated
// metadata shape; the legacy non-issuer-gated fallback stays in
// HandleGetAuthorizationServer.
func (s *Service) ServeGetAuthorizationServer(w http.ResponseWriter, r *http.Request, endpoint *ResolvedMcpEndpoint) error {
	ctx := r.Context()
	baseURL := s.BaseURLForRequest(r)
	urls, err := endpoint.AuthorizationServerURLs(baseURL)
	if err != nil {
		return oops.E(oops.CodeUnexpected, err, "build OAuth server URLs").Log(ctx, s.logger)
	}
	return writeJSONMetadata(ctx, w, s.logger, oauthAuthorizationServerMetadata{
		Issuer:                            urls.Issuer,
		AuthorizationEndpoint:             urls.Authorize,
		TokenEndpoint:                     urls.Token,
		RegistrationEndpoint:              urls.Register,
		RevocationEndpoint:                urls.Revoke,
		ScopesSupported:                   nil,
		ResponseTypesSupported:            usersessions.SupportedResponseTypes,
		GrantTypesSupported:               usersessions.SupportedGrantTypes,
		TokenEndpointAuthMethodsSupported: usersessions.SupportedAuthMethods,
		CodeChallengeMethodsSupported:     usersessions.SupportedCodeChallengeMethods,
	})
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
