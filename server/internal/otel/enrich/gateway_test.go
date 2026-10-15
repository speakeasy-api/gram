package enrich

import (
	"testing"

	otelv1 "github.com/speakeasy-api/gram/infra/gen/gram/otel/v1"
	"github.com/speakeasy-api/gram/server/internal/otel/dialect"
	"github.com/stretchr/testify/require"
)

// gatewayRecord is shaped like the record the MCP gateway emits for one hosted tool call.
func gatewayRecord(attributes ...*otelv1.InboundLogRecord_KeyValue) *otelv1.InboundLogRecord {
	return inboundTestLog(dialect.GatewayLogScope, "gram-server", "execute_tool", append([]*otelv1.InboundLogRecord_KeyValue{
		logStringAttribute("gen_ai.operation.name", "execute_tool"),
		logStringAttribute("mcp.method.name", "tools/call"),
		logStringAttribute("gen_ai.tool.call.id", "call-1"),
		logStringAttribute("gen_ai.tool.name", "list_repos"),
		logStringAttribute("gen_ai.conversation.id", "mcp-session-1"),
		logStringAttribute("mcp.session.id", "mcp-session-1"),
		logStringAttribute("user.id", "ext-user-1"),
		logStringAttribute("user.email", "dev@example.com"),
		logStringAttribute("gram.toolset.slug", "github"),
		logStringAttribute("gram.mcp.client.name", "claude-code"),
		inboundTestDoubleAttribute("gram.tool_call.duration", 1.25),
	}, attributes...)...)
}

func TestGatewayRecordFillsEveryColumn(t *testing.T) {
	t.Parallel()

	attrs := agentAttributes(t, gatewayRecord(inboundTestIntAttribute("http.response.status_code", 200)))
	require.Equal(t, dialect.EventTypeToolCall, attrs[AgentEventTypeKey].AsString())
	require.Equal(t, "execute_tool", attrs[AgentRawEventNameKey].AsString())
	require.Equal(t, "gram-server", attrs[AgentSourceKey].AsString())
	require.NotContains(t, attrs, AgentProviderKey, "a tool call involves no model provider")
	require.Equal(t, "claude-code", attrs[AgentSurfaceKey].AsString(), "the surface is whatever MCP client made the call")
	require.Equal(t, "call-1", attrs[AgentEventIDKey].AsString())
	require.Equal(t, "mcp-session-1", attrs[AgentSessionIDKey].AsString())
	require.Equal(t, "ext-user-1", attrs[AgentExternalUserIDKey].AsString())
	require.Equal(t, "dev@example.com", attrs[AgentUserEmailKey].AsString())
	require.NotContains(t, attrs, AgentTurnIDKey)
	require.Equal(t, "list_repos", attrs[AgentNameKey].AsString())
	require.Equal(t, "list_repos", attrs[AgentToolNameKey].AsString())
	require.Equal(t, "list_repos", attrs[AgentMCPToolNameKey].AsString())
	require.Equal(t, "github", attrs[AgentMCPServerNameKey].AsString())
	require.Equal(t, dialect.OutcomeOK, attrs[AgentOutcomeKey].AsString())
	require.NotContains(t, attrs, AgentOutcomeMessageKey, "a call that went fine has no message")
	require.Equal(t, int64(1_250_000_000), attrs[AgentDurationNanoKey].AsInt64())
	require.NotContains(t, attrs, AgentModelKey, "a tool call involves no model")
}

func TestGatewayRecordOutcomeFollowsErrorType(t *testing.T) {
	t.Parallel()

	refused := operation(t, gatewayRecord(
		logStringAttribute("error.type", "forbidden"),
		logStringAttribute("error.message", "blocked by policy"),
	))
	require.Equal(t, dialect.OutcomeError, refused[AgentOutcomeKey].AsString())
	require.Equal(t, "blocked by policy", refused[AgentOutcomeMessageKey].AsString())

	failed := operation(t, gatewayRecord(logStringAttribute("error.type", "tool_error")))
	require.Equal(t, dialect.OutcomeError, failed[AgentOutcomeKey].AsString())
	require.NotContains(t, failed, AgentOutcomeMessageKey, "the tool's own error document is the result, not a message about it")
}

func TestGatewayColumnsApplyOnlyToTheGatewaysScope(t *testing.T) {
	t.Parallel()

	record := inboundTestLog("some.other.scope", "other-agent", "execute_tool",
		logStringAttribute("gen_ai.operation.name", "execute_tool"),
		logStringAttribute("gen_ai.tool.name", "list_repos"),
		logStringAttribute("gram.toolset.slug", "github"),
		logStringAttribute("gram.mcp.client.name", "claude-code"),
		logStringAttribute("error.type", "tool_error"),
	)

	attrs := agentAttributes(t, record)
	require.Equal(t, dialect.EventTypeToolCall, attrs[AgentEventTypeKey].AsString())
	require.NotContains(t, attrs, AgentSurfaceKey)
	require.NotContains(t, attrs, AgentMCPServerNameKey)
	require.NotContains(t, attrs, AgentMCPToolNameKey)
	require.NotContains(t, attrs, AgentOutcomeKey, "another producer's tool_call states no outcome")
}
