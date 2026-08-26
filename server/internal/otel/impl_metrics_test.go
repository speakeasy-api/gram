package otel

import (
	"bytes"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/google/uuid"
	otelv1 "github.com/speakeasy-api/gram/infra/gen/gram/otel/v1"
	"github.com/speakeasy-api/gram/infra/pkg/gcp"
	otelserver "github.com/speakeasy-api/gram/server/gen/http/otel/server"
	gen "github.com/speakeasy-api/gram/server/gen/otel"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/testenv"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
	collectormetricsv1 "go.opentelemetry.io/proto/otlp/collector/metrics/v1"
	commonv1 "go.opentelemetry.io/proto/otlp/common/v1"
	metricsv1 "go.opentelemetry.io/proto/otlp/metrics/v1"
	resourcev1 "go.opentelemetry.io/proto/otlp/resource/v1"
	"google.golang.org/protobuf/proto"
)

const (
	testMetricOrganizationID = "00000000-0000-0000-0000-000000000301"
	testMetricProjectID      = "00000000-0000-0000-0000-000000000302"
)

func TestMetricsResponseUsesOTLPSuccessStatus(t *testing.T) {
	t.Parallel()

	response := httptest.NewRecorder()
	err := otelserver.EncodeMetricsResponse(nil)(t.Context(), response, nil)

	require.NoError(t, err)
	require.Equal(t, http.StatusOK, response.Code)
}

func TestMetricsPublishesInboundWithoutEnrichment(t *testing.T) {
	t.Parallel()

	request := metricRelayTestExport()
	body, err := proto.Marshal(request)
	require.NoError(t, err)

	var published *otelv1.InboundMetric
	publisher := gcp.NewMockPublisher[*otelv1.InboundMetric]()
	publisher.On("Publish", mock.Anything, mock.Anything).Run(func(args mock.Arguments) {
		item, ok := args.Get(1).(*otelv1.InboundMetric)
		require.True(t, ok)
		published = item
	}).Return(gcp.NewSuccessPublishResult()).Once()
	service := &Service{
		logger:          testenv.NewLogger(t),
		tracer:          testenv.NewTracerProvider(t).Tracer("test"),
		auth:            nil,
		authz:           nil,
		chRepo:          nil,
		logsEnabled:     nil,
		logPublisher:    nil,
		metricPublisher: publisher,
		spanPublisher:   nil,
	}
	projectID := uuid.MustParse(testMetricProjectID)
	ctx := contextvalues.SetAuthContext(t.Context(), &contextvalues.AuthContext{
		ActiveOrganizationID: testMetricOrganizationID,
		ProjectID:            &projectID,
	})

	err = service.Metrics(ctx, &gen.MetricsPayload{
		ApikeyToken:      nil,
		ProjectSlugInput: nil,
		ContentEncoding:  nil,
	}, io.NopCloser(bytes.NewReader(body)))

	require.NoError(t, err)
	publisher.AssertExpectations(t)
	require.NotNil(t, published)
	require.Equal(t, "request.duration", published.GetName())
	require.Equal(t, "producer", published.GetResource().GetAttributes()[0].GetValue().GetStringValue())
	require.Equal(t, "producer.scope", published.GetScope().GetName())
	require.Equal(t, ProvenanceSource, published.GetProvenance().GetSource())
	require.Equal(t, testMetricOrganizationID, published.GetProvenance().GetOrganizationId())
	require.Equal(t, testMetricProjectID, published.GetProvenance().GetProjectId())

	normalized, err := metricFromInbound(published)
	require.NoError(t, err)
	rebuilt, err := newMetricRelayExportRequest([]*otelv1.Metric{normalized})
	require.NoError(t, err)
	require.True(t, proto.Equal(request, rebuilt), "pipeline must preserve producer metric identity and attributes")
}

func TestMetricsRejectsInvalidExportBeforePublishing(t *testing.T) {
	t.Parallel()

	request := metricRelayTestExport()
	request.ResourceMetrics[0].ScopeMetrics[0].Metrics = append(
		request.ResourceMetrics[0].ScopeMetrics[0].Metrics,
		&metricsv1.Metric{Name: "", Data: &metricsv1.Metric_Gauge{Gauge: &metricsv1.Gauge{DataPoints: nil}}},
	)
	body, err := proto.Marshal(request)
	require.NoError(t, err)

	publisher := gcp.NewMockPublisher[*otelv1.InboundMetric]()
	publisher.On("Publish", mock.Anything, mock.Anything).Return(gcp.NewSuccessPublishResult()).Maybe()
	service := &Service{
		logger:          testenv.NewLogger(t),
		tracer:          testenv.NewTracerProvider(t).Tracer("test"),
		auth:            nil,
		authz:           nil,
		chRepo:          nil,
		logsEnabled:     nil,
		logPublisher:    nil,
		metricPublisher: publisher,
		spanPublisher:   nil,
	}
	projectID := uuid.MustParse(testMetricProjectID)
	ctx := contextvalues.SetAuthContext(t.Context(), &contextvalues.AuthContext{
		ActiveOrganizationID: testMetricOrganizationID,
		ProjectID:            &projectID,
	})

	err = service.Metrics(ctx, &gen.MetricsPayload{
		ApikeyToken:      nil,
		ProjectSlugInput: nil,
		ContentEncoding:  nil,
	}, io.NopCloser(bytes.NewReader(body)))

	require.ErrorContains(t, err, "invalid OTLP metric export")
	publisher.AssertNotCalled(t, "Publish", mock.Anything, mock.Anything)
}

func metricRelayTestExport() *collectormetricsv1.ExportMetricsServiceRequest {
	return &collectormetricsv1.ExportMetricsServiceRequest{
		ResourceMetrics: []*metricsv1.ResourceMetrics{{
			Resource: &resourcev1.Resource{Attributes: []*commonv1.KeyValue{{
				Key: "service.name",
				Value: &commonv1.AnyValue{Value: &commonv1.AnyValue_StringValue{
					StringValue: "producer",
				}},
			}}},
			SchemaUrl: "resource-schema",
			ScopeMetrics: []*metricsv1.ScopeMetrics{{
				Scope:     &commonv1.InstrumentationScope{Name: "producer.scope", Version: "1.2.3"},
				SchemaUrl: "scope-schema",
				Metrics: []*metricsv1.Metric{{
					Name:        "request.duration",
					Description: "request latency",
					Unit:        "s",
					Metadata: []*commonv1.KeyValue{{
						Key: "prometheus.type",
						Value: &commonv1.AnyValue{Value: &commonv1.AnyValue_StringValue{
							StringValue: "histogram",
						}},
					}},
					Data: &metricsv1.Metric_Histogram{Histogram: &metricsv1.Histogram{
						AggregationTemporality: metricsv1.AggregationTemporality_AGGREGATION_TEMPORALITY_DELTA,
						DataPoints: []*metricsv1.HistogramDataPoint{{
							Attributes: []*commonv1.KeyValue{{
								Key: "http.request.method",
								Value: &commonv1.AnyValue{Value: &commonv1.AnyValue_StringValue{
									StringValue: "GET",
								}},
							}},
							StartTimeUnixNano: 100,
							TimeUnixNano:      200,
							Count:             2,
							Sum:               new(0.3),
							BucketCounts:      []uint64{1, 1},
							ExplicitBounds:    []float64{0.25},
							Min:               new(0.1),
							Max:               new(0.2),
							Exemplars:         nil,
							Flags:             0,
						}},
					}},
				}},
			}},
		}},
	}
}
