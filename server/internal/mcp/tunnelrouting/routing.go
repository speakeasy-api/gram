// Package tunnelrouting selects tunnel gateway owners and handles retry policy.
package tunnelrouting

import (
	"context"
	cryptorand "crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"math/big"
	"net"
	"net/http"
	"net/url"
	"strings"

	"github.com/speakeasy-api/gram/server/internal/mcp/httpheaders"
	"github.com/speakeasy-api/gram/server/internal/remotemcp/proxy"
	"github.com/speakeasy-api/gram/tunnel/route"
	"github.com/speakeasy-api/gram/tunnel/wire"
)

const (
	// ErrorHeader is set by the gateway when a tunneled forward fails before the
	// backend MCP response can be relayed. Aliased to the shared wire constant so
	// gateway and gram-server routing agree on the header name.
	ErrorHeader = wire.HeaderTunnelError

	clientAffinityAuthPrefix = "auth"
)

// ClientAffinityKeyFromRequest derives the stable affinity key used for both
// gateway-owner selection and local agent-session selection.
func ClientAffinityKeyFromRequest(r *http.Request) string {
	return ClientAffinityKey(httpheaders.AuthorizationOrChatSessionToken(r))
}

// ClientAffinityKey hashes an auth/session token into an affinity key.
func ClientAffinityKey(identityToken string) string {
	if identityToken == "" {
		return ""
	}
	return HashedClientAffinityKey(clientAffinityAuthPrefix, identityToken)
}

// HashedClientAffinityKey hashes a value under a namespace prefix.
func HashedClientAffinityKey(prefix, value string) string {
	sum := sha256.Sum256([]byte(value))
	return prefix + ":" + hex.EncodeToString(sum[:])
}

// Headers builds the forwarding headers for a selected tunnel route.
func Headers(tunnelID, forwardToken, clientAffinityKey string) []proxy.ConfiguredHeader {
	headers := []proxy.ConfiguredHeader{
		{
			IsRequired:             true,
			Name:                   wire.HeaderTunnelID,
			StaticValue:            tunnelID,
			ValueFromRequestHeader: "",
		},
	}
	if forwardToken != "" {
		headers = append(headers, proxy.ConfiguredHeader{
			IsRequired:             true,
			Name:                   wire.HeaderTunnelForwardToken,
			StaticValue:            forwardToken,
			ValueFromRequestHeader: "",
		})
	}
	if clientAffinityKey != "" {
		headers = append(headers, proxy.ConfiguredHeader{
			IsRequired:             false,
			Name:                   wire.HeaderTunnelConsumerSession,
			StaticValue:            clientAffinityKey,
			ValueFromRequestHeader: "",
		})
	}
	return headers
}

// SelectRoute chooses a gateway owner. Stable client keys use rendezvous
// hashing; anonymous requests are intentionally random rather than sticky.
func SelectRoute(clientAffinityKey string, candidates []string, exclude map[string]struct{}) (string, bool) {
	if clientAffinityKey != "" {
		return wire.RendezvousPick(clientAffinityKey, candidates, exclude)
	}

	available := make([]string, 0, len(candidates))
	for _, candidate := range candidates {
		if _, skip := exclude[candidate]; skip {
			continue
		}
		available = append(available, candidate)
	}
	if len(available) == 0 {
		return "", false
	}
	index, err := cryptorand.Int(cryptorand.Reader, big.NewInt(int64(len(available))))
	if err != nil {
		return "", false
	}
	return available[index.Int64()], true
}

// Retryer returns the tunnel-specific proxy retry policy.
func Retryer(routes route.Store, tunnelID, selectedAddr, clientAffinityKey, forwardToken string) proxy.UpstreamRetryer {
	return func(ctx context.Context, resp *http.Response, forwardErr error) (*proxy.UpstreamRetry, error) {
		if forwardErr != nil {
			var opErr *net.OpError
			if ctx.Err() != nil || !errors.As(forwardErr, &opErr) || opErr.Op != "dial" {
				return nil, nil
			}
			if err := unpublishSelected(ctx, routes, tunnelID, selectedAddr); err != nil {
				return nil, err
			}
			return retryTarget(ctx, routes, tunnelID, selectedAddr, clientAffinityKey, forwardToken)
		}
		if resp == nil || resp.StatusCode != http.StatusBadGateway {
			return nil, nil
		}

		tunnelErr := resp.Header.Get(ErrorHeader)
		switch tunnelErr {
		case wire.TunnelErrorNoLiveSession:
			if err := unpublishSelected(ctx, routes, tunnelID, selectedAddr); err != nil {
				return nil, err
			}
		case wire.TunnelErrorTunnelBusy:
			// The gateway is healthy but at its substream cap: keep its route
			// published and try another candidate. The request never entered
			// a substream, so replay is safe for any method.
		case wire.TunnelErrorSubstreamFailed:
			// substream-failed can fire after the request body reached the
			// backend (the substream died awaiting response headers), so the
			// call may have executed. Replaying a POST would double-execute
			// non-idempotent tools/call requests — only retry methods that
			// are safe to re-issue. no-live-session (above) is always safe:
			// the gateway never opened a substream, so the request cannot
			// have reached the backend.
			if resp.Request == nil || (resp.Request.Method != http.MethodGet && resp.Request.Method != http.MethodDelete) {
				return nil, nil
			}
		default:
			return nil, nil
		}

		excludedAddr := selectedAddr
		if tunnelErr == wire.TunnelErrorSubstreamFailed {
			excludedAddr = ""
		}
		return retryTarget(ctx, routes, tunnelID, excludedAddr, clientAffinityKey, forwardToken)
	}
}

func unpublishSelected(ctx context.Context, routes route.Store, tunnelID, selectedAddr string) error {
	if selectedAddr == "" {
		return nil
	}
	if err := routes.Unpublish(ctx, tunnelID, selectedAddr); err != nil {
		return fmt.Errorf("unpublish stale tunnel route: %w", err)
	}
	return nil
}

func retryTarget(ctx context.Context, routes route.Store, tunnelID, excludedAddr, clientAffinityKey, forwardToken string) (*proxy.UpstreamRetry, error) {
	candidates, err := routes.Candidates(ctx, tunnelID)
	if err != nil {
		return nil, fmt.Errorf("list tunnel retry routes: %w", err)
	}
	var exclude map[string]struct{}
	if excludedAddr != "" {
		exclude = map[string]struct{}{excludedAddr: {}}
	}
	addr, ok := SelectRoute(clientAffinityKey, candidates, exclude)
	if !ok {
		return nil, nil
	}
	gatewayURL, err := GatewayURL(addr)
	if err != nil {
		return nil, fmt.Errorf("build tunnel retry route URL: %w", err)
	}
	return &proxy.UpstreamRetry{
		RemoteURL: gatewayURL,
		Headers:   Headers(tunnelID, forwardToken, clientAffinityKey),
	}, nil
}

// GatewayURL normalizes a route store address into the proxy's upstream URL.
func GatewayURL(addr string) (string, error) {
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
