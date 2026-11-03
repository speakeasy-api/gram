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

// deviceKeyBytes is how much of the hash is kept, 32 hex characters.
const deviceKeyBytes = 16

// RecorderOption configures a Recorder.
type RecorderOption func(*[]byte)

// WithDeviceKeySecret sets the key device_key is hashed with. Without one device_key stays empty.
func WithDeviceKeySecret(secret string) RecorderOption {
	return func(key *[]byte) { *key = []byte(secret) }
}

func recorderDeviceKey(opts []RecorderOption) []byte {
	var key []byte
	for _, opt := range opts {
		opt(&key)
	}
	return key
}

// connectionIP returns the address that connected to our public ingress, or "" when unsure. The
// ingress appends it as the last X-Forwarded-For hop, and anything before that came from the caller.
// RemoteAddr is the ingress itself. A private network request does not pass through that ingress.
func connectionIP(r *http.Request) string {
	if origin, ok := requestorigin.FromContext(r.Context()); ok && origin.Surface == requestorigin.SurfacePrivateNetwork {
		return ""
	}
	lines := r.Header.Values("X-Forwarded-For")
	if len(lines) == 0 {
		return ""
	}
	line := lines[len(lines)-1]
	hop := strings.TrimSpace(line[strings.LastIndexByte(line, ',')+1:])
	addr, err := netip.ParseAddr(hop)
	if err != nil {
		return ""
	}
	return addr.Unmap().WithZone("").String()
}

// stampDevice hashes the waiting address on the writer, after the logs check, and drops the address.
func (r *Recorder) stampDevice(row *Row) {
	ip := row.clientIP
	row.clientIP = ""
	if ip == "" || len(r.deviceKey) == 0 {
		return
	}
	mac := hmac.New(sha256.New, r.deviceKey)
	mac.Write([]byte(row.OrganizationID))
	mac.Write([]byte{0})
	mac.Write([]byte(ip))
	row.DeviceKey = hex.EncodeToString(mac.Sum(nil)[:deviceKeyBytes])
}
