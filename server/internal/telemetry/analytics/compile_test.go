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
	return Compile(Default, "org-1", "project-1", req)
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
		{name: "repeated dimension", req: Request{Dataset: "sessions", Dimensions: []string{"user", "user"}, Measures: count}, code: ErrUnsatisfiable, field: "dimensions[1]"},
		{name: "too many dimensions", req: Request{Dataset: "sessions", Dimensions: []string{"user", "model", "surface", "provider"}, Measures: count}, code: ErrTooManyDimensions, field: "dimensions"},
		{name: "aggregation the field does not admit", req: Request{Dataset: "sessions", Measures: []Measure{{Op: "p95", Field: "turn_count", Alias: ""}}}, code: ErrUnsupportedAggregation, field: "measures[0].op"},
		{name: "unknown measure field", req: Request{Dataset: "sessions", Measures: []Measure{{Op: "sum", Field: "cost_usd", Alias: ""}}}, code: ErrUnknownField, field: "measures[0].field"},
		{name: "count with a field", req: Request{Dataset: "sessions", Measures: []Measure{{Op: "count", Field: "turn_count", Alias: ""}}}, code: ErrUnsatisfiable, field: "measures[0].field"},
		{name: "alias that is a field name", req: Request{Dataset: "sessions", Measures: []Measure{{Op: "count", Field: "", Alias: "user"}}}, code: ErrUnsatisfiable, field: "measures[0].alias"},
		{name: "alias that is not an identifier", req: Request{Dataset: "sessions", Measures: []Measure{{Op: "count", Field: "", Alias: "x; DROP TABLE"}}}, code: ErrUnsatisfiable, field: "measures[0].alias"},
		{name: "duplicate alias", req: Request{Dataset: "sessions", Measures: []Measure{{Op: "count", Field: "", Alias: "n"}, {Op: "sum", Field: "turn_count", Alias: "n"}}}, code: ErrUnsatisfiable, field: "measures[1].alias"},
		{name: "grouped without measures", req: Request{Dataset: "sessions"}, code: ErrUnsatisfiable, field: "measures"},
		{name: "operator the dimension does not admit", req: Request{Dataset: "sessions", Measures: count, Filters: []Filter{{Field: "user", Operator: "contains", Values: []string{"a"}}}}, code: ErrUnsupportedOperator, field: "filters[0].operator"},
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
