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

// Hook rows as withAgentActor leaves them: an agent key stamps the trusted
// actor and drops self-reported identity, other keys carry no actor at all.
func TestToolUsageHookEventsFilterByAgentIdentity(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	conn, err := infra.NewClickhouseClient(t)
	require.NoError(t, err)
	queries := repo.New(conn)

	projectID := uuid.NewString()
	now := time.Now().UTC().Add(-time.Minute)
	timeStart := now.Add(-time.Hour).UnixNano()
	timeEnd := now.Add(time.Hour).UnixNano()

	agentID := uuid.NewString()
	otherAgentID := uuid.NewString()
	claudeTraceID := strings.ReplaceAll(uuid.NewString(), "-", "")
	cursorTraceID := strings.ReplaceAll(uuid.NewString(), "-", "")
	otherAgentTraceID := strings.ReplaceAll(uuid.NewString(), "-", "")
	humanTraceID := strings.ReplaceAll(uuid.NewString(), "-", "")

	for _, row := range []struct {
		traceID    string
		harness    string
		agentID    string
		userEmail  string
		authorizer string
	}{
		// One agent across two harnesses must read as one identity.
		{claudeTraceID, "claude-code", agentID, "", ""},
		{cursorTraceID, "cursor", agentID, "", ""},
		// A second agent on the same harness as the first must stay apart.
		// The hooks service does not strip a client-supplied authorizer, so
		// it must not surface as who the agent acted for.
		{otherAgentTraceID, "claude-code", otherAgentID, "", uuid.NewString()},
		// A person sending hooks with an organization key.
		{humanTraceID, "claude-code", "", "dev@example.com", ""},
	} {
		for _, event := range []string{"PreToolUse", "PostToolUse"} {
			attrs := map[string]any{
				"gram.event.source": "hook",
				"gram.hook.source":  row.harness,
				"gram.hook.event":   event,
				"gram.tool.name":    "Bash",
			}
			if event == "PostToolUse" {
				attrs["gen_ai.tool.call.result"] = "ok"
			}
			if row.agentID != "" {
				attrs["gram.authorization.actor.type"] = "agent"
				attrs["gram.authorization.actor.id"] = row.agentID
			}
			if row.userEmail != "" {
				attrs["user.email"] = row.userEmail
			}
			if row.authorizer != "" {
				attrs["gram.authorization.authorizer_user_id"] = row.authorizer
			}
			encoded, err := json.Marshal(attrs)
			require.NoError(t, err)
			err = queries.InsertTelemetryLogsSync(ctx, []repo.InsertTelemetryLogParams{{
				ID: uuid.NewString(), GramProjectID: projectID,
				TimeUnixNano: now.UnixNano(), ObservedTimeUnixNano: now.UnixNano(),
				TraceID: &row.traceID, GramURN: "hooks:Bash", ServiceName: "gram-hooks",
				Body: "hook tool event", Attributes: string(encoded), ResourceAttributes: "{}",
			}})
			require.NoError(t, err)
		}
	}

	type attribution struct {
		kind  string
		key   string
		label string
	}
	wantAttribution := map[string]attribution{
		claudeTraceID:     {"agent_id", agentID, "agent:" + agentID},
		cursorTraceID:     {"agent_id", agentID, "agent:" + agentID},
		otherAgentTraceID: {"agent_id", otherAgentID, "agent:" + otherAgentID},
		humanTraceID:      {"email", "dev@example.com", "dev@example.com"},
	}

	tests := []struct {
		name      string
		agents    []string
		wantTrace []string
	}{
		{"it attributes every hook trace to its sender", nil, []string{claudeTraceID, cursorTraceID, otherAgentTraceID, humanTraceID}},
		{"it narrows to one agent's hook calls across harnesses", []string{agentID}, []string{claudeTraceID, cursorTraceID}},
		{"it matches several agents' hook calls", []string{agentID, otherAgentID}, []string{claudeTraceID, cursorTraceID, otherAgentTraceID}},
	}
	for _, tt := range tests {
		for path, query := range map[string]string{"summary": "", "raw": ":"} {
			t.Run(tt.name+" ("+path+" path)", func(t *testing.T) {
				t.Parallel()
				traces, err := queries.ListToolUsageTraces(ctx, repo.ListToolUsageTracesParams{
					GramProjectID: projectID,
					TimeStart:     timeStart,
					TimeEnd:       timeEnd,
					AgentIDs:      tt.agents,
					Query:         query,
					SortOrder:     "desc",
					Limit:         10,
				})
				require.NoError(t, err)
				got := make([]string, 0, len(traces))
				for _, trace := range traces {
					got = append(got, trace.TraceID)
					gotAttribution := attribution{trace.UserKind, trace.UserKey, trace.UserLabel}
					require.Equal(t, wantAttribution[trace.TraceID], gotAttribution, trace.TraceID)
					require.Empty(t, trace.AuthorizerUserID, trace.TraceID)
				}
				require.ElementsMatch(t, tt.wantTrace, got)
			})
		}
	}

	t.Run("it offers each agent as one option however many harnesses it used", func(t *testing.T) {
		t.Parallel()
		options, err := queries.GetToolUsageFilterOptions(ctx, repo.GetToolUsageSummaryParams{
			GramProjectID: projectID,
			TimeStart:     timeStart,
			TimeEnd:       timeEnd,
		})
		require.NoError(t, err)
		agents := make(map[string]uint64)
		for _, user := range options.Users {
			if user.UserKind != "agent_id" {
				continue
			}
			_, dup := agents[user.UserKey]
			require.False(t, dup, user.UserKey)
			agents[user.UserKey] = user.EventCount
		}
		require.Equal(t, map[string]uint64{agentID: 2, otherAgentID: 1}, agents)
		require.Empty(t, options.Authorizers)
	})

	t.Run("it counts one agent's hook calls in the totals", func(t *testing.T) {
		t.Parallel()
		totals, err := queries.GetToolUsageTotals(ctx, repo.GetToolUsageSummaryParams{
			GramProjectID: projectID,
			TimeStart:     timeStart,
			TimeEnd:       timeEnd,
			AgentIDs:      []string{agentID},
		})
		require.NoError(t, err)
		require.EqualValues(t, 2, totals.EventCount)
	})
}
