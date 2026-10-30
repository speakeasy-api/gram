package telemetry_test

import (
	"math/rand/v2"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	gen "github.com/speakeasy-api/gram/server/gen/telemetry"
	"github.com/speakeasy-api/gram/server/internal/orggateway"
)

func TestOrganizationGatewayActivity(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestLogsService(t)
	// Every test shares the fixture organization, and with it the gateway id
	// and the nil project its discovery rows carry, so this test keeps to a
	// window of its own.
	at := time.Now().UTC().Add(-time.Duration(1000+rand.IntN(100_000)) * time.Minute).Truncate(time.Second)
	gatewayID := orggateway.GatewayID(ti.orgID).String()
	member := createTunneledMCPServerFixture(t, ctx, ti, tunneledMCPServerFixtureParams{name: "Member A", slug: "member-a-" + uuid.NewString()[:8]}).mcpServerID.String()
	deleted := uuid.NewString()

	nilProject := uuid.Nil.String()
	logGatewayEvent(t, ctx, ti, gatewayEvent{projectID: nilProject, gatewayID: gatewayID, memberID: "", tool: "list_servers", status: 200, at: at})
	logGatewayEvent(t, ctx, ti, gatewayEvent{projectID: nilProject, gatewayID: gatewayID, memberID: "", tool: "describe_tools", status: 200, at: at})
	logGatewayEvent(t, ctx, ti, gatewayEvent{projectID: ti.projectID, gatewayID: gatewayID, memberID: member, tool: "ping", status: 200, at: at})
	logGatewayEvent(t, ctx, ti, gatewayEvent{projectID: ti.projectID, gatewayID: gatewayID, memberID: member, tool: "ping", status: 502, at: at})
	// A server no longer in the organization still counts as a dispatch but
	// is left out of the breakdown.
	logGatewayEvent(t, ctx, ti, gatewayEvent{projectID: ti.projectID, gatewayID: gatewayID, memberID: deleted, tool: "ping", status: 200, at: at})
	// Another organization's gateway, a project outside the organization, and
	// a call not dispatched through the gateway: none may count.
	logGatewayEvent(t, ctx, ti, gatewayEvent{projectID: nilProject, gatewayID: orggateway.GatewayID(uuid.NewString()).String(), memberID: "", tool: "list_servers", status: 200, at: at})
	logGatewayEvent(t, ctx, ti, gatewayEvent{projectID: uuid.NewString(), gatewayID: gatewayID, memberID: member, tool: "ping", status: 200, at: at})
	logGatewayEvent(t, ctx, ti, gatewayEvent{projectID: ti.projectID, gatewayID: "", memberID: member, tool: "ping", status: 200, at: at})

	from := at.Add(-30 * time.Second).Format(time.RFC3339)
	to := at.Add(30 * time.Second).Format(time.RFC3339)

	usage, err := ti.service.GetOrganizationGatewayUsage(ctx, &gen.GetOrganizationGatewayUsagePayload{From: from, To: to, SessionToken: nil})
	require.NoError(t, err)
	require.Equal(t, int64(1), usage.Funnel.ListServers)
	require.Equal(t, int64(0), usage.Funnel.DescribeServer)
	require.Equal(t, int64(1), usage.Funnel.DescribeTools)
	require.Equal(t, int64(3), usage.Funnel.ExecuteTool)
	require.Len(t, usage.Members, 1)
	require.Equal(t, member, usage.Members[0].McpServerID)
	require.Equal(t, int64(2), usage.Members[0].ToolCalls)
	require.Equal(t, int64(1), usage.Members[0].ErrorCount)
	require.NotNil(t, usage.Members[0].McpServerName)
	require.Equal(t, "Member A", *usage.Members[0].McpServerName)

	overview, err := ti.service.GetOrganizationGatewayOverview(ctx, &gen.GetOrganizationGatewayOverviewPayload{From: from, To: to, SessionToken: nil})
	require.NoError(t, err)
	require.Equal(t, int64(3), overview.Summary.TotalToolCalls)
	require.Equal(t, int64(1), overview.Summary.FailedToolCalls)
	require.NotEmpty(t, overview.TimeSeries)
}
