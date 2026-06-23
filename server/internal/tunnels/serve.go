// Package tunnels implements the gram-server side of the secure tunnel: given a
// tunnel-backed MCP server, look up the owning gateway pod (route cache) and
// reverse-proxy the MCP request to it over plain pod-to-pod HTTP. The gateway
// maps it onto a yamux substream to the customer's agent.
//
// POC scope: no database. The tenant-isolation invariant still holds — a
// tunnelID is never accepted as caller input; ServeTunnel takes it as a
// resolved argument that, in the full build, comes from a project-scoped join
// on the mcp_servers row (same trust shape as RemoteMcpServerID / ToolsetID).
// Wiring into internal/mcp's serveendpoint + the remotemcp interceptor chain is
// left as a one-line call site (see ServeTunnel doc) and intentionally omitted
// here to keep the POC free of the DB-backed serve path.
package tunnels

import (
	"context"
	"fmt"
	"log/slog"
	"net/http"
	"net/http/httputil"
	"net/url"

	"github.com/speakeasy-api/gram/tunnel/route"
	"github.com/speakeasy-api/gram/tunnel/wire"
)

// Service resolves tunnel routes and forwards MCP requests to the owning
// gateway pod.
type Service struct {
	routes route.Store
	logger *slog.Logger
	// client transport reused across forwards (pod-to-pod, keep-alive friendly).
	transport http.RoundTripper
}

// NewService builds the serve-tunnel service over a route store.
func NewService(routes route.Store, logger *slog.Logger) *Service {
	return &Service{
		routes:    routes,
		logger:    logger.With(slog.String("component", "tunnel")),
		transport: http.DefaultTransport,
	}
}

// ServeTunnel forwards an MCP request to the agent behind tunnelID. In the full
// build this is invoked from internal/mcp's serve path after the project-scoped
// join yields tunnelID and after the shared remotemcp interceptor chain (authz,
// usage limits, usage tracking, ClickHouse logging) has run — i.e. tunnel
// traffic is metered exactly like remotemcp traffic.
func (s *Service) ServeTunnel(w http.ResponseWriter, r *http.Request, tunnelID string) {
	addr, ok, err := s.routes.Lookup(r.Context(), tunnelID)
	if err != nil {
		s.logger.ErrorContext(r.Context(), "tunnel route lookup failed",
			slog.String("tunnel_id", tunnelID), slog.Any("error", err))
		writeTunnelError(w, http.StatusBadGateway, "route-lookup-failed")
		return
	}
	if !ok {
		// No live route: the tunnel exists but no agent is currently connected.
		s.logger.WarnContext(r.Context(), "tunnel has no route",
			slog.String("tunnel_id", tunnelID))
		writeTunnelError(w, http.StatusBadGateway, "no-route")
		return
	}

	gatewayURL := &url.URL{Scheme: "http", Host: addr}
	proxy := &httputil.ReverseProxy{
		Director: func(req *http.Request) {
			req.URL.Scheme = gatewayURL.Scheme
			req.URL.Host = gatewayURL.Host
			// tunnelID is injected here, server-side — never from the caller.
			req.Header.Set(wire.HeaderTunnelID, tunnelID)
		},
		Transport:     s.transport,
		FlushInterval: -1, // stream SSE immediately
		ErrorHandler: func(rw http.ResponseWriter, _ *http.Request, perr error) {
			s.logger.WarnContext(r.Context(), "tunnel forward failed",
				slog.String("tunnel_id", tunnelID), slog.Any("error", perr))
			writeTunnelError(rw, http.StatusBadGateway, "gateway-unreachable")
		},
	}
	proxy.ServeHTTP(w, r)
}

// RouteExists reports whether a live route is currently published for tunnelID
// (for /rpc/tunnels.get style status surfacing without the DB).
func (s *Service) RouteExists(ctx context.Context, tunnelID string) (bool, error) {
	_, ok, err := s.routes.Lookup(ctx, tunnelID)
	if err != nil {
		return false, fmt.Errorf("route lookup: %w", err)
	}
	return ok, nil
}

func writeTunnelError(w http.ResponseWriter, status int, reason string) {
	w.Header().Set("X-Gram-Tunnel-Error", reason)
	http.Error(w, "tunnel: "+reason, status)
}
