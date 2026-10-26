package telemetry_test

import (
	"encoding/json"
	"maps"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	gen "github.com/speakeasy-api/gram/server/gen/telemetry"
	telemetryRepo "github.com/speakeasy-api/gram/server/internal/telemetry/repo"
	"github.com/stretchr/testify/require"
)

// Hook rows as withAgentActor leaves them: an agent key stamps the trusted
// actor and drops self-reported identity, other keys carry no actor at all.
func TestListToolUsageTraces_HookEventsFilterByAgentIdentity(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestLogsService(t)
	now := time.Now().UTC().Add(-time.Minute)

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
			err = ti.chClient.InsertTelemetryLogsSync(ctx, []telemetryRepo.InsertTelemetryLogParams{{
				ID: uuid.NewString(), GramProjectID: ti.projectID,
				TimeUnixNano: now.UnixNano(), ObservedTimeUnixNano: now.UnixNano(),
				TraceID: &row.traceID, GramURN: "hooks:Bash", ServiceName: "gram-hooks",
				Body: "hook tool event", Attributes: string(encoded), ResourceAttributes: "{}",
			}})
			require.NoError(t, err)
		}
	}

	from := now.Add(-time.Hour).Format(time.RFC3339)
	to := now.Add(time.Hour).Format(time.RFC3339)
	rawQuery := ":"

	type attribution struct {
		kind  gen.ToolUsageUserKind
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
		for _, query := range []*string{nil, &rawQuery} {
			path := "summary"
			if query != nil {
				path = "raw"
			}
			t.Run(tt.name+" ("+path+" path)", func(t *testing.T) {
				t.Parallel()
				res, err := ti.service.ListToolUsageTraces(ctx, &gen.ListToolUsageTracesPayload{
					From: from, To: to, Limit: 10, AgentIds: tt.agents, Query: query,
				})
				require.NoError(t, err)
				got := make(map[string]*gen.ToolUsageTraceSummary, len(res.Traces))
				for _, trace := range res.Traces {
					require.NotNil(t, trace.TraceID)
					got[*trace.TraceID] = trace
				}
				require.ElementsMatch(t, tt.wantTrace, slices.Collect(maps.Keys(got)))
				for traceID, trace := range got {
					gotAttribution := attribution{trace.UserKind, trace.UserKey, trace.UserLabel}
					require.Equal(t, wantAttribution[traceID], gotAttribution, traceID)
					require.Nil(t, trace.AuthorizerUserID, traceID)
				}
			})
		}
	}

	t.Run("it offers each agent as one option however many harnesses it used", func(t *testing.T) {
		t.Parallel()
		res, err := ti.service.GetToolUsageFilterOptions(ctx, &gen.GetToolUsageFilterOptionsPayload{
			From: from, To: to, OptionTypes: []gen.ToolUsageFilterOptionType{"users"},
		})
		require.NoError(t, err)
		agents := make(map[string]int64)
		for _, user := range res.Users {
			if user.UserKind != "agent_id" {
				continue
			}
			_, dup := agents[user.UserKey]
			require.False(t, dup, user.UserKey)
			agents[user.UserKey] = user.EventCount
		}
		require.Equal(t, map[string]int64{agentID: 2, otherAgentID: 1}, agents)
	})

	t.Run("it counts one agent's hook calls in the totals", func(t *testing.T) {
		t.Parallel()
		res, err := ti.service.GetToolUsageTotals(ctx, &gen.GetToolUsageTotalsPayload{
			From: from, To: to, AgentIds: []string{agentID},
		})
		require.NoError(t, err)
		require.EqualValues(t, 2, res.Totals.EventCount)
	})
}
