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
		columns := enrichedColumns(t, enricher, gramStartedRecord(logStringAttribute("gram.mcp.client.name", "claude-code")))
		require.Equal(t, dialect.EventTypeToolCall, columns[EventTypeColumnKey].AsString())
		require.Equal(t, dialect.GramToolCallStartedEvent, columns[RawEventNameColumnKey].AsString())
		require.Equal(t, "gram-server", columns[SourceColumnKey].AsString())
		require.NotContains(t, columns, ProviderColumnKey, "a tool call involves no model provider")
		require.Equal(t, "claude-code", columns[SurfaceColumnKey].AsString(), "the surface is whatever MCP client made the call")
	})

	t.Run("a completed record is the tool_call_result", func(t *testing.T) {
		t.Parallel()
		columns := enrichedColumns(t, enricher, gramCompletedRecord())
		require.Equal(t, dialect.EventTypeToolCallResult, columns[EventTypeColumnKey].AsString())
		require.Equal(t, dialect.GramToolCallCompletedEvent, columns[RawEventNameColumnKey].AsString())
		require.NotContains(t, columns, SurfaceColumnKey, "a call from a client that gave no name has no surface")
	})
}

func TestIdentityColumnsForGram(t *testing.T) {
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
			columns := identity(t, m, record)
			require.Equal(t, "mcp-session-1", columns[SessionIDColumnKey].AsString())
			require.Equal(t, "call-1", columns[EventIDColumnKey].AsString())
			require.Equal(t, "ext-user-1", columns[ExternalUserIDColumnKey].AsString())
			require.Equal(t, "dev@example.com", columns[UserEmailColumnKey].AsString())
			require.NotContains(t, columns, TurnIDColumnKey)
			require.NotContains(t, columns, ExternalOrgIDColumnKey)
		})
	}

	require.Zero(t, counterValue(t, reader, meterColumnEnricherMissing, attr.AgentEventSurface("claude-code")),
		"a surface read off the record is not a counter label")
	require.Zero(t, counterValue(t, reader, meterColumnEnricherMissing, attr.AgentEventColumn("session_id")))
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

func TestOperationColumnsForGram(t *testing.T) {
	t.Parallel()

	t.Run("a started record names the tool and its server and nothing about how it went", func(t *testing.T) {
		t.Parallel()
		reader, meterProvider := readableMeter(t)
		in := NewInstruments(testenv.NewLogger(t), meterProvider)
		record := gramStartedRecord(
			logStringAttribute("gram.tool.name", "list_repos"),
			logStringAttribute("gram.toolset.slug", "github"),
		)

		columns := operation(t, in, record)
		require.Equal(t, "list_repos", columns[NameColumnKey].AsString())
		require.Equal(t, "list_repos", columns[ToolNameColumnKey].AsString())
		require.Equal(t, "list_repos", columns[MCPToolNameColumnKey].AsString())
		require.Equal(t, "github", columns[MCPServerNameColumnKey].AsString())
		require.NotContains(t, columns, OutcomeColumnKey)
		require.NotContains(t, columns, DurationNanoColumnKey)
		require.Zero(t, counterValue(t, reader, meterColumnEnricherMissing, attr.AgentEventType(dialect.EventTypeToolCall)),
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

		columns := operation(t, in, record)
		require.Equal(t, "list_repos", columns[ToolNameColumnKey].AsString())
		require.Equal(t, "github", columns[MCPServerNameColumnKey].AsString())
		require.Equal(t, dialect.OutcomeOK, columns[OutcomeColumnKey].AsString())
		require.Equal(t, int64(1_250_000_000), columns[DurationNanoColumnKey].AsInt64())
		require.NotContains(t, columns, ModelColumnKey, "a tool call involves no model")
		require.NotContains(t, columns, OutcomeMessageColumnKey)
		require.NotContains(t, columns, TextColumnKey)
		require.Zero(t, counterValue(t, reader, meterColumnEnricherMissing, attr.AgentEventColumn("outcome_message")), "a call that went fine owes no message")
		require.Zero(t, counterValue(t, reader, meterColumnEnricherMissing, attr.AgentEventColumn("mcp_server_name")))
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

		columns := operation(t, in, record)
		require.Equal(t, dialect.OutcomeError, columns[OutcomeColumnKey].AsString())
		require.Equal(t, "blocked by policy", columns[OutcomeMessageColumnKey].AsString())
		require.Zero(t, counterValue(t, reader, meterColumnEnricherMissing, attr.AgentEventColumn("outcome_message")))
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

		columns := operation(t, in, record)
		require.Equal(t, dialect.OutcomeError, columns[OutcomeColumnKey].AsString())
		require.NotContains(t, columns, OutcomeMessageColumnKey, "the gateway records the tool's error document, not a message about it")
		// The surface was read off the record, not known from the scope, so
		// it is not a label: a client can call itself anything, and a
		// producer-controlled label would make the counter's series unbounded.
		require.Equal(t, int64(1), counterValue(t, reader, meterColumnEnricherMissing,
			attr.AgentEventSurface(missingLabelOther),
			attr.AgentEventType(dialect.EventTypeToolCallResult),
			attr.AgentEventColumn("outcome_message"),
		))
		require.Zero(t, counterValue(t, reader, meterColumnEnricherMissing, attr.AgentEventSurface("my-custom-agent-7")))
	})
}
