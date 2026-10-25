package telemetry_test

import (
	"context"
	"strconv"
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
)

type gatewayEvent struct {
	projectID string
	gatewayID string
	memberID  string
	// sourceID is the member's backend id in the tool urn; defaults to memberID.
	sourceID string
	tool     string
	status   int
	at       time.Time
	// traceID groups rows written by one request; defaults to a fresh trace.
	traceID string
}

// logGatewayEvent writes one row the way the meta surface emits it: discovery
// under meta_discovery when memberID is empty, else a member tool_call
// stamped with the gateway id. Each row gets its own trace so it lands in
// trace_summaries.
func logGatewayEvent(t *testing.T, ctx context.Context, ti *testInstance, e gatewayEvent) {
	t.Helper()

	traceID := e.traceID
	if traceID == "" {
		traceID = strings.ReplaceAll(uuid.NewString(), "-", "")
	}
	attrs := telemetry.HTTPLogAttributes{
		attr.TraceIDKey: traceID,
	}
	if e.gatewayID != "" {
		attrs[attr.MetaMcpServerIDKey] = e.gatewayID
	}
	sourceID := e.sourceID
	if sourceID == "" {
		sourceID = e.memberID
	}
	urn := "tools:externalmcp:" + sourceID + ":" + e.tool
	if e.memberID == "" {
		attrs[attr.EventSourceKey] = "meta_discovery"
		urn = "metamcp:" + e.gatewayID + ":" + e.tool
	} else {
		attrs[attr.EventSourceKey] = "tool_call"
		attrs[attr.McpServerIDKey] = e.memberID
	}
	attrs.RecordStatusCode(e.status)
	require.NoError(t, ti.telemLogger.LogSyncForTest(ctx, telemetry.LogParams{
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
	}))
}

// seedGatewayTraffic writes two list_servers, one describe_server, and three
// member calls (memberA x2 with one 502, memberB x1) for the gateway.
func seedGatewayTraffic(t *testing.T, ctx context.Context, ti *testInstance, projectID, gatewayID, memberA, memberB string, at time.Time) {
	t.Helper()

	logGatewayEvent(t, ctx, ti, gatewayEvent{projectID: projectID, gatewayID: gatewayID, memberID: "", tool: "list_servers", status: 200, at: at})
	logGatewayEvent(t, ctx, ti, gatewayEvent{projectID: projectID, gatewayID: gatewayID, memberID: "", tool: "list_servers", status: 200, at: at})
	logGatewayEvent(t, ctx, ti, gatewayEvent{projectID: projectID, gatewayID: gatewayID, memberID: "", tool: "describe_server", status: 200, at: at})
	logGatewayEvent(t, ctx, ti, gatewayEvent{projectID: projectID, gatewayID: gatewayID, memberID: memberA, tool: "ping", status: 200, at: at})
	logGatewayEvent(t, ctx, ti, gatewayEvent{projectID: projectID, gatewayID: gatewayID, memberID: memberA, tool: "ping", status: 502, at: at})
	logGatewayEvent(t, ctx, ti, gatewayEvent{projectID: projectID, gatewayID: gatewayID, memberID: memberB, tool: "ping", status: 200, at: at})
}

func TestGetMetaMcpServerUsage_FunnelAndMembers(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestLogsService(t)
	now := time.Now().UTC()
	gateway := createGateway(t, ctx, ti, "Funnel Gateway")
	gatewayID := gateway.ID.String()
	memberA := createTunneledMCPServerFixture(t, ctx, ti, tunneledMCPServerFixtureParams{name: "Member A", slug: "member-a-" + uuid.NewString()[:8]}).mcpServerID.String()
	memberB := createTunneledMCPServerFixture(t, ctx, ti, tunneledMCPServerFixtureParams{name: "Member B", slug: "member-b-" + uuid.NewString()[:8]}).mcpServerID.String()
	addGatewayMember(t, ctx, ti, gateway.ID, uuid.MustParse(memberA), 0)
	addGatewayMember(t, ctx, ti, gateway.ID, uuid.MustParse(memberB), 1)
	seedGatewayTraffic(t, ctx, ti, ti.projectID, gatewayID, memberA, memberB, now)
	// Outside the window, another project, and no gateway: none may count.
	seedGatewayTraffic(t, ctx, ti, ti.projectID, gatewayID, memberA, memberB, now.Add(-2*time.Hour))
	seedGatewayTraffic(t, ctx, ti, uuid.NewString(), gatewayID, memberA, memberB, now)
	logGatewayEvent(t, ctx, ti, gatewayEvent{projectID: ti.projectID, gatewayID: "", memberID: memberA, tool: "ping", status: 200, at: now})

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

// logListingAttempt writes one connection attempt row the way a Direct
// listing does, committed before it returns.
func logListingAttempt(t *testing.T, ctx context.Context, ti *testInstance, projectID, gatewayID string, at time.Time, stage, outcome string) {
	t.Helper()
	require.NoError(t, ti.telemLogger.LogSyncForTest(ctx, telemetry.LogParams{
		Timestamp: at,
		ToolInfo: telemetry.ToolInfo{
			ID:             "",
			URN:            "",
			Name:           "",
			ProjectID:      projectID,
			DeploymentID:   "",
			FunctionID:     nil,
			OrganizationID: ti.orgID,
		},
		UserInfo: telemetry.UserInfoByID(""),
		Attributes: map[attr.Key]any{
			attr.EventURNKey:                 "urn:telemetry:gram_service:log:mcp_connection_attempt",
			attr.MetaMcpServerIDKey:          gatewayID,
			attr.McpServerIDKey:              uuid.NewString(),
			attr.ConnectionAttemptStageKey:   stage,
			attr.ConnectionAttemptOutcomeKey: outcome,
		},
	}))
}

// discoveryFailureCounts reads a gateway's failure series for [from, to) as
// bucket start → count, keeping only nonzero buckets.
func discoveryFailureCounts(t *testing.T, ctx context.Context, ti *testInstance, gatewayID string, from, to time.Time) (int64, []time.Time, map[time.Time]int64) {
	t.Helper()
	result, err := ti.service.GetMetaMcpServerUsage(ctx, &gen.GetMetaMcpServerUsagePayload{
		MetaMcpServerID: gatewayID,
		From:            from.Format(time.RFC3339),
		To:              to.Format(time.RFC3339),
	})
	require.NoError(t, err)
	starts := []time.Time{}
	counts := map[time.Time]int64{}
	for _, bucket := range result.DiscoveryFailures.Buckets {
		nanos, err := strconv.ParseInt(bucket.BucketTimeUnixNano, 10, 64)
		require.NoError(t, err)
		start := time.Unix(0, nanos).UTC()
		starts = append(starts, start)
		if bucket.Count != 0 {
			counts[start] = bucket.Count
		}
	}
	return result.DiscoveryFailures.IntervalSeconds, starts, counts
}

func TestGetMetaMcpServerUsage_DiscoveryFailures(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestLogsService(t)
	day := time.Now().UTC().Truncate(24 * time.Hour).Add(-24 * time.Hour)
	at := func(hour, minute int) time.Time {
		return day.Add(time.Duration(hour)*time.Hour + time.Duration(minute)*time.Minute)
	}
	failure := func(gatewayID string, when time.Time) {
		logListingAttempt(t, ctx, ti, ti.projectID, gatewayID, when, "discovery_member", "failed")
	}

	t.Run("counts exactly the window, interior from the rollup and edges from raw rows", func(t *testing.T) {
		t.Parallel()
		gatewayID := uuid.NewString()
		from, to := at(10, 10), at(11, 50)
		failure(gatewayID, from.Add(-time.Nanosecond))
		failure(gatewayID, from)
		failure(gatewayID, at(10, 30).Add(-time.Nanosecond))
		failure(gatewayID, at(10, 30))
		failure(gatewayID, at(11, 0))
		failure(gatewayID, at(11, 30).Add(-time.Nanosecond))
		failure(gatewayID, at(11, 30))
		failure(gatewayID, to.Add(-time.Nanosecond))
		failure(gatewayID, to)
		// Not failures, another gateway, and another project, in the interior
		// and at an edge: none count.
		logListingAttempt(t, ctx, ti, ti.projectID, gatewayID, at(10, 45), "discovery_member", "listed")
		logListingAttempt(t, ctx, ti, ti.projectID, gatewayID, at(10, 15), "discovery_member", "credential_blocked")
		logListingAttempt(t, ctx, ti, ti.projectID, gatewayID, at(10, 45), "discovery_listing", "failed")
		failure(uuid.NewString(), at(10, 45))
		logListingAttempt(t, ctx, ti, uuid.NewString(), gatewayID, at(10, 45), "discovery_member", "failed")
		logListingAttempt(t, ctx, ti, uuid.NewString(), gatewayID, at(10, 15), "discovery_member", "failed")

		interval, starts, counts := discoveryFailureCounts(t, ctx, ti, gatewayID, from, to)
		require.Equal(t, int64(1800), interval)
		require.Equal(t, []time.Time{at(10, 0), at(10, 30), at(11, 0), at(11, 30)}, starts)
		require.Equal(t, map[time.Time]int64{at(10, 0): 2, at(10, 30): 1, at(11, 0): 2, at(11, 30): 2}, counts)
	})

	t.Run("reads a window inside one half-hour once", func(t *testing.T) {
		t.Parallel()
		gatewayID := uuid.NewString()
		from, to := at(10, 5), at(10, 20)
		failure(gatewayID, from.Add(-time.Second))
		failure(gatewayID, from)
		failure(gatewayID, to.Add(-time.Second))
		failure(gatewayID, to)
		_, starts, counts := discoveryFailureCounts(t, ctx, ti, gatewayID, from, to)
		require.Equal(t, []time.Time{at(10, 0)}, starts)
		require.Equal(t, map[time.Time]int64{at(10, 0): 2}, counts)
	})

	t.Run("reads a window across one grid line with no whole half-hour once", func(t *testing.T) {
		t.Parallel()
		gatewayID := uuid.NewString()
		failure(gatewayID, at(10, 25))
		failure(gatewayID, at(10, 35))
		_, starts, counts := discoveryFailureCounts(t, ctx, ti, gatewayID, at(10, 20), at(10, 40))
		require.Equal(t, []time.Time{at(10, 0), at(10, 30)}, starts)
		require.Equal(t, map[time.Time]int64{at(10, 0): 1, at(10, 30): 1}, counts)
	})

	t.Run("shows the current half-hour at once", func(t *testing.T) {
		t.Parallel()
		gatewayID := uuid.NewString()
		to := time.Now().UTC().Truncate(time.Second).Add(time.Second)
		if to.Equal(to.Truncate(30 * time.Minute)) {
			to = to.Add(time.Second)
		}
		failure(gatewayID, to.Add(-time.Nanosecond))
		_, starts, counts := discoveryFailureCounts(t, ctx, ti, gatewayID, to.Add(-time.Hour), to)
		current := to.Truncate(30 * time.Minute)
		require.Equal(t, current, starts[len(starts)-1])
		require.Equal(t, map[time.Time]int64{current: 1}, counts)
	})
}

func TestGetMetaMcpServerUsage_FunnelIgnoresDirectListings(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestLogsService(t)
	from := time.Now().UTC().Add(-2 * time.Hour).Truncate(time.Second)
	to := from.Add(time.Hour)
	gatewayID := uuid.NewString()
	// A row labelled as gateway tools/list discovery is a left-out member, not
	// a funnel stage.
	logGatewayEvent(t, ctx, ti, gatewayEvent{projectID: ti.projectID, gatewayID: gatewayID, memberID: "", tool: "tools/list", status: 429, at: from.Add(5 * time.Minute)})
	logGatewayEvent(t, ctx, ti, gatewayEvent{projectID: ti.projectID, gatewayID: gatewayID, memberID: "", tool: "list_servers", status: 200, at: from.Add(5 * time.Minute)})
	logGatewayEvent(t, ctx, ti, gatewayEvent{projectID: ti.projectID, gatewayID: gatewayID, memberID: "", tool: "list_servers", status: 200, at: to})
	logGatewayEvent(t, ctx, ti, gatewayEvent{projectID: ti.projectID, gatewayID: gatewayID, memberID: "", tool: "describe_server", status: 200, at: from.Add(5 * time.Minute)})
	logGatewayEvent(t, ctx, ti, gatewayEvent{projectID: ti.projectID, gatewayID: gatewayID, memberID: uuid.NewString(), tool: "tools/list", status: 502, at: from.Add(5 * time.Minute)})

	result, err := ti.service.GetMetaMcpServerUsage(ctx, &gen.GetMetaMcpServerUsagePayload{
		MetaMcpServerID: gatewayID,
		From:            from.Format(time.RFC3339),
		To:              to.Format(time.RFC3339),
	})
	require.NoError(t, err)
	require.Equal(t, int64(2), result.Funnel.ListServers)
	require.Equal(t, int64(1), result.Funnel.DescribeServer)
	require.Equal(t, int64(0), result.Funnel.DescribeTools)
	require.Equal(t, int64(1), result.Funnel.ExecuteTool)
}

func TestGetMetaMcpServerUsage_DiscoveryFailureBucketsAreBounded(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestLogsService(t)
	gatewayID := uuid.NewString()
	to := time.Now().UTC().Truncate(time.Second)

	for _, tc := range []struct {
		name     string
		window   time.Duration
		interval int64
	}{
		{name: "it uses the half-hour floor for an hour", window: time.Hour, interval: 1800},
		{name: "it uses the half-hour floor for a day", window: 24 * time.Hour, interval: 1800},
		{name: "it uses hour buckets for a week", window: 7 * 24 * time.Hour, interval: 3600},
		{name: "it uses six hour buckets for a month", window: 30 * 24 * time.Hour, interval: 21600},
		{name: "it uses day buckets for a quarter", window: 90 * 24 * time.Hour, interval: 86400},
		{name: "it uses day buckets for a year", window: 365 * 24 * time.Hour, interval: 86400},
		{name: "it widens buckets past a year in half-hour steps", window: 1000 * 24 * time.Hour, interval: 237600},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			from := to.Add(-tc.window)
			interval, starts, _ := discoveryFailureCounts(t, ctx, ti, gatewayID, from, to)
			require.Equal(t, tc.interval, interval)
			require.LessOrEqual(t, len(starts), 366)
			step := time.Duration(interval) * time.Second
			aligned := time.Unix(0, from.UnixNano()/int64(step)*int64(step)).UTC()
			require.Equal(t, aligned, starts[0], "buckets align to UTC epoch multiples of the interval")
			require.True(t, starts[len(starts)-1].Before(to))
			require.False(t, starts[len(starts)-1].Add(step).Before(to))
		})
	}
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
	seedGatewayTraffic(t, ctx, ti, ti.projectID, gatewayID, uuid.NewString(), uuid.NewString(), now)

	from := now.Add(-time.Hour).Format(time.RFC3339)
	to := now.Add(time.Hour).Format(time.RFC3339)

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

	seedGatewayTraffic(t, ctx, ti, ti.projectID, namedID, memberA, memberB, now)
	// Inside the 90-day lookback but outside the 14-day recent window.
	logGatewayEvent(t, ctx, ti, gatewayEvent{projectID: ti.projectID, gatewayID: namedID, memberID: memberA, tool: "ping", status: 200, at: now.Add(-30 * 24 * time.Hour)})
	// A gateway with no Postgres row keeps its id as the label.
	logGatewayEvent(t, ctx, ti, gatewayEvent{projectID: ti.projectID, gatewayID: deletedID, memberID: memberA, tool: "ping", status: 200, at: now})
	// Another project's traffic through the same gateway must not leak in.
	seedGatewayTraffic(t, ctx, ti, uuid.NewString(), namedID, memberA, memberB, now)
	// A direct (non-gateway) member call must not produce an empty gateway row.
	logGatewayEvent(t, ctx, ti, gatewayEvent{projectID: ti.projectID, gatewayID: "", memberID: memberA, tool: "ping", status: 200, at: now})

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
	seedGatewayTraffic(t, ctx, ti, ti.projectID, gatewayID, uuid.NewString(), uuid.NewString(), now)

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
