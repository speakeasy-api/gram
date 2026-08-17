package otel

import (
	"context"
	"errors"
	"testing"

	otelv1 "github.com/speakeasy-api/gram/infra/gen/gram/otel/v1"
	"github.com/speakeasy-api/gram/server/internal/testenv"
	"github.com/stretchr/testify/require"
	"go.opentelemetry.io/otel/attribute"
)

func TestEnrichSpanCombinesAttributesInEnricherOrder(t *testing.T) {
	t.Parallel()

	enrichers := []SpanEnricher{
		stubSpanEnricher{name: "first", enrich: func(context.Context, *otelv1.InboundSpan) ([]attribute.KeyValue, error) {
			return []attribute.KeyValue{attribute.String("first", "value")}, nil
		}},
		stubSpanEnricher{name: "second", enrich: func(context.Context, *otelv1.InboundSpan) ([]attribute.KeyValue, error) {
			return []attribute.KeyValue{attribute.Int("second", 2)}, nil
		}},
	}

	attrs, err := enrichSpan(
		t.Context(),
		newMetrics(testenv.NewLogger(t), testenv.NewMeterProvider(t)),
		(&otelv1.InboundSpan_builder{}).Build(),
		enrichers,
	)

	require.NoError(t, err)
	require.Equal(t, []attribute.KeyValue{
		attribute.String("first", "value"),
		attribute.Int("second", 2),
	}, attrs)
}

func TestEnrichSpanReturnsEnricherErrors(t *testing.T) {
	t.Parallel()

	enrichErr := errors.New("enrichment failed")
	attrs, err := enrichSpan(
		t.Context(),
		newMetrics(testenv.NewLogger(t), testenv.NewMeterProvider(t)),
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

func TestEnrichSpanConvertsPanicsToErrors(t *testing.T) {
	t.Parallel()

	attrs, err := enrichSpan(
		t.Context(),
		newMetrics(testenv.NewLogger(t), testenv.NewMeterProvider(t)),
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

func TestValidateSpanAcceptsCompleteSpan(t *testing.T) {
	t.Parallel()

	err := validateSpan(stubSpanLike{
		traceID: []byte{1},
		spanID:  []byte{2},
		name:    "operation",
		start:   10,
		end:     20,
	})

	require.NoError(t, err)
}

func TestValidateSpanRejectsInvalidFields(t *testing.T) {
	t.Parallel()

	require.ErrorContains(t, validateSpan(nil), "span is nil")
	require.ErrorContains(t, validateSpan(stubSpanLike{name: "operation", spanID: []byte{2}, start: 10, end: 20}), "trace_id is empty")
	require.ErrorContains(t, validateSpan(stubSpanLike{name: "operation", traceID: []byte{1}, start: 10, end: 20}), "span_id is empty")
	require.ErrorContains(t, validateSpan(stubSpanLike{traceID: []byte{1}, spanID: []byte{2}, name: "  ", start: 10, end: 20}), "name is empty")
	require.ErrorContains(t, validateSpan(stubSpanLike{traceID: []byte{1}, spanID: []byte{2}, name: "operation", end: 20}), "start_time_unix_nano is zero")
	require.ErrorContains(t, validateSpan(stubSpanLike{traceID: []byte{1}, spanID: []byte{2}, name: "operation", start: 10}), "end_time_unix_nano is zero")
	require.ErrorContains(t, validateSpan(stubSpanLike{traceID: []byte{1}, spanID: []byte{2}, name: "operation", start: 20, end: 10}), "before start_time_unix_nano")
}

type stubSpanEnricher struct {
	name   string
	enrich func(context.Context, *otelv1.InboundSpan) ([]attribute.KeyValue, error)
}

func (e stubSpanEnricher) Name() string { return e.name }

func (e stubSpanEnricher) Enrich(ctx context.Context, span *otelv1.InboundSpan) ([]attribute.KeyValue, error) {
	return e.enrich(ctx, span)
}

type stubSpanLike struct {
	traceID []byte
	spanID  []byte
	name    string
	start   uint64
	end     uint64
}

func (s stubSpanLike) GetTraceId() []byte           { return s.traceID }
func (s stubSpanLike) GetSpanId() []byte            { return s.spanID }
func (s stubSpanLike) GetName() string              { return s.name }
func (s stubSpanLike) GetStartTimeUnixNano() uint64 { return s.start }
func (s stubSpanLike) GetEndTimeUnixNano() uint64   { return s.end }
