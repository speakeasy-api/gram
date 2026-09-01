package telemetry_test

import (
	"strings"
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
		BeforeToolCallID:             first[len(first)-1].ToolCallID,
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
	require.Equal(t, first[0].ToolCallID, second[0].ToolCallID)
	require.Equal(t, telemetryRepo.MCPOutcomeUnauthorized, first[0].Outcome)
}

// TestGetMCPToolOutcomeBreakdown_HookObservedMultiServerSession pins that a
// single session (shared trace_id) calling several MCP servers contributes
// only the selected server's tools when filtered. This is the customer-reported
// drill-down bug: Datadog showed Linear and GitHub tools from the same session.
func TestGetMCPToolOutcomeBreakdown_HookObservedMultiServerSession(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestLogsService(t)
	authCtx, _ := contextvalues.GetAuthContext(ctx)
	projectID := authCtx.ProjectID.String()
	deploymentID := uuid.New().String()
	now := time.Now().UTC()
	sharedTraceID := strings.ReplaceAll(uuid.New().String(), "-", "")

	for _, event := range []struct {
		mcpServerURL string
		toolName     string
		result       string
		errorMsg     string
	}{
		{"https://api.example.com/mcp/datadog", "mcp__datadog__get_logs", `"ok"`, ""},
		{"https://api.example.com/mcp/datadog", "mcp__datadog__search_metrics", `"ok"`, ""},
		{"https://api.example.com/mcp/linear", "mcp__linear__get_issues", `"ok"`, ""},
		{"https://api.example.com/mcp/linear", "mcp__linear__create_issue", "", "boom"},
		{"https://api.example.com/mcp/github", "mcp__github__list_repos", `"ok"`, ""},
	} {
		insertHookEvent(t, ctx, hookEventParams{
			projectID:    projectID,
			deploymentID: deploymentID,
			timestamp:    now.Add(-10 * time.Minute),
			traceID:      sharedTraceID,
			hookSource:   "claude-code",
			toolName:     event.toolName,
			result:       event.result,
			errorMsg:     event.errorMsg,
			mcpServerURL: event.mcpServerURL,
			customAttrs:  map[string]any{"gen_ai.tool.call.id": uuid.New().String()},
		})
	}
	testenv.FlushClickHouseAsyncInserts(t, ti.chConn)

	base := func(suffix string) telemetryRepo.GetMCPOutcomeBreakdownParams {
		return telemetryRepo.GetMCPOutcomeBreakdownParams{
			GramProjectIDs:       []string{projectID},
			MCPServerURLSuffixes: []string{suffix},
			TimeStart:            now.Add(-time.Hour).UnixNano(),
			TimeEnd:              now.UnixNano(),
		}
	}

	rows, err := ti.chClient.GetMCPToolOutcomeBreakdown(ctx, telemetryRepo.GetMCPToolOutcomeBreakdownParams{
		GetMCPOutcomeBreakdownParams: base("/mcp/datadog"),
	})
	require.NoError(t, err)
	counts := toolOutcomeCounts(rows)
	require.Equal(t, map[string]map[string]uint64{
		"mcp__datadog__get_logs":       {telemetryRepo.MCPOutcomeSuccess: 1},
		"mcp__datadog__search_metrics": {telemetryRepo.MCPOutcomeSuccess: 1},
	}, counts)

	outcomes, err := ti.chClient.GetMCPOutcomeBreakdown(ctx, base("/mcp/datadog"))
	require.NoError(t, err)
	require.Equal(t, map[string]uint64{telemetryRepo.MCPOutcomeSuccess: 2}, outcomeCounts(outcomes))

	linearRows, err := ti.chClient.GetMCPToolOutcomeBreakdown(ctx, telemetryRepo.GetMCPToolOutcomeBreakdownParams{
		GetMCPOutcomeBreakdownParams: base("/mcp/linear"),
	})
	require.NoError(t, err)
	require.Equal(t, map[string]map[string]uint64{
		"mcp__linear__get_issues":   {telemetryRepo.MCPOutcomeSuccess: 1},
		"mcp__linear__create_issue": {telemetryRepo.MCPOutcomeFailed: 1},
	}, toolOutcomeCounts(linearRows))
}

// TestListMCPTraceReferences_PagesSameSessionCalls pins that several tool
// calls sharing a session trace_id each appear as their own occurrence and
// page on (time, trace, call) rather than collapsing to one row per session.
func TestListMCPTraceReferences_PagesSameSessionCalls(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestLogsService(t)
	authCtx, _ := contextvalues.GetAuthContext(ctx)
	projectID := authCtx.ProjectID.String()
	deploymentID := uuid.New().String()
	now := time.Now().UTC()
	sharedTraceID := strings.ReplaceAll(uuid.New().String(), "-", "")

	for index, toolName := range []string{"get_logs", "search_metrics", "list_monitors"} {
		insertHookEvent(t, ctx, hookEventParams{
			projectID:    projectID,
			deploymentID: deploymentID,
			timestamp:    now.Add(-time.Duration(30-index*10) * time.Minute),
			traceID:      sharedTraceID,
			hookSource:   "claude-code",
			toolName:     toolName,
			result:       `"ok"`,
			mcpServerURL: "https://api.example.com/mcp/datadog",
			customAttrs:  map[string]any{"gen_ai.tool.call.id": "call-" + toolName},
		})
	}
	testenv.FlushClickHouseAsyncInserts(t, ti.chConn)

	base := telemetryRepo.GetMCPOutcomeBreakdownParams{
		GramProjectIDs:       []string{projectID},
		MCPServerURLSuffixes: []string{"/mcp/datadog"},
		TimeStart:            now.Add(-time.Hour).UnixNano(),
		TimeEnd:              now.UnixNano(),
	}

	first, err := ti.chClient.ListMCPTraceReferences(ctx, telemetryRepo.ListMCPTraceReferencesParams{
		GetMCPOutcomeBreakdownParams: base,
		Limit:                        2,
	})
	require.NoError(t, err)
	require.Len(t, first, 2)
	require.Equal(t, sharedTraceID, first[0].TraceID)
	require.Equal(t, sharedTraceID, first[1].TraceID)
	require.NotEqual(t, first[0].ToolCallID, first[1].ToolCallID)

	next, err := ti.chClient.ListMCPTraceReferences(ctx, telemetryRepo.ListMCPTraceReferencesParams{
		GetMCPOutcomeBreakdownParams: base,
		BeforeUnixNano:               first[len(first)-1].OccurredAt,
		BeforeTraceID:                first[len(first)-1].TraceID,
		BeforeToolCallID:             first[len(first)-1].ToolCallID,
		Limit:                        2,
	})
	require.NoError(t, err)
	require.Len(t, next, 1)
	require.Equal(t, sharedTraceID, next[0].TraceID)
	require.NotEqual(t, first[0].ToolCallID, next[0].ToolCallID)
	require.NotEqual(t, first[1].ToolCallID, next[0].ToolCallID)
	require.Less(t, next[0].OccurredAt, first[1].OccurredAt)
}
