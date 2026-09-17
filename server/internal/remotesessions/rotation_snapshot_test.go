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

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	orgclientsgen "github.com/speakeasy-api/gram/server/gen/organization_remote_session_clients"
	"github.com/speakeasy-api/gram/server/internal/audit"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/conv"
	"github.com/speakeasy-api/gram/server/internal/guardian"
	"github.com/speakeasy-api/gram/server/internal/oops"
	"github.com/speakeasy-api/gram/server/internal/remotesessions"
	"github.com/speakeasy-api/gram/server/internal/remotesessions/repo"
	"github.com/speakeasy-api/gram/server/internal/testenv"
	"github.com/speakeasy-api/gram/server/internal/urn"
	"github.com/stretchr/testify/require"
)

func TestRotationSnapshot_RejectsChangesDuringHTTP(t *testing.T) {
	t.Parallel()
	for _, stage := range []string{"registration", "rejected_probe", "recognized_probe"} {
		for _, mutation := range []string{"issuer_endpoint", "client_repoint"} {
			t.Run(stage+"/"+mutation, func(t *testing.T) {
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
				_, err = q.ForceRemoteSessionClientRegistrationFixture(ctx, repo.ForceRemoteSessionClientRegistrationFixtureParams{ID: in.ClientID, UpstreamRejectedAt: conv.ToPGTimestamptz(time.Now())})
				require.NoError(t, err)
				before, err := q.GetRemoteSessionClientForRotation(ctx, in.ClientID)
				require.NoError(t, err)
				logger, tracer, meter := testenv.NewLogger(t), testenv.NewTracerProvider(t), testenv.NewMeterProvider(t)
				policy, err := guardian.NewUnsafePolicy(tracer, []string{})
				require.NoError(t, err)
				enc := testenv.NewEncryptionClient(t)
				base, err := url.Parse(testServerURL)
				require.NoError(t, err)
				revoker := remotesessions.NewUpstreamRevoker(logger, tracer, meter, ti.conn, enc, policy, nil)
				rotator := remotesessions.NewClientRotator(logger, ti.conn, enc, policy, nil, ti.redisCache, base, revoker, audit.NewLogger())
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
				if mutation == "issuer_endpoint" {
					_, err = q.UpdateRemoteSessionIssuer(bounded, repo.UpdateRemoteSessionIssuerParams{ID: in.RemoteSessionIssuerID, ProjectID: conv.ToNullUUID(*auth.ProjectID), TokenEndpoint: conv.ToPGText(upstream.URL + "/changed-token")})
				} else {
					target := uuid.MustParse(createRemoteIssuer(t, ctx, ti, "rotation-target", ""))
					_, err = q.UpdateRemoteSessionClientsToRemoteSessionIssuer(bounded, repo.UpdateRemoteSessionClientsToRemoteSessionIssuerParams{SourceIssuerID: in.RemoteSessionIssuerID, TargetIssuerID: target})
				}
				require.NoError(t, err)
				once.Do(func() { close(release) })
				select {
				case err := <-done:
					require.ErrorIs(t, err, remotesessions.ErrRotationSnapshotChanged)
					if stage == "registration" {
						requireOopsCode(t, err, oops.CodeConflict)
					}
				case <-time.After(5 * time.Second):
					t.Fatal("rotation did not finish")
				}
				after, err := q.GetRemoteSessionClientForRotation(ctx, in.ClientID)
				require.NoError(t, err)
				require.Equal(t, before.RemoteSessionClient.ClientID, after.RemoteSessionClient.ClientID)
				require.Equal(t, before.RemoteSessionClient.ClientSecretEncrypted, after.RemoteSessionClient.ClientSecretEncrypted)
				require.Equal(t, before.RemoteSessionClient.UpstreamRejectedAt, after.RemoteSessionClient.UpstreamRejectedAt, "a stale recognized probe cannot clear rejection")
				if stage == "registration" {
					require.EqualValues(t, 1, registrations.Load())
					require.Zero(t, probes.Load(), "manual rotation does not probe")
				} else {
					require.EqualValues(t, 1, probes.Load(), "the intended probe must complete before snapshot rejection")
					require.Zero(t, registrations.Load(), "revalidate before issuing a registration after the probe")
				}
			})
		}
	}
}

func TestRotationSnapshot_RegistrationCASRequiresTenantAndIssuer(t *testing.T) {
	t.Parallel()
	ctx, ti, in := preparationFixture(t)
	q := repo.New(ti.conn)
	before, err := q.GetRemoteSessionClientForRotation(ctx, in.ClientID)
	require.NoError(t, err)
	c := before.RemoteSessionClient
	params := repo.ReplaceRemoteSessionClientRegistrationParams{ID: c.ID, ClientID: "must-not-publish", ExpectedClientID: c.ClientID, ExpectedUpdatedAt: c.UpdatedAt, ExpectedIssuerID: c.RemoteSessionIssuerID, ExpectedProjectID: c.ProjectID, ExpectedOrganizationID: c.OrganizationID, TokenEndpointAuthMethod: c.TokenEndpointAuthMethod}
	for _, kind := range []string{"project", "organization", "issuer"} {
		t.Run(kind, func(t *testing.T) {
			t.Parallel()
			wrong := params
			switch kind {
			case "project":
				wrong.ExpectedProjectID = conv.ToNullUUID(uuid.New())
			case "organization":
				wrong.ExpectedOrganizationID = conv.ToPGText("different-organization")
			case "issuer":
				wrong.ExpectedIssuerID = uuid.New()
			}
			_, err := q.ReplaceRemoteSessionClientRegistration(ctx, wrong)
			require.ErrorIs(t, err, pgx.ErrNoRows)
			after, err := q.GetRemoteSessionClientForRotation(ctx, in.ClientID)
			require.NoError(t, err)
			require.Equal(t, before, after)
		})
	}
}
