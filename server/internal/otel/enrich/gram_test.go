package enrich

import (
	"testing"

	otelv1 "github.com/speakeasy-api/gram/infra/gen/gram/otel/v1"
	"github.com/speakeasy-api/gram/server/internal/otel/dialect"
	"github.com/stretchr/testify/require"
)

// The gateway's own records: the started and completed halves of one tool
// call, under the gateway's scope, from the server's own resource.

func gramStartedRecord(attributes ...*otelv1.InboundLogRecord_KeyValue) *otelv1.InboundLogRecord {
	return inboundTestLog(dialect.GramGatewayLogScope, "gram-server", dialect.GramToolCallStartedEvent, attributes...)
}

func gramCompletedRecord(attributes ...*otelv1.InboundLogRecord_KeyValue) *otelv1.InboundLogRecord {
	return inboundTestLog(dialect.GramGatewayLogScope, "gram-server", dialect.GramToolCallCompletedEvent, attributes...)
}

func TestClassificationForGram(t *testing.T) {
	t.Parallel()

	enricher := &logClassification{}

	t.Run("a started record is a tool_call from no provider on the client that made it", func(t *testing.T) {
		t.Parallel()
		attrs := enriched(t, enricher, gramStartedRecord(logStringAttribute("gram.mcp.client.name", "claude-code")))
		require.Equal(t, dialect.EventTypeToolCall, attrs[AgentEventTypeKey].AsString())
		require.Equal(t, dialect.GramToolCallStartedEvent, attrs[AgentRawEventNameKey].AsString())
		require.Equal(t, "gram-server", attrs[AgentSourceKey].AsString())
		require.NotContains(t, attrs, AgentProviderKey, "a tool call involves no model provider")
		require.Equal(t, "claude-code", attrs[AgentSurfaceKey].AsString(), "the surface is whatever MCP client made the call")
	})

	t.Run("a completed record is the tool_call_result", func(t *testing.T) {
		t.Parallel()
		attrs := enriched(t, enricher, gramCompletedRecord())
		require.Equal(t, dialect.EventTypeToolCallResult, attrs[AgentEventTypeKey].AsString())
		require.Equal(t, dialect.GramToolCallCompletedEvent, attrs[AgentRawEventNameKey].AsString())
		require.NotContains(t, attrs, AgentSurfaceKey, "a call from a client that gave no name has no surface")
	})
}

func TestIdentityForGram(t *testing.T) {
	t.Parallel()

	// Both halves of a call carry the same identity: the session, the user
	// and the call id that is the event. The gateway has no turn, and an
	// external org only when the caller carried one.
	for name, record := range map[string]*otelv1.InboundLogRecord{
		"started":   gramStartedRecord(gramIdentity()...),
		"completed": gramCompletedRecord(gramIdentity()...),
	} {
		attrs := identity(t, record)
		require.Equal(t, "mcp-session-1", attrs[AgentSessionIDKey].AsString(), name)
		require.Equal(t, "call-1", attrs[AgentEventIDKey].AsString(), name)
		require.Equal(t, "ext-user-1", attrs[AgentExternalUserIDKey].AsString(), name)
		require.Equal(t, "dev@example.com", attrs[AgentUserEmailKey].AsString(), name)
		require.NotContains(t, attrs, AgentTurnIDKey, name)
		require.NotContains(t, attrs, AgentExternalOrgIDKey, name)
	}
}

func gramIdentity() []*otelv1.InboundLogRecord_KeyValue {
	return []*otelv1.InboundLogRecord_KeyValue{
		logStringAttribute("gram.tool_call.id", "call-1"),
		logStringAttribute("gram.session.id", "mcp-session-1"),
		logStringAttribute("gram.external_user.id", "ext-user-1"),
		logStringAttribute("user.email", "dev@example.com"),
		logStringAttribute("gram.mcp.client.name", "claude-code"),
	}
}

func TestOperationForGram(t *testing.T) {
	t.Parallel()

	t.Run("a started record names the tool and its server and nothing about how it went", func(t *testing.T) {
		t.Parallel()
		record := gramStartedRecord(
			logStringAttribute("gram.tool.name", "list_repos"),
			logStringAttribute("gram.toolset.slug", "github"),
		)

		attrs := operation(t, record)
		require.Equal(t, "list_repos", attrs[AgentNameKey].AsString())
		require.Equal(t, "list_repos", attrs[AgentToolNameKey].AsString())
		require.Equal(t, "list_repos", attrs[AgentMCPToolNameKey].AsString())
		require.Equal(t, "github", attrs[AgentMCPServerNameKey].AsString())
		require.NotContains(t, attrs, AgentOutcomeKey, "a call that has not returned has no outcome")
		require.NotContains(t, attrs, AgentDurationNanoKey)
	})

	t.Run("a completed record says how the call went and how long it took", func(t *testing.T) {
		t.Parallel()
		record := gramCompletedRecord(
			logStringAttribute("gram.tool.name", "list_repos"),
			logStringAttribute("gram.toolset.slug", "github"),
			logStringAttribute("gram.outcome", dialect.OutcomeOK),
			inboundTestIntAttribute("http.response.status_code", 200),
			inboundTestDoubleAttribute("gram.tool_call.duration", 1.25),
		)

		attrs := operation(t, record)
		require.Equal(t, "list_repos", attrs[AgentToolNameKey].AsString())
		require.Equal(t, "github", attrs[AgentMCPServerNameKey].AsString())
		require.Equal(t, dialect.OutcomeOK, attrs[AgentOutcomeKey].AsString())
		require.Equal(t, int64(1_250_000_000), attrs[AgentDurationNanoKey].AsInt64())
		require.NotContains(t, attrs, AgentModelKey, "a tool call involves no model")
		require.NotContains(t, attrs, AgentOutcomeMessageKey, "a call that went fine has no message")
		require.NotContains(t, attrs, AgentTextKey)
	})

	t.Run("a call the gateway refused carries the message the client was given", func(t *testing.T) {
		t.Parallel()
		record := gramCompletedRecord(
			logStringAttribute("gram.tool.name", "list_repos"),
			logStringAttribute("gram.toolset.slug", "github"),
			logStringAttribute("gram.outcome", dialect.OutcomeError),
			logStringAttribute("error.message", "blocked by policy"),
			inboundTestDoubleAttribute("gram.tool_call.duration", 0.01),
		)

		attrs := operation(t, record)
		require.Equal(t, dialect.OutcomeError, attrs[AgentOutcomeKey].AsString())
		require.Equal(t, "blocked by policy", attrs[AgentOutcomeMessageKey].AsString())
	})

	t.Run("a tool that failed on its own is an error with no message", func(t *testing.T) {
		t.Parallel()
		record := gramCompletedRecord(
			logStringAttribute("gram.tool.name", "list_repos"),
			logStringAttribute("gram.toolset.slug", "github"),
			logStringAttribute("gram.mcp.client.name", "my-custom-agent-7"),
			logStringAttribute("gram.outcome", dialect.OutcomeError),
			inboundTestIntAttribute("http.response.status_code", 502),
			inboundTestDoubleAttribute("gram.tool_call.duration", 0.5),
		)

		attrs := operation(t, record)
		require.Equal(t, dialect.OutcomeError, attrs[AgentOutcomeKey].AsString())
		require.NotContains(t, attrs, AgentOutcomeMessageKey, "the gateway records the tool's error document, not a message about it")
	})
}
