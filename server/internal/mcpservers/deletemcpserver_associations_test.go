package mcpservers_test

import (
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	gen "github.com/speakeasy-api/gram/server/gen/mcp_servers"
	"github.com/speakeasy-api/gram/server/internal/audit"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/mcpservers"
	"github.com/speakeasy-api/gram/server/internal/mcpservers/upstreamclients"
	backfill "github.com/speakeasy-api/gram/server/internal/mcpservers/upstreamclients/authclientbackfill"
	upstreamclientsrepo "github.com/speakeasy-api/gram/server/internal/mcpservers/upstreamclients/repo"
	"github.com/speakeasy-api/gram/server/internal/testenv"
)

// Deleting a server removes its associations and retires the typed links
// derived from them in the deletion's own transaction.
func TestDeleteMcpServer_PrunesItsAssociations(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestService(t)
	auth, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	issuer := createOrganizationIssuer(t, ctx, ti, auth.ActiveOrganizationID)
	source := uuid.New()
	fixtures := backfill.New(ti.conn)
	require.NoError(t, fixtures.SeedRemoteMCPServerFixture(ctx, backfill.SeedRemoteMCPServerFixtureParams{ID: source, ProjectID: *auth.ProjectID, Url: "https://mcp.example.com/deleted"}))
	tx := testenv.BeginTx(t, ctx, ti.conn)
	server, err := mcpservers.CreateProjectMCPServerInTransaction(ctx, tx, audit.NewLogger(), mcpservers.MCPServerTransactionInput{
		OrganizationID: auth.ActiveOrganizationID, ProjectID: *auth.ProjectID, ActorUserID: auth.UserID, ActorEmail: auth.Email,
		Name: "Deleted server", Visibility: mcpservers.VisibilityPrivate,
		UserSessionIssuerID: uuid.NullUUID{UUID: uuid.MustParse(issuer), Valid: true},
		RemoteMCPServerID:   uuid.NullUUID{UUID: source, Valid: true},
	})
	require.NoError(t, err)
	require.NoError(t, tx.Commit(ctx))
	_, remoteClient := linkRemoteClientWithID(t, ctx, ti, auth.ActiveOrganizationID, *auth.ProjectID, issuer)
	require.NoError(t, upstreamclients.Reconcile(ctx, ti.conn, auth.ActiveOrganizationID, *auth.ProjectID, []uuid.UUID{uuid.MustParse(issuer)}))
	associations := func() int {
		rows, err := upstreamclientsrepo.New(ti.conn).ListMCPServerRemoteSessionClients(ctx, upstreamclientsrepo.ListMCPServerRemoteSessionClientsParams{McpServerID: server.ID, ProjectID: *auth.ProjectID})
		require.NoError(t, err)
		return len(rows)
	}
	require.Equal(t, 1, associations())
	links, err := fixtures.ListServerAuthClientsFixture(ctx, server.ID)
	require.NoError(t, err)
	require.Equal(t, []uuid.UUID{backfill.OAuthClientID(auth.ActiveOrganizationID, remoteClient, backfill.TypeRemoteSession)}, links)

	require.NoError(t, ti.service.DeleteMcpServer(ctx, &gen.DeleteMcpServerPayload{ID: server.ID.String()}))

	require.Zero(t, associations())
	links, err = fixtures.ListServerAuthClientsFixture(ctx, server.ID)
	require.NoError(t, err)
	require.Empty(t, links)
}
