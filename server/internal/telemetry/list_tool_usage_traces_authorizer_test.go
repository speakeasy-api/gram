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

	// Two people linked a provider account under the same address, so the
	// address belongs to neither of them.
	sharerID, sharerEmail := seedConnectedOrgUser(t, ctx, ti, "sharer")
	coSharerID, _ := seedConnectedOrgUser(t, ctx, ti, "co-sharer")
	sharedEmail := "shared-" + uuid.NewString() + "@example.com"
	linkUserAccount(t, ctx, ti, sharerID, sharedEmail, "personal")
	linkUserAccount(t, ctx, ti, coSharerID, sharedEmail, "personal")
	sharerAgentID := uuid.NewString()
	sharerTraceID := strings.ReplaceAll(uuid.NewString(), "-", "")

	for _, row := range []struct {
		traceID    string
		userEmail  string
		actorType  string
		agentID    string
		authorizer string
	}{
		// The agent acted for the signed-in person.
		{delegatedTraceID, "", "agent", agentID, authCtx.UserID},
		// The agent signed in as itself: nobody stands behind the call.
		{selfSignedTraceID, "", "agent", agentID, ""},
		// The person called the tool directly.
		{directTraceID, email, "user", "", ""},
		{otherTraceID, "other@example.com", "user", "", ""},
		// Another agent acted for one of the people sharing an address.
		{sharerTraceID, "", "agent", sharerAgentID, sharerID},
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
			attrs["gram.authorization.actor.id"] = row.agentID
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

	// Every listed row names who it acted for, or nobody when no person
	// stands behind it.
	wantAuthorizer := map[string]struct{ userID, label string }{
		delegatedTraceID: {authCtx.UserID, email},
		sharerTraceID:    {sharerID, sharerEmail},
	}
	requireAuthorizers := func(t *testing.T, got map[string]*gen.ToolUsageTraceSummary) {
		t.Helper()
		for traceID, trace := range got {
			want, delegated := wantAuthorizer[traceID]
			if !delegated {
				require.Nil(t, trace.AuthorizerUserID, traceID)
				require.Nil(t, trace.AuthorizerLabel, traceID)
				continue
			}
			require.NotNil(t, trace.AuthorizerUserID, traceID)
			require.Equal(t, want.userID, *trace.AuthorizerUserID, traceID)
			require.NotNil(t, trace.AuthorizerLabel, traceID)
			require.Equal(t, want.label, *trace.AuthorizerLabel, traceID)
		}
	}

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
		{"it matches the calls made on behalf of a person picked by their own email", []*gen.ToolUsageUserFilter{{Kind: "email", Key: sharerEmail}}, nil, []string{sharerTraceID}},
		{"it matches the calls made on behalf of a person picked by user id", []*gen.ToolUsageUserFilter{{Kind: "user_id", Key: sharerID}}, nil, []string{sharerTraceID}},
		{"it does not match calls made on behalf of anyone sharing an ambiguous email", []*gen.ToolUsageUserFilter{{Kind: "email", Key: sharedEmail}}, nil, nil},
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
				requireAuthorizers(t, got)
			})
		}
	}

	t.Run("it names the authorizer on the delegated row", func(t *testing.T) {
		t.Parallel()
		got := traceIDs(t, &gen.ListToolUsageTracesPayload{From: from, To: to, Limit: 10})
		require.Len(t, got, 5)
		require.Equal(t, gen.ToolUsageUserKind("agent_id"), got[delegatedTraceID].UserKind)
		requireAuthorizers(t, got)
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
