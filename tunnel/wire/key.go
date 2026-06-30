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
	HeaderTunnelServiceID       = "X-Gram-Tunnel-Service-Id"
	HeaderTunnelServiceSlug     = "X-Gram-Tunnel-Service-Slug"
	HeaderTunnelServiceVersion  = "X-Gram-Tunnel-Service-Version"
	HeaderTunnelServiceMetadata = "X-Gram-Tunnel-Service-Metadata"
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
