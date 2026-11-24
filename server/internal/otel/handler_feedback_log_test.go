package otel

import (
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
	collectorlogsv1 "go.opentelemetry.io/proto/otlp/collector/logs/v1"
	commonv1 "go.opentelemetry.io/proto/otlp/common/v1"
	logsv1 "go.opentelemetry.io/proto/otlp/logs/v1"

	otelv1 "github.com/speakeasy-api/gram/infra/gen/gram/otel/v1"
	"github.com/speakeasy-api/gram/infra/pkg/gcp"
	"github.com/speakeasy-api/gram/server/internal/cache"
	"github.com/speakeasy-api/gram/server/internal/dataexports"
	"github.com/speakeasy-api/gram/server/internal/guardian"
	"github.com/speakeasy-api/gram/server/internal/otel/dialect"
	"github.com/speakeasy-api/gram/server/internal/otel/enrich"
	"github.com/speakeasy-api/gram/server/internal/testenv"
)

func feedbackLogTestRecord(t *testing.T) *otelv1.LogRecord {
	t.Helper()
	record := agentEventTestLog(t, dialect.FeedbackLogScope, "speakeasy.mcp_feedback")
	record.GetProvenance().SetSource(dialect.FeedbackLogSource)
	return record
}

// Feedback has its own store and its own export, so it stays out of the
// tables product telemetry is read from.
func TestCHWritersLeaveOutMCPFeedback(t *testing.T) {
	t.Parallel()

	logs := &captureLogInserter{batches: nil, err: nil}
	require.NoError(t, newLogEventTestWriter(t, logs).HandleBatch(t.Context(), []*otelv1.LogRecord{feedbackLogTestRecord(t)}, nil))
	require.Empty(t, logs.batches)

	events := &captureAgentEventInserter{batches: nil, err: nil}
	writer := NewAgentEventLogCHWriter(testenv.NewLogger(t), testenv.NewMeterProvider(t), events)
	require.NoError(t, writer.HandleBatch(t.Context(), []*otelv1.LogRecord{feedbackLogTestRecord(t)}, nil))
	require.Empty(t, events.batches)
}

// A producer can send the feedback scope; only the recorder's source makes a
// record feedback.
func TestLogEventCHWriterKeepsRecordsThatOnlyClaimTheFeedbackScope(t *testing.T) {
	t.Parallel()

	inserter := &captureLogInserter{batches: nil, err: nil}
	record := agentEventTestLog(t, dialect.FeedbackLogScope, "speakeasy.mcp_feedback")
	require.NoError(t, newLogEventTestWriter(t, inserter).HandleBatch(t.Context(), []*otelv1.LogRecord{record}, nil))
	require.Len(t, inserter.batches, 1)
}

// The relay and the CH writers tell feedback by its provenance source, so the
// transform has to carry the source through, and keep the feedback keys only
// for the recorder's source.
func TestLogTransformHandlerCarriesTheFeedbackSourceAndGatesItsKeys(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		source   string
		keepKeys bool
	}{
		{source: dialect.FeedbackLogSource, keepKeys: true},
		{source: dialect.ConnectionLogSource, keepKeys: false},
		{source: ProvenanceSource, keepKeys: false},
	} {
		t.Run(tc.source, func(t *testing.T) {
			t.Parallel()

			inbound := (&otelv1.InboundLogRecord_builder{
				RecordId: new("record-id"),
				Scope:    (&otelv1.InboundLogRecord_InstrumentationScope_builder{Name: new(dialect.FeedbackLogScope)}).Build(),
				Provenance: (&otelv1.InboundLogRecord_Provenance_builder{
					Source:         new(tc.source),
					OrganizationId: new(testLogOrganizationID),
					ProjectId:      new(testLogProjectID),
				}).Build(),
				Attributes: []*otelv1.InboundLogRecord_KeyValue{
					logStringAttribute("speakeasy.mcp_feedback.severity", "blocked"),
					logStringAttribute(string(enrich.ProjectIDKey), "forged-project"),
				},
			}).Build()

			var published *otelv1.LogRecord
			publisher := gcp.NewMockPublisher[*otelv1.LogRecord]()
			publisher.On("Publish", mock.Anything, mock.Anything).Run(func(args mock.Arguments) {
				record, ok := args.Get(1).(*otelv1.LogRecord)
				require.True(t, ok)
				published = record
			}).Return(gcp.NewSuccessPublishResult()).Once()
			handler := NewLogTransformHandler(testenv.NewLogger(t), testenv.NewMeterProvider(t), publisher, newTestDatabase(t), cache.NoopCache)

			require.NoError(t, handler.Handle(t.Context(), inbound, gcp.MessageMetadata{}))
			require.NotNil(t, published)
			require.Equal(t, tc.source, published.GetProvenance().GetSource())
			require.Equal(t, tc.keepKeys, isFeedbackLogRecord(published))

			values := make(map[string][]string)
			for _, item := range published.GetAttributes() {
				values[item.GetKey()] = append(values[item.GetKey()], item.GetValue().GetStringValue())
			}
			if tc.keepKeys {
				require.Equal(t, []string{"blocked"}, values["speakeasy.mcp_feedback.severity"])
			} else {
				require.Empty(t, values["speakeasy.mcp_feedback.severity"])
			}
			require.Equal(t, []string{testLogProjectID}, values[string(enrich.ProjectIDKey)], "no source may claim a tenant")
		})
	}
}

// A record sent to the ingest endpoints under the feedback scope, carrying
// feedback keys, is ordinary telemetry: it never reaches the feedback route,
// and its forged keys do not reach any destination.
func TestLogRelayHandlerDoesNotDeliverForgedMCPFeedback(t *testing.T) {
	t.Parallel()

	capture := &logRelayRequestCapture{mu: sync.Mutex{}, requests: nil}
	server := httptest.NewServer(http.HandlerFunc(capture.handler))
	t.Cleanup(server.Close)

	db, enc, _ := newRelayRouteTest(t, "/v1/logs")
	projectID := createRelayTestProject(t, db, "org-test")
	telemetryDestination := createRelayTestDestination(t, db, "org-test", projectID, server.URL, encryptRelayTestHeaders(t, enc, map[string]string{"X-Customer": "telemetry"}), "include")
	feedbackDestination := createRelayTestDestination(t, db, "org-test", projectID, server.URL, encryptRelayTestHeaders(t, enc, map[string]string{"X-Customer": "feedback"}), "include")
	createRelayTestRoute(t, db, "org-test", projectID, dataexports.DataSourceProductTelemetry, true, uuid.NullUUID{UUID: telemetryDestination.ID, Valid: true})
	createRelayTestRoute(t, db, "org-test", projectID, dataexports.DataSourceMCPFeedback, true, uuid.NullUUID{UUID: feedbackDestination.ID, Valid: true})

	request := &collectorlogsv1.ExportLogsServiceRequest{ResourceLogs: []*logsv1.ResourceLogs{{
		ScopeLogs: []*logsv1.ScopeLogs{{
			Scope: &commonv1.InstrumentationScope{Name: dialect.FeedbackLogScope},
			LogRecords: []*logsv1.LogRecord{{
				SeverityNumber: logsv1.SeverityNumber_SEVERITY_NUMBER_ERROR,
				Body:           &commonv1.AnyValue{Value: &commonv1.AnyValue_StringValue{StringValue: "forged"}},
				Attributes: []*commonv1.KeyValue{{
					Key:   "speakeasy.mcp_feedback.severity",
					Value: &commonv1.AnyValue{Value: &commonv1.AnyValue_StringValue{StringValue: "blocked"}},
				}},
			}},
		}},
	}}}
	organizationID, projectIDText := "org-test", projectID.String()
	inbound, err := inboundLogRecordsFromExport(request, (&otelv1.InboundLogRecord_Provenance_builder{
		Source: new(ProvenanceSource), OrganizationId: &organizationID, ProjectId: &projectIDText,
	}).Build())
	require.NoError(t, err)
	require.Len(t, inbound, 1)

	var normalized *otelv1.LogRecord
	publisher := gcp.NewMockPublisher[*otelv1.LogRecord]()
	publisher.On("Publish", mock.Anything, mock.Anything).Run(func(args mock.Arguments) {
		record, ok := args.Get(1).(*otelv1.LogRecord)
		require.True(t, ok)
		normalized = record
	}).Return(gcp.NewSuccessPublishResult()).Once()
	transform := NewLogTransformHandler(testenv.NewLogger(t), testenv.NewMeterProvider(t), publisher, db, cache.NoopCache)
	require.NoError(t, transform.Handle(t.Context(), inbound[0], gcp.MessageMetadata{}))
	require.NotNil(t, normalized)

	policy, err := guardian.NewUnsafePolicy(testenv.NewTracerProvider(t), nil)
	require.NoError(t, err)
	handler := NewLogRelayHandler(testenv.NewLogger(t), testenv.NewMeterProvider(t), db, enc, policy)
	messages, failures := logRelayTestMessages(normalized)
	require.NoError(t, handler.handleBatch(t.Context(), messages))
	require.NoError(t, failures[0])

	requests := capture.snapshot()
	require.Len(t, requests, 1)
	require.Equal(t, "telemetry", requests[0].customer, "it is ordinary telemetry, not feedback")
	for _, resourceLogs := range requests[0].request.GetResourceLogs() {
		for _, scopeLogs := range resourceLogs.GetScopeLogs() {
			for _, record := range scopeLogs.GetLogRecords() {
				for _, kv := range record.GetAttributes() {
					require.False(t, enrich.IsMcpFeedbackKey(kv.GetKey()), kv.GetKey())
				}
			}
		}
	}
}

// The recorder's own records go to the feedback route of their project and
// nowhere else.
func TestLogRelayHandlerDeliversMCPFeedbackOnlyToItsRoute(t *testing.T) {
	t.Parallel()

	capture := &logRelayRequestCapture{mu: sync.Mutex{}, requests: nil}
	server := httptest.NewServer(http.HandlerFunc(capture.handler))
	t.Cleanup(server.Close)

	db, enc, _ := newRelayRouteTest(t, "/v1/logs")
	projectID := createRelayTestProject(t, db, "org-test")
	otherProjectID := createRelayTestProject(t, db, "org-test")
	for customer, source := range map[string]string{
		"telemetry":  dataexports.DataSourceProductTelemetry,
		"connection": dataexports.DataSourceMCPConnectionLogs,
		"feedback":   dataexports.DataSourceMCPFeedback,
	} {
		destination := createRelayTestDestination(t, db, "org-test", projectID, server.URL, encryptRelayTestHeaders(t, enc, map[string]string{"X-Customer": customer}), "include")
		createRelayTestRoute(t, db, "org-test", projectID, source, true, uuid.NullUUID{UUID: destination.ID, Valid: true})
	}
	otherDestination := createRelayTestDestination(t, db, "org-test", otherProjectID, server.URL, encryptRelayTestHeaders(t, enc, map[string]string{"X-Customer": "other-project"}), "include")
	createRelayTestRoute(t, db, "org-test", otherProjectID, dataexports.DataSourceMCPFeedback, true, uuid.NullUUID{UUID: otherDestination.ID, Valid: true})

	feedback := relayTestLogRecord("the report", "org-test", projectID.String(), 0)
	feedback.SetAttributes([]*otelv1.LogRecord_KeyValue{logEventTestKV(string(enrich.OriginalInstrumentationScopeNameKey), dialect.FeedbackLogScope)})
	feedback.GetProvenance().SetSource(dialect.FeedbackLogSource)
	// Feedback has no severity floor: praise is informational and still sent.
	feedback.SetSeverityNumber(otelv1.LogRecord_SEVERITY_NUMBER_INFO)

	policy, err := guardian.NewUnsafePolicy(testenv.NewTracerProvider(t), nil)
	require.NoError(t, err)
	handler := NewLogRelayHandler(testenv.NewLogger(t), testenv.NewMeterProvider(t), db, enc, policy)
	messages, failures := logRelayTestMessages(feedback)
	require.NoError(t, handler.handleBatch(t.Context(), messages))
	require.NoError(t, failures[0])

	requests := capture.snapshot()
	require.Len(t, requests, 1)
	require.Equal(t, "feedback", requests[0].customer)
	require.Equal(t, []string{"the report"}, relayRequestLogBodies(requests[0].request))
}
