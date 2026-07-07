package mcp

import (
	"context"
	"log/slog"
	"net/http"

	"github.com/speakeasy-api/gram/server/internal/guardian"
	"github.com/speakeasy-api/gram/server/internal/mcp/tunnelrouting"
	mcpendpointsrepo "github.com/speakeasy-api/gram/server/internal/mcpendpoints/repo"
	mcpserversrepo "github.com/speakeasy-api/gram/server/internal/mcpservers/repo"
	"github.com/speakeasy-api/gram/server/internal/oops"
	"github.com/speakeasy-api/gram/server/internal/remotemcp"
	"github.com/speakeasy-api/gram/server/internal/remotemcp/proxy"
	"github.com/speakeasy-api/gram/tunnel/route"
)

type tunnelManager struct {
	routes       route.Store
	forwardToken string
	proxyManager *remotemcp.ProxyManager
	// gatewayCIDRs are the CIDR blocks tunnel gateway advertise addresses live
	// in (typically the cluster pod range). They are allowlisted past the
	// guardian egress policy for tunnel forwards only — gateway addresses come
	// from the trusted route store, but the default policy blocks RFC1918 and
	// would otherwise reject every in-cluster gateway dial. Empty means no
	// relaxation: tunnels to private addresses then fail closed.
	gatewayCIDRs []string
}

func newTunnelManager(routes route.Store, forwardToken string, proxyManager *remotemcp.ProxyManager, gatewayCIDRs []string) *tunnelManager {
	return &tunnelManager{
		routes:       routes,
		forwardToken: forwardToken,
		proxyManager: proxyManager,
		gatewayCIDRs: gatewayCIDRs,
	}
}

func (m *tunnelManager) buildProxy(
	ctx context.Context,
	r *http.Request,
	logger *slog.Logger,
	endpoint *mcpendpointsrepo.McpEndpoint,
	mcpServer *mcpserversrepo.McpServer,
	upstreamAuth string,
) (*proxy.Proxy, error) {
	if m == nil || m.proxyManager == nil {
		return nil, oops.E(oops.CodeUnexpected, nil, "remote MCP proxy manager is unavailable").LogError(ctx, logger)
	}

	tunnelID := mcpServer.TunneledMcpServerID.UUID.String()
	if m.routes == nil {
		return nil, oops.E(oops.CodeGatewayError, nil, "tunnel route store unavailable").LogError(ctx, logger)
	}

	clientAffinityKey := tunnelrouting.ClientAffinityKeyFromRequest(r)
	candidates, err := m.routes.Candidates(ctx, tunnelID)
	if err != nil {
		return nil, oops.E(oops.CodeGatewayError, err, "list tunnel routes").LogError(ctx, logger)
	}
	addr, ok := tunnelrouting.SelectRoute(clientAffinityKey, candidates, nil)
	if !ok {
		return nil, oops.E(oops.CodeGatewayError, nil, "tunnel has no live route").LogWarn(ctx, logger)
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
		},
		gatewayURL,
		tunnelrouting.Headers(tunnelID, m.forwardToken, clientAffinityKey),
		mcpServer.Visibility,
		endpoint.ProjectID.String(),
		upstreamAuth,
	)
	p.UpstreamResponseRetryer = tunnelrouting.Retryer(m.routes, tunnelID, addr, clientAffinityKey, m.forwardToken)
	if len(m.gatewayCIDRs) > 0 {
		p.GuardianClientOptions = []guardian.ClientOption{guardian.WithAllowedCIDRBlocks(m.gatewayCIDRs...)}
	}
	return p, nil
}
