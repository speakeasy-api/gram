package xmcp

import (
	"fmt"
	"net/http"

	"github.com/go-chi/chi/v5"

	"github.com/speakeasy-api/gram/server/internal/attr"
	"github.com/speakeasy-api/gram/server/internal/oops"
)

// HandleWellKnownOAuthServerMetadata serves
// /.well-known/oauth-authorization-server/x/mcp/{mcpSlug}.
//
// Resolution walks slug → mcp_endpoint → mcp_server, then delegates to the
// shared per-backend dispatch on mcp.Service with the "x/mcp" route base.
// Unlike /mcp, /x/mcp has no legacy toolsets.mcp_slug fallback — it is a
// fresh surface keyed entirely on mcp_endpoints. See
// [mcp.Service.ServeWellKnownAuthorizationServerForServer] for the
// per-backend semantics (issuer-gated → Gram-hosted metadata; remote-backed
// → 404; toolset-backed → legacy wellknown resolver).
func (s *Service) HandleWellKnownOAuthServerMetadata(w http.ResponseWriter, r *http.Request) error {
	ctx := r.Context()
	slug := chi.URLParam(r, "mcpSlug")
	if slug == "" {
		return oops.E(oops.CodeBadRequest, nil, "an mcp slug must be provided")
	}

	logger := s.logger.With(attr.SlogToolsetMCPSlug(slug))

	endpoint, mcpServer, err := s.mcpService.ResolveMCPEndpointAndServer(ctx, logger, slug)
	if err != nil {
		return fmt.Errorf("resolve mcp endpoint: %w", err)
	}

	if err := s.mcpService.ServeWellKnownAuthorizationServerForServer(w, r, logger, endpoint, mcpServer, "x/mcp"); err != nil {
		return fmt.Errorf("serve oauth authorization server metadata: %w", err)
	}
	return nil
}

// HandleWellKnownOAuthProtectedResourceMetadata serves
// /.well-known/oauth-protected-resource/x/mcp/{mcpSlug}. Same resolution
// model as [Service.HandleWellKnownOAuthServerMetadata]; the emitted resource
// URL is the runtime URL the caller addressed (`<baseURL>/x/mcp/<slug>`).
func (s *Service) HandleWellKnownOAuthProtectedResourceMetadata(w http.ResponseWriter, r *http.Request) error {
	ctx := r.Context()
	slug := chi.URLParam(r, "mcpSlug")
	if slug == "" {
		return oops.E(oops.CodeBadRequest, nil, "an mcp slug must be provided")
	}

	logger := s.logger.With(attr.SlogToolsetMCPSlug(slug))

	endpoint, mcpServer, err := s.mcpService.ResolveMCPEndpointAndServer(ctx, logger, slug)
	if err != nil {
		return fmt.Errorf("resolve mcp endpoint: %w", err)
	}

	if err := s.mcpService.ServeWellKnownProtectedResourceForServer(w, r, logger, endpoint, mcpServer, "x/mcp"); err != nil {
		return fmt.Errorf("serve oauth protected resource metadata: %w", err)
	}
	return nil
}
