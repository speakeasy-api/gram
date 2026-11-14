package sc

import (
	"context"
	"errors"
	"slices"
	"time"
)

// Hooks is a set of functions a driver calls while it runs machine instances,
// for logs, metrics, and tracing. Every field is optional. The hooks run on
// the driver's goroutines, so they must be safe for concurrent use, and they
// should return quickly: an [Actor] processes no further event until its
// hooks return.
//
// The sco11y subpackage builds Hooks that reports to OpenTelemetry.
type Hooks[S comparable, C any] struct {
	// Transition is called after each event an instance accepts, with the
	// snapshots before and after it and the effects it produced. ctx is
	// the context the event was sent with, so a span it carries becomes
	// the parent of what the hook records. For an event from a service, it
	// is the context EffectStarted returned for that service run.
	Transition func(ctx context.Context, step Step[S, C])

	// Ignored is called for an event that changed nothing: one no active
	// state handles, one about a timer or service run that is no longer
	// outstanding, or one sent to an instance that is done. Late results
	// are expected after a state is left, but a steady stream of them can
	// point at a driver delivering late.
	Ignored func(ctx context.Context, snap Snapshot[S, C], event any)

	// EffectStarted is called before an action runs or a service run
	// starts. The context it returns is the one the effect runs with, which
	// is how a span started here becomes the parent of the effect's own
	// calls. Returning nil keeps ctx.
	EffectStarted func(ctx context.Context, effect Effect[C]) context.Context

	// EffectFinished is called when an action or service run that
	// EffectStarted reported has ended, with the context EffectStarted
	// returned, how long the effect took, and the error it ended with. A
	// service that returned an error reports that error, and a service
	// stopped because its state was left reports the context's error.
	//
	// The transition caused by a service's result may be reported before
	// or after EffectFinished for that run, depending on the driver.
	EffectFinished func(ctx context.Context, effect Effect[C], elapsed time.Duration, err error)
}

// Observe reports a step: to Transition when it changed the instance, and
// to Ignored when it changed nothing. Drivers call it after each
// [Machine.Transition]. It does nothing on nil Hooks.
func (h *Hooks[S, C]) Observe(ctx context.Context, step Step[S, C]) {
	if h == nil {
		return
	}

	if !step.Accepted() {
		if h.Ignored != nil {
			h.Ignored(ctx, step.To, step.Event)
		}

		return
	}

	if h.Transition != nil {
		h.Transition(ctx, step)
	}
}

// Exec carries out an action or service effect with [Machine.Exec] and
// reports it to EffectStarted and EffectFinished. Drivers call it in place of
// Exec. now is the driver's clock reading, from which a service's
// [StateBuilder.Within] budget is measured; Exec applies the budget as a
// timeout on the service's context. send receives each event the effect
// produces, with the context the effect runs with, so the driver can hand
// that context to [Hooks.Observe]. On nil Hooks it only runs the effect.
func (h *Hooks[S, C]) Exec(ctx context.Context, m *Machine[S, C], e Effect[C], now time.Time, send func(ctx context.Context, ev any)) error {
	if budget, ok := e.Budget(now); ok {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, budget)
		defer cancel()

		// The budget holds whether or not the service heeds its context:
		// when it runs out the chart is told the service failed, and
		// whatever the service reports afterwards is ignored.
		if e.Kind == EffectStartInvocation {
			overdue := time.AfterFunc(budget, func() {
				send(ctx, Failed{ID: e.ID, Err: context.DeadlineExceeded})
			})
			defer overdue.Stop()
		}
	}

	if h != nil && h.EffectStarted != nil {
		if started := h.EffectStarted(ctx, e); started != nil {
			ctx = started
		}
	}

	// outcome is the error a service run ended with, and ended is set once
	// it reported an outcome at all.
	var outcome error
	ended := false

	begin := time.Now()
	err := m.Exec(ctx, e, func(ev any) {
		if r, ok := ev.(invocationResult); ok && r.invocationID() == e.ID {
			ended = true
			if f, failed := ev.(Failed); failed {
				outcome = f.Err
			}
		}

		send(ctx, ev)
	})
	elapsed := time.Since(begin)

	if h != nil && h.EffectFinished != nil {
		reported := err
		switch {
		case reported != nil:
		case outcome != nil:
			reported = outcome
		case e.Kind == EffectStartInvocation && !ended:
			reported = context.Cause(ctx)
			if reported == nil {
				reported = errServiceEndedEarly
			}
		}

		h.EffectFinished(ctx, e, elapsed, reported)
	}

	return err
}

// errServiceEndedEarly is reported for a service run that ended without a
// result although nothing stopped it. Exec never produces this for a
// service built with Invoke or Stream.
var errServiceEndedEarly = errors.New("service ended without reporting a result")

// joinedContexts carries, through an effect's context, the context each
// joined hook set's EffectStarted returned, so each EffectFinished gets its
// own.
type joinedContexts struct{}

// JoinHooks returns hooks that call each of hs in order, skipping nil ones,
// so that, for example, OpenTelemetry hooks and a caller's own logging can
// both observe one driver. Each EffectStarted receives the context the
// previous one returned, and each EffectFinished receives the context its
// own EffectStarted returned, in reverse order.
func JoinHooks[S comparable, C any](hs ...*Hooks[S, C]) *Hooks[S, C] {
	var set []*Hooks[S, C]
	for _, h := range hs {
		if h != nil {
			set = append(set, h)
		}
	}

	return &Hooks[S, C]{
		Transition: func(ctx context.Context, step Step[S, C]) {
			for _, h := range set {
				if h.Transition != nil {
					h.Transition(ctx, step)
				}
			}
		},
		Ignored: func(ctx context.Context, snap Snapshot[S, C], event any) {
			for _, h := range set {
				if h.Ignored != nil {
					h.Ignored(ctx, snap, event)
				}
			}
		},
		EffectStarted: func(ctx context.Context, effect Effect[C]) context.Context {
			own := make([]context.Context, len(set))
			for i, h := range set {
				if h.EffectStarted != nil {
					if started := h.EffectStarted(ctx, effect); started != nil {
						ctx = started
					}
				}
				own[i] = ctx
			}

			return context.WithValue(ctx, joinedContexts{}, own)
		},
		EffectFinished: func(ctx context.Context, effect Effect[C], elapsed time.Duration, err error) {
			own, _ := ctx.Value(joinedContexts{}).([]context.Context)
			for i, s := range slices.Backward(set) {
				if s.EffectFinished == nil {
					continue
				}

				hookCtx := ctx
				if i < len(own) {
					hookCtx = own[i]
				}
				s.EffectFinished(hookCtx, effect, elapsed, err)
			}
		},
	}
}
