package privatekeyjwt_test

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
	"github.com/speakeasy-api/gram/server/internal/usersessions/assertion/privatekeyjwt"
	"github.com/speakeasy-api/gram/server/internal/usersessions/jwks"
	"github.com/speakeasy-api/gram/server/internal/usersessions/replay"
)

func TestVerify_HappyPath(t *testing.T) {
	t.Parallel()

	s := newSigner(t, testKeyID)
	result, err := newVerifier(t).Verify(t.Context(), assertionFor(s.sign(t, validClaims())), expectationFor(t, s))
	require.NoError(t, err)
	require.Equal(t, privatekeyjwt.AudienceKindIssuer, result.Audience)
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
	require.Equal(t, privatekeyjwt.AudienceKindEndpoint, result.Audience)
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
	requireRejected(t, err, privatekeyjwt.ReasonAudienceMismatch)
}

func TestVerify_AudienceMissingRejected(t *testing.T) {
	t.Parallel()

	s := newSigner(t, testKeyID)
	claims := validClaims()
	claims.Audience = nil

	_, err := newVerifier(t).Verify(t.Context(), assertionFor(s.sign(t, claims)), expectationFor(t, s))
	requireRejected(t, err, privatekeyjwt.ReasonAudienceMismatch)
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
	requireRejected(t, err, privatekeyjwt.ReasonMalformed)
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
	requireRejected(t, err, privatekeyjwt.ReasonMalformed)
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
	requireRejected(t, err, privatekeyjwt.ReasonSignatureInvalid)
}

// A kid naming no published key is terminal for an inline key set, which has
// no upstream to refresh from.
func TestVerify_UnknownKidRejected(t *testing.T) {
	t.Parallel()

	published := newSigner(t, testKeyID)
	other := newSigner(t, "some-other-kid")

	expect := expectationFor(t, published)
	_, err := newVerifier(t).Verify(t.Context(), assertionFor(other.sign(t, validClaims())), expect)
	requireRejected(t, err, privatekeyjwt.ReasonKeyUnknown)
}

func TestVerify_IssuerSubjectMustEqualClientID(t *testing.T) {
	t.Parallel()

	s := newSigner(t, testKeyID)

	mismatchedSubject := validClaims()
	mismatchedSubject.Subject = "https://client.example.com/oauth/other.json"
	_, err := newVerifier(t).Verify(t.Context(), assertionFor(s.sign(t, mismatchedSubject)), expectationFor(t, s))
	requireRejected(t, err, privatekeyjwt.ReasonSubjectMismatch)

	mismatchedIssuer := validClaims()
	mismatchedIssuer.Issuer = "https://client.example.com/oauth/other.json"
	_, err = newVerifier(t).Verify(t.Context(), assertionFor(s.sign(t, mismatchedIssuer)), expectationFor(t, s))
	requireRejected(t, err, privatekeyjwt.ReasonSubjectMismatch)
}

func TestVerify_ExpiredRejected(t *testing.T) {
	t.Parallel()

	s := newSigner(t, testKeyID)
	claims := validClaims()
	claims.Expiry = jwt.NewNumericDate(time.Now().Add(-10 * time.Minute))

	_, err := newVerifier(t).Verify(t.Context(), assertionFor(s.sign(t, claims)), expectationFor(t, s))
	requireRejected(t, err, privatekeyjwt.ReasonExpired)
}

func TestVerify_MissingExpiryRejected(t *testing.T) {
	t.Parallel()

	s := newSigner(t, testKeyID)
	claims := validClaims()
	claims.Expiry = nil

	_, err := newVerifier(t).Verify(t.Context(), assertionFor(s.sign(t, claims)), expectationFor(t, s))
	requireRejected(t, err, privatekeyjwt.ReasonExpiryMissing)
}

// An assertion valid far into the future is refused: the ceiling bounds how
// long the replay guard has to remember its identifier.
func TestVerify_OverlongLifetimeRejected(t *testing.T) {
	t.Parallel()

	s := newSigner(t, testKeyID)
	claims := validClaims()
	claims.Expiry = jwt.NewNumericDate(time.Now().Add(25 * time.Hour))

	_, err := newVerifier(t).Verify(t.Context(), assertionFor(s.sign(t, claims)), expectationFor(t, s))
	requireRejected(t, err, privatekeyjwt.ReasonLifetimeTooLong)
}

// An hour is inside the ceiling, because that is what stock client libraries
// emit and rejecting them was the failure this bound was chosen to avoid.
func TestVerify_OneHourLifetimeAccepted(t *testing.T) {
	t.Parallel()

	s := newSigner(t, testKeyID)
	claims := validClaims()
	claims.Expiry = jwt.NewNumericDate(time.Now().Add(privatekeyjwt.DefaultMaxLifetime))

	_, err := newVerifier(t).Verify(t.Context(), assertionFor(s.sign(t, claims)), expectationFor(t, s))
	require.NoError(t, err)
}

func TestVerify_NotYetValidRejected(t *testing.T) {
	t.Parallel()

	s := newSigner(t, testKeyID)

	future := validClaims()
	future.NotBefore = jwt.NewNumericDate(time.Now().Add(10 * time.Minute))
	_, err := newVerifier(t).Verify(t.Context(), assertionFor(s.sign(t, future)), expectationFor(t, s))
	requireRejected(t, err, privatekeyjwt.ReasonNotYetValid)

	issuedAhead := validClaims()
	issuedAhead.IssuedAt = jwt.NewNumericDate(time.Now().Add(10 * time.Minute))
	_, err = newVerifier(t).Verify(t.Context(), assertionFor(s.sign(t, issuedAhead)), expectationFor(t, s))
	requireRejected(t, err, privatekeyjwt.ReasonNotYetValid)
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
	requireRejected(t, err, privatekeyjwt.ReasonIDMissing)
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
	requireRejected(t, err, privatekeyjwt.ReasonReplayed)
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
	requireRejected(t, err, privatekeyjwt.ReasonReplayed)
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

	expect := privatekeyjwt.ClientExpectation(
		otherClientID,
		second.source(t),
		t.Name(),
		privatekeyjwt.Audiences{Issuer: testIssuer, Endpoint: testTokenURL},
	)
	_, err = verifier.Verify(t.Context(), assertionFor(second.sign(t, otherClaims)), expect)
	require.NoError(t, err, "another client reusing the same jti is not a replay")
}

func TestVerify_WrongAssertionTypeRejected(t *testing.T) {
	t.Parallel()

	s := newSigner(t, testKeyID)
	req := privatekeyjwt.Assertion{Value: s.sign(t, validClaims()), Type: "urn:example:something-else"}

	_, err := newVerifier(t).Verify(t.Context(), req, expectationFor(t, s))
	requireRejected(t, err, privatekeyjwt.ReasonTypeUnsupported)
}

func TestVerify_MissingAssertionRejected(t *testing.T) {
	t.Parallel()

	s := newSigner(t, testKeyID)

	_, err := newVerifier(t).Verify(t.Context(), privatekeyjwt.Assertion{}, expectationFor(t, s))
	requireRejected(t, err, privatekeyjwt.ReasonAssertionMissing)

	_, err = newVerifier(t).Verify(t.Context(), privatekeyjwt.Assertion{Type: privatekeyjwt.AssertionType}, expectationFor(t, s))
	requireRejected(t, err, privatekeyjwt.ReasonAssertionMissing)
}

func TestVerify_GarbageAssertionRejected(t *testing.T) {
	t.Parallel()

	s := newSigner(t, testKeyID)

	_, err := newVerifier(t).Verify(t.Context(), assertionFor("not-a-jwt"), expectationFor(t, s))
	requireRejected(t, err, privatekeyjwt.ReasonMalformed)
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
	noAudiences.Audiences = privatekeyjwt.Audiences{Issuer: "", Endpoint: ""}
	_, err := verifier.Verify(t.Context(), assertionFor(assertion), noAudiences)
	requireRejected(t, err, privatekeyjwt.ReasonVerifierMisconfigured)

	noReplayIssuer := expectationFor(t, s)
	noReplayIssuer.ReplayIssuer = ""
	_, err = verifier.Verify(t.Context(), assertionFor(assertion), noReplayIssuer)
	requireRejected(t, err, privatekeyjwt.ReasonVerifierMisconfigured)

	// An empty client_id would otherwise be satisfied by an assertion that
	// simply omits iss and sub.
	noClientID := expectationFor(t, s)
	noClientID.ClientID = ""
	_, err = verifier.Verify(t.Context(), assertionFor(assertion), noClientID)
	requireRejected(t, err, privatekeyjwt.ReasonVerifierMisconfigured)
}

// Matching iss alone cannot authenticate a client whose sub names another.
func TestVerify_SubjectMustMatchIndependentlyOfIssuer(t *testing.T) {
	t.Parallel()

	s := newSigner(t, testKeyID)

	claims := validClaims()
	claims.Subject = "someone-else"

	_, err := newVerifier(t).Verify(t.Context(), assertionFor(s.sign(t, claims)), expectationFor(t, s))
	requireRejected(t, err, privatekeyjwt.ReasonSubjectMismatch)
}

// A guard whose cap is shorter than the window an assertion stays acceptable
// would release identifiers while they can still be replayed. The mismatch is
// refused at wiring time, since nothing at request time would notice it.
func TestNewVerifier_RejectsShortReplayHold(t *testing.T) {
	t.Parallel()

	client, err := infra.NewRedisClient(t, 0)
	require.NoError(t, err)

	short, err := replay.NewRedisGuard(client, string(testenv.NewCacheSuffix(t, "short")), privatekeyjwt.DefaultMaxReplayHold-time.Second)
	require.NoError(t, err)

	_, err = privatekeyjwt.NewVerifier(newKeyResolver(t, client), short)
	require.Error(t, err)
}

// The hold must cover the whole window in which an accepted assertion still
// verifies: exp may sit MaxSkew beyond the lifetime bound and is then honoured for
// another MaxSkew. Pinned as an arithmetic fact so the constants cannot drift
// apart without this failing.
func TestMaxReplayHold_CoversAcceptanceWindow(t *testing.T) {
	t.Parallel()

	require.GreaterOrEqual(t, privatekeyjwt.DefaultMaxReplayHold, privatekeyjwt.DefaultMaxLifetime+2*privatekeyjwt.MaxSkew)
}

// An assertion presented without its type parameter is a malformed request,
// not an unsupported type, and the label says so: an operator triaging it
// should look for a missing parameter rather than a wrong URN.
func TestVerify_AssertionWithoutTypeRejected(t *testing.T) {
	t.Parallel()

	s := newSigner(t, testKeyID)
	req := privatekeyjwt.Assertion{Value: s.sign(t, validClaims()), Type: ""}

	_, err := newVerifier(t).Verify(t.Context(), req, expectationFor(t, s))
	requireRejected(t, err, privatekeyjwt.ReasonTypeUnsupported)
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
	requireRejected(t, err, privatekeyjwt.ReasonMalformed)
	require.ErrorContains(t, err, "exceeds")
}

func TestNewVerifier_RequiresDependencies(t *testing.T) {
	t.Parallel()

	_, err := privatekeyjwt.NewVerifier(nil, nil)
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
	requireRejected(t, err, privatekeyjwt.ReasonKeyUnknown)
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
	guard, err := replay.NewRedisGuard(dead, string(testenv.NewCacheSuffix(t, "outage")), privatekeyjwt.DefaultMaxReplayHold)
	require.NoError(t, err)
	verifier, err := privatekeyjwt.NewVerifier(newKeyResolver(t, live), guard)
	require.NoError(t, err)

	_, err = verifier.Verify(t.Context(), assertionFor(s.sign(t, validClaims())), expectationFor(t, s))
	requireRejected(t, err, privatekeyjwt.ReasonReplayStoreUnavailable)
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
	requireRejected(t, err, privatekeyjwt.ReasonKeyUnresolvable)
}

// The client profile is unchanged, and this is what proves the relaxation is
// scoped to a profile rather than applied to the package.
func TestVerify_ClientWithoutJTIStillRejected(t *testing.T) {
	t.Parallel()

	s := newSigner(t, testKeyID)

	claims := validClaims()
	claims.ID = ""

	_, err := newVerifier(t).Verify(t.Context(), assertionFor(s.sign(t, claims)), expectationFor(t, s))
	requireRejected(t, err, privatekeyjwt.ReasonIDMissing)
}

// A client assertion carries a jti and never consults uti, so whatever an
// unrelated party happens to put in that claim must not decide whether the
// assertion verifies.
func TestVerify_ClientWithUnreadableUTIStillAccepted(t *testing.T) {
	t.Parallel()

	s := newSigner(t, testKeyID)

	assertion := assertionFor(s.signWith(t, validClaims(), map[string]any{"uti": 12345}))

	_, err := newVerifier(t).Verify(t.Context(), assertion, expectationFor(t, s))
	require.NoError(t, err, "a non-string uti is not this client's problem")
}

// keySourceWithOps republishes s's single key with an explicit key_ops member.
func keySourceWithOps(t *testing.T, s *signer, ops string) jwks.Source {
	t.Helper()

	var set struct {
		Keys []map[string]json.RawMessage `json:"keys"`
	}
	require.NoError(t, json.Unmarshal(s.jwks, &set))
	require.Len(t, set.Keys, 1)
	set.Keys[0]["key_ops"] = json.RawMessage(ops)
	raw, err := json.Marshal(set)
	require.NoError(t, err)
	source, err := jwks.NewInlineSource(raw)
	require.NoError(t, err)
	return source
}

// An explicit key_ops keeps the key as long as it names verify, whatever else
// it lists; one that leaves verify out publishes no key the assertion can use.
func TestVerify_KeyOpsMustIncludeVerify(t *testing.T) {
	t.Parallel()

	s := newSigner(t, testKeyID)

	expect := expectationFor(t, s)
	expect.KeySource = keySourceWithOps(t, s, `["verify","encrypt"]`)
	_, err := newVerifier(t).Verify(t.Context(), assertionFor(s.sign(t, validClaims())), expect)
	require.NoError(t, err)

	expect.KeySource = keySourceWithOps(t, s, `["sign"]`)
	_, err = newVerifier(t).Verify(t.Context(), assertionFor(s.sign(t, validClaims())), expect)
	requireRejected(t, err, privatekeyjwt.ReasonKeyUnknown)
}

func TestVerify_SelectsMatchingAlgorithmWhenKidIsShared(t *testing.T) {
	t.Parallel()
	ec := newSigner(t, "shared")
	rsa := newRSASigner(t, "shared", jose.PS256)
	var ecSet, rsaSet jose.JSONWebKeySet
	require.NoError(t, json.Unmarshal(ec.jwks, &ecSet))
	require.NoError(t, json.Unmarshal(rsa.jwks, &rsaSet))
	combined, err := json.Marshal(jose.JSONWebKeySet{Keys: append(rsaSet.Keys, ecSet.Keys...)})
	require.NoError(t, err)
	ec.jwks = combined
	_, err = newVerifier(t).Verify(t.Context(), assertionFor(ec.sign(t, validClaims())), expectationFor(t, ec))
	require.NoError(t, err)
}
