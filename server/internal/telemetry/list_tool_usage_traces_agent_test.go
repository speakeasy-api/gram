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
	workloadAgentID := uuid.NewString()
	workloadTraceID := strings.ReplaceAll(uuid.NewString(), "-", "")
	for _, row := range []struct {
		traceID     string
		actorType   string
		actorID     string
		agentAttr   string
		eventSource string
	}{
		{agentTraceID, "", "", "", "tool_call"},
		{agentTraceID, "agent", agentID, agentID, "tool_call"},
		{humanTraceID, "user", agentID, "", "tool_call"}, // Same ID must not make a human an agent.
		// A workload stays the actor; its call counts as the agent it acted for.
		{workloadTraceID, "workload", uuid.NewString() + ":repo:acme/api", workloadAgentID, "tool_call"},
		// Client-supplied actor metadata must not become a managed-agent identity.
		{strings.ReplaceAll(uuid.NewString(), "-", ""), "agent", agentID, "", "hosted"},
		{strings.ReplaceAll(uuid.NewString(), "-", ""), "workload", "", workloadAgentID, "hosted"},
	} {
		attrs, err := json.Marshal(map[string]any{
			"gram.event.source":             row.eventSource,
			"gram.toolset.slug":             "payments",
			"gen_ai.tool.name":              "charge",
			"user.email":                    "approver@example.com",
			"gram.authorization.actor.type": row.actorType,
			"gram.authorization.actor.id":   row.actorID,
			"gram.authorization.agent.id":   row.agentAttr,
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
	require.Len(t, summary.Traces, 5)
	query := ":"
	payload.Query = &query
	raw, err := ti.service.ListToolUsageTraces(ctx, payload)
	require.NoError(t, err)
	require.Len(t, raw.Traces, 5)
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
		switch *trace.TraceID {
		case agentTraceID:
			require.Equal(t, gen.ToolUsageUserKind("agent_id"), trace.UserKind)
			require.Equal(t, agentID, trace.UserKey)
			require.Equal(t, "agent:"+agentID, trace.UserLabel)
			require.EqualValues(t, 2, trace.LogCount)
		case workloadTraceID:
			require.Equal(t, gen.ToolUsageUserKind("agent_id"), trace.UserKind)
			require.Equal(t, workloadAgentID, trace.UserKey)
			require.Equal(t, "agent:"+workloadAgentID, trace.UserLabel)
			require.EqualValues(t, 1, trace.LogCount)
		default:
			require.Equal(t, gen.ToolUsageUserKind("email"), trace.UserKind)
			require.Equal(t, "approver@example.com", trace.UserKey)
			require.EqualValues(t, 1, trace.LogCount)
		}
	}
}
