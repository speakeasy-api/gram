package remotesessions

import (
	"crypto/rand"
	"crypto/rsa"
	"encoding/json"
	"testing"
	"time"

	"github.com/go-jose/go-jose/v4"
	"github.com/go-jose/go-jose/v4/jwt"
	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/guardian"
	"github.com/speakeasy-api/gram/server/internal/ratelimit"
	"github.com/speakeasy-api/gram/server/internal/testenv"
	"github.com/speakeasy-api/gram/server/internal/usersessions/jwks"
)

const (
	rsaKeyPolicyIssuer   = "https://issuer.example.com"
	rsaKeyPolicyJWKSURI  = rsaKeyPolicyIssuer + "/keys"
	rsaKeyPolicyClientID = "oauth-client"
)

// newRSAKeyPolicyFixture publishes one RSA key of the given size under a warm key cache.
func newRSAKeyPolicyFixture(t *testing.T, bits int) (*rsa.PrivateKey, *jwks.KeyResolver, *guardian.Policy) {
	t.Helper()
	key, err := rsa.GenerateKey(rand.Reader, bits)
	require.NoError(t, err)
	document, err := json.Marshal(jose.JSONWebKeySet{Keys: []jose.JSONWebKey{{Key: &key.PublicKey, KeyID: "example-key", Algorithm: "RS256", Use: "sig"}}})
	require.NoError(t, err)
	logger := testenv.NewLogger(t)
	policy := guardian.NewDefaultPolicy(testenv.NewTracerProvider(t))
	cache := jwks.NewMemoryCache()
	now := time.Now()
	require.NoError(t, cache.Put(t.Context(), rsaKeyPolicyJWKSURI, jwks.CacheState{Document: document, RefreshedAt: now, ExpiresAt: now.Add(time.Hour)}))
	keys, err := jwks.NewKeyResolver(jwks.NewResolver(policy, testenv.NewMeterProvider(t), logger), cache, ratelimit.New(nil, "access-key-policy-test", ratelimit.PerMinute(1)), nil, logger)
	require.NoError(t, err)
	return key, keys, policy
}

func mintRSAAccessToken(t *testing.T, key *rsa.PrivateKey, alg jose.SignatureAlgorithm, typ jose.ContentType) string {
	t.Helper()
	now := time.Now()
	claims := map[string]any{
		"iss": rsaKeyPolicyIssuer, "sub": "example-subject", "aud": rsaKeyPolicyClientID,
		"exp": now.Add(time.Hour).Unix(), "iat": now.Unix(), "jti": "example-token",
		"client_id": rsaKeyPolicyClientID,
	}
	signer, err := jose.NewSigner(jose.SigningKey{Algorithm: alg, Key: key}, (&jose.SignerOptions{}).WithType(typ).WithHeader("kid", "example-key"))
	require.NoError(t, err)
	raw, err := jwt.Signed(signer).Claims(claims).Serialize()
	require.NoError(t, err)
	return raw
}

func TestAccessTokenKeyPolicyDoesNotChangeIDTokenVerification(t *testing.T) {
	t.Parallel()
	key, keys, policy := newRSAKeyPolicyFixture(t, 1024)
	_, err := NewIDTokenVerifier(keys).Verify(t.Context(), mintRSAAccessToken(t, key, jose.RS256, "JWT"), IDTokenExpectation{
		issuer: rsaKeyPolicyIssuer, clientID: rsaKeyPolicyClientID, jwksURI: rsaKeyPolicyJWKSURI, fetchScope: "example-issuer",
		signingAlgs: []string{"RS256"}, nonce: "", subject: "",
	})
	require.NoError(t, err, "the access-token key policy must not change established ID-token verification")

	enricher := NewSessionEnricher(testenv.NewLogger(t), nil, policy, keys, nil)
	result := enricher.jwtAccessToken(t.Context(), enrichmentTarget{
		issuerID: uuid.New(), issuerURL: rsaKeyPolicyIssuer, jwksURI: rsaKeyPolicyJWKSURI, externalClientID: rsaKeyPolicyClientID,
	}, mintRSAAccessToken(t, key, jose.RS256, "at+jwt"))
	require.True(t, result.ran)
	require.Equal(t, interfaceStatusFailed, result.Status)
	require.Equal(t, "unverifiable token", result.Reason)
	require.Nil(t, result.identity, "RFC 7518 §3.3 requires at least 2048 bits for the access-token path")
}

func TestAccessTokenRSAAlgorithmsVerify(t *testing.T) {
	t.Parallel()
	key, keys, policy := newRSAKeyPolicyFixture(t, 2048)
	enricher := NewSessionEnricher(testenv.NewLogger(t), nil, policy, keys, nil)
	target := enrichmentTarget{issuerID: uuid.New(), issuerURL: rsaKeyPolicyIssuer, jwksURI: rsaKeyPolicyJWKSURI, externalClientID: rsaKeyPolicyClientID}
	result := enricher.jwtAccessToken(t.Context(), target, mintRSAAccessToken(t, key, jose.RS256, "at+jwt"))
	require.Equal(t, interfaceStatusOK, result.Status, result.Reason)
	require.Equal(t, "example-subject", result.identity.Subject)
}
