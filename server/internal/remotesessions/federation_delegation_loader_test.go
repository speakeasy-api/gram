//nolint:glint // Database fixtures exercise the loader's tenant and issuer predicates.
package remotesessions

import (
	"context"
	"net"
	"net/http"
	"net/http/httptest"
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
	policy := guardian.NewDefaultPolicy(testenv.NewTracerProvider(t), guardian.WithResolver(dns.NewMockResolver(dns.MockResolverConfig{
		LookupIPFunc: func(context.Context, string, string) ([]net.IP, error) {
			resolutions.Add(1)
			return []net.IP{net.ParseIP("127.0.0.1")}, nil
		},
	})))
	manager := &ChallengeManager{db: db, policy: policy}
	const org = "org_delegation_loader_test"
	const otherOrg = "org_delegation_loader_other"
	issuer, client, otherIssuer, otherClient := uuid.New(), uuid.New(), uuid.New(), uuid.New()
	_, err = db.Exec(ctx, `INSERT INTO organization_metadata (id,name,slug) VALUES ($1,'Loader test','loader-test'),($2,'Other test','loader-other')`, org, otherOrg)
	require.NoError(t, err)
	_, err = db.Exec(ctx, `INSERT INTO remote_session_issuers (id,organization_id,slug,issuer,authorization_endpoint,token_endpoint,jwks_uri) VALUES ($1,$2,'loader-test',$3,$3 || '/authorize',$3 || '/token',$3 || '/jwks'),($4,$5,'loader-other',$3 || '/other',$3 || '/authorize',$3 || '/token',$3 || '/jwks')`, issuer, org, upstream.URL, otherIssuer, otherOrg)
	require.NoError(t, err)
	_, err = db.Exec(ctx, `INSERT INTO remote_session_clients (id,organization_id,remote_session_issuer_id,client_id,scope,token_endpoint_auth_method) VALUES ($1,$2,$3,'loader-client',ARRAY['openid','email'],'client_secret_basic'),($4,$5,$6,'other-client',ARRAY['openid','email'],'client_secret_basic')`, client, org, issuer, otherClient, otherOrg, otherIssuer)
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
	require.Zero(t, requests.Load(), "registration lookup must not contact upstream HTTP endpoints")
	require.Zero(t, resolutions.Load(), "registration lookup must not resolve upstream hosts")
}
