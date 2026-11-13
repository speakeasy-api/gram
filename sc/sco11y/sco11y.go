// Package sco11y reports statechart instances to OpenTelemetry.
//
// [New] builds [sc.Hooks] for a machine. Pass them to an actor with
// [sc.WithHooks] or to the Postgres driver with pgdriver.WithHooks. They
// record:
//
//   - a span for each accepted event, named "sc.transition", whose parent is
//     the span in the context the event was sent with;
//   - a span for each action and service run, and for each acquiring and
//     releasing of a resource, named "sc.action", "sc.service", "sc.acquire"
//     or "sc.release", which becomes the parent of the calls the effect
//     makes;
//   - an "sc.event.ignored" span event on the current span for each event
//     that changed nothing;
//   - the metrics sc.transitions, sc.events.ignored, and sc.effect.duration.
//
// Every attribute value comes from the chart: machine, state, action, service
// and event names. None comes from the context, so cardinality is bounded by
// the chart.
//
// The one value that does not come from the chart is the error of a failed
// action or service, which is recorded on its span as OpenTelemetry records
// any error, and never on a metric. An effect whose errors may carry data
// that must not reach traces should return errors that leave it out.
package sco11y

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/metric"
	"go.opentelemetry.io/otel/trace"

	"github.com/speakeasy-api/gram/sc"
)

// instrumentationName identifies this package as the source of its spans
// and metrics.
const instrumentationName = "github.com/speakeasy-api/gram/sc/sco11y"

// Attribute keys recorded on spans and metrics.
const (
	// keyMachine is the machine's ID.
	keyMachine = attribute.Key("sc.machine")

	// keyEvent is the name of the event the machine processed.
	keyEvent = attribute.Key("sc.event")

	// keyFrom lists the states active before an event.
	keyFrom = attribute.Key("sc.states.from")

	// keyTo lists the states active after an event.
	keyTo = attribute.Key("sc.states.to")

	// keyDone is set when an event finished the machine.
	keyDone = attribute.Key("sc.done")

	// keyState is the state an effect belongs to.
	keyState = attribute.Key("sc.state")

	// keyEffectKind is "action", "service", "acquire" or "release".
	keyEffectKind = attribute.Key("sc.effect.kind")

	// keyEffectName is the action or service name.
	keyEffectName = attribute.Key("sc.effect.name")

	// keyOutcome is how an effect ended: ok, error, or stopped.
	keyOutcome = attribute.Key("sc.effect.outcome")
)

// Outcomes recorded under sc.effect.outcome.
const (
	// outcomeOK is an effect that ended without an error.
	outcomeOK = "ok"

	// outcomeError is an effect that ended with an error.
	outcomeError = "error"

	// outcomeStopped is a service stopped because its state was left, or
	// an effect cut short because its driver stopped.
	outcomeStopped = "stopped"
)

// New returns hooks that report m's instances to tp and mp.
func New[S comparable, C any](m *sc.Machine[S, C], tp trace.TracerProvider, mp metric.MeterProvider) (*sc.Hooks[S, C], error) {
	tracer := tp.Tracer(instrumentationName)
	meter := mp.Meter(instrumentationName)

	transitions, err := meter.Int64Counter("sc.transitions",
		metric.WithDescription("Events accepted by statechart instances."),
		metric.WithUnit("{event}"),
	)
	if err != nil {
		return nil, fmt.Errorf("create transitions counter: %w", err)
	}

	ignored, err := meter.Int64Counter("sc.events.ignored",
		metric.WithDescription("Events that changed nothing because the timer or service run they refer to was no longer outstanding, or the instance was done."),
		metric.WithUnit("{event}"),
	)
	if err != nil {
		return nil, fmt.Errorf("create ignored events counter: %w", err)
	}

	durations, err := meter.Float64Histogram("sc.effect.duration",
		metric.WithDescription("How long statechart actions, service runs, and resource acquisitions and releases took."),
		metric.WithUnit("s"),
	)
	if err != nil {
		return nil, fmt.Errorf("create effect duration histogram: %w", err)
	}

	machine := keyMachine.String(m.ID())

	return &sc.Hooks[S, C]{
		Transition: func(ctx context.Context, step sc.Step[S, C]) {
			attrs := []attribute.KeyValue{
				machine,
				keyEvent.String(eventName(m, step.Event)),
				keyTo.String(states(step.To)),
			}
			transitions.Add(ctx, 1, metric.WithAttributes(attrs...))

			_, span := tracer.Start(ctx, "sc.transition", trace.WithAttributes(append(attrs,
				keyFrom.String(states(step.From)),
				keyDone.Bool(step.To.Done()),
			)...))
			span.End()
		},
		Ignored: func(ctx context.Context, _ sc.Snapshot[S, C], event any) {
			attrs := []attribute.KeyValue{machine, keyEvent.String(eventName(m, event))}
			ignored.Add(ctx, 1, metric.WithAttributes(attrs...))
			trace.SpanFromContext(ctx).AddEvent("sc.event.ignored", trace.WithAttributes(attrs...))
		},
		EffectStarted: func(ctx context.Context, e sc.Effect[C]) context.Context {
			ctx, _ = tracer.Start(ctx, "sc."+effectKind(e), trace.WithAttributes(effectAttributes(machine, e)...))
			return ctx
		},
		EffectFinished: func(ctx context.Context, e sc.Effect[C], elapsed time.Duration, err error) {
			outcome := outcomeOf(err)

			span := trace.SpanFromContext(ctx)
			if outcome == outcomeError {
				span.RecordError(err)
				span.SetStatus(codes.Error, err.Error())
			}
			span.SetAttributes(keyOutcome.String(outcome))
			span.End()

			durations.Record(ctx, elapsed.Seconds(), metric.WithAttributes(append(effectAttributes(machine, e), keyOutcome.String(outcome))...))
		},
	}, nil
}

// effectKind is the kind recorded for an effect: an action, a service, or
// the acquiring or releasing of a resource.
func effectKind[C any](e sc.Effect[C]) string {
	switch e.Kind {
	case sc.EffectStartInvocation:
		return "service"
	case sc.EffectAcquire:
		return "acquire"
	case sc.EffectRelease:
		return "release"
	case sc.EffectAction, sc.EffectStopInvocation, sc.EffectStartTimer, sc.EffectCancelTimer:
		return "action"
	}

	return "action"
}

func effectAttributes[C any](machine attribute.KeyValue, e sc.Effect[C]) []attribute.KeyValue {
	return []attribute.KeyValue{
		machine,
		keyState.String(e.State),
		keyEffectKind.String(effectKind(e)),
		keyEffectName.String(e.Name),
	}
}

func outcomeOf(err error) string {
	switch {
	case err == nil:
		return outcomeOK
	case errors.Is(err, context.Canceled):
		return outcomeStopped
	default:
		return outcomeError
	}
}

// eventName names an event for spans and metrics. The events the package
// itself defines get short fixed names, and a stream's event is named after
// the event it carries.
func eventName[S comparable, C any](m *sc.Machine[S, C], ev any) string {
	switch e := ev.(type) {
	case sc.TimerFired:
		return "sc.timer"
	case sc.Failed:
		return "sc.failed"
	case sc.Emitted:
		return m.EventName(e.Event)
	}

	name := m.EventName(ev)
	if strings.HasPrefix(name, "sc.done:") {
		return "sc.done"
	}
	if strings.HasPrefix(name, "sc.acquired:") {
		return "sc.acquired"
	}

	return name
}

// states lists a snapshot's active states.
func states[S comparable, C any](snap sc.Snapshot[S, C]) string {
	active := snap.Active()
	names := make([]string, 0, len(active))
	for _, s := range active {
		names = append(names, fmt.Sprint(s))
	}

	return strings.Join(names, ",")
}
