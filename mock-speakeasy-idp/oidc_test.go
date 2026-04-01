package mockidp

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestIsOidcMode(t *testing.T) {
	tests := []struct {
		name   string
		cfg    OidcConfig
		expect bool
	}{
		{"all set", OidcConfig{Issuer: "https://x", ClientID: "id", ClientSecret: "secret"}, true},
		{"missing issuer", OidcConfig{ClientID: "id", ClientSecret: "secret"}, false},
		{"missing client id", OidcConfig{Issuer: "https://x", ClientSecret: "secret"}, false},
		{"missing client secret", OidcConfig{Issuer: "https://x", ClientID: "id"}, false},
		{"all empty", OidcConfig{}, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.expect, tt.cfg.IsOidcMode())
		})
	}
}

func TestComputeCodeChallenge(t *testing.T) {
	// PKCE S256: BASE64URL(SHA256(verifier))
	// A given verifier should always produce the same challenge.
	verifier := "test-verifier-12345"
	c1 := computeCodeChallenge(verifier)
	c2 := computeCodeChallenge(verifier)
	assert.Equal(t, c1, c2, "same verifier should produce same challenge")
	assert.NotEmpty(t, c1)

	// Different verifier should produce a different challenge.
	c3 := computeCodeChallenge("different-verifier")
	assert.NotEqual(t, c1, c3)
}

func TestGenerateCodeVerifier(t *testing.T) {
	v1 := generateCodeVerifier()
	v2 := generateCodeVerifier()
	assert.NotEmpty(t, v1)
	assert.NotEqual(t, v1, v2, "verifiers should be unique")
	// PKCE verifiers should be base64url-encoded (no +, /, or =)
	assert.NotContains(t, v1, "+")
	assert.NotContains(t, v1, "/")
	assert.NotContains(t, v1, "=")
}

func TestBuildAuthorizeURL(t *testing.T) {
	cfg := OidcConfig{
		Issuer:       "https://test.authkit.app/",
		ClientID:     "client_abc",
		ClientSecret: "sk_test",
		ExternalURL:  "http://localhost:35291",
	}
	u, err := buildAuthorizeURL(context.Background(), cfg, "state123", "verifier456")
	require.NoError(t, err)
	assert.Contains(t, u, "https://api.workos.com/user_management/authorize?")
	assert.Contains(t, u, "client_id=client_abc")
	assert.Contains(t, u, "state=state123")
	assert.Contains(t, u, "provider=authkit")
	assert.Contains(t, u, "response_type=code")
	assert.Contains(t, u, "code_challenge_method=S256")
	assert.Contains(t, u, "redirect_uri=http")
}

func TestExtractJWTClaim(t *testing.T) {
	// Build a minimal JWT with a known payload.
	// JWT = base64url(header).base64url(payload).base64url(signature)
	// We only need a valid 3-part structure with decodable payload.
	header := "eyJhbGciOiJSUzI1NiIsInR5cCI6IkpXVCJ9"             // {"alg":"RS256","typ":"JWT"}
	payload := "eyJzdWIiOiJ1c2VyMSIsInNpZCI6InNlc3NfYWJjMTIzIn0" // {"sub":"user1","sid":"sess_abc123"}
	sig := "fakesignature"
	jwt := header + "." + payload + "." + sig

	t.Run("extract existing claim", func(t *testing.T) {
		val, err := extractJWTClaim(jwt, "sid")
		require.NoError(t, err)
		assert.Equal(t, "sess_abc123", val)
	})

	t.Run("extract sub claim", func(t *testing.T) {
		val, err := extractJWTClaim(jwt, "sub")
		require.NoError(t, err)
		assert.Equal(t, "user1", val)
	})

	t.Run("missing claim", func(t *testing.T) {
		_, err := extractJWTClaim(jwt, "nonexistent")
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "not found")
	})

	t.Run("invalid JWT format", func(t *testing.T) {
		_, err := extractJWTClaim("not-a-jwt", "sid")
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "invalid JWT")
	})

	t.Run("invalid base64 payload", func(t *testing.T) {
		_, err := extractJWTClaim("a.!!!invalid.b", "sid")
		assert.Error(t, err)
	})
}
