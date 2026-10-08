package enrich

import (
	"testing"

	otelv1 "github.com/speakeasy-api/gram/infra/gen/gram/otel/v1"
	"github.com/speakeasy-api/gram/server/internal/attr"
	"github.com/speakeasy-api/gram/server/internal/testenv"
	"github.com/stretchr/testify/require"
)

func TestIdentityForClaudeCode(t *testing.T) {
	t.Parallel()

	in := NewInstruments(testenv.NewLogger(t), testenv.NewMeterProvider(t))
	require.Equal(t, "enrich-identity", (&logIdentity{instruments: in}).Name())
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

		attrs := identity(t, in, record)
		require.Len(t, attrs, 6)
		require.Equal(t, "session-1", attrs[SessionIDColumnKey].AsString())
		require.Equal(t, "turn-1", attrs[TurnIDColumnKey].AsString())
		require.Equal(t, "message-1", attrs[EventIDColumnKey].AsString())
		require.Equal(t, "dev@example.com", attrs[UserEmailColumnKey].AsString())
		require.Equal(t, "acct-1", attrs[ExternalUserIDColumnKey].AsString())
		require.Equal(t, "anthropic-org-1", attrs[ExternalOrgIDColumnKey].AsString())
	})

	t.Run("an api_request names the request", func(t *testing.T) {
		t.Parallel()
		record := inboundTestLog(claudeCodeScopeName, "claude-code", "api_request", append(who, logStringAttribute("request_id", "req_011"))...)
		require.Equal(t, "req_011", identity(t, in, record)[EventIDColumnKey].AsString())
	})

	t.Run("a tool_result names the tool invocation", func(t *testing.T) {
		t.Parallel()
		record := inboundTestLog(claudeCodeScopeName, "claude-code", "tool_result", append(who, logStringAttribute("tool_use_id", "toolu_1"))...)
		require.Equal(t, "toolu_1", identity(t, in, record)[EventIDColumnKey].AsString())
	})

	t.Run("a response body lands beside the request it answers", func(t *testing.T) {
		t.Parallel()
		record := inboundTestLog(claudeCodeScopeName, "claude-code", "api_response_body", append(who, logStringAttribute("request_id", "req_011"))...)
		require.Equal(t, "req_011", identity(t, in, record)[EventIDColumnKey].AsString())
	})

	t.Run("a request body and a compaction get no event id, so the writer keeps the record id", func(t *testing.T) {
		t.Parallel()
		reader, meterProvider := readableMeter(t)
		counted := NewInstruments(testenv.NewLogger(t), meterProvider)

		body := inboundTestLog(claudeCodeScopeName, "claude-code", "api_request_body", append(who, logStringAttribute("request_id", "req_011"))...)
		attrs := identity(t, counted, body)
		require.NotContains(t, attrs, EventIDColumnKey, "a request id is present, but the type has no subject of its own")
		require.Equal(t, "session-1", attrs[SessionIDColumnKey].AsString(), "the session attributes still apply")

		compaction := inboundTestLog(claudeCodeScopeName, "claude-code", "compaction", who...)
		require.NotContains(t, identity(t, counted, compaction), EventIDColumnKey)

		require.Zero(t, counterValue(t, reader, meterColumnEnricherMissing, attr.AgentEventColumn("event_id")), "not expected is never, not missing")
	})

	t.Run("an unclassified record gets none of them", func(t *testing.T) {
		t.Parallel()
		record := inboundTestLog(claudeCodeScopeName, "claude-code", "hook_registered", who...)
		require.Empty(t, identity(t, in, record))
	})
}

func TestIdentityCountsWhatAProviderNeverStates(t *testing.T) {
	t.Parallel()

	reader, meterProvider := readableMeter(t)
	in := NewInstruments(testenv.NewLogger(t), meterProvider)

	// Codex states a conversation, a user and a response id, but no turn
	// and no organization, so those two are counted on its api_request.
	record := inboundTestLog(codexScopeName, "codex", "codex.sse_event",
		logStringAttribute("event.kind", "response.completed"),
		logStringAttribute("conversation.id", "conv-1"),
		logStringAttribute("user.email", "dev@example.com"),
		logStringAttribute("user.account_id", "acct-1"),
		logStringAttribute("response.id", "resp-1"),
	)

	attrs := identity(t, in, record)
	require.Equal(t, "conv-1", attrs[SessionIDColumnKey].AsString())
	require.Equal(t, "resp-1", attrs[EventIDColumnKey].AsString())
	require.Equal(t, "dev@example.com", attrs[UserEmailColumnKey].AsString())
	require.Equal(t, "acct-1", attrs[ExternalUserIDColumnKey].AsString())
	require.NotContains(t, attrs, TurnIDColumnKey)
	require.NotContains(t, attrs, ExternalOrgIDColumnKey)
	require.Equal(t, int64(1), counterValue(t, reader, meterColumnEnricherMissing, attr.AgentEventColumn("turn_id")))
	require.Equal(t, int64(1), counterValue(t, reader, meterColumnEnricherMissing, attr.AgentEventColumn("external_org_id")))
	require.Zero(t, counterValue(t, reader, meterColumnEnricherMissing, attr.AgentEventColumn("session_id")))
}

func TestIdentityForSemconv(t *testing.T) {
	t.Parallel()

	in := NewInstruments(testenv.NewLogger(t), testenv.NewMeterProvider(t))

	t.Run("a chat record names its conversation and response", func(t *testing.T) {
		t.Parallel()
		record := inboundTestLog("litellm", "litellm", "gen_ai.client.inference.operation.details",
			logStringAttribute("gen_ai.operation.name", "chat"),
			logStringAttribute("gen_ai.conversation.id", "session-9"),
			logStringAttribute("gen_ai.response.id", "resp-1"),
			logStringAttribute("user.email", "dev@example.com"),
		)

		attrs := identity(t, in, record)
		require.Equal(t, "session-9", attrs[SessionIDColumnKey].AsString())
		require.Equal(t, "resp-1", attrs[EventIDColumnKey].AsString())
		require.Equal(t, "dev@example.com", attrs[UserEmailColumnKey].AsString())
		require.NotContains(t, attrs, TurnIDColumnKey)
	})

	t.Run("a tool call names the call", func(t *testing.T) {
		t.Parallel()
		record := inboundTestLog("my-agent", "my-agent", "gen_ai.client.inference.operation.details",
			logStringAttribute("gen_ai.operation.name", "execute_tool"),
			logStringAttribute("gen_ai.tool.call.id", "call-1"),
		)
		require.Equal(t, "call-1", identity(t, in, record)[EventIDColumnKey].AsString())
	})
}

func TestSpanIdentityNamesTheConversationAndCountsItsAbsence(t *testing.T) {
	t.Parallel()

	reader, meterProvider := readableMeter(t)
	in := NewInstruments(testenv.NewLogger(t), meterProvider)
	enricher := &spanIdentity{instruments: in}
	require.Equal(t, "enrich-identity", enricher.Name())

	named := inboundTestSpan("litellm", "litellm", "chat gpt-4o", otelv1.InboundSpan_STATUS_CODE_OK,
		spanStringAttribute("gen_ai.operation.name", "chat"),
		spanStringAttribute("gen_ai.response.id", "resp-1"),
		spanStringAttribute("gen_ai.conversation.id", "session-9"),
	)
	attrs := enrichedSpan(t, enricher, named)
	require.Equal(t, "resp-1", attrs[EventIDColumnKey].AsString())
	require.Equal(t, "session-9", attrs[SessionIDColumnKey].AsString())

	unnamed := inboundTestSpan("litellm", "litellm", "chat gpt-4o", otelv1.InboundSpan_STATUS_CODE_OK,
		spanStringAttribute("gen_ai.operation.name", "chat"),
	)
	require.NotContains(t, enrichedSpan(t, enricher, unnamed), SessionIDColumnKey)
	require.Equal(t, int64(1), counterValue(t, reader, meterColumnEnricherMissing, attr.AgentEventSurface(missingLabelOther), attr.AgentEventColumn("session_id")))
}
