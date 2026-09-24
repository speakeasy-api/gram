package otel

import (
	"io"
	"net/http"
	"net/http/httptest"
	"slices"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"

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
	bodySize    int
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
		bodySize:    len(body),
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

func TestGroupMetricsByProvenanceRejectsInvalidProjectIDs(t *testing.T) {
	t.Parallel()

	messages, _ := metricRelayTestMessages(
		nil,
		(&otelv1.Metric_builder{}).Build(),
		relayTestMetric("empty-org", "", testMetricProjectID),
		relayTestMetric("missing-project", testMetricOrganizationID, ""),
		relayTestMetric("malformed-project", testMetricOrganizationID, "malformed"),
		relayTestMetric("valid", testMetricOrganizationID, testMetricProjectID),
	)

	groups, invalid := groupMetricsByProvenance(messages)
	require.Equal(t, 5, invalid)
	require.Len(t, groups, 1)
	require.Equal(t, uuid.MustParse(testMetricProjectID), groups[0].key.projectID)
}

func TestMetricRelayHandlerPreservesMetricsWithoutMixingProvenance(t *testing.T) {
	t.Parallel()

	captureA := &metricRelayRequestCapture{mu: sync.Mutex{}, requests: nil}
	serverA := httptest.NewServer(http.HandlerFunc(captureA.handler))
	t.Cleanup(serverA.Close)
	captureB := &metricRelayRequestCapture{mu: sync.Mutex{}, requests: nil}
	serverB := httptest.NewServer(http.HandlerFunc(captureB.handler))
	t.Cleanup(serverB.Close)
	handler := newMetricRelayTestHandler(t, testenv.NewMeterProvider(t))
	cacheMetricRelayTestDestination(t, handler, testMetricOrganizationID, testMetricProjectID, serverA.URL, true)
	cacheMetricRelayTestDestination(t, handler, testMetricOrganizationID, testLogOtherProjectID, serverB.URL, true)

	messages, failures := metricRelayTestMessages(
		relayTestMetric("requests", testMetricOrganizationID, testMetricProjectID),
		relayTestMetric("latency", testMetricOrganizationID, testMetricProjectID),
		relayTestMetric("tokens", testMetricOrganizationID, testLogOtherProjectID),
	)
	require.NoError(t, handler.handleBatch(t.Context(), messages))
	for _, failure := range failures {
		require.NoError(t, failure)
	}

	requestsA := captureA.snapshot()
	require.Len(t, requestsA, 1)
	require.NoError(t, requestsA[0].err)
	require.Equal(t, "/v1/metrics", requestsA[0].path)
	require.Equal(t, "application/x-protobuf", requestsA[0].contentType)
	namesA := relayRequestMetricNames(requestsA[0].request)
	slices.Sort(namesA)
	require.Equal(t, []string{"latency", "requests"}, namesA)

	requestsB := captureB.snapshot()
	require.Len(t, requestsB, 1)
	require.NoError(t, requestsB[0].err)
	require.Equal(t, "/v1/metrics", requestsB[0].path)
	require.Equal(t, "application/x-protobuf", requestsB[0].contentType)
	require.Equal(t, []string{"tokens"}, relayRequestMetricNames(requestsB[0].request))

	for _, captured := range []capturedMetricRelayRequest{requestsA[0], requestsB[0]} {
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
					require.Equal(t, "model", item.GetGauge().GetDataPoints()[0].GetAttributes()[0].GetKey())
					require.Empty(t, item.ProtoReflect().GetUnknown(), "Gram provenance must not reach the customer destination")
				}
			}
		}
	}
}

func TestMetricRelayHandlerLimitsDestinationExportsTo512KiB(t *testing.T) {
	t.Parallel()

	capture := &metricRelayRequestCapture{mu: sync.Mutex{}, requests: nil}
	server := httptest.NewServer(http.HandlerFunc(capture.handler))
	t.Cleanup(server.Close)
	handler := newMetricRelayTestHandler(t, testenv.NewMeterProvider(t))
	cacheMetricRelayTestDestination(t, handler, testMetricOrganizationID, testMetricProjectID, server.URL, true)

	items := []*otelv1.Metric{
		relayTestMetric("one", testMetricOrganizationID, testMetricProjectID),
		relayTestMetric("two", testMetricOrganizationID, testMetricProjectID),
		relayTestMetric("three", testMetricOrganizationID, testMetricProjectID),
	}
	for _, item := range items {
		item.SetMetadata([]*otelv1.Metric_KeyValue{
			(&otelv1.Metric_KeyValue_builder{
				Key: new("padding"),
				Value: (&otelv1.Metric_AnyValue_builder{
					BytesValue: make([]byte, maxOTLPMetricBytes/2-1024),
				}).Build(),
			}).Build(),
		})
	}

	messages, failures := metricRelayTestMessages(items...)
	require.NoError(t, handler.handleBatch(t.Context(), messages))
	for _, failure := range failures {
		require.NoError(t, failure)
	}

	require.Equal(t, 512*1024, maxMetricRelayExportBytes)
	requests := capture.snapshot()
	require.Len(t, requests, 2)
	for _, captured := range requests {
		require.NoError(t, captured.err)
		require.Positive(t, captured.bodySize)
		require.LessOrEqual(t, captured.bodySize, maxMetricRelayExportBytes)
	}
}
func TestMetricRelayExportAppliesSensitiveDataPolicyWithoutMutatingSource(t *testing.T) {
	t.Parallel()

	item := relayTestMetric("redacted", testMetricOrganizationID, testMetricProjectID)
	item.GetGauge().GetDataPoints()[0].SetAttributes([]*otelv1.Metric_KeyValue{
		(&otelv1.Metric_KeyValue_builder{
			Key: new("gen_ai.tool.call.arguments"),
			Value: (&otelv1.Metric_AnyValue_builder{
				StringValue: new("sensitive"),
			}).Build(),
		}).Build(),
		(&otelv1.Metric_KeyValue_builder{
			Key: new("model"),
			Value: (&otelv1.Metric_AnyValue_builder{
				StringValue: new("preserved"),
			}).Build(),
		}).Build(),
	})
	before := proto.Clone(item)

	excluded, err := newMetricRelayExportRequest([]*otelv1.Metric{item}, false)
	require.NoError(t, err)
	require.True(t, proto.Equal(before, item))
	excludedAttributes := excluded.GetResourceMetrics()[0].GetScopeMetrics()[0].GetMetrics()[0].GetGauge().GetDataPoints()[0].GetAttributes()
	require.Len(t, excludedAttributes, 2)
	require.Equal(t, "gen_ai.tool.call.arguments", excludedAttributes[0].GetKey())
	require.Equal(t, redactedSensitiveDataValue, excludedAttributes[0].GetValue().GetStringValue())
	require.Equal(t, "model", excludedAttributes[1].GetKey())
	require.Equal(t, "preserved", excludedAttributes[1].GetValue().GetStringValue())

	included, err := newMetricRelayExportRequest([]*otelv1.Metric{item}, true)
	require.NoError(t, err)
	includedAttributes := included.GetResourceMetrics()[0].GetScopeMetrics()[0].GetMetrics()[0].GetGauge().GetDataPoints()[0].GetAttributes()
	require.Len(t, includedAttributes, 2)
	require.Equal(t, "sensitive", includedAttributes[0].GetValue().GetStringValue())
	require.Equal(t, "preserved", includedAttributes[1].GetValue().GetStringValue())
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

func cacheMetricRelayTestDestination(
	t *testing.T,
	handler *MetricRelayHandler,
	organizationID string,
	projectID string,
	baseURL string,
	includeSensitiveData bool,
) {
	t.Helper()
	key := relayTestRouteKey(organizationID, projectID)
	destination, err := handler.relay.newDestination(key, baseURL, nil, includeSensitiveData)
	require.NoError(t, err)
	handler.relay.destinationCache[key] = cachedRelayDestination{
		destination: destination,
		expiresAt:   time.Now().Add(time.Hour),
	}
}

func relayRequestMetricNames(request *collectormetricsv1.ExportMetricsServiceRequest) []string {
	var names []string
	for _, resourceMetrics := range request.GetResourceMetrics() {
		for _, scopeMetrics := range resourceMetrics.GetScopeMetrics() {
			for _, item := range scopeMetrics.GetMetrics() {
				names = append(names, item.GetName())
			}
		}
	}
	return names
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
