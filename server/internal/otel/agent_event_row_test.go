package otel

import (
	"strings"
	"testing"

	otelv1 "github.com/speakeasy-api/gram/infra/gen/gram/otel/v1"
	"github.com/speakeasy-api/gram/server/internal/otel/dialect"
	"github.com/stretchr/testify/require"
)

const (
	testObservedAt      = int64(1_724_500_000_000_000_002)
	claudeCodeScopeName = "com.anthropic.claude_code.events"
	codexScopeName      = "codex_otel.log_only"
)

func agentEventTestIntKV(key string, value int64) *otelv1.LogRecord_KeyValue {
	return (&otelv1.LogRecord_KeyValue_builder{
		Key:   &key,
		Value: (&otelv1.LogRecord_AnyValue_builder{IntValue: &value}).Build(),
	}).Build()
}

func agentEventTestDoubleKV(key string, value float64) *otelv1.LogRecord_KeyValue {
	return (&otelv1.LogRecord_KeyValue_builder{
		Key:   &key,
		Value: (&otelv1.LogRecord_AnyValue_builder{DoubleValue: &value}).Build(),
	}).Build()
}

func agentEventTestBoolKV(key string, value bool) *otelv1.LogRecord_KeyValue {
	return (&otelv1.LogRecord_KeyValue_builder{
		Key:   &key,
		Value: (&otelv1.LogRecord_AnyValue_builder{BoolValue: &value}).Build(),
	}).Build()
}

func agentEventTestArrayKV(key string, values ...string) *otelv1.LogRecord_KeyValue {
	items := make([]*otelv1.LogRecord_AnyValue, 0, len(values))
	for _, value := range values {
		items = append(items, (&otelv1.LogRecord_AnyValue_builder{StringValue: new(value)}).Build())
	}
	return (&otelv1.LogRecord_KeyValue_builder{
		Key:   &key,
		Value: (&otelv1.LogRecord_AnyValue_builder{ArrayValue: (&otelv1.LogRecord_ArrayValue_builder{Values: items}).Build()}).Build(),
	}).Build()
}

// agentEventTestLog builds a log record the way it looks on the normalized
// topic: scope rewritten to Gram's, the producer's scope kept as an
// attribute, tenancy stamped in provenance.
func agentEventTestLog(originalScope, eventName string, attributes ...*otelv1.LogRecord_KeyValue) *otelv1.LogRecord {
	record := logEventTestRecord("record-1", "org-1", "claude-code")
	record.SetEventName(eventName)
	if originalScope != "" {
		attributes = append(attributes, logEventTestKV(string(OriginalInstrumentationScopeNameKey), originalScope))
	}
	record.SetAttributes(attributes)
	return record
}

func TestAgentEventRowFromLog(t *testing.T) {
	t.Parallel()

	t.Run("it projects a Claude Code api_request into an api_request row", func(t *testing.T) {
		t.Parallel()
		record := agentEventTestLog(claudeCodeScopeName, "api_request",
			logEventTestKV("session.id", "session-1"),
			logEventTestKV("user.email", "dev@example.com"),
			logEventTestKV("user.account_id", "acct-1"),
			logEventTestKV("organization.id", "anthropic-org-1"),
			logEventTestKV("prompt.id", "turn-1"),
			logEventTestKV("request_id", "req_011"),
			logEventTestKV("model", "claude-sonnet-4"),
			agentEventTestIntKV("input_tokens", 120),
			agentEventTestIntKV("output_tokens", 30),
			logEventTestKV("cache_read_tokens", "5"),
			agentEventTestIntKV("cache_creation_tokens", 7),
			agentEventTestDoubleKV("cost_usd", 0.0125),
			agentEventTestDoubleKV("duration_ms", 1500),
			logEventTestKV("query_source", "user_prompt"),
			logEventTestKV("skill.name", "deploy"),
			logEventTestKV(string(directoryDepartmentNameKey), "Platform"),
			agentEventTestArrayKV(string(GramUserRolesKey), "admin", "member"),
			agentEventTestArrayKV(string(DirectoryGroupNamesKey), "eng"),
		)

		row, skip := agentEventRowFromLog(record, testObservedAt)
		require.Empty(t, skip)

		require.Equal(t, "org-1", row.OrganizationID)
		require.Equal(t, "project-1", row.ProjectID)
		require.Equal(t, int64(1_724_500_000_000_000_001), row.OccurredAtUnixNano)
		require.Equal(t, int64(1_724_500_000_000_000_002), row.ObservedAtUnixNano)
		require.Equal(t, "record-1", row.RecordID)
		require.Equal(t, "req_011", row.EventID, "the API request id is the subject, and what joins the response to it")
		require.Equal(t, string(dialect.EventTypeAPIRequest), row.EventType)
		require.Equal(t, "api_request", row.RawEventName)
		require.Equal(t, "session-1", row.SessionID)
		require.Equal(t, "turn-1", row.TurnID)
		require.Equal(t, "claude-code", row.Source)
		require.Equal(t, "anthropic", row.Provider)
		require.Equal(t, "claude-code", row.Surface)
		require.Equal(t, "dev@example.com", row.UserEmail)
		require.Equal(t, "acct-1", row.ExternalUserID)
		require.Equal(t, "anthropic-org-1", row.ExternalOrgID)
		require.Equal(t, "claude-sonnet-4", row.Model)
		require.Equal(t, "user_prompt", row.QuerySource)
		require.Equal(t, "deploy", row.SkillName)
		require.Equal(t, int64(120), row.InputTokens)
		require.Equal(t, int64(30), row.OutputTokens)
		require.Equal(t, int64(5), row.CacheReadTokens, "stringified numbers still count")
		require.Equal(t, int64(7), row.CacheWriteTokens)
		require.InDelta(t, 0.0125, row.CostUSD, 1e-9)
		require.Equal(t, int64(1_500_000_000), row.DurationNano)
		require.Equal(t, string(dialect.OutcomeOK), row.Outcome)
		require.Empty(t, row.Text, "api_request puts everything in attributes")
		require.Equal(t, "Platform", row.DepartmentName)
		require.Equal(t, []string{"admin", "member"}, row.Roles)
		require.Equal(t, []string{"eng"}, row.Groups)
		require.Contains(t, row.Attributes, `"input_tokens":120`)
		require.Contains(t, row.ResourceAttributes, `"service.name":"claude-code"`)
		require.Contains(t, row.ScopeAttributes, `"scope.key":"scope-value"`)
	})

	t.Run("it reads a legacy Claude Code body prefix as the raw event name", func(t *testing.T) {
		t.Parallel()
		record := agentEventTestLog(claudeCodeScopeName, "",
			logEventTestKV("session.id", "session-1"),
			logEventTestKV("user_prompt", "fix the tests"),
		)
		record.SetBody((&otelv1.LogRecord_AnyValue_builder{StringValue: new("claude_code.user_prompt")}).Build())

		row, skip := agentEventRowFromLog(record, testObservedAt)
		require.Empty(t, skip)
		require.Equal(t, string(dialect.EventTypePrompt), row.EventType)
		require.Equal(t, "claude_code.user_prompt", row.RawEventName)
		require.Equal(t, "fix the tests", row.Text)
		require.Contains(t, row.InputContent, `"fix the tests"`)
		require.Empty(t, row.OutputContent)
	})

	t.Run("it projects a failed Claude Code tool_result with its natural id", func(t *testing.T) {
		t.Parallel()
		record := agentEventTestLog(claudeCodeScopeName, "tool_result",
			logEventTestKV("session.id", "session-1"),
			logEventTestKV("tool_name", "Bash"),
			logEventTestKV("tool_use_id", "toolu_1"),
			logEventTestKV("success", "false"),
			logEventTestKV("error", "exit status 1"),
			agentEventTestIntKV("duration_ms", 12),
		)

		row, skip := agentEventRowFromLog(record, testObservedAt)
		require.Empty(t, skip)
		require.Equal(t, string(dialect.EventTypeToolCallResult), row.EventType)
		require.Equal(t, "toolu_1", row.EventID)
		require.Equal(t, "Bash", row.ToolName)
		require.Equal(t, string(dialect.OutcomeError), row.Outcome)
		require.Equal(t, "exit status 1", row.OutcomeMessage)
		require.Equal(t, int64(12_000_000), row.DurationNano)
	})

	t.Run("it projects a successful tool_result as ok", func(t *testing.T) {
		t.Parallel()
		record := agentEventTestLog(claudeCodeScopeName, "tool_result",
			logEventTestKV("tool_name", "Read"),
			agentEventTestBoolKV("success", true),
		)
		row, skip := agentEventRowFromLog(record, testObservedAt)
		require.Empty(t, skip)
		require.Equal(t, string(dialect.OutcomeOK), row.Outcome)
		require.Empty(t, row.OutcomeMessage)
	})

	t.Run("it projects an assistant_response with its transcript message as the subject", func(t *testing.T) {
		t.Parallel()
		record := agentEventTestLog(claudeCodeScopeName, "assistant_response",
			logEventTestKV("message.uuid", "msg-9"),
			logEventTestKV("request_id", "req_011"),
			logEventTestKV("model", "claude-sonnet-4"),
			logEventTestKV("response", "Done. Two files changed."),
			logEventTestKV("query_source", "repl_main_thread"),
		)
		row, skip := agentEventRowFromLog(record, testObservedAt)
		require.Empty(t, skip)
		require.Equal(t, string(dialect.EventTypeAPIResponse), row.EventType)
		require.Equal(t, "msg-9", row.EventID)
		require.Equal(t, "Done. Two files changed.", row.Text)
		require.Contains(t, row.OutputContent, `"role":"assistant"`)
		require.Contains(t, row.OutputContent, `"Done. Two files changed."`)
		require.Empty(t, row.InputContent)
		require.Equal(t, "repl_main_thread", row.QuerySource)
		require.Equal(t, string(dialect.OutcomeOK), row.Outcome)
	})

	t.Run("it projects an api_refusal as refused", func(t *testing.T) {
		t.Parallel()
		record := agentEventTestLog(claudeCodeScopeName, "api_refusal",
			logEventTestKV("request_id", "req_012"),
			logEventTestKV("model", "claude-sonnet-4"),
		)
		row, skip := agentEventRowFromLog(record, testObservedAt)
		require.Empty(t, skip)
		require.Equal(t, string(dialect.EventTypeAPIRefusal), row.EventType)
		require.Equal(t, "req_012", row.EventID)
		require.Equal(t, string(dialect.OutcomeRefused), row.Outcome)
	})

	t.Run("it projects a rejected tool_decision as the terminal observation of that call", func(t *testing.T) {
		t.Parallel()
		record := agentEventTestLog(claudeCodeScopeName, "tool_decision",
			logEventTestKV("tool_name", "Bash"),
			logEventTestKV("tool_use_id", "toolu_2"),
			logEventTestKV("decision_type", "reject"),
			logEventTestKV("decision_source", "user_reject"),
			logEventTestKV("mcp_server_name", "github"),
			logEventTestKV("mcp_tool_name", "create_issue"),
		)
		row, skip := agentEventRowFromLog(record, testObservedAt)
		require.Empty(t, skip)
		require.Equal(t, string(dialect.EventTypeToolDecision), row.EventType)
		require.Equal(t, "toolu_2", row.EventID)
		require.Equal(t, string(dialect.OutcomeRejected), row.Outcome)
		require.Equal(t, "reject", row.Text)
		require.Equal(t, "github", row.MCPServerName)
		require.Equal(t, "create_issue", row.MCPToolName)
		require.Contains(t, row.Attributes, `"decision_source":"user_reject"`, "the source stays in the payload")
	})

	t.Run("it reads tool attribution out of tool_parameters on a tool_result", func(t *testing.T) {
		t.Parallel()
		record := agentEventTestLog(claudeCodeScopeName, "tool_result",
			logEventTestKV("tool_name", "Skill"),
			logEventTestKV("tool_use_id", "toolu_3"),
			agentEventTestBoolKV("success", true),
			logEventTestKV("tool_parameters", `{"skill_name":"deploy","mcp_server_name":"github","mcp_tool_name":"get_pr"}`),
		)
		row, skip := agentEventRowFromLog(record, testObservedAt)
		require.Empty(t, skip)
		require.Equal(t, "deploy", row.SkillName)
		require.Equal(t, "github", row.MCPServerName)
		require.Equal(t, "get_pr", row.MCPToolName)
	})

	t.Run("it uses the error category when the full error is not logged", func(t *testing.T) {
		t.Parallel()
		record := agentEventTestLog(claudeCodeScopeName, "tool_result",
			logEventTestKV("tool_name", "Bash"),
			logEventTestKV("success", "false"),
			logEventTestKV("error_type", "ShellError"),
		)
		row, skip := agentEventRowFromLog(record, testObservedAt)
		require.Empty(t, skip)
		require.Equal(t, string(dialect.OutcomeError), row.Outcome)
		require.Equal(t, "ShellError", row.OutcomeMessage)
	})

	t.Run("it normalizes Codex response.completed usage to disjoint tokens", func(t *testing.T) {
		t.Parallel()
		record := agentEventTestLog(codexScopeName, "codex.sse_event",
			logEventTestKV("event.kind", "response.completed"),
			logEventTestKV("conversation.id", "conv-1"),
			logEventTestKV("user.email", "dev@example.com"),
			logEventTestKV("model", "gpt-5"),
			logEventTestKV("input_token_count", "100"),
			agentEventTestIntKV("cached_token_count", 30),
			agentEventTestIntKV("output_token_count", 7),
		)

		row, skip := agentEventRowFromLog(record, testObservedAt)
		require.Empty(t, skip)
		require.Equal(t, string(dialect.EventTypeAPIRequest), row.EventType)
		require.Equal(t, "codex.sse_event", row.RawEventName)
		require.Equal(t, "conv-1", row.SessionID)
		require.Empty(t, row.TurnID, "Codex states no turn id")
		require.Equal(t, "openai", row.Provider)
		require.Equal(t, "codex", row.Surface)
		require.Equal(t, "gpt-5", row.Model)
		require.Equal(t, int64(70), row.InputTokens, "input excludes cache reads")
		require.Equal(t, int64(30), row.CacheReadTokens)
		require.Equal(t, int64(7), row.OutputTokens)
		require.Zero(t, row.CacheWriteTokens)
		require.Zero(t, row.CostUSD, "Codex reports no cost")
	})

	t.Run("it leaves a non-terminal Codex SSE event unclassified but named", func(t *testing.T) {
		t.Parallel()
		record := agentEventTestLog(codexScopeName, "codex.sse_event",
			logEventTestKV("event.kind", "response.output_item.done"),
		)
		row, skip := agentEventRowFromLog(record, testObservedAt)
		require.Empty(t, skip)
		require.Equal(t, string(dialect.EventTypeUnclassified), row.EventType)
		require.Equal(t, "codex.sse_event", row.RawEventName)
		require.Equal(t, "codex", row.Surface, "the producer is still known even when the event is not")
	})

	t.Run("it keeps a record no dialect recognises, with its body as the text", func(t *testing.T) {
		t.Parallel()
		record := agentEventTestLog("com.example.app", "")
		record.SetBody((&otelv1.LogRecord_AnyValue_builder{StringValue: new("something happened")}).Build())

		row, skip := agentEventRowFromLog(record, testObservedAt)
		require.Empty(t, skip)
		require.Equal(t, string(dialect.EventTypeUnclassified), row.EventType)
		require.Empty(t, row.RawEventName)
		require.Equal(t, "something happened", row.Text)
		require.Equal(t, row.RecordID, row.EventID)
		require.Empty(t, row.Provider)
		require.Empty(t, row.Surface)
		require.Contains(t, row.Attributes, `"speakeasy.original_instrumentation_scope.name":"com.example.app"`, "the payload survives verbatim")
	})

	t.Run("it prefers pipeline-resolved attribution over what the dialect infers", func(t *testing.T) {
		t.Parallel()
		record := agentEventTestLog(claudeCodeScopeName, "api_request",
			logEventTestKV("gram.provider", "bedrock"),
			logEventTestKV("gram.account_type", "enterprise"),
			logEventTestKV("gram.billing_mode", "payg"),
			logEventTestKV("gram.device_id", "device-1"),
			logEventTestKV("user.id", "user-1"),
		)
		row, skip := agentEventRowFromLog(record, testObservedAt)
		require.Empty(t, skip)
		require.Equal(t, "bedrock", row.Provider)
		require.Equal(t, "enterprise", row.AccountType)
		require.Equal(t, "payg", row.BillingMode)
		require.Equal(t, "device-1", row.DeviceID)
		require.Equal(t, "user-1", row.UserID)
	})

	t.Run("it mints a stable delivery id when the record has none", func(t *testing.T) {
		t.Parallel()
		record := agentEventTestLog(claudeCodeScopeName, "api_request")
		record.SetRecordId("")

		first, skip := agentEventRowFromLog(record, testObservedAt)
		require.Empty(t, skip)
		second, skip := agentEventRowFromLog(record, testObservedAt)
		require.Empty(t, skip)
		require.Len(t, first.RecordID, 64)
		require.Equal(t, first.RecordID, second.RecordID)
		require.Equal(t, first.RecordID, first.EventID)
	})

	t.Run("it falls back to the consumer clock for observation time", func(t *testing.T) {
		t.Parallel()
		record := agentEventTestLog(claudeCodeScopeName, "api_request")
		record.SetObservedTimeUnixNano(0)
		row, skip := agentEventRowFromLog(record, testObservedAt)
		require.Empty(t, skip)
		require.Equal(t, testObservedAt, row.ObservedAtUnixNano)
	})

	t.Run("it drops records that cannot be projected, naming the reason", func(t *testing.T) {
		t.Parallel()
		_, skip := agentEventRowFromLog(nil, testObservedAt)
		require.Equal(t, "nil_record", skip)

		noOrg := agentEventTestLog(claudeCodeScopeName, "api_request")
		noOrg.GetProvenance().SetOrganizationId("")
		_, skip = agentEventRowFromLog(noOrg, testObservedAt)
		require.Equal(t, "missing_organization_id", skip)

		noTime := agentEventTestLog(claudeCodeScopeName, "api_request")
		noTime.SetTimeUnixNano(0)
		noTime.SetObservedTimeUnixNano(0)
		_, skip = agentEventRowFromLog(noTime, 0)
		require.Equal(t, "missing_timestamp", skip)
	})
}

func TestAgentEventRowFromSpan(t *testing.T) {
	t.Parallel()

	t.Run("it projects a semconv chat span into an api_request row", func(t *testing.T) {
		t.Parallel()
		span := spanEventTestSpan("org-1", "litellm")
		span.SetName("chat gpt-4o")
		span.SetAttributes([]*otelv1.Span_KeyValue{
			spanEventTestKV(string(OriginalInstrumentationScopeNameKey), "litellm"),
			spanEventTestKV("gen_ai.operation.name", "chat"),
			spanEventTestKV("gen_ai.provider.name", "openai"),
			spanEventTestKV("gen_ai.response.model", "gpt-4o-2024-08-06"),
			spanEventTestKV("gen_ai.response.id", "resp-1"),
			spanEventTestKV("gen_ai.conversation.id", "session-9"),
			spanEventTestKV("gen_ai.usage.input_tokens", "200"),
			spanEventTestKV("gen_ai.usage.output_tokens", "50"),
			spanEventTestKV("gen_ai.usage.cost", "0.002"),
		})

		row, skip := agentEventRowFromSpan(span, testObservedAt)
		require.Empty(t, skip)
		require.Equal(t, "org-1", row.OrganizationID)
		require.Equal(t, strings.Repeat("ab", 16)+":"+strings.Repeat("cd", 8), row.RecordID)
		require.Equal(t, "resp-1", row.EventID)
		require.Equal(t, string(dialect.EventTypeAPIRequest), row.EventType)
		require.Equal(t, "chat gpt-4o", row.RawEventName)
		require.Equal(t, "session-9", row.SessionID)
		require.Equal(t, "openai", row.Provider)
		require.Empty(t, row.Surface, "a proxy span does not say which agent was behind it")
		require.Equal(t, "gpt-4o-2024-08-06", row.Model)
		require.Equal(t, int64(200), row.InputTokens)
		require.Equal(t, int64(50), row.OutputTokens)
		require.InDelta(t, 0.002, row.CostUSD, 1e-9)
		require.Equal(t, int64(1_724_500_000_000_000_001), row.OccurredAtUnixNano)
		require.Equal(t, testObservedAt, row.ObservedAtUnixNano)
		require.Equal(t, int64(500), row.DurationNano)
		require.Equal(t, string(dialect.OutcomeError), row.Outcome)
		require.Equal(t, "boom", row.OutcomeMessage)
		require.Equal(t, "litellm", row.Source)
		require.Empty(t, row.Text)
	})

	t.Run("it projects a semconv execute_tool span into a tool_call row", func(t *testing.T) {
		t.Parallel()
		span := spanEventTestSpan("org-1", "my-agent")
		span.SetName("execute_tool search")
		span.SetAttributes([]*otelv1.Span_KeyValue{
			spanEventTestKV("gen_ai.operation.name", "execute_tool"),
			spanEventTestKV("gen_ai.tool.name", "search"),
			spanEventTestKV("gen_ai.tool.call.id", "call-1"),
		})
		span.SetStatus((&otelv1.Span_Status_builder{Code: otelv1.Span_STATUS_CODE_OK.Enum()}).Build())

		row, skip := agentEventRowFromSpan(span, testObservedAt)
		require.Empty(t, skip)
		require.Equal(t, string(dialect.EventTypeToolCall), row.EventType)
		require.Equal(t, "search", row.ToolName)
		require.Equal(t, "call-1", row.EventID)
		require.Equal(t, string(dialect.OutcomeOK), row.Outcome)
	})

	t.Run("it keeps an unrecognised span with the span name as its raw name", func(t *testing.T) {
		t.Parallel()
		span := spanEventTestSpan("org-1", "my-agent")
		span.SetName("GET /health")
		span.SetAttributes(nil)
		row, skip := agentEventRowFromSpan(span, testObservedAt)
		require.Empty(t, skip)
		require.Equal(t, string(dialect.EventTypeUnclassified), row.EventType)
		require.Equal(t, "GET /health", row.RawEventName)
		require.Equal(t, row.RecordID, row.EventID)
	})

	t.Run("it drops spans that cannot be projected, naming the reason", func(t *testing.T) {
		t.Parallel()
		_, skip := agentEventRowFromSpan(nil, testObservedAt)
		require.Equal(t, "nil_span", skip)

		noIdentity := spanEventTestSpan("org-1", "x")
		noIdentity.SetSpanId(nil)
		_, skip = agentEventRowFromSpan(noIdentity, testObservedAt)
		require.Equal(t, "missing_span_identity", skip)

		noObserved := spanEventTestSpan("org-1", "x")
		_, skip = agentEventRowFromSpan(noObserved, 0)
		require.Equal(t, "missing_observed_time", skip)

		noTime := spanEventTestSpan("org-1", "x")
		noTime.SetStartTimeUnixNano(0)
		noTime.SetEndTimeUnixNano(0)
		_, skip = agentEventRowFromSpan(noTime, testObservedAt)
		require.Equal(t, "missing_timestamp", skip)
	})
}

// A record shaped the way Claude Code 2.1 ships it: no OTLP event name, the
// name repeated as an event.name attribute and as a claude_code.-prefixed
// body, under the com.anthropic.claude_code.events scope, with every id
// as a log attribute rather than a resource attribute.
func TestAgentEventRowFromLogReadsWhatClaudeCodeActuallySends(t *testing.T) {
	t.Parallel()

	record := agentEventTestLog(claudeCodeScopeName, "",
		logEventTestKV("event.name", "user_prompt"),
		logEventTestKV("event.timestamp", "2026-09-15T00:45:12.569Z"),
		logEventTestKV("session.id", "session-1"),
		logEventTestKV("prompt.id", "turn-1"),
		logEventTestKV("message.uuid", "message-1"),
		logEventTestKV("prompt", "fix the tests"),
		agentEventTestIntKV("prompt_length", 13),
		logEventTestKV("user.email", "dev@example.com"),
		logEventTestKV("user.account_id", "acct-1"),
		logEventTestKV("user.account_uuid", "acct-uuid-1"),
		logEventTestKV("organization.id", "anthropic-org-1"),
		logEventTestKV("terminal.type", "tmux"),
	)
	record.SetBody((&otelv1.LogRecord_AnyValue_builder{StringValue: new("claude_code.user_prompt")}).Build())

	row, skip := agentEventRowFromLog(record, testObservedAt)
	require.Empty(t, skip)
	require.Equal(t, string(dialect.EventTypePrompt), row.EventType)
	require.Equal(t, "user_prompt", row.RawEventName, "the event.name attribute names it before the body does")
	require.Equal(t, "message-1", row.EventID)
	require.Equal(t, "session-1", row.SessionID)
	require.Equal(t, "turn-1", row.TurnID)
	require.Equal(t, "anthropic", row.Provider)
	require.Equal(t, "claude-code", row.Surface)
	require.Equal(t, "dev@example.com", row.UserEmail)
	require.Equal(t, "acct-1", row.ExternalUserID)
	require.Equal(t, "anthropic-org-1", row.ExternalOrgID)
	require.Equal(t, "fix the tests", row.Text)
}

// An unclassified Claude Code event whose body is only its name again, under
// the legacy claude_code. prefix, has no words worth keeping as text.
func TestAgentEventRowFromLogDropsABodyThatRepeatsTheEventName(t *testing.T) {
	t.Parallel()

	record := agentEventTestLog(claudeCodeScopeName, "",
		logEventTestKV("event.name", "hook_registered"),
		logEventTestKV("session.id", "session-1"),
		logEventTestKV("hook_event", "PostToolUse"),
	)
	record.SetBody((&otelv1.LogRecord_AnyValue_builder{StringValue: new("claude_code.hook_registered")}).Build())

	row, skip := agentEventRowFromLog(record, testObservedAt)
	require.Empty(t, skip)
	require.Equal(t, dialect.EventTypeUnclassified, row.EventType)
	require.Equal(t, "hook_registered", row.RawEventName)
	require.Empty(t, row.Text)
}

// An MCP tool as a 2.1 CLI reports it with tool details on: the tool is
// named mcp_tool and the server and tool live inside tool_parameters.
func TestAgentEventRowFromLogReadsMCPAttributionFromToolParameters(t *testing.T) {
	t.Parallel()

	record := agentEventTestLog(claudeCodeScopeName, "",
		logEventTestKV("event.name", "tool_result"),
		logEventTestKV("session.id", "session-1"),
		logEventTestKV("tool_name", "mcp_tool"),
		logEventTestKV("tool_use_id", "toolu_1"),
		logEventTestKV("success", "true"),
		logEventTestKV("tool_input", "{}"),
		logEventTestKV("tool_parameters", `{"mcp_server_name":"assistants-dev","mcp_tool_name":"whoami"}`),
	)

	row, skip := agentEventRowFromLog(record, testObservedAt)
	require.Empty(t, skip)
	require.Equal(t, string(dialect.EventTypeToolCallResult), row.EventType)
	require.Equal(t, "mcp_tool", row.ToolName)
	require.Equal(t, "assistants-dev", row.MCPServerName)
	require.Equal(t, "whoami", row.MCPToolName)
}

// The raw payload captures and the session's own compaction, as a 2.1 CLI
// reports them once OTEL_LOG_RAW_API_BODIES is set.
func TestAgentEventRowFromLogClassifiesPayloadsAndCompaction(t *testing.T) {
	t.Parallel()

	t.Run("it lands a response body beside the request it answers", func(t *testing.T) {
		t.Parallel()
		record := agentEventTestLog(claudeCodeScopeName, "",
			logEventTestKV("event.name", "api_response_body"),
			logEventTestKV("session.id", "session-1"),
			logEventTestKV("request_id", "req_011"),
			logEventTestKV("model", "claude-sonnet-4"),
			logEventTestKV("query_source", "compact"),
			logEventTestKV("body", `{"content":[{"type":"text","text":"done"}]}`),
			agentEventTestIntKV("body_length", 42),
		)

		row, skip := agentEventRowFromLog(record, testObservedAt)
		require.Empty(t, skip)
		require.Equal(t, dialect.EventTypeAPIResponseBody, row.EventType)
		require.Equal(t, "req_011", row.EventID, "the same subject as the api_request it answers")
		require.Equal(t, "claude-sonnet-4", row.Model)
		require.Equal(t, "compact", row.QuerySource)
		require.Empty(t, row.Outcome, "the request states the outcome, not its payload")
		require.Contains(t, row.Attributes, `"body"`, "the payload stays in the attributes")
	})

	t.Run("it keeps a request body under its own record id", func(t *testing.T) {
		t.Parallel()
		record := agentEventTestLog(claudeCodeScopeName, "",
			logEventTestKV("event.name", "api_request_body"),
			logEventTestKV("session.id", "session-1"),
			logEventTestKV("model", "claude-sonnet-4"),
			logEventTestKV("body", `{"messages":[]}`),
		)

		row, skip := agentEventRowFromLog(record, testObservedAt)
		require.Empty(t, skip)
		require.Equal(t, dialect.EventTypeAPIRequestBody, row.EventType)
		require.Equal(t, "record-1", row.EventID, "the producer gives a request body no id of its own")
	})

	t.Run("it projects a compaction with its duration and outcome", func(t *testing.T) {
		t.Parallel()
		record := agentEventTestLog(claudeCodeScopeName, "",
			logEventTestKV("event.name", "compaction"),
			logEventTestKV("session.id", "session-1"),
			logEventTestKV("trigger", "auto"),
			agentEventTestBoolKV("success", true),
			agentEventTestDoubleKV("duration_ms", 2500),
			agentEventTestIntKV("pre_tokens", 150_000),
			agentEventTestIntKV("post_tokens", 20_000),
		)

		row, skip := agentEventRowFromLog(record, testObservedAt)
		require.Empty(t, skip)
		require.Equal(t, dialect.EventTypeCompaction, row.EventType)
		require.Equal(t, "compaction", row.RawEventName)
		require.Equal(t, "session-1", row.SessionID)
		require.Equal(t, string(dialect.OutcomeOK), row.Outcome)
		require.Equal(t, int64(2_500_000_000), row.DurationNano)
		require.Zero(t, row.InputTokens, "compaction's token counts are not a request's usage")
		require.Zero(t, row.OutputTokens)
		require.Contains(t, row.Attributes, `"pre_tokens":150000`)
	})

	t.Run("it records why a compaction failed", func(t *testing.T) {
		t.Parallel()
		record := agentEventTestLog(claudeCodeScopeName, "",
			logEventTestKV("event.name", "compaction"),
			logEventTestKV("trigger", "manual"),
			agentEventTestBoolKV("success", false),
			logEventTestKV("error", "context window exceeded"),
		)

		row, skip := agentEventRowFromLog(record, testObservedAt)
		require.Empty(t, skip)
		require.Equal(t, string(dialect.OutcomeError), row.Outcome)
		require.Equal(t, "context window exceeded", row.OutcomeMessage)
	})
}
