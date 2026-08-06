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

	model, ok := def.Model("turn.usage")
	require.True(t, ok, "turn.usage model must exist")
	require.Len(t, model.Dimensions, len(def.Dimensions), "turn.usage carries every catalog dimension")
	require.Len(t, model.Measures, 8)
	require.ElementsMatch(t, []string{"agent.usage", "provider.usage"}, model.ExclusiveWith)

	// The canonical measure declaration order drives the compiled SELECT order.
	names := make([]string, 0, len(model.Measures))
	for _, ms := range model.Measures {
		names = append(names, ms.Name)
	}
	require.Equal(t, []string{
		"cost_usd", "input_tokens", "output_tokens", "tokens_total",
		"cache_read_tokens", "cache_write_tokens", "tool_calls", "chats",
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
	require.Len(t, summaries.Measures, 8)
	require.Len(t, raw.Measures, 8)
}

// TestLoad_Phase2ModelShapes pins the structural facts of the phase-2 models
// that the planner's routing depends on (window-gated summaries bindings, the
// settled-only provider model, exclusivity between the usage authorities).
func TestLoad_Phase2ModelShapes(t *testing.T) {
	t.Parallel()

	def, err := semantic.Load()
	require.NoError(t, err)

	agentUsage, ok := def.Model("agent.usage")
	require.True(t, ok)
	require.Equal(t, "turn.usage", agentUsage.RollupOf)
	require.NotContains(t, agentUsage.Dimensions, "turn")
	require.Contains(t, agentUsage.Dimensions, "session")
	require.Len(t, agentUsage.Measures, 8)
	require.ElementsMatch(t, []string{"turn.usage", "provider.usage"}, agentUsage.ExclusiveWith)
	require.Len(t, agentUsage.Bindings, 2)
	var agentSummary, agentRaw *semantic.Binding
	for i := range agentUsage.Bindings {
		switch agentUsage.Bindings[i].Source {
		case "chat_session_summaries":
			agentSummary = &agentUsage.Bindings[i]
		case "telemetry_logs":
			agentRaw = &agentUsage.Bindings[i]
		}
	}
	require.NotNil(t, agentSummary)
	require.NotNil(t, agentRaw)
	require.EqualValues(t, 172800, agentSummary.MinWindowSeconds)
	require.Equal(t, semantic.TimeKindHourBucket, agentSummary.Time.Kind)
	// The summaries table only carries per-chat facts; identity dims are
	// merged arrays there and deliberately not exposed.
	require.Len(t, agentSummary.Dimensions, 2)
	require.Contains(t, agentSummary.Dimensions, "project")
	require.Contains(t, agentSummary.Dimensions, "session")
	require.Len(t, agentSummary.Measures, 8)
	// The raw binding is turn.usage's minus the turn grain.
	turnUsage, ok := def.Model("turn.usage")
	require.True(t, ok)
	var turnRaw *semantic.Binding
	for i := range turnUsage.Bindings {
		if turnUsage.Bindings[i].Source == "telemetry_logs" {
			turnRaw = &turnUsage.Bindings[i]
		}
	}
	require.NotNil(t, turnRaw)
	require.Equal(t, turnRaw.RowFilter, agentRaw.RowFilter)
	require.Equal(t, turnRaw.Measures, agentRaw.Measures)
	require.NotContains(t, agentRaw.Dimensions, "turn")
	for name, expr := range agentRaw.Dimensions {
		require.Equal(t, turnRaw.Dimensions[name], expr, "agent.usage raw dim %s must mirror turn.usage", name)
	}
	require.Len(t, agentRaw.Dimensions, len(turnRaw.Dimensions)-1)

	providerUsage, ok := def.Model("provider.usage")
	require.True(t, ok)
	require.Empty(t, providerUsage.RollupOf)
	require.NotContains(t, providerUsage.Dimensions, "session")
	require.NotContains(t, providerUsage.Dimensions, "turn")
	require.NotContains(t, providerUsage.Dimensions, "hostname")
	require.ElementsMatch(t, []string{"turn.usage", "agent.usage"}, providerUsage.ExclusiveWith)
	require.Len(t, providerUsage.Bindings, 1, "no summaries binding: the table cannot discriminate settled from observed rows")
	require.Equal(t, "telemetry_logs", providerUsage.Bindings[0].Source)
	charged, ok := providerUsage.Measure("charged_usd")
	require.True(t, ok)
	require.Equal(t, "usd", charged.Unit)
	require.Equal(t, "unavailable", charged.NullSemantics)
	// Shared dims reuse turn.usage's raw expressions verbatim.
	for name, expr := range providerUsage.Bindings[0].Dimensions {
		require.Equal(t, turnRaw.Dimensions[name], expr, "provider.usage dim %s must mirror turn.usage raw", name)
	}

	agentChat, ok := def.Model("agent.chat")
	require.True(t, ok)
	require.Empty(t, agentChat.ExclusiveWith)
	require.NotContains(t, agentChat.Dimensions, "turn")
	require.Contains(t, agentChat.Dimensions, "session")
	names := make([]string, 0, len(agentChat.Measures))
	for _, ms := range agentChat.Measures {
		names = append(names, ms.Name)
	}
	require.Equal(t, []string{"messages", "chats"}, names)
	require.Len(t, agentChat.Bindings, 2)
	for i := range agentChat.Bindings {
		b := &agentChat.Bindings[i]
		if b.Source == "chat_session_summaries" {
			require.EqualValues(t, 172800, b.MinWindowSeconds)
		} else {
			require.Equal(t, turnRaw.RowFilter, b.RowFilter, "agent.chat raw population is turn.usage's observed population")
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
