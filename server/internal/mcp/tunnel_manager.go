package mcp

import (
	"context"
	"log/slog"
	"net/http"

	"github.com/google/uuid"

	"github.com/speakeasy-api/gram/server/internal/attr"
	"github.com/speakeasy-api/gram/server/internal/guardian"
	"github.com/speakeasy-api/gram/server/internal/mcp/toolfilter"
	"github.com/speakeasy-api/gram/server/internal/mcp/tunnelrouting"
	"github.com/speakeasy-api/gram/server/internal/mcpauthz"
	"github.com/speakeasy-api/gram/server/internal/mcpservers"
	mcpserversrepo "github.com/speakeasy-api/gram/server/internal/mcpservers/repo"
	"github.com/speakeasy-api/gram/server/internal/oops"
	"github.com/speakeasy-api/gram/server/internal/remotemcp"
	"github.com/speakeasy-api/gram/server/internal/remotemcp/proxy"
	"github.com/speakeasy-api/gram/tunnel/route"
)

type tunnelManager struct {
	routes           route.Store
	forwardToken     string
	proxyManager     *remotemcp.ProxyManager
	callerAssertions *mcpauthz.Issuer
	// gatewayCIDRs are the CIDR blocks tunnel gateway advertise addresses live
	// in (typically the cluster pod range). They are allowlisted past the
	// guardian egress policy for tunnel forwards only — gateway addresses come
	// from the trusted route store, but the default policy blocks RFC1918 and
	// would otherwise reject every in-cluster gateway dial. Empty means no
	// relaxation: tunnels to private addresses then fail closed.
	gatewayCIDRs []string
}

func newTunnelManager(routes route.Store, forwardToken string, proxyManager *remotemcp.ProxyManager, gatewayCIDRs []string, callerAssertions *mcpauthz.Issuer) *tunnelManager {
	return &tunnelManager{
		routes:           routes,
		forwardToken:     forwardToken,
		proxyManager:     proxyManager,
		gatewayCIDRs:     gatewayCIDRs,
		callerAssertions: callerAssertions,
	}
}

type buildProxyParams struct {
	// ClientAffinityKey pins route selection, forwarding headers, and retry to
	// a stable client identity: runtime callers derive it from the request,
	// while consent-time enumeration derives it from the challenge state so
	// every request of one enumeration session lands on the same gateway.
	ClientAffinityKey string

	// ProjectID is the destination server's project.
	ProjectID uuid.UUID

	// OrganizationID is the destination server's owning organization.
	OrganizationID string

	// MCPServer is the tunneled destination server.
	MCPServer *mcpserversrepo.McpServer

	// ResourceIdentifier is the saved audience for caller assertions; empty
	// uses the tunneled server ID.
	ResourceIdentifier string

	// UpstreamAuth is the Authorization value forwarded upstream. Empty
	// forwards none; the incoming Authorization header is always dropped.
	UpstreamAuth string

	// WWWAuthenticate replaces the upstream's challenge on 401/403. Empty
	// relays the upstream challenge verbatim.
	WWWAuthenticate string

	// Selection restricts the tools exposed through this proxy.
	Selection *toolfilter.SessionSelection
}

// buildProxy constructs the tunnel-backed proxy for one request.
func (m *tunnelManager) buildProxy(
	ctx context.Context,
	logger *slog.Logger,
	params buildProxyParams,
	options ...remotemcp.BuildOption,
) (*proxy.Proxy, error) {
	mcpServer := params.MCPServer
	tunnelID := mcpServer.TunneledMcpServerID.UUID.String()

	candidates, err := m.routes.Candidates(ctx, tunnelID)
	if err != nil {
		return nil, oops.E(oops.CodeGatewayError, err, "list tunnel routes").LogError(ctx, logger)
	}
	addr, ok := tunnelrouting.SelectRoute(params.ClientAffinityKey, candidates, nil)
	if !ok {
		// Nowhere to route the request. Tunnel outages are likely customer-side
		// rather than something the platform administrators can control. While
		// tunnel route selection may be a platform issue, this is not a great
		// place to signal that class of issue, especially to a MCP Client user.
		// If necessary, use another observability solution if this requires
		// explicit monitoring, ideally alerting the customer instead of using
		// the platform 5xx error budget which alerts platform administrators.
		return nil, oops.E(oops.CodeNotFound, nil, "not found").LogWarn(ctx, logger.With(attr.SlogErrorMessage("tunnel has no live route")))
	}

	gatewayURL, err := tunnelrouting.GatewayURL(addr)
	if err != nil {
		return nil, oops.E(oops.CodeGatewayError, err, "tunnel route is invalid").LogError(ctx, logger)
	}

	p := m.proxyManager.BuildTarget(
		logger,
		proxy.ServerIdentity{
			RemoteMCPServerID:   "",
			TunneledMCPServerID: tunnelID,
			McpServerID:         mcpServer.ID.String(),
			MetaMCPServerID:     "",
		},
		gatewayURL,
		tunnelrouting.Headers(tunnelID, m.forwardToken, params.ClientAffinityKey),
		mcpServer.Visibility,
		params.OrganizationID,
		params.ProjectID.String(),
		params.UpstreamAuth,
		params.WWWAuthenticate,
		params.Selection,
		options...,
	)
	if mcpServer.Visibility == mcpservers.VisibilityPrivate {
		target := mcpauthz.Target{
			OrganizationID:     params.OrganizationID,
			ProjectID:          params.ProjectID,
			TunnelID:           mcpServer.TunneledMcpServerID.UUID,
			ResourceIdentifier: params.ResourceIdentifier,
		}
		p.CallerAssertion = func(ctx context.Context) (string, error) {
			return m.callerAssertions.Mint(ctx, target)
		}
	}
	p.UpstreamResponseRetryer = tunnelrouting.Retryer(m.routes, tunnelID, addr, params.ClientAffinityKey, m.forwardToken)
	p.UpstreamResponseInterceptor = func(_ context.Context, resp *http.Response) error {
		if rejection := tunnelrouting.GatewayFailureRejection(resp); rejection != nil {
			return rejection
		}
		return nil
	}
	// Redirects won't work across a tunnel boundary; disable.
	p.DisableRedirects = true
	p.GuardianClientOptions = m.guardianClientOptions()
	return p, nil
}

// guardianClientOptions builds the shared client options for dialing tunnel
// gateways.
func (m *tunnelManager) guardianClientOptions() []guardian.ClientOption {
	opts := []guardian.ClientOption{guardian.WithDialTimeout(tunnelrouting.GatewayDialTimeout)}
	if len(m.gatewayCIDRs) > 0 {
		opts = append(opts, guardian.WithAllowedCIDRBlocks(m.gatewayCIDRs...))
	}
	return opts
}
