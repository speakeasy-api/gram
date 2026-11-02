package mcp

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"

	"github.com/google/uuid"

	"github.com/speakeasy-api/gram/server/internal/attr"
	"github.com/speakeasy-api/gram/server/internal/constants"
	"github.com/speakeasy-api/gram/server/internal/guardian"
	"github.com/speakeasy-api/gram/server/internal/mcp/mcpversions"
	"github.com/speakeasy-api/gram/server/internal/mcp/toolfilter"
	"github.com/speakeasy-api/gram/server/internal/mcp/tunnelrouting"
	"github.com/speakeasy-api/gram/server/internal/mcpauthz"
	"github.com/speakeasy-api/gram/server/internal/mcpservers"
	mcpserversrepo "github.com/speakeasy-api/gram/server/internal/mcpservers/repo"
	"github.com/speakeasy-api/gram/server/internal/oops"
	"github.com/speakeasy-api/gram/server/internal/remotemcp"
	"github.com/speakeasy-api/gram/server/internal/remotemcp/proxy"
	"github.com/speakeasy-api/gram/tunnel/metrics"
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

	// Request is the user's MCP request on the serving paths. When no agent is
	// connected, it is recorded as a failed request in the tunnel's activity.
	// Speakeasy-initiated callers leave it nil.
	Request *http.Request
}

// errTunnelNoRoute marks a tunneled server whose tunnel has no live route.
var errTunnelNoRoute = errors.New("tunnel has no live route")

// tunnelUnavailableMessage stays free of tunnel internals: public callers read it.
const tunnelUnavailableMessage = "This MCP server can't be reached right now. Retry shortly. If it keeps happening, ask your administrator to check that it's running."

// tunnelOutageRetryAfter paces the retry and tags the 503 external: the customer runs the tunnel.
func tunnelOutageRetryAfter(w http.ResponseWriter, err error) {
	if errors.Is(err, errTunnelNoRoute) {
		setOutageRetryAfter(w)
		w.Header().Set(constants.HeaderProxiedResponse, "1")
	}
}

// buildProxy constructs the tunnel-backed proxy for one request.
func (m *tunnelManager) buildProxy(
	ctx context.Context,
	logger *slog.Logger,
	params buildProxyParams,
	options ...remotemcp.BuildOption,
) (*proxy.Proxy, error) {
	mcpServer := params.MCPServer
	if m.proxyManager == nil {
		return nil, oops.E(oops.CodeUnexpected, nil, "remote MCP proxy manager is unavailable").LogError(ctx, logger)
	}

	tunnelID := mcpServer.TunneledMcpServerID.UUID.String()
	if m.routes == nil {
		return nil, oops.E(oops.CodeGatewayError, nil, "tunnel route store unavailable").LogError(ctx, logger)
	}

	candidates, err := m.routes.Candidates(ctx, tunnelID)
	if err != nil {
		return nil, oops.E(oops.CodeGatewayError, err, "list tunnel routes").LogError(ctx, logger)
	}
	addr, ok := tunnelrouting.SelectRoute(params.ClientAffinityKey, candidates, nil)
	if !ok {
		// Likely customer-side, so warn. A 404 would read as a lost session or server.
		m.observeNoRoute(tunnelID, mcpServer.ID.String(), params.Request)
		return nil, oops.E(oops.CodeUnavailable, errTunnelNoRoute, "%s", tunnelUnavailableMessage).LogWarn(ctx, logger.With(attr.SlogErrorMessage("tunnel has no live route")))
	}

	gatewayURL, err := tunnelrouting.GatewayURL(addr)
	if err != nil {
		return nil, oops.E(oops.CodeGatewayError, err, "tunnel route is invalid").LogError(ctx, logger)
	}

	options = append(options, remotemcp.WithHeaderPolicy(proxy.HeaderPolicyTunneled))
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

// noRouteBodyLimit bounds how much of a request body is read to find its
// JSON-RPC method when no agent is connected.
const noRouteBodyLimit = 64 << 10

// observeNoRoute records a request that failed because no agent was connected,
// so an outage shows in the tunnel's activity rather than as a gap. Like the
// proxy's own observation, only a single JSON-RPC request with an ID counts.
func (m *tunnelManager) observeNoRoute(tunnelID, mcpServerID string, r *http.Request) {
	if r == nil || r.Body == nil || m.proxyManager == nil || m.proxyManager.TunnelMetrics == nil {
		return
	}
	body, err := io.ReadAll(io.LimitReader(r.Body, noRouteBodyLimit))
	r.Body = io.NopCloser(bytes.NewReader(body))
	if err != nil {
		return
	}
	var rpc struct {
		ID     json.RawMessage `json:"id"`
		Method string          `json:"method"`
		Params json.RawMessage `json:"params"`
	}
	if json.Unmarshal(body, &rpc) != nil || rpc.Method == "" || len(rpc.ID) == 0 || string(rpc.ID) == "null" {
		return
	}
	// An initialize is attributed to the client it reports, as when routed.
	// Other requests keep their User-Agent rather than pay a session lookup.
	reported := ""
	if rpc.Method == mcpversions.MethodInitialize {
		var params struct {
			ClientInfo struct {
				Name string `json:"name"`
			} `json:"clientInfo"`
		}
		if json.Unmarshal(rpc.Params, &params) == nil {
			reported = reportedClientFamily(params.ClientInfo.Name)
		}
	}
	method, client := metrics.Method(rpc.Method), metrics.PreferredFamily(reported, metrics.ClientFamily(r.UserAgent()))
	m.proxyManager.TunnelMetrics.Observe(tunnelID, mcpServerID, method, client, "attempt", 0)
	m.proxyManager.TunnelMetrics.Observe(tunnelID, mcpServerID, method, client, "unrouted", 0)
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
