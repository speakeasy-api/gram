package otel

import (
	"io"
	"net/http"
	"net/http/httptest"
	"slices"
	"sync"
	"testing"
	"time"

	otelv1 "github.com/speakeasy-api/gram/infra/gen/gram/otel/v1"
	"go.opentelemetry.io/otel/metric"
	collectormetricsv1 "go.opentelemetry.io/proto/otlp/collector/metrics/v1"
	"google.golang.org/protobuf/proto"

	"github.com/speakeasy-api/gram/server/internal/guardian"
	"github.com/speakeasy-api/gram/server/internal/testenv"
	"github.com/stretchr/testify/require"
)

type capturedMetricRelayRequest struct {
	path        string
	contentType string
	request     *collectormetricsv1.ExportMetricsServiceRequest
	err         error
}

type metricRelayRequestCapture struct {
	mu       sync.Mutex
	requests []capturedMetricRelayRequest
}

func (c *metricRelayRequestCapture) handler(w http.ResponseWriter, r *http.Request) {
	body, err := io.ReadAll(r.Body)
	request := new(collectormetricsv1.ExportMetricsServiceRequest)
	if err == nil {
		err = proto.Unmarshal(body, request)
	}

	c.mu.Lock()
	c.requests = append(c.requests, capturedMetricRelayRequest{
		path:        r.URL.Path,
		contentType: r.Header.Get("Content-Type"),
		request:     request,
		err:         err,
	})
	c.mu.Unlock()
	w.WriteHeader(http.StatusOK)
}

func (c *metricRelayRequestCapture) snapshot() []capturedMetricRelayRequest {
	c.mu.Lock()
	defer c.mu.Unlock()
	return slices.Clone(c.requests)
}

func TestMetricRelayHandlerPreservesMetricsWithoutMixingProvenance(t *testing.T) {
	t.Parallel()

	capture := &metricRelayRequestCapture{mu: sync.Mutex{}, requests: nil}
	server := httptest.NewServer(http.HandlerFunc(capture.handler))
	t.Cleanup(server.Close)
	handler := newMetricRelayTestHandler(t, testenv.NewMeterProvider(t))
	cacheMetricRelayTestDestination(t, handler, testMetricOrganizationID, server.URL)

	messages, failures := metricRelayTestMessages(
		relayTestMetric("requests", testMetricOrganizationID, testMetricProjectID),
		relayTestMetric("latency", testMetricOrganizationID, testMetricProjectID),
		relayTestMetric("tokens", testMetricOrganizationID, testLogOtherProjectID),
	)
	require.NoError(t, handler.handleBatch(t.Context(), messages))
	for _, failure := range failures {
		require.NoError(t, failure)
	}

	requests := capture.snapshot()
	require.Len(t, requests, 2)
	var names []string
	for _, captured := range requests {
		require.NoError(t, captured.err)
		require.Equal(t, "/v1/metrics", captured.path)
		require.Equal(t, "application/x-protobuf", captured.contentType)
		for _, resourceMetrics := range captured.request.GetResourceMetrics() {
			require.Equal(t, "https://opentelemetry.io/schemas/1.27.0", resourceMetrics.GetSchemaUrl())
			require.Equal(t, "service.name", resourceMetrics.GetResource().GetAttributes()[0].GetKey())
			require.Equal(t, "metrics-producer", resourceMetrics.GetResource().GetAttributes()[0].GetValue().GetStringValue())
			require.Equal(t, uint32(2), resourceMetrics.GetResource().GetDroppedAttributesCount())
			for _, scopeMetrics := range resourceMetrics.GetScopeMetrics() {
				require.Equal(t, "https://opentelemetry.io/schemas/1.28.0", scopeMetrics.GetSchemaUrl())
				require.Equal(t, "producer.metrics", scopeMetrics.GetScope().GetName())
				require.Equal(t, "1.2.3", scopeMetrics.GetScope().GetVersion())
				require.Equal(t, "scope.attribute", scopeMetrics.GetScope().GetAttributes()[0].GetKey())
				require.Equal(t, "scope-value", scopeMetrics.GetScope().GetAttributes()[0].GetValue().GetStringValue())
				require.Equal(t, uint32(3), scopeMetrics.GetScope().GetDroppedAttributesCount())
				for _, item := range scopeMetrics.GetMetrics() {
					names = append(names, item.GetName())
					require.Equal(t, "model", item.GetGauge().GetDataPoints()[0].GetAttributes()[0].GetKey())
					require.Empty(t, item.ProtoReflect().GetUnknown(), "Gram provenance must not reach the customer destination")
				}
			}
		}
	}
	slices.Sort(names)
	require.Equal(t, []string{"latency", "requests", "tokens"}, names)
}

func metricRelayTestMessages(items ...*otelv1.Metric) ([]metricRelayMessage, []error) {
	messages := make([]metricRelayMessage, len(items))
	failures := make([]error, len(items))
	for i, item := range items {
		index := i
		messages[i] = metricRelayMessage{
			metric: item,
			fail: func(err error) {
				failures[index] = err
			},
		}
	}
	return messages, failures
}

func newMetricRelayTestHandler(t *testing.T, meterProvider metric.MeterProvider) *MetricRelayHandler {
	t.Helper()
	policy, err := guardian.NewUnsafePolicy(testenv.NewTracerProvider(t), nil)
	require.NoError(t, err)
	return NewMetricRelayHandler(
		testenv.NewLogger(t),
		meterProvider,
		nil,
		testenv.NewEncryptionClient(t),
		policy,
	)
}

func cacheMetricRelayTestDestination(t *testing.T, handler *MetricRelayHandler, organizationID, baseURL string) {
	t.Helper()
	destination, err := handler.relay.newDestination(organizationID, baseURL, nil)
	require.NoError(t, err)
	handler.relay.destinationCache[organizationID] = cachedRelayDestination{
		destination: destination,
		expiresAt:   time.Now().Add(time.Hour),
	}
}

func relayTestMetric(name, organizationID, projectID string) *otelv1.Metric {
	resourceSchemaURL := "https://opentelemetry.io/schemas/1.27.0"
	scopeSchemaURL := "https://opentelemetry.io/schemas/1.28.0"
	return (&otelv1.Metric_builder{
		Name: &name,
		Gauge: (&otelv1.Metric_Gauge_builder{
			DataPoints: []*otelv1.Metric_NumberDataPoint{
				(&otelv1.Metric_NumberDataPoint_builder{
					Attributes: []*otelv1.Metric_KeyValue{
						(&otelv1.Metric_KeyValue_builder{
							Key: new("model"),
							Value: (&otelv1.Metric_AnyValue_builder{
								StringValue: new("test-model"),
							}).Build(),
						}).Build(),
					},
					TimeUnixNano: new(uint64(200)),
					AsInt:        new(int64(1)),
				}).Build(),
			},
		}).Build(),
		Resource: (&otelv1.Metric_Resource_builder{
			Attributes: []*otelv1.Metric_KeyValue{
				(&otelv1.Metric_KeyValue_builder{
					Key: new("service.name"),
					Value: (&otelv1.Metric_AnyValue_builder{
						StringValue: new("metrics-producer"),
					}).Build(),
				}).Build(),
			},
			DroppedAttributesCount: new(uint32(2)),
		}).Build(),
		ResourceSchemaUrl: &resourceSchemaURL,
		Scope: (&otelv1.Metric_InstrumentationScope_builder{
			Name:    new("producer.metrics"),
			Version: new("1.2.3"),
			Attributes: []*otelv1.Metric_KeyValue{
				(&otelv1.Metric_KeyValue_builder{
					Key: new("scope.attribute"),
					Value: (&otelv1.Metric_AnyValue_builder{
						StringValue: new("scope-value"),
					}).Build(),
				}).Build(),
			},
			DroppedAttributesCount: new(uint32(3)),
		}).Build(),
		ScopeSchemaUrl: &scopeSchemaURL,
		Provenance: (&otelv1.Metric_Provenance_builder{
			Source:         new("test"),
			OrganizationId: &organizationID,
			ProjectId:      &projectID,
		}).Build(),
	}).Build()
}
