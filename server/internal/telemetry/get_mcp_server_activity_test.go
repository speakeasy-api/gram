package telemetry_test

import (
	"errors"
	"testing"
	"time"

	gen "github.com/speakeasy-api/gram/server/gen/telemetry"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/testenv"
	"github.com/stretchr/testify/require"
)

func mcpActivityByTarget(activity []*gen.McpServerActivity) map[string]*gen.McpServerActivity {
	byTarget := make(map[string]*gen.McpServerActivity, len(activity))
	for _, entry := range activity {
		byTarget[entry.TargetID] = entry
	}
	return byTarget
}

func TestGetMcpServerActivity_Empty(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestLogsService(t)

	result, err := ti.service.GetMcpServerActivity(ctx, &gen.GetMcpServerActivityPayload{})

	require.NoError(t, err, "cause: %v", errors.Unwrap(err))
	require.NotNil(t, result)
	require.Empty(t, result.Activity)
	require.Equal(t, 14, result.RecentWindowDays)
	require.Equal(t, 90, result.LookbackDays)
}

func TestGetMcpServerActivity_FlagsNeverAndStale(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestLogsService(t)

	authCtx, _ := contextvalues.GetAuthContext(ctx)
	projectID := authCtx.ProjectID.String()
	now := time.Now().UTC()

	// A server used inside the recent window is healthy.
	insertHostedToolEvent(t, ctx, ti, hostedToolEventParams{
		projectID:   projectID,
		timestamp:   now.Add(-2 * 24 * time.Hour),
		toolsetSlug: "active-svc",
		toolName:    "charge",
		userEmail:   "alice@example.com",
		statusCode:  200,
	})
	// A server used only outside the recent window (but inside the lookback) is stale.
	insertHostedToolEvent(t, ctx, ti, hostedToolEventParams{
		projectID:   projectID,
		timestamp:   now.Add(-30 * 24 * time.Hour),
		toolsetSlug: "stale-svc",
		toolName:    "refund",
		userEmail:   "bob@example.com",
		statusCode:  200,
	})

	// Flush the server-side async insert queue so the rows (and the
	// trace_summaries MV rows they trigger) are visible, then assert directly
	// rather than polling. This is deterministic and matches the telemetry
	// README guidance for ClickHouse-backed tests.
	testenv.FlushClickHouseAsyncInserts(t, ti.chConn)

	result, err := ti.service.GetMcpServerActivity(ctx, &gen.GetMcpServerActivityPayload{
		RecentWindowDays: 14,
	})
	require.NoError(t, err, "cause: %v", errors.Unwrap(err))
	require.NotNil(t, result)

	byTarget := mcpActivityByTarget(result.Activity)

	active := byTarget["active-svc"]
	require.NotNil(t, active)
	require.Equal(t, gen.McpServerActivityTargetType("hosted_mcp_server"), active.TargetType)
	require.Positive(t, active.TotalToolCalls)
	require.Positive(t, active.RecentToolCalls)
	require.NotNil(t, active.LastToolCallAt)

	stale := byTarget["stale-svc"]
	require.NotNil(t, stale)
	require.Positive(t, stale.TotalToolCalls)
	require.Equal(t, int64(0), stale.RecentToolCalls)
	require.NotNil(t, stale.LastToolCallAt)

	// A server that never received a tool call is simply absent from the list.
	require.Nil(t, byTarget["never-svc"])
}
