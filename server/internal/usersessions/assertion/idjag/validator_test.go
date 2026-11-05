package idjag

import (
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"encoding/json"
	"errors"
	"testing"
	"time"

	"github.com/go-jose/go-jose/v4"
	"github.com/go-jose/go-jose/v4/jwt"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/urn"
	assertioncore "github.com/speakeasy-api/gram/server/internal/usersessions/assertion"
	"github.com/speakeasy-api/gram/server/internal/usersessions/jwks"
	"github.com/speakeasy-api/gram/server/internal/usersessions/replay"
)

const (
	testIssuer   = "https://idp.example.com"
	testAudience = "https://gram.example.com/mcp/demo"
	testResource = "https://gram.example.com/mcp/demo/mcp"
	testClientID = "https://client.example.com/metadata.json"
)

type testStore struct {
	trusted      TrustedIssuer
	user         string
	trustErr     error
	resolveErr   error
	resolveCalls int
}

func (s *testStore) TrustedIssuer(_ context.Context, _ string, _ uuid.UUID) (TrustedIssuer, error) {
	return s.trusted, s.trustErr
}

func (s *testStore) ResolveUser(_ context.Context, _, _ string) (string, error) {
	s.resolveCalls++
	return s.user, s.resolveErr
}

type testKeys struct {
	key   *jose.JSONWebKey
	err   error
	calls int
}

func (k *testKeys) VerificationKeyForAlgorithm(_ context.Context, _ jwks.Source, _ string, _ jose.SignatureAlgorithm) (*jose.JSONWebKey, error) {
	k.calls++
	return k.key, k.err
}

type testGuard struct {
	claimed map[string]bool
	owners  map[string]string
	calls   int
}

func (g *testGuard) MaxHold() time.Duration { return assertioncore.ReplayHoldFor(MaxLifetime) }

func (g *testGuard) Reserve(_ context.Context, key replay.Key, _ time.Time) (bool, error) {
	g.calls++
	if g.claimed[key.ID] {
		return false, nil
	}
	g.claimed[key.ID] = true
	return true, nil
}

func (g *testGuard) ReserveOwned(_ context.Context, key replay.Key, _ time.Time, owner string) (bool, error) {
	g.calls++
	if g.owners == nil {
		g.owners = map[string]string{}
	}
	if current, held := g.owners[key.ID]; held {
		return current == owner, nil
	}
	if g.claimed[key.ID] {
		return false, nil
	}
	g.owners[key.ID] = owner
	return true, nil
}

func (g *testGuard) Release(_ context.Context, key replay.Key, owner string) error {
	if g.owners[key.ID] == owner {
		delete(g.owners, key.ID)
	}
	return nil
}

type testAssertion struct {
	signer jose.Signer
	key    *jose.JSONWebKey
}

func newTestAssertion(t *testing.T, typ string) testAssertion {
	t.Helper()
	private, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	require.NoError(t, err)
	signer, err := jose.NewSigner(jose.SigningKey{Algorithm: jose.ES256, Key: private},
		(&jose.SignerOptions{}).WithType(jose.ContentType(typ)).WithHeader(jose.HeaderKey("kid"), "key-1"))
	require.NoError(t, err)
	return testAssertion{
		signer: signer,
		key:    &jose.JSONWebKey{Key: private.Public(), KeyID: "key-1", Algorithm: string(jose.ES256), Use: "sig"},
	}
}

func validTestClaims() jwt.Claims {
	now := time.Now()
	return jwt.Claims{
		Issuer: testIssuer, Subject: "enterprise-user-1", Audience: jwt.Audience{testAudience},
		Expiry: jwt.NewNumericDate(now.Add(2 * time.Minute)), NotBefore: jwt.NewNumericDate(now.Add(-time.Minute)),
		IssuedAt: jwt.NewNumericDate(now.Add(-time.Minute)), ID: uuid.NewString(),
	}
}

func validTestExtra() additionalClaims {
	return additionalClaims{Resource: resourceClaim(testResource), ClientID: testClientID, Email: "person@example.com", Scope: "read"}
}

func resourceClaim(value string) json.RawMessage {
	raw, _ := json.Marshal(value)
	return raw
}

func signTestAssertion(t *testing.T, signer jose.Signer, claims jwt.Claims, extra additionalClaims) string {
	t.Helper()
	raw, err := jwt.Signed(signer).Claims(claims).Claims(extra).Serialize()
	require.NoError(t, err)
	return raw
}

func newTestValidator(t *testing.T, key *jose.JSONWebKey) (*Validator, *testStore, *testKeys, *testGuard, Request) {
	t.Helper()
	store := &testStore{
		trusted: TrustedIssuer{ID: uuid.New(), Issuer: testIssuer, JWKSURI: "https://idp.example.com/jwks"},
		user:    "gram-user-1", trustErr: nil, resolveErr: nil, resolveCalls: 0,
	}
	keys := &testKeys{key: key, err: nil, calls: 0}
	guard := &testGuard{claimed: make(map[string]bool), calls: 0}
	validator, err := NewValidator(keys, guard, store)
	require.NoError(t, err)
	request := Request{
		OrganizationID: "org-test", UserSessionIssuerID: uuid.New(),
		Audience: testAudience, Resource: testResource, ClientID: testClientID,
	}
	return validator, store, keys, guard, request
}

func TestValidateAcceptsProvisionedIDJAGAndRejectsReplay(t *testing.T) {
	t.Parallel()
	assertion := newTestAssertion(t, Type)
	validator, store, _, guard, request := newTestValidator(t, assertion.key)
	raw := signTestAssertion(t, assertion.signer, validTestClaims(), validTestExtra())
	result, err := validator.Validate(t.Context(), raw, request)
	require.NoError(t, err)
	require.Equal(t, urn.NewUserSubject("gram-user-1"), result.Subject)
	require.Equal(t, store.trusted.ID, result.TrustedIssuerID)
	require.Equal(t, "enterprise-user-1", result.Claims.ExternalSubject)
	require.Equal(t, testResource, result.Claims.Resource)
	require.Equal(t, 1, guard.calls)
	_, err = validator.Validate(t.Context(), raw, request)
	require.Equal(t, ReasonReplayed, ReasonOf(err))
}

func TestIsOutage(t *testing.T) {
	t.Parallel()

	down := &pgconn.PgError{Code: "57P01"}
	broken := &pgconn.PgError{Code: "42P01"}
	for _, tc := range []struct {
		name string
		err  error
		want bool
	}{
		{name: "directory down", err: reject(ReasonSubjectUnavailable, down), want: true},
		{name: "directory query broken", err: reject(ReasonSubjectUnavailable, broken), want: false},
		{name: "replay store down", err: reject(ReasonReplayStoreUnavailable, down), want: true},
		{name: "keys down", err: reject(ReasonKeyUnresolvable, errors.New("jwks")), want: true},
		{name: "local key budget spent", err: reject(ReasonKeyBudgetExhausted, errors.New("budget")), want: true},
		{name: "key store down", err: reject(ReasonKeyStoreUnavailable, down), want: true},
		{name: "key store query broken", err: reject(ReasonKeyStoreUnavailable, broken), want: false},
		{name: "unusable keys", err: reject(ReasonKeyUnusable, errors.New("alg mismatch")), want: false},
		{name: "trust read down", err: reject(ReasonTrustUnavailable, down), want: true},
		{name: "trust query broken", err: reject(ReasonTrustUnavailable, broken), want: false},
		{name: "no trust link", err: reject(ReasonTrustUnavailable, ErrNoTrustedIssuer), want: false},
		{name: "trust link has no keys", err: reject(ReasonTrustUnavailable, ErrTrustedIssuerHasNoJWKS), want: false},
		{name: "replayed", err: reject(ReasonReplayed, errors.New("jti")), want: false},
		{name: "bad signature", err: reject(ReasonSignatureInvalid, errors.New("sig")), want: false},
		{name: "misconfigured", err: reject(ReasonVerifierMisconfigured, errors.New("cfg")), want: false},
		{name: "another error", err: errors.New("other"), want: false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			require.Equal(t, tc.want, IsOutage(tc.err))
		})
	}
}

// Spending our own fetch budget isn't the customer's outage, so its reason stays apart from theirs.
func TestValidateMapsFetchBudgetApartFromKeySetOutage(t *testing.T) {
	t.Parallel()

	require.NotEqual(t, ReasonKeyUnresolvable, ReasonKeyBudgetExhausted)
	require.True(t, IsOutage(reject(ReasonKeyBudgetExhausted, jwks.ErrFetchRateLimited)))
}

func TestIsStoreFault(t *testing.T) {
	t.Parallel()

	require.True(t, IsStoreFault(reject(ReasonSubjectUnavailable, &pgconn.PgError{Code: "42P01"})))
	require.False(t, IsStoreFault(reject(ReasonSubjectUnavailable, &pgconn.PgError{Code: "57P01"})), "an outage")
	require.False(t, IsStoreFault(reject(ReasonTrustUnavailable, ErrNoTrustedIssuer)), "configuration")
	require.False(t, IsStoreFault(reject(ReasonSignatureInvalid, errors.New("sig"))), "the assertion")
}

func TestValidateChecksTypeBeforeKeys(t *testing.T) {
	t.Parallel()
	assertion := newTestAssertion(t, "JWT")
	validator, _, keys, guard, request := newTestValidator(t, assertion.key)
	raw := signTestAssertion(t, assertion.signer, validTestClaims(), validTestExtra())
	_, err := validator.Validate(t.Context(), raw, request)
	require.Equal(t, ReasonTypeMismatch, ReasonOf(err))
	require.Zero(t, keys.calls)
	require.Zero(t, guard.calls)
}

func TestValidateChecksLinkedIssuerBeforeKeys(t *testing.T) {
	t.Parallel()
	assertion := newTestAssertion(t, Type)
	validator, _, keys, guard, request := newTestValidator(t, assertion.key)
	claims := validTestClaims()
	claims.Issuer = "https://other.example.com"
	raw := signTestAssertion(t, assertion.signer, claims, validTestExtra())
	_, err := validator.Validate(t.Context(), raw, request)
	require.Equal(t, ReasonIssuerMismatch, ReasonOf(err))
	require.Zero(t, keys.calls)
	require.Zero(t, guard.calls)
}

func TestValidateRequiresExactSingleAudience(t *testing.T) {
	t.Parallel()
	assertion := newTestAssertion(t, Type)
	validator, _, _, guard, request := newTestValidator(t, assertion.key)
	claims := validTestClaims()
	claims.Audience = jwt.Audience{testAudience, "https://other.example.com"}
	raw := signTestAssertion(t, assertion.signer, claims, validTestExtra())
	_, err := validator.Validate(t.Context(), raw, request)
	require.Equal(t, ReasonAudienceMismatch, ReasonOf(err))
	require.Zero(t, guard.calls)
}

func TestValidateDoesNotSpendJTIOnDirectoryFailure(t *testing.T) {
	t.Parallel()
	assertion := newTestAssertion(t, Type)
	validator, store, _, guard, request := newTestValidator(t, assertion.key)
	raw := signTestAssertion(t, assertion.signer, validTestClaims(), validTestExtra())
	store.resolveErr = errors.New("database unavailable")
	_, err := validator.Validate(t.Context(), raw, request)
	require.Equal(t, ReasonSubjectUnavailable, ReasonOf(err))
	require.Zero(t, guard.calls)
	store.resolveErr = nil
	_, err = validator.Validate(t.Context(), raw, request)
	require.NoError(t, err)
	require.Equal(t, 1, guard.calls)
}

func TestValidateRejectsUnprovisionedSubject(t *testing.T) {
	t.Parallel()
	assertion := newTestAssertion(t, Type)
	validator, store, _, guard, request := newTestValidator(t, assertion.key)
	store.resolveErr = ErrNotProvisioned
	raw := signTestAssertion(t, assertion.signer, validTestClaims(), validTestExtra())
	_, err := validator.Validate(t.Context(), raw, request)
	require.Equal(t, ReasonNotProvisioned, ReasonOf(err))
	require.Zero(t, guard.calls)
}

func TestValidateRejectsLongLifetime(t *testing.T) {
	t.Parallel()
	assertion := newTestAssertion(t, Type)
	validator, _, _, guard, request := newTestValidator(t, assertion.key)
	claims := validTestClaims()
	claims.Expiry = jwt.NewNumericDate(time.Now().Add(20 * time.Minute))
	raw := signTestAssertion(t, assertion.signer, claims, validTestExtra())
	_, err := validator.Validate(t.Context(), raw, request)
	require.Equal(t, ReasonLifetimeTooLong, ReasonOf(err))
	require.Zero(t, guard.calls)
}

func TestValidateRejectsInvalidClaimsBeforeSpendingJTI(t *testing.T) {
	t.Parallel()
	assertion := newTestAssertion(t, Type)
	cases := []struct {
		name   string
		mutate func(*jwt.Claims, *additionalClaims)
		reason Reason
	}{
		{name: "missing email", mutate: func(_ *jwt.Claims, extra *additionalClaims) { extra.Email = "" }, reason: ReasonEmailMissing},
		{name: "missing expiry", mutate: func(claims *jwt.Claims, _ *additionalClaims) { claims.Expiry = nil }, reason: ReasonExpiryMissing},
		{name: "missing issued at", mutate: func(claims *jwt.Claims, _ *additionalClaims) { claims.IssuedAt = nil }, reason: ReasonIssuedAtMissing},
		{name: "missing jti", mutate: func(claims *jwt.Claims, _ *additionalClaims) { claims.ID = "" }, reason: ReasonIDMissing},
		{name: "expired", mutate: func(claims *jwt.Claims, _ *additionalClaims) {
			claims.Expiry = jwt.NewNumericDate(time.Now().Add(-10 * time.Minute))
		}, reason: ReasonExpired},
		{name: "future nbf", mutate: func(claims *jwt.Claims, _ *additionalClaims) {
			claims.NotBefore = jwt.NewNumericDate(time.Now().Add(10 * time.Minute))
		}, reason: ReasonNotYetValid},
		{name: "future iat", mutate: func(claims *jwt.Claims, _ *additionalClaims) {
			claims.IssuedAt = jwt.NewNumericDate(time.Now().Add(10 * time.Minute))
		}, reason: ReasonNotYetValid},
		{name: "token endpoint audience", mutate: func(claims *jwt.Claims, _ *additionalClaims) {
			claims.Audience = jwt.Audience{"https://gram.example.com/token"}
		}, reason: ReasonAudienceMismatch},
		{name: "missing resource", mutate: func(_ *jwt.Claims, extra *additionalClaims) { extra.Resource = nil }, reason: ReasonResourceNotIndicated},
		{name: "empty resource", mutate: func(_ *jwt.Claims, extra *additionalClaims) { extra.Resource = resourceClaim("") }, reason: ReasonResourceMismatch},
		{name: "null resource", mutate: func(_ *jwt.Claims, extra *additionalClaims) { extra.Resource = json.RawMessage("null") }, reason: ReasonResourceMismatch},
		{name: "non-string resource", mutate: func(_ *jwt.Claims, extra *additionalClaims) { extra.Resource = json.RawMessage("42") }, reason: ReasonResourceMismatch},
		{name: "resource mismatch", mutate: func(_ *jwt.Claims, extra *additionalClaims) {
			extra.Resource = resourceClaim("https://gram.example.com/mcp/other/mcp")
		}, reason: ReasonResourceMismatch},
		{name: "client mismatch", mutate: func(_ *jwt.Claims, extra *additionalClaims) { extra.ClientID = "https://other.example.com/client" }, reason: ReasonClientMismatch},
	}
	for _, testCase := range cases {
		validator, _, _, guard, request := newTestValidator(t, assertion.key)
		claims, extra := validTestClaims(), validTestExtra()
		testCase.mutate(&claims, &extra)
		raw := signTestAssertion(t, assertion.signer, claims, extra)
		_, err := validator.Validate(t.Context(), raw, request)
		require.Equal(t, testCase.reason, ReasonOf(err), testCase.name)
		require.Zero(t, guard.calls, testCase.name)
	}
}

// oktaStyleExtra is an ID-JAG's additional claims as Okta issues them, with no resource claim.
type oktaStyleExtra struct {
	ClientID string `json:"client_id"`
	Email    string `json:"email"`
}

func TestValidateAcceptsAssertionWithoutResourceWhenRequestIndicatesOne(t *testing.T) {
	t.Parallel()
	assertion := newTestAssertion(t, Type)
	validator, _, _, guard, request := newTestValidator(t, assertion.key)
	request.ResourceIndicated = true
	raw, err := jwt.Signed(assertion.signer).Claims(validTestClaims()).Claims(oktaStyleExtra{ClientID: testClientID, Email: "person@example.com"}).Serialize()
	require.NoError(t, err)
	result, err := validator.Validate(t.Context(), raw, request)
	require.NoError(t, err)
	require.Empty(t, result.Claims.Resource)
	require.Equal(t, 1, guard.calls)
}

func TestValidateRejectsAssertionWithoutResourceWhenRequestIndicatesNone(t *testing.T) {
	t.Parallel()
	assertion := newTestAssertion(t, Type)
	validator, _, _, guard, request := newTestValidator(t, assertion.key)
	raw, err := jwt.Signed(assertion.signer).Claims(validTestClaims()).Claims(oktaStyleExtra{ClientID: testClientID, Email: "person@example.com"}).Serialize()
	require.NoError(t, err)
	_, err = validator.Validate(t.Context(), raw, request)
	require.Equal(t, ReasonResourceNotIndicated, ReasonOf(err))
	require.Zero(t, guard.calls)
}

func TestValidateRejectsMismatchedResourceClaimEvenWhenRequestIndicatesOne(t *testing.T) {
	t.Parallel()
	assertion := newTestAssertion(t, Type)
	validator, _, _, guard, request := newTestValidator(t, assertion.key)
	request.ResourceIndicated = true
	extra := validTestExtra()
	extra.Resource = resourceClaim("https://gram.example.com/mcp/other/mcp")
	raw := signTestAssertion(t, assertion.signer, validTestClaims(), extra)
	_, err := validator.Validate(t.Context(), raw, request)
	require.Equal(t, ReasonResourceMismatch, ReasonOf(err))
	require.Zero(t, guard.calls)
}

// A present but empty or null resource claim never falls back to the request's resource.
func TestValidateRejectsEmptyOrNullResourceClaimWhenRequestIndicatesOne(t *testing.T) {
	t.Parallel()
	assertion := newTestAssertion(t, Type)
	for name, claim := range map[string]json.RawMessage{"empty": resourceClaim(""), "null": json.RawMessage("null")} {
		validator, _, _, guard, request := newTestValidator(t, assertion.key)
		request.ResourceIndicated = true
		extra := validTestExtra()
		extra.Resource = claim
		raw := signTestAssertion(t, assertion.signer, validTestClaims(), extra)
		_, err := validator.Validate(t.Context(), raw, request)
		require.Equal(t, ReasonResourceMismatch, ReasonOf(err), name)
		require.Zero(t, guard.calls, name)
	}
}
