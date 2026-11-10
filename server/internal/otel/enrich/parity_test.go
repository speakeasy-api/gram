package enrich

import (
	"testing"

	otelv1 "github.com/speakeasy-api/gram/infra/gen/gram/otel/v1"
	"github.com/stretchr/testify/require"
	"go.opentelemetry.io/otel/attribute"
)

func TestSpanAgentAttributesAreTheLogAgentAttributesInTheSameOrder(t *testing.T) {
	t.Parallel()

	logs := LogAgentAttributes()
	spans := SpanAgentAttributes()
	require.Len(t, spans, len(logs))
	for i := range logs {
		require.Equal(t, logs[i].Name(), spans[i].Name())
	}
	require.Equal(t, []string{"enrich-classification", "enrich-identity", "enrich-operation", "enrich-usage"}, []string{logs[0].Name(), logs[1].Name(), logs[2].Name(), logs[3].Name()})
}

// A log record and a span that say the same thing in the semantic
// conventions get the same agent attributes, except the duration: a span is
// the whole operation and has one of its own, a log record does not. The
// semantic conventions are the one vocabulary both signal dialects read; the
// other event types come from log-only producers.
func TestALogAndASpanThatSayTheSameThingGetTheSameAttributes(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name  string
		event string
		span  string
		attrs map[string]string
	}{
		{
			name: "a chat is an api_request", event: "gen_ai.client.inference.operation.details", span: "chat gpt-4o",
			attrs: map[string]string{
				"gen_ai.operation.name":      "chat",
				"gen_ai.provider.name":       "openai",
				"gen_ai.conversation.id":     "session-9",
				"gen_ai.response.id":         "resp-1",
				"gen_ai.response.model":      "gpt-4o-2024-08-06",
				"gen_ai.agent.name":          "planner",
				"user.email":                 "dev@example.com",
				"gen_ai.usage.input_tokens":  "200",
				"gen_ai.usage.output_tokens": "50",
				"gen_ai.usage.cost":          "0.002",
			},
		},
		{
			name: "a tool execution is a tool_call", event: "gen_ai.client.inference.operation.details", span: "execute_tool search",
			attrs: map[string]string{
				"gen_ai.operation.name":  "execute_tool",
				"gen_ai.conversation.id": "session-9",
				"gen_ai.tool.name":       "search",
				"gen_ai.tool.call.id":    "call-1",
				"user.email":             "dev@example.com",
			},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			var logAttrs []*otelv1.InboundLogRecord_KeyValue
			var spanAttrs []*otelv1.InboundSpan_KeyValue
			for k, v := range tc.attrs {
				logAttrs = append(logAttrs, logStringAttribute(k, v))
				spanAttrs = append(spanAttrs, spanStringAttribute(k, v))
			}
			fromLog := agentAttributes(t, inboundTestLog("litellm", "litellm", tc.event, logAttrs...))
			fromSpan := spanAgentAttributes(t, inboundTestSpan("litellm", "litellm", tc.span, otelv1.InboundSpan_STATUS_CODE_OK, spanAttrs...))

			require.NotContains(t, fromLog, AgentDurationNanoKey)
			require.Equal(t, int64(500), fromSpan[AgentDurationNanoKey].AsInt64())
			delete(fromSpan, AgentDurationNanoKey)
			// A span's raw name is the span name; a log record's is its event name.
			delete(fromLog, AgentRawEventNameKey)
			delete(fromSpan, AgentRawEventNameKey)

			require.Equal(t, fromLog, fromSpan)
			require.Contains(t, fromLog, AgentEventTypeKey)
			require.Contains(t, fromLog, AgentSessionIDKey)
			require.Contains(t, fromLog, AgentEventIDKey)
		})
	}
}

func TestSpanAgentAttributesForAChatSpan(t *testing.T) {
	t.Parallel()

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

	attrs := spanAgentAttributes(t, span)
	want := map[attribute.Key]string{
		AgentEventIDKey:   "resp-1",
		AgentSessionIDKey: "session-9",
		AgentModelKey:     "gpt-4o-2024-08-06",
		AgentProviderKey:  "openai",
	}
	for key, value := range want {
		require.Equal(t, value, attrs[key].AsString(), string(key))
	}
	require.Equal(t, int64(200), attrs[AgentInputTokensKey].AsInt64())
	require.Equal(t, int64(50), attrs[AgentOutputTokensKey].AsInt64())
	require.InDelta(t, 0.002, attrs[AgentCostUSDKey].AsFloat64(), 1e-9)
	require.Equal(t, int64(500), attrs[AgentDurationNanoKey].AsInt64())
	require.NotContains(t, attrs, AgentOutcomeKey, "a request carries no outcome, so the span's error status lands nowhere")
}
