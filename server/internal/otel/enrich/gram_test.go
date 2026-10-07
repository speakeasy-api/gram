package enrich

import (
	"testing"

	otelv1 "github.com/speakeasy-api/gram/infra/gen/gram/otel/v1"
	"github.com/speakeasy-api/gram/server/internal/attr"
	"github.com/speakeasy-api/gram/server/internal/otel/dialect"
	"github.com/speakeasy-api/gram/server/internal/testenv"
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

	reader, meterProvider := readableMeter(t)
	m := NewInstruments(testenv.NewLogger(t), meterProvider)

	// Both halves of a call carry the same identity: the session, the user
	// and the call id that is the event. The gateway has no turn, and an
	// external org only when the caller carried one, so those are counted.
	for name, record := range map[string]*otelv1.InboundLogRecord{
		"started":   gramStartedRecord(gramIdentity()...),
		"completed": gramCompletedRecord(gramIdentity()...),
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			attrs := identity(t, m, record)
			require.Equal(t, "mcp-session-1", attrs[AgentSessionIDKey].AsString())
			require.Equal(t, "call-1", attrs[AgentEventIDKey].AsString())
			require.Equal(t, "ext-user-1", attrs[AgentExternalUserIDKey].AsString())
			require.Equal(t, "dev@example.com", attrs[AgentUserEmailKey].AsString())
			require.NotContains(t, attrs, AgentTurnIDKey)
			require.NotContains(t, attrs, AgentExternalOrgIDKey)
		})
	}

	require.Zero(t, counterValue(t, reader, meterAgentAttributeMissing, attr.AgentEventSurface("claude-code")),
		"a surface read off the record is not a counter label")
	require.Zero(t, counterValue(t, reader, meterAgentAttributeMissing, attr.AgentEventColumn("session_id")))
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
		reader, meterProvider := readableMeter(t)
		in := NewInstruments(testenv.NewLogger(t), meterProvider)
		record := gramStartedRecord(
			logStringAttribute("gram.tool.name", "list_repos"),
			logStringAttribute("gram.toolset.slug", "github"),
		)

		attrs := operation(t, in, record)
		require.Equal(t, "list_repos", attrs[AgentNameKey].AsString())
		require.Equal(t, "list_repos", attrs[AgentToolNameKey].AsString())
		require.Equal(t, "list_repos", attrs[AgentMCPToolNameKey].AsString())
		require.Equal(t, "github", attrs[AgentMCPServerNameKey].AsString())
		require.NotContains(t, attrs, AgentOutcomeKey)
		require.NotContains(t, attrs, AgentDurationNanoKey)
		require.Zero(t, counterValue(t, reader, meterAgentAttributeMissing, attr.AgentEventType(dialect.EventTypeToolCall)),
			"a call that has not returned owes no outcome and no duration")
	})

	t.Run("a completed record says how the call went and how long it took", func(t *testing.T) {
		t.Parallel()
		reader, meterProvider := readableMeter(t)
		in := NewInstruments(testenv.NewLogger(t), meterProvider)
		record := gramCompletedRecord(
			logStringAttribute("gram.tool.name", "list_repos"),
			logStringAttribute("gram.toolset.slug", "github"),
			logStringAttribute("gram.outcome", dialect.OutcomeOK),
			inboundTestIntAttribute("http.response.status_code", 200),
			inboundTestDoubleAttribute("gram.tool_call.duration", 1.25),
		)

		attrs := operation(t, in, record)
		require.Equal(t, "list_repos", attrs[AgentToolNameKey].AsString())
		require.Equal(t, "github", attrs[AgentMCPServerNameKey].AsString())
		require.Equal(t, dialect.OutcomeOK, attrs[AgentOutcomeKey].AsString())
		require.Equal(t, int64(1_250_000_000), attrs[AgentDurationNanoKey].AsInt64())
		require.NotContains(t, attrs, AgentModelKey, "a tool call involves no model")
		require.NotContains(t, attrs, AgentOutcomeMessageKey)
		require.NotContains(t, attrs, AgentTextKey)
		require.Zero(t, counterValue(t, reader, meterAgentAttributeMissing, attr.AgentEventColumn("outcome_message")), "a call that went fine owes no message")
		require.Zero(t, counterValue(t, reader, meterAgentAttributeMissing, attr.AgentEventColumn("mcp_server_name")))
	})

	t.Run("a call the gateway refused carries the message the client was given", func(t *testing.T) {
		t.Parallel()
		reader, meterProvider := readableMeter(t)
		in := NewInstruments(testenv.NewLogger(t), meterProvider)
		record := gramCompletedRecord(
			logStringAttribute("gram.tool.name", "list_repos"),
			logStringAttribute("gram.toolset.slug", "github"),
			logStringAttribute("gram.outcome", dialect.OutcomeError),
			logStringAttribute("error.message", "blocked by policy"),
			inboundTestDoubleAttribute("gram.tool_call.duration", 0.01),
		)

		attrs := operation(t, in, record)
		require.Equal(t, dialect.OutcomeError, attrs[AgentOutcomeKey].AsString())
		require.Equal(t, "blocked by policy", attrs[AgentOutcomeMessageKey].AsString())
		require.Zero(t, counterValue(t, reader, meterAgentAttributeMissing, attr.AgentEventColumn("outcome_message")))
	})

	t.Run("a tool that failed on its own is an error with no message, which is counted under other", func(t *testing.T) {
		t.Parallel()
		reader, meterProvider := readableMeter(t)
		in := NewInstruments(testenv.NewLogger(t), meterProvider)
		record := gramCompletedRecord(
			logStringAttribute("gram.tool.name", "list_repos"),
			logStringAttribute("gram.toolset.slug", "github"),
			logStringAttribute("gram.mcp.client.name", "my-custom-agent-7"),
			logStringAttribute("gram.outcome", dialect.OutcomeError),
			inboundTestIntAttribute("http.response.status_code", 502),
			inboundTestDoubleAttribute("gram.tool_call.duration", 0.5),
		)

		attrs := operation(t, in, record)
		require.Equal(t, dialect.OutcomeError, attrs[AgentOutcomeKey].AsString())
		require.NotContains(t, attrs, AgentOutcomeMessageKey, "the gateway records the tool's error document, not a message about it")
		// The surface was read off the record, not known from the scope, so
		// it is not a label: a client can call itself anything, and a
		// producer-controlled label would make the counter's series unbounded.
		require.Equal(t, int64(1), counterValue(t, reader, meterAgentAttributeMissing,
			attr.AgentEventSurface(missingLabelOther),
			attr.AgentEventType(dialect.EventTypeToolCallResult),
			attr.AgentEventColumn("outcome_message"),
		))
		require.Zero(t, counterValue(t, reader, meterAgentAttributeMissing, attr.AgentEventSurface("my-custom-agent-7")))
	})
}
