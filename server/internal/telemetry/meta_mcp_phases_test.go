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
	"github.com/speakeasy-api/gram/server/internal/telemetry"
)

// gatewayPhaseEvent is one gateway row: a member tool_call when member is
// set, else a meta_discovery row named tool.
type gatewayPhaseEvent struct {
	projectID string
	gatewayID string
	member    string
	tool      string
	mode      string
	session   string
	chat      string
	at        time.Time
}

func logGatewayPhaseEvent(t *testing.T, ctx context.Context, ti *testInstance, e gatewayPhaseEvent) {
	t.Helper()

	attrs := telemetry.HTTPLogAttributes{
		attr.TraceIDKey:         strings.ReplaceAll(uuid.NewString(), "-", ""),
		attr.MetaMcpServerIDKey: e.gatewayID,
	}
	urn := "metamcp:" + e.gatewayID + ":" + e.tool
	if e.member != "" {
		attrs[attr.EventSourceKey] = string(telemetry.EventSourceToolCall)
		attrs[attr.McpServerIDKey] = e.member
		urn = "tools:externalmcp:" + e.member + ":" + e.tool
	} else {
		attrs[attr.EventSourceKey] = string(telemetry.EventSourceMetaDiscovery)
	}
	if e.mode != "" {
		attrs[attr.MetaMcpDiscoveryModeKey] = e.mode
	}
	if e.session != "" {
		attrs[attr.McpSessionIDKey] = e.session
	}
	if e.chat != "" {
		attrs[attr.GenAIConversationIDKey] = e.chat
	}
	attrs.RecordStatusCode(200)
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

func TestGetMetaMcpServerUsage_PhasesByModeAndPosition(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestLogsService(t)
	from := time.Now().UTC().Add(-2 * time.Hour).Truncate(time.Second)
	to := from.Add(time.Hour)
	gatewayID := uuid.NewString()
	member := uuid.NewString()
	log := func(e gatewayPhaseEvent) {
		if e.projectID == "" {
			e.projectID = ti.projectID
		}
		if e.gatewayID == "" {
			e.gatewayID = gatewayID
		}
		logGatewayPhaseEvent(t, ctx, ti, e)
	}
	at := func(seconds int) time.Time { return from.Add(time.Duration(seconds) * time.Second) }

	// Progressive walks the drill-down before calling. connection_status and a
	// Direct listing's omission row are neither discovery nor execution, so
	// they never take a position.
	log(gatewayPhaseEvent{tool: "list_servers", mode: "progressive", session: "p1", at: at(1)})
	log(gatewayPhaseEvent{tool: "describe_server", mode: "progressive", session: "p1", at: at(2)})
	log(gatewayPhaseEvent{tool: "connection_status", mode: "progressive", session: "p1", at: at(3)})
	log(gatewayPhaseEvent{tool: "describe_tools", mode: "progressive", session: "p1", at: at(4)})
	log(gatewayPhaseEvent{tool: "tools/list", mode: "progressive", session: "p1", at: at(5)})
	log(gatewayPhaseEvent{member: member, tool: "ping", mode: "progressive", session: "p1", at: at(6)})
	log(gatewayPhaseEvent{member: member, tool: "ping", mode: "progressive", session: "p1", at: at(7)})
	// Direct calls from the first position; one lands on the inclusive end.
	log(gatewayPhaseEvent{member: member, tool: "ping", mode: "direct", session: "d1", at: at(10)})
	log(gatewayPhaseEvent{member: member, tool: "ping", mode: "direct", session: "d1", at: at(11)})
	log(gatewayPhaseEvent{member: member, tool: "ping", mode: "direct", session: "d1", at: at(12)})
	log(gatewayPhaseEvent{member: member, tool: "ping", mode: "direct", session: "d1", at: to})
	// Code Mode keyed by its chat: execute runs contain calls and take no
	// position themselves.
	log(gatewayPhaseEvent{tool: "execute", mode: "code_mode", chat: "chat-c1", at: at(20)})
	log(gatewayPhaseEvent{tool: "tools.search", mode: "code_mode", chat: "chat-c1", at: at(21)})
	log(gatewayPhaseEvent{tool: "tools.describe", mode: "code_mode", chat: "chat-c1", at: at(22)})
	log(gatewayPhaseEvent{member: member, tool: "ping", mode: "code_mode", chat: "chat-c1", at: at(23)})
	log(gatewayPhaseEvent{tool: "execute", mode: "code_mode", chat: "chat-c1", at: at(24)})
	log(gatewayPhaseEvent{member: member, tool: "ping", mode: "code_mode", chat: "chat-c1", at: at(25)})
	// Rows from before modes were recorded, with no session: a member call is
	// unrecorded and a drill-down tool is still Progressive.
	log(gatewayPhaseEvent{member: member, tool: "ping", at: at(30)})
	log(gatewayPhaseEvent{tool: "list_servers", at: at(31)})
	// The previous window.
	log(gatewayPhaseEvent{tool: "list_servers", mode: "progressive", session: "p0", at: from.Add(-30 * time.Minute)})
	log(gatewayPhaseEvent{member: member, tool: "ping", mode: "progressive", session: "p0", at: from.Add(-29 * time.Minute)})
	// After the window, another gateway, and another project: none may count.
	log(gatewayPhaseEvent{member: member, tool: "ping", mode: "direct", session: "d1", at: to.Add(time.Second)})
	log(gatewayPhaseEvent{gatewayID: uuid.NewString(), member: member, tool: "ping", mode: "direct", session: "d1", at: at(40)})
	log(gatewayPhaseEvent{projectID: uuid.NewString(), member: member, tool: "ping", mode: "direct", session: "d1", at: at(40)})

	result, err := ti.service.GetMetaMcpServerUsage(ctx, &gen.GetMetaMcpServerUsagePayload{
		MetaMcpServerID: gatewayID,
		From:            from.Format(time.RFC3339),
		To:              to.Format(time.RFC3339),
	})
	require.NoError(t, err)
	phases := result.Phases
	require.NotNil(t, phases)

	require.Equal(t, []*gen.MetaMcpModePhases{
		{Mode: "progressive", DiscoveryCalls: 4, ExecutionCalls: 2, ExecuteRuns: 0, PreviousDiscoveryCalls: 1, PreviousExecutionCalls: 1, PreviousExecuteRuns: 0},
		{Mode: "code_mode", DiscoveryCalls: 2, ExecutionCalls: 2, ExecuteRuns: 2, PreviousDiscoveryCalls: 0, PreviousExecutionCalls: 0, PreviousExecuteRuns: 0},
		{Mode: "direct", DiscoveryCalls: 0, ExecutionCalls: 4, ExecuteRuns: 0, PreviousDiscoveryCalls: 0, PreviousExecutionCalls: 0, PreviousExecuteRuns: 0},
		{Mode: "unrecorded", DiscoveryCalls: 0, ExecutionCalls: 1, ExecuteRuns: 0, PreviousDiscoveryCalls: 0, PreviousExecutionCalls: 0, PreviousExecuteRuns: 0},
	}, phases.Modes)

	require.Equal(t, []*gen.MetaMcpPositionPhases{
		{Mode: "progressive", Position: "1", DiscoveryCalls: 1, ExecutionCalls: 0},
		{Mode: "progressive", Position: "2", DiscoveryCalls: 1, ExecutionCalls: 0},
		{Mode: "progressive", Position: "3-5", DiscoveryCalls: 1, ExecutionCalls: 2},
		{Mode: "code_mode", Position: "1", DiscoveryCalls: 1, ExecutionCalls: 0},
		{Mode: "code_mode", Position: "2", DiscoveryCalls: 1, ExecutionCalls: 0},
		{Mode: "code_mode", Position: "3-5", DiscoveryCalls: 0, ExecutionCalls: 2},
		{Mode: "direct", Position: "1", DiscoveryCalls: 0, ExecutionCalls: 1},
		{Mode: "direct", Position: "2", DiscoveryCalls: 0, ExecutionCalls: 1},
		{Mode: "direct", Position: "3-5", DiscoveryCalls: 0, ExecutionCalls: 2},
	}, phases.Positions)
	require.Equal(t, int64(2), phases.CallsWithoutSession)

	// An hour buckets by five minutes from the window start; the row on the
	// inclusive end joins the last bucket.
	require.Equal(t, int64(300), phases.IntervalSeconds)
	first := strconv.FormatInt(from.UnixNano(), 10)
	last := strconv.FormatInt(from.Add(55*time.Minute).UnixNano(), 10)
	require.Equal(t, []*gen.MetaMcpPhaseBucket{
		{BucketTimeUnixNano: first, Mode: "progressive", DiscoveryCalls: 4, ExecutionCalls: 2, ExecuteRuns: 0},
		{BucketTimeUnixNano: first, Mode: "code_mode", DiscoveryCalls: 2, ExecutionCalls: 2, ExecuteRuns: 2},
		{BucketTimeUnixNano: first, Mode: "direct", DiscoveryCalls: 0, ExecutionCalls: 3, ExecuteRuns: 0},
		{BucketTimeUnixNano: first, Mode: "unrecorded", DiscoveryCalls: 0, ExecutionCalls: 1, ExecuteRuns: 0},
		{BucketTimeUnixNano: last, Mode: "direct", DiscoveryCalls: 0, ExecutionCalls: 1, ExecuteRuns: 0},
	}, phases.Series)

	// The Progressive funnel keeps counting its stages beside the phases.
	require.Equal(t, int64(2), result.Funnel.ListServers)
	require.Equal(t, int64(1), result.Funnel.DescribeServer)
	require.Equal(t, int64(1), result.Funnel.DescribeTools)
}

func TestGetMetaMcpServerUsage_PhaseIntervalFollowsWindow(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestLogsService(t)
	gatewayID := uuid.NewString()
	to := time.Now().UTC().Truncate(time.Second)

	for _, tc := range []struct {
		name     string
		window   time.Duration
		interval int64
	}{
		{name: "it uses five minute buckets for an hour", window: time.Hour, interval: 300},
		{name: "it uses five minute buckets for six hours", window: 6 * time.Hour, interval: 300},
		{name: "it uses hour buckets for a day", window: 24 * time.Hour, interval: 3600},
		{name: "it uses day buckets for a week", window: 7 * 24 * time.Hour, interval: 86400},
		{name: "it uses day buckets for a quarter", window: 90 * 24 * time.Hour, interval: 86400},
		{name: "it widens buckets past a year", window: 1000 * 24 * time.Hour, interval: 236066},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			result, err := ti.service.GetMetaMcpServerUsage(ctx, &gen.GetMetaMcpServerUsagePayload{
				MetaMcpServerID: gatewayID,
				From:            to.Add(-tc.window).Format(time.RFC3339),
				To:              to.Format(time.RFC3339),
			})
			require.NoError(t, err)
			require.Equal(t, tc.interval, result.Phases.IntervalSeconds)
			require.Empty(t, result.Phases.Series)
			require.Empty(t, result.Phases.Modes)
		})
	}
}
