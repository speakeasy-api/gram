package semantic_test

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/telemetry/semantic"
)

// validDefinition returns a minimal valid definition for validation tests to
// mutate.
func validDefinition() *semantic.Definition {
	return &semantic.Definition{
		Version: 1,
		Dimensions: []semantic.CatalogDimension{
			{Name: "project", Type: semantic.DimTypeID, LegacyKey: "project_id", Description: "project"},
			{Name: "color", Type: semantic.DimTypeString, LegacyKey: "color_name", Description: "color"},
			{Name: "tags", Type: semantic.DimTypeStringArray, Description: "tags"},
		},
		Models: []semantic.Model{{
			Name:          "test.usage",
			Description:   "test model",
			Dimensions:    []string{"project", "color", "tags"},
			Time:          semantic.ModelTime{MinGranularitySeconds: 1},
			ExclusiveWith: []string{},
			Measures: []semantic.Measure{
				{Name: "cost", LegacyKey: "total_cost", Unit: "usd", Aggregation: "sum", Additivity: "full", Type: semantic.MeasureTypeFloat64},
				{Name: "events", Unit: "count", Aggregation: "count", Additivity: "full", Type: semantic.MeasureTypeUint64},
			},
			Bindings: []semantic.Binding{{
				Source:     "some_table",
				Precedence: 100,
				RowFilter:  "is_active = 1",
				Time:       semantic.BindingTime{Kind: semantic.TimeKindHourBucket, Column: "time_bucket", MinGranularitySeconds: 3600},
				Dimensions: map[string]semantic.BindingExpr{
					"project": {SQL: "project_id"},
					"color":   {SQL: "color"},
					"tags":    {SQL: "tags"},
				},
				Measures: map[string]semantic.BindingExpr{
					"cost":   {SQL: "sumMerge(cost)"},
					"events": {SQL: "countMerge(events)"},
				},
			}},
		}},
	}
}

func TestLoad_EmbeddedDefinitionIsValid(t *testing.T) {
	t.Parallel()

	def, err := semantic.Load()
	require.NoError(t, err)
	require.Equal(t, 1, def.Version)

	model, ok := def.Model("usage")
	require.True(t, ok, "usage model must exist")
	require.Len(t, model.Dimensions, len(def.Dimensions), "usage carries every catalog dimension")
	require.Len(t, model.Measures, 11)
	require.ElementsMatch(t, []string{"sessions", "provider_reports"}, model.ExclusiveWith)

	// The canonical measure declaration order drives the compiled SELECT order.
	names := make([]string, 0, len(model.Measures))
	for _, ms := range model.Measures {
		names = append(names, ms.Name)
	}
	require.Equal(t, []string{
		"cost_usd", "input_tokens", "output_tokens", "tokens_total",
		"cache_read_tokens", "cache_write_tokens", "tool_calls", "chats",
		"total_work_units", "scored_cost", "scored_tokens",
	}, names)

	require.Len(t, model.Bindings, 2)
	var summaries, raw *semantic.Binding
	for i := range model.Bindings {
		switch model.Bindings[i].Source {
		case "attribute_metrics_summaries":
			summaries = &model.Bindings[i]
		case "telemetry_logs":
			raw = &model.Bindings[i]
		}
	}
	require.NotNil(t, summaries)
	require.NotNil(t, raw)
	require.Greater(t, summaries.Precedence, raw.Precedence)

	// The hour-bucketed summary cannot serve the session/turn grain; the raw
	// binding serves everything.
	require.NotContains(t, summaries.Dimensions, "session")
	require.NotContains(t, summaries.Dimensions, "turn")
	require.Len(t, summaries.Dimensions, len(def.Dimensions)-2)
	require.Len(t, raw.Dimensions, len(def.Dimensions))
	require.Len(t, summaries.Measures, 11)
	// The work-units measures exist only in the aggregate (they come from
	// synthetic chat_analysis score rows the session expressions don't cover),
	// so the raw binding serves the original 8.
	require.Len(t, raw.Measures, 8)
}

// TestLoad_Phase2ModelShapes pins the structural facts of the phase-2 models
// that the planner's routing depends on (window-gated summaries bindings, the
// settled-only provider model, exclusivity between the usage authorities).
func TestLoad_Phase2ModelShapes(t *testing.T) {
	t.Parallel()

	def, err := semantic.Load()
	require.NoError(t, err)

	sessions, ok := def.Model("sessions")
	require.True(t, ok)
	require.False(t, sessions.Internal)
	require.Equal(t, "usage", sessions.RollupOf)
	require.NotContains(t, sessions.Dimensions, "turn")
	require.Contains(t, sessions.Dimensions, "session")
	require.Len(t, sessions.Measures, 8)
	require.ElementsMatch(t, []string{"usage", "provider_reports"}, sessions.ExclusiveWith)
	require.Len(t, sessions.Bindings, 2)
	var sessionsSummary, sessionsRaw *semantic.Binding
	for i := range sessions.Bindings {
		switch sessions.Bindings[i].Source {
		case "chat_session_summaries":
			sessionsSummary = &sessions.Bindings[i]
		case "telemetry_logs":
			sessionsRaw = &sessions.Bindings[i]
		}
	}
	require.NotNil(t, sessionsSummary)
	require.NotNil(t, sessionsRaw)
	require.EqualValues(t, 172800, sessionsSummary.MinWindowSeconds)
	require.Equal(t, semantic.TimeKindHourBucket, sessionsSummary.Time.Kind)
	// The summaries table only carries per-chat facts; identity dims are
	// merged arrays there and deliberately not exposed.
	require.Len(t, sessionsSummary.Dimensions, 2)
	require.Contains(t, sessionsSummary.Dimensions, "project")
	require.Contains(t, sessionsSummary.Dimensions, "session")
	require.Len(t, sessionsSummary.Measures, 8)
	// The raw binding is the usage model's minus the turn grain.
	usage, ok := def.Model("usage")
	require.True(t, ok)
	require.False(t, usage.Internal)
	var usageRaw *semantic.Binding
	for i := range usage.Bindings {
		if usage.Bindings[i].Source == "telemetry_logs" {
			usageRaw = &usage.Bindings[i]
		}
	}
	require.NotNil(t, usageRaw)
	require.Equal(t, usageRaw.RowFilter, sessionsRaw.RowFilter)
	require.Equal(t, usageRaw.Measures, sessionsRaw.Measures)
	require.NotContains(t, sessionsRaw.Dimensions, "turn")
	for name, expr := range sessionsRaw.Dimensions {
		require.Equal(t, usageRaw.Dimensions[name], expr, "sessions raw dim %s must mirror usage", name)
	}
	require.Len(t, sessionsRaw.Dimensions, len(usageRaw.Dimensions)-1)

	providerReports, ok := def.Model("provider_reports")
	require.True(t, ok)
	require.True(t, providerReports.Internal, "provider_reports declares the settled population; it is not a public model")
	require.Empty(t, providerReports.RollupOf)
	require.NotContains(t, providerReports.Dimensions, "session")
	require.NotContains(t, providerReports.Dimensions, "turn")
	require.NotContains(t, providerReports.Dimensions, "hostname")
	require.ElementsMatch(t, []string{"usage", "sessions"}, providerReports.ExclusiveWith)
	require.Len(t, providerReports.Bindings, 1, "no summaries binding: the table cannot discriminate settled from observed rows")
	require.Equal(t, "telemetry_logs", providerReports.Bindings[0].Source)
	charged, ok := providerReports.Measure("charged_usd")
	require.True(t, ok)
	require.Equal(t, "usd", charged.Unit)
	require.Equal(t, "unavailable", charged.NullSemantics)
	// Shared dims reuse the usage model's raw expressions verbatim.
	for name, expr := range providerReports.Bindings[0].Dimensions {
		require.Equal(t, usageRaw.Dimensions[name], expr, "provider_reports dim %s must mirror the usage raw binding", name)
	}

	messages, ok := def.Model("messages")
	require.True(t, ok)
	require.False(t, messages.Internal)
	require.Empty(t, messages.ExclusiveWith)
	require.NotContains(t, messages.Dimensions, "turn")
	require.Contains(t, messages.Dimensions, "session")
	names := make([]string, 0, len(messages.Measures))
	for _, ms := range messages.Measures {
		names = append(names, ms.Name)
	}
	require.Equal(t, []string{"messages", "chats"}, names)
	require.Len(t, messages.Bindings, 2)
	for i := range messages.Bindings {
		b := &messages.Bindings[i]
		if b.Source == "chat_session_summaries" {
			require.EqualValues(t, 172800, b.MinWindowSeconds)
		} else {
			require.Equal(t, usageRaw.RowFilter, b.RowFilter, "messages raw population is the usage model's observed population")
		}
	}
}

func TestParse_RejectsUnknownFields(t *testing.T) {
	t.Parallel()

	_, err := semantic.Parse([]byte(`{"version": 1, "dimensions": [], "models": [], "bogus": true}`))
	require.ErrorContains(t, err, "bogus")
}

func TestValidate_RejectsWrongVersion(t *testing.T) {
	t.Parallel()

	def := validDefinition()
	def.Version = 2
	require.ErrorContains(t, def.Validate(), "unsupported definition version 2")
}

func TestValidate_RejectsUnknownDimensionType(t *testing.T) {
	t.Parallel()

	def := validDefinition()
	def.Dimensions[1].Type = "uuid"
	require.ErrorContains(t, def.Validate(), `unknown type "uuid"`)
}

func TestValidate_RejectsDuplicateDimensionLegacyKeys(t *testing.T) {
	t.Parallel()

	def := validDefinition()
	def.Dimensions[1].LegacyKey = "project_id"
	require.ErrorContains(t, def.Validate(), `share legacy_key "project_id"`)
}

func TestValidate_RejectsModelDimensionOutsideCatalog(t *testing.T) {
	t.Parallel()

	def := validDefinition()
	def.Models[0].Dimensions = append(def.Models[0].Dimensions, "shade")
	require.ErrorContains(t, def.Validate(), `dimension "shade" not in the catalog`)
}

func TestValidate_RejectsUnknownMeasureType(t *testing.T) {
	t.Parallel()

	def := validDefinition()
	def.Models[0].Measures[0].Type = "decimal"
	require.ErrorContains(t, def.Validate(), `unknown scan type "decimal"`)
}

func TestValidate_RejectsDuplicateMeasureLegacyKeys(t *testing.T) {
	t.Parallel()

	def := validDefinition()
	def.Models[0].Measures[1].LegacyKey = "total_cost"
	require.ErrorContains(t, def.Validate(), `share legacy_key "total_cost"`)
}

func TestValidate_RejectsBindingDimensionNotOnModel(t *testing.T) {
	t.Parallel()

	def := validDefinition()
	def.Models[0].Bindings[0].Dimensions["shade"] = semantic.BindingExpr{SQL: "shade"}
	require.ErrorContains(t, def.Validate(), `serves dimension "shade" not declared on the model`)
}

func TestValidate_RejectsBindingMeasureWithoutSQL(t *testing.T) {
	t.Parallel()

	def := validDefinition()
	def.Models[0].Bindings[0].Measures["cost"] = semantic.BindingExpr{SQL: ""}
	require.ErrorContains(t, def.Validate(), `empty SQL for measure "cost"`)
}

func TestValidate_RejectsUnknownTimeKind(t *testing.T) {
	t.Parallel()

	def := validDefinition()
	def.Models[0].Bindings[0].Time.Kind = "day_bucket"
	require.ErrorContains(t, def.Validate(), `unknown time kind "day_bucket"`)
}

func TestValidate_RejectsAsymmetricExclusiveWith(t *testing.T) {
	t.Parallel()

	def := validDefinition()
	other := def.Models[0]
	other.Name = "other.usage"
	def.Models = append(def.Models, other)
	def.Models[0].ExclusiveWith = []string{"other.usage"}
	require.ErrorContains(t, def.Validate(), "exclusive_with is not symmetric")
}

func TestValidate_RejectsExclusiveWithUnknownModel(t *testing.T) {
	t.Parallel()

	def := validDefinition()
	def.Models[0].ExclusiveWith = []string{"ghost.usage"}
	require.ErrorContains(t, def.Validate(), `unknown model "ghost.usage"`)
}

func TestValidate_RejectsRollupOfUnknownModel(t *testing.T) {
	t.Parallel()

	def := validDefinition()
	def.Models[0].RollupOf = "ghost.usage"
	require.ErrorContains(t, def.Validate(), `rollup_of unknown model "ghost.usage"`)
}

func TestValidate_RejectsNegativeMinWindow(t *testing.T) {
	t.Parallel()

	def := validDefinition()
	def.Models[0].Bindings[0].MinWindowSeconds = -1
	require.ErrorContains(t, def.Validate(), "negative min_window_seconds")
}
