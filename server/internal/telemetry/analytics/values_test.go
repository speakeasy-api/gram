package analytics

import (
	"math"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	gen "github.com/speakeasy-api/gram/server/gen/analytics"
	"github.com/speakeasy-api/gram/server/internal/oops"
	"github.com/speakeasy-api/gram/server/internal/otel/chrepo"
)

func TestCompileValues(t *testing.T) {
	t.Parallel()

	plan, err := CompileValues(Default, Tenant{OrganizationID: "org-1", ProjectID: "project-1"}, ValuesRequest{Dataset: "tool_calls", Dimension: "tool_name", FromUnixNano: testFrom, ToUnixNano: testTo, Limit: 0})
	require.NoError(t, err)
	require.Contains(t, plan.SQL, "SELECT tool_name AS value, count() AS n FROM (")
	require.Contains(t, plan.SQL, "LIMIT 1 BY organization_id, project_id, record_id", "values come from the collapsed rows")
	require.Contains(t, plan.SQL, "WHERE tool_name <> ?")
	require.Contains(t, plan.SQL, "GROUP BY value ORDER BY n DESC, value ASC LIMIT 50")

	cases := []struct {
		name string
		req  ValuesRequest
		code ErrorCode
	}{
		{name: "unknown dataset", req: ValuesRequest{Dataset: "x", Dimension: "user", FromUnixNano: testFrom, ToUnixNano: testTo}, code: ErrUnknownDataset},
		{name: "measure is not a dimension", req: ValuesRequest{Dataset: "sessions", Dimension: "turn_count", FromUnixNano: testFrom, ToUnixNano: testTo}, code: ErrUnknownField},
		{name: "limit above the maximum", req: ValuesRequest{Dataset: "sessions", Dimension: "user", FromUnixNano: testFrom, ToUnixNano: testTo, Limit: MaxValuesLimit + 1}, code: ErrLimitExceeded},
		{name: "negative limit", req: ValuesRequest{Dataset: "sessions", Dimension: "user", FromUnixNano: testFrom, ToUnixNano: testTo, Limit: -1}, code: ErrLimitExceeded},
		{name: "empty window", req: ValuesRequest{Dataset: "sessions", Dimension: "user", FromUnixNano: testTo, ToUnixNano: testFrom}, code: ErrInvalidTimeRange},
		{name: "window beyond the dataset's retention", req: ValuesRequest{Dataset: "sessions", Dimension: "user", FromUnixNano: testFrom, ToUnixNano: testFrom + Sessions.MaxTimeRangeNanos() + 1}, code: ErrInvalidTimeRange},
		{name: "window whose signed span wraps", req: ValuesRequest{Dataset: "sessions", Dimension: "user", FromUnixNano: math.MinInt64, ToUnixNano: math.MaxInt64}, code: ErrInvalidTimeRange},
	}
	for _, tc := range cases {
		t.Run("it rejects "+tc.name, func(t *testing.T) {
			t.Parallel()
			_, err := CompileValues(Default, Tenant{OrganizationID: "org-1", ProjectID: "project-1"}, tc.req)
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
	row := func(recordID, sessionID, model string, at time.Time) chrepo.AgentEventRow {
		r := agentEventFixture(ti.organizationID, recordID, sessionID, "t1", recordID, "api_request", at.UnixNano())
		r.ProjectID = ti.projectID
		r.Model = model
		return r
	}
	require.NoError(t, chrepo.New(ti.ch).InsertAgentEvents(ctx, []chrepo.AgentEventRow{
		row("v1", "s1", "claude-sonnet-4", base),
		// A later observation of s1 settles the model on the collapsed row.
		row("v2", "s1", "claude-opus-4", base.Add(time.Minute)),
		row("v3", "s2", "claude-opus-4", base.Add(2*time.Minute)),
		row("v4", "s3", "gpt-5", base.Add(3*time.Minute)),
		row("v5", "s4", "", base.Add(4*time.Minute)),
	}))

	from, to := base.Add(-time.Hour).Format(time.RFC3339), base.Add(time.Hour).Format(time.RFC3339)

	result, err := ti.service.DimensionValues(ctx, &gen.DimensionValuesPayload{
		Dataset: "sessions", Dimension: "model", From: from, To: to, Limit: 0, SessionToken: nil, ProjectSlugInput: nil,
	})
	require.NoError(t, err)
	require.Equal(t, "sessions", result.Dataset)
	require.Equal(t, "model", result.Dimension)
	require.Len(t, result.Values, 2, "the empty model is never offered")
	require.Equal(t, "claude-opus-4", result.Values[0].Value)
	require.EqualValues(t, 2, result.Values[0].Count, "s1 settled on opus, s2 is opus")
	require.Equal(t, "gpt-5", result.Values[1].Value)
	require.EqualValues(t, 1, result.Values[1].Count)

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
	})

	t.Run("it names an unknown dimension", func(t *testing.T) {
		t.Parallel()
		_, err := ti.service.DimensionValues(ctx, &gen.DimensionValuesPayload{
			Dataset: "sessions", Dimension: "department", From: from, To: to, Limit: 0, SessionToken: nil, ProjectSlugInput: nil,
		})
		requireOopsCode(t, err, oops.CodeBadRequest)
		require.ErrorContains(t, err, "unknown_field")
	})

	t.Run("it requires an authenticated project", func(t *testing.T) {
		t.Parallel()
		_, err := ti.service.DimensionValues(t.Context(), &gen.DimensionValuesPayload{
			Dataset: "sessions", Dimension: "model", From: from, To: to, Limit: 0, SessionToken: nil, ProjectSlugInput: nil,
		})
		requireOopsCode(t, err, oops.CodeUnauthorized)
	})
}
