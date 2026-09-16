package clientauth_test

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/dev-idp/pkg/devidptest"
	"github.com/speakeasy-api/gram/server/internal/guardian"
	"github.com/speakeasy-api/gram/server/internal/oauthtest"
	"github.com/speakeasy-api/gram/server/internal/ratelimit"
	"github.com/speakeasy-api/gram/server/internal/testenv"
	"github.com/speakeasy-api/gram/server/internal/usersessions/clientauth"
	"github.com/speakeasy-api/gram/server/internal/usersessions/jwks"
	"github.com/speakeasy-api/gram/server/internal/usersessions/replay"
)

// The external subject the issuer vouches for.
const testExternalSubject = "repo:acme/payments-api:ref:refs/heads/main"

// launchWorkloadIssuer starts a dev-idp over HTTPS and discovers its jwks_uri
// before any test takes it offline.
func launchWorkloadIssuer(t *testing.T) (*devidptest.Instance, string) {
	t.Helper()

	issuer := devidptest.Launch(t, devidptest.LaunchOpts{EnableWorkOS: false, Key: nil, TLS: true})
	return issuer, oauthtest.DiscoverWorkloadJWKSURI(t, issuer)
}

// liveWorkloadExpectationFor is workloadExpectationFor with the key set
// resolved from the issuer's advertised jwks_uri.
func liveWorkloadExpectationFor(t *testing.T, issuer *devidptest.Instance, jwksURI string) clientauth.Expectation {
	t.Helper()

	source, err := jwks.NewRemoteSource(jwksURI)
	require.NoError(t, err, "build remote key source")

	return clientauth.WorkloadExpectation(
		issuer.OAuth21URL,
		testExternalSubject,
		source,
		// Spent identifiers are scoped by this endpoint, never by anything the
		// assertion carries.
		testIssuer,
		issuer.OAuth21URL,
		testExternalSubject,
		clientauth.Audiences{
			Issuer:   testIssuer,
			Endpoint: testTokenURL,
		},
		clientauth.DefaultMaxLifetime,
	)
}

// newWorkloadVerifier is newVerifier with a key resolver that trusts the
// issuer's certificate.
func newWorkloadVerifier(t *testing.T, issuer *devidptest.Instance) *clientauth.Verifier {
	t.Helper()

	client, err := infra.NewRedisClient(t, 0)
	require.NoError(t, err)

	logger := testenv.NewLogger(t)
	policy, err := guardian.NewUnsafePolicy(
		testenv.NewTracerProvider(t),
		[]string{},
		guardian.WithTLSRootCAs(issuer.RootCAs()),
	)
	require.NoError(t, err)

	limiter := ratelimit.New(
		ratelimit.NewRedisStore(client),
		string(testenv.NewCacheSuffix(t, "workload-jwks")),
		ratelimit.PerMinute(1000),
	)
	keys, err := jwks.NewKeyResolver(
		jwks.NewResolver(policy, testenv.NewMeterProvider(t), logger),
		jwks.NewMemoryCache(),
		limiter,
		nil,
		logger,
	)
	require.NoError(t, err)

	guard, err := replay.NewRedisGuard(client, string(testenv.NewCacheSuffix(t, "workload-replay")), clientauth.DefaultMaxReplayHold)
	require.NoError(t, err)

	verifier, err := clientauth.NewVerifier(keys, guard)
	require.NoError(t, err)
	return verifier
}

// An assertion verifies against a key set fetched over HTTPS from a real issuer.
func TestWorkloadIssuer_AssertionFromALiveIssuerVerifies(t *testing.T) {
	t.Parallel()

	issuer, jwksURI := launchWorkloadIssuer(t)
	assertion := oauthtest.MintWorkloadAssertion(t, issuer, oauthtest.WorkloadClaims(issuer, testExternalSubject, testIssuer))

	result, err := newWorkloadVerifier(t, issuer).Verify(t.Context(), assertionFor(assertion), liveWorkloadExpectationFor(t, issuer, jwksURI))

	require.NoError(t, err)
	require.NotNil(t, result)
}

// A key retired by rotation is refused once the resolver holds the new set.
//
// Each presentation uses a cold resolver cache. A warm cache keeps the stored
// set until its cache TTL expires, during which a retired key may still
// verify; the test uses cold resolvers to avoid depending on that timing.
//
// Assertions use distinct jti values, since the replay guard would otherwise
// refuse the second before its key is checked.
func TestWorkloadIssuer_RetiredKeyIsRejectedOnceTheCurrentSetIsHeld(t *testing.T) {
	t.Parallel()

	issuer, jwksURI := launchWorkloadIssuer(t)

	proof := oauthtest.MintWorkloadAssertion(t, issuer, oauthtest.WorkloadClaims(issuer, testExternalSubject, testIssuer))
	retired := oauthtest.MintWorkloadAssertion(t, issuer, oauthtest.WorkloadClaims(issuer, testExternalSubject, testIssuer))

	_, err := newWorkloadVerifier(t, issuer).Verify(t.Context(), assertionFor(proof), liveWorkloadExpectationFor(t, issuer, jwksURI))
	require.NoError(t, err, "the retiring key must work before the rotation, or the test proves nothing")

	issuer.RotateKey(t)

	// A resolver holding the current set finds no key for the retired kid.
	_, err = newWorkloadVerifier(t, issuer).Verify(t.Context(), assertionFor(retired), liveWorkloadExpectationFor(t, issuer, jwksURI))
	requireRejected(t, err, clientauth.ReasonKeyUnknown)

	// The issuer itself is still healthy.
	current := oauthtest.MintWorkloadAssertion(t, issuer, oauthtest.WorkloadClaims(issuer, testExternalSubject, testIssuer))
	_, err = newWorkloadVerifier(t, issuer).Verify(t.Context(), assertionFor(current), liveWorkloadExpectationFor(t, issuer, jwksURI))
	require.NoError(t, err, "the republished key must verify against a resolver that fetched it")
}

// An unreachable key set is refused even though the assertion is well-formed.
func TestWorkloadIssuer_UnreachableKeySetIsRefused(t *testing.T) {
	t.Parallel()

	issuer, jwksURI := launchWorkloadIssuer(t)
	// Minted while the issuer is up, so only the key set is missing.
	assertion := oauthtest.MintWorkloadAssertion(t, issuer, oauthtest.WorkloadClaims(issuer, testExternalSubject, testIssuer))

	issuer.Stop()

	_, err := newWorkloadVerifier(t, issuer).Verify(t.Context(), assertionFor(assertion), liveWorkloadExpectationFor(t, issuer, jwksURI))

	requireRejected(t, err, clientauth.ReasonKeyUnresolvable)
}

// A valid assertion from a trusted issuer for another subject fails to match
// the expected subject.
func TestWorkloadIssuer_SubjectOtherThanTheAdmittedOneIsRejected(t *testing.T) {
	t.Parallel()

	issuer, jwksURI := launchWorkloadIssuer(t)
	// Another workload on the same issuer.
	assertion := oauthtest.MintWorkloadAssertion(t, issuer, oauthtest.WorkloadClaims(issuer, "repo:someone-else/their-api:ref:refs/heads/main", testIssuer))

	_, err := newWorkloadVerifier(t, issuer).Verify(t.Context(), assertionFor(assertion), liveWorkloadExpectationFor(t, issuer, jwksURI))

	requireRejected(t, err, clientauth.ReasonSubjectMismatch)
}

// An assertion addressed to another audience is refused.
func TestWorkloadIssuer_AudienceForAnotherServerIsRejected(t *testing.T) {
	t.Parallel()

	issuer, jwksURI := launchWorkloadIssuer(t)
	assertion := oauthtest.MintWorkloadAssertion(t, issuer, oauthtest.WorkloadClaims(issuer, testExternalSubject, "https://gram.example.com/mcp/someone-else"))

	_, err := newWorkloadVerifier(t, issuer).Verify(t.Context(), assertionFor(assertion), liveWorkloadExpectationFor(t, issuer, jwksURI))

	requireRejected(t, err, clientauth.ReasonAudienceMismatch)
}

// A replayed assertion is refused even though it still verifies.
func TestWorkloadIssuer_ReplayedAssertionIsRejected(t *testing.T) {
	t.Parallel()

	issuer, jwksURI := launchWorkloadIssuer(t)
	expectation := liveWorkloadExpectationFor(t, issuer, jwksURI)
	verifier := newWorkloadVerifier(t, issuer)

	assertion := assertionFor(oauthtest.MintWorkloadAssertion(t, issuer, oauthtest.WorkloadClaims(issuer, testExternalSubject, testIssuer)))

	_, err := verifier.Verify(t.Context(), assertion, expectation)
	require.NoError(t, err)

	_, err = verifier.Verify(t.Context(), assertion, expectation)
	requireRejected(t, err, clientauth.ReasonReplayed)
}
