package sessions

import (
	"encoding/base64"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

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
