package remotesessions_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"net/url"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	orgclientsgen "github.com/speakeasy-api/gram/server/gen/organization_remote_session_clients"
	"github.com/speakeasy-api/gram/server/internal/audit"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/conv"
	"github.com/speakeasy-api/gram/server/internal/guardian"
	"github.com/speakeasy-api/gram/server/internal/remotesessions"
	"github.com/speakeasy-api/gram/server/internal/remotesessions/repo"
	"github.com/speakeasy-api/gram/server/internal/testenv"
	"github.com/speakeasy-api/gram/server/internal/testenv/testrepo"
	"github.com/speakeasy-api/gram/server/internal/urn"
	"github.com/stretchr/testify/require"
)

func TestRotationSnapshot_AllowsMetadataRefreshDuringHTTP(t *testing.T) {
	t.Parallel()
	for _, stage := range []string{"registration", "rejected_probe", "recognized_probe"} {
		t.Run(stage, func(t *testing.T) {
			t.Parallel()
			ctx, ti, in := preparationFixture(t)
			auth, _ := contextvalues.GetAuthContext(ctx)
			entered, release := make(chan struct{}), make(chan struct{})
			var once sync.Once
			defer once.Do(func() { close(release) })
			var registrations, probes atomic.Int32
			upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Content-Type", "application/json")
				if r.Method != http.MethodPost {
					t.Errorf("unexpected rotation request method: %s", r.Method)
					w.WriteHeader(http.StatusMethodNotAllowed)
					return
				}
				switch r.URL.Path {
				case "/register":
					registrations.Add(1)
				case "/token":
					probes.Add(1)
				default:
					t.Errorf("unexpected rotation request path: %s", r.URL.Path)
					http.NotFound(w, r)
					return
				}
				if (stage == "registration" && r.URL.Path == "/register") || (stage != "registration" && r.URL.Path == "/token") {
					close(entered)
					<-release
				}
				if r.URL.Path == "/register" {
					w.WriteHeader(http.StatusCreated)
					_, _ = w.Write([]byte(`{"client_id":"stale-replacement","client_secret":"replacement-secret","token_endpoint_auth_method":"client_secret_basic"}`))
				} else {
					w.WriteHeader(http.StatusBadRequest)
					if stage == "recognized_probe" {
						_, _ = w.Write([]byte(`{"error":"invalid_grant"}`))
					} else {
						_, _ = w.Write([]byte(`{"error":"invalid_client"}`))
					}
				}
			}))
			t.Cleanup(upstream.Close)
			q := repo.New(ti.conn)
			_, err := q.UpdateRemoteSessionIssuer(ctx, repo.UpdateRemoteSessionIssuerParams{ID: in.RemoteSessionIssuerID, ProjectID: conv.ToNullUUID(*auth.ProjectID), TokenEndpoint: conv.ToPGText(upstream.URL + "/token"), RegistrationEndpoint: conv.ToPGText(upstream.URL + "/register")})
			require.NoError(t, err)
			affected, err := testrepo.New(ti.conn).ForceRemoteSessionClientRegistrationFixture(ctx, testrepo.ForceRemoteSessionClientRegistrationFixtureParams{ProjectID: conv.ToNullUUID(*auth.ProjectID), OrganizationID: conv.ToPGText(auth.ActiveOrganizationID), ID: in.ClientID, UpstreamRejectedAt: conv.ToPGTimestamptz(time.Now())})
			require.NoError(t, err)
			require.EqualValues(t, 1, affected)
			before, err := q.GetRemoteSessionClientForRotation(ctx, in.ClientID)
			require.NoError(t, err)
			logger, tracer, meter := testenv.NewLogger(t), testenv.NewTracerProvider(t), testenv.NewMeterProvider(t)
			policy, err := guardian.NewUnsafePolicy(tracer, []string{})
			require.NoError(t, err)
			enc := testenv.NewEncryptionClient(t)
			base, err := url.Parse(testServerURL)
			require.NoError(t, err)
			revoker := remotesessions.NewUpstreamRevoker(logger, tracer, meter, ti.conn, enc, policy, nil)
			rotator := remotesessions.NewClientRotator(logger, ti.conn, enc, policy, nil, ti.redisCache, base, revoker, audit.NewLogger(), nil)
			done := make(chan error, 1)
			go func() {
				if stage == "registration" {
					_, err := ti.service.RotateClient(ctx, &orgclientsgen.RotateClientPayload{ID: in.ClientID.String()})
					done <- err
					return
				}
				_, err := rotator.Rotate(ctx, remotesessions.RotateClientRegistrationParams{ClientID: in.ClientID, Trigger: remotesessions.RotationTriggerManual, ConfirmUpstreamRejection: true, OrganizationID: auth.ActiveOrganizationID, Actor: urn.NewSystemPrincipal("rotation-test")})
				done <- err
			}()
			select {
			case <-entered:
			case <-time.After(5 * time.Second):
				t.Fatal("rotation did not reach HTTP")
			}
			// Real parent writes complete while HTTP is blocked: no transaction or
			// parent row lock spans the network request.
			bounded, cancel := context.WithTimeout(ctx, 2*time.Second)
			defer cancel()
			_, err = q.UpdateRemoteSessionIssuerDiscoveredMetadata(bounded, repo.UpdateRemoteSessionIssuerDiscoveredMetadataParams{
				ID: in.RemoteSessionIssuerID, Issuer: before.IssuerUrl, ProjectID: before.IssuerProjectID, OrganizationID: before.IssuerOrganizationID,
				TokenEndpoint: before.IssuerTokenEndpoint.String, RegistrationEndpoint: before.IssuerRegistrationEndpoint.String,
				ScopesSupported: []string{}, GrantTypesSupported: []string{}, AuthorizationGrantProfilesSupported: []string{},
				ResponseTypesSupported: []string{}, TokenEndpointAuthMethodsSupported: []string{},
				CodeChallengeMethodsSupported: []string{}, IntrospectionEndpointAuthMethodsSupported: []string{},
				IDTokenSigningAlgValuesSupported: []string{}, ClaimsSupported: []string{},
			})
			require.NoError(t, err)
			once.Do(func() { close(release) })
			select {
			case err := <-done:
				if stage == "recognized_probe" {
					require.ErrorIs(t, err, remotesessions.ErrClientStillRecognized)
				} else {
					require.NoError(t, err)
				}
			case <-time.After(5 * time.Second):
				t.Fatal("rotation did not finish")
			}
			after, err := q.GetRemoteSessionClientForRotation(ctx, in.ClientID)
			require.NoError(t, err)
			require.NotEqual(t, before.IssuerUpdatedAt, after.IssuerUpdatedAt)
			if stage == "recognized_probe" {
				require.Equal(t, before.RemoteSessionClient.ClientID, after.RemoteSessionClient.ClientID)
				require.False(t, after.RemoteSessionClient.UpstreamRejectedAt.Valid)
				require.Zero(t, registrations.Load())
			} else {
				require.Equal(t, "stale-replacement", after.RemoteSessionClient.ClientID)
				require.NotEqual(t, before.RemoteSessionClient.ClientSecretEncrypted, after.RemoteSessionClient.ClientSecretEncrypted)
				require.EqualValues(t, 1, registrations.Load())
			}
			if stage == "registration" {
				require.Zero(t, probes.Load())
			} else {
				require.EqualValues(t, 1, probes.Load())
			}
		})
	}
}
