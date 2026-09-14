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

// The subject a platform puts in a workload assertion: a declared, bounded
// resource rather than the ephemeral job.
const testExternalSubject = "repo:acme/payments-api:ref:refs/heads/main"

// launchWorkloadIssuer starts a dev-idp over HTTPS and discovers the key set
// it publishes. Discovery runs before anything is presented, so a test that
// takes the issuer offline still holds a jwks_uri to fail to reach.
func launchWorkloadIssuer(t *testing.T) (*devidptest.Instance, string) {
	t.Helper()

	issuer := devidptest.Launch(t, devidptest.LaunchOpts{EnableWorkOS: false, Key: nil, TLS: true})
	return issuer, oauthtest.DiscoverWorkloadJWKSURI(t, issuer)
}

// liveWorkloadExpectationFor is workloadExpectationFor against a live issuer:
// iss is the platform that vouched for the workload and sub is the machine,
// and the key set is resolved from the jwks_uri the issuer advertises rather
// than handed over inline.
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
// issuer's certificate, because these assertions verify against a key set
// fetched over the network rather than one handed over inline.
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

// The baseline the negative cases below are departures from: a real issuer,
// its key set fetched over HTTPS from the jwks_uri it advertises, and an
// assertion that verifies.
func TestWorkloadIssuer_AssertionFromALiveIssuerVerifies(t *testing.T) {
	t.Parallel()

	issuer, jwksURI := launchWorkloadIssuer(t)
	assertion := oauthtest.MintWorkloadAssertion(t, issuer, oauthtest.WorkloadClaims(issuer, testExternalSubject, testIssuer))

	result, err := newWorkloadVerifier(t, issuer).Verify(t.Context(), assertionFor(assertion), liveWorkloadExpectationFor(t, issuer, jwksURI))

	require.NoError(t, err)
	require.NotNil(t, result)
}

// Key rotation is the issuer behaviour most likely to break in production, and
// it cannot be posed against an inline key set: there is no server to rotate.
// Here the issuer really replaces its key and republishes it at the same URL.
//
// Each presentation uses a resolver with a cold cache, which is what makes
// this deterministic. A warm cache behaves differently, for a reason that is
// easy to miss:
//
// jwks holds a 30s refreshCooldown after any successful consult: a forced
// refresh inside that window re-selects from the stored set rather than
// spending refresh budget, which is what makes probing with random kids free
// after the first refresh. So on a replica that has already fetched, for up to
// the cooldown, a retired key still verifies and a just-published one is
// refused as unknown. Both are the documented cost of that design rather than
// defects, and neither is asserted here: pinning them would be pinning a
// 30-second timing window.
//
// The assertions below are minted with distinct jti values on purpose. The
// replay guard is Redis-backed and scoped per test, not per verifier, so
// presenting one assertion twice is refused as a replay before the key is
// looked at — which would pass this test for entirely the wrong reason.
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

	// And the issuer is still healthy: the rejection above is about the key
	// that was retired, not about a server the rotation broke.
	current := oauthtest.MintWorkloadAssertion(t, issuer, oauthtest.WorkloadClaims(issuer, testExternalSubject, testIssuer))
	_, err = newWorkloadVerifier(t, issuer).Verify(t.Context(), assertionFor(current), liveWorkloadExpectationFor(t, issuer, jwksURI))
	require.NoError(t, err, "the republished key must verify against a resolver that fetched it")
}

// An issuer that goes off the network is the availability coupling this
// feature accepts, and it has to be refused rather than admitted: the
// assertion is still perfectly well-formed, and only the key set is missing.
//
// Also unavailable against an inline source, which has nothing to take away.
func TestWorkloadIssuer_UnreachableKeySetIsRefused(t *testing.T) {
	t.Parallel()

	issuer, jwksURI := launchWorkloadIssuer(t)
	// Minted while the issuer is up, so the assertion itself is beyond
	// reproach when it is presented.
	assertion := oauthtest.MintWorkloadAssertion(t, issuer, oauthtest.WorkloadClaims(issuer, testExternalSubject, testIssuer))

	issuer.Stop()

	_, err := newWorkloadVerifier(t, issuer).Verify(t.Context(), assertionFor(assertion), liveWorkloadExpectationFor(t, issuer, jwksURI))

	requireRejected(t, err, clientauth.ReasonKeyUnresolvable)
}

// A genuine assertion from a trusted issuer, naming a workload nobody
// admitted. The verifier is the wrong layer to catch this by identity — it
// catches it because the expectation names the admitted subject, so an
// assertion for any other one fails to match.
func TestWorkloadIssuer_SubjectOtherThanTheAdmittedOneIsRejected(t *testing.T) {
	t.Parallel()

	issuer, jwksURI := launchWorkloadIssuer(t)
	// Somebody else's job on the same CI provider: correctly signed, genuinely
	// issued, and not ours.
	assertion := oauthtest.MintWorkloadAssertion(t, issuer, oauthtest.WorkloadClaims(issuer, "repo:someone-else/their-api:ref:refs/heads/main", testIssuer))

	_, err := newWorkloadVerifier(t, issuer).Verify(t.Context(), assertionFor(assertion), liveWorkloadExpectationFor(t, issuer, jwksURI))

	requireRejected(t, err, clientauth.ReasonSubjectMismatch)
}

// The audience mismatch is the dominant rollout failure and is invisible from
// the client side, so it is worth pinning against a real issuer too.
func TestWorkloadIssuer_AudienceForAnotherServerIsRejected(t *testing.T) {
	t.Parallel()

	issuer, jwksURI := launchWorkloadIssuer(t)
	assertion := oauthtest.MintWorkloadAssertion(t, issuer, oauthtest.WorkloadClaims(issuer, testExternalSubject, "https://gram.example.com/mcp/someone-else"))

	_, err := newWorkloadVerifier(t, issuer).Verify(t.Context(), assertionFor(assertion), liveWorkloadExpectationFor(t, issuer, jwksURI))

	requireRejected(t, err, clientauth.ReasonAudienceMismatch)
}

// Replay protection has to hold across a real fetch too: the second
// presentation of one assertion is refused even though its signature still
// verifies and its key set is still reachable.
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
