package mcp

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"log/slog"
	"net/http"
	"net/url"
	"strings"

	mcpendpointsrepo "github.com/speakeasy-api/gram/server/internal/mcpendpoints/repo"
	mcpserversrepo "github.com/speakeasy-api/gram/server/internal/mcpservers/repo"
	"github.com/speakeasy-api/gram/server/internal/oops"
	"github.com/speakeasy-api/gram/server/internal/remotemcp"
	"github.com/speakeasy-api/gram/server/internal/remotemcp/proxy"
	"github.com/speakeasy-api/gram/tunnel/route"
	"github.com/speakeasy-api/gram/tunnel/wire"
)

const (
	tunnelErrorHeader = "X-Gram-Tunnel-Error"

	// tunnelConsumerSessionMCPPrefix identifies requests already bound to an MCP session.
	tunnelConsumerSessionMCPPrefix = "mcp"
	// tunnelConsumerSessionAuthPrefix identifies requests by their user/auth session.
	tunnelConsumerSessionAuthPrefix = "auth"
	// tunnelConsumerSessionAnonymousPrefix is the best-effort fallback for unauthenticated clients.
	tunnelConsumerSessionAnonymousPrefix = "anon"
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

	addr, ok, err := m.routes.Lookup(ctx, tunnelID)
	if err != nil {
		w.Header().Set(tunnelErrorHeader, "route-lookup-failed")
		return nil, oops.E(oops.CodeGatewayError, err, "lookup tunnel route").LogError(ctx, logger)
	}
	if !ok {
		w.Header().Set(tunnelErrorHeader, "no-route")
		return nil, oops.E(oops.CodeGatewayError, nil, "tunnel has no live route").LogWarn(ctx, logger)
	}

	gatewayURL, err := tunnelGatewayURL(addr)
	if err != nil {
		w.Header().Set(tunnelErrorHeader, "invalid-route")
		return nil, oops.E(oops.CodeGatewayError, err, "tunnel route is invalid").LogError(ctx, logger)
	}

	return m.proxyManager.BuildTarget(
		logger,
		tunnelID,
		gatewayURL,
		mcpServer.ID.String(),
		m.tunnelHeaders(r, tunnelID),
		mcpServer.Visibility,
		endpoint.ProjectID.String(),
		upstreamAuth,
	), nil
}

func (m *tunnelManager) tunnelHeaders(r *http.Request, tunnelID string) []proxy.ConfiguredHeader {
	headers := []proxy.ConfiguredHeader{
		{
			IsRequired:             true,
			Name:                   wire.HeaderTunnelID,
			StaticValue:            tunnelID,
			ValueFromRequestHeader: "",
		},
	}
	if m.forwardToken != "" {
		headers = append(headers, proxy.ConfiguredHeader{
			IsRequired:             true,
			Name:                   wire.HeaderTunnelForwardToken,
			StaticValue:            m.forwardToken,
			ValueFromRequestHeader: "",
		})
	}
	if consumerSession := tunnelConsumerSessionKey(r); consumerSession != "" {
		headers = append(headers, proxy.ConfiguredHeader{
			IsRequired:             false,
			Name:                   wire.HeaderTunnelConsumerSession,
			StaticValue:            consumerSession,
			ValueFromRequestHeader: "",
		})
	}
	return headers
}

func tunnelGatewayURL(addr string) (string, error) {
	addr = strings.TrimSpace(addr)
	if addr == "" {
		return "", fmt.Errorf("empty tunnel route address")
	}
	u, err := url.Parse(addr)
	if err == nil && u.Scheme != "" {
		switch u.Scheme {
		case "http", "https":
			if u.Hostname() == "" {
				return "", fmt.Errorf("tunnel route URL %q is missing a host", addr)
			}
			return u.String(), nil
		default:
			if strings.Contains(addr, "://") {
				return "", fmt.Errorf("unsupported tunnel route URL scheme %q", u.Scheme)
			}
		}
	}
	if strings.Contains(addr, "://") {
		return "", fmt.Errorf("invalid tunnel route URL %q", addr)
	}
	return (&url.URL{Scheme: "http", Host: addr}).String(), nil
}

func tunnelConsumerSessionKey(r *http.Request) string {
	if value := strings.TrimSpace(r.Header.Get(proxy.McpSessionIDHeader)); value != "" {
		return hashedTunnelConsumerSession(tunnelConsumerSessionMCPPrefix, value)
	}
	if value := AuthorizationOrChatSessionToken(r); value != "" {
		return hashedTunnelConsumerSession(tunnelConsumerSessionAuthPrefix, value)
	}
	if value := strings.TrimSpace(r.Header.Get("User-Agent")); value != "" && r.RemoteAddr != "" {
		return hashedTunnelConsumerSession(tunnelConsumerSessionAnonymousPrefix, r.RemoteAddr+"|"+value)
	}
	if r.RemoteAddr != "" {
		return hashedTunnelConsumerSession(tunnelConsumerSessionAnonymousPrefix, r.RemoteAddr)
	}
	return ""
}

func hashedTunnelConsumerSession(prefix, value string) string {
	sum := sha256.Sum256([]byte(value))
	return prefix + ":" + hex.EncodeToString(sum[:])
}
