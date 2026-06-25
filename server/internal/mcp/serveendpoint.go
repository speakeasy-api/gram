package mcp

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"net/url"
	"strings"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/speakeasy-api/gram/server/internal/attr"
	"github.com/speakeasy-api/gram/server/internal/authz"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/customdomains"
	customdomainsrepo "github.com/speakeasy-api/gram/server/internal/customdomains/repo"
	"github.com/speakeasy-api/gram/server/internal/mcpendpoints"
	mcpendpointsrepo "github.com/speakeasy-api/gram/server/internal/mcpendpoints/repo"
	"github.com/speakeasy-api/gram/server/internal/mcpservers"
	mcpserversrepo "github.com/speakeasy-api/gram/server/internal/mcpservers/repo"
	"github.com/speakeasy-api/gram/server/internal/oops"
	projectsrepo "github.com/speakeasy-api/gram/server/internal/projects/repo"
	"github.com/speakeasy-api/gram/server/internal/remotemcp"
	"github.com/speakeasy-api/gram/server/internal/remotemcp/proxy"
	remotemcprepo "github.com/speakeasy-api/gram/server/internal/remotemcp/repo"
	toolsetsrepo "github.com/speakeasy-api/gram/server/internal/toolsets/repo"
	"github.com/speakeasy-api/gram/tunnel/wire"
)

// ServeMCPEndpoint resolves the given mcp_endpoint slug to its mcp_server,
// optionally runs the issuer gate, and dispatches to the appropriate
// backend (remote_mcp_servers via the remotemcp proxy, tunnelled_mcp_servers
// once the tunnel gateway exists, or toolsets via ServeToolsetResolved). It is
// the unified runtime entry point used by both /mcp and /x/mcp.
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

	if err := s.enforceCustomDomainLockdown(ctx, logger, mcpEndpoint.ProjectID); err != nil {
		return err
	}

	return s.serveResolvedMCPEndpoint(w, r, logger, mcpEndpoint, mcpServer, slug, mcpRouteBase)
}

// enforceCustomDomainLockdown 403s a public-host MCP request when the owning
// org's custom domain carries a non-empty IP allowlist. Such orgs require all
// MCP traffic to flow through their custom domain, where the allowlist is
// enforced at the ingress/gateway. Requests that arrived via a custom-domain
// context are allowed through unconditionally — the ingress already enforced
// the allowlist for that hostname. The lockdown engages as soon as an allowlist
// is configured, regardless of whether the domain is verified/activated yet.
//
// This guard is wired ONLY into the runtime MCP dispatch (ServePublic,
// ServeMCPEndpoint). The install page (ServeInstallPage / HandleGetServer's
// inline browser path) and OAuth metadata routes are intentionally left
// ungated: private-MCP install pages must keep working on the platform host
// (app.getgram.ai), where the dashboard session cookie lives, even when the
// org's custom domain has an allowlist. Do not call this from those handlers.
func (s *Service) enforceCustomDomainLockdown(ctx context.Context, logger *slog.Logger, projectID uuid.UUID) error {
	if customdomains.FromContext(ctx) != nil {
		return nil
	}

	project, err := projectsrepo.New(s.db).GetProjectByID(ctx, projectID)
	switch {
	case errors.Is(err, pgx.ErrNoRows):
		return oops.E(oops.CodeNotFound, err, "project not found")
	case err != nil:
		return oops.E(oops.CodeUnexpected, err, "load project for custom domain lockdown").LogError(ctx, logger)
	}

	domain, err := customdomainsrepo.New(s.db).GetCustomDomainByOrganization(ctx, project.OrganizationID)
	switch {
	case errors.Is(err, pgx.ErrNoRows):
		return nil
	case err != nil:
		return oops.E(oops.CodeUnexpected, err, "load custom domain for lockdown").LogError(ctx, logger)
	}

	if len(domain.IpAllowlist) > 0 {
		return oops.E(oops.CodeForbidden, nil, "this MCP server is only accessible via its custom domain")
	}

	return nil
}

// serveResolvedMCPEndpoint dispatches an already-resolved (mcp_endpoint,
// mcp_server) pair: it runs the issuer gate when the mcp_server is
// issuer-gated and then dispatches to the appropriate backend.
//
// Split from ServeMCPEndpoint so ServePublic can avoid a redundant
// resolve+lookup when it already has the rows in hand (ServePublic tries
// mcp_endpoints first and falls back to the legacy toolsets lookup on
// miss; only the hit case needs dispatch).
func (s *Service) serveResolvedMCPEndpoint(
	w http.ResponseWriter,
	r *http.Request,
	logger *slog.Logger,
	mcpEndpoint *mcpendpointsrepo.McpEndpoint,
	mcpServer *mcpserversrepo.McpServer,
	slug, mcpRouteBase string,
) error {
	ctx := r.Context()

	logger = logger.With(attr.SlogMcpServerID(mcpServer.ID.String()))

	issuerGated := mcpServer.UserSessionIssuerID.Valid

	// Issuer-gated mcp_servers run the JWT-validation branch here, before
	// backend dispatch. ServeToolsetResolved then skips its in-toolset
	// gate (skipIssuerGate=true) so the same request isn't gated twice;
	// remote-backed proxying forwards the upstream remote-session token
	// via AuthorizationOverride.
	var upstreamTokens map[uuid.UUID]string
	if issuerGated {
		resolvedEndpoint, err := s.BuildResolvedMcpEndpointForServer(ctx, logger, mcpEndpoint, mcpServer, mcpRouteBase)
		if err != nil {
			return err
		}
		newCtx, tokens, err := s.ApplyIssuerGate(ctx, w, AuthorizationBearerToken(r), s.BaseURLForRequest(r), resolvedEndpoint)
		if err != nil {
			return fmt.Errorf("apply issuer gate: %w", err)
		}
		ctx = newCtx
		r = r.WithContext(ctx)
		upstreamTokens = tokens
	}

	switch {
	case mcpServer.RemoteMcpServerID.Valid:
		upstreamToken, err := singleUpstreamToken(upstreamTokens)
		if err != nil {
			return oops.E(oops.CodeUnexpected, err, "resolve upstream token for remote MCP backend").LogError(ctx, logger)
		}
		return s.serveRemoteBackend(w, r, logger, mcpEndpoint, mcpServer, upstreamToken)
	case mcpServer.TunnelledMcpServerID.Valid:
		upstreamToken, err := singleUpstreamToken(upstreamTokens)
		if err != nil {
			return oops.E(oops.CodeUnexpected, err, "resolve upstream token for tunnelled MCP backend").LogError(ctx, logger)
		}
		return s.serveTunnelledBackend(w, r, logger, mcpEndpoint, mcpServer, upstreamToken)
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
			return oops.E(oops.CodeUnexpected, err, "load toolset").LogError(ctx, logger)
		}

		// The mcp_servers row's variation group, when set, overrides the
		// toolset's own column. Pass it through so ServeToolsetResolved resolves
		// the effective group (mcp_server, then toolset, then project default).
		var mcpServerVariationsGroupID *uuid.UUID
		if mcpServer.ToolVariationsGroupID.Valid {
			id := mcpServer.ToolVariationsGroupID.UUID
			mcpServerVariationsGroupID = &id
		}

		if err := s.ServeToolsetResolved(w, r, &toolset, slug, mcpRouteBase, issuerGated, upstreamTokens, mcpServerVariationsGroupID, &mcpServer.ID); err != nil {
			return fmt.Errorf("serve toolset-backed mcp: %w", err)
		}
		return nil
	default:
		// CHECK constraint mcp_servers_backend_exclusivity_check guarantees
		// exactly one backend is set; this is defensive.
		return oops.E(oops.CodeUnexpected, nil, "mcp server has no backend configured").LogError(ctx, logger)
	}
}

// singleUpstreamToken collapses the per-remote-issuer token map from
// ApplyIssuerGate to the one Authorization value a remote MCP backend
// forwards upstream. A remote-backed mcp_server proxies to exactly one
// upstream, so at most one remote_session token is meaningful: the
// remote_session_client_user_session_issuers one_per_issuer index binds a
// user_session_issuer to a single remote issuer, so the map holds 0 or 1
// entries. More than one token means the runtime cannot tell which upstream
// credential the backend needs, so it fails closed rather than forwarding an
// arbitrary (possibly mismatched) token; resolving the right token per
// upstream is tracked in AIS-152.
func singleUpstreamToken(tokens map[uuid.UUID]string) (string, error) {
	if len(tokens) > 1 {
		return "", fmt.Errorf("remote MCP backend bound to %d remote_session_issuers; cannot determine which upstream token to forward", len(tokens))
	}
	// len <= 1 here, so this returns the sole entry (or "" for an empty map).
	for _, token := range tokens {
		return token, nil
	}
	return "", nil
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
//
// Thin wrapper around mcpendpoints.BySlugAndCustomDomain; kept as a method
// for the existing /mcp and /x/mcp call sites.
func (s *Service) ResolveMCPEndpointAndServer(ctx context.Context, logger *slog.Logger, slug string) (*mcpendpointsrepo.McpEndpoint, *mcpserversrepo.McpServer, error) {
	return mcpendpoints.BySlugAndCustomDomain(ctx, s.db, logger, slug) //nolint:wrapcheck // thin passthrough; underlying error already carries context.
}

// LoadResolvedMcpEndpointBySlug resolves a slug to a *ResolvedMcpEndpoint
// for the issuer-gated OAuth handlers, shared by both the /mcp and /x/mcp
// surfaces. It mirrors the well-known handlers' resolution model:
//
//   - Addressing hit, issuer-gated: build the endpoint from the
//     (mcp_endpoint, mcp_server) pair.
//   - Addressing hit, not issuer-gated: CodeNotFound. The mcp_server is
//     authoritative for the slug and is not an OAuth endpoint, so we do
//     NOT fall back — this keeps non-issuer-gated remote-backed servers
//     returning not-found, matching the well-known surface.
//   - Addressing miss (CodeNotFound): fall back to the legacy
//     toolsets.mcp_slug lookup so issuer-gated toolset-backed servers
//     without an mcp_endpoint row (predating the toolsets → mcp_servers
//     migration) still resolve.
//
// mcpRouteBase ("mcp" or "x/mcp") propagates into the resolved endpoint's
// URL building on both the primary and fallback paths.
func (s *Service) LoadResolvedMcpEndpointBySlug(ctx context.Context, logger *slog.Logger, slug, mcpRouteBase string) (*ResolvedMcpEndpoint, error) {
	mcpEndpoint, mcpServer, err := s.ResolveMCPEndpointAndServer(ctx, logger, slug)
	var shareErr *oops.ShareableError
	switch {
	case err == nil:
		if !mcpServer.UserSessionIssuerID.Valid {
			return nil, oops.E(oops.CodeNotFound, nil, "not found")
		}
		return s.BuildResolvedMcpEndpointForServer(ctx, logger, mcpEndpoint, mcpServer, mcpRouteBase)
	case errors.As(err, &shareErr) && shareErr.Code == oops.CodeNotFound:
		return s.loadResolvedMcpEndpointByToolsetSlug(ctx, slug, mcpRouteBase)
	default:
		return nil, err
	}
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
		return nil, oops.E(oops.CodeUnexpected, err, "load project").LogError(ctx, logger)
	}
	resolved := NewResolvedMcpEndpointFromMcpServer(mcpEndpoint, mcpServer, project.OrganizationID)
	resolved.RouteBase = mcpRouteBase
	upstreamResource, err := s.resolveUpstreamResource(ctx, logger, mcpEndpoint.ProjectID, mcpServer)
	if err != nil {
		return nil, err
	}
	resolved.UpstreamResource = upstreamResource
	if err := s.RequireUserSessionIssuer(ctx, resolved); err != nil {
		return nil, fmt.Errorf("require user session issuer: %w", err)
	}
	return resolved, nil
}

// resolveUpstreamResource derives the RFC 8707 resource indicator for an
// mcp_server's upstream: the remote backend URL (sans trailing slash) for
// remote-backed servers, empty otherwise.
func (s *Service) resolveUpstreamResource(
	ctx context.Context,
	logger *slog.Logger,
	projectID uuid.UUID,
	mcpServer *mcpserversrepo.McpServer,
) (string, error) {
	if !mcpServer.RemoteMcpServerID.Valid {
		return "", nil
	}
	remote, err := remotemcprepo.New(s.db).GetServerByID(ctx, remotemcprepo.GetServerByIDParams{
		ID:        mcpServer.RemoteMcpServerID.UUID,
		ProjectID: projectID,
	})
	switch {
	case errors.Is(err, pgx.ErrNoRows):
		return "", oops.E(oops.CodeNotFound, err, "remote mcp server not found")
	case err != nil:
		return "", oops.E(oops.CodeUnexpected, err, "load remote mcp server").LogError(ctx, logger)
	}
	return strings.TrimRight(remote.Url, "/"), nil
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

	var err error
	ctx, err = s.prepareProxyBackendContext(ctx, w, r, logger, endpoint, mcpServer)
	if err != nil {
		return err
	}

	server, err := remotemcprepo.New(s.db).GetServerByID(ctx, remotemcprepo.GetServerByIDParams{
		ID:        mcpServer.RemoteMcpServerID.UUID,
		ProjectID: endpoint.ProjectID,
	})
	switch {
	case errors.Is(err, pgx.ErrNoRows):
		return oops.E(oops.CodeNotFound, err, "remote mcp server not found").LogError(ctx, logger)
	case err != nil:
		return oops.E(oops.CodeUnexpected, err, "load remote mcp server").LogError(ctx, logger)
	}

	headers, err := remotemcp.NewHeaders(s.logger, s.db, s.enc).ListHeaders(ctx, server.ID, false)
	if err != nil {
		return oops.E(oops.CodeUnexpected, err, "load remote mcp server headers").LogError(ctx, logger)
	}

	if s.remoteProxyManager == nil {
		return oops.E(oops.CodeUnexpected, nil, "remote MCP proxy manager is unavailable").LogError(ctx, logger)
	}

	p := s.remoteProxyManager.Build(logger, &server, mcpServer.ID.String(), headers, mcpServer.Visibility, endpoint.ProjectID.String(), upstreamAuth)

	return serveProxyBackend(w, r.WithContext(ctx), p)
}

func serveProxyBackend(w http.ResponseWriter, r *http.Request, p *proxy.Proxy) error {
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

// serveTunnelledBackend handles an mcp_server backed by a
// tunnelled_mcp_server. It resolves the live route from Redis and then reuses
// the remotemcp proxy interceptor stack so tunnel traffic is logged, metered,
// and authorized like remote MCP traffic. The tunnel id header is injected
// here server-side and overrides any caller-supplied value.
func (s *Service) serveTunnelledBackend(
	w http.ResponseWriter,
	r *http.Request,
	logger *slog.Logger,
	endpoint *mcpendpointsrepo.McpEndpoint,
	mcpServer *mcpserversrepo.McpServer,
	upstreamAuth string,
) error {
	ctx := r.Context()
	tunnelID := mcpServer.TunnelledMcpServerID.UUID.String()
	logger = logger.With(attr.SlogResourceID(tunnelID))

	var err error
	ctx, err = s.prepareProxyBackendContext(ctx, w, r, logger, endpoint, mcpServer)
	if err != nil {
		return err
	}

	if s.remoteProxyManager == nil {
		return oops.E(oops.CodeUnexpected, nil, "remote MCP proxy manager is unavailable").LogError(ctx, logger)
	}
	if s.tunnelRoutes == nil {
		w.Header().Set("X-Gram-Tunnel-Error", "route-store-unavailable")
		return oops.E(oops.CodeGatewayError, nil, "tunnel route store unavailable").LogError(ctx, logger)
	}

	addr, ok, err := s.tunnelRoutes.Lookup(ctx, tunnelID)
	if err != nil {
		w.Header().Set("X-Gram-Tunnel-Error", "route-lookup-failed")
		return oops.E(oops.CodeGatewayError, err, "lookup tunnel route").LogError(ctx, logger)
	}
	if !ok {
		w.Header().Set("X-Gram-Tunnel-Error", "no-route")
		return oops.E(oops.CodeGatewayError, nil, "tunnel has no live route").LogError(ctx, logger)
	}

	gatewayURL := (&url.URL{Scheme: "http", Host: addr}).String()
	headers := []proxy.ConfiguredHeader{
		{
			IsRequired:             true,
			Name:                   wire.HeaderTunnelID,
			StaticValue:            tunnelID,
			ValueFromRequestHeader: "",
		},
	}
	if consumerSession := tunnelConsumerSessionKey(r); consumerSession != "" {
		headers = append(headers, proxy.ConfiguredHeader{
			IsRequired:             false,
			Name:                   wire.HeaderTunnelConsumerSession,
			StaticValue:            consumerSession,
			ValueFromRequestHeader: "",
		})
	}
	p := s.remoteProxyManager.BuildTarget(logger, tunnelID, gatewayURL, mcpServer.ID.String(), headers, mcpServer.Visibility, endpoint.ProjectID.String(), upstreamAuth)

	return serveProxyBackend(w, r.WithContext(ctx), p)
}

func tunnelConsumerSessionKey(r *http.Request) string {
	if value := strings.TrimSpace(r.Header.Get(proxy.McpSessionIDHeader)); value != "" {
		return hashedTunnelConsumerSession("mcp", value)
	}
	if value := AuthorizationOrChatSessionToken(r); value != "" {
		return hashedTunnelConsumerSession("auth", value)
	}
	if value := strings.TrimSpace(r.Header.Get("User-Agent")); value != "" && r.RemoteAddr != "" {
		return hashedTunnelConsumerSession("anon", r.RemoteAddr+"|"+value)
	}
	if r.RemoteAddr != "" {
		return hashedTunnelConsumerSession("anon", r.RemoteAddr)
	}
	return ""
}

func hashedTunnelConsumerSession(prefix, value string) string {
	sum := sha256.Sum256([]byte(value))
	return prefix + ":" + hex.EncodeToString(sum[:])
}

func (s *Service) prepareProxyBackendContext(
	ctx context.Context,
	w http.ResponseWriter,
	r *http.Request,
	logger *slog.Logger,
	endpoint *mcpendpointsrepo.McpEndpoint,
	mcpServer *mcpserversrepo.McpServer,
) (context.Context, error) {
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
				return nil, fmt.Errorf("private identity auth: %w", err)
			}

			project, err := projectsrepo.New(s.db).GetProjectByID(ctx, endpoint.ProjectID)
			if err != nil {
				return nil, oops.E(oops.CodeUnexpected, err, "load mcp server project").LogError(ctx, logger)
			}
			authCtx, ok := contextvalues.GetAuthContext(ctx)
			if !ok || authCtx == nil || project.OrganizationID != authCtx.ActiveOrganizationID {
				return nil, oops.C(oops.CodeUnauthorized)
			}
			ctx = setProxyBackendProjectContext(ctx, authCtx, project.ID, project.Slug)
		}

		// Prepare RBAC grants for both the issuer-gated and non-issuer-gated
		// paths. The proxy attaches the private-visibility mcp:connect
		// interceptors (tools/list filter, tools/call authz) regardless of how
		// the caller authenticated, and for RBAC-enforced callers those run
		// FindMatched / Require, which fail with ErrMissingGrants unless grants
		// are in context. Issuer-gated callers were authenticated by
		// ApplyIssuerGate, which stamps the principal but does not load grants,
		// so without this they hit that failure (AGE-2672). PrepareContext runs
		// after the non-issuer-gated identity auth above has stamped the auth
		// context, and is a no-op for callers RBAC never enforces.
		var prepErr error
		ctx, prepErr = s.authz.PrepareContext(ctx)
		if prepErr != nil {
			return nil, oops.E(oops.CodeUnexpected, prepErr, "load access grants").LogError(ctx, logger)
		}

		// Non-issuer-gated callers get an upfront mcp:connect fail-fast before
		// the proxy. Issuer-gated callers rely on the per-tool response/request
		// interceptors instead, which is acceptable since the JWT
		// audience/issuer is already bound to the endpoint's project.
		if !issuerGated {
			if err := s.authz.Require(ctx, authz.Check{Scope: authz.ScopeMCPConnect, ResourceKind: "", ResourceID: mcpServer.ID.String(), Dimensions: nil}); err != nil {
				return nil, err
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
				return nil, fmt.Errorf("public identity auth: %w", err)
			}
			ctx, err = s.setProxyBackendProjectContextIfOwner(ctx, logger, endpoint.ProjectID)
			if err != nil {
				return nil, err
			}
		}
	default:
		return nil, oops.E(oops.CodeUnexpected, nil, "unrecognized mcp server visibility %q", mcpServer.Visibility).LogError(ctx, logger)
	}

	return ctx, nil
}

func setProxyBackendProjectContext(ctx context.Context, authCtx *contextvalues.AuthContext, projectID uuid.UUID, projectSlug string) context.Context {
	if authCtx.ProjectID == nil {
		id := projectID
		authCtx.ProjectID = &id
	}
	if authCtx.ProjectSlug == nil || *authCtx.ProjectSlug == "" {
		slug := projectSlug
		authCtx.ProjectSlug = &slug
	}
	return contextvalues.SetAuthContext(ctx, authCtx)
}

func (s *Service) setProxyBackendProjectContextIfOwner(ctx context.Context, logger *slog.Logger, projectID uuid.UUID) (context.Context, error) {
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	if !ok || authCtx == nil || authCtx.ProjectID != nil {
		return ctx, nil
	}

	project, err := projectsrepo.New(s.db).GetProjectByID(ctx, projectID)
	switch {
	case errors.Is(err, pgx.ErrNoRows):
		return ctx, oops.E(oops.CodeNotFound, err, "project not found")
	case err != nil:
		return ctx, oops.E(oops.CodeUnexpected, err, "load mcp server project").LogError(ctx, logger)
	}

	if project.OrganizationID != authCtx.ActiveOrganizationID {
		return ctx, nil
	}

	return setProxyBackendProjectContext(ctx, authCtx, project.ID, project.Slug), nil
}
