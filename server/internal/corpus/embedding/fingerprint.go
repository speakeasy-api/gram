package embedding

import (
	"crypto/sha256"
	"encoding/hex"
)

// Fingerprint computes a SHA-256 hash of the content, strategy, metadata, and
// manifest fingerprint. Chunks with identical fingerprints can skip re-embedding.
func Fingerprint(content string, strategy string, metadata string, manifestFingerprint string) string {
	h := sha256.New()
	// Use null-byte separators to avoid collisions between concatenated fields.
	h.Write([]byte(content))
	h.Write([]byte{0})
	h.Write([]byte(strategy))
	h.Write([]byte{0})
	h.Write([]byte(metadata))
	h.Write([]byte{0})
	h.Write([]byte(manifestFingerprint))
	return hex.EncodeToString(h.Sum(nil))
}
