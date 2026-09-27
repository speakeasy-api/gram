package mcp

import (
	"context"
	"crypto/rand"
	"crypto/rsa"
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/go-jose/go-jose/v4"
	"github.com/go-jose/go-jose/v4/jwt"
	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/usersessions/assertion/workload"
	"github.com/speakeasy-api/gram/server/internal/usersessions/jwks"
	"github.com/speakeasy-api/gram/server/internal/usersessions/replay"
	workloadidentity_repo "github.com/speakeasy-api/gram/server/internal/workloadidentity/repo"
)

const workloadGrantTestIssuer = "https://idp.example.test"

// failingWorkloadKeys answers every key lookup with err.
type failingWorkloadKeys struct{ err error }

func (k failingWorkloadKeys) VerificationKeyForAlgorithm(context.Context, jwks.Source, string, jose.SignatureAlgorithm) (*jose.JSONWebKey, error) {
	return nil, k.err
}

// unusedReplayGuard holds long enough for any assertion and is never reached
// by a test that fails before the replay stage.
type unusedReplayGuard struct{}

func (unusedReplayGuard) MaxHold() time.Duration { return 24 * time.Hour }
func (unusedReplayGuard) Reserve(context.Context, replay.Key, time.Time) (bool, error) {
	return false, errors.New("replay reservation is not expected")
}

// admitWithFailingKeys runs the grant's stages against a trusted issuer whose
// key lookup fails with keyErr.
func admitWithFailingKeys(t *testing.T, keyErr error) error {
	t.Helper()

	verifier, err := workload.NewVerifier(failingWorkloadKeys{err: keyErr}, unusedReplayGuard{})
	require.NoError(t, err)
	lookup := &countingLookup{issuer: workloadidentity_repo.WorkloadIssuer{
		ID:      uuid.New(),
		Name:    "test issuer",
		Issuer:  workloadGrantTestIssuer,
		JwksUri: workloadGrantTestIssuer + "/jwks",
	}, found: true}
	grant := &workloadGrant{
		issuers:    newWorkloadTestAdmission(t, lookup.fn(), allowAllWorkloadLookups),
		identities: newStaticWorkloadIdentityLookup(),
		verifier:   verifier,
	}

	key, err := rsa.GenerateKey(rand.Reader, 2048)
	require.NoError(t, err)
	signer, err := jose.NewSigner(jose.SigningKey{Algorithm: jose.RS256, Key: key}, (&jose.SignerOptions{}).WithHeader(jose.HeaderKey("kid"), "rotated-key"))
	require.NoError(t, err)
	audience := "https://gram.example.test/mcp/workload"
	now := time.Now()
	raw, err := jwt.Signed(signer).Claims(jwt.Claims{
		Issuer:   workloadGrantTestIssuer,
		Subject:  "repo:acme/app:ref:refs/heads/main",
		Audience: jwt.Audience{audience},
		Expiry:   jwt.NewNumericDate(now.Add(time.Minute)),
		IssuedAt: jwt.NewNumericDate(now),
		ID:       uuid.NewString(),
	}).Serialize()
	require.NoError(t, err)

	_, err = admitWorkloadAssertion(t.Context(), grant, workloadTestTenant(), []string{audience}, raw)
	return err
}

// A key set that could not be consulted for budget decides nothing about the
// assertion's kid, so the grant answers retryably rather than invalid_grant.
func TestAdmitWorkloadAssertion_KeyRateLimitIsRetryable(t *testing.T) {
	t.Parallel()

	for _, limit := range []error{jwks.ErrFetchRateLimited, jwks.ErrRefreshRateLimited} {
		err := admitWithFailingKeys(t, fmt.Errorf("resolve key: %w", limit))

		refusal, ok := errors.AsType[*workloadGrantError](err)
		require.True(t, ok, "%v", err)
		require.Equal(t, workloadGrantUnavailable, refusal.outcome, "%v", limit)
		require.Equal(t, "issuer_keys_rate_limited", refusal.reason)
	}
}

// A kid the issuer does not publish is a verdict on the assertion.
func TestAdmitWorkloadAssertion_UnknownKeyIsRefused(t *testing.T) {
	t.Parallel()

	err := admitWithFailingKeys(t, fmt.Errorf("resolve key: %w", jwks.ErrKeyNotFound))

	refusal, ok := errors.AsType[*workloadGrantError](err)
	require.True(t, ok, "%v", err)
	require.Equal(t, workloadGrantRefused, refusal.outcome)
	require.Equal(t, string(workload.ReasonKeyUnknown), refusal.reason)
}
