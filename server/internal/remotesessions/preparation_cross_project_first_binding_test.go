package remotesessions_test

import (
	"context"
	"testing"
	"time"

	clientsgen "github.com/speakeasy-api/gram/server/gen/remote_session_clients"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/conv"
	"github.com/speakeasy-api/gram/server/internal/oauthwire"
	"github.com/speakeasy-api/gram/server/internal/remotesessions/repo"
	"github.com/speakeasy-api/gram/server/internal/testenv"
	"github.com/speakeasy-api/gram/server/internal/testenv/testrepo"
	"github.com/stretchr/testify/require"
)

// Both projects start without a binding. The second project must not discover
// its preparation scope until the first project's binding has committed. That
// new binding expands its scope, so an opposing source writer must still finish
// without preparation holding the inherited registration while waiting for it.
func TestPrepareEMA_CrossProjectFirstBindingsSerializeBeforeScopeDiscovery(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestService(t)
	ctx, cancel := context.WithTimeout(ctx, 20*time.Second)
	defer cancel()
	auth, _ := contextvalues.GetAuthContext(ctx)
	otherProject := createProject(t, ctx, ti.conn, "prospective-second-project")
	otherAuth := *auth
	otherAuth.ProjectID = &otherProject
	otherCtx := contextvalues.WithValidatedGramSession(ctx, &otherAuth, false)
	firstUser := createUserSessionIssuerInProject(t, ctx, ti.conn, *auth.ProjectID, "prospective-first-user")
	secondUser := createUserSessionIssuerInProject(t, otherCtx, ti.conn, otherProject, "prospective-second-user")
	issuer, err := repo.New(ti.conn).CreateRemoteSessionIssuer(ctx, repo.CreateRemoteSessionIssuerParams{
		OrganizationID: conv.ToPGText(auth.ActiveOrganizationID), Slug: "prospective-inherited",
		Issuer: "https://issuer.example.com", TokenEndpoint: conv.ToPGText("https://issuer.example.com/token"),
		MetadataFetchedAt: conv.ToPGTimestamptz(time.Now()), ScopesSupported: []string{"openid"},
		GrantTypesSupported:                 []string{oauthwire.GrantTypeJWTBearer},
		AuthorizationGrantProfilesSupported: []string{"urn:ietf:params:oauth:grant-profile:id-jag"},
		ResponseTypesSupported:              []string{"code"}, TokenEndpointAuthMethodsSupported: []string{"client_secret_basic"},
	})
	require.NoError(t, err)
	client := seedOrgLevelRemoteClient(t, ctx, ti.conn, auth.ActiveOrganizationID, issuer.ID, "prospective-inherited-client", firstUser, secondUser)
	secret, err := testenv.NewEncryptionClient(t).Encrypt([]byte("prospective-test-secret"))
	require.NoError(t, err)
	_, err = ti.conn.Exec(ctx, `UPDATE remote_session_clients SET token_endpoint_auth_method = 'client_secret_basic', client_secret_encrypted = $1, grant_types = $2 WHERE id = $3 AND organization_id = $4 AND project_id IS NULL`, secret, []string{oauthwire.GrantTypeJWTBearer}, client, auth.ActiveOrganizationID)
	require.NoError(t, err)
	resource := "https://resource.example.com/"
	firstServer := seedRemoteMCPServerForIssuer(t, ctx, ti, firstUser, "prospective-first", resource)
	secondServer := seedRemoteMCPServerForIssuer(t, otherCtx, ti, secondUser, "prospective-second", resource)
	var bindings int
	require.NoError(t, ti.conn.QueryRow(ctx, `SELECT count(*) FROM remote_session_ema_bindings WHERE organization_id = $1 AND remote_session_issuer_id = $2`, auth.ActiveOrganizationID, issuer.ID).Scan(&bindings))
	require.Zero(t, bindings, "both preparations must be prospective first bindings")

	// These normal source-writer preflights hold their respective project/source
	// locks, but no registration lock. Neither project is yet in the other's scope.
	firstWriter, _ := lockPreparationHeaderSource(t, ctx, ti, firstServer)
	secondWriter, secondSource := lockPreparationHeaderSource(t, otherCtx, ti, secondServer)
	type outcome struct {
		result *clientsgen.IdentityChainingPreparation
		err    error
	}
	firstDone, secondDone := make(chan outcome, 1), make(chan outcome, 1)
	firstPayload := &clientsgen.PrepareEMAPayload{
		UserSessionIssuerID: firstUser.String(), RemoteSessionIssuerID: issuer.ID.String(),
		ClientID: conv.PtrEmpty(client.String()), Resource: resource, Scopes: []string{"openid"}, Mechanism: "manual",
	}
	secondPayload := *firstPayload
	secondPayload.UserSessionIssuerID = secondUser.String()
	go func() {
		result, err := ti.service.PrepareEMA(ctx, firstPayload)
		firstDone <- outcome{result, err}
	}()
	waitForPreparationSourceWait(t, ctx, ti, firstWriter)
	firstPID, err := testrepo.New(ti.conn).GetAdvisoryLockHolderFixture(ctx, "ema-registration-issuer:"+issuer.ID.String())
	require.NoError(t, err)
	go func() {
		result, err := ti.service.PrepareEMA(otherCtx, &secondPayload)
		secondDone <- outcome{result, err}
	}()
	testenv.WaitForQueryBlockedBy(t, ctx, ti.conn, uint32(firstPID), "%LockPreparationSubmission :exec%")
	var secondPID int32
	require.NoError(t, ti.conn.QueryRow(ctx, `SELECT pid FROM pg_stat_activity WHERE datname = current_database() AND query LIKE '%LockPreparationSubmission :exec%' AND $1 = ANY(pg_blocking_pids(pid))`, firstPID).Scan(&secondPID))
	var grantedAdvisory, scopeRelations int
	require.NoError(t, ti.conn.QueryRow(ctx, `SELECT count(*) FROM pg_locks WHERE pid = $1 AND locktype = 'advisory' AND granted`, secondPID).Scan(&grantedAdvisory))
	require.Equal(t, 1, grantedAdvisory, "only the per-binding session lock is held; no preparation-scope gate has been acquired")
	require.NoError(t, ti.conn.QueryRow(ctx, `SELECT count(*) FROM pg_locks WHERE pid = $1 AND granted AND relation IN ('mcp_servers'::regclass, 'remote_mcp_servers'::regclass, 'remote_session_ema_bindings'::regclass)`, secondPID).Scan(&scopeRelations))
	require.Zero(t, scopeRelations, "the second request has not run scope discovery or begun binding mutation")

	require.NoError(t, firstWriter.Commit(ctx))
	var first outcome
	select {
	case first = <-firstDone:
		require.NoError(t, first.err)
		require.Equal(t, "ready", first.result.State)
	case <-ctx.Done():
		t.Fatal(ctx.Err())
	}
	// The second request now discovers the committed first-project binding. It
	// must wait on the source writer without holding the shared registration.
	waitForPreparationSourceWait(t, otherCtx, ti, secondWriter)
	finishPreparationHeaderWrite(t, otherCtx, ti, secondWriter, secondSource)
	select {
	case second := <-secondDone:
		require.NoError(t, second.err)
		require.Equal(t, "ready", second.result.State)
		require.NotEqual(t, first.result.BindingID, second.result.BindingID)
	case <-ctx.Done():
		t.Fatal(ctx.Err())
	}
	require.NoError(t, ti.conn.QueryRow(ctx, `SELECT count(*) FROM remote_session_ema_bindings WHERE organization_id = $1 AND remote_session_issuer_id = $2 AND state = 'ready'`, auth.ActiveOrganizationID, issuer.ID).Scan(&bindings))
	require.Equal(t, 2, bindings)
	typed := assertPreparationTypedClient(t, ctx, ti, client, resource)
	var links int
	require.NoError(t, ti.conn.QueryRow(ctx, `SELECT count(*) FROM mcp_server_auth_clients WHERE organization_id = $1 AND auth_client_id = $2 AND NOT deleted AND mcp_server_id IN ($3, $4)`, auth.ActiveOrganizationID, typed, firstServer, secondServer).Scan(&links))
	require.Equal(t, 2, links, "both projects publish their live link to the inherited typed EMA client")
}
