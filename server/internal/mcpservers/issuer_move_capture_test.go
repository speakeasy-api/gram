package mcpservers_test

import (
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	gen "github.com/speakeasy-api/gram/server/gen/mcp_servers"
	"github.com/speakeasy-api/gram/server/gen/types"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	backfill "github.com/speakeasy-api/gram/server/internal/mcpservers/upstreamclients/authclientbackfill"
	"github.com/speakeasy-api/gram/server/internal/productfeatures"
	productfeaturesrepo "github.com/speakeasy-api/gram/server/internal/productfeatures/repo"
)

// Moving a server onto a user session issuer configures its sign-in there.
// Under upstream auth enforcement a server that is the issuer's only consumer
// offers the clients bound to it, and one that shares the issuer offers none.
func TestUpdateMcpServer_IssuerMoveOffersSoleConsumerClientsUnderEnforcement(t *testing.T) {
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
			if tc.shared {
				create("already there", target)
			}
			_, client := linkRemoteClientWithID(t, ctx, ti, orgID, projectID, target)

			_, err = ti.service.UpdateMcpServer(ctx, &gen.UpdateMcpServerPayload{
				ID: moved, RemoteMcpServerID: &movedBackend, UserSessionIssuerID: &target, Visibility: types.McpServerVisibility("disabled"),
			})
			require.NoError(t, err)

			links, err := backfill.New(ti.conn).ListServerAuthClientsFixture(ctx, uuid.MustParse(moved))
			require.NoError(t, err)
			if tc.shared {
				require.Empty(t, links, "an issuer another server uses configures nothing for the moved server")
				return
			}
			require.Equal(t, []uuid.UUID{backfill.OAuthClientID(orgID, client, backfill.TypeRemoteSession)}, links)
		})
	}
}
