package idjag

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

func (k *testKeys) VerificationKey(_ context.Context, _ jwks.Source, _ string) (*jose.JSONWebKey, error) {
	k.calls++
	return k.key, k.err
}

type testGuard struct {
	claimed map[string]bool
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
	return additionalClaims{Resource: testResource, ClientID: testClientID, Email: "person@example.com", Scope: "read"}
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
	require.Equal(t, 1, guard.calls)
	_, err = validator.Validate(t.Context(), raw, request)
	require.Equal(t, ReasonReplayed, ReasonOf(err))
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
