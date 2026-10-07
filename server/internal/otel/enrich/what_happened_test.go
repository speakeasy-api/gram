package enrich

import (
	"maps"
	"testing"

	otelv1 "github.com/speakeasy-api/gram/infra/gen/gram/otel/v1"
	"github.com/speakeasy-api/gram/server/internal/attr"
	"github.com/speakeasy-api/gram/server/internal/otel/dialect"
	"github.com/speakeasy-api/gram/server/internal/testenv"
	"github.com/stretchr/testify/require"
	"go.opentelemetry.io/otel/attribute"
)

// whatHappened runs the twelve what-happened column enrichers over one
// record, as the transform does, and indexes what they wrote by key.
func whatHappened(t *testing.T, in *Instruments, record *otelv1.InboundLogRecord) map[attribute.Key]attribute.Value {
	t.Helper()
	columns := map[attribute.Key]attribute.Value{}
	for _, enricher := range []LogEnricher{
		columnModel().log(in), columnQuerySource().log(in), columnSkillName().log(in), columnAgentName().log(in),
		columnMCPServerName().log(in), columnMCPToolName().log(in), columnName().log(in), columnToolName().log(in),
		columnText().log(in), columnOutcome().log(in), columnOutcomeMessage().log(in), columnDurationNano().log(in),
	} {
		maps.Copy(columns, enrichedColumns(t, enricher, record))
	}
	return columns
}

func inboundTestBoolAttribute(key string, value bool) *otelv1.InboundLogRecord_KeyValue {
	return (&otelv1.InboundLogRecord_KeyValue_builder{
		Key:   &key,
		Value: (&otelv1.InboundLogRecord_AnyValue_builder{BoolValue: &value}).Build(),
	}).Build()
}

func TestWhatHappenedColumnsForClaudeCode(t *testing.T) {
	t.Parallel()

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

		columns := whatHappened(t, in, record)
		require.Equal(t, "claude-sonnet-4", columns[ModelColumnKey].AsString())
		require.Equal(t, "user_prompt", columns[QuerySourceColumnKey].AsString())
		require.Equal(t, "deploy", columns[SkillNameColumnKey].AsString())
		require.Equal(t, "reviewer", columns[AgentNameColumnKey].AsString())
		require.Equal(t, "github", columns[MCPServerNameColumnKey].AsString())
		require.Equal(t, "get_pr", columns[MCPToolNameColumnKey].AsString())
		require.Equal(t, int64(1_500_000_000), columns[DurationNanoColumnKey].AsInt64())
		require.NotContains(t, columns, OutcomeColumnKey, "a request records that a call was made, not how it went")
		require.NotContains(t, columns, NameColumnKey, "a request has no subject with a name")
		require.NotContains(t, columns, ToolNameColumnKey)
		require.NotContains(t, columns, TextColumnKey)
		require.Zero(t, counterValue(t, reader, meterColumnEnricherMissing, attr.AgentEventColumn("outcome")))
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

		columns := whatHappened(t, in, record)
		require.NotContains(t, columns, SkillNameColumnKey)
		require.NotContains(t, columns, AgentNameColumnKey)
		require.NotContains(t, columns, MCPServerNameColumnKey)
		for _, column := range []string{"skill_name", "agent_name", "mcp_server_name", "mcp_tool_name"} {
			require.Zero(t, counterValue(t, reader, meterColumnEnricherMissing, attr.AgentEventColumn(column)), column)
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

		columns := whatHappened(t, in, record)
		require.Equal(t, "Bash", columns[NameColumnKey].AsString(), "the tool is the subject")
		require.Equal(t, "Bash", columns[ToolNameColumnKey].AsString(), "and the deprecated column says the same")
		require.Equal(t, dialect.OutcomeError, columns[OutcomeColumnKey].AsString())
		require.Equal(t, "ShellError", columns[OutcomeMessageColumnKey].AsString())
		require.Equal(t, int64(12_000_000), columns[DurationNanoColumnKey].AsInt64())
		require.NotContains(t, columns, ModelColumnKey)
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

		columns := whatHappened(t, in, record)
		require.Equal(t, dialect.OutcomeOK, columns[OutcomeColumnKey].AsString())
		require.NotContains(t, columns, OutcomeMessageColumnKey)
		require.Zero(t, counterValue(t, reader, meterColumnEnricherMissing, attr.AgentEventColumn("outcome_message")))
		require.NotContains(t, columns, MCPServerNameColumnKey, "a built-in tool has no MCP server")
		require.Zero(t, counterValue(t, reader, meterColumnEnricherMissing, attr.AgentEventColumn("mcp_server_name")))
		require.Zero(t, counterValue(t, reader, meterColumnEnricherMissing, attr.AgentEventColumn("mcp_tool_name")))
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

		columns := whatHappened(t, in, record)
		require.Equal(t, "whoami", columns[MCPToolNameColumnKey].AsString())
		require.NotContains(t, columns, MCPServerNameColumnKey)
		require.Equal(t, int64(1), counterValue(t, reader, meterColumnEnricherMissing, attr.AgentEventColumn("mcp_server_name")))
		require.Zero(t, counterValue(t, reader, meterColumnEnricherMissing, attr.AgentEventColumn("mcp_tool_name")))
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
		columns := whatHappened(t, in, rejected)
		require.Equal(t, dialect.OutcomeRejected, columns[OutcomeColumnKey].AsString())
		require.Equal(t, "reject", columns[TextColumnKey].AsString())
		require.Equal(t, "Bash", columns[NameColumnKey].AsString())
		require.Equal(t, "github", columns[MCPServerNameColumnKey].AsString())
		require.Equal(t, "create_issue", columns[MCPToolNameColumnKey].AsString())

		accepted := inboundTestLog(claudeCodeScopeName, "claude-code", "tool_decision",
			logStringAttribute("tool_name", "Bash"),
			logStringAttribute("decision_type", "accept"),
		)
		columns = whatHappened(t, in, accepted)
		require.NotContains(t, columns, OutcomeColumnKey, "the result row carries how an accepted call went")
		require.Zero(t, counterValue(t, reader, meterColumnEnricherMissing, attr.AgentEventColumn("outcome")))
	})

	t.Run("the api event types imply their outcome", func(t *testing.T) {
		t.Parallel()
		in := NewInstruments(testenv.NewLogger(t), testenv.NewMeterProvider(t))

		response := inboundTestLog(claudeCodeScopeName, "claude-code", "assistant_response",
			logStringAttribute("model", "claude-sonnet-4"),
			logStringAttribute("response", "Done."),
		)
		columns := whatHappened(t, in, response)
		require.Equal(t, dialect.OutcomeOK, columns[OutcomeColumnKey].AsString())
		require.Equal(t, "Done.", columns[TextColumnKey].AsString())
		require.Equal(t, "claude-sonnet-4", columns[ModelColumnKey].AsString())
		require.NotContains(t, columns, OutcomeMessageColumnKey)

		apiError := inboundTestLog(claudeCodeScopeName, "claude-code", "api_error",
			logStringAttribute("model", "claude-sonnet-4"),
			logStringAttribute("error", "overloaded"),
		)
		columns = whatHappened(t, in, apiError)
		require.Equal(t, dialect.OutcomeError, columns[OutcomeColumnKey].AsString())
		require.Equal(t, "overloaded", columns[OutcomeMessageColumnKey].AsString())
		require.Equal(t, "overloaded", columns[TextColumnKey].AsString())

		refusal := inboundTestLog(claudeCodeScopeName, "claude-code", "api_refusal", logStringAttribute("model", "claude-sonnet-4"))
		columns = whatHappened(t, in, refusal)
		require.Equal(t, dialect.OutcomeRefused, columns[OutcomeColumnKey].AsString())
		require.Equal(t, "claude-sonnet-4", columns[ModelColumnKey].AsString())
	})

	t.Run("a payload capture carries its model and nothing about how it went", func(t *testing.T) {
		t.Parallel()
		reader, meterProvider := readableMeter(t)
		in := NewInstruments(testenv.NewLogger(t), meterProvider)

		withModel := inboundTestLog(claudeCodeScopeName, "claude-code", "api_request_body",
			logStringAttribute("model", "claude-sonnet-4"),
			logStringAttribute("body", `{"messages":[]}`),
		)
		columns := whatHappened(t, in, withModel)
		require.Equal(t, "claude-sonnet-4", columns[ModelColumnKey].AsString())
		require.NotContains(t, columns, OutcomeColumnKey)
		require.NotContains(t, columns, TextColumnKey, "the body stays whole in the attributes")

		withoutModel := inboundTestLog(claudeCodeScopeName, "claude-code", "api_response_body", logStringAttribute("body", `{}`))
		require.NotContains(t, whatHappened(t, in, withoutModel), ModelColumnKey)
		require.Zero(t, counterValue(t, reader, meterColumnEnricherMissing, attr.AgentEventColumn("model")), "a capture states the model only sometimes")
	})

	t.Run("a compaction states its outcome and, when it failed, why", func(t *testing.T) {
		t.Parallel()
		reader, meterProvider := readableMeter(t)
		in := NewInstruments(testenv.NewLogger(t), meterProvider)

		ok := inboundTestLog(claudeCodeScopeName, "claude-code", "compaction",
			inboundTestBoolAttribute("success", true),
			inboundTestDoubleAttribute("duration_ms", 2500),
		)
		columns := whatHappened(t, in, ok)
		require.Equal(t, dialect.OutcomeOK, columns[OutcomeColumnKey].AsString())
		require.NotContains(t, columns, OutcomeMessageColumnKey)
		require.NotContains(t, columns, DurationNanoColumnKey, "a compaction's duration is housekeeping, not a request or a tool")

		failed := inboundTestLog(claudeCodeScopeName, "claude-code", "compaction",
			inboundTestBoolAttribute("success", false),
			logStringAttribute("error", "context too large"),
		)
		columns = whatHappened(t, in, failed)
		require.Equal(t, dialect.OutcomeError, columns[OutcomeColumnKey].AsString())
		require.Equal(t, "context too large", columns[OutcomeMessageColumnKey].AsString())
		require.Zero(t, counterValue(t, reader, meterColumnEnricherMissing, attr.AgentEventColumn("outcome_message")))
	})

	t.Run("a prompt whose words were not logged is a choice, not a gap", func(t *testing.T) {
		t.Parallel()
		reader, meterProvider := readableMeter(t)
		in := NewInstruments(testenv.NewLogger(t), meterProvider)
		record := inboundTestLog(claudeCodeScopeName, "claude-code", "user_prompt", inboundTestIntAttribute("prompt_length", 13))

		require.NotContains(t, whatHappened(t, in, record), TextColumnKey)
		require.Zero(t, counterValue(t, reader, meterColumnEnricherMissing, attr.AgentEventColumn("text")))
	})
}

func TestWhatHappenedColumnsForCodex(t *testing.T) {
	t.Parallel()

	t.Run("a response.completed is a request with a model and a duration but no outcome", func(t *testing.T) {
		t.Parallel()
		in := NewInstruments(testenv.NewLogger(t), testenv.NewMeterProvider(t))
		record := inboundTestLog(codexScopeName, "codex", "codex.sse_event",
			logStringAttribute("event.kind", "response.completed"),
			logStringAttribute("model", "gpt-5"),
			inboundTestDoubleAttribute("duration_ms", 800),
		)

		columns := whatHappened(t, in, record)
		require.Equal(t, "gpt-5", columns[ModelColumnKey].AsString())
		require.Equal(t, int64(800_000_000), columns[DurationNanoColumnKey].AsInt64())
		require.NotContains(t, columns, OutcomeColumnKey, "a completed request is a request; the type carries that it completed")
	})

	t.Run("a tool result names the tool in both columns and states how it went", func(t *testing.T) {
		t.Parallel()
		in := NewInstruments(testenv.NewLogger(t), testenv.NewMeterProvider(t))
		record := inboundTestLog(codexScopeName, "codex", "codex.tool_result",
			logStringAttribute("tool_name", "shell"),
			logStringAttribute("call_id", "call-1"),
			inboundTestBoolAttribute("success", false),
			logStringAttribute("error", "exit 1"),
		)

		columns := whatHappened(t, in, record)
		require.Equal(t, "shell", columns[NameColumnKey].AsString())
		require.Equal(t, "shell", columns[ToolNameColumnKey].AsString())
		require.Equal(t, dialect.OutcomeError, columns[OutcomeColumnKey].AsString())
		require.Equal(t, "exit 1", columns[OutcomeMessageColumnKey].AsString())
	})
}

func TestWhatHappenedColumnsForSemconv(t *testing.T) {
	t.Parallel()

	t.Run("a tool call names its tool", func(t *testing.T) {
		t.Parallel()
		in := NewInstruments(testenv.NewLogger(t), testenv.NewMeterProvider(t))
		record := inboundTestLog("my-agent", "my-agent", "gen_ai.client.inference.operation.details",
			logStringAttribute("gen_ai.operation.name", "execute_tool"),
			logStringAttribute("gen_ai.tool.name", "search"),
		)

		columns := whatHappened(t, in, record)
		require.Equal(t, "search", columns[NameColumnKey].AsString())
		require.Equal(t, "search", columns[ToolNameColumnKey].AsString())
		require.NotContains(t, columns, OutcomeColumnKey, "a tool call records that a call was made, not how it went")
	})

	t.Run("a chat record names its model", func(t *testing.T) {
		t.Parallel()
		in := NewInstruments(testenv.NewLogger(t), testenv.NewMeterProvider(t))
		record := inboundTestLog("litellm", "litellm", "gen_ai.client.inference.operation.details",
			logStringAttribute("gen_ai.operation.name", "chat"),
			logStringAttribute("gen_ai.response.model", "gpt-4o-2024-08-06"),
			logStringAttribute("gen_ai.agent.name", "planner"),
		)

		columns := whatHappened(t, in, record)
		require.Equal(t, "gpt-4o-2024-08-06", columns[ModelColumnKey].AsString())
		require.Equal(t, "planner", columns[AgentNameColumnKey].AsString())
	})
}

func TestRequirementLevelsSayWhenAnAbsenceIsAGap(t *testing.T) {
	t.Parallel()

	skill := getter[string]{log: dialect.LogDialect.SkillName, span: dialect.SpanDialect.SkillName}
	message := getter[string]{log: dialect.LogDialect.OutcomeMessage, span: dialect.SpanDialect.OutcomeMessage}
	tool := getter[string]{log: dialect.LogDialect.ToolName, span: dialect.SpanDialect.ToolName}

	succeeded := inboundTestLog(claudeCodeScopeName, "claude-code", "tool_result", inboundTestBoolAttribute("success", true))
	d := dialect.ForLog(succeeded)

	// Recommended and Opt-In: an absent value is not required, so not a gap.
	_, _, err := recommended(skill).log(d, succeeded)
	require.ErrorIs(t, err, errNotRequired)
	_, _, err = optIn(skill).log(d, succeeded)
	require.ErrorIs(t, err, errNotRequired)

	// Conditionally Required: the message of a result that succeeded is not
	// required, and the message of one that failed is, so its absence there
	// comes back as a plain gap for the caller to count.
	_, _, err = conditionallyRequired(message, outcomeIsError).log(d, succeeded)
	require.ErrorIs(t, err, errNotRequired)
	failed := inboundTestLog(claudeCodeScopeName, "claude-code", "tool_result", inboundTestBoolAttribute("success", false))
	key, _, err := conditionallyRequired(message, outcomeIsError).log(dialect.ForLog(failed), failed)
	require.NoError(t, err)
	require.Empty(t, key)

	// A stated value is written at every level.
	named := inboundTestLog(claudeCodeScopeName, "claude-code", "tool_result", logStringAttribute("tool_name", "Bash"))
	key, value, err := recommended(tool).log(dialect.ForLog(named), named)
	require.NoError(t, err)
	require.Equal(t, "tool_name", key)
	require.Equal(t, "Bash", value)

	// statedBy is the condition behind the MCP pair: one half is required
	// once the other half is present.
	require.True(t, statedBy(tool).log(dialect.ForLog(named), named))
	require.False(t, statedBy(tool).log(d, succeeded))
}
