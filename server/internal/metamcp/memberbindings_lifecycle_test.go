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
	"github.com/speakeasy-api/gram/server/internal/testenv"
	usersessionsrepo "github.com/speakeasy-api/gram/server/internal/usersessions/repo"
)

// gatewayWithMember creates a gateway whose one member's provider client the
// gateway bound to its issuer, and returns the gateway, its issuer, the member
// server and the member's provider.
func gatewayWithMember(t *testing.T, ctx context.Context, ti *testInstance, name string) (gatewayID string, gatewayIssuerID, serverID, providerID uuid.UUID) {
	t.Helper()

	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	projectID, orgID := *authCtx.ProjectID, authCtx.ActiveOrganizationID

	meta := seedMetaMcpServer(t, ctx, ti, name)
	require.NotNil(t, meta.UserSessionIssuerID, "create mints the gateway issuer")
	providerID = seedRemoteSessionIssuer(t, ctx, ti.conn, projectID, orgID, "lifecycle-rsi")
	serverID = seedMcpServer(t, ctx, ti.conn, projectID)
	stampAndWireMemberClient(t, ctx, ti.conn, projectID, orgID, serverID, createRemoteSessionClient(t, ctx, ti.conn, projectID, orgID, providerID, "lifecycle-client-"+uuid.NewString()[:8]))
	_, err := ti.service.AddMetaMcpMember(ctx, &gen.AddMetaMcpMemberPayload{
		SessionToken:     nil,
		ApikeyToken:      nil,
		ProjectSlugInput: nil,
		MetaMcpServerID:  meta.ID,
		McpServerID:      serverID.String(),
		SortOrder:        nil,
	})
	require.NoError(t, err)
	gatewayIssuerID = uuid.MustParse(*meta.UserSessionIssuerID)
	require.Equal(t, 1, countGatewayClientsForUpstream(t, ctx, ti.conn, projectID, orgID, gatewayIssuerID, providerID), "add binds the member's client to the gateway issuer")
	return meta.ID, gatewayIssuerID, serverID, providerID
}

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

// Deleting a member's MCP server ends its membership, so the binding the
// membership put on the gateway issuer goes with it in the same transaction.
func TestDeleteMcpServer_UnbindsMemberProviderClient(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	projectID, orgID := *authCtx.ProjectID, authCtx.ActiveOrganizationID
	_, gatewayIssuerID, serverID, providerID := gatewayWithMember(t, ctx, ti, "member server delete host")

	tx := testenv.BeginTx(t, ctx, ti.conn)
	locked, err := tombstone.Lock(ctx, tx, orgID, projectID, serverID)
	require.NoError(t, err)
	_, err = tombstone.Tombstone(ctx, tx, audit.NewLogger(), locked, tombstone.Input{
		OrganizationID: orgID,
		ProjectID:      projectID,
		ActorUserID:    authCtx.UserID,
		ActorEmail:     authCtx.Email,
		ActorSystem:    "",
		TracerProvider: testenv.NewTracerProvider(t),
		Logger:         testenv.NewLogger(t),
	})
	require.NoError(t, err)
	require.NoError(t, tx.Commit(ctx))

	require.Equal(t, 0, countGatewayClientsForUpstream(t, ctx, ti.conn, projectID, orgID, gatewayIssuerID, providerID))
}

// Deleting a gateway removes the bindings its members put on its issuer, and
// the issuer itself once nothing else uses it.
func TestDeleteMetaMcpServer_UnbindsMemberProviderClientsAndReleasesIssuer(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	projectID, orgID := *authCtx.ProjectID, authCtx.ActiveOrganizationID
	gatewayID, gatewayIssuerID, _, providerID := gatewayWithMember(t, ctx, ti, "gateway delete host")

	require.NoError(t, ti.service.DeleteMetaMcpServer(ctx, &gen.DeleteMetaMcpServerPayload{
		ID:               gatewayID,
		SessionToken:     nil,
		ApikeyToken:      nil,
		ProjectSlugInput: nil,
	}))

	require.Equal(t, 0, countGatewayClientsForUpstream(t, ctx, ti.conn, projectID, orgID, gatewayIssuerID, providerID))
	require.False(t, issuerLive(t, ctx, ti, gatewayIssuerID), "the gateway was its issuer's last user")
}

// A gateway sharing its issuer with another keeps both the issuer and the
// bindings the other gateway's members still need.
func TestDeleteMetaMcpServer_KeepsAnIssuerAnotherGatewayUses(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	projectID, orgID := *authCtx.ProjectID, authCtx.ActiveOrganizationID
	gatewayID, gatewayIssuerID, _, providerID := gatewayWithMember(t, ctx, ti, "shared issuer delete host")

	sharedIssuer := gatewayIssuerID.String()
	other, err := ti.service.CreateMetaMcpServer(ctx, &gen.CreateMetaMcpServerPayload{
		SessionToken:        nil,
		ApikeyToken:         nil,
		ProjectSlugInput:    nil,
		Name:                "shared issuer survivor",
		UserSessionIssuerID: &sharedIssuer,
		Visibility:          nil,
	})
	require.NoError(t, err)
	otherServer := seedMcpServer(t, ctx, ti.conn, projectID)
	stampAndWireMemberClient(t, ctx, ti.conn, projectID, orgID, otherServer, createRemoteSessionClient(t, ctx, ti.conn, projectID, orgID, providerID, "lifecycle-survivor-client"))
	_, err = ti.service.AddMetaMcpMember(ctx, &gen.AddMetaMcpMemberPayload{
		SessionToken:     nil,
		ApikeyToken:      nil,
		ProjectSlugInput: nil,
		MetaMcpServerID:  other.ID,
		McpServerID:      otherServer.String(),
		SortOrder:        nil,
	})
	require.NoError(t, err)

	require.NoError(t, ti.service.DeleteMetaMcpServer(ctx, &gen.DeleteMetaMcpServerPayload{
		ID:               gatewayID,
		SessionToken:     nil,
		ApikeyToken:      nil,
		ProjectSlugInput: nil,
	}))

	require.Equal(t, 1, countGatewayClientsForUpstream(t, ctx, ti.conn, projectID, orgID, gatewayIssuerID, providerID), "the other gateway's member still fronts the provider")
	require.True(t, issuerLive(t, ctx, ti, gatewayIssuerID), "the other gateway still uses the issuer")
}
