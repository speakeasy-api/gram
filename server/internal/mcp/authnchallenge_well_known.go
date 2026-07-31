// Well-known metadata handlers for the issuer-gated OAuth surface:
// RFC 9728 protected-resource metadata and RFC 8414 authorization-server
// metadata. Both routes resolve through mcp_endpoints → mcp_servers —
// issuer-gated servers get the new metadata shape, non-issuer-gated
// toolset-backed servers fall through to wellknown.Resolve*.

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
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/speakeasy-api/gram/server/internal/attr"
	"github.com/speakeasy-api/gram/server/internal/conv"
	"github.com/speakeasy-api/gram/server/internal/httpcache"
	"github.com/speakeasy-api/gram/server/internal/mcpendpoints"
	mcpendpoints_repo "github.com/speakeasy-api/gram/server/internal/mcpendpoints/repo"
	mcpservers_repo "github.com/speakeasy-api/gram/server/internal/mcpservers/repo"
	"github.com/speakeasy-api/gram/server/internal/oauth/wellknown"
	"github.com/speakeasy-api/gram/server/internal/oops"
	toolsets_repo "github.com/speakeasy-api/gram/server/internal/toolsets/repo"
	"github.com/speakeasy-api/gram/server/internal/usersessions"
)

// metadataCacheMaxAgeSeconds is the Cache-Control max-age for public well-known
// OAuth-metadata responses. Deliberately short: the documents change rarely but
// an issuer's config can, so a 60s TTL caps how long a stale document lingers
// while still absorbing bursts.
const metadataCacheMaxAgeSeconds = 60

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
	// ClientIDMetadataDocumentSupported advertises inbound CIMD support
	// (draft-ietf-oauth-client-id-metadata-document-02 §6). Emitted as true
	// only when the issuer organization's gram-user-session-cimd flag is
	// on; omitted otherwise.
	ClientIDMetadataDocumentSupported *bool `json:"client_id_metadata_document_supported,omitempty"`
}

// HandleGetProtectedResource serves RFC 9728 protected-resource metadata at
// the canonical RFC path `/.well-known/oauth-protected-resource/mcp/{mcpSlug}`
// — the only path a spec-compliant client constructs from a resource URL of
// `<base>/mcp/{slug}`.
//
// Resolution follows ServePublic: mcp_endpoints → mcp_servers, then the
// shared per-backend dispatch (ServeWellKnownProtectedResourceForServer).
// mcp_endpoints is authoritative for its slug — an address miss is a 404.
func (s *Service) HandleGetProtectedResource(w http.ResponseWriter, r *http.Request) error {
	ctx := r.Context()
	mcpSlug := chi.URLParam(r, "mcpSlug")
	if mcpSlug == "" {
		return oops.E(oops.CodeBadRequest, nil, "an mcp slug must be provided").LogError(ctx, s.logger)
	}

	logger := s.logger.With(attr.SlogToolsetMCPSlug(mcpSlug))

	mcpEndpoint, mcpServer, err := s.ResolveMCPEndpointAndServer(ctx, logger, mcpSlug)
	if err != nil {
		if errors.Is(err, mcpendpoints.ErrAddressMiss) {
			return oops.E(oops.CodeNotFound, err, "mcp server not found")
		}
		return err
	}
	return s.ServeWellKnownProtectedResourceForServer(w, r, logger, mcpEndpoint, mcpServer, "mcp")
}

// HandleGetAuthorizationServer serves RFC 8414 authorization-server metadata
// at the canonical RFC path
// `/.well-known/oauth-authorization-server/mcp/{mcpSlug}` — the only path a
// spec-compliant client constructs from an issuer URL of `<base>/mcp/{slug}`.
// Same resolution model as HandleGetProtectedResource.
func (s *Service) HandleGetAuthorizationServer(w http.ResponseWriter, r *http.Request) error {
	ctx := r.Context()
	mcpSlug := chi.URLParam(r, "mcpSlug")
	if mcpSlug == "" {
		return oops.E(oops.CodeBadRequest, nil, "an mcp slug must be provided").LogError(ctx, s.logger)
	}

	logger := s.logger.With(attr.SlogToolsetMCPSlug(mcpSlug))

	mcpEndpoint, mcpServer, err := s.ResolveMCPEndpointAndServer(ctx, logger, mcpSlug)
	if err != nil {
		if errors.Is(err, mcpendpoints.ErrAddressMiss) {
			return oops.E(oops.CodeNotFound, err, "mcp server not found")
		}
		return err
	}
	return s.ServeWellKnownAuthorizationServerForServer(w, r, logger, mcpEndpoint, mcpServer, "mcp")
}

// ServeWellKnownProtectedResourceForServer serves RFC 9728 protected-resource
// metadata for an already-resolved (mcp_endpoint, mcp_server) pair. It is the
// single per-backend dispatch shared by the /mcp (routeBase "mcp") and /x/mcp
// (routeBase "x/mcp") well-known surfaces:
//
//   - Issuer-gated (any backend): emit the Gram-hosted metadata shape rooted
//     at the resolved endpoint's URL on routeBase's surface.
//   - Remote-backed, not issuer-gated: 404 — the upstream remote MCP server
//     publishes its own .well-known and Gram is not its authorization server.
//   - Toolset-backed, not issuer-gated: reuse the legacy wellknown resolver
//     (oauth_proxy_server_id / external_oauth_server_id).
func (s *Service) ServeWellKnownProtectedResourceForServer(
	w http.ResponseWriter,
	r *http.Request,
	logger *slog.Logger,
	mcpEndpoint *mcpendpoints_repo.McpEndpoint,
	mcpServer *mcpservers_repo.McpServer,
	routeBase string,
) error {
	ctx := r.Context()

	// Public tunneled servers are anonymous: no OAuth metadata exists for
	// them despite the populated issuer column.
	if isTunneledPublic(mcpServer) {
		return oops.E(oops.CodeNotFound, nil, "no OAuth configuration found")
	}

	if mcpServer.UserSessionIssuerID.Valid {
		endpoint, err := s.BuildResolvedMcpEndpointForServer(ctx, logger, mcpEndpoint, mcpServer, routeBase)
		if err != nil {
			return fmt.Errorf("build resolved mcp endpoint: %w", err)
		}
		return s.ServeGetProtectedResource(w, r, endpoint)
	}

	switch {
	case mcpServer.RemoteMcpServerID.Valid:
		return oops.E(oops.CodeNotFound, nil, "no OAuth configuration found")
	case mcpServer.TunneledMcpServerID.Valid:
		return oops.E(oops.CodeNotFound, nil, "no OAuth configuration found")
	case mcpServer.ToolsetID.Valid:
		toolset, err := s.loadToolsetForServer(ctx, logger, mcpServer.ToolsetID.UUID, mcpEndpoint.ProjectID)
		if err != nil {
			return err
		}
		// A toolset user-session issuer gates this endpoint even though
		// the wrapper's own issuer column is NULL: serve the Gram-hosted
		// metadata shape bound to the endpoint's identity.
		if toolset.UserSessionIssuerID.Valid {
			endpoint := newResolvedMcpEndpointFromToolsetBackedServer(mcpEndpoint, mcpServer, toolset, routeBase)
			if err := s.RequireUserSessionIssuer(ctx, endpoint); err != nil {
				return err
			}
			return s.ServeGetProtectedResource(w, r, endpoint)
		}
		resourceURL, err := url.JoinPath(s.BaseURLForRequest(r), routeBase, mcpEndpoint.Slug)
		if err != nil {
			return oops.E(oops.CodeUnexpected, err, "build resource URL").LogError(ctx, logger)
		}
		return s.serveLegacyToolsetProtectedResource(ctx, w, r, logger, toolset, resourceURL)
	default:
		return oops.E(oops.CodeUnexpected, nil, "mcp server has no backend configured").LogError(ctx, logger)
	}
}

// ServeWellKnownAuthorizationServerForServer serves RFC 8414
// authorization-server metadata for an already-resolved (mcp_endpoint,
// mcp_server) pair. Shared per-backend dispatch with the same backend
// semantics as [Service.ServeWellKnownProtectedResourceForServer].
func (s *Service) ServeWellKnownAuthorizationServerForServer(
	w http.ResponseWriter,
	r *http.Request,
	logger *slog.Logger,
	mcpEndpoint *mcpendpoints_repo.McpEndpoint,
	mcpServer *mcpservers_repo.McpServer,
	routeBase string,
) error {
	ctx := r.Context()

	if isTunneledPublic(mcpServer) {
		return oops.E(oops.CodeNotFound, nil, "no OAuth configuration found")
	}

	if mcpServer.UserSessionIssuerID.Valid {
		endpoint, err := s.BuildResolvedMcpEndpointForServer(ctx, logger, mcpEndpoint, mcpServer, routeBase)
		if err != nil {
			return fmt.Errorf("build resolved mcp endpoint: %w", err)
		}
		return s.ServeGetAuthorizationServer(w, r, endpoint)
	}

	switch {
	case mcpServer.RemoteMcpServerID.Valid:
		return oops.E(oops.CodeNotFound, nil, "no OAuth configuration found")
	case mcpServer.TunneledMcpServerID.Valid:
		return oops.E(oops.CodeNotFound, nil, "no OAuth configuration found")
	case mcpServer.ToolsetID.Valid:
		toolset, err := s.loadToolsetForServer(ctx, logger, mcpServer.ToolsetID.UUID, mcpEndpoint.ProjectID)
		if err != nil {
			return err
		}
		// A toolset user-session issuer gates this endpoint even though
		// the wrapper's own issuer column is NULL: serve the Gram-hosted
		// metadata shape bound to the endpoint's identity.
		if toolset.UserSessionIssuerID.Valid {
			endpoint := newResolvedMcpEndpointFromToolsetBackedServer(mcpEndpoint, mcpServer, toolset, routeBase)
			if err := s.RequireUserSessionIssuer(ctx, endpoint); err != nil {
				return err
			}
			return s.ServeGetAuthorizationServer(w, r, endpoint)
		}
		// The /oauth machinery is keyed on the endpoint's public slug — the
		// same address the client used — and the resource URL mirrors
		// ServeWellKnownProtectedResourceForServer (routeBase +
		// mcp_endpoints.slug) so the served issuer matches the
		// protected-resource metadata's authorization_servers entry.
		resourceURL, err := url.JoinPath(s.BaseURLForRequest(r), routeBase, mcpEndpoint.Slug)
		if err != nil {
			return oops.E(oops.CodeUnexpected, err, "build resource URL").LogError(ctx, logger)
		}
		return s.serveLegacyToolsetAuthorizationServer(ctx, w, r, logger, toolset, mcpEndpoint.Slug, resourceURL)
	default:
		return oops.E(oops.CodeUnexpected, nil, "mcp server has no backend configured").LogError(ctx, logger)
	}
}

// loadToolsetForServer loads the toolset a toolset-backed mcp_server points
// at, mapping a missing row to 404. Shared by the toolset-backed branch of
// both well-known dispatchers.
func (s *Service) loadToolsetForServer(ctx context.Context, logger *slog.Logger, toolsetID, projectID uuid.UUID) (*toolsets_repo.Toolset, error) {
	toolset, err := toolsets_repo.New(s.db).GetToolsetByIDAndProject(ctx, toolsets_repo.GetToolsetByIDAndProjectParams{
		ID:        toolsetID,
		ProjectID: projectID,
	})
	switch {
	case errors.Is(err, pgx.ErrNoRows):
		return nil, oops.E(oops.CodeNotFound, err, "toolset not found")
	case err != nil:
		return nil, oops.E(oops.CodeUnexpected, err, "load toolset").LogError(ctx, logger)
	}
	return &toolset, nil
}

// serveLegacyToolsetProtectedResource resolves and writes RFC 9728
// protected-resource metadata for a toolset via the legacy wellknown
// resolver. A nil result means the toolset carries no OAuth configuration —
// 404. resourceURL is the runtime URL the caller addressed; it is emitted
// verbatim as both `resource` and `authorization_servers`.
func (s *Service) serveLegacyToolsetProtectedResource(ctx context.Context, w http.ResponseWriter, r *http.Request, logger *slog.Logger, toolset *toolsets_repo.Toolset, resourceURL string) error {
	metadata, err := wellknown.ResolveOAuthProtectedResourceFromToolset(ctx, logger, s.db, &s.toolsetCache, toolset, resourceURL)
	if err != nil {
		return oops.E(oops.CodeUnexpected, err, "failed to resolve OAuth protected resource metadata").LogError(ctx, logger)
	}
	if metadata == nil {
		return oops.E(oops.CodeNotFound, nil, "no OAuth configuration found")
	}
	return writeOAuthProtectedResourceMetadataResponse(ctx, logger, w, r, metadata)
}

// serveLegacyToolsetAuthorizationServer resolves and writes RFC 8414
// authorization-server metadata for a toolset via the legacy wellknown
// resolver, dispatching the proxy variant through a reverse proxy. A nil
// result means the toolset carries no OAuth configuration — 404. oauthSlug
// keys the emitted issuer / endpoint URLs onto the legacy /oauth/{slug}
// surface.
func (s *Service) serveLegacyToolsetAuthorizationServer(ctx context.Context, w http.ResponseWriter, r *http.Request, logger *slog.Logger, toolset *toolsets_repo.Toolset, oauthSlug, resourceURL string) error {
	result, err := wellknown.ResolveOAuthServerMetadataFromToolset(ctx, logger, s.db, s.oauthRepo, &s.toolsetCache, toolset, s.BaseURLForRequest(r), oauthSlug, resourceURL)
	if err != nil {
		return oops.E(oops.CodeUnexpected, err, "failed to resolve OAuth server metadata").LogError(ctx, logger)
	}
	if result == nil {
		return oops.E(oops.CodeNotFound, nil, "no OAuth configuration found")
	}

	if result.Kind == wellknown.OAuthServerMetadataResultKindProxy {
		target, parseErr := url.Parse(result.ProxyURL)
		if parseErr != nil {
			return oops.E(oops.CodeUnexpected, parseErr, "failed to parse well-known URL").LogError(ctx, logger)
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

	return writeOAuthServerMetadataResponse(ctx, logger, w, r, result)
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
		return oops.E(oops.CodeUnexpected, err, "build resource URL").LogError(ctx, s.logger)
	}
	return writeJSONMetadata(ctx, w, r, s.logger, oauthProtectedResourceMetadata{
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
		return oops.E(oops.CodeUnexpected, err, "build OAuth server URLs").LogError(ctx, s.logger)
	}
	// The response is served with cache headers (writeJSONMetadata), so a
	// flag flip propagates to clients only after their cached copy expires.
	var cimdSupported *bool
	if s.userSessionCIMDEnabled(ctx, s.logger, endpoint) {
		cimdSupported = conv.PtrEmpty(true)
	}
	return writeJSONMetadata(ctx, w, r, s.logger, oauthAuthorizationServerMetadata{
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
		ClientIDMetadataDocumentSupported: cimdSupported,
	})
}

// writeJSONMetadata is the shared write path for issuer-gated metadata
// responses. Marshals the value, then commits a public, cacheable 200 (or a 304
// when the caller's If-None-Match matches).
func writeJSONMetadata(ctx context.Context, w http.ResponseWriter, r *http.Request, logger *slog.Logger, v any) error {
	body, err := json.Marshal(v)
	if err != nil {
		return oops.E(oops.CodeUnexpected, err, "marshal metadata").LogError(ctx, logger)
	}
	return httpcache.WriteCacheableJSON(ctx, w, r, logger, "application/json", metadataCacheMaxAgeSeconds, body)
}
