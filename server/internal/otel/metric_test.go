package otel

import (
	"context"
	"errors"
	"testing"

	otelv1 "github.com/speakeasy-api/gram/infra/gen/gram/otel/v1"
	oteldialect "github.com/speakeasy-api/gram/server/internal/otel/dialect"
	"github.com/speakeasy-api/gram/server/internal/testenv"
	"github.com/stretchr/testify/require"
	"go.opentelemetry.io/otel/attribute"
)

func TestEnrichMetricCombinesResourceAttributesInEnricherOrder(t *testing.T) {
	t.Parallel()

	enrichers := []MetricEnricher{
		stubMetricEnricher{name: "first", enrich: func(context.Context, *otelv1.InboundMetric, oteldialect.MetricDialect) ([]attribute.KeyValue, error) {
			return []attribute.KeyValue{attribute.String("first", "one")}, nil
		}},
		stubMetricEnricher{name: "second", enrich: func(context.Context, *otelv1.InboundMetric, oteldialect.MetricDialect) ([]attribute.KeyValue, error) {
			return []attribute.KeyValue{attribute.String("second", "two")}, nil
		}},
	}

	attrs, err := enrichMetric(
		t.Context(),
		newMetrics(testenv.NewLogger(t), testenv.NewMeterProvider(t)),
		new(otelv1.InboundMetric),
		enrichers,
	)

	require.NoError(t, err)
	require.Equal(t, []attribute.KeyValue{
		attribute.String("first", "one"),
		attribute.String("second", "two"),
	}, attrs)
}

func TestEnrichMetricReturnsEnricherErrors(t *testing.T) {
	t.Parallel()

	expected := errors.New("lookup failed")
	_, err := enrichMetric(
		t.Context(),
		newMetrics(testenv.NewLogger(t), testenv.NewMeterProvider(t)),
		new(otelv1.InboundMetric),
		[]MetricEnricher{stubMetricEnricher{name: "failing", enrich: func(context.Context, *otelv1.InboundMetric, oteldialect.MetricDialect) ([]attribute.KeyValue, error) {
			return nil, expected
		}}},
	)

	require.ErrorIs(t, err, expected)
	require.ErrorContains(t, err, "failing")
}

func TestEnrichMetricConvertsPanicsToErrors(t *testing.T) {
	t.Parallel()

	_, err := enrichMetric(
		t.Context(),
		newMetrics(testenv.NewLogger(t), testenv.NewMeterProvider(t)),
		new(otelv1.InboundMetric),
		[]MetricEnricher{stubMetricEnricher{name: "panicking", enrich: func(context.Context, *otelv1.InboundMetric, oteldialect.MetricDialect) ([]attribute.KeyValue, error) {
			panic("boom")
		}}},
	)

	require.ErrorContains(t, err, "panic in metric enricher panicking: boom")
}
