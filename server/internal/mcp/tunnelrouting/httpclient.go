package tunnelrouting

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"

	"github.com/speakeasy-api/gram/server/internal/guardian"
	"github.com/speakeasy-api/gram/tunnel/route"
	"github.com/speakeasy-api/gram/tunnel/wire"
)

// ErrNoTunnelRoute means no live gateway route exists to carry a tunneled
// request. Tunnel outages are usually customer-side (the agent is down), so
// callers should surface this as an upstream-availability problem rather than
// a platform fault.
var ErrNoTunnelRoute = errors.New("tunnel has no live route")

// maxForwardAttempts bounds the gateway failover loop for one request. Each
// retried attempt excludes the gateway that refused, so the loop also ends
// when candidates run out.
const maxForwardAttempts = 3

// HTTPClient sends ordinary HTTP requests through an MCP tunnel: it selects a
// live gateway route for the tunnel, rewrites the request onto the gateway
// address, and attaches the forwarding headers. The request URL's scheme and
// host are identity only — the tunnel agent pins them from its local
// configuration, so only the path and query reach the customer network.
//
// This is the transport for Gram's back-channel calls to authorization
// servers that sit inside a customer network (token exchange, refresh,
// revocation, dynamic client registration). The MCP serving path has its own
// tunnel transport in the reverse proxy; this one exists for plain
// request/response callers that hold an *http.Request.
type HTTPClient struct {
	routes       route.Store
	forwardToken string
	client       *guardian.HTTPClient
}

// NewHTTPClient builds a tunnel-forwarding HTTP client. gatewayCIDRs are
// allowlisted past the guardian egress policy for the gateway dial only —
// gateway addresses come from the trusted route store but typically live in
// RFC1918 cluster ranges the default policy blocks. Empty means no
// relaxation: forwards to private gateway addresses then fail closed.
func NewHTTPClient(routes route.Store, forwardToken string, policy *guardian.Policy, gatewayCIDRs []string) *HTTPClient {
	options := []guardian.ClientOption{}
	if len(gatewayCIDRs) > 0 {
		options = append(options, guardian.WithAllowedCIDRBlocks(gatewayCIDRs...))
	}
	client := policy.PooledClient(options...)
	// Redirects cannot be followed across a tunnel boundary, and a redirect
	// from a token endpoint must never re-send client credentials to a new
	// location. The 3xx is returned to the caller, which treats any non-2xx
	// as a failed call.
	client.CheckRedirect = func(*http.Request, []*http.Request) error {
		return http.ErrUseLastResponse
	}
	return &HTTPClient{
		routes:       routes,
		forwardToken: forwardToken,
		client:       client,
	}
}

// Do sends req through the tunnel identified by tunnelID.
//
// Failover mirrors the MCP proxy's retry policy: a gateway reporting
// no-live-session has its stale route unpublished and another candidate is
// tried; a gateway at its substream cap (tunnel-busy) keeps its route and
// another candidate is tried. A substream-failed response is returned as-is,
// never replayed — the request may already have reached the backend, and the
// calls this client carries are non-idempotent POSTs.
func (c *HTTPClient) Do(req *http.Request, tunnelID string) (*http.Response, error) {
	if c == nil || c.routes == nil {
		return nil, fmt.Errorf("tunnel forwarding is not configured")
	}
	if c.forwardToken == "" {
		return nil, fmt.Errorf("tunnel forward token is not configured")
	}
	ctx := req.Context()

	// Buffered so a failover attempt can replay it. The bodies this client
	// carries are small token-endpoint forms and registration documents,
	// bounded by their callers.
	var body []byte
	if req.Body != nil {
		b, err := io.ReadAll(req.Body)
		_ = req.Body.Close()
		if err != nil {
			return nil, fmt.Errorf("buffer tunnel request body: %w", err)
		}
		body = b
	}

	candidates, err := c.routes.Candidates(ctx, tunnelID)
	if err != nil {
		return nil, fmt.Errorf("list tunnel routes: %w", err)
	}

	exclude := map[string]struct{}{}
	for range maxForwardAttempts {
		addr, ok := SelectRoute("", candidates, exclude)
		if !ok {
			return nil, ErrNoTunnelRoute
		}
		gatewayURL, err := GatewayURL(addr)
		if err != nil {
			return nil, fmt.Errorf("build tunnel route URL: %w", err)
		}
		forward, err := buildForward(req, body, gatewayURL, tunnelID, c.forwardToken)
		if err != nil {
			return nil, err
		}

		resp, err := c.client.Do(forward)
		if err != nil {
			return nil, fmt.Errorf("forward request via tunnel: %w", err)
		}

		if resp.StatusCode != http.StatusBadGateway {
			return resp, nil
		}
		switch resp.Header.Get(ErrorHeader) {
		case wire.TunnelErrorNoLiveSession:
			drainAndClose(resp)
			if err := c.routes.Unpublish(ctx, tunnelID, addr); err != nil {
				return nil, fmt.Errorf("unpublish stale tunnel route: %w", err)
			}
			exclude[addr] = struct{}{}
		case wire.TunnelErrorTunnelBusy:
			drainAndClose(resp)
			exclude[addr] = struct{}{}
		default:
			return resp, nil
		}
	}

	return nil, fmt.Errorf("no tunnel gateway accepted the request after %d attempts: %w", maxForwardAttempts, ErrNoTunnelRoute)
}

// buildForward clones req onto the gateway address, keeping only the path and
// query of the original URL, and attaches the tunnel forwarding headers.
func buildForward(req *http.Request, body []byte, gatewayURL, tunnelID, forwardToken string) (*http.Request, error) {
	target, err := url.Parse(gatewayURL)
	if err != nil {
		return nil, fmt.Errorf("parse tunnel gateway URL: %w", err)
	}

	u := *req.URL
	u.Scheme = target.Scheme
	u.Host = target.Host
	u.User = nil
	if target.Path != "" && target.Path != "/" {
		u.Path = strings.TrimRight(target.Path, "/") + u.Path
	}

	forward := req.Clone(req.Context())
	forward.URL = &u
	// The original request's host names the (unreachable) upstream endpoint;
	// clearing it makes the client send the gateway's host instead.
	forward.Host = ""
	forward.Body = io.NopCloser(bytes.NewReader(body))
	forward.ContentLength = int64(len(body))
	forward.GetBody = func() (io.ReadCloser, error) {
		return io.NopCloser(bytes.NewReader(body)), nil
	}
	forward.Header.Set(wire.HeaderTunnelID, tunnelID)
	forward.Header.Set(wire.HeaderTunnelForwardToken, forwardToken)
	return forward, nil
}

// drainAndClose consumes a response that is about to be retried so its
// connection can return to the pool.
func drainAndClose(resp *http.Response) {
	_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, 4<<10))
	_ = resp.Body.Close()
}
