package otel

import (
	"context"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	collectorlogsv1 "go.opentelemetry.io/proto/otlp/collector/logs/v1"
	collectormetricsv1 "go.opentelemetry.io/proto/otlp/collector/metrics/v1"
	collectortracev1 "go.opentelemetry.io/proto/otlp/collector/trace/v1"
	commonv1 "go.opentelemetry.io/proto/otlp/common/v1"
	logsv1 "go.opentelemetry.io/proto/otlp/logs/v1"
	metricsv1 "go.opentelemetry.io/proto/otlp/metrics/v1"
	"google.golang.org/protobuf/proto"

	"github.com/speakeasy-api/gram/server/internal/dataexports"
	"github.com/speakeasy-api/gram/server/internal/guardian"
	"github.com/speakeasy-api/gram/server/internal/testenv"
)

// stubRouteTestSource sends one empty logs request, standing in for a source this package does not own.
type stubRouteTestSource struct {
	dataSource string
	signals    []dataexports.Signal
}

var _ RouteTestSource = (*stubRouteTestSource)(nil)

func (s *stubRouteTestSource) DataSource() string { return s.dataSource }

func (s *stubRouteTestSource) BuildTestRequests(context.Context, dataexports.TestDestination) ([]RouteTestRequest, error) {
	requests := make([]RouteTestRequest, len(s.signals))
	for i, signal := range s.signals {
		requests[i] = RouteTestRequest{Signal: signal, Message: &collectorlogsv1.ExportLogsServiceRequest{ResourceLogs: nil}}
	}
	return requests, nil
}

func connectionLogStub() *stubRouteTestSource {
	return &stubRouteTestSource{dataSource: dataexports.DataSourceMCPConnectionLogs, signals: []dataexports.Signal{dataexports.SignalLogs}}
}

func unsafeRouteTestPolicy(t *testing.T) *guardian.Policy {
	t.Helper()
	// Unsafe so the test can reach an httptest server on loopback.
	policy, err := guardian.NewUnsafePolicy(testenv.NewTracerProvider(t), nil)
	require.NoError(t, err)
	return policy
}

func newTestRouteTester(t *testing.T, policy *guardian.Policy) *RouteTester {
	t.Helper()
	tester, err := NewRouteTester(policy, append(NewRouteTestSources(testenv.NewLogger(t), testenv.NewMeterProvider(t)), connectionLogStub())...)
	require.NoError(t, err)
	return tester
}

func routeTestDestination(endpointURL string, includeSensitiveData bool) dataexports.TestDestination {
	return dataexports.TestDestination{
		OrganizationID:       "org-route-test",
		ProjectID:            uuid.MustParse("01900000-0000-7000-8000-000000000202"),
		EndpointURL:          endpointURL,
		Headers:              map[string]string{"DD-API-KEY": "secret"},
		IncludeSensitiveData: includeSensitiveData,
		TestEventID:          routeTestEventID,
	}
}

const routeTestEventID = "0b1e5f8a-3c2d-4e6f-9a7b-1c2d3e4f5a6b"

func requireTestMarks(t *testing.T, attributes []*commonv1.KeyValue) {
	t.Helper()
	attrs := otlpAttributeMap(attributes)
	require.True(t, attrs["speakeasy.test_event"].GetBoolValue())
	require.Equal(t, routeTestEventID, attrs["speakeasy.test_event.id"].GetStringValue())
}

func otlpAttributeMap(kvs []*commonv1.KeyValue) map[string]*commonv1.AnyValue {
	out := make(map[string]*commonv1.AnyValue, len(kvs))
	for _, kv := range kvs {
		out[kv.GetKey()] = kv.GetValue()
	}
	return out
}

func buildRouteTestRequests(t *testing.T, source RouteTestSource, includeSensitiveData bool) []RouteTestRequest {
	t.Helper()
	requests, err := source.BuildTestRequests(t.Context(), routeTestDestination("https://collector.example.com", includeSensitiveData))
	require.NoError(t, err)
	registered, ok := dataexports.LookupSource(source.DataSource())
	require.True(t, ok)
	require.Len(t, requests, len(registered.Signals))
	for i, request := range requests {
		require.Equal(t, registered.Signals[i], request.Signal)
	}
	return requests
}

func singleTestLog(t *testing.T, request RouteTestRequest) (*logsv1.ResourceLogs, *logsv1.ScopeLogs, *logsv1.LogRecord) {
	t.Helper()
	export, ok := request.Message.(*collectorlogsv1.ExportLogsServiceRequest)
	require.True(t, ok)
	resourceLogs := requireOne(t, export.GetResourceLogs())
	scopeLogs := requireOne(t, resourceLogs.GetScopeLogs())
	return resourceLogs, scopeLogs, requireOne(t, scopeLogs.GetLogRecords())
}

func TestNewRouteTester_RequiresExactlyOneSourcePerRegistryEntry(t *testing.T) {
	t.Parallel()

	logger, meterProvider := testenv.NewLogger(t), testenv.NewMeterProvider(t)
	policy := guardian.NewDefaultPolicy(testenv.NewTracerProvider(t))

	_, err := NewRouteTester(policy, NewRouteTestSources(logger, meterProvider)...)
	require.ErrorContains(t, err, `"mcp_connection_logs" has no route test source`)

	_, err = NewRouteTester(policy, append(NewRouteTestSources(logger, meterProvider), connectionLogStub(), connectionLogStub())...)
	require.ErrorContains(t, err, "registered twice")

	_, err = NewRouteTester(policy, append(NewRouteTestSources(logger, meterProvider), connectionLogStub(), &stubRouteTestSource{dataSource: "unknown", signals: nil})...)
	require.ErrorContains(t, err, "not a registered data export source")

	_, err = NewRouteTester(policy, append(NewRouteTestSources(logger, meterProvider), connectionLogStub())...)
	require.NoError(t, err)
}

func TestRouteTester_RefusesASourceWhoseSignalsDifferFromTheRegistry(t *testing.T) {
	t.Parallel()

	sources := []RouteTestSource{
		newToolCallLogTestSource(),
		newRiskFindingTestSource(),
		&stubRouteTestSource{dataSource: dataexports.DataSourceProductTelemetry, signals: []dataexports.Signal{dataexports.SignalLogs}},
		connectionLogStub(),
	}
	tester, err := NewRouteTester(unsafeRouteTestPolicy(t), sources...)
	require.NoError(t, err)

	var calls atomic.Int64
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		calls.Add(1)
		w.WriteHeader(http.StatusOK)
	}))
	t.Cleanup(server.Close)

	_, err = tester.SendTest(t.Context(), dataexports.DataSourceProductTelemetry, routeTestDestination(server.URL, false))
	require.ErrorContains(t, err, "the registry lists")
	require.Zero(t, calls.Load())

	_, err = tester.SendTest(t.Context(), "unknown", routeTestDestination(server.URL, false))
	require.ErrorIs(t, err, dataexports.ErrRouteTestUnsupported)
}

func TestRouteTester_SendsEverySignalToItsPathInParallel(t *testing.T) {
	t.Parallel()

	statuses := map[string]int{"/v1/logs": http.StatusOK, "/v1/traces": http.StatusNotFound, "/v1/metrics": http.StatusUnauthorized}
	var mu sync.Mutex
	bodies := make(map[string][]byte)
	// Every handler waits for all three requests, so a serial sender would time out.
	var arrived sync.WaitGroup
	arrived.Add(len(statuses))
	allArrived := make(chan struct{})
	go func() {
		arrived.Wait()
		close(allArrived)
	}()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, err := io.ReadAll(r.Body)
		assert.NoError(t, err)
		assert.Equal(t, "secret", r.Header.Get("DD-API-KEY"))
		assert.Equal(t, "application/x-protobuf", r.Header.Get("Content-Type"))
		mu.Lock()
		bodies[r.URL.Path] = body
		mu.Unlock()
		arrived.Done()
		select {
		case <-allArrived:
		case <-time.After(3 * time.Second):
			t.Error("signals were not sent in parallel")
		}
		w.WriteHeader(statuses[r.URL.Path])
	}))
	t.Cleanup(server.Close)

	results, err := newTestRouteTester(t, unsafeRouteTestPolicy(t)).SendTest(t.Context(), dataexports.DataSourceProductTelemetry, routeTestDestination(server.URL, false))
	require.NoError(t, err)
	require.Equal(t, []dataexports.SignalTestResult{
		{Signal: dataexports.SignalLogs, StatusCode: http.StatusOK, Unreachable: false},
		{Signal: dataexports.SignalTraces, StatusCode: http.StatusNotFound, Unreachable: false},
		{Signal: dataexports.SignalMetrics, StatusCode: http.StatusUnauthorized, Unreachable: false},
	}, results)

	require.NoError(t, proto.Unmarshal(bodies["/v1/logs"], new(collectorlogsv1.ExportLogsServiceRequest)))
	require.NoError(t, proto.Unmarshal(bodies["/v1/traces"], new(collectortracev1.ExportTraceServiceRequest)))
	require.NoError(t, proto.Unmarshal(bodies["/v1/metrics"], new(collectormetricsv1.ExportMetricsServiceRequest)))
}

func TestRouteTester_ReportsStatusesWithoutRetrying(t *testing.T) {
	t.Parallel()

	for _, status := range []int{http.StatusAccepted, http.StatusUnauthorized, http.StatusNotFound, http.StatusRequestTimeout, http.StatusServiceUnavailable} {
		t.Run(http.StatusText(status), func(t *testing.T) {
			t.Parallel()
			var calls atomic.Int64
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				calls.Add(1)
				w.WriteHeader(status)
			}))
			t.Cleanup(server.Close)

			results, err := newTestRouteTester(t, unsafeRouteTestPolicy(t)).SendTest(t.Context(), dataexports.DataSourceRiskFindings, routeTestDestination(server.URL, false))
			require.NoError(t, err)
			require.Equal(t, []dataexports.SignalTestResult{{Signal: dataexports.SignalLogs, StatusCode: status, Unreachable: false}}, results)
			require.EqualValues(t, 1, calls.Load())
		})
	}
}

func TestRouteTester_ReportsUnreachableDestinations(t *testing.T) {
	t.Parallel()

	unreachable := []dataexports.SignalTestResult{{Signal: dataexports.SignalLogs, StatusCode: 0, Unreachable: true}}

	t.Run("connection refused", func(t *testing.T) {
		t.Parallel()
		listener, err := net.Listen("tcp", "127.0.0.1:0")
		require.NoError(t, err)
		endpoint := "http://" + listener.Addr().String()
		require.NoError(t, listener.Close())

		results, err := newTestRouteTester(t, unsafeRouteTestPolicy(t)).SendTest(t.Context(), dataexports.DataSourceToolCallLogs, routeTestDestination(endpoint, false))
		require.NoError(t, err)
		require.Equal(t, unreachable, results)
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
		results, err := newTestRouteTester(t, unsafeRouteTestPolicy(t)).SendTest(ctx, dataexports.DataSourceToolCallLogs, routeTestDestination(server.URL, false))
		require.NoError(t, err)
		require.Equal(t, unreachable, results)
	})

	t.Run("blocked by the outbound policy", func(t *testing.T) {
		t.Parallel()
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			t.Error("the default policy must not reach loopback")
			w.WriteHeader(http.StatusOK)
		}))
		t.Cleanup(server.Close)

		results, err := newTestRouteTester(t, guardian.NewDefaultPolicy(testenv.NewTracerProvider(t))).SendTest(t.Context(), dataexports.DataSourceToolCallLogs, routeTestDestination(server.URL, false))
		require.NoError(t, err)
		require.Equal(t, unreachable, results)
	})

	t.Run("one signal unreachable, the rest answered", func(t *testing.T) {
		t.Parallel()
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.URL.Path == "/v1/traces" {
				hijacker, ok := w.(http.Hijacker)
				if !assert.True(t, ok) {
					return
				}
				conn, _, err := hijacker.Hijack()
				if assert.NoError(t, err) {
					_ = conn.Close()
				}
				return
			}
			w.WriteHeader(http.StatusOK)
		}))
		t.Cleanup(server.Close)

		results, err := newTestRouteTester(t, unsafeRouteTestPolicy(t)).SendTest(t.Context(), dataexports.DataSourceProductTelemetry, routeTestDestination(server.URL, false))
		require.NoError(t, err)
		require.Equal(t, []dataexports.SignalTestResult{
			{Signal: dataexports.SignalLogs, StatusCode: http.StatusOK, Unreachable: false},
			{Signal: dataexports.SignalTraces, StatusCode: 0, Unreachable: true},
			{Signal: dataexports.SignalMetrics, StatusCode: http.StatusOK, Unreachable: false},
		}, results)
	})
}

func TestToolCallLogTestSource_UsesTheToolCallEnvelopeAndATestName(t *testing.T) {
	t.Parallel()

	for _, includeSensitiveData := range []bool{false, true} {
		requests := buildRouteTestRequests(t, newToolCallLogTestSource(), includeSensitiveData)
		resourceLogs, scopeLogs, record := singleTestLog(t, requests[0])

		require.Equal(t, toolCallLogServiceName, otlpAttributeMap(resourceLogs.GetResource().GetAttributes())["service.name"].GetStringValue())
		require.Equal(t, toolCallLogScopeName, scopeLogs.GetScope().GetName())
		require.Equal(t, "speakeasy.test_event", record.GetEventName())
		require.Equal(t, logsv1.SeverityNumber_SEVERITY_NUMBER_INFO, record.GetSeverityNumber())
		requireTestMarks(t, record.GetAttributes())
		attrs := otlpAttributeMap(record.GetAttributes())
		require.NotContains(t, attrs, "gen_ai.tool.name")
		require.NotContains(t, attrs, "gen_ai.tool.call.id")
		require.NotContains(t, attrs, "gram.telemetry_log.id", "a test row has no telemetry_logs row")
		// The test message is kept even where sensitive data is excluded.
		require.Equal(t, TestEventMessage, record.GetBody().GetStringValue())
	}
}

func TestRiskFindingTestSource_UsesTheRiskFindingEnvelopeAndATestName(t *testing.T) {
	t.Parallel()

	requests := buildRouteTestRequests(t, newRiskFindingTestSource(), false)
	resourceLogs, scopeLogs, record := singleTestLog(t, requests[0])

	require.Equal(t, "gram", otlpAttributeMap(resourceLogs.GetResource().GetAttributes())["service.name"].GetStringValue())
	require.Equal(t, riskFindingScopeName, scopeLogs.GetScope().GetName())
	require.Equal(t, "speakeasy.test_event", record.GetEventName())
	require.False(t, strings.HasPrefix(record.GetEventName(), "gram.risk.finding"))
	require.Equal(t, logsv1.SeverityNumber_SEVERITY_NUMBER_INFO, record.GetSeverityNumber())
	require.Equal(t, TestEventMessage, record.GetBody().GetStringValue())
	requireTestMarks(t, record.GetAttributes())
	attrs := otlpAttributeMap(record.GetAttributes())
	for key := range attrs {
		require.NotContains(t, key, "gram.risk.", "a test finding carries no finding, policy or rule")
	}
}

func TestProductTelemetryTestSource_SendsOneMarkedLogSpanAndMetric(t *testing.T) {
	t.Parallel()

	source := newProductTelemetryTestSource(testenv.NewLogger(t), testenv.NewMeterProvider(t))
	for _, includeSensitiveData := range []bool{false, true} {
		requests := buildRouteTestRequests(t, source, includeSensitiveData)

		resourceLogs, scopeLogs, record := singleTestLog(t, requests[0])
		require.Equal(t, productTelemetryTestServiceName, otlpAttributeMap(resourceLogs.GetResource().GetAttributes())["service.name"].GetStringValue())
		require.Equal(t, normalizedLogInstrumentationScopeName, scopeLogs.GetScope().GetName())
		require.Equal(t, "speakeasy.test_event", record.GetEventName())
		require.Equal(t, logsv1.SeverityNumber_SEVERITY_NUMBER_INFO, record.GetSeverityNumber())
		requireTestMarks(t, record.GetAttributes())
		require.Equal(t, TestEventMessage, record.GetBody().GetStringValue())

		traces, ok := requests[1].Message.(*collectortracev1.ExportTraceServiceRequest)
		require.True(t, ok)
		resourceSpans := requireOne(t, traces.GetResourceSpans())
		require.Equal(t, productTelemetryTestServiceName, otlpAttributeMap(resourceSpans.GetResource().GetAttributes())["service.name"].GetStringValue())
		scopeSpans := requireOne(t, resourceSpans.GetScopeSpans())
		require.Equal(t, normalizedInstrumentationScopeName, scopeSpans.GetScope().GetName())
		span := requireOne(t, scopeSpans.GetSpans())
		require.Equal(t, "speakeasy.test_event", span.GetName())
		requireTestMarks(t, span.GetAttributes())

		metrics, ok := requests[2].Message.(*collectormetricsv1.ExportMetricsServiceRequest)
		require.True(t, ok)
		resourceMetrics := requireOne(t, metrics.GetResourceMetrics())
		require.Equal(t, productTelemetryTestServiceName, otlpAttributeMap(resourceMetrics.GetResource().GetAttributes())["service.name"].GetStringValue())
		item := requireOne(t, requireOne(t, resourceMetrics.GetScopeMetrics()).GetMetrics())
		require.Equal(t, "speakeasy.test_event", item.GetName())
		point := requireOne(t, item.GetGauge().GetDataPoints())
		require.EqualValues(t, 1, point.GetAsInt())
		require.Len(t, point.GetAttributes(), 1, "no test event ID: it would start a new time series every test")
		require.True(t, otlpAttributeMap(point.GetAttributes())["speakeasy.test_event"].GetBoolValue())
	}
}

func TestRouteTester_MarksEveryRequestOfEverySource(t *testing.T) {
	t.Parallel()

	for _, source := range []string{dataexports.DataSourceToolCallLogs, dataexports.DataSourceRiskFindings, dataexports.DataSourceProductTelemetry} {
		t.Run(source, func(t *testing.T) {
			t.Parallel()

			var mu sync.Mutex
			bodies := make(map[string][]byte)
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				body, err := io.ReadAll(r.Body)
				assert.NoError(t, err)
				mu.Lock()
				bodies[r.URL.Path] = body
				mu.Unlock()
				w.WriteHeader(http.StatusOK)
			}))
			t.Cleanup(server.Close)

			_, err := newTestRouteTester(t, unsafeRouteTestPolicy(t)).SendTest(t.Context(), source, routeTestDestination(server.URL, false))
			require.NoError(t, err)

			registered, ok := dataexports.LookupSource(source)
			require.True(t, ok)
			require.Len(t, bodies, len(registered.Signals))
			for path, body := range bodies {
				switch path {
				case "/v1/logs":
					var request collectorlogsv1.ExportLogsServiceRequest
					require.NoError(t, proto.Unmarshal(body, &request))
					for _, resourceLogs := range request.GetResourceLogs() {
						require.True(t, otlpAttributeMap(resourceLogs.GetResource().GetAttributes())["speakeasy.test_event"].GetBoolValue())
						for _, scopeLogs := range resourceLogs.GetScopeLogs() {
							for _, record := range scopeLogs.GetLogRecords() {
								requireTestMarks(t, record.GetAttributes())
								require.True(t, strings.HasPrefix(record.GetBody().GetStringValue(), "Speakeasy test event:"))
							}
						}
					}
				case "/v1/traces":
					var request collectortracev1.ExportTraceServiceRequest
					require.NoError(t, proto.Unmarshal(body, &request))
					resourceSpans := requireOne(t, request.GetResourceSpans())
					require.True(t, otlpAttributeMap(resourceSpans.GetResource().GetAttributes())["speakeasy.test_event"].GetBoolValue())
					requireTestMarks(t, requireOne(t, requireOne(t, resourceSpans.GetScopeSpans()).GetSpans()).GetAttributes())
				case "/v1/metrics":
					var request collectormetricsv1.ExportMetricsServiceRequest
					require.NoError(t, proto.Unmarshal(body, &request))
					resourceMetrics := requireOne(t, request.GetResourceMetrics())
					require.True(t, otlpAttributeMap(resourceMetrics.GetResource().GetAttributes())["speakeasy.test_event"].GetBoolValue())
					item := requireOne(t, requireOne(t, resourceMetrics.GetScopeMetrics()).GetMetrics())
					pointAttrs := otlpAttributeMap(requireOne(t, item.GetGauge().GetDataPoints()).GetAttributes())
					require.True(t, pointAttrs["speakeasy.test_event"].GetBoolValue())
					require.NotContains(t, pointAttrs, "speakeasy.test_event.id")
				default:
					t.Fatalf("unexpected path %s", path)
				}
			}
		})
	}
}

// unmarkedLogSource sends one log record without the test marks.
type unmarkedLogSource struct{}

var _ RouteTestSource = (*unmarkedLogSource)(nil)

func (s *unmarkedLogSource) DataSource() string { return dataexports.DataSourceMCPConnectionLogs }

func (s *unmarkedLogSource) BuildTestRequests(context.Context, dataexports.TestDestination) ([]RouteTestRequest, error) {
	return []RouteTestRequest{{Signal: dataexports.SignalLogs, Message: &collectorlogsv1.ExportLogsServiceRequest{ResourceLogs: []*logsv1.ResourceLogs{{
		ScopeLogs: []*logsv1.ScopeLogs{{LogRecords: []*logsv1.LogRecord{{EventName: "real"}}}},
	}}}}}, nil
}

func TestRouteTester_RefusesUnmarkedRecordsAndAMissingID(t *testing.T) {
	t.Parallel()

	var calls atomic.Int64
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		calls.Add(1)
		w.WriteHeader(http.StatusOK)
	}))
	t.Cleanup(server.Close)

	tester, err := NewRouteTester(unsafeRouteTestPolicy(t), append(NewRouteTestSources(testenv.NewLogger(t), testenv.NewMeterProvider(t)), &unmarkedLogSource{})...)
	require.NoError(t, err)
	_, err = tester.SendTest(t.Context(), dataexports.DataSourceMCPConnectionLogs, routeTestDestination(server.URL, false))
	require.ErrorContains(t, err, "lacks the test marks")

	destination := routeTestDestination(server.URL, false)
	destination.TestEventID = ""
	_, err = tester.SendTest(t.Context(), dataexports.DataSourceRiskFindings, destination)
	require.ErrorContains(t, err, "test event ID")
	require.Zero(t, calls.Load())
}

// partlyUnencodableSource is product telemetry whose metrics request cannot be encoded.
type partlyUnencodableSource struct{}

var _ RouteTestSource = (*partlyUnencodableSource)(nil)

func (s *partlyUnencodableSource) DataSource() string { return dataexports.DataSourceProductTelemetry }

func (s *partlyUnencodableSource) BuildTestRequests(context.Context, dataexports.TestDestination) ([]RouteTestRequest, error) {
	return []RouteTestRequest{
		{Signal: dataexports.SignalLogs, Message: &collectorlogsv1.ExportLogsServiceRequest{}},
		{Signal: dataexports.SignalTraces, Message: &collectortracev1.ExportTraceServiceRequest{}},
		{Signal: dataexports.SignalMetrics, Message: &collectormetricsv1.ExportMetricsServiceRequest{ResourceMetrics: []*metricsv1.ResourceMetrics{{
			ScopeMetrics: []*metricsv1.ScopeMetrics{{Metrics: []*metricsv1.Metric{{Name: "\xff"}}}},
		}}}},
	}, nil
}

func TestRouteTester_SendsNothingWhenARequestCannotBeEncoded(t *testing.T) {
	t.Parallel()

	var calls atomic.Int64
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		calls.Add(1)
		w.WriteHeader(http.StatusOK)
	}))
	t.Cleanup(server.Close)

	sources := []RouteTestSource{newToolCallLogTestSource(), newRiskFindingTestSource(), &partlyUnencodableSource{}, connectionLogStub()}
	tester, err := NewRouteTester(unsafeRouteTestPolicy(t), sources...)
	require.NoError(t, err)

	_, err = tester.SendTest(t.Context(), dataexports.DataSourceProductTelemetry, routeTestDestination(server.URL, false))
	require.ErrorContains(t, err, "encode metrics test")
	require.Zero(t, calls.Load())
}
