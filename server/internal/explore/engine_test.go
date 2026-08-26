package explore

import (
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
)

func TestCompileCanonicalEventQuery(t *testing.T) {
	t.Parallel()

	projectID := uuid.MustParse("11111111-1111-1111-1111-111111111111")
	queryPlan, err := plan(Query{
		Dataset:          "events",
		Calculations:     []Calculation{{Op: "COUNT", Column: ""}},
		GroupBy:          []string{"response_model"},
		GroupExpressions: nil,
		Filters: []Filter{{
			Dimension: "provider",
			Op:        "in",
			Values:    []string{"anthropic"},
		}},
		TimeStart:          100,
		TimeEnd:            200,
		GranularitySeconds: 0,
		ProjectIDs:         []uuid.UUID{projectID},
		SortBy:             "",
		SortDesc:           false,
		Limit:              0,
	})
	require.NoError(t, err)

	sql, args, err := compile(queryPlan)
	require.NoError(t, err)

	require.Contains(t, sql, "WITH canonical AS (")
	require.Contains(t, sql, "FROM chat_events")
	require.Contains(t, sql, "project_id IN (?)")
	require.Contains(t, sql, "GROUP BY project_id, natural_id")
	require.Contains(t, sql, "argMaxIf(request_model")
	require.Contains(t, sql, "isNotNull(request_model)")
	require.Contains(t, sql, "argMaxIf(response_model")
	require.Contains(t, sql, "isNotNull(response_model)")
	require.Contains(t, sql, "ifNull(c_response_model, '') AS g_0")
	require.Contains(t, sql, "FROM canonical")
	require.NotContains(t, sql, " FINAL")
	require.NotContains(t, sql, "chat_measurements")

	canonicalEnd := strings.Index(sql, ") SELECT")
	require.Positive(t, canonicalEnd)
	require.Greater(t, strings.LastIndex(sql, "c_provider IN (?)"), canonicalEnd)
	require.Equal(t, projectID, args[0])
}

func TestCompileCanonicalUserUsageQuery(t *testing.T) {
	t.Parallel()

	queryPlan, err := plan(Query{
		Dataset: "user_usage",
		Calculations: []Calculation{
			{Op: "SUM", Column: "cost_usd"},
			{Op: "SUM", Column: "input_tokens"},
		},
		GroupBy:            []string{"response_model"},
		GroupExpressions:   nil,
		Filters:            nil,
		TimeStart:          100,
		TimeEnd:            200,
		GranularitySeconds: 60,
		ProjectIDs:         []uuid.UUID{uuid.New()},
		SortBy:             "",
		SortDesc:           false,
		Limit:              0,
	})
	require.NoError(t, err)

	sql, args, err := compile(queryPlan)
	require.NoError(t, err)

	require.Contains(t, sql, "FROM chat_measurements")
	require.Contains(t, sql, "measurement_name = ?")
	require.Contains(t, sql, "GROUP BY project_id, natural_id")
	require.Contains(t, sql, "argMaxIf(cost_usd")
	require.Contains(t, sql, "isNotNull(cost_usd)")
	require.Contains(t, sql, "sum(c_cost_usd)")
	require.Contains(t, sql, "sum(c_input_tokens)")
	require.NotContains(t, sql, "metric")
	require.NotContains(t, sql, "value")
	require.Equal(t, "user_usage", args[0])
	require.Less(t, strings.Index(sql, "measurement_name = ?"), strings.Index(sql, "GROUP BY project_id, natural_id"))
}

func TestCompileTurnUsageUsesThreeStageCanonicalization(t *testing.T) {
	t.Parallel()

	queryPlan, err := plan(Query{
		Dataset: "turn_usage",
		Calculations: []Calculation{
			{Op: "SUM", Column: "input_tokens"},
			{Op: "SUM", Column: "output_tokens"},
		},
		GroupBy:          []string{"response_model"},
		GroupExpressions: nil,
		Filters: []Filter{{
			Dimension: "provider",
			Op:        "in",
			Values:    []string{"anthropic"},
		}},
		TimeStart:          100,
		TimeEnd:            200,
		GranularitySeconds: 0,
		ProjectIDs:         []uuid.UUID{uuid.New()},
		SortBy:             "SUM(input_tokens)",
		SortDesc:           true,
		Limit:              10,
	})
	require.NoError(t, err)

	sql, args, err := compile(queryPlan)
	require.NoError(t, err)

	require.Contains(t, sql, "FROM chat_measurements")
	require.Contains(t, sql, "measurement_name = ?")
	require.Contains(t, sql, "components AS (")
	require.Contains(t, sql, "GROUP BY project_id, natural_id, source_channel, observation_kind, component_id")
	require.Contains(t, sql, "source_channel = 'provider_otel'")
	require.Contains(t, sql, "observation_kind = 'component'")
	require.Contains(t, sql, "if(countIf(isNotNull(component_input_tokens)) > 0, sumIf(component_input_tokens, isNotNull(component_input_tokens)), NULL) AS candidate_input_tokens")
	require.Contains(t, sql, "source_channel = 'agent_hook'")
	require.Contains(t, sql, "observation_kind = 'total'")
	require.Contains(t, sql, "source_candidates AS (")
	require.Contains(t, sql, "multiIf(source_channel = 'provider_otel', toUInt16(200), source_channel = 'agent_hook', toUInt16(100)")
	require.Contains(t, sql, "GROUP BY project_id, natural_id")
	require.Contains(t, sql, "sum(c_input_tokens)")
	require.Contains(t, sql, "sum(c_output_tokens)")
	require.Contains(t, sql, "ORDER BY m_0 DESC")
	require.Contains(t, sql, "LIMIT 10")
	require.Equal(t, "turn_usage", args[0])
	require.Less(t, strings.Index(sql, "measurement_name = ?"), strings.Index(sql, "GROUP BY project_id, natural_id, source_channel"))

	canonicalEnd := strings.LastIndex(sql, ") SELECT")
	require.Positive(t, canonicalEnd)
	require.Greater(t, strings.LastIndex(sql, "c_provider IN (?)"), canonicalEnd)
}

func TestExplicitZeroMeasureFiltersRequirePresence(t *testing.T) {
	t.Parallel()

	queryPlan, err := plan(Query{
		Dataset:          "turn_usage",
		Calculations:     []Calculation{{Op: "AVG", Column: "cache_read_tokens"}},
		GroupBy:          nil,
		GroupExpressions: nil,
		Filters: []Filter{{
			Dimension: "cache_read_tokens",
			Op:        "eq",
			Values:    []string{"0"},
		}},
		TimeStart:          100,
		TimeEnd:            200,
		GranularitySeconds: 0,
		ProjectIDs:         []uuid.UUID{uuid.New()},
		SortBy:             "",
		SortDesc:           false,
		Limit:              0,
	})
	require.NoError(t, err)

	sql, _, err := compile(queryPlan)
	require.NoError(t, err)

	require.Contains(t, sql, "avgIf(c_cache_read_tokens, isNotNull(c_cache_read_tokens) = 1)")
	require.Contains(t, sql, "isNotNull(c_cache_read_tokens) = 1")
	require.Contains(t, sql, "c_cache_read_tokens = ?")
}

func TestConditionalGroupCompilesAfterCanonicalization(t *testing.T) {
	t.Parallel()

	queryPlan, err := plan(Query{
		Dataset:      "events",
		Calculations: []Calculation{{Op: "COUNT", Column: ""}},
		GroupBy:      []string{"provider"},
		GroupExpressions: []GroupExpression{{
			Name:      "Is Claude",
			Dimension: "response_model",
			Op:        "in",
			Values:    []string{"claude"},
		}},
		Filters:            nil,
		TimeStart:          100,
		TimeEnd:            200,
		GranularitySeconds: 0,
		ProjectIDs:         []uuid.UUID{uuid.New()},
		SortBy:             "",
		SortDesc:           false,
		Limit:              0,
	})
	require.NoError(t, err)
	require.Equal(t, []string{"provider", "Is Claude"}, queryPlan.groupNames())

	sql, _, err := compile(queryPlan)
	require.NoError(t, err)

	canonicalEnd := strings.Index(sql, ") SELECT")
	require.Positive(t, canonicalEnd)
	expressionStart := strings.Index(sql, "if(c_response_model IN (?), 'true', 'false') AS g_1")
	require.Greater(t, expressionStart, canonicalEnd)
	require.Contains(t, sql, "GROUP BY g_0, g_1")
	require.NotContains(t, sql, "WHERE c_response_model IN")
}

func TestConditionalGroupValidation(t *testing.T) {
	t.Parallel()

	query := Query{
		Dataset:      "turn_usage",
		Calculations: []Calculation{{Op: "COUNT", Column: ""}},
		GroupBy:      []string{"response_model"},
		GroupExpressions: []GroupExpression{{
			Name:      "Is expensive",
			Dimension: "cost_usd",
			Op:        "gte",
			Values:    []string{"1"},
		}},
		Filters:            nil,
		TimeStart:          100,
		TimeEnd:            200,
		GranularitySeconds: 0,
		ProjectIDs:         nil,
		SortBy:             "",
		SortDesc:           false,
		Limit:              0,
	}

	query.GroupExpressions[0].Name = " "
	_, err := plan(query)
	require.ErrorContains(t, err, "name must not be empty")

	query.GroupExpressions = []GroupExpression{
		{Name: "response_model", Dimension: "cost_usd", Op: "gte", Values: []string{"1"}},
	}
	_, err = plan(query)
	require.ErrorContains(t, err, `group name "response_model" appears more than once`)

	query.GroupExpressions = []GroupExpression{
		{Name: "Cost band", Dimension: "cost_usd", Op: "gte", Values: []string{"1"}},
		{Name: "Cost band", Dimension: "cost_usd", Op: "lt", Values: []string{"1"}},
	}
	_, err = plan(query)
	require.ErrorContains(t, err, `group name "Cost band" appears more than once`)

	query.GroupExpressions = []GroupExpression{{
		Name:      "Unknown",
		Dimension: "not_a_field",
		Op:        "in",
		Values:    []string{"value"},
	}}
	_, err = plan(query)
	require.ErrorContains(t, err, `unknown field "not_a_field"`)

	query.GroupExpressions = []GroupExpression{{
		Name:      "Invalid operator",
		Dimension: "response_model",
		Op:        "gt",
		Values:    []string{"1"},
	}}
	_, err = plan(query)
	require.ErrorContains(t, err, `operator "gt" is not legal on string field "response_model"`)

	query.GroupExpressions = []GroupExpression{{
		Name:      "Malformed value",
		Dimension: "cost_usd",
		Op:        "gte",
		Values:    []string{"NaN"},
	}}
	_, err = plan(query)
	require.ErrorContains(t, err, `"NaN" is not a finite number`)
}

func TestCatalogExposesOnlyGrainFirstDatasets(t *testing.T) {
	t.Parallel()

	require.Len(t, datasets, 3)
	require.Equal(t, []string{"events", "turn_usage", "user_usage"}, []string{
		datasets[0].Name,
		datasets[1].Name,
		datasets[2].Name,
	})
	require.Equal(t, []string{"Events", "Turn usage", "User usage"}, []string{
		datasets[0].Label,
		datasets[1].Label,
		datasets[2].Label,
	})
	require.Equal(t, []string{"event", "usage", "usage"}, []string{
		datasets[0].Category,
		datasets[1].Category,
		datasets[2].Category,
	})

	for _, removed := range []string{"costs", "tokens", "measurements"} {
		_, ok := datasetByName(removed)
		require.False(t, ok, removed)
	}
}

func TestCatalogUsesDirectWideMeasures(t *testing.T) {
	t.Parallel()

	for _, datasetName := range []string{"turn_usage", "user_usage"} {
		dataset, ok := datasetByName(datasetName)
		require.True(t, ok)
		require.Equal(t, "chat_measurements", dataset.Table)
		require.Equal(t, datasetName, dataset.MeasurementName)
		require.Equal(t, []string{
			"cost_usd",
			"input_tokens",
			"output_tokens",
			"cache_read_tokens",
			"cache_write_tokens",
		}, dataset.measureNames())
		for _, measureName := range dataset.measureNames() {
			field, ok := dataset.fieldByName(measureName)
			require.True(t, ok)
			require.Equal(t, measureName, field.Expr)
			require.NotEmpty(t, field.Label)
		}
	}
}

func TestUserUsageExposesReportingInterval(t *testing.T) {
	t.Parallel()

	dataset, ok := datasetByName("user_usage")
	require.True(t, ok)

	column, ok := dataset.dimensionColumn("granularity")
	require.True(t, ok)
	require.Equal(t, "c_granularity", column)

	field, ok := dataset.fieldByName("granularity")
	require.True(t, ok)
	require.Equal(t, "Reporting interval", field.Label)
}

func TestCatalogSeparatesRequestAndResponseModels(t *testing.T) {
	t.Parallel()

	for _, dataset := range datasets {
		_, hasLegacyModel := dataset.fieldByName("model")
		require.False(t, hasLegacyModel, dataset.Name)

		requestModel, ok := dataset.fieldByName("request_model")
		require.True(t, ok, dataset.Name)
		require.Equal(t, "Request model", requestModel.Label)

		responseModel, ok := dataset.fieldByName("response_model")
		require.True(t, ok, dataset.Name)
		require.Equal(t, "Response model", responseModel.Label)
	}
}

func TestCatalogDatasetCopyUsesSourceAgnosticProductLanguage(t *testing.T) {
	t.Parallel()

	bannedTerms := []string{
		"api",
		"channel",
		"chat_events",
		"chat_measurements",
		"eav",
		"hook",
		"ingest",
		"mcp",
		"provider",
		"source",
	}
	for _, dataset := range datasets {
		require.NotEmpty(t, dataset.Label)
		require.NotEmpty(t, dataset.Description)
		require.NotEmpty(t, dataset.Grain)
		for _, text := range []string{dataset.Description, dataset.Grain} {
			for _, term := range bannedTerms {
				require.NotContains(t, strings.ToLower(text), term, dataset.Name)
			}
		}
		for _, field := range dataset.Fields {
			require.NotEmpty(t, field.Label, dataset.Name+"."+field.Name)
			require.NotEmpty(t, field.Description, dataset.Name+"."+field.Name)
		}
	}
}

func TestUsageAuthorityPrefersProviderOTelToAgentHook(t *testing.T) {
	t.Parallel()

	rank := authorityRankExpr("input_tokens")
	require.Less(t, strings.Index(rank, "provider_otel"), strings.Index(rank, "agent_hook"))
}

func TestLegacyDatasetIsRejected(t *testing.T) {
	t.Parallel()

	_, err := plan(Query{
		Dataset:            "costs",
		Calculations:       []Calculation{{Op: "SUM", Column: "cost_usd"}},
		GroupBy:            nil,
		GroupExpressions:   nil,
		Filters:            nil,
		TimeStart:          100,
		TimeEnd:            200,
		GranularitySeconds: 0,
		ProjectIDs:         nil,
		SortBy:             "",
		SortDesc:           false,
		Limit:              0,
	})
	require.Error(t, err)

	var unknown *UnknownMemberError
	require.ErrorAs(t, err, &unknown)
	require.Equal(t, "dataset", unknown.Kind)
}

func TestTurnUsageCanonicalizationDeduplicatesSumsAndAppliesAuthority(t *testing.T) {
	t.Parallel()

	conn := newExploreTestClickhouse(t)
	projectID := uuid.New()
	occurredAt := time.Now().UTC().Add(-time.Minute)
	naturalID := "turn:session-1:turn-1"

	insertTurnUsageObservation(t, conn, turnUsageObservation{
		ProjectID:        projectID,
		NaturalID:        naturalID,
		ComponentID:      "request-1",
		SourceEventID:    uuid.New(),
		OccurredAt:       occurredAt,
		ObservedAt:       occurredAt.Add(time.Second),
		SourceChannel:    "provider_otel",
		ObservationKind:  "component",
		Provider:         "anthropic",
		RequestModel:     "provider-request-model",
		ResponseModel:    "provider-response-model",
		SessionID:        "session-1",
		TurnID:           "turn-1",
		CostUSD:          nil,
		InputTokens:      10,
		OutputTokens:     2,
		CacheReadTokens:  nil,
		CacheWriteTokens: nil,
	})
	insertTurnUsageObservation(t, conn, turnUsageObservation{
		ProjectID:        projectID,
		NaturalID:        naturalID,
		ComponentID:      "request-1",
		SourceEventID:    uuid.New(),
		OccurredAt:       occurredAt,
		ObservedAt:       occurredAt.Add(2 * time.Second),
		SourceChannel:    "provider_otel",
		ObservationKind:  "component",
		Provider:         "anthropic",
		RequestModel:     "provider-request-model",
		ResponseModel:    "provider-response-model",
		SessionID:        "session-1",
		TurnID:           "turn-1",
		CostUSD:          nil,
		InputTokens:      12,
		OutputTokens:     nil,
		CacheReadTokens:  0,
		CacheWriteTokens: nil,
	})
	insertTurnUsageObservation(t, conn, turnUsageObservation{
		ProjectID:        projectID,
		NaturalID:        naturalID,
		ComponentID:      "request-2",
		SourceEventID:    uuid.New(),
		OccurredAt:       occurredAt.Add(time.Second),
		ObservedAt:       occurredAt.Add(3 * time.Second),
		SourceChannel:    "provider_otel",
		ObservationKind:  "component",
		Provider:         "anthropic",
		RequestModel:     "provider-request-model",
		ResponseModel:    "provider-response-model",
		SessionID:        "session-1",
		TurnID:           "turn-1",
		CostUSD:          nil,
		InputTokens:      5,
		OutputTokens:     3,
		CacheReadTokens:  nil,
		CacheWriteTokens: nil,
	})
	insertTurnUsageObservation(t, conn, turnUsageObservation{
		ProjectID:        projectID,
		NaturalID:        naturalID,
		ComponentID:      "completed-turn",
		SourceEventID:    uuid.New(),
		OccurredAt:       occurredAt.Add(2 * time.Second),
		ObservedAt:       occurredAt.Add(4 * time.Second),
		SourceChannel:    "agent_hook",
		ObservationKind:  "total",
		Provider:         "anthropic",
		RequestModel:     "hook-request-model",
		ResponseModel:    "hook-response-model",
		SessionID:        "session-1",
		TurnID:           "turn-1",
		CostUSD:          1.25,
		InputTokens:      100,
		OutputTokens:     100,
		CacheReadTokens:  nil,
		CacheWriteTokens: 7,
	})

	queryPlan, err := plan(Query{
		Dataset: "turn_usage",
		Calculations: []Calculation{
			{Op: "SUM", Column: "cost_usd"},
			{Op: "SUM", Column: "input_tokens"},
			{Op: "SUM", Column: "output_tokens"},
			{Op: "AVG", Column: "cache_read_tokens"},
			{Op: "SUM", Column: "cache_write_tokens"},
		},
		GroupBy:          []string{"response_model"},
		GroupExpressions: nil,
		Filters: []Filter{{
			Dimension: "cache_read_tokens",
			Op:        "eq",
			Values:    []string{"0"},
		}},
		TimeStart:          occurredAt.Add(-time.Second).UnixNano(),
		TimeEnd:            occurredAt.Add(time.Minute).UnixNano(),
		GranularitySeconds: 0,
		ProjectIDs:         []uuid.UUID{projectID},
		SortBy:             "",
		SortDesc:           false,
		Limit:              0,
	})
	require.NoError(t, err)

	result, err := execute(t.Context(), conn, queryPlan)
	require.NoError(t, err)
	require.Equal(t, "turn_usage", result.Dataset)
	require.Len(t, result.Rows, 1)
	require.Equal(t, []string{"provider-response-model"}, result.Rows[0].Group)
	require.InDelta(t, 1.25, result.Rows[0].Values["SUM(cost_usd)"], 0.000001)
	require.InDelta(t, 17, result.Rows[0].Values["SUM(input_tokens)"], 0.000001)
	require.InDelta(t, 5, result.Rows[0].Values["SUM(output_tokens)"], 0.000001)
	require.InDelta(t, 0, result.Rows[0].Values["AVG(cache_read_tokens)"], 0.000001)
	require.InDelta(t, 7, result.Rows[0].Values["SUM(cache_write_tokens)"], 0.000001)

	values, err := dimensionValues(
		t.Context(),
		conn,
		[]uuid.UUID{projectID},
		"turn_usage",
		"response_model",
		occurredAt.Add(-time.Second).UnixNano(),
		occurredAt.Add(time.Minute).UnixNano(),
	)
	require.NoError(t, err)
	require.Equal(t, []string{"provider-response-model"}, values)

	values, err = dimensionValues(
		t.Context(),
		conn,
		[]uuid.UUID{projectID},
		"turn_usage",
		"request_model",
		occurredAt.Add(-time.Second).UnixNano(),
		occurredAt.Add(time.Minute).UnixNano(),
	)
	require.NoError(t, err)
	require.Equal(t, []string{"provider-request-model"}, values)
}

func TestUserUsageCanonicalizationMergesPartialReportsAndExplicitZero(t *testing.T) {
	t.Parallel()

	conn := newExploreTestClickhouse(t)
	projectID := uuid.New()
	occurredAt := time.Now().UTC().Add(-time.Minute)
	naturalID := "provider-report:shared"

	insertUserUsageObservation(t, conn, userUsageObservation{
		ProjectID:        projectID,
		NaturalID:        naturalID,
		SourceEventID:    uuid.New(),
		OccurredAt:       occurredAt,
		ObservedAt:       occurredAt.Add(time.Second),
		Provider:         "anthropic",
		RequestModel:     "request-model-1",
		ResponseModel:    "response-model-1",
		CostUSD:          1.25,
		InputTokens:      nil,
		OutputTokens:     nil,
		CacheReadTokens:  nil,
		CacheWriteTokens: nil,
	})
	insertUserUsageObservation(t, conn, userUsageObservation{
		ProjectID:        projectID,
		NaturalID:        naturalID,
		SourceEventID:    uuid.New(),
		OccurredAt:       occurredAt,
		ObservedAt:       occurredAt.Add(2 * time.Second),
		Provider:         "anthropic",
		RequestModel:     "request-model-1",
		ResponseModel:    "response-model-1",
		CostUSD:          nil,
		InputTokens:      100,
		OutputTokens:     nil,
		CacheReadTokens:  0,
		CacheWriteTokens: nil,
	})
	insertTurnUsageObservation(t, conn, turnUsageObservation{
		ProjectID:        projectID,
		NaturalID:        naturalID,
		ComponentID:      "cross-grain-request",
		SourceEventID:    uuid.New(),
		OccurredAt:       occurredAt,
		ObservedAt:       occurredAt.Add(3 * time.Second),
		SourceChannel:    "provider_otel",
		ObservationKind:  "component",
		Provider:         "anthropic",
		RequestModel:     "turn-only-request-model",
		ResponseModel:    "turn-only-response-model",
		SessionID:        "session-cross-grain",
		TurnID:           "turn-cross-grain",
		CostUSD:          nil,
		InputTokens:      nil,
		OutputTokens:     999,
		CacheReadTokens:  nil,
		CacheWriteTokens: nil,
	})

	queryPlan, err := plan(Query{
		Dataset: "user_usage",
		Calculations: []Calculation{
			{Op: "SUM", Column: "cost_usd"},
			{Op: "SUM", Column: "input_tokens"},
			{Op: "SUM", Column: "output_tokens"},
			{Op: "AVG", Column: "cache_read_tokens"},
		},
		GroupBy:          []string{"response_model"},
		GroupExpressions: nil,
		Filters: []Filter{{
			Dimension: "cache_read_tokens",
			Op:        "eq",
			Values:    []string{"0"},
		}},
		TimeStart:          occurredAt.Add(-time.Second).UnixNano(),
		TimeEnd:            occurredAt.Add(time.Minute).UnixNano(),
		GranularitySeconds: 0,
		ProjectIDs:         []uuid.UUID{projectID},
		SortBy:             "",
		SortDesc:           false,
		Limit:              0,
	})
	require.NoError(t, err)

	result, err := execute(t.Context(), conn, queryPlan)
	require.NoError(t, err)
	require.Equal(t, "user_usage", result.Dataset)
	require.Len(t, result.Rows, 1)
	require.Equal(t, []string{"response-model-1"}, result.Rows[0].Group)
	require.InDelta(t, 1.25, result.Rows[0].Values["SUM(cost_usd)"], 0.000001)
	require.InDelta(t, 100, result.Rows[0].Values["SUM(input_tokens)"], 0.000001)
	require.InDelta(t, 0, result.Rows[0].Values["SUM(output_tokens)"], 0.000001)
	require.InDelta(t, 0, result.Rows[0].Values["AVG(cache_read_tokens)"], 0.000001)

	values, err := dimensionValues(
		t.Context(),
		conn,
		[]uuid.UUID{projectID},
		"user_usage",
		"response_model",
		occurredAt.Add(-time.Second).UnixNano(),
		occurredAt.Add(time.Minute).UnixNano(),
	)
	require.NoError(t, err)
	require.Equal(t, []string{"response-model-1"}, values)
}

func TestConditionalGroupExecutionPreservesMatchingAndOtherRows(t *testing.T) {
	t.Parallel()

	conn := newExploreTestClickhouse(t)
	projectID := uuid.New()
	occurredAt := time.Now().UTC().Add(-time.Minute)
	for i, responseModel := range []string{"claude", "other", "other"} {
		insertExploreEventObservation(t, conn, exploreEventObservation{
			ProjectID:     projectID,
			NaturalID:     fmt.Sprintf("request:conditional-group:%d", i),
			SourceEventID: uuid.New(),
			OccurredAt:    occurredAt.Add(time.Duration(i) * time.Second),
			ObservedAt:    occurredAt.Add(time.Duration(i) * time.Second),
			SourceChannel: "provider_otel",
			EventName:     "api_request",
			Provider:      "anthropic",
			RequestModel:  nil,
			ResponseModel: responseModel,
			ToolName:      nil,
		})
	}

	queryPlan, err := plan(Query{
		Dataset:      "events",
		Calculations: []Calculation{{Op: "COUNT", Column: ""}},
		GroupBy:      []string{"provider"},
		GroupExpressions: []GroupExpression{{
			Name:      "Is Claude",
			Dimension: "response_model",
			Op:        "in",
			Values:    []string{"claude"},
		}},
		Filters:            nil,
		TimeStart:          occurredAt.Add(-time.Second).UnixNano(),
		TimeEnd:            occurredAt.Add(time.Minute).UnixNano(),
		GranularitySeconds: 0,
		ProjectIDs:         []uuid.UUID{projectID},
		SortBy:             "",
		SortDesc:           false,
		Limit:              0,
	})
	require.NoError(t, err)

	result, err := execute(t.Context(), conn, queryPlan)
	require.NoError(t, err)
	require.Len(t, result.Rows, 2)
	require.Equal(t, []string{"anthropic", "false"}, result.Rows[0].Group)
	require.InDelta(t, 2, result.Rows[0].Values["COUNT"], 0.000001)
	require.Equal(t, []string{"anthropic", "true"}, result.Rows[1].Group)
	require.InDelta(t, 1, result.Rows[1].Values["COUNT"], 0.000001)
	require.InDelta(t, 3, result.Rows[0].Values["COUNT"]+result.Rows[1].Values["COUNT"], 0.000001)
}

type turnUsageObservation struct {
	ProjectID        uuid.UUID
	NaturalID        string
	ComponentID      string
	SourceEventID    uuid.UUID
	OccurredAt       time.Time
	ObservedAt       time.Time
	SourceChannel    string
	ObservationKind  string
	Provider         any
	RequestModel     any
	ResponseModel    any
	SessionID        any
	TurnID           any
	CostUSD          any
	InputTokens      any
	OutputTokens     any
	CacheReadTokens  any
	CacheWriteTokens any
}

func insertTurnUsageObservation(t *testing.T, conn Conn, observation turnUsageObservation) {
	t.Helper()

	insertExploreMeasurementObservation(t, conn, exploreMeasurementObservation{
		ProjectID:        observation.ProjectID,
		MeasurementName:  "turn_usage",
		NaturalID:        observation.NaturalID,
		ComponentID:      observation.ComponentID,
		SourceEventID:    observation.SourceEventID,
		OccurredAt:       observation.OccurredAt,
		ObservedAt:       observation.ObservedAt,
		SourceChannel:    observation.SourceChannel,
		ObservationKind:  observation.ObservationKind,
		Granularity:      nil,
		Provider:         observation.Provider,
		Surface:          nil,
		AccountType:      nil,
		UserKey:          nil,
		SessionID:        observation.SessionID,
		TurnID:           observation.TurnID,
		QuerySource:      nil,
		RequestModel:     observation.RequestModel,
		ResponseModel:    observation.ResponseModel,
		CostUSD:          observation.CostUSD,
		InputTokens:      observation.InputTokens,
		OutputTokens:     observation.OutputTokens,
		CacheReadTokens:  observation.CacheReadTokens,
		CacheWriteTokens: observation.CacheWriteTokens,
	})
}

type userUsageObservation struct {
	ProjectID        uuid.UUID
	NaturalID        string
	SourceEventID    uuid.UUID
	OccurredAt       time.Time
	ObservedAt       time.Time
	Provider         any
	RequestModel     any
	ResponseModel    any
	CostUSD          any
	InputTokens      any
	OutputTokens     any
	CacheReadTokens  any
	CacheWriteTokens any
}

func insertUserUsageObservation(t *testing.T, conn Conn, observation userUsageObservation) {
	t.Helper()

	insertExploreMeasurementObservation(t, conn, exploreMeasurementObservation{
		ProjectID:        observation.ProjectID,
		MeasurementName:  "user_usage",
		NaturalID:        observation.NaturalID,
		ComponentID:      "test-provider-report",
		SourceEventID:    observation.SourceEventID,
		OccurredAt:       observation.OccurredAt,
		ObservedAt:       observation.ObservedAt,
		SourceChannel:    "provider_api",
		ObservationKind:  "report",
		Granularity:      "minute",
		Provider:         observation.Provider,
		Surface:          nil,
		AccountType:      nil,
		UserKey:          nil,
		SessionID:        nil,
		TurnID:           nil,
		QuerySource:      nil,
		RequestModel:     observation.RequestModel,
		ResponseModel:    observation.ResponseModel,
		CostUSD:          observation.CostUSD,
		InputTokens:      observation.InputTokens,
		OutputTokens:     observation.OutputTokens,
		CacheReadTokens:  observation.CacheReadTokens,
		CacheWriteTokens: observation.CacheWriteTokens,
	})
}
