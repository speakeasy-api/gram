package remotesessions

import (
	"net/http"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/conv"
	"github.com/speakeasy-api/gram/server/internal/remotesessions/repo"
)

func ageRefusal(t *testing.T, store *delegationAdapterStore, b DelegationBinding) {
	t.Helper()
	row, err := store.load(t.Context(), b)
	require.NoError(t, err)
	affected, err := repo.New(store.db).SetTrustedDelegationStateFixture(t.Context(), repo.SetTrustedDelegationStateFixtureParams{
		OrganizationID: b.OrganizationID, ClientID: b.ClientID,
		ObservedAt: conv.ToPGTimestamptz(row.ObservedAt.Time.Add(-73 * time.Hour)),
	})
	require.NoError(t, err)
	require.EqualValues(t, 1, affected)
}

func TestDelegationDeadTokenWithOddCodeMovesToSignInAfterTheLimit(t *testing.T) {
	t.Parallel()
	s, store, b, allow, endpoint, _ := rejectionFixture(t, nil)
	endpoint.answer(http.StatusUnauthorized, `{"error":"invalid_token"}`)
	_, err := s.Resolve(t.Context(), b, allow)
	require.ErrorIs(t, err, ErrDelegationConfiguration)
	row, err := store.load(t.Context(), b)
	require.NoError(t, err)
	require.Equal(t, "secret-refresh", delegationPlain(t, s, row.RefreshTokenEncrypted.String), "the code alone doesn't prove the token is dead")

	ageRefusal(t, store, b)
	moveDelegationClock(s)
	_, err = s.Resolve(t.Context(), b, allow)
	require.ErrorIs(t, err, ErrDelegationReauthentication)
	require.Len(t, endpoint.received(), 2, "past the limit one more request still tests the token")
	row, err = store.load(t.Context(), b)
	require.NoError(t, err)
	require.Equal(t, "reauthentication_required", row.ObservationStatus.String)
	require.False(t, row.RefreshTokenEncrypted.Valid)
}

func TestDelegationKeptTokenRecoversAfterTheLimitWhenTheProviderIsFixed(t *testing.T) {
	t.Parallel()
	s, store, b, allow, endpoint, _ := rejectionFixture(t, nil)
	endpoint.answer(http.StatusForbidden, `<html>blocked</html>`)
	_, err := s.Resolve(t.Context(), b, allow)
	require.ErrorIs(t, err, ErrDelegationConfiguration)

	ageRefusal(t, store, b)
	endpoint.answer(http.StatusOK, `{"access_token":"secret-access","refresh_token":"secret-rotated"}`)
	moveDelegationClock(s)
	_, err = s.Resolve(t.Context(), b, allow)
	require.ErrorIs(t, err, ErrDelegationReauthentication, "this response has no ID token to renew the assertion with")
	row, err := store.load(t.Context(), b)
	require.NoError(t, err)
	require.Equal(t, "secret-rotated", delegationPlain(t, s, row.RefreshTokenEncrypted.String), "the token was never dropped")
}

func TestDelegationClientRejectionNeverDropsTheToken(t *testing.T) {
	t.Parallel()
	s, store, b, allow, endpoint, _ := rejectionFixture(t, nil)
	endpoint.answer(http.StatusUnauthorized, `{"error":"invalid_client"}`)
	_, err := s.Resolve(t.Context(), b, allow)
	require.ErrorIs(t, err, ErrDelegationConfiguration)

	ageRefusal(t, store, b)
	moveDelegationClock(s)
	_, err = s.Resolve(t.Context(), b, allow)
	require.ErrorIs(t, err, ErrDelegationConfiguration)
	require.Len(t, endpoint.received(), 2)
	row, err := store.load(t.Context(), b)
	require.NoError(t, err)
	require.Equal(t, "configuration_failure", row.ObservationStatus.String)
	require.Equal(t, "secret-refresh", delegationPlain(t, s, row.RefreshTokenEncrypted.String))
}
