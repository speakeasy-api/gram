package clientauth_test

import (
	"testing"

	"github.com/stretchr/testify/require"

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

// workloadExpectationFor is the assertion shape a workload presents: iss is
// the platform that vouched for it and sub is the machine, where a client
// assertion requires both to be the client_id. The key set is resolved from
// the issuer over the network rather than handed over inline.
func workloadExpectationFor(t *testing.T, issuer *oauthtest.WorkloadIssuer) clientauth.Expectation {
	t.Helper()

	return clientauth.Expectation{
		Issuer:    issuer.URL,
		Subject:   testExternalSubject,
		KeySource: issuer.KeySource(t),
		// Spent identifiers are scoped by this endpoint, never by anything the
		// assertion carries.
		ReplayIssuer:  testIssuer,
		ReplayParty:   issuer.URL,
		ReplaySubject: testExternalSubject,
		Audiences: clientauth.Audiences{
			Issuer:   testIssuer,
			Endpoint: testTokenURL,
		},
		MaxLifetime: 0,
	}
}

// newWorkloadVerifier is newVerifier with a key resolver that trusts the
// issuer's certificate, because these assertions verify against a key set
// fetched over the network rather than one handed over inline.
func newWorkloadVerifier(t *testing.T, issuer *oauthtest.WorkloadIssuer) *clientauth.Verifier {
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
// its key set fetched over HTTP, and an assertion that verifies.
func TestWorkloadIssuer_AssertionFromALiveIssuerVerifies(t *testing.T) {
	t.Parallel()

	issuer := oauthtest.LaunchWorkloadIssuer(t)
	assertion := issuer.Mint(t, issuer.WorkloadClaims(testExternalSubject, testIssuer))

	result, err := newWorkloadVerifier(t, issuer).Verify(t.Context(), assertionFor(assertion), workloadExpectationFor(t, issuer))

	require.NoError(t, err)
	require.NotNil(t, result)
}

// Key rotation is the issuer behaviour most likely to break in production, and
// it cannot be posed against an inline key set: there is no server to rotate.
// Here the issuer really replaces its key and republishes it.
//
// Each presentation uses a resolver with a cold cache, which is what makes
// this deterministic — and the reason is worth stating, because the warm case
// surprised this test twice while it was being written.
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

	issuer := oauthtest.LaunchWorkloadIssuer(t)

	proof := issuer.Mint(t, issuer.WorkloadClaims(testExternalSubject, testIssuer))
	retired := issuer.Mint(t, issuer.WorkloadClaims(testExternalSubject, testIssuer))

	_, err := newWorkloadVerifier(t, issuer).Verify(t.Context(), assertionFor(proof), workloadExpectationFor(t, issuer))
	require.NoError(t, err, "the retiring key must work before the rotation, or the test proves nothing")

	issuer.Rotate(t)

	// A resolver holding the current set finds no key for the retired kid.
	_, err = newWorkloadVerifier(t, issuer).Verify(t.Context(), assertionFor(retired), workloadExpectationFor(t, issuer))
	requireRejected(t, err, clientauth.ReasonKeyUnknown)

	// And the issuer is still healthy: the rejection above is about the key
	// that was retired, not about a server the rotation broke.
	current := issuer.Mint(t, issuer.WorkloadClaims(testExternalSubject, testIssuer))
	_, err = newWorkloadVerifier(t, issuer).Verify(t.Context(), assertionFor(current), workloadExpectationFor(t, issuer))
	require.NoError(t, err, "the republished key must verify against a resolver that fetched it")
}

// An issuer that goes off the network is the availability coupling this
// feature accepts, and it has to be refused rather than admitted: the
// assertion is still perfectly well-formed, and only the key set is missing.
//
// Also unavailable against an inline source, which has nothing to take away.
func TestWorkloadIssuer_UnreachableKeySetIsRefused(t *testing.T) {
	t.Parallel()

	issuer := oauthtest.LaunchWorkloadIssuer(t)
	// Minted while the issuer is up, so the assertion itself is beyond
	// reproach when it is presented.
	assertion := issuer.Mint(t, issuer.WorkloadClaims(testExternalSubject, testIssuer))

	issuer.Stop()

	_, err := newWorkloadVerifier(t, issuer).Verify(t.Context(), assertionFor(assertion), workloadExpectationFor(t, issuer))

	requireRejected(t, err, clientauth.ReasonKeyUnresolvable)
}

// A genuine assertion from a trusted issuer, naming a workload nobody
// admitted. The verifier is the wrong layer to catch this by identity — it
// catches it because the expectation names the admitted subject, so an
// assertion for any other one fails to match.
func TestWorkloadIssuer_SubjectOtherThanTheAdmittedOneIsRejected(t *testing.T) {
	t.Parallel()

	issuer := oauthtest.LaunchWorkloadIssuer(t)
	// Somebody else's job on the same CI provider: correctly signed, genuinely
	// issued, and not ours.
	assertion := issuer.Mint(t, issuer.WorkloadClaims("repo:someone-else/their-api:ref:refs/heads/main", testIssuer))

	_, err := newWorkloadVerifier(t, issuer).Verify(t.Context(), assertionFor(assertion), workloadExpectationFor(t, issuer))

	requireRejected(t, err, clientauth.ReasonSubjectMismatch)
}

// The audience mismatch is the dominant rollout failure and is invisible from
// the client side, so it is worth pinning against a real issuer too.
func TestWorkloadIssuer_AudienceForAnotherServerIsRejected(t *testing.T) {
	t.Parallel()

	issuer := oauthtest.LaunchWorkloadIssuer(t)
	assertion := issuer.Mint(t, issuer.WorkloadClaims(testExternalSubject, "https://gram.example.com/mcp/someone-else"))

	_, err := newWorkloadVerifier(t, issuer).Verify(t.Context(), assertionFor(assertion), workloadExpectationFor(t, issuer))

	requireRejected(t, err, clientauth.ReasonAudienceMismatch)
}

// Replay protection has to hold across a real fetch too: the second
// presentation of one assertion is refused even though its signature still
// verifies and its key set is still reachable.
func TestWorkloadIssuer_ReplayedAssertionIsRejected(t *testing.T) {
	t.Parallel()

	issuer := oauthtest.LaunchWorkloadIssuer(t)
	expectation := workloadExpectationFor(t, issuer)
	verifier := newWorkloadVerifier(t, issuer)

	assertion := assertionFor(issuer.Mint(t, issuer.WorkloadClaims(testExternalSubject, testIssuer)))

	_, err := verifier.Verify(t.Context(), assertion, expectation)
	require.NoError(t, err)

	_, err = verifier.Verify(t.Context(), assertion, expectation)
	requireRejected(t, err, clientauth.ReasonReplayed)
}
