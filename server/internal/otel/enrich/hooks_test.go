package enrich

import (
	"testing"

	otelv1 "github.com/speakeasy-api/gram/infra/gen/gram/otel/v1"
	"github.com/speakeasy-api/gram/server/internal/otel/dialect"
	"github.com/stretchr/testify/require"
	"go.opentelemetry.io/otel/attribute"
)

// hooksRecord is a row the hooks ingest endpoint republished: named by the
// provider-style hook event, under the hooks scope, with the gram.hook.*
// attributes the row carries.
func hooksRecord(eventName string, attributes ...*otelv1.InboundLogRecord_KeyValue) *otelv1.InboundLogRecord {
	return inboundTestLog(dialect.HooksLogScope, "codex", eventName, attributes...)
}

// requireColumn asserts a column's value, or that it is absent when want is empty.
func requireColumn(t *testing.T, attrs map[attribute.Key]attribute.Value, key attribute.Key, want string) {
	t.Helper()
	if want == "" {
		require.NotContains(t, attrs, key)
		return
	}
	require.Equal(t, want, attrs[key].AsString(), "column %s", key)
}

func TestHooksRecordEventColumns(t *testing.T) {
	t.Parallel()

	toolCall := []*otelv1.InboundLogRecord_KeyValue{
		logStringAttribute("gram.hook.source", "codex"),
		logStringAttribute("gen_ai.tool.call.id", "call-1"),
		logStringAttribute("gram.tool.name", "shell"),
		inboundTestDoubleAttribute("gram.tool_call.duration", 0.75),
	}
	with := func(extra ...*otelv1.InboundLogRecord_KeyValue) []*otelv1.InboundLogRecord_KeyValue {
		return append(append([]*otelv1.InboundLogRecord_KeyValue{}, toolCall...), extra...)
	}

	cases := []struct {
		name      string
		record    *otelv1.InboundLogRecord
		eventType string
		eventID   string
		outcome   string
		message   string
		text      string
	}{
		{
			name:      "PreToolUse is a tool_call on the tool call id with no outcome yet",
			record:    hooksRecord("PreToolUse", toolCall...),
			eventType: dialect.EventTypeToolCall, eventID: "call-1",
		},
		{
			name:      "BeforeMCPExecution is a tool_call too",
			record:    hooksRecord("BeforeMCPExecution", toolCall...),
			eventType: dialect.EventTypeToolCall, eventID: "call-1",
		},
		{
			name:      "PostToolUse is a tool_call_result that went fine",
			record:    hooksRecord("PostToolUse", toolCall...),
			eventType: dialect.EventTypeToolCallResult, eventID: "call-1", outcome: dialect.OutcomeOK,
		},
		{
			name:      "PostToolUse carrying an error failed, as Cursor reports a failed tool",
			record:    hooksRecord("PostToolUse", with(logStringAttribute("gram.hook.error", "exit 1"))...),
			eventType: dialect.EventTypeToolCallResult, eventID: "call-1", outcome: dialect.OutcomeError, message: "exit 1",
		},
		{
			name:      "PostToolUseFailure is a tool_call_result that failed, with the error as its message",
			record:    hooksRecord("PostToolUseFailure", with(logStringAttribute("gram.hook.error", `{"message":"exit 1"}`))...),
			eventType: dialect.EventTypeToolCallResult, eventID: "call-1", outcome: dialect.OutcomeError, message: `{"message":"exit 1"}`,
		},
		{
			name:      "AfterMCPExecution is ok unless it reported an error",
			record:    hooksRecord("AfterMCPExecution", toolCall...),
			eventType: dialect.EventTypeToolCallResult, eventID: "call-1", outcome: dialect.OutcomeOK,
		},
		{
			name:      "AfterMCPExecution with an error failed",
			record:    hooksRecord("AfterMCPExecution", with(logStringAttribute("gram.hook.error", "timeout"))...),
			eventType: dialect.EventTypeToolCallResult, eventID: "call-1", outcome: dialect.OutcomeError, message: "timeout",
		},
		{
			name:      "UserPromptSubmit is a prompt whose words went to chat, not here",
			record:    hooksRecord("UserPromptSubmit", logStringAttribute("gram.hook.source", "claude-code"), logStringAttribute("gram.hook.decision", "allow")),
			eventType: dialect.EventTypePrompt,
		},
		{
			name:      "BeforeSubmitPrompt is a prompt too",
			record:    hooksRecord("BeforeSubmitPrompt", logStringAttribute("gram.hook.source", "cursor")),
			eventType: dialect.EventTypePrompt,
		},
		{
			name:      "PermissionRequest Speakeasy allowed is a tool_decision with the verdict as text and no outcome",
			record:    hooksRecord("PermissionRequest", with(logStringAttribute("gram.hook.decision", "allow"))...),
			eventType: dialect.EventTypeToolDecision, eventID: "call-1", text: "allow",
		},
		{
			name:      "PermissionRequest Speakeasy denied was rejected",
			record:    hooksRecord("PermissionRequest", with(logStringAttribute("gram.hook.decision", "deny"), logStringAttribute("gram.hook.block_reason", "shadow MCP"))...),
			eventType: dialect.EventTypeToolDecision, eventID: "call-1", outcome: dialect.OutcomeRejected, text: "deny",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			attrs := agentAttributes(t, tc.record)
			requireColumn(t, attrs, AgentEventTypeKey, tc.eventType)
			requireColumn(t, attrs, AgentEventIDKey, tc.eventID)
			requireColumn(t, attrs, AgentOutcomeKey, tc.outcome)
			requireColumn(t, attrs, AgentOutcomeMessageKey, tc.message)
			requireColumn(t, attrs, AgentTextKey, tc.text)
		})
	}
}

func TestHooksRecordLeavesLifecycleAndDerivedRowsUnclassified(t *testing.T) {
	t.Parallel()

	for _, name := range []string{
		"SessionStart", "SessionEnd", "Stop", "SubagentStop", "Notification", "ConfigChange",
		"AfterAgentResponse", "AfterAgentThought", "session.updated", "usage.reported", "mcp.inventory", "skill.activated",
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			attrs := agentAttributes(t, hooksRecord(name, logStringAttribute("gram.hook.source", "claude-code"), logStringAttribute("gen_ai.tool.call.id", "call-1")))
			require.NotContains(t, attrs, AgentEventTypeKey)
			require.Equal(t, name, attrs[AgentRawEventNameKey].AsString(), "the raw name is kept for the catalog")
			require.NotContains(t, attrs, AgentEventIDKey, "only the tool hooks name a subject")
		})
	}
}

func TestHooksRecordWhoAndWhere(t *testing.T) {
	t.Parallel()

	attrs := agentAttributes(t, hooksRecord("PostToolUse",
		logStringAttribute("gram.hook.source", "codex"),
		logStringAttribute("gram.session.id", "codex-session-1"),
		logStringAttribute("gen_ai.conversation.id", "chat-uuid-1"),
		logStringAttribute("gram.hook.turn_id", "turn-1"),
		logStringAttribute("user.email", "dev@example.com"),
		logStringAttribute("user.id", "user-1"),
		logStringAttribute("gram.external_user.id", "acct-1"),
		logStringAttribute("gram.external_org_id", "ext-org-1"),
	))
	require.Equal(t, "codex-session-1", attrs[AgentSessionIDKey].AsString(), "the agent's own session as the tee stamps it, not the chat it maps to")
	require.Equal(t, "turn-1", attrs[AgentTurnIDKey].AsString())
	require.Equal(t, "dev@example.com", attrs[AgentUserEmailKey].AsString())
	require.Equal(t, "acct-1", attrs[AgentExternalUserIDKey].AsString(), "the AI account the endpoint attributed the session to, not the Speakeasy user")
	require.Equal(t, "ext-org-1", attrs[AgentExternalOrgIDKey].AsString())
	require.Equal(t, "codex", attrs[AgentSurfaceKey].AsString())

	legacy := identity(t, hooksRecord("PostToolUse", logStringAttribute("session.id", "codex-session-2")))
	require.Equal(t, "codex-session-2", legacy[AgentSessionIDKey].AsString(), "a row carrying the raw key is still read")

	bare := identity(t, hooksRecord("PostToolUse", logStringAttribute("user.id", "user-1")))
	require.NotContains(t, bare, AgentExternalUserIDKey, "the Speakeasy user is never read as an external account")
}

func TestHooksRecordProvider(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name     string
		attrs    []*otelv1.InboundLogRecord_KeyValue
		provider string
	}{
		{name: "the attributed provider wins", attrs: []*otelv1.InboundLogRecord_KeyValue{logStringAttribute("gram.provider", "anthropic"), logStringAttribute("gram.hook.source", "codex")}, provider: "anthropic"},
		{name: "claude-code is anthropic", attrs: []*otelv1.InboundLogRecord_KeyValue{logStringAttribute("gram.hook.source", "claude-code")}, provider: "anthropic"},
		{name: "claude-code-desktop is anthropic", attrs: []*otelv1.InboundLogRecord_KeyValue{logStringAttribute("gram.hook.source", "claude-code-desktop")}, provider: "anthropic"},
		{name: "cowork is anthropic", attrs: []*otelv1.InboundLogRecord_KeyValue{logStringAttribute("gram.hook.source", "cowork")}, provider: "anthropic"},
		{name: "claude-tag is anthropic", attrs: []*otelv1.InboundLogRecord_KeyValue{logStringAttribute("gram.hook.source", "claude-tag")}, provider: "anthropic"},
		{name: "codex is openai", attrs: []*otelv1.InboundLogRecord_KeyValue{logStringAttribute("gram.hook.source", "codex")}, provider: "openai"},
		{name: "cursor states no provider", attrs: []*otelv1.InboundLogRecord_KeyValue{logStringAttribute("gram.hook.source", "cursor")}, provider: ""},
		{name: "opencode states no provider", attrs: []*otelv1.InboundLogRecord_KeyValue{logStringAttribute("gram.hook.source", "opencode")}, provider: ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			requireColumn(t, enriched(t, &logClassification{}, hooksRecord("PostToolUse", tc.attrs...)), AgentProviderKey, tc.provider)
		})
	}
}

func TestHooksRecordToolAndMCPNames(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name      string
		record    *otelv1.InboundLogRecord
		tool      string
		mcpServer string
		mcpTool   string
	}{
		{
			name:   "a built-in tool has no MCP half",
			record: hooksRecord("PostToolUse", logStringAttribute("gram.tool.name", "Bash")),
			tool:   "Bash",
		},
		{
			name:   "a Claude-style MCP tool name carries the function",
			record: hooksRecord("PostToolUse", logStringAttribute("gram.tool.name", "mcp__github__get_pr"), logStringAttribute("gram.tool_call.source", "github")),
			tool:   "mcp__github__get_pr", mcpServer: "github", mcpTool: "get_pr",
		},
		{
			name:   "a Cursor MCP call names the tool as the MCP tool once a server is stated",
			record: hooksRecord("AfterMCPExecution", logStringAttribute("gram.tool.name", "get_pr"), logStringAttribute("gram.tool_call.source", "github")),
			tool:   "get_pr", mcpServer: "github", mcpTool: "get_pr",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			attrs := operation(t, tc.record)
			require.Equal(t, tc.tool, attrs[AgentToolNameKey].AsString())
			require.Equal(t, tc.tool, attrs[AgentNameKey].AsString())
			requireColumn(t, attrs, AgentMCPServerNameKey, tc.mcpServer)
			requireColumn(t, attrs, AgentMCPToolNameKey, tc.mcpTool)
		})
	}
}

func TestHooksRecordDuration(t *testing.T) {
	t.Parallel()

	record := hooksRecord("PostToolUse",
		inboundTestDoubleAttribute("gram.tool_call.duration", 0.75),
		inboundTestIntAttribute("gen_ai.usage.input_tokens", 120),
		inboundTestDoubleAttribute("gen_ai.usage.cost", 0.0123),
	)
	require.Equal(t, int64(750_000_000), operation(t, record)[AgentDurationNanoKey].AsInt64())
	require.Empty(t, usage(t, record), "only an api_request carries usage")

	require.NotContains(t, operation(t, hooksRecord("PostToolUse")), AgentDurationNanoKey)
}

func TestHooksColumnsApplyOnlyToTheHooksScope(t *testing.T) {
	t.Parallel()

	attrs := agentAttributes(t, inboundTestLog("some.other.scope", "codex", "PostToolUse",
		logStringAttribute("gram.hook.source", "codex"),
		logStringAttribute("gram.tool.name", "shell"),
	))
	require.NotContains(t, attrs, AgentEventTypeKey, "the hook vocabulary alone does not make a record the hooks endpoint's")
	require.NotContains(t, attrs, AgentSurfaceKey)
}
