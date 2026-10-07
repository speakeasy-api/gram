package enrich

import (
	"testing"

	otelv1 "github.com/speakeasy-api/gram/infra/gen/gram/otel/v1"
	"github.com/speakeasy-api/gram/server/internal/attr"
	"github.com/speakeasy-api/gram/server/internal/otel/dialect"
	"github.com/speakeasy-api/gram/server/internal/testenv"
	"github.com/stretchr/testify/require"
	"go.opentelemetry.io/otel/attribute"
)

// inboundTestSpan builds an inbound span as a producer sends it: its own
// scope, its resource's service name, its name, status and attributes, with
// tenancy stamped by the ingest edge.
func inboundTestSpan(scope, serviceName, name string, status otelv1.InboundSpan_StatusCode, attributes ...*otelv1.InboundSpan_KeyValue) *otelv1.InboundSpan {
	var resourceAttributes []*otelv1.InboundSpan_KeyValue
	if serviceName != "" {
		resourceAttributes = append(resourceAttributes, spanStringAttribute("service.name", serviceName))
	}
	return (&otelv1.InboundSpan_builder{
		TraceId:           []byte{1, 2, 3, 4, 5, 6, 7, 8, 9, 10, 11, 12, 13, 14, 15, 16},
		SpanId:            []byte{1, 2, 3, 4, 5, 6, 7, 8},
		Name:              &name,
		StartTimeUnixNano: new(uint64(1_724_500_000_000_000_001)),
		EndTimeUnixNano:   new(uint64(1_724_500_000_000_000_501)),
		Scope:             (&otelv1.InboundSpan_InstrumentationScope_builder{Name: &scope}).Build(),
		Resource:          (&otelv1.InboundSpan_Resource_builder{Attributes: resourceAttributes}).Build(),
		Status:            (&otelv1.InboundSpan_Status_builder{Code: status.Enum(), Message: new("boom")}).Build(),
		Provenance: (&otelv1.InboundSpan_Provenance_builder{
			Source:         new("speakeasy"),
			OrganizationId: new("org-1"),
			ProjectId:      new("project-1"),
		}).Build(),
		Attributes: attributes,
	}).Build()
}

// enrichedSpanColumns runs every span column enricher over one span, as the
// span transform does, and indexes what they wrote by key.
func enrichedSpanColumns(t *testing.T, in *Instruments, span *otelv1.InboundSpan) map[attribute.Key]attribute.Value {
	t.Helper()
	columns := map[attribute.Key]attribute.Value{}
	for _, enricher := range SpanColumns(in) {
		out, err := enricher.Enrich(t.Context(), span)
		require.NoError(t, err)
		for _, kv := range out {
			columns[kv.Key] = kv.Value
		}
	}
	return columns
}

func TestSpanColumnsAreTheLogColumnsInTheSameOrder(t *testing.T) {
	t.Parallel()

	in := NewInstruments(testenv.NewLogger(t), testenv.NewMeterProvider(t))
	logs := LogColumns(in)
	spans := SpanColumns(in)
	require.Len(t, spans, len(logs))
	for i := range logs {
		require.Equal(t, logs[i].Name(), spans[i].Name(), "one table serves both signals")
	}
	require.Equal(t, "enrich-classification", logs[0].Name())
}

func TestSpanClassificationNamesWhatASpanIs(t *testing.T) {
	t.Parallel()

	in := NewInstruments(testenv.NewLogger(t), testenv.NewMeterProvider(t))

	t.Run("a semconv chat span is an api_request named by the span, from its provider, with no surface", func(t *testing.T) {
		t.Parallel()
		span := inboundTestSpan("litellm", "LiteLLM", "chat gpt-4o", otelv1.InboundSpan_STATUS_CODE_OK,
			spanStringAttribute("gen_ai.operation.name", "chat"),
			spanStringAttribute("gen_ai.provider.name", "openai"),
		)

		columns := enrichedSpanColumns(t, in, span)
		require.Equal(t, dialect.EventTypeAPIRequest, columns[EventTypeColumnKey].AsString())
		require.Equal(t, "chat gpt-4o", columns[RawEventNameColumnKey].AsString(), "a span's raw name is the span name")
		require.Equal(t, "litellm", columns[SourceColumnKey].AsString())
		require.Equal(t, "openai", columns[ProviderColumnKey].AsString())
		require.NotContains(t, columns, SurfaceColumnKey, "a proxy span does not say which agent was behind it")
	})

	t.Run("a Claude Code span names its producer", func(t *testing.T) {
		t.Parallel()
		span := inboundTestSpan("com.anthropic.claude_code.tracing", "claude-code", "chat claude-sonnet-4", otelv1.InboundSpan_STATUS_CODE_OK,
			spanStringAttribute("gen_ai.operation.name", "chat"),
		)

		columns := enrichedSpanColumns(t, in, span)
		require.Equal(t, dialect.EventTypeAPIRequest, columns[EventTypeColumnKey].AsString())
		require.Equal(t, "anthropic", columns[ProviderColumnKey].AsString())
		require.Equal(t, "claude-code", columns[SurfaceColumnKey].AsString())
	})

	t.Run("pipeline attribution wins over what the dialect infers for the provider", func(t *testing.T) {
		t.Parallel()
		span := inboundTestSpan("com.anthropic.claude_code.tracing", "claude-code", "chat claude-sonnet-4", otelv1.InboundSpan_STATUS_CODE_OK,
			spanStringAttribute("gen_ai.operation.name", "chat"),
			spanStringAttribute("gram.provider", "bedrock"),
		)
		require.Equal(t, "bedrock", enrichedSpanColumns(t, in, span)[ProviderColumnKey].AsString())
	})

	t.Run("an unrecognised span keeps its name and source and gets no type", func(t *testing.T) {
		t.Parallel()
		span := inboundTestSpan("com.example.app", "", "GET /health", otelv1.InboundSpan_STATUS_CODE_OK)

		columns := enrichedSpanColumns(t, in, span)
		require.Len(t, columns, 2)
		require.NotContains(t, columns, EventTypeColumnKey)
		require.Equal(t, "GET /health", columns[RawEventNameColumnKey].AsString())
		require.Equal(t, SourceUnknown, columns[SourceColumnKey].AsString())
	})
}

func TestSpanColumnsAnswerFromTheSameTablesAsLogs(t *testing.T) {
	t.Parallel()

	t.Run("a chat span carries its identity, model, usage and duration, and no outcome", func(t *testing.T) {
		t.Parallel()
		reader, meterProvider := readableMeter(t)
		in := NewInstruments(testenv.NewLogger(t), meterProvider)
		span := inboundTestSpan("litellm", "litellm", "chat gpt-4o", otelv1.InboundSpan_STATUS_CODE_ERROR,
			spanStringAttribute("gen_ai.operation.name", "chat"),
			spanStringAttribute("gen_ai.provider.name", "openai"),
			spanStringAttribute("gen_ai.response.model", "gpt-4o-2024-08-06"),
			spanStringAttribute("gen_ai.response.id", "resp-1"),
			spanStringAttribute("gen_ai.conversation.id", "session-9"),
			spanStringAttribute("gen_ai.usage.input_tokens", "200"),
			spanStringAttribute("gen_ai.usage.output_tokens", "50"),
			spanStringAttribute("gen_ai.usage.cost", "0.002"),
		)

		columns := enrichedSpanColumns(t, in, span)
		require.Equal(t, "resp-1", columns[EventIDColumnKey].AsString())
		require.Equal(t, "session-9", columns[SessionIDColumnKey].AsString())
		require.Equal(t, "gpt-4o-2024-08-06", columns[ModelColumnKey].AsString())
		require.Equal(t, int64(200), columns[InputTokensColumnKey].AsInt64())
		require.Equal(t, int64(50), columns[OutputTokensColumnKey].AsInt64())
		require.InDelta(t, 0.002, columns[CostUSDColumnKey].AsFloat64(), 1e-9)
		require.Equal(t, int64(500), columns[DurationNanoColumnKey].AsInt64())
		// The span's status is an error, but an api_request is not in the
		// outcome table: in the log vocabulary a request records that a call
		// was made and its response or error says how it went. Where a span's
		// status should land is an open question; until it is decided the
		// status lands nowhere, and nothing is counted since the type is not
		// in the table.
		require.NotContains(t, columns, OutcomeColumnKey)
		require.NotContains(t, columns, OutcomeMessageColumnKey)
		require.Zero(t, counterValue(t, reader, meterColumnEnricherMissing, attr.AgentEventColumn("outcome")))
	})

	t.Run("a tool span names its tool and the call", func(t *testing.T) {
		t.Parallel()
		in := NewInstruments(testenv.NewLogger(t), testenv.NewMeterProvider(t))
		span := inboundTestSpan("my-agent", "my-agent", "execute_tool search", otelv1.InboundSpan_STATUS_CODE_OK,
			spanStringAttribute("gen_ai.operation.name", "execute_tool"),
			spanStringAttribute("gen_ai.tool.name", "search"),
			spanStringAttribute("gen_ai.tool.call.id", "call-1"),
		)

		columns := enrichedSpanColumns(t, in, span)
		require.Equal(t, dialect.EventTypeToolCall, columns[EventTypeColumnKey].AsString())
		require.Equal(t, "search", columns[NameColumnKey].AsString())
		require.Equal(t, "search", columns[ToolNameColumnKey].AsString())
		require.Equal(t, "call-1", columns[EventIDColumnKey].AsString())
		require.NotContains(t, columns, OutcomeColumnKey, "a tool_call is not in the outcome table")
		require.NotContains(t, columns, InputTokensColumnKey, "a tool call carries no usage")
	})

	t.Run("a chat span that states no conversation is counted missing on session_id", func(t *testing.T) {
		t.Parallel()
		reader, meterProvider := readableMeter(t)
		in := NewInstruments(testenv.NewLogger(t), meterProvider)
		span := inboundTestSpan("litellm", "litellm", "chat gpt-4o", otelv1.InboundSpan_STATUS_CODE_OK,
			spanStringAttribute("gen_ai.operation.name", "chat"),
		)

		require.NotContains(t, enrichedSpanColumns(t, in, span), SessionIDColumnKey)
		require.Equal(t, int64(1), counterValue(t, reader, meterColumnEnricherMissing, attr.AgentEventColumn("session_id")))
		require.Equal(t, int64(1), counterValue(t, reader, meterColumnEnricherMissing, attr.AgentEventSurface(counterSurfaceOther)))
	})
}

// A tool_call that is a span is the whole call, so its duration is the
// span's own timing; the same type as a log record is only the call's start
// and carries none, which the table treats as Recommended rather than a gap.
func TestSpanColumnsGiveAToolCallSpanItsDuration(t *testing.T) {
	t.Parallel()

	reader, meterProvider := readableMeter(t)
	in := NewInstruments(testenv.NewLogger(t), meterProvider)
	span := inboundTestSpan("my-agent", "my-agent", "execute_tool search", otelv1.InboundSpan_STATUS_CODE_OK,
		spanStringAttribute("gen_ai.operation.name", "execute_tool"),
		spanStringAttribute("gen_ai.tool.name", "search"),
	)

	columns := enrichedSpanColumns(t, in, span)
	require.Equal(t, int64(500), columns[DurationNanoColumnKey].AsInt64())
	require.Zero(t, counterValue(t, reader, meterColumnEnricherMissing, attr.AgentEventColumn("duration_nano")))
}
