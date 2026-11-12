package otelsink_test

import (
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/google/uuid"
	otelv1 "github.com/speakeasy-api/gram/infra/gen/gram/otel/v1"
	"github.com/speakeasy-api/gram/infra/pkg/gcp"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
	"go.opentelemetry.io/otel/log"
	collectorlogsv1 "go.opentelemetry.io/proto/otlp/collector/logs/v1"
	commonv1 "go.opentelemetry.io/proto/otlp/common/v1"
	logsv1 "go.opentelemetry.io/proto/otlp/logs/v1"
	"google.golang.org/protobuf/proto"

	"github.com/speakeasy-api/gram/server/internal/dataexports"
	"github.com/speakeasy-api/gram/server/internal/guardian"
	"github.com/speakeasy-api/gram/server/internal/mcpconnectionattempts/otelsink"
	otelsvc "github.com/speakeasy-api/gram/server/internal/otel"
	"github.com/speakeasy-api/gram/server/internal/otel/dialect"
	"github.com/speakeasy-api/gram/server/internal/testenv"
)

func newTestConnectionLogExport(t *testing.T) (*otelsink.Sink, *[]*otelv1.InboundLogRecord) {
	t.Helper()

	published := &[]*otelv1.InboundLogRecord{}
	publisher := gcp.NewMockPublisher[*otelv1.InboundLogRecord]()
	publisher.On("Publish", mock.Anything, mock.Anything).
		Run(func(args mock.Arguments) {
			record, ok := args.Get(1).(*otelv1.InboundLogRecord)
			require.True(t, ok)
			*published = append(*published, record)
		}).Return(gcp.NewSuccessPublishResult()).Maybe()

	return otelsink.New(publisher, "gram-server"), published
}

func connectionLogTestRecord() log.Record {
	var record log.Record
	record.SetEventName("speakeasy.mcp_connection")
	record.SetTimestamp(time.Now())
	record.SetSeverity(log.SeverityError)
	record.AddAttributes(log.String("error.type", "idp_unavailable"), log.Float64("speakeasy.mcp_connection.duration", 1.5), log.Int64("speakeasy.mcp_connection.repeated_failures", 2), log.Bool("speakeasy.mcp_connection.final", true))
	return record
}

func TestConnectionLogExport_PublishesUnderTheRowTenant(t *testing.T) {
	t.Parallel()

	export, published := newTestConnectionLogExport(t)
	_, err := export.Publish(t.Context(), "org-row", "project-row", "attempt:step:reason", connectionLogTestRecord()).Get(t.Context())
	require.NoError(t, err)

	require.Len(t, *published, 1)
	record := (*published)[0]
	require.Equal(t, "org-row", record.GetProvenance().GetOrganizationId())
	require.Equal(t, "project-row", record.GetProvenance().GetProjectId())
	require.Equal(t, "attempt:step:reason", record.GetRecordId())
	require.Equal(t, dialect.ConnectionLogScope, record.GetScope().GetName())
	require.Equal(t, dialect.ConnectionLogSource, record.GetProvenance().GetSource())
}

func newTestSource(t *testing.T) *otelsink.TestSource {
	t.Helper()
	return otelsink.NewTestSource(otelsvc.NewConnectionLogTestRequestBuilder(testenv.NewLogger(t), testenv.NewMeterProvider(t)), "gram-server")
}

func testEventDestination(includeSensitiveData bool) dataexports.TestDestination {
	return dataexports.TestDestination{
		OrganizationID:       "org-test",
		ProjectID:            uuid.MustParse("01900000-0000-7000-8000-000000000101"),
		EndpointURL:          "https://collector.example.com",
		Headers:              map[string]string{"DD-API-KEY": "secret"},
		IncludeSensitiveData: includeSensitiveData,
		TestEventID:          "0b1e5f8a-3c2d-4e6f-9a7b-1c2d3e4f5a6b",
	}
}

func otlpAttributes(kvs []*commonv1.KeyValue) map[string]*commonv1.AnyValue {
	out := make(map[string]*commonv1.AnyValue, len(kvs))
	for _, kv := range kvs {
		out[kv.GetKey()] = kv.GetValue()
	}
	return out
}

func buildTestRecord(t *testing.T, includeSensitiveData bool) (*logsv1.ResourceLogs, *logsv1.LogRecord) {
	t.Helper()
	requests, err := newTestSource(t).BuildTestRequests(t.Context(), testEventDestination(includeSensitiveData))
	require.NoError(t, err)
	require.Len(t, requests, 1)
	require.Equal(t, dataexports.SignalLogs, requests[0].Signal)
	request, ok := requests[0].Message.(*collectorlogsv1.ExportLogsServiceRequest)
	require.True(t, ok)
	require.Len(t, request.GetResourceLogs(), 1)
	resourceLogs := request.GetResourceLogs()[0]
	require.Len(t, resourceLogs.GetScopeLogs(), 1)
	require.Len(t, resourceLogs.GetScopeLogs()[0].GetLogRecords(), 1)
	return resourceLogs, resourceLogs.GetScopeLogs()[0].GetLogRecords()[0]
}

func TestTestSource_BuildsOneMarkedRecordAndNothingReal(t *testing.T) {
	t.Parallel()

	require.Equal(t, dataexports.DataSourceMCPConnectionLogs, newTestSource(t).DataSource())
	for _, includeSensitiveData := range []bool{false, true} {
		resourceLogs, record := buildTestRecord(t, includeSensitiveData)
		require.Equal(t, "gram-server", otlpAttributes(resourceLogs.GetResource().GetAttributes())["service.name"].GetStringValue())

		require.Equal(t, "speakeasy.test_event", record.GetEventName())
		require.Equal(t, logsv1.SeverityNumber_SEVERITY_NUMBER_INFO, record.GetSeverityNumber())
		// The marked test message is kept even where sensitive data is excluded.
		require.Equal(t, "Speakeasy test event: this is not real data.", record.GetBody().GetStringValue())
		attrs := otlpAttributes(record.GetAttributes())
		require.True(t, attrs["speakeasy.test_event"].GetBoolValue())
		require.Equal(t, "0b1e5f8a-3c2d-4e6f-9a7b-1c2d3e4f5a6b", attrs["speakeasy.test_event.id"].GetStringValue())
		require.NotEmpty(t, attrs["log.record.uid"].GetStringValue())
		require.Equal(t, "Speakeasy test event: this is not real data.", attrs["speakeasy.mcp_connection.user_message"].GetStringValue())
		require.Equal(t, dialect.ConnectionLogScope, attrs["speakeasy.original_instrumentation_scope.name"].GetStringValue())
		for key := range attrs {
			switch key {
			case "speakeasy.test_event", "speakeasy.test_event.id", "speakeasy.mcp_connection.user_message", "log.record.uid",
				"speakeasy.original_instrumentation_scope.name", "speakeasy.organization.id", "speakeasy.project.id":
			default:
				t.Errorf("unexpected attribute %q on the test record", key)
			}
		}
		require.Empty(t, record.GetTraceId())
	}

	// Each test gets its own uid.
	_, first := buildTestRecord(t, false)
	_, second := buildTestRecord(t, false)
	require.NotEqual(t, otlpAttributes(first.GetAttributes())["log.record.uid"].GetStringValue(), otlpAttributes(second.GetAttributes())["log.record.uid"].GetStringValue())
}

func TestTestSource_SendsThroughTheRouteTester(t *testing.T) {
	t.Parallel()

	var bodies [][]byte
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, err := io.ReadAll(r.Body)
		assert.NoError(t, err)
		assert.Equal(t, "/v1/logs", r.URL.Path)
		assert.Equal(t, "secret", r.Header.Get("DD-API-KEY"))
		bodies = append(bodies, body)
		w.WriteHeader(http.StatusOK)
	}))
	t.Cleanup(server.Close)

	// Unsafe so the test can reach an httptest server on loopback.
	policy, err := guardian.NewUnsafePolicy(testenv.NewTracerProvider(t), nil)
	require.NoError(t, err)
	tester, err := otelsvc.NewRouteTester(policy, append(otelsvc.NewRouteTestSources(testenv.NewLogger(t), testenv.NewMeterProvider(t)), newTestSource(t))...)
	require.NoError(t, err)
	destination := testEventDestination(false)
	destination.EndpointURL = server.URL

	results, err := tester.SendTest(t.Context(), dataexports.DataSourceMCPConnectionLogs, destination)
	require.NoError(t, err)
	require.Equal(t, []dataexports.SignalTestResult{{Signal: dataexports.SignalLogs, StatusCode: http.StatusOK, Unreachable: false}}, results)
	require.Len(t, bodies, 1)
	var request collectorlogsv1.ExportLogsServiceRequest
	require.NoError(t, proto.Unmarshal(bodies[0], &request))
	resource := otlpAttributes(request.GetResourceLogs()[0].GetResource().GetAttributes())
	require.True(t, resource["speakeasy.test_event"].GetBoolValue())
	require.Equal(t, "gram-server", resource["service.name"].GetStringValue())
	record := request.GetResourceLogs()[0].GetScopeLogs()[0].GetLogRecords()[0]
	require.Equal(t, "speakeasy.test_event", record.GetEventName())
	require.Equal(t, destination.TestEventID, otlpAttributes(record.GetAttributes())["speakeasy.test_event.id"].GetStringValue())
}
