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

// identity runs the six who-and-where column enrichers over one record,
// as the transform does, and indexes what they wrote by key.
func identity(t *testing.T, m *Instruments, record *otelv1.InboundLogRecord) map[attribute.Key]attribute.Value {
	t.Helper()
	columns := map[attribute.Key]attribute.Value{}
	for _, definition := range identityColumns() {
		maps.Copy(columns, enrichedColumns(t, definition.log(m), record))
	}
	return columns
}

func TestIdentityColumnsForClaudeCode(t *testing.T) {
	t.Parallel()

	m := NewInstruments(testenv.NewLogger(t), testenv.NewMeterProvider(t))
	who := []*otelv1.InboundLogRecord_KeyValue{
		logStringAttribute("session.id", "session-1"),
		logStringAttribute("prompt.id", "turn-1"),
		logStringAttribute("user.email", "dev@example.com"),
		logStringAttribute("user.account_id", "acct-1"),
		logStringAttribute("organization.id", "anthropic-org-1"),
	}

	t.Run("a prompt names its transcript message", func(t *testing.T) {
		t.Parallel()
		record := inboundTestLog(claudeCodeScopeName, "claude-code", "user_prompt", append(who, logStringAttribute("message.uuid", "message-1"))...)

		columns := identity(t, m, record)
		require.Len(t, columns, 6)
		require.Equal(t, "session-1", columns[SessionIDColumnKey].AsString())
		require.Equal(t, "turn-1", columns[TurnIDColumnKey].AsString())
		require.Equal(t, "message-1", columns[EventIDColumnKey].AsString())
		require.Equal(t, "dev@example.com", columns[UserEmailColumnKey].AsString())
		require.Equal(t, "acct-1", columns[ExternalUserIDColumnKey].AsString())
		require.Equal(t, "anthropic-org-1", columns[ExternalOrgIDColumnKey].AsString())
	})

	t.Run("an api_request names the request", func(t *testing.T) {
		t.Parallel()
		record := inboundTestLog(claudeCodeScopeName, "claude-code", "api_request", append(who, logStringAttribute("request_id", "req_011"))...)
		require.Equal(t, "req_011", identity(t, m, record)[EventIDColumnKey].AsString())
	})

	t.Run("a tool_result names the tool invocation", func(t *testing.T) {
		t.Parallel()
		record := inboundTestLog(claudeCodeScopeName, "claude-code", "tool_result", append(who, logStringAttribute("tool_use_id", "toolu_1"))...)
		require.Equal(t, "toolu_1", identity(t, m, record)[EventIDColumnKey].AsString())
	})

	t.Run("a response body lands beside the request it answers", func(t *testing.T) {
		t.Parallel()
		record := inboundTestLog(claudeCodeScopeName, "claude-code", "api_response_body", append(who, logStringAttribute("request_id", "req_011"))...)
		require.Equal(t, "req_011", identity(t, m, record)[EventIDColumnKey].AsString())
	})

	t.Run("a request body and a compaction get no event id, so the writer keeps the record id", func(t *testing.T) {
		t.Parallel()
		reader, meterProvider := readableMeter(t)
		counted := NewInstruments(testenv.NewLogger(t), meterProvider)

		body := inboundTestLog(claudeCodeScopeName, "claude-code", "api_request_body", append(who, logStringAttribute("request_id", "req_011"))...)
		columns := identity(t, counted, body)
		require.NotContains(t, columns, EventIDColumnKey, "the type is not in the table, even though a request id is present")
		require.Equal(t, "session-1", columns[SessionIDColumnKey].AsString(), "the session columns still apply")

		compaction := inboundTestLog(claudeCodeScopeName, "claude-code", "compaction", who...)
		require.NotContains(t, identity(t, counted, compaction), EventIDColumnKey)

		require.Zero(t, counterValue(t, reader, meterColumnEnricherMissing, attr.AgentEventColumn("event_id")), "not in the table is never, not missing")
	})

	t.Run("an unclassified record gets none of them", func(t *testing.T) {
		t.Parallel()
		record := inboundTestLog(claudeCodeScopeName, "claude-code", "hook_registered", who...)
		require.Empty(t, identity(t, m, record))
	})
}

func TestIdentityColumnsCountWhatAProviderNeverStates(t *testing.T) {
	t.Parallel()

	reader, meterProvider := readableMeter(t)
	m := NewInstruments(testenv.NewLogger(t), meterProvider)

	// Codex states a conversation, a user and a response id, but no turn
	// and no organization, so those two are counted on its api_request.
	record := inboundTestLog(codexScopeName, "codex", "codex.sse_event",
		logStringAttribute("event.kind", "response.completed"),
		logStringAttribute("conversation.id", "conv-1"),
		logStringAttribute("user.email", "dev@example.com"),
		logStringAttribute("user.account_id", "acct-1"),
		logStringAttribute("response.id", "resp-1"),
	)

	columns := identity(t, m, record)
	require.Equal(t, "conv-1", columns[SessionIDColumnKey].AsString())
	require.Equal(t, "resp-1", columns[EventIDColumnKey].AsString())
	require.Equal(t, "dev@example.com", columns[UserEmailColumnKey].AsString())
	require.Equal(t, "acct-1", columns[ExternalUserIDColumnKey].AsString())
	require.NotContains(t, columns, TurnIDColumnKey)
	require.NotContains(t, columns, ExternalOrgIDColumnKey)
	require.Equal(t, int64(1), counterValue(t, reader, meterColumnEnricherMissing, attr.AgentEventColumn("turn_id")))
	require.Equal(t, int64(1), counterValue(t, reader, meterColumnEnricherMissing, attr.AgentEventColumn("external_org_id")))
	require.Zero(t, counterValue(t, reader, meterColumnEnricherMissing, attr.AgentEventColumn("session_id")))
}

func TestIdentityColumnsForSemconv(t *testing.T) {
	t.Parallel()

	m := NewInstruments(testenv.NewLogger(t), testenv.NewMeterProvider(t))

	t.Run("a chat record names its conversation and response", func(t *testing.T) {
		t.Parallel()
		record := inboundTestLog("litellm", "litellm", "gen_ai.client.inference.operation.details",
			logStringAttribute("gen_ai.operation.name", "chat"),
			logStringAttribute("gen_ai.conversation.id", "session-9"),
			logStringAttribute("gen_ai.response.id", "resp-1"),
			logStringAttribute("user.email", "dev@example.com"),
		)

		columns := identity(t, m, record)
		require.Equal(t, "session-9", columns[SessionIDColumnKey].AsString())
		require.Equal(t, "resp-1", columns[EventIDColumnKey].AsString())
		require.Equal(t, "dev@example.com", columns[UserEmailColumnKey].AsString())
		require.NotContains(t, columns, TurnIDColumnKey)
	})

	t.Run("a tool call names the call", func(t *testing.T) {
		t.Parallel()
		record := inboundTestLog("my-agent", "my-agent", "gen_ai.client.inference.operation.details",
			logStringAttribute("gen_ai.operation.name", "execute_tool"),
			logStringAttribute("gen_ai.tool.call.id", "call-1"),
		)
		require.Equal(t, "call-1", identity(t, m, record)[EventIDColumnKey].AsString())
	})
}

func TestEventIDTableNamesEveryTypeWithASubject(t *testing.T) {
	t.Parallel()

	definition, ok := columnEventID().(column[string])
	require.True(t, ok)
	require.Len(t, definition.byType, 9)
	require.NotContains(t, definition.byType, dialect.EventTypeAPIRequestBody)
	require.NotContains(t, definition.byType, dialect.EventTypeCompaction)
}

// operation runs the what-happened column enrichers over one record, as
// the transform does, and indexes what they wrote by key.
func operation(t *testing.T, in *Instruments, record *otelv1.InboundLogRecord) map[attribute.Key]attribute.Value {
	t.Helper()
	columns := map[attribute.Key]attribute.Value{}
	for _, definition := range operationColumns() {
		maps.Copy(columns, enrichedColumns(t, definition.log(in), record))
	}
	return columns
}

func inboundTestBoolAttribute(key string, value bool) *otelv1.InboundLogRecord_KeyValue {
	return (&otelv1.InboundLogRecord_KeyValue_builder{
		Key:   &key,
		Value: (&otelv1.InboundLogRecord_AnyValue_builder{BoolValue: &value}).Build(),
	}).Build()
}

func TestOperationColumnsForClaudeCode(t *testing.T) {
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

		columns := operation(t, in, record)
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

		columns := operation(t, in, record)
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

		columns := operation(t, in, record)
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

		columns := operation(t, in, record)
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

		columns := operation(t, in, record)
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
		columns := operation(t, in, rejected)
		require.Equal(t, dialect.OutcomeRejected, columns[OutcomeColumnKey].AsString())
		require.Equal(t, "reject", columns[TextColumnKey].AsString())
		require.Equal(t, "Bash", columns[NameColumnKey].AsString())
		require.Equal(t, "github", columns[MCPServerNameColumnKey].AsString())
		require.Equal(t, "create_issue", columns[MCPToolNameColumnKey].AsString())

		accepted := inboundTestLog(claudeCodeScopeName, "claude-code", "tool_decision",
			logStringAttribute("tool_name", "Bash"),
			logStringAttribute("decision_type", "accept"),
		)
		columns = operation(t, in, accepted)
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
		columns := operation(t, in, response)
		require.Equal(t, dialect.OutcomeOK, columns[OutcomeColumnKey].AsString())
		require.Equal(t, "Done.", columns[TextColumnKey].AsString())
		require.Equal(t, "claude-sonnet-4", columns[ModelColumnKey].AsString())
		require.NotContains(t, columns, OutcomeMessageColumnKey)

		apiError := inboundTestLog(claudeCodeScopeName, "claude-code", "api_error",
			logStringAttribute("model", "claude-sonnet-4"),
			logStringAttribute("error", "overloaded"),
		)
		columns = operation(t, in, apiError)
		require.Equal(t, dialect.OutcomeError, columns[OutcomeColumnKey].AsString())
		require.Equal(t, "overloaded", columns[OutcomeMessageColumnKey].AsString())
		require.Equal(t, "overloaded", columns[TextColumnKey].AsString())

		refusal := inboundTestLog(claudeCodeScopeName, "claude-code", "api_refusal", logStringAttribute("model", "claude-sonnet-4"))
		columns = operation(t, in, refusal)
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
		columns := operation(t, in, withModel)
		require.Equal(t, "claude-sonnet-4", columns[ModelColumnKey].AsString())
		require.NotContains(t, columns, OutcomeColumnKey)
		require.NotContains(t, columns, TextColumnKey, "the body stays whole in the attributes")

		withoutModel := inboundTestLog(claudeCodeScopeName, "claude-code", "api_response_body", logStringAttribute("body", `{}`))
		require.NotContains(t, operation(t, in, withoutModel), ModelColumnKey)
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
		columns := operation(t, in, ok)
		require.Equal(t, dialect.OutcomeOK, columns[OutcomeColumnKey].AsString())
		require.NotContains(t, columns, OutcomeMessageColumnKey)
		require.NotContains(t, columns, DurationNanoColumnKey, "a compaction's duration is housekeeping, not a request or a tool")

		failed := inboundTestLog(claudeCodeScopeName, "claude-code", "compaction",
			inboundTestBoolAttribute("success", false),
			logStringAttribute("error", "context too large"),
		)
		columns = operation(t, in, failed)
		require.Equal(t, dialect.OutcomeError, columns[OutcomeColumnKey].AsString())
		require.Equal(t, "context too large", columns[OutcomeMessageColumnKey].AsString())
		require.Zero(t, counterValue(t, reader, meterColumnEnricherMissing, attr.AgentEventColumn("outcome_message")))
	})

	t.Run("a prompt whose words were not logged is a choice, not a gap", func(t *testing.T) {
		t.Parallel()
		reader, meterProvider := readableMeter(t)
		in := NewInstruments(testenv.NewLogger(t), meterProvider)
		record := inboundTestLog(claudeCodeScopeName, "claude-code", "user_prompt", inboundTestIntAttribute("prompt_length", 13))

		require.NotContains(t, operation(t, in, record), TextColumnKey)
		require.Zero(t, counterValue(t, reader, meterColumnEnricherMissing, attr.AgentEventColumn("text")))
	})
}

func TestOperationColumnsForCodex(t *testing.T) {
	t.Parallel()

	t.Run("a response.completed is a request with a model and a duration but no outcome", func(t *testing.T) {
		t.Parallel()
		in := NewInstruments(testenv.NewLogger(t), testenv.NewMeterProvider(t))
		record := inboundTestLog(codexScopeName, "codex", "codex.sse_event",
			logStringAttribute("event.kind", "response.completed"),
			logStringAttribute("model", "gpt-5"),
			inboundTestDoubleAttribute("duration_ms", 800),
		)

		columns := operation(t, in, record)
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

		columns := operation(t, in, record)
		require.Equal(t, "shell", columns[NameColumnKey].AsString())
		require.Equal(t, "shell", columns[ToolNameColumnKey].AsString())
		require.Equal(t, dialect.OutcomeError, columns[OutcomeColumnKey].AsString())
		require.Equal(t, "exit 1", columns[OutcomeMessageColumnKey].AsString())
	})
}

func TestOperationColumnsForSemconv(t *testing.T) {
	t.Parallel()

	t.Run("a tool call names its tool", func(t *testing.T) {
		t.Parallel()
		in := NewInstruments(testenv.NewLogger(t), testenv.NewMeterProvider(t))
		record := inboundTestLog("my-agent", "my-agent", "gen_ai.client.inference.operation.details",
			logStringAttribute("gen_ai.operation.name", "execute_tool"),
			logStringAttribute("gen_ai.tool.name", "search"),
		)

		columns := operation(t, in, record)
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

		columns := operation(t, in, record)
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

// A tool_call log record is the moment the call started and states no
// duration; that is Recommended, not a gap, so nothing is counted.
func TestOperationColumnsLeaveAToolCallLogWithoutADurationUncounted(t *testing.T) {
	t.Parallel()

	reader, meterProvider := readableMeter(t)
	in := NewInstruments(testenv.NewLogger(t), meterProvider)
	record := inboundTestLog("my-agent", "my-agent", "gen_ai.client.inference.operation.details",
		logStringAttribute("gen_ai.operation.name", "execute_tool"),
		logStringAttribute("gen_ai.tool.name", "search"),
	)

	require.NotContains(t, operation(t, in, record), DurationNanoColumnKey)
	require.Zero(t, counterValue(t, reader, meterColumnEnricherMissing, attr.AgentEventColumn("duration_nano")))
}

// usage runs the five usage column enrichers over one record, as the
// transform does, and indexes what they wrote by key.
func usage(t *testing.T, in *Instruments, record *otelv1.InboundLogRecord) map[attribute.Key]attribute.Value {
	t.Helper()
	columns := map[attribute.Key]attribute.Value{}
	for _, definition := range usageColumns() {
		maps.Copy(columns, enrichedColumns(t, definition.log(in), record))
	}
	return columns
}

func TestUsageColumnsForClaudeCode(t *testing.T) {
	t.Parallel()

	t.Run("an api_request states its tokens and its cost in dollars", func(t *testing.T) {
		t.Parallel()
		in := NewInstruments(testenv.NewLogger(t), testenv.NewMeterProvider(t))
		record := inboundTestLog(claudeCodeScopeName, "claude-code", "api_request",
			inboundTestIntAttribute("input_tokens", 120),
			inboundTestIntAttribute("output_tokens", 30),
			logStringAttribute("cache_read_tokens", "5"),
			inboundTestIntAttribute("cache_creation_tokens", 7),
			inboundTestDoubleAttribute("cost_usd", 0.0125),
		)

		columns := usage(t, in, record)
		require.Len(t, columns, 5)
		require.Equal(t, int64(120), columns[InputTokensColumnKey].AsInt64())
		require.Equal(t, int64(30), columns[OutputTokensColumnKey].AsInt64())
		require.Equal(t, int64(5), columns[CacheReadTokensColumnKey].AsInt64(), "stringified numbers still count")
		require.Equal(t, int64(7), columns[CacheWriteTokensColumnKey].AsInt64())
		require.InDelta(t, 0.0125, columns[CostUSDColumnKey].AsFloat64(), 1e-9)
	})

	t.Run("a cost stated in micros lands in dollars", func(t *testing.T) {
		t.Parallel()
		in := NewInstruments(testenv.NewLogger(t), testenv.NewMeterProvider(t))
		record := inboundTestLog(claudeCodeScopeName, "claude-code", "api_request",
			inboundTestIntAttribute("input_tokens", 1),
			inboundTestIntAttribute("cost_usd_micros", 12_500),
		)

		require.InDelta(t, 0.0125, usage(t, in, record)[CostUSDColumnKey].AsFloat64(), 1e-9)
	})

	t.Run("a cost stated as zero is written as zero, not dropped", func(t *testing.T) {
		t.Parallel()
		in := NewInstruments(testenv.NewLogger(t), testenv.NewMeterProvider(t))
		record := inboundTestLog(claudeCodeScopeName, "claude-code", "api_request",
			inboundTestIntAttribute("input_tokens", 1),
			inboundTestDoubleAttribute("cost_usd", 0),
		)

		columns := usage(t, in, record)
		require.Contains(t, columns, CostUSDColumnKey, "a stated zero is still stated")
		require.Equal(t, attribute.FLOAT64, columns[CostUSDColumnKey].Type())
		require.InDelta(t, 0, columns[CostUSDColumnKey].AsFloat64(), 1e-12)
	})

	t.Run("a compaction and a tool_result carry no usage, whatever their attributes say", func(t *testing.T) {
		t.Parallel()
		reader, meterProvider := readableMeter(t)
		in := NewInstruments(testenv.NewLogger(t), meterProvider)

		compaction := inboundTestLog(claudeCodeScopeName, "claude-code", "compaction",
			inboundTestIntAttribute("pre_tokens", 150_000),
			inboundTestIntAttribute("post_tokens", 20_000),
			inboundTestIntAttribute("input_tokens", 99),
		)
		require.Empty(t, usage(t, in, compaction), "a compaction's token counts are housekeeping, not a request's usage")

		result := inboundTestLog(claudeCodeScopeName, "claude-code", "tool_result",
			logStringAttribute("tool_name", "Bash"),
			inboundTestIntAttribute("input_tokens", 99),
			inboundTestDoubleAttribute("cost_usd", 1),
		)
		require.Empty(t, usage(t, in, result))

		for _, column := range []string{"input_tokens", "output_tokens", "cache_read_tokens", "cache_write_tokens", "cost_usd"} {
			require.Zero(t, counterValue(t, reader, meterColumnEnricherMissing, attr.AgentEventColumn(column)), column)
		}
	})
}

func TestUsageColumnsForCodex(t *testing.T) {
	t.Parallel()

	t.Run("a response.completed lands as disjoint input and cache-read tokens", func(t *testing.T) {
		t.Parallel()
		reader, meterProvider := readableMeter(t)
		in := NewInstruments(testenv.NewLogger(t), meterProvider)
		record := inboundTestLog(codexScopeName, "codex", "codex.sse_event",
			logStringAttribute("event.kind", "response.completed"),
			logStringAttribute("input_token_count", "100"),
			inboundTestIntAttribute("cached_token_count", 30),
			inboundTestIntAttribute("output_token_count", 7),
		)

		columns := usage(t, in, record)
		require.Equal(t, int64(70), columns[InputTokensColumnKey].AsInt64(), "input excludes cache reads")
		require.Equal(t, int64(30), columns[CacheReadTokensColumnKey].AsInt64())
		require.Equal(t, int64(7), columns[OutputTokensColumnKey].AsInt64())
		require.NotContains(t, columns, CacheWriteTokensColumnKey, "Codex reports no cache writes")
		require.NotContains(t, columns, CostUSDColumnKey, "Codex reports no cost")
		require.Equal(t, int64(1), counterValue(t, reader, meterColumnEnricherMissing, attr.AgentEventColumn("cache_write_tokens")))
		require.Equal(t, int64(1), counterValue(t, reader, meterColumnEnricherMissing, attr.AgentEventColumn("cost_usd")))
		require.Zero(t, counterValue(t, reader, meterColumnEnricherMissing, attr.AgentEventColumn("input_tokens")))
	})

	t.Run("a cached count larger than the input is clamped so bad data never increases usage", func(t *testing.T) {
		t.Parallel()
		in := NewInstruments(testenv.NewLogger(t), testenv.NewMeterProvider(t))
		record := inboundTestLog(codexScopeName, "codex", "codex.sse_event",
			logStringAttribute("event.kind", "response.completed"),
			inboundTestIntAttribute("input_token_count", 10),
			inboundTestIntAttribute("cached_token_count", 50),
		)

		columns := usage(t, in, record)
		require.Zero(t, columns[InputTokensColumnKey].AsInt64())
		require.Equal(t, int64(10), columns[CacheReadTokensColumnKey].AsInt64())
	})
}

func TestUsageColumnsForSemconv(t *testing.T) {
	t.Parallel()

	in := NewInstruments(testenv.NewLogger(t), testenv.NewMeterProvider(t))
	record := inboundTestLog("litellm", "litellm", "gen_ai.client.inference.operation.details",
		logStringAttribute("gen_ai.operation.name", "chat"),
		logStringAttribute("gen_ai.usage.input_tokens", "200"),
		logStringAttribute("gen_ai.usage.output_tokens", "50"),
		logStringAttribute("gen_ai.usage.cost", "0.002"),
	)

	columns := usage(t, in, record)
	require.Equal(t, int64(200), columns[InputTokensColumnKey].AsInt64())
	require.Equal(t, int64(50), columns[OutputTokensColumnKey].AsInt64())
	require.InDelta(t, 0.002, columns[CostUSDColumnKey].AsFloat64(), 1e-9)
}

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
		// An api_request is not in the outcome table, so the span's error
		// status lands nowhere and is not counted.
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
		require.Equal(t, int64(1), counterValue(t, reader, meterColumnEnricherMissing, attr.AgentEventSurface(missingLabelOther)))
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
