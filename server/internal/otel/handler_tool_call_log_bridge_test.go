package otel

import (
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/metric/metricdata"
	"google.golang.org/protobuf/proto"

	otelv1 "github.com/speakeasy-api/gram/infra/gen/gram/otel/v1"
	telemetryv1 "github.com/speakeasy-api/gram/infra/gen/gram/telemetry/v1"
	"github.com/speakeasy-api/gram/infra/pkg/gcp"
	"github.com/speakeasy-api/gram/server/internal/attr"
	"github.com/speakeasy-api/gram/server/internal/cache"
	"github.com/speakeasy-api/gram/server/internal/otel/dialect"
	"github.com/speakeasy-api/gram/server/internal/testenv"
)

// gatewayToolCallAttributes are the attributes the MCP gateway stamps on a
// hosted tool call's telemetry row once the call has run.
func gatewayToolCallAttributes() map[string]any {
	return map[string]any{
		string(attr.HTTPResponseStatusCodeKey): 200,
		string(attr.GenAIConversationIDKey):    "chat-1",
		string(attr.SessionIDKey):              "mcp-session-1",
		string(attr.ExternalUserIDKey):         "ext-user-1",
		string(attr.ToolsetSlugKey):            "github",
		string(attr.ToolNameKey):               "list_repos",
		string(attr.McpClientNameKey):          "claude-code",
		string(attr.ToolCallDurationKey):       1.25,
	}
}

func toolCallLogBridgeTestMessages(records ...*telemetryv1.LogRecord) ([]toolCallLogBridgeMessage, []error) {
	failures := make([]error, len(records))
	messages := make([]toolCallLogBridgeMessage, len(records))
	for i, record := range records {
		index := i
		messages[i] = toolCallLogBridgeMessage{
			record: record,
			fail: func(err error) {
				failures[index] = err
			},
		}
	}
	return messages, failures
}

// bridgeTestPublisher captures every record the bridge publishes and
// answers each with the given result.
func bridgeTestPublisher(t *testing.T, result gcp.PublishResult) (*gcp.MockPublisher[*otelv1.InboundLogRecord], *[]*otelv1.InboundLogRecord) {
	t.Helper()
	published := make([]*otelv1.InboundLogRecord, 0)
	publisher := gcp.NewMockPublisher[*otelv1.InboundLogRecord]()
	publisher.On("Publish", mock.Anything, mock.Anything).Run(func(args mock.Arguments) {
		record, ok := args.Get(1).(*otelv1.InboundLogRecord)
		require.True(t, ok)
		published = append(published, record)
	}).Return(result)
	return publisher, &published
}

func newToolCallLogBridgeTestHandler(t *testing.T, meterProvider *sdkmetric.MeterProvider, publisher gcp.Publisher[*otelv1.InboundLogRecord]) *ToolCallLogBridgeHandler {
	t.Helper()
	handler := NewToolCallLogBridgeHandler(testenv.NewLogger(t), meterProvider, publisher)
	handler.now = func() time.Time { return time.Unix(0, testObservedAt) }
	return handler
}

// bridgeCounterTotal sums every data point of a counter, for the bridged
// counter that carries no label.
func bridgeCounterTotal(t *testing.T, reader *sdkmetric.ManualReader, metricName string) int64 {
	t.Helper()
	var resourceMetrics metricdata.ResourceMetrics
	require.NoError(t, reader.Collect(t.Context(), &resourceMetrics))
	var total int64
	for _, scopeMetrics := range resourceMetrics.ScopeMetrics {
		for _, candidate := range scopeMetrics.Metrics {
			if candidate.Name != metricName {
				continue
			}
			sum, ok := candidate.Data.(metricdata.Sum[int64])
			require.True(t, ok)
			for _, point := range sum.DataPoints {
				total += point.Value
			}
		}
	}
	return total
}

func inboundLogAttributes(record *otelv1.InboundLogRecord) map[string]*otelv1.InboundLogRecord_AnyValue {
	attributes := make(map[string]*otelv1.InboundLogRecord_AnyValue, len(record.GetAttributes()))
	for _, item := range record.GetAttributes() {
		attributes[item.GetKey()] = item.GetValue()
	}
	return attributes
}

func TestToolCallLogBridgeRepublishesToolCallsAndLeavesOtherSourcesAlone(t *testing.T) {
	t.Parallel()

	reader, meterProvider := readableMeter(t)
	publisher, published := bridgeTestPublisher(t, gcp.NewSuccessPublishResult())
	handler := newToolCallLogBridgeTestHandler(t, meterProvider, publisher)

	messages, failures := toolCallLogBridgeTestMessages(
		toolCallLogTestRecord("tool-1", testLogOrganizationID, testLogProjectID, "tool_call", gatewayToolCallAttributes()),
		// Hook rows already enter the pipeline through the hooks tee, and the
		// rest of the topic is not tool calls at all.
		toolCallLogTestRecord("hook-1", testLogOrganizationID, testLogProjectID, "hook", nil),
		toolCallLogTestRecord("discovery-1", testLogOrganizationID, testLogProjectID, "meta_discovery", nil),
		toolCallLogTestRecord("tool-2", testLogOrganizationID, testLogProjectID, "tool_call", gatewayToolCallAttributes()),
	)

	require.NoError(t, handler.handleBatch(t.Context(), messages))
	for _, failure := range failures {
		require.NoError(t, failure)
	}
	publisher.AssertExpectations(t)

	require.Len(t, *published, 2)
	require.Equal(t, "log-tool-1", (*published)[0].GetRecordId())
	require.Equal(t, "log-tool-2", (*published)[1].GetRecordId())
	require.Equal(t, int64(2), bridgeCounterTotal(t, reader, meterToolCallLogBridgeBridged))
	require.Equal(t, int64(2), agentEventCount(t, reader, meterToolCallLogBridgeDropped, attr.ReasonKey, string(relayReasonExcluded)))
}

func TestToolCallLogBridgeStampsWhatTheIngestEdgeWould(t *testing.T) {
	t.Parallel()

	_, meterProvider := readableMeter(t)
	publisher, published := bridgeTestPublisher(t, gcp.NewSuccessPublishResult())
	handler := newToolCallLogBridgeTestHandler(t, meterProvider, publisher)

	row := toolCallLogTestRecord("tool-1", testLogOrganizationID, testLogProjectID, "tool_call", gatewayToolCallAttributes())
	messages, _ := toolCallLogBridgeTestMessages(row)
	require.NoError(t, handler.handleBatch(t.Context(), messages))
	require.Len(t, *published, 1)
	record := (*published)[0]

	require.NoError(t, ValidateInboundLogRecord(record))
	require.Equal(t, row.GetId(), record.GetRecordId(), "the row id is the record id, so a redelivery collapses at read time")
	require.Equal(t, dialect.GramTelemetryLogScope, record.GetScope().GetName())
	require.Equal(t, dialect.GramToolCallEvent, record.GetEventName())
	require.Equal(t, ProvenanceSource, record.GetProvenance().GetSource())
	require.Equal(t, testLogOrganizationID, record.GetProvenance().GetOrganizationId())
	require.Equal(t, testLogProjectID, record.GetProvenance().GetProjectId())
	require.Equal(t, uint64(row.GetObservedTimeUnixNano()), record.GetObservedTimeUnixNano(), "observed time is the row's, never the clock's")
	require.Equal(t, uint64(row.GetTimeUnixNano()), record.GetTimeUnixNano())
	require.Equal(t, "tool-1", record.GetBody().GetStringValue())

	attributes := inboundLogAttributes(record)
	require.Equal(t, row.GetId(), attributes[string(attr.TelemetryLogIDKey)].GetStringValue())
	require.Equal(t, "tool_call", attributes[string(attr.EventSourceKey)].GetStringValue())
	require.Equal(t, testLogOrganizationID, attributes[string(attr.OrganizationIDKey)].GetStringValue())
	require.Equal(t, int64(200), attributes[string(attr.HTTPResponseStatusCodeKey)].GetIntValue())
	require.InDelta(t, 1.25, attributes[string(attr.ToolCallDurationKey)].GetDoubleValue(), 0)
	require.Equal(t, "claude-code", attributes[string(attr.McpClientNameKey)].GetStringValue())

	resource := make(map[string]string, len(record.GetResource().GetAttributes()))
	for _, item := range record.GetResource().GetAttributes() {
		resource[item.GetKey()] = item.GetValue().GetStringValue()
	}
	require.Equal(t, "gram-server", resource[string(attr.ServiceNameKey)])
}

func TestToolCallLogBridgeIsDeterministicAcrossRedeliveries(t *testing.T) {
	t.Parallel()

	row := toolCallLogTestRecord("tool-1", testLogOrganizationID, testLogProjectID, "tool_call", gatewayToolCallAttributes())
	first, _, ok := bridgedToolCallLogRecord(row, func() time.Time { return time.Unix(0, 1) })
	require.True(t, ok)
	second, _, ok := bridgedToolCallLogRecord(row, func() time.Time { return time.Unix(0, 2) })
	require.True(t, ok)
	require.True(t, proto.Equal(first, second), "the same row must bridge to the same record whatever the clock says")
}

func TestToolCallLogBridgeDerivesObservedTimeFromTheRow(t *testing.T) {
	t.Parallel()

	t.Run("from the row's event time when it was never observed", func(t *testing.T) {
		t.Parallel()
		row := toolCallLogTestRecord("tool-1", testLogOrganizationID, testLogProjectID, "tool_call", nil)
		row.SetObservedTimeUnixNano(0)
		record, _, ok := bridgedToolCallLogRecord(row, func() time.Time { return time.Unix(0, 99) })
		require.True(t, ok)
		require.Equal(t, uint64(row.GetTimeUnixNano()), record.GetObservedTimeUnixNano())
	})

	t.Run("from the clock only when the row carries no time at all", func(t *testing.T) {
		t.Parallel()
		row := toolCallLogTestRecord("tool-1", testLogOrganizationID, testLogProjectID, "tool_call", nil)
		row.SetObservedTimeUnixNano(0)
		row.SetTimeUnixNano(0)
		record, _, ok := bridgedToolCallLogRecord(row, func() time.Time { return time.Unix(0, 99) })
		require.True(t, ok)
		require.Equal(t, uint64(99), record.GetObservedTimeUnixNano())
	})
}

func TestToolCallLogBridgeDropsRowsItCannotBridgeWithoutFailingTheBatch(t *testing.T) {
	t.Parallel()

	reader, meterProvider := readableMeter(t)
	publisher, published := bridgeTestPublisher(t, gcp.NewSuccessPublishResult())
	handler := newToolCallLogBridgeTestHandler(t, meterProvider, publisher)

	badJSON := toolCallLogTestRecord("bad-json", testLogOrganizationID, testLogProjectID, "tool_call", nil)
	badJSON.SetAttributesJson("{not json")
	messages, failures := toolCallLogBridgeTestMessages(
		toolCallLogTestRecord("no-org", "", testLogProjectID, "tool_call", nil),
		toolCallLogTestRecord("bad-project", testLogOrganizationID, "not-a-uuid", "tool_call", nil),
		badJSON,
		toolCallLogTestRecord("tool-1", testLogOrganizationID, testLogProjectID, "tool_call", nil),
	)

	require.NoError(t, handler.handleBatch(t.Context(), messages))
	for _, failure := range failures {
		require.NoError(t, failure, "a row redelivery cannot fix is acknowledged, not retried")
	}
	require.Len(t, *published, 1)
	require.Equal(t, "log-tool-1", (*published)[0].GetRecordId())
	require.Equal(t, int64(3), agentEventCount(t, reader, meterToolCallLogBridgeDropped, attr.ReasonKey, string(relayReasonInvalid)))
}

func TestToolCallLogBridgeFailsOnlyTheRowsTheTopicRefused(t *testing.T) {
	t.Parallel()

	reader, meterProvider := readableMeter(t)
	publisher := gcp.NewMockPublisher[*otelv1.InboundLogRecord]()
	refused := errors.New("topic unavailable")
	publisher.On("Publish", mock.Anything, mock.MatchedBy(func(record *otelv1.InboundLogRecord) bool {
		return record.GetRecordId() == "log-tool-2"
	})).Return(gcp.NewErrPublishResult(refused)).Once()
	publisher.On("Publish", mock.Anything, mock.Anything).Return(gcp.NewSuccessPublishResult())
	handler := newToolCallLogBridgeTestHandler(t, meterProvider, publisher)

	messages, failures := toolCallLogBridgeTestMessages(
		toolCallLogTestRecord("tool-1", testLogOrganizationID, testLogProjectID, "tool_call", nil),
		toolCallLogTestRecord("tool-2", testLogOrganizationID, testLogProjectID, "tool_call", nil),
		toolCallLogTestRecord("tool-3", testLogOrganizationID, testLogProjectID, "tool_call", nil),
	)

	require.NoError(t, handler.handleBatch(t.Context(), messages))
	require.NoError(t, failures[0])
	require.ErrorIs(t, failures[1], refused)
	require.NoError(t, failures[2])
	require.Equal(t, int64(2), bridgeCounterTotal(t, reader, meterToolCallLogBridgeBridged))
	require.Equal(t, int64(1), agentEventCount(t, reader, meterToolCallLogBridgeFailed, attr.ReasonKey, string(bridgeReasonPublishError)))
}

// The whole path a hosted tool call takes once the gateway wrote its row:
// bridged, transformed by the same handler as every producer's records,
// and read by the agent_events writer with the columns the gateway's own
// attributes fill.
func TestAgentEventRowFromLogReadsWhatTheGatewayActuallySends(t *testing.T) {
	t.Parallel()

	row := toolCallLogTestRecord("tool-1", testLogOrganizationID, testLogProjectID, "tool_call", gatewayToolCallAttributes())
	row.SetId(uuid.NewString())
	inbound, _, ok := bridgedToolCallLogRecord(row, time.Now)
	require.True(t, ok)

	var published *otelv1.LogRecord
	publisher := gcp.NewMockPublisher[*otelv1.LogRecord]()
	publisher.On("Publish", mock.Anything, mock.Anything).Run(func(args mock.Arguments) {
		record, ok := args.Get(1).(*otelv1.LogRecord)
		require.True(t, ok)
		published = record
	}).Return(gcp.NewSuccessPublishResult()).Once()
	transform := NewLogTransformHandler(testenv.NewLogger(t), testenv.NewMeterProvider(t), publisher, newTestDatabase(t), cache.NoopCache)
	require.NoError(t, transform.Handle(t.Context(), inbound, gcp.MessageMetadata{}))
	require.NotNil(t, published)

	agentEvent, skip := agentEventRowFromLog(published, testObservedAt)
	require.Empty(t, skip)
	require.Equal(t, row.GetId(), agentEvent.RecordID)
	require.Equal(t, row.GetId(), agentEvent.EventID, "the row is the one observation of the call, so it names the event")
	require.Equal(t, testLogOrganizationID, agentEvent.OrganizationID)
	require.Equal(t, testLogProjectID, agentEvent.ProjectID)
	require.Equal(t, string(dialect.EventTypeToolCallResult), agentEvent.EventType)
	require.Equal(t, dialect.GramToolCallEvent, agentEvent.RawEventName)
	require.Equal(t, "gram-server", agentEvent.Source)
	require.Equal(t, "claude-code", agentEvent.Surface)
	require.Empty(t, agentEvent.Provider)
	require.Equal(t, "mcp-session-1", agentEvent.SessionID)
	require.Empty(t, agentEvent.TurnID)
	require.Equal(t, "ext-user-1", agentEvent.ExternalUserID)
	require.Equal(t, "list_repos", agentEvent.Name)
	require.Equal(t, "list_repos", agentEvent.ToolName)
	require.Equal(t, "list_repos", agentEvent.MCPToolName)
	require.Equal(t, "github", agentEvent.MCPServerName)
	require.Equal(t, dialect.OutcomeOK, agentEvent.Outcome)
	require.Empty(t, agentEvent.OutcomeMessage)
	require.Equal(t, int64(1_250_000_000), agentEvent.DurationNano)
	require.Equal(t, uint64(row.GetTimeUnixNano()), uint64(agentEvent.OccurredAtUnixNano))
}
