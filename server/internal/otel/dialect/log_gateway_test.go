package dialect

import (
	"testing"

	otelv1 "github.com/speakeasy-api/gram/infra/gen/gram/otel/v1"
	"github.com/stretchr/testify/require"
)

// gatewayRecord is shaped like the record the MCP gateway emits for one hosted tool call.
func gatewayRecord(scope string, attributes ...*otelv1.InboundLogRecord_KeyValue) *otelv1.InboundLogRecord {
	return accessorTestRecord(scope, "execute_tool", append([]*otelv1.InboundLogRecord_KeyValue{
		accessorTestKV("gen_ai.operation.name", "execute_tool"),
		accessorTestKV("mcp.method.name", "tools/call"),
		accessorTestKV("gen_ai.tool.call.id", "call-1"),
		accessorTestKV("gen_ai.tool.name", "list_repos"),
		accessorTestKV("gen_ai.conversation.id", "mcp-session-1"),
		accessorTestKV("mcp.session.id", "mcp-session-1"),
		accessorTestKV("user.id", "ext-user-1"),
		accessorTestKV("user.email", "dev@example.com"),
		accessorTestKV("gram.toolset.slug", "github"),
		accessorTestKV("gram.mcp.client.name", "claude-code"),
		accessorTestDoubleKV("gram.tool_call.duration", 1.25),
	}, attributes...)...)
}

func requireLogAnswer[V any](t *testing.T, wantKey string, want V, key string, got V, err error) {
	t.Helper()
	require.NoError(t, err)
	require.Equal(t, wantKey, key)
	require.Equal(t, want, got)
}

func TestGatewayLogAnswersEveryColumn(t *testing.T) {
	t.Parallel()

	record := gatewayRecord(GatewayLogScope, accessorTestIntKV("http.response.status_code", 200))
	selected := ForLog(record)
	require.IsType(t, GatewayLog{}, selected)

	key, v, err := selected.EventType(record)
	requireLogAnswer(t, "gen_ai.operation.name", EventTypeToolCallResult, key, v, err)
	key, v, err = selected.EventName(record)
	requireLogAnswer(t, "event_name", "execute_tool", key, v, err)
	key, v, err = selected.Surface(record)
	requireLogAnswer(t, "gram.mcp.client.name", "claude-code", key, v, err)
	key, v, err = selected.Provider(record)
	requireLogAnswer(t, "", "", key, v, err)
	key, v, err = selected.SubjectID(record)
	requireLogAnswer(t, "gen_ai.tool.call.id", "call-1", key, v, err)
	key, v, err = selected.SessionID(record)
	requireLogAnswer(t, "gen_ai.conversation.id", "mcp-session-1", key, v, err)
	key, v, err = selected.ExternalUserID(record)
	requireLogAnswer(t, "user.id", "ext-user-1", key, v, err)
	key, v, err = selected.ExternalUserEmail(record)
	requireLogAnswer(t, "user.email", "dev@example.com", key, v, err)
	key, v, err = selected.TurnID(record)
	requireLogAnswer(t, "", "", key, v, err)
	key, v, err = selected.ToolName(record)
	requireLogAnswer(t, "gen_ai.tool.name", "list_repos", key, v, err)
	key, v, err = selected.MCPToolName(record)
	requireLogAnswer(t, "gen_ai.tool.name", "list_repos", key, v, err)
	key, v, err = selected.MCPServerName(record)
	requireLogAnswer(t, "gram.toolset.slug", "github", key, v, err)
	key, v, err = selected.Outcome(record)
	requireLogAnswer(t, scopeNameKey, OutcomeOK, key, v, err)
	key, v, err = selected.OutcomeMessage(record)
	requireLogAnswer(t, "", "", key, v, err)
	key, v, err = selected.Text(record)
	requireLogAnswer(t, "", "", key, v, err)
	key, v, err = selected.Model(record)
	requireLogAnswer(t, "", "", key, v, err)

	key, nanos, err := selected.DurationNano(record)
	requireLogAnswer(t, "gram.tool_call.duration", int64(1_250_000_000), key, nanos, err)
}

func TestGatewayLogOutcomeFollowsErrorType(t *testing.T) {
	t.Parallel()

	refused := gatewayRecord(GatewayLogScope,
		accessorTestKV("error.type", "forbidden"),
		accessorTestKV("error.message", "blocked by policy"),
	)
	key, v, err := ForLog(refused).Outcome(refused)
	requireLogAnswer(t, "error.type", OutcomeError, key, v, err)
	key, v, err = ForLog(refused).OutcomeMessage(refused)
	requireLogAnswer(t, "error.message", "blocked by policy", key, v, err)

	failed := gatewayRecord(GatewayLogScope, accessorTestKV("error.type", "tool_error"))
	key, v, err = ForLog(failed).Outcome(failed)
	requireLogAnswer(t, "error.type", OutcomeError, key, v, err)
	// The tool's own error document is the result, not a message about it.
	key, v, err = ForLog(failed).OutcomeMessage(failed)
	requireLogAnswer(t, "", "", key, v, err)
}

func TestGatewayLogAppliesOnlyToTheGatewaysScope(t *testing.T) {
	t.Parallel()

	record := gatewayRecord("some.other.scope", accessorTestKV("error.type", "tool_error"))
	selected := ForLog(record)
	require.IsType(t, SemconvLog{}, selected)

	key, v, err := selected.EventType(record)
	requireLogAnswer(t, "gen_ai.operation.name", EventTypeToolCall, key, v, err)
	key, v, err = selected.Surface(record)
	requireLogAnswer(t, "", "", key, v, err)
	key, v, err = selected.MCPServerName(record)
	requireLogAnswer(t, "", "", key, v, err)
	key, v, err = selected.MCPToolName(record)
	requireLogAnswer(t, "", "", key, v, err)
	key, v, err = selected.Outcome(record)
	requireLogAnswer(t, "", "", key, v, err)
}
