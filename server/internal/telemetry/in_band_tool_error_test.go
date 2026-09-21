package telemetry_test

import (
	"testing"
	"time"

	gen "github.com/speakeasy-api/gram/server/gen/telemetry"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/stretchr/testify/require"
)

// An upstream MCP server can report failure in-band, flagging isError on a
// result it returned over a successful HTTP 200. The status code alone reads
// as a success, so these cover the separate signal that keeps such a call out
// of the success bucket everywhere the outcome is derived.

func TestListToolUsageTraces_InBandToolErrorCountsAsFailure(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestLogsService(t)
	authCtx, _ := contextvalues.GetAuthContext(ctx)
	projectID := authCtx.ProjectID.String()
	now := time.Now().UTC()

	insertHostedToolEvent(t, ctx, ti, hostedToolEventParams{
		projectID:   projectID,
		timestamp:   now.Add(-10 * time.Minute),
		toolsetSlug: "payments",
		toolName:    "charge_ok",
		userEmail:   "alice@example.com",
		statusCode:  200,
	})
	insertHostedToolEvent(t, ctx, ti, hostedToolEventParams{
		projectID:   projectID,
		timestamp:   now.Add(-9 * time.Minute),
		toolsetSlug: "payments",
		toolName:    "charge_in_band_error",
		userEmail:   "alice@example.com",
		statusCode:  200,
		toolError:   true,
	})

	from := now.Add(-1 * time.Hour).Format(time.RFC3339)
	to := now.Add(1 * time.Hour).Format(time.RFC3339)

	all := waitForToolUsageTraces(t, ctx, ti, &gen.ListToolUsageTracesPayload{
		From: from, To: to, Limit: 10, Sort: "desc",
	}, func(result *gen.ListToolUsageTracesResult) bool {
		return len(result.Traces) == 2
	})
	byTool := make(map[string]*gen.ToolUsageTraceSummary, len(all.Traces))
	for _, trace := range all.Traces {
		byTool[trace.ToolName] = trace
	}

	errored := byTool["charge_in_band_error"]
	require.NotNil(t, errored)
	require.NotNil(t, errored.ToolError)
	require.True(t, *errored.ToolError)
	// The status code is untouched: the upstream really did answer 200, and
	// the failure is carried separately rather than by rewriting it.
	require.NotNil(t, errored.HTTPStatusCode)
	require.Equal(t, int32(200), *errored.HTTPStatusCode)

	succeeded := byTool["charge_ok"]
	require.NotNil(t, succeeded)
	require.Nil(t, succeeded.ToolError)

	errorFiltered, err := ti.service.ListToolUsageTraces(ctx, &gen.ListToolUsageTracesPayload{
		From: from, To: to, Limit: 10, Sort: "desc",
		Statuses: []gen.ToolUsageStatus{gen.ToolUsageStatus("error")},
	})
	require.NoError(t, err)
	require.Len(t, errorFiltered.Traces, 1)
	require.Equal(t, "charge_in_band_error", errorFiltered.Traces[0].ToolName)

	successFiltered, err := ti.service.ListToolUsageTraces(ctx, &gen.ListToolUsageTracesPayload{
		From: from, To: to, Limit: 10, Sort: "desc",
		Statuses: []gen.ToolUsageStatus{gen.ToolUsageStatus("success")},
	})
	require.NoError(t, err)
	require.Len(t, successFiltered.Traces, 1)
	require.Equal(t, "charge_ok", successFiltered.Traces[0].ToolName)
}

func TestListToolUsageTraces_InBandToolErrorOnRawSearchPath(t *testing.T) {
	t.Parallel()

	// A search query moves the list off the trace_summaries view and onto a
	// raw telemetry_logs scan, which derives the outcome independently.
	ctx, ti := newTestLogsService(t)
	authCtx, _ := contextvalues.GetAuthContext(ctx)
	projectID := authCtx.ProjectID.String()
	now := time.Now().UTC()

	insertHostedToolEvent(t, ctx, ti, hostedToolEventParams{
		projectID:   projectID,
		timestamp:   now.Add(-8 * time.Minute),
		toolsetSlug: "payments",
		toolName:    "charge_in_band_error",
		userEmail:   "alice@example.com",
		statusCode:  200,
		toolError:   true,
	})

	from := now.Add(-1 * time.Hour).Format(time.RFC3339)
	to := now.Add(1 * time.Hour).Format(time.RFC3339)
	query := "charge_in_band_error"

	found := waitForToolUsageTraces(t, ctx, ti, &gen.ListToolUsageTracesPayload{
		From: from, To: to, Limit: 10, Sort: "desc", Query: &query,
	}, func(result *gen.ListToolUsageTracesResult) bool {
		return len(result.Traces) == 1
	})
	require.NotNil(t, found.Traces[0].ToolError)
	require.True(t, *found.Traces[0].ToolError)

	errorFiltered, err := ti.service.ListToolUsageTraces(ctx, &gen.ListToolUsageTracesPayload{
		From: from, To: to, Limit: 10, Sort: "desc", Query: &query,
		Statuses: []gen.ToolUsageStatus{gen.ToolUsageStatus("error")},
	})
	require.NoError(t, err)
	require.Len(t, errorFiltered.Traces, 1)

	successFiltered, err := ti.service.ListToolUsageTraces(ctx, &gen.ListToolUsageTracesPayload{
		From: from, To: to, Limit: 10, Sort: "desc", Query: &query,
		Statuses: []gen.ToolUsageStatus{gen.ToolUsageStatus("success")},
	})
	require.NoError(t, err)
	require.Empty(t, successFiltered.Traces)
}

func TestGetToolUsageTotals_InBandToolErrorCountsAsFailure(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestLogsService(t)
	authCtx, _ := contextvalues.GetAuthContext(ctx)
	projectID := authCtx.ProjectID.String()
	now := time.Now().UTC()

	insertHostedToolEvent(t, ctx, ti, hostedToolEventParams{
		projectID:   projectID,
		timestamp:   now.Add(-10 * time.Minute),
		toolsetSlug: "payments",
		toolName:    "charge_ok",
		userEmail:   "alice@example.com",
		statusCode:  200,
	})
	insertHostedToolEvent(t, ctx, ti, hostedToolEventParams{
		projectID:   projectID,
		timestamp:   now.Add(-9 * time.Minute),
		toolsetSlug: "payments",
		toolName:    "charge_in_band_error",
		userEmail:   "alice@example.com",
		statusCode:  200,
		toolError:   true,
	})

	from := now.Add(-1 * time.Hour).Format(time.RFC3339)
	to := now.Add(1 * time.Hour).Format(time.RFC3339)

	var totals *gen.GetToolUsageTotalsResult
	require.Eventually(t, func() bool {
		var err error
		totals, err = ti.service.GetToolUsageTotals(ctx, &gen.GetToolUsageTotalsPayload{From: from, To: to})
		return err == nil && totals != nil && totals.Totals.EventCount == 2
	}, 2*time.Second, 50*time.Millisecond, "expected both tool events to be query-ready")

	require.Equal(t, int64(1), totals.Totals.FailureCount)
	require.Equal(t, int64(1), totals.Totals.SuccessCount)
}
