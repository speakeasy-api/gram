package telemetry_test

import (
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	gen "github.com/speakeasy-api/gram/server/gen/telemetry"
	telemetryRepo "github.com/speakeasy-api/gram/server/internal/telemetry/repo"
	"github.com/stretchr/testify/require"
)

func TestListToolUsageTraces_SparseAgentAttributionMatchesSummary(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestLogsService(t)
	now := time.Now().UTC().Add(-time.Minute)
	agentID := uuid.NewString()
	agentTraceID := strings.ReplaceAll(uuid.NewString(), "-", "")
	humanTraceID := strings.ReplaceAll(uuid.NewString(), "-", "")
	for _, row := range []struct {
		traceID     string
		actorType   string
		actorID     string
		eventSource string
	}{
		{agentTraceID, "", "", "tool_call"},
		{agentTraceID, "agent", agentID, "tool_call"},
		{humanTraceID, "user", agentID, "tool_call"}, // Same ID must not make a human an agent.
		// Client-supplied actor metadata must not become a managed-agent identity.
		{strings.ReplaceAll(uuid.NewString(), "-", ""), "agent", agentID, "hosted"},
	} {
		attrs, err := json.Marshal(map[string]any{
			"gram.event.source":             row.eventSource,
			"gram.toolset.slug":             "payments",
			"gen_ai.tool.name":              "charge",
			"user.email":                    "approver@example.com",
			"gram.authorization.actor.type": row.actorType,
			"gram.authorization.actor.id":   row.actorID,
			"http.response.status_code":     200,
		})
		require.NoError(t, err)
		// One committed insert per row, so sparse attribution is also exercised
		// across separate MV insert blocks.
		err = ti.chClient.InsertTelemetryLogsSync(ctx, []telemetryRepo.InsertTelemetryLogParams{{
			ID: uuid.NewString(), GramProjectID: ti.projectID,
			TimeUnixNano: now.UnixNano(), ObservedTimeUnixNano: now.UnixNano(),
			TraceID: &row.traceID, GramURN: "tools:http:gram:charge",
			Body: "hosted tool event", Attributes: string(attrs), ResourceAttributes: "{}",
		}})
		require.NoError(t, err)
	}
	payload := &gen.ListToolUsageTracesPayload{
		From: now.Add(-time.Hour).Format(time.RFC3339),
		To:   now.Add(time.Hour).Format(time.RFC3339), Limit: 10,
	}
	summary, err := ti.service.ListToolUsageTraces(ctx, payload)
	require.NoError(t, err)
	require.Len(t, summary.Traces, 3)
	query := ":"
	payload.Query = &query
	raw, err := ti.service.ListToolUsageTraces(ctx, payload)
	require.NoError(t, err)
	require.Len(t, raw.Traces, 3)
	byID := make(map[string]*gen.ToolUsageTraceSummary)
	for _, trace := range summary.Traces {
		require.NotNil(t, trace.TraceID)
		byID[*trace.TraceID] = trace
	}
	for _, trace := range raw.Traces {
		require.NotNil(t, trace.TraceID)
		want := byID[*trace.TraceID]
		require.NotNil(t, want)
		require.Equal(t, want.UserKey, trace.UserKey)
		require.Equal(t, want.UserKind, trace.UserKind)
		require.Equal(t, want.UserLabel, trace.UserLabel)
		require.Equal(t, want.LogCount, trace.LogCount)
		require.Equal(t, want.HTTPStatusCode, trace.HTTPStatusCode)
		if *trace.TraceID == agentTraceID {
			require.Equal(t, gen.ToolUsageUserKind("agent_id"), trace.UserKind)
			require.Equal(t, agentID, trace.UserKey)
			require.Equal(t, "agent:"+agentID, trace.UserLabel)
			require.EqualValues(t, 2, trace.LogCount)
		} else {
			require.Equal(t, gen.ToolUsageUserKind("email"), trace.UserKind)
			require.Equal(t, "approver@example.com", trace.UserKey)
			require.EqualValues(t, 1, trace.LogCount)
		}
	}
}

// An agent's reads narrow to its traces before they group. The narrowing
// must keep every summary row of those traces, including a sibling row that
// names no agent, and must not let another agent's or a person's trace in.
func TestToolUsage_AgentScopedReadsKeepWholeTraces(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestLogsService(t)
	now := time.Now().UTC().Add(-time.Minute)
	agentID := uuid.NewString()
	otherAgentID := uuid.NewString()
	agentTraceID := strings.ReplaceAll(uuid.NewString(), "-", "")
	otherAgentTraceID := strings.ReplaceAll(uuid.NewString(), "-", "")
	humanTraceID := strings.ReplaceAll(uuid.NewString(), "-", "")
	for _, row := range []struct {
		traceID   string
		actorType string
		actorID   string
		status    int
	}{
		// The agent's trace: its first row names no agent and carries the
		// failure, the second names the agent.
		{agentTraceID, "", "", 500},
		{agentTraceID, "agent", agentID, 500},
		{otherAgentTraceID, "agent", otherAgentID, 200},
		{humanTraceID, "user", agentID, 200},
	} {
		attrs, err := json.Marshal(map[string]any{
			"gram.event.source":             "tool_call",
			"gram.toolset.slug":             "payments",
			"gen_ai.tool.name":              "charge",
			"user.email":                    "approver@example.com",
			"gram.authorization.actor.type": row.actorType,
			"gram.authorization.actor.id":   row.actorID,
			"http.response.status_code":     row.status,
		})
		require.NoError(t, err)
		err = ti.chClient.InsertTelemetryLogsSync(ctx, []telemetryRepo.InsertTelemetryLogParams{{
			ID: uuid.NewString(), GramProjectID: ti.projectID,
			TimeUnixNano: now.UnixNano(), ObservedTimeUnixNano: now.UnixNano(),
			TraceID: &row.traceID, GramURN: "tools:http:gram:charge",
			Body: "hosted tool event", Attributes: string(attrs), ResourceAttributes: "{}",
		}})
		require.NoError(t, err)
	}
	from := now.Add(-time.Hour).Format(time.RFC3339)
	to := now.Add(time.Hour).Format(time.RFC3339)
	agentFilter := []*gen.ToolUsageUserFilter{{Kind: "agent_id", Key: agentID}}

	traces, err := ti.service.ListToolUsageTraces(ctx, &gen.ListToolUsageTracesPayload{
		From: from, To: to, Limit: 10, UserFilters: agentFilter,
	})
	require.NoError(t, err)
	require.Len(t, traces.Traces, 1)
	require.Equal(t, agentTraceID, *traces.Traces[0].TraceID)
	require.EqualValues(t, 2, traces.Traces[0].LogCount)
	require.Equal(t, agentID, traces.Traces[0].UserKey)

	for name, payload := range map[string]*gen.GetToolUsageTotalsPayload{
		"user filter": {From: from, To: to, UserFilters: agentFilter},
		"agent ids":   {From: from, To: to, AgentIds: []string{agentID}},
	} {
		totals, err := ti.service.GetToolUsageTotals(ctx, payload)
		require.NoError(t, err, name)
		require.Equal(t, int64(1), totals.Totals.EventCount, name)
		require.Equal(t, int64(1), totals.Totals.FailureCount, name)
	}

	options, err := ti.service.GetToolUsageFilterOptions(ctx, &gen.GetToolUsageFilterOptionsPayload{
		From: from, To: to, UserFilters: agentFilter,
	})
	require.NoError(t, err)
	require.NotEmpty(t, options.HostedServers)
	require.NotEmpty(t, options.Users)

	// An identity's own Tool logs shows no user facet, so it asks for the
	// server and client groups alone and the user reads do not run.
	serversOnly, err := ti.service.GetToolUsageFilterOptions(ctx, &gen.GetToolUsageFilterOptionsPayload{
		From: from, To: to, UserFilters: agentFilter,
		OptionTypes: []gen.ToolUsageFilterOptionType{"hosted_servers", "shadow_servers", "gateways", "clients"},
	})
	require.NoError(t, err)
	require.NotEmpty(t, serversOnly.HostedServers)
	require.NotEmpty(t, serversOnly.Clients)
	require.Empty(t, serversOnly.Users)

	// A person's filter is not narrowed by agent, so it still finds every
	// call recorded under the email.
	people, err := ti.service.GetToolUsageTotals(ctx, &gen.GetToolUsageTotalsPayload{
		From: from, To: to,
		UserFilters: []*gen.ToolUsageUserFilter{{Kind: "email", Key: "approver@example.com"}},
	})
	require.NoError(t, err)
	require.Equal(t, int64(1), people.Totals.EventCount)
}
