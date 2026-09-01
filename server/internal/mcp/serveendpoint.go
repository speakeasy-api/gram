package mcp

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"slices"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/speakeasy-api/gram/server/internal/attr"
	"github.com/speakeasy-api/gram/server/internal/authz"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/customdomains"
	customdomainsrepo "github.com/speakeasy-api/gram/server/internal/customdomains/repo"
	"github.com/speakeasy-api/gram/server/internal/mcp/httpheaders"
	"github.com/speakeasy-api/gram/server/internal/mcp/toolfilter"
	"github.com/speakeasy-api/gram/server/internal/mcp/tunnelrouting"
	"github.com/speakeasy-api/gram/server/internal/mcpaccess"
	"github.com/speakeasy-api/gram/server/internal/mcpendpoints"
	mcpendpointsrepo "github.com/speakeasy-api/gram/server/internal/mcpendpoints/repo"
	"github.com/speakeasy-api/gram/server/internal/mcpservers"
	mcpserversrepo "github.com/speakeasy-api/gram/server/internal/mcpservers/repo"
	metamcprepo "github.com/speakeasy-api/gram/server/internal/metamcp/repo"
	"github.com/speakeasy-api/gram/server/internal/networkaccess"
	"github.com/speakeasy-api/gram/server/internal/networkingress"
	"github.com/speakeasy-api/gram/server/internal/oops"
	projectsrepo "github.com/speakeasy-api/gram/server/internal/projects/repo"
	"github.com/speakeasy-api/gram/server/internal/remotemcp"
	"github.com/speakeasy-api/gram/server/internal/remotemcp/proxy"
	remotemcprepo "github.com/speakeasy-api/gram/server/internal/remotemcp/repo"
	"github.com/speakeasy-api/gram/server/internal/remotesessions"
	"github.com/speakeasy-api/gram/server/internal/requestorigin"
	toolsetsrepo "github.com/speakeasy-api/gram/server/internal/toolsets/repo"
	tunneledmcprepo "github.com/speakeasy-api/gram/server/internal/tunneledmcp/repo"
)

// ServeMCPEndpoint resolves a public MCP route; mcpRouteBase preserves the called surface in auth URLs.
func (s *Service) ServeMCPEndpoint(w http.ResponseWriter, r *http.Request, slug, mcpRouteBase string) error {
	ctx := r.Context()
	logger := s.logger.With(attr.SlogToolsetMCPSlug(slug))

	mcpEndpoint, mcpServer, metaServer, err := s.ResolveMCPEndpointAndServer(ctx, logger, slug)
	if err != nil {
		return err
	}

	if metaServer != nil {
		// Meta-backed endpoints are served only on the canonical /mcp
		// surface; /x/mcp stays a generic-backend surface with no meta
		// exposure.
		if mcpRouteBase != "mcp" {
			return oops.E(oops.CodeNotFound, nil, "mcp endpoint not found")
		}
		if err := s.enforceCustomDomainLockdown(ctx, logger, mcpEndpoint.ProjectID); err != nil {
			return err
		}
		return s.serveResolvedMetaMCPEndpoint(w, r, logger, mcpEndpoint, metaServer)
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
// This guard is wired into runtime MCP dispatch (ServePublic,
// ServeMCPEndpoint) and the consent-scoped MCP transport, which can enumerate
// live inventories. The install page (ServeInstallPage / HandleGetServer's
// inline browser path), consent HTML, and OAuth metadata routes are
// intentionally left ungated: private-MCP install and consent pages must keep
// working on the platform host (app.getgram.ai), where the dashboard session
// cookie lives, even when the org's custom domain has an allowlist.
func (s *Service) enforceCustomDomainLockdown(ctx context.Context, logger *slog.Logger, projectID uuid.UUID) error {
	lockedDown, err := s.customDomainLockdownApplies(ctx, logger, projectID)
	if err != nil {
		return err
	}
	if lockedDown {
		return oops.E(oops.CodeForbidden, nil, "this MCP server is only accessible via its custom domain")
	}
	return nil
}

// customDomainLockdownApplies reports whether a platform-origin request must
// be kept away from runtime-like MCP surfaces. Requests already carrying a
// custom-domain context passed through the ingress allowlist and are never
// locked down here.
func (s *Service) customDomainLockdownApplies(ctx context.Context, logger *slog.Logger, projectID uuid.UUID) (bool, error) {
	if origin, ok := requestorigin.FromContext(ctx); ok && origin.Surface == requestorigin.SurfacePrivateNetwork {
		// The private ingress has already established its own network admission,
		// workload identity, organization, namespace, and Host authority. A custom
		// domain's public-edge IP allowlist does not govern this separate surface.
		return false, nil
	}
	if customdomains.FromContext(ctx) != nil {
		return false, nil
	}

	project, err := projectsrepo.New(s.db).GetProjectByID(ctx, projectID)
	switch {
	case errors.Is(err, pgx.ErrNoRows):
		return false, oops.E(oops.CodeNotFound, err, "project not found")
	case err != nil:
		return false, oops.E(oops.CodeUnexpected, err, "load project for custom domain lockdown").LogError(ctx, logger)
	}

	domain, err := customdomainsrepo.New(s.db).GetCustomDomainByOrganization(ctx, project.OrganizationID)
	switch {
	case errors.Is(err, pgx.ErrNoRows):
		return false, nil
	case err != nil:
		return false, oops.E(oops.CodeUnexpected, err, "load custom domain for lockdown").LogError(ctx, logger)
	}

	return len(domain.IpAllowlist) > 0, nil
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

	// Public tunneled servers serve anonymously: no OAuth handshake, so the
	// issuer gate is skipped even though the issuer column is populated.
	// Gate on owner consent before dispatch so ungated callers are never
	// challenged for a server that will not serve them.
	if isTunneledPublic(mcpServer) {
		if err := s.requireTunneledPublicConsent(ctx, logger, mcpEndpoint, mcpServer); err != nil {
			return err
		}
		issuerGated = false
	}

	// Issuer-gated mcp_servers run the JWT-validation branch here, before
	// backend dispatch. ServeToolsetResolved then skips its in-toolset
	// gate (skipIssuerGate=true) so the same request isn't gated twice.
	// Credential resolution stays pending until backend dispatch: remote
	// backends resolve immediately, while hosted tools/call waits until after
	// kill-switch evaluation.
	var upstreamTokens map[uuid.UUID]remotesessions.UpstreamToken
	var pendingIssuerGate *issuerGateAuthentication
	var err error
	var upstreamResource string
	var sessionToolSelection *toolfilter.SessionSelection
	var wwwAuthenticate string
	if issuerGated {
		resolvedEndpoint, err := s.BuildResolvedMcpEndpointForServer(ctx, logger, mcpEndpoint, mcpServer, mcpRouteBase)
		if err != nil {
			return err
		}
		upstreamResource = resolvedEndpoint.UpstreamResource
		newCtx, authentication, toolSelection, err := s.authenticateIssuerGate(ctx, w, httpheaders.AuthorizationBearerToken(r), s.BaseURLForRequest(r), resolvedEndpoint)
		if err != nil {
			return fmt.Errorf("apply issuer gate: %w", err)
		}
		ctx = newCtx
		r = r.WithContext(ctx)
		pendingIssuerGate = authentication
		sessionToolSelection = toolSelection

		// Issuer-gated clients authenticate with this server's AS, so an
		// upstream 401/403 relayed by the proxy must challenge them with
		// this endpoint's resource metadata — the upstream's own challenge
		// would misdirect their re-auth at the upstream's AS.
		protectedResourceURL, err := resolvedEndpoint.ProtectedResourceURL(s.BaseURLForRequest(r))
		if err != nil {
			return oops.E(oops.CodeUnexpected, err, "build protected-resource URL").LogError(ctx, logger)
		}
		wwwAuthenticate = AuthenticateChallengeHeader(protectedResourceURL)
	}

	switch {
	case mcpServer.RemoteMcpServerID.Valid, mcpServer.TunneledMcpServerID.Valid:
		if pendingIssuerGate != nil {
			upstreamTokens, err = s.resolveIssuerGateAccessTokens(ctx, w, pendingIssuerGate)
			if err != nil {
				return fmt.Errorf("resolve issuer-gated upstream tokens: %w", err)
			}
		}
		upstreamToken, err := routeUpstreamToken(ctx, logger, upstreamTokens, upstreamResource, tunneledBackendIssuer(mcpServer))
		var routeErr *upstreamRoutingError
		switch {
		case errors.As(err, &routeErr):
			// routeUpstreamToken already logged the structured detail.
			return oops.E(oops.CodeFailedPrecondition, err, "this MCP server's upstream credentials are not configured unambiguously")
		case err != nil:
			return oops.E(oops.CodeUnexpected, err, "resolve upstream token for proxied MCP backend").LogError(ctx, logger)
		}
		if mcpServer.RemoteMcpServerID.Valid {
			return s.serveRemoteBackend(w, r, logger, mcpEndpoint, mcpServer, upstreamToken, wwwAuthenticate, sessionToolSelection)
		}
		return s.serveTunneledBackend(w, r, logger, mcpEndpoint, mcpServer, upstreamToken, wwwAuthenticate, sessionToolSelection)
	case mcpServer.ToolsetID.Valid:
		// Wrapper-governed dispatch (AIS-633): visibility, issuer gating, the
		// RBAC resource id, and the variation-group override come from the
		// mcp_servers row; the toolset supplies only what remains a toolset
		// concern (tools, resources, prompts, environment, tool selection
		// mode, external OAuth). A soft-deleted toolset behind a live wrapper
		// surfaces as not found here.
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

		if err := s.serveToolsetResolved(w, r, &toolset, slug, mcpRouteBase, hostedServingFromWrapper(mcpServer, issuerGated), nil, sessionToolSelection, pendingIssuerGate); err != nil {
			return fmt.Errorf("serve toolset-backed mcp: %w", err)
		}
		return nil
	default:
		// CHECK constraint mcp_servers_backend_exclusivity_check guarantees
		// exactly one backend is set; this is defensive.
		return oops.E(oops.CodeUnexpected, nil, "mcp server has no backend configured").LogError(ctx, logger)
	}
}

// hostedServingFromWrapper derives the hosting configuration for a request
// that resolved through an mcp_endpoints → mcp_servers pair: the wrapper row
// governs visibility, issuer gating, RBAC, and the variation group (AIS-633).
// callerGated reports whether the caller already ran the issuer gate keyed on
// mcp_servers.user_session_issuer_id; the in-toolset gate never runs on this
// path regardless.
func hostedServingFromWrapper(mcpServer *mcpserversrepo.McpServer, callerGated bool) *hostedServing {
	var groupID *uuid.UUID
	if mcpServer.ToolVariationsGroupID.Valid {
		id := mcpServer.ToolVariationsGroupID.UUID
		groupID = &id
	}
	serverID := mcpServer.ID
	return &hostedServing{
		isPublic:              mcpServer.Visibility == mcpservers.VisibilityPublic,
		runInToolsetGate:      false,
		callerGated:           callerGated,
		rbacResourceID:        mcpServer.ID,
		toolVariationsGroupID: groupID,
		mcpServerID:           &serverID,
	}
}

// routeUpstreamToken selects the one Authorization value a proxied
// (remote or tunneled) MCP backend forwards upstream, from the
// per-remote-issuer token map ApplyIssuerGate resolved.
//
// A proxied mcp_server talks to exactly one upstream, so exactly one entry is
// meaningful. A user_session_issuer may be bound to several
// remote_session_clients (the one_per_issuer index was dropped in AIS-137),
// so the map can hold several entries; selection is by qualified identity —
// the RFC 8707 resource recorded on each credential at grant time must match
// the backend's own upstream resource. There is no lone-token shortcut: an
// unmatched credential is never forwarded regardless of how few there are.
//
// tunneledIssuerID is a tunneled backend's own derived remote_session_issuer
// (invalid for remote backends). A tunneled backend is routed by that identity
// alone rather than by scanning recorded resources: its dial target is the
// tunnel, decoupled from whatever resource its identifier claims, so an
// operator-supplied identifier colliding with a sibling's upstream would
// otherwise deliver that sibling's bearer into the tunnel. A remote backend's
// routing key is the URL the proxy dials, so matching across the map returns
// each credential to the audience it names.
//
// A tunneled backend with no usable entry calls anonymously; an unmatched or
// ambiguous resource on a remote backend fails closed so a mismatched bearer
// is never forwarded.
func routeUpstreamToken(ctx context.Context, logger *slog.Logger, tokens map[uuid.UUID]remotesessions.UpstreamToken, upstreamResource string, tunneledIssuerID uuid.NullUUID) (string, error) {
	want := strings.TrimRight(upstreamResource, "/")
	if len(tokens) == 0 {
		return "", nil
	}

	if tunneledIssuerID.Valid {
		return tunneledIssuerToken(tokens, tunneledIssuerID, want), nil
	}
	if want == "" {
		return "", nil
	}

	var match string
	found, nullResources := 0, 0
	for _, entry := range tokens {
		if entry.Resource == "" {
			nullResources++
		}
		if strings.TrimRight(entry.Resource, "/") == want {
			found++
			match = entry.Token
		}
	}
	switch {
	case found == 1:
		return match, nil
	case found > 1:
		return "", routeFailClosed(ctx, logger, "duplicate_resource", tokens, upstreamResource,
			fmt.Sprintf("%d of %d resolved remote_session tokens match the backend's upstream resource", found, len(tokens)))
	}
	// Distinguish routing failures by cause: legacy grants minted before
	// the resource column vs genuinely unmatched credentials.
	reason := "no_match"
	if nullResources > 0 {
		reason = "legacy_null_resource"
	}
	return "", routeFailClosed(ctx, logger, reason, tokens, upstreamResource,
		fmt.Sprintf("0 of %d resolved remote_session tokens match the backend's upstream resource", len(tokens)))
}

// tunneledIssuerToken selects a tunneled backend's bearer from the entry keyed
// by its own derived remote_session_issuer. The grant is accepted when it is
// unqualified — the backend records no resource identifier, or the grant was
// minted before it did — or when it names the identifier passed as want. A
// grant audience-bound elsewhere, a missing entry, and a backend with no
// derived issuer all yield "" for an anonymous call. hostedMemberTokens
// applies the same identity rule to hosted members.
func tunneledIssuerToken(tokens map[uuid.UUID]remotesessions.UpstreamToken, issuerID uuid.NullUUID, want string) string {
	if !issuerID.Valid {
		return ""
	}
	entry, ok := tokens[issuerID.UUID]
	switch {
	case !ok:
		return ""
	case entry.Resource == "":
		return entry.Token
	case want != "" && strings.TrimRight(entry.Resource, "/") == want:
		return entry.Token
	default:
		return ""
	}
}

// tunneledBackendIssuer yields the identity routeUpstreamToken routes a
// resourceless tunneled backend by: the server's own derived
// remote_session_issuer, and only for tunneled backends — remote backends
// route strictly by recorded resource.
func tunneledBackendIssuer(mcpServer *mcpserversrepo.McpServer) uuid.NullUUID {
	if !mcpServer.TunneledMcpServerID.Valid {
		return uuid.NullUUID{UUID: uuid.Nil, Valid: false}
	}
	return mcpServer.RemoteSessionIssuerID
}

// upstreamRoutingError is a fail-closed routing outcome: the endpoint's
// credentials are ambiguous for this backend, which is a configuration state
// an operator must resolve rather than a runtime fault. Call sites map it to a
// precondition failure so a misconfigured tenant does not page as a 500.
type upstreamRoutingError struct {
	reason string
	detail string
}

func (e *upstreamRoutingError) Error() string {
	return "upstream token routing failed closed (" + e.reason + "): " + e.detail
}

// routeFailClosed emits the one structured line per fail-closed routing
// outcome — so legacy-NULL, unmatched, and duplicate causes are
// distinguishable in aggregate — and returns the typed error. Call sites do
// not log it again.
func routeFailClosed(ctx context.Context, logger *slog.Logger, reason string, tokens map[uuid.UUID]remotesessions.UpstreamToken, upstreamResource, detail string) error {
	recorded := make([]string, 0, len(tokens))
	for _, entry := range tokens {
		recorded = append(recorded, entry.Resource)
	}
	slices.Sort(recorded)
	logger.WarnContext(ctx, "remote_session token routing failed closed",
		attr.SlogReason(reason),
		attr.SlogResourceURI(upstreamResource),
		attr.SlogOAuthResource(strings.Join(recorded, ",")),
	)
	return &upstreamRoutingError{reason: reason, detail: detail}
}

// ResolveMCPEndpointAndServer walks the runtime addressing chain shared by
// the /mcp and /x/mcp slug handlers and the .well-known routes. Public and
// custom-domain requests use the ordinary context-derived namespace. Private
// requests re-load their live ingress authority and supply its pinned namespace,
// organization, and surface explicitly to the centralized resolver.
//
// A private namespace miss or policy mismatch is always authoritative and never
// permits legacy toolset fallback. Transient ingress lookup failures return 503;
// stale, deleted, disabled, or mismatched authority remains externally 404 while
// being warning-logged. logger should already carry the slug attribute.
func (s *Service) ResolveMCPEndpointAndServer(ctx context.Context, logger *slog.Logger, slug string) (*mcpendpointsrepo.McpEndpoint, *mcpserversrepo.McpServer, *metamcprepo.MetaMcpServer, error) {
	origin, ok := requestorigin.FromContext(ctx)
	if !ok || origin.Surface != requestorigin.SurfacePrivateNetwork {
		return mcpendpoints.BySlugAndCustomDomain(ctx, s.db, logger, slug) //nolint:wrapcheck // thin passthrough; underlying error already carries context.
	}

	started := time.Now()
	record := func(result, reason string) {
		s.networkIngressTelemetry.Record(ctx, networkingress.OperationResolution, result, reason, "unknown", time.Since(started))
	}
	authority, err := networkingress.LoadRequestAuthority(ctx, s.db)
	if errors.Is(err, networkingress.ErrAuthorityUnavailable) {
		record(networkingress.ResultError, networkingress.ReasonAuthorityUnavailable)
		return nil, nil, nil, oops.E(oops.CodeUnavailable, err, "private network ingress authority is unavailable").LogError(ctx, logger)
	}
	if err != nil {
		record(networkingress.ResultDenied, networkingress.ReasonAuthorityRejected)
		logger.WarnContext(ctx, "private network ingress authority rejected", attr.SlogError(err))
		return nil, nil, nil, oops.E(oops.CodeNotFound, errors.Join(mcpendpoints.ErrPolicyDenied, err), "mcp endpoint not found")
	}
	namespaceKind, err := privateEndpointNamespace(authority.NamespaceKind)
	if err != nil {
		record(networkingress.ResultDenied, networkingress.ReasonNamespaceRejected)
		logger.WarnContext(ctx, "private network ingress namespace rejected", attr.SlogError(err))
		return nil, nil, nil, oops.E(oops.CodeNotFound, errors.Join(mcpendpoints.ErrPolicyDenied, err), "mcp endpoint not found")
	}
	result, err := mcpendpoints.Resolve(ctx, s.db, logger, mcpendpoints.ResolutionInput{
		Slug:                 slug,
		NamespaceKind:        namespaceKind,
		CustomDomainID:       authority.CustomDomainID,
		ExpectedOrganization: authority.OrganizationID,
		Surface:              networkaccess.SurfacePrivate,
	})
	if err != nil {
		record(networkingress.ResultError, networkingress.ReasonDependencyFailed)
		return nil, nil, nil, fmt.Errorf("resolve private MCP endpoint: %w", err)
	}
	if !result.Found {
		record(networkingress.ResultDenied, networkingress.ReasonEndpointNotFound)
		return nil, nil, nil, oops.E(oops.CodeNotFound, mcpendpoints.ErrPolicyDenied, "mcp endpoint not found")
	}
	if !result.Allowed {
		record(networkingress.ResultDenied, networkingress.ReasonPolicyDenied)
		return nil, nil, nil, oops.E(oops.CodeNotFound, mcpendpoints.ErrPolicyDenied, "mcp endpoint not found")
	}
	record(networkingress.ResultAllowed, networkingress.ReasonNone)
	return result.Endpoint, result.Server, result.MetaServer, nil
}

func privateEndpointNamespace(kind string) (mcpendpoints.NamespaceKind, error) {
	switch kind {
	case networkingress.NamespacePlatform:
		return mcpendpoints.NamespacePlatform, nil
	case networkingress.NamespaceCustomDomain:
		return mcpendpoints.NamespaceCustomDomain, nil
	default:
		return "", fmt.Errorf("unsupported private endpoint namespace %q", kind)
	}
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
//   - Addressing miss (CodeNotFound, not ErrEndpointUnavailable): fall back
//     to the legacy toolsets.mcp_slug lookup so issuer-gated toolset-backed
//     servers without an mcp_endpoint row (predating the toolsets →
//     mcp_servers migration) still resolve. A resolvable-but-unavailable
//     address (disabled wrapper, dangling backend) is terminal.
//
// mcpRouteBase ("mcp" or "x/mcp") propagates into the resolved endpoint's
// URL building on both the primary and fallback paths.
func (s *Service) LoadResolvedMcpEndpointBySlug(ctx context.Context, logger *slog.Logger, slug, mcpRouteBase string) (*ResolvedMcpEndpoint, error) {
	mcpEndpoint, mcpServer, metaServer, err := s.ResolveMCPEndpointAndServer(ctx, logger, slug)
	switch {
	case err == nil:
		if metaServer != nil {
			// Meta-backed endpoints expose OAuth handlers only on the
			// canonical /mcp surface, and only when issuer-gated.
			if mcpRouteBase != "mcp" || !metaServer.UserSessionIssuerID.Valid {
				return nil, oops.E(oops.CodeNotFound, nil, "not found")
			}
			return s.BuildResolvedMcpEndpointForMetaServer(ctx, logger, mcpEndpoint, metaServer, mcpRouteBase)
		}
		// Public tunneled servers serve anonymously and expose no OAuth
		// surface: every issuer-gated handler resolving through here
		// (authorize, token, register, revoke, consent) must 404 even
		// though the issuer column is populated.
		if !mcpServer.UserSessionIssuerID.Valid || isTunneledPublic(mcpServer) {
			return nil, oops.E(oops.CodeNotFound, nil, "not found")
		}
		return s.BuildResolvedMcpEndpointForServer(ctx, logger, mcpEndpoint, mcpServer, mcpRouteBase)
	case mcpendpoints.IsAddressMiss(err):
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
	resolved := NewResolvedMcpEndpointFromMcpServer(mcpEndpoint, mcpServer, project.OrganizationID, mcpRouteBase)
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
// mcp_server's upstream (sans trailing slash): the remote backend URL for
// remote-backed servers, the recorded resource identifier for tunneled
// servers (empty when none is recorded), empty for other backends.
func (s *Service) resolveUpstreamResource(
	ctx context.Context,
	logger *slog.Logger,
	projectID uuid.UUID,
	mcpServer *mcpserversrepo.McpServer,
) (string, error) {
	switch {
	case mcpServer.RemoteMcpServerID.Valid:
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
	case mcpServer.TunneledMcpServerID.Valid:
		tunneled, err := tunneledmcprepo.New(s.db).GetServerByID(ctx, tunneledmcprepo.GetServerByIDParams{
			ID:        mcpServer.TunneledMcpServerID.UUID,
			ProjectID: projectID,
		})
		switch {
		case errors.Is(err, pgx.ErrNoRows):
			return "", oops.E(oops.CodeNotFound, err, "tunneled mcp server not found")
		case err != nil:
			return "", oops.E(oops.CodeUnexpected, err, "load tunneled mcp server").LogError(ctx, logger)
		}
		return strings.TrimRight(tunneled.ResourceIdentifier.String, "/"), nil
	default:
		return "", nil
	}
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
//
// selection is the session's consent-screen tool selection; non-nil attaches
// the proxy's exact-name enforcement interceptors.
func (s *Service) serveRemoteBackend(
	w http.ResponseWriter,
	r *http.Request,
	logger *slog.Logger,
	endpoint *mcpendpointsrepo.McpEndpoint,
	mcpServer *mcpserversrepo.McpServer,
	upstreamAuth string,
	wwwAuthenticate string,
	selection *toolfilter.SessionSelection,
) error {
	ctx := r.Context()
	logger = logger.With(attr.SlogRemoteMCPServerID(mcpServer.RemoteMcpServerID.UUID.String()))

	ctx, organizationID, err := s.prepareProxyBackendContext(ctx, w, r, logger, endpoint, mcpServer)
	if err != nil {
		return err
	}

	server, err := remotemcprepo.New(s.db).GetServerByID(ctx, remotemcprepo.GetServerByIDParams{
		ID:        mcpServer.RemoteMcpServerID.UUID,
		ProjectID: endpoint.ProjectID,
	})
	switch {
	case errors.Is(err, pgx.ErrNoRows):
		return oops.E(oops.CodeNotFound, err, "remote mcp server not found").LogWarn(ctx, logger)
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

	p := s.remoteProxyManager.Build(logger, &server, mcpServer.ID.String(), headers, mcpServer.Visibility, organizationID, endpoint.ProjectID.String(), upstreamAuth, wwwAuthenticate, selection)

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
		return oops.E(oops.CodeBadRequest, nil, "unsupported method %s", r.Method)
	}
}

// Tunneled MCP reuses the remote proxy stack; Gram injects the tunnel ID header server-side.
func (s *Service) serveTunneledBackend(
	w http.ResponseWriter,
	r *http.Request,
	logger *slog.Logger,
	endpoint *mcpendpointsrepo.McpEndpoint,
	mcpServer *mcpserversrepo.McpServer,
	upstreamAuth string,
	wwwAuthenticate string,
	selection *toolfilter.SessionSelection,
) error {
	ctx := r.Context()
	logger = logger.With(attr.SlogTunneledMCPServerID(mcpServer.TunneledMcpServerID.UUID.String()))

	if mcpServer.Visibility == mcpservers.VisibilityPublic {
		return s.serveTunneledPublicBackend(w, r, logger, endpoint, mcpServer)
	}

	ctx, organizationID, err := s.prepareProxyBackendContext(ctx, w, r, logger, endpoint, mcpServer)
	if err != nil {
		return err
	}

	p, err := s.tunnelManager.buildProxy(ctx, tunnelrouting.ClientAffinityKeyFromRequest(r), logger, endpoint.ProjectID, organizationID, mcpServer, upstreamAuth, wwwAuthenticate, selection)
	if err != nil {
		return err
	}

	return serveProxyBackend(w, r.WithContext(ctx), p)
}

func (s *Service) prepareProxyBackendContext(
	ctx context.Context,
	w http.ResponseWriter,
	r *http.Request,
	logger *slog.Logger,
	endpoint *mcpendpointsrepo.McpEndpoint,
	mcpServer *mcpserversrepo.McpServer,
) (context.Context, string, error) {
	project, err := projectsrepo.New(s.db).GetProjectByID(ctx, endpoint.ProjectID)
	switch {
	case errors.Is(err, pgx.ErrNoRows):
		return nil, "", oops.E(oops.CodeNotFound, err, "mcp server project not found")
	case err != nil:
		return nil, "", oops.E(oops.CodeUnexpected, err, "load mcp server project").LogError(ctx, logger)
	}

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
	issuerGated := mcpServer.UserSessionIssuerID.Valid && !isTunneledPublic(mcpServer)
	if issuerGated {
		// Public issuer-gated endpoints may carry an anonymous subject, which
		// intentionally has no dashboard AuthContext. Private endpoints still
		// require one; when present, always bind it to the owning organization
		// before exposing a project context to the proxy.
		authCtx, ok := contextvalues.GetAuthContext(ctx)
		if !ok || authCtx == nil {
			if mcpServer.Visibility == mcpservers.VisibilityPrivate {
				return nil, "", oops.C(oops.CodeUnauthorized)
			}
		} else {
			if project.OrganizationID != authCtx.ActiveOrganizationID {
				return nil, "", oops.C(oops.CodeUnauthorized)
			}
			ctx = setProxyBackendProjectContext(ctx, authCtx, project.ID, project.Slug)
		}
	}
	switch mcpServer.Visibility {
	case mcpservers.VisibilityPrivate:
		// Private mcp_servers require identity auth, that the caller's
		// active org owns the project that owns the server, and an
		// mcp:connect grant. RBAC enforcement only applies to RBAC-gated
		// callers — API keys bypass RBAC by design (they have their own
		// scoping), so the org-membership check is the meaningful gate
		// for API-key callers.
		if !issuerGated {
			ctx, err = s.RequirePrivateIdentityAuth(ctx, w, r, false, mcpServer.ID, "")
			if err != nil {
				return nil, "", fmt.Errorf("private identity auth: %w", err)
			}

			authCtx, ok := contextvalues.GetAuthContext(ctx)
			if !ok || authCtx == nil || project.OrganizationID != authCtx.ActiveOrganizationID {
				return nil, "", oops.C(oops.CodeUnauthorized)
			}
			ctx = setProxyBackendProjectContext(ctx, authCtx, project.ID, project.Slug)
		}
	case mcpservers.VisibilityPublic:
		// Public, no OAuth: optionally probe Gram identity if the
		// caller supplied an Authorization or Gram-Chat-Session
		// token so authenticated callers carry the right context
		// downstream. Nothing meaningful to forward upstream.
		if !issuerGated {
			ctx, err = s.TryPublicIdentityAuth(ctx, r, false, mcpServer.ID)
			if err != nil {
				return nil, "", fmt.Errorf("public identity auth: %w", err)
			}
			authCtx, ok := contextvalues.GetAuthContext(ctx)
			if ok && authCtx != nil && authCtx.ProjectID == nil && project.OrganizationID == authCtx.ActiveOrganizationID {
				ctx = setProxyBackendProjectContext(ctx, authCtx, project.ID, project.Slug)
			}
		}
	default:
		return nil, "", oops.E(oops.CodeUnexpected, nil, "unrecognized mcp server visibility %q", mcpServer.Visibility).LogError(ctx, logger)
	}

	ctx, err = s.authorizeProxyBackendAccess(ctx, logger, endpoint.ProjectID, mcpServer)
	return ctx, project.OrganizationID, err
}

// authorizeProxyBackendAccess runs the visibility-scoped RBAC gate for a
// proxied MCP backend after identity/AuthContext setup, shared by the
// runtime dispatch path and consent-time tool enumeration.
//
// For private servers it prepares RBAC grants for both the issuer-gated and
// non-issuer-gated paths: the proxy attaches the private-visibility
// mcp:connect interceptors (tools/list filter, tools/call authz) regardless
// of how the caller authenticated, and for RBAC-enforced callers those run
// FindMatched / Require, which fail with ErrMissingGrants unless grants are
// in context. Issuer-gated callers were authenticated by ApplyIssuerGate,
// which stamps the principal but does not load grants, so without this they
// hit that failure (AGE-2672). PrepareContext runs after identity auth has
// stamped the auth context, and is a no-op for callers RBAC never enforces.
//
// Public servers bypass server-level RBAC by design; unknown visibility
// fails closed.
func (s *Service) authorizeProxyBackendAccess(
	ctx context.Context,
	logger *slog.Logger,
	projectID uuid.UUID,
	mcpServer *mcpserversrepo.McpServer,
) (context.Context, error) {
	switch mcpServer.Visibility {
	case mcpservers.VisibilityPrivate:
		var prepErr error
		ctx, prepErr = s.authz.PrepareContext(ctx)
		if prepErr != nil {
			return nil, oops.E(oops.CodeUnexpected, prepErr, "load access grants").LogError(ctx, logger)
		}

		// mcp:connect covers non-tool proxy methods; tool interceptors still enforce per-tool scopes.
		if err := s.authz.Require(ctx, authz.MCPCheck(authz.ScopeMCPConnect, mcpServer.ID.String(), projectID.String())); err != nil {
			serverName := ""
			if mcpServer.Name.Valid {
				serverName = mcpServer.Name.String
			}
			return nil, fmt.Errorf("authorize MCP server access: %w", mcpaccess.ServerPermissionDenied(err, s.requestAccessURL(ctx, mcpServer.ID.String(), serverName)))
		}
		return ctx, nil
	case mcpservers.VisibilityPublic:
		return ctx, nil
	default:
		return nil, oops.E(oops.CodeUnexpected, nil, "unrecognized mcp server visibility %q", mcpServer.Visibility).LogError(ctx, logger)
	}
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

// BuildResolvedMcpEndpointForMetaServer materialises a ResolvedMcpEndpoint
// from a resolved (mcp_endpoint, meta_mcp_server) pair and verifies its
// issuer FK is still live. Caller is responsible for first checking
// metaServer.UserSessionIssuerID.Valid. Unlike the generic-server builder no
// project lookup is needed: meta_mcp_servers carries the organization id
// directly.
func (s *Service) BuildResolvedMcpEndpointForMetaServer(
	ctx context.Context,
	logger *slog.Logger,
	mcpEndpoint *mcpendpointsrepo.McpEndpoint,
	metaServer *metamcprepo.MetaMcpServer,
	mcpRouteBase string,
) (*ResolvedMcpEndpoint, error) {
	resolved := NewResolvedMcpEndpointFromMetaMcpServer(mcpEndpoint, metaServer, metaServer.OrganizationID, mcpRouteBase)
	if err := s.RequireUserSessionIssuer(ctx, resolved); err != nil {
		return nil, err
	}
	logger.DebugContext(ctx, "resolved meta mcp endpoint", attr.SlogMetaMcpServerID(metaServer.ID.String()))
	return resolved, nil
}
