package usersessions_test

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/stretchr/testify/require"

	orggen "github.com/speakeasy-api/gram/server/gen/organization_user_session_issuers"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/conv"
	"github.com/speakeasy-api/gram/server/internal/mcpservers"
	mcpserversrepo "github.com/speakeasy-api/gram/server/internal/mcpservers/repo"
	"github.com/speakeasy-api/gram/server/internal/mcpservers/upstreamclients"
	"github.com/speakeasy-api/gram/server/internal/mcpservers/upstreamclients/authclientbackfill"
	upstreamclientsrepo "github.com/speakeasy-api/gram/server/internal/mcpservers/upstreamclients/repo"
	remotemcprepo "github.com/speakeasy-api/gram/server/internal/remotemcp/repo"
	remotesessionsrepo "github.com/speakeasy-api/gram/server/internal/remotesessions/repo"
	"github.com/speakeasy-api/gram/server/internal/testenv"
	"github.com/speakeasy-api/gram/server/internal/testenv/testrepo"
	usersessionsrepo "github.com/speakeasy-api/gram/server/internal/usersessions/repo"
)

// organizationGatewayAssociation is a server in a sibling project associated
// with a client bound to the organization's only organization-level issuer:
// the organization gateway fronts the server, so the binding serves it.
type organizationGatewayAssociation struct {
	organizationIssuer uuid.UUID
	projectID          uuid.UUID
	serverID           uuid.UUID
}

func seedOrganizationGatewayAssociation(t *testing.T, ctx context.Context, ti *testInstance, slug string) organizationGatewayAssociation {
	t.Helper()
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	orgID := authCtx.ActiveOrganizationID

	created, err := ti.service.CreateIssuer(ctx, &orggen.CreateIssuerPayload{
		SessionToken:         nil,
		Slug:                 slug + "-org",
		AuthnChallengeMode:   "chain",
		SessionDurationHours: 24,
	})
	require.NoError(t, err)
	organizationIssuer := uuid.MustParse(created.ID)
	live, err := upstreamclientsrepo.New(ti.conn).ListOrganizationLevelUserSessionIssuerIDs(ctx, orgID)
	require.NoError(t, err)
	require.Equal(t, []uuid.UUID{organizationIssuer}, live, "the organization gateway needs a single organization-level issuer")

	projectID := createSiblingProject(t, ctx, ti.conn, slug+"-sibling")
	projectIssuer, err := usersessionsrepo.New(ti.conn).CreateUserSessionIssuer(ctx, usersessionsrepo.CreateUserSessionIssuerParams{
		ProjectID:          projectID,
		Slug:               slug + "-project",
		AuthnChallengeMode: "interactive",
		SessionDuration:    pgtype.Interval{Microseconds: int64(time.Hour / time.Microsecond), Valid: true},
	})
	require.NoError(t, err)
	remote, err := remotemcprepo.New(ti.conn).CreateServer(ctx, remotemcprepo.CreateServerParams{
		ID: uuid.New(), ProjectID: projectID, TransportType: "streamable-http", Url: "https://" + slug + ".example.com/mcp",
	})
	require.NoError(t, err)
	server, err := mcpserversrepo.New(ti.conn).CreateMCPServer(ctx, mcpserversrepo.CreateMCPServerParams{
		ID:                  uuid.New(),
		ProjectID:           projectID,
		Name:                conv.ToPGText(slug),
		Slug:                conv.ToPGText(slug),
		UserSessionIssuerID: conv.ToNullUUID(projectIssuer.ID),
		RemoteMcpServerID:   conv.ToNullUUID(remote.ID),
		Visibility:          mcpservers.VisibilityPrivate,
	})
	require.NoError(t, err)

	rq := remotesessionsrepo.New(ti.conn)
	provider, err := rq.CreateRemoteSessionIssuer(ctx, remotesessionsrepo.CreateRemoteSessionIssuerParams{
		OrganizationID:                    conv.ToPGText(orgID),
		Slug:                              slug + "-provider",
		Issuer:                            "https://" + slug + "-provider.example.com",
		AuthorizationEndpoint:             conv.ToPGText("https://" + slug + "-provider.example.com/authorize"),
		TokenEndpoint:                     conv.ToPGText("https://" + slug + "-provider.example.com/token"),
		ScopesSupported:                   []string{},
		GrantTypesSupported:               []string{"authorization_code"},
		ResponseTypesSupported:            []string{"code"},
		TokenEndpointAuthMethodsSupported: []string{"none"},
	})
	require.NoError(t, err)
	client, err := rq.CreateRemoteSessionClient(ctx, remotesessionsrepo.CreateRemoteSessionClientParams{
		OrganizationID:        conv.ToPGText(orgID),
		RemoteSessionIssuerID: provider.ID,
		ClientID:              slug + "-client",
		ClientIDIssuedAt:      conv.ToPGTimestamptz(time.Now()),
	})
	require.NoError(t, err)
	require.NoError(t, rq.AttachRemoteSessionClientToUserSessionIssuer(ctx, remotesessionsrepo.AttachRemoteSessionClientToUserSessionIssuerParams{
		RemoteSessionClientID: client.ID,
		UserSessionIssuerID:   organizationIssuer,
	}))
	require.NoError(t, testrepo.New(ti.conn).InsertMCPServerRemoteSessionClientFixture(ctx, testrepo.InsertMCPServerRemoteSessionClientFixtureParams{
		ProjectID:             projectID,
		OrganizationID:        orgID,
		McpServerID:           server.ID,
		RemoteSessionClientID: client.ID,
	}))
	tx := testenv.BeginTx(t, ctx, ti.conn)
	require.NoError(t, authclientbackfill.SyncServerAssociations(ctx, tx, orgID, server.ID))
	require.NoError(t, tx.Commit(ctx))

	seeded := organizationGatewayAssociation{organizationIssuer: organizationIssuer, projectID: projectID, serverID: server.ID}
	require.NoError(t, upstreamclients.Reconcile(ctx, ti.conn, orgID, projectID, nil))
	requireOrganizationGatewayAssociation(t, ctx, ti, seeded, true)
	return seeded
}

func requireOrganizationGatewayAssociation(t *testing.T, ctx context.Context, ti *testInstance, seeded organizationGatewayAssociation, present bool) {
	t.Helper()
	rows, err := upstreamclientsrepo.New(ti.conn).ListMCPServerRemoteSessionClients(ctx, upstreamclientsrepo.ListMCPServerRemoteSessionClientsParams{
		McpServerID: seeded.serverID,
		ProjectID:   seeded.projectID,
	})
	require.NoError(t, err)
	links, err := authclientbackfill.New(ti.conn).ListServerAuthClientsFixture(ctx, seeded.serverID)
	require.NoError(t, err)
	if present {
		require.Len(t, rows, 1)
		require.Len(t, links, 1)
		return
	}
	require.Empty(t, rows, "the organization gateway no longer fronts the server")
	require.Empty(t, links, "so the typed link derived from the association is retired")
}

// Whether an organization has one organization-level issuer decides whether
// its organization gateway fronts every server. Each writer that changes it
// re-decides the associations that gateway served, in its own transaction.
func TestOrganizationIssuerWritersPruneOrganizationGatewayAssociations(t *testing.T) {
	t.Parallel()

	t.Run("creating a second organization issuer", func(t *testing.T) {
		t.Parallel()
		ctx, ti := newTestService(t)
		seeded := seedOrganizationGatewayAssociation(t, ctx, ti, "gw-create")

		_, err := ti.service.CreateIssuer(ctx, &orggen.CreateIssuerPayload{
			SessionToken:         nil,
			Slug:                 "gw-create-second",
			AuthnChallengeMode:   "chain",
			SessionDurationHours: 24,
		})
		require.NoError(t, err)

		requireOrganizationGatewayAssociation(t, ctx, ti, seeded, false)
	})

	t.Run("deleting the organization issuer", func(t *testing.T) {
		t.Parallel()
		ctx, ti := newTestService(t)
		seeded := seedOrganizationGatewayAssociation(t, ctx, ti, "gw-delete")

		require.NoError(t, ti.service.DeleteIssuer(ctx, &orggen.DeleteIssuerPayload{ID: seeded.organizationIssuer.String(), SessionToken: nil}))

		requireOrganizationGatewayAssociation(t, ctx, ti, seeded, false)
	})

	t.Run("moving the organization issuer into a project", func(t *testing.T) {
		t.Parallel()
		ctx, ti := newTestService(t)
		authCtx, ok := contextvalues.GetAuthContext(ctx)
		require.True(t, ok)
		seeded := seedOrganizationGatewayAssociation(t, ctx, ti, "gw-move")

		_, err := ti.service.MoveIssuer(ctx, &orggen.MoveIssuerPayload{ID: seeded.organizationIssuer.String(), ProjectID: new(authCtx.ProjectID.String()), SessionToken: nil})
		require.NoError(t, err)

		requireOrganizationGatewayAssociation(t, ctx, ti, seeded, false)
	})
}
