package remotesessions

import (
	"context"
	"net"
	"net/http"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgtype"
	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/conv"
	"github.com/speakeasy-api/gram/server/internal/dns"
	"github.com/speakeasy-api/gram/server/internal/guardian"
	"github.com/speakeasy-api/gram/server/internal/remotesessions/repo"
	"github.com/speakeasy-api/gram/server/internal/testenv"
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

// A fault on our side sends nothing, so it can't count as the provider refusing the token.
func TestDelegationLocalSetupFaultNeverDropsTheToken(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name    string
		breakIt func(*FederatedProvider)
	}{
		{name: "metadata no longer matches the issuer", breakIt: func(p *FederatedProvider) { p.metadata.Issuer = "https://other.example.test" }},
		{name: "client secret does not decrypt", breakIt: func(p *FederatedProvider) {
			p.client.ClientSecretEncrypted = pgtype.Text{String: "not-a-ciphertext", Valid: true}
		}},
		{name: "signing key missing", breakIt: func(p *FederatedProvider) {
			p.client.TokenEndpointAuthMethod = pgtype.Text{String: string(TokenEndpointAuthMethodPrivateKeyJWT), Valid: true}
			p.client.ClientSecretEncrypted = pgtype.Text{}
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			s, store, b, allow, endpoint, _ := rejectionFixture(t, nil)
			endpoint.answer(http.StatusForbidden, `<html>blocked</html>`)
			_, err := s.Resolve(t.Context(), b, allow)
			require.ErrorIs(t, err, ErrDelegationConfiguration)
			require.Len(t, endpoint.received(), 1)

			refresh := s.refreshIdentity
			s.refreshIdentity = func(ctx context.Context, p *FederatedProvider, token, subject, nonce string) (*FederatedRefreshResult, error) {
				broken := *p
				tc.breakIt(&broken)
				return refresh(ctx, &broken, token, subject, nonce)
			}
			ageRefusal(t, store, b)
			moveDelegationClock(s)
			_, err = s.Resolve(t.Context(), b, allow)
			require.ErrorIs(t, err, ErrDelegationConfiguration)
			require.Len(t, endpoint.received(), 1, "the fault stops the request before it is sent")
			row, err := store.load(t.Context(), b)
			require.NoError(t, err)
			require.Equal(t, "configuration_failure", row.ObservationStatus.String)
			require.Equal(t, "secret-refresh", delegationPlain(t, s, row.RefreshTokenEncrypted.String), "the provider never refused this token")
		})
	}
}

func TestFederatedRefreshHostLookupOutageIsRetryable(t *testing.T) {
	t.Parallel()
	cause := &net.DNSError{Err: "temporary resolver outage", Name: "idp.example.test", IsTemporary: true}
	policy := guardian.NewDefaultPolicy(testenv.NewTracerProvider(t), guardian.WithResolver(dns.NewMockResolver(dns.MockResolverConfig{
		LookupIPFunc: func(context.Context, string, string) ([]net.IP, error) { return nil, cause },
	})))
	m := &ChallengeManager{policy: policy, logger: testenv.NewLogger(t)}
	_, err := m.RefreshFederatedIdentity(t.Context(), federatedFixture(t), "refresh", "subject", "nonce")
	require.ErrorIs(t, err, &FederatedRefreshError{Kind: FederatedRefreshSetupRetryable})

	s, store, b, allow, _, _ := rejectionFixture(t, nil)
	s.refreshIdentity = func(ctx context.Context, _ *FederatedProvider, token, subject, nonce string) (*FederatedRefreshResult, error) {
		return m.RefreshFederatedIdentity(ctx, federatedFixture(t), token, subject, nonce)
	}
	_, err = s.Resolve(t.Context(), b, allow)
	require.ErrorIs(t, err, ErrDelegationTemporary)
	row, err := store.load(t.Context(), b)
	require.NoError(t, err)
	require.Equal(t, "temporary_failure", row.ObservationStatus.String)
	require.False(t, row.RefreshClaimID.Valid, "nothing was sent, so nothing can have rotated")
	require.Equal(t, "secret-refresh", delegationPlain(t, s, row.RefreshTokenEncrypted.String))
}
