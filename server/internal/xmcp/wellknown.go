package xmcp

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

	"github.com/speakeasy-api/gram/server/internal/attr"
	"github.com/speakeasy-api/gram/server/internal/customdomains"
	"github.com/speakeasy-api/gram/server/internal/oauth/repo"
	"github.com/speakeasy-api/gram/server/internal/oauth/wellknown"
	"github.com/speakeasy-api/gram/server/internal/oops"
	toolsetsrepo "github.com/speakeasy-api/gram/server/internal/toolsets/repo"
)

// baseURLForRequest returns the appropriate base URL for well-known metadata
// responses, switching to the custom domain when the request was resolved
// through one.
func (s *Service) baseURLForRequest(r *http.Request) string {
	if domainCtx := customdomains.FromContext(r.Context()); domainCtx != nil {
		return fmt.Sprintf("https://%s", domainCtx.Domain)
	}
	return s.serverURL.String()
}

// HandleWellKnownOAuthServerMetadata serves
// /.well-known/oauth-authorization-server/x/mcp/{mcpSlug}.
//
// Resolution mirrors the runtime path: slug → mcp_endpoint → mcp_server.
// Dispatch is per-backend so each backend can source OAuth state from the
// model that fits it best:
//
//   - Toolset-backed: load the linked toolset by ID and reuse the existing
//     toolset-keyed wellknown resolver. The OAuth flow itself is still
//     keyed by toolsets.mcp_slug today; the production model assumes
//     mcp_endpoints.slug == toolsets.mcp_slug for these servers, and a
//     separate upcoming OAuth migration (tracked independently of
//     AGE-1902) will re-key the OAuth machinery onto mcp_servers.id, at
//     which point the dependency on toolsets.mcp_slug drops entirely.
//   - Remote-backed: returns 404 today. Remote MCP servers publish their
//     own .well-known and Gram does not yet act as an authorization server
//     for them. Once that upcoming OAuth migration generalises the
//     machinery off toolset_id, this branch will source from mcp_servers /
//     oauth_proxy_servers.
func (s *Service) HandleWellKnownOAuthServerMetadata(w http.ResponseWriter, r *http.Request) error {
	ctx := r.Context()
	slug := chi.URLParam(r, "mcpSlug")
	if slug == "" {
		return oops.E(oops.CodeBadRequest, nil, "an mcp slug must be provided")
	}

	logger := s.logger.With(attr.SlogToolsetMCPSlug(slug))

	endpoint, mcpServer, err := s.resolveMCPEndpointAndServer(ctx, logger, slug)
	if err != nil {
		return err
	}

	switch {
	case mcpServer.RemoteMcpServerID.Valid:
		// The upcoming OAuth migration (separate from AGE-1902) will
		// surface OAuth metadata from mcp_servers / oauth_proxy_servers
		// directly. Until then there is no Gram-hosted authorization
		// server for remote-backed mcp_servers — the upstream remote MCP
		// server publishes its own .well-known.
		return oops.E(oops.CodeNotFound, nil, "no OAuth configuration found")
	case mcpServer.ToolsetID.Valid:
		toolset, err := toolsetsrepo.New(s.db).GetToolsetByIDAndProject(ctx, toolsetsrepo.GetToolsetByIDAndProjectParams{
			ID:        mcpServer.ToolsetID.UUID,
			ProjectID: endpoint.ProjectID,
		})
		switch {
		case errors.Is(err, pgx.ErrNoRows):
			return oops.E(oops.CodeNotFound, err, "toolset not found")
		case err != nil:
			return oops.E(oops.CodeUnexpected, err, "load toolset").Log(ctx, logger)
		}

		// Today's OAuth machinery (issuance, callback, token exchange) is
		// still keyed by toolsets.mcp_slug, so the issuer family points at
		// /oauth/{toolset.mcp_slug}/... — see the function comment for
		// the upcoming OAuth migration that will re-key onto mcp_servers.
		oauthSlug := toolset.McpSlug.String
		if oauthSlug == "" {
			return oops.E(oops.CodeNotFound, nil, "no OAuth configuration found")
		}

		baseURL := s.baseURLForRequest(r)
		result, err := wellknown.ResolveOAuthServerMetadataFromToolset(
			ctx,
			logger,
			s.db,
			repo.New(s.db),
			nil,
			&toolset,
			baseURL,
			oauthSlug,
		)
		if err != nil {
			return oops.E(oops.CodeUnexpected, err, "failed to resolve OAuth server metadata").Log(ctx, logger)
		}

		if result == nil {
			return oops.E(oops.CodeNotFound, nil, "no OAuth configuration found")
		}

		return writeOAuthServerMetadata(ctx, w, r, logger, result)
	default:
		return oops.E(oops.CodeUnexpected, nil, "mcp server has no backend configured").Log(ctx, logger)
	}
}

// HandleWellKnownOAuthProtectedResourceMetadata serves
// /.well-known/oauth-protected-resource/x/mcp/{mcpSlug}.
//
// The resource URL embedded in the response is the runtime URL the caller
// is actually addressing — `<baseURL>/x/mcp/<mcp_endpoint.slug>`. See
// [Service.HandleWellKnownOAuthServerMetadata] for the dispatch rationale.
func (s *Service) HandleWellKnownOAuthProtectedResourceMetadata(w http.ResponseWriter, r *http.Request) error {
	ctx := r.Context()
	slug := chi.URLParam(r, "mcpSlug")
	if slug == "" {
		return oops.E(oops.CodeBadRequest, nil, "an mcp slug must be provided")
	}

	logger := s.logger.With(attr.SlogToolsetMCPSlug(slug))

	endpoint, mcpServer, err := s.resolveMCPEndpointAndServer(ctx, logger, slug)
	if err != nil {
		return err
	}

	switch {
	case mcpServer.RemoteMcpServerID.Valid:
		// See HandleWellKnownOAuthServerMetadata for the rationale —
		// remote-backed Gram-hosted OAuth metadata is gated on the
		// upcoming OAuth migration.
		return oops.E(oops.CodeNotFound, nil, "not found")
	case mcpServer.ToolsetID.Valid:
		toolset, err := toolsetsrepo.New(s.db).GetToolsetByIDAndProject(ctx, toolsetsrepo.GetToolsetByIDAndProjectParams{
			ID:        mcpServer.ToolsetID.UUID,
			ProjectID: endpoint.ProjectID,
		})
		switch {
		case errors.Is(err, pgx.ErrNoRows):
			return oops.E(oops.CodeNotFound, err, "toolset not found")
		case err != nil:
			return oops.E(oops.CodeUnexpected, err, "load toolset").Log(ctx, logger)
		}

		resourceURL := s.baseURLForRequest(r) + "/x/mcp/" + endpoint.Slug
		metadata, err := wellknown.ResolveOAuthProtectedResourceFromToolset(
			ctx,
			logger,
			s.db,
			nil,
			&toolset,
			resourceURL,
		)
		if err != nil {
			return oops.E(oops.CodeUnexpected, err, "failed to resolve OAuth protected resource metadata").Log(ctx, logger)
		}

		if metadata == nil {
			return oops.E(oops.CodeNotFound, nil, "not found")
		}

		// Marshal before committing the 200 — see writeOAuthServerMetadata.
		body, err := json.Marshal(metadata)
		if err != nil {
			return oops.E(oops.CodeUnexpected, err, "failed to marshal OAuth protected resource metadata").Log(ctx, logger)
		}

		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		if _, err := w.Write(body); err != nil {
			return oops.E(oops.CodeUnexpected, err, "failed to write response body").Log(ctx, logger)
		}
		return nil
	default:
		return oops.E(oops.CodeUnexpected, nil, "mcp server has no backend configured").Log(ctx, logger)
	}
}

// writeOAuthServerMetadata serialises a resolver result onto the response,
// dispatching the proxy variant via httputil.ReverseProxy.
func writeOAuthServerMetadata(
	ctx context.Context,
	w http.ResponseWriter,
	r *http.Request,
	logger *slog.Logger,
	result *wellknown.OAuthServerMetadataResult,
) error {
	if result.Kind == wellknown.OAuthServerMetadataResultKindProxy {
		target, err := url.Parse(result.ProxyURL)
		if err != nil {
			return oops.E(oops.CodeUnexpected, err, "failed to parse well-known URL").Log(ctx, logger)
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

	// Resolve the response body before committing the 200 status. Once
	// WriteHeader is called we lose the ability to surface a downstream
	// failure (marshalling, unexpected kind) as a proper error response,
	// so any work that can fail must happen first.
	var body []byte
	switch result.Kind {
	case wellknown.OAuthServerMetadataResultKindRaw:
		body = result.Raw
	case wellknown.OAuthServerMetadataResultKindStatic:
		marshalled, err := json.Marshal(result.Static)
		if err != nil {
			return oops.E(oops.CodeUnexpected, err, "failed to marshal OAuth server metadata").Log(ctx, logger)
		}
		body = marshalled
	default:
		return oops.E(oops.CodeUnexpected, nil, "unexpected OAuth server metadata result kind").Log(ctx, logger)
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	if _, err := w.Write(body); err != nil {
		return oops.E(oops.CodeUnexpected, err, "failed to write response body").Log(ctx, logger)
	}
	return nil
}
