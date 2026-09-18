package dialect

import (
	"testing"

	otelv1 "github.com/speakeasy-api/gram/infra/gen/gram/otel/v1"
	"github.com/speakeasy-api/gram/server/internal/genaiconv"
	"github.com/stretchr/testify/require"
)

func accessorTestKV(key, value string) *otelv1.InboundLogRecord_KeyValue {
	return (&otelv1.InboundLogRecord_KeyValue_builder{
		Key:   &key,
		Value: (&otelv1.InboundLogRecord_AnyValue_builder{StringValue: &value}).Build(),
	}).Build()
}

func accessorTestIntKV(key string, value int64) *otelv1.InboundLogRecord_KeyValue {
	return (&otelv1.InboundLogRecord_KeyValue_builder{
		Key:   &key,
		Value: (&otelv1.InboundLogRecord_AnyValue_builder{IntValue: &value}).Build(),
	}).Build()
}

func accessorTestRecord(scope, eventName string, attributes ...*otelv1.InboundLogRecord_KeyValue) *otelv1.InboundLogRecord {
	return (&otelv1.InboundLogRecord_builder{
		EventName:  &eventName,
		Attributes: attributes,
		Scope:      (&otelv1.InboundLogRecord_InstrumentationScope_builder{Name: &scope}).Build(),
	}).Build()
}

const claudeScope = "com.anthropic.claude_code.events"

func TestClaudeCodeLogEventAccessors(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name       string
		record     *otelv1.InboundLogRecord
		eventType  string
		subjectKey string
		subject    string
		outcome    string
		text       string
	}{
		{
			name:       "user_prompt is a prompt identified by its transcript message",
			record:     accessorTestRecord(claudeScope, "user_prompt", accessorTestKV("message.uuid", "m1"), accessorTestKV("prompt", "fix it")),
			eventType:  EventTypePrompt,
			subjectKey: "message.uuid", subject: "m1", outcome: "", text: "fix it",
		},
		{
			name:       "assistant_response is an api_response identified by its transcript message",
			record:     accessorTestRecord(claudeScope, "assistant_response", accessorTestKV("message.uuid", "m2"), accessorTestKV("response", "done")),
			eventType:  EventTypeAPIResponse,
			subjectKey: "message.uuid", subject: "m2", outcome: OutcomeOK, text: "done",
		},
		{
			// A request records that a call was made, not how it went.
			name:       "api_request is identified by the request id and states no outcome",
			record:     accessorTestRecord(claudeScope, "api_request", accessorTestKV("request_id", "req_1"), accessorTestKV("client_request_id", "c1")),
			eventType:  EventTypeAPIRequest,
			subjectKey: "request_id", subject: "req_1", outcome: "", text: "",
		},
		{
			name:       "api_error falls back to the client request id when the server never answered",
			record:     accessorTestRecord(claudeScope, "api_error", accessorTestKV("client_request_id", "c2"), accessorTestKV("error", "timeout")),
			eventType:  EventTypeAPIError,
			subjectKey: "client_request_id", subject: "c2", outcome: OutcomeError, text: "timeout",
		},
		{
			name:       "api_refusal is refused",
			record:     accessorTestRecord(claudeScope, "api_refusal", accessorTestKV("request_id", "req_3")),
			eventType:  EventTypeAPIRefusal,
			subjectKey: "request_id", subject: "req_3", outcome: OutcomeRefused, text: "",
		},
		{
			name:       "tool_result is identified by the tool use id and succeeds",
			record:     accessorTestRecord(claudeScope, "tool_result", accessorTestKV("tool_use_id", "t1"), accessorTestKV("success", "true")),
			eventType:  EventTypeToolCallResult,
			subjectKey: "tool_use_id", subject: "t1", outcome: OutcomeOK, text: "",
		},
		{
			name:       "tool_decision reject is rejected",
			record:     accessorTestRecord(claudeScope, "tool_decision", accessorTestKV("tool_use_id", "t2"), accessorTestKV("decision_type", "reject")),
			eventType:  EventTypeToolDecision,
			subjectKey: "tool_use_id", subject: "t2", outcome: OutcomeRejected, text: "reject",
		},
		{
			name:       "tool_decision accept has no outcome of its own",
			record:     accessorTestRecord(claudeScope, "tool_decision", accessorTestKV("tool_use_id", "t3"), accessorTestKV("decision_type", "accept")),
			eventType:  EventTypeToolDecision,
			subjectKey: "tool_use_id", subject: "t3", outcome: "", text: "accept",
		},
		{
			name:       "api_response_body lands beside the request it answers",
			record:     accessorTestRecord(claudeScope, "api_response_body", accessorTestKV("request_id", "req_4"), accessorTestKV("body", `{"content":[]}`)),
			eventType:  EventTypeAPIResponseBody,
			subjectKey: "request_id", subject: "req_4", outcome: "", text: "",
		},
		{
			name:       "api_request_body is given no id of its own",
			record:     accessorTestRecord(claudeScope, "api_request_body", accessorTestKV("body", `{"messages":[]}`), accessorTestKV("query_source", "compact")),
			eventType:  EventTypeAPIRequestBody,
			subjectKey: "", subject: "", outcome: "", text: "",
		},
		{
			name:       "a completed compaction succeeded",
			record:     accessorTestRecord(claudeScope, "compaction", accessorTestKV("trigger", "auto"), accessorTestKV("success", "true"), accessorTestIntKV("pre_tokens", 150_000), accessorTestIntKV("post_tokens", 20_000)),
			eventType:  EventTypeCompaction,
			subjectKey: "", subject: "", outcome: OutcomeOK, text: "",
		},
		{
			name:       "a compaction that did not finish failed",
			record:     accessorTestRecord(claudeScope, "compaction", accessorTestKV("trigger", "manual"), accessorTestKV("success", "false"), accessorTestKV("error", "context window exceeded")),
			eventType:  EventTypeCompaction,
			subjectKey: "", subject: "", outcome: OutcomeError, text: "",
		},
		{
			name:       "legacy body prefix names the event",
			record:     withBody(accessorTestRecord(claudeScope, ""), "claude_code.api_request"),
			eventType:  EventTypeAPIRequest,
			subjectKey: "", subject: "", outcome: "", text: "",
		},
		{
			name:       "an unknown event stays unclassified",
			record:     accessorTestRecord(claudeScope, "something_new"),
			eventType:  EventTypeUnclassified,
			subjectKey: "", subject: "", outcome: "", text: "",
		},
	}
	for _, tc := range cases {
		t.Run("it says "+tc.name, func(t *testing.T) {
			t.Parallel()
			d := ClaudeCodeLog{}
			_, kind, err := d.EventType(tc.record)
			require.NoError(t, err)
			require.Equal(t, tc.eventType, kind)
			key, subject, err := d.SubjectID(tc.record)
			require.NoError(t, err)
			require.Equal(t, tc.subjectKey, key)
			require.Equal(t, tc.subject, subject)
			_, outcome, err := d.Outcome(tc.record)
			require.NoError(t, err)
			require.Equal(t, tc.outcome, outcome)
			_, text, err := d.Text(tc.record)
			require.NoError(t, err)
			require.Equal(t, tc.text, text)
		})
	}

	t.Run("it reads cost in micros when dollars are absent", func(t *testing.T) {
		t.Parallel()
		key, cost, err := ClaudeCodeLog{}.CostUSD(accessorTestRecord(claudeScope, "api_request", accessorTestKV("cost_usd_micros", "12500")))
		require.NoError(t, err)
		require.Equal(t, "cost_usd_micros", key)
		require.InDelta(t, 0.0125, cost, 1e-9)
	})

	t.Run("it reads attribution from tool_parameters when the flat keys are absent", func(t *testing.T) {
		t.Parallel()
		record := accessorTestRecord(claudeScope, "tool_result", accessorTestKV("tool_parameters", `{"subagent_type":"reviewer","skill_name":"deploy"}`))
		key, agent, err := ClaudeCodeLog{}.AgentName(record)
		require.NoError(t, err)
		require.Equal(t, "tool_parameters.subagent_type", key)
		require.Equal(t, "reviewer", agent)
		key, skill, err := ClaudeCodeLog{}.SkillName(record)
		require.NoError(t, err)
		require.Equal(t, "tool_parameters.skill_name", key)
		require.Equal(t, "deploy", skill)
	})

	t.Run("it falls back to the account uuid for the external user id", func(t *testing.T) {
		t.Parallel()
		key, id, err := ClaudeCodeLog{}.ExternalUserID(accessorTestRecord(claudeScope, "api_request", accessorTestKV("user.account_uuid", "acct-uuid")))
		require.NoError(t, err)
		require.Equal(t, "user.account_uuid", key)
		require.Equal(t, "acct-uuid", id)
	})

	t.Run("it names the producer with the scope as the key", func(t *testing.T) {
		t.Parallel()
		key, provider, err := ClaudeCodeLog{}.Provider(accessorTestRecord(claudeScope, "api_request"))
		require.NoError(t, err)
		require.Equal(t, scopeNameKey, key)
		require.Equal(t, "anthropic", provider)
	})
}

func withBody(record *otelv1.InboundLogRecord, body string) *otelv1.InboundLogRecord {
	record.SetBody((&otelv1.InboundLogRecord_AnyValue_builder{StringValue: &body}).Build())
	return record
}

func TestCodexLogEventAccessors(t *testing.T) {
	t.Parallel()

	t.Run("it keeps a redacted prompt out of the text", func(t *testing.T) {
		t.Parallel()
		_, text, err := CodexLog{}.Text(accessorTestRecord(codexLogScopeName, codexUserPromptEvent, accessorTestKV(codexPromptKey, codexRedactedUserPrompt)))
		require.NoError(t, err)
		require.Empty(t, text)
	})

	t.Run("it normalizes inclusive input tokens to the disjoint shape", func(t *testing.T) {
		t.Parallel()
		record := accessorTestRecord(codexLogScopeName, codexSSEEvent,
			accessorTestKV(codexEventKindKey, codexKindCompleted),
			accessorTestKV("input_token_count", "100"),
			accessorTestIntKV("cached_token_count", 30),
			accessorTestIntKV("output_token_count", 7),
		)
		_, kind, err := CodexLog{}.EventType(record)
		require.NoError(t, err)
		require.Equal(t, EventTypeAPIRequest, kind)
		_, input, err := CodexLog{}.InputTokens(record)
		require.NoError(t, err)
		require.Equal(t, int64(70), input)
		_, cached, err := CodexLog{}.CacheReadTokens(record)
		require.NoError(t, err)
		require.Equal(t, int64(30), cached)
		_, output, err := CodexLog{}.OutputTokens(record)
		require.NoError(t, err)
		require.Equal(t, int64(7), output)
	})

	t.Run("it leaves a non-terminal SSE event unclassified", func(t *testing.T) {
		t.Parallel()
		_, kind, err := CodexLog{}.EventType(accessorTestRecord(codexLogScopeName, codexSSEEvent, accessorTestKV(codexEventKindKey, "response.output_item.done")))
		require.NoError(t, err)
		require.Equal(t, EventTypeUnclassified, kind)
	})

	t.Run("it records a failed response as an api_error", func(t *testing.T) {
		t.Parallel()
		record := accessorTestRecord(codexLogScopeName, codexSSEEvent, accessorTestKV(codexEventKindKey, codexKindFailed), accessorTestKV("error", "rate limited"))
		_, kind, err := CodexLog{}.EventType(record)
		require.NoError(t, err)
		require.Equal(t, EventTypeAPIError, kind)
		_, message, err := CodexLog{}.OutcomeMessage(record)
		require.NoError(t, err)
		require.Equal(t, "rate limited", message)
	})
}

func TestLogFallbackAnswersFieldByField(t *testing.T) {
	t.Parallel()

	// A Claude-scoped record whose event name Claude does not know, but whose
	// gen_ai attributes the semantic conventions can read: the producer's
	// dialect still answers producer questions, semconv answers the rest.
	record := accessorTestRecord(claudeScope, "something_new",
		accessorTestKV("gen_ai.operation.name", "chat"),
		accessorTestKV("gen_ai.response.id", "resp-1"),
		accessorTestIntKV("gen_ai.usage.input_tokens", 12),
	)
	d := ForLog(record)

	key, provider, err := d.Provider(record)
	require.NoError(t, err)
	require.Equal(t, scopeNameKey, key)
	require.Equal(t, "anthropic", provider)

	key, kind, err := d.EventType(record)
	require.NoError(t, err)
	require.Equal(t, "gen_ai.operation.name", key, "the answer names where it came from")
	require.Equal(t, EventTypeAPIRequest, kind)

	_, subject, err := d.SubjectID(record)
	require.NoError(t, err)
	require.Equal(t, "resp-1", subject)

	_, input, err := d.InputTokens(record)
	require.NoError(t, err)
	require.Equal(t, int64(12), input)
}

func TestTypedAttributeReaders(t *testing.T) {
	t.Parallel()

	record := accessorTestRecord(claudeScope, "x",
		accessorTestKV("n_string", "42"),
		accessorTestKV("n_float", "4.7"),
		accessorTestIntKV("n_int", 9),
		accessorTestKV("junk", "not a number"),
		accessorTestKV("b_string", "true"),
	)

	key, n := getOneLogInt64(record, "n_string")
	require.Equal(t, "n_string", key)
	require.Equal(t, int64(42), n)

	_, n = getOneLogInt64(record, "n_float")
	require.Equal(t, int64(4), n)

	key, n = getOneLogInt64(record, "missing", "n_int")
	require.Equal(t, "n_int", key, "the first key that reads wins")
	require.Equal(t, int64(9), n)

	key, _ = getOneLogInt64(record, "junk")
	require.Empty(t, key, "present but unparseable is absent, not zero")

	_, b, present := getOneLogBool(record, "b_string")
	require.True(t, present)
	require.True(t, b)

	_, s := getOneLogAttrAny(record, "n_int")
	require.Equal(t, "9", s)
}

func TestTokensDisjoint(t *testing.T) {
	t.Parallel()

	input, cached := tokensDisjoint(100, 30)
	require.Equal(t, int64(70), input)
	require.Equal(t, int64(30), cached)

	input, cached = tokensDisjoint(100, 500)
	require.Equal(t, int64(0), input, "cached is clamped to input so bad data cannot increase usage")
	require.Equal(t, int64(100), cached)

	input, cached = tokensDisjoint(-5, -3)
	require.Zero(t, input)
	require.Zero(t, cached)
}

func TestClaudeCodeLogTreatsRedactedContentAsAbsent(t *testing.T) {
	t.Parallel()

	prompt := accessorTestRecord(claudeScope, "user_prompt", accessorTestKV("message.uuid", "m1"), accessorTestKV("prompt", "<REDACTED>"))
	key, text, err := ClaudeCodeLog{}.Text(prompt)
	require.NoError(t, err)
	require.Empty(t, key, "a withheld value is not a stated one")
	require.Empty(t, text)
	_, input, err := ClaudeCodeLog{}.InputContent(prompt)
	require.NoError(t, err)
	require.Nil(t, input)

	response := accessorTestRecord(claudeScope, "assistant_response", accessorTestKV("message.uuid", "m2"), accessorTestKV("response", "<REDACTED>"))
	_, text, err = ClaudeCodeLog{}.Text(response)
	require.NoError(t, err)
	require.Empty(t, text)

	failure := accessorTestRecord(claudeScope, "api_error", accessorTestKV("request_id", "req_1"), accessorTestKV("error", "<REDACTED>"))
	_, message, err := ClaudeCodeLog{}.OutcomeMessage(failure)
	require.NoError(t, err)
	require.Empty(t, message)

	stated := accessorTestRecord(claudeScope, "user_prompt", accessorTestKV("message.uuid", "m3"), accessorTestKV("prompt", "fix it"))
	key, text, err = ClaudeCodeLog{}.Text(stated)
	require.NoError(t, err)
	require.Equal(t, "prompt", key)
	require.Equal(t, "fix it", text)
}

func TestClaudeCodeLogReadsMCPAttributionFromToolParameters(t *testing.T) {
	t.Parallel()

	// How a 2.1 CLI reports an MCP tool with tool details on: the tool is
	// named mcp_tool and the server and tool live in tool_parameters.
	record := accessorTestRecord(claudeScope, "tool_result",
		accessorTestKV("tool_name", "mcp_tool"),
		accessorTestKV("tool_use_id", "toolu_1"),
		accessorTestKV("success", "true"),
		accessorTestKV("tool_parameters", `{"mcp_server_name":"assistants-dev","mcp_tool_name":"whoami"}`),
	)
	selected := ForLog(record)

	key, server, err := selected.MCPServerName(record)
	require.NoError(t, err)
	require.Equal(t, "tool_parameters.mcp_server_name", key)
	require.Equal(t, "assistants-dev", server)

	key, tool, err := selected.MCPToolName(record)
	require.NoError(t, err)
	require.Equal(t, "tool_parameters.mcp_tool_name", key)
	require.Equal(t, "whoami", tool)
}

func TestClaudeCodeLogOutputContentIsTheResponse(t *testing.T) {
	t.Parallel()

	response := accessorTestRecord(claudeScope, "assistant_response", accessorTestKV("message.uuid", "m2"), accessorTestKV("response", "Done. Two files changed."))
	key, output, err := ClaudeCodeLog{}.OutputContent(response)
	require.NoError(t, err)
	require.Equal(t, "response", key)
	require.Len(t, output, 1)
	require.Equal(t, genaiconv.RoleAssistant, output[0].Role)
	part, ok := output[0].Parts[0].(*genaiconv.TextPart)
	require.True(t, ok)
	require.Equal(t, "Done. Two files changed.", part.Content)

	redacted := accessorTestRecord(claudeScope, "assistant_response", accessorTestKV("message.uuid", "m3"), accessorTestKV("response", "<REDACTED>"))
	_, output, err = ClaudeCodeLog{}.OutputContent(redacted)
	require.NoError(t, err)
	require.Nil(t, output)

	// A prompt carries no response, whatever attributes it has.
	prompt := accessorTestRecord(claudeScope, "user_prompt", accessorTestKV("response", "not a response"))
	_, output, err = ClaudeCodeLog{}.OutputContent(prompt)
	require.NoError(t, err)
	require.Nil(t, output)
}

func TestClaudeCodeLogReadsWhyACompactionFailed(t *testing.T) {
	t.Parallel()

	failed := accessorTestRecord(claudeScope, "compaction",
		accessorTestKV("success", "false"),
		accessorTestKV("error", "context window exceeded"),
	)
	key, message, err := ClaudeCodeLog{}.OutcomeMessage(failed)
	require.NoError(t, err)
	require.Equal(t, "error", key)
	require.Equal(t, "context window exceeded", message)

	// Compaction's before and after token counts are not a request's usage,
	// so nothing reads them into the token columns.
	done := accessorTestRecord(claudeScope, "compaction",
		accessorTestKV("success", "true"),
		accessorTestIntKV("pre_tokens", 150_000),
		accessorTestIntKV("post_tokens", 20_000),
	)
	_, input, err := ClaudeCodeLog{}.InputTokens(done)
	require.NoError(t, err)
	require.Zero(t, input)
	_, output, err := ClaudeCodeLog{}.OutputTokens(done)
	require.NoError(t, err)
	require.Zero(t, output)
}
