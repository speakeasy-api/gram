package enrich

import (
	"testing"

	otelv1 "github.com/speakeasy-api/gram/infra/gen/gram/otel/v1"
	"github.com/speakeasy-api/gram/server/internal/agentsurface"
	"github.com/speakeasy-api/gram/server/internal/attr"
	"github.com/speakeasy-api/gram/server/internal/otel/dialect"
	"github.com/speakeasy-api/gram/server/internal/testenv"
	"github.com/stretchr/testify/require"
)

func TestLogClassificationNamesWhatARecordIs(t *testing.T) {
	t.Parallel()

	enricher := &logClassification{}
	require.Equal(t, "enrich-classification", enricher.Name())

	t.Run("a Claude Code api_request", func(t *testing.T) {
		t.Parallel()
		record := inboundTestLog(claudeCodeScopeName, "claude-code", "api_request", logStringAttribute("model", "claude-sonnet-4"))

		attrs := enriched(t, enricher, record)
		require.Len(t, attrs, 5)
		require.Equal(t, dialect.EventTypeAPIRequest, attrs[AgentEventTypeKey].AsString())
		require.Equal(t, "api_request", attrs[AgentRawEventNameKey].AsString())
		require.Equal(t, "claude-code", attrs[AgentSourceKey].AsString())
		require.Equal(t, "anthropic", attrs[AgentProviderKey].AsString())
		require.Equal(t, "claude-code", attrs[AgentSurfaceKey].AsString())
	})

	t.Run("a Codex response.completed is an api_request from openai on codex", func(t *testing.T) {
		t.Parallel()
		record := inboundTestLog(codexScopeName, "codex", "codex.sse_event", logStringAttribute("event.kind", "response.completed"))

		attrs := enriched(t, enricher, record)
		require.Equal(t, dialect.EventTypeAPIRequest, attrs[AgentEventTypeKey].AsString())
		require.Equal(t, "codex.sse_event", attrs[AgentRawEventNameKey].AsString())
		require.Equal(t, "codex", attrs[AgentSourceKey].AsString())
		require.Equal(t, "openai", attrs[AgentProviderKey].AsString())
		require.Equal(t, "codex", attrs[AgentSurfaceKey].AsString())
	})

	t.Run("a semconv chat record names its provider and no surface", func(t *testing.T) {
		t.Parallel()
		record := inboundTestLog("litellm", "litellm", "gen_ai.client.inference.operation.details",
			logStringAttribute("gen_ai.operation.name", "chat"),
			logStringAttribute("gen_ai.provider.name", "openai"),
		)

		attrs := enriched(t, enricher, record)
		require.Equal(t, dialect.EventTypeAPIRequest, attrs[AgentEventTypeKey].AsString())
		require.Equal(t, "gen_ai.client.inference.operation.details", attrs[AgentRawEventNameKey].AsString())
		require.Equal(t, "litellm", attrs[AgentSourceKey].AsString())
		require.Equal(t, "openai", attrs[AgentProviderKey].AsString())
		require.NotContains(t, attrs, AgentSurfaceKey, "the semantic conventions do not say which agent was behind a request")
	})

	t.Run("an unclassified Claude Code record keeps its name and producer and gets no type", func(t *testing.T) {
		t.Parallel()
		record := inboundTestLog(claudeCodeScopeName, "claude-code", "hook_registered")

		attrs := enriched(t, enricher, record)
		require.NotContains(t, attrs, AgentEventTypeKey)
		require.Equal(t, "hook_registered", attrs[AgentRawEventNameKey].AsString())
		require.Equal(t, "claude-code", attrs[AgentSourceKey].AsString())
		require.Equal(t, "anthropic", attrs[AgentProviderKey].AsString())
		require.Equal(t, "claude-code", attrs[AgentSurfaceKey].AsString())
	})

	t.Run("a record no dialect recognises gets only its source", func(t *testing.T) {
		t.Parallel()
		record := inboundTestLog("com.example.app", "", "")

		attrs := enriched(t, enricher, record)
		require.Len(t, attrs, 1)
		require.Equal(t, SourceUnknown, attrs[AgentSourceKey].AsString())
	})

	t.Run("pipeline attribution wins over what the dialect infers for the provider", func(t *testing.T) {
		t.Parallel()
		record := inboundTestLog(claudeCodeScopeName, "claude-code", "api_request", logStringAttribute("gram.provider", "bedrock"))

		attrs := enriched(t, enricher, record)
		require.Equal(t, "bedrock", attrs[AgentProviderKey].AsString())
		require.Equal(t, "claude-code", attrs[AgentSurfaceKey].AsString(), "the surface is still the dialect's")
	})

	t.Run("the source is canonicalised the way the event feed stores it", func(t *testing.T) {
		t.Parallel()
		record := inboundTestLog(claudeCodeScopeName, "ClaudeCode", "api_request")

		require.Equal(t, "claude-code", enriched(t, enricher, record)[AgentSourceKey].AsString())
	})

	t.Run("a hook row the hooks endpoint republished is typed by its hook event, on the hook source", func(t *testing.T) {
		t.Parallel()
		record := inboundTestLog(dialect.HooksLogScopeName, "codex", "PostToolUse",
			logStringAttribute("gram.hook.source", "codex"),
			logStringAttribute("gram.tool.name", "shell"),
		)

		attrs := enriched(t, enricher, record)
		require.Equal(t, dialect.EventTypeToolCallResult, attrs[AgentEventTypeKey].AsString())
		require.Equal(t, "PostToolUse", attrs[AgentRawEventNameKey].AsString())
		require.Equal(t, "codex", attrs[AgentSourceKey].AsString())
		require.Equal(t, "openai", attrs[AgentProviderKey].AsString())
		require.Equal(t, "codex", attrs[AgentSurfaceKey].AsString())
	})
}

// An event the vocabulary does not name is counted on its agent surface,
// folded into the vocabulary's spelling; a free-form surface lands under
// "other"; a typed record is not counted at all.
func TestLogClassificationCountsWhatNoDialectNames(t *testing.T) {
	t.Parallel()

	reader, meterProvider := readableMeter(t)
	enricher := &logClassification{instruments: NewInstruments(testenv.NewLogger(t), meterProvider)}

	attrs := enriched(t, enricher, inboundTestLog(claudeCodeScopeName, "claude-code", "hook_registered"))
	require.NotContains(t, attrs, AgentEventTypeKey)
	attrs = enriched(t, enricher, inboundTestLog(dialect.HooksLogScopeName, "claude-code", "SessionStart", logStringAttribute("gram.hook.source", "claude-code")))
	require.NotContains(t, attrs, AgentEventTypeKey)
	attrs = enriched(t, enricher, inboundTestLog(dialect.HooksLogScopeName, "codex", "PostToolUse", logStringAttribute("gram.hook.source", "codex")))
	require.Equal(t, dialect.EventTypeToolCallResult, attrs[AgentEventTypeKey].AsString())

	require.Equal(t, int64(2), counterValue(t, reader, meterClassificationUnclassified, attr.AgentEventSurface(string(agentsurface.SurfaceClaudeCode))), "the scope-known and the hook-stamped claude-code surfaces fold to one label")
	require.Zero(t, counterValue(t, reader, meterClassificationUnclassified, attr.AgentEventSurface("claude-code")), "the raw surface is never a label")
	require.Zero(t, counterValue(t, reader, meterClassificationUnclassified, attr.AgentEventSurface(string(agentsurface.SurfaceCodex))))

	attrs = enriched(t, enricher, inboundTestLog(dialect.HooksLogScopeName, "x", "SessionStart", logStringAttribute("gram.hook.source", "my-custom-agent-7")))
	require.NotContains(t, attrs, AgentEventTypeKey)
	require.Equal(t, int64(1), counterValue(t, reader, meterClassificationUnclassified, attr.AgentEventSurface(missingLabelOther)), "a surface outside the vocabulary lands under other")
	require.Zero(t, counterValue(t, reader, meterClassificationUnclassified, attr.AgentEventSurface("my-custom-agent-7")))
}

func TestSpanClassificationNamesWhatASpanIs(t *testing.T) {
	t.Parallel()

	enricher := &spanClassification{}
	require.Equal(t, "enrich-classification", enricher.Name())

	t.Run("a semconv chat span is an api_request named by the span, from its provider, with no surface", func(t *testing.T) {
		t.Parallel()
		span := inboundTestSpan("litellm", "LiteLLM", "chat gpt-4o", otelv1.InboundSpan_STATUS_CODE_OK,
			spanStringAttribute("gen_ai.operation.name", "chat"),
			spanStringAttribute("gen_ai.provider.name", "openai"),
		)

		attrs := enrichedSpan(t, enricher, span)
		require.Equal(t, dialect.EventTypeAPIRequest, attrs[AgentEventTypeKey].AsString())
		require.Equal(t, "chat gpt-4o", attrs[AgentRawEventNameKey].AsString(), "a span's raw name is the span name")
		require.Equal(t, "litellm", attrs[AgentSourceKey].AsString())
		require.Equal(t, "openai", attrs[AgentProviderKey].AsString())
		require.NotContains(t, attrs, AgentSurfaceKey, "a proxy span does not say which agent was behind it")
	})

	t.Run("a Claude Code span names its producer", func(t *testing.T) {
		t.Parallel()
		span := inboundTestSpan("com.anthropic.claude_code.tracing", "claude-code", "chat claude-sonnet-4", otelv1.InboundSpan_STATUS_CODE_OK,
			spanStringAttribute("gen_ai.operation.name", "chat"),
		)

		attrs := enrichedSpan(t, enricher, span)
		require.Equal(t, dialect.EventTypeAPIRequest, attrs[AgentEventTypeKey].AsString())
		require.Equal(t, "anthropic", attrs[AgentProviderKey].AsString())
		require.Equal(t, "claude-code", attrs[AgentSurfaceKey].AsString())
	})

	t.Run("pipeline attribution wins over what the dialect infers for the provider", func(t *testing.T) {
		t.Parallel()
		span := inboundTestSpan("com.anthropic.claude_code.tracing", "claude-code", "chat claude-sonnet-4", otelv1.InboundSpan_STATUS_CODE_OK,
			spanStringAttribute("gen_ai.operation.name", "chat"),
			spanStringAttribute("gram.provider", "bedrock"),
		)
		require.Equal(t, "bedrock", enrichedSpan(t, enricher, span)[AgentProviderKey].AsString())
	})

	t.Run("an unrecognised span keeps its name and source and gets no type", func(t *testing.T) {
		t.Parallel()
		span := inboundTestSpan("com.example.app", "", "GET /health", otelv1.InboundSpan_STATUS_CODE_OK)

		attrs := enrichedSpan(t, enricher, span)
		require.Len(t, attrs, 2)
		require.NotContains(t, attrs, AgentEventTypeKey)
		require.Equal(t, "GET /health", attrs[AgentRawEventNameKey].AsString())
		require.Equal(t, SourceUnknown, attrs[AgentSourceKey].AsString())
	})
}

func TestInboundLogSourceIsTheEventFeedSlugOrUnknown(t *testing.T) {
	t.Parallel()

	require.Equal(t, "claude-code", inboundLogSource(inboundTestLog(claudeCodeScopeName, "ClaudeCode", "api_request")))
	require.Equal(t, "my-agent", inboundLogSource(inboundTestLog("com.example.app", "My Agent", "")))
	require.Equal(t, SourceUnknown, inboundLogSource(inboundTestLog("com.example.app", "", "")))
}
