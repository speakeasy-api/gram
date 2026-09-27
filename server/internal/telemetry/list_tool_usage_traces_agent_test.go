package telemetry_test

import (
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	gen "github.com/speakeasy-api/gram/server/gen/telemetry"
	telemetryRepo "github.com/speakeasy-api/gram/server/internal/telemetry/repo"
	"github.com/speakeasy-api/gram/server/internal/testenv"
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
		err = ti.chClient.InsertTelemetryLog(ctx, telemetryRepo.InsertTelemetryLogParams{
			ID: uuid.NewString(), GramProjectID: ti.projectID,
			TimeUnixNano: now.UnixNano(), ObservedTimeUnixNano: now.UnixNano(),
			TraceID: &row.traceID, GramURN: "tools:http:gram:charge",
			Body: "hosted tool event", Attributes: string(attrs), ResourceAttributes: "{}",
		})
		require.NoError(t, err)
		// Exercise sparse attribution across separate MV insert blocks, too.
		testenv.FlushClickHouseAsyncInserts(t, ti.chConn)
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
