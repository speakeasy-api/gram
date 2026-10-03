package dialect

import (
	"testing"

	otelv1 "github.com/speakeasy-api/gram/infra/gen/gram/otel/v1"
	"github.com/stretchr/testify/require"
)

func hookLogRecord(eventName string, attributes ...*otelv1.InboundLogRecord_KeyValue) *otelv1.InboundLogRecord {
	return codexLogRecord(HookLogScopeName, eventName, attributes...)
}

func hookDoubleAttribute(key string, value float64) *otelv1.InboundLogRecord_KeyValue {
	return (&otelv1.InboundLogRecord_KeyValue_builder{
		Key:   &key,
		Value: (&otelv1.InboundLogRecord_AnyValue_builder{DoubleValue: &value}).Build(),
	}).Build()
}

func hookIntAttribute(key string, value int64) *otelv1.InboundLogRecord_KeyValue {
	return (&otelv1.InboundLogRecord_KeyValue_builder{
		Key:   &key,
		Value: (&otelv1.InboundLogRecord_AnyValue_builder{IntValue: &value}).Build(),
	}).Build()
}

func TestForLogReadsHookEventsWithoutTheSemconvFallback(t *testing.T) {
	t.Parallel()

	require.Equal(t, HookLog{}, ForLog(hookLogRecord("PreToolUse")))
}

func TestHookLogClassifiesHookEvents(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name      string
		eventName string
		want      string
	}{
		{name: "claude pre tool use", eventName: "PreToolUse", want: EventTypeToolCall},
		{name: "cursor before mcp execution", eventName: "BeforeMCPExecution", want: EventTypeToolCall},
		{name: "codex permission request", eventName: "PermissionRequest", want: EventTypeToolCall},
		{name: "post tool use", eventName: "PostToolUse", want: EventTypeToolCallResult},
		{name: "cursor after mcp execution", eventName: "AfterMCPExecution", want: EventTypeToolCallResult},
		{name: "post tool use failure", eventName: "PostToolUseFailure", want: EventTypeToolCallResult},
		{name: "prompt submit", eventName: "UserPromptSubmit", want: EventTypePrompt},
		{name: "cursor before submit prompt", eventName: "BeforeSubmitPrompt", want: EventTypePrompt},
		{name: "agent response", eventName: "AfterAgentResponse", want: EventTypeAPIResponse},
		{name: "case folded", eventName: "pretooluse", want: EventTypeToolCall},
		{name: "session start stays unclassified", eventName: "SessionStart", want: EventTypeUnclassified},
		{name: "stop stays unclassified", eventName: "Stop", want: EventTypeUnclassified},
		{name: "skill activation stays unclassified", eventName: "skill.activated", want: EventTypeUnclassified},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			record := hookLogRecord(tc.eventName)
			_, kind, err := HookLog{}.EventType(record)
			require.NoError(t, err)
			require.Equal(t, tc.want, kind)

			key, name, err := HookLog{}.EventName(record)
			require.NoError(t, err)
			require.Equal(t, "event_name", key)
			require.Equal(t, tc.eventName, name)
		})
	}
}

func TestHookLogReadsAToolCallResult(t *testing.T) {
	t.Parallel()

	record := hookLogRecord("PostToolUse",
		logDialectStringAttribute("gen_ai.conversation.id", "3f1a3a7e-6f1b-4d8e-9d52-6c1f0f7b2a10"),
		logDialectStringAttribute("gen_ai.tool.call.id", "toolu_01"),
		logDialectStringAttribute("gram.tool.name", "search_issues"),
		logDialectStringAttribute("gram.tool_call.source", "linear"),
		logDialectStringAttribute("gram.hook.source", "Claude Code"),
		logDialectStringAttribute("gram.provider", "anthropic"),
		logDialectStringAttribute("gram.external_org_id", "external-org"),
		logDialectStringAttribute("user.email", "user@example.invalid"),
		logDialectStringAttribute("gen_ai.response.model", "claude-sonnet-5"),
		hookDoubleAttribute("gram.tool_call.duration", 1.5),
	)
	d := HookLog{}

	_, kind, _ := d.EventType(record)
	require.Equal(t, EventTypeToolCallResult, kind)
	_, session, _ := d.SessionID(record)
	require.Equal(t, "3f1a3a7e-6f1b-4d8e-9d52-6c1f0f7b2a10", session)
	_, subject, _ := d.SubjectID(record)
	require.Equal(t, "toolu_01", subject)
	_, tool, _ := d.ToolName(record)
	require.Equal(t, "search_issues", tool)
	_, server, _ := d.MCPServerName(record)
	require.Equal(t, "linear", server)
	_, mcpTool, _ := d.MCPToolName(record)
	require.Equal(t, "search_issues", mcpTool)
	_, surface, _ := d.Surface(record)
	require.Equal(t, "claude-code", surface)
	_, provider, _ := d.Provider(record)
	require.Equal(t, "anthropic", provider)
	_, externalOrg, _ := d.ExternalOrgID(record)
	require.Equal(t, "external-org", externalOrg)
	_, email, _ := d.ExternalUserEmail(record)
	require.Equal(t, "user@example.invalid", email)
	_, model, _ := d.Model(record)
	require.Equal(t, "claude-sonnet-5", model)
	_, outcome, _ := d.Outcome(record)
	require.Equal(t, OutcomeOK, outcome)
	_, duration, _ := d.DurationNano(record)
	require.Equal(t, int64(1_500_000_000), duration)
}

func TestHookLogTreatsABlockedToolCallAsARejectedDecision(t *testing.T) {
	t.Parallel()

	record := hookLogRecord("PreToolUse",
		logDialectStringAttribute("gen_ai.tool.call.id", "toolu_02"),
		logDialectStringAttribute("gram.tool.name", "Bash"),
		logDialectStringAttribute("gram.hook.block_reason", "matched policy \"no-secrets\""),
	)
	d := HookLog{}

	_, kind, _ := d.EventType(record)
	require.Equal(t, EventTypeToolDecision, kind)
	_, subject, _ := d.SubjectID(record)
	require.Equal(t, "toolu_02", subject)
	_, outcome, _ := d.Outcome(record)
	require.Equal(t, OutcomeRejected, outcome)
	_, message, _ := d.OutcomeMessage(record)
	require.Equal(t, "matched policy \"no-secrets\"", message)
	key, _, _ := d.MCPServerName(record)
	require.Empty(t, key, "a native tool names no MCP server")
}

func TestHookLogRejectsABlockedPrompt(t *testing.T) {
	t.Parallel()

	record := hookLogRecord("UserPromptSubmit",
		logDialectStringAttribute("gram.hook.block_reason", "over budget"),
	)

	_, kind, _ := HookLog{}.EventType(record)
	require.Equal(t, EventTypePrompt, kind)
	_, outcome, _ := HookLog{}.Outcome(record)
	require.Equal(t, OutcomeRejected, outcome)
}

func TestHookLogReportsFailedToolCallsAsErrors(t *testing.T) {
	t.Parallel()

	t.Run("a failure event", func(t *testing.T) {
		t.Parallel()

		record := hookLogRecord("PostToolUseFailure", logDialectStringAttribute("gram.hook.error", "exit status 1"))
		_, outcome, _ := HookLog{}.Outcome(record)
		require.Equal(t, OutcomeError, outcome)
		_, message, _ := HookLog{}.OutcomeMessage(record)
		require.Equal(t, "exit status 1", message)
	})

	t.Run("a result carrying an error", func(t *testing.T) {
		t.Parallel()

		record := hookLogRecord("AfterMCPExecution", logDialectStringAttribute("gram.hook.error", "server unavailable"))
		_, outcome, _ := HookLog{}.Outcome(record)
		require.Equal(t, OutcomeError, outcome)
	})

	t.Run("an empty error is no error", func(t *testing.T) {
		t.Parallel()

		record := hookLogRecord("PostToolUse", logDialectStringAttribute("gram.hook.error", " "))
		_, outcome, _ := HookLog{}.Outcome(record)
		require.Equal(t, OutcomeOK, outcome)
	})
}

func TestHookLogNeverStatesUsage(t *testing.T) {
	t.Parallel()

	record := hookLogRecord("Stop",
		hookIntAttribute("gen_ai.usage.input_tokens", 1200),
		hookIntAttribute("gen_ai.usage.output_tokens", 300),
		hookDoubleAttribute("gen_ai.usage.cost", 0.02),
	)
	selected := ForLog(record)

	key, _, err := selected.InputTokens(record)
	require.NoError(t, err)
	require.Empty(t, key)
	key, _, err = selected.OutputTokens(record)
	require.NoError(t, err)
	require.Empty(t, key)
	key, _, err = selected.CostUSD(record)
	require.NoError(t, err)
	require.Empty(t, key)
}

func TestHookLogReadsTheActivatedSkill(t *testing.T) {
	t.Parallel()

	record := hookLogRecord("PreToolUse",
		logDialectStringAttribute("gram.tool.name", "Skill"),
		logDialectStringAttribute("gen_ai.tool.call.arguments", `{"skill":"pdf"}`),
	)
	key, skill, err := HookLog{}.SkillName(record)
	require.NoError(t, err)
	require.Equal(t, "gen_ai.tool.call.arguments", key)
	require.Equal(t, "pdf", skill)

	other := hookLogRecord("PreToolUse",
		logDialectStringAttribute("gram.tool.name", "Read"),
		logDialectStringAttribute("gen_ai.tool.call.arguments", `{"skill":"pdf"}`),
	)
	key, _, err = HookLog{}.SkillName(other)
	require.NoError(t, err)
	require.Empty(t, key)
}

func TestHookLogKeepsABareClaudeSourceUnattributed(t *testing.T) {
	t.Parallel()

	record := hookLogRecord("PreToolUse", logDialectStringAttribute("gram.hook.source", "claude"))
	_, surface, _ := HookLog{}.Surface(record)
	require.Equal(t, "claude", surface)
}
