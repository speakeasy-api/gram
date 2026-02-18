package mcp

import (
	"crypto/sha256"
	"encoding/hex"
	"net/http"
	"strings"
)

// ConnectionFingerprint holds request metadata used to identify anonymous users.
type ConnectionFingerprint struct {
	IPAddress string
	UserAgent string
	Origin    string
}

// ExtractConnectionFingerprint creates a fingerprint from HTTP request metadata.
// Used to identify anonymous users on public MCPs.
func ExtractConnectionFingerprint(r *http.Request) ConnectionFingerprint {
	ipAddress := r.Header.Get("X-Forwarded-For")
	if ipAddress == "" {
		ipAddress = r.Header.Get("X-Real-IP")
	}
	if ipAddress == "" {
		ipAddress = r.RemoteAddr
	}
	// Take first IP if multiple (X-Forwarded-For can have chains)
	if idx := strings.Index(ipAddress, ","); idx > 0 {
		ipAddress = strings.TrimSpace(ipAddress[:idx])
	}

	return ConnectionFingerprint{
		IPAddress: ipAddress,
		UserAgent: r.Header.Get("User-Agent"),
		Origin:    r.Header.Get("Origin"),
	}
}

// Hash returns a SHA256 hash of the fingerprint for storage.
// This provides privacy by not storing raw IP/user-agent values.
// Returns empty string if fingerprint has no data.
func (f ConnectionFingerprint) Hash() string {
	if f.IsEmpty() {
		return ""
	}
	data := f.IPAddress + "|" + f.UserAgent + "|" + f.Origin
	hash := sha256.Sum256([]byte(data))
	return hex.EncodeToString(hash[:])
}

// IsEmpty returns true if the fingerprint has no meaningful data.
func (f ConnectionFingerprint) IsEmpty() bool {
	return f.IPAddress == "" && f.UserAgent == "" && f.Origin == ""
}
