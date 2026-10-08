package analytics

import (
	"math"
	"testing"

	"github.com/stretchr/testify/require"
)

const (
	testFrom = int64(1_756_684_800_000_000_000) // 2025-09-01T00:00:00Z
	testTo   = int64(1_757_289_600_000_000_000) // 2025-09-08T00:00:00Z
)

func compileTest(t *testing.T, req Request) (*Plan, error) {
	t.Helper()
	if req.FromUnixNano == 0 {
		req.FromUnixNano = testFrom
	}
	if req.ToUnixNano == 0 {
		req.ToUnixNano = testTo
	}
	return Compile(Default, Tenant{OrganizationID: "org-1", ProjectID: "project-1"}, nil, req)
}

func TestCompileGrouped(t *testing.T) {
	t.Parallel()

	plan, err := compileTest(t, Request{
		Dataset:    "sessions",
		Grain:      TimeGrainDay,
		Dimensions: []string{"user", "model"},
		Measures: []Measure{
			{Op: "count", Field: "", Alias: ""},
			{Op: "sum", Field: "tool_call_count", Alias: "tool_calls"},
			{Op: "p95", Field: "duration_seconds", Alias: ""},
		},
		Filters:   []Filter{{Field: "surface", Operator: "in", Values: []string{"claude-code", "codex"}}},
		OrderBy:   []OrderBy{{Measure: "tool_calls", Direction: "desc"}},
		Limit:     0,
		Ungrouped: false,
	})
	require.NoError(t, err)

	require.Equal(t, "sessions", plan.Dataset)
	require.Equal(t, "sessions.agent_events", plan.Name)
	require.Equal(t, []Column{
		{Name: "time_bucket", Kind: ColumnTime},
		{Name: "user", Kind: ColumnDimension},
		{Name: "model", Kind: ColumnDimension},
		{Name: "count", Kind: ColumnMeasure},
		{Name: "tool_calls", Kind: ColumnMeasure},
		{Name: "p95_duration_seconds", Kind: ColumnMeasure},
	}, plan.Columns)

	require.Contains(t, plan.SQL, "toStartOfDay(fromUnixTimestamp64Nano(started_at, 'UTC')) AS time_bucket")
	require.Contains(t, plan.SQL, "user_email AS user")
	require.Contains(t, plan.SQL, "count() AS count")
	require.Contains(t, plan.SQL, "sum(tool_call_count) AS tool_calls")
	require.Contains(t, plan.SQL, "quantile(0.95)((ended_at - started_at) / 1e9) AS p95_duration_seconds")
	require.Contains(t, plan.SQL, "LIMIT 1 BY organization_id, project_id, record_id", "the source query de-duplicates before anything aggregates")
	require.Contains(t, plan.SQL, "WHERE surface IN (?,?)")
	require.Contains(t, plan.SQL, "GROUP BY time_bucket, user, model")
	require.Contains(t, plan.SQL, "ORDER BY tool_calls DESC, time_bucket ASC, user ASC, model ASC")
	require.Contains(t, plan.SQL, "LIMIT 100", "the default limit applies")
	require.Equal(t, []any{"org-1", "project-1", testFrom, testTo, "", "claude-code", "codex"}, plan.Args, "tenancy, window, the source query's own session_id != '' guard, then the filter values")
	require.NotContains(t, plan.SQL, "project-1", "tenancy is bound, never interpolated")
}

// TestCompileCountDistinct: count_distinct is an aggregation over a
// dimension's own values, exact, with the same alias and ordering rules as
// every other measure.
func TestCompileCountDistinct(t *testing.T) {
	t.Parallel()

	plan, err := compileTest(t, Request{
		Dataset:    "tool_calls",
		Grain:      "",
		Dimensions: []string{"mcp_server"},
		Measures: []Measure{
			{Op: "count", Field: "", Alias: ""},
			{Op: "count_distinct", Field: "tool_name", Alias: ""},
			{Op: "count_distinct", Field: "user", Alias: "people"},
		},
		Filters:   nil,
		OrderBy:   []OrderBy{{Measure: "people", Direction: "desc"}},
		Limit:     0,
		Ungrouped: false,
	})
	require.NoError(t, err)

	require.Equal(t, []Column{
		{Name: "mcp_server", Kind: ColumnDimension},
		{Name: "count", Kind: ColumnMeasure},
		{Name: "count_distinct_tool_name", Kind: ColumnMeasure},
		{Name: "people", Kind: ColumnMeasure},
	}, plan.Columns)
	require.Contains(t, plan.SQL, "uniqExact(nullIf(tool_name, '')) AS count_distinct_tool_name", "a collapsed row with no value is not a distinct value")
	require.Contains(t, plan.SQL, "uniqExact(nullIf(user_email, '')) AS people")
	require.Contains(t, plan.SQL, "ORDER BY people DESC, mcp_server ASC")
}

// TestCompileSkills: skills is its own dataset over the calls that named a
// skill, so skill is grouped, filtered and distinct-counted like any other
// dimension, over a source that keeps only those calls.
func TestCompileSkills(t *testing.T) {
	t.Parallel()

	plan, err := compileTest(t, Request{
		Dataset:    "skills",
		Grain:      "",
		Dimensions: []string{"skill"},
		Measures: []Measure{
			{Op: "count", Field: "", Alias: ""},
			{Op: "count_distinct", Field: "user", Alias: "people"},
			{Op: "count_distinct", Field: "skill", Alias: ""},
		},
		Filters:   []Filter{{Field: "skill", Operator: "in", Values: []string{"deploy", "review"}}},
		OrderBy:   nil,
		Limit:     0,
		Ungrouped: false,
	})
	require.NoError(t, err)

	require.Equal(t, []Column{
		{Name: "skill", Kind: ColumnDimension},
		{Name: "count", Kind: ColumnMeasure},
		{Name: "people", Kind: ColumnMeasure},
		{Name: "count_distinct_skill", Kind: ColumnMeasure},
	}, plan.Columns)
	require.Contains(t, plan.SQL, "skill_name AS skill")
	require.Contains(t, plan.SQL, "uniqExact(nullIf(skill_name, '')) AS count_distinct_skill", "skills used counts the invocations that named one")
	require.Contains(t, plan.SQL, "HAVING skill_name != ''", "the source keeps only the collapsed calls that named a skill")
	require.Contains(t, plan.SQL, "WHERE skill_name IN (?,?)")
	require.Contains(t, plan.SQL, "GROUP BY skill")
	require.Equal(t, []any{"deploy", "review"}, plan.Args[len(plan.Args)-2:], "the filter values are bound last")
}

func TestCompileUngrouped(t *testing.T) {
	t.Parallel()

	plan, err := compileTest(t, Request{
		Dataset:    "tool_calls",
		Grain:      "",
		Dimensions: []string{"tool_name", "status"},
		Measures:   nil,
		Filters:    []Filter{{Field: "status", Operator: "equals", Values: []string{"error"}}},
		OrderBy:    nil,
		Limit:      25,
		Ungrouped:  true,
	})
	require.NoError(t, err)

	require.Equal(t, []Column{
		{Name: "time", Kind: ColumnTime},
		{Name: "tool_name", Kind: ColumnDimension},
		{Name: "status", Kind: ColumnDimension},
	}, plan.Columns)
	require.Contains(t, plan.SQL, "fromUnixTimestamp64Nano(started_at, 'UTC') AS time")
	require.Contains(t, plan.SQL, "WHERE status = ?")
	require.Contains(t, plan.SQL, "ORDER BY started_at DESC LIMIT 25", "rows are newest first, always")
	require.NotContains(t, plan.SQL, "GROUP BY time")
}

func TestCompileRejects(t *testing.T) {
	t.Parallel()

	count := []Measure{{Op: "count", Field: "", Alias: ""}}
	cases := []struct {
		name  string
		req   Request
		code  ErrorCode
		field string
	}{
		{name: "unknown dataset", req: Request{Dataset: "departments", Measures: count}, code: ErrUnknownDataset, field: "dataset"},
		{name: "unknown dimension", req: Request{Dataset: "sessions", Dimensions: []string{"department"}, Measures: count}, code: ErrUnknownField, field: "dimensions[0]"},
		{name: "measure used as a dimension", req: Request{Dataset: "sessions", Dimensions: []string{"turn_count"}, Measures: count}, code: ErrUnknownField, field: "dimensions[0]"},
		{name: "project is not a field", req: Request{Dataset: "sessions", Dimensions: []string{"project"}, Measures: count}, code: ErrUnknownField, field: "dimensions[0]"},
		{name: "skill on tool_calls, where a Skill call is an ordinary call", req: Request{Dataset: "tool_calls", Dimensions: []string{"skill"}, Measures: count}, code: ErrUnknownField, field: "dimensions[0]"},
		{name: "repeated dimension", req: Request{Dataset: "sessions", Dimensions: []string{"user", "user"}, Measures: count}, code: ErrUnsatisfiable, field: "dimensions[1]"},
		{name: "too many dimensions", req: Request{Dataset: "sessions", Dimensions: []string{"user", "model", "surface", "provider"}, Measures: count}, code: ErrTooManyDimensions, field: "dimensions"},
		{name: "aggregation the field does not admit", req: Request{Dataset: "sessions", Measures: []Measure{{Op: "p95", Field: "turn_count", Alias: ""}}}, code: ErrUnsupportedAggregation, field: "measures[0].op"},
		{name: "unknown measure field", req: Request{Dataset: "sessions", Measures: []Measure{{Op: "sum", Field: "cost_usd", Alias: ""}}}, code: ErrUnknownField, field: "measures[0].field"},
		{name: "count with a field", req: Request{Dataset: "sessions", Measures: []Measure{{Op: "count", Field: "turn_count", Alias: ""}}}, code: ErrUnsatisfiable, field: "measures[0].field"},
		{name: "count_distinct without a field", req: Request{Dataset: "tool_calls", Measures: []Measure{{Op: "count_distinct", Field: "", Alias: ""}}}, code: ErrUnsatisfiable, field: "measures[0].field"},
		{name: "count_distinct over a measure", req: Request{Dataset: "tool_calls", Measures: []Measure{{Op: "count_distinct", Field: "duration_ms", Alias: ""}}}, code: ErrUnsupportedAggregation, field: "measures[0].op"},
		{name: "count_distinct over a dimension that does not declare it", req: Request{Dataset: "tool_calls", Measures: []Measure{{Op: "count_distinct", Field: "status", Alias: ""}}}, code: ErrUnsupportedAggregation, field: "measures[0].op"},
		{name: "count_distinct over an unknown field", req: Request{Dataset: "tool_calls", Measures: []Measure{{Op: "count_distinct", Field: "department", Alias: ""}}}, code: ErrUnknownField, field: "measures[0].field"},
		{name: "a measure's aggregation over a dimension", req: Request{Dataset: "tool_calls", Measures: []Measure{{Op: "sum", Field: "tool_name", Alias: ""}}}, code: ErrUnknownField, field: "measures[0].field"},
		{name: "alias that is a field name", req: Request{Dataset: "sessions", Measures: []Measure{{Op: "count", Field: "", Alias: "user"}}}, code: ErrUnsatisfiable, field: "measures[0].alias"},
		{name: "alias that is not an identifier", req: Request{Dataset: "sessions", Measures: []Measure{{Op: "count", Field: "", Alias: "x; DROP TABLE"}}}, code: ErrUnsatisfiable, field: "measures[0].alias"},
		{name: "duplicate alias", req: Request{Dataset: "sessions", Measures: []Measure{{Op: "count", Field: "", Alias: "n"}, {Op: "sum", Field: "turn_count", Alias: "n"}}}, code: ErrUnsatisfiable, field: "measures[1].alias"},
		{name: "grouped without measures", req: Request{Dataset: "sessions"}, code: ErrUnsatisfiable, field: "measures"},
		{name: "operator the dimension does not admit", req: Request{Dataset: "sessions", Measures: count, Filters: []Filter{{Field: "user", Operator: "contains", Values: []string{"a"}}}}, code: ErrUnsupportedOperator, field: "filters[0].operator"},
		{name: "an aggregation offered as a filter operator", req: Request{Dataset: "sessions", Measures: count, Filters: []Filter{{Field: "user", Operator: "count_distinct", Values: []string{"a"}}}}, code: ErrUnsupportedOperator, field: "filters[0].operator"},
		{name: "filter on a measure", req: Request{Dataset: "sessions", Measures: count, Filters: []Filter{{Field: "turn_count", Operator: "equals", Values: []string{"1"}}}}, code: ErrUnknownField, field: "filters[0].field"},
		{name: "equals with two values", req: Request{Dataset: "sessions", Measures: count, Filters: []Filter{{Field: "user", Operator: "equals", Values: []string{"a", "b"}}}}, code: ErrUnsatisfiable, field: "filters[0].values"},
		{name: "too many filter values", req: Request{Dataset: "sessions", Measures: count, Filters: []Filter{{Field: "user", Operator: "in", Values: make([]string, MaxFilterValues+1)}}}, code: ErrLimitExceeded, field: "filters[0].values"},
		{name: "order by a measure not requested", req: Request{Dataset: "sessions", Measures: count, OrderBy: []OrderBy{{Measure: "tool_calls", Direction: "desc"}}}, code: ErrUnknownField, field: "order_by[0].measure"},
		{name: "order by on ungrouped rows", req: Request{Dataset: "sessions", Ungrouped: true, OrderBy: []OrderBy{{Measure: "count", Direction: "desc"}}}, code: ErrUnsatisfiable, field: "order_by"},
		{name: "measures on ungrouped rows", req: Request{Dataset: "sessions", Ungrouped: true, Measures: count}, code: ErrUnsatisfiable, field: "measures"},
		{name: "grain on ungrouped rows", req: Request{Dataset: "sessions", Ungrouped: true, Grain: TimeGrainDay}, code: ErrUnsatisfiable, field: "grain"},
		{name: "unknown grain", req: Request{Dataset: "sessions", Measures: count, Grain: "minute"}, code: ErrUnsupportedGrain, field: "grain"},
		{name: "limit above the maximum", req: Request{Dataset: "sessions", Measures: count, Limit: MaxLimit + 1}, code: ErrLimitExceeded, field: "limit"},
		{name: "to before from", req: Request{Dataset: "sessions", Measures: count, FromUnixNano: testTo, ToUnixNano: testFrom}, code: ErrInvalidTimeRange, field: "to"},
		{name: "range beyond the dataset's retention", req: Request{Dataset: "sessions", Measures: count, FromUnixNano: testFrom, ToUnixNano: testFrom + Sessions.MaxTimeRangeNanos() + 1}, code: ErrInvalidTimeRange, field: "to"},
		{name: "range whose signed span wraps", req: Request{Dataset: "sessions", Measures: count, FromUnixNano: math.MinInt64, ToUnixNano: math.MaxInt64}, code: ErrInvalidTimeRange, field: "to"},
	}
	for _, tc := range cases {
		t.Run("it rejects "+tc.name, func(t *testing.T) {
			t.Parallel()
			_, err := compileTest(t, tc.req)
			var invalid *Error
			require.ErrorAs(t, err, &invalid)
			require.Equal(t, tc.code, invalid.Code, err.Error())
			require.Equal(t, tc.field, invalid.Field, err.Error())
			require.Contains(t, err.Error(), string(tc.code))
		})
	}
}

// TestCompileAcceptsAWindowUpToTheDatasetRetention: the bound is the
// dataset's own, and it is inclusive.
func TestCompileAcceptsAWindowUpToTheDatasetRetention(t *testing.T) {
	t.Parallel()

	_, err := compileTest(t, Request{
		Dataset:      "sessions",
		Measures:     []Measure{{Op: "count", Field: "", Alias: ""}},
		FromUnixNano: testFrom,
		ToUnixNano:   testFrom + Sessions.MaxTimeRangeNanos(),
	})
	require.NoError(t, err)
}

func TestReadExprFoldsThroughALookup(t *testing.T) {
	t.Parallel()

	plain := Field{Name: "tool_name", Type: TypeString, Role: RoleDimension, Default: false, Unit: "", Operators: equalsIn, Aggregations: nil, Expr: "tool_name", Description: "", Lookup: ""}
	folded := Field{Name: "mcp_server", Type: TypeString, Role: RoleDimension, Default: false, Unit: "", Operators: equalsIn, Aggregations: nil, Expr: "mcp_server_name", Description: "", Lookup: "names"}
	maps := LookupMaps{"names": {"github-mcp": "GitHub", "gh": "GitHub", "": "Nothing", "blank": ""}}

	expr, args := readExpr(QueryContext{Tenant: Tenant{OrganizationID: "", ProjectID: ""}, Window: Window{FromUnixNano: 0, ToUnixNano: 0}, Lookups: maps}, &plain)
	require.Equal(t, "tool_name", expr)
	require.Nil(t, args)

	expr, args = readExpr(QueryContext{Tenant: Tenant{OrganizationID: "", ProjectID: ""}, Window: Window{FromUnixNano: 0, ToUnixNano: 0}, Lookups: nil}, &folded)
	require.Equal(t, "mcp_server_name", expr, "no loaded map, no fold")
	require.Nil(t, args)

	expr, args = readExpr(QueryContext{Tenant: Tenant{OrganizationID: "", ProjectID: ""}, Window: Window{FromUnixNano: 0, ToUnixNano: 0}, Lookups: maps}, &folded)
	require.Equal(t, "transform(mcp_server_name, ?, ?, mcp_server_name)", expr)
	require.Equal(t, []any{[]string{"gh", "github-mcp"}, []string{"GitHub", "GitHub"}}, args, "raw values sorted, and never an empty side")
}

func TestCompileFoldsADimensionThroughItsLookup(t *testing.T) {
	t.Parallel()

	req := Request{
		Dataset:      "tool_calls",
		FromUnixNano: testFrom,
		ToUnixNano:   testTo,
		Grain:        "",
		Dimensions:   []string{"mcp_server"},
		Measures: []Measure{
			{Op: "count", Field: "", Alias: ""},
			{Op: "count_distinct", Field: "mcp_server", Alias: "servers"},
		},
		Filters:   []Filter{{Field: "mcp_server", Operator: "equals", Values: []string{"GitHub"}}},
		OrderBy:   nil,
		Limit:     0,
		Ungrouped: false,
	}
	tenant := Tenant{OrganizationID: "org-1", ProjectID: "project-1"}
	maps := LookupMaps{MCPServerDisplayNamesLookup: {"gh": "GitHub", "github-mcp": "GitHub"}}
	plan, err := Compile(Default, tenant, maps, req)
	require.NoError(t, err)
	const fold = "transform(mcp_server_name, ?, ?, mcp_server_name)"
	require.Contains(t, plan.SQL, fold+" AS mcp_server")
	require.Contains(t, plan.SQL, "uniqExact(nullIf("+fold+", '')) AS servers", "the distinct count reads the fold once")
	require.Contains(t, plan.SQL, "WHERE "+fold+" = ?", "the filter compares the folded value")
	require.Equal(t, []string{"gh", "github-mcp"}, plan.Args[0], "the select list's arrays come first")
	require.Equal(t, []string{"GitHub", "GitHub"}, plan.Args[1])
	require.Equal(t, []string{"gh", "github-mcp"}, plan.Args[2], "the distinct count binds its arrays once")
	require.Equal(t, "org-1", plan.Args[4], "the tenant follows the select list's binds")
	require.Equal(t, "GitHub", plan.Args[len(plan.Args)-1])
	require.NotContains(t, plan.SQL, "GROUP BY "+fold, "the group names the alias, not the expression")

	plain, err := compileTest(t, req)
	require.NoError(t, err)
	require.NotContains(t, plain.SQL, "transform(", "no loaded map, no fold")
	require.Contains(t, plain.SQL, "WHERE mcp_server_name = ?")

	in := req
	in.Filters = []Filter{{Field: "mcp_server", Operator: "in", Values: []string{"GitHub", "linear"}}}
	plan, err = Compile(Default, tenant, maps, in)
	require.NoError(t, err)
	require.Contains(t, plan.SQL, "WHERE "+fold+" IN (?,?)")
	require.Equal(t, []any{"GitHub", "linear"}, plan.Args[len(plan.Args)-2:])
}

func TestCompileFoldsAFilterValueThroughItsLookup(t *testing.T) {
	t.Parallel()

	tenant := Tenant{OrganizationID: "org-1", ProjectID: "project-1"}
	maps := LookupMaps{MCPServerDisplayNamesLookup: {"gh": "GitHub", "github-mcp": "GitHub"}}
	req := Request{
		Dataset:      "tool_calls",
		FromUnixNano: testFrom,
		ToUnixNano:   testTo,
		Grain:        "",
		Dimensions:   []string{"mcp_server"},
		Measures:     []Measure{{Op: "count", Field: "", Alias: ""}},
		Filters:      []Filter{{Field: "mcp_server", Operator: "equals", Values: []string{"gh"}}},
		OrderBy:      nil,
		Limit:        0,
		Ungrouped:    false,
	}

	plan, err := Compile(Default, tenant, maps, req)
	require.NoError(t, err)
	require.Contains(t, plan.SQL, "IN (?,?)", "equals on a raw name compares against the name and what it folds to")
	require.Equal(t, []any{"gh", "GitHub"}, plan.Args[len(plan.Args)-2:], "a saved filter on a raw name keeps matching once the name is overridden")

	chained := LookupMaps{MCPServerDisplayNamesLookup: {"alpha": "beta", "beta": "gamma"}}
	display := req
	display.Filters = []Filter{{Field: "mcp_server", Operator: "equals", Values: []string{"beta"}}}
	plan, err = Compile(Default, tenant, chained, display)
	require.NoError(t, err)
	require.Equal(t, []any{"beta", "gamma"}, plan.Args[len(plan.Args)-2:], "a display name that is also a raw name still matches what folds to it")

	two := req
	two.Filters = []Filter{{Field: "mcp_server", Operator: "equals", Values: []string{"gh", "github-mcp"}}}
	_, err = Compile(Default, tenant, maps, two)
	require.ErrorContains(t, err, "equals takes exactly one value", "the guard reads the request, not the folded list")

	in := req
	in.Filters = []Filter{{Field: "mcp_server", Operator: "in", Values: []string{"gh", "github-mcp", "linear", "GitHub"}}}
	plan, err = Compile(Default, tenant, maps, in)
	require.NoError(t, err)
	require.Contains(t, plan.SQL, "IN (?,?,?,?)", "each value and its fold, once")
	require.Equal(t, []any{"gh", "GitHub", "github-mcp", "linear"}, plan.Args[len(plan.Args)-4:])

	plain, err := Compile(Default, tenant, nil, req)
	require.NoError(t, err)
	require.Contains(t, plain.SQL, "WHERE mcp_server_name = ?")
	require.Equal(t, "gh", plain.Args[len(plain.Args)-1], "no loaded map, the value is read as given")
}
