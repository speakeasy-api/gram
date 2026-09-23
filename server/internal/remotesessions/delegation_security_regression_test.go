package remotesessions

import (
	"context"
	"io"
	"net"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/dns"
	"github.com/speakeasy-api/gram/server/internal/guardian"
	"github.com/speakeasy-api/gram/server/internal/remotesessions/repo"
	"github.com/speakeasy-api/gram/server/internal/testenv"
	"github.com/speakeasy-api/gram/server/internal/testenv/testrepo"
	"github.com/speakeasy-api/gram/server/internal/urn"
)

func TestDelegationRefreshRechecksLiveAuthorityAndScopesRelease(t *testing.T) {
	t.Parallel()
	for _, mutation := range []struct {
		name  string
		apply func(*testrepo.Queries, context.Context, string) error
	}{
		{"organization_metadata", (*testrepo.Queries).DisableDelegationOrganizationFixture},
		{"user_session_issuers", (*testrepo.Queries).RevokeDelegationUserIssuersFixture},
		{"organization_user_relationships", (*testrepo.Queries).ForceSoftDeleteOrganizationUserRelationshipsFixture},
		{"remote_session_clients", (*testrepo.Queries).RevokeDelegationClientsFixture},
		{"remote_session_issuers", (*testrepo.Queries).RevokeDelegationIssuersFixture},
	} {
		t.Run(mutation.name, func(t *testing.T) {
			t.Parallel()
			s, store, p, b, allow := newDelegationUnitFixture(t)
			require.NoError(t, s.RetainVerifiedLogin(t.Context(), p, b.HumanID, delegationLogin(p, s.now(), "old-id", "old-refresh", 30*time.Second), true))
			s.loadProvider = func(ctx context.Context, _ string, _, _ uuid.UUID) (*FederatedProvider, error) {
				row, err := store.load(ctx, b)
				require.NoError(t, err)
				require.True(t, row.RefreshClaimID.Valid)
				count, err := repo.New(s.db).ReleaseTrustedDelegationRefresh(ctx, repo.ReleaseTrustedDelegationRefreshParams{
					OrganizationID: b.OrganizationID, IssuerID: uuid.New(), ClientID: b.ClientID,
					SubjectUrn: urn.NewUserSubject(b.HumanID).String(), ExpectedGeneration: row.CredentialGeneration.Int64, RefreshClaimID: row.RefreshClaimID.UUID,
				})
				require.NoError(t, err)
				require.Zero(t, count, "another issuer must not release this claim")
				err = mutation.apply(testrepo.New(s.db), ctx, b.OrganizationID)
				require.NoError(t, err)
				return p, nil
			}
			posts := 0
			s.refreshIdentity = func(context.Context, *FederatedProvider, string, string, string) (*FederatedRefreshResult, error) {
				posts++
				return nil, nil
			}
			_, err := s.Resolve(t.Context(), b, allow)
			require.ErrorIs(t, err, ErrDelegationTemporary)
			require.Zero(t, posts)
			claim, err := testrepo.New(s.db).GetDelegationRefreshClaimFixture(t.Context(), testrepo.GetDelegationRefreshClaimFixtureParams{
				OrganizationID: b.OrganizationID, ClientID: b.ClientID, Subject: urn.NewUserSubject(b.HumanID).String(),
			})
			require.NoError(t, err)
			require.False(t, claim.RefreshClaimID.Valid, "correct issuer must release even after revocation")
			require.False(t, claim.LastRefreshAttemptAt.Valid)
		})
	}
}

func TestDelegationVerificationFailureQuarantinesRotation(t *testing.T) {
	t.Parallel()
	for _, kind := range []FederatedRefreshFailure{FederatedRefreshAmbiguous, FederatedRefreshInvalidIdentity, FederatedRefreshConfiguration} {
		t.Run(string(kind), func(t *testing.T) {
			t.Parallel()
			s, store, p, b, allow := newDelegationUnitFixture(t)
			require.NoError(t, s.RetainVerifiedLogin(t.Context(), p, b.HumanID, delegationLogin(p, s.now(), "old-id", "old-refresh", 30*time.Second), true))
			posts := 0
			s.refreshIdentity = func(context.Context, *FederatedProvider, string, string, string) (*FederatedRefreshResult, error) {
				posts++
				return delegationRenewal(p, s.now(), "unverified-id", "rotated-refresh"), &FederatedRefreshError{Kind: kind}
			}
			assertion, err := s.Resolve(t.Context(), b, allow)
			require.Empty(t, assertion.Value())
			row, loadErr := store.load(t.Context(), b)
			require.NoError(t, loadErr)
			require.Empty(t, row.IdentityAssertionEncrypted.String)
			if kind == FederatedRefreshAmbiguous {
				require.ErrorIs(t, err, ErrDelegationTemporary)
				require.True(t, row.RefreshClaimID.Valid)
				require.NotEqual(t, "rotated-refresh", row.RefreshTokenEncrypted.String)
				require.Equal(t, "rotated-refresh", delegationPlain(t, s, row.RefreshTokenEncrypted.String))
				require.False(t, row.LastRefreshSucceededAt.Valid)
				later := s.now().Add(24 * time.Hour)
				s.now = func() time.Time { return later }
				_, err = s.Resolve(t.Context(), b, allow)
				require.ErrorIs(t, err, ErrDelegationTemporary)
			} else {
				require.ErrorIs(t, err, ErrDelegationConfiguration)
				require.Empty(t, row.RefreshTokenEncrypted.String)
				require.Empty(t, row.UpstreamSubjectEncrypted.String)
				require.Empty(t, row.NonceEncrypted.String)
				require.Equal(t, "configuration_failure", row.ObservationStatus.String)
				require.True(t, row.ObservedAt.Valid)
			}
			require.Equal(t, 1, posts)
		})
	}
}

func TestFederatedMetadataClassifiesDependencyFailures(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name   string
		status int
		cause  error
		want   error
	}{
		{"timeout", 0, context.DeadlineExceeded, ErrFederatedUnavailable},
		{"unavailable", 503, nil, ErrFederatedUnavailable},
		{"invalid registration", 404, nil, ErrFederatedConfiguration},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			p := federatedFixture(t)
			m := &ChallengeManager{policy: federatedPublicPolicy(t)}
			_, err := m.loadFederatedMetadata(t.Context(), p.organizationID, p.issuer, federatedHTTPDoerFunc(func(*http.Request) (*http.Response, error) {
				if tc.cause != nil {
					return nil, tc.cause
				}
				return &http.Response{StatusCode: tc.status, Body: io.NopCloser(strings.NewReader("unavailable"))}, nil
			}))
			require.ErrorIs(t, err, tc.want)
			if tc.cause != nil {
				require.ErrorIs(t, err, tc.cause)
			}
		})
	}
}

func TestFederatedProviderDatabaseTimeoutIsNotConfiguration(t *testing.T) {
	t.Parallel()
	s, _, _, b, _ := newDelegationUnitFixture(t)
	ctx, cancel := context.WithDeadline(t.Context(), time.Now().Add(-time.Second))
	defer cancel()
	m := &ChallengeManager{db: s.db}
	_, err := m.LoadFederatedProvider(ctx, b.OrganizationID, b.IssuerID, b.ClientID)
	require.ErrorIs(t, err, context.DeadlineExceeded)
	require.NotErrorIs(t, err, ErrFederatedConfiguration)
}

func TestFederatedMetadataDNSDeadlineIsTemporary(t *testing.T) {
	t.Parallel()
	policy := guardian.NewDefaultPolicy(testenv.NewTracerProvider(t), guardian.WithResolver(dns.NewMockResolver(dns.MockResolverConfig{
		LookupIPFunc: func(context.Context, string, string) ([]net.IP, error) { return nil, context.DeadlineExceeded },
	})))
	m := &ChallengeManager{policy: policy}
	err := m.validateFederatedHost(t.Context(), "https://idp.example.test", false)
	require.ErrorIs(t, err, ErrFederatedUnavailable)
	require.ErrorIs(t, err, context.DeadlineExceeded)
	require.NotErrorIs(t, err, ErrFederatedConfiguration)
}
