package mcpconnectionattempts

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"net/http"
	"net/netip"
	"strings"

	"github.com/speakeasy-api/gram/server/internal/requestorigin"
)

const (
	// deviceKeyBytes is how much of the hash is kept, 32 hex characters.
	deviceKeyBytes = 16

	// deviceKeyInfo keeps device keys cryptographically separate from other uses of the same secret.
	deviceKeyInfo = "gram/connection/device"
)

// DeriveKey derives a per-organization key for a purpose, as risk.Fingerprinter.DeriveCurrentKey
// does. This package can't import risk, which depends on it through remotesessions.
type DeriveKey func(info, organizationID string) ([]byte, error)

// RecorderOption configures a Recorder.
type RecorderOption func(*Recorder)

// WithDeviceKeys sets where device keys come from. Without them device_key stays empty.
func WithDeviceKeys(derive DeriveKey) RecorderOption {
	return func(r *Recorder) { r.deviceKeys = derive }
}

// lastForwardedFor returns the end of the last X-Forwarded-For line, unparsed. A private network
// request does not pass through our public ingress, so it has no hop we can trust.
func lastForwardedFor(r *http.Request) string {
	if origin, ok := requestorigin.FromContext(r.Context()); ok && origin.Surface == requestorigin.SurfacePrivateNetwork {
		return ""
	}
	lines := r.Header.Values("X-Forwarded-For")
	if len(lines) == 0 {
		return ""
	}
	line := lines[len(lines)-1]
	return strings.Clone(line[max(0, len(line)-maxForwardedForBytes):])
}

// connectionIP returns the address that connected to our public ingress, or "" when unsure. The
// ingress appends it as the last X-Forwarded-For hop, and anything before that came from the caller.
// RemoteAddr is the ingress itself, so it never stands in.
func connectionIP(forwardedFor string) string {
	hop := strings.TrimSpace(forwardedFor[strings.LastIndexByte(forwardedFor, ',')+1:])
	addr, err := netip.ParseAddr(hop)
	if err != nil {
		return ""
	}
	return addr.Unmap().WithZone("").String()
}

// deviceKeyFor hashes an address under the organization's key. With no address, or no usable
// key, it returns "". After a key rotation a device reads as new.
func (r *Recorder) deviceKeyFor(organizationID, ip string) string {
	if ip == "" || r.deviceKeys == nil {
		return ""
	}
	key, err := r.deviceKeys(deviceKeyInfo, organizationID)
	if err != nil {
		return ""
	}
	mac := hmac.New(sha256.New, key)
	mac.Write([]byte(ip))
	return hex.EncodeToString(mac.Sum(nil)[:deviceKeyBytes])
}
