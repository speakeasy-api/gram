package jwtclaims

import (
	"encoding/base64"
	"encoding/json"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

// buildUnsignedJWT constructs a minimal unsigned JWT (alg:"none") with the
// given claims payload. Good enough for ParseUnverified.
func buildUnsignedJWT(claims map[string]any) string {
	header, _ := json.Marshal(map[string]string{"alg": "none", "typ": "JWT"})
	payload, _ := json.Marshal(claims)
	encode := base64.RawURLEncoding.EncodeToString
	return encode(header) + "." + encode(payload) + "."
}

func TestUnsafeExtractSubjectValidJWT(t *testing.T) {
	t.Parallel()
	token := buildUnsignedJWT(map[string]any{"sub": "user-123", "iss": "https://example.com"})
	got := UnsafeExtractSubject(token)
	require.Equal(t, "user-123", got)
}

func TestUnsafeExtractSubjectMissingSub(t *testing.T) {
	t.Parallel()
	token := buildUnsignedJWT(map[string]any{"iss": "https://example.com"})
	got := UnsafeExtractSubject(token)
	require.Empty(t, got)
}

func TestUnsafeExtractSubjectEmptyString(t *testing.T) {
	t.Parallel()
	got := UnsafeExtractSubject("")
	require.Empty(t, got)
}

func TestUnsafeExtractSubjectNotAJWT(t *testing.T) {
	t.Parallel()
	got := UnsafeExtractSubject("not-a-jwt-token")
	require.Empty(t, got)
}

func TestUnsafeExtractSubjectOpaqueToken(t *testing.T) {
	t.Parallel()
	got := UnsafeExtractSubject("eyJhbGciOiJSUzI1NiJ9.notvalidbase64.sig")
	require.Empty(t, got)
}

func TestUnsafeExtractExpiryValidJWT(t *testing.T) {
	t.Parallel()
	exp := time.Date(2026, 8, 27, 12, 0, 0, 0, time.UTC)
	token := buildUnsignedJWT(map[string]any{"sub": "user-123", "exp": exp.Unix()})
	got, ok := UnsafeExtractExpiry(token)
	require.True(t, ok)
	require.Equal(t, exp, got.UTC())
}

func TestUnsafeExtractExpiryMissingExp(t *testing.T) {
	t.Parallel()
	token := buildUnsignedJWT(map[string]any{"sub": "user-123"})
	_, ok := UnsafeExtractExpiry(token)
	require.False(t, ok)
}

func TestUnsafeExtractExpiryNonNumericExp(t *testing.T) {
	t.Parallel()
	token := buildUnsignedJWT(map[string]any{"exp": "tomorrow"})
	_, ok := UnsafeExtractExpiry(token)
	require.False(t, ok)
}

func TestUnsafeExtractExpiryEmptyString(t *testing.T) {
	t.Parallel()
	_, ok := UnsafeExtractExpiry("")
	require.False(t, ok)
}

func TestUnsafeExtractExpiryNotAJWT(t *testing.T) {
	t.Parallel()
	_, ok := UnsafeExtractExpiry("xoxp-opaque-token")
	require.False(t, ok)
}
