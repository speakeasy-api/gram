package dialect

import (
	"testing"

	otelv1 "github.com/speakeasy-api/gram/infra/gen/gram/otel/v1"
	"github.com/stretchr/testify/require"
)

// hookRecord is a row the hooks ingest endpoint republished: named by the
// provider-style hook event, under the hooks scope, with the gram.hook.*
// attributes the row carries.
func hookRecord(eventName string, attributes ...*otelv1.InboundLogRecord_KeyValue) *otelv1.InboundLogRecord {
	return accessorTestRecord(HooksLogScope, eventName, attributes...)
}

// requireStated asserts a dialect's answer, or that it stated none when want is empty.
func requireStated(t *testing.T, want string, key string, got string, err error) {
	t.Helper()
	require.NoError(t, err)
	if want == "" {
		require.Empty(t, key, "stated %q", got)
		return
	}
	require.NotEmpty(t, key)
	require.Equal(t, want, got)
}

func TestHookLogIsSelectedOnItsOwn(t *testing.T) {
	t.Parallel()

	require.IsType(t, HookLog{}, ForLog(hookRecord("PostToolUse")))
}

func TestHookLogEventAnswers(t *testing.T) {
	t.Parallel()

	toolCall := []*otelv1.InboundLogRecord_KeyValue{
		accessorTestKV("gram.hook.source", "codex"),
		accessorTestKV("gen_ai.tool.call.id", "call-1"),
		accessorTestKV("gram.tool.name", "shell"),
		accessorTestDoubleKV("gram.tool_call.duration", 0.75),
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
			record:    hookRecord("PreToolUse", toolCall...),
			eventType: EventTypeToolCall, eventID: "call-1",
		},
		{
			name:      "BeforeMCPExecution is a tool_call too",
			record:    hookRecord("BeforeMCPExecution", toolCall...),
			eventType: EventTypeToolCall, eventID: "call-1",
		},
		{
			name:      "PostToolUse is a tool_call_result that went fine",
			record:    hookRecord("PostToolUse", toolCall...),
			eventType: EventTypeToolCallResult, eventID: "call-1", outcome: OutcomeOK,
		},
		{
			name:      "PostToolUse carrying an error failed, as Cursor reports a failed tool",
			record:    hookRecord("PostToolUse", with(accessorTestKV("gram.hook.error", "exit 1"))...),
			eventType: EventTypeToolCallResult, eventID: "call-1", outcome: OutcomeError, message: "exit 1",
		},
		{
			name:      "PostToolUseFailure is a tool_call_result that failed, with the error as its message",
			record:    hookRecord("PostToolUseFailure", with(accessorTestKV("gram.hook.error", `{"message":"exit 1"}`))...),
			eventType: EventTypeToolCallResult, eventID: "call-1", outcome: OutcomeError, message: `{"message":"exit 1"}`,
		},
		{
			name:      "AfterMCPExecution is ok unless it reported an error",
			record:    hookRecord("AfterMCPExecution", toolCall...),
			eventType: EventTypeToolCallResult, eventID: "call-1", outcome: OutcomeOK,
		},
		{
			name:      "AfterMCPExecution with an error failed",
			record:    hookRecord("AfterMCPExecution", with(accessorTestKV("gram.hook.error", "timeout"))...),
			eventType: EventTypeToolCallResult, eventID: "call-1", outcome: OutcomeError, message: "timeout",
		},
		{
			name:      "UserPromptSubmit is a prompt whose words went to chat, not here",
			record:    hookRecord("UserPromptSubmit", accessorTestKV("gram.hook.source", "claude-code"), accessorTestKV("gram.hook.decision", "allow")),
			eventType: EventTypePrompt,
		},
		{
			name:      "BeforeSubmitPrompt is a prompt too",
			record:    hookRecord("BeforeSubmitPrompt", accessorTestKV("gram.hook.source", "cursor")),
			eventType: EventTypePrompt,
		},
		{
			name:      "PermissionRequest Speakeasy allowed is a tool_decision with the verdict as text and no outcome",
			record:    hookRecord("PermissionRequest", with(accessorTestKV("gram.hook.decision", "allow"))...),
			eventType: EventTypeToolDecision, eventID: "call-1", text: "allow",
		},
		{
			name:      "PermissionRequest Speakeasy denied was rejected",
			record:    hookRecord("PermissionRequest", with(accessorTestKV("gram.hook.decision", "deny"), accessorTestKV("gram.hook.block_reason", "shadow MCP"))...),
			eventType: EventTypeToolDecision, eventID: "call-1", outcome: OutcomeRejected, text: "deny",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			d := ForLog(tc.record)
			key, v, err := d.EventType(tc.record)
			requireStated(t, tc.eventType, key, v, err)
			key, v, err = d.SubjectID(tc.record)
			requireStated(t, tc.eventID, key, v, err)
			key, v, err = d.Outcome(tc.record)
			requireStated(t, tc.outcome, key, v, err)
			key, v, err = d.OutcomeMessage(tc.record)
			requireStated(t, tc.message, key, v, err)
			key, v, err = d.Text(tc.record)
			requireStated(t, tc.text, key, v, err)
		})
	}
}

func TestHookLogLeavesLifecycleAndDerivedRowsUnclassified(t *testing.T) {
	t.Parallel()

	for _, name := range []string{
		"SessionStart", "SessionEnd", "Stop", "SubagentStop", "Notification", "ConfigChange",
		"AfterAgentResponse", "AfterAgentThought", "session.updated", "usage.reported", "mcp.inventory", "skill.activated",
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			record := hookRecord(name, accessorTestKV("gram.hook.source", "claude-code"), accessorTestKV("gen_ai.tool.call.id", "call-1"))
			d := ForLog(record)
			key, v, err := d.EventType(record)
			requireStated(t, "", key, v, err)
			key, v, err = d.EventName(record)
			requireStated(t, name, key, v, err)
			// Only the tool hooks name a subject.
			key, v, err = d.SubjectID(record)
			requireStated(t, "", key, v, err)
		})
	}
}

func TestHookLogWhoAndWhere(t *testing.T) {
	t.Parallel()

	record := hookRecord("PostToolUse",
		accessorTestKV("gram.hook.source", "codex"),
		accessorTestKV("gram.session.id", "codex-session-1"),
		accessorTestKV("gen_ai.conversation.id", "chat-uuid-1"),
		accessorTestKV("gram.hook.turn_id", "turn-1"),
		accessorTestKV("user.email", "dev@example.com"),
		accessorTestKV("user.id", "user-1"),
		accessorTestKV("gram.external_user.id", "acct-1"),
		accessorTestKV("gram.external_org_id", "ext-org-1"),
	)
	d := ForLog(record)
	// The agent's own session as the tee stamps it, not the chat it maps to.
	key, v, err := d.SessionID(record)
	requireStated(t, "codex-session-1", key, v, err)
	key, v, err = d.TurnID(record)
	requireStated(t, "turn-1", key, v, err)
	key, v, err = d.ExternalUserEmail(record)
	requireStated(t, "dev@example.com", key, v, err)
	// The AI account the endpoint attributed the session to, not the Speakeasy user.
	key, v, err = d.ExternalUserID(record)
	requireStated(t, "acct-1", key, v, err)
	key, v, err = d.ExternalOrgID(record)
	requireStated(t, "ext-org-1", key, v, err)
	key, v, err = d.Surface(record)
	requireStated(t, "codex", key, v, err)

	legacy := hookRecord("PostToolUse", accessorTestKV("session.id", "codex-session-2"))
	key, v, err = ForLog(legacy).SessionID(legacy)
	requireStated(t, "codex-session-2", key, v, err)

	bare := hookRecord("PostToolUse", accessorTestKV("user.id", "user-1"), accessorTestKV("gen_ai.conversation.id", "chat-uuid-1"))
	key, v, err = ForLog(bare).ExternalUserID(bare)
	requireStated(t, "", key, v, err)
	key, v, err = ForLog(bare).SessionID(bare)
	requireStated(t, "", key, v, err)
}

func TestHookLogProvider(t *testing.T) {
	t.Parallel()

	cases := []struct {
		source   string
		provider string
	}{
		{source: "claude-code", provider: "anthropic"},
		{source: "claude-code-desktop", provider: "anthropic"},
		{source: "cowork", provider: "anthropic"},
		{source: "claude-tag", provider: "anthropic"},
		{source: "codex", provider: "openai"},
		{source: "cursor", provider: ""},
		{source: "opencode", provider: ""},
	}
	for _, tc := range cases {
		t.Run(tc.source, func(t *testing.T) {
			t.Parallel()
			record := hookRecord("PostToolUse", accessorTestKV("gram.hook.source", tc.source))
			key, v, err := ForLog(record).Provider(record)
			requireStated(t, tc.provider, key, v, err)
		})
	}
}

func TestHookLogToolAndMCPNames(t *testing.T) {
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
			record: hookRecord("PostToolUse", accessorTestKV("gram.tool.name", "Bash")),
			tool:   "Bash",
		},
		{
			name:   "a Claude-style MCP tool name carries the function",
			record: hookRecord("PostToolUse", accessorTestKV("gram.tool.name", "mcp__github__get_pr"), accessorTestKV("gram.tool_call.source", "github")),
			tool:   "mcp__github__get_pr", mcpServer: "github", mcpTool: "get_pr",
		},
		{
			name:   "a Cursor MCP call names the tool as the MCP tool once a server is stated",
			record: hookRecord("AfterMCPExecution", accessorTestKV("gram.tool.name", "get_pr"), accessorTestKV("gram.tool_call.source", "github")),
			tool:   "get_pr", mcpServer: "github", mcpTool: "get_pr",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			d := ForLog(tc.record)
			key, v, err := d.ToolName(tc.record)
			requireStated(t, tc.tool, key, v, err)
			key, v, err = d.MCPServerName(tc.record)
			requireStated(t, tc.mcpServer, key, v, err)
			key, v, err = d.MCPToolName(tc.record)
			requireStated(t, tc.mcpTool, key, v, err)
		})
	}
}

func TestHookLogDuration(t *testing.T) {
	t.Parallel()

	record := hookRecord("PostToolUse", accessorTestDoubleKV("gram.tool_call.duration", 0.75))
	key, nanos, err := ForLog(record).DurationNano(record)
	requireLogAnswer(t, "gram.tool_call.duration", int64(750_000_000), key, nanos, err)

	bare := hookRecord("PostToolUse")
	key, nanos, err = ForLog(bare).DurationNano(bare)
	requireLogAnswer(t, "", int64(0), key, nanos, err)
}

func TestHookLogAppliesOnlyToTheHooksScope(t *testing.T) {
	t.Parallel()

	// The hook vocabulary alone does not make a record the hooks endpoint's.
	record := accessorTestRecord("some.other.scope", "PostToolUse",
		accessorTestKV("gram.hook.source", "codex"),
		accessorTestKV("gram.tool.name", "shell"),
	)
	d := ForLog(record)
	require.IsType(t, SemconvLog{}, d)
	key, v, err := d.EventType(record)
	requireStated(t, "", key, v, err)
	key, v, err = d.Surface(record)
	requireStated(t, "", key, v, err)
}
