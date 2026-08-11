package outbox

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/require"
	"go.opentelemetry.io/otel/propagation"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"

	webhooksv1 "github.com/speakeasy-api/gram/infra/gen/gram/webhooks/v1"
)

// TestBuildEntry_CapturesProducerTraceContext covers the reason the attributes
// column exists. The relay publishes the row long afterwards under a trace of
// its own, so the link back to the request that produced the event survives
// only if it is captured here, at write time.
//
// Tested against buildEntry with an explicit propagator rather than through
// Publish: the trace context is the one input Publish takes from a process-wide
// singleton, and setting that singleton from a test would leak into every other
// test in the package and race the parallel ones. The propagator is a parameter
// precisely so this test does not have to.
func TestBuildEntry_CapturesProducerTraceContext(t *testing.T) {
	t.Parallel()

	tp := sdktrace.NewTracerProvider()
	t.Cleanup(func() { _ = tp.Shutdown(context.Background()) })

	ctx, span := tp.Tracer("test").Start(t.Context(), "produce")
	defer span.End()

	entry, err := buildEntry(ctx, "org-1", Message{Proto: &webhooksv1.Event{}}, propagation.TraceContext{})
	require.NoError(t, err)

	var attrs map[string]string
	require.NoError(t, json.Unmarshal(entry.Attributes, &attrs))

	require.Contains(t, attrs, "traceparent")
	require.Contains(t, attrs["traceparent"], span.SpanContext().TraceID().String(),
		"the row has to carry the producing trace, not whatever trace the relay runs under")
}

// TestBuildEntry_TraceContextCannotBeSpoofed pins the merge order. A caller
// supplying its own traceparent would otherwise decide what the subscriber
// links back to, and the producing span is the one thing only this call site
// knows.
func TestBuildEntry_TraceContextCannotBeSpoofed(t *testing.T) {
	t.Parallel()

	tp := sdktrace.NewTracerProvider()
	t.Cleanup(func() { _ = tp.Shutdown(context.Background()) })

	ctx, span := tp.Tracer("test").Start(t.Context(), "produce")
	defer span.End()

	entry, err := buildEntry(ctx, "org-1", Message{
		Proto: &webhooksv1.Event{},
		Attributes: map[string]string{
			"traceparent": "00-4bf92f3577b34da6a3ce929d0e0e4736-00f067aa0ba902b7-01",
			"event_type":  "audit_log.asset_event_v1",
		},
	}, propagation.TraceContext{})
	require.NoError(t, err)

	var attrs map[string]string
	require.NoError(t, json.Unmarshal(entry.Attributes, &attrs))

	require.Contains(t, attrs["traceparent"], span.SpanContext().TraceID().String())
	require.Equal(t, "audit_log.asset_event_v1", attrs["event_type"],
		"everything the caller sets other than trace context is still theirs")
}

// TestBuildEntry_WithoutPropagatorStillBuilds covers the production default
// before any SDK is configured: the global propagator injects nothing, and an
// event with no trace context is still a publishable row.
func TestBuildEntry_WithoutPropagatorStillBuilds(t *testing.T) {
	t.Parallel()

	entry, err := buildEntry(t.Context(), "org-1", Message{Proto: &webhooksv1.Event{}},
		propagation.NewCompositeTextMapPropagator())
	require.NoError(t, err)

	var attrs map[string]string
	require.NoError(t, json.Unmarshal(entry.Attributes, &attrs))
	require.NotContains(t, attrs, "traceparent")
}
