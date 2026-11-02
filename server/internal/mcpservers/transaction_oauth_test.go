package mcpservers_test

import (
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/audit"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/mcpservers"
	backfill "github.com/speakeasy-api/gram/server/internal/mcpservers/upstreamclients/authclientbackfill"
	"github.com/speakeasy-api/gram/server/internal/testenv"
)

func TestCreateMCPServerInTransaction_PublishesOAuthBeforeCommit(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestService(t)
	auth, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	issuer := createOrganizationIssuer(t, ctx, ti, auth.ActiveOrganizationID)
	remoteIssuer, remoteClient := linkRemoteClientWithID(t, ctx, ti, auth.ActiveOrganizationID, *auth.ProjectID, issuer)
	source := uuid.New()
	fixtures := backfill.New(ti.conn)
	require.NoError(t, fixtures.SeedRemoteMCPServerFixture(ctx, backfill.SeedRemoteMCPServerFixtureParams{ID: source, ProjectID: *auth.ProjectID, Url: "https://mcp.example.com/atomic-create"}))
	tx := testenv.BeginTx(t, ctx, ti.conn)
	server, err := mcpservers.CreateProjectMCPServerInTransaction(ctx, tx, audit.NewLogger(), mcpservers.MCPServerTransactionInput{
		OrganizationID: auth.ActiveOrganizationID, ProjectID: *auth.ProjectID, ActorUserID: auth.UserID, ActorEmail: auth.Email,
		Name: "Atomic OAuth server", Visibility: mcpservers.VisibilityPrivate,
		UserSessionIssuerID: uuid.NullUUID{UUID: uuid.MustParse(issuer), Valid: true},
		RemoteMCPServerID:   uuid.NullUUID{UUID: source, Valid: true},
	})
	require.NoError(t, err)
	require.Equal(t, uuid.NullUUID{UUID: remoteIssuer, Valid: true}, server.RemoteSessionIssuerID)
	links, err := backfill.New(tx).ListServerAuthClientsFixture(ctx, server.ID)
	require.NoError(t, err)
	require.Equal(t, []uuid.UUID{backfill.OAuthClientID(auth.ActiveOrganizationID, remoteClient, backfill.TypeRemoteSession)}, links, "the configured registration must have its stable typed OAuth link before commit")
	require.NoError(t, tx.Rollback(ctx))
	links, err = fixtures.ListServerAuthClientsFixture(ctx, server.ID)
	require.NoError(t, err)
	require.Empty(t, links, "rollback must not publish typed OAuth state")
}
