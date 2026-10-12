package enrich

import (
	"context"
	"errors"
	"testing"

	otelv1 "github.com/speakeasy-api/gram/infra/gen/gram/otel/v1"
	"github.com/speakeasy-api/gram/server/internal/otel/dialect"
	"github.com/speakeasy-api/gram/server/internal/testenv"
	"github.com/stretchr/testify/require"
	"go.opentelemetry.io/otel/attribute"
)

func TestSpanCombinesAttributesInEnricherOrder(t *testing.T) {
	t.Parallel()

	enrichers := []SpanEnricher{
		stubSpanEnricher{name: "first", enrich: func(context.Context, *otelv1.InboundSpan) ([]attribute.KeyValue, error) {
			return []attribute.KeyValue{attribute.String("first", "value")}, nil
		}},
		stubSpanEnricher{name: "second", enrich: func(context.Context, *otelv1.InboundSpan) ([]attribute.KeyValue, error) {
			return []attribute.KeyValue{attribute.Int("second", 2)}, nil
		}},
	}

	attrs, err := Span(
		t.Context(),
		NewInstruments(testenv.NewLogger(t), testenv.NewMeterProvider(t)),
		(&otelv1.InboundSpan_builder{}).Build(),
		enrichers,
	)

	require.NoError(t, err)
	require.Equal(t, []attribute.KeyValue{
		attribute.String("first", "value"),
		attribute.Int("second", 2),
	}, attrs)
}

func TestSpanReturnsEnricherErrors(t *testing.T) {
	t.Parallel()

	enrichErr := errors.New("enrichment failed")
	attrs, err := Span(
		t.Context(),
		NewInstruments(testenv.NewLogger(t), testenv.NewMeterProvider(t)),
		(&otelv1.InboundSpan_builder{}).Build(),
		[]SpanEnricher{
			stubSpanEnricher{name: "broken", enrich: func(context.Context, *otelv1.InboundSpan) ([]attribute.KeyValue, error) {
				return nil, enrichErr
			}},
		},
	)

	require.ErrorIs(t, err, enrichErr)
	require.ErrorContains(t, err, "broken")
	require.Nil(t, attrs)
}

func TestSpanConvertsPanicsToErrors(t *testing.T) {
	t.Parallel()

	attrs, err := Span(
		t.Context(),
		NewInstruments(testenv.NewLogger(t), testenv.NewMeterProvider(t)),
		(&otelv1.InboundSpan_builder{}).Build(),
		[]SpanEnricher{
			stubSpanEnricher{name: "panicking", enrich: func(context.Context, *otelv1.InboundSpan) ([]attribute.KeyValue, error) {
				panic("boom")
			}},
		},
	)

	require.EqualError(t, err, "enrich span: panic in span enricher panicking: boom")
	require.Nil(t, attrs)
}

func TestMetricCombinesResourceAttributesInEnricherOrder(t *testing.T) {
	t.Parallel()

	enrichers := []MetricEnricher{
		stubMetricEnricher{name: "first", enrich: func(context.Context, *otelv1.InboundMetric, dialect.MetricDialect) ([]attribute.KeyValue, error) {
			return []attribute.KeyValue{attribute.String("first", "one")}, nil
		}},
		stubMetricEnricher{name: "second", enrich: func(context.Context, *otelv1.InboundMetric, dialect.MetricDialect) ([]attribute.KeyValue, error) {
			return []attribute.KeyValue{attribute.String("second", "two")}, nil
		}},
	}

	attrs, err := Metric(
		t.Context(),
		NewInstruments(testenv.NewLogger(t), testenv.NewMeterProvider(t)),
		new(otelv1.InboundMetric),
		enrichers,
	)

	require.NoError(t, err)
	require.Equal(t, []attribute.KeyValue{
		attribute.String("first", "one"),
		attribute.String("second", "two"),
	}, attrs)
}

func TestMetricReturnsEnricherErrors(t *testing.T) {
	t.Parallel()

	expected := errors.New("lookup failed")
	_, err := Metric(
		t.Context(),
		NewInstruments(testenv.NewLogger(t), testenv.NewMeterProvider(t)),
		new(otelv1.InboundMetric),
		[]MetricEnricher{stubMetricEnricher{name: "failing", enrich: func(context.Context, *otelv1.InboundMetric, dialect.MetricDialect) ([]attribute.KeyValue, error) {
			return nil, expected
		}}},
	)

	require.ErrorIs(t, err, expected)
	require.ErrorContains(t, err, "failing")
}

func TestMetricConvertsPanicsToErrors(t *testing.T) {
	t.Parallel()

	_, err := Metric(
		t.Context(),
		NewInstruments(testenv.NewLogger(t), testenv.NewMeterProvider(t)),
		new(otelv1.InboundMetric),
		[]MetricEnricher{stubMetricEnricher{name: "panicking", enrich: func(context.Context, *otelv1.InboundMetric, dialect.MetricDialect) ([]attribute.KeyValue, error) {
			panic("boom")
		}}},
	)

	require.EqualError(t, err, "enrich metric: panic in metric enricher panicking: boom")
}

func TestMetricWithNoEnrichersWritesNothing(t *testing.T) {
	t.Parallel()

	attrs, err := Metric(t.Context(), NewInstruments(testenv.NewLogger(t), testenv.NewMeterProvider(t)), new(otelv1.InboundMetric), nil)
	require.NoError(t, err)
	require.Nil(t, attrs)
}

type stubSpanEnricher struct {
	name   string
	enrich func(context.Context, *otelv1.InboundSpan) ([]attribute.KeyValue, error)
}

func (e stubSpanEnricher) Name() string { return e.name }

func (e stubSpanEnricher) Enrich(ctx context.Context, span *otelv1.InboundSpan) ([]attribute.KeyValue, error) {
	return e.enrich(ctx, span)
}

type stubMetricEnricher struct {
	name   string
	enrich func(context.Context, *otelv1.InboundMetric, dialect.MetricDialect) ([]attribute.KeyValue, error)
}

func (e stubMetricEnricher) Name() string { return e.name }

func (e stubMetricEnricher) Enrich(ctx context.Context, item *otelv1.InboundMetric, metricDialect dialect.MetricDialect) ([]attribute.KeyValue, error) {
	return e.enrich(ctx, item, metricDialect)
}
