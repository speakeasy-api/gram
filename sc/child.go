package sc

import (
	"context"
	"encoding/json"
	"fmt"
	"iter"
	"reflect"
	"sync"
)

// childEnded is what the goroutine watching a child machine puts in the
// queue of its events once the child is done or the watch is over, and
// childMoved what it puts there when the child's snapshot has changed.
type (
	childEnded struct{ err error }
	childMoved struct{}
)

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
// opts. Its snapshot is kept inside the parent's, as the service's
// [Checkpoint], and replaced whenever the child moves, so each step of the
// child is also a step of the parent. A parent resumed from a stored
// snapshot continues its child from where the child was: the child's timers
// and unfinished services start again, and it does not run its entry
// actions a second time. An event the child emitted just before a crash may
// be emitted again.
//
// In an exploration of the parent the child is a service like any other,
// with the outcomes given for name; explore the child on its own.
func (s *StateBuilder[S, C]) InvokeMachine[S2 comparable, C2, R any](name string, child *Machine[S2, C2], input func(C) C2, output func(Snapshot[S2, C2]) (R, error), opts ...RunOption) *Invocation[S, C, R] {
	inv := &invocation[S, C]{
		state:    s.n,
		name:     name,
		doneType: reflect.TypeFor[Done[R]](),
		run:      runChild(child, func(c C, _ string) C2 { return input(c) }, output, opts),
		resource: nil,
		retry:    nil,
		spawned:  false,
	}
	s.n.invocations = append(s.n.invocations, inv)

	return &Invocation[S, C, R]{b: s.b, n: s.n, inv: inv}
}

// SpawnMachine declares a child machine like [StateBuilder.InvokeMachine],
// spawned as [Invocation.Spawned] describes: any number run at once, each
// under the key its [Transition.Spawn] gives, which input receives so that
// each child can start with its own context. The parent sends to one child
// with [Transition.SendToKey], and tells the children's results apart with
// [SpawnKey] on their IDs. An event a child emits arrives as a plain event,
// so a child that must be told apart puts its identity, which input gave
// it, in the events it emits.
func (s *StateBuilder[S, C]) SpawnMachine[S2 comparable, C2, R any](name string, child *Machine[S2, C2], input func(c C, key string) C2, output func(Snapshot[S2, C2]) (R, error), opts ...RunOption) *Invocation[S, C, R] {
	inv := &invocation[S, C]{
		state:    s.n,
		name:     name,
		doneType: reflect.TypeFor[Done[R]](),
		run:      runChild(child, input, output, opts),
		resource: nil,
		retry:    nil,
		spawned:  true,
	}
	s.n.invocations = append(s.n.invocations, inv)

	return &Invocation[S, C, R]{b: s.b, n: s.n, inv: inv}
}

// runChild is the service that runs child for a parent.
func runChild[C any, S2 comparable, C2, R any](child *Machine[S2, C2], input func(C, string) C2, output func(Snapshot[S2, C2]) (R, error), opts []RunOption) func(context.Context, C, string) iter.Seq[any] {
	return func(ctx context.Context, c C, id string) iter.Seq[any] {
		return func(yield func(any) bool) {
			// The child's events queue here, so that neither machine waits
			// for the other while it is busy.
			// It needs no limit of its own: every step of the child is
			// saved through it, so the child cannot run ahead unseen.
			up := newInbox(0)

			settings := append([]RunOption{WithClock(clockFrom(ctx))}, opts...)

			// Under a caller that drives time itself, the child's timers
			// are driven with the parent's.
			manual, _ := ctx.Value(manualClock{}).(bool)
			if manual {
				settings = append(settings, ManualTimers())
			}

			// What the child emits during a step waits until that step's
			// snapshot has been handed up, so the parent never handles an
			// event from a child it still sees in the state before.
			var (
				mu      sync.Mutex
				emitted []any
			)
			settings = append(settings, OnEmit(func(_ context.Context, ev any) {
				mu.Lock()
				defer mu.Unlock()

				emitted = append(emitted, ev)
			}))

			// moved hands up the child's snapshot, then what it emitted
			// on the way there.
			moved := func() {
				mu.Lock()
				defer mu.Unlock()

				up.put(childMoved{})
				for _, ev := range emitted {
					up.put(ev)
				}
				emitted = nil
			}

			inst, _, err := startChild(ctx, child, input(c, keyOf(id)), settings)
			if err != nil {
				yield(Failed{ID: id, Err: err})
				return
			}
			defer inst.Close()

			// The child's snapshot is handed up at once: a new child must
			// be saved so that a resumed parent does not start it a second
			// time, and either kind may already have taken a step, on a
			// timer or a service of its own. The watcher below counts from
			// the snapshot read before that, so no step falls between.
			last := inst.Snapshot().Seq()
			moved()

			ctx, stop := context.WithCancel(ctx)
			defer stop()

			go func() {
				box, ok := ctx.Value(serviceInbox{}).(*inbox)
				if !ok {
					return
				}

				// An action's error has no caller here; the child's hooks
				// see it.
				for ev := range box.events(ctx) {
					if _, moved := ev.(advance); moved {
						_, _ = inst.Advance(ctx)
						continue
					}

					_, _ = inst.Send(ctx, ev)
				}
			}()

			go func() {
				for {
					snap, err := inst.Await(ctx, func(snap Snapshot[S2, C2]) bool { return snap.Seq() != last || snap.Done() })
					if err != nil {
						up.put(childEnded{err: err})
						return
					}

					if snap.Seq() != last {
						last = snap.Seq()
						moved()
					}

					if snap.Done() {
						up.put(childEnded{err: nil})
						return
					}
				}
			}()

			for ev := range up.events(ctx) {
				var next any = Emitted{ID: id, Event: ev}

				if _, moved := ev.(childMoved); moved {
					data, err := json.Marshal(inst.Snapshot())
					if err != nil {
						yield(Failed{ID: id, Err: fmt.Errorf("save child machine: %w", err)})
						return
					}

					next = Checkpointed{ID: id, Data: data}
				}

				ended, ok := ev.(childEnded)
				if !ok {
					if !yield(next) {
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

// startChild starts the child, or continues it from the snapshot an earlier
// run of the service saved, and reports which.
func startChild[S2 comparable, C2 any](ctx context.Context, child *Machine[S2, C2], c C2, settings []RunOption) (*Instance[S2, C2], bool, error) {
	saved, _ := ctx.Value(savedProgress{}).(json.RawMessage)
	if len(saved) == 0 {
		inst, err := child.Instance(ctx, c, settings...)

		return inst, false, err
	}

	snap, _, err := child.Restore(saved, clockFrom(ctx)())
	if err != nil {
		return nil, false, fmt.Errorf("restore child machine: %w", err)
	}

	inst, err := child.InstanceFrom(ctx, snap, settings...)

	return inst, true, err
}
