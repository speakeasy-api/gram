//nolint:glint // Database fixtures exercise the loader's tenant and issuer predicates.
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
	"github.com/speakeasy-api/gram/server/internal/dns"
	"github.com/speakeasy-api/gram/server/internal/guardian"
	"github.com/speakeasy-api/gram/server/internal/testenv"
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
	_, err = db.Exec(ctx, `INSERT INTO organization_metadata (id,name,slug) VALUES ($1,'Loader test','loader-test'),($2,'Other test','loader-other')`, org, otherOrg)
	require.NoError(t, err)
	_, err = db.Exec(ctx, `INSERT INTO remote_session_issuers (id,organization_id,slug,issuer,authorization_endpoint,token_endpoint,jwks_uri) VALUES ($1,$2,'loader-test',$3,$3 || '/authorize',$3 || '/token',$3 || '/jwks'),($4,$5,'loader-other',$3 || '/other',$3 || '/authorize',$3 || '/token',$3 || '/jwks')`, issuer, org, upstreamURL.String(), otherIssuer, otherOrg)
	require.NoError(t, err)
	_, err = db.Exec(ctx, `INSERT INTO remote_session_clients (id,organization_id,remote_session_issuer_id,client_id,scope,token_endpoint_auth_method) VALUES ($1,$2,$3,'loader-client',ARRAY['openid','email'],'client_secret_basic'),($4,$5,$6,'other-client',ARRAY['openid','email'],'client_secret_basic')`, client, org, issuer, otherClient, otherOrg, otherIssuer)
	require.NoError(t, err)

	globalIssuer, globalClient := uuid.New(), uuid.New()
	_, err = db.Exec(ctx, `INSERT INTO remote_session_issuers (id,slug,issuer) VALUES ($1,'loader-global','https://global.example.test')`, globalIssuer)
	require.NoError(t, err)
	_, err = db.Exec(ctx, `INSERT INTO remote_session_clients (id,organization_id,remote_session_issuer_id,client_id,scope,token_endpoint_auth_method) VALUES ($1,$2,$3,'global-client',ARRAY['openid','email'],'client_secret_basic')`, globalClient, org, globalIssuer)
	require.NoError(t, err)
	global, err := manager.LoadFederatedDelegationProvider(ctx, org, globalIssuer, globalClient)
	require.NoError(t, err)
	require.NotEmpty(t, global.DelegationConfigurationHash())
	require.Equal(t, globalClient, global.client.ID)

	orgClientForeignIssuer, foreignClientOrgIssuer := uuid.New(), uuid.New()
	_, err = db.Exec(ctx, `INSERT INTO remote_session_clients (id,organization_id,remote_session_issuer_id,client_id,scope,token_endpoint_auth_method) VALUES ($1,$2,$3,'cross-issuer',ARRAY['openid'],'client_secret_basic'),($4,$5,$6,'cross-client',ARRAY['openid'],'client_secret_basic')`, orgClientForeignIssuer, org, otherIssuer, foreignClientOrgIssuer, otherOrg, issuer)
	require.NoError(t, err)

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
