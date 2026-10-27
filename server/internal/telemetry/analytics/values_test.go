package analytics

import (
	"math"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	gen "github.com/speakeasy-api/gram/server/gen/analytics"
	hooksRepo "github.com/speakeasy-api/gram/server/internal/hooks/repo"
	"github.com/speakeasy-api/gram/server/internal/oops"
	"github.com/speakeasy-api/gram/server/internal/otel/chrepo"
)

func TestCompileValues(t *testing.T) {
	t.Parallel()

	plan, err := CompileValues(Default, Tenant{OrganizationID: "org-1", ProjectID: "project-1"}, nil, ValuesRequest{Dataset: "tool_calls", Dimension: "tool_name", FromUnixNano: testFrom, ToUnixNano: testTo, Limit: 0})
	require.NoError(t, err)
	require.Contains(t, plan.SQL, "SELECT tool_name AS value, uniqExact(event_id) AS n FROM (SELECT * FROM agent_events WHERE")
	require.NotContains(t, plan.SQL, "argMaxIf", "values come from the observations, not the folded rows")
	require.Contains(t, plan.SQL, "WHERE tool_name <> ?")
	require.Contains(t, plan.SQL, "GROUP BY value ORDER BY n DESC, value ASC LIMIT 50")

	cases := []struct {
		name string
		req  ValuesRequest
		code ErrorCode
	}{
		{name: "unknown dataset", req: ValuesRequest{Dataset: "x", Dimension: "user", FromUnixNano: testFrom, ToUnixNano: testTo}, code: ErrUnknownDataset},
		{name: "measure is not a dimension", req: ValuesRequest{Dataset: "tool_calls", Dimension: "duration_ms", FromUnixNano: testFrom, ToUnixNano: testTo}, code: ErrUnknownField},
		{name: "limit above the maximum", req: ValuesRequest{Dataset: "tool_calls", Dimension: "user", FromUnixNano: testFrom, ToUnixNano: testTo, Limit: MaxValuesLimit + 1}, code: ErrLimitExceeded},
		{name: "negative limit", req: ValuesRequest{Dataset: "tool_calls", Dimension: "user", FromUnixNano: testFrom, ToUnixNano: testTo, Limit: -1}, code: ErrLimitExceeded},
		{name: "empty window", req: ValuesRequest{Dataset: "tool_calls", Dimension: "user", FromUnixNano: testTo, ToUnixNano: testFrom}, code: ErrInvalidTimeRange},
		{name: "window beyond the dataset's retention", req: ValuesRequest{Dataset: "tool_calls", Dimension: "user", FromUnixNano: testFrom, ToUnixNano: testFrom + ToolCalls.MaxTimeRangeNanos() + 1}, code: ErrInvalidTimeRange},
		{name: "window whose signed span wraps", req: ValuesRequest{Dataset: "tool_calls", Dimension: "user", FromUnixNano: math.MinInt64, ToUnixNano: math.MaxInt64}, code: ErrInvalidTimeRange},
	}
	for _, tc := range cases {
		t.Run("it rejects "+tc.name, func(t *testing.T) {
			t.Parallel()
			_, err := CompileValues(Default, Tenant{OrganizationID: "org-1", ProjectID: "project-1"}, nil, tc.req)
			var invalid *Error
			require.ErrorAs(t, err, &invalid)
			require.Equal(t, tc.code, invalid.Code)
		})
	}
}

func TestDimensionValues(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestService(t)

	base := time.Date(2026, 9, 2, 10, 0, 0, 0, time.UTC)
	row := func(recordID, eventID, user string, at time.Time) chrepo.AgentEventRow {
		r := agentEventFixture(ti.organizationID, recordID, "s1", "t1", eventID, "tool_call_result", at.UnixNano())
		r.ProjectID = ti.projectID
		r.ToolName = "Bash"
		r.UserEmail = user
		return r
	}
	require.NoError(t, chrepo.New(ti.ch).InsertAgentEvents(ctx, []chrepo.AgentEventRow{
		row("v1", "c1", "ada@example.com", base),
		// A later observation of c1 changes its user; both stay listed.
		row("v2", "c1", "grace@example.com", base.Add(time.Minute)),
		row("v3", "c2", "grace@example.com", base.Add(2*time.Minute)),
		row("v4", "c3", "linus@example.com", base.Add(3*time.Minute)),
		row("v5", "c4", "", base.Add(4*time.Minute)),
	}))

	from, to := base.Add(-time.Hour).Format(time.RFC3339), base.Add(time.Hour).Format(time.RFC3339)

	result, err := ti.service.DimensionValues(ctx, &gen.DimensionValuesPayload{
		Dataset: "tool_calls", Dimension: "user", From: from, To: to, Limit: 0, SessionToken: nil, ProjectSlugInput: nil,
	})
	require.NoError(t, err)
	require.Equal(t, "tool_calls", result.Dataset)
	require.Equal(t, "user", result.Dimension)
	require.Equal(t, []*gen.AnalyticsDimensionValue{
		{Value: "grace@example.com", Count: 2},
		{Value: "ada@example.com", Count: 1},
		{Value: "linus@example.com", Count: 1},
	}, result.Values, "every user a call stated, counted in calls, and never the empty one")

	t.Run("it offers the skills a window saw", func(t *testing.T) {
		t.Parallel()
		call := func(recordID, tool, skill string, at time.Time) chrepo.AgentEventRow {
			r := agentEventFixture(ti.organizationID, recordID, "", "", recordID, "tool_call_result", at.UnixNano())
			r.ProjectID = ti.projectID
			r.ToolName = tool
			r.SkillName = skill
			return r
		}
		require.NoError(t, chrepo.New(ti.ch).InsertAgentEvents(ctx, []chrepo.AgentEventRow{
			call("k1", "Skill", "deploy", base.Add(5*time.Minute)),
			call("k2", "Skill", "deploy", base.Add(6*time.Minute)),
			call("k3", "Skill", "review", base.Add(7*time.Minute)),
			call("k4", "Bash", "", base.Add(8*time.Minute)),
		}))

		result, err := ti.service.DimensionValues(ctx, &gen.DimensionValuesPayload{
			Dataset: "skills", Dimension: "skill", From: from, To: to, Limit: 0, SessionToken: nil, ProjectSlugInput: nil,
		})
		require.NoError(t, err)
		require.Len(t, result.Values, 2, "a call that named no skill is no invocation, so the empty value is never offered")
		require.Equal(t, "deploy", result.Values[0].Value)
		require.EqualValues(t, 2, result.Values[0].Count)
		require.Equal(t, "review", result.Values[1].Value)
		require.EqualValues(t, 1, result.Values[1].Count)

		calls, err := ti.service.DimensionValues(ctx, &gen.DimensionValuesPayload{
			Dataset: "skills", Dimension: "tool_call", From: from, To: to, Limit: 0, SessionToken: nil, ProjectSlugInput: nil,
		})
		require.NoError(t, err)
		ids := make([]string, 0, len(calls.Values))
		for _, v := range calls.Values {
			ids = append(ids, v.Value)
		}
		require.ElementsMatch(t, []string{"k1", "k2", "k3"}, ids, "only calls that named a skill are invocations")
	})

	t.Run("it offers MCP servers under the project's display names", func(t *testing.T) {
		t.Parallel()
		call := func(recordID, server string, at time.Time) chrepo.AgentEventRow {
			r := agentEventFixture(ti.organizationID, recordID, "", "", recordID, "tool_call_result", at.UnixNano())
			r.ProjectID = ti.projectID
			r.ToolName = "mcp_tool"
			r.MCPServerName = server
			return r
		}
		require.NoError(t, chrepo.New(ti.ch).InsertAgentEvents(ctx, []chrepo.AgentEventRow{
			call("m1", "github-mcp", base.Add(10*time.Minute)),
			call("m2", "gh", base.Add(11*time.Minute)),
			call("m3", "linear", base.Add(12*time.Minute)),
		}))
		projectID, err := uuid.Parse(ti.projectID)
		require.NoError(t, err)
		for raw, display := range map[string]string{"github-mcp": "GitHub", "gh": "GitHub"} {
			_, err := hooksRepo.New(ti.db).UpsertHooksServerNameOverride(ctx, hooksRepo.UpsertHooksServerNameOverrideParams{ProjectID: projectID, RawServerName: raw, DisplayName: display})
			require.NoError(t, err)
		}

		result, err := ti.service.DimensionValues(ctx, &gen.DimensionValuesPayload{
			Dataset: "tool_calls", Dimension: "mcp_server", From: from, To: to, Limit: 0, SessionToken: nil, ProjectSlugInput: nil,
		})
		require.NoError(t, err)
		require.Len(t, result.Values, 2, "two raw names with one display name are one server")
		require.Equal(t, "GitHub", result.Values[0].Value)
		require.EqualValues(t, 2, result.Values[0].Count)
		require.Equal(t, "linear", result.Values[1].Value, "a raw name with no override shows as reported")
		require.EqualValues(t, 1, result.Values[1].Count)
	})

	t.Run("it counts a redelivered call once", func(t *testing.T) {
		t.Parallel()
		call := func(recordID, eventID, eventType string, at time.Time) chrepo.AgentEventRow {
			r := agentEventFixture(ti.organizationID, recordID, "", "", eventID, eventType, at.UnixNano())
			r.ProjectID = ti.projectID
			r.ToolName = "redelivered_tool"
			return r
		}
		redelivered := call("d1", "call-d", "tool_call_result", base.Add(20*time.Minute))
		require.NoError(t, chrepo.New(ti.ch).InsertAgentEvents(ctx, []chrepo.AgentEventRow{
			call("d0", "call-d", "tool_call", base.Add(19*time.Minute)),
			redelivered,
			redelivered,
		}))

		result, err := ti.service.DimensionValues(ctx, &gen.DimensionValuesPayload{
			Dataset: "tool_calls", Dimension: "tool_name", From: from, To: to, Limit: 0, SessionToken: nil, ProjectSlugInput: nil,
		})
		require.NoError(t, err)
		var count int64
		for _, v := range result.Values {
			if v.Value == "redelivered_tool" {
				count = v.Count
			}
		}
		require.EqualValues(t, 1, count, "one call, observed twice and redelivered once, is one call")
	})

	t.Run("it names an unknown dimension", func(t *testing.T) {
		t.Parallel()
		_, err := ti.service.DimensionValues(ctx, &gen.DimensionValuesPayload{
			Dataset: "tool_calls", Dimension: "department", From: from, To: to, Limit: 0, SessionToken: nil, ProjectSlugInput: nil,
		})
		requireOopsCode(t, err, oops.CodeBadRequest)
		require.ErrorContains(t, err, "unknown_field")
	})

	t.Run("it requires an authenticated project", func(t *testing.T) {
		t.Parallel()
		_, err := ti.service.DimensionValues(t.Context(), &gen.DimensionValuesPayload{
			Dataset: "tool_calls", Dimension: "user", From: from, To: to, Limit: 0, SessionToken: nil, ProjectSlugInput: nil,
		})
		requireOopsCode(t, err, oops.CodeUnauthorized)
	})
}
