package platformmcp

import (
	"encoding/json"
	"strconv"
	"strings"
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
	window, err := resolveWindow("24h", now, drilldownWindowSpec)
	require.NoError(t, err)

	output := QueryMCPTracesOutput{
		ProjectID: "00000000-0000-0000-0000-000000000001",
		MCPID:     "00000000-0000-0000-0000-000000000002",
		Envelope:  newDataEnvelope(now, now.Add(-time.Minute), window, true),
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
		"data", "queried_at", "data_through", "freshness", "no_observations", "resolved_window", "window", "from", "to",
		"traces", "reference", "occurred_at", "tool_name", "outcome", "client",
		"next_cursor",
	}, decodeKeys(t, output))
}

func TestGetUserMCPStatusOutput_ProjectsOnlyAllowlistedFields(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 8, 21, 12, 0, 0, 0, time.UTC)
	window, err := resolveWindow("24h", now, drilldownWindowSpec)
	require.NoError(t, err)

	output := GetUserMCPStatusOutput{
		ProjectID:      "00000000-0000-0000-0000-000000000001",
		MCPID:          "00000000-0000-0000-0000-000000000002",
		Envelope:       newDataEnvelope(now, now.Add(-time.Minute), window, true),
		MaskedIdentity: "a***@e***",
		Activity:       SubjectStateActive,
	}

	// Note what is absent: no email, no user id, no name, no account id, and no
	// activity count that repeated calls could assemble into a profile.
	require.ElementsMatch(t, []string{
		"project_id", "mcp_id",
		"data", "queried_at", "data_through", "freshness", "no_observations", "resolved_window", "window", "from", "to",
		"masked_identity", "activity", "unavailable",
	}, decodeKeys(t, output))
}

// TestParseSubjectIdentity_RefusesUnknownColumns pins that a reference states
// which column its identifier lives in. Guessing wrong filters the wrong column,
// matches nothing, and reports an active person as inactive.
func TestParseSubjectIdentity_RefusesUnknownColumns(t *testing.T) {
	t.Parallel()

	for _, test := range []struct{ value, wantKind, wantID string }{
		{value: FormatSubjectIdentity(SubjectIdentityEmail, "alice@example.com"), wantKind: SubjectIdentityEmail, wantID: "alice@example.com"},
		{value: FormatSubjectIdentity(SubjectIdentityExternal, "ext-1"), wantKind: SubjectIdentityExternal, wantID: "ext-1"},
		{value: FormatSubjectIdentity(SubjectIdentityUser, "user-1"), wantKind: SubjectIdentityUser, wantID: "user-1"},
	} {
		identityKind, identifier, err := parseSubjectIdentity(test.value)
		require.NoError(t, err)
		require.Equal(t, test.wantKind, identityKind)
		require.Equal(t, test.wantID, identifier)
	}

	for _, value := range []string{"", "alice@example.com", "unknown:alice", "email:", ":alice"} {
		_, _, err := parseSubjectIdentity(value)
		require.ErrorIs(t, err, ErrSubjectReferenceNotFound, value)
	}
}

func TestMaskSubject_RecognizableNotLearnable(t *testing.T) {
	t.Parallel()

	tests := []struct {
		subject string
		want    string
	}{
		{subject: "alice@example.com", want: "a***@e***"},
		{subject: "bo@x.io", want: "b*@x***"},
		{subject: "a@b", want: "*@*"},
		{subject: "external-user-1234", want: "e***"},
		{subject: "x", want: "*"},
	}

	// An empty subject masks to empty; it is covered separately because the
	// "must not equal the original" rule is vacuous for it.
	require.Empty(t, maskSubject(""))

	for _, test := range tests {
		t.Run(test.subject, func(t *testing.T) {
			t.Parallel()

			masked := maskSubject(test.subject)
			require.Equal(t, test.want, masked)
			// The mask must never carry the original back.
			require.NotEqual(t, test.subject, masked)
			require.NotContains(t, masked, "lice")
			require.NotContains(t, masked, "xample")
		})
	}
}

func TestQueryMCPEventsOutput_ProjectsOnlyAllowlistedFields(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 8, 21, 12, 0, 0, 0, time.UTC)
	window, err := resolveWindow("1h", now, drilldownWindowSpec)
	require.NoError(t, err)

	output := QueryMCPEventsOutput{
		ProjectID: "00000000-0000-0000-0000-000000000001",
		MCPID:     "00000000-0000-0000-0000-000000000002",
		Envelope:  newDataEnvelope(now, now.Add(-time.Minute), window, true),
		Tools:     []MCPToolEvents{{ToolName: "charge", Outcomes: MCPOutcomeSummary{Total: 4, Success: 1, ServerError: 3}}},
		Truncated: false,
	}

	require.ElementsMatch(t, []string{
		"project_id", "mcp_id",
		"data", "queried_at", "data_through", "freshness", "no_observations", "resolved_window", "window", "from", "to",
		"tools", "tool_name",
		"outcomes", "total", "success", "unauthorized", "client_error", "server_error", "failed", "unknown",
		"truncated",
	}, decodeKeys(t, output))
}

func TestQueryMCPMetricsOutput_ProjectsOnlyAllowlistedFields(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 8, 21, 12, 0, 0, 0, time.UTC)
	window, err := resolveWindow("7d", now, metricsWindowSpec)
	require.NoError(t, err)

	output := QueryMCPMetricsOutput{
		ProjectID:       "00000000-0000-0000-0000-000000000001",
		MCPID:           "00000000-0000-0000-0000-000000000002",
		Envelope:        newDataEnvelope(now, now.Add(-time.Minute), window, true),
		ToolCalls:       100,
		FailedToolCalls: 5,
		FailureRate:     0.05,
		AvgLatencyMs:    42.5,
		ActiveUsers:     NewSubjectCount(7),
	}

	require.ElementsMatch(t, []string{
		"project_id", "mcp_id",
		"data", "queried_at", "data_through", "freshness", "no_observations", "resolved_window", "window", "from", "to",
		"tool_calls", "failed_tool_calls", "failure_rate", "avg_latency_ms", "active_users",
		"active_users_unavailable",
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

// TestSummaryIdentityParams_UsesExactlyOneIdentityFilter pins that the summary
// read never ANDs the two identity filters. Hosted telemetry carries a toolset
// slug and no mcp_server id, so requiring both matches nothing — and the
// failure is silent, surfacing as a latency of zero rather than an error.
func TestSummaryIdentityParams_UsesExactlyOneIdentityFilter(t *testing.T) {
	t.Parallel()

	hosted := drilldownTarget{
		toolsetSlugs: []string{"billing"},
		projectID:    "project-1",
		mcpServerID:  "mcp-1",
	}
	params := summaryIdentityParams(hosted, 1, 2)
	require.Equal(t, "billing", params.ToolsetSlug)
	require.Empty(t, params.MCPServerID)

	// A remote, tunneled, or unproxied server carries no slug, so it is scoped
	// by its server id instead — never by neither, which would read as "no
	// filter" and return the whole project.
	remote := drilldownTarget{
		projectID:   "project-1",
		mcpServerID: "mcp-1",
	}
	params = summaryIdentityParams(remote, 1, 2)
	require.Empty(t, params.ToolsetSlug)
	require.Equal(t, "mcp-1", params.MCPServerID)
	require.Equal(t, "project-1", params.GramProjectID)
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

// TestCursorPosition_CarriesTheCompositeKey pins that a cursor keeps both
// halves of the ordering key. A timestamp-only cursor skips every trace sharing
// the boundary nanosecond, so on a busy server those occurrences appear on
// neither page.
func TestCursorPosition_CarriesTheCompositeKey(t *testing.T) {
	t.Parallel()

	position, traceID, traversed, err := parseCursorPosition(formatCursorPosition(1_700_000_000_000_000_000, "abc123", 40))
	require.NoError(t, err)
	require.Equal(t, int64(1_700_000_000_000_000_000), position)
	require.Equal(t, "abc123", traceID)
	require.Equal(t, 40, traversed)

	for _, value := range []string{"", "1700000000", "t:", "t:abc", "t:-1:0:x", "t:0:0:x", "t:1700000000", "t:1700000000:x", "t:1700000000:abc:x", "t:1700000000:-1:x"} {
		_, _, _, err := parseCursorPosition(value)
		require.ErrorIs(t, err, ErrSubjectReferenceNotFound, value)
	}
}

// TestCursorPosition_RefusesATraversalPastTheCap pins that the traversal count
// a cursor carries cannot exceed the cap. The count is sealed inside the token,
// but a decode that accepted an over-large value would let one forged or
// replayed cursor reopen a traversal the cap had already closed.
func TestCursorPosition_RefusesATraversalPastTheCap(t *testing.T) {
	t.Parallel()

	_, _, _, err := parseCursorPosition(formatCursorPosition(1_700_000_000_000_000_000, "abc123", maxTraceTraversal+1))
	require.ErrorIs(t, err, ErrSubjectReferenceNotFound)
}

// TestFitRows_TrimsToTheResponseCap pins that an oversized projection is cut to
// fit rather than returned whole. The rows arrive ordered by what a caller
// should see first, so the trim costs it the least useful ones.
func TestFitRows_TrimsToTheResponseCap(t *testing.T) {
	t.Parallel()

	traces := make([]MCPTraceReference, 0, 4000)
	for i := range 4000 {
		traces = append(traces, MCPTraceReference{
			Reference:  strings.Repeat("r", 128) + strconv.Itoa(i),
			OccurredAt: "2026-08-21T12:00:00Z",
			ToolName:   "search",
			Outcome:    "server_error",
			Client:     "claude-code",
		})
	}
	output := QueryMCPTracesOutput{ProjectID: "project-1", MCPID: "mcp-1", Traces: traces}

	fitted, dropped, err := fitRows(output.Traces, func(rows []MCPTraceReference) any {
		candidate := output
		candidate.Traces = rows
		return candidate
	})
	require.NoError(t, err)
	require.True(t, dropped)
	require.Less(t, len(fitted), len(traces))

	candidate := output
	candidate.Traces = fitted
	encoded, err := json.Marshal(candidate)
	require.NoError(t, err)
	require.LessOrEqual(t, len(encoded), maxDrilldownResponseBytes)
}

// TestFitRows_LeavesAFittingResponseAlone pins that the cap never trims a
// result that already fits: a caller must not silently lose rows to a
// measurement that was not needed.
func TestFitRows_LeavesAFittingResponseAlone(t *testing.T) {
	t.Parallel()

	output := QueryMCPEventsOutput{
		ProjectID: "project-1",
		MCPID:     "mcp-1",
		Tools:     []MCPToolEvents{{ToolName: "search", Outcomes: MCPOutcomeSummary{Total: 3, Success: 3}}},
	}
	fitted, dropped, err := fitRows(output.Tools, func(rows []MCPToolEvents) any {
		candidate := output
		candidate.Tools = rows
		return candidate
	})
	require.NoError(t, err)
	require.False(t, dropped)
	require.Len(t, fitted, 1)
}
