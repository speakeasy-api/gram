package enrich

import (
	"errors"
	"testing"

	otelv1 "github.com/speakeasy-api/gram/infra/gen/gram/otel/v1"
	"github.com/speakeasy-api/gram/server/internal/attr"
	"github.com/speakeasy-api/gram/server/internal/otel/dialect"
	"github.com/speakeasy-api/gram/server/internal/testenv"
	"github.com/stretchr/testify/require"
	"go.opentelemetry.io/otel/attribute"
)

// inboundTestLog builds an inbound log record as a producer sends it: its
// own scope, its resource's service name, and its attributes, with tenancy
// stamped by the ingest edge.
func inboundTestLog(scope, serviceName, eventName string, attributes ...*otelv1.InboundLogRecord_KeyValue) *otelv1.InboundLogRecord {
	var resourceAttributes []*otelv1.InboundLogRecord_KeyValue
	if serviceName != "" {
		resourceAttributes = append(resourceAttributes, logStringAttribute("service.name", serviceName))
	}
	builder := &otelv1.InboundLogRecord_builder{
		RecordId: new("record-1"),
		Scope:    (&otelv1.InboundLogRecord_InstrumentationScope_builder{Name: &scope}).Build(),
		Resource: (&otelv1.InboundLogRecord_Resource_builder{Attributes: resourceAttributes}).Build(),
		Provenance: (&otelv1.InboundLogRecord_Provenance_builder{
			Source:         new("speakeasy"),
			OrganizationId: new("org-1"),
			ProjectId:      new("project-1"),
		}).Build(),
		Attributes: attributes,
	}
	if eventName != "" {
		builder.EventName = &eventName
	}
	return builder.Build()
}

func inboundTestIntAttribute(key string, value int64) *otelv1.InboundLogRecord_KeyValue {
	return (&otelv1.InboundLogRecord_KeyValue_builder{
		Key:   &key,
		Value: (&otelv1.InboundLogRecord_AnyValue_builder{IntValue: &value}).Build(),
	}).Build()
}

func inboundTestDoubleAttribute(key string, value float64) *otelv1.InboundLogRecord_KeyValue {
	return (&otelv1.InboundLogRecord_KeyValue_builder{
		Key:   &key,
		Value: (&otelv1.InboundLogRecord_AnyValue_builder{DoubleValue: &value}).Build(),
	}).Build()
}

// enrichedColumns runs one enricher and indexes what it wrote by key.
func enrichedColumns(t *testing.T, enricher LogEnricher, record *otelv1.InboundLogRecord) map[attribute.Key]attribute.Value {
	t.Helper()
	out, err := enricher.Enrich(t.Context(), record)
	require.NoError(t, err)
	indexed := make(map[attribute.Key]attribute.Value, len(out))
	for _, kv := range out {
		indexed[kv.Key] = kv.Value
	}
	return indexed
}

func TestLogColumnEnricherWritesTheColumnWhenTheTableNamesTheTypeAndTheProviderStatedIt(t *testing.T) {
	t.Parallel()

	reader, meterProvider := readableMeter(t)
	enricher := &logColumnEnricher[string]{
		column:      column[string]{key: TurnIDColumnKey, byType: perEventType[string]{dialect.EventTypeAPIRequest: getter[string]{log: dialect.LogDialect.TurnID, span: dialect.SpanDialect.TurnID}}},
		instruments: NewInstruments(testenv.NewLogger(t), meterProvider),
	}
	require.Equal(t, "enrich-column-turn_id", enricher.Name())

	record := inboundTestLog(claudeCodeScopeName, "claude-code", "api_request", logStringAttribute("prompt.id", "turn-1"))

	columns := enrichedColumns(t, enricher, record)
	require.Len(t, columns, 1)
	require.Equal(t, "turn-1", columns[TurnIDColumnKey].AsString())
	require.Zero(t, counterValue(t, reader, meterColumnEnricherMissing, attr.AgentEventColumn("turn_id")))
}

func TestLogColumnEnricherWritesNothingForATypeOutsideTheTable(t *testing.T) {
	t.Parallel()

	reader, meterProvider := readableMeter(t)
	enricher := &logColumnEnricher[string]{
		column:      column[string]{key: TurnIDColumnKey, byType: perEventType[string]{dialect.EventTypeAPIRequest: getter[string]{log: dialect.LogDialect.TurnID, span: dialect.SpanDialect.TurnID}}},
		instruments: NewInstruments(testenv.NewLogger(t), meterProvider),
	}

	// A tool result carries a turn id, but the table does not name tool
	// results, so the column is never set for them and nothing is counted.
	record := inboundTestLog(claudeCodeScopeName, "claude-code", "tool_result", logStringAttribute("prompt.id", "turn-1"))
	require.Empty(t, enrichedColumns(t, enricher, record))

	// An unclassified record matches no table either.
	unclassified := inboundTestLog(claudeCodeScopeName, "claude-code", "hook_registered", logStringAttribute("prompt.id", "turn-1"))
	require.Empty(t, enrichedColumns(t, enricher, unclassified))

	require.Zero(t, counterValue(t, reader, meterColumnEnricherMissing, attr.AgentEventColumn("turn_id")))
}

func TestLogColumnEnricherCountsATypeInTheTableWhoseProviderSaidNothing(t *testing.T) {
	t.Parallel()

	reader, meterProvider := readableMeter(t)
	enricher := &logColumnEnricher[string]{
		column:      column[string]{key: TurnIDColumnKey, byType: perEventType[string]{dialect.EventTypeAPIRequest: getter[string]{log: dialect.LogDialect.TurnID, span: dialect.SpanDialect.TurnID}}},
		instruments: NewInstruments(testenv.NewLogger(t), meterProvider),
	}

	// Codex never states a turn id, so its api_request is in the table and
	// yet writes nothing: absent, not an error, and counted by surface, type
	// and column on one data point so the gap is visible.
	record := inboundTestLog(codexScopeName, "codex", "codex.sse_event", logStringAttribute("event.kind", "response.completed"))

	require.Empty(t, enrichedColumns(t, enricher, record))
	require.Equal(t, int64(1), counterValue(t, reader, meterColumnEnricherMissing,
		attr.AgentEventSurface("codex"),
		attr.AgentEventType(dialect.EventTypeAPIRequest),
		attr.AgentEventColumn("turn_id"),
	))
	require.Zero(t, counterValue(t, reader, meterColumnEnricherMissing, attr.AgentEventSurface(missingLabelOther)))
}

func TestLogColumnEnricherLabelsAnUnrecognisedProducerAsOther(t *testing.T) {
	t.Parallel()

	reader, meterProvider := readableMeter(t)
	enricher := &logColumnEnricher[string]{
		column:      column[string]{key: TurnIDColumnKey, byType: perEventType[string]{dialect.EventTypeAPIRequest: getter[string]{log: dialect.LogDialect.TurnID, span: dialect.SpanDialect.TurnID}}},
		instruments: NewInstruments(testenv.NewLogger(t), meterProvider),
	}

	// A semconv producer is classified but recognised as no surface, so its
	// gap is counted under "other" rather than under its free-form service
	// name, which would make the counter's series unbounded.
	record := inboundTestLog("litellm", "some-proxy-42", "gen_ai.client.inference.operation.details", logStringAttribute("gen_ai.operation.name", "chat"))

	require.Empty(t, enrichedColumns(t, enricher, record))
	require.Equal(t, int64(1), counterValue(t, reader, meterColumnEnricherMissing,
		attr.AgentEventSurface(missingLabelOther),
		attr.AgentEventColumn("turn_id"),
	))
}

func TestMissingLabelFoldsTheSurfaceIntoTheVocabulary(t *testing.T) {
	t.Parallel()

	// A surface the dialect reports is folded into the agent surface
	// vocabulary, so the label spelling is the vocabulary's, wherever the
	// dialect read it: from the producer's scope or from the attribute the
	// hooks tee stamps.
	require.Equal(t, "claude_code", missingLabel("scope.name", "claude-code", nil))
	require.Equal(t, "codex", missingLabel("scope.name", "codex", nil))
	require.Equal(t, "claude_code", missingLabel("gram.hook.source", "claude-code", nil))
	require.Equal(t, "cursor", missingLabel("gram.hook.source", "cursor", nil))

	// A surface outside the vocabulary, no surface at all, and an unreadable
	// one all land under other: nothing a producer sends can add a series.
	require.Equal(t, missingLabelOther, missingLabel("scope.name", "some-proxy-42", nil))
	require.Equal(t, missingLabelOther, missingLabel("", "", nil))
	require.Equal(t, missingLabelOther, missingLabel("scope.name", "claude-code", errors.New("unreadable")))
}

func TestLogColumnEnricherWritesAConstantTheTypeImplies(t *testing.T) {
	t.Parallel()

	enricher := &logColumnEnricher[string]{
		column:      column[string]{key: OutcomeColumnKey, byType: perEventType[string]{dialect.EventTypeAPIResponse: constant(dialect.OutcomeOK)}},
		instruments: NewInstruments(testenv.NewLogger(t), testenv.NewMeterProvider(t)),
	}

	record := inboundTestLog(claudeCodeScopeName, "claude-code", "assistant_response")

	columns := enrichedColumns(t, enricher, record)
	require.Equal(t, dialect.OutcomeOK, columns[OutcomeColumnKey].AsString())
}

func TestLogColumnEnricherWritesNumbersAsNumbers(t *testing.T) {
	t.Parallel()

	m := NewInstruments(testenv.NewLogger(t), testenv.NewMeterProvider(t))
	tokens := &logColumnEnricher[int64]{
		column:      column[int64]{key: CacheReadTokensColumnKey, byType: perEventType[int64]{dialect.EventTypeAPIRequest: getter[int64]{log: dialect.LogDialect.CacheReadTokens, span: dialect.SpanDialect.CacheReadTokens}}},
		instruments: m,
	}
	cost := &logColumnEnricher[float64]{
		column:      column[float64]{key: CostUSDColumnKey, byType: perEventType[float64]{dialect.EventTypeAPIRequest: getter[float64]{log: dialect.LogDialect.CostUSD, span: dialect.SpanDialect.CostUSD}}},
		instruments: m,
	}

	// A stated zero is still stated: a request that read nothing from the
	// cache says so, and the column is written as 0 rather than left absent.
	record := inboundTestLog(claudeCodeScopeName, "claude-code", "api_request",
		inboundTestIntAttribute("cache_read_tokens", 0),
		inboundTestDoubleAttribute("cost_usd", 0.0125),
	)

	columns := enrichedColumns(t, tokens, record)
	require.Equal(t, attribute.INT64, columns[CacheReadTokensColumnKey].Type())
	require.Zero(t, columns[CacheReadTokensColumnKey].AsInt64())

	columns = enrichedColumns(t, cost, record)
	require.Equal(t, attribute.FLOAT64, columns[CostUSDColumnKey].Type())
	require.InDelta(t, 0.0125, columns[CostUSDColumnKey].AsFloat64(), 1e-9)
}

func TestEveryClassifiedTypeNamesTheWholeVocabularyAndNeverUnclassified(t *testing.T) {
	t.Parallel()

	table := everyClassifiedType(getter[string]{log: dialect.LogDialect.SessionID, span: dialect.SpanDialect.SessionID})
	require.Len(t, table, 11)
	require.NotContains(t, table, dialect.EventTypeUnclassified)
	for _, eventType := range []string{
		dialect.EventTypePrompt, dialect.EventTypeAPIRequest, dialect.EventTypeAPIResponse, dialect.EventTypeAPIError,
		dialect.EventTypeAPIRefusal, dialect.EventTypeToolCall, dialect.EventTypeToolCallResult, dialect.EventTypeToolDecision,
		dialect.EventTypeAPIRequestBody, dialect.EventTypeAPIResponseBody, dialect.EventTypeCompaction,
	} {
		require.Contains(t, table, eventType)
	}
}

func TestInboundLogSourceIsTheEventFeedSlugOrUnknown(t *testing.T) {
	t.Parallel()

	require.Equal(t, "claude-code", inboundLogSource(inboundTestLog(claudeCodeScopeName, "ClaudeCode", "api_request")))
	require.Equal(t, "my-agent", inboundLogSource(inboundTestLog("com.example.app", "My Agent", "")))
	require.Equal(t, SourceUnknown, inboundLogSource(inboundTestLog("com.example.app", "", "")))
}

// A capped column cuts the canonical copy of a value the producer sent in
// full, at a character boundary, and counts the cut; a value within the cap
// is copied whole and counted as nothing.
func TestCappedColumnCutsTheCopyAtACharacterBoundaryAndCountsIt(t *testing.T) {
	t.Parallel()

	reader, meterProvider := readableMeter(t)
	enricher := cappedColumn{
		column: column[string]{key: TextColumnKey, byType: perEventType[string]{
			dialect.EventTypePrompt: {log: dialect.LogDialect.Text, span: dialect.SpanDialect.Text},
		}},
		capBytes: 8,
	}.log(NewInstruments(testenv.NewLogger(t), meterProvider))

	// Seven ASCII bytes then a three-byte character: a byte-wise cut at 8
	// would split it, so the copy ends after the seventh byte.
	long := inboundTestLog(claudeCodeScopeName, "claude-code", "user_prompt", logStringAttribute("prompt", "abcdefg€hij"))
	columns := enrichedColumns(t, enricher, long)
	require.Equal(t, "abcdefg", columns[TextColumnKey].AsString())
	require.Equal(t, int64(1), counterValue(t, reader, meterColumnEnricherTruncated,
		attr.AgentEventSurface("claude_code"),
		attr.AgentEventType(dialect.EventTypePrompt),
		attr.AgentEventColumn("text"),
	))

	short := inboundTestLog(claudeCodeScopeName, "claude-code", "user_prompt", logStringAttribute("prompt", "fix it"))
	require.Equal(t, "fix it", enrichedColumns(t, enricher, short)[TextColumnKey].AsString())
	require.Equal(t, int64(1), counterValue(t, reader, meterColumnEnricherTruncated, attr.AgentEventColumn("text")), "a value within the cap is not counted")
}
