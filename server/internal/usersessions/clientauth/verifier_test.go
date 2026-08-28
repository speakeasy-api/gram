package clientauth_test

import (
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/go-jose/go-jose/v4"
	"github.com/go-jose/go-jose/v4/jwt"
	"github.com/redis/go-redis/v9"
	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/testenv"
	"github.com/speakeasy-api/gram/server/internal/usersessions/clientauth"
	"github.com/speakeasy-api/gram/server/internal/usersessions/jwks"
	"github.com/speakeasy-api/gram/server/internal/usersessions/replay"
)

func TestVerify_HappyPath(t *testing.T) {
	t.Parallel()

	s := newSigner(t, testKeyID)
	result, err := newVerifier(t).Verify(t.Context(), assertionFor(s.sign(t, validClaims())), expectationFor(t, s))
	require.NoError(t, err)
	require.Equal(t, clientauth.AudienceKindIssuer, result.Audience)
	require.False(t, result.ExpiresAt.IsZero())
}

// The token endpoint URL is accepted alongside the issuer identifier, and the
// two are distinguished in the result so production traffic shows which one
// real clients send.
func TestVerify_AudienceEndpointAccepted(t *testing.T) {
	t.Parallel()

	s := newSigner(t, testKeyID)
	claims := validClaims()
	claims.Audience = jwt.Audience{testTokenURL}

	result, err := newVerifier(t).Verify(t.Context(), assertionFor(s.sign(t, claims)), expectationFor(t, s))
	require.NoError(t, err)
	require.Equal(t, clientauth.AudienceKindEndpoint, result.Audience)
}

// An assertion naming several audiences is accepted when any one of them is
// ours, which is what RFC 7523 §3 requires.
func TestVerify_AudienceAmongSeveralAccepted(t *testing.T) {
	t.Parallel()

	s := newSigner(t, testKeyID)
	claims := validClaims()
	claims.Audience = jwt.Audience{"https://elsewhere.example.com/", testIssuer}

	_, err := newVerifier(t).Verify(t.Context(), assertionFor(s.sign(t, claims)), expectationFor(t, s))
	require.NoError(t, err)
}

// The load-bearing audience property: an assertion minted for a different MCP
// server must not authenticate here. Both accepted values are derived per
// request from the endpoint addressed, so a neighbouring server's issuer is
// simply not among them.
func TestVerify_AudienceForAnotherServerRejected(t *testing.T) {
	t.Parallel()

	s := newSigner(t, testKeyID)
	claims := validClaims()
	claims.Audience = jwt.Audience{"https://gram.example.com/mcp/other-tenant"}

	_, err := newVerifier(t).Verify(t.Context(), assertionFor(s.sign(t, claims)), expectationFor(t, s))
	requireRejected(t, err, clientauth.ReasonAudienceMismatch)
}

func TestVerify_AudienceMissingRejected(t *testing.T) {
	t.Parallel()

	s := newSigner(t, testKeyID)
	claims := validClaims()
	claims.Audience = nil

	_, err := newVerifier(t).Verify(t.Context(), assertionFor(s.sign(t, claims)), expectationFor(t, s))
	requireRejected(t, err, clientauth.ReasonAudienceMismatch)
}

// The canonical algorithm-confusion attack: sign HS256 using the client's own
// public key as the shared secret. The allowlist is applied at parse time, so
// this never reaches signature verification.
func TestVerify_HS256WithPublicKeyRejected(t *testing.T) {
	t.Parallel()

	s := newSigner(t, testKeyID)

	// The key set is public, so the attacker has the client's public key and
	// uses its serialized bytes as the HMAC secret. A verifier that took the
	// algorithm from the token would resolve the same key by kid and hand
	// those same bytes to HMAC, and the forgery would verify.
	var set jose.JSONWebKeySet
	require.NoError(t, json.Unmarshal(s.jwks, &set))
	publicBytes, err := set.Keys[0].MarshalJSON()
	require.NoError(t, err)

	hmacSigner, err := jose.NewSigner(
		jose.SigningKey{Algorithm: jose.HS256, Key: publicBytes},
		(&jose.SignerOptions{}).WithType("JWT").WithHeader(jose.HeaderKey("kid"), testKeyID),
	)
	require.NoError(t, err)
	forged, err := jwt.Signed(hmacSigner).Claims(validClaims()).Serialize()
	require.NoError(t, err)

	_, err = newVerifier(t).Verify(t.Context(), assertionFor(forged), expectationFor(t, s))
	requireRejected(t, err, clientauth.ReasonMalformed)
}

// An unsigned assertion proves nothing and is refused at parse time for the
// same reason every HS* is.
func TestVerify_AlgNoneRejected(t *testing.T) {
	t.Parallel()

	s := newSigner(t, testKeyID)
	// {"alg":"none"} over the standard claims, with an empty signature.
	unsigned := "eyJhbGciOiJub25lIiwidHlwIjoiSldUIn0." +
		"eyJpc3MiOiJodHRwczovL2NsaWVudC5leGFtcGxlLmNvbS9vYXV0aC9jbGllbnQuanNvbiJ9."

	_, err := newVerifier(t).Verify(t.Context(), assertionFor(unsigned), expectationFor(t, s))
	requireRejected(t, err, clientauth.ReasonMalformed)
}

// A signature made with a key the client never published must not verify,
// which is the whole point of resolving keys from the client's own key set.
func TestVerify_SignatureFromForeignKeyRejected(t *testing.T) {
	t.Parallel()

	published := newSigner(t, testKeyID)
	// A different key pair reusing the published kid, so key selection
	// succeeds and the signature check is what fails.
	attacker := newSigner(t, testKeyID)

	_, err := newVerifier(t).Verify(t.Context(), assertionFor(attacker.sign(t, validClaims())), expectationFor(t, published))
	requireRejected(t, err, clientauth.ReasonSignatureInvalid)
}

// A kid naming no published key is terminal for an inline key set, which has
// no upstream to refresh from.
func TestVerify_UnknownKidRejected(t *testing.T) {
	t.Parallel()

	published := newSigner(t, testKeyID)
	other := newSigner(t, "some-other-kid")

	expect := expectationFor(t, published)
	_, err := newVerifier(t).Verify(t.Context(), assertionFor(other.sign(t, validClaims())), expect)
	requireRejected(t, err, clientauth.ReasonKeyUnknown)
}

func TestVerify_IssuerSubjectMustEqualClientID(t *testing.T) {
	t.Parallel()

	s := newSigner(t, testKeyID)

	mismatchedSubject := validClaims()
	mismatchedSubject.Subject = "https://client.example.com/oauth/other.json"
	_, err := newVerifier(t).Verify(t.Context(), assertionFor(s.sign(t, mismatchedSubject)), expectationFor(t, s))
	requireRejected(t, err, clientauth.ReasonSubjectMismatch)

	mismatchedIssuer := validClaims()
	mismatchedIssuer.Issuer = "https://client.example.com/oauth/other.json"
	_, err = newVerifier(t).Verify(t.Context(), assertionFor(s.sign(t, mismatchedIssuer)), expectationFor(t, s))
	requireRejected(t, err, clientauth.ReasonSubjectMismatch)
}

func TestVerify_ExpiredRejected(t *testing.T) {
	t.Parallel()

	s := newSigner(t, testKeyID)
	claims := validClaims()
	claims.Expiry = jwt.NewNumericDate(time.Now().Add(-10 * time.Minute))

	_, err := newVerifier(t).Verify(t.Context(), assertionFor(s.sign(t, claims)), expectationFor(t, s))
	requireRejected(t, err, clientauth.ReasonExpired)
}

func TestVerify_MissingExpiryRejected(t *testing.T) {
	t.Parallel()

	s := newSigner(t, testKeyID)
	claims := validClaims()
	claims.Expiry = nil

	_, err := newVerifier(t).Verify(t.Context(), assertionFor(s.sign(t, claims)), expectationFor(t, s))
	requireRejected(t, err, clientauth.ReasonExpiryMissing)
}

// An assertion valid far into the future is refused: the ceiling bounds how
// long the replay guard has to remember its identifier.
func TestVerify_OverlongLifetimeRejected(t *testing.T) {
	t.Parallel()

	s := newSigner(t, testKeyID)
	claims := validClaims()
	claims.Expiry = jwt.NewNumericDate(time.Now().Add(25 * time.Hour))

	_, err := newVerifier(t).Verify(t.Context(), assertionFor(s.sign(t, claims)), expectationFor(t, s))
	requireRejected(t, err, clientauth.ReasonLifetimeTooLong)
}

// An hour is inside the ceiling, because that is what stock client libraries
// emit and rejecting them was the failure this bound was chosen to avoid.
func TestVerify_OneHourLifetimeAccepted(t *testing.T) {
	t.Parallel()

	s := newSigner(t, testKeyID)
	claims := validClaims()
	claims.Expiry = jwt.NewNumericDate(time.Now().Add(clientauth.DefaultMaxLifetime))

	_, err := newVerifier(t).Verify(t.Context(), assertionFor(s.sign(t, claims)), expectationFor(t, s))
	require.NoError(t, err)
}

func TestVerify_NotYetValidRejected(t *testing.T) {
	t.Parallel()

	s := newSigner(t, testKeyID)

	future := validClaims()
	future.NotBefore = jwt.NewNumericDate(time.Now().Add(10 * time.Minute))
	_, err := newVerifier(t).Verify(t.Context(), assertionFor(s.sign(t, future)), expectationFor(t, s))
	requireRejected(t, err, clientauth.ReasonNotYetValid)

	issuedAhead := validClaims()
	issuedAhead.IssuedAt = jwt.NewNumericDate(time.Now().Add(10 * time.Minute))
	_, err = newVerifier(t).Verify(t.Context(), assertionFor(s.sign(t, issuedAhead)), expectationFor(t, s))
	requireRejected(t, err, clientauth.ReasonNotYetValid)
}

// Clock drift within the tolerated skew must not break a legitimate client.
func TestVerify_SmallClockSkewTolerated(t *testing.T) {
	t.Parallel()

	s := newSigner(t, testKeyID)
	claims := validClaims()
	claims.NotBefore = jwt.NewNumericDate(time.Now().Add(20 * time.Second))
	claims.IssuedAt = jwt.NewNumericDate(time.Now().Add(20 * time.Second))

	_, err := newVerifier(t).Verify(t.Context(), assertionFor(s.sign(t, claims)), expectationFor(t, s))
	require.NoError(t, err)
}

// nbf and iat are optional per RFC 7519 §4.1 and their absence is not a
// rejection.
func TestVerify_OptionalTemporalClaimsMayBeAbsent(t *testing.T) {
	t.Parallel()

	s := newSigner(t, testKeyID)
	claims := validClaims()
	claims.NotBefore = nil
	claims.IssuedAt = nil

	_, err := newVerifier(t).Verify(t.Context(), assertionFor(s.sign(t, claims)), expectationFor(t, s))
	require.NoError(t, err)
}

func TestVerify_MissingJTIRejected(t *testing.T) {
	t.Parallel()

	s := newSigner(t, testKeyID)
	claims := validClaims()
	claims.ID = ""

	_, err := newVerifier(t).Verify(t.Context(), assertionFor(s.sign(t, claims)), expectationFor(t, s))
	requireRejected(t, err, clientauth.ReasonIDMissing)
}

// The replay property: presenting the same assertion twice fails the second
// time, even though everything about it is still valid.
func TestVerify_ReplayedAssertionRejected(t *testing.T) {
	t.Parallel()

	s := newSigner(t, testKeyID)
	verifier := newVerifier(t)
	expect := expectationFor(t, s)
	assertion := s.sign(t, validClaims())

	_, err := verifier.Verify(t.Context(), assertionFor(assertion), expect)
	require.NoError(t, err)

	_, err = verifier.Verify(t.Context(), assertionFor(assertion), expect)
	requireRejected(t, err, clientauth.ReasonReplayed)
}

// The reservation spans every endpoint sharing a replay issuer, so an
// assertion spent at the token endpoint cannot be re-presented at revocation.
func TestVerify_ReplaySpansEndpoints(t *testing.T) {
	t.Parallel()

	s := newSigner(t, testKeyID)
	verifier := newVerifier(t)
	assertion := s.sign(t, validClaims())

	atToken := expectationFor(t, s)
	_, err := verifier.Verify(t.Context(), assertionFor(assertion), atToken)
	require.NoError(t, err)

	// Same server, different endpoint: the revocation endpoint accepts the
	// same audiences and shares the replay issuer.
	atRevoke := expectationFor(t, s)
	atRevoke.Audiences.Endpoint = "https://gram.example.com/mcp/demo/revoke"
	_, err = verifier.Verify(t.Context(), assertionFor(assertion), atRevoke)
	requireRejected(t, err, clientauth.ReasonReplayed)
}

// A different client's identical jti is not a replay: identifiers are only
// unique within the client that minted them.
func TestVerify_ReplayScopedToClient(t *testing.T) {
	t.Parallel()

	first := newSigner(t, testKeyID)
	verifier := newVerifier(t)

	claims := validClaims()
	_, err := verifier.Verify(t.Context(), assertionFor(first.sign(t, claims)), expectationFor(t, first))
	require.NoError(t, err)

	const otherClientID = "https://other.example.com/oauth/client.json"
	second := newSigner(t, testKeyID)
	otherClaims := claims
	otherClaims.Issuer = otherClientID
	otherClaims.Subject = otherClientID

	expect := clientauth.ClientExpectation(
		otherClientID,
		second.source(t),
		t.Name(),
		clientauth.Audiences{Issuer: testIssuer, Endpoint: testTokenURL},
	)
	_, err = verifier.Verify(t.Context(), assertionFor(second.sign(t, otherClaims)), expect)
	require.NoError(t, err, "another client reusing the same jti is not a replay")
}

func TestVerify_WrongAssertionTypeRejected(t *testing.T) {
	t.Parallel()

	s := newSigner(t, testKeyID)
	req := clientauth.Assertion{Value: s.sign(t, validClaims()), Type: "urn:example:something-else"}

	_, err := newVerifier(t).Verify(t.Context(), req, expectationFor(t, s))
	requireRejected(t, err, clientauth.ReasonTypeUnsupported)
}

func TestVerify_MissingAssertionRejected(t *testing.T) {
	t.Parallel()

	s := newSigner(t, testKeyID)

	_, err := newVerifier(t).Verify(t.Context(), clientauth.Assertion{}, expectationFor(t, s))
	requireRejected(t, err, clientauth.ReasonAssertionMissing)

	_, err = newVerifier(t).Verify(t.Context(), clientauth.Assertion{Type: clientauth.AssertionType}, expectationFor(t, s))
	requireRejected(t, err, clientauth.ReasonAssertionMissing)
}

func TestVerify_GarbageAssertionRejected(t *testing.T) {
	t.Parallel()

	s := newSigner(t, testKeyID)

	_, err := newVerifier(t).Verify(t.Context(), assertionFor("not-a-jwt"), expectationFor(t, s))
	requireRejected(t, err, clientauth.ReasonMalformed)
}

// An incompletely assembled Expectation is a wiring fault, and each omission
// is labelled as one rather than as the client-shaped failure it would
// otherwise produce: no audiences would read as an audience mismatch, and an
// empty replay issuer as a store outage.
func TestVerify_MisconfiguredExpectationRejected(t *testing.T) {
	t.Parallel()

	s := newSigner(t, testKeyID)
	verifier := newVerifier(t)
	assertion := s.sign(t, validClaims())

	noAudiences := expectationFor(t, s)
	noAudiences.Audiences = clientauth.Audiences{Issuer: "", Endpoint: ""}
	_, err := verifier.Verify(t.Context(), assertionFor(assertion), noAudiences)
	requireRejected(t, err, clientauth.ReasonVerifierMisconfigured)

	noReplayIssuer := expectationFor(t, s)
	noReplayIssuer.ReplayIssuer = ""
	_, err = verifier.Verify(t.Context(), assertionFor(assertion), noReplayIssuer)
	requireRejected(t, err, clientauth.ReasonVerifierMisconfigured)

	noReplayParty := expectationFor(t, s)
	noReplayParty.ReplayParty = ""
	_, err = verifier.Verify(t.Context(), assertionFor(assertion), noReplayParty)
	requireRejected(t, err, clientauth.ReasonVerifierMisconfigured)

	// An empty Issuer or Subject would otherwise be satisfied by an
	// assertion that simply omits that claim.
	noIssuer := expectationFor(t, s)
	noIssuer.Issuer = ""
	_, err = verifier.Verify(t.Context(), assertionFor(assertion), noIssuer)
	requireRejected(t, err, clientauth.ReasonVerifierMisconfigured)

	noSubject := expectationFor(t, s)
	noSubject.Subject = ""
	_, err = verifier.Verify(t.Context(), assertionFor(assertion), noSubject)
	requireRejected(t, err, clientauth.ReasonVerifierMisconfigured)
}

// A guard sized for the default bound cannot serve an expectation that lets
// assertions live longer: the reservation would lapse while the assertion
// still verifies, and Guard.Reserve clamps the hold silently rather than
// complaining. Refused per request, since the bound is only known then.
func TestVerify_LifetimeBeyondGuardHoldRejected(t *testing.T) {
	t.Parallel()

	s := newSigner(t, testKeyID)
	verifier := newVerifier(t)

	expect := expectationFor(t, s)
	expect.MaxLifetime = 8 * time.Hour

	_, err := verifier.Verify(t.Context(), assertionFor(s.sign(t, validClaims())), expect)
	requireRejected(t, err, clientauth.ReasonVerifierMisconfigured)
}

// The per-expectation bound is what the exp ceiling is measured against, not
// the package default. A workload profile that permits a longer lifetime must
// accept an assertion the client bound would have rejected — this is the
// Kubernetes case, where expirationSeconds is routinely past an hour.
func TestVerify_ExpectationLifetimeWidensTheCeiling(t *testing.T) {
	t.Parallel()

	s := newSigner(t, testKeyID)

	claims := validClaims()
	claims.Expiry = jwt.NewNumericDate(time.Now().Add(3 * time.Hour))
	assertion := assertionFor(s.sign(t, claims))

	// The default client bound rejects it for lifetime, not for anything else.
	_, err := newVerifier(t).Verify(t.Context(), assertion, expectationFor(t, s))
	requireRejected(t, err, clientauth.ReasonLifetimeTooLong)

	// A guard sized for the longer bound, and an expectation carrying it,
	// accepts the same assertion.
	client, err := infra.NewRedisClient(t, 0)
	require.NoError(t, err)
	lifetime := 4 * time.Hour
	guard, err := replay.NewRedisGuard(client, string(testenv.NewCacheSuffix(t, "long")), clientauth.ReplayHoldFor(lifetime))
	require.NoError(t, err)
	verifier, err := clientauth.NewVerifier(newKeyResolver(t, client), guard)
	require.NoError(t, err)

	expect := expectationFor(t, s)
	expect.MaxLifetime = lifetime
	_, err = verifier.Verify(t.Context(), assertion, expect)
	require.NoError(t, err)
}

// iss and sub are matched separately, so an assertion satisfying one and not
// the other is still rejected. A workload's two values differ, so nothing may
// collapse them into a single comparison.
func TestVerify_SubjectMustMatchIndependentlyOfIssuer(t *testing.T) {
	t.Parallel()

	s := newSigner(t, testKeyID)

	claims := validClaims()
	claims.Subject = "someone-else"

	_, err := newVerifier(t).Verify(t.Context(), assertionFor(s.sign(t, claims)), expectationFor(t, s))
	requireRejected(t, err, clientauth.ReasonSubjectMismatch)
}

// A guard whose cap is shorter than the window an assertion stays acceptable
// would release identifiers while they can still be replayed. The mismatch is
// refused at wiring time, since nothing at request time would notice it.
func TestNewVerifier_RejectsShortReplayHold(t *testing.T) {
	t.Parallel()

	client, err := infra.NewRedisClient(t, 0)
	require.NoError(t, err)

	short, err := replay.NewRedisGuard(client, string(testenv.NewCacheSuffix(t, "short")), clientauth.DefaultMaxReplayHold-time.Second)
	require.NoError(t, err)

	_, err = clientauth.NewVerifier(newKeyResolver(t, client), short)
	require.Error(t, err)
}

// The hold must cover the whole window in which an accepted assertion still
// verifies: exp may sit MaxSkew beyond MaxLifetime and is then honoured for
// another MaxSkew. Pinned as an arithmetic fact so the constants cannot drift
// apart without this failing.
func TestMaxReplayHold_CoversAcceptanceWindow(t *testing.T) {
	t.Parallel()

	require.GreaterOrEqual(t, clientauth.DefaultMaxReplayHold, clientauth.DefaultMaxLifetime+2*clientauth.MaxSkew)
}

// An assertion presented without its type parameter is a malformed request,
// not an unsupported type, and the label says so: an operator triaging it
// should look for a missing parameter rather than a wrong URN.
func TestVerify_AssertionWithoutTypeRejected(t *testing.T) {
	t.Parallel()

	s := newSigner(t, testKeyID)
	req := clientauth.Assertion{Value: s.sign(t, validClaims()), Type: ""}

	_, err := newVerifier(t).Verify(t.Context(), req, expectationFor(t, s))
	requireRejected(t, err, clientauth.ReasonTypeUnsupported)
	require.ErrorContains(t, err, "client_assertion_type is required")
}

// An oversized assertion is refused before it is parsed, so the cost of a
// rejected assertion is bounded by what a real one could be rather than by the
// calling endpoint's body limit.
func TestVerify_OversizedAssertionRejected(t *testing.T) {
	t.Parallel()

	s := newSigner(t, testKeyID)
	oversized := s.sign(t, validClaims()) + strings.Repeat("A", 8*1024)

	_, err := newVerifier(t).Verify(t.Context(), assertionFor(oversized), expectationFor(t, s))
	requireRejected(t, err, clientauth.ReasonMalformed)
	require.ErrorContains(t, err, "exceeds")
}

func TestNewVerifier_RequiresDependencies(t *testing.T) {
	t.Parallel()

	_, err := clientauth.NewVerifier(nil, nil)
	require.Error(t, err, "a verifier with no key resolver cannot check a signature")
}

// The allowlist is not ES256-only: an RSA client signing RS256 and PS256
// verifies too, which is what most private_key_jwt libraries emit.
func TestVerify_RSAAlgorithmsAccepted(t *testing.T) {
	t.Parallel()

	for _, alg := range []jose.SignatureAlgorithm{jose.RS256, jose.PS256} {
		s := newRSASigner(t, testKeyID, alg)
		_, err := newVerifier(t).Verify(t.Context(), assertionFor(s.sign(t, validClaims())), expectationFor(t, s))
		require.NoError(t, err, "%s must verify", alg)
	}
}

// An assertion with no kid selects the key when the set holds exactly one and
// is refused when it holds several: guessing among keys until one verifies
// would make verification an oracle.
func TestVerify_NoKidResolution(t *testing.T) {
	t.Parallel()

	single := newSigner(t, "")
	_, err := newVerifier(t).Verify(t.Context(), assertionFor(single.sign(t, validClaims())), expectationFor(t, single))
	require.NoError(t, err, "a single-key set needs no kid")

	// Two keys published, assertion names neither.
	first := newSigner(t, "")
	second := newSigner(t, "other")
	var set jose.JSONWebKeySet
	require.NoError(t, json.Unmarshal(first.jwks, &set))
	var more jose.JSONWebKeySet
	require.NoError(t, json.Unmarshal(second.jwks, &more))
	set.Keys = append(set.Keys, more.Keys...)
	merged, err := json.Marshal(set)
	require.NoError(t, err)
	source, err := jwks.NewInlineSource(merged)
	require.NoError(t, err)

	expect := expectationFor(t, first)
	expect.KeySource = source
	_, err = newVerifier(t).Verify(t.Context(), assertionFor(first.sign(t, validClaims())), expect)
	requireRejected(t, err, clientauth.ReasonKeyUnknown)
}

// A replay store that cannot be consulted refuses the assertion with its own
// label rather than admitting it: an identifier whose status is unknown must
// be treated as already seen, or an outage would suspend replay protection
// exactly when it matters.
func TestVerify_ReplayStoreOutageRefuses(t *testing.T) {
	t.Parallel()

	s := newSigner(t, testKeyID)
	live, err := infra.NewRedisClient(t, 0)
	require.NoError(t, err)

	// A client pointed at nothing: every command fails at dial time.
	dead := redis.NewClient(&redis.Options{Addr: "127.0.0.1:1", DialTimeout: 100 * time.Millisecond, MaxRetries: -1})
	t.Cleanup(func() { _ = dead.Close() })
	guard, err := replay.NewRedisGuard(dead, string(testenv.NewCacheSuffix(t, "outage")), clientauth.DefaultMaxReplayHold)
	require.NoError(t, err)
	verifier, err := clientauth.NewVerifier(newKeyResolver(t, live), guard)
	require.NoError(t, err)

	_, err = verifier.Verify(t.Context(), assertionFor(s.sign(t, validClaims())), expectationFor(t, s))
	requireRejected(t, err, clientauth.ReasonReplayStoreUnavailable)
}

// A source that resolves to nothing usable is distinguished from a key that
// is merely unknown, because the two mean different things operationally.
func TestVerify_UnresolvableKeySourceReported(t *testing.T) {
	t.Parallel()

	s := newSigner(t, testKeyID)
	broken, err := jwks.NewInlineSource(json.RawMessage(`{"keys":`))
	require.NoError(t, err)

	expect := expectationFor(t, s)
	expect.KeySource = broken

	_, err = newVerifier(t).Verify(t.Context(), assertionFor(s.sign(t, validClaims())), expect)
	requireRejected(t, err, clientauth.ReasonKeyUnresolvable)
}
