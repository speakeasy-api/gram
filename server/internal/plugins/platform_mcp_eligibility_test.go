package plugins_test

import (
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	mcpendpointsrepo "github.com/speakeasy-api/gram/server/internal/mcpendpoints/repo"
	mcpserversrepo "github.com/speakeasy-api/gram/server/internal/mcpservers/repo"
	"github.com/speakeasy-api/gram/server/internal/platformmcp"
	"github.com/speakeasy-api/gram/server/internal/remotemcp/remotemcptest"
	remotemcprepo "github.com/speakeasy-api/gram/server/internal/remotemcp/repo"
	usersessionsrepo "github.com/speakeasy-api/gram/server/internal/usersessions/repo"
)

func TestPostgresNewModelEligibilityExcludesDisabledServer(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestPluginsService(t)
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	require.NotNil(t, authCtx.ProjectID)

	issuer, err := usersessionsrepo.New(ti.conn).CreateUserSessionIssuer(ctx, usersessionsrepo.CreateUserSessionIssuerParams{
		ProjectID:          *authCtx.ProjectID,
		Slug:               "platform-mcp-eligibility-issuer",
		AuthnChallengeMode: "interactive",
		SessionDuration:    pgtype.Interval{Microseconds: time.Hour.Microseconds(), Valid: true},
	})
	require.NoError(t, err)

	remoteServer := remotemcptest.SeedServer(t, ctx, ti.conn, remotemcprepo.CreateServerParams{
		ProjectID:     *authCtx.ProjectID,
		TransportType: "streamable-http",
		Url:           "https://example.invalid/platform-mcp-eligibility",
	})

	serverID := uuid.New()
	mcpServer, err := mcpserversrepo.New(ti.conn).CreateMCPServer(ctx, mcpserversrepo.CreateMCPServerParams{
		ID:                  serverID,
		ProjectID:           *authCtx.ProjectID,
		Name:                pgtype.Text{String: "Platform MCP eligibility", Valid: true},
		Slug:                pgtype.Text{String: "platform-mcp-eligibility", Valid: true},
		UserSessionIssuerID: uuid.NullUUID{UUID: issuer.ID, Valid: true},
		RemoteMcpServerID:   uuid.NullUUID{UUID: remoteServer.ID, Valid: true},
		Visibility:          "public",
	})
	require.NoError(t, err)

	_, err = mcpendpointsrepo.New(ti.conn).CreateMCPEndpoint(ctx, mcpendpointsrepo.CreateMCPEndpointParams{
		ProjectID:   *authCtx.ProjectID,
		McpServerID: serverID,
		Slug:        "platform-mcp-eligibility",
	})
	require.NoError(t, err)

	eligibility := platformmcp.NewPostgresNewModelEligibility(ti.conn)
	eligible, err := eligibility.EligibleForPlatformMCP(ctx, authCtx.ActiveOrganizationID)
	require.NoError(t, err)
	require.True(t, eligible)

	_, err = mcpserversrepo.New(ti.conn).UpdateMCPServer(ctx, mcpserversrepo.UpdateMCPServerParams{
		Name:                  mcpServer.Name,
		Slug:                  mcpServer.Slug,
		EnvironmentID:         mcpServer.EnvironmentID,
		UserSessionIssuerID:   mcpServer.UserSessionIssuerID,
		RemoteMcpServerID:     mcpServer.RemoteMcpServerID,
		TunneledMcpServerID:   mcpServer.TunneledMcpServerID,
		ToolsetID:             mcpServer.ToolsetID,
		UnproxiedMcpServerID:  mcpServer.UnproxiedMcpServerID,
		ToolVariationsGroupID: mcpServer.ToolVariationsGroupID,
		Visibility:            "disabled",
		ID:                    mcpServer.ID,
		ProjectID:             mcpServer.ProjectID,
	})
	require.NoError(t, err)

	eligible, err = eligibility.EligibleForPlatformMCP(ctx, authCtx.ActiveOrganizationID)
	require.NoError(t, err)
	require.False(t, eligible)
}
