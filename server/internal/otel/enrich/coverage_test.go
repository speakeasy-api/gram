package enrich

import (
	"errors"
	"testing"

	"github.com/speakeasy-api/gram/server/internal/attr"
	"github.com/speakeasy-api/gram/server/internal/otel/dialect"
	"github.com/speakeasy-api/gram/server/internal/testenv"
	"github.com/stretchr/testify/require"
	"go.opentelemetry.io/otel/attribute"
)

func TestCountMissingCountsOnlyWhatTheTypeIsExpectedToCarry(t *testing.T) {
	t.Parallel()

	surface := func() string { return "claude_code" }
	count := func(t *testing.T, eventType string, expectations []expectation, written ...attribute.KeyValue) (int64, func(name string) int64) {
		t.Helper()
		reader, meterProvider := readableMeter(t)
		in := NewInstruments(testenv.NewLogger(t), meterProvider)
		countMissing(t.Context(), in, surface, eventType, expectations, written)
		total := counterValue(t, reader, meterColumnEnricherMissing, attr.AgentEventSurface("claude_code"))
		return total, func(name string) int64 {
			return counterValue(t, reader, meterColumnEnricherMissing, attr.AgentEventColumn(name))
		}
	}

	t.Run("a required attribute the enricher did not write is counted once, by surface, type and name", func(t *testing.T) {
		t.Parallel()
		reader, meterProvider := readableMeter(t)
		in := NewInstruments(testenv.NewLogger(t), meterProvider)
		countMissing(t.Context(), in, surface, dialect.EventTypeAPIRequest, usageExpectations, []attribute.KeyValue{InputTokensColumnKey.Int64(1)})
		require.Zero(t, counterValue(t, reader, meterColumnEnricherMissing, attr.AgentEventColumn("input_tokens")))
		require.Equal(t, int64(1), counterValue(t, reader, meterColumnEnricherMissing,
			attr.AgentEventSurface("claude_code"),
			attr.AgentEventType(dialect.EventTypeAPIRequest),
			attr.AgentEventColumn("cost_usd"),
		))
	})

	t.Run("a type the expectation does not name is never counted", func(t *testing.T) {
		t.Parallel()
		total, _ := count(t, dialect.EventTypeToolCallResult, usageExpectations)
		require.Zero(t, total)
	})

	t.Run("a conditional expectation counts only when its condition holds", func(t *testing.T) {
		t.Parallel()
		_, missing := count(t, dialect.EventTypeToolCallResult, operationExpectations,
			NameColumnKey.String("Bash"), ToolNameColumnKey.String("Bash"), OutcomeColumnKey.String(dialect.OutcomeOK), DurationNanoColumnKey.Int64(1))
		require.Zero(t, missing("outcome_message"), "a result that succeeded has no message to carry")

		_, missing = count(t, dialect.EventTypeToolCallResult, operationExpectations,
			NameColumnKey.String("Bash"), ToolNameColumnKey.String("Bash"), OutcomeColumnKey.String(dialect.OutcomeError), DurationNanoColumnKey.Int64(1))
		require.Equal(t, int64(1), missing("outcome_message"), "a result that failed owes its message")
	})

	t.Run("the MCP pair: each half is required once the other is stated", func(t *testing.T) {
		t.Parallel()
		_, missing := count(t, dialect.EventTypeToolCall, operationExpectations, NameColumnKey.String("mcp_tool"), ToolNameColumnKey.String("mcp_tool"))
		require.Zero(t, missing("mcp_server_name"), "a built-in tool states neither")
		require.Zero(t, missing("mcp_tool_name"))

		_, missing = count(t, dialect.EventTypeToolCall, operationExpectations, NameColumnKey.String("mcp_tool"), ToolNameColumnKey.String("mcp_tool"), MCPToolNameColumnKey.String("whoami"))
		require.Equal(t, int64(1), missing("mcp_server_name"))
		require.Zero(t, missing("mcp_tool_name"))
	})

	t.Run("recommended and opt-in attributes have no expectation", func(t *testing.T) {
		t.Parallel()
		for _, e := range operationExpectations {
			require.NotContains(t, []attribute.Key{SkillNameColumnKey, AgentNameColumnKey, TextColumnKey}, e.key)
		}
		_, missing := count(t, dialect.EventTypePrompt, operationExpectations)
		require.Zero(t, missing("text"), "words not logged are a choice, not a gap")
	})

	t.Run("a surface the dialect read off the record counts under other", func(t *testing.T) {
		t.Parallel()
		reader, meterProvider := readableMeter(t)
		in := NewInstruments(testenv.NewLogger(t), meterProvider)
		countMissing(t.Context(), in, func() string { return missingLabelOther }, dialect.EventTypeAPIRequest, usageExpectations, nil)
		for _, name := range []string{"input_tokens", "output_tokens", "cache_read_tokens", "cache_write_tokens", "cost_usd"} {
			require.Equal(t, int64(1), counterValue(t, reader, meterColumnEnricherMissing, attr.AgentEventSurface(missingLabelOther), attr.AgentEventColumn(name)), name)
		}
	})
}

func TestExpectationsNameOnlyClassifiedTypesAndTheRightSubjects(t *testing.T) {
	t.Parallel()

	require.Len(t, classifiedEventTypes, 11)
	require.NotContains(t, classifiedEventTypes, dialect.EventTypeUnclassified)
	require.Len(t, typesWithASubject, 9)
	require.NotContains(t, typesWithASubject, dialect.EventTypeAPIRequestBody)
	require.NotContains(t, typesWithASubject, dialect.EventTypeCompaction)

	for _, list := range [][]expectation{identityExpectations, operationExpectations, usageExpectations} {
		for _, e := range list {
			require.True(t, IsAgentColumnKey(string(e.key)), "%s is not an agent attribute", e.key)
			for _, eventType := range e.on {
				require.Contains(t, classifiedEventTypes, eventType, "%s expected on an unknown type %q", e.key, eventType)
			}
		}
	}
}

// The counter's surface label comes from the dialect, never from the
// resource's service name, which is free-form and would make the series
// unbounded.
func TestMissingValuesAreLabelledByTheDialectsSurface(t *testing.T) {
	t.Parallel()

	t.Run("a recognised producer is labelled by its folded surface", func(t *testing.T) {
		t.Parallel()
		reader, meterProvider := readableMeter(t)
		in := NewInstruments(testenv.NewLogger(t), meterProvider)
		// The service name is outside the surface vocabulary on purpose.
		record := inboundTestLog(codexScopeName, "codex-prod-1", "codex.sse_event", logStringAttribute("event.kind", "response.completed"))

		require.NotContains(t, identity(t, in, record), TurnIDColumnKey)
		require.Equal(t, int64(1), counterValue(t, reader, meterColumnEnricherMissing,
			attr.AgentEventSurface("codex"),
			attr.AgentEventType(dialect.EventTypeAPIRequest),
			attr.AgentEventColumn("turn_id"),
		))
		require.Zero(t, counterValue(t, reader, meterColumnEnricherMissing, attr.AgentEventSurface(missingLabelOther)))
	})

	t.Run("a producer recognised as no surface is labelled other", func(t *testing.T) {
		t.Parallel()
		reader, meterProvider := readableMeter(t)
		in := NewInstruments(testenv.NewLogger(t), meterProvider)
		record := inboundTestLog("litellm", "some-proxy-42", "gen_ai.client.inference.operation.details", logStringAttribute("gen_ai.operation.name", "chat"))

		require.NotContains(t, identity(t, in, record), TurnIDColumnKey)
		require.Equal(t, int64(1), counterValue(t, reader, meterColumnEnricherMissing,
			attr.AgentEventSurface(missingLabelOther),
			attr.AgentEventColumn("turn_id"),
		))
	})
}

func TestMissingLabelFoldsTheSurfaceIntoTheVocabulary(t *testing.T) {
	t.Parallel()

	// A surface the dialect reports is folded into the agent surface
	// vocabulary, wherever the dialect read it: from the producer's scope or
	// from the attribute the hooks tee stamps.
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
