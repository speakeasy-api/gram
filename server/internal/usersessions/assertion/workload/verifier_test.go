package workload

import (
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"errors"
	"testing"
	"time"

	"github.com/go-jose/go-jose/v4"
	"github.com/go-jose/go-jose/v4/jwt"
	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	assertioncore "github.com/speakeasy-api/gram/server/internal/usersessions/assertion"
	"github.com/speakeasy-api/gram/server/internal/usersessions/jwks"
	"github.com/speakeasy-api/gram/server/internal/usersessions/replay"
)

const (
	testIssuer   = "https://platform.example.com"
	testSubject  = "repo:example/deploy:environment:prod"
	testAudience = "https://gram.example.com/mcp/demo"
)

type testKeys struct {
	key *jose.JSONWebKey
}

func (k *testKeys) VerificationKeyForAlgorithm(_ context.Context, _ jwks.Source, _ string, _ jose.SignatureAlgorithm) (*jose.JSONWebKey, error) {
	return k.key, nil
}

type testGuard struct {
	seen    map[replay.Key]bool
	maxHold time.Duration
	err     error
}

func (g *testGuard) MaxHold() time.Duration { return g.maxHold }

func (g *testGuard) Reserve(_ context.Context, key replay.Key, _ time.Time) (bool, error) {
	if g.err != nil {
		return false, g.err
	}
	if g.seen[key] {
		return false, nil
	}
	g.seen[key] = true
	return true, nil
}

type testSigner struct {
	signer jose.Signer
	key    *jose.JSONWebKey
}

func newTestSigner(t *testing.T) testSigner {
	t.Helper()
	private, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	require.NoError(t, err)
	signer, err := jose.NewSigner(jose.SigningKey{Algorithm: jose.ES256, Key: private},
		(&jose.SignerOptions{}).WithType("JWT").WithHeader(jose.HeaderKey("kid"), "key-1"))
	require.NoError(t, err)
	return testSigner{signer: signer, key: &jose.JSONWebKey{Key: private.Public(), KeyID: "key-1", Algorithm: string(jose.ES256), Use: "sig"}}
}

func testClaims() jwt.Claims {
	now := time.Now()
	return jwt.Claims{
		Issuer: testIssuer, Subject: testSubject, Audience: jwt.Audience{testAudience},
		Expiry: jwt.NewNumericDate(now.Add(2 * time.Minute)), NotBefore: jwt.NewNumericDate(now.Add(-time.Minute)),
		IssuedAt: jwt.NewNumericDate(now.Add(-time.Minute)), ID: "",
	}
}

func (s testSigner) sign(t *testing.T, claims jwt.Claims, extra any) string {
	t.Helper()
	builder := jwt.Signed(s.signer).Claims(claims)
	if extra != nil {
		builder = builder.Claims(extra)
	}
	raw, err := builder.Serialize()
	require.NoError(t, err)
	return raw
}

func newTestVerifier(t *testing.T, signer testSigner) (*Verifier, *testGuard, Expectation) {
	t.Helper()
	source, err := jwks.NewRemoteSource("https://platform.example.com/jwks")
	require.NoError(t, err)
	guard := &testGuard{seen: make(map[replay.Key]bool), maxHold: assertioncore.ReplayHoldFor(4 * time.Hour), err: nil}
	verifier, err := NewVerifier(&testKeys{key: signer.key}, guard)
	require.NoError(t, err)
	expect := Expectation{
		Issuer: testIssuer, Subject: testSubject, KeySource: source,
		ReplayIssuer: "user-session-issuer-1", ReplayParty: "workload-issuer-1",
		Audiences: []string{testAudience}, MaxLifetime: 4 * time.Hour,
	}
	return verifier, guard, expect
}

func TestVerifyCachedPlatformTokenCanBeReusedByDigest(t *testing.T) {
	t.Parallel()
	signer := newTestSigner(t)
	verifier, _, expect := newTestVerifier(t, signer)
	raw := signer.sign(t, testClaims(), nil)
	first, err := verifier.Verify(t.Context(), raw, expect)
	require.NoError(t, err)
	require.False(t, first.ReusedAssertion)
	second, err := verifier.Verify(t.Context(), raw, expect)
	require.NoError(t, err)
	require.True(t, second.ReusedAssertion)
}

func TestVerifyUTIIsSingleUse(t *testing.T) {
	t.Parallel()
	signer := newTestSigner(t)
	verifier, _, expect := newTestVerifier(t, signer)
	raw := signer.sign(t, testClaims(), map[string]any{"uti": uuid.NewString()})
	_, err := verifier.Verify(t.Context(), raw, expect)
	require.NoError(t, err)
	_, err = verifier.Verify(t.Context(), raw, expect)
	require.Equal(t, ReasonReplayed, ReasonOf(err))
}

func TestVerifyJTIPrecedesUTI(t *testing.T) {
	t.Parallel()
	signer := newTestSigner(t)
	verifier, _, expect := newTestVerifier(t, signer)
	claims := testClaims()
	claims.ID = uuid.NewString()
	first := signer.sign(t, claims, map[string]any{"uti": "first"})
	_, err := verifier.Verify(t.Context(), first, expect)
	require.NoError(t, err)
	second := signer.sign(t, claims, map[string]any{"uti": "second"})
	_, err = verifier.Verify(t.Context(), second, expect)
	require.Equal(t, ReasonReplayed, ReasonOf(err))
}

func TestVerifyUnreadableUTIFallsBackToDigest(t *testing.T) {
	t.Parallel()
	signer := newTestSigner(t)
	verifier, _, expect := newTestVerifier(t, signer)
	raw := signer.sign(t, testClaims(), map[string]any{"uti": []string{"unexpected"}})
	_, err := verifier.Verify(t.Context(), raw, expect)
	require.NoError(t, err)
	result, err := verifier.Verify(t.Context(), raw, expect)
	require.NoError(t, err)
	require.True(t, result.ReusedAssertion)
}

func TestVerifyIssuerAndSubjectAreIndependent(t *testing.T) {
	t.Parallel()
	signer := newTestSigner(t)
	verifier, _, expect := newTestVerifier(t, signer)
	claims := testClaims()
	claims.Subject = "repo:other/deploy"
	_, err := verifier.Verify(t.Context(), signer.sign(t, claims, nil), expect)
	require.Equal(t, ReasonSubjectMismatch, ReasonOf(err))
	claims = testClaims()
	claims.Issuer = "https://other.example.com"
	_, err = verifier.Verify(t.Context(), signer.sign(t, claims, nil), expect)
	require.Equal(t, ReasonSubjectMismatch, ReasonOf(err))
}

func TestVerifyReplayIdentifierIsScopedToWorkloadSubject(t *testing.T) {
	t.Parallel()
	signer := newTestSigner(t)
	verifier, _, expect := newTestVerifier(t, signer)
	first := testClaims()
	first.ID = uuid.NewString()
	_, err := verifier.Verify(t.Context(), signer.sign(t, first, nil), expect)
	require.NoError(t, err)

	second := first
	second.Subject = "repo:example/other:environment:prod"
	expect.Subject = second.Subject
	_, err = verifier.Verify(t.Context(), signer.sign(t, second, nil), expect)
	require.NoError(t, err, "one admitted workload must not spend another's identifier")
}

func TestVerifyRejectsAnotherAuthorizationServerAudience(t *testing.T) {
	t.Parallel()
	signer := newTestSigner(t)
	verifier, _, expect := newTestVerifier(t, signer)
	claims := testClaims()
	claims.Audience = jwt.Audience{"https://gram.example.com/mcp/other"}
	_, err := verifier.Verify(t.Context(), signer.sign(t, claims, nil), expect)
	require.Equal(t, ReasonAudienceMismatch, ReasonOf(err))
}

func TestVerifyWorkloadLifetimeIsIndependentOfClientCeiling(t *testing.T) {
	t.Parallel()
	signer := newTestSigner(t)
	verifier, guard, expect := newTestVerifier(t, signer)
	claims := testClaims()
	claims.Expiry = jwt.NewNumericDate(time.Now().Add(3 * time.Hour))
	raw := signer.sign(t, claims, nil)
	_, err := verifier.Verify(t.Context(), raw, expect)
	require.NoError(t, err)
	guard.maxHold = assertioncore.ReplayHoldFor(time.Hour)
	_, err = verifier.Verify(t.Context(), raw, expect)
	require.Equal(t, ReasonVerifierMisconfigured, ReasonOf(err))
}

func TestVerifyReplayStoreFailureIsClosed(t *testing.T) {
	t.Parallel()
	signer := newTestSigner(t)
	verifier, guard, expect := newTestVerifier(t, signer)
	guard.err = errors.New("store unavailable")
	_, err := verifier.Verify(t.Context(), signer.sign(t, testClaims(), nil), expect)
	require.Equal(t, ReasonReplayStoreUnavailable, ReasonOf(err))
}
