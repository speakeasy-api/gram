package repo_test

import (
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/telemetry/repo"
)

// TestListToolUsageTracesAttributesAgentKeyHookEvents pins that a hook event
// sent with an agent key reads as that agent on both the trace_summaries and
// the raw telemetry_logs paths, while hook events sent with organization or
// personal keys keep their human attribution.
func TestListToolUsageTracesAttributesAgentKeyHookEvents(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	conn, err := infra.NewClickhouseClient(t)
	require.NoError(t, err)
	queries := repo.New(conn)

	projectID := uuid.NewString()
	now := time.Now().UTC().Add(-time.Minute)
	agentID := uuid.NewString()
	agentTraceID := strings.ReplaceAll(uuid.NewString(), "-", "")
	orgKeyTraceID := strings.ReplaceAll(uuid.NewString(), "-", "")
	personalKeyTraceID := strings.ReplaceAll(uuid.NewString(), "-", "")

	// Rows as withAgentActor leaves them: an agent key stamps the trusted actor
	// and drops self-reported identity, other keys carry no actor at all.
	for _, row := range []struct {
		traceID string
		attrs   map[string]any
	}{
		{agentTraceID, map[string]any{"gram.hook.event": "PreToolUse"}},
		{agentTraceID, map[string]any{
			"gram.hook.event":               "PostToolUse",
			"gen_ai.tool.call.result":       "ok",
			"gram.authorization.actor.type": "agent",
			"gram.authorization.actor.id":   agentID,
		}},
		{orgKeyTraceID, map[string]any{"gram.hook.event": "PostToolUse", "user.email": "dev@example.com"}},
		{personalKeyTraceID, map[string]any{"gram.hook.event": "PostToolUse", "user.id": "user_personal"}},
	} {
		row.attrs["gram.event.source"] = "hook"
		row.attrs["gram.hook.source"] = "claude-code"
		row.attrs["gram.tool.name"] = "Bash"
		attrs, err := json.Marshal(row.attrs)
		require.NoError(t, err)
		// One committed insert per row, so the agent trace's sparse actor is
		// also merged across separate MV insert blocks.
		err = queries.InsertTelemetryLogsSync(ctx, []repo.InsertTelemetryLogParams{{
			ID: uuid.NewString(), GramProjectID: projectID,
			TimeUnixNano: now.UnixNano(), ObservedTimeUnixNano: now.UnixNano(),
			TraceID: &row.traceID, GramURN: "hooks:Bash", ServiceName: "gram-hooks",
			Body: "hook tool event", Attributes: string(attrs), ResourceAttributes: "{}",
		}})
		require.NoError(t, err)
	}

	type attribution struct {
		kind  string
		key   string
		label string
	}
	want := map[string]attribution{
		agentTraceID:       {"agent_id", agentID, "agent:" + agentID},
		orgKeyTraceID:      {"email", "dev@example.com", "dev@example.com"},
		personalKeyTraceID: {"user_id", "user_personal", "user_personal"},
	}
	for name, query := range map[string]string{"summary": "", "raw": ":"} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			traces, err := queries.ListToolUsageTraces(ctx, repo.ListToolUsageTracesParams{
				GramProjectID: projectID,
				TimeStart:     now.Add(-time.Hour).UnixNano(),
				TimeEnd:       now.Add(time.Hour).UnixNano(),
				Query:         query,
				SortOrder:     "desc",
				Limit:         10,
			})
			require.NoError(t, err)
			require.Len(t, traces, len(want))
			for _, trace := range traces {
				expected, ok := want[trace.TraceID]
				require.True(t, ok, "unexpected trace %s", trace.TraceID)
				require.Equal(t, expected, attribution{trace.UserKind, trace.UserKey, trace.UserLabel}, trace.TraceID)
			}
		})
	}
}
