package metamcp_test

import (
	"context"
	"errors"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/stretchr/testify/require"

	gen "github.com/speakeasy-api/gram/server/gen/meta_mcp"
	"github.com/speakeasy-api/gram/server/internal/audit"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/mcpservers/tombstone"
	metamcprepo "github.com/speakeasy-api/gram/server/internal/metamcp/repo"
	"github.com/speakeasy-api/gram/server/internal/testenv"
	usersessionsrepo "github.com/speakeasy-api/gram/server/internal/usersessions/repo"
)

func issuerLive(t *testing.T, ctx context.Context, ti *testInstance, issuerID uuid.UUID) bool {
	t.Helper()

	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	_, err := usersessionsrepo.New(ti.conn).GetUserSessionIssuerByID(ctx, usersessionsrepo.GetUserSessionIssuerByIDParams{
		ID:             issuerID,
		ProjectID:      *authCtx.ProjectID,
		OrganizationID: authCtx.ActiveOrganizationID,
	})
	if errors.Is(err, pgx.ErrNoRows) {
		return false
	}
	require.NoError(t, err)
	return true
}

// Tombstoning a member stops it from being a gateway member but does not
// delete the gateway's downstream issuer.
func TestDeleteMcpServer_LeavesGatewayIssuerAndRemovesMember(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestService(t)
	auth, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	projectID, orgID := *auth.ProjectID, auth.ActiveOrganizationID
	gateway := seedMetaMcpServer(t, ctx, ti, "gateway with deleted member")
	require.NotNil(t, gateway.UserSessionIssuerID)
	server := seedServableMember(t, ctx, ti, projectID)
	provider := seedRemoteSessionIssuer(t, ctx, ti.conn, projectID, orgID, "member-provider")
	registration := createRemoteSessionClient(t, ctx, ti.conn, projectID, orgID, provider, "member-registration")
	seedMemberOffer(t, ctx, ti.conn, projectID, orgID, server, registration)
	_, err := ti.service.AddMetaMcpMember(ctx, &gen.AddMetaMcpMemberPayload{MetaMcpServerID: gateway.ID, McpServerID: server.String()})
	require.NoError(t, err)
	members := func() []metamcprepo.ListMetaMCPMembersForRemoteSessionClientRow {
		rows, err := metamcprepo.New(ti.conn).ListMetaMCPMembersForRemoteSessionClient(ctx, metamcprepo.ListMetaMCPMembersForRemoteSessionClientParams{
			MetaMcpServerID: uuid.MustParse(gateway.ID), ProjectID: projectID, TypedOffers: true, RemoteSessionClientID: registration,
		})
		require.NoError(t, err)
		return rows
	}
	require.Len(t, members(), 1)

	tx := testenv.BeginTx(t, ctx, ti.conn)
	locked, err := tombstone.Lock(ctx, tx, orgID, projectID, server)
	require.NoError(t, err)
	_, err = tombstone.Tombstone(ctx, tx, audit.NewLogger(), locked, tombstone.Input{
		OrganizationID: orgID, ProjectID: projectID, ActorUserID: auth.UserID, ActorEmail: auth.Email,
		TracerProvider: testenv.NewTracerProvider(t), Logger: testenv.NewLogger(t),
	})
	require.NoError(t, err)
	require.NoError(t, tx.Commit(ctx))
	require.Empty(t, members(), "a deleted server cannot contribute a typed offer to its gateway")
	require.True(t, issuerLive(t, ctx, ti, uuid.MustParse(*gateway.UserSessionIssuerID)))
}

func TestDeleteMetaMcpServer_KeepsAnIssuerAnotherGatewayUses(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestService(t)
	auth, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	projectID, orgID := *auth.ProjectID, auth.ActiveOrganizationID
	gateway := seedMetaMcpServer(t, ctx, ti, "shared issuer delete host")
	require.NotNil(t, gateway.UserSessionIssuerID)
	other, err := ti.service.CreateMetaMcpServer(ctx, &gen.CreateMetaMcpServerPayload{
		Name: "shared issuer survivor", UserSessionIssuerID: gateway.UserSessionIssuerID,
	})
	require.NoError(t, err)
	server := seedMcpServer(t, ctx, ti.conn, projectID)
	provider := seedRemoteSessionIssuer(t, ctx, ti.conn, projectID, orgID, "surviving-provider")
	registration := createRemoteSessionClient(t, ctx, ti.conn, projectID, orgID, provider, "surviving-registration")
	offer := seedMemberOffer(t, ctx, ti.conn, projectID, orgID, server, registration)
	_, err = ti.service.AddMetaMcpMember(ctx, &gen.AddMetaMcpMemberPayload{MetaMcpServerID: other.ID, McpServerID: server.String()})
	require.NoError(t, err)

	require.NoError(t, ti.service.DeleteMetaMcpServer(ctx, &gen.DeleteMetaMcpServerPayload{ID: gateway.ID}))
	requireMemberOffers(t, ctx, ti, server, offer)
	require.True(t, issuerLive(t, ctx, ti, uuid.MustParse(*gateway.UserSessionIssuerID)), "the other gateway still uses the issuer")
}
