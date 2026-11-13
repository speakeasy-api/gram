package sc

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"reflect"
	"runtime/debug"
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

	// Err is the error the service returned. Its message survives being
	// stored and loaded again, and so does the code of an error that
	// implements [Coder]; route on that with [ErrorCode] rather than on the
	// error's type when the event may be persisted.
	Err error
}

// Coder is implemented by errors that carry a stable code, such as
// "rate_limited". A [Failed] event keeps the code when it is stored, so a
// chart can route on it with [ErrorCode] under any driver.
type Coder interface {
	Code() string
}

// codedError is what a stored error with a code becomes when it is loaded.
type codedError struct {
	msg  string
	code string
}

func (e codedError) Error() string { return e.msg }

func (e codedError) Code() string { return e.code }

// WithCode returns err carrying code, for an error from a package that has
// no codes of its own. The result wraps err, so errors.Is and errors.As
// still see it.
func WithCode(err error, code string) error {
	if err == nil {
		return nil
	}

	return codedWrapError{err: err, code: code}
}

// codedWrapError is an error given a code by WithCode.
type codedWrapError struct {
	err  error
	code string
}

func (e codedWrapError) Error() string { return e.err.Error() }

func (e codedWrapError) Code() string { return e.code }

func (e codedWrapError) Unwrap() error { return e.err }

// ErrorCode returns the code of the first error in err's tree that
// implements [Coder], or "" when none does.
func ErrorCode(err error) string {
	var c Coder
	if errors.As(err, &c) {
		return c.Code()
	}

	return ""
}

func (f Failed) invocationID() string { return f.ID }

// failedJSON is the stored form of a Failed event.
type failedJSON struct {
	// ID identifies the run of the service that failed.
	ID string `json:"id"`

	// Error is the message of the service's error.
	Error string `json:"error"`

	// Code is the error's code, when it implements Coder.
	Code string `json:"code,omitempty"`
}

// MarshalJSON stores the error as its message.
func (f Failed) MarshalJSON() ([]byte, error) {
	msg := ""
	if f.Err != nil {
		msg = f.Err.Error()
	}

	data, err := json.Marshal(failedJSON{ID: f.ID, Error: msg, Code: ErrorCode(f.Err)})
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
	if raw.Code != "" {
		f.Err = codedError{msg: raw.Error, code: raw.Code}
	}

	return nil
}

// Acquired is the event delivered when a resource declared with
// [StateBuilder.Holds] has been acquired. The driver keeps Value for the
// services and actions inside the resource's state; the chart may read it
// in the OnAcquired transition, but putting a live value into the context
// would defeat the point of a resource.
type Acquired[R any] struct {
	// ID identifies the acquisition.
	ID string

	// Value is the acquired resource.
	Value R
}

func (a Acquired[R]) acquisition() (string, any) { return a.ID, a.Value }

// acquisition is implemented by Acquired events.
type acquisition interface {
	acquisition() (id string, value any)
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

	// EffectAcquire acquires a resource with [Machine.Exec], which reports
	// an [Acquired] event carrying the value, or [Failed]. The driver keeps
	// the value until the EffectRelease with the same ID. Only the [Instance]
	// and [Actor] drivers hold resources.
	EffectAcquire EffectKind = "resource.acquire"

	// EffectRelease gives back the resource acquired under the same ID. If
	// the acquisition is still in flight, the driver releases the value as
	// soon as it arrives.
	EffectRelease EffectKind = "resource.release"

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

	// Name is the action or service name. For a timer it is the name of a
	// computed delay or deadline, and empty for a fixed delay.
	Name string

	// Delay is how long an EffectStartTimer timer runs.
	Delay time.Duration

	// At is when an EffectStartTimer timer is due, measured on the clock
	// the driver passed to the core. Drivers schedule the timer for At; a
	// time already past fires at once. For an EffectStartInvocation or an
	// EffectAcquire it is the deadline of the budgets declared with
	// [StateBuilder.Within] around the service or the acquisition, on the
	// same clock; zero for none. [Hooks.Exec] applies
	// it, and [Effect.Budget] turns it into a duration for other drivers.
	At time.Time

	// Jitter is the fraction by which the driver varies a timer's wait at
	// random, in either direction. [Effect.Wait] applies it.
	Jitter float64

	// Context is the context an action or service must see.
	Context C

	// Event is the event that triggered an action declared on a
	// transition, or the event of the step a chart action runs in. It is
	// nil for a state's own entry and exit actions.
	Event any

	// From and To name the states active before and after the step an
	// action belongs to. A chart action sees them as its [Move].
	From []string

	// To names the states active after the step.
	To []string

	// Needs lists the IDs of the held resources a service or an
	// acquisition runs inside. A driver resuming an instance waits for them
	// to be acquired again before it runs the effect. It is not persisted.
	Needs []string
}

// Budget returns how long an EffectStartInvocation service or an
// EffectAcquire acquisition may run, from now on the driver's clock, and false when no [StateBuilder.Within] budget
// applies. A budget already spent is zero. Drivers turn it into a context
// timeout rather than a deadline, because the driver's clock need not be the
// wall clock.
func (e Effect[C]) Budget(now time.Time) (time.Duration, bool) {
	if (e.Kind != EffectStartInvocation && e.Kind != EffectAcquire) || e.At.IsZero() {
		return 0, false
	}

	return max(e.At.Sub(now), 0), true
}

// Wait is how long a driver waits, from now, before an EffectStartTimer
// timer fires: until At, varied by up to Jitter of that wait using random,
// which returns a number in [0, 1). The result is never negative.
func (e Effect[C]) Wait(now time.Time, random func() float64) time.Duration {
	wait := e.At.Sub(now)

	if e.Jitter > 0 && wait > 0 {
		wait = time.Duration(float64(wait) * (1 + e.Jitter*(2*random()-1)))
	}

	return max(wait, 0)
}

// ErrPanicked is returned by [Machine.Exec] when an action or service
// panicked. The error carries the panic value and the stack. Drivers deliver
// a service's panic to the chart as its [Failed] event, so a chart decides
// what a crashed service means the way it does for any other failure.
var ErrPanicked = errors.New("action or service panicked")

// sendPanic carries a panic raised by a driver's send callback through the
// service that was producing events, so that Exec does not report it as the
// service's own.
type sendPanic struct{ value any }

func guardedSend(send func(ev any), ev any) {
	defer func() {
		if p := recover(); p != nil {
			panic(sendPanic{value: p})
		}
	}()

	send(ev)
}

// ErrUnknownEffect is returned when an effect names an action or service the
// chart does not declare, which happens when an effect outlives a chart
// change.
var ErrUnknownEffect = errors.New("effect does not match the chart")

// Exec carries out an [EffectAction], [EffectStartInvocation] or
// [EffectAcquire] effect. For a service or an acquisition it blocks until
// that finishes or ctx is cancelled, calling send with each event it
// produces; the driver passes those events to [Machine.Transition]. Timer,
// stop and release effects are the driver's to handle and are rejected.
//
// A panic in the action or service comes back as [ErrPanicked]. A panic in
// send is the driver's own and is not caught.
func (m *Machine[S, C]) Exec(ctx context.Context, e Effect[C], send func(ev any)) (err error) {
	defer func() {
		if p := recover(); p != nil {
			if own, ok := p.(sendPanic); ok {
				panic(own.value)
			}

			err = fmt.Errorf("%w: %s %q in state %q: %v\n%s", ErrPanicked, e.Kind, e.Name, e.State, p, debug.Stack())
		}
	}()

	n, ok := m.names[e.State]
	if !ok {
		return fmt.Errorf("%w: state %q", ErrUnknownEffect, e.State)
	}

	switch e.Kind {
	case EffectAction:
		return m.execAction(ctx, n, e)
	case EffectStartInvocation, EffectAcquire:
		for _, inv := range n.invocations {
			if inv.name != e.Name || (inv.resource != nil) != (e.Kind == EffectAcquire) {
				continue
			}

			run := m.serviceRunner(inv)
			if run == nil {
				return fmt.Errorf("%w: service %q in state %q", ErrNotBound, e.Name, e.State)
			}

			for ev := range run(ctx, e.Context, e.ID) {
				guardedSend(send, ev)
				if ctx.Err() != nil {
					break
				}
			}

			return nil
		}

		return fmt.Errorf("%w: service %q in state %q", ErrUnknownEffect, e.Name, e.State)
	case EffectStopInvocation, EffectStartTimer, EffectCancelTimer, EffectRelease:
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

	if fn, ok := m.actions[e.Name]; ok {
		if fn == nil {
			return fmt.Errorf("%w: action %q in state %q", ErrNotBound, e.Name, e.State)
		}

		fn(ctx, e.Context, Move[S]{Source: n.id, From: m.stateIDs(e.From), To: m.stateIDs(e.To), Event: e.Event})

		return nil
	}

	return fmt.Errorf("%w: action %q in state %q", ErrUnknownEffect, e.Name, e.State)
}

// release gives back a resource's value, as the EffectRelease e asks.
func (m *Machine[S, C]) release(ctx context.Context, e Effect[C], value any, closed bool) (err error) {
	defer func() {
		if p := recover(); p != nil {
			err = fmt.Errorf("%w: release of %q in state %q: %v\n%s", ErrPanicked, e.Name, e.State, p, debug.Stack())
		}
	}()

	n, ok := m.names[e.State]
	if !ok {
		return fmt.Errorf("%w: state %q", ErrUnknownEffect, e.State)
	}

	for _, inv := range n.invocations {
		if inv.name != e.Name || inv.resource == nil {
			continue
		}

		release := m.resourceRelease(inv)
		if release == nil {
			return fmt.Errorf("%w: resource %q in state %q", ErrNotBound, e.Name, e.State)
		}

		why := Leave[S]{Move: Move[S]{Source: n.id, From: m.stateIDs(e.From), To: m.stateIDs(e.To), Event: e.Event}, Closed: false}
		if closed {
			var none Move[S]
			why = Leave[S]{Move: none, Closed: true}
		}
		release(ctx, value, why)

		return nil
	}

	return fmt.Errorf("%w: resource %q in state %q", ErrUnknownEffect, e.Name, e.State)
}

// stateIDs turns state names back into identifiers, skipping names the
// chart no longer has.
func (m *Machine[S, C]) stateIDs(names []string) []S {
	ids := make([]S, 0, len(names))
	for _, name := range names {
		if n, ok := m.names[name]; ok {
			ids = append(ids, n.id)
		}
	}

	return ids
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

	// At is when a deadline timer fires.
	At time.Time `json:"at,omitzero"`

	// Jitter is the fraction by which the driver varies the wait.
	Jitter float64 `json:"jitter,omitempty"`

	// Context is the context an action or service must see. Timer and
	// stop effects carry none.
	Context json.RawMessage `json:"context,omitempty"`

	// EventName is the registered name of the triggering event's type.
	EventName string `json:"event_name,omitempty"`

	// Event is the triggering event.
	Event json.RawMessage `json:"event,omitempty"`

	// From names the states active before the action's step.
	From []string `json:"from,omitempty"`

	// To names the states active after it.
	To []string `json:"to,omitempty"`
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
		At:         e.At,
		Jitter:     e.Jitter,
		Context:    nil,
		EventName:  "",
		Event:      nil,
		From:       e.From,
		To:         e.To,
	}

	if e.Kind == EffectAction || e.Kind == EffectStartInvocation || e.Kind == EffectAcquire {
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
		At:      raw.At,
		Jitter:  raw.Jitter,
		Context: zero.Context,
		Event:   nil,
		From:    raw.From,
		To:      raw.To,
		Needs:   nil,
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
