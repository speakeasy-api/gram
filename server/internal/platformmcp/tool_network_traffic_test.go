package platformmcp

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/authz"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	mcpserversrepo "github.com/speakeasy-api/gram/server/internal/mcpservers/repo"
	telemetryrepo "github.com/speakeasy-api/gram/server/internal/telemetry/repo"
	"github.com/speakeasy-api/gram/server/internal/testenv"
	"github.com/speakeasy-api/gram/server/internal/urn"
)

type recordingNetworkTrafficReader struct {
	params telemetryrepo.GetMCPNetworkTrafficParams
	rows   []telemetryrepo.MCPNetworkTrafficRow
}

func (r *recordingNetworkTrafficReader) GetMCPNetworkTraffic(_ context.Context, params telemetryrepo.GetMCPNetworkTrafficParams) ([]telemetryrepo.MCPNetworkTrafficRow, error) {
	r.params = params
	return r.rows, nil
}

func TestMCPNetworkTrafficToolSchemaMatchesUnavailable(t *testing.T) {
	t.Parallel()
	live := newRegistrar(mcp.NewServer(&mcp.Implementation{Name: "traffic-live", Version: "0.0.1"}, nil))
	unavailable := newRegistrar(mcp.NewServer(&mcp.Implementation{Name: "traffic-unavailable", Version: "0.0.1"}, nil))
	registerMCPNetworkTrafficTool(live, nil)
	registerUnavailableMCPNetworkTrafficTool(unavailable)
	require.JSONEq(t, string(live.Descriptors()[0].InputSchema), string(unavailable.Descriptors()[0].InputSchema))
	require.Equal(t, live.Descriptors()[0].Meta, unavailable.Descriptors()[0].Meta)
}

func TestMCPNetworkTrafficRequiresTargetReadAndReturnsBoundedSummary(t *testing.T) {
	t.Parallel()
	ctx := t.Context()
	conn, err := platformMCPInfra.CloneTestDatabase(t, "platform_mcp_network_traffic")
	require.NoError(t, err)
	principal, project := seedRegistrationLifecycle(t, ctx, conn)
	servers, err := mcpserversrepo.New(conn).ListMCPServersForTelemetryByProjectID(ctx, project.ID)
	require.NoError(t, err)
	require.Len(t, servers, 1)
	server := servers[0]
	lastSeen := time.Now().UTC().Add(-time.Minute)
	traffic := &recordingNetworkTrafficReader{rows: []telemetryrepo.MCPNetworkTrafficRow{{Surface: "public", RequestCount: 3, LastSeen: lastSeen}, {Surface: "private", RequestCount: 2, LastSeen: lastSeen.Add(-time.Minute)}}}
	engine := authz.NewEngine(testenv.NewLogger(t), conn, func(context.Context, string) (bool, error) { return false, nil }, nil)
	reader := NewPostgresReader(testenv.NewLogger(t), conn).WithAuthorization(engine).WithMCPNetworkTraffic(traffic, alwaysEnabledFeature)
	input := MCPNetworkTrafficInput{ProjectID: project.ID.String(), TargetKind: "mcp", TargetID: server.ID.String(), Window: "24h"}
	ctx = contextvalues.WithAuthenticatedActor(ctx, &contextvalues.AuthContext{ActiveOrganizationID: principal.OrganizationID, UserID: principal.UserID}, urn.NewPrincipal(urn.PrincipalTypeUser, principal.UserID))
	ctx = contextvalues.SetActingSurface(ctx, contextvalues.ActingSurfacePlatformMCP)
	projectGrant := authz.NewGrant(authz.ScopeProjectRead, project.ID.String())
	_, err = reader.GetMCPNetworkTraffic(authz.GrantsToContext(ctx, []authz.Grant{projectGrant}), principal, input)
	require.ErrorIs(t, err, ErrMCPConnectionSettingsNotFound)
	require.Empty(t, traffic.params.ServerID)

	serverRow, err := mcpserversrepo.New(conn).GetMCPServerByIDAndProjectID(ctx, mcpserversrepo.GetMCPServerByIDAndProjectIDParams{ID: server.ID, ProjectID: project.ID})
	require.NoError(t, err)
	resourceID := serverRow.ID.String()
	if serverRow.ToolsetID.Valid {
		resourceID = serverRow.ToolsetID.UUID.String()
	}
	ctx = authz.GrantsToContext(ctx, []authz.Grant{projectGrant, authz.NewGrant(authz.ScopeMCPRead, resourceID)})
	output, err := reader.GetMCPNetworkTraffic(ctx, principal, input)
	require.NoError(t, err)
	require.Equal(t, project.ID.String(), traffic.params.GramProjectID)
	require.Equal(t, server.ID.String(), traffic.params.ServerID)
	require.Equal(t, telemetryrepo.MCPNetworkTrafficServerKindMCP, traffic.params.ServerKind)
	require.Equal(t, 24*time.Hour, traffic.params.To.Sub(traffic.params.From))
	require.EqualValues(t, 3, output.Public.Requests)
	require.EqualValues(t, 2, output.Private.Requests)
	require.Equal(t, lastSeen.Format(time.RFC3339), output.Public.LastSeenAt)
	require.Equal(t, lastSeen.Add(-time.Minute).Format(time.RFC3339), output.Private.LastSeenAt)
	require.Equal(t, "24h", output.Window)
	require.Equal(t, traffic.params.From.Format(time.RFC3339), output.From)
	require.Equal(t, traffic.params.To.Format(time.RFC3339), output.To)
	require.NotEmpty(t, output.Caveat)
	encoded, err := json.Marshal(output)
	require.NoError(t, err)
	require.NotContains(t, string(encoded), "attributes")

	input.TargetID = "not-an-id"
	_, err = reader.GetMCPNetworkTraffic(ctx, principal, input)
	require.Error(t, err)

	input.TargetID = server.ID.String()
	reader.WithMCPNetworkTraffic(traffic, alwaysDisabledFeature)
	_, err = reader.GetMCPNetworkTraffic(ctx, principal, input)
	var refusal *ToolRefusalError
	require.ErrorAs(t, err, &refusal)
	require.Equal(t, "observability_disabled", refusal.Code)
}
