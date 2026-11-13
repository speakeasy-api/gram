package sc

import (
	"context"
	"iter"
	"reflect"
)

// childEnded is what the goroutine waiting for a child machine puts in the
// queue of its events once the child is done or the wait is over.
type childEnded struct {
	err error
}

// InvokeMachine declares a service that runs another machine as a child for
// as long as the state is active. The child starts with the context input
// returns for the parent's, and when it reaches a top-level final state the
// service succeeds with what output returns for the child's last snapshot,
// or fails with output's error, which is how a child that ended badly
// reaches the parent's OnError.
//
// The two talk in events. The parent sends to the child with
// [Transition.SendTo], naming this service; the child sends to the parent
// with [Transition.Emit], and the parent handles those events with On like
// any other. Leaving the state closes the child.
//
// The child runs under an [Instance] with the parent driver's clock and
// opts. Its snapshot is not part of the parent's: when the parent is resumed
// from a stored snapshot, the child starts again from input, so its work
// must be safe to repeat. In an exploration of the parent the child is a
// service like any other, with the outcomes given for name; explore the
// child on its own.
func (s *StateBuilder[S, C]) InvokeMachine[S2 comparable, C2, R any](name string, child *Machine[S2, C2], input func(C) C2, output func(Snapshot[S2, C2]) (R, error), opts ...RunOption) *Invocation[S, C, R] {
	inv := &invocation[S, C]{
		state:    s.n,
		name:     name,
		doneType: reflect.TypeFor[Done[R]](),
		run:      runChild(child, input, output, opts),
		resource: nil,
		retry:    nil,
	}
	s.n.invocations = append(s.n.invocations, inv)

	return &Invocation[S, C, R]{b: s.b, n: s.n, inv: inv}
}

// runChild is the service that runs child for a parent.
func runChild[C any, S2 comparable, C2, R any](child *Machine[S2, C2], input func(C) C2, output func(Snapshot[S2, C2]) (R, error), opts []RunOption) func(context.Context, C, string) iter.Seq[any] {
	return func(ctx context.Context, c C, id string) iter.Seq[any] {
		return func(yield func(any) bool) {
			// The child's events queue here, so that neither machine waits
			// for the other while it is busy.
			up := newInbox()

			settings := append([]RunOption{WithClock(clockFrom(ctx))}, opts...)
			settings = append(settings, OnEmit(func(_ context.Context, ev any) { up.put(ev) }))

			inst, err := child.Instance(ctx, input(c), settings...)
			if err != nil {
				yield(Failed{ID: id, Err: err})
				return
			}
			defer inst.Close()

			ctx, stop := context.WithCancel(ctx)
			defer stop()

			go func() {
				for ev := range Inbox(ctx) {
					// An action's error has no caller here; the child's
					// hooks see it.
					_, _ = inst.Send(ctx, ev)
				}
			}()

			go func() {
				_, err := inst.Await(ctx, func(snap Snapshot[S2, C2]) bool { return snap.Done() })
				up.put(childEnded{err: err})
			}()

			for ev := range up.events(ctx) {
				ended, ok := ev.(childEnded)
				if !ok {
					if !yield(Emitted{ID: id, Event: ev}) {
						return
					}

					continue
				}

				// The wait only fails when the service was stopped.
				if ended.err != nil {
					return
				}

				result, err := output(inst.Snapshot())
				if err != nil {
					yield(Failed{ID: id, Err: err})
					return
				}

				yield(Done[R]{ID: id, Result: result})

				return
			}
		}
	}
}
