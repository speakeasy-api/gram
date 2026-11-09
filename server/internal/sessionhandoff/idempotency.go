package sessionhandoff

import (
	"crypto/sha256"
	"encoding/hex"
	"strconv"
	"strings"
	"time"

	"github.com/google/uuid"
)

const MaxIdempotencyKeyBytes = 128

// PlatformIdempotencyInputHash binds a Platform MCP retry key to the exact
// canonical session and effective TTL confirmed by the user.
func PlatformIdempotencyInputHash(sessionID string, ttl time.Duration) string {
	return canonicalInputHash("platform-v1", canonicalSessionID(sessionID), strconv.FormatInt(int64(ttl/time.Second), 10))
}

// DeviceIdempotencyInputHash additionally binds a device retry to the exact
// rendered content and normalized source/device identity fields.
func DeviceIdempotencyInputHash(sessionID, content, sourceSurface, deviceSerial, deviceHostname string, ttl time.Duration) string {
	contentHash := sha256.Sum256([]byte(content))
	return canonicalInputHash(
		"device-v1",
		canonicalSessionID(sessionID),
		strconv.FormatInt(int64(ttl/time.Second), 10),
		hex.EncodeToString(contentHash[:]),
		strings.TrimSpace(sourceSurface),
		strings.TrimSpace(deviceSerial),
		strings.TrimSpace(deviceHostname),
	)
}

// canonicalSessionID normalizes UUID forms accepted by session lookup while
// preserving trimmed non-UUID harness identifiers verbatim.
func canonicalSessionID(sessionID string) string {
	trimmed := strings.TrimSpace(sessionID)
	parsed, err := uuid.Parse(trimmed)
	if err != nil {
		return trimmed
	}
	return parsed.String()
}

func canonicalInputHash(fields ...string) string {
	h := sha256.New()
	for _, field := range fields {
		_, _ = h.Write([]byte(strconv.Itoa(len(field))))
		_, _ = h.Write([]byte{':'})
		_, _ = h.Write([]byte(field))
	}
	return hex.EncodeToString(h.Sum(nil))
}

func validInputHash(hash string) bool {
	if len(hash) != sha256.Size*2 {
		return false
	}
	_, err := hex.DecodeString(hash)
	return err == nil && strings.ToLower(hash) == hash
}
