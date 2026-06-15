package gcp

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"
	"go.opentelemetry.io/otel/propagation"
	"go.opentelemetry.io/otel/trace"
	"google.golang.org/protobuf/types/known/emptypb"
)

// testPropagator is the W3C composite the server installs globally, passed
// explicitly so these tests neither depend on nor mutate global propagator
// state and can run in parallel.
func testPropagator() propagation.TextMapPropagator {
	return propagation.NewCompositeTextMapPropagator(
		propagation.TraceContext{},
		propagation.Baggage{},
	)
}

func TestMessageAttributes_AlwaysCarriesContentTypeAndSchema(t *testing.T) {
	t.Parallel()

	attrs := messageAttributes(t.Context(), testPropagator(), &emptypb.Empty{})

	require.Equal(t, "application/x-protobuf", attrs["content-type"])
	require.Equal(t, "google.protobuf.Empty", attrs["schema"])
}

func TestMessageAttributes_NoTraceContextWithoutActiveSpan(t *testing.T) {
	t.Parallel()

	attrs := messageAttributes(t.Context(), testPropagator(), &emptypb.Empty{})

	// With no span in ctx the propagator injects nothing, so unpropagated
	// messages still let the subscriber start a fresh trace.
	require.NotContains(t, attrs, "traceparent")
}

func TestMessageAttributes_RoundTripsTraceContextToSubscriber(t *testing.T) {
	t.Parallel()

	prop := testPropagator()

	// A known remote span context, as if some upstream request started the trace.
	traceID, err := trace.TraceIDFromHex("0af7651916cd43dd8448eb211c80319c")
	require.NoError(t, err)
	spanID, err := trace.SpanIDFromHex("b7ad6b7169203331")
	require.NoError(t, err)
	parentSC := trace.NewSpanContext(trace.SpanContextConfig{
		TraceID:    traceID,
		SpanID:     spanID,
		TraceFlags: trace.FlagsSampled,
		Remote:     true,
	})
	publishCtx := trace.ContextWithSpanContext(context.Background(), parentSC)

	// Publisher side: messageAttributes injects the active trace context.
	attrs := messageAttributes(publishCtx, prop, &emptypb.Empty{})
	require.Contains(t, attrs, "traceparent", "an active span should be propagated")

	// Subscriber side (mirrors streams.go): extract from the message attributes.
	extracted := prop.Extract(context.Background(), propagation.MapCarrier(attrs))
	gotSC := trace.SpanContextFromContext(extracted)

	require.Equal(t, parentSC.TraceID(), gotSC.TraceID(), "subscriber should continue the producer's trace")
	require.Equal(t, parentSC.SpanID(), gotSC.SpanID(), "subscriber's parent should be the publishing span")
	require.True(t, gotSC.IsSampled())
}
