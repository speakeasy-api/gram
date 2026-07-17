package telemetry_test

import (
	"errors"
	"testing"
	"time"

	gen "github.com/speakeasy-api/gram/server/gen/telemetry"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/stretchr/testify/assert"
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

	require.EventuallyWithT(t, func(c *assert.CollectT) {
		result, err := ti.service.GetMcpServerActivity(ctx, &gen.GetMcpServerActivityPayload{
			RecentWindowDays: 14,
		})
		if !assert.NoError(c, err, "cause: %v", errors.Unwrap(err)) {
			return
		}
		if !assert.NotNil(c, result) {
			return
		}

		byTarget := mcpActivityByTarget(result.Activity)

		active := byTarget["active-svc"]
		if !assert.NotNil(c, active) {
			return
		}
		assert.Equal(c, gen.ToolUsageTargetType("hosted_mcp_server"), active.TargetType)
		assert.Positive(c, active.TotalToolCalls)
		assert.Positive(c, active.RecentToolCalls)
		assert.NotNil(c, active.LastToolCallAt)

		stale := byTarget["stale-svc"]
		if !assert.NotNil(c, stale) {
			return
		}
		assert.Positive(c, stale.TotalToolCalls)
		assert.Equal(c, int64(0), stale.RecentToolCalls)
		assert.NotNil(c, stale.LastToolCallAt)

		// A server that never received a tool call is simply absent from the list.
		assert.Nil(c, byTarget["never-svc"])
	}, 10*time.Second, 200*time.Millisecond)
}
