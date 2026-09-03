package telemetry_test

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	gen "github.com/speakeasy-api/gram/server/gen/telemetry"
	"github.com/speakeasy-api/gram/server/internal/attr"
	metamcprepo "github.com/speakeasy-api/gram/server/internal/metamcp/repo"
	"github.com/speakeasy-api/gram/server/internal/metamcp/visibility"
	"github.com/speakeasy-api/gram/server/internal/telemetry"
	"github.com/speakeasy-api/gram/server/internal/telemetry/repo"
	"github.com/speakeasy-api/gram/server/internal/testenv"
)

type gatewayEvent struct {
	projectID string
	gatewayID string
	memberID  string
	tool      string
	status    int
	at        time.Time
}

// logGatewayEvent writes one row the way the meta surface emits it: discovery
// under meta_discovery when memberID is empty, else a member tool_call
// stamped with the gateway id. Each row gets its own trace so it lands in
// trace_summaries.
func logGatewayEvent(ctx context.Context, ti *testInstance, e gatewayEvent) {
	attrs := telemetry.HTTPLogAttributes{
		attr.TraceIDKey: strings.ReplaceAll(uuid.NewString(), "-", ""),
	}
	if e.gatewayID != "" {
		attrs[attr.MetaMcpServerIDKey] = e.gatewayID
	}
	urn := "tools:externalmcp:" + e.memberID + ":" + e.tool
	if e.memberID == "" {
		attrs[attr.EventSourceKey] = "meta_discovery"
		urn = "metamcp:" + e.gatewayID + ":" + e.tool
	} else {
		attrs[attr.EventSourceKey] = "tool_call"
		attrs[attr.McpServerIDKey] = e.memberID
	}
	attrs.RecordStatusCode(e.status)
	ti.telemLogger.Log(ctx, telemetry.LogParams{
		Timestamp: e.at,
		ToolInfo: telemetry.ToolInfo{
			ID:             uuid.NewString(),
			URN:            urn,
			Name:           e.tool,
			ProjectID:      e.projectID,
			DeploymentID:   "",
			FunctionID:     nil,
			OrganizationID: ti.orgID,
		},
		UserInfo:   telemetry.UserInfoByID(""),
		Attributes: attrs,
	})
}

// seedGatewayTraffic writes two list_servers, one describe_server, and three
// member calls (memberA x2 with one 502, memberB x1) for the gateway.
func seedGatewayTraffic(ctx context.Context, ti *testInstance, projectID, gatewayID, memberA, memberB string, at time.Time) {
	logGatewayEvent(ctx, ti, gatewayEvent{projectID: projectID, gatewayID: gatewayID, memberID: "", tool: "list_servers", status: 200, at: at})
	logGatewayEvent(ctx, ti, gatewayEvent{projectID: projectID, gatewayID: gatewayID, memberID: "", tool: "list_servers", status: 200, at: at})
	logGatewayEvent(ctx, ti, gatewayEvent{projectID: projectID, gatewayID: gatewayID, memberID: "", tool: "describe_server", status: 200, at: at})
	logGatewayEvent(ctx, ti, gatewayEvent{projectID: projectID, gatewayID: gatewayID, memberID: memberA, tool: "ping", status: 200, at: at})
	logGatewayEvent(ctx, ti, gatewayEvent{projectID: projectID, gatewayID: gatewayID, memberID: memberA, tool: "ping", status: 502, at: at})
	logGatewayEvent(ctx, ti, gatewayEvent{projectID: projectID, gatewayID: gatewayID, memberID: memberB, tool: "ping", status: 200, at: at})
}

func TestGetMetaMcpServerUsage_FunnelAndMembers(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestLogsService(t)
	now := time.Now().UTC()
	gatewayID := uuid.NewString()
	memberA := uuid.NewString()
	memberB := uuid.NewString()
	seedGatewayTraffic(ctx, ti, ti.projectID, gatewayID, memberA, memberB, now)
	// Outside the window, another project, and no gateway: none may count.
	seedGatewayTraffic(ctx, ti, ti.projectID, gatewayID, memberA, memberB, now.Add(-2*time.Hour))
	seedGatewayTraffic(ctx, ti, uuid.NewString(), gatewayID, memberA, memberB, now)
	logGatewayEvent(ctx, ti, gatewayEvent{projectID: ti.projectID, gatewayID: "", memberID: memberA, tool: "ping", status: 200, at: now})

	testenv.FlushClickHouseAsyncInserts(t, ti.chConn)
	result, err := ti.service.GetMetaMcpServerUsage(ctx, &gen.GetMetaMcpServerUsagePayload{
		MetaMcpServerID: gatewayID,
		From:            now.Add(-time.Hour).Format(time.RFC3339),
		To:              now.Add(time.Hour).Format(time.RFC3339),
	})
	require.NoError(t, err)

	require.Equal(t, int64(2), result.Funnel.ListServers)
	require.Equal(t, int64(1), result.Funnel.DescribeServer)
	require.Equal(t, int64(0), result.Funnel.DescribeTools)
	require.Equal(t, int64(3), result.Funnel.ExecuteTool)

	require.Len(t, result.Members, 2)
	require.Equal(t, memberA, result.Members[0].McpServerID, "most active member first")
	require.Equal(t, int64(2), result.Members[0].ToolCalls)
	require.Equal(t, int64(1), result.Members[0].ErrorCount)
	require.Equal(t, memberB, result.Members[1].McpServerID)
	require.Equal(t, int64(1), result.Members[1].ToolCalls)
	require.Equal(t, int64(0), result.Members[1].ErrorCount)
	require.NotNil(t, result.Members[0].LastCalledAt)
}

func TestGetMetaMcpServerUsage_RejectsEmptyGatewayID(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestLogsService(t)
	now := time.Now().UTC()
	_, err := ti.service.GetMetaMcpServerUsage(ctx, &gen.GetMetaMcpServerUsagePayload{
		MetaMcpServerID: "",
		From:            now.Add(-time.Hour).Format(time.RFC3339),
		To:              now.Add(time.Hour).Format(time.RFC3339),
	})
	require.Error(t, err)
}

func TestGetObservabilityOverview_MetaMcpServerFilter(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestLogsService(t)
	now := time.Now().UTC()
	gatewayID := uuid.NewString()
	seedGatewayTraffic(ctx, ti, ti.projectID, gatewayID, uuid.NewString(), uuid.NewString(), now)

	from := now.Add(-time.Hour).Format(time.RFC3339)
	to := now.Add(time.Hour).Format(time.RFC3339)

	testenv.FlushClickHouseAsyncInserts(t, ti.chConn)
	filter := gatewayID
	result, err := ti.service.GetObservabilityOverview(ctx, &gen.GetObservabilityOverviewPayload{
		From:            from,
		To:              to,
		MetaMcpServerID: &filter,
	})
	require.NoError(t, err)
	require.Equal(t, int64(3), result.Summary.TotalToolCalls, "member tool calls only, never discovery rows")

	other := uuid.NewString()
	result, err = ti.service.GetObservabilityOverview(ctx, &gen.GetObservabilityOverviewPayload{
		From:            from,
		To:              to,
		MetaMcpServerID: &other,
	})
	require.NoError(t, err)
	require.Equal(t, int64(0), result.Summary.TotalToolCalls)
}

func TestGetMcpServerActivity_IncludesGateways(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestLogsService(t)
	now := time.Now().UTC()

	named, err := metamcprepo.New(ti.conn).CreateMetaMCPServer(ctx, metamcprepo.CreateMetaMCPServerParams{
		OrganizationID:      ti.orgID,
		ProjectID:           uuid.MustParse(ti.projectID),
		Name:                "Named Gateway",
		UserSessionIssuerID: uuid.NullUUID{UUID: uuid.Nil, Valid: false},
		Visibility:          visibility.Private,
	})
	require.NoError(t, err)
	namedID := named.ID.String()
	deletedID := uuid.NewString()
	memberA := uuid.NewString()
	memberB := uuid.NewString()

	seedGatewayTraffic(ctx, ti, ti.projectID, namedID, memberA, memberB, now)
	// Inside the 90-day lookback but outside the 14-day recent window.
	logGatewayEvent(ctx, ti, gatewayEvent{projectID: ti.projectID, gatewayID: namedID, memberID: memberA, tool: "ping", status: 200, at: now.Add(-30 * 24 * time.Hour)})
	// A gateway with no Postgres row keeps its id as the label.
	logGatewayEvent(ctx, ti, gatewayEvent{projectID: ti.projectID, gatewayID: deletedID, memberID: memberA, tool: "ping", status: 200, at: now})
	// Another project's traffic through the same gateway must not leak in.
	seedGatewayTraffic(ctx, ti, uuid.NewString(), namedID, memberA, memberB, now)
	// A direct (non-gateway) member call must not produce an empty gateway row.
	logGatewayEvent(ctx, ti, gatewayEvent{projectID: ti.projectID, gatewayID: "", memberID: memberA, tool: "ping", status: 200, at: now})

	testenv.FlushClickHouseAsyncInserts(t, ti.chConn)
	result, err := ti.service.GetMcpServerActivity(ctx, &gen.GetMcpServerActivityPayload{RecentWindowDays: 14})
	require.NoError(t, err)

	gateways := map[string]*gen.McpServerActivity{}
	for _, row := range result.Activity {
		if row.TargetType == gen.McpServerActivityTargetType(repo.ToolUsageTargetTypeMetaMCP) {
			require.NotEmpty(t, row.TargetID, "direct calls must not surface as an empty gateway")
			gateways[row.TargetID] = row
		}
	}
	require.Len(t, gateways, 2)

	require.Equal(t, "Named Gateway", gateways[namedID].TargetLabel)
	require.Equal(t, int64(4), gateways[namedID].TotalToolCalls, "member tool calls only, across the lookback")
	require.Equal(t, int64(3), gateways[namedID].RecentToolCalls)
	require.NotNil(t, gateways[namedID].LastToolCallAt)

	require.Equal(t, deletedID, gateways[deletedID].TargetLabel)
	require.Equal(t, int64(1), gateways[deletedID].TotalToolCalls)

	for i := 1; i < len(result.Activity); i++ {
		require.GreaterOrEqual(t, result.Activity[i-1].TotalToolCalls, result.Activity[i].TotalToolCalls, "rows sorted by total calls across target types")
	}
}

func TestListToolTraces_HidesGatewayDiscoveryByDefault(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestLogsService(t)
	now := time.Now().UTC()
	gatewayID := uuid.NewString()
	seedGatewayTraffic(ctx, ti, ti.projectID, gatewayID, uuid.NewString(), uuid.NewString(), now)

	testenv.FlushClickHouseAsyncInserts(t, ti.chConn)
	params := repo.ListToolTracesParams{
		GramProjectID:    ti.projectID,
		TimeStart:        now.Add(-time.Hour).UnixNano(),
		TimeEnd:          now.Add(time.Hour).UnixNano(),
		GramDeploymentID: "",
		GramFunctionID:   "",
		GramURN:          "",
		EventSource:      "",
		SortOrder:        "desc",
		Cursor:           "",
		Limit:            50,
	}
	traces, err := ti.chClient.ListToolTraces(ctx, params)
	require.NoError(t, err)
	require.Len(t, traces, 3, "only the member tool calls are tool traces")
	for _, tr := range traces {
		require.NotNil(t, tr.EventSource)
		require.Equal(t, "tool_call", *tr.EventSource)
	}

	params.EventSource = "meta_discovery"
	traces, err = ti.chClient.ListToolTraces(ctx, params)
	require.NoError(t, err)
	require.Len(t, traces, 3, "an explicit event_source filter still surfaces discovery")
}
