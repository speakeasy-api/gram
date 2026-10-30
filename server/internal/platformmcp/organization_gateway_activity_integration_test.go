package platformmcp

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/authz"
	"github.com/speakeasy-api/gram/server/internal/conv"
	mcpserversrepo "github.com/speakeasy-api/gram/server/internal/mcpservers/repo"
	organizationsrepo "github.com/speakeasy-api/gram/server/internal/organizations/repo"
	"github.com/speakeasy-api/gram/server/internal/orggateway"
	projectsrepo "github.com/speakeasy-api/gram/server/internal/projects/repo"
	"github.com/speakeasy-api/gram/server/internal/remotemcp/remotemcptest"
	remotemcprepo "github.com/speakeasy-api/gram/server/internal/remotemcp/repo"
	telemetryrepo "github.com/speakeasy-api/gram/server/internal/telemetry/repo"
)

type stubGatewayActivityReader struct {
	summary      telemetryrepo.GetOverviewSummaryParams
	usage        telemetryrepo.GetMetaMCPServerUsageParams
	usageResult  telemetryrepo.MetaMCPUsage
	summaryCalls int
}

func (s *stubGatewayActivityReader) GetOverviewSummary(_ context.Context, arg telemetryrepo.GetOverviewSummaryParams) (*telemetryrepo.OverviewSummary, error) {
	s.summary = arg
	s.summaryCalls++
	return &telemetryrepo.OverviewSummary{TotalToolCalls: 5, FailedToolCalls: 2}, nil
}

func (s *stubGatewayActivityReader) GetMetaMCPServerUsage(_ context.Context, arg telemetryrepo.GetMetaMCPServerUsageParams) (telemetryrepo.MetaMCPUsage, error) {
	s.usage = arg
	return s.usageResult, nil
}

func (s *stubGatewayActivityReader) GetTelemetryWatermark(context.Context, telemetryrepo.GetTelemetryWatermarkParams) (int64, error) {
	return time.Now().UnixNano(), nil
}

func seedGatewayActivityServer(t *testing.T, ctx context.Context, conn *pgxpool.Pool, projectID uuid.UUID, name string) uuid.UUID {
	t.Helper()
	remote := remotemcptest.SeedServer(t, ctx, conn, remotemcprepo.CreateServerParams{
		ProjectID: projectID, TransportType: "streamable-http", Url: "https://upstream.example.test/mcp/" + uuid.NewString(),
	})
	server, err := mcpserversrepo.New(conn).CreateMCPServer(ctx, mcpserversrepo.CreateMCPServerParams{
		ID: uuid.New(), ProjectID: projectID, Name: conv.ToPGText(name), Slug: conv.ToPGText("server-" + uuid.NewString()[:8]),
		RemoteMcpServerID: uuid.NullUUID{UUID: remote.ID, Valid: true}, Visibility: "private",
	})
	require.NoError(t, err)
	return server.ID
}

func TestOrganizationGatewayActivitySpansProjects(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	conn, err := platformMCPInfra.CloneTestDatabase(t, "platform_mcp_org_gateway_activity")
	require.NoError(t, err)

	organizationID := "org_" + uuid.NewString()
	_, err = organizationsrepo.New(conn).UpsertOrganizationMetadata(ctx, organizationsrepo.UpsertOrganizationMetadataParams{
		ID: organizationID, Name: "Gateway activity organization", Slug: "org-" + uuid.NewString()[:8], WorkosID: pgtype.Text{}, Whitelisted: pgtype.Bool{},
	})
	require.NoError(t, err)
	projectIDs := make([]uuid.UUID, 0, 2)
	for range 2 {
		project, err := projectsrepo.New(conn).CreateProject(ctx, projectsrepo.CreateProjectParams{
			Name: "Gateway activity project", Slug: "project-" + uuid.NewString()[:8], OrganizationID: organizationID,
		})
		require.NoError(t, err)
		projectIDs = append(projectIDs, project.ID)
	}
	first := seedGatewayActivityServer(t, ctx, conn, projectIDs[0], "Alpha")
	second := seedGatewayActivityServer(t, ctx, conn, projectIDs[1], "Beta")

	reader := &stubGatewayActivityReader{usageResult: telemetryrepo.MetaMCPUsage{
		Funnel: telemetryrepo.MetaMCPFunnelCounts{ListServers: 3, DescribeServer: 2, DescribeTools: 1, ExecuteTool: 5},
		Members: []telemetryrepo.MetaMCPMemberUsageRow{
			{McpServerID: second.String(), ToolCalls: 3, ErrorCount: 2, LastCalledUnixNano: 1},
			{McpServerID: uuid.NewString(), ToolCalls: 1, ErrorCount: 0, LastCalledUnixNano: 1},
			{McpServerID: first.String(), ToolCalls: 1, ErrorCount: 0, LastCalledUnixNano: 1},
		},
	}}
	authorizer := &stubOrgReadAuthorizer{}
	service := NewOrganizationGatewayActivityService(conn, reader, authorizer, allowingBudget())
	require.NotNil(t, service)

	principal := Principal{UserID: "user_1", OrganizationID: organizationID, ConnectionID: "conn_1", Generation: "g1", ClientID: "client_1", Surface: SurfacePlatformMCP}
	out, err := service.Get(ctx, principal, GetOrganizationGatewayActivityInput{Window: "7d"})
	require.NoError(t, err)

	require.Equal(t, []authz.Check{{Scope: authz.ScopeOrgRead, ResourceID: organizationID}}, authorizer.checks)
	gatewayID := orggateway.GatewayID(organizationID).String()
	require.Equal(t, gatewayID, reader.summary.MetaMCPServerID)
	require.Equal(t, gatewayID, reader.usage.MetaMCPServerID)
	wantProjects := []string{projectIDs[0].String(), projectIDs[1].String(), uuid.Nil.String()}
	require.ElementsMatch(t, wantProjects, reader.summary.GramProjectIDs)
	require.ElementsMatch(t, wantProjects, reader.usage.GramProjectIDs)

	require.EqualValues(t, 5, out.DispatchedCalls)
	require.EqualValues(t, 2, out.FailedCalls)
	require.Equal(t, OrganizationGatewayDiscovery{ListServers: 3, DescribeServer: 2, DescribeTools: 1}, out.Discovery)
	require.Equal(t, []OrganizationGatewayServer{
		{Name: "Beta", MCPID: second.String(), ProjectID: projectIDs[1].String(), ToolCalls: 3, ErrorCount: 2},
		{Name: "Alpha", MCPID: first.String(), ProjectID: projectIDs[0].String(), ToolCalls: 1, ErrorCount: 0},
	}, out.TopServers, "servers no longer in the organization are left unnamed")
	require.False(t, out.Envelope.NoObservations)
}

// The dashboard tab requires org:read, so this surface must refuse the same
// callers before reading anything.
func TestOrganizationGatewayActivityRefusesWithoutLiveOrgRead(t *testing.T) {
	t.Parallel()

	conn, err := platformMCPInfra.CloneTestDatabase(t, "platform_mcp_org_gateway_activity_denied")
	require.NoError(t, err)
	denied := errors.New("forbidden")
	reader := &stubGatewayActivityReader{}
	service := NewOrganizationGatewayActivityService(conn, reader, &stubOrgReadAuthorizer{err: denied}, allowingBudget())
	require.NotNil(t, service)

	for _, window := range []string{"", "1y"} {
		_, err = service.Get(t.Context(), shadowAIPrincipal(), GetOrganizationGatewayActivityInput{Window: window})
		require.ErrorIs(t, err, denied, "window %q", window)
	}
	require.Zero(t, reader.summaryCalls)
}

func TestOrganizationGatewayActivityRefusesUnsupportedWindowAsInvalid(t *testing.T) {
	t.Parallel()

	for _, window := range []string{"1y", "90d"} {
		_, err := resolveWindow(window, time.Now(), overviewWindowSpec)
		require.Error(t, err)
		result, ok := organizationGatewayActivityToolResult(err)
		require.True(t, ok, "window %q", window)
		require.True(t, result.IsError)
		text, ok := result.Content[0].(*mcp.TextContent)
		require.True(t, ok)
		require.Contains(t, text.Text, `"invalid_request"`)
	}
}
