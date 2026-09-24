package telemetry_test

import (
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	telemetryRepo "github.com/speakeasy-api/gram/server/internal/telemetry/repo"
	"github.com/speakeasy-api/gram/server/internal/testenv"
)

// outcomeCounts folds a breakdown into outcome -> count so assertions do not
// depend on the row order the query happens to return.
func outcomeCounts(rows []telemetryRepo.MCPOutcomeBreakdownRow) map[string]uint64 {
	counts := make(map[string]uint64, len(rows))
	for _, row := range rows {
		counts[row.Outcome] += row.CallCount
	}
	return counts
}

func clientCounts(rows []telemetryRepo.MCPOutcomeBreakdownRow) map[string]uint64 {
	counts := make(map[string]uint64, len(rows))
	for _, row := range rows {
		counts[row.Client] += row.CallCount
	}
	return counts
}

func TestGetMCPOutcomeBreakdown_Empty(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestLogsService(t)
	authCtx, _ := contextvalues.GetAuthContext(ctx)
	now := time.Now().UTC()

	testenv.FlushClickHouseAsyncInserts(t, ti.chConn)

	rows, err := ti.chClient.GetMCPOutcomeBreakdown(ctx, telemetryRepo.GetMCPOutcomeBreakdownParams{
		GramProjectIDs: []string{authCtx.ProjectID.String()},
		TimeStart:      now.Add(-time.Hour).UnixNano(),
		TimeEnd:        now.UnixNano(),
	})
	require.NoError(t, err)
	require.Empty(t, rows)
}

// TestGetMCPOutcomeBreakdown_NoProjectsIsNotAnUnscopedRead pins that an empty
// project list reads nothing rather than every project in the deployment.
func TestGetMCPOutcomeBreakdown_NoProjectsIsNotAnUnscopedRead(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestLogsService(t)
	now := time.Now().UTC()

	testenv.FlushClickHouseAsyncInserts(t, ti.chConn)

	rows, err := ti.chClient.GetMCPOutcomeBreakdown(ctx, telemetryRepo.GetMCPOutcomeBreakdownParams{
		GramProjectIDs: nil,
		TimeStart:      now.Add(-time.Hour).UnixNano(),
		TimeEnd:        now.UnixNano(),
	})
	require.NoError(t, err)
	require.Empty(t, rows)

	watermark, err := ti.chClient.GetTelemetryWatermark(ctx, telemetryRepo.GetTelemetryWatermarkParams{GramProjectIDs: nil})
	require.NoError(t, err)
	require.Zero(t, watermark)
}

func TestGetMCPOutcomeBreakdown_ClassifiesDirectCallsByStatus(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestLogsService(t)
	authCtx, _ := contextvalues.GetAuthContext(ctx)
	projectID := authCtx.ProjectID.String()
	now := time.Now().UTC()

	for _, status := range []int{200, 401, 403, 404, 500, 503} {
		insertHostedToolEvent(t, ctx, ti, hostedToolEventParams{
			projectID:   projectID,
			timestamp:   now.Add(-10 * time.Minute),
			toolsetSlug: "billing",
			toolName:    "charge",
			userEmail:   "alice@example.com",
			statusCode:  status,
		})
	}
	// A second server's traffic must not be counted against the first.
	insertHostedToolEvent(t, ctx, ti, hostedToolEventParams{
		projectID:   projectID,
		timestamp:   now.Add(-10 * time.Minute),
		toolsetSlug: "shipping",
		toolName:    "quote",
		userEmail:   "alice@example.com",
		statusCode:  500,
	})

	testenv.FlushClickHouseAsyncInserts(t, ti.chConn)

	rows, err := ti.chClient.GetMCPOutcomeBreakdown(ctx, telemetryRepo.GetMCPOutcomeBreakdownParams{
		GramProjectIDs: []string{projectID},
		ToolsetSlugs:   []string{"billing"},
		TimeStart:      now.Add(-time.Hour).UnixNano(),
		TimeEnd:        now.UnixNano(),
	})
	require.NoError(t, err)

	require.Equal(t, map[string]uint64{
		telemetryRepo.MCPOutcomeSuccess:      1,
		telemetryRepo.MCPOutcomeUnauthorized: 2,
		telemetryRepo.MCPOutcomeClientError:  1,
		telemetryRepo.MCPOutcomeServerError:  2,
	}, outcomeCounts(rows))

	// Nothing records which client made a direct call yet, so every one of them
	// is attributed to the explicit "we do not know" label rather than to a
	// client name inferred from something else.
	require.Equal(t, map[string]uint64{telemetryRepo.MCPClientUnattributed: 6}, clientCounts(rows))

	// Unfiltered, the same read covers the whole organization scope.
	all, err := ti.chClient.GetMCPOutcomeBreakdown(ctx, telemetryRepo.GetMCPOutcomeBreakdownParams{
		GramProjectIDs: []string{projectID},
		TimeStart:      now.Add(-time.Hour).UnixNano(),
		TimeEnd:        now.UnixNano(),
	})
	require.NoError(t, err)
	require.Equal(t, uint64(3), outcomeCounts(all)[telemetryRepo.MCPOutcomeServerError])
}

func TestGetMCPOutcomeBreakdown_AttributesHookObservedCallsToTheirClient(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestLogsService(t)
	authCtx, _ := contextvalues.GetAuthContext(ctx)
	projectID := authCtx.ProjectID.String()
	deploymentID := uuid.New().String()
	now := time.Now().UTC()

	insertHookEvent(t, ctx, hookEventParams{
		projectID:    projectID,
		deploymentID: deploymentID,
		timestamp:    now.Add(-5 * time.Minute),
		traceID:      uuid.New().String(),
		hookSource:   "claude-code",
		toolSource:   "billing",
		toolName:     "charge",
		result:       `"ok"`,
		mcpServerURL: "https://api.getgram.ai/mcp/billing",
	})
	insertHookEvent(t, ctx, hookEventParams{
		projectID:    projectID,
		deploymentID: deploymentID,
		timestamp:    now.Add(-4 * time.Minute),
		traceID:      uuid.New().String(),
		hookSource:   "claude-code",
		toolSource:   "billing",
		toolName:     "charge",
		errorMsg:     "boom",
		mcpServerURL: "https://api.getgram.ai/mcp/billing",
	})
	// A different server behind the same client must not be folded in.
	insertHookEvent(t, ctx, hookEventParams{
		projectID:    projectID,
		deploymentID: deploymentID,
		timestamp:    now.Add(-3 * time.Minute),
		traceID:      uuid.New().String(),
		hookSource:   "claude-code",
		toolSource:   "shipping",
		toolName:     "quote",
		errorMsg:     "boom",
		mcpServerURL: "https://api.getgram.ai/mcp/shipping",
	})

	testenv.FlushClickHouseAsyncInserts(t, ti.chConn)

	rows, err := ti.chClient.GetMCPOutcomeBreakdown(ctx, telemetryRepo.GetMCPOutcomeBreakdownParams{
		GramProjectIDs:       []string{projectID},
		MCPServerURLSuffixes: []string{"/mcp/billing"},
		TimeStart:            now.Add(-time.Hour).UnixNano(),
		TimeEnd:              now.UnixNano(),
	})
	require.NoError(t, err)

	require.Equal(t, map[string]uint64{
		telemetryRepo.MCPOutcomeSuccess: 1,
		telemetryRepo.MCPOutcomeFailed:  1,
	}, outcomeCounts(rows))
	require.Equal(t, map[string]uint64{"claude-code": 2}, clientCounts(rows))
}

func TestGetMCPOutcomeBreakdown_ExcludesCallsOutsideTheWindow(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestLogsService(t)
	authCtx, _ := contextvalues.GetAuthContext(ctx)
	projectID := authCtx.ProjectID.String()
	now := time.Now().UTC()

	insertHostedToolEvent(t, ctx, ti, hostedToolEventParams{
		projectID:   projectID,
		timestamp:   now.Add(-10 * time.Minute),
		toolsetSlug: "billing",
		toolName:    "charge",
		userEmail:   "alice@example.com",
		statusCode:  200,
	})
	insertHostedToolEvent(t, ctx, ti, hostedToolEventParams{
		projectID:   projectID,
		timestamp:   now.Add(-30 * 24 * time.Hour),
		toolsetSlug: "billing",
		toolName:    "charge",
		userEmail:   "alice@example.com",
		statusCode:  500,
	})

	testenv.FlushClickHouseAsyncInserts(t, ti.chConn)

	rows, err := ti.chClient.GetMCPOutcomeBreakdown(ctx, telemetryRepo.GetMCPOutcomeBreakdownParams{
		GramProjectIDs: []string{projectID},
		ToolsetSlugs:   []string{"billing"},
		TimeStart:      now.Add(-time.Hour).UnixNano(),
		TimeEnd:        now.UnixNano(),
	})
	require.NoError(t, err)
	require.Equal(t, map[string]uint64{telemetryRepo.MCPOutcomeSuccess: 1}, outcomeCounts(rows))
}

func TestGetTelemetryWatermark_ReportsNewestObservation(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestLogsService(t)
	authCtx, _ := contextvalues.GetAuthContext(ctx)
	projectID := authCtx.ProjectID.String()
	now := time.Now().UTC()

	// A project with no telemetry reports zero, which the caller reads as "no
	// observations" rather than as a timestamp.
	watermark, err := ti.chClient.GetTelemetryWatermark(ctx, telemetryRepo.GetTelemetryWatermarkParams{
		GramProjectIDs: []string{uuid.New().String()},
	})
	require.NoError(t, err)
	require.Zero(t, watermark)

	newest := now.Add(-2 * time.Minute)
	insertHostedToolEvent(t, ctx, ti, hostedToolEventParams{
		projectID:   projectID,
		timestamp:   now.Add(-time.Hour),
		toolsetSlug: "billing",
		toolName:    "charge",
		userEmail:   "alice@example.com",
		statusCode:  200,
	})
	insertHostedToolEvent(t, ctx, ti, hostedToolEventParams{
		projectID:   projectID,
		timestamp:   newest,
		toolsetSlug: "billing",
		toolName:    "charge",
		userEmail:   "alice@example.com",
		statusCode:  200,
	})

	testenv.FlushClickHouseAsyncInserts(t, ti.chConn)

	watermark, err = ti.chClient.GetTelemetryWatermark(ctx, telemetryRepo.GetTelemetryWatermarkParams{
		GramProjectIDs: []string{projectID},
	})
	require.NoError(t, err)
	require.Equal(t, newest.UnixNano(), watermark)
}
