package otel

import (
	"context"
	"testing"

	otelv1 "github.com/speakeasy-api/gram/infra/gen/gram/otel/v1"
	"github.com/speakeasy-api/gram/infra/pkg/gcp"
	oteldialect "github.com/speakeasy-api/gram/server/internal/otel/dialect"
	"github.com/speakeasy-api/gram/server/internal/testenv"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
	"go.opentelemetry.io/otel/attribute"
)

func TestMetricTransformHandlerNoEnrichersPreservesProducerAttributes(t *testing.T) {
	t.Parallel()

	inbound := transformTestInboundMetric()
	var published *otelv1.Metric
	publisher := gcp.NewMockPublisher[*otelv1.Metric]()
	publisher.On("Publish", mock.Anything, mock.Anything).Run(func(args mock.Arguments) {
		item, ok := args.Get(1).(*otelv1.Metric)
		require.True(t, ok)
		published = item
	}).Return(gcp.NewSuccessPublishResult()).Once()
	handler := NewMetricTransformHandler(testenv.NewLogger(t), testenv.NewMeterProvider(t), publisher)

	err := handler.Handle(t.Context(), inbound, gcp.MessageMetadata{})

	require.NoError(t, err)
	publisher.AssertExpectations(t)
	require.NotNil(t, published)
	require.Equal(t, "requests", published.GetName())
	require.Len(t, published.GetResource().GetAttributes(), 1)
	require.Equal(t, "service.name", published.GetResource().GetAttributes()[0].GetKey())
	require.Len(t, published.GetGauge().GetDataPoints()[0].GetAttributes(), 1)
	require.Equal(t, "http.request.method", published.GetGauge().GetDataPoints()[0].GetAttributes()[0].GetKey())
	require.Equal(t, testMetricOrganizationID, published.GetProvenance().GetOrganizationId())
	require.Equal(t, testMetricProjectID, published.GetProvenance().GetProjectId())
}

func TestMetricTransformHandlerAppliesOnlyResourceEnrichments(t *testing.T) {
	t.Parallel()

	inbound := transformTestInboundMetric()
	var published *otelv1.Metric
	publisher := gcp.NewMockPublisher[*otelv1.Metric]()
	publisher.On("Publish", mock.Anything, mock.Anything).Run(func(args mock.Arguments) {
		item, ok := args.Get(1).(*otelv1.Metric)
		require.True(t, ok)
		published = item
	}).Return(gcp.NewSuccessPublishResult()).Once()
	handler := NewMetricTransformHandler(testenv.NewLogger(t), testenv.NewMeterProvider(t), publisher)
	handler.enrichers = []MetricEnricher{stubMetricEnricher{
		name: "bounded-resource",
		enrich: func(context.Context, *otelv1.InboundMetric, oteldialect.MetricDialect) ([]attribute.KeyValue, error) {
			return []attribute.KeyValue{attribute.String("deployment.environment.name", "test")}, nil
		},
	}}

	err := handler.Handle(t.Context(), inbound, gcp.MessageMetadata{})

	require.NoError(t, err)
	publisher.AssertExpectations(t)
	require.Len(t, published.GetResource().GetAttributes(), 2)
	require.Equal(t, "deployment.environment.name", published.GetResource().GetAttributes()[1].GetKey())
	require.Len(t, published.GetGauge().GetDataPoints()[0].GetAttributes(), 1, "resource enrichment must not create a new data point dimension")
}

func TestMetricTransformHandlerPassesSelectedDialectToEnrichers(t *testing.T) {
	t.Parallel()

	inbound := transformTestInboundMetric()
	inbound.SetName("claude_code.token.usage")
	point := inbound.GetGauge().GetDataPoints()[0]
	attributes := append(point.GetAttributes(), (&otelv1.InboundMetric_KeyValue_builder{
		Key:   new("session.id"),
		Value: (&otelv1.InboundMetric_AnyValue_builder{StringValue: new("session-id")}).Build(),
	}).Build())
	point.SetAttributes(attributes)

	publisher := gcp.NewMockPublisher[*otelv1.Metric]()
	publisher.On("Publish", mock.Anything, mock.Anything).Return(gcp.NewSuccessPublishResult()).Once()
	handler := NewMetricTransformHandler(testenv.NewLogger(t), testenv.NewMeterProvider(t), publisher)
	var dialectKey, dialectValue string
	var dialectErr error
	handler.enrichers = []MetricEnricher{stubMetricEnricher{
		name: "capture-dialect",
		enrich: func(_ context.Context, _ *otelv1.InboundMetric, metricDialect oteldialect.MetricDialect) ([]attribute.KeyValue, error) {
			dialectKey, dialectValue, dialectErr = metricDialect.SessionID(point)
			return nil, nil
		},
	}}

	err := handler.Handle(t.Context(), inbound, gcp.MessageMetadata{})

	require.NoError(t, err)
	publisher.AssertExpectations(t)
	require.NoError(t, dialectErr)
	require.Equal(t, "session.id", dialectKey)
	require.Equal(t, "session-id", dialectValue)
}

func transformTestInboundMetric() *otelv1.InboundMetric {
	return (&otelv1.InboundMetric_builder{
		Name: new("requests"),
		Gauge: (&otelv1.InboundMetric_Gauge_builder{
			DataPoints: []*otelv1.InboundMetric_NumberDataPoint{
				(&otelv1.InboundMetric_NumberDataPoint_builder{
					Attributes: []*otelv1.InboundMetric_KeyValue{
						(&otelv1.InboundMetric_KeyValue_builder{
							Key:   new("http.request.method"),
							Value: (&otelv1.InboundMetric_AnyValue_builder{StringValue: new("GET")}).Build(),
						}).Build(),
					},
					TimeUnixNano: new(uint64(200)),
					AsInt:        new(int64(1)),
				}).Build(),
			},
		}).Build(),
		Resource: (&otelv1.InboundMetric_Resource_builder{
			Attributes: []*otelv1.InboundMetric_KeyValue{
				(&otelv1.InboundMetric_KeyValue_builder{
					Key:   new("service.name"),
					Value: (&otelv1.InboundMetric_AnyValue_builder{StringValue: new("producer")}).Build(),
				}).Build(),
			},
		}).Build(),
		Provenance: (&otelv1.InboundMetric_Provenance_builder{
			Source:         new("test"),
			OrganizationId: new(testMetricOrganizationID),
			ProjectId:      new(testMetricProjectID),
		}).Build(),
	}).Build()
}

type stubMetricEnricher struct {
	name   string
	enrich func(context.Context, *otelv1.InboundMetric, oteldialect.MetricDialect) ([]attribute.KeyValue, error)
}

func (e stubMetricEnricher) Name() string { return e.name }

func (e stubMetricEnricher) Enrich(ctx context.Context, item *otelv1.InboundMetric, metricDialect oteldialect.MetricDialect) ([]attribute.KeyValue, error) {
	return e.enrich(ctx, item, metricDialect)
}
