package jwtclaims

import (
	"encoding/base64"
	"encoding/json"
	"testing"

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
