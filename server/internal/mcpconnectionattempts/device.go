package mcpconnectionattempts

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"net/http"

	"github.com/speakeasy-api/gram/server/internal/requestip"
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

// publicClientIP uses only the public listener's resolved address. Private
// traffic has no public ingress address, even when a caller sends X-Client-IP.
func publicClientIP(r *http.Request) string {
	if origin, ok := requestorigin.FromContext(r.Context()); ok && origin.Surface == requestorigin.SurfacePrivateNetwork {
		return ""
	}
	resolved, _ := requestip.FromContext(r.Context())
	return resolved.String()
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
