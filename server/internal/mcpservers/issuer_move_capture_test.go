package mcpservers_test

import (
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	gen "github.com/speakeasy-api/gram/server/gen/mcp_servers"
	"github.com/speakeasy-api/gram/server/gen/types"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/conv"
	mcpserversrepo "github.com/speakeasy-api/gram/server/internal/mcpservers/repo"
	backfill "github.com/speakeasy-api/gram/server/internal/mcpservers/upstreamclients/authclientbackfill"
	"github.com/speakeasy-api/gram/server/internal/productfeatures"
	productfeaturesrepo "github.com/speakeasy-api/gram/server/internal/productfeatures/repo"
)

// Moving to another downstream issuer must not add upstream offers.
func TestUpdateMcpServer_IssuerMoveDoesNotInheritOffers(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name   string
		shared bool
	}{
		{name: "sole consumer", shared: false},
		{name: "shared issuer", shared: true},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			ctx, ti := newTestService(t)
			authCtx, ok := contextvalues.GetAuthContext(ctx)
			require.True(t, ok)
			projectID := *authCtx.ProjectID
			orgID := authCtx.ActiveOrganizationID
			_, err := productfeaturesrepo.New(ti.conn).EnableFeature(ctx, productfeaturesrepo.EnableFeatureParams{
				OrganizationID: orgID,
				FeatureName:    string(productfeatures.FeatureUpstreamAuthEnforced),
			})
			require.NoError(t, err)

			origin := createOrganizationIssuer(t, ctx, ti, orgID)
			target := createOrganizationIssuer(t, ctx, ti, orgID)
			create := func(name, issuer string) (string, string) {
				remoteID := seedRemoteMcpServer(t, ctx, ti.conn, projectID).String()
				created, err := ti.service.CreateMcpServer(ctx, &gen.CreateMcpServerPayload{Name: name, RemoteMcpServerID: &remoteID, UserSessionIssuerID: &issuer, Visibility: types.McpServerVisibility("disabled")})
				require.NoError(t, err)
				return created.ID, remoteID
			}
			moved, movedBackend := create("moved", origin)
			_, registration := seedRemoteRegistration(t, ctx, ti, orgID, projectID)
			sibling, _ := create("configured on target", target)
			siblingID := uuid.MustParse(sibling)
			q := backfill.New(ti.conn)
			offer, err := q.SeedAuthClientFixture(ctx, backfill.SeedAuthClientFixtureParams{
				OrganizationID: orgID, ProjectID: conv.ToNullUUID(projectID), ClientType: backfill.TypeRemoteSession,
				Source: "admin", RemoteSessionClientID: conv.ToNullUUID(registration),
			})
			require.NoError(t, err)
			require.NoError(t, q.SeedServerAuthClientLinkFixture(ctx, backfill.SeedServerAuthClientLinkFixtureParams{
				OrganizationID: orgID, ProjectID: projectID, McpServerID: siblingID, AuthClientID: offer, Source: "admin",
			}))
			if !tc.shared {
				// Leave an offer record for a former owner, but no live target server.
				deleted, err := mcpserversrepo.New(ti.conn).DeleteMCPServer(ctx, mcpserversrepo.DeleteMCPServerParams{ID: siblingID, ProjectID: projectID})
				require.NoError(t, err)
				require.True(t, deleted.Deleted)
			}
			links, err := q.ListServerAuthClientsFixture(ctx, siblingID)
			require.NoError(t, err)
			require.Equal(t, []uuid.UUID{offer}, links)

			_, err = ti.service.UpdateMcpServer(ctx, &gen.UpdateMcpServerPayload{
				ID: moved, RemoteMcpServerID: &movedBackend, UserSessionIssuerID: &target, Visibility: types.McpServerVisibility("disabled"),
			})
			require.NoError(t, err)

			links, err = q.ListServerAuthClientsFixture(ctx, uuid.MustParse(moved))
			require.NoError(t, err)
			require.Empty(t, links, "moving downstream issuer must not inherit an offer")
		})
	}
}
