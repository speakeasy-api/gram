package sc

import (
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
}

// RunOption configures a driver in this package: an [Actor] or an
// [Instance].
type RunOption func(*runOptions)

// runSettings applies opts over the defaults.
func runSettings(opts []RunOption) runOptions {
	o := runOptions{hooks: nil, services: nil, now: time.Now, manualTimers: false}
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
// to advance.
func WithClock(now func() time.Time) RunOption {
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
