package authclientbackfill

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"
	authclientsrepo "github.com/speakeasy-api/gram/server/internal/authclients/repo"
	"github.com/speakeasy-api/gram/server/internal/testenv"
	"github.com/stretchr/testify/require"
)

func TestSyncEMARegistrations_ConcurrentAdminDetach(t *testing.T) {
	t.Parallel()
	f := newFixture(t)
	usi := f.userSessionIssuer()
	source, server := f.emaSourceAt(gatewayResource, usi)
	f.staticHeader(source, "X-Enterprise-Key")
	client, issuer := f.emaClient()
	binding := f.binding(usi, issuer, client, gatewayResource, []string{"read"})
	tx := testenv.BeginTx(t, t.Context(), f.pool)
	require.NoError(t, SyncEMABinding(t.Context(), tx, f.orgID, binding))
	require.NoError(t, tx.Commit(t.Context()))
	id := EMAClientID(f.orgID, issuer, client, gatewayResource)
	require.Len(t, f.headers(id), 1)

	// Mirror the actual admin API: it locks the typed client before touching
	// its link. A sync waiting on that client must not hold the link first.
	admin := testenv.BeginTx(t, t.Context(), f.pool)
	aq := authclientsrepo.New(admin)
	_, err := aq.LockAuthClient(t.Context(), authclientsrepo.LockAuthClientParams{ID: id, OrganizationID: f.orgID})
	require.NoError(t, err)
	syncTx := testenv.BeginTx(t, t.Context(), f.pool)
	var pid int
	require.NoError(t, syncTx.QueryRow(t.Context(), "SELECT pg_backend_pid()").Scan(&pid))
	ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
	defer cancel()
	done := make(chan error, 1)
	go func() {
		err := LockEMAWriterScope(ctx, syncTx, f.orgID, []uuid.UUID{f.projectID})
		if err == nil {
			err = SyncEMARegistrations(ctx, syncTx, f.orgID, []uuid.UUID{client})
		}
		if err == nil {
			err = syncTx.Commit(ctx)
		}
		done <- err
	}()
	require.Eventually(t, func() bool {
		var waiting bool
		err := f.pool.QueryRow(t.Context(), "SELECT cardinality(pg_blocking_pids($1)) > 0", pid).Scan(&waiting)
		return err == nil && waiting
	}, 5*time.Second, 10*time.Millisecond, "sync never reached the administrator's client lock")
	_, err = aq.DetachAuthClientFromServer(ctx, authclientsrepo.DetachAuthClientFromServerParams{AuthClientID: id, McpServerID: server, OrganizationID: f.orgID})
	require.NoError(t, err)
	require.NoError(t, admin.Commit(ctx))
	require.NoError(t, <-done)
	require.Empty(t, f.links(server), "sync resurrected an administrator detach")
	require.Empty(t, f.headers(id), "detached client kept copied credentials")
	var sourceKind string
	require.NoError(t, f.pool.QueryRow(t.Context(), "SELECT source FROM mcp_server_auth_clients WHERE auth_client_id=$1 AND mcp_server_id=$2 ORDER BY updated_at DESC LIMIT 1", id, server).Scan(&sourceKind))
	require.Equal(t, "admin", sourceKind)
}

func TestSyncEMARegistrations_PreservesAdministratorClientState(t *testing.T) {
	t.Parallel()
	for _, state := range []string{"disabled", "admin_owned"} {
		t.Run(state, func(t *testing.T) {
			t.Parallel()
			f := newFixture(t)
			usi := f.userSessionIssuer()
			server := f.serverAt(gatewayResource, usi)
			client, issuer := f.emaClient()
			binding := f.binding(usi, issuer, client, gatewayResource, []string{"read"})
			tx := testenv.BeginTx(t, t.Context(), f.pool)
			require.NoError(t, SyncEMABinding(t.Context(), tx, f.orgID, binding))
			require.NoError(t, tx.Commit(t.Context()))
			id := EMAClientID(f.orgID, issuer, client, gatewayResource)
			if state == "disabled" {
				_, err := f.pool.Exec(t.Context(), "UPDATE mcp_auth_clients SET enabled=false WHERE id=$1", id)
				require.NoError(t, err)
			} else {
				_, err := f.pool.Exec(t.Context(), "UPDATE mcp_auth_clients SET source='admin' WHERE id=$1", id)
				require.NoError(t, err)
			}
			tx = testenv.BeginTx(t, t.Context(), f.pool)
			require.NoError(t, LockEMAWriterScope(t.Context(), tx, f.orgID, []uuid.UUID{f.projectID}))
			_, err := tx.Exec(t.Context(), "UPDATE remote_session_ema_bindings SET state='manual_setup_required', remote_session_client_id=NULL WHERE id=$1", binding)
			require.NoError(t, err)
			require.NoError(t, SyncEMARegistrations(t.Context(), tx, f.orgID, []uuid.UUID{client}))
			require.NoError(t, tx.Commit(t.Context()))
			var deleted, enabled bool
			var owner string
			require.NoError(t, f.pool.QueryRow(t.Context(), "SELECT deleted,enabled,source FROM mcp_auth_clients WHERE id=$1", id).Scan(&deleted, &enabled, &owner))
			require.False(t, deleted)
			if state == "disabled" {
				require.False(t, enabled)
			} else {
				require.Equal(t, "admin", owner)
			}
			require.Equal(t, []uuid.UUID{id}, f.links(server), "normal retirement overwrote administrator client state")
		})
	}
}

func TestSyncEMARegistrations_AmbiguousSiblingPreservesExistingOwnership(t *testing.T) {
	t.Parallel()
	for _, conflict := range []string{"scopes", "resource_variant"} {
		t.Run(conflict, func(t *testing.T) {
			t.Parallel()
			f := newFixture(t)
			usi := f.userSessionIssuer()
			source, server := f.emaSourceAt(gatewayResource, usi)
			f.staticHeader(source, "X-Enterprise-Key")
			client, issuer := f.emaClient()
			binding := f.binding(usi, issuer, client, gatewayResource, []string{"read"})
			tx := testenv.BeginTx(t, t.Context(), f.pool)
			require.NoError(t, SyncEMABinding(t.Context(), tx, f.orgID, binding))
			require.NoError(t, tx.Commit(t.Context()))
			id := EMAClientID(f.orgID, issuer, client, gatewayResource)
			copies := f.headers(id)
			require.Len(t, copies, 1)
			siblingIssuer := f.userSessionIssuer()
			siblingServer := f.serverAt(gatewayResource, siblingIssuer)
			resource, scopes := gatewayResource, []string{"write"}
			if conflict == "resource_variant" {
				resource += "/"
				scopes = []string{"read"}
			}
			f.binding(siblingIssuer, issuer, client, resource, scopes)
			tx = testenv.BeginTx(t, t.Context(), f.pool)
			require.NoError(t, LockEMAWriterScope(t.Context(), tx, f.orgID, []uuid.UUID{f.projectID}))
			require.NoError(t, SyncEMARegistrations(t.Context(), tx, f.orgID, []uuid.UUID{client}))
			require.NoError(t, tx.Commit(t.Context()))
			require.Equal(t, []uuid.UUID{id}, f.links(server), "ambiguity must not retire previously preserved ownership")
			require.Empty(t, f.links(siblingServer), "ambiguous configuration must not publish a new link")
			require.Equal(t, copies, f.headers(id), "ambiguity retired existing credential copies")
		})
	}
}
