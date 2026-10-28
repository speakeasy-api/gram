package idjag_test

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"testing"

	"github.com/go-jose/go-jose/v4"
	"github.com/go-jose/go-jose/v4/jwt"
	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/usersessions/assertion/idjag"
)

func signUnverifiedTestAssertion(t *testing.T, extra map[string]any) string {
	t.Helper()
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	require.NoError(t, err)
	signer, err := jose.NewSigner(jose.SigningKey{Algorithm: jose.ES256, Key: key}, (&jose.SignerOptions{}).WithType(jose.ContentType(idjag.Type)))
	require.NoError(t, err)
	raw, err := jwt.Signed(signer).Claims(jwt.Claims{Issuer: "https://idp.example.test"}).Claims(extra).Serialize()
	require.NoError(t, err)
	return raw
}

func TestUnverifiedResourceReturnsClaim(t *testing.T) {
	t.Parallel()

	resource, ok, err := idjag.UnverifiedResource(signUnverifiedTestAssertion(t, map[string]any{"resource": "https://mcp.example.test/mcp/a"}))
	require.NoError(t, err)
	require.True(t, ok)
	require.Equal(t, "https://mcp.example.test/mcp/a", resource)
}

func TestUnverifiedResourceReportsAbsentClaim(t *testing.T) {
	t.Parallel()

	_, ok, err := idjag.UnverifiedResource(signUnverifiedTestAssertion(t, map[string]any{"client_id": "x"}))
	require.NoError(t, err)
	require.False(t, ok)
}

func TestUnverifiedResourceRejectsInvalidClaim(t *testing.T) {
	t.Parallel()

	for _, claim := range []any{"", []string{"https://mcp.example.test/mcp/a"}, 42, nil} {
		_, ok, err := idjag.UnverifiedResource(signUnverifiedTestAssertion(t, map[string]any{"resource": claim}))
		require.True(t, ok, claim)
		require.Equal(t, idjag.ReasonResourceMismatch, idjag.ReasonOf(err), claim)
	}
}

func TestUnverifiedResourceRejectsMalformedAssertion(t *testing.T) {
	t.Parallel()

	_, ok, err := idjag.UnverifiedResource("not.a.jwt")
	require.False(t, ok)
	require.Equal(t, idjag.ReasonMalformed, idjag.ReasonOf(err))
}
