package remotesessions_test

import (
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	clientsgen "github.com/speakeasy-api/gram/server/gen/remote_session_clients"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
)

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
	client, _ := seedActiveClient(t, ctx, ti.conn, projectID, uuid.Nil, orgID, "scope-delete")
	associateClient(t, ctx, ti.conn, orgID, projectID, server, client)
	recordTypedClients(t, ctx, ti.conn, orgID, server)
	require.Equal(t, []uuid.UUID{client}, associatedClients(t, ctx, ti.conn, projectID, server))
	require.NotEmpty(t, offeredAuthClients(t, ctx, ti.conn, server))

	require.NoError(t, ti.service.DeleteRemoteSessionClient(ctx, &clientsgen.DeleteRemoteSessionClientPayload{ID: client.String()}))

	require.Empty(t, associatedClients(t, ctx, ti.conn, projectID, server))
	require.Empty(t, offeredAuthClients(t, ctx, ti.conn, server))
}
