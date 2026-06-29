package wire

import (
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/hex"
	"fmt"
	"strings"
)

// KeyPrefix is the fixed prefix every tunnel API key carries. The cheap prefix
// check lets the gateway reject garbage before any store lookup.
const KeyPrefix = "gram_tunnel_"

// MaxServiceMetadataBytes is the serialized JSON limit for optional
// user-provided tunnel metadata.
const MaxServiceMetadataBytes = 1024

// HeaderAgentVersion / HeaderTunnelID are the wire header names.
const (
	HeaderAgentVersion          = "X-Gram-Agent-Version"
	HeaderTunnelID              = "X-Gram-Tunnel-Id"
	HeaderTunnelConsumerSession = "X-Gram-Tunnel-Consumer-Session"
	// HeaderTunnelForwardToken carries the shared secret that authenticates a
	// forward request as originating from gram-server. It is verified and then
	// stripped by the gateway; it is never forwarded to the agent.
	HeaderTunnelForwardToken    = "X-Gram-Tunnel-Forward-Token"
	HeaderTunnelServiceID       = "X-Gram-Tunnel-Service-Id"
	HeaderTunnelServiceSlug     = "X-Gram-Tunnel-Service-Slug"
	HeaderTunnelServiceVersion  = "X-Gram-Tunnel-Service-Version"
	HeaderTunnelServiceMetadata = "X-Gram-Tunnel-Service-Metadata"
)

// NewKey mints a fresh tunnel key, returning the one-time plaintext and its
// SHA-256 hash (hex). Only the hash is ever stored.
func NewKey() (plaintext, hashHex string, err error) {
	buf := make([]byte, 32)
	if _, err := rand.Read(buf); err != nil {
		return "", "", fmt.Errorf("generate tunnel key: %w", err)
	}
	plaintext = KeyPrefix + hex.EncodeToString(buf)
	return plaintext, HashKey(plaintext), nil
}

// HashKey returns the hex-encoded SHA-256 of a key string.
func HashKey(key string) string {
	sum := sha256.Sum256([]byte(key))
	return hex.EncodeToString(sum[:])
}

// HasKeyPrefix reports whether s looks like a tunnel key (prefix only).
func HasKeyPrefix(s string) bool { return strings.HasPrefix(s, KeyPrefix) }

// ConstantTimeEqual compares two hex hashes without leaking timing.
func ConstantTimeEqual(a, b string) bool {
	return subtle.ConstantTimeCompare([]byte(a), []byte(b)) == 1
}
