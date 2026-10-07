package enrich

import (
	"testing"

	"github.com/speakeasy-api/gram/server/internal/attr"

	"github.com/speakeasy-api/gram/server/internal/otel/dialect"
	"github.com/speakeasy-api/gram/server/internal/testenv"
	"github.com/stretchr/testify/require"
)

func TestEnrichLogClassificationNamesWhatARecordIs(t *testing.T) {
	t.Parallel()

	enricher := &logClassification{}
	require.Equal(t, "enrich-classification", enricher.Name())

	t.Run("a Claude Code api_request", func(t *testing.T) {
		t.Parallel()
		record := inboundTestLog(claudeCodeScopeName, "claude-code", "api_request", logStringAttribute("model", "claude-sonnet-4"))

		columns := enrichedColumns(t, enricher, record)
		require.Len(t, columns, 5)
		require.Equal(t, dialect.EventTypeAPIRequest, columns[EventTypeColumnKey].AsString())
		require.Equal(t, "api_request", columns[RawEventNameColumnKey].AsString())
		require.Equal(t, "claude-code", columns[SourceColumnKey].AsString())
		require.Equal(t, "anthropic", columns[ProviderColumnKey].AsString())
		require.Equal(t, "claude-code", columns[SurfaceColumnKey].AsString())
	})

	t.Run("a Codex response.completed is an api_request from openai on codex", func(t *testing.T) {
		t.Parallel()
		record := inboundTestLog(codexScopeName, "codex", "codex.sse_event", logStringAttribute("event.kind", "response.completed"))

		columns := enrichedColumns(t, enricher, record)
		require.Equal(t, dialect.EventTypeAPIRequest, columns[EventTypeColumnKey].AsString())
		require.Equal(t, "codex.sse_event", columns[RawEventNameColumnKey].AsString())
		require.Equal(t, "codex", columns[SourceColumnKey].AsString())
		require.Equal(t, "openai", columns[ProviderColumnKey].AsString())
		require.Equal(t, "codex", columns[SurfaceColumnKey].AsString())
	})

	t.Run("a semconv chat record names its provider and no surface", func(t *testing.T) {
		t.Parallel()
		record := inboundTestLog("litellm", "litellm", "gen_ai.client.inference.operation.details",
			logStringAttribute("gen_ai.operation.name", "chat"),
			logStringAttribute("gen_ai.provider.name", "openai"),
		)

		columns := enrichedColumns(t, enricher, record)
		require.Equal(t, dialect.EventTypeAPIRequest, columns[EventTypeColumnKey].AsString())
		require.Equal(t, "gen_ai.client.inference.operation.details", columns[RawEventNameColumnKey].AsString())
		require.Equal(t, "litellm", columns[SourceColumnKey].AsString())
		require.Equal(t, "openai", columns[ProviderColumnKey].AsString())
		require.NotContains(t, columns, SurfaceColumnKey, "the semantic conventions do not say which agent was behind a request")
	})

	t.Run("a hook row the hooks endpoint republished is typed by its hook event, on the hook source", func(t *testing.T) {
		t.Parallel()
		record := inboundTestLog(dialect.HooksLogScopeName, "codex", "PostToolUse",
			logStringAttribute("gram.hook.source", "codex"),
			logStringAttribute("gram.tool.name", "shell"),
		)

		columns := enrichedColumns(t, enricher, record)
		require.Equal(t, dialect.EventTypeToolCallResult, columns[EventTypeColumnKey].AsString())
		require.Equal(t, "PostToolUse", columns[RawEventNameColumnKey].AsString())
		require.Equal(t, "codex", columns[SourceColumnKey].AsString())
		require.Equal(t, "openai", columns[ProviderColumnKey].AsString())
		require.Equal(t, "codex", columns[SurfaceColumnKey].AsString())
	})

	t.Run("an unclassified Claude Code record keeps its name and producer and gets no type", func(t *testing.T) {
		t.Parallel()
		record := inboundTestLog(claudeCodeScopeName, "claude-code", "hook_registered")

		columns := enrichedColumns(t, enricher, record)
		require.NotContains(t, columns, EventTypeColumnKey)
		require.Equal(t, "hook_registered", columns[RawEventNameColumnKey].AsString())
		require.Equal(t, "claude-code", columns[SourceColumnKey].AsString())
		require.Equal(t, "anthropic", columns[ProviderColumnKey].AsString())
		require.Equal(t, "claude-code", columns[SurfaceColumnKey].AsString())
	})

	t.Run("a record no dialect recognises gets only its source", func(t *testing.T) {
		t.Parallel()
		record := inboundTestLog("com.example.app", "", "")

		columns := enrichedColumns(t, enricher, record)
		require.Len(t, columns, 1)
		require.Equal(t, SourceUnknown, columns[SourceColumnKey].AsString())
	})

	t.Run("pipeline attribution wins over what the dialect infers for the provider", func(t *testing.T) {
		t.Parallel()
		record := inboundTestLog(claudeCodeScopeName, "claude-code", "api_request", logStringAttribute("gram.provider", "bedrock"))

		columns := enrichedColumns(t, enricher, record)
		require.Equal(t, "bedrock", columns[ProviderColumnKey].AsString())
		require.Equal(t, "claude-code", columns[SurfaceColumnKey].AsString(), "the surface is still the dialect's")
	})

	t.Run("the source is canonicalised the way the event feed stores it", func(t *testing.T) {
		t.Parallel()
		record := inboundTestLog(claudeCodeScopeName, "ClaudeCode", "api_request")

		columns := enrichedColumns(t, enricher, record)
		require.Equal(t, "claude-code", columns[SourceColumnKey].AsString())
	})
}

func TestEnrichLogClassificationCountsWhatNoDialectNames(t *testing.T) {
	t.Parallel()

	reader, meterProvider := readableMeter(t)
	enricher := &logClassification{instruments: NewInstruments(testenv.NewLogger(t), meterProvider)}

	// A Claude Code event the vocabulary does not name is counted on its
	// surface; a hook row's surface is read off the record, so it is counted
	// under "other"; a typed record is not counted at all.
	columns := enrichedColumns(t, enricher, inboundTestLog(claudeCodeScopeName, "claude-code", "hook_registered"))
	require.NotContains(t, columns, EventTypeColumnKey)
	columns = enrichedColumns(t, enricher, inboundTestLog(dialect.HooksLogScopeName, "claude-code", "SessionStart", logStringAttribute("gram.hook.source", "claude-code")))
	require.NotContains(t, columns, EventTypeColumnKey)
	columns = enrichedColumns(t, enricher, inboundTestLog(dialect.HooksLogScopeName, "codex", "PostToolUse", logStringAttribute("gram.hook.source", "codex")))
	require.Equal(t, dialect.EventTypeToolCallResult, columns[EventTypeColumnKey].AsString())

	require.Equal(t, int64(1), counterValue(t, reader, meterClassificationUnclassified, attr.AgentEventSurface("claude-code")))
	require.Equal(t, int64(1), counterValue(t, reader, meterClassificationUnclassified, attr.AgentEventSurface(counterSurfaceOther)))
	require.Zero(t, counterValue(t, reader, meterClassificationUnclassified, attr.AgentEventSurface("codex")))
}
