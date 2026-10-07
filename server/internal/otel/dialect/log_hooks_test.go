package dialect

import (
	"testing"

	otelv1 "github.com/speakeasy-api/gram/infra/gen/gram/otel/v1"
	"github.com/stretchr/testify/require"
)

// hooksRecord is a row the hooks ingest endpoint republished: named by the
// provider-style hook event, under the hooks scope, with the gram.hook.*
// attributes the row carries.
func hooksRecord(eventName string, attributes ...*otelv1.InboundLogRecord_KeyValue) *otelv1.InboundLogRecord {
	return accessorTestRecord(HooksLogScopeName, eventName, attributes...)
}

func TestHooksLogClaimsOnlyTheHooksScope(t *testing.T) {
	t.Parallel()

	selected := ForLog(hooksRecord("PostToolUse"))
	fallback, ok := selected.(LogFallback)
	require.True(t, ok)
	require.Equal(t, []LogDialect{HooksLog{}, SemconvLog{}}, fallback.Candidates)

	other := accessorTestRecord("some.other.scope", "PostToolUse", accessorTestKV("gram.hook.source", "codex"))
	require.IsType(t, SemconvLog{}, ForLog(other), "the hook vocabulary alone does not make a record the hooks endpoint's")
}

func TestHooksLogEventAccessors(t *testing.T) {
	t.Parallel()

	toolCall := []*otelv1.InboundLogRecord_KeyValue{
		accessorTestKV("gram.hook.source", "codex"),
		accessorTestKV("gen_ai.tool.call.id", "call-1"),
		accessorTestKV("gram.tool.name", "shell"),
		hooksTestDoubleKV("gram.tool_call.duration", 0.75),
	}

	cases := []struct {
		name       string
		record     *otelv1.InboundLogRecord
		eventType  string
		subjectKey string
		subject    string
		outcomeKey string
		outcome    string
		message    string
		text       string
	}{
		{
			name:      "PreToolUse is a tool_call on the tool call id with no outcome yet",
			record:    hooksRecord("PreToolUse", toolCall...),
			eventType: EventTypeToolCall, subjectKey: "gen_ai.tool.call.id", subject: "call-1",
		},
		{
			name:      "BeforeMCPExecution is a tool_call too",
			record:    hooksRecord("BeforeMCPExecution", toolCall...),
			eventType: EventTypeToolCall, subjectKey: "gen_ai.tool.call.id", subject: "call-1",
		},
		{
			name:      "PostToolUse is a tool_call_result that went fine",
			record:    hooksRecord("PostToolUse", toolCall...),
			eventType: EventTypeToolCallResult, subjectKey: "gen_ai.tool.call.id", subject: "call-1",
			outcomeKey: "event_name", outcome: OutcomeOK,
		},
		{
			name:      "PostToolUseFailure is a tool_call_result that failed, with the error as its message",
			record:    hooksRecord("PostToolUseFailure", append(toolCall, accessorTestKV("gram.hook.error", `{"message":"exit 1"}`))...),
			eventType: EventTypeToolCallResult, subjectKey: "gen_ai.tool.call.id", subject: "call-1",
			outcomeKey: "event_name", outcome: OutcomeError, message: `{"message":"exit 1"}`,
		},
		{
			name:      "AfterMCPExecution is ok unless it reported an error",
			record:    hooksRecord("AfterMCPExecution", toolCall...),
			eventType: EventTypeToolCallResult, subjectKey: "gen_ai.tool.call.id", subject: "call-1",
			outcomeKey: "event_name", outcome: OutcomeOK,
		},
		{
			name:      "AfterMCPExecution with an error failed",
			record:    hooksRecord("AfterMCPExecution", append(toolCall, accessorTestKV("gram.hook.error", "timeout"))...),
			eventType: EventTypeToolCallResult, subjectKey: "gen_ai.tool.call.id", subject: "call-1",
			outcomeKey: "gram.hook.error", outcome: OutcomeError, message: "timeout",
		},
		{
			name:      "UserPromptSubmit is a prompt whose words went to chat, not here",
			record:    hooksRecord("UserPromptSubmit", accessorTestKV("gram.hook.source", "claude-code"), accessorTestKV("gram.hook.decision", "allow")),
			eventType: EventTypePrompt,
		},
		{
			name:      "BeforeSubmitPrompt is a prompt too",
			record:    hooksRecord("BeforeSubmitPrompt", accessorTestKV("gram.hook.source", "cursor")),
			eventType: EventTypePrompt,
		},
		{
			name:      "PermissionRequest Gram allowed is a tool_decision with the verdict as text and no outcome",
			record:    hooksRecord("PermissionRequest", append(toolCall, accessorTestKV("gram.hook.decision", "allow"))...),
			eventType: EventTypeToolDecision, subjectKey: "gen_ai.tool.call.id", subject: "call-1", text: "allow",
		},
		{
			name:      "PermissionRequest Gram denied was rejected",
			record:    hooksRecord("PermissionRequest", append(toolCall, accessorTestKV("gram.hook.decision", "deny"), accessorTestKV("gram.hook.block_reason", "shadow MCP"))...),
			eventType: EventTypeToolDecision, subjectKey: "gen_ai.tool.call.id", subject: "call-1",
			outcomeKey: "gram.hook.block_reason", outcome: OutcomeRejected, text: "deny",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			selected := ForLog(tc.record)

			key, value, err := selected.EventType(tc.record)
			require.NoError(t, err)
			require.Equal(t, "event_name", key)
			require.Equal(t, tc.eventType, value)

			key, value, err = selected.SubjectID(tc.record)
			require.NoError(t, err)
			require.Equal(t, tc.subjectKey, key)
			require.Equal(t, tc.subject, value)

			key, value, err = selected.Outcome(tc.record)
			require.NoError(t, err)
			require.Equal(t, tc.outcomeKey, key)
			require.Equal(t, tc.outcome, value)

			_, value, err = selected.OutcomeMessage(tc.record)
			require.NoError(t, err)
			require.Equal(t, tc.message, value)

			_, value, err = selected.Text(tc.record)
			require.NoError(t, err)
			require.Equal(t, tc.text, value)
		})
	}
}

func TestHooksLogLeavesLifecycleAndDerivedRowsUnclassified(t *testing.T) {
	t.Parallel()

	for _, name := range []string{
		"SessionStart", "SessionEnd", "Stop", "SubagentStop", "Notification", "ConfigChange",
		"AfterAgentResponse", "AfterAgentThought", "session.updated", "usage.reported", "mcp.inventory", "skill.activated",
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			record := hooksRecord(name, accessorTestKV("gram.hook.source", "claude-code"), accessorTestKV("gen_ai.tool.call.id", "call-1"))
			selected := ForLog(record)

			key, value, err := selected.EventType(record)
			require.NoError(t, err)
			require.Empty(t, key)
			require.Empty(t, value)

			key, value, err = selected.EventName(record)
			require.NoError(t, err)
			require.Equal(t, "event_name", key)
			require.Equal(t, name, value, "the raw name is kept for the catalog")

			key, _, err = selected.SubjectID(record)
			require.NoError(t, err)
			require.Empty(t, key, "only the tool hooks name a subject")
		})
	}
}

func TestHooksLogWhoAndWhere(t *testing.T) {
	t.Parallel()

	record := hooksRecord("PostToolUse",
		accessorTestKV("gram.hook.source", "codex"),
		accessorTestKV("gram.session.id", "codex-session-1"),
		accessorTestKV("gen_ai.conversation.id", "chat-uuid-1"),
		accessorTestKV("gram.hook.turn_id", "turn-1"),
		accessorTestKV("user.email", "dev@example.com"),
		accessorTestKV("user.id", "user-1"),
		accessorTestKV("gram.external_user.id", "acct-1"),
		accessorTestKV("gram.external_org_id", "ext-org-1"),
		accessorTestKV("gen_ai.response.model", "gpt-5"),
	)
	selected := ForLog(record)

	key, value, err := selected.SessionID(record)
	require.NoError(t, err)
	require.Equal(t, "gram.session.id", key, "the agent's own session as the tee stamps it, not the chat it maps to")
	require.Equal(t, "codex-session-1", value)

	legacy := hooksRecord("PostToolUse", accessorTestKV("session.id", "codex-session-2"))
	key, value, err = ForLog(legacy).SessionID(legacy)
	require.NoError(t, err)
	require.Equal(t, "session.id", key, "a row carrying the raw key is still read")
	require.Equal(t, "codex-session-2", value)

	key, value, err = selected.TurnID(record)
	require.NoError(t, err)
	require.Equal(t, "gram.hook.turn_id", key)
	require.Equal(t, "turn-1", value)

	key, value, err = selected.ExternalUserEmail(record)
	require.NoError(t, err)
	require.Equal(t, "user.email", key)
	require.Equal(t, "dev@example.com", value)

	key, value, err = selected.ExternalUserID(record)
	require.NoError(t, err)
	require.Equal(t, "gram.external_user.id", key, "the AI account the endpoint attributed the session to, not the Gram user")
	require.Equal(t, "acct-1", value)

	bare := hooksRecord("PostToolUse", accessorTestKV("user.id", "user-1"))
	key, value, err = HooksLog{}.ExternalUserID(bare)
	require.NoError(t, err)
	require.Empty(t, key, "the dialect itself never reads the Gram user as an external account")
	require.Empty(t, value)

	key, value, err = selected.ExternalOrgID(record)
	require.NoError(t, err)
	require.Equal(t, "gram.external_org_id", key)
	require.Equal(t, "ext-org-1", value)

	key, value, err = selected.Model(record)
	require.NoError(t, err)
	require.Equal(t, "gen_ai.response.model", key)
	require.Equal(t, "gpt-5", value)

	key, value, err = selected.Surface(record)
	require.NoError(t, err)
	require.Equal(t, "gram.hook.source", key)
	require.Equal(t, "codex", value)
}

func TestHooksLogProvider(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name     string
		attrs    []*otelv1.InboundLogRecord_KeyValue
		key      string
		provider string
	}{
		{name: "the attributed provider wins", attrs: []*otelv1.InboundLogRecord_KeyValue{accessorTestKV("gram.provider", "anthropic"), accessorTestKV("gram.hook.source", "codex")}, key: "gram.provider", provider: "anthropic"},
		{name: "claude-code is anthropic", attrs: []*otelv1.InboundLogRecord_KeyValue{accessorTestKV("gram.hook.source", "claude-code")}, key: "gram.hook.source", provider: "anthropic"},
		{name: "claude-code-desktop is anthropic", attrs: []*otelv1.InboundLogRecord_KeyValue{accessorTestKV("gram.hook.source", "claude-code-desktop")}, key: "gram.hook.source", provider: "anthropic"},
		{name: "cowork is anthropic", attrs: []*otelv1.InboundLogRecord_KeyValue{accessorTestKV("gram.hook.source", "cowork")}, key: "gram.hook.source", provider: "anthropic"},
		{name: "codex is openai", attrs: []*otelv1.InboundLogRecord_KeyValue{accessorTestKV("gram.hook.source", "codex")}, key: "gram.hook.source", provider: "openai"},
		{name: "cursor states no provider", attrs: []*otelv1.InboundLogRecord_KeyValue{accessorTestKV("gram.hook.source", "cursor")}, key: "", provider: ""},
		{name: "opencode states no provider", attrs: []*otelv1.InboundLogRecord_KeyValue{accessorTestKV("gram.hook.source", "opencode")}, key: "", provider: ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			record := hooksRecord("PostToolUse", tc.attrs...)
			key, provider, err := ForLog(record).Provider(record)
			require.NoError(t, err)
			require.Equal(t, tc.key, key)
			require.Equal(t, tc.provider, provider)
		})
	}
}

func TestHooksLogToolAndMCPNames(t *testing.T) {
	t.Parallel()

	t.Run("a built-in tool has no MCP half", func(t *testing.T) {
		t.Parallel()
		record := hooksRecord("PostToolUse", accessorTestKV("gram.tool.name", "Bash"))
		selected := ForLog(record)

		key, value, err := selected.ToolName(record)
		require.NoError(t, err)
		require.Equal(t, "gram.tool.name", key)
		require.Equal(t, "Bash", value)

		key, _, err = selected.MCPServerName(record)
		require.NoError(t, err)
		require.Empty(t, key)

		key, _, err = selected.MCPToolName(record)
		require.NoError(t, err)
		require.Empty(t, key)
	})

	t.Run("a Claude-style MCP tool name carries the function", func(t *testing.T) {
		t.Parallel()
		record := hooksRecord("PostToolUse",
			accessorTestKV("gram.tool.name", "mcp__github__get_pr"),
			accessorTestKV("gram.tool_call.source", "github"),
		)
		selected := ForLog(record)

		key, value, err := selected.MCPServerName(record)
		require.NoError(t, err)
		require.Equal(t, "gram.tool_call.source", key)
		require.Equal(t, "github", value)

		key, value, err = selected.MCPToolName(record)
		require.NoError(t, err)
		require.Equal(t, "gram.tool.name", key)
		require.Equal(t, "get_pr", value)
	})

	t.Run("a Cursor MCP call names the tool as the MCP tool once a server is stated", func(t *testing.T) {
		t.Parallel()
		record := hooksRecord("AfterMCPExecution",
			accessorTestKV("gram.tool.name", "get_pr"),
			accessorTestKV("gram.tool_call.source", "github"),
		)
		key, value, err := ForLog(record).MCPToolName(record)
		require.NoError(t, err)
		require.Equal(t, "gram.tool.name", key)
		require.Equal(t, "get_pr", value)
	})
}

func TestHooksLogDurationAndUsage(t *testing.T) {
	t.Parallel()

	record := hooksRecord("PostToolUse",
		hooksTestDoubleKV("gram.tool_call.duration", 0.75),
		accessorTestIntKV("gen_ai.usage.input_tokens", 120),
		accessorTestIntKV("gen_ai.usage.output_tokens", 30),
		accessorTestIntKV("gen_ai.usage.cache_read.input_tokens", 10),
		accessorTestIntKV("gen_ai.usage.cache_creation.input_tokens", 5),
		hooksTestDoubleKV("gen_ai.usage.cost", 0.0123),
	)
	selected := ForLog(record)

	key, nanos, err := selected.DurationNano(record)
	require.NoError(t, err)
	require.Equal(t, "gram.tool_call.duration", key)
	require.Equal(t, int64(750_000_000), nanos)

	_, tokens, err := selected.InputTokens(record)
	require.NoError(t, err)
	require.Equal(t, int64(120), tokens)
	_, tokens, err = selected.OutputTokens(record)
	require.NoError(t, err)
	require.Equal(t, int64(30), tokens)
	_, tokens, err = selected.CacheReadTokens(record)
	require.NoError(t, err)
	require.Equal(t, int64(10), tokens)
	_, tokens, err = selected.CacheWriteTokens(record)
	require.NoError(t, err)
	require.Equal(t, int64(5), tokens)

	legacy := hooksRecord("PostToolUse",
		accessorTestIntKV("gen_ai.usage.cache_read_input_tokens", 7),
		accessorTestIntKV("gen_ai.usage.cache_creation_input_tokens", 3),
	)
	_, tokens, err = ForLog(legacy).CacheReadTokens(legacy)
	require.NoError(t, err)
	require.Equal(t, int64(7), tokens, "the underscore spelling is still read")
	_, tokens, err = ForLog(legacy).CacheWriteTokens(legacy)
	require.NoError(t, err)
	require.Equal(t, int64(3), tokens)
	_, cost, err := selected.CostUSD(record)
	require.NoError(t, err)
	require.InDelta(t, 0.0123, cost, 1e-9)

	bare := hooksRecord("PostToolUse")
	key, nanos, err = selected.DurationNano(bare)
	require.NoError(t, err)
	require.Empty(t, key)
	require.Zero(t, nanos)
}

func hooksTestDoubleKV(key string, value float64) *otelv1.InboundLogRecord_KeyValue {
	return (&otelv1.InboundLogRecord_KeyValue_builder{
		Key:   &key,
		Value: (&otelv1.InboundLogRecord_AnyValue_builder{DoubleValue: &value}).Build(),
	}).Build()
}
