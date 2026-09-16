package remotesessions_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/google/uuid"
	orgclientsgen "github.com/speakeasy-api/gram/server/gen/organization_remote_session_clients"
	orgissuersgen "github.com/speakeasy-api/gram/server/gen/organization_remote_session_issuers"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/conv"
	mcpserversrepo "github.com/speakeasy-api/gram/server/internal/mcpservers/repo"
	"github.com/speakeasy-api/gram/server/internal/oops"
	"github.com/speakeasy-api/gram/server/internal/remotesessions"
	"github.com/speakeasy-api/gram/server/internal/remotesessions/repo"
	"github.com/speakeasy-api/gram/server/internal/testenv"
	"github.com/speakeasy-api/gram/server/internal/testenv/testrepo"
	"github.com/stretchr/testify/require"
)

func TestPreparationResourceMetadataDoesNotFetchAuthorizationServers(t *testing.T) {
	t.Parallel()
	ctx, ti, in := preparationFixture(t)
	var probes atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) { probes.Add(1) }))
	defer server.Close()
	auth, _ := contextvalues.GetAuthContext(ctx)
	issuer, err := repo.New(ti.conn).GetRemoteSessionIssuerByID(ctx, repo.GetRemoteSessionIssuerByIDParams{ID: in.RemoteSessionIssuerID, ProjectID: conv.ToNullUUID(*auth.ProjectID), OrganizationID: conv.ToPGText(auth.ActiveOrganizationID)})
	require.NoError(t, err)
	preparationRecordGrants(t, ctx, ti, in.ClientID, []string{preparationJWTGrant})
	in.ResourceMetadata = &remotesessions.PreparationResourceMetadata{Resource: in.Resource, AuthorizationServers: []string{issuer.Issuer, server.URL, "file:///not-a-network-destination"}}
	result, err := ti.service.PrepareIdentityChaining(ctx, in)
	require.NoError(t, err)
	require.Equal(t, "ready", result.State)
	require.Equal(t, "administrator_declared", result.GrantSource, "caller-declared metadata must not elevate registration provenance")
	require.Zero(t, probes.Load(), "resource metadata is compared with the known issuer, never fetched")
	in.ResourceMetadata.AuthorizationServers = []string{server.URL}
	_, err = ti.service.PrepareIdentityChaining(ctx, in)
	requireOopsCode(t, err, oops.CodeBadRequest)
	require.Zero(t, probes.Load())
}

func TestPreparationOrganizationDeleteRejectsForeignIssuerBeforeAdvisoryLock(t *testing.T) {
	t.Parallel()
	ctx, ti, _ := preparationFixture(t)
	otherOrg := createOrganization(t, ctx, ti.conn, "foreign-lock-org")
	foreign := seedOrgLevelRemoteIssuer(t, ctx, ti.conn, otherOrg, "foreign-lock-issuer")
	tx := testenv.BeginTx(t, ctx, ti.conn)
	require.NoError(t, repo.New(tx).LockRemoteSessionIssuerForClientBinding(ctx, foreign))
	bounded, cancel := context.WithTimeout(ctx, time.Second)
	defer cancel()
	err := ti.service.DeleteIssuer(bounded, &orgissuersgen.DeleteIssuerPayload{ID: foreign.String()})
	requireOopsCode(t, err, oops.CodeNotFound)
}

func TestPreparationOrganizationDetachRevalidatesScopeAfterWait(t *testing.T) {
	t.Parallel()
	for _, kind := range []string{"client", "user issuer"} {
		t.Run(kind, func(t *testing.T) {
			t.Parallel()
			ctx, ti, in := preparationFixture(t)
			auth, _ := contextvalues.GetAuthContext(ctx)
			servers := mcpserversrepo.New(ti.conn)
			serverID := seedMCPServerInOrg(t, ctx, ti.conn, auth.ActiveOrganizationID, "detach-scope-server")
			server, err := servers.GetMCPServerByIDAndOrganizationID(ctx, mcpserversrepo.GetMCPServerByIDAndOrganizationIDParams{ID: serverID, OrganizationID: auth.ActiveOrganizationID})
			require.NoError(t, err)
			client := seedProjectRemoteClientNoOrg(t, ctx, ti.conn, server.ProjectID, in.RemoteSessionIssuerID, "detach-scope-client")
			q := repo.New(ti.conn)
			require.NoError(t, q.AttachRemoteSessionClientToUserSessionIssuer(ctx, repo.AttachRemoteSessionClientToUserSessionIssuerParams{RemoteSessionClientID: client, UserSessionIssuerID: server.UserSessionIssuerID.UUID}))
			foreignOrg := createOrganization(t, ctx, ti.conn, "detach-foreign-org")
			foreignID := seedMCPServerInOrg(t, ctx, ti.conn, foreignOrg, "detach-foreign-server")
			foreign, err := servers.GetMCPServerByIDAndOrganizationID(ctx, mcpserversrepo.GetMCPServerByIDAndOrganizationIDParams{ID: foreignID, OrganizationID: foreignOrg})
			require.NoError(t, err)
			tx := testenv.BeginTx(t, ctx, ti.conn)
			tq := repo.New(tx)
			pattern := "%LockOrganizationUserIssuerForDetach :one%"
			if kind == "client" {
				_, err = tq.LockRemoteSessionClientForSessionWrite(ctx, client)
				pattern = "%LockEMAClientForLifecycle :one%"
			} else {
				_, err = tq.LockOrganizationUserIssuerForDetach(ctx, repo.LockOrganizationUserIssuerForDetachParams{ID: server.UserSessionIssuerID.UUID, OrganizationID: auth.ActiveOrganizationID})
			}
			require.NoError(t, err)
			done := make(chan error, 1)
			go func() {
				done <- ti.service.RemoveClientFromMcpServer(ctx, &orgclientsgen.RemoveClientFromMcpServerPayload{ClientID: client.String(), McpServerID: serverID.String()})
			}()
			require.Eventually(t, func() bool {
				blocked, err := testrepo.New(ti.conn).IsQueryBlockedOnLockFixture(ctx, pattern)
				return err == nil && blocked
			}, 5*time.Second, 10*time.Millisecond)
			if kind == "client" {
				_, err = tq.MovePreparationFixtureClientProject(ctx, repo.MovePreparationFixtureClientProjectParams{ID: client, ProjectID: conv.ToNullUUID(server.ProjectID), TargetProjectID: conv.ToNullUUID(foreign.ProjectID)})
			} else {
				_, err = tq.MovePreparationFixtureUserIssuerProject(ctx, repo.MovePreparationFixtureUserIssuerProjectParams{ID: server.UserSessionIssuerID.UUID, ProjectID: conv.ToNullUUID(server.ProjectID), TargetProjectID: conv.ToNullUUID(foreign.ProjectID)})
			}
			require.NoError(t, err)
			require.NoError(t, tx.Commit(ctx))
			select {
			case err := <-done:
				requireOopsCode(t, err, oops.CodeNotFound)
			case <-time.After(5 * time.Second):
				t.Fatal("detach did not finish")
			}
			require.Equal(t, 1, countRemoteSessionClientUserSessionIssuerBindings(t, ctx, ti.conn, client, server.UserSessionIssuerID.UUID))
		})
	}
}

func TestPreparationSerializesWithInteractiveRotation(t *testing.T) {
	t.Parallel()
	ctx, ti, in := preparationFixture(t)
	auth, _ := contextvalues.GetAuthContext(ctx)
	entered, release := make(chan struct{}), make(chan struct{})
	var once sync.Once
	defer once.Do(func() { close(release) })
	var posts atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		posts.Add(1)
		close(entered)
		<-release
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusCreated)
		_, _ = w.Write([]byte(`{"client_id":"rotated-serialized","client_secret":"rotated-secret","token_endpoint_auth_method":"client_secret_basic"}`))
	}))
	t.Cleanup(server.Close)
	q := repo.New(ti.conn)
	require.NoError(t, q.SetPreparationFixtureDCREndpoint(ctx, repo.SetPreparationFixtureDCREndpointParams{ID: in.RemoteSessionIssuerID, ProjectID: conv.ToNullUUID(*auth.ProjectID), Endpoint: conv.ToPGText(server.URL)}))
	preparationRecordGrants(t, ctx, ti, in.ClientID, []string{preparationJWTGrant})
	rotated := make(chan error, 1)
	go func() {
		_, err := ti.service.RotateClient(ctx, &orgclientsgen.RotateClientPayload{ID: in.ClientID.String()})
		rotated <- err
	}()
	select {
	case <-entered:
	case <-time.After(5 * time.Second):
		t.Fatal("rotation did not submit")
	}
	// The network phase must not hold the client row in a transaction.
	bounded, cancel := context.WithTimeout(ctx, time.Second)
	defer cancel()
	tx := testenv.BeginTx(t, bounded, ti.conn)
	_, err := repo.New(tx).LockRemoteSessionClientForSessionWrite(bounded, in.ClientID)
	require.NoError(t, err)
	require.NoError(t, tx.Rollback(ctx))
	waiting, stop := context.WithTimeout(ctx, 200*time.Millisecond)
	defer stop()
	_, err = restartPreparationService(t, ti).PrepareIdentityChaining(waiting, in)
	require.Error(t, err, "preparation cannot install a binding while rotation is in flight")
	once.Do(func() { close(release) })
	require.NoError(t, <-rotated)
	current, err := q.GetRemoteSessionClientForRotation(ctx, in.ClientID)
	require.NoError(t, err)
	require.Equal(t, "rotated-serialized", current.RemoteSessionClient.ClientID, "provider result is persisted, not orphaned")
	prepared, err := ti.service.PrepareIdentityChaining(ctx, in)
	require.NoError(t, err)
	require.Equal(t, "unknown_grants", prepared.State, "new registration must not inherit the old registration's grant evidence")
	require.NotEqual(t, uuid.Nil, prepared.BindingID)
	_, err = ti.service.RotateClient(ctx, &orgclientsgen.RotateClientPayload{ID: in.ClientID.String()})
	requireOopsCode(t, err, oops.CodeConflict)
	require.EqualValues(t, 1, posts.Load(), "an EMA-bound registration is refused before another provider call")
}
