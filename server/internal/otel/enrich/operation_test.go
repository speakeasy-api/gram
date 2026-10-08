package enrich

import (
	"testing"

	otelv1 "github.com/speakeasy-api/gram/infra/gen/gram/otel/v1"
	"github.com/speakeasy-api/gram/server/internal/attr"
	"github.com/speakeasy-api/gram/server/internal/otel/dialect"
	"github.com/speakeasy-api/gram/server/internal/testenv"
	"github.com/stretchr/testify/require"
)

func TestOperationForClaudeCode(t *testing.T) {
	t.Parallel()

	require.Equal(t, "enrich-operation", (&logOperation{instruments: nil}).Name())

	t.Run("an api_request names the model and the request, and never gets an outcome", func(t *testing.T) {
		t.Parallel()
		reader, meterProvider := readableMeter(t)
		in := NewInstruments(testenv.NewLogger(t), meterProvider)
		record := inboundTestLog(claudeCodeScopeName, "claude-code", "api_request",
			logStringAttribute("model", "claude-sonnet-4"),
			logStringAttribute("query_source", "user_prompt"),
			logStringAttribute("skill.name", "deploy"),
			logStringAttribute("agent.name", "reviewer"),
			logStringAttribute("mcp_server.name", "github"),
			logStringAttribute("mcp_tool.name", "get_pr"),
			inboundTestDoubleAttribute("duration_ms", 1500),
			inboundTestBoolAttribute("success", true),
		)

		attrs := operation(t, in, record)
		require.Equal(t, "claude-sonnet-4", attrs[AgentModelKey].AsString())
		require.Equal(t, "user_prompt", attrs[AgentQuerySourceKey].AsString())
		require.Equal(t, "deploy", attrs[AgentSkillNameKey].AsString())
		require.Equal(t, "reviewer", attrs[AgentAgentNameKey].AsString())
		require.Equal(t, "github", attrs[AgentMCPServerNameKey].AsString())
		require.Equal(t, "get_pr", attrs[AgentMCPToolNameKey].AsString())
		require.Equal(t, int64(1_500_000_000), attrs[AgentDurationNanoKey].AsInt64())
		require.NotContains(t, attrs, AgentOutcomeKey, "a request records that a call was made, not how it went")
		require.NotContains(t, attrs, AgentNameKey, "a request has no subject with a name")
		require.NotContains(t, attrs, AgentToolNameKey)
		require.NotContains(t, attrs, AgentTextKey)
		require.Zero(t, counterValue(t, reader, meterAgentAttributeMissing, attr.AgentEventColumn("outcome")))
	})

	t.Run("a plain api_request is not counted missing on what most requests do not carry", func(t *testing.T) {
		t.Parallel()
		reader, meterProvider := readableMeter(t)
		in := NewInstruments(testenv.NewLogger(t), meterProvider)
		record := inboundTestLog(claudeCodeScopeName, "claude-code", "api_request",
			logStringAttribute("model", "claude-sonnet-4"),
			logStringAttribute("query_source", "user_prompt"),
			inboundTestDoubleAttribute("duration_ms", 10),
		)

		attrs := operation(t, in, record)
		require.NotContains(t, attrs, AgentSkillNameKey)
		require.NotContains(t, attrs, AgentAgentNameKey)
		require.NotContains(t, attrs, AgentMCPServerNameKey)
		for _, name := range []string{"skill_name", "agent_name", "mcp_server_name", "mcp_tool_name"} {
			require.Zero(t, counterValue(t, reader, meterAgentAttributeMissing, attr.AgentEventColumn(name)), name)
		}
	})

	t.Run("a failed tool_result names its tool twice and says what went wrong", func(t *testing.T) {
		t.Parallel()
		in := NewInstruments(testenv.NewLogger(t), testenv.NewMeterProvider(t))
		record := inboundTestLog(claudeCodeScopeName, "claude-code", "tool_result",
			logStringAttribute("tool_name", "Bash"),
			logStringAttribute("success", "false"),
			logStringAttribute("error_type", "ShellError"),
			inboundTestIntAttribute("duration_ms", 12),
		)

		attrs := operation(t, in, record)
		require.Equal(t, "Bash", attrs[AgentNameKey].AsString(), "the tool is the subject")
		require.Equal(t, "Bash", attrs[AgentToolNameKey].AsString(), "and the deprecated key says the same")
		require.Equal(t, dialect.OutcomeError, attrs[AgentOutcomeKey].AsString())
		require.Equal(t, "ShellError", attrs[AgentOutcomeMessageKey].AsString())
		require.Equal(t, int64(12_000_000), attrs[AgentDurationNanoKey].AsInt64())
		require.NotContains(t, attrs, AgentModelKey)
	})

	t.Run("a successful tool_result has no message to carry and is not counted", func(t *testing.T) {
		t.Parallel()
		reader, meterProvider := readableMeter(t)
		in := NewInstruments(testenv.NewLogger(t), meterProvider)
		record := inboundTestLog(claudeCodeScopeName, "claude-code", "tool_result",
			logStringAttribute("tool_name", "Read"),
			inboundTestBoolAttribute("success", true),
			inboundTestIntAttribute("duration_ms", 3),
		)

		attrs := operation(t, in, record)
		require.Equal(t, dialect.OutcomeOK, attrs[AgentOutcomeKey].AsString())
		require.NotContains(t, attrs, AgentOutcomeMessageKey)
		require.Zero(t, counterValue(t, reader, meterAgentAttributeMissing, attr.AgentEventColumn("outcome_message")))
		require.NotContains(t, attrs, AgentMCPServerNameKey, "a built-in tool has no MCP server")
		require.Zero(t, counterValue(t, reader, meterAgentAttributeMissing, attr.AgentEventColumn("mcp_server_name")))
		require.Zero(t, counterValue(t, reader, meterAgentAttributeMissing, attr.AgentEventColumn("mcp_tool_name")))
	})

	t.Run("an MCP tool_result that names the tool but not the server is counted on the server", func(t *testing.T) {
		t.Parallel()
		reader, meterProvider := readableMeter(t)
		in := NewInstruments(testenv.NewLogger(t), meterProvider)
		record := inboundTestLog(claudeCodeScopeName, "claude-code", "tool_result",
			logStringAttribute("tool_name", "mcp_tool"),
			inboundTestBoolAttribute("success", true),
			logStringAttribute("tool_parameters", `{"mcp_tool_name":"whoami"}`),
		)

		attrs := operation(t, in, record)
		require.Equal(t, "whoami", attrs[AgentMCPToolNameKey].AsString())
		require.NotContains(t, attrs, AgentMCPServerNameKey)
		require.Equal(t, int64(1), counterValue(t, reader, meterAgentAttributeMissing, attr.AgentEventColumn("mcp_server_name")))
		require.Zero(t, counterValue(t, reader, meterAgentAttributeMissing, attr.AgentEventColumn("mcp_tool_name")))
	})

	t.Run("a rejected tool_decision is rejected and an accepted one has no outcome and is not counted", func(t *testing.T) {
		t.Parallel()
		reader, meterProvider := readableMeter(t)
		in := NewInstruments(testenv.NewLogger(t), meterProvider)

		rejected := inboundTestLog(claudeCodeScopeName, "claude-code", "tool_decision",
			logStringAttribute("tool_name", "Bash"),
			logStringAttribute("decision_type", "reject"),
			logStringAttribute("mcp_server_name", "github"),
			logStringAttribute("mcp_tool_name", "create_issue"),
		)
		attrs := operation(t, in, rejected)
		require.Equal(t, dialect.OutcomeRejected, attrs[AgentOutcomeKey].AsString())
		require.Equal(t, "reject", attrs[AgentTextKey].AsString())
		require.Equal(t, "Bash", attrs[AgentNameKey].AsString())
		require.Equal(t, "github", attrs[AgentMCPServerNameKey].AsString())
		require.Equal(t, "create_issue", attrs[AgentMCPToolNameKey].AsString())

		accepted := inboundTestLog(claudeCodeScopeName, "claude-code", "tool_decision",
			logStringAttribute("tool_name", "Bash"),
			logStringAttribute("decision_type", "accept"),
		)
		attrs = operation(t, in, accepted)
		require.NotContains(t, attrs, AgentOutcomeKey, "the result row carries how an accepted call went")
		require.Zero(t, counterValue(t, reader, meterAgentAttributeMissing, attr.AgentEventColumn("outcome")))
	})

	t.Run("the api event types imply their outcome", func(t *testing.T) {
		t.Parallel()
		in := NewInstruments(testenv.NewLogger(t), testenv.NewMeterProvider(t))

		response := inboundTestLog(claudeCodeScopeName, "claude-code", "assistant_response",
			logStringAttribute("model", "claude-sonnet-4"),
			logStringAttribute("response", "Done."),
		)
		attrs := operation(t, in, response)
		require.Equal(t, dialect.OutcomeOK, attrs[AgentOutcomeKey].AsString())
		require.Equal(t, "Done.", attrs[AgentTextKey].AsString())
		require.Equal(t, "claude-sonnet-4", attrs[AgentModelKey].AsString())
		require.NotContains(t, attrs, AgentOutcomeMessageKey)

		apiError := inboundTestLog(claudeCodeScopeName, "claude-code", "api_error",
			logStringAttribute("model", "claude-sonnet-4"),
			logStringAttribute("error", "overloaded"),
		)
		attrs = operation(t, in, apiError)
		require.Equal(t, dialect.OutcomeError, attrs[AgentOutcomeKey].AsString())
		require.Equal(t, "overloaded", attrs[AgentOutcomeMessageKey].AsString())
		require.Equal(t, "overloaded", attrs[AgentTextKey].AsString())

		refusal := inboundTestLog(claudeCodeScopeName, "claude-code", "api_refusal", logStringAttribute("model", "claude-sonnet-4"))
		attrs = operation(t, in, refusal)
		require.Equal(t, dialect.OutcomeRefused, attrs[AgentOutcomeKey].AsString())
		require.Equal(t, "claude-sonnet-4", attrs[AgentModelKey].AsString())
	})

	t.Run("a payload capture carries its model and nothing about how it went", func(t *testing.T) {
		t.Parallel()
		reader, meterProvider := readableMeter(t)
		in := NewInstruments(testenv.NewLogger(t), meterProvider)

		withModel := inboundTestLog(claudeCodeScopeName, "claude-code", "api_request_body",
			logStringAttribute("model", "claude-sonnet-4"),
			logStringAttribute("body", `{"messages":[]}`),
		)
		attrs := operation(t, in, withModel)
		require.Equal(t, "claude-sonnet-4", attrs[AgentModelKey].AsString())
		require.NotContains(t, attrs, AgentOutcomeKey)
		require.NotContains(t, attrs, AgentTextKey, "the body stays whole in the attributes")

		withoutModel := inboundTestLog(claudeCodeScopeName, "claude-code", "api_response_body", logStringAttribute("body", `{}`))
		require.NotContains(t, operation(t, in, withoutModel), AgentModelKey)
		require.Zero(t, counterValue(t, reader, meterAgentAttributeMissing, attr.AgentEventColumn("model")), "a capture states the model only sometimes")
	})

	t.Run("a compaction states its outcome and, when it failed, why", func(t *testing.T) {
		t.Parallel()
		reader, meterProvider := readableMeter(t)
		in := NewInstruments(testenv.NewLogger(t), meterProvider)

		ok := inboundTestLog(claudeCodeScopeName, "claude-code", "compaction",
			inboundTestBoolAttribute("success", true),
			inboundTestDoubleAttribute("duration_ms", 2500),
		)
		attrs := operation(t, in, ok)
		require.Equal(t, dialect.OutcomeOK, attrs[AgentOutcomeKey].AsString())
		require.NotContains(t, attrs, AgentOutcomeMessageKey)
		require.NotContains(t, attrs, AgentDurationNanoKey, "a compaction's duration is housekeeping, not a request or a tool")

		failed := inboundTestLog(claudeCodeScopeName, "claude-code", "compaction",
			inboundTestBoolAttribute("success", false),
			logStringAttribute("error", "context too large"),
		)
		attrs = operation(t, in, failed)
		require.Equal(t, dialect.OutcomeError, attrs[AgentOutcomeKey].AsString())
		require.Equal(t, "context too large", attrs[AgentOutcomeMessageKey].AsString())
		require.Zero(t, counterValue(t, reader, meterAgentAttributeMissing, attr.AgentEventColumn("outcome_message")))
	})

	t.Run("a prompt whose words were not logged is a choice, not a gap", func(t *testing.T) {
		t.Parallel()
		reader, meterProvider := readableMeter(t)
		in := NewInstruments(testenv.NewLogger(t), meterProvider)
		record := inboundTestLog(claudeCodeScopeName, "claude-code", "user_prompt", inboundTestIntAttribute("prompt_length", 13))

		require.NotContains(t, operation(t, in, record), AgentTextKey)
		require.Zero(t, counterValue(t, reader, meterAgentAttributeMissing, attr.AgentEventColumn("text")))
	})

	t.Run("an unclassified record gets nothing", func(t *testing.T) {
		t.Parallel()
		in := NewInstruments(testenv.NewLogger(t), testenv.NewMeterProvider(t))
		record := inboundTestLog(claudeCodeScopeName, "claude-code", "hook_registered", logStringAttribute("tool_name", "Bash"))
		require.Empty(t, operation(t, in, record))
	})
}

func TestOperationForCodex(t *testing.T) {
	t.Parallel()

	t.Run("a response.completed is a request with a model and a duration but no outcome", func(t *testing.T) {
		t.Parallel()
		in := NewInstruments(testenv.NewLogger(t), testenv.NewMeterProvider(t))
		record := inboundTestLog(codexScopeName, "codex", "codex.sse_event",
			logStringAttribute("event.kind", "response.completed"),
			logStringAttribute("model", "gpt-5"),
			inboundTestDoubleAttribute("duration_ms", 800),
		)

		attrs := operation(t, in, record)
		require.Equal(t, "gpt-5", attrs[AgentModelKey].AsString())
		require.Equal(t, int64(800_000_000), attrs[AgentDurationNanoKey].AsInt64())
		require.NotContains(t, attrs, AgentOutcomeKey, "a completed request is a request; the type carries that it completed")
	})

	t.Run("a tool result names the tool in both attributes and states how it went", func(t *testing.T) {
		t.Parallel()
		in := NewInstruments(testenv.NewLogger(t), testenv.NewMeterProvider(t))
		record := inboundTestLog(codexScopeName, "codex", "codex.tool_result",
			logStringAttribute("tool_name", "shell"),
			logStringAttribute("call_id", "call-1"),
			inboundTestBoolAttribute("success", false),
			logStringAttribute("error", "exit 1"),
		)

		attrs := operation(t, in, record)
		require.Equal(t, "shell", attrs[AgentNameKey].AsString())
		require.Equal(t, "shell", attrs[AgentToolNameKey].AsString())
		require.Equal(t, dialect.OutcomeError, attrs[AgentOutcomeKey].AsString())
		require.Equal(t, "exit 1", attrs[AgentOutcomeMessageKey].AsString())
	})
}

func TestOperationForSemconv(t *testing.T) {
	t.Parallel()

	t.Run("a tool call names its tool and, as a log record, has no duration and is not counted for one", func(t *testing.T) {
		t.Parallel()
		reader, meterProvider := readableMeter(t)
		in := NewInstruments(testenv.NewLogger(t), meterProvider)
		record := inboundTestLog("my-agent", "my-agent", "gen_ai.client.inference.operation.details",
			logStringAttribute("gen_ai.operation.name", "execute_tool"),
			logStringAttribute("gen_ai.tool.name", "search"),
		)

		attrs := operation(t, in, record)
		require.Equal(t, "search", attrs[AgentNameKey].AsString())
		require.Equal(t, "search", attrs[AgentToolNameKey].AsString())
		require.NotContains(t, attrs, AgentOutcomeKey, "a tool call records that a call was made, not how it went")
		require.NotContains(t, attrs, AgentDurationNanoKey)
		require.Zero(t, counterValue(t, reader, meterAgentAttributeMissing, attr.AgentEventColumn("duration_nano")))
	})

	t.Run("a chat record names its model", func(t *testing.T) {
		t.Parallel()
		in := NewInstruments(testenv.NewLogger(t), testenv.NewMeterProvider(t))
		record := inboundTestLog("litellm", "litellm", "gen_ai.client.inference.operation.details",
			logStringAttribute("gen_ai.operation.name", "chat"),
			logStringAttribute("gen_ai.response.model", "gpt-4o-2024-08-06"),
			logStringAttribute("gen_ai.agent.name", "planner"),
		)

		attrs := operation(t, in, record)
		require.Equal(t, "gpt-4o-2024-08-06", attrs[AgentModelKey].AsString())
		require.Equal(t, "planner", attrs[AgentAgentNameKey].AsString())
	})
}

func TestSpanOperationReadsTheSpansOwnTiming(t *testing.T) {
	t.Parallel()

	require.Equal(t, "enrich-operation", (&spanOperation{instruments: nil}).Name())

	t.Run("a tool span names its tool and has the span's duration", func(t *testing.T) {
		t.Parallel()
		reader, meterProvider := readableMeter(t)
		in := NewInstruments(testenv.NewLogger(t), meterProvider)
		span := inboundTestSpan("my-agent", "my-agent", "execute_tool search", otelv1.InboundSpan_STATUS_CODE_OK,
			spanStringAttribute("gen_ai.operation.name", "execute_tool"),
			spanStringAttribute("gen_ai.tool.name", "search"),
		)

		attrs := enrichedSpan(t, &spanOperation{instruments: in}, span)
		require.Equal(t, "search", attrs[AgentNameKey].AsString())
		require.Equal(t, "search", attrs[AgentToolNameKey].AsString())
		require.Equal(t, int64(500), attrs[AgentDurationNanoKey].AsInt64())
		require.NotContains(t, attrs, AgentOutcomeKey, "a tool_call records that a call was made")
		require.Zero(t, counterValue(t, reader, meterAgentAttributeMissing, attr.AgentEventColumn("duration_nano")))
	})

	t.Run("a chat span's error status lands nowhere, since a request carries no outcome", func(t *testing.T) {
		t.Parallel()
		reader, meterProvider := readableMeter(t)
		in := NewInstruments(testenv.NewLogger(t), meterProvider)
		span := inboundTestSpan("litellm", "litellm", "chat gpt-4o", otelv1.InboundSpan_STATUS_CODE_ERROR,
			spanStringAttribute("gen_ai.operation.name", "chat"),
			spanStringAttribute("gen_ai.response.model", "gpt-4o-2024-08-06"),
		)

		attrs := enrichedSpan(t, &spanOperation{instruments: in}, span)
		require.Equal(t, "gpt-4o-2024-08-06", attrs[AgentModelKey].AsString())
		require.Equal(t, int64(500), attrs[AgentDurationNanoKey].AsInt64())
		require.NotContains(t, attrs, AgentOutcomeKey)
		require.NotContains(t, attrs, AgentOutcomeMessageKey)
		require.Zero(t, counterValue(t, reader, meterAgentAttributeMissing, attr.AgentEventColumn("outcome")))
	})
}

// The canonical copy of a record's words is cut at the cap on a character
// boundary and counted; a value within the cap is copied whole and counted
// as nothing. The producer's own attribute is untouched.
func TestTextIsCappedAtACharacterBoundaryAndCounted(t *testing.T) {
	t.Parallel()

	reader, meterProvider := readableMeter(t)
	in := NewInstruments(testenv.NewLogger(t), meterProvider)
	surface := func() string { return "claude_code" }

	// Seven ASCII bytes then a three-byte character: a byte-wise cut at 8
	// would split it, so the copy ends after the seventh byte.
	require.Equal(t, "abcdefg", capText(t.Context(), in, surface, dialect.EventTypePrompt, "abcdefg€hij", 8))
	require.Equal(t, int64(1), counterValue(t, reader, meterAgentAttributeTruncated,
		attr.AgentEventSurface("claude_code"),
		attr.AgentEventType(dialect.EventTypePrompt),
		attr.AgentEventColumn("text"),
	))

	require.Equal(t, "fix it", capText(t.Context(), in, surface, dialect.EventTypePrompt, "fix it", 8))
	require.Equal(t, int64(1), counterValue(t, reader, meterAgentAttributeTruncated, attr.AgentEventColumn("text")), "a value within the cap is not counted")

	// The operation enricher applies the real cap to a prompt's words.
	long := inboundTestLog(claudeCodeScopeName, "claude-code", "user_prompt", logStringAttribute("prompt", string(make([]byte, maxTextBytes+1))))
	require.Len(t, operation(t, in, long)[AgentTextKey].AsString(), maxTextBytes)
	require.Equal(t, int64(2), counterValue(t, reader, meterAgentAttributeTruncated, attr.AgentEventColumn("text")))
}
