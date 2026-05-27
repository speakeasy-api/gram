package mcp

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/speakeasy-api/gram/server/internal/attr"
	"github.com/speakeasy-api/gram/server/internal/authz"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/customdomains"
	mcpendpointsrepo "github.com/speakeasy-api/gram/server/internal/mcpendpoints/repo"
	"github.com/speakeasy-api/gram/server/internal/mcpservers"
	mcpserversrepo "github.com/speakeasy-api/gram/server/internal/mcpservers/repo"
	"github.com/speakeasy-api/gram/server/internal/oops"
	projectsrepo "github.com/speakeasy-api/gram/server/internal/projects/repo"
	"github.com/speakeasy-api/gram/server/internal/remotemcp"
	remotemcprepo "github.com/speakeasy-api/gram/server/internal/remotemcp/repo"
	toolsetsrepo "github.com/speakeasy-api/gram/server/internal/toolsets/repo"
)

// ServeMCPEndpoint resolves the given mcp_endpoint slug to its mcp_server,
// optionally runs the issuer gate, and dispatches to the appropriate
// backend (remote_mcp_servers via the remotemcp proxy, or toolsets via
// ServeToolsetResolved). It is the unified runtime entry point used by
// both /mcp and /x/mcp.
//
// mcpRouteBase is the URL route segment the request arrived under ("mcp"
// or "x/mcp"), without leading or trailing slashes. It propagates into
// WWW-Authenticate URLs, OAuth issuer URLs, and consent action targets so
// post-resolution URLs match the surface the client called.
//
// The caller is responsible for closing r.Body.
func (s *Service) ServeMCPEndpoint(w http.ResponseWriter, r *http.Request, slug, mcpRouteBase string) error {
	ctx := r.Context()
	logger := s.logger.With(attr.SlogToolsetMCPSlug(slug))

	mcpEndpoint, mcpServer, err := s.ResolveMCPEndpointAndServer(ctx, logger, slug)
	if err != nil {
		return err
	}

	// Issuer-gated mcp_servers run the JWT-validation branch here, before
	// backend dispatch. ServeToolsetResolved then skips its in-toolset
	// gate (skipIssuerGate=true) so the same request isn't gated twice;
	// remote-backed proxying forwards the upstream remote-session token
	// via AuthorizationOverride.
	var upstreamToken string
	if mcpServer.UserSessionIssuerID.Valid {
		resolvedEndpoint, err := s.BuildResolvedMcpEndpointForServer(ctx, logger, mcpEndpoint, mcpServer, mcpRouteBase)
		if err != nil {
			return err
		}
		newCtx, token, err := s.ApplyIssuerGate(ctx, w, AuthorizationBearerToken(r), s.BaseURLForRequest(r), resolvedEndpoint)
		if err != nil {
			return fmt.Errorf("apply issuer gate: %w", err)
		}
		ctx = newCtx
		r = r.WithContext(ctx)
		upstreamToken = token
	}

	switch {
	case mcpServer.RemoteMcpServerID.Valid:
		return s.serveRemoteBackend(w, r, logger, mcpEndpoint, mcpServer, upstreamToken)
	case mcpServer.ToolsetID.Valid:
		// AGE-1902: toolset-backed branch still reads runtime config from the
		// toolsets row (visibility, OAuth, default environment). Once
		// /mcp/{mcpSlug} is migrated to source these from the linked
		// mcp_servers row instead, this branch should switch to passing the
		// mcp_server config into ServeToolsetResolved (or its successor) and
		// the toolset load below can be dropped.
		toolset, err := toolsetsrepo.New(s.db).GetToolsetByIDAndProject(ctx, toolsetsrepo.GetToolsetByIDAndProjectParams{
			ID:        mcpServer.ToolsetID.UUID,
			ProjectID: mcpEndpoint.ProjectID,
		})
		switch {
		case errors.Is(err, pgx.ErrNoRows):
			return oops.E(oops.CodeNotFound, err, "toolset not found")
		case err != nil:
			return oops.E(oops.CodeUnexpected, err, "load toolset").Log(ctx, logger)
		}

		if err := s.ServeToolsetResolved(w, r, &toolset, slug, mcpRouteBase, mcpServer.UserSessionIssuerID.Valid, upstreamToken); err != nil {
			return fmt.Errorf("serve toolset-backed mcp: %w", err)
		}
		return nil
	default:
		// CHECK constraint mcp_servers_backend_exclusivity_check guarantees
		// exactly one backend is set; this is defensive.
		return oops.E(oops.CodeUnexpected, nil, "mcp server has no backend configured").Log(ctx, logger)
	}
}

// ResolveMCPEndpointAndServer walks the runtime addressing chain shared by
// the /mcp and /x/mcp slug handlers and the .well-known routes: it scopes
// the lookup to the request's customdomains.Context, loads the
// mcp_endpoint by (slug, custom domain), then loads the linked mcp_server.
// Disabled servers and missing rows both surface as 404 to avoid leaking
// existence to unauthenticated callers. logger should already carry the
// slug attribute.
//
// Returns CodeNotFound when no row matches. Callers that want to fall
// back to a legacy lookup (e.g. /mcp's existing toolsets path) should
// check for oops.CodeNotFound and proceed accordingly.
func (s *Service) ResolveMCPEndpointAndServer(ctx context.Context, logger *slog.Logger, slug string) (*mcpendpointsrepo.McpEndpoint, *mcpserversrepo.McpServer, error) {
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

	if mcpServer.Visibility == mcpservers.VisibilityDisabled {
		return nil, nil, oops.C(oops.CodeNotFound)
	}

	return &endpoint, &mcpServer, nil
}

// LoadResolvedMcpEndpointBySlug resolves a slug all the way to a
// *ResolvedMcpEndpoint via the mcp_endpoints → mcp_servers path,
// verifying its issuer FK is still live. Used by the OAuth route adapters
// in /x/mcp (and eventually /mcp) that need to dispatch to Serve*
// post-resolution handlers. Returns CodeNotFound when either the
// addressing resolves to no row or the resolved mcp_server is not
// issuer-gated. mcpRouteBase ("mcp" or "x/mcp") propagates into the
// resolved endpoint's URL building.
func (s *Service) LoadResolvedMcpEndpointBySlug(ctx context.Context, logger *slog.Logger, slug, mcpRouteBase string) (*ResolvedMcpEndpoint, error) {
	mcpEndpoint, mcpServer, err := s.ResolveMCPEndpointAndServer(ctx, logger, slug)
	if err != nil {
		return nil, err
	}
	if !mcpServer.UserSessionIssuerID.Valid {
		return nil, oops.E(oops.CodeNotFound, nil, "not found")
	}
	return s.BuildResolvedMcpEndpointForServer(ctx, logger, mcpEndpoint, mcpServer, mcpRouteBase)
}

// BuildResolvedMcpEndpointForServer materialises a ResolvedMcpEndpoint
// from a resolved (mcp_endpoint, mcp_server) pair and verifies its
// issuer FK is still live. Loads the owning project for its
// organization id (not carried on mcp_servers directly). Caller is
// responsible for first checking mcpServer.UserSessionIssuerID.Valid;
// this helper assumes the column has been validated and 404s if the FK
// target row has since been deleted. mcpRouteBase ("mcp" or "x/mcp") is
// applied to the resolved endpoint so subsequent URL building lands on
// the request's surface.
//
// Exported so /x/mcp's wellknown handlers can build a ResolvedMcpEndpoint
// from a previously-loaded (mcp_endpoint, mcp_server) pair without
// re-querying.
func (s *Service) BuildResolvedMcpEndpointForServer(
	ctx context.Context,
	logger *slog.Logger,
	mcpEndpoint *mcpendpointsrepo.McpEndpoint,
	mcpServer *mcpserversrepo.McpServer,
	mcpRouteBase string,
) (*ResolvedMcpEndpoint, error) {
	project, err := projectsrepo.New(s.db).GetProjectByID(ctx, mcpEndpoint.ProjectID)
	switch {
	case errors.Is(err, pgx.ErrNoRows):
		return nil, oops.E(oops.CodeNotFound, err, "project not found")
	case err != nil:
		return nil, oops.E(oops.CodeUnexpected, err, "load project").Log(ctx, logger)
	}
	resolved := NewResolvedMcpEndpointFromMcpServer(mcpEndpoint, mcpServer, project.OrganizationID)
	resolved.RouteBase = mcpRouteBase
	if err := s.RequireUserSessionIssuer(ctx, resolved); err != nil {
		return nil, fmt.Errorf("require user session issuer: %w", err)
	}
	return resolved, nil
}

// serveRemoteBackend handles an mcp_server backed by a remote_mcp_server.
// Auth and visibility come from the mcp_servers row; AuthN flow mirrors a
// strict subset of /mcp's identity-auth handling (skipping OAuth-proxy
// refresh, custom-OAuth validation, and per-tool security since those
// are toolset-only concerns — the upstream Remote MCP server handles
// its own OAuth where applicable).
//
// upstreamAuth is the resolved user-session access token forwarded to the
// remote server. It's only populated when the caller ran the issuer
// gate; otherwise it's empty and the proxy does not forward an
// Authorization header upstream.
func (s *Service) serveRemoteBackend(
	w http.ResponseWriter,
	r *http.Request,
	logger *slog.Logger,
	endpoint *mcpendpointsrepo.McpEndpoint,
	mcpServer *mcpserversrepo.McpServer,
	upstreamAuth string,
) error {
	ctx := r.Context()
	logger = logger.With(attr.SlogRemoteMCPServerID(mcpServer.RemoteMcpServerID.UUID.String()))

	// Identity auth + access checks, mirroring the relevant cases of
	// mcp.ServeToolsetResolved. Unrecognised visibility values fail closed
	// in the default branch — disabled was already filtered upstream in
	// ResolveMCPEndpointAndServer.
	//
	// Issuer-gated requests have already been authenticated by
	// ApplyIssuerGate in ServeMCPEndpoint: the bearer is a user-session JWT
	// validated against the issuer's audience, and the AuthContext on ctx
	// is stamped from it. Re-running the legacy identity-auth chain here
	// would only know how to validate API keys / OAuth tokens / chat
	// sessions, and would reject a perfectly valid user-session JWT. Skip
	// it and trust the gate.
	issuerGated := mcpServer.UserSessionIssuerID.Valid
	switch mcpServer.Visibility {
	case mcpservers.VisibilityPrivate:
		// Private mcp_servers require identity auth, that the caller's
		// active org owns the project that owns the server, and an
		// mcp:connect grant. RBAC enforcement only applies to RBAC-gated
		// callers — API keys bypass RBAC by design (they have their own
		// scoping), so the org-membership check is the meaningful gate
		// for API-key callers.
		if !issuerGated {
			var err error
			ctx, err = s.RequirePrivateIdentityAuth(ctx, w, r, false, mcpServer.ID, "")
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

			var prepErr error
			ctx, prepErr = s.authz.PrepareContext(ctx)
			if prepErr != nil {
				return oops.E(oops.CodeUnexpected, prepErr, "load access grants").Log(ctx, logger)
			}
			if err := s.authz.Require(ctx, authz.Check{Scope: authz.ScopeMCPConnect, ResourceKind: "", ResourceID: mcpServer.ID.String(), Dimensions: nil}); err != nil {
				return err
			}
		}
	case mcpservers.VisibilityPublic:
		// Public, no OAuth: optionally probe Gram identity if the
		// caller supplied an Authorization or Gram-Chat-Session
		// token so authenticated callers carry the right context
		// downstream. Nothing meaningful to forward upstream.
		if !issuerGated {
			var err error
			ctx, err = s.TryPublicIdentityAuth(ctx, r, false, mcpServer.ID)
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

	headers, err := remotemcp.NewHeaders(s.logger, s.db, s.enc).ListHeaders(ctx, server.ID, false)
	if err != nil {
		return oops.E(oops.CodeUnexpected, err, "load remote mcp server headers").Log(ctx, logger)
	}

	p := s.remoteProxyManager.Build(logger, &server, headers, mcpServer.Visibility, endpoint.ProjectID.String(), upstreamAuth)

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
