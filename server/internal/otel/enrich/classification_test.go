package enrich

import (
	"testing"

	otelv1 "github.com/speakeasy-api/gram/infra/gen/gram/otel/v1"
	"github.com/speakeasy-api/gram/server/internal/otel/dialect"
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
		require.Equal(t, dialect.EventTypeAPIRequest, attrs[EventTypeColumnKey].AsString())
		require.Equal(t, "api_request", attrs[RawEventNameColumnKey].AsString())
		require.Equal(t, "claude-code", attrs[SourceColumnKey].AsString())
		require.Equal(t, "anthropic", attrs[ProviderColumnKey].AsString())
		require.Equal(t, "claude-code", attrs[SurfaceColumnKey].AsString())
	})

	t.Run("a Codex response.completed is an api_request from openai on codex", func(t *testing.T) {
		t.Parallel()
		record := inboundTestLog(codexScopeName, "codex", "codex.sse_event", logStringAttribute("event.kind", "response.completed"))

		attrs := enriched(t, enricher, record)
		require.Equal(t, dialect.EventTypeAPIRequest, attrs[EventTypeColumnKey].AsString())
		require.Equal(t, "codex.sse_event", attrs[RawEventNameColumnKey].AsString())
		require.Equal(t, "codex", attrs[SourceColumnKey].AsString())
		require.Equal(t, "openai", attrs[ProviderColumnKey].AsString())
		require.Equal(t, "codex", attrs[SurfaceColumnKey].AsString())
	})

	t.Run("a semconv chat record names its provider and no surface", func(t *testing.T) {
		t.Parallel()
		record := inboundTestLog("litellm", "litellm", "gen_ai.client.inference.operation.details",
			logStringAttribute("gen_ai.operation.name", "chat"),
			logStringAttribute("gen_ai.provider.name", "openai"),
		)

		attrs := enriched(t, enricher, record)
		require.Equal(t, dialect.EventTypeAPIRequest, attrs[EventTypeColumnKey].AsString())
		require.Equal(t, "gen_ai.client.inference.operation.details", attrs[RawEventNameColumnKey].AsString())
		require.Equal(t, "litellm", attrs[SourceColumnKey].AsString())
		require.Equal(t, "openai", attrs[ProviderColumnKey].AsString())
		require.NotContains(t, attrs, SurfaceColumnKey, "the semantic conventions do not say which agent was behind a request")
	})

	t.Run("an unclassified Claude Code record keeps its name and producer and gets no type", func(t *testing.T) {
		t.Parallel()
		record := inboundTestLog(claudeCodeScopeName, "claude-code", "hook_registered")

		attrs := enriched(t, enricher, record)
		require.NotContains(t, attrs, EventTypeColumnKey)
		require.Equal(t, "hook_registered", attrs[RawEventNameColumnKey].AsString())
		require.Equal(t, "claude-code", attrs[SourceColumnKey].AsString())
		require.Equal(t, "anthropic", attrs[ProviderColumnKey].AsString())
		require.Equal(t, "claude-code", attrs[SurfaceColumnKey].AsString())
	})

	t.Run("a record no dialect recognises gets only its source", func(t *testing.T) {
		t.Parallel()
		record := inboundTestLog("com.example.app", "", "")

		attrs := enriched(t, enricher, record)
		require.Len(t, attrs, 1)
		require.Equal(t, SourceUnknown, attrs[SourceColumnKey].AsString())
	})

	t.Run("pipeline attribution wins over what the dialect infers for the provider", func(t *testing.T) {
		t.Parallel()
		record := inboundTestLog(claudeCodeScopeName, "claude-code", "api_request", logStringAttribute("gram.provider", "bedrock"))

		attrs := enriched(t, enricher, record)
		require.Equal(t, "bedrock", attrs[ProviderColumnKey].AsString())
		require.Equal(t, "claude-code", attrs[SurfaceColumnKey].AsString(), "the surface is still the dialect's")
	})

	t.Run("the source is canonicalised the way the event feed stores it", func(t *testing.T) {
		t.Parallel()
		record := inboundTestLog(claudeCodeScopeName, "ClaudeCode", "api_request")

		require.Equal(t, "claude-code", enriched(t, enricher, record)[SourceColumnKey].AsString())
	})
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
		require.Equal(t, dialect.EventTypeAPIRequest, attrs[EventTypeColumnKey].AsString())
		require.Equal(t, "chat gpt-4o", attrs[RawEventNameColumnKey].AsString(), "a span's raw name is the span name")
		require.Equal(t, "litellm", attrs[SourceColumnKey].AsString())
		require.Equal(t, "openai", attrs[ProviderColumnKey].AsString())
		require.NotContains(t, attrs, SurfaceColumnKey, "a proxy span does not say which agent was behind it")
	})

	t.Run("a Claude Code span names its producer", func(t *testing.T) {
		t.Parallel()
		span := inboundTestSpan("com.anthropic.claude_code.tracing", "claude-code", "chat claude-sonnet-4", otelv1.InboundSpan_STATUS_CODE_OK,
			spanStringAttribute("gen_ai.operation.name", "chat"),
		)

		attrs := enrichedSpan(t, enricher, span)
		require.Equal(t, dialect.EventTypeAPIRequest, attrs[EventTypeColumnKey].AsString())
		require.Equal(t, "anthropic", attrs[ProviderColumnKey].AsString())
		require.Equal(t, "claude-code", attrs[SurfaceColumnKey].AsString())
	})

	t.Run("pipeline attribution wins over what the dialect infers for the provider", func(t *testing.T) {
		t.Parallel()
		span := inboundTestSpan("com.anthropic.claude_code.tracing", "claude-code", "chat claude-sonnet-4", otelv1.InboundSpan_STATUS_CODE_OK,
			spanStringAttribute("gen_ai.operation.name", "chat"),
			spanStringAttribute("gram.provider", "bedrock"),
		)
		require.Equal(t, "bedrock", enrichedSpan(t, enricher, span)[ProviderColumnKey].AsString())
	})

	t.Run("an unrecognised span keeps its name and source and gets no type", func(t *testing.T) {
		t.Parallel()
		span := inboundTestSpan("com.example.app", "", "GET /health", otelv1.InboundSpan_STATUS_CODE_OK)

		attrs := enrichedSpan(t, enricher, span)
		require.Len(t, attrs, 2)
		require.NotContains(t, attrs, EventTypeColumnKey)
		require.Equal(t, "GET /health", attrs[RawEventNameColumnKey].AsString())
		require.Equal(t, SourceUnknown, attrs[SourceColumnKey].AsString())
	})
}

func TestInboundLogSourceIsTheEventFeedSlugOrUnknown(t *testing.T) {
	t.Parallel()

	require.Equal(t, "claude-code", inboundLogSource(inboundTestLog(claudeCodeScopeName, "ClaudeCode", "api_request")))
	require.Equal(t, "my-agent", inboundLogSource(inboundTestLog("com.example.app", "My Agent", "")))
	require.Equal(t, SourceUnknown, inboundLogSource(inboundTestLog("com.example.app", "", "")))
}
