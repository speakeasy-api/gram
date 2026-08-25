package clientauth_test

import (
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/rsa"
	"encoding/json"
	"log"
	"os"
	"testing"
	"time"

	"github.com/go-jose/go-jose/v4"
	"github.com/go-jose/go-jose/v4/jwt"
	"github.com/google/uuid"
	"github.com/redis/go-redis/v9"
	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/guardian"
	"github.com/speakeasy-api/gram/server/internal/ratelimit"
	"github.com/speakeasy-api/gram/server/internal/testenv"
	"github.com/speakeasy-api/gram/server/internal/usersessions/clientauth"
	"github.com/speakeasy-api/gram/server/internal/usersessions/jwks"
	"github.com/speakeasy-api/gram/server/internal/usersessions/replay"
)

const (
	testClientID = "https://client.example.com/oauth/client.json"
	testIssuer   = "https://gram.example.com/mcp/demo"
	testTokenURL = "https://gram.example.com/mcp/demo/token"
	testKeyID    = "test-key-1"
)

var infra *testenv.Environment

func TestMain(m *testing.M) {
	res, cleanup, err := testenv.Launch(context.Background(), testenv.LaunchOptions{Redis: true})
	if err != nil {
		log.Fatalf("launch test infrastructure: %v", err)
	}

	infra = res
	code := m.Run()

	if err := cleanup(); err != nil {
		log.Fatalf("cleanup test infrastructure: %v", err)
	}

	os.Exit(code)
}

// signer holds a client's key pair: the private half to mint assertions with
// and the public half published in its key set.
type signer struct {
	jwks  json.RawMessage
	inner jose.Signer
}

// newSigner generates an ES256 key pair and the single-key JWK Set that
// publishes its public half. ECDSA over RSA purely for generation speed.
func newSigner(t *testing.T, kid string) *signer {
	t.Helper()

	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	require.NoError(t, err)

	set := jose.JSONWebKeySet{Keys: []jose.JSONWebKey{{
		Key:       key.Public(),
		KeyID:     kid,
		Algorithm: string(jose.ES256),
		Use:       "sig",
	}}}
	body, err := json.Marshal(set)
	require.NoError(t, err)

	inner, err := jose.NewSigner(
		jose.SigningKey{Algorithm: jose.ES256, Key: key},
		(&jose.SignerOptions{}).WithType("JWT").WithHeader(jose.HeaderKey("kid"), kid),
	)
	require.NoError(t, err)

	return &signer{jwks: body, inner: inner}
}

// newRSASigner is newSigner with an RSA key pair, signing with alg (RS256 or
// PS256). Slower to generate than ECDSA, so used only where the algorithm
// family is the point.
func newRSASigner(t *testing.T, kid string, alg jose.SignatureAlgorithm) *signer {
	t.Helper()

	key, err := rsa.GenerateKey(rand.Reader, 2048)
	require.NoError(t, err)

	set := jose.JSONWebKeySet{Keys: []jose.JSONWebKey{{
		Key:       key.Public(),
		KeyID:     kid,
		Algorithm: string(alg),
		Use:       "sig",
	}}}
	body, err := json.Marshal(set)
	require.NoError(t, err)

	inner, err := jose.NewSigner(
		jose.SigningKey{Algorithm: alg, Key: key},
		(&jose.SignerOptions{}).WithType("JWT").WithHeader(jose.HeaderKey("kid"), kid),
	)
	require.NoError(t, err)

	return &signer{jwks: body, inner: inner}
}

// source is the inline key source publishing this signer's public key.
func (s *signer) source(t *testing.T) jwks.Source {
	t.Helper()

	src, err := jwks.NewInlineSource(s.jwks)
	require.NoError(t, err)
	return src
}

// sign mints a compact JWS over claims.
func (s *signer) sign(t *testing.T, claims jwt.Claims) string {
	t.Helper()

	raw, err := jwt.Signed(s.inner).Claims(claims).Serialize()
	require.NoError(t, err)
	return raw
}

// validClaims is an assertion body that satisfies every rule, for tests to
// mutate one field of at a time.
func validClaims() jwt.Claims {
	now := time.Now()
	return jwt.Claims{
		Issuer:    testClientID,
		Subject:   testClientID,
		Audience:  jwt.Audience{testIssuer},
		Expiry:    jwt.NewNumericDate(now.Add(2 * time.Minute)),
		NotBefore: jwt.NewNumericDate(now),
		IssuedAt:  jwt.NewNumericDate(now),
		ID:        "jti-" + uuid.NewString(),
	}
}

// newKeyResolver builds a key resolver whose refresh budget is generous.
// Inline key sources never fetch, so the guardian policy is never exercised.
func newKeyResolver(t *testing.T, client *redis.Client) *jwks.KeyResolver {
	t.Helper()

	logger := testenv.NewLogger(t)
	policy, err := guardian.NewUnsafePolicy(testenv.NewTracerProvider(t), []string{})
	require.NoError(t, err)

	limiter := ratelimit.New(ratelimit.NewRedisStore(client), string(testenv.NewCacheSuffix(t, "clientauth-jwks")), ratelimit.PerMinute(1000))
	keys, err := jwks.NewKeyResolver(
		jwks.NewResolver(policy, testenv.NewMeterProvider(t), logger),
		jwks.NewMemoryCache(),
		limiter,
		nil,
		logger,
	)
	require.NoError(t, err)
	return keys
}

// newVerifier builds a Verifier over a real Redis-backed replay guard sized
// to the verifier's own hold requirement.
func newVerifier(t *testing.T) *clientauth.Verifier {
	t.Helper()

	client, err := infra.NewRedisClient(t, 0)
	require.NoError(t, err)

	guard, err := replay.NewRedisGuard(client, string(testenv.NewCacheSuffix(t, "clientauth-replay")), clientauth.MaxReplayHold)
	require.NoError(t, err)

	verifier, err := clientauth.NewVerifier(newKeyResolver(t, client), guard)
	require.NoError(t, err)
	return verifier
}

// expectationFor is the standard Expectation naming this signer's key source.
func expectationFor(t *testing.T, s *signer) clientauth.Expectation {
	t.Helper()

	return clientauth.Expectation{
		ClientID:     testClientID,
		KeySource:    s.source(t),
		ReplayIssuer: t.Name(),
		Audiences: clientauth.Audiences{
			Issuer:   testIssuer,
			Endpoint: testTokenURL,
		},
	}
}

// assertionFor wraps a raw assertion in a well-formed Assertion.
func assertionFor(assertion string) clientauth.Assertion {
	return clientauth.Assertion{Value: assertion, Type: clientauth.AssertionType}
}

// requireRejected asserts that verification failed for exactly the expected
// reason, so a test cannot pass because the assertion was refused for an
// unrelated one.
func requireRejected(t *testing.T, err error, want clientauth.Reason) {
	t.Helper()

	require.Error(t, err)
	require.Equal(t, want, clientauth.ReasonOf(err), "rejected for the wrong reason: %v", err)
}
