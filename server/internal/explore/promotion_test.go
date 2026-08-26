package explore

import (
	"maps"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/o11y"
)

func TestAnthropicProviderOTelAPIRequestPromotesOneEventAndWideTurnComponent(t *testing.T) {
	t.Parallel()

	conn := newExploreTestClickhouse(t)
	projectID := uuid.New()
	occurredAt := time.Now().UTC().Add(-time.Minute)

	insertExploreOTelLog(t, conn, exploreOTelLog{
		ProjectID:  projectID,
		OccurredAt: occurredAt,
		Source:     "claude-code",
		EventName:  "api_request",
		Attributes: map[string]any{
			"request_id":            "request-1",
			"session.id":            "session-1",
			"prompt.id":             "turn-1",
			"gram.provider":         "anthropic",
			"gram.account_type":     "team",
			"user.email":            "user@example.test",
			"gen_ai.request.model":  "request-model-1",
			"gen_ai.response.model": "response-model-1",
			"cost_usd":              1.25,
			"input_tokens":          100,
			"output_tokens":         25,
			"cache_read_tokens":     0,
			"cache_creation_tokens": 5,
		},
	})

	var eventCount uint64
	var eventName string
	err := conn.QueryRow(t.Context(), `
		SELECT count(), any(event_name)
		FROM chat_events
		WHERE natural_id = 'request:request-1'
	`).Scan(&eventCount, &eventName)
	require.NoError(t, err)
	require.Equal(t, uint64(1), eventCount)
	require.Equal(t, "api_request", eventName)

	var usageCount uint64
	var measurementName string
	var observationKind string
	var cacheReadIsPresent bool
	var costUSD float64
	var inputTokens uint64
	var outputTokens uint64
	var cacheReadTokens uint64
	var cacheWriteTokens uint64
	err = conn.QueryRow(t.Context(), `
		SELECT
			count(),
			any(measurement_name),
			any(toString(observation_kind)),
			countIf(isNotNull(cache_read_tokens)) = 1,
			any(toFloat64(cost_usd)),
			any(input_tokens),
			any(output_tokens),
			any(cache_read_tokens),
			any(cache_write_tokens)
		FROM chat_measurements
		WHERE measurement_name = 'turn_usage'
		  AND natural_id = 'turn:9:session-1:6:turn-1'
	`).Scan(
		&usageCount,
		&measurementName,
		&observationKind,
		&cacheReadIsPresent,
		&costUSD,
		&inputTokens,
		&outputTokens,
		&cacheReadTokens,
		&cacheWriteTokens,
	)
	require.NoError(t, err)
	require.Equal(t, uint64(1), usageCount)
	require.Equal(t, "turn_usage", measurementName)
	require.Equal(t, "component", observationKind)
	require.True(t, cacheReadIsPresent)
	require.InDelta(t, 1.25, costUSD, 0.00000001)
	require.Equal(t, uint64(100), inputTokens)
	require.Equal(t, uint64(25), outputTokens)
	require.Zero(t, cacheReadTokens)
	require.Equal(t, uint64(5), cacheWriteTokens)
}

func TestAnthropicProviderOTelRedeliveryDoesNotDoubleCountCanonicalFacts(t *testing.T) {
	t.Parallel()

	conn := newExploreTestClickhouse(t)
	projectID := uuid.New()
	occurredAt := time.Now().UTC().Add(-time.Minute)
	log := exploreOTelLog{
		ProjectID:  projectID,
		OccurredAt: occurredAt,
		ObservedAt: occurredAt.Add(time.Second),
		Source:     "claude-code",
		EventName:  "api_request",
		Attributes: map[string]any{
			"request_id":    "request-redelivered",
			"session.id":    "session-redelivered",
			"prompt.id":     "turn-redelivered",
			"gram.provider": "anthropic",
			"input_tokens":  10,
		},
	}
	insertExploreOTelLog(t, conn, log)
	insertExploreOTelLog(t, conn, log)

	var rawEventObservations uint64
	err := conn.QueryRow(t.Context(), `
		SELECT count()
		FROM chat_events
		WHERE natural_id = 'request:request-redelivered'
	`).Scan(&rawEventObservations)
	require.NoError(t, err)
	require.Equal(t, uint64(2), rawEventObservations)

	eventPlan, err := plan(Query{
		Dataset:            "events",
		Calculations:       []Calculation{{Op: "COUNT", Column: ""}},
		GroupBy:            nil,
		GroupExpressions:   nil,
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
	eventResult, err := execute(t.Context(), conn, eventPlan)
	require.NoError(t, err)
	require.Len(t, eventResult.Rows, 1)
	require.InDelta(t, 1, eventResult.Rows[0].Values["COUNT"], 0.000001)

	turnPlan, err := plan(Query{
		Dataset:            "turn_usage",
		Calculations:       []Calculation{{Op: "SUM", Column: "input_tokens"}},
		GroupBy:            nil,
		GroupExpressions:   nil,
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
	turnResult, err := execute(t.Context(), conn, turnPlan)
	require.NoError(t, err)
	require.Len(t, turnResult.Rows, 1)
	require.InDelta(t, 10, turnResult.Rows[0].Values["SUM(input_tokens)"], 0.000001)
}

func TestAnthropicProviderGrainMVsHandleBothPhysicalSources(t *testing.T) {
	t.Parallel()

	conn := newExploreTestClickhouse(t)
	projectID := uuid.New()
	occurredAt := time.Now().UTC().Add(-time.Minute)

	insertExploreOTelLog(t, conn, exploreOTelLog{
		ProjectID:  projectID,
		OccurredAt: occurredAt,
		Source:     "claude-code",
		EventName:  "api_request",
		Attributes: map[string]any{
			"request_id":            "request-mixed-block",
			"session.id":            "session-mixed-block",
			"prompt.id":             "turn-mixed-block",
			"gram.provider":         "anthropic",
			"gen_ai.request.model":  "request-model",
			"gen_ai.response.model": "response-model",
			"input_tokens":          10,
		},
	})
	insertExploreOTelLog(t, conn, exploreOTelLog{
		ProjectID:  projectID,
		OccurredAt: occurredAt.Add(time.Second),
		Source:     "gram-server",
		EventName:  "AfterAgentResponse",
		Attributes: map[string]any{
			"gram.event.urn":                 "urn:telemetry:agent_hook:log:afteragentresponse",
			"gram.hook.event":                "AfterAgentResponse",
			"gram.hook.source":               "claude-code",
			"gram.hook.turn_id":              "turn-mixed-block",
			"gram.hook.usage.present_fields": []string{"input_tokens"},
			"gram.provider":                  "anthropic",
			"gen_ai.conversation.id":         "session-mixed-block",
			"gen_ai.usage.input_tokens":      10,
		},
	})
	insertExploreOTelLog(t, conn, exploreOTelLog{
		ProjectID:  projectID,
		OccurredAt: occurredAt.Add(2 * time.Second),
		Source:     "gram-server",
		EventName:  "AfterAgentResponse",
		Attributes: map[string]any{
			"gram.event.urn":                 "urn:telemetry:agent_hook:log:afteragentresponse",
			"gram.hook.event":                "AfterAgentResponse",
			"gram.hook.source":               "claude-code",
			"gram.hook.turn_id":              "turn-other-provider",
			"gram.hook.usage.present_fields": []string{"input_tokens"},
			"gram.provider":                  "openai",
			"gen_ai.conversation.id":         "session-other-provider",
			"gen_ai.usage.input_tokens":      20,
		},
	})

	var eventCount uint64
	var providerOTelEvents uint64
	var anthropicHookEvents uint64
	err := conn.QueryRow(t.Context(), `
		SELECT
			count(),
			countIf(source_channel = 'provider_otel'),
			countIf(source_channel = 'agent_hook')
		FROM chat_events
		WHERE project_id = ?
	`, projectID).Scan(
		&eventCount,
		&providerOTelEvents,
		&anthropicHookEvents,
	)
	require.NoError(t, err)
	require.Equal(t, uint64(2), eventCount)
	require.Equal(t, uint64(1), providerOTelEvents)
	require.Equal(t, uint64(1), anthropicHookEvents)

	var measurementCount uint64
	var providerOTelMeasurements uint64
	var anthropicHookMeasurements uint64
	err = conn.QueryRow(t.Context(), `
		SELECT
			count(),
			countIf(source_channel = 'provider_otel'),
			countIf(source_channel = 'agent_hook')
		FROM chat_measurements
		WHERE measurement_name = 'turn_usage'
		  AND project_id = ?
	`, projectID).Scan(
		&measurementCount,
		&providerOTelMeasurements,
		&anthropicHookMeasurements,
	)
	require.NoError(t, err)
	require.Equal(t, uint64(2), measurementCount)
	require.Equal(t, uint64(1), providerOTelMeasurements)
	require.Equal(t, uint64(1), anthropicHookMeasurements)
}

func TestExplorePromotionUsesProviderGrainMaterializedViews(t *testing.T) {
	t.Parallel()

	conn := newExploreTestClickhouse(t)
	rows, err := conn.Query(t.Context(), `
		SELECT name, create_table_query
		FROM system.tables
		WHERE database = currentDatabase()
		  AND (
		      startsWith(name, 'chat_events_')
		      OR startsWith(name, 'chat_measurements_')
		  )
		  AND endsWith(name, '_mv')
		ORDER BY name
	`)
	require.NoError(t, err)
	defer o11y.NoLogDefer(func() error { return rows.Close() })

	var names []string
	var definitions []string
	for rows.Next() {
		var name string
		var definition string
		require.NoError(t, rows.Scan(&name, &definition))
		names = append(names, name)
		definitions = append(definitions, definition)
	}
	require.NoError(t, rows.Err())
	require.Equal(t, []string{
		"chat_events_anthropic_agent_hook_mv",
		"chat_events_anthropic_provider_otel_mv",
		"chat_measurements_anthropic_turn_usage_agent_hook_mv",
		"chat_measurements_anthropic_turn_usage_provider_otel_mv",
		"chat_measurements_anthropic_user_usage_provider_api_mv",
	}, names)
	for i, definition := range definitions {
		require.Contains(t, definition, "otel_logs", names[i])
	}
}

func TestExploreModelColumnsAreSeparateAndNullable(t *testing.T) {
	t.Parallel()

	conn := newExploreTestClickhouse(t)
	rows, err := conn.Query(t.Context(), `
		SELECT table, name, type
		FROM system.columns
		WHERE database = currentDatabase()
		  AND table IN ('chat_events', 'chat_measurements')
		  AND name IN ('model', 'request_model', 'response_model')
		ORDER BY table, name
	`)
	require.NoError(t, err)
	defer o11y.NoLogDefer(func() error { return rows.Close() })

	var columns []string
	for rows.Next() {
		var table string
		var name string
		var columnType string
		require.NoError(t, rows.Scan(&table, &name, &columnType))
		columns = append(columns, table+"."+name+" "+columnType)
	}
	require.NoError(t, rows.Err())
	require.Equal(t, []string{
		"chat_events.request_model LowCardinality(Nullable(String))",
		"chat_events.response_model LowCardinality(Nullable(String))",
		"chat_measurements.request_model LowCardinality(Nullable(String))",
		"chat_measurements.response_model LowCardinality(Nullable(String))",
	}, columns)
}

func TestAnthropicPromotionKeepsRequestAndResponseModelsSeparate(t *testing.T) {
	t.Parallel()

	conn := newExploreTestClickhouse(t)
	projectID := uuid.New()

	insertExploreOTelLog(t, conn, exploreOTelLog{
		ProjectID:  projectID,
		OccurredAt: time.Now().UTC().Add(-time.Minute),
		Source:     "claude-code",
		EventName:  "api_request",
		Attributes: map[string]any{
			"request_id":            "request-models",
			"gram.provider":         "",
			"session.id":            "session-models",
			"prompt.id":             "turn-models",
			"model":                 "",
			"gen_ai.response.model": "response-model",
			"input_tokens":          1,
		},
	})

	var provider string
	var requestModel string
	var responseModel string
	var providerIsPresent bool
	var requestModelIsPresent bool
	var responseModelIsPresent bool
	var surface string
	err := conn.QueryRow(t.Context(), `
		SELECT
			any(provider),
			any(request_model),
			any(response_model),
			countIf(isNotNull(provider)) = 1,
			countIf(isNotNull(request_model)) = 1,
			countIf(isNotNull(response_model)) = 1,
			any(surface)
		FROM chat_events
		WHERE natural_id = 'request:request-models'
	`).Scan(
		&provider,
		&requestModel,
		&responseModel,
		&providerIsPresent,
		&requestModelIsPresent,
		&responseModelIsPresent,
		&surface,
	)
	require.NoError(t, err)
	require.Empty(t, provider)
	require.Empty(t, requestModel)
	require.Equal(t, "response-model", responseModel)
	require.True(t, providerIsPresent)
	require.True(t, requestModelIsPresent)
	require.True(t, responseModelIsPresent)
	require.Equal(t, "claude-code", surface)

	var measurementRequestModel string
	var measurementResponseModel string
	var measurementRequestModelIsPresent bool
	var measurementResponseModelIsPresent bool
	err = conn.QueryRow(t.Context(), `
		SELECT
			any(request_model),
			any(response_model),
			countIf(isNotNull(request_model)) = 1,
			countIf(isNotNull(response_model)) = 1
		FROM chat_measurements
		WHERE measurement_name = 'turn_usage'
		  AND natural_id = 'turn:14:session-models:11:turn-models'
	`).Scan(
		&measurementRequestModel,
		&measurementResponseModel,
		&measurementRequestModelIsPresent,
		&measurementResponseModelIsPresent,
	)
	require.NoError(t, err)
	require.Empty(t, measurementRequestModel)
	require.Equal(t, "response-model", measurementResponseModel)
	require.True(t, measurementRequestModelIsPresent)
	require.True(t, measurementResponseModelIsPresent)
}

func TestAnthropicProviderOTelAPIRequestWithoutTurnIdentityRemainsEventOnly(t *testing.T) {
	t.Parallel()

	conn := newExploreTestClickhouse(t)
	projectID := uuid.New()

	insertExploreOTelLog(t, conn, exploreOTelLog{
		ProjectID:  projectID,
		OccurredAt: time.Now().UTC().Add(-time.Minute),
		Source:     "claude-code",
		EventName:  "api_request",
		Attributes: map[string]any{
			"request_id":   "uncorrelated-request",
			"input_tokens": 100,
		},
	})

	var eventCount uint64
	err := conn.QueryRow(t.Context(), "SELECT count() FROM chat_events WHERE project_id = ?", projectID).Scan(&eventCount)
	require.NoError(t, err)
	require.Equal(t, uint64(1), eventCount)

	var usageCount uint64
	err = conn.QueryRow(t.Context(), "SELECT count() FROM chat_measurements WHERE measurement_name = 'turn_usage' AND project_id = ?", projectID).Scan(&usageCount)
	require.NoError(t, err)
	require.Zero(t, usageCount)
}

func TestProviderOTelURNWithoutProviderSourceIsNotPromoted(t *testing.T) {
	t.Parallel()

	conn := newExploreTestClickhouse(t)
	projectID := uuid.New()

	insertExploreOTelLog(t, conn, exploreOTelLog{
		ProjectID:  projectID,
		OccurredAt: time.Now().UTC().Add(-time.Minute),
		Source:     "gram-server",
		EventName:  "api_request",
		Attributes: map[string]any{
			"gram.event.urn": "urn:telemetry:provider_otel:log:api_request",
			"event.name":     "api_request",
			"input_tokens":   100,
		},
	})

	var eventCount uint64
	err := conn.QueryRow(t.Context(), "SELECT count() FROM chat_events WHERE project_id = ?", projectID).Scan(&eventCount)
	require.NoError(t, err)
	require.Zero(t, eventCount)

	var usageCount uint64
	err = conn.QueryRow(t.Context(), "SELECT count() FROM chat_measurements WHERE measurement_name = 'turn_usage' AND project_id = ?", projectID).Scan(&usageCount)
	require.NoError(t, err)
	require.Zero(t, usageCount)
}

func TestClaudeWebProviderAPIPartialsShareUserReportIdentity(t *testing.T) {
	t.Parallel()

	conn := newExploreTestClickhouse(t)
	projectID := uuid.New()
	occurredAt := time.Now().UTC().Add(-time.Minute).Truncate(time.Minute)
	common := map[string]any{
		"gram.provider":         "anthropic",
		"gram.hook.source":      "claude-chat",
		"gram.account_type":     "team",
		"gram.external_user.id": "provider-user-1",
		"user.email":            "user@example.test",
		"gen_ai.request.model":  "requested-model-1",
		"gen_ai.response.model": "model-1",
	}
	usageAttrs := make(map[string]any, len(common)+6)
	maps.Copy(usageAttrs, common)
	usageAttrs["gram.event.urn"] = "urn:telemetry:provider_api:metric:usage"
	usageAttrs["gram.resource.urn"] = "claude_chat:usage:metrics"
	usageAttrs["claude_chat.event_hash"] = "usage-row-hash"
	usageAttrs["gen_ai.usage.input_tokens"] = 100
	usageAttrs["gen_ai.usage.output_tokens"] = 25
	usageAttrs["gen_ai.usage.cache_read.input_tokens"] = 0
	usageAttrs["gen_ai.usage.cache_creation.input_tokens"] = 5

	costAttrs := make(map[string]any, len(common)+3)
	maps.Copy(costAttrs, common)
	costAttrs["gram.event.urn"] = "urn:telemetry:provider_api:metric:cost"
	costAttrs["gram.resource.urn"] = "claude_chat:cost:metrics"
	costAttrs["claude_chat.event_hash"] = "cost-row-hash"
	costAttrs["gen_ai.usage.cost"] = 1.25

	insertExploreOTelLog(t, conn, exploreOTelLog{
		ProjectID:  projectID,
		OccurredAt: occurredAt,
		Source:     "gram-server",
		EventName:  "usage",
		Attributes: usageAttrs,
	})
	insertExploreOTelLog(t, conn, exploreOTelLog{
		ProjectID:  projectID,
		OccurredAt: occurredAt,
		Source:     "gram-server",
		EventName:  "cost",
		Attributes: costAttrs,
	})

	var observationCount uint64
	var naturalIDs uint64
	var measurementName string
	var granularity string
	var inputTokens uint64
	var cacheReadTokens uint64
	var costUSD float64
	var requestModel string
	var responseModel string
	var cacheReadIsPresent bool
	var costRowCountIsOne bool
	var inputRowCountIsOne bool
	err := conn.QueryRow(t.Context(), `
		SELECT
			count(),
			uniqExact(natural_id),
			any(measurement_name),
			any(granularity),
			maxIf(input_tokens, isNotNull(input_tokens)),
			maxIf(cache_read_tokens, isNotNull(cache_read_tokens)),
			maxIf(toFloat64(cost_usd), isNotNull(cost_usd)),
			any(request_model),
			any(response_model),
			countIf(isNotNull(cache_read_tokens)) = 1,
			countIf(isNotNull(cost_usd)) = 1,
			countIf(isNotNull(input_tokens)) = 1
		FROM chat_measurements
		WHERE measurement_name = 'user_usage'
		  AND project_id = ?
	`, projectID).Scan(
		&observationCount,
		&naturalIDs,
		&measurementName,
		&granularity,
		&inputTokens,
		&cacheReadTokens,
		&costUSD,
		&requestModel,
		&responseModel,
		&cacheReadIsPresent,
		&costRowCountIsOne,
		&inputRowCountIsOne,
	)
	require.NoError(t, err)
	require.Equal(t, uint64(2), observationCount)
	require.Equal(t, uint64(1), naturalIDs)
	require.Equal(t, "user_usage", measurementName)
	require.Equal(t, "minute", granularity)
	require.Equal(t, uint64(100), inputTokens)
	require.Zero(t, cacheReadTokens)
	require.InDelta(t, 1.25, costUSD, 0.00000001)
	require.Equal(t, "requested-model-1", requestModel)
	require.Equal(t, "model-1", responseModel)
	require.True(t, cacheReadIsPresent)
	require.True(t, costRowCountIsOne)
	require.True(t, inputRowCountIsOne)
}

func TestAgentHookCompletedTurnPromotesExplicitUsageTotal(t *testing.T) {
	t.Parallel()

	conn := newExploreTestClickhouse(t)
	projectID := uuid.New()

	insertExploreOTelLog(t, conn, exploreOTelLog{
		ProjectID:  projectID,
		OccurredAt: time.Now().UTC().Add(-time.Minute),
		Source:     "gram-server",
		EventName:  "AfterAgentResponse",
		Attributes: map[string]any{
			"gram.event.urn":                       "urn:telemetry:agent_hook:log:afteragentresponse",
			"gram.hook.event":                      "AfterAgentResponse",
			"gram.hook.source":                     "claude-code",
			"gram.hook.turn_id":                    "claude:message-1",
			"gram.hook.usage.present_fields":       []string{"input_tokens", "output_tokens", "cache_read_tokens"},
			"gram.provider":                        "anthropic",
			"gen_ai.conversation.id":               "session-1",
			"gen_ai.request.model":                 "request-model-1",
			"gen_ai.response.model":                "response-model-1",
			"gen_ai.usage.input_tokens":            50,
			"gen_ai.usage.output_tokens":           0,
			"gen_ai.usage.cache_read.input_tokens": 10,
		},
	})

	var usageCount uint64
	var measurementName string
	var observationKind string
	var outputTokens uint64
	var outputIsPresent bool
	var costIsAbsent bool
	err := conn.QueryRow(t.Context(), `
		SELECT
			count(),
			any(measurement_name),
			any(toString(observation_kind)),
			any(output_tokens),
			countIf(isNotNull(output_tokens)) = 1,
			countIf(isNull(cost_usd)) = 1
		FROM chat_measurements
		WHERE measurement_name = 'turn_usage'
		  AND project_id = ?
	`, projectID).Scan(&usageCount, &measurementName, &observationKind, &outputTokens, &outputIsPresent, &costIsAbsent)
	require.NoError(t, err)
	require.Equal(t, uint64(1), usageCount)
	require.Equal(t, "turn_usage", measurementName)
	require.Equal(t, "total", observationKind)
	require.Zero(t, outputTokens)
	require.True(t, outputIsPresent)
	require.True(t, costIsAbsent)

	var eventTurnID string
	var eventRequestModel string
	var eventResponseModel string
	var turnIsPresent bool
	var requestModelIsPresent bool
	var responseModelIsPresent bool
	err = conn.QueryRow(t.Context(), `
		SELECT
			any(turn_id),
			any(request_model),
			any(response_model),
			countIf(isNotNull(turn_id)) = 1,
			countIf(isNotNull(request_model)) = 1,
			countIf(isNotNull(response_model)) = 1
		FROM chat_events
		WHERE project_id = ?
	`, projectID).Scan(
		&eventTurnID,
		&eventRequestModel,
		&eventResponseModel,
		&turnIsPresent,
		&requestModelIsPresent,
		&responseModelIsPresent,
	)
	require.NoError(t, err)
	require.Equal(t, "claude:message-1", eventTurnID)
	require.Equal(t, "request-model-1", eventRequestModel)
	require.Equal(t, "response-model-1", eventResponseModel)
	require.True(t, turnIsPresent)
	require.True(t, requestModelIsPresent)
	require.True(t, responseModelIsPresent)
}

func TestAgentHookTurnRequiresIdentityAndExplicitUsagePresence(t *testing.T) {
	t.Parallel()

	conn := newExploreTestClickhouse(t)
	projectID := uuid.New()
	occurredAt := time.Now().UTC().Add(-time.Minute)

	insertExploreOTelLog(t, conn, exploreOTelLog{
		ProjectID:  projectID,
		OccurredAt: occurredAt,
		Source:     "gram-server",
		EventName:  "AfterAgentResponse",
		Attributes: map[string]any{
			"gram.event.urn":                 "urn:telemetry:agent_hook:log:afteragentresponse",
			"gram.hook.event":                "AfterAgentResponse",
			"gram.hook.source":               "claude-code",
			"gram.provider":                  "anthropic",
			"gram.hook.usage.present_fields": []string{"input_tokens"},
			"gen_ai.conversation.id":         "session-1",
			"gen_ai.usage.input_tokens":      50,
		},
	})
	insertExploreOTelLog(t, conn, exploreOTelLog{
		ProjectID:  projectID,
		OccurredAt: occurredAt.Add(time.Second),
		Source:     "gram-server",
		EventName:  "AfterAgentResponse",
		Attributes: map[string]any{
			"gram.event.urn":            "urn:telemetry:agent_hook:log:afteragentresponse",
			"gram.hook.event":           "AfterAgentResponse",
			"gram.hook.source":          "claude-code",
			"gram.provider":             "anthropic",
			"gram.hook.turn_id":         "claude:message-1",
			"gen_ai.conversation.id":    "session-1",
			"gen_ai.usage.input_tokens": 50,
		},
	})

	var usageCount uint64
	err := conn.QueryRow(t.Context(), `
		SELECT count()
		FROM chat_measurements
		WHERE measurement_name = 'turn_usage'
		  AND project_id = ?
	`, projectID).Scan(&usageCount)
	require.NoError(t, err)
	require.Zero(t, usageCount)
}

func TestCanonicalEventAuthorityPreventsDoubleCountingAndStaleValues(t *testing.T) {
	t.Parallel()

	conn := newExploreTestClickhouse(t)
	projectID := uuid.New()
	occurredAt := time.Now().UTC().Add(-time.Minute)
	naturalID := "tool:shared-call"

	insertExploreEventObservation(t, conn, exploreEventObservation{
		ProjectID:     projectID,
		NaturalID:     naturalID,
		SourceEventID: uuid.New(),
		OccurredAt:    occurredAt,
		ObservedAt:    occurredAt.Add(2 * time.Second),
		SourceChannel: "provider_otel",
		EventName:     "tool_result",
		Provider:      "anthropic",
		RequestModel:  nil,
		ResponseModel: nil,
		ToolName:      "stale-tool",
	})
	insertExploreEventObservation(t, conn, exploreEventObservation{
		ProjectID:     projectID,
		NaturalID:     naturalID,
		SourceEventID: uuid.New(),
		OccurredAt:    occurredAt,
		ObservedAt:    occurredAt.Add(time.Second),
		SourceChannel: "agent_hook",
		EventName:     "tool_result",
		Provider:      "anthropic",
		RequestModel:  nil,
		ResponseModel: nil,
		ToolName:      "canonical-tool",
	})
	insertExploreEventObservation(t, conn, exploreEventObservation{
		ProjectID:     uuid.New(),
		NaturalID:     "tool:other-project",
		SourceEventID: uuid.New(),
		OccurredAt:    occurredAt,
		ObservedAt:    occurredAt,
		SourceChannel: "provider_otel",
		EventName:     "tool_result",
		Provider:      "anthropic",
		RequestModel:  nil,
		ResponseModel: nil,
		ToolName:      "other-project-tool",
	})

	query := Query{
		Dataset:            "events",
		Calculations:       []Calculation{{Op: "COUNT", Column: ""}},
		GroupBy:            []string{"tool_name"},
		GroupExpressions:   nil,
		Filters:            nil,
		TimeStart:          occurredAt.Add(-time.Second).UnixNano(),
		TimeEnd:            occurredAt.Add(time.Minute).UnixNano(),
		GranularitySeconds: 0,
		ProjectIDs:         []uuid.UUID{projectID},
		SortBy:             "",
		SortDesc:           false,
		Limit:              0,
	}
	plan, err := plan(query)
	require.NoError(t, err)
	result, err := execute(t.Context(), conn, plan)
	require.NoError(t, err)
	require.Len(t, result.Rows, 1)
	require.Equal(t, []string{"canonical-tool"}, result.Rows[0].Group)
	require.InDelta(t, 1, result.Rows[0].Values["COUNT"], 0.000001)

	values, err := dimensionValues(
		t.Context(),
		conn,
		[]uuid.UUID{projectID},
		"events",
		"tool_name",
		query.TimeStart,
		query.TimeEnd,
	)
	require.NoError(t, err)
	require.Equal(t, []string{"canonical-tool"}, values)
}

func TestCanonicalEventAuthorityPreservesExplicitEmptyAndFallsBackOnNull(t *testing.T) {
	t.Parallel()

	conn := newExploreTestClickhouse(t)
	projectID := uuid.New()
	occurredAt := time.Now().UTC().Add(-time.Minute)
	naturalID := "request:explicit-empty"

	insertExploreEventObservation(t, conn, exploreEventObservation{
		ProjectID:     projectID,
		NaturalID:     naturalID,
		SourceEventID: uuid.New(),
		OccurredAt:    occurredAt,
		ObservedAt:    occurredAt.Add(2 * time.Second),
		SourceChannel: "provider_otel",
		EventName:     "api_request",
		Provider:      "",
		RequestModel:  nil,
		ResponseModel: nil,
		ToolName:      nil,
	})
	insertExploreEventObservation(t, conn, exploreEventObservation{
		ProjectID:     projectID,
		NaturalID:     naturalID,
		SourceEventID: uuid.New(),
		OccurredAt:    occurredAt,
		ObservedAt:    occurredAt.Add(time.Second),
		SourceChannel: "agent_hook",
		EventName:     "afteragentresponse",
		Provider:      "anthropic",
		RequestModel:  "fallback-request-model",
		ResponseModel: "fallback-response-model",
		ToolName:      nil,
	})

	queryPlan, err := plan(Query{
		Dataset:            "events",
		Calculations:       []Calculation{{Op: "COUNT", Column: ""}},
		GroupBy:            []string{"provider", "request_model", "response_model"},
		GroupExpressions:   nil,
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
	require.Len(t, result.Rows, 1)
	require.Equal(t, []string{"", "fallback-request-model", "fallback-response-model"}, result.Rows[0].Group)
	require.InDelta(t, 1, result.Rows[0].Values["COUNT"], 0.000001)
}
