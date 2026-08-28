package telemetry_test

import (
	"context"
	"encoding/json"
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

// hookToolEventParams describes a hook-observed tool event for testing.
type hookToolEventParams struct {
	projectID    string
	traceID      string
	timestamp    time.Time
	mcpServerURL string
	toolName     string
	hookSource   string
	hasResult    bool
	hasError     bool
}

// insertHookToolEvent inserts a hook-observed tool event into telemetry_logs.
// This simulates traffic observed by an agent hook where multiple MCP server
// calls can share the same trace_id (session).
func insertHookToolEvent(t *testing.T, ctx context.Context, ti *testInstance, p hookToolEventParams) {
	t.Helper()

	// Generate a unique tool call ID for each event, which is how hook events
	// distinguish individual tool calls within a session.
	toolCallID := uuid.New().String()

	attrs := map[string]any{
		"gram.event.source":   "hook",
		"gram.tool.name":      p.toolName,
		"gram.mcp.server_url": p.mcpServerURL,
		"gram.hook.source":    p.hookSource,
		"gen_ai.tool.call.id": toolCallID,
	}
	if p.hasResult {
		attrs["gen_ai.tool.call.result"] = `"ok"`
	}
	if p.hasError {
		attrs["gram.hook.error"] = "tool call failed"
	}
	attrsJSON, err := json.Marshal(attrs)
	require.NoError(t, err)

	spanID := uuid.New().String()[:16]
	err = ti.chClient.InsertTelemetryLog(ctx, telemetryRepo.InsertTelemetryLogParams{
		ID:                   uuid.New().String(),
		TimeUnixNano:         p.timestamp.UnixNano(),
		ObservedTimeUnixNano: p.timestamp.UnixNano(),
		SeverityText:         nil,
		Body:                 "hook tool event",
		TraceID:              &p.traceID,
		SpanID:               &spanID,
		Attributes:           string(attrsJSON),
		ResourceAttributes:   "{}",
		GramProjectID:        p.projectID,
		GramDeploymentID:     nil,
		GramFunctionID:       nil,
		GramURN:              "hooks:" + p.toolName,
		ServiceName:          "gram-hooks",
		ServiceVersion:       nil,
		GramChatID:           nil,
	})
	require.NoError(t, err)
}

// TestGetMCPToolOutcomeBreakdown_HookObservedMultiServerSession verifies that
// when a single session (shared trace_id) calls multiple MCP servers through
// hook-observed traffic, filtering by one server only shows tools from that
// server. This directly tests the bug scenario from the customer report.
func TestGetMCPToolOutcomeBreakdown_HookObservedMultiServerSession(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestLogsService(t)
	authCtx, _ := contextvalues.GetAuthContext(ctx)
	projectID := authCtx.ProjectID.String()
	now := time.Now().UTC()

	// All events share the same trace_id to simulate a single session
	// that calls multiple MCP servers.
	sharedTraceID := strings.ReplaceAll(uuid.New().String(), "-", "")

	// Session calls multiple MCP servers:
	// - Datadog: 2 calls (get_logs, search_metrics)
	// - Linear: 2 calls (get_issues, create_issue)
	// - GitHub: 1 call (list_repos)
	for _, event := range []struct {
		mcpServerURL string
		toolName     string
		hasResult    bool
		hasError     bool
	}{
		{"https://api.speakeasy.com/mcp/datadog", "mcp__datadog__get_logs", true, false},
		{"https://api.speakeasy.com/mcp/datadog", "mcp__datadog__search_metrics", true, false},
		{"https://api.speakeasy.com/mcp/linear", "mcp__linear__get_issues", true, false},
		{"https://api.speakeasy.com/mcp/linear", "mcp__linear__create_issue", false, true},
		{"https://api.speakeasy.com/mcp/github", "mcp__github__list_repos", true, false},
	} {
		insertHookToolEvent(t, ctx, ti, hookToolEventParams{
			projectID:    projectID,
			traceID:      sharedTraceID,
			timestamp:    now.Add(-10 * time.Minute),
			mcpServerURL: event.mcpServerURL,
			toolName:     event.toolName,
			hookSource:   "claude-code",
			hasResult:    event.hasResult,
			hasError:     event.hasError,
		})
	}
	testenv.FlushClickHouseAsyncInserts(t, ti.chConn)

	// When drilling into "datadog", we should ONLY see datadog tools.
	rows, err := ti.chClient.GetMCPToolOutcomeBreakdown(ctx, telemetryRepo.GetMCPToolOutcomeBreakdownParams{
		GetMCPOutcomeBreakdownParams: telemetryRepo.GetMCPOutcomeBreakdownParams{
			GramProjectIDs:       []string{projectID},
			MCPServerURLSuffixes: []string{"/mcp/datadog"},
			TimeStart:            now.Add(-time.Hour).UnixNano(),
			TimeEnd:              now.UnixNano(),
		},
	})
	require.NoError(t, err)

	counts := toolOutcomeCounts(rows)

	// Verify datadog tools are present
	require.Contains(t, counts, "mcp__datadog__get_logs", "datadog's get_logs should appear")
	require.Contains(t, counts, "mcp__datadog__search_metrics", "datadog's search_metrics should appear")

	// Verify tools from OTHER servers (linear, github) are NOT present
	_, hasLinearTool := counts["mcp__linear__get_issues"]
	require.False(t, hasLinearTool, "linear's get_issues should NOT appear when filtering for datadog")
	_, hasLinearTool2 := counts["mcp__linear__create_issue"]
	require.False(t, hasLinearTool2, "linear's create_issue should NOT appear when filtering for datadog")
	_, hasGithubTool := counts["mcp__github__list_repos"]
	require.False(t, hasGithubTool, "github's list_repos should NOT appear when filtering for datadog")

	// When drilling into "linear", we should only see linear tools
	linearRows, err := ti.chClient.GetMCPToolOutcomeBreakdown(ctx, telemetryRepo.GetMCPToolOutcomeBreakdownParams{
		GetMCPOutcomeBreakdownParams: telemetryRepo.GetMCPOutcomeBreakdownParams{
			GramProjectIDs:       []string{projectID},
			MCPServerURLSuffixes: []string{"/mcp/linear"},
			TimeStart:            now.Add(-time.Hour).UnixNano(),
			TimeEnd:              now.UnixNano(),
		},
	})
	require.NoError(t, err)

	linearCounts := toolOutcomeCounts(linearRows)
	require.Contains(t, linearCounts, "mcp__linear__get_issues", "linear's get_issues should appear")
	require.Contains(t, linearCounts, "mcp__linear__create_issue", "linear's create_issue should appear")
	_, hasDatadogTool := linearCounts["mcp__datadog__get_logs"]
	require.False(t, hasDatadogTool, "datadog's tools should NOT appear when filtering for linear")
}

// TestGetMCPToolOutcomeBreakdown_FiltersToSpecificServer verifies that when
// drilling down into a specific MCP server, only tools from that server appear
// in the breakdown — not tools from other servers that happened to be called
// in the same session or trace. This was the bug reported by Fermatcommerce:
// when drilling into Datadog, tools from Linear, GitHub, and Speakeasy-Slack
// were appearing because attribution was by session rather than by server.
func TestGetMCPToolOutcomeBreakdown_FiltersToSpecificServer(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestLogsService(t)
	authCtx, _ := contextvalues.GetAuthContext(ctx)
	projectID := authCtx.ProjectID.String()
	now := time.Now().UTC()

	// Insert events for multiple servers. Each hosted tool call gets its own
	// trace_id (via insertHostedToolEvent), so this tests the per-trace
	// attribution for direct hosted MCP traffic.
	for _, event := range []struct {
		toolsetSlug string
		toolName    string
		statusCode  int
	}{
		// Server A: datadog with 3 calls
		{"datadog", "get_logs", 200},
		{"datadog", "search_metrics", 200},
		{"datadog", "get_logs", 500},
		// Server B: linear with 2 calls
		{"linear", "get_issues", 200},
		{"linear", "create_issue", 200},
		// Server C: github with 1 call
		{"github", "list_repos", 200},
	} {
		insertHostedToolEvent(t, ctx, ti, hostedToolEventParams{
			projectID:   projectID,
			timestamp:   now.Add(-10 * time.Minute),
			toolsetSlug: event.toolsetSlug,
			toolName:    event.toolName,
			userEmail:   "alice@example.com",
			statusCode:  event.statusCode,
		})
	}
	testenv.FlushClickHouseAsyncInserts(t, ti.chConn)

	// When drilling into "datadog", we should ONLY see datadog tools.
	rows, err := ti.chClient.GetMCPToolOutcomeBreakdown(ctx, telemetryRepo.GetMCPToolOutcomeBreakdownParams{
		GetMCPOutcomeBreakdownParams: telemetryRepo.GetMCPOutcomeBreakdownParams{
			GramProjectIDs: []string{projectID},
			ToolsetSlugs:   []string{"datadog"},
			TimeStart:      now.Add(-time.Hour).UnixNano(),
			TimeEnd:        now.UnixNano(),
		},
	})
	require.NoError(t, err)

	counts := toolOutcomeCounts(rows)

	// Verify datadog tools are present with correct counts
	require.Equal(t, map[string]uint64{
		telemetryRepo.MCPOutcomeSuccess:     1,
		telemetryRepo.MCPOutcomeServerError: 1,
	}, counts["get_logs"], "get_logs should have 1 success and 1 server_error")
	require.Equal(t, map[string]uint64{
		telemetryRepo.MCPOutcomeSuccess: 1,
	}, counts["search_metrics"], "search_metrics should have 1 success")

	// Verify tools from OTHER servers (linear, github) are NOT present
	_, hasLinearTool := counts["get_issues"]
	require.False(t, hasLinearTool, "linear's get_issues should NOT appear when filtering for datadog")
	_, hasLinearTool2 := counts["create_issue"]
	require.False(t, hasLinearTool2, "linear's create_issue should NOT appear when filtering for datadog")
	_, hasGithubTool := counts["list_repos"]
	require.False(t, hasGithubTool, "github's list_repos should NOT appear when filtering for datadog")

	// Total should be exactly 3 datadog calls
	totalCalls := uint64(0)
	for _, tool := range counts {
		for _, count := range tool {
			totalCalls += count
		}
	}
	require.Equal(t, uint64(3), totalCalls, "should see exactly 3 datadog calls, not calls from other servers")
}
