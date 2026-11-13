package access

import (
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/conv"
	mcpendpointsrepo "github.com/speakeasy-api/gram/server/internal/mcpendpoints/repo"
	mcpserversrepo "github.com/speakeasy-api/gram/server/internal/mcpservers/repo"
	remotemcprepo "github.com/speakeasy-api/gram/server/internal/remotemcp/repo"
)

func TestService_ReadShadowMCPInventoryTarget_ReportsOfficialServer(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestAccessService(t)
	authCtx := testAccessAuthContext(t, ctx)
	ti.service.WithServerURL(ti.siteURL)

	seedShadowMCPInventoryServer(t, ctx, ti, authCtx.ProjectID.String(), "Tracker")
	insertShadowMCPInventoryTelemetry(t, ctx, ti, shadowMCPInventoryTelemetryInput{
		ProjectID:  authCtx.ProjectID.String(),
		ServerURL:  "https://github.example.com/mcp",
		ServerName: "Tracker",
		UserEmail:  "reader@example.com",
		ObservedAt: time.Now().UTC(),
	})

	// The project serves the same upstream URL through Speakeasy.
	remote, err := remotemcprepo.New(ti.conn).CreateServer(ctx, remotemcprepo.CreateServerParams{
		ID:            uuid.New(),
		ProjectID:     *authCtx.ProjectID,
		Name:          conv.ToPGText("Tracker"),
		Slug:          conv.ToPGText("remote-" + uuid.NewString()),
		TransportType: "streamable-http",
		Url:           "https://github.example.com/mcp/",
	})
	require.NoError(t, err)
	serverSlug := "tracker-" + uuid.NewString()
	official, err := mcpserversrepo.New(ti.conn).CreateMCPServer(ctx, mcpserversrepo.CreateMCPServerParams{
		ID:                    uuid.New(),
		ProjectID:             *authCtx.ProjectID,
		Name:                  conv.ToPGText("Tracker (official)"),
		Slug:                  conv.ToPGText(serverSlug),
		EnvironmentID:         uuid.NullUUID{UUID: uuid.Nil, Valid: false},
		UserSessionIssuerID:   uuid.NullUUID{UUID: uuid.Nil, Valid: false},
		RemoteMcpServerID:     uuid.NullUUID{UUID: remote.ID, Valid: true},
		TunneledMcpServerID:   uuid.NullUUID{UUID: uuid.Nil, Valid: false},
		ToolsetID:             uuid.NullUUID{UUID: uuid.Nil, Valid: false},
		UnproxiedMcpServerID:  uuid.NullUUID{UUID: uuid.Nil, Valid: false},
		ToolVariationsGroupID: uuid.NullUUID{UUID: uuid.Nil, Valid: false},
		Visibility:            "private",
		NetworkAccessMode:     pgtype.Text{String: "", Valid: false},
	})
	require.NoError(t, err)
	endpointSlug := "tracker-endpoint-" + uuid.NewString()
	_, err = mcpendpointsrepo.New(ti.conn).CreateMCPEndpoint(ctx, mcpendpointsrepo.CreateMCPEndpointParams{
		ProjectID:       *authCtx.ProjectID,
		CustomDomainID:  uuid.NullUUID{UUID: uuid.Nil, Valid: false},
		McpServerID:     uuid.NullUUID{UUID: official.ID, Valid: true},
		MetaMcpServerID: uuid.NullUUID{UUID: uuid.Nil, Valid: false},
		Slug:            endpointSlug,
	})
	require.NoError(t, err)

	server, err := ti.service.ReadShadowMCPInventoryTarget(ctx, ShadowMCPInventoryTargetInput{
		OrganizationID: authCtx.ActiveOrganizationID,
		ProjectID:      *authCtx.ProjectID,
		TargetKind:     shadowMCPTargetKindServerURL,
		TargetKey:      "https://github.example.com/mcp",
	})
	require.NoError(t, err)
	require.NotNil(t, server.OfficialServer)
	require.Equal(t, official.ID.String(), server.OfficialServer.McpServerID)
	require.Equal(t, "Tracker (official)", server.OfficialServer.Name)
	require.Equal(t, serverSlug, conv.PtrValOr(server.OfficialServer.Slug, ""))
	require.Equal(t, "https://app.example.com/mcp/"+endpointSlug, conv.PtrValOr(server.OfficialServer.URL, ""))
	require.Nil(t, server.OfficialServer.GatewayName)

	// The list reports the same thing as the single read.
	list, err := ti.service.ReadShadowMCPInventory(ctx, ShadowMCPInventoryReadInput{
		OrganizationID: authCtx.ActiveOrganizationID,
		ProjectID:      *authCtx.ProjectID,
		Limit:          50,
		Cursor:         nil,
	})
	require.NoError(t, err)
	require.Len(t, list.Servers, 1)
	require.NotNil(t, list.Servers[0].OfficialServer)
	require.Equal(t, official.ID.String(), list.Servers[0].OfficialServer.McpServerID)
}

func TestService_ReadShadowMCPInventoryTarget_NoOfficialServerByDefault(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestAccessService(t)
	authCtx := testAccessAuthContext(t, ctx)
	seedShadowMCPInventoryServer(t, ctx, ti, authCtx.ProjectID.String(), "Unmanaged")
	insertShadowMCPInventoryTelemetry(t, ctx, ti, shadowMCPInventoryTelemetryInput{
		ProjectID:  authCtx.ProjectID.String(),
		ServerURL:  "https://github.example.com/mcp",
		ServerName: "Unmanaged",
		UserEmail:  "reader@example.com",
		ObservedAt: time.Now().UTC(),
	})

	server, err := ti.service.ReadShadowMCPInventoryTarget(ctx, ShadowMCPInventoryTargetInput{
		OrganizationID: authCtx.ActiveOrganizationID,
		ProjectID:      *authCtx.ProjectID,
		TargetKind:     shadowMCPTargetKindServerURL,
		TargetKey:      "https://github.example.com/mcp",
	})
	require.NoError(t, err)
	require.Nil(t, server.OfficialServer)
}
