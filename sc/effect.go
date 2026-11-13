package sc

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"reflect"
	"time"
)

// Done is the event delivered when an invoked service succeeds.
type Done[R any] struct {
	// ID identifies the run of the service that produced the result.
	ID string `json:"id"`

	// Result is the value the service returned.
	Result R `json:"result"`
}

func (d Done[R]) invocationID() string { return d.ID }

// Failed is the event delivered when an invoked service returns an error.
type Failed struct {
	// ID identifies the run of the service that failed.
	ID string

	// Err is the error the service returned. Only its message survives
	// being stored and loaded again.
	Err error
}

func (f Failed) invocationID() string { return f.ID }

// failedJSON is the stored form of a Failed event.
type failedJSON struct {
	// ID identifies the run of the service that failed.
	ID string `json:"id"`

	// Error is the message of the service's error.
	Error string `json:"error"`
}

// MarshalJSON stores the error as its message.
func (f Failed) MarshalJSON() ([]byte, error) {
	msg := ""
	if f.Err != nil {
		msg = f.Err.Error()
	}

	data, err := json.Marshal(failedJSON{ID: f.ID, Error: msg})
	if err != nil {
		return nil, fmt.Errorf("marshal failed event: %w", err)
	}

	return data, nil
}

// UnmarshalJSON restores the error from its message.
func (f *Failed) UnmarshalJSON(data []byte) error {
	var raw failedJSON
	if err := json.Unmarshal(data, &raw); err != nil {
		return fmt.Errorf("unmarshal failed event: %w", err)
	}

	f.ID = raw.ID
	f.Err = errors.New(raw.Error)

	return nil
}

// invocationResult is implemented by the events that end a service run.
type invocationResult interface {
	invocationID() string
}

// Emitted wraps an event produced by a [StateBuilder.Stream] service so the
// machine can tell which run of the service it came from.
type Emitted struct {
	// ID identifies the run of the service that produced the event.
	ID string

	// Event is the value the service yielded.
	Event any
}

// TimerFired is the event a driver sends when a timer started by an
// [EffectStartTimer] effect is due.
type TimerFired struct {
	// ID is the ID of the effect that started the timer.
	ID string
}

// EffectKind says what a driver must do with an [Effect].
type EffectKind string

const (
	// EffectAction runs an action with [Machine.Exec].
	EffectAction EffectKind = "action"

	// EffectStartInvocation runs a service with [Machine.Exec] and sends
	// the events it produces back to the machine.
	EffectStartInvocation EffectKind = "invocation.start"

	// EffectStopInvocation stops the service run with the same ID. A
	// driver that cannot stop it may let it finish; its events are
	// ignored.
	EffectStopInvocation EffectKind = "invocation.stop"

	// EffectStartTimer asks for a [TimerFired] event with the effect's ID
	// once Delay has passed.
	EffectStartTimer EffectKind = "timer.start"

	// EffectCancelTimer cancels the timer with the same ID. A driver that
	// cannot cancel it may let it fire; the event is ignored.
	EffectCancelTimer EffectKind = "timer.cancel"
)

// Effect is one piece of work a transition asks its driver to carry out.
// Effects are returned in the order they must take effect.
type Effect[C any] struct {
	// Kind says what the driver must do.
	Kind EffectKind

	// ID identifies a timer or a service run and is unique within one
	// machine instance. It is empty for actions.
	ID string

	// State is the name of the state the effect belongs to.
	State string

	// Name is the action or service name. It is empty for timers.
	Name string

	// Delay is how long an EffectStartTimer timer runs.
	Delay time.Duration

	// Context is the context an action or service must see.
	Context C

	// Event is the event that triggered an action declared on a
	// transition. It is nil for entry and exit actions.
	Event any
}

// ErrUnknownEffect is returned when an effect names an action or service the
// chart does not declare, which happens when an effect outlives a chart
// change.
var ErrUnknownEffect = errors.New("effect does not match the chart")

// Exec carries out an [EffectAction] or [EffectStartInvocation] effect. For a
// service it blocks until the service finishes or ctx is cancelled, calling
// send with each event the service produces; the driver passes those events
// to [Machine.Transition]. Timer and stop effects are the driver's to handle
// and are rejected.
func (m *Machine[S, C]) Exec(ctx context.Context, e Effect[C], send func(ev any)) error {
	n, ok := m.names[e.State]
	if !ok {
		return fmt.Errorf("%w: state %q", ErrUnknownEffect, e.State)
	}

	switch e.Kind {
	case EffectAction:
		return m.execAction(ctx, n, e)
	case EffectStartInvocation:
		for _, inv := range n.invocations {
			if inv.name != e.Name {
				continue
			}

			for ev := range inv.run(ctx, e.Context, e.ID) {
				send(ev)
				if ctx.Err() != nil {
					break
				}
			}

			return nil
		}

		return fmt.Errorf("%w: service %q in state %q", ErrUnknownEffect, e.Name, e.State)
	case EffectStopInvocation, EffectStartTimer, EffectCancelTimer:
		return fmt.Errorf("%w: %s effects are handled by the driver", errors.ErrUnsupported, e.Kind)
	default:
		return fmt.Errorf("%w: kind %q", ErrUnknownEffect, e.Kind)
	}
}

func (m *Machine[S, C]) execAction(ctx context.Context, n *node[S, C], e Effect[C]) error {
	for _, a := range n.entry {
		if a.name == e.Name {
			a.fn(ctx, e.Context)
			return nil
		}
	}

	for _, a := range n.exit {
		if a.name == e.Name {
			a.fn(ctx, e.Context)
			return nil
		}
	}

	for _, t := range n.transitions {
		for _, s := range t.steps {
			if s.kind == stepDo && s.name == e.Name {
				s.do(ctx, e.Context, e.Event)
				return nil
			}
		}
	}

	return fmt.Errorf("%w: action %q in state %q", ErrUnknownEffect, e.Name, e.State)
}

// effectJSON is the stored form of an Effect.
type effectJSON struct {
	// Kind says what the driver must do.
	Kind EffectKind `json:"kind"`

	// ID identifies a timer or a service run.
	ID string `json:"id,omitempty"`

	// State is the name of the state the effect belongs to.
	State string `json:"state"`

	// Name is the action or service name.
	Name string `json:"name,omitempty"`

	// DelayNanos is the timer duration in nanoseconds.
	DelayNanos int64 `json:"delay_ns,omitempty"`

	// Context is the context an action or service must see. Timer and
	// stop effects carry none.
	Context json.RawMessage `json:"context,omitempty"`

	// EventName is the registered name of the triggering event's type.
	EventName string `json:"event_name,omitempty"`

	// Event is the triggering event.
	Event json.RawMessage `json:"event,omitempty"`
}

// EncodeEffect serialises an effect so a driver can store it and carry it
// out later. The context and any triggering event must marshal to JSON.
func (m *Machine[S, C]) EncodeEffect(e Effect[C]) ([]byte, error) {
	raw := effectJSON{
		Kind:       e.Kind,
		ID:         e.ID,
		State:      e.State,
		Name:       e.Name,
		DelayNanos: int64(e.Delay),
		Context:    nil,
		EventName:  "",
		Event:      nil,
	}

	if e.Kind == EffectAction || e.Kind == EffectStartInvocation {
		ctx, err := json.Marshal(e.Context)
		if err != nil {
			return nil, fmt.Errorf("marshal effect context: %w", err)
		}
		raw.Context = ctx
	}

	if e.Event != nil {
		name, ok := m.eventNames[reflect.TypeOf(e.Event)]
		if !ok {
			return nil, fmt.Errorf("marshal effect event: type %T is not handled by the chart", e.Event)
		}

		ev, err := json.Marshal(e.Event)
		if err != nil {
			return nil, fmt.Errorf("marshal effect event %q: %w", name, err)
		}
		raw.EventName = name
		raw.Event = ev
	}

	data, err := json.Marshal(raw)
	if err != nil {
		return nil, fmt.Errorf("marshal effect: %w", err)
	}

	return data, nil
}

// DecodeEffect is the inverse of [Machine.EncodeEffect].
func (m *Machine[S, C]) DecodeEffect(data []byte) (Effect[C], error) {
	var zero Effect[C]

	var raw effectJSON
	if err := json.Unmarshal(data, &raw); err != nil {
		return zero, fmt.Errorf("unmarshal effect: %w", err)
	}

	e := Effect[C]{
		Kind:    raw.Kind,
		ID:      raw.ID,
		State:   raw.State,
		Name:    raw.Name,
		Delay:   time.Duration(raw.DelayNanos),
		Context: zero.Context,
		Event:   nil,
	}

	if len(raw.Context) > 0 {
		if err := json.Unmarshal(raw.Context, &e.Context); err != nil {
			return zero, fmt.Errorf("unmarshal effect context: %w", err)
		}
	}

	if raw.EventName != "" {
		t, ok := m.eventTypes[raw.EventName]
		if !ok {
			return zero, fmt.Errorf("%w: event %q", ErrUnknownEffect, raw.EventName)
		}

		ev := reflect.New(t)
		if err := json.Unmarshal(raw.Event, ev.Interface()); err != nil {
			return zero, fmt.Errorf("unmarshal effect event %q: %w", raw.EventName, err)
		}
		e.Event = ev.Elem().Interface()
	}

	return e, nil
}
