package mcp

import (
	"context"
	"log/slog"
	"net/http"

	"github.com/speakeasy-api/gram/server/internal/mcp/tunnelrouting"
	mcpendpointsrepo "github.com/speakeasy-api/gram/server/internal/mcpendpoints/repo"
	mcpserversrepo "github.com/speakeasy-api/gram/server/internal/mcpservers/repo"
	"github.com/speakeasy-api/gram/server/internal/oops"
	"github.com/speakeasy-api/gram/server/internal/remotemcp"
	"github.com/speakeasy-api/gram/server/internal/remotemcp/proxy"
	"github.com/speakeasy-api/gram/tunnel/route"
)

const (
	tunnelErrorHeader = tunnelrouting.ErrorHeader
)

type tunnelManager struct {
	routes       route.Store
	forwardToken string
	proxyManager *remotemcp.ProxyManager
}

func newTunnelManager(routes route.Store, forwardToken string, proxyManager *remotemcp.ProxyManager) *tunnelManager {
	return &tunnelManager{
		routes:       routes,
		forwardToken: forwardToken,
		proxyManager: proxyManager,
	}
}

func (m *tunnelManager) buildProxy(
	ctx context.Context,
	w http.ResponseWriter,
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
		w.Header().Set(tunnelErrorHeader, "route-store-unavailable")
		return nil, oops.E(oops.CodeGatewayError, nil, "tunnel route store unavailable").LogError(ctx, logger)
	}

	clientAffinityKey := tunnelrouting.ClientAffinityKeyFromRequest(r)
	candidates, err := m.routes.Candidates(ctx, tunnelID)
	if err != nil {
		w.Header().Set(tunnelErrorHeader, "route-lookup-failed")
		return nil, oops.E(oops.CodeGatewayError, err, "list tunnel routes").LogError(ctx, logger)
	}
	addr, ok := tunnelrouting.SelectRoute(clientAffinityKey, candidates, nil)
	if !ok {
		w.Header().Set(tunnelErrorHeader, "no-route")
		return nil, oops.E(oops.CodeGatewayError, nil, "tunnel has no live route").LogWarn(ctx, logger)
	}

	gatewayURL, err := tunnelrouting.GatewayURL(addr)
	if err != nil {
		w.Header().Set(tunnelErrorHeader, "invalid-route")
		return nil, oops.E(oops.CodeGatewayError, err, "tunnel route is invalid").LogError(ctx, logger)
	}

	p := m.proxyManager.BuildTarget(
		logger,
		tunnelID,
		gatewayURL,
		mcpServer.ID.String(),
		m.tunnelHeaders(tunnelID, clientAffinityKey),
		mcpServer.Visibility,
		endpoint.ProjectID.String(),
		upstreamAuth,
	)
	p.UpstreamResponseRetryer = tunnelrouting.Retryer(m.routes, tunnelID, addr, clientAffinityKey, m.forwardToken)
	return p, nil
}

func (m *tunnelManager) tunnelHeaders(tunnelID, clientAffinityKey string) []proxy.ConfiguredHeader {
	return tunnelrouting.Headers(tunnelID, m.forwardToken, clientAffinityKey)
}
