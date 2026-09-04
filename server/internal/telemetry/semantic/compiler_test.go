package semantic_test

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/telemetry/semantic"
)

// assertGolden compares got against the committed fixture. Regenerate with
// UPDATE_GOLDEN=1 mise exec -- go test ./server/internal/telemetry/semantic/.
func assertGolden(t *testing.T, name string, got string) {
	t.Helper()

	path := filepath.Join("testdata", name)
	if os.Getenv("UPDATE_GOLDEN") != "" {
		require.NoError(t, os.MkdirAll("testdata", 0o750))
		require.NoError(t, os.WriteFile(path, []byte(got+"\n"), 0o600))
		return
	}
	want, err := os.ReadFile(path)
	require.NoError(t, err, "missing golden fixture %s (regenerate with UPDATE_GOLDEN=1)", path)
	require.Equal(t, string(want), got+"\n", "compiled SQL drifted from golden fixture %s", path)
}

func marshalArgs(t *testing.T, args []any) string {
	t.Helper()

	data, err := json.MarshalIndent(args, "", "  ")
	require.NoError(t, err)
	return string(data)
}

func allUsageMeasures() []string {
	return []string{
		"cost_usd",
		"input_tokens",
		"output_tokens",
		"tokens_total",
		"cache_read_tokens",
		"cache_write_tokens",
		"tool_calls",
		"chats",
		"total_work_units",
		"scored_cost",
		"scored_tokens",
	}
}

// rawServedMeasures are the measures the raw telemetry_logs binding serves:
// everything except the work-units measures, which exist only in the
// aggregate (they come from synthetic chat_analysis score rows the session
// expressions don't cover).
func rawServedMeasures() []string {
	return []string{
		"cost_usd",
		"input_tokens",
		"output_tokens",
		"tokens_total",
		"cache_read_tokens",
		"cache_write_tokens",
		"tool_calls",
		"chats",
	}
}

// demoQuery is the cost explorer's telemetry.query group_by=email request in
// catalog vocabulary: all measures, grouped by user, ranked by cost.
func demoQuery(granularitySeconds int64) semantic.Query {
	return semantic.Query{
		Model:                  "usage",
		Measures:               allUsageMeasures(),
		GroupBy:                "user",
		Filters:                nil,
		TimeStart:              testTimeStart,
		TimeEnd:                testTimeEnd,
		GranularitySeconds:     granularitySeconds,
		Scope:                  testScope,
		Sort:                   &semantic.Sort{Measure: "cost_usd", Desc: true},
		IncludeDimensionValues: granularitySeconds == 0,
	}
}

func TestCompile_DemoTableGolden(t *testing.T) {
	t.Parallel()

	def, err := semantic.Load()
	require.NoError(t, err)

	plan, err := semantic.Plan(def, demoQuery(0))
	require.NoError(t, err)
	require.Equal(t, "attribute_metrics_summaries", plan.Binding.Source)

	query, args, err := semantic.Compile(plan)
	require.NoError(t, err)
	assertGolden(t, "demo_table.sql", query)
	assertGolden(t, "demo_table.args.json", marshalArgs(t, args))
}

func TestCompile_DemoTimeseriesGolden(t *testing.T) {
	t.Parallel()

	def, err := semantic.Load()
	require.NoError(t, err)

	plan, err := semantic.Plan(def, demoQuery(3600))
	require.NoError(t, err)
	require.Equal(t, "attribute_metrics_summaries", plan.Binding.Source)

	query, args, err := semantic.Compile(plan)
	require.NoError(t, err)
	assertGolden(t, "demo_timeseries.sql", query)
	assertGolden(t, "demo_timeseries.args.json", marshalArgs(t, args))
}

// TestCompile_RawTableGolden pins the raw telemetry_logs shape: a session
// filter is only served by the raw binding, whose row_filter admits the
// locally-observed population only.
func TestCompile_RawTableGolden(t *testing.T) {
	t.Parallel()

	def, err := semantic.Load()
	require.NoError(t, err)

	plan, err := semantic.Plan(def, semantic.Query{
		Model:                  "usage",
		Measures:               rawServedMeasures(),
		GroupBy:                "user",
		Filters:                []semantic.Filter{{Dimension: "session", Values: []string{"chat-1", "chat-2"}}},
		TimeStart:              testTimeStart,
		TimeEnd:                testTimeEnd,
		GranularitySeconds:     0,
		Scope:                  testScope,
		Sort:                   &semantic.Sort{Measure: "cost_usd", Desc: true},
		IncludeDimensionValues: false,
	})
	require.NoError(t, err)
	require.Equal(t, "telemetry_logs", plan.Binding.Source)

	query, args, err := semantic.Compile(plan)
	require.NoError(t, err)
	assertGolden(t, "raw_table.sql", query)
	assertGolden(t, "raw_table.args.json", marshalArgs(t, args))
}

func TestCompile_ArrayGroupByAndFilters(t *testing.T) {
	t.Parallel()

	def, err := semantic.Load()
	require.NoError(t, err)

	// Array group_by arrayJoins with the empty→[''] mapping; an array filter
	// mixing "" and values ORs hasAny with emptiness so the "(unset)" bucket
	// stays drillable; scalar filters compile to IN.
	plan, err := semantic.Plan(def, semantic.Query{
		Model:     "usage",
		Measures:  []string{"cost_usd"},
		GroupBy:   "role",
		Filters:   []semantic.Filter{{Dimension: "group", Values: []string{"eng", ""}}, {Dimension: "department", Values: []string{"Engineering"}}},
		TimeStart: testTimeStart,
		TimeEnd:   testTimeEnd,
		Scope:     testScope,
	})
	require.NoError(t, err)

	query, args, err := semantic.Compile(plan)
	require.NoError(t, err)
	require.Contains(t, query, "arrayJoin(if(empty(roles), [''], roles)) AS group_value")
	require.Contains(t, query, "(hasAny(groups, ?) OR empty(groups))")
	require.Contains(t, query, "department_name IN (?)")
	require.Equal(t, []any{"11111111-1111-1111-1111-111111111111", testTimeStart, testTimeEnd, []string{"eng"}, "Engineering"}, args)
}
