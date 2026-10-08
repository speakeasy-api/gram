package analytics

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	gen "github.com/speakeasy-api/gram/server/gen/analytics"
	"github.com/speakeasy-api/gram/server/internal/oops"
	"github.com/speakeasy-api/gram/server/internal/otel/chrepo"
)

func TestDescribe(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestService(t)

	result, err := ti.service.Describe(ctx, &gen.DescribePayload{SessionToken: nil, ProjectSlugInput: nil})
	require.NoError(t, err)
	require.Len(t, result.Datasets, 3)

	sessions := datasetNamed(t, result.Datasets, "sessions")
	require.Equal(t, "event", sessions.Kind)
	require.NotEmpty(t, sessions.Grain)

	byName := fieldsByName(sessions)
	require.Equal(t, "dimension", byName["user"].Role)
	require.Equal(t, []string{"equals", "in"}, byName["user"].Operators)
	require.Equal(t, []string{"count_distinct"}, byName["user"].Aggregations, "describe advertises the one aggregation a dimension admits")
	require.Empty(t, byName["model"].Aggregations)
	require.Equal(t, "measure", byName["duration_seconds"].Role)
	require.Equal(t, "float64", byName["duration_seconds"].Type)
	require.NotNil(t, byName["duration_seconds"].Unit)
	require.Equal(t, "s", *byName["duration_seconds"].Unit)
	require.Equal(t, []string{"sum", "avg", "p95"}, byName["duration_seconds"].Aggregations)
	require.NotContains(t, byName, "project")
	require.True(t, byName["user"].Default, "the catalog names the opening group-by, not the client")
	require.False(t, byName["session"].Default)
	require.False(t, byName["duration_seconds"].Default)

	toolCalls := datasetNamed(t, result.Datasets, "tool_calls")
	require.Equal(t, []string{"tool_name"}, defaultFields(toolCalls), "one flagged dimension opens the tool calls view")
	toolFields := fieldsByName(toolCalls)
	require.NotContains(t, toolFields, "skill", "a Skill call is an ordinary call here; skills are their own dataset")
	require.Nil(t, toolFields["tool_name"].Description, "a field with nothing to add has no description")
	require.Nil(t, byName["user"].Description)

	skills := datasetNamed(t, result.Datasets, "skills")
	require.Equal(t, "skill invocation", skills.Grain)
	require.Contains(t, skills.Description, "Claude Code", "describe carries the caveat on which producers report skills")
	require.Equal(t, []string{"skill"}, defaultFields(skills), "the skills view opens broken down by skill")
	skillFields := fieldsByName(skills)
	require.Equal(t, []string{"count_distinct"}, skillFields["skill"].Aggregations, "skills used is a distinct count")
	require.Equal(t, "dimension", skillFields["tool_call"].Role, "the invocation's call id tells repeated invocations apart in rows mode")
	require.Equal(t, []string{"count_distinct"}, skillFields["user"].Aggregations)

	t.Run("it requires an authenticated project", func(t *testing.T) {
		t.Parallel()
		_, err := ti.service.Describe(t.Context(), &gen.DescribePayload{SessionToken: nil, ProjectSlugInput: nil})
		requireOopsCode(t, err, oops.CodeUnauthorized)
	})
}

// TestDescribeDatasetsCarriesFieldDescriptions: a field's description
// reaches describe when the catalog gives one, and is absent otherwise.
func TestDescribeDatasetsCarriesFieldDescriptions(t *testing.T) {
	t.Parallel()

	described := describeDatasets(MustCatalog(&Dataset{
		Name:        "things",
		Kind:        KindEvent,
		Grain:       "thing",
		Description: "things",
		TimeExpr:    "started_at",
		Fields: []Field{
			{Name: "kind", Type: TypeString, Role: RoleDimension, Default: true, Unit: "", Operators: equalsIn, Aggregations: nil, Expr: "kind", Description: "Which producers fill it."},
			{Name: "thing", Type: TypeString, Role: RoleDimension, Default: false, Unit: "", Operators: equalsIn, Aggregations: nil, Expr: "thing_id", Description: ""},
		},
		Source: sessionsSource,
	}))
	require.Len(t, described, 1)
	require.NotNil(t, described[0].Fields[0].Description)
	require.Equal(t, "Which producers fill it.", *described[0].Fields[0].Description)
	require.Nil(t, described[0].Fields[1].Description, "a field with nothing to add has no description")
}

func TestQuery(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestService(t)

	base := time.Date(2026, 9, 2, 10, 0, 0, 0, time.UTC)
	row := func(recordID, sessionID, turnID, eventID, eventType, user string, at time.Time) chrepo.AgentEventRow {
		r := agentEventFixture(ti.organizationID, recordID, sessionID, turnID, eventID, eventType, at.UnixNano())
		r.ProjectID = ti.projectID
		r.UserEmail = user
		if eventType == "tool_call" || eventType == "tool_call_result" {
			r.ToolName = "Bash"
		}
		return r
	}
	require.NoError(t, chrepo.New(ti.ch).InsertAgentEvents(ctx, []chrepo.AgentEventRow{
		row("r1", "s1", "t1", "r1", "api_request", "ann@example.com", base),
		row("r1", "s1", "t1", "r1", "api_request", "ann@example.com", base), // redelivered
		row("r2", "s1", "t1", "tc1", "tool_call", "ann@example.com", base.Add(time.Second)),
		row("r3", "s1", "t1", "tc1", "tool_call_result", "ann@example.com", base.Add(2*time.Second)),
		row("r4", "s1", "t2", "r4", "api_request", "ann@example.com", base.Add(3*time.Second)),
		row("r5", "s2", "t1", "r5", "api_request", "bob@example.com", base.Add(24*time.Hour)),
		row("r6", "s3", "t1", "tc2", "tool_call", "bob@example.com", base.Add(25*time.Hour+250*time.Millisecond)),
	}))

	from, to := base.Add(-time.Hour).Format(time.RFC3339), base.Add(48*time.Hour).Format(time.RFC3339)
	str := func(s string) *string { return &s }

	t.Run("it aggregates sessions by user with identity-aware measures", func(t *testing.T) {
		t.Parallel()
		result, err := ti.service.Query(ctx, &gen.QueryPayload{
			SessionToken: nil, ProjectSlugInput: nil,
			Dataset: "sessions", From: from, To: to, Grain: nil,
			Dimensions: []string{"user"},
			Measures: []*gen.AnalyticsMeasure{
				{Op: "count", Field: nil, Alias: nil},
				{Op: "sum", Field: str("tool_call_count"), Alias: str("tool_calls")},
				{Op: "sum", Field: str("turn_count"), Alias: nil},
			},
			Filters: nil, OrderBy: []*gen.AnalyticsOrderBy{{Measure: "count", Direction: "desc"}}, Limit: 0, Ungrouped: false,
		})
		require.NoError(t, err)
		require.Equal(t, "sessions", result.Dataset)
		require.Equal(t, "sessions.agent_events", result.Plan)
		require.Len(t, result.Rows, 2)

		bob := result.Rows[0]
		require.Equal(t, "bob@example.com", bob["user"])
		require.EqualValues(t, 2, bob["count"], "s2 and s3")
		require.EqualValues(t, 1, bob["tool_calls"])
		require.EqualValues(t, 2, bob["sum_turn_count"])

		ann := result.Rows[1]
		require.Equal(t, "ann@example.com", ann["user"])
		require.EqualValues(t, 1, ann["count"])
		require.EqualValues(t, 1, ann["tool_calls"], "two observations of tc1 are one call")
		require.EqualValues(t, 2, ann["sum_turn_count"], "the redelivered record does not add a turn")
	})

	t.Run("it counts distinct values of a dimension", func(t *testing.T) {
		t.Parallel()
		result, err := ti.service.Query(ctx, &gen.QueryPayload{
			SessionToken: nil, ProjectSlugInput: nil,
			Dataset: "tool_calls", From: from, To: to, Grain: nil,
			Dimensions: nil,
			Measures: []*gen.AnalyticsMeasure{
				{Op: "count", Field: nil, Alias: nil},
				{Op: "count_distinct", Field: str("tool_name"), Alias: str("tools_used")},
				{Op: "count_distinct", Field: str("user"), Alias: str("people")},
			},
			Filters: nil, OrderBy: nil, Limit: 0, Ungrouped: false,
		})
		require.NoError(t, err)
		require.Len(t, result.Rows, 1)
		require.EqualValues(t, 2, result.Rows[0]["count"], "tc1 and tc2")
		require.EqualValues(t, 1, result.Rows[0]["tools_used"], "both calls are Bash")
		require.EqualValues(t, 2, result.Rows[0]["people"])
	})

	t.Run("it buckets by day and formats the bucket as RFC 3339", func(t *testing.T) {
		t.Parallel()
		result, err := ti.service.Query(ctx, &gen.QueryPayload{
			SessionToken: nil, ProjectSlugInput: nil,
			Dataset: "sessions", From: from, To: to, Grain: str("day"),
			Dimensions: nil,
			Measures:   []*gen.AnalyticsMeasure{{Op: "count", Field: nil, Alias: nil}},
			Filters:    nil, OrderBy: nil, Limit: 0, Ungrouped: false,
		})
		require.NoError(t, err)
		require.Len(t, result.Rows, 2)
		require.Equal(t, "2026-09-02T00:00:00Z", result.Rows[0]["time_bucket"])
		require.EqualValues(t, 1, result.Rows[0]["count"])
		require.Equal(t, "2026-09-03T00:00:00Z", result.Rows[1]["time_bucket"])
		require.EqualValues(t, 2, result.Rows[1]["count"])
	})

	t.Run("it returns ungrouped rows newest first with a time column", func(t *testing.T) {
		t.Parallel()
		result, err := ti.service.Query(ctx, &gen.QueryPayload{
			SessionToken: nil, ProjectSlugInput: nil,
			Dataset: "tool_calls", From: from, To: to, Grain: nil,
			Dimensions: []string{"tool_call", "session"},
			Measures:   nil,
			Filters:    []*gen.AnalyticsFilter{{Field: "user", Operator: "in", Values: []string{"ann@example.com", "bob@example.com"}}},
			OrderBy:    nil, Limit: 0, Ungrouped: true,
		})
		require.NoError(t, err)
		require.Len(t, result.Rows, 2)
		require.Equal(t, "tc2", result.Rows[0]["tool_call"])
		require.Equal(t, "s3", result.Rows[0]["session"])
		require.Equal(t, base.Add(25*time.Hour+250*time.Millisecond).Format(time.RFC3339Nano), result.Rows[0]["time"], "the event time keeps its sub-second precision")
		require.Equal(t, "tc1", result.Rows[1]["tool_call"])
		require.Len(t, result.Rows[1], 3, "row keys are the time plus the projected dimensions")
	})

	t.Run("it names what is wrong with a request", func(t *testing.T) {
		t.Parallel()
		_, err := ti.service.Query(ctx, &gen.QueryPayload{
			SessionToken: nil, ProjectSlugInput: nil,
			Dataset: "sessions", From: from, To: to, Grain: nil,
			Dimensions: []string{"department"},
			Measures:   []*gen.AnalyticsMeasure{{Op: "count", Field: nil, Alias: nil}},
			Filters:    nil, OrderBy: nil, Limit: 0, Ungrouped: false,
		})
		requireOopsCode(t, err, oops.CodeBadRequest)
		require.ErrorContains(t, err, "unknown_field")
		require.ErrorContains(t, err, "dimensions[0]")
	})

	t.Run("it rejects a malformed time", func(t *testing.T) {
		t.Parallel()
		_, err := ti.service.Query(ctx, &gen.QueryPayload{
			SessionToken: nil, ProjectSlugInput: nil,
			Dataset: "sessions", From: "yesterday", To: to, Grain: nil,
			Dimensions: nil, Measures: []*gen.AnalyticsMeasure{{Op: "count", Field: nil, Alias: nil}},
			Filters: nil, OrderBy: nil, Limit: 0, Ungrouped: false,
		})
		requireOopsCode(t, err, oops.CodeBadRequest)
		require.ErrorContains(t, err, "invalid_time_range")
	})

	t.Run("it rejects a bound that nanoseconds cannot represent", func(t *testing.T) {
		t.Parallel()
		_, err := ti.service.Query(ctx, &gen.QueryPayload{
			SessionToken: nil, ProjectSlugInput: nil,
			Dataset: "sessions", From: "1500-01-01T00:00:00Z", To: to, Grain: nil,
			Dimensions: nil, Measures: []*gen.AnalyticsMeasure{{Op: "count", Field: nil, Alias: nil}},
			Filters: nil, OrderBy: nil, Limit: 0, Ungrouped: false,
		})
		requireOopsCode(t, err, oops.CodeBadRequest)
		require.ErrorContains(t, err, "invalid_time_range: from")
	})

	t.Run("it requires an authenticated project", func(t *testing.T) {
		t.Parallel()
		_, err := ti.service.Query(t.Context(), &gen.QueryPayload{
			SessionToken: nil, ProjectSlugInput: nil,
			Dataset: "sessions", From: from, To: to, Grain: nil,
			Dimensions: nil, Measures: []*gen.AnalyticsMeasure{{Op: "count", Field: nil, Alias: nil}},
			Filters: nil, OrderBy: nil, Limit: 0, Ungrouped: false,
		})
		requireOopsCode(t, err, oops.CodeUnauthorized)
	})
}

// datasetNamed finds a described dataset by name, so the assertions do not
// depend on the catalog's declaration order.
func datasetNamed(t *testing.T, datasets []*gen.AnalyticsDataset, name string) *gen.AnalyticsDataset {
	t.Helper()
	for _, ds := range datasets {
		if ds.Name == name {
			return ds
		}
	}
	require.Failf(t, "dataset not described", "no dataset named %q", name)
	return nil
}

// fieldsByName indexes a described dataset's fields by name.
func fieldsByName(ds *gen.AnalyticsDataset) map[string]*gen.AnalyticsField {
	byName := make(map[string]*gen.AnalyticsField, len(ds.Fields))
	for _, f := range ds.Fields {
		byName[f.Name] = f
	}
	return byName
}

// defaultFields lists the names describe flags as default, in declaration
// order.
func defaultFields(ds *gen.AnalyticsDataset) []string {
	var names []string
	for _, f := range ds.Fields {
		if f.Default {
			names = append(names, f.Name)
		}
	}
	return names
}
