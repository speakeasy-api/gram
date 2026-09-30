package telemetry_test

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/telemetry"
	telemetryRepo "github.com/speakeasy-api/gram/server/internal/telemetry/repo"
	"github.com/speakeasy-api/gram/server/internal/testenv"
)

type healthToolCallParams struct {
	projectID   string
	timestamp   time.Time
	mcpServerID string
	toolsetSlug string
	statusCode  int
}

// insertHealthToolCall records one direct-lane tools/call as the hosted or
// proxied MCP path writes it: whichever of the server id and toolset slug the
// server carries, a tools: URN, a status code and a trace id.
func insertHealthToolCall(t *testing.T, ctx context.Context, ti *testInstance, p healthToolCallParams) {
	t.Helper()

	attrs := map[string]any{
		"gram.event.source":         "tool_call",
		"gram.tool.name":            "search",
		"http.response.status_code": p.statusCode,
	}
	if p.mcpServerID != "" {
		attrs["gram.mcp_server.id"] = p.mcpServerID
	}
	if p.toolsetSlug != "" {
		attrs["gram.toolset.slug"] = p.toolsetSlug
	}
	attrsJSON, err := json.Marshal(attrs)
	require.NoError(t, err)

	traceID := strings.ReplaceAll(uuid.New().String(), "-", "")
	spanID := traceID[:16]
	err = ti.chClient.InsertTelemetryLogsSync(ctx, []telemetryRepo.InsertTelemetryLogParams{{
		ID:                   uuid.New().String(),
		TimeUnixNano:         p.timestamp.UnixNano(),
		ObservedTimeUnixNano: p.timestamp.UnixNano(),
		SeverityText:         nil,
		Body:                 "tool call",
		TraceID:              &traceID,
		SpanID:               &spanID,
		Attributes:           string(attrsJSON),
		ResourceAttributes:   "{}",
		GramProjectID:        p.projectID,
		GramDeploymentID:     nil,
		GramFunctionID:       nil,
		GramURN:              "tools:http:health:search",
		ServiceName:          "gram-server",
		ServiceVersion:       nil,
		GramChatID:           nil,
	}})
	require.NoError(t, err)
}

func healthWindow() (time.Time, time.Time) {
	to := time.Now().UTC()
	return to.Add(-14 * 24 * time.Hour), to
}

func seriesTotals(points []telemetry.MCPServerSeriesPoint) (int64, int64) {
	var total, failed int64
	for _, p := range points {
		total += p.Total
		failed += p.Failed
	}
	return total, failed
}

func TestMCPServerHealth_RemoteServer(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestLogsService(t)
	reader := telemetry.NewMCPServerHealth(ti.conn, ti.chConn)
	projectID := uuid.NewString()
	serverID := uuid.NewString()
	now := time.Now().UTC()

	insertHealthToolCall(t, ctx, ti, healthToolCallParams{projectID: projectID, timestamp: now.Add(-5 * time.Minute), mcpServerID: serverID, statusCode: 200})
	insertHealthToolCall(t, ctx, ti, healthToolCallParams{projectID: projectID, timestamp: now.Add(-4 * time.Minute), mcpServerID: serverID, statusCode: 401})
	insertHealthToolCall(t, ctx, ti, healthToolCallParams{projectID: projectID, timestamp: now.Add(-3 * time.Minute), mcpServerID: serverID, statusCode: 502})
	insertHealthToolCall(t, ctx, ti, healthToolCallParams{projectID: projectID, timestamp: now.Add(-2 * time.Minute), mcpServerID: uuid.NewString(), statusCode: 200})
	// Hook-observed call to the server's URL.
	insertHookEvent(t, ctx, hookEventParams{
		projectID: projectID, deploymentID: uuid.NewString(), timestamp: now.Add(-time.Minute), traceID: uuid.NewString(),
		hookSource: "claude-code", toolName: "search", result: `"ok"`, mcpServerURL: "https://mcp.example.com/mcp/remote-slug",
	})
	testenv.FlushClickHouseAsyncInserts(t, ti.chConn)

	target := telemetry.MCPServerTelemetryTarget{ProjectID: projectID, MCPServerID: serverID, ToolsetSlug: "", URLSlug: "remote-slug"}
	from, to := healthWindow()

	outcomes, err := reader.Outcomes(ctx, target, from, to)
	require.NoError(t, err)
	require.Equal(t, int64(2), outcomes.Success)
	require.Equal(t, int64(1), outcomes.Unauthorized)
	require.Equal(t, int64(1), outcomes.ServerError)
	require.Zero(t, outcomes.ClientError)
	require.False(t, outcomes.Watermark.IsZero())

	points, err := reader.Series(ctx, target, from, to, 24*time.Hour)
	require.NoError(t, err)
	total, failed := seriesTotals(points)
	require.Equal(t, int64(3), total, "the series reads the direct lane only")
	require.Equal(t, int64(2), failed)
}

func TestMCPServerHealth_ToolsetBackedServer(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestLogsService(t)
	reader := telemetry.NewMCPServerHealth(ti.conn, ti.chConn)
	projectID := uuid.NewString()
	serverID := uuid.NewString()
	now := time.Now().UTC()

	insertHealthToolCall(t, ctx, ti, healthToolCallParams{projectID: projectID, timestamp: now.Add(-5 * time.Minute), mcpServerID: serverID, toolsetSlug: "backed", statusCode: 200})
	insertHealthToolCall(t, ctx, ti, healthToolCallParams{projectID: projectID, timestamp: now.Add(-4 * time.Minute), toolsetSlug: "backed", statusCode: 404})
	testenv.FlushClickHouseAsyncInserts(t, ti.chConn)

	target := telemetry.MCPServerTelemetryTarget{ProjectID: projectID, MCPServerID: serverID, ToolsetSlug: "backed", URLSlug: "backed"}
	from, to := healthWindow()

	outcomes, err := reader.Outcomes(ctx, target, from, to)
	require.NoError(t, err)
	require.Equal(t, int64(1), outcomes.Success)
	require.Equal(t, int64(1), outcomes.ClientError, "a row carrying only the toolset slug is matched through it")

	points, err := reader.Series(ctx, target, from, to, 24*time.Hour)
	require.NoError(t, err)
	total, failed := seriesTotals(points)
	require.Equal(t, int64(2), total, "the series matches either identity and counts a row carrying both once")
	require.Equal(t, int64(1), failed)
}

func TestMCPServerHealth_ToolsetOnlyServer(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestLogsService(t)
	reader := telemetry.NewMCPServerHealth(ti.conn, ti.chConn)
	projectID := uuid.NewString()
	now := time.Now().UTC()

	insertHealthToolCall(t, ctx, ti, healthToolCallParams{projectID: projectID, timestamp: now.Add(-5 * time.Minute), toolsetSlug: "only", statusCode: 200})
	insertHealthToolCall(t, ctx, ti, healthToolCallParams{projectID: projectID, timestamp: now.Add(-4 * time.Minute), toolsetSlug: "only", statusCode: 500})
	insertHealthToolCall(t, ctx, ti, healthToolCallParams{projectID: projectID, timestamp: now.Add(-3 * time.Minute), toolsetSlug: "another", statusCode: 200})
	testenv.FlushClickHouseAsyncInserts(t, ti.chConn)

	target := telemetry.MCPServerTelemetryTarget{ProjectID: projectID, MCPServerID: "", ToolsetSlug: "only", URLSlug: "only"}
	from, to := healthWindow()

	outcomes, err := reader.Outcomes(ctx, target, from, to)
	require.NoError(t, err)
	require.Equal(t, int64(1), outcomes.Success)
	require.Equal(t, int64(1), outcomes.ServerError)

	points, err := reader.Series(ctx, target, from, to, 24*time.Hour)
	require.NoError(t, err)
	total, failed := seriesTotals(points)
	require.Equal(t, int64(2), total)
	require.Equal(t, int64(1), failed)
}

func TestMCPServerHealth_SharedToolsetDropsSlug(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestLogsService(t)
	reader := telemetry.NewMCPServerHealth(ti.conn, ti.chConn)
	projectID := uuid.NewString()
	first := uuid.NewString()
	second := uuid.NewString()
	now := time.Now().UTC()

	insertHealthToolCall(t, ctx, ti, healthToolCallParams{projectID: projectID, timestamp: now.Add(-5 * time.Minute), mcpServerID: first, toolsetSlug: "shared", statusCode: 200})
	insertHealthToolCall(t, ctx, ti, healthToolCallParams{projectID: projectID, timestamp: now.Add(-4 * time.Minute), mcpServerID: second, toolsetSlug: "shared", statusCode: 200})
	insertHealthToolCall(t, ctx, ti, healthToolCallParams{projectID: projectID, timestamp: now.Add(-3 * time.Minute), mcpServerID: second, toolsetSlug: "shared", statusCode: 500})
	testenv.FlushClickHouseAsyncInserts(t, ti.chConn)

	// The admin drops the slug when two live wrappers share the toolset, so
	// only the server id selects rows.
	target := telemetry.MCPServerTelemetryTarget{ProjectID: projectID, MCPServerID: first, ToolsetSlug: "", URLSlug: "first-wrapper"}
	from, to := healthWindow()

	outcomes, err := reader.Outcomes(ctx, target, from, to)
	require.NoError(t, err)
	require.Equal(t, int64(1), outcomes.Success)
	require.Zero(t, outcomes.ServerError, "the other wrapper's calls are not counted")
}

func TestMCPServerHealth_SeriesBuckets(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestLogsService(t)
	reader := telemetry.NewMCPServerHealth(ti.conn, ti.chConn)
	projectID := uuid.NewString()
	serverID := uuid.NewString()
	to := time.Now().UTC()

	insertHealthToolCall(t, ctx, ti, healthToolCallParams{projectID: projectID, timestamp: to.Add(-2 * time.Hour), mcpServerID: serverID, statusCode: 200})
	insertHealthToolCall(t, ctx, ti, healthToolCallParams{projectID: projectID, timestamp: to.Add(-20 * 24 * time.Hour), mcpServerID: serverID, statusCode: 500})
	testenv.FlushClickHouseAsyncInserts(t, ti.chConn)

	target := telemetry.MCPServerTelemetryTarget{ProjectID: projectID, MCPServerID: serverID, ToolsetSlug: "", URLSlug: "series"}

	daily, err := reader.Series(ctx, target, to.Add(-30*24*time.Hour), to, 24*time.Hour)
	require.NoError(t, err)
	require.GreaterOrEqual(t, len(daily), 30, "every day in the window is filled")
	require.LessOrEqual(t, len(daily), 32)
	for i := 1; i < len(daily); i++ {
		require.Equal(t, 24*time.Hour, daily[i].BucketStart.Sub(daily[i-1].BucketStart))
	}
	total, failed := seriesTotals(daily)
	require.Equal(t, int64(2), total)
	require.Equal(t, int64(1), failed)

	weekly, err := reader.Series(ctx, target, to.Add(-90*24*time.Hour), to, 7*24*time.Hour)
	require.NoError(t, err)
	require.GreaterOrEqual(t, len(weekly), 13)
	require.LessOrEqual(t, len(weekly), 15)
	for i := 1; i < len(weekly); i++ {
		require.Equal(t, 7*24*time.Hour, weekly[i].BucketStart.Sub(weekly[i-1].BucketStart))
	}
	total, failed = seriesTotals(weekly)
	require.Equal(t, int64(2), total)
	require.Equal(t, int64(1), failed)
}

func TestMCPServerHealth_RefusesTargetWithoutIdentity(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestLogsService(t)
	reader := telemetry.NewMCPServerHealth(ti.conn, ti.chConn)
	from, to := healthWindow()
	target := telemetry.MCPServerTelemetryTarget{ProjectID: uuid.NewString(), MCPServerID: "", ToolsetSlug: "", URLSlug: ""}

	_, err := reader.Outcomes(ctx, target, from, to)
	require.Error(t, err, "an unscoped read would count every server in the project")

	_, err = reader.Series(ctx, target, from, to, 24*time.Hour)
	require.Error(t, err)
}
