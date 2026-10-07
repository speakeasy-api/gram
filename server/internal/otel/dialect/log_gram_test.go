package dialect

import (
	"testing"

	otelv1 "github.com/speakeasy-api/gram/infra/gen/gram/otel/v1"
	"github.com/stretchr/testify/require"
)

func gramTestDoubleKV(key string, value float64) *otelv1.InboundLogRecord_KeyValue {
	return (&otelv1.InboundLogRecord_KeyValue_builder{
		Key:   &key,
		Value: (&otelv1.InboundLogRecord_AnyValue_builder{DoubleValue: &value}).Build(),
	}).Build()
}

// gramRecord is a record the gateway emitted under its scope.
func gramRecord(eventName string, attributes ...*otelv1.InboundLogRecord_KeyValue) *otelv1.InboundLogRecord {
	return accessorTestRecord(GramGatewayLogScope, eventName, attributes...)
}

func TestGramLogClaimsOnlyTheGatewaysScope(t *testing.T) {
	t.Parallel()

	selected := ForLog(gramRecord(GramToolCallStartedEvent))
	fallback, ok := selected.(LogFallback)
	require.True(t, ok)
	require.Equal(t, []LogDialect{GramLog{}, SemconvLog{}}, fallback.Candidates)

	other := accessorTestRecord("some.other.scope", GramToolCallStartedEvent)
	require.IsType(t, SemconvLog{}, ForLog(other), "the event name alone does not make a record the gateway's")
}

func TestGramLogAStartedRecordIsTheToolCall(t *testing.T) {
	t.Parallel()

	record := gramRecord(GramToolCallStartedEvent,
		accessorTestKV("gram.tool_call.id", "call-1"),
		accessorTestKV("gram.session.id", "session-1"),
		accessorTestKV("gram.tool.name", "list_repos"),
		accessorTestKV("gram.toolset.slug", "github"),
		accessorTestKV("gram.mcp.client.name", "claude-code"),
	)
	selected := ForLog(record)

	key, value, err := selected.EventType(record)
	require.NoError(t, err)
	require.Equal(t, "event_name", key)
	require.Equal(t, EventTypeToolCall, value)

	key, value, err = selected.SubjectID(record)
	require.NoError(t, err)
	require.Equal(t, "gram.tool_call.id", key)
	require.Equal(t, "call-1", value)

	key, value, err = selected.Outcome(record)
	require.NoError(t, err)
	require.Empty(t, key, "a call that has not returned has no outcome")
	require.Empty(t, value)

	key, nanos, err := selected.DurationNano(record)
	require.NoError(t, err)
	require.Empty(t, key)
	require.Zero(t, nanos)
}

func TestGramLogACompletedRecordIsTheToolCallResult(t *testing.T) {
	t.Parallel()

	record := gramRecord(GramToolCallCompletedEvent,
		accessorTestKV("gram.tool_call.id", "call-1"),
		accessorTestKV("gram.tool.name", "list_repos"),
		accessorTestKV("gram.toolset.slug", "github"),
		accessorTestKV("gram.mcp.client.name", "claude-code"),
		accessorTestKV("gram.outcome", OutcomeOK),
		accessorTestIntKV("http.response.status_code", 200),
		gramTestDoubleKV("gram.tool_call.duration", 1.5),
	)
	selected := ForLog(record)

	key, value, err := selected.EventType(record)
	require.NoError(t, err)
	require.Equal(t, "event_name", key)
	require.Equal(t, EventTypeToolCallResult, value)

	key, value, err = selected.EventName(record)
	require.NoError(t, err)
	require.Equal(t, "event_name", key)
	require.Equal(t, GramToolCallCompletedEvent, value)

	key, value, err = selected.SubjectID(record)
	require.NoError(t, err)
	require.Equal(t, "gram.tool_call.id", key)
	require.Equal(t, "call-1", value, "the same subject as the started record")

	key, value, err = selected.ToolName(record)
	require.NoError(t, err)
	require.Equal(t, "gram.tool.name", key)
	require.Equal(t, "list_repos", value)

	key, value, err = selected.MCPToolName(record)
	require.NoError(t, err)
	require.Equal(t, "gram.tool.name", key)
	require.Equal(t, "list_repos", value)

	key, value, err = selected.MCPServerName(record)
	require.NoError(t, err)
	require.Equal(t, "gram.toolset.slug", key)
	require.Equal(t, "github", value)

	key, value, err = selected.Surface(record)
	require.NoError(t, err)
	require.Equal(t, "gram.mcp.client.name", key, "the surface is read off the record, not known from the scope")
	require.Equal(t, "claude-code", value)

	key, value, err = selected.Provider(record)
	require.NoError(t, err)
	require.Empty(t, key, "a tool call involves no model provider")
	require.Empty(t, value)

	key, value, err = selected.Outcome(record)
	require.NoError(t, err)
	require.Equal(t, "gram.outcome", key)
	require.Equal(t, OutcomeOK, value)

	key, nanos, err := selected.DurationNano(record)
	require.NoError(t, err)
	require.Equal(t, "gram.tool_call.duration", key)
	require.Equal(t, int64(1_500_000_000), nanos)

	for name, accessor := range map[string]func(*otelv1.InboundLogRecord) (string, string, error){
		"model":           selected.Model,
		"turn id":         selected.TurnID,
		"response id":     selected.ResponseID,
		"outcome message": selected.OutcomeMessage,
		"text":            selected.Text,
		"query source":    selected.QuerySource,
		"skill name":      selected.SkillName,
		"agent name":      selected.AgentName,
	} {
		key, value, err := accessor(record)
		require.NoError(t, err, name)
		require.Empty(t, key, name)
		require.Empty(t, value, name)
	}
}

func TestGramLogLeavesOtherEventsOnItsScopeUnclassified(t *testing.T) {
	t.Parallel()

	record := gramRecord("gram.meta_discovery", accessorTestKV("gram.tool_call.id", "call-2"))

	key, value, err := ForLog(record).EventType(record)
	require.NoError(t, err)
	require.Empty(t, key)
	require.Equal(t, EventTypeUnclassified, value)
}

func TestGramLogOutcomeIsTheGatewaysVerdictOrTheStatus(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name    string
		attrs   []*otelv1.InboundLogRecord_KeyValue
		key     string
		outcome string
	}{
		{name: "the gateway's own verdict wins", attrs: []*otelv1.InboundLogRecord_KeyValue{accessorTestKV("gram.outcome", OutcomeError), accessorTestIntKV("http.response.status_code", 200)}, key: "gram.outcome", outcome: OutcomeError},
		{name: "a 2xx is ok", attrs: []*otelv1.InboundLogRecord_KeyValue{accessorTestIntKV("http.response.status_code", 200)}, key: "http.response.status_code", outcome: OutcomeOK},
		{name: "a 3xx is not a tool result, so it is an error like isError says", attrs: []*otelv1.InboundLogRecord_KeyValue{accessorTestIntKV("http.response.status_code", 304)}, key: "http.response.status_code", outcome: OutcomeError},
		{name: "a 4xx is an error", attrs: []*otelv1.InboundLogRecord_KeyValue{accessorTestIntKV("http.response.status_code", 404)}, key: "http.response.status_code", outcome: OutcomeError},
		{name: "a 5xx is an error", attrs: []*otelv1.InboundLogRecord_KeyValue{accessorTestIntKV("http.response.status_code", 503)}, key: "http.response.status_code", outcome: OutcomeError},
		{name: "no verdict and no status states no outcome", attrs: []*otelv1.InboundLogRecord_KeyValue{accessorTestKV("gram.tool.name", "x")}, key: "", outcome: ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			record := gramRecord(GramToolCallCompletedEvent, tc.attrs...)
			key, outcome, err := ForLog(record).Outcome(record)
			require.NoError(t, err)
			require.Equal(t, tc.key, key)
			require.Equal(t, tc.outcome, outcome)
		})
	}
}

func TestGramLogOutcomeMessageIsWhatTheClientWasTold(t *testing.T) {
	t.Parallel()

	record := gramRecord(GramToolCallCompletedEvent,
		accessorTestKV("gram.outcome", OutcomeError),
		accessorTestKV("error.message", "blocked by policy"),
	)
	key, value, err := ForLog(record).OutcomeMessage(record)
	require.NoError(t, err)
	require.Equal(t, "error.message", key)
	require.Equal(t, "blocked by policy", value)
}

func TestGramLogSessionIDPrecedence(t *testing.T) {
	t.Parallel()

	t.Run("the MCP session wins", func(t *testing.T) {
		t.Parallel()
		record := gramRecord(GramToolCallStartedEvent,
			accessorTestKV("gram.session.id", "mcp-session"),
			accessorTestKV("gram.chat.id", "chat-1"),
			accessorTestKV("gen_ai.conversation.id", "conv-1"),
		)
		key, value, err := ForLog(record).SessionID(record)
		require.NoError(t, err)
		require.Equal(t, "gram.session.id", key)
		require.Equal(t, "mcp-session", value)
	})

	t.Run("then the chat the call was made from", func(t *testing.T) {
		t.Parallel()
		record := gramRecord(GramToolCallStartedEvent,
			accessorTestKV("gram.chat.id", "chat-1"),
			accessorTestKV("gen_ai.conversation.id", "conv-1"),
		)
		key, value, err := ForLog(record).SessionID(record)
		require.NoError(t, err)
		require.Equal(t, "gram.chat.id", key)
		require.Equal(t, "chat-1", value)
	})

	t.Run("then the conversation the call carried", func(t *testing.T) {
		t.Parallel()
		record := gramRecord(GramToolCallStartedEvent, accessorTestKV("gen_ai.conversation.id", "conv-1"))
		key, value, err := ForLog(record).SessionID(record)
		require.NoError(t, err)
		require.Equal(t, "gen_ai.conversation.id", key)
		require.Equal(t, "conv-1", value)
	})
}

func TestGramLogIdentity(t *testing.T) {
	t.Parallel()

	record := gramRecord(GramToolCallStartedEvent,
		accessorTestKV("gram.external_user.id", "ext-user-1"),
		accessorTestKV("user.email", "dev@example.com"),
		accessorTestKV("gram.external_org_id", "ext-org-1"),
	)
	selected := ForLog(record)

	key, value, err := selected.ExternalUserID(record)
	require.NoError(t, err)
	require.Equal(t, "gram.external_user.id", key)
	require.Equal(t, "ext-user-1", value)

	key, value, err = selected.ExternalUserEmail(record)
	require.NoError(t, err)
	require.Equal(t, "user.email", key)
	require.Equal(t, "dev@example.com", value)

	key, value, err = selected.ExternalOrgID(record)
	require.NoError(t, err)
	require.Equal(t, "gram.external_org_id", key)
	require.Equal(t, "ext-org-1", value)
}
