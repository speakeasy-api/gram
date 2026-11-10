package remotesessions_test

import (
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	clientsgen "github.com/speakeasy-api/gram/server/gen/remote_session_clients"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
)

// A binding change on an organization-level issuer reaches servers in every
// project that sits on it, so the writer re-decides each of them in its own
// transaction rather than leaving the others to a later resync.
func TestClientBindingWritersReachEveryProjectOnAnOrganizationIssuer(t *testing.T) {
	t.Parallel()

	t.Run("attaching captures the client on another project's sole server", func(t *testing.T) {
		t.Parallel()
		ctx, ti := newTestService(t)
		authCtx, ok := contextvalues.GetAuthContext(ctx)
		require.True(t, ok)
		orgID := authCtx.ActiveOrganizationID

		orgUserIssuer := seedOrganizationTierUserSessionIssuer(t, ctx, ti.conn, "scope-attach-usi")
		otherProject := createProject(t, ctx, ti.conn, "scope-attach-other")
		other := seedServerOnIssuer(t, ctx, ti.conn, otherProject, orgUserIssuer, "scope-attach-other")
		provider := seedOrgLevelRemoteIssuer(t, ctx, ti.conn, orgID, "scope-attach-provider")
		client := seedOrgLevelRemoteClient(t, ctx, ti.conn, orgID, provider, "scope-attach-cid")

		_, err := ti.service.AttachUserSessionIssuer(ctx, &clientsgen.AttachUserSessionIssuerPayload{
			ID:                  client.String(),
			UserSessionIssuerID: orgUserIssuer.String(),
		})
		require.NoError(t, err)

		require.Equal(t, []uuid.UUID{client}, associatedClients(t, ctx, ti.conn, otherProject, other))
		require.NotEmpty(t, offeredAuthClients(t, ctx, ti.conn, other))
	})

	t.Run("detaching prunes the client from another project's server", func(t *testing.T) {
		t.Parallel()
		ctx, ti := newTestService(t)
		authCtx, ok := contextvalues.GetAuthContext(ctx)
		require.True(t, ok)
		orgID := authCtx.ActiveOrganizationID

		orgUserIssuer := seedOrganizationTierUserSessionIssuer(t, ctx, ti.conn, "scope-detach-usi")
		otherProject := createProject(t, ctx, ti.conn, "scope-detach-other")
		other := seedServerOnIssuer(t, ctx, ti.conn, otherProject, orgUserIssuer, "scope-detach-other")
		provider := seedOrgLevelRemoteIssuer(t, ctx, ti.conn, orgID, "scope-detach-provider")
		client := seedOrgLevelRemoteClient(t, ctx, ti.conn, orgID, provider, "scope-detach-cid", orgUserIssuer)
		resync(t, ctx, ti.conn, orgID, otherProject, orgUserIssuer)
		recordTypedClients(t, ctx, ti.conn, orgID, other)
		require.Equal(t, []uuid.UUID{client}, associatedClients(t, ctx, ti.conn, otherProject, other))
		require.NotEmpty(t, offeredAuthClients(t, ctx, ti.conn, other))

		_, err := ti.service.DetachUserSessionIssuer(ctx, &clientsgen.DetachUserSessionIssuerPayload{
			ID:                  client.String(),
			UserSessionIssuerID: orgUserIssuer.String(),
		})
		require.NoError(t, err)

		require.Empty(t, associatedClients(t, ctx, ti.conn, otherProject, other))
		require.Empty(t, offeredAuthClients(t, ctx, ti.conn, other))
	})
}

// Deleting a client removes its associations and retires the typed links
// derived from them before the deletion returns.
func TestDeleteRemoteSessionClientPrunesItsAssociations(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestService(t)
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	projectID, orgID := *authCtx.ProjectID, authCtx.ActiveOrganizationID

	userIssuer := createUserSessionIssuerInProject(t, ctx, ti.conn, projectID, "scope-delete-usi")
	server := seedServerOnIssuer(t, ctx, ti.conn, projectID, userIssuer, "scope-delete")
	client, _ := seedActiveClient(t, ctx, ti.conn, projectID, userIssuer, orgID, "scope-delete")
	resync(t, ctx, ti.conn, orgID, projectID, userIssuer)
	recordTypedClients(t, ctx, ti.conn, orgID, server)
	require.Equal(t, []uuid.UUID{client}, associatedClients(t, ctx, ti.conn, projectID, server))
	require.NotEmpty(t, offeredAuthClients(t, ctx, ti.conn, server))

	require.NoError(t, ti.service.DeleteRemoteSessionClient(ctx, &clientsgen.DeleteRemoteSessionClientPayload{ID: client.String()}))

	require.Empty(t, associatedClients(t, ctx, ti.conn, projectID, server))
	require.Empty(t, offeredAuthClients(t, ctx, ti.conn, server))
}
