package sessionhandoff

import (
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
)

func TestCanonicalInputHashFramesFieldBoundaries(t *testing.T) {
	t.Parallel()
	require.NotEqual(t, canonicalInputHash("ab", "c"), canonicalInputHash("a", "bc"))
}

func TestIdempotencyHashesNormalizeInputsAndSeparateDomains(t *testing.T) {
	t.Parallel()
	id := uuid.New()
	ttl := 5 * time.Minute
	canonical := id.String()
	alternate := " urn:uuid:" + strings.ToUpper(canonical) + " "

	platformCanonical := PlatformIdempotencyInputHash(canonical, ttl)
	require.Equal(t, platformCanonical, PlatformIdempotencyInputHash(alternate, ttl))

	deviceCanonical := DeviceIdempotencyInputHash(canonical, "content", "codex", "serial", "host", ttl)
	require.Equal(t, deviceCanonical, DeviceIdempotencyInputHash(alternate, "content", " codex ", " serial ", " host ", ttl))
	require.NotEqual(t, platformCanonical, deviceCanonical)
	require.NotEqual(t, deviceCanonical, DeviceIdempotencyInputHash(canonical, "changed", "codex", "serial", "host", ttl))
}

func TestCanonicalSessionIDPreservesTrimmedNonUUID(t *testing.T) {
	t.Parallel()
	require.Equal(t, "native-session", canonicalSessionID("  native-session\t"))
}

func TestValidInputHashRequiresLowercaseSHA256Hex(t *testing.T) {
	t.Parallel()
	valid := strings.Repeat("a", 64)
	require.True(t, validInputHash(valid))
	require.False(t, validInputHash(strings.ToUpper(valid)))
	require.False(t, validInputHash(valid[:63]))
	require.False(t, validInputHash(valid+"a"))
	require.False(t, validInputHash(strings.Repeat("z", 64)))
}
