package workload

import (
	"crypto/rand"
	"crypto/rsa"
	"testing"
	"time"

	"github.com/go-jose/go-jose/v4"
	"github.com/go-jose/go-jose/v4/jwt"
	"github.com/stretchr/testify/require"
)

// This is a locally signed fixture of Google's documented service-account
// ID-token shape, not a live Google token or an end-to-end metadata test.
func TestVerifyGoogleServiceAccountIDTokenShape(t *testing.T) {
	t.Parallel()
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	require.NoError(t, err)
	signer, err := jose.NewSigner(jose.SigningKey{Algorithm: jose.RS256, Key: key}, (&jose.SignerOptions{}).WithType("JWT").WithHeader(jose.HeaderKey("kid"), "key-1"))
	require.NoError(t, err)
	fixture := testSigner{signer: signer, key: &jose.JSONWebKey{Key: key.Public(), KeyID: "key-1", Algorithm: string(jose.RS256), Use: "sig"}}
	verifier, _, _, expect := newTestVerifier(t, fixture)
	expect.Issuer = "https://accounts.google.com"
	expect.Subject = "100000000000000000001"
	expect.MaxLifetime = time.Hour
	now := time.Now()
	claims := jwt.Claims{Issuer: expect.Issuer, Subject: expect.Subject, Audience: jwt.Audience{testAudience}, IssuedAt: jwt.NewNumericDate(now), Expiry: jwt.NewNumericDate(now.Add(time.Hour))}
	raw := fixture.sign(t, claims, map[string]any{"azp": expect.Subject, "email": "runtime@example.iam.gserviceaccount.com", "email_verified": true})
	result, err := verifier.Verify(t.Context(), raw, expect)
	require.NoError(t, err)
	require.False(t, result.ReusedAssertion)
	result, err = verifier.Verify(t.Context(), raw, expect)
	require.NoError(t, err)
	require.True(t, result.ReusedAssertion)

	// A Google IAM service-account subject must not silently substitute for
	// the existing Kubernetes per-runtime subject.
	expect.Subject = "system:serviceaccount:runtime-test:kit-runtime"
	_, err = verifier.Verify(t.Context(), raw, expect)
	require.Error(t, err)
}
