package platformmcp

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	telemetryrepo "github.com/speakeasy-api/gram/server/internal/telemetry/repo"
)

// TestQueryMCPTracesOutput_ProjectsOnlyAllowlistedFields pins the drill-down's
// serialized shape. This is the tool closest to being a log reader, so the
// allowlist is what keeps it from becoming one: a body, an argument, a header,
// a URL, or an identity can only appear here by being added to this list first.
func TestQueryMCPTracesOutput_ProjectsOnlyAllowlistedFields(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 8, 21, 12, 0, 0, 0, time.UTC)
	window, err := resolveWindow("24h", now)
	require.NoError(t, err)

	output := QueryMCPTracesOutput{
		ProjectID: "00000000-0000-0000-0000-000000000001",
		MCPID:     "00000000-0000-0000-0000-000000000002",
		Envelope:  newDataEnvelope(now, now.Add(-time.Minute), window),
		Traces: []MCPTraceReference{{
			Reference:  "opaque",
			OccurredAt: now.Format(time.RFC3339),
			ToolName:   "charge",
			Outcome:    telemetryrepo.MCPOutcomeUnauthorized,
			Client:     "claude-code",
		}},
		NextCursor: "opaque-cursor",
	}

	require.ElementsMatch(t, []string{
		"project_id", "mcp_id",
		"data", "queried_at", "data_through", "freshness", "resolved_window", "window", "from", "to",
		"traces", "reference", "occurred_at", "tool_name", "outcome", "client",
		"next_cursor",
	}, decodeKeys(t, output))
}

func TestGetUserMCPStatusOutput_ProjectsOnlyAllowlistedFields(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 8, 21, 12, 0, 0, 0, time.UTC)
	window, err := resolveWindow("7d", now)
	require.NoError(t, err)

	output := GetUserMCPStatusOutput{
		ProjectID:      "00000000-0000-0000-0000-000000000001",
		MCPID:          "00000000-0000-0000-0000-000000000002",
		Envelope:       newDataEnvelope(now, now.Add(-time.Minute), window),
		ActiveUsers:    NewSubjectCount(9),
		RowsSuppressed: false,
		Users:          []MCPUserStatus{{SubjectReference: "opaque", ToolCalls: 12}},
	}

	// Note what is absent: no email, no user id, no name, no account id. A
	// subject is only ever a reference.
	require.ElementsMatch(t, []string{
		"project_id", "mcp_id",
		"data", "queried_at", "data_through", "freshness", "resolved_window", "window", "from", "to",
		"active_users", "rows_suppressed",
		"users", "subject_reference", "tool_calls",
	}, decodeKeys(t, output))
}

func TestQueryMCPEventsOutput_ProjectsOnlyAllowlistedFields(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 8, 21, 12, 0, 0, 0, time.UTC)
	window, err := resolveWindow("1h", now)
	require.NoError(t, err)

	output := QueryMCPEventsOutput{
		ProjectID: "00000000-0000-0000-0000-000000000001",
		MCPID:     "00000000-0000-0000-0000-000000000002",
		Envelope:  newDataEnvelope(now, now.Add(-time.Minute), window),
		Tools:     []MCPToolEvents{{ToolName: "charge", Outcomes: MCPOutcomeSummary{Total: 4, Success: 1, ServerError: 3}}},
		Truncated: false,
	}

	require.ElementsMatch(t, []string{
		"project_id", "mcp_id",
		"data", "queried_at", "data_through", "freshness", "resolved_window", "window", "from", "to",
		"tools", "tool_name",
		"outcomes", "total", "success", "unauthorized", "client_error", "server_error", "failed", "unknown",
		"truncated",
	}, decodeKeys(t, output))
}

func TestQueryMCPMetricsOutput_ProjectsOnlyAllowlistedFields(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 8, 21, 12, 0, 0, 0, time.UTC)
	window, err := resolveWindow("30d", now)
	require.NoError(t, err)

	output := QueryMCPMetricsOutput{
		ProjectID:       "00000000-0000-0000-0000-000000000001",
		MCPID:           "00000000-0000-0000-0000-000000000002",
		Envelope:        newDataEnvelope(now, now.Add(-time.Minute), window),
		ToolCalls:       100,
		FailedToolCalls: 5,
		FailureRate:     0.05,
		AvgLatencyMs:    42.5,
		ActiveUsers:     NewSubjectCount(7),
	}

	require.ElementsMatch(t, []string{
		"project_id", "mcp_id",
		"data", "queried_at", "data_through", "freshness", "resolved_window", "window", "from", "to",
		"tool_calls", "failed_tool_calls", "failure_rate", "avg_latency_ms", "active_users",
	}, decodeKeys(t, output))
}

func TestToolEvents_OrdersBrokenToolsFirstAndReportsTruncation(t *testing.T) {
	t.Parallel()

	events, truncated := toolEvents([]telemetryrepo.MCPToolOutcomeBreakdownRow{
		{ToolName: "list", Outcome: telemetryrepo.MCPOutcomeSuccess, CallCount: 500},
		{ToolName: "charge", Outcome: telemetryrepo.MCPOutcomeSuccess, CallCount: 1},
		{ToolName: "charge", Outcome: telemetryrepo.MCPOutcomeServerError, CallCount: 9},
	})
	require.False(t, truncated)
	require.Equal(t, []MCPToolEvents{
		{ToolName: "charge", Outcomes: MCPOutcomeSummary{Total: 10, Success: 1, ServerError: 9}},
		{ToolName: "list", Outcomes: MCPOutcomeSummary{Total: 500, Success: 500}},
	}, events)

	var many []telemetryrepo.MCPToolOutcomeBreakdownRow
	for index := range maxDrilldownTools + 5 {
		many = append(many, telemetryrepo.MCPToolOutcomeBreakdownRow{
			ToolName:  string(rune('a' + index)),
			Outcome:   telemetryrepo.MCPOutcomeServerError,
			CallCount: uint64(index + 1),
		})
	}
	capped, truncated := toolEvents(many)
	require.True(t, truncated)
	require.Len(t, capped, maxDrilldownTools)
}

func TestValidOutcomeClass_IsAClosedSet(t *testing.T) {
	t.Parallel()

	for _, outcome := range []string{
		telemetryrepo.MCPOutcomeSuccess,
		telemetryrepo.MCPOutcomeUnauthorized,
		telemetryrepo.MCPOutcomeClientError,
		telemetryrepo.MCPOutcomeServerError,
		telemetryrepo.MCPOutcomeFailed,
		telemetryrepo.MCPOutcomeUnknown,
	} {
		require.True(t, validOutcomeClass(outcome), outcome)
	}
	// Anything that looks like a filter expression is not an outcome class.
	// Accepting one would be the first step toward a query grammar.
	for _, outcome := range []string{"", "SUCCESS", "error", "outcome = 'success'", "success OR 1=1"} {
		require.False(t, validOutcomeClass(outcome), outcome)
	}
}

func TestFailureRate_RoundsServerSide(t *testing.T) {
	t.Parallel()

	require.InDelta(t, 0.0, failureRate(0, 0), 1e-9)
	require.InDelta(t, 0.0, failureRate(0, 5), 1e-9)
	require.InDelta(t, 0.5, failureRate(10, 5), 1e-9)
	require.InDelta(t, 0.3333, failureRate(3, 1), 1e-9)
	require.InDelta(t, 1.0, failureRate(7, 7), 1e-9)
}

func TestCursorPosition_RoundTripsAndRejectsJunk(t *testing.T) {
	t.Parallel()

	position, err := parseCursorPosition(formatCursorPosition(1_700_000_000_000_000_000))
	require.NoError(t, err)
	require.Equal(t, int64(1_700_000_000_000_000_000), position)

	for _, value := range []string{"", "1700000000", "t:", "t:abc", "t:-1", "t:0"} {
		_, err := parseCursorPosition(value)
		require.ErrorIs(t, err, ErrSubjectReferenceInvalid, value)
	}
}
