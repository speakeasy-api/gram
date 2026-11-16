package toolsets_test

import (
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	gen "github.com/speakeasy-api/gram/server/gen/toolsets"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/conv"
	"github.com/speakeasy-api/gram/server/internal/mcpservers/upstreamclients/authclientbackfill"
	upstreamclientsrepo "github.com/speakeasy-api/gram/server/internal/mcpservers/upstreamclients/repo"
	remoterepo "github.com/speakeasy-api/gram/server/internal/remotesessions/repo"
	"github.com/speakeasy-api/gram/server/internal/testenv"
	"github.com/speakeasy-api/gram/server/internal/testenv/testrepo"
)

// Deleting a toolset tombstones its hosted server, whose associations and the
// typed links derived from them go in the same transaction.
func TestToolsetsService_DeleteToolset_PrunesHostedServerAssociations(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestToolsetsService(t)
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	projectID, orgID := *authCtx.ProjectID, authCtx.ActiveOrganizationID
	toolset := createMinimalPrivateToolset(t, ctx, ti, "Toolset with an upstream client")
	issuerID := createShimIssuer(t, ctx, ti, "delete-associations-usi")
	_, err := ti.service.SetUserSessionIssuer(ctx, &gen.SetUserSessionIssuerPayload{Slug: toolset.Slug, UserSessionIssuerID: &issuerID})
	require.NoError(t, err)
	serverID := uuid.MustParse(toolset.ID)

	rq := remoterepo.New(ti.conn)
	provider, err := rq.CreateRemoteSessionIssuer(ctx, remoterepo.CreateRemoteSessionIssuerParams{
		ProjectID:                         conv.ToNullUUID(projectID),
		Slug:                              "delete-associations-provider",
		Issuer:                            "https://delete-associations.example.com",
		AuthorizationEndpoint:             conv.ToPGText("https://delete-associations.example.com/authorize"),
		TokenEndpoint:                     conv.ToPGText("https://delete-associations.example.com/token"),
		ScopesSupported:                   []string{},
		GrantTypesSupported:               []string{"authorization_code"},
		ResponseTypesSupported:            []string{"code"},
		TokenEndpointAuthMethodsSupported: []string{"none"},
	})
	require.NoError(t, err)
	client, err := rq.CreateRemoteSessionClient(ctx, remoterepo.CreateRemoteSessionClientParams{
		ProjectID:             conv.ToNullUUID(projectID),
		OrganizationID:        conv.ToPGText(orgID),
		RemoteSessionIssuerID: provider.ID,
		ClientID:              "delete-associations-client",
		ClientIDIssuedAt:      conv.ToPGTimestamptz(time.Now()),
	})
	require.NoError(t, err)
	require.NoError(t, rq.AttachRemoteSessionClientToUserSessionIssuer(ctx, remoterepo.AttachRemoteSessionClientToUserSessionIssuerParams{
		RemoteSessionClientID: client.ID,
		UserSessionIssuerID:   uuid.MustParse(issuerID),
	}))
	require.NoError(t, testrepo.New(ti.conn).InsertMCPServerRemoteSessionClientFixture(ctx, testrepo.InsertMCPServerRemoteSessionClientFixtureParams{
		ProjectID: projectID, OrganizationID: orgID, McpServerID: serverID, RemoteSessionClientID: client.ID,
	}))
	tx := testenv.BeginTx(t, ctx, ti.conn)
	require.NoError(t, authclientbackfill.SyncServerAssociations(ctx, tx, orgID, serverID))
	require.NoError(t, tx.Commit(ctx))
	associations := func() int {
		rows, err := upstreamclientsrepo.New(ti.conn).ListMCPServerRemoteSessionClients(ctx, upstreamclientsrepo.ListMCPServerRemoteSessionClientsParams{McpServerID: serverID, ProjectID: projectID})
		require.NoError(t, err)
		return len(rows)
	}
	offers := func() int {
		ids, err := authclientbackfill.New(ti.conn).ListServerAuthClientsFixture(ctx, serverID)
		require.NoError(t, err)
		return len(ids)
	}
	require.Equal(t, 1, associations())
	require.Equal(t, 1, offers())

	require.NoError(t, ti.service.DeleteToolset(ctx, &gen.DeleteToolsetPayload{Slug: toolset.Slug, SessionToken: nil, ProjectSlugInput: nil}))

	require.Zero(t, associations())
	require.Zero(t, offers())
}
