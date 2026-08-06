package semantic_test

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/telemetry/semantic"
)

var (
	testTimeStart = time.Date(2026, time.July, 14, 0, 0, 0, 0, time.UTC).UnixNano()
	testTimeEnd   = time.Date(2026, time.July, 14, 2, 0, 0, 0, time.UTC).UnixNano()
	testScope     = semantic.Scope{ProjectIDs: []string{"11111111-1111-1111-1111-111111111111"}}
)

// summariesOnlyDefinition scopes usage-like content down to a single
// hour-bucketed binding so unsatisfiable cases are constructible (the real
// definition's raw binding serves everything at second granularity).
func summariesOnlyDefinition(t *testing.T) *semantic.Definition {
	t.Helper()

	def := &semantic.Definition{
		Version: 1,
		Dimensions: []semantic.CatalogDimension{
			{Name: "project", Type: semantic.DimTypeID, Description: "project"},
			{Name: "color", Type: semantic.DimTypeString, Description: "color"},
			{Name: "shade", Type: semantic.DimTypeString, Description: "shade, unserved by the binding"},
		},
		Models: []semantic.Model{{
			Name:          "test.usage",
			Description:   "test model",
			Dimensions:    []string{"project", "color", "shade"},
			Time:          semantic.ModelTime{MinGranularitySeconds: 1},
			ExclusiveWith: []string{},
			Measures: []semantic.Measure{
				{Name: "cost", Unit: "usd", Aggregation: "sum", Additivity: "full", Type: semantic.MeasureTypeFloat64},
				{Name: "events", Unit: "count", Aggregation: "count", Additivity: "full", Type: semantic.MeasureTypeUint64},
			},
			Bindings: []semantic.Binding{{
				Source:     "summaries",
				Precedence: 100,
				RowFilter:  "",
				Time:       semantic.BindingTime{Kind: semantic.TimeKindHourBucket, Column: "time_bucket", MinGranularitySeconds: 3600},
				Dimensions: map[string]semantic.BindingExpr{
					"project": {SQL: "project_id"},
					"color":   {SQL: "color"},
				},
				Measures: map[string]semantic.BindingExpr{
					"cost": {SQL: "sumMerge(cost)"},
				},
			}},
		}},
	}
	require.NoError(t, def.Validate())
	return def
}

func TestPlan_PicksHighestPrecedenceBinding(t *testing.T) {
	t.Parallel()

	def, err := semantic.Load()
	require.NoError(t, err)

	plan, err := semantic.Plan(def, semantic.Query{
		Model:              "usage",
		Measures:           []string{"cost_usd", "chats"},
		GroupBy:            "user",
		TimeStart:          testTimeStart,
		TimeEnd:            testTimeEnd,
		GranularitySeconds: 3600,
		Scope:              testScope,
	})
	require.NoError(t, err)
	require.Equal(t, "attribute_metrics_summaries", plan.Binding.Source)
}

func TestPlan_OrdersMeasuresByModelDeclaration(t *testing.T) {
	t.Parallel()

	def, err := semantic.Load()
	require.NoError(t, err)

	// Requested out of order (and duplicated); the plan reorders to the
	// model's declaration order, which drives the SELECT order.
	plan, err := semantic.Plan(def, semantic.Query{
		Model:     "usage",
		Measures:  []string{"chats", "cost_usd", "input_tokens", "cost_usd"},
		TimeStart: testTimeStart,
		TimeEnd:   testTimeEnd,
		Scope:     testScope,
	})
	require.NoError(t, err)
	names := make([]string, 0, len(plan.Measures))
	for _, ms := range plan.Measures {
		names = append(names, ms.Name)
	}
	require.Equal(t, []string{"cost_usd", "input_tokens", "chats"}, names)
}

func TestPlan_SessionDimensionFallsThroughToRawBinding(t *testing.T) {
	t.Parallel()

	def, err := semantic.Load()
	require.NoError(t, err)

	// The summaries binding does not serve the session grain, so the planner
	// must fall through to the raw telemetry_logs binding.
	plan, err := semantic.Plan(def, semantic.Query{
		Model:     "usage",
		Measures:  []string{"cost_usd"},
		GroupBy:   "session",
		TimeStart: testTimeStart,
		TimeEnd:   testTimeEnd,
		Scope:     testScope,
	})
	require.NoError(t, err)
	require.Equal(t, "telemetry_logs", plan.Binding.Source)
}

func TestPlan_FineGranularityFallsThroughToRawBinding(t *testing.T) {
	t.Parallel()

	def, err := semantic.Load()
	require.NoError(t, err)

	// 60s buckets are finer than the hourly summary supports; the raw binding
	// serves them.
	plan, err := semantic.Plan(def, semantic.Query{
		Model:              "usage",
		Measures:           []string{"cost_usd"},
		GroupBy:            "user",
		TimeStart:          testTimeStart,
		TimeEnd:            testTimeEnd,
		GranularitySeconds: 60,
		Scope:              testScope,
	})
	require.NoError(t, err)
	require.Equal(t, "telemetry_logs", plan.Binding.Source)
}

func TestPlan_DimensionValuesCoverBindingDimensionsMinusGroupBy(t *testing.T) {
	t.Parallel()

	def, err := semantic.Load()
	require.NoError(t, err)

	plan, err := semantic.Plan(def, semantic.Query{
		Model:                  "usage",
		Measures:               []string{"cost_usd"},
		GroupBy:                "user",
		TimeStart:              testTimeStart,
		TimeEnd:                testTimeEnd,
		Scope:                  testScope,
		IncludeDimensionValues: true,
	})
	require.NoError(t, err)
	require.Equal(t, "attribute_metrics_summaries", plan.Binding.Source)
	// Every summaries-binding dimension except the grouped one, sorted.
	require.Len(t, plan.DimensionValuesDims, len(plan.Binding.Dimensions)-1)
	require.NotContains(t, plan.DimensionValuesDims, "user")
	require.IsIncreasing(t, plan.DimensionValuesDims)
}

func TestPlan_RejectsMissingModel(t *testing.T) {
	t.Parallel()

	def, err := semantic.Load()
	require.NoError(t, err)

	_, err = semantic.Plan(def, semantic.Query{
		Measures:  []string{"cost_usd"},
		TimeStart: testTimeStart,
		TimeEnd:   testTimeEnd,
		Scope:     testScope,
	})
	require.ErrorContains(t, err, "query names no model")
}

func TestPlan_RejectsUnknownModel(t *testing.T) {
	t.Parallel()

	def, err := semantic.Load()
	require.NoError(t, err)

	_, err = semantic.Plan(def, semantic.Query{
		Model:     "ghost",
		Measures:  []string{"cost_usd"},
		TimeStart: testTimeStart,
		TimeEnd:   testTimeEnd,
		Scope:     testScope,
	})
	require.ErrorContains(t, err, `unknown model "ghost"`)
}

func TestPlan_RejectsUnknownMeasure(t *testing.T) {
	t.Parallel()

	def, err := semantic.Load()
	require.NoError(t, err)

	_, err = semantic.Plan(def, semantic.Query{
		Model:     "usage",
		Measures:  []string{"margin_usd"},
		TimeStart: testTimeStart,
		TimeEnd:   testTimeEnd,
		Scope:     testScope,
	})
	require.ErrorContains(t, err, `unknown measure "margin_usd"`)
}

func TestPlan_RejectsUnknownGroupByDimension(t *testing.T) {
	t.Parallel()

	def, err := semantic.Load()
	require.NoError(t, err)

	_, err = semantic.Plan(def, semantic.Query{
		Model:     "usage",
		Measures:  []string{"cost_usd"},
		GroupBy:   "skill_version",
		TimeStart: testTimeStart,
		TimeEnd:   testTimeEnd,
		Scope:     testScope,
	})
	require.ErrorContains(t, err, `unknown group_by dimension "skill_version"`)
}

func TestPlan_RejectsSortMeasureNotRequested(t *testing.T) {
	t.Parallel()

	def, err := semantic.Load()
	require.NoError(t, err)

	_, err = semantic.Plan(def, semantic.Query{
		Model:     "usage",
		Measures:  []string{"cost_usd"},
		TimeStart: testTimeStart,
		TimeEnd:   testTimeEnd,
		Scope:     testScope,
		Sort:      &semantic.Sort{Measure: "chats", Desc: true},
	})
	require.ErrorContains(t, err, "not among the requested measures")
}

func TestPlan_UnsatisfiableGranularity(t *testing.T) {
	t.Parallel()

	def := summariesOnlyDefinition(t)

	_, err := semantic.Plan(def, semantic.Query{
		Model:              "test.usage",
		Measures:           []string{"cost"},
		GroupBy:            "color",
		TimeStart:          testTimeStart,
		TimeEnd:            testTimeEnd,
		GranularitySeconds: 60,
		Scope:              testScope,
	})
	var unsat *semantic.UnsatisfiableError
	require.ErrorAs(t, err, &unsat)
	require.Equal(t, "test.usage", unsat.Model)
	require.Empty(t, unsat.MissingDimensions)
	require.Empty(t, unsat.MissingMeasures)
	require.EqualValues(t, 60, unsat.GranularitySeconds)
	require.ErrorContains(t, err, "granularity 60s")
}

func TestPlan_UnsatisfiableDimension(t *testing.T) {
	t.Parallel()

	def := summariesOnlyDefinition(t)

	// shade is a model dimension no binding serves.
	_, err := semantic.Plan(def, semantic.Query{
		Model:     "test.usage",
		Measures:  []string{"cost"},
		GroupBy:   "shade",
		TimeStart: testTimeStart,
		TimeEnd:   testTimeEnd,
		Scope:     testScope,
	})
	var unsat *semantic.UnsatisfiableError
	require.ErrorAs(t, err, &unsat)
	require.Equal(t, []string{"shade"}, unsat.MissingDimensions)
	require.Empty(t, unsat.MissingMeasures)
	require.Zero(t, unsat.GranularitySeconds)
	require.ErrorContains(t, err, "dimensions shade not served by any binding")
}

func TestPlan_UnsatisfiableMeasure(t *testing.T) {
	t.Parallel()

	def := summariesOnlyDefinition(t)

	// events is a model measure no binding serves.
	_, err := semantic.Plan(def, semantic.Query{
		Model:     "test.usage",
		Measures:  []string{"cost", "events"},
		TimeStart: testTimeStart,
		TimeEnd:   testTimeEnd,
		Scope:     testScope,
	})
	var unsat *semantic.UnsatisfiableError
	require.ErrorAs(t, err, &unsat)
	require.Empty(t, unsat.MissingDimensions)
	require.Equal(t, []string{"events"}, unsat.MissingMeasures)
	require.ErrorContains(t, err, "measures events not served by any binding")
}

// Note: combining measures from two models in one query is syntactically
// impossible since Query names a single model, which is how the mutually
// exclusive usage authorities (usage / sessions / provider_reports) stay
// separate. The exclusive_with declarations remain as definition-level
// documentation, validated at load time (see model_test.go).

func TestPlan_MinWindowRoutesSessions(t *testing.T) {
	t.Parallel()

	def, err := semantic.Load()
	require.NoError(t, err)

	end := time.Date(2026, time.July, 14, 0, 0, 0, 0, time.UTC)

	// A 7-day window satisfies chat_session_summaries' 48h min_window.
	wide, err := semantic.Plan(def, semantic.Query{
		Model:     "sessions",
		Measures:  []string{"cost_usd"},
		GroupBy:   "session",
		TimeStart: end.Add(-7 * 24 * time.Hour).UnixNano(),
		TimeEnd:   end.UnixNano(),
		Scope:     testScope,
	})
	require.NoError(t, err)
	require.Equal(t, "chat_session_summaries", wide.Binding.Source)

	// A 1-day window is below it; fall through to raw telemetry_logs.
	narrow, err := semantic.Plan(def, semantic.Query{
		Model:     "sessions",
		Measures:  []string{"cost_usd"},
		GroupBy:   "session",
		TimeStart: end.Add(-24 * time.Hour).UnixNano(),
		TimeEnd:   end.UnixNano(),
		Scope:     testScope,
	})
	require.NoError(t, err)
	require.Equal(t, "telemetry_logs", narrow.Binding.Source)
}

func TestPlan_SessionsIdentityDimensionFallsThroughToRaw(t *testing.T) {
	t.Parallel()

	def, err := semantic.Load()
	require.NoError(t, err)

	// chat_session_summaries only exposes project/session (identity columns
	// there are merged per-chat arrays); grouping by user must route to raw
	// even when the window is wide enough for the summary.
	end := time.Date(2026, time.July, 14, 0, 0, 0, 0, time.UTC)
	plan, err := semantic.Plan(def, semantic.Query{
		Model:     "sessions",
		Measures:  []string{"cost_usd"},
		GroupBy:   "user",
		TimeStart: end.Add(-7 * 24 * time.Hour).UnixNano(),
		TimeEnd:   end.UnixNano(),
		Scope:     testScope,
	})
	require.NoError(t, err)
	require.Equal(t, "telemetry_logs", plan.Binding.Source)
}

func TestPlan_ProviderReportsRejectsSessionDimension(t *testing.T) {
	t.Parallel()

	def, err := semantic.Load()
	require.NoError(t, err)

	// Provider-settled records carry no session grain; session is not on the
	// model's dimension allowlist at all, so this is a validation error, not
	// an unsatisfiable binding search.
	_, err = semantic.Plan(def, semantic.Query{
		Model:     "provider_reports",
		Measures:  []string{"cost_usd"},
		GroupBy:   "session",
		TimeStart: testTimeStart,
		TimeEnd:   testTimeEnd,
		Scope:     testScope,
	})
	require.ErrorContains(t, err, `unknown group_by dimension "session" for model "provider_reports"`)
	var unsat *semantic.UnsatisfiableError
	require.NotErrorAs(t, err, &unsat)
}

// minWindowOnlyDefinition has a single binding gated by min_window_seconds so
// the window alone can make a query unsatisfiable.
func minWindowOnlyDefinition(t *testing.T) *semantic.Definition {
	t.Helper()

	def := summariesOnlyDefinition(t)
	def.Models[0].Bindings[0].MinWindowSeconds = 172800
	require.NoError(t, def.Validate())
	return def
}

func TestPlan_UnsatisfiableMinWindow(t *testing.T) {
	t.Parallel()

	def := minWindowOnlyDefinition(t)

	// testTimeStart..testTimeEnd is a 2h window, far below the 48h minimum
	// of the only binding; nothing to fall through to.
	_, err := semantic.Plan(def, semantic.Query{
		Model:     "test.usage",
		Measures:  []string{"cost"},
		GroupBy:   "color",
		TimeStart: testTimeStart,
		TimeEnd:   testTimeEnd,
		Scope:     testScope,
	})
	var unsat *semantic.UnsatisfiableError
	require.ErrorAs(t, err, &unsat)
	require.Empty(t, unsat.MissingDimensions)
	require.Empty(t, unsat.MissingMeasures)
	require.Zero(t, unsat.GranularitySeconds)
	require.EqualValues(t, 172800, unsat.MinWindowSeconds)
	require.ErrorContains(t, err, "time window is narrower than the 172800s minimum")
}
