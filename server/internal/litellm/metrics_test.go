package litellm

import (
	"bytes"
	"context"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
	collectorv1 "go.opentelemetry.io/proto/otlp/collector/metrics/v1"
	commonv1 "go.opentelemetry.io/proto/otlp/common/v1"
	metricsv1 "go.opentelemetry.io/proto/otlp/metrics/v1"
	resourcev1 "go.opentelemetry.io/proto/otlp/resource/v1"

	"github.com/speakeasy-api/gram/server/internal/attr"
	"github.com/speakeasy-api/gram/server/internal/telemetry"
	"github.com/speakeasy-api/gram/server/internal/testenv"
)

func TestMetricHTTPAcceptsLiteLLMJSONAndProtobuf(t *testing.T) {
	t.Parallel()

	persisted := make(chan []telemetry.LogParams, 4)
	authCtx := testAuthContext()
	authorizer := &traceTestAuthorizer{authCtx: authCtx, key: "valid-key", project: "project-test", mu: sync.Mutex{}, schemes: nil}
	service, _ := newTraceTestService(t, authorizer, testenv.NewMeterProvider(t), func(_ context.Context, params []telemetry.LogParams) error {
		persisted <- params
		return nil
	})
	instanceID := uuid.New()
	service.instances.Remember(authCtx.ActiveOrganizationID, *authCtx.ProjectID, authCtx.APIKeyID, instanceID)
	handler := service.metricHTTPHandler()
	jsonBody := testenv.ReadFixture(t, contractFixtureDir+"otlp-metrics.json")
	protobufBody := testenv.ReadFixture(t, contractFixtureDir+"otlp-metrics.pb")

	responses := []*httptest.ResponseRecorder{
		serveMetricRequest(t, handler, jsonBody, "application/json", "", "valid-key", "project-test"),
		serveMetricRequest(t, handler, gzipBody(t, jsonBody), "application/json", "gzip", "valid-key", "project-test"),
		serveMetricRequest(t, handler, protobufBody, "application/x-protobuf", "", "valid-key", "project-test"),
		serveMetricRequest(t, handler, gzipBody(t, protobufBody), "application/protobuf", "gzip", "valid-key", "project-test"),
	}
	for _, response := range responses {
		require.Equal(t, http.StatusAccepted, response.Code)
	}
	for range responses {
		params := <-persisted
		require.Len(t, params, len(litellmMetricNames))
		for _, param := range params {
			require.Equal(t, authCtx.APIKeyID, param.Attributes[attr.APIKeyIDKey])
			require.Equal(t, instanceID.String(), param.Attributes[attr.LiteLLMInstanceIDKey])
		}
	}
}

func TestMetricLogParamsUsesAllowlistAndMetricOnlyFields(t *testing.T) {
	t.Parallel()

	service, _ := newTraceTestService(t, fixedAuthorizer{authCtx: testAuthContext()}, testenv.NewMeterProvider(t), func(context.Context, []telemetry.LogParams) error { return nil })
	request := liteLLMMetricFixture()
	params := service.metricLogParams(t.Context(), request, "org-test", "project-test")
	require.Len(t, params, len(litellmMetricNames))

	var row telemetry.LogParams
	found := false
	for _, candidate := range params {
		if candidate.Attributes[attr.MetricNameKey] == "gen_ai.client.operation.duration" {
			row = candidate
			found = true
			break
		}
	}
	require.True(t, found)
	require.Equal(t, litellmOTLPMetricsURN, row.ToolInfo.URN)
	require.Equal(t, "litellm", row.Attributes[attr.HookSourceKey])
	require.Equal(t, litellmMetricEventURN, row.Attributes[attr.EventURNKey])
	require.Equal(t, "gen_ai.client.operation.duration", row.Attributes[attr.MetricNameKey])
	require.Equal(t, "chat", row.Attributes[attr.GenAIOperationNameKey])
	require.Equal(t, "gpt-4o-mini", row.Attributes[attr.GenAIRequestModelKey])
	require.NotContains(t, row.Attributes, attr.GenAIUsageInputTokensKey)
	require.NotContains(t, row.Attributes, attr.GenAIUsageOutputTokensKey)
	require.NotContains(t, row.Attributes, attr.GenAIUsageCostKey)
	require.NotContains(t, row.Attributes, attr.LiteLLMCallIDKey)
	require.NotContains(t, row.Attributes, attr.Key("metadata.requester_ip_address"))
	require.NotContains(t, row.Attributes, attr.Key("novel.attribute"))
	require.Equal(t, "s", row.Attributes[attr.Key("metric.unit")])
	require.Equal(t, "AGGREGATION_TEMPORALITY_CUMULATIVE", row.Attributes[attr.Key("metric.aggregation_temporality")])
	require.EqualValues(t, 100, row.Attributes[attr.Key("metric.start_time_unix_nano")])
	require.EqualValues(t, 200, row.Attributes[attr.Key("metric.time_unix_nano")])
	require.EqualValues(t, 2, row.Attributes[attr.Key("metric.count")])
	require.Equal(t, []uint64{1, 1}, row.Attributes[attr.Key("metric.bucket_counts")])
	require.Equal(t, []float64{0.5}, row.Attributes[attr.Key("metric.explicit_bounds")])
	resourceAttrs := service.sanitizeOTLPMetricResourceAttributes(t.Context(), []otlpKeyValue{
		{Key: "service.name", Value: otlpAnyValue{StringValue: new("litellm")}},
		{Key: "service.instance.id", Value: otlpAnyValue{StringValue: new("high-cardinality-instance")}},
	})
	require.Equal(t, "litellm", resourceAttrs[attr.ServiceNameKey])
	require.NotContains(t, resourceAttrs, attr.ServiceInstanceIDKey)
}

func TestMetricExportIgnoresUnknownAndNonHistogramInstruments(t *testing.T) {
	t.Parallel()

	service, _ := newTraceTestService(t, fixedAuthorizer{authCtx: testAuthContext()}, testenv.NewMeterProvider(t), func(context.Context, []telemetry.LogParams) error { return nil })
	request := liteLLMMetricFixture()
	request.ResourceMetrics[0].ScopeMetrics[0].Metrics = append(request.ResourceMetrics[0].ScopeMetrics[0].Metrics,
		&metricsv1.Metric{Name: "custom.metric", Unit: "1", Description: "", Data: &metricsv1.Metric_Histogram{Histogram: histogramPoint(1)}},
		&metricsv1.Metric{Name: "gen_ai.client.token.cost", Unit: "USD", Description: "", Data: &metricsv1.Metric_Sum{Sum: &metricsv1.Sum{DataPoints: nil, AggregationTemporality: metricsv1.AggregationTemporality_AGGREGATION_TEMPORALITY_CUMULATIVE, IsMonotonic: false}}},
	)

	require.Len(t, service.metricLogParams(t.Context(), request, "org-test", "project-test"), len(litellmMetricNames))
	require.Empty(t, service.metricLogParams(t.Context(), &collectorv1.ExportMetricsServiceRequest{ResourceMetrics: nil}, "org-test", "project-test"))
}

func TestMetricExportEnforcesCollectionLimits(t *testing.T) {
	t.Parallel()

	request := liteLLMMetricFixture()
	request.ResourceMetrics[0].ScopeMetrics[0].Metrics[0].GetHistogram().DataPoints[0].BucketCounts = make([]uint64, maxOTLPHistogramBuckets+1)
	require.ErrorIs(t, validateMetricExport(request), errTooManyOTLPMetrics)
}

func TestMetricHTTPAuthenticatesBeforeReadingBody(t *testing.T) {
	t.Parallel()

	service, _ := newTraceTestService(t, &traceTestAuthorizer{authCtx: testAuthContext(), key: "valid-key", project: "project-test", mu: sync.Mutex{}, schemes: nil}, testenv.NewMeterProvider(t), func(context.Context, []telemetry.LogParams) error { return nil })
	reader := &trackingReader{}
	req := httptest.NewRequest(http.MethodPost, "/rpc/hooks.otel/v1/metrics", reader)
	req.Header.Set("Content-Type", "application/json")
	recorder := httptest.NewRecorder()
	service.metricHTTPHandler().ServeHTTP(recorder, req)
	require.Equal(t, http.StatusUnauthorized, recorder.Code)
	require.Zero(t, reader.reads)
}

func TestMetricProcessorCannotConsumeTraceQueueCapacity(t *testing.T) {
	t.Parallel()

	batch := []telemetry.LogParams{{
		Timestamp: time.Unix(0, 1),
		ToolInfo: telemetry.ToolInfo{
			ID: "", URN: litellmOTLPMetricsURN, Name: "litellm", ProjectID: "project-test", DeploymentID: "", FunctionID: nil, OrganizationID: "org-test",
		},
		UserInfo:   telemetry.UserInfoByID(""),
		Attributes: map[attr.Key]any{},
	}}
	metricEntered := make(chan struct{}, traceProcessorWorkers)
	releaseMetrics := make(chan struct{})
	metricProcessor := newMetricProcessor(testenv.NewLogger(t), testenv.NewMeterProvider(t), func(context.Context, []telemetry.LogParams) error {
		select {
		case metricEntered <- struct{}{}:
			<-releaseMetrics
		case <-releaseMetrics:
		}
		return nil
	}, traceProcessorWorkers, traceProcessorQueueSize)
	metricProcessor.Start(t.Context())
	for range traceProcessorWorkers {
		require.True(t, metricProcessor.Enqueue(t.Context(), batch))
	}
	for range traceProcessorWorkers {
		<-metricEntered
	}
	for range traceProcessorQueueSize {
		require.True(t, metricProcessor.Enqueue(t.Context(), batch))
	}
	require.False(t, metricProcessor.Enqueue(t.Context(), batch))

	tracePersisted := make(chan struct{}, 1)
	traceProcessor := newTraceProcessor(testenv.NewLogger(t), testenv.NewMeterProvider(t), func(context.Context, []telemetry.LogParams) error {
		tracePersisted <- struct{}{}
		return nil
	}, 1, 1)
	traceProcessor.Start(t.Context())
	require.True(t, traceProcessor.Enqueue(t.Context(), batch))
	require.Eventually(t, func() bool { return len(tracePersisted) == 1 }, time.Second, 10*time.Millisecond)
	require.NoError(t, traceProcessor.Shutdown(t.Context()))
	close(releaseMetrics)
	require.NoError(t, metricProcessor.Shutdown(t.Context()))
}

func serveMetricRequest(t *testing.T, handler http.Handler, body []byte, contentType, contentEncoding, key, project string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(http.MethodPost, "/rpc/hooks.otel/v1/metrics", bytes.NewReader(body))
	if contentType != "" {
		req.Header.Set("Content-Type", contentType)
	}
	if contentEncoding != "" {
		req.Header.Set("Content-Encoding", contentEncoding)
	}
	if key != "" {
		req.Header.Set("Gram-Key", key)
	}
	if project != "" {
		req.Header.Set("Gram-Project", project)
	}
	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, req)
	return recorder
}

func liteLLMMetricFixture() *collectorv1.ExportMetricsServiceRequest {
	metrics := make([]*metricsv1.Metric, 0, len(litellmMetricNames))
	units := map[string]string{
		"gen_ai.client.operation.duration":             "s",
		"gen_ai.client.token.usage":                    "{token}",
		"gen_ai.client.token.cost":                     "USD",
		"gen_ai.client.response.time_to_first_token":   "s",
		"gen_ai.client.response.time_per_output_token": "s",
		"gen_ai.client.response.duration":              "s",
	}
	for name := range litellmMetricNames {
		metrics = append(metrics, &metricsv1.Metric{Name: name, Unit: units[name], Description: "", Data: &metricsv1.Metric_Histogram{Histogram: histogramPoint(1.25)}})
	}
	return &collectorv1.ExportMetricsServiceRequest{ResourceMetrics: []*metricsv1.ResourceMetrics{{
		Resource: &resourcev1.Resource{Attributes: []*commonv1.KeyValue{stringKeyValue("service.name", "litellm")}, DroppedAttributesCount: 0},
		ScopeMetrics: []*metricsv1.ScopeMetrics{{
			Scope:     &commonv1.InstrumentationScope{Name: "litellm", Version: "1.94.0", Attributes: nil, DroppedAttributesCount: 0},
			Metrics:   metrics,
			SchemaUrl: "",
		}},
		SchemaUrl: "",
	}}}
}

func histogramPoint(sum float64) *metricsv1.Histogram {
	minValue, maxValue := 0.25, 1.0
	return &metricsv1.Histogram{
		DataPoints: []*metricsv1.HistogramDataPoint{{
			Attributes: []*commonv1.KeyValue{
				stringKeyValue("gen_ai.operation.name", "chat"),
				stringKeyValue("gen_ai.system", "openai"),
				stringKeyValue("gen_ai.request.model", "gpt-4o-mini"),
				stringKeyValue("gen_ai.framework", "litellm"),
				stringKeyValue("litellm.call_id", "must-be-dropped"),
				stringKeyValue("metadata.requester_ip_address", "192.0.2.1"),
				stringKeyValue("novel.attribute", "must-also-be-dropped"),
			},
			StartTimeUnixNano: 100,
			TimeUnixNano:      200,
			Count:             2,
			Sum:               &sum,
			BucketCounts:      []uint64{1, 1},
			ExplicitBounds:    []float64{0.5},
			Exemplars:         nil,
			Flags:             0,
			Min:               &minValue,
			Max:               &maxValue,
		}},
		AggregationTemporality: metricsv1.AggregationTemporality_AGGREGATION_TEMPORALITY_CUMULATIVE,
	}
}

func stringKeyValue(key, value string) *commonv1.KeyValue {
	return &commonv1.KeyValue{Key: key, Value: &commonv1.AnyValue{Value: &commonv1.AnyValue_StringValue{StringValue: value}}}
}
