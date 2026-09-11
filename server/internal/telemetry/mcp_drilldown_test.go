package telemetry_test

import (
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
		BeforeEventID:                first[len(first)-1].EventID,
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

// TestGetMCPToolOutcomeBreakdown_HookObservedSharedSessionStaysServerScoped
// pins that a session containing several MCP calls is filtered at the call,
// before any dimensions or outcomes are aggregated.
func TestGetMCPToolOutcomeBreakdown_HookObservedSharedSessionStaysServerScoped(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestLogsService(t)
	authCtx, _ := contextvalues.GetAuthContext(ctx)
	projectID := authCtx.ProjectID.String()
	now := time.Now().UTC()
	sharedTraceID := strings.ReplaceAll(uuid.NewString(), "-", "")

	insert := func(serverURL, toolName, toolCallID string, hasError bool) {
		attrs := map[string]any{
			"gram.event.source":       "hook",
			"gram.hook.source":        "claude-code",
			"gram.mcp.server_url":     serverURL,
			"gram.tool.name":          toolName,
			"gen_ai.tool.call.id":     toolCallID,
			"gen_ai.tool.call.result": `"ok"`,
			"user.email":              "user@example.com",
		}
		if hasError {
			delete(attrs, "gen_ai.tool.call.result")
			attrs["gram.hook.error"] = "redacted failure"
		}
		encoded, err := json.Marshal(attrs)
		require.NoError(t, err)
		spanID := uuid.NewString()[:16]
		require.NoError(t, ti.chClient.InsertTelemetryLog(ctx, telemetryRepo.InsertTelemetryLogParams{
			ID:                   uuid.NewString(),
			TimeUnixNano:         now.Add(-time.Minute).UnixNano(),
			ObservedTimeUnixNano: now.Add(-time.Minute).UnixNano(),
			SeverityText:         nil,
			Body:                 "hook tool event",
			TraceID:              &sharedTraceID,
			SpanID:               &spanID,
			Attributes:           string(encoded),
			ResourceAttributes:   "{}",
			GramProjectID:        projectID,
			GramDeploymentID:     nil,
			GramFunctionID:       nil,
			GramURN:              "hooks:" + toolName,
			ServiceName:          "gram-hooks",
			ServiceVersion:       nil,
			GramChatID:           nil,
		}))
	}
	insert("https://gram.example/mcp/billing", "charge", "call-billing-1", false)
	insert("https://gram.example/mcp/billing", "refund", "call-billing-2", true)
	insert("https://gram.example/mcp/shipping", "dispatch", "call-shipping-1", false)
	testenv.FlushClickHouseAsyncInserts(t, ti.chConn)

	rows, err := ti.chClient.GetMCPToolOutcomeBreakdown(ctx, telemetryRepo.GetMCPToolOutcomeBreakdownParams{
		GetMCPOutcomeBreakdownParams: telemetryRepo.GetMCPOutcomeBreakdownParams{
			GramProjectIDs:       []string{projectID},
			MCPServerURLSuffixes: []string{"/mcp/billing"},
			TimeStart:            now.Add(-time.Hour).UnixNano(),
			TimeEnd:              now.UnixNano(),
		},
	})
	require.NoError(t, err)
	require.Equal(t, map[string]map[string]uint64{
		"charge": {telemetryRepo.MCPOutcomeSuccess: 1},
		"refund": {telemetryRepo.MCPOutcomeFailed: 1},
	}, toolOutcomeCounts(rows))

	hostedOnly, err := ti.chClient.GetMCPToolOutcomeBreakdown(ctx, telemetryRepo.GetMCPToolOutcomeBreakdownParams{
		GetMCPOutcomeBreakdownParams: telemetryRepo.GetMCPOutcomeBreakdownParams{
			GramProjectIDs: []string{projectID},
			ToolsetSlugs:   []string{"billing"},
			TimeStart:      now.Add(-time.Hour).UnixNano(),
			TimeEnd:        now.UnixNano(),
		},
	})
	require.NoError(t, err)
	require.Empty(t, hostedOnly, "a hosted-only selector must exclude the unfilterable hook lane")

	directOnly, err := ti.chClient.ListMCPTraceReferences(ctx, telemetryRepo.ListMCPTraceReferencesParams{
		GetMCPOutcomeBreakdownParams: telemetryRepo.GetMCPOutcomeBreakdownParams{
			GramProjectIDs:       []string{projectID},
			MCPServerURLSuffixes: []string{"/mcp/billing"},
			TimeStart:            now.Add(-time.Hour).UnixNano(),
			TimeEnd:              now.UnixNano(),
		},
	})
	require.NoError(t, err)
	require.Len(t, directOnly, 2, "a URL-only selector must exclude the unfilterable direct lane")

	users, err := ti.chClient.ListMCPUsageUsers(ctx, telemetryRepo.GetMCPOutcomeBreakdownParams{
		GramProjectIDs:       []string{projectID},
		MCPServerURLSuffixes: []string{"/mcp/billing"},
		TimeStart:            now.Add(-time.Hour).UnixNano(),
		TimeEnd:              now.UnixNano(),
	})
	require.NoError(t, err)
	require.Equal(t, []telemetryRepo.MCPUsageUserRow{{
		IdentityKind: "email",
		Identifier:   "user@example.com",
		HasSuccess:   true,
		HasError:     true,
		HasBlocked:   false,
		LastUsedAt:   now.Add(-time.Minute).UnixNano(),
	}}, users)
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
