package telemetry_test

import (
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	gen "github.com/speakeasy-api/gram/server/gen/telemetry"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	telemetryRepo "github.com/speakeasy-api/gram/server/internal/telemetry/repo"
	"github.com/stretchr/testify/require"
)

func TestListToolUsageTraces_AuthorizerFoldsIntoUserFilter(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestLogsService(t)
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	require.NotNil(t, authCtx.Email)
	email := *authCtx.Email

	now := time.Now().UTC().Add(-time.Minute)
	agentID := uuid.NewString()
	delegatedTraceID := strings.ReplaceAll(uuid.NewString(), "-", "")
	selfSignedTraceID := strings.ReplaceAll(uuid.NewString(), "-", "")
	directTraceID := strings.ReplaceAll(uuid.NewString(), "-", "")
	otherTraceID := strings.ReplaceAll(uuid.NewString(), "-", "")
	for _, row := range []struct {
		traceID    string
		userEmail  string
		actorType  string
		authorizer string
	}{
		// The agent acted for the signed-in person.
		{delegatedTraceID, "", "agent", authCtx.UserID},
		// The agent signed in as itself: nobody stands behind the call.
		{selfSignedTraceID, "", "agent", ""},
		// The person called the tool directly.
		{directTraceID, email, "user", ""},
		{otherTraceID, "other@example.com", "user", ""},
	} {
		attrs := map[string]any{
			"gram.event.source":         "tool_call",
			"gram.toolset.slug":         "payments",
			"gen_ai.tool.name":          "charge",
			"http.response.status_code": 200,
		}
		if row.userEmail != "" {
			attrs["user.email"] = row.userEmail
		}
		if row.actorType == "agent" {
			attrs["gram.authorization.actor.type"] = "agent"
			attrs["gram.authorization.actor.id"] = agentID
		}
		if row.authorizer != "" {
			attrs["gram.authorization.authorizer_user_id"] = row.authorizer
		}
		encoded, err := json.Marshal(attrs)
		require.NoError(t, err)
		err = ti.chClient.InsertTelemetryLogsSync(ctx, []telemetryRepo.InsertTelemetryLogParams{{
			ID: uuid.NewString(), GramProjectID: ti.projectID,
			TimeUnixNano: now.UnixNano(), ObservedTimeUnixNano: now.UnixNano(),
			TraceID: &row.traceID, GramURN: "tools:http:gram:charge",
			Body: "hosted tool event", Attributes: string(encoded), ResourceAttributes: "{}",
		}})
		require.NoError(t, err)
	}

	from := now.Add(-time.Hour).Format(time.RFC3339)
	to := now.Add(time.Hour).Format(time.RFC3339)
	byEmail := []*gen.ToolUsageUserFilter{{Kind: "email", Key: email}}
	rawQuery := ":"

	traceIDs := func(t *testing.T, payload *gen.ListToolUsageTracesPayload) map[string]*gen.ToolUsageTraceSummary {
		t.Helper()
		res, err := ti.service.ListToolUsageTraces(ctx, payload)
		require.NoError(t, err)
		out := make(map[string]*gen.ToolUsageTraceSummary, len(res.Traces))
		for _, trace := range res.Traces {
			require.NotNil(t, trace.TraceID)
			out[*trace.TraceID] = trace
		}
		return out
	}

	tests := []struct {
		name      string
		users     []*gen.ToolUsageUserFilter
		agents    []string
		wantTrace []string
	}{
		{"it matches a person's direct calls and the calls made on their behalf", byEmail, nil, []string{directTraceID, delegatedTraceID}},
		{"it narrows a person to one agent's calls on their behalf", byEmail, []string{agentID}, []string{delegatedTraceID}},
		{"it matches every call an agent made when no person is picked", nil, []string{agentID}, []string{delegatedTraceID, selfSignedTraceID}},
	}
	for _, tt := range tests {
		for _, query := range []*string{nil, &rawQuery} {
			path := "summary"
			if query != nil {
				path = "raw"
			}
			t.Run(tt.name+" ("+path+" path)", func(t *testing.T) {
				t.Parallel()
				got := traceIDs(t, &gen.ListToolUsageTracesPayload{
					From: from, To: to, Limit: 10,
					UserFilters: tt.users, AgentIds: tt.agents, Query: query,
				})
				require.ElementsMatch(t, tt.wantTrace, keys(got))
			})
		}
	}

	t.Run("it names the authorizer on the delegated row", func(t *testing.T) {
		t.Parallel()
		got := traceIDs(t, &gen.ListToolUsageTracesPayload{From: from, To: to, Limit: 10})
		delegated := got[delegatedTraceID]
		require.NotNil(t, delegated)
		require.Equal(t, gen.ToolUsageUserKind("agent_id"), delegated.UserKind)
		require.NotNil(t, delegated.AuthorizerUserID)
		require.Equal(t, authCtx.UserID, *delegated.AuthorizerUserID)
		require.NotNil(t, delegated.AuthorizerLabel)
		require.Equal(t, email, *delegated.AuthorizerLabel)
		require.Nil(t, got[selfSignedTraceID].AuthorizerUserID)
		require.Nil(t, got[directTraceID].AuthorizerUserID)
	})

	t.Run("it counts the delegated calls in the totals", func(t *testing.T) {
		t.Parallel()
		res, err := ti.service.GetToolUsageTotals(ctx, &gen.GetToolUsageTotalsPayload{
			From: from, To: to, UserFilters: byEmail,
		})
		require.NoError(t, err)
		require.EqualValues(t, 2, res.Totals.EventCount)

		res, err = ti.service.GetToolUsageTotals(ctx, &gen.GetToolUsageTotalsPayload{
			From: from, To: to, UserFilters: byEmail, AgentIds: []string{agentID},
		})
		require.NoError(t, err)
		require.EqualValues(t, 1, res.Totals.EventCount)
	})
}

func keys[V any](m map[string]V) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	return out
}
