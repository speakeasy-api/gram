package xmcp

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/speakeasy-api/gram/server/internal/attr"
	"github.com/speakeasy-api/gram/server/internal/authz"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/customdomains"
	"github.com/speakeasy-api/gram/server/internal/mcp"
	mcpendpointsrepo "github.com/speakeasy-api/gram/server/internal/mcpendpoints/repo"
	mcpserversrepo "github.com/speakeasy-api/gram/server/internal/mcpservers/repo"
	"github.com/speakeasy-api/gram/server/internal/o11y"
	"github.com/speakeasy-api/gram/server/internal/oops"
	projectsrepo "github.com/speakeasy-api/gram/server/internal/projects/repo"
	"github.com/speakeasy-api/gram/server/internal/remotemcp/proxy"
	remotemcprepo "github.com/speakeasy-api/gram/server/internal/remotemcp/repo"
	toolsetsrepo "github.com/speakeasy-api/gram/server/internal/toolsets/repo"
)

const (
	visibilityPublic   = "public"
	visibilityPrivate  = "private"
	visibilityDisabled = "disabled"
)

// ServeMCP handles DELETE, GET, and POST on /x/mcp/{slug}. It resolves the
// slug (and optional custom domain context) to an mcp_endpoint, loads the
// associated mcp_server, and dispatches to the backend implementation:
// Remote MCP proxy when remote_mcp_server_id is set, or the existing
// toolset-backed serving body when toolset_id is set.
func (s *Service) ServeMCP(w http.ResponseWriter, r *http.Request) error {
	ctx := r.Context()
	defer o11y.LogDefer(ctx, s.logger, func() error {
		return r.Body.Close()
	})

	slug := chi.URLParam(r, "slug")
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
		return s.serveRemoteBackend(w, r, logger, endpoint, mcpServer)
	case mcpServer.ToolsetID.Valid:
		// AGE-1902: toolset-backed branch still reads runtime config from the
		// toolsets row (visibility, OAuth, default environment). Once
		// /mcp/{mcpSlug} is migrated to source these from the linked
		// mcp_servers row instead, this branch should switch to passing the
		// mcp_server config into ServeToolsetResolved (or its successor) and
		// the toolset load below can be dropped.
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

		if err := s.mcpService.ServeToolsetResolved(w, r, &toolset, slug, "x/mcp"); err != nil {
			return fmt.Errorf("serve toolset-backed mcp: %w", err)
		}
		return nil
	default:
		// CHECK constraint mcp_servers_backend_exclusivity_check guarantees
		// exactly one backend is set; this is defensive.
		return oops.E(oops.CodeUnexpected, nil, "mcp server has no backend configured").Log(ctx, logger)
	}
}

// resolveMCPEndpointAndServer walks the runtime addressing chain shared by
// /x/mcp/{slug} and the .well-known routes: it scopes the lookup to the
// request's customdomains.Context, loads the mcp_endpoint by (slug, custom
// domain), then loads the linked mcp_server. Disabled servers and missing
// rows both surface as 404 to avoid leaking existence to unauthenticated
// callers. logger should already carry the slug attribute.
func (s *Service) resolveMCPEndpointAndServer(ctx context.Context, logger *slog.Logger, slug string) (*mcpendpointsrepo.McpEndpoint, *mcpserversrepo.McpServer, error) {
	var customDomainID uuid.NullUUID
	if domainCtx := customdomains.FromContext(ctx); domainCtx != nil {
		customDomainID = uuid.NullUUID{UUID: domainCtx.DomainID, Valid: true}
	}

	endpoint, err := mcpendpointsrepo.New(s.db).GetMCPEndpointByCustomDomainAndSlug(ctx, mcpendpointsrepo.GetMCPEndpointByCustomDomainAndSlugParams{
		Slug:           slug,
		CustomDomainID: customDomainID,
	})
	switch {
	case errors.Is(err, pgx.ErrNoRows):
		return nil, nil, oops.E(oops.CodeNotFound, err, "mcp endpoint not found")
	case err != nil:
		return nil, nil, oops.E(oops.CodeUnexpected, err, "load mcp endpoint").Log(ctx, logger)
	}

	mcpServer, err := mcpserversrepo.New(s.db).GetMCPServerByID(ctx, mcpserversrepo.GetMCPServerByIDParams{
		ID:        endpoint.McpServerID,
		ProjectID: endpoint.ProjectID,
	})
	switch {
	case errors.Is(err, pgx.ErrNoRows):
		return nil, nil, oops.E(oops.CodeNotFound, err, "mcp server not found")
	case err != nil:
		return nil, nil, oops.E(oops.CodeUnexpected, err, "load mcp server").Log(ctx, logger)
	}

	if mcpServer.Visibility == visibilityDisabled {
		return nil, nil, oops.C(oops.CodeNotFound)
	}

	return &endpoint, &mcpServer, nil
}

// serveRemoteBackend handles /x/mcp/{slug} for an mcp_server backed by a
// remote_mcp_server. Auth and visibility come from the mcp_servers row;
// AuthN flow mirrors a strict subset of /mcp's identity-auth handling
// (skipping OAuth-proxy refresh, custom-OAuth validation, and per-tool
// security since those are toolset-only concerns — the upstream Remote
// MCP server handles its own OAuth where applicable).
func (s *Service) serveRemoteBackend(
	w http.ResponseWriter,
	r *http.Request,
	logger *slog.Logger,
	endpoint *mcpendpointsrepo.McpEndpoint,
	mcpServer *mcpserversrepo.McpServer,
) error {
	ctx := r.Context()
	logger = logger.With(attr.SlogRemoteMCPServerID(mcpServer.RemoteMcpServerID.UUID.String()))

	// authorizationOverride is the Bearer token to set on the outgoing
	// upstream request. The caller's Authorization is always dropped by
	// the proxy (Gram credentials don't apply upstream); this value
	// replaces it. Empty means send no Authorization upstream.
	var authorizationOverride string

	// Identity auth + access checks, mirroring the relevant cases of
	// mcp.ServeToolsetResolved. Unrecognised visibility values fail closed
	// in the default branch — disabled was already filtered upstream in
	// ServeMCP.
	switch mcpServer.Visibility {
	case visibilityPrivate:
		// Private mcp_servers require identity auth, that the caller's
		// active org owns the project that owns the server, and an
		// mcp:connect grant. RBAC enforcement only applies to RBAC-gated
		// callers — API keys bypass RBAC by design (they have their own
		// scoping), so the org-membership check is the meaningful gate
		// for API-key callers.
		//
		// TODO: when mcpServer.OauthProxyServerID is set with a "gram"
		// provider, isOAuthCapable below should be true so the caller's
		// Bearer is validated as a Gram-issued OAuth token (rather than
		// only as an API key / chat session). Today it's hardcoded to
		// false because the supporting oauth machinery is keyed by
		// toolset_id and needs generalising to mcp_servers.id first
		// (same blocker as ResolveOAuthProxyUpstreamToken). When
		// isOAuthCapable becomes true, wwwAuthResourceMetadataURL must
		// also be plumbed through (see /x/mcp .well-known route).
		var err error
		ctx, err = s.mcpService.RequirePrivateIdentityAuth(ctx, w, r, false, mcpServer.ID, "")
		if err != nil {
			return fmt.Errorf("private identity auth: %w", err)
		}

		project, err := projectsrepo.New(s.db).GetProjectByID(ctx, endpoint.ProjectID)
		if err != nil {
			return oops.E(oops.CodeUnexpected, err, "load mcp server project").Log(ctx, logger)
		}
		authCtx, ok := contextvalues.GetAuthContext(ctx)
		if !ok || authCtx == nil || project.OrganizationID != authCtx.ActiveOrganizationID {
			return oops.C(oops.CodeUnauthorized)
		}

		ctx, err = s.authz.PrepareContext(ctx)
		if err != nil {
			return oops.E(oops.CodeUnexpected, err, "load access grants").Log(ctx, logger)
		}
		if err := s.authz.Require(ctx, authz.Check{Scope: authz.ScopeMCPConnect, ResourceKind: "", ResourceID: mcpServer.ID.String(), Dimensions: nil}); err != nil {
			return err
		}
	case visibilityPublic:
		switch {
		case mcpServer.OauthProxyServerID.Valid:
			// Public + OAuth proxy ("custom" provider): the caller's
			// Bearer is a Gram-issued OAuth token; resolve it to the
			// user's stored upstream credential and forward that to the
			// remote MCP server.
			//
			// TODO: ResolveOAuthProxyUpstreamToken is currently a stub
			// returning ("", nil) until the OAuth resource model is
			// generalised from toolset_id to mcp_servers.id. For now
			// this branch behaves like a no-token public flow — the
			// upstream receives no Authorization and may reject. Once
			// the stub is implemented, callers with a valid Gram OAuth
			// token will get their stored upstream credential forwarded
			// automatically.
			var err error
			authorizationOverride, err = s.mcpService.ResolveOAuthProxyUpstreamToken(
				ctx,
				mcpServer.OauthProxyServerID.UUID,
				mcpServer.ID,
				mcp.AuthorizationBearerToken(r),
			)
			if err != nil {
				return fmt.Errorf("resolve oauth proxy upstream token: %w", err)
			}
		case mcpServer.ExternalOauthServerID.Valid:
			// Public + external OAuth: the caller's Bearer is intended
			// for the upstream remote MCP server (Gram is not the AS in
			// this configuration), so forward it verbatim.
			authorizationOverride = mcp.AuthorizationBearerToken(r)
		default:
			// Public, no OAuth: optionally probe Gram identity if the
			// caller supplied an Authorization or Gram-Chat-Session
			// token so authenticated callers carry the right context
			// downstream. Nothing meaningful to forward upstream.
			var err error
			ctx, err = s.mcpService.TryPublicIdentityAuth(ctx, r, false, mcpServer.ID)
			if err != nil {
				return fmt.Errorf("public identity auth: %w", err)
			}
		}
	default:
		return oops.E(oops.CodeUnexpected, nil, "unrecognized mcp server visibility %q", mcpServer.Visibility).Log(ctx, logger)
	}

	server, err := remotemcprepo.New(s.db).GetServerByID(ctx, remotemcprepo.GetServerByIDParams{
		ID:        mcpServer.RemoteMcpServerID.UUID,
		ProjectID: endpoint.ProjectID,
	})
	switch {
	case errors.Is(err, pgx.ErrNoRows):
		return oops.E(oops.CodeNotFound, err, "remote mcp server not found").Log(ctx, logger)
	case err != nil:
		return oops.E(oops.CodeUnexpected, err, "load remote mcp server").Log(ctx, logger)
	}

	headers, err := s.newHeadersRepo().ListHeaders(ctx, server.ID, false)
	if err != nil {
		return oops.E(oops.CodeUnexpected, err, "load remote mcp server headers").Log(ctx, logger)
	}

	p := s.buildProxy(logger, &server, headers, authorizationOverride)

	r = r.WithContext(ctx)

	switch r.Method {
	case http.MethodDelete:
		if err := p.Delete(w, r); err != nil {
			return fmt.Errorf("proxy delete: %w", err)
		}
		return nil
	case http.MethodGet:
		if err := p.Get(w, r); err != nil {
			return fmt.Errorf("proxy get: %w", err)
		}
		return nil
	case http.MethodPost:
		if err := p.Post(w, r); err != nil {
			return fmt.Errorf("proxy post: %w", err)
		}
		return nil
	default:
		// The mux only registers the three supported methods, so this is
		// defensive.
		return oops.E(oops.CodeBadRequest, nil, "unsupported method %s", r.Method)
	}
}

// buildProxy converts the loaded DB types into the typed configuration
// expected by the remotemcp/proxy package. authorizationOverride is the
// Bearer token to set on the outgoing upstream request — leave empty to
// send no Authorization. The caller's incoming Authorization header is
// always dropped by the proxy regardless of this value.
func (s *Service) buildProxy(logger *slog.Logger, server *remotemcprepo.RemoteMcpServer, headers []remotemcprepo.RemoteMcpServerHeader, authorizationOverride string) *proxy.Proxy {
	configured := make([]proxy.ConfiguredHeader, 0, len(headers))
	for _, h := range headers {
		configured = append(configured, proxy.ConfiguredHeader{
			Name:                   h.Name,
			StaticValue:            h.Value.String,
			ValueFromRequestHeader: h.ValueFromRequestHeader.String,
			IsRequired:             h.IsRequired,
		})
	}

	return &proxy.Proxy{
		GuardianPolicy:            s.guardianPolicy,
		Logger:                    logger,
		Tracer:                    s.tracer,
		NonStreamingTimeout:       proxy.DefaultNonStreamingTimeout,
		StreamingTimeout:          proxy.DefaultStreamingTimeout,
		Metrics:                   s.proxyMetrics,
		MaxBufferedBodyBytes:      proxy.DefaultMaxBufferedBodyBytes,
		ServerID:                  server.ID.String(),
		RemoteURL:                 server.Url,
		Headers:                   configured,
		AuthorizationOverride:     authorizationOverride,
		UserRequestInterceptors:   nil,
		RemoteMessageInterceptors: nil,
		ToolsCallRequestInterceptors: []proxy.ToolsCallRequestInterceptor{
			s.toolUsageLimitsInterceptor,
		},
		ToolsCallResponseInterceptors: []proxy.ToolsCallResponseInterceptor{
			s.toolUsageTrackingInterceptor,
		},
		ToolsListRequestInterceptors:  nil,
		ToolsListResponseInterceptors: nil,
	}
}
