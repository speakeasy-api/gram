package workload_test

import (
	"context"
	"log"
	"os"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/dev-idp/pkg/devidptest"
	"github.com/speakeasy-api/gram/server/internal/guardian"
	"github.com/speakeasy-api/gram/server/internal/oauthtest"
	"github.com/speakeasy-api/gram/server/internal/ratelimit"
	"github.com/speakeasy-api/gram/server/internal/testenv"
	assertioncore "github.com/speakeasy-api/gram/server/internal/usersessions/assertion"
	"github.com/speakeasy-api/gram/server/internal/usersessions/assertion/workload"
	"github.com/speakeasy-api/gram/server/internal/usersessions/jwks"
	"github.com/speakeasy-api/gram/server/internal/usersessions/replay"
)

const (
	// The external subject the issuer vouches for.
	testExternalSubject = "repo:acme/payments-api:ref:refs/heads/main"

	// The authorization server the assertions address.
	testAudience = "https://gram.example.com/mcp/demo"

	testMaxLifetime = time.Hour
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

// launchWorkloadIssuer starts a dev-idp over HTTPS and discovers its jwks_uri
// before any test takes it offline.
func launchWorkloadIssuer(t *testing.T) (*devidptest.Instance, string) {
	t.Helper()

	issuer := devidptest.Launch(t, devidptest.LaunchOpts{EnableWorkOS: false, Key: nil, TLS: true})
	return issuer, oauthtest.DiscoverWorkloadJWKSURI(t, issuer)
}

// liveExpectationFor binds the admitted subject to the key set resolved from
// the issuer's advertised jwks_uri.
func liveExpectationFor(t *testing.T, issuer *devidptest.Instance, jwksURI string) workload.Expectation {
	t.Helper()

	source, err := jwks.NewRemoteSource(jwksURI)
	require.NoError(t, err, "build remote key source")

	return workload.Expectation{
		Issuer:    issuer.OAuth21URL,
		Subject:   testExternalSubject,
		KeySource: source,
		// Spent identifiers are scoped by this endpoint, never by anything the
		// assertion carries.
		ReplayIssuer: testAudience,
		ReplayParty:  issuer.OAuth21URL,
		Audiences:    []string{testAudience, testAudience + "/token"},
		MaxLifetime:  testMaxLifetime,
	}
}

// newLiveVerifier builds a Verifier whose key resolver trusts the issuer's
// certificate, over a real Redis-backed replay guard.
func newLiveVerifier(t *testing.T, issuer *devidptest.Instance) *workload.Verifier {
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

	guard, err := replay.NewRedisGuard(client, string(testenv.NewCacheSuffix(t, "workload-replay")), assertioncore.ReplayHoldFor(testMaxLifetime))
	require.NoError(t, err)

	verifier, err := workload.NewVerifier(keys, guard)
	require.NoError(t, err)
	return verifier
}

// requireRejected asserts that verification failed for exactly the expected
// reason, so a test cannot pass because the assertion was refused for an
// unrelated one.
func requireRejected(t *testing.T, err error, want workload.Reason) {
	t.Helper()

	require.Error(t, err)
	require.Equal(t, want, workload.ReasonOf(err), "rejected for the wrong reason: %v", err)
}

// An assertion verifies against a key set fetched over HTTPS from a real issuer.
func TestWorkloadIssuer_AssertionFromALiveIssuerVerifies(t *testing.T) {
	t.Parallel()

	issuer, jwksURI := launchWorkloadIssuer(t)
	assertion := oauthtest.MintWorkloadAssertion(t, issuer, oauthtest.WorkloadClaims(issuer, testExternalSubject, testAudience))

	result, err := newLiveVerifier(t, issuer).Verify(t.Context(), assertion, liveExpectationFor(t, issuer, jwksURI))

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

	proof := oauthtest.MintWorkloadAssertion(t, issuer, oauthtest.WorkloadClaims(issuer, testExternalSubject, testAudience))
	retired := oauthtest.MintWorkloadAssertion(t, issuer, oauthtest.WorkloadClaims(issuer, testExternalSubject, testAudience))

	_, err := newLiveVerifier(t, issuer).Verify(t.Context(), proof, liveExpectationFor(t, issuer, jwksURI))
	require.NoError(t, err, "the retiring key must work before the rotation, or the test proves nothing")

	issuer.RotateKey(t)

	// A resolver holding the current set finds no key for the retired kid.
	_, err = newLiveVerifier(t, issuer).Verify(t.Context(), retired, liveExpectationFor(t, issuer, jwksURI))
	requireRejected(t, err, workload.ReasonKeyUnknown)

	// The issuer itself is still healthy.
	current := oauthtest.MintWorkloadAssertion(t, issuer, oauthtest.WorkloadClaims(issuer, testExternalSubject, testAudience))
	_, err = newLiveVerifier(t, issuer).Verify(t.Context(), current, liveExpectationFor(t, issuer, jwksURI))
	require.NoError(t, err, "the republished key must verify against a resolver that fetched it")
}

// An unreachable key set is refused even though the assertion is well-formed.
func TestWorkloadIssuer_UnreachableKeySetIsRefused(t *testing.T) {
	t.Parallel()

	issuer, jwksURI := launchWorkloadIssuer(t)
	// Minted while the issuer is up, so only the key set is missing.
	assertion := oauthtest.MintWorkloadAssertion(t, issuer, oauthtest.WorkloadClaims(issuer, testExternalSubject, testAudience))

	issuer.Stop()

	_, err := newLiveVerifier(t, issuer).Verify(t.Context(), assertion, liveExpectationFor(t, issuer, jwksURI))

	requireRejected(t, err, workload.ReasonKeyUnresolvable)
}

// A valid assertion from a trusted issuer for another subject fails to match
// the expected subject.
func TestWorkloadIssuer_SubjectOtherThanTheAdmittedOneIsRejected(t *testing.T) {
	t.Parallel()

	issuer, jwksURI := launchWorkloadIssuer(t)
	// Another workload on the same issuer.
	assertion := oauthtest.MintWorkloadAssertion(t, issuer, oauthtest.WorkloadClaims(issuer, "repo:someone-else/their-api:ref:refs/heads/main", testAudience))

	_, err := newLiveVerifier(t, issuer).Verify(t.Context(), assertion, liveExpectationFor(t, issuer, jwksURI))

	requireRejected(t, err, workload.ReasonSubjectMismatch)
}

// An assertion addressed to another audience is refused.
func TestWorkloadIssuer_AudienceForAnotherServerIsRejected(t *testing.T) {
	t.Parallel()

	issuer, jwksURI := launchWorkloadIssuer(t)
	assertion := oauthtest.MintWorkloadAssertion(t, issuer, oauthtest.WorkloadClaims(issuer, testExternalSubject, "https://gram.example.com/mcp/someone-else"))

	_, err := newLiveVerifier(t, issuer).Verify(t.Context(), assertion, liveExpectationFor(t, issuer, jwksURI))

	requireRejected(t, err, workload.ReasonAudienceMismatch)
}

// A replayed assertion is refused even though it still verifies.
func TestWorkloadIssuer_ReplayedAssertionIsRejected(t *testing.T) {
	t.Parallel()

	issuer, jwksURI := launchWorkloadIssuer(t)
	expectation := liveExpectationFor(t, issuer, jwksURI)
	verifier := newLiveVerifier(t, issuer)

	assertion := oauthtest.MintWorkloadAssertion(t, issuer, oauthtest.WorkloadClaims(issuer, testExternalSubject, testAudience))

	_, err := verifier.Verify(t.Context(), assertion, expectation)
	require.NoError(t, err)

	_, err = verifier.Verify(t.Context(), assertion, expectation)
	requireRejected(t, err, workload.ReasonReplayed)
}
