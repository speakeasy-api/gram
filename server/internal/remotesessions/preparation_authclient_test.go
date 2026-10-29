package remotesessions_test

import (
	"context"
	"testing"

	"github.com/google/uuid"
	clientsgen "github.com/speakeasy-api/gram/server/gen/remote_session_clients"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/conv"
	"github.com/speakeasy-api/gram/server/internal/oauthwire"
	"github.com/stretchr/testify/require"
)

func assertPreparationTypedClient(t *testing.T, ctx context.Context, ti *testInstance, clientID uuid.UUID, resource string) uuid.UUID {
	t.Helper()
	auth, _ := contextvalues.GetAuthContext(ctx)
	var ids []uuid.UUID
	rows, err := ti.conn.Query(ctx, `SELECT c.id FROM mcp_auth_clients c
 JOIN mcp_auth_client_emas e ON e.auth_client_id = c.id
 WHERE c.organization_id = $1 AND c.remote_session_client_id = $2
 AND c.client_type = 'ema' AND NOT c.deleted AND NOT e.deleted
 AND rtrim(e.resource, '/') = rtrim($3, '/')`, auth.ActiveOrganizationID, clientID, resource)
	require.NoError(t, err)
	defer rows.Close()
	for rows.Next() {
		var id uuid.UUID
		require.NoError(t, rows.Scan(&id))
		ids = append(ids, id)
	}
	require.NoError(t, rows.Err())
	require.Len(t, ids, 1, "normal preparation must publish exactly one typed EMA client without migration backfill")
	return ids[0]
}

func TestPrepareEMA_NormalTypedCatalog(t *testing.T) {
	t.Parallel()
	ctx, ti, in := preparationFixture(t)
	auth, _ := contextvalues.GetAuthContext(ctx)
	serverID := seedRemoteMCPServerForIssuer(t, ctx, ti, in.UserSessionIssuerID, "normal-ema", in.Resource)
	payload := &clientsgen.PrepareEMAPayload{
		UserSessionIssuerID: in.UserSessionIssuerID.String(), RemoteSessionIssuerID: in.RemoteSessionIssuerID.String(),
		ClientID: conv.PtrEmpty(in.ClientID.String()), Resource: in.Resource, Scopes: in.Scopes, Mechanism: "manual",
		ConfirmGrants: []string{oauthwire.GrantTypeJWTBearer},
	}
	grants := func() int {
		var count int
		require.NoError(t, ti.conn.QueryRow(ctx, `SELECT count(*) FROM principal_grants WHERE organization_id = $1`, auth.ActiveOrganizationID).Scan(&count))
		return count
	}
	before := grants()
	first, err := ti.service.PrepareEMA(ctx, payload)
	require.NoError(t, err)
	require.Equal(t, "ready", first.State)
	id := assertPreparationTypedClient(t, ctx, ti, in.ClientID, in.Resource)
	var linkID uuid.UUID
	require.NoError(t, ti.conn.QueryRow(ctx, `SELECT id FROM mcp_server_auth_clients WHERE organization_id = $1 AND mcp_server_id = $2 AND auth_client_id = $3 AND NOT deleted`, auth.ActiveOrganizationID, serverID, id).Scan(&linkID))
	again, err := ti.service.PrepareEMA(ctx, payload)
	require.NoError(t, err)
	require.Equal(t, first, again)
	require.Equal(t, id, assertPreparationTypedClient(t, ctx, ti, in.ClientID, in.Resource))
	require.Equal(t, before, grants(), "publishing a choice must not authorize any principal")

	// An administrator's disabled client and detached link are not undone by preparation.
	_, err = ti.conn.Exec(ctx, `UPDATE mcp_auth_clients SET enabled = false WHERE organization_id = $1 AND id = $2`, auth.ActiveOrganizationID, id)
	require.NoError(t, err)
	_, err = ti.conn.Exec(ctx, `UPDATE mcp_server_auth_clients SET source = 'admin', deleted_at = clock_timestamp() WHERE organization_id = $1 AND id = $2`, auth.ActiveOrganizationID, linkID)
	require.NoError(t, err)
	_, err = ti.service.PrepareEMA(ctx, payload)
	require.NoError(t, err)
	var enabled, detached bool
	require.NoError(t, ti.conn.QueryRow(ctx, `SELECT c.enabled, l.deleted FROM mcp_auth_clients c JOIN mcp_server_auth_clients l ON l.auth_client_id = c.id WHERE c.organization_id = $1 AND c.id = $2 AND l.id = $3`, auth.ActiveOrganizationID, id, linkID).Scan(&enabled, &detached))
	require.False(t, enabled)
	require.True(t, detached)
	var liveLinks int
	require.NoError(t, ti.conn.QueryRow(ctx, `SELECT count(*) FROM mcp_server_auth_clients WHERE organization_id = $1 AND mcp_server_id = $2 AND auth_client_id = $3 AND NOT deleted`, auth.ActiveOrganizationID, serverID, id).Scan(&liveLinks))
	require.Zero(t, liveLinks, "preparation must not create a replacement for an administrator-detached link")
	require.Equal(t, id, assertPreparationTypedClient(t, ctx, ti, in.ClientID, in.Resource))
	require.Equal(t, before, grants())
}

func TestPrepareEMA_TypedCatalogFailureRollsBack(t *testing.T) {
	t.Parallel()
	ctx, ti, in := preparationFixture(t)
	auth, _ := contextvalues.GetAuthContext(ctx)
	seedRemoteMCPServerForIssuer(t, ctx, ti, in.UserSessionIssuerID, "rollback-ema", in.Resource)
	// Each fixture owns an isolated database. Reject only typed EMA insertion.
	_, err := ti.conn.Exec(ctx, `ALTER TABLE mcp_auth_clients ADD CONSTRAINT reject_test_ema CHECK (client_type <> 'ema')`)
	require.NoError(t, err)
	result, err := ti.service.PrepareEMA(ctx, &clientsgen.PrepareEMAPayload{
		UserSessionIssuerID: in.UserSessionIssuerID.String(), RemoteSessionIssuerID: in.RemoteSessionIssuerID.String(),
		ClientID: conv.PtrEmpty(in.ClientID.String()), Resource: in.Resource, Scopes: in.Scopes, Mechanism: "manual",
		ConfirmGrants: []string{oauthwire.GrantTypeJWTBearer},
	})
	require.Error(t, err, "sync errors must reach the caller, not become postcommit warnings")
	require.Nil(t, result)
	var bindings int
	require.NoError(t, ti.conn.QueryRow(ctx, `SELECT count(*) FROM remote_session_ema_bindings WHERE organization_id = $1 AND project_id = $2 AND user_session_issuer_id = $3`, auth.ActiveOrganizationID, *auth.ProjectID, in.UserSessionIssuerID).Scan(&bindings))
	require.Zero(t, bindings, "the ready binding must roll back with typed-client failure")
	var grants []string
	require.NoError(t, ti.conn.QueryRow(ctx, `SELECT grant_types FROM remote_session_clients WHERE id = $1`, in.ClientID).Scan(&grants))
	require.NotContains(t, grants, oauthwire.GrantTypeJWTBearer, "grant confirmation must also roll back")
}
