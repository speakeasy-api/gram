package wire

import (
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/hex"
	"fmt"
	"strings"
)

const KeyPrefix = "gram_tunnel_"

const MaxServiceMetadataBytes = 1024

const (
	HeaderAgentVersion          = "X-Gram-Agent-Version"
	HeaderTunnelID              = "X-Gram-Tunnel-Id"
	HeaderTunnelConsumerSession = "X-Gram-Tunnel-Consumer-Session"
	// HeaderTunnelForwardToken authenticates gram-server -> gateway hops and is stripped before agent forwarding.
	HeaderTunnelForwardToken    = "X-Gram-Tunnel-Forward-Token"
	HeaderTunnelServiceVersion  = "X-Gram-Tunnel-Service-Version"
	HeaderTunnelServiceMetadata = "X-Gram-Tunnel-Service-Metadata"
	// HeaderTunnelError carries a TunnelError* status when a tunneled forward
	// fails before the backend MCP response can be relayed. Set by the gateway
	// and by gram-server routing; read by the retry/failover policy.
	HeaderTunnelError = "X-Gram-Tunnel-Error"
)

// Tunnel forward error statuses reported in HeaderTunnelError. Callers switch on
// these to decide retry/failover: *NoLiveSession and *SubstreamFailed are
// gateway-side (a route was picked but the agent session was gone/broken);
// *NoRoute, *InvalidRoute, *RouteStoreUnavailable and *RouteLookupFailed are
// gram-server-side (routing could not select a gateway owner at all).
const (
	TunnelErrorNoLiveSession         = "no-live-session"
	TunnelErrorSubstreamFailed       = "substream-failed"
	TunnelErrorNoRoute               = "no-route"
	TunnelErrorInvalidRoute          = "invalid-route"
	TunnelErrorRouteStoreUnavailable = "route-store-unavailable"
	TunnelErrorRouteLookupFailed     = "route-lookup-failed"
)

// NewKey returns one-time plaintext plus the stored SHA-256 hash.
func NewKey() (plaintext, hashHex string, err error) {
	buf := make([]byte, 32)
	if _, err := rand.Read(buf); err != nil {
		return "", "", fmt.Errorf("generate tunnel key: %w", err)
	}
	plaintext = KeyPrefix + hex.EncodeToString(buf)
	return plaintext, HashKey(plaintext), nil
}

func HashKey(key string) string {
	sum := sha256.Sum256([]byte(key))
	return hex.EncodeToString(sum[:])
}

func HasKeyPrefix(s string) bool { return strings.HasPrefix(s, KeyPrefix) }

func ConstantTimeEqual(a, b string) bool {
	return subtle.ConstantTimeCompare([]byte(a), []byte(b)) == 1
}
