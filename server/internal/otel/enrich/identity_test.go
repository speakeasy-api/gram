package enrich

import (
	"testing"

	otelv1 "github.com/speakeasy-api/gram/infra/gen/gram/otel/v1"
	"github.com/stretchr/testify/require"
)

func TestIdentityForClaudeCode(t *testing.T) {
	t.Parallel()

	require.Equal(t, "enrich-identity", (&logIdentity{}).Name())
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

		attrs := identity(t, record)
		require.Len(t, attrs, 6)
		require.Equal(t, "session-1", attrs[AgentSessionIDKey].AsString())
		require.Equal(t, "turn-1", attrs[AgentTurnIDKey].AsString())
		require.Equal(t, "message-1", attrs[AgentEventIDKey].AsString())
		require.Equal(t, "dev@example.com", attrs[AgentUserEmailKey].AsString())
		require.Equal(t, "acct-1", attrs[AgentExternalUserIDKey].AsString())
		require.Equal(t, "anthropic-org-1", attrs[AgentExternalOrgIDKey].AsString())
	})

	t.Run("an api_request names the request", func(t *testing.T) {
		t.Parallel()
		record := inboundTestLog(claudeCodeScopeName, "claude-code", "api_request", append(who, logStringAttribute("request_id", "req_011"))...)
		require.Equal(t, "req_011", identity(t, record)[AgentEventIDKey].AsString())
	})

	t.Run("a tool_result names the tool invocation", func(t *testing.T) {
		t.Parallel()
		record := inboundTestLog(claudeCodeScopeName, "claude-code", "tool_result", append(who, logStringAttribute("tool_use_id", "toolu_1"))...)
		require.Equal(t, "toolu_1", identity(t, record)[AgentEventIDKey].AsString())
	})

	t.Run("a response body lands beside the request it answers", func(t *testing.T) {
		t.Parallel()
		record := inboundTestLog(claudeCodeScopeName, "claude-code", "api_response_body", append(who, logStringAttribute("request_id", "req_011"))...)
		require.Equal(t, "req_011", identity(t, record)[AgentEventIDKey].AsString())
	})

	t.Run("a request body and a compaction get no event id, so the writer keeps the record id", func(t *testing.T) {
		t.Parallel()

		body := inboundTestLog(claudeCodeScopeName, "claude-code", "api_request_body", append(who, logStringAttribute("request_id", "req_011"))...)
		attrs := identity(t, body)
		require.NotContains(t, attrs, AgentEventIDKey, "a request id is present, but the type has no subject of its own")
		require.Equal(t, "session-1", attrs[AgentSessionIDKey].AsString(), "the session attributes still apply")

		compaction := inboundTestLog(claudeCodeScopeName, "claude-code", "compaction", who...)
		require.NotContains(t, identity(t, compaction), AgentEventIDKey)

	})

	t.Run("an unclassified record gets none of them", func(t *testing.T) {
		t.Parallel()
		record := inboundTestLog(claudeCodeScopeName, "claude-code", "hook_registered", who...)
		require.Empty(t, identity(t, record))
	})
}

func TestIdentityLeavesOutWhatAProviderNeverStates(t *testing.T) {
	t.Parallel()

	// Codex states a conversation, a user and a response id, but no turn
	// and no organization.
	record := inboundTestLog(codexScopeName, "codex", "codex.sse_event",
		logStringAttribute("event.kind", "response.completed"),
		logStringAttribute("conversation.id", "conv-1"),
		logStringAttribute("user.email", "dev@example.com"),
		logStringAttribute("user.account_id", "acct-1"),
		logStringAttribute("response.id", "resp-1"),
	)

	attrs := identity(t, record)
	require.Equal(t, "conv-1", attrs[AgentSessionIDKey].AsString())
	require.Equal(t, "resp-1", attrs[AgentEventIDKey].AsString())
	require.Equal(t, "dev@example.com", attrs[AgentUserEmailKey].AsString())
	require.Equal(t, "acct-1", attrs[AgentExternalUserIDKey].AsString())
	require.NotContains(t, attrs, AgentTurnIDKey)
	require.NotContains(t, attrs, AgentExternalOrgIDKey)
}

func TestIdentityForSemconv(t *testing.T) {
	t.Parallel()

	t.Run("a chat record names its conversation and response", func(t *testing.T) {
		t.Parallel()
		record := inboundTestLog("litellm", "litellm", "gen_ai.client.inference.operation.details",
			logStringAttribute("gen_ai.operation.name", "chat"),
			logStringAttribute("gen_ai.conversation.id", "session-9"),
			logStringAttribute("gen_ai.response.id", "resp-1"),
			logStringAttribute("user.email", "dev@example.com"),
		)

		attrs := identity(t, record)
		require.Equal(t, "session-9", attrs[AgentSessionIDKey].AsString())
		require.Equal(t, "resp-1", attrs[AgentEventIDKey].AsString())
		require.Equal(t, "dev@example.com", attrs[AgentUserEmailKey].AsString())
		require.NotContains(t, attrs, AgentTurnIDKey)
	})

	t.Run("a tool call names the call", func(t *testing.T) {
		t.Parallel()
		record := inboundTestLog("my-agent", "my-agent", "gen_ai.client.inference.operation.details",
			logStringAttribute("gen_ai.operation.name", "execute_tool"),
			logStringAttribute("gen_ai.tool.call.id", "call-1"),
		)
		require.Equal(t, "call-1", identity(t, record)[AgentEventIDKey].AsString())
	})
}

func TestSpanIdentityNamesTheConversation(t *testing.T) {
	t.Parallel()

	enricher := &spanIdentity{}
	require.Equal(t, "enrich-identity", enricher.Name())

	named := inboundTestSpan("litellm", "litellm", "chat gpt-4o", otelv1.InboundSpan_STATUS_CODE_OK,
		spanStringAttribute("gen_ai.operation.name", "chat"),
		spanStringAttribute("gen_ai.response.id", "resp-1"),
		spanStringAttribute("gen_ai.conversation.id", "session-9"),
	)
	attrs := enrichedSpan(t, enricher, named)
	require.Equal(t, "resp-1", attrs[AgentEventIDKey].AsString())
	require.Equal(t, "session-9", attrs[AgentSessionIDKey].AsString())

	unnamed := inboundTestSpan("litellm", "litellm", "chat gpt-4o", otelv1.InboundSpan_STATUS_CODE_OK,
		spanStringAttribute("gen_ai.operation.name", "chat"),
	)
	require.NotContains(t, enrichedSpan(t, enricher, unnamed), AgentSessionIDKey)
}
