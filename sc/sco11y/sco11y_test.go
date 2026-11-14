package sco11y_test

import (
	"context"
	"errors"
	"testing"
	"testing/synctest"
	"time"

	"github.com/stretchr/testify/require"
	"go.opentelemetry.io/otel/attribute"
	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/metric/metricdata"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"

	"github.com/speakeasy-api/gram/sc"
	"github.com/speakeasy-api/gram/sc/sco11y"
)

type (
	submit struct{}
	stale  struct{}
)

var errDeclined = errors.New("card declined")

// epoch is the time the tests start instances at.
var epoch = time.Date(2026, time.January, 1, 0, 0, 0, 0, time.UTC)

func TestHooksRecordSpansAndMetrics(t *testing.T) {
	t.Parallel()

	synctest.Test(t, func(t *testing.T) {
		spans := tracetest.NewSpanRecorder()
		tp := sdktrace.NewTracerProvider(sdktrace.WithSpanProcessor(spans))
		reader := sdkmetric.NewManualReader()
		mp := sdkmetric.NewMeterProvider(sdkmetric.WithReader(reader))

		m, err := sc.New("payment", func(b *sc.Builder[string, int]) {
			b.Initial("idle")
			b.State("idle").On[submit]().Do("announce", func(context.Context, int, submit) {}).To("charging")
			charge := b.State("charging").Invoke("charge", func(context.Context, *sc.Run, int) (string, error) { return "", errDeclined })
			charge.OnDone().To("paid")
			charge.OnError().To("declined")
			b.Final("paid")
			b.Final("declined")
		})
		require.NoError(t, err)

		hooks, err := sco11y.New(m, tp, mp)
		require.NoError(t, err)

		a := m.Actor(0, sc.WithHooks(hooks))
		done := make(chan error, 1)
		go func() { done <- a.Run(t.Context()) }()

		ctx, parent := tp.Tracer("test").Start(t.Context(), "request")
		require.NoError(t, a.Send(ctx, sc.TimerFired{ID: "stale"}))
		require.NoError(t, a.Send(ctx, submit{}))
		require.NoError(t, <-done)
		parent.End()

		byName := map[string][]sdktrace.ReadOnlySpan{}
		for _, s := range spans.Ended() {
			byName[s.Name()] = append(byName[s.Name()], s)
		}

		// The submit's transition is a child of the request that sent
		// it; the failure's transition is a child of the service run
		// that produced it.
		transitions := byName["sc.transition"]
		require.Len(t, transitions, 2)
		require.Equal(t, parent.SpanContext().SpanID(), transitions[0].Parent().SpanID())

		service := byName["sc.service"]
		require.Len(t, service, 1)
		require.Equal(t, "Error", service[0].Status().Code.String())
		require.Equal(t, service[0].SpanContext().SpanID(), transitions[1].Parent().SpanID())
		require.Contains(t, service[0].Attributes(), attribute.String("sc.effect.outcome", "error"))

		require.Len(t, byName["sc.action"], 1)

		var ignored bool
		for _, s := range byName["request"] {
			for _, ev := range s.Events() {
				ignored = ignored || ev.Name == "sc.event.ignored"
			}
		}
		require.True(t, ignored)

		var data metricdata.ResourceMetrics
		require.NoError(t, reader.Collect(context.Background(), &data))

		totals := map[string]int64{}
		var effects uint64
		for _, sm := range data.ScopeMetrics {
			for _, metric := range sm.Metrics {
				switch d := metric.Data.(type) {
				case metricdata.Sum[int64]:
					for _, p := range d.DataPoints {
						totals[metric.Name] += p.Value
					}
				case metricdata.Histogram[float64]:
					for _, p := range d.DataPoints {
						effects += p.Count
					}
				}
			}
		}
		require.Equal(t, map[string]int64{"sc.transitions": 2, "sc.events.ignored": 1}, totals)
		require.Equal(t, uint64(2), effects)
	})
}

func TestHooksNameTheEventsThePackageDefines(t *testing.T) {
	t.Parallel()

	spans := tracetest.NewSpanRecorder()
	tp := sdktrace.NewTracerProvider(sdktrace.WithSpanProcessor(spans))
	mp := sdkmetric.NewMeterProvider()

	m, err := sc.New("names", func(b *sc.Builder[string, int]) {
		b.Initial("a")
		b.State("a").On[stale]().To("b")
		b.Final("b")
	})
	require.NoError(t, err)

	hooks, err := sco11y.New(m, tp, mp)
	require.NoError(t, err)

	from, _ := m.Start(0, epoch)
	to, effects := m.Transition(from, stale{}, epoch)
	hooks.Observe(t.Context(), sc.Step[string, int]{At: epoch, From: from, Event: stale{}, To: to, Effects: effects})

	ended := spans.Ended()
	require.Len(t, ended, 1)
	require.Contains(t, ended[0].Attributes(), attribute.String("sc.event", "stale"))
	require.Contains(t, ended[0].Attributes(), attribute.Bool("sc.done", true))
}
