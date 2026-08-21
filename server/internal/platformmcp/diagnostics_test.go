package platformmcp

import (
	"encoding/json"
	"sort"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	telemetryrepo "github.com/speakeasy-api/gram/server/internal/telemetry/repo"
)

// jsonKeys walks a decoded result and returns every key that appears anywhere
// in it, so a leak test asserts on the whole shape rather than the top level.
func jsonKeys(value any) []string {
	var keys []string
	switch typed := value.(type) {
	case map[string]any:
		for key, nested := range typed {
			keys = append(keys, key)
			keys = append(keys, jsonKeys(nested)...)
		}
	case []any:
		for _, nested := range typed {
			keys = append(keys, jsonKeys(nested)...)
		}
	}
	sort.Strings(keys)
	return keys
}

func decodeKeys(t *testing.T, value any) []string {
	t.Helper()

	encoded, err := json.Marshal(value)
	require.NoError(t, err)
	var decoded any
	require.NoError(t, json.Unmarshal(encoded, &decoded))
	return jsonKeys(decoded)
}

// TestGetProjectOverviewOutput_ProjectsOnlyAllowlistedFields pins the overview's
// serialized shape. The projection is positive: a field that is not listed here
// is not served, so a future addition has to be made deliberately in this test
// before it can reach a caller.
func TestGetProjectOverviewOutput_ProjectsOnlyAllowlistedFields(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 8, 21, 12, 0, 0, 0, time.UTC)
	window, err := resolveWindow("24h", now)
	require.NoError(t, err)

	output := GetProjectOverviewOutput{
		ProjectID:       "00000000-0000-0000-0000-000000000001",
		Envelope:        newDataEnvelope(now, now.Add(-time.Minute), window),
		MetricsMode:     "tool_call",
		ToolCalls:       120,
		FailedToolCalls: 4,
		ActiveServers:   3,
		ActiveUsers:     NewSubjectCount(11),
		TopServers:      []ProjectOverviewServer{{Name: "billing", ToolCalls: 40}},
	}

	require.ElementsMatch(t, []string{
		"project_id",
		"data", "queried_at", "data_through", "freshness", "resolved_window", "window", "from", "to",
		"metrics_mode",
		"tool_calls", "failed_tool_calls", "active_servers", "active_users",
		"top_servers", "name", "tool_calls",
	}, decodeKeys(t, output))
}

// TestGetMCPDiagnosticsOutput_ProjectsOnlyAllowlistedFields does the same for
// the diagnosis. Nothing here carries a prompt, an argument, a result, a
// header, a URL, an email, an external user id, or a raw status code.
func TestGetMCPDiagnosticsOutput_ProjectsOnlyAllowlistedFields(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 8, 21, 12, 0, 0, 0, time.UTC)
	window, err := resolveWindow("7d", now)
	require.NoError(t, err)

	output := GetMCPDiagnosticsOutput{
		ProjectID:            "00000000-0000-0000-0000-000000000001",
		MCPID:                "00000000-0000-0000-0000-000000000002",
		Envelope:             newDataEnvelope(now, now.Add(-time.Minute), window),
		Readiness:            MCPDiagnosticsReadiness{State: string(ReadinessReady), Freshness: "fresh", CheckedAt: now.Format(time.RFC3339)},
		Outcomes:             MCPOutcomeSummary{Total: 10, Success: 4, Unauthorized: 6},
		OrganizationOutcomes: MCPOutcomeSummary{Total: 100, Success: 98, ServerError: 2},
		Clients:              []MCPClientEvidence{{Client: "claude-code", Calls: 6, Failures: 6}},
		ClientsTruncated:     false,
		Attribution: FaultAttribution{
			Fault:               FaultGramConfiguration,
			Reason:              reasonUnauthorizedDominant,
			ReadinessExonerates: false,
			Scope:               FaultScopeServerSpecific,
		},
	}

	require.ElementsMatch(t, []string{
		"project_id", "mcp_id",
		"data", "queried_at", "data_through", "freshness", "resolved_window", "window", "from", "to",
		"readiness", "state", "freshness", "checked_at",
		"outcomes", "total", "success", "unauthorized", "client_error", "server_error", "failed", "unknown",
		"organization_outcomes", "total", "success", "unauthorized", "client_error", "server_error", "failed", "unknown",
		"clients", "client", "calls", "failures",
		"clients_truncated",
		"attribution", "fault", "reason", "readiness_exonerates", "scope",
	}, decodeKeys(t, output))
}

func TestTotalsFromRows_SumsPerOutcomeClass(t *testing.T) {
	t.Parallel()

	totals := totalsFromRows([]telemetryrepo.MCPOutcomeBreakdownRow{
		{Client: "claude-code", Outcome: telemetryrepo.MCPOutcomeSuccess, CallCount: 10},
		{Client: "claude-code", Outcome: telemetryrepo.MCPOutcomeUnauthorized, CallCount: 3},
		{Client: telemetryrepo.MCPClientUnattributed, Outcome: telemetryrepo.MCPOutcomeServerError, CallCount: 2},
		{Client: telemetryrepo.MCPClientUnattributed, Outcome: telemetryrepo.MCPOutcomeClientError, CallCount: 1},
		{Client: "cursor", Outcome: telemetryrepo.MCPOutcomeFailed, CallCount: 4},
		// An outcome this build does not know classifies as unknown rather
		// than being dropped, so the totals still add up.
		{Client: "cursor", Outcome: "something-new", CallCount: 5},
	})

	require.Equal(t, outcomeTotals{Total: 25, Success: 10, Unauthorized: 3, ClientError: 1, ServerError: 2, Failed: 4, Unknown: 5}, totals)
	require.Equal(t, int64(10), totals.failures())
}

func TestClientEvidence_OrdersSuspectsFirstAndReportsTruncation(t *testing.T) {
	t.Parallel()

	rows := []telemetryrepo.MCPOutcomeBreakdownRow{
		{Client: "quiet", Outcome: telemetryrepo.MCPOutcomeSuccess, CallCount: 50},
		{Client: "noisy", Outcome: telemetryrepo.MCPOutcomeSuccess, CallCount: 1},
		{Client: "noisy", Outcome: telemetryrepo.MCPOutcomeUnauthorized, CallCount: 9},
	}

	clients, truncated := clientEvidence(rows)
	require.False(t, truncated)
	require.Equal(t, []MCPClientEvidence{
		{Client: "noisy", Calls: 10, Failures: 9},
		{Client: "quiet", Calls: 50, Failures: 0},
	}, clients)

	// A cut list says it was cut: a silently truncated list reads as complete
	// coverage of the clients calling this server.
	var many []telemetryrepo.MCPOutcomeBreakdownRow
	for index := range maxDiagnosticClients + 3 {
		many = append(many, telemetryrepo.MCPOutcomeBreakdownRow{
			Client:    string(rune('a' + index)),
			Outcome:   telemetryrepo.MCPOutcomeUnauthorized,
			CallCount: uint64(index + 1),
		})
	}
	capped, truncated := clientEvidence(many)
	require.True(t, truncated)
	require.Len(t, capped, maxDiagnosticClients)
}

// TestClientEvidence_UnknownOutcomesAreNotFailures pins that an outcome the
// server could not classify is not counted against a client. Charging a client
// for a call nobody could classify would manufacture a suspect.
func TestClientEvidence_UnknownOutcomesAreNotFailures(t *testing.T) {
	t.Parallel()

	clients, _ := clientEvidence([]telemetryrepo.MCPOutcomeBreakdownRow{
		{Client: "claude-code", Outcome: telemetryrepo.MCPOutcomeUnknown, CallCount: 7},
	})
	require.Equal(t, []MCPClientEvidence{{Client: "claude-code", Calls: 7, Failures: 0}}, clients)
}

func TestMCPURLSuffixes_OnlyWhenSlugKnown(t *testing.T) {
	t.Parallel()

	require.Nil(t, mcpURLSuffixes(""))
	require.Equal(t, []string{"/mcp/billing"}, mcpURLSuffixes("billing"))
	require.Nil(t, nonEmpty(""))
	require.Equal(t, []string{"billing"}, nonEmpty("billing"))
}
