package sessions

import (
	"encoding/base64"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/constants"
)

func TestCurrentPlatformAdminRequiresDurableEntitlementAndLiveUser(t *testing.T) {
	t.Parallel()

	require.True(t, isCurrentPlatformAdmin(true, false))
	require.False(t, isCurrentPlatformAdmin(false, false))
	require.False(t, isCurrentPlatformAdmin(true, true))
	require.False(t, isCurrentPlatformAdmin(false, true))
}

func TestValidSupportSessionRejectsExpiredSession(t *testing.T) {
	t.Parallel()

	now := time.Now()
	session := Session{
		ActiveOrganizationID:  "org_123",
		SupportOrganizationID: "org_123",
		SupportExpiresAt:      now.Add(-time.Second),
	}
	require.False(t, validSupportSession(session, true, now))
}

func TestSessionTTLUsesIdleTimeout(t *testing.T) {
	t.Parallel()

	require.Equal(t, constants.SessionIdleTimeout, (Session{}).TTL())
	require.Equal(t, "sessions:v2:session-id", SessionCacheKey("session-id"))
}

func TestSupportSessionTTLUsesAbsoluteExpiry(t *testing.T) {
	t.Parallel()

	expiresAt := time.Now().Add(time.Hour)
	ttl := (Session{SupportExpiresAt: expiresAt}).TTL()
	require.InDelta(t, time.Hour, ttl, float64(time.Second))
}

func TestNewSessionID(t *testing.T) {
	t.Parallel()

	const iterations = 1000
	seen := make(map[string]struct{}, iterations)

	for range iterations {
		token, err := NewSessionID()
		if err != nil {
			t.Fatalf("NewSessionID() returned error: %v", err)
		}

		if _, dup := seen[token]; dup {
			t.Fatalf("NewSessionID() produced a duplicate token: %q", token)
		}
		seen[token] = struct{}{}

		decoded, err := base64.RawURLEncoding.DecodeString(token)
		if err != nil {
			t.Fatalf("token %q is not valid base64url: %v", token, err)
		}

		if len(decoded) != sessionTokenBytes {
			t.Fatalf("token decoded to %d bytes, want %d", len(decoded), sessionTokenBytes)
		}
	}
}
