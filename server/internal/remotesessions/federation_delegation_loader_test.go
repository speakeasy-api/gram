package remotesessions

import (
	"context"
	"crypto/x509"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"sync/atomic"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/speakeasy-api/gram/server/internal/dns"
	"github.com/speakeasy-api/gram/server/internal/guardian"
	"github.com/speakeasy-api/gram/server/internal/testenv"
	"github.com/speakeasy-api/gram/server/internal/testenv/testrepo"
	"github.com/stretchr/testify/require"
)

func TestFederatedDelegationLoaderDatabaseOnly(t *testing.T) {
	t.Parallel()
	ctx := t.Context()
	container, clone, err := testenv.NewTestPostgres(ctx)
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, container.Terminate(context.Background())) })
	db, err := clone(t, "delegation_loader")
	require.NoError(t, err)

	var requests, resolutions atomic.Int64
	upstream := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		requests.Add(1)
		w.WriteHeader(http.StatusServiceUnavailable)
	}))
	t.Cleanup(upstream.Close)
	roots := x509.NewCertPool()
	roots.AddCert(upstream.Certificate())
	upstreamURL, err := url.Parse(upstream.URL)
	require.NoError(t, err)
	require.NotEmpty(t, upstream.Certificate().DNSNames)
	upstreamURL.Host = net.JoinHostPort(upstream.Certificate().DNSNames[0], upstreamURL.Port())
	policy, err := guardian.NewUnsafePolicy(testenv.NewTracerProvider(t), nil, guardian.WithTLSRootCAs(roots), guardian.WithResolver(dns.NewMockResolver(dns.MockResolverConfig{
		LookupIPFunc: func(context.Context, string, string) ([]net.IP, error) {
			resolutions.Add(1)
			return []net.IP{net.ParseIP("127.0.0.1")}, nil
		},
	})))
	require.NoError(t, err)
	// Positive control: the exact policy used by the manager must reach the
	// TLS handler and resolver, rather than silently failing before either counter.
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, upstreamURL.String(), nil)
	require.NoError(t, err)
	response, err := policy.Client().Do(request)
	require.NoError(t, err)
	require.NoError(t, response.Body.Close())
	require.Equal(t, http.StatusServiceUnavailable, response.StatusCode)
	require.Equal(t, int64(1), requests.Load())
	require.Positive(t, resolutions.Load())
	requests.Store(0)
	resolutions.Store(0)
	manager := &ChallengeManager{db: db, policy: policy}
	const org = "org_delegation_loader_test"
	const otherOrg = "org_delegation_loader_other"
	issuer, client, otherIssuer, otherClient := uuid.New(), uuid.New(), uuid.New(), uuid.New()
	fixtures := testrepo.New(db)
	for _, row := range []testrepo.SeedDelegationLoaderOrganizationFixtureParams{
		{OrganizationID: org, Name: "Loader test", Slug: "loader-test"},
		{OrganizationID: otherOrg, Name: "Other test", Slug: "loader-other"},
	} {
		require.NoError(t, fixtures.SeedDelegationLoaderOrganizationFixture(ctx, row))
	}
	globalIssuer, globalClient := uuid.New(), uuid.New()
	for _, row := range []testrepo.SeedDelegationLoaderIssuerFixtureParams{
		{ID: issuer, OrganizationID: pgtype.Text{String: org, Valid: true}, Slug: "loader-test", Issuer: upstreamURL.String(), AuthorizationEndpoint: pgtype.Text{String: upstreamURL.String() + "/authorize", Valid: true}, TokenEndpoint: pgtype.Text{String: upstreamURL.String() + "/token", Valid: true}, JwksUri: pgtype.Text{String: upstreamURL.String() + "/jwks", Valid: true}},
		{ID: otherIssuer, OrganizationID: pgtype.Text{String: otherOrg, Valid: true}, Slug: "loader-other", Issuer: upstreamURL.String() + "/other", AuthorizationEndpoint: pgtype.Text{String: upstreamURL.String() + "/authorize", Valid: true}, TokenEndpoint: pgtype.Text{String: upstreamURL.String() + "/token", Valid: true}, JwksUri: pgtype.Text{String: upstreamURL.String() + "/jwks", Valid: true}},
		{ID: globalIssuer, Slug: "loader-global", Issuer: "https://global.example.test"},
	} {
		require.NoError(t, fixtures.SeedDelegationLoaderIssuerFixture(ctx, row))
	}
	orgClientForeignIssuer, foreignClientOrgIssuer := uuid.New(), uuid.New()
	for _, row := range []testrepo.SeedDelegationLoaderClientFixtureParams{
		{ID: client, OrganizationID: org, RemoteSessionIssuerID: issuer, ClientID: "loader-client", Scope: []string{"openid", "email"}},
		{ID: otherClient, OrganizationID: otherOrg, RemoteSessionIssuerID: otherIssuer, ClientID: "other-client", Scope: []string{"openid", "email"}},
		{ID: globalClient, OrganizationID: org, RemoteSessionIssuerID: globalIssuer, ClientID: "global-client", Scope: []string{"openid", "email"}},
		{ID: orgClientForeignIssuer, OrganizationID: org, RemoteSessionIssuerID: otherIssuer, ClientID: "cross-issuer", Scope: []string{"openid"}},
		{ID: foreignClientOrgIssuer, OrganizationID: otherOrg, RemoteSessionIssuerID: issuer, ClientID: "cross-client", Scope: []string{"openid"}},
	} {
		require.NoError(t, fixtures.SeedDelegationLoaderClientFixture(ctx, row))
	}
	global, err := manager.LoadFederatedDelegationProvider(ctx, org, globalIssuer, globalClient)
	require.NoError(t, err)
	require.NotEmpty(t, global.DelegationConfigurationHash())
	require.Equal(t, globalClient, global.client.ID)

	// This loader supplies registration state only. Authority and credential
	// repository operations separately enforce live trust and organization state.
	provider, err := manager.LoadFederatedDelegationProvider(ctx, org, issuer, client)
	require.NoError(t, err)
	require.NotEmpty(t, provider.DelegationConfigurationHash())
	require.Equal(t, client, provider.client.ID)
	for _, tc := range []struct {
		name, organization string
		issuer, client     uuid.UUID
	}{
		{"wrong tenant", otherOrg, issuer, client},
		{"wrong issuer pair", org, otherIssuer, client},
		{"foreign client", org, otherIssuer, otherClient},
		{"own client foreign issuer", org, otherIssuer, orgClientForeignIssuer},
		{"foreign client own issuer", org, issuer, foreignClientOrgIssuer},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			provider, err := manager.LoadFederatedDelegationProvider(ctx, tc.organization, tc.issuer, tc.client)
			require.Nil(t, provider)
			require.ErrorIs(t, err, ErrFederatedConfiguration)
		})
	}
	canceled, cancel := context.WithCancel(ctx)
	cancel()
	provider, err = manager.LoadFederatedDelegationProvider(canceled, org, issuer, client)
	require.Nil(t, provider)
	require.ErrorIs(t, err, context.Canceled)
	require.NotErrorIs(t, err, ErrFederatedConfiguration)
	// Cleanup runs after the parallel subtests, so their network attempts are
	// included too, and before the upstream server is closed.
	t.Cleanup(func() {
		require.Zero(t, requests.Load(), "registration lookup must not contact upstream HTTP endpoints")
		require.Zero(t, resolutions.Load(), "registration lookup must not resolve upstream hosts")
	})
}
