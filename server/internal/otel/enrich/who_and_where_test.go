package enrich

import (
	"maps"

	otelv1 "github.com/speakeasy-api/gram/infra/gen/gram/otel/v1"
	"testing"

	"github.com/speakeasy-api/gram/server/internal/attr"
	"github.com/speakeasy-api/gram/server/internal/otel/dialect"
	"github.com/speakeasy-api/gram/server/internal/testenv"
	"github.com/stretchr/testify/require"
	"go.opentelemetry.io/otel/attribute"
)

// whoAndWhere runs the six who-and-where column enrichers over one record,
// as the transform does, and indexes what they wrote by key.
func whoAndWhere(t *testing.T, m *Instruments, record *otelv1.InboundLogRecord) map[attribute.Key]attribute.Value {
	t.Helper()
	columns := map[attribute.Key]attribute.Value{}
	for _, definition := range whoAndWhereColumns() {
		maps.Copy(columns, enrichedColumns(t, definition.log(m), record))
	}
	return columns
}

func TestWhoAndWhereColumnsForClaudeCode(t *testing.T) {
	t.Parallel()

	m := NewInstruments(testenv.NewLogger(t), testenv.NewMeterProvider(t))
	identity := []*otelv1.InboundLogRecord_KeyValue{
		logStringAttribute("session.id", "session-1"),
		logStringAttribute("prompt.id", "turn-1"),
		logStringAttribute("user.email", "dev@example.com"),
		logStringAttribute("user.account_id", "acct-1"),
		logStringAttribute("organization.id", "anthropic-org-1"),
	}

	t.Run("a prompt names its transcript message", func(t *testing.T) {
		t.Parallel()
		record := inboundTestLog(claudeCodeScopeName, "claude-code", "user_prompt", append(identity, logStringAttribute("message.uuid", "message-1"))...)

		columns := whoAndWhere(t, m, record)
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
		record := inboundTestLog(claudeCodeScopeName, "claude-code", "api_request", append(identity, logStringAttribute("request_id", "req_011"))...)
		require.Equal(t, "req_011", whoAndWhere(t, m, record)[EventIDColumnKey].AsString())
	})

	t.Run("a tool_result names the tool invocation", func(t *testing.T) {
		t.Parallel()
		record := inboundTestLog(claudeCodeScopeName, "claude-code", "tool_result", append(identity, logStringAttribute("tool_use_id", "toolu_1"))...)
		require.Equal(t, "toolu_1", whoAndWhere(t, m, record)[EventIDColumnKey].AsString())
	})

	t.Run("a response body lands beside the request it answers", func(t *testing.T) {
		t.Parallel()
		record := inboundTestLog(claudeCodeScopeName, "claude-code", "api_response_body", append(identity, logStringAttribute("request_id", "req_011"))...)
		require.Equal(t, "req_011", whoAndWhere(t, m, record)[EventIDColumnKey].AsString())
	})

	t.Run("a request body and a compaction get no event id, so the writer keeps the record id", func(t *testing.T) {
		t.Parallel()
		reader, meterProvider := readableMeter(t)
		counted := NewInstruments(testenv.NewLogger(t), meterProvider)

		body := inboundTestLog(claudeCodeScopeName, "claude-code", "api_request_body", append(identity, logStringAttribute("request_id", "req_011"))...)
		columns := whoAndWhere(t, counted, body)
		require.NotContains(t, columns, EventIDColumnKey, "the type is not in the table, even though a request id is present")
		require.Equal(t, "session-1", columns[SessionIDColumnKey].AsString(), "the session columns still apply")

		compaction := inboundTestLog(claudeCodeScopeName, "claude-code", "compaction", identity...)
		require.NotContains(t, whoAndWhere(t, counted, compaction), EventIDColumnKey)

		require.Zero(t, counterValue(t, reader, meterColumnEnricherMissing, attr.AgentEventColumn("event_id")), "not in the table is never, not missing")
	})

	t.Run("an unclassified record gets none of them", func(t *testing.T) {
		t.Parallel()
		record := inboundTestLog(claudeCodeScopeName, "claude-code", "hook_registered", identity...)
		require.Empty(t, whoAndWhere(t, m, record))
	})
}

func TestWhoAndWhereColumnsCountWhatAProviderNeverStates(t *testing.T) {
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

	columns := whoAndWhere(t, m, record)
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

func TestWhoAndWhereColumnsForSemconv(t *testing.T) {
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

		columns := whoAndWhere(t, m, record)
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
		require.Equal(t, "call-1", whoAndWhere(t, m, record)[EventIDColumnKey].AsString())
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
