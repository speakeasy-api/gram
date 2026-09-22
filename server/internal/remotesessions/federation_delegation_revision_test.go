package remotesessions

import (
	"context"
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/stretchr/testify/require"
)

func TestFederatedDelegationSameSetKeyRotationChangesRevision(t *testing.T) {
	t.Parallel()
	p := federatedFixture(t)
	p.signingKeyRevision = "active-key-version-one"
	registration, offline := p.DelegationConfigurationHash(), p.OfflineConfigurationHash()
	fingerprint := p.Fingerprint()
	require.NotEmpty(t, registration)
	require.NotEmpty(t, offline)
	p.signingKeyRevision = "active-key-version-two"
	require.NotEqual(t, fingerprint, p.Fingerprint(), "same-set rotation must invalidate pending callbacks")
	require.NotEqual(t, registration, p.DelegationConfigurationHash())
	require.NotEqual(t, offline, p.OfflineConfigurationHash())
	rotated := p.DelegationConfigurationHash()
	p.organizationID = "another-organization"
	require.NotEqual(t, rotated, p.DelegationConfigurationHash(), "retained credentials must remain tenant-bound")
}

func TestDelegationLoginExplicitExpiredRefreshIsNotDurable(t *testing.T) {
	t.Parallel()
	s, store, p, b, _ := newDelegationUnitFixture(t)
	identity := delegationLogin(p, s.now(), "valid-id", "expired-refresh", time.Hour)
	expiry := s.now()
	identity.credentials.value.refreshExpiresAt = &expiry
	require.NoError(t, s.RetainVerifiedLogin(t.Context(), p, b.HumanID, identity, true))
	credential, err := store.load(t.Context(), b)
	require.NoError(t, err)
	require.Empty(t, credential.RefreshTokenEncrypted.String)
	require.Equal(t, "refused", credential.ObservationStatus.String)
	require.NotEmpty(t, credential.IdentityAssertionEncrypted.String)
}

func TestFederatedSigningRevisionDependencyErrors(t *testing.T) {
	t.Parallel()
	require.ErrorIs(t, federatedSigningRevisionError(pgx.ErrNoRows), ErrFederatedConfiguration)
	require.ErrorIs(t, federatedSigningRevisionError(fmt.Errorf("wrapped: %w", pgx.ErrNoRows)), ErrFederatedConfiguration)
	for _, cause := range []error{context.Canceled, context.DeadlineExceeded, errors.New("database unavailable")} {
		err := federatedSigningRevisionError(cause)
		require.ErrorIs(t, err, cause)
		require.NotErrorIs(t, err, ErrFederatedConfiguration)
	}
}
