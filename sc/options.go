package sc

import (
	"context"
	"fmt"
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

	emit func(ctx context.Context, ev any)

	throttle Waiter
}

// RunOption configures a driver in this package: an [Actor] or an
// [Instance].
type RunOption func(*runOptions)

// runSettings applies opts over the defaults.
func runSettings(opts []RunOption) runOptions {
	o := runOptions{hooks: nil, services: nil, now: time.Now, manualTimers: false, emit: nil, throttle: nil}
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

// OnEmit makes the driver call fn with each event the chart emits with
// [Transition.Emit], in order, on the goroutine processing the step that
// emitted it. fn must not block or send events to the same instance; hand
// the event to a queue when the listener is slow. ctx is the context of the
// event that caused the step.
func OnEmit(fn func(ctx context.Context, ev any)) RunOption {
	return func(o *runOptions) { o.emit = fn }
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
