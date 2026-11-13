package remotesessions_test

import (
	"testing"

	"github.com/google/uuid"
	clientsgen "github.com/speakeasy-api/gram/server/gen/remote_session_clients"
	"github.com/speakeasy-api/gram/server/internal/audit"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/conv"
	"github.com/speakeasy-api/gram/server/internal/mcpservers"
	backfill "github.com/speakeasy-api/gram/server/internal/mcpservers/upstreamclients/authclientbackfill"
	"github.com/speakeasy-api/gram/server/internal/oauthwire"
	"github.com/speakeasy-api/gram/server/internal/testenv"
	"github.com/stretchr/testify/require"
)

func TestPrepareEMA_BeforeNormalServerCreation(t *testing.T) {
	t.Parallel()
	ctx, ti, in := preparationFixture(t)
	auth, _ := contextvalues.GetAuthContext(ctx)
	prepared, err := ti.service.PrepareEMA(ctx, &clientsgen.PrepareEMAPayload{UserSessionIssuerID: in.UserSessionIssuerID.String(), RemoteSessionIssuerID: in.RemoteSessionIssuerID.String(), ClientID: conv.PtrEmpty(in.ClientID.String()), Resource: in.Resource, Scopes: in.Scopes, Mechanism: "manual", ConfirmGrants: []string{oauthwire.GrantTypeJWTBearer}})
	require.NoError(t, err)
	require.Equal(t, "ready", prepared.State)
	source := uuid.New()
	q := backfill.New(ti.conn)
	require.NoError(t, q.SeedRemoteMCPServerFixture(ctx, backfill.SeedRemoteMCPServerFixtureParams{ID: source, ProjectID: *auth.ProjectID, Url: in.Resource}))
	tx := testenv.BeginTx(t, ctx, ti.conn)
	server, err := mcpservers.CreateProjectMCPServerInTransaction(ctx, tx, audit.NewLogger(), mcpservers.MCPServerTransactionInput{OrganizationID: auth.ActiveOrganizationID, ProjectID: *auth.ProjectID, ActorUserID: auth.UserID, ActorEmail: auth.Email, Name: "Prepared EMA server", Visibility: mcpservers.VisibilityPrivate, UserSessionIssuerID: uuid.NullUUID{UUID: in.UserSessionIssuerID, Valid: true}, RemoteMCPServerID: uuid.NullUUID{UUID: source, Valid: true}})
	require.NoError(t, err)
	require.NoError(t, tx.Commit(ctx))
	id := backfill.EMAClientID(auth.ActiveOrganizationID, in.RemoteSessionIssuerID, in.ClientID, in.Resource)
	linked, err := q.ListBackfillLinkedServersFixture(ctx, id)
	require.NoError(t, err)
	require.Equal(t, []uuid.UUID{server.ID}, linked, "normal creation required preparation replay or migration")
}
