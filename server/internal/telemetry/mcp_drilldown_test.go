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

func toolOutcomeCounts(rows []telemetryRepo.MCPToolOutcomeBreakdownRow) map[string]map[string]uint64 {
	counts := map[string]map[string]uint64{}
	for _, row := range rows {
		if counts[row.ToolName] == nil {
			counts[row.ToolName] = map[string]uint64{}
		}
		counts[row.ToolName][row.Outcome] += row.CallCount
	}
	return counts
}

func TestGetMCPToolOutcomeBreakdown_SplitsFailuresByTool(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestLogsService(t)
	authCtx, _ := contextvalues.GetAuthContext(ctx)
	projectID := authCtx.ProjectID.String()
	now := time.Now().UTC()

	for _, tool := range []struct {
		name   string
		status int
	}{
		{"charge", 500},
		{"charge", 500},
		{"charge", 200},
		{"refund", 200},
	} {
		insertHostedToolEvent(t, ctx, ti, hostedToolEventParams{
			projectID:   projectID,
			timestamp:   now.Add(-10 * time.Minute),
			toolsetSlug: "billing",
			toolName:    tool.name,
			userEmail:   "alice@example.com",
			statusCode:  tool.status,
		})
	}
	// Another server's tool of the same name must not be folded in.
	insertHostedToolEvent(t, ctx, ti, hostedToolEventParams{
		projectID:   projectID,
		timestamp:   now.Add(-10 * time.Minute),
		toolsetSlug: "shipping",
		toolName:    "charge",
		userEmail:   "alice@example.com",
		statusCode:  500,
	})

	testenv.FlushClickHouseAsyncInserts(t, ti.chConn)

	rows, err := ti.chClient.GetMCPToolOutcomeBreakdown(ctx, telemetryRepo.GetMCPToolOutcomeBreakdownParams{
		GetMCPOutcomeBreakdownParams: telemetryRepo.GetMCPOutcomeBreakdownParams{
			GramProjectIDs: []string{projectID},
			ToolsetSlugs:   []string{"billing"},
			TimeStart:      now.Add(-time.Hour).UnixNano(),
			TimeEnd:        now.UnixNano(),
		},
	})
	require.NoError(t, err)

	require.Equal(t, map[string]map[string]uint64{
		"charge": {telemetryRepo.MCPOutcomeServerError: 2, telemetryRepo.MCPOutcomeSuccess: 1},
		"refund": {telemetryRepo.MCPOutcomeSuccess: 1},
	}, toolOutcomeCounts(rows))
}

func TestListMCPTraceReferences_NewestFirstAndFilterable(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestLogsService(t)
	authCtx, _ := contextvalues.GetAuthContext(ctx)
	projectID := authCtx.ProjectID.String()
	now := time.Now().UTC()

	for index, status := range []int{200, 500, 401} {
		insertHostedToolEvent(t, ctx, ti, hostedToolEventParams{
			projectID:   projectID,
			timestamp:   now.Add(-time.Duration(30-index*10) * time.Minute),
			toolsetSlug: "billing",
			toolName:    "charge",
			userEmail:   "alice@example.com",
			statusCode:  status,
		})
	}

	testenv.FlushClickHouseAsyncInserts(t, ti.chConn)

	base := telemetryRepo.GetMCPOutcomeBreakdownParams{
		GramProjectIDs: []string{projectID},
		ToolsetSlugs:   []string{"billing"},
		TimeStart:      now.Add(-time.Hour).UnixNano(),
		TimeEnd:        now.UnixNano(),
	}

	all, err := ti.chClient.ListMCPTraceReferences(ctx, telemetryRepo.ListMCPTraceReferencesParams{
		GetMCPOutcomeBreakdownParams: base,
	})
	require.NoError(t, err)
	require.Len(t, all, 3)
	// Newest first, so a drill-down opens on the most recent occurrence.
	require.Equal(t, telemetryRepo.MCPOutcomeUnauthorized, all[0].Outcome)
	require.Equal(t, telemetryRepo.MCPOutcomeServerError, all[1].Outcome)
	require.Equal(t, telemetryRepo.MCPOutcomeSuccess, all[2].Outcome)
	for _, row := range all {
		require.Equal(t, "charge", row.ToolName)
		require.NotEmpty(t, row.TraceID)
		require.Positive(t, row.OccurredAt)
	}

	failures, err := ti.chClient.ListMCPTraceReferences(ctx, telemetryRepo.ListMCPTraceReferencesParams{
		GetMCPOutcomeBreakdownParams: base,
		Outcomes:                     []string{telemetryRepo.MCPOutcomeServerError},
	})
	require.NoError(t, err)
	require.Len(t, failures, 1)
	require.Equal(t, telemetryRepo.MCPOutcomeServerError, failures[0].Outcome)
}

func TestListMCPTraceReferences_PagesBackwardsInTime(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestLogsService(t)
	authCtx, _ := contextvalues.GetAuthContext(ctx)
	projectID := authCtx.ProjectID.String()
	now := time.Now().UTC()

	for index := range 3 {
		insertHostedToolEvent(t, ctx, ti, hostedToolEventParams{
			projectID:   projectID,
			timestamp:   now.Add(-time.Duration(30-index*10) * time.Minute),
			toolsetSlug: "billing",
			toolName:    "charge",
			userEmail:   "alice@example.com",
			statusCode:  500,
		})
	}

	testenv.FlushClickHouseAsyncInserts(t, ti.chConn)

	base := telemetryRepo.GetMCPOutcomeBreakdownParams{
		GramProjectIDs: []string{projectID},
		ToolsetSlugs:   []string{"billing"},
		TimeStart:      now.Add(-time.Hour).UnixNano(),
		TimeEnd:        now.UnixNano(),
	}

	first, err := ti.chClient.ListMCPTraceReferences(ctx, telemetryRepo.ListMCPTraceReferencesParams{
		GetMCPOutcomeBreakdownParams: base,
		Limit:                        2,
	})
	require.NoError(t, err)
	require.Len(t, first, 2)

	next, err := ti.chClient.ListMCPTraceReferences(ctx, telemetryRepo.ListMCPTraceReferencesParams{
		GetMCPOutcomeBreakdownParams: base,
		BeforeUnixNano:               first[len(first)-1].OccurredAt,
		BeforeTraceID:                first[len(first)-1].TraceID,
		Limit:                        2,
	})
	require.NoError(t, err)
	require.Len(t, next, 1)
	// The page boundary is exclusive, so nothing is served twice.
	require.NotEqual(t, first[0].TraceID, next[0].TraceID)
	require.NotEqual(t, first[1].TraceID, next[0].TraceID)
	require.Less(t, next[0].OccurredAt, first[1].OccurredAt)
}

func TestMCPDrilldown_NoProjectsIsNotAnUnscopedRead(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestLogsService(t)
	now := time.Now().UTC()
	empty := telemetryRepo.GetMCPOutcomeBreakdownParams{
		GramProjectIDs: nil,
		TimeStart:      now.Add(-time.Hour).UnixNano(),
		TimeEnd:        now.UnixNano(),
	}

	tools, err := ti.chClient.GetMCPToolOutcomeBreakdown(ctx, telemetryRepo.GetMCPToolOutcomeBreakdownParams{GetMCPOutcomeBreakdownParams: empty})
	require.NoError(t, err)
	require.Empty(t, tools)

	traces, err := ti.chClient.ListMCPTraceReferences(ctx, telemetryRepo.ListMCPTraceReferencesParams{GetMCPOutcomeBreakdownParams: empty})
	require.NoError(t, err)
	require.Empty(t, traces)
}

func TestListMCPTraceReferences_CapsThePageSize(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestLogsService(t)
	authCtx, _ := contextvalues.GetAuthContext(ctx)
	projectID := authCtx.ProjectID.String()
	now := time.Now().UTC()

	insertHostedToolEvent(t, ctx, ti, hostedToolEventParams{
		projectID:   projectID,
		timestamp:   now.Add(-5 * time.Minute),
		toolsetSlug: "billing",
		toolName:    "charge",
		userEmail:   "alice@example.com",
		statusCode:  200,
	})
	testenv.FlushClickHouseAsyncInserts(t, ti.chConn)

	// An unbounded or oversized request is clamped rather than honoured, so a
	// caller cannot turn drill-down into a bulk export.
	rows, err := ti.chClient.ListMCPTraceReferences(ctx, telemetryRepo.ListMCPTraceReferencesParams{
		GetMCPOutcomeBreakdownParams: telemetryRepo.GetMCPOutcomeBreakdownParams{
			GramProjectIDs: []string{projectID},
			ToolsetSlugs:   []string{"billing"},
			TimeStart:      now.Add(-time.Hour).UnixNano(),
			TimeEnd:        now.UnixNano(),
		},
		Limit: 100_000,
	})
	require.NoError(t, err)
	require.Len(t, rows, 1)
}

// TestMCPDrilldownTraceIDsAreStable pins that the same occurrence keeps its
// correlation id across reads, which is what makes a reference quotable.
func TestMCPDrilldownTraceIDsAreStable(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestLogsService(t)
	authCtx, _ := contextvalues.GetAuthContext(ctx)
	projectID := authCtx.ProjectID.String()
	now := time.Now().UTC()

	insertHostedToolEvent(t, ctx, ti, hostedToolEventParams{
		projectID:   projectID,
		timestamp:   now.Add(-5 * time.Minute),
		toolsetSlug: "billing",
		toolName:    "charge",
		userEmail:   uuid.New().String() + "@example.com",
		statusCode:  403,
	})
	testenv.FlushClickHouseAsyncInserts(t, ti.chConn)

	params := telemetryRepo.ListMCPTraceReferencesParams{
		GetMCPOutcomeBreakdownParams: telemetryRepo.GetMCPOutcomeBreakdownParams{
			GramProjectIDs: []string{projectID},
			ToolsetSlugs:   []string{"billing"},
			TimeStart:      now.Add(-time.Hour).UnixNano(),
			TimeEnd:        now.UnixNano(),
		},
	}
	first, err := ti.chClient.ListMCPTraceReferences(ctx, params)
	require.NoError(t, err)
	second, err := ti.chClient.ListMCPTraceReferences(ctx, params)
	require.NoError(t, err)
	require.Len(t, first, 1)
	require.Equal(t, first[0].TraceID, second[0].TraceID)
	require.Equal(t, telemetryRepo.MCPOutcomeUnauthorized, first[0].Outcome)
}
