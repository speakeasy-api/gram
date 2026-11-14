package sc

import (
	"context"
	"errors"
	"fmt"
	"reflect"
	"time"

	"golang.org/x/sync/semaphore"
)

// runOptions holds what the RunOption values passed to a driver set.
type runOptions struct {
	// hooks is a *Hooks[S, C] for the driver's S and C.
	hooks any

	services *semaphore.Weighted

	now func() time.Time

	manualTimers bool

	// emit is an unexported catch-all for emitted events, which a child
	// machine's runner uses to hand them to its parent, and listeners the
	// typed listeners given with Listen, by event type.
	emit      func(ctx context.Context, ev any)
	listeners map[reflect.Type][]func(ctx context.Context, ev any)

	throttle Waiter
}

// RunOption configures a driver in this package: an [Actor] or an
// [Instance].
type RunOption func(*runOptions)

// runSettings applies opts over the defaults.
func runSettings(opts []RunOption) runOptions {
	o := runOptions{hooks: nil, services: nil, now: time.Now, manualTimers: false, emit: nil, listeners: map[reflect.Type][]func(context.Context, any){}, throttle: nil}
	for _, opt := range opts {
		opt(&o)
	}

	return o
}

// hooksFor returns the hooks set by WithHooks, panicking when they are for
// another state or context type.
func hooksFor[S comparable, C any](o runOptions, machine string) *Hooks[S, C] {
	if o.hooks == nil {
		return nil
	}

	hooks, ok := o.hooks.(*Hooks[S, C])
	if !ok {
		panic(fmt.Sprintf("sc: WithHooks was given %T, which is not for machine %q", o.hooks, machine))
	}

	return hooks
}

// WithHooks makes the driver report to h. Building a driver panics when h is
// for another state or context type.
func WithHooks[S comparable, C any](h *Hooks[S, C]) RunOption {
	return func(o *runOptions) { o.hooks = h }
}

// WithClock sets the clock the driver passes to the core: the time every
// event is processed at, which guards, assignments and timers see. The
// default is [time.Now]. Drivers still wait for timers with the time
// package, so under a clock that does not follow real time, timers only fire
// when an event arrives after their due time, or when the driver is asked
// to advance. WithClock panics when now is nil.
func WithClock(now func() time.Time) RunOption {
	if now == nil {
		panic("sc: WithClock: the clock is nil")
	}

	return func(o *runOptions) { o.now = now }
}

// LimitServices makes each service run wait for one unit of sem before it
// starts and release it when it ends. Sharing one semaphore between drivers
// bounds how many services run at once across all of them, for example to
// stay within an external API's rate limit. A run still waiting when its
// state is left never starts.
func LimitServices(sem *semaphore.Weighted) RunOption {
	return func(o *runOptions) { o.services = sem }
}

// ManualTimers stops an [Instance] from waking itself when a timer falls
// due. Timers then fire only when the caller sends an event or calls
// [Instance.Advance], which suits an instance driven by the caller's own
// loop or by a clock that does not follow real time. The actor ignores it.
func ManualTimers() RunOption {
	return func(o *runOptions) { o.manualTimers = true }
}

// Listen makes the driver call fn with each event of type T the chart emits
// with [Transition.Emit] or [StateBuilder.EmitOnEntry], in order, on the
// goroutine processing the step that emitted it. T is inferred from fn;
// give one Listen per type of event to hear. Several listeners for one type
// are all called, in the order they were given. Building an instance with a
// listener for a type the chart never emits fails, so a listener that can
// never be called is caught at once.
//
// fn must not block or send events to the same instance; hand the event to a
// queue when the listener is slow. ctx is the context of the event that
// caused the step. A listener that panics fails like an action: the step
// stands, and the panic is returned from the send as [ErrPanicked].
func Listen[T any](fn func(ctx context.Context, ev T)) RunOption {
	return func(o *runOptions) {
		t := reflect.TypeFor[T]()
		o.listeners[t] = append(o.listeners[t], func(ctx context.Context, ev any) {
			if typed, ok := ev.(T); ok {
				fn(ctx, typed)
			}
		})
	}
}

// onEmit hands every emitted event to fn, whatever its type.
func onEmit(fn func(ctx context.Context, ev any)) RunOption {
	return func(o *runOptions) { o.emit = fn }
}

// listener returns what hears the chart's emitted events: the typed
// listeners by type, and then the catch-all. It is nil when there is
// neither.
func (o runOptions) listener() func(context.Context, any) {
	if len(o.listeners) == 0 && o.emit == nil {
		return nil
	}

	listeners, all := o.listeners, o.emit

	return func(ctx context.Context, ev any) {
		for _, fn := range listeners[reflect.TypeOf(ev)] {
			fn(ctx, ev)
		}
		if all != nil {
			all(ctx, ev)
		}
	}
}

// ErrNoSuchEmit is returned when a driver is built with a listener for a
// type of event the chart never emits.
var ErrNoSuchEmit = errors.New("listener for an event the chart never emits")

// checkListeners reports a listener for a type the chart never emits.
func (m *Machine[S, C]) checkListeners(o runOptions) error {
	for t := range o.listeners {
		if !m.EmitsType(t) {
			return fmt.Errorf("%w: %s", ErrNoSuchEmit, t)
		}
	}

	return nil
}

// Waiter is something a service run waits on before it starts, such as a
// *rate.Limiter from golang.org/x/time/rate. Wait returns once the run may
// go ahead, or an error when ctx ended first.
type Waiter interface {
	Wait(ctx context.Context) error
}

// ThrottleServices makes each service run wait on w before it starts.
// Sharing one rate limiter between drivers bounds how often services start
// across all of them, which [LimitServices] does for how many run at once. A
// run still waiting when its state is left never starts.
func ThrottleServices(w Waiter) RunOption {
	return func(o *runOptions) { o.throttle = w }
}
