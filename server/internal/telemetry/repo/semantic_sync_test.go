package repo_test

import (
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/telemetry/repo"
	"github.com/speakeasy-api/gram/server/internal/telemetry/semantic"
)

// While the legacy Go registries in this package and the embedded semantic
// definition both exist, these tests pin them to each other so they cannot
// drift: every catalog dimension with a legacy_key must carry exactly the
// registry's physical expressions, and every usage-model measure must carry
// exactly the aggregate SELECTs / session expression constants.

// legacyQueryDimensionKeys is the public telemetry.query dimension allowlist
// (queryDimensions in server/design/telemetry/design.go) minus skill_version,
// which is served by the legacy raw+mappings path and stays outside the
// semantic model.
var legacyQueryDimensionKeys = []string{
	"department_name",
	"job_title",
	"employee_type",
	"division_name",
	"cost_center_name",
	"email",
	"hostname",
	"model",
	"hook_source",
	"account_type",
	"provider",
	"billing_mode",
	"query_source",
	"skill_name",
	"agent_name",
	"mcp_server_name",
	"mcp_tool_name",
	"role",
	"group",
	"project_id",
}

func usageBindings(t *testing.T) (def *semantic.Definition, summaries, raw *semantic.Binding) {
	t.Helper()

	def, err := semantic.Load()
	require.NoError(t, err)
	model, ok := def.Model("usage")
	require.True(t, ok, "usage model must exist")
	for i := range model.Bindings {
		switch model.Bindings[i].Source {
		case "attribute_metrics_summaries":
			summaries = &model.Bindings[i]
		case "telemetry_logs":
			raw = &model.Bindings[i]
		}
	}
	require.NotNil(t, summaries, "summaries binding must exist")
	require.NotNil(t, raw, "raw binding must exist")
	return def, summaries, raw
}

func TestSemanticDefinition_DimensionsMatchLegacyRegistry(t *testing.T) {
	t.Parallel()

	def, summaries, raw := usageBindings(t)
	registry := repo.TelemetryDimensionRegistryForTest()

	kindToType := map[string]string{
		"scalar":  semantic.DimTypeString,
		"array":   semantic.DimTypeStringArray,
		"project": semantic.DimTypeID,
	}

	coveredLegacyKeys := make([]string, 0, len(def.Dimensions))
	for _, dim := range def.Dimensions {
		if dim.LegacyKey == "" {
			continue
		}
		coveredLegacyKeys = append(coveredLegacyKeys, dim.LegacyKey)

		spec, ok := registry[dim.LegacyKey]
		require.True(t, ok, "catalog dimension %q has legacy_key %q not in telemetryDimensionRegistry", dim.Name, dim.LegacyKey)

		require.Equal(t, spec.AggregateColumn, summaries.Dimensions[dim.Name].SQL,
			"dimension %q: summaries binding SQL drifted from the registry aggregateColumn", dim.Name)
		require.Equal(t, spec.RawExpr, raw.Dimensions[dim.Name].SQL,
			"dimension %q: raw binding SQL drifted from the registry rawExpr", dim.Name)
		require.Equal(t, kindToType[spec.Kind], dim.Type,
			"dimension %q: catalog type drifted from the registry kind %q", dim.Name, spec.Kind)
		require.Equal(t, spec.EmptyIsNotApplicable, dim.EmptyMeans == "not_applicable",
			"dimension %q: empty_means drifted from the registry emptyIsNotApplicable", dim.Name)
	}

	// The legacy alias surface is exactly the design allowlist (minus
	// skill_version), which is also exactly the registry's key set.
	require.ElementsMatch(t, legacyQueryDimensionKeys, coveredLegacyKeys,
		"catalog legacy_key set drifted from the telemetry.query dimension allowlist")
	registryKeys := make([]string, 0, len(registry))
	for key := range registry {
		registryKeys = append(registryKeys, key)
	}
	require.ElementsMatch(t, legacyQueryDimensionKeys, registryKeys,
		"telemetryDimensionRegistry drifted from the design allowlist pinned here — update both")
}

func TestSemanticDefinition_SummariesMeasuresMatchLegacySelects(t *testing.T) {
	t.Parallel()

	def, summaries, _ := usageBindings(t)
	model, ok := def.Model("usage")
	require.True(t, ok)

	selects := repo.AttributeMeasureSelectsForTest()
	require.Len(t, model.Measures, len(selects),
		"usage measure count drifted from attributeMeasureSelects")

	// attributeMeasureSelects is ordered; the model declares measures in the
	// same order with catalog names, so compare pairwise after stripping the
	// legacy alias suffix.
	for i, ms := range model.Measures {
		require.NotEmpty(t, ms.LegacyKey, "measure %q needs a legacy_key while the legacy path exists", ms.Name)
		aliasSuffix := " AS m_" + ms.LegacyKey
		require.True(t, strings.HasSuffix(selects[i], aliasSuffix),
			"attributeMeasureSelects[%d] = %q does not end in %q — measure order or legacy key drifted", i, selects[i], aliasSuffix)
		wantSQL := strings.TrimSuffix(selects[i], aliasSuffix)
		require.Equal(t, wantSQL, summaries.Measures[ms.Name].SQL,
			"measure %q: summaries binding SQL drifted from attributeMeasureSelects", ms.Name)
	}
}

func TestSemanticDefinition_RawMeasuresMatchSessionExprs(t *testing.T) {
	t.Parallel()

	_, _, raw := usageBindings(t)

	want := map[string]string{
		"cost_usd":           repo.SessionCostExprForTest,
		"input_tokens":       repo.SessionInputTokensExprForTest,
		"output_tokens":      repo.SessionOutputTokensExprForTest,
		"tokens_total":       repo.SessionTotalTokensExprForTest,
		"cache_read_tokens":  repo.SessionCacheReadTokensExprForTest,
		"cache_write_tokens": repo.SessionCacheCreationTokensExprForTest,
		"tool_calls":         "uniqExactIf(" + repo.SessionToolCallDedupIDExprForTest + ", " + repo.SessionCountedToolCallPredicateForTest + ")",
		"chats":              "uniqExactIf(chat_id, chat_id != '' AND " + repo.SessionUsageMeasureFilterForTest + ")",
	}
	require.Len(t, raw.Measures, len(want))
	for name, wantSQL := range want {
		require.Equal(t, wantSQL, raw.Measures[name].SQL,
			"measure %q: raw binding SQL drifted from the sessions.go expression constants", name)
	}
}

func TestSemanticDefinition_RawGrainDimensionsMatchSessionExprs(t *testing.T) {
	t.Parallel()

	_, summaries, raw := usageBindings(t)

	// session/turn have no legacy keys (the registry doesn't carry them), so
	// pin their raw expressions here explicitly. They must never appear on the
	// hour-bucketed summaries binding.
	require.Equal(t, "chat_id", raw.Dimensions["session"].SQL)
	require.Equal(t, repo.SessionMessageIDExprForTest, raw.Dimensions["turn"].SQL,
		"turn dimension drifted from sessionMessageIDExpr")
	require.NotContains(t, summaries.Dimensions, "session")
	require.NotContains(t, summaries.Dimensions, "turn")
}

func TestSemanticDefinition_RawRowFilterIsObservedPopulation(t *testing.T) {
	t.Parallel()

	_, summaries, raw := usageBindings(t)

	require.Equal(t, "is_active = 1", summaries.RowFilter)

	// The raw binding admits the locally-OBSERVED population only. It is built
	// from the sessions.go predicates but deliberately diverges from
	// sessionSourceRowPredicate: provider-reported rows are excluded —
	// claude_chat:usage/claude_chat:cost and chatgpt:usage URNs entirely, and
	// cursor:usage/codex:usage rows stamped gram.event.source = 'api' by the
	// Cursor Admin-API poller and the OpenAI compliance COSTS import (the
	// hook writers stamp 'hook'). The provider-reports model serves the
	// settled complement.
	wantRowFilter := "(" + repo.SessionClaudeAPIRequestPredicateForTest +
		" OR " + repo.SessionClaudeToolResultPredicateForTest +
		" OR " + repo.SessionCodexAPIRequestPredicateForTest +
		" OR " + repo.SessionAgentToolCallPredicateForTest +
		" OR " + repo.SessionOpencodeUsageRowPredicateForTest +
		" OR " + repo.SessionLiteLLMUsageRowPredicateForTest +
		" OR (startsWith(gram_urn, 'cursor:usage') AND toString(attributes.gram.event.source) != 'api')" +
		" OR (startsWith(gram_urn, 'codex:usage') AND toString(attributes.gram.event.source) != 'api'))"
	require.Equal(t, wantRowFilter, raw.RowFilter,
		"raw binding row_filter drifted from the observed-population predicate")
}

// bindingBySource returns the model's binding on the given physical table.
func bindingBySource(t *testing.T, def *semantic.Definition, modelName, source string) *semantic.Binding {
	t.Helper()

	model, ok := def.Model(modelName)
	require.True(t, ok, "model %q must exist", modelName)
	for i := range model.Bindings {
		if model.Bindings[i].Source == source {
			return &model.Bindings[i]
		}
	}
	require.Failf(t, "missing binding", "model %q has no %q binding", modelName, source)
	return nil
}

// stripSummaryQualifier removes the s. table qualifier that
// sessionSummaryMeasureSelects carries (needed there because output aliases
// shadow the source column names; the semantic compiler's m_ aliases don't).
func stripSummaryQualifier(sql string) string {
	return strings.ReplaceAll(sql, "s.", "")
}

func TestSemanticDefinition_SessionSummaryBindingsMatchSessionsRepo(t *testing.T) {
	t.Parallel()

	def, err := semantic.Load()
	require.NoError(t, err)

	usageSummary := bindingBySource(t, def, "sessions", "chat_session_summaries")
	chatSummary := bindingBySource(t, def, "messages", "chat_session_summaries")

	// The window gate mirrors the ListSessions raw-vs-summary routing
	// threshold exactly.
	wantWindow := int64(repo.SessionSummaryMinWindow / time.Second)
	require.Equal(t, wantWindow, usageSummary.MinWindowSeconds,
		"sessions summaries min_window drifted from repo.SessionSummaryMinWindow")
	require.Equal(t, wantWindow, chatSummary.MinWindowSeconds,
		"messages summaries min_window drifted from repo.SessionSummaryMinWindow")

	// Where the semantic measures overlap sessionSummaryMeasureSelects, the
	// SQL must match modulo the s. qualifier.
	summarySelects := repo.SessionSummaryMeasureSelectsForTest()
	overlap := map[string]string{
		"cost_usd":           "total_cost",
		"input_tokens":       "total_input_tokens",
		"output_tokens":      "total_output_tokens",
		"tokens_total":       "total_tokens",
		"cache_read_tokens":  "cache_read_input_tokens",
		"cache_write_tokens": "cache_creation_input_tokens",
		"tool_calls":         "tool_call_count",
	}
	for name, legacyKey := range overlap {
		want, ok := summarySelects[legacyKey]
		require.True(t, ok, "sessionSummaryMeasureSelects lost key %q", legacyKey)
		require.Equal(t, stripSummaryQualifier(want), usageSummary.Measures[name].SQL,
			"sessions summaries measure %q drifted from sessionSummaryMeasureSelects[%q]", name, legacyKey)
	}
	// chats has no sessionSummaryMeasureSelects counterpart (ListSessions
	// returns per-chat rows, never a chat count); pin the literal.
	require.Equal(t, "uniqExact(chat_id)", usageSummary.Measures["chats"].SQL)

	wantMessages, ok := summarySelects["message_count"]
	require.True(t, ok)
	require.Equal(t, stripSummaryQualifier(wantMessages), chatSummary.Measures["messages"].SQL,
		"messages summaries messages drifted from sessionSummaryMeasureSelects[message_count]")
	require.Equal(t, "uniqExact(chat_id)", chatSummary.Measures["chats"].SQL)

	// Both summary bindings serve only the per-chat grain the table keys on.
	for _, b := range []*semantic.Binding{usageSummary, chatSummary} {
		require.Len(t, b.Dimensions, 2)
		require.Equal(t, "gram_project_id", b.Dimensions["project"].SQL)
		require.Equal(t, "chat_id", b.Dimensions["session"].SQL)
	}
}

func TestSemanticDefinition_AgentChatRawMeasuresMatchSessionExprs(t *testing.T) {
	t.Parallel()

	def, err := semantic.Load()
	require.NoError(t, err)

	chatRaw := bindingBySource(t, def, "messages", "telemetry_logs")
	require.Equal(t, repo.SessionMessageCountExprForTest, chatRaw.Measures["messages"].SQL,
		"messages raw messages drifted from sessionMessageCountExpr")
	require.Equal(t, "uniqExactIf(chat_id, chat_id != '')", chatRaw.Measures["chats"].SQL)
}

func TestSemanticDefinition_ProviderRowFilterIsSettledComplement(t *testing.T) {
	t.Parallel()

	def, err := semantic.Load()
	require.NoError(t, err)

	// provider_reports admits exactly the provider-reported rows the usage model's
	// raw binding excludes: the claude_chat and chatgpt URNs (polled/imported
	// only) and the cursor:usage/codex:usage rows the provider-API writers
	// stamp gram.event.source = 'api'.
	providerRaw := bindingBySource(t, def, "provider_reports", "telemetry_logs")
	require.Equal(t,
		"(startsWith(gram_urn, 'claude_chat:usage') OR startsWith(gram_urn, 'claude_chat:cost') OR startsWith(gram_urn, 'chatgpt:usage') OR (startsWith(gram_urn, 'cursor:usage') AND toString(attributes.gram.event.source) = 'api') OR (startsWith(gram_urn, 'codex:usage') AND toString(attributes.gram.event.source) = 'api'))",
		providerRaw.RowFilter,
		"provider_reports row_filter drifted from the settled complement")

	// Its dimension expressions reuse the observed raw binding's verbatim.
	usageRaw := bindingBySource(t, def, "usage", "telemetry_logs")
	for name, expr := range providerRaw.Dimensions {
		require.Equal(t, usageRaw.Dimensions[name].SQL, expr.SQL,
			"provider_reports dimension %q drifted from the usage model's raw expression", name)
	}
}
