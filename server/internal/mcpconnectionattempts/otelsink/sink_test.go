package otelsink_test

import (
	"context"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
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

func newTestEventSender(t *testing.T) *otelsink.TestSender {
	t.Helper()
	// Unsafe so the test can reach an httptest server on loopback.
	policy, err := guardian.NewUnsafePolicy(testenv.NewTracerProvider(t), nil)
	require.NoError(t, err)
	return otelsink.NewTestSender(otelsvc.NewConnectionLogProbe(testenv.NewLogger(t), testenv.NewMeterProvider(t), policy), "gram-server")
}

func testEventDestination(endpointURL string) dataexports.TestDestination {
	return dataexports.TestDestination{
		OrganizationID:       "org-test",
		ProjectID:            uuid.MustParse("01900000-0000-7000-8000-000000000101"),
		EndpointURL:          endpointURL,
		Headers:              map[string]string{"DD-API-KEY": "secret"},
		IncludeSensitiveData: false,
	}
}

func otlpAttributes(kvs []*commonv1.KeyValue) map[string]*commonv1.AnyValue {
	out := make(map[string]*commonv1.AnyValue, len(kvs))
	for _, kv := range kvs {
		out[kv.GetKey()] = kv.GetValue()
	}
	return out
}

func TestTestSender_SendsOneMarkedRecordAndNothingReal(t *testing.T) {
	t.Parallel()

	var bodies [][]byte
	var headers []http.Header
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, err := io.ReadAll(r.Body)
		assert.NoError(t, err)
		assert.Equal(t, "/v1/logs", r.URL.Path)
		bodies = append(bodies, body)
		headers = append(headers, r.Header.Clone())
		w.WriteHeader(http.StatusOK)
	}))
	t.Cleanup(server.Close)

	status, err := newTestEventSender(t).SendConnectionLogTest(t.Context(), testEventDestination(server.URL))
	require.NoError(t, err)
	require.Equal(t, http.StatusOK, status)
	require.Len(t, bodies, 1)
	require.Equal(t, "secret", headers[0].Get("DD-API-KEY"))
	require.Equal(t, "application/x-protobuf", headers[0].Get("Content-Type"))

	var request collectorlogsv1.ExportLogsServiceRequest
	require.NoError(t, proto.Unmarshal(bodies[0], &request))
	require.Len(t, request.GetResourceLogs(), 1)
	resourceLogs := request.GetResourceLogs()[0]
	require.Equal(t, "gram-server", otlpAttributes(resourceLogs.GetResource().GetAttributes())["service.name"].GetStringValue())
	require.Len(t, resourceLogs.GetScopeLogs(), 1)
	require.Len(t, resourceLogs.GetScopeLogs()[0].GetLogRecords(), 1)
	record := resourceLogs.GetScopeLogs()[0].GetLogRecords()[0]

	require.Equal(t, "speakeasy.mcp_connection.test", record.GetEventName())
	require.Equal(t, logsv1.SeverityNumber_SEVERITY_NUMBER_INFO, record.GetSeverityNumber())
	require.Nil(t, record.GetBody())
	attrs := otlpAttributes(record.GetAttributes())
	require.True(t, attrs["speakeasy.mcp_connection.test"].GetBoolValue())
	require.NotEmpty(t, attrs["log.record.uid"].GetStringValue())
	require.Contains(t, attrs["speakeasy.mcp_connection.user_message"].GetStringValue(), "test event from Speakeasy")
	require.Equal(t, dialect.ConnectionLogScope, attrs["speakeasy.original_instrumentation_scope.name"].GetStringValue())
	for key := range attrs {
		switch key {
		case "speakeasy.mcp_connection.test", "speakeasy.mcp_connection.user_message", "log.record.uid",
			"speakeasy.original_instrumentation_scope.name", "speakeasy.organization.id", "speakeasy.project.id":
		default:
			t.Errorf("unexpected attribute %q on the test record", key)
		}
	}
	require.Empty(t, record.GetTraceId())

	// Each test gets its own uid.
	server2 := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, err := io.ReadAll(r.Body)
		assert.NoError(t, err)
		bodies = append(bodies, body)
		w.WriteHeader(http.StatusAccepted)
	}))
	t.Cleanup(server2.Close)
	_, err = newTestEventSender(t).SendConnectionLogTest(t.Context(), testEventDestination(server2.URL))
	require.NoError(t, err)
	var second collectorlogsv1.ExportLogsServiceRequest
	require.NoError(t, proto.Unmarshal(bodies[1], &second))
	secondAttrs := otlpAttributes(second.GetResourceLogs()[0].GetScopeLogs()[0].GetLogRecords()[0].GetAttributes())
	require.NotEqual(t, attrs["log.record.uid"].GetStringValue(), secondAttrs["log.record.uid"].GetStringValue())
}

func TestTestSender_ReportsStatusesWithoutRetrying(t *testing.T) {
	t.Parallel()

	for _, status := range []int{http.StatusAccepted, http.StatusUnauthorized, http.StatusForbidden, http.StatusNotFound, http.StatusRequestTimeout, http.StatusServiceUnavailable} {
		t.Run(http.StatusText(status), func(t *testing.T) {
			t.Parallel()
			var calls atomic.Int64
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				calls.Add(1)
				w.WriteHeader(status)
			}))
			t.Cleanup(server.Close)

			got, err := newTestEventSender(t).SendConnectionLogTest(t.Context(), testEventDestination(server.URL))
			require.NoError(t, err)
			require.Equal(t, status, got)
			require.EqualValues(t, 1, calls.Load())
		})
	}
}

func TestTestSender_ReportsUnreachableDestinations(t *testing.T) {
	t.Parallel()

	t.Run("connection refused", func(t *testing.T) {
		t.Parallel()
		listener, err := net.Listen("tcp", "127.0.0.1:0")
		require.NoError(t, err)
		endpoint := "http://" + listener.Addr().String()
		require.NoError(t, listener.Close())

		status, err := newTestEventSender(t).SendConnectionLogTest(t.Context(), testEventDestination(endpoint))
		require.ErrorIs(t, err, dataexports.ErrTestUnreachable)
		require.Zero(t, status)
	})

	t.Run("timeout", func(t *testing.T) {
		t.Parallel()
		release := make(chan struct{})
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			select {
			case <-release:
			case <-r.Context().Done():
			}
			w.WriteHeader(http.StatusOK)
		}))
		t.Cleanup(server.Close)
		t.Cleanup(func() { close(release) })

		ctx, cancel := context.WithTimeout(t.Context(), 200*time.Millisecond)
		defer cancel()
		status, err := newTestEventSender(t).SendConnectionLogTest(ctx, testEventDestination(server.URL))
		require.ErrorIs(t, err, dataexports.ErrTestUnreachable)
		require.Zero(t, status)
	})

	t.Run("blocked by the outbound policy", func(t *testing.T) {
		t.Parallel()
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			t.Error("the default policy must not reach loopback")
			w.WriteHeader(http.StatusOK)
		}))
		t.Cleanup(server.Close)
		sender := otelsink.NewTestSender(otelsvc.NewConnectionLogProbe(testenv.NewLogger(t), testenv.NewMeterProvider(t), guardian.NewDefaultPolicy(testenv.NewTracerProvider(t))), "gram-server")

		status, err := sender.SendConnectionLogTest(t.Context(), testEventDestination(server.URL))
		require.ErrorIs(t, err, dataexports.ErrTestUnreachable)
		require.Zero(t, status)
	})
}
