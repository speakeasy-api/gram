package sc_test

// Ports of XState's packages/core/test/errors.test.ts, errorPrecedence.test.ts
// and timeout.test.ts (6.0.0-beta.2), to check how sc treats the errors of
// actions, guards and invoked services, and its time limits, against XState.
// Each test names the XState case it ports. Cases that test an XState-only
// API are listed at the end of the file with the reason they were not ported.
//
// The two error models differ in ways that shape every port:
//
//   - An XState actor has an "error" status: an error nothing handles stops
//     the actor and its children, and goes to the actor's error observers or
//     to a global reporter. An sc instance has no such status and no error
//     observers. What it has is the error a driver call returns
//     (Machine.Instance, Instance.Send, Actor.Run) and the Hooks.
//   - An XState state can declare onError, which catches what its actions,
//     transition functions and invoked actors throw. sc has no state-level
//     onError. A service's failure, or its panic, arrives as sc.Failed at
//     the OnError transition its invocation must declare. An action cannot
//     fail a transition: its panic, or an OnEmit listener's, comes back
//     from an Instance as an error wrapping sc.ErrPanicked after the
//     transition was taken, and the rest of the step is still carried out.
//     An Actor has no caller to hand it to and goes on running; only a
//     failing initial action makes its Run return the error. A guard or an
//     assignment is a plain function, and its panic propagates out of
//     Machine.Transition.
//   - XState's callback actor is an sc stream that blocks until its context
//     ends and counts its cleanup on the way out; a callback that throws as
//     it starts is a stream that panics. XState's promise actor is Invoke.
//   - What an XState root declares for itself is declared on Builder.Chart,
//     and a machine that declares no states is a chart without states.
//   - XState's state timeout and onTimeout are an sc delayed transition
//     (After, Delay). Its invoke timeout is StateBuilder.Within, whose
//     expiry reaches the chart as the service's Failed carrying
//     context.DeadlineExceeded.

import (
	"context"
	"encoding/json"
	"errors"
	"iter"
	"sync"
	"sync/atomic"
	"testing"
	"testing/synctest"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/sc"
)

type (
	xerrFail        struct{}
	xerrNext        struct{}
	xerrActivate    struct{}
	xerrDo          struct{}
	xerrSpawn       struct{}
	xerrSomeEvent   struct{}
	xerrEmitted     struct{ Foo string }
	xerrPing        struct{}
	xerrMoved       struct{}
	xerrApprove     struct{}
	xerrInvokeDone  struct{}
	xerrActivateFor struct{ Duration int }
)

type xerrBuilder = sc.Builder[string, counter]

// xerrReason is the context of XState's cross-state patch charts.
type xerrReason struct {
	Reason string `json:"reason"`
}

// xerrSLA is a context holding a time limit in milliseconds.
type xerrSLA struct {
	Millis int `json:"millis"`
}

var (
	errXerrParent   = errors.New("parent failed")
	errXerrEffect   = errors.New("effect failed")
	errXerrCleanup  = errors.New("cleanup failed")
	errXerrOops     = errors.New("oops")
	errXerrBoom     = errors.New("boom")
	errXerrObserver = errors.New("observer")
	errXerrListener = errors.New("listener")
	errXerrStart    = errors.New("sync_error_in_actor_start")
	errXerrReject   = errors.New("rejection_in_promise_actor")
	errXerrEntry    = errors.New("error_thrown_in_entry_action")
)

// xerrSeen is one error something reported, with the name it came under.
type xerrSeen struct {
	name string
	err  error
}

// xerrBox collects reported errors from any goroutine.
type xerrBox struct {
	mu   sync.Mutex
	seen []xerrSeen
}

func xerrNewBox() *xerrBox {
	return &xerrBox{mu: sync.Mutex{}, seen: nil}
}

func (b *xerrBox) put(name string, err error) {
	b.mu.Lock()
	defer b.mu.Unlock()

	b.seen = append(b.seen, xerrSeen{name: name, err: err})
}

func (b *xerrBox) all() []xerrSeen {
	b.mu.Lock()
	defer b.mu.Unlock()

	return append([]xerrSeen(nil), b.seen...)
}

// only returns the one error the box holds.
func (b *xerrBox) only(t *testing.T) xerrSeen {
	t.Helper()

	seen := b.all()
	require.Len(t, seen, 1)

	return seen[0]
}

// xerrRecord is an action for an OnError transition that keeps the failure.
func xerrRecord[C any](box *xerrBox) func(context.Context, C, sc.Failed) {
	return func(_ context.Context, _ C, f sc.Failed) { box.put(f.ID, f.Err) }
}

// xerrServiceEnds returns hooks that keep the error each service run ended
// with, under the service's name.
func xerrServiceEnds(box *xerrBox) *sc.Hooks[string, counter] {
	return &sc.Hooks[string, counter]{
		Transition:    nil,
		Ignored:       nil,
		EffectStarted: nil,
		EffectFinished: func(_ context.Context, e sc.Effect[counter], _ time.Duration, err error) {
			if e.Kind == sc.EffectStartInvocation {
				box.put(e.Name, err)
			}
		},
	}
}

// xerrActionEnds returns hooks that keep the error each action that failed
// ended with, under the action's name.
func xerrActionEnds(box *xerrBox) *sc.Hooks[string, counter] {
	return &sc.Hooks[string, counter]{
		Transition:    nil,
		Ignored:       nil,
		EffectStarted: nil,
		EffectFinished: func(_ context.Context, e sc.Effect[counter], _ time.Duration, err error) {
			if e.Kind == sc.EffectAction && err != nil {
				box.put(e.Name, err)
			}
		},
	}
}

// xerrCallback stands for an XState callback actor that returns a cleanup:
// it runs until it is stopped and counts its cleanup then.
func xerrCallback[C any](cleanup *atomic.Int32) func(context.Context, C) iter.Seq2[any, error] {
	return func(ctx context.Context, _ C) iter.Seq2[any, error] {
		return func(func(any, error) bool) {
			defer cleanup.Add(1)

			<-ctx.Done()
		}
	}
}

// xerrThrows stands for an XState callback actor that throws as it starts.
func xerrThrows[C any](err error) func(context.Context, C) iter.Seq2[any, error] {
	return func(context.Context, C) iter.Seq2[any, error] { panic(err) }
}

// xerrRejects stands for an XState promise actor that rejects with err.
func xerrRejects[C any](err error) func(context.Context, C) (int, error) {
	return func(context.Context, C) (int, error) { return 0, err }
}

// xerrRejectsWhen rejects once gate is closed, so a test can let the
// services beside it start first.
func xerrRejectsWhen(gate <-chan struct{}, err error) func(context.Context, counter) (int, error) {
	return func(ctx context.Context, _ counter) (int, error) {
		select {
		case <-gate:
			return 0, err
		case <-ctx.Done():
			return 0, context.Cause(ctx)
		}
	}
}

// xerrSleep moves the synctest clock on by d and lets what that woke settle.
func xerrSleep(d time.Duration) {
	time.Sleep(d) //nolint:forbidigo // GG013: advances the synctest fake clock instantly (the only way to move past a timer inside a synctest.Test bubble); this exemption is valid ONLY within synctest.Test — do not copy it to a real-time time.Sleep
	synctest.Wait()
}

// xerrOutcome is how an actor's Run ended: with an error, or by panicking.
type xerrOutcome struct {
	err      error
	panicked any
}

// xerrRun runs the actor on a goroutine of its own, as runActor does, and
// also catches a panic that comes out of Run.
func xerrRun[S comparable, C any](ctx context.Context, a *sc.Actor[S, C]) (wait func() xerrOutcome) {
	result := make(chan xerrOutcome, 1)

	go func() {
		out := xerrOutcome{err: nil, panicked: nil}
		defer func() {
			out.panicked = recover()
			result <- out
		}()

		out.err = a.Run(ctx)
	}()

	return func() xerrOutcome { return <-result }
}

// xerrFamily is the parent and child of XState's "stops descendants" case,
// with what its actions count.
type xerrFamily struct {
	parent *sc.Machine[string, counter]
	child  *sc.Machine[string, counter]

	cleanup *atomic.Int32
	exit    *atomic.Int32
	timer   *atomic.Int32
}

func xerrNewFamily(t *testing.T, failure string) xerrFamily {
	t.Helper()

	fam := xerrFamily{parent: nil, child: nil, cleanup: new(atomic.Int32), exit: new(atomic.Int32), timer: new(atomic.Int32)}

	child, err := sc.New("child", func(b *xerrBuilder) {
		b.Initial("running")
		running := b.State("running").Exit("exit", func(context.Context, counter) { fam.exit.Add(1) })
		running.Stream("callback", xerrCallback[counter](fam.cleanup)).OnError().Stay()
		running.After(10*time.Millisecond).Do("timer", func(context.Context, counter, struct{}) { fam.timer.Add(1) })
	})
	require.NoError(t, err)

	parent, err := sc.New("parent", func(b *xerrBuilder) {
		b.Chart().InvokeMachine("child", child,
			func(c counter) counter { return c },
			func(sc.Snapshot[string, counter]) (int, error) { return 0, nil },
		).OnError().Stay()

		if failure == "effect" {
			b.On[xerrFail]().Do("throw", func(context.Context, counter, xerrFail) { panic(errXerrParent) })
			return
		}

		b.On[xerrFail]().Assign("throw", func(counter, xerrFail) counter { panic(errXerrParent) })
	})
	require.NoError(t, err)

	fam.parent, fam.child = parent, child

	return fam
}

// errors.test.ts: error handling > stops descendants after a parent %s fails
// (run for "calculation", a transition function that throws, and "effect",
// an enqueued action that throws).
//
// DIVERGES from XState: XState puts the actor in its "error" status either
// way, stops its descendants and tells its error observer. In sc the two
// failures are different things. A throwing calculation is a panic in an
// assignment, which comes out of an Actor's Run as a panic; Run's own
// cleanup still stops the services on the way, which is XState's outcome. A
// throwing effect is an action's panic, which fails no transition: an
// Instance returns it from Send wrapping sc.ErrPanicked, an Actor only shows
// it to its hooks, and either carries on with its child still running.
func TestXErrErrorsStopsDescendantsAfterAParentFails(t *testing.T) {
	t.Parallel()

	t.Run("calculation under an actor", func(t *testing.T) {
		t.Parallel()

		synctest.Test(t, func(t *testing.T) {
			fam := xerrNewFamily(t, "calculation")

			a := fam.parent.Actor(counter{})
			wait := xerrRun(t.Context(), a)
			synctest.Wait()

			kid, ok := a.Snapshot().Child(fam.child, "child")
			require.True(t, ok)
			require.Equal(t, []string{"running"}, kid.Active())

			require.NoError(t, a.Send(t.Context(), xerrFail{}))
			out := wait()
			xerrSleep(20 * time.Millisecond)

			require.Equal(t, errXerrParent, out.panicked)
			require.NoError(t, out.err)
			require.ErrorIs(t, a.Send(t.Context(), xerrFail{}), sc.ErrActorStopped)
			require.Equal(t, int32(1), fam.cleanup.Load())
			require.Zero(t, fam.exit.Load())
			require.Zero(t, fam.timer.Load())
		})
	})

	t.Run("effect under an actor", func(t *testing.T) {
		t.Parallel()

		synctest.Test(t, func(t *testing.T) {
			fam := xerrNewFamily(t, "effect")

			ctx, cancel := context.WithCancel(t.Context())
			defer cancel()

			a := fam.parent.Actor(counter{})
			wait := xerrRun(ctx, a)
			synctest.Wait()

			require.NoError(t, a.Send(t.Context(), xerrFail{}))
			xerrSleep(20 * time.Millisecond)

			// The actor is not stopped, and neither is its child, whose
			// timer fires.
			require.NoError(t, a.Send(t.Context(), xerrNext{}))
			kid, ok := a.Snapshot().Child(fam.child, "child")
			require.True(t, ok)
			require.Equal(t, []string{"running"}, kid.Active())
			require.Zero(t, fam.cleanup.Load())
			require.Zero(t, fam.exit.Load())
			require.Equal(t, int32(1), fam.timer.Load())

			cancel()
			out := wait()
			require.Nil(t, out.panicked)
			require.ErrorIs(t, out.err, context.Canceled)
		})
	})

	t.Run("effect under an instance", func(t *testing.T) {
		t.Parallel()

		synctest.Test(t, func(t *testing.T) {
			fam := xerrNewFamily(t, "effect")

			inst, err := fam.parent.Instance(t.Context(), counter{})
			require.NoError(t, err)
			defer inst.Close()
			synctest.Wait()

			_, err = inst.Send(t.Context(), xerrFail{})
			require.ErrorIs(t, err, sc.ErrPanicked)
			require.ErrorContains(t, err, "parent failed")
			xerrSleep(20 * time.Millisecond)

			// The instance is not stopped, and neither is its child, whose
			// timer fires.
			kid, ok := inst.Snapshot().Child(fam.child, "child")
			require.True(t, ok)
			require.Equal(t, []string{"running"}, kid.Active())
			require.Zero(t, fam.cleanup.Load())
			require.Zero(t, fam.exit.Load())
			require.Equal(t, int32(1), fam.timer.Load())
		})
	})
}

// errors.test.ts: error handling > stops a removed child exactly once when
// its stop effect ran: %s (run with the stop before and after the action
// that throws).
//
// Only a spawned service can be stopped by a transition in sc, and nothing
// spawns when a chart starts, so an event spawns the child first. The spy on
// XState's runtime stopActor is dropped.
func TestXErrErrorsStopsARemovedChildExactlyOnceWhenItsStopEffectRan(t *testing.T) {
	t.Parallel()

	for _, test := range []struct {
		name         string
		stoppedFirst bool
	}{{name: "false", stoppedFirst: false}, {name: "true", stoppedFirst: true}} {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			synctest.Test(t, func(t *testing.T) {
				var cleanup atomic.Int32

				key := func(counter, xerrFail) string { return "child" }

				m, err := sc.New("stopper", func(b *xerrBuilder) {
					b.Chart().Stream("child", xerrCallback[counter](&cleanup)).Spawned().OnError().Stay()
					b.On[xerrSpawn]().Spawn("start", "child", func(counter, xerrSpawn) string { return "child" })

					fail := b.On[xerrFail]()
					if test.stoppedFirst {
						fail.StopSpawned("stop", "child", key)
					}
					fail.Do("throw", func(context.Context, counter, xerrFail) { panic(errXerrEffect) })
					if !test.stoppedFirst {
						fail.StopSpawned("stop", "child", key)
					}
				})
				require.NoError(t, err)

				ctx, cancel := context.WithCancel(t.Context())
				defer cancel()

				a := m.Actor(counter{})
				wait := xerrRun(ctx, a)

				_, err = a.Call(t.Context(), xerrSpawn{})
				require.NoError(t, err)
				synctest.Wait()
				require.Len(t, a.Snapshot().Services(), 1)

				// The throw does not hold up the stop, whichever side of
				// it the stop is on.
				_, err = a.Call(t.Context(), xerrFail{})
				require.NoError(t, err)
				synctest.Wait()

				require.Empty(t, a.Snapshot().Services())
				require.Equal(t, int32(1), cleanup.Load())

				cancel()
				out := wait()
				require.ErrorIs(t, out.err, context.Canceled)
				require.Equal(t, int32(1), cleanup.Load())
			})
		})
	}
}

// errors.test.ts: error handling > uses runtime child stops and continues
// after a child cleanup throws
//
// The order of XState's runtime stopActor calls is dropped: an actor stops
// its services together.
//
// DIVERGES from XState: XState tells the error observer of the transition
// function's error. sc's assignment panics out of Actor.Run with it. The
// services are still stopped, and the cleanup that panics is contained.
func TestXErrErrorsUsesRuntimeChildStopsAndContinuesAfterAChildCleanupThrows(t *testing.T) {
	t.Parallel()

	synctest.Test(t, func(t *testing.T) {
		var cleanup atomic.Int32

		m, err := sc.New("stops", func(b *xerrBuilder) {
			b.Chart().Stream("bad", func(ctx context.Context, _ counter) iter.Seq2[any, error] {
				return func(func(any, error) bool) {
					defer func() { panic(errXerrCleanup) }()

					<-ctx.Done()
				}
			}).OnError().Stay()
			b.Chart().Stream("good", xerrCallback[counter](&cleanup)).OnError().Stay()
			b.On[xerrFail]().Assign("throw", func(counter, xerrFail) counter { panic(errXerrParent) })
		})
		require.NoError(t, err)

		a := m.Actor(counter{})
		wait := xerrRun(t.Context(), a)
		synctest.Wait()
		require.Len(t, a.Snapshot().Services(), 2)

		require.NoError(t, a.Send(t.Context(), xerrFail{}))
		out := wait()
		synctest.Wait()

		require.Equal(t, errXerrParent, out.panicked)
		require.Equal(t, int32(1), cleanup.Load())
	})
}

// xerrUnhandled checks that the chart def declares, which leaves a failing
// service's error unrouted as XState's chart does, is not a chart sc
// accepts.
func xerrUnhandled(t *testing.T, def func(b *xerrBuilder)) {
	t.Helper()

	_, err := sc.New("unhandled", def)
	require.ErrorContains(t, err, `service "failing" has no OnError transition`)
}

// errors.test.ts: error handling > unhandled invoked actor errors > stops
// sibling invoked actors at the root
//
// DIVERGES from XState: XState lets the failing actor's error go unhandled,
// which puts the machine in its "error" status and stops the siblings. sc
// rejects a chart whose service declares no OnError. A chart that ends on
// the failure says so, by routing it to a top-level final state, and that
// stops the sibling.
func TestXErrErrorsUnhandledInvokedActorErrorsStopsSiblingInvokedActorsAtTheRoot(t *testing.T) {
	t.Parallel()

	xerrUnhandled(t, func(b *xerrBuilder) {
		b.Chart().Invoke("failing", xerrRejects[counter](errXerrOops))
	})

	synctest.Test(t, func(t *testing.T) {
		var cleanup atomic.Int32

		gate := make(chan struct{})
		box := xerrNewBox()

		m, err := sc.New("root", func(b *xerrBuilder) {
			b.Chart().Stream("callback", xerrCallback[counter](&cleanup)).OnError().Stay()
			b.Chart().Invoke("failing", xerrRejectsWhen(gate, errXerrOops)).OnError().Do("record", xerrRecord[counter](box)).To("failed")
			// As XState's machine has no states, the chart starts in none: its
			// only state is the final one the failure leads to.
			b.Final("failed")
		})
		require.NoError(t, err)

		inst, err := m.Instance(t.Context(), counter{})
		require.NoError(t, err)
		defer inst.Close()
		synctest.Wait()
		require.Len(t, inst.Snapshot().Services(), 2)

		close(gate)
		synctest.Wait()

		snap := inst.Snapshot()
		require.True(t, snap.Done())
		require.Equal(t, errXerrOops, box.only(t).err)
		require.Equal(t, int32(1), cleanup.Load())
		require.Empty(t, snap.Services())
	})
}

// errors.test.ts: error handling > unhandled invoked actor errors > stops
// sibling invoked actors of a state
//
// DIVERGES from XState: as for the case before it, the chart must route the
// failure; routed to a top-level final state, it stops the sibling.
func TestXErrErrorsUnhandledInvokedActorErrorsStopsSiblingInvokedActorsOfAState(t *testing.T) {
	t.Parallel()

	xerrUnhandled(t, func(b *xerrBuilder) {
		b.Initial("working")
		b.State("working").Invoke("failing", xerrRejects[counter](errXerrOops))
	})

	synctest.Test(t, func(t *testing.T) {
		var cleanup atomic.Int32

		gate := make(chan struct{})

		m, err := sc.New("root", func(b *xerrBuilder) {
			b.Initial("working")
			working := b.State("working")
			working.Stream("callback", xerrCallback[counter](&cleanup)).OnError().Stay()
			working.Invoke("failing", xerrRejectsWhen(gate, errXerrOops)).OnError().To("failed")
			b.Final("failed")
		})
		require.NoError(t, err)

		inst, err := m.Instance(t.Context(), counter{})
		require.NoError(t, err)
		defer inst.Close()
		synctest.Wait()

		close(gate)
		synctest.Wait()

		snap := inst.Snapshot()
		require.True(t, snap.Done())
		require.Equal(t, int32(1), cleanup.Load())
		require.Empty(t, snap.Services())
	})
}

// errors.test.ts: error handling > unhandled invoked actor errors > stops
// spawned actors
//
// XState spawns the child as the context is created. Nothing spawns when an
// sc chart starts, so an event spawns it.
//
// DIVERGES from XState: as for the cases before it, the chart must route the
// failure; routed to a top-level final state, it stops the spawned service.
func TestXErrErrorsUnhandledInvokedActorErrorsStopsSpawnedActors(t *testing.T) {
	t.Parallel()

	synctest.Test(t, func(t *testing.T) {
		var cleanup atomic.Int32

		gate := make(chan struct{})

		m, err := sc.New("root", func(b *xerrBuilder) {
			b.Chart().Stream("spawned", xerrCallback[counter](&cleanup)).Spawned().OnError().Stay()
			b.On[xerrSpawn]().Spawn("spawn", "spawned", func(counter, xerrSpawn) string { return "spawned" })
			b.Chart().Invoke("failing", xerrRejectsWhen(gate, errXerrOops)).OnError().To("failed")
			// As XState's machine has no states, the chart starts in none: its
			// only state is the final one the failure leads to.
			b.Final("failed")
		})
		require.NoError(t, err)

		inst, err := m.Instance(t.Context(), counter{})
		require.NoError(t, err)
		defer inst.Close()

		_, err = inst.Send(t.Context(), xerrSpawn{})
		require.NoError(t, err)
		synctest.Wait()
		require.Len(t, inst.Snapshot().Services(), 2)
		require.Zero(t, cleanup.Load())

		close(gate)
		synctest.Wait()

		snap := inst.Snapshot()
		require.True(t, snap.Done())
		require.Equal(t, int32(1), cleanup.Load())
		require.Empty(t, snap.Services())
	})
}

// errors.test.ts: error handling > unhandled invoked actor errors > stops
// grandchildren and their timers
//
// DIVERGES from XState: as for the cases before it, the chart must route the
// failure; routed to a top-level final state, it stops the child machine,
// the service that machine runs, and its timer.
func TestXErrErrorsUnhandledInvokedActorErrorsStopsGrandchildrenAndTheirTimers(t *testing.T) {
	t.Parallel()

	synctest.Test(t, func(t *testing.T) {
		var cleanup, timer atomic.Int32

		gate := make(chan struct{})

		child, err := sc.New("child", func(b *xerrBuilder) {
			b.Chart().Stream("grandchild", xerrCallback[counter](&cleanup)).OnError().Stay()
			b.Chart().After(10*time.Millisecond).Do("timer", func(context.Context, counter, struct{}) { timer.Add(1) })
		})
		require.NoError(t, err)

		m, err := sc.New("root", func(b *xerrBuilder) {
			b.Chart().InvokeMachine("child", child,
				func(c counter) counter { return c },
				func(sc.Snapshot[string, counter]) (int, error) { return 0, nil },
			).OnError().Stay()
			b.Chart().Invoke("failing", xerrRejectsWhen(gate, errXerrOops)).OnError().To("failed")
			// As XState's machine has no states, the chart starts in none: its
			// only state is the final one the failure leads to.
			b.Final("failed")
		})
		require.NoError(t, err)

		inst, err := m.Instance(t.Context(), counter{})
		require.NoError(t, err)
		defer inst.Close()
		synctest.Wait()

		kid, ok := inst.Snapshot().Child(child, "child")
		require.True(t, ok)
		require.Len(t, kid.Services(), 1)

		close(gate)
		synctest.Wait()
		xerrSleep(20 * time.Millisecond)

		require.True(t, inst.Snapshot().Done())
		require.Equal(t, int32(1), cleanup.Load())
		require.Zero(t, timer.Load())
	})
}

// xerrActivation is XState's two-state machine that an "activate" event
// moves from initial to active, where "do" runs an action.
func xerrActivation(t *testing.T, spy *atomic.Int32) *sc.Machine[string, counter] {
	t.Helper()

	m, err := sc.New("machine", func(b *xerrBuilder) {
		b.Initial("initial")
		b.State("initial").On[xerrActivate]().To("active")
		b.State("active").On[xerrDo]().Do("spy", func(context.Context, counter, xerrDo) { spy.Add(1) })
	})
	require.NoError(t, err)

	return m
}

// xerrThrowingSubscriber returns hooks whose Transition panics for its
// first times calls, and counts every call. Hooks.Transition is the nearest
// thing sc has to an XState subscriber.
func xerrThrowingSubscriber(calls *atomic.Int32, times int32, err error) *sc.Hooks[string, counter] {
	return &sc.Hooks[string, counter]{
		Transition: func(context.Context, sc.Step[string, counter]) {
			if calls.Add(1) <= times {
				panic(err)
			}
		},
		Ignored:        nil,
		EffectStarted:  nil,
		EffectFinished: nil,
	}
}

// errors.test.ts: error handling > does not cause an infinite loop when an
// error is thrown in subscribe
//
// DIVERGES from XState: XState contains the subscriber's error and reports
// it globally later. sc's instance lets the hook's panic out of Send. As in
// XState, the hook is called once and the step it was told about stands:
// the instance has moved to "active".
func TestXErrErrorsDoesNotCauseAnInfiniteLoopWhenAnErrorIsThrownInSubscribe(t *testing.T) {
	t.Parallel()

	var calls, spy atomic.Int32

	m := xerrActivation(t, &spy)

	inst, err := m.Instance(t.Context(), counter{}, sc.WithHooks(xerrThrowingSubscriber(&calls, 1, errXerrObserver)))
	require.NoError(t, err)
	defer inst.Close()

	require.PanicsWithValue(t, errXerrObserver, func() { _, _ = inst.Send(t.Context(), xerrActivate{}) })

	require.Equal(t, int32(1), calls.Load())
	require.Equal(t, []string{"active"}, inst.Snapshot().Active())
}

// errors.test.ts: error handling > doesn't crash the actor when an error is
// thrown in subscribe
//
// DIVERGES from XState: as for the case before it, the hook's panic comes
// out of Send rather than being reported globally. Otherwise the instance
// behaves as XState's actor does: the step stands, so it is in "active", and
// it is not broken: "do" then runs its action.
func TestXErrErrorsDoesntCrashTheActorWhenAnErrorIsThrownInSubscribe(t *testing.T) {
	t.Parallel()

	var calls, spy atomic.Int32

	m := xerrActivation(t, &spy)

	inst, err := m.Instance(t.Context(), counter{}, sc.WithHooks(xerrThrowingSubscriber(&calls, 1, errXerrObserver)))
	require.NoError(t, err)
	defer inst.Close()

	require.PanicsWithValue(t, errXerrObserver, func() { _, _ = inst.Send(t.Context(), xerrActivate{}) })
	require.Equal(t, int32(1), calls.Load())
	require.Equal(t, []string{"active"}, inst.Snapshot().Active())

	_, err = inst.Send(t.Context(), xerrDo{})
	require.NoError(t, err)
	require.Equal(t, int32(1), spy.Load())
}

// xerrPending is XState's machine whose pending state invokes an actor that
// fails as it starts. XState's charts route only its success, or nothing,
// which sc cannot declare; onError says what the port does with the
// failure.
func xerrPending(t *testing.T, service func(b *sc.StateBuilder[string, counter]) *sc.Invocation[string, counter, int], onError func(t *sc.Transition[string, counter, sc.Failed])) *sc.Machine[string, counter] {
	t.Helper()

	m, err := sc.New("machine", func(b *xerrBuilder) {
		b.Initial("pending")
		inv := service(b.State("pending"))
		inv.OnDone().To("success")
		onError(inv.OnError())
		b.Final("success")
		b.Final("failed")
	}, sc.AllowUnreachable())
	require.NoError(t, err)

	return m
}

// xerrThrowingCallback invokes a callback actor that throws as it starts.
func xerrThrowingCallback(b *sc.StateBuilder[string, counter]) *sc.Invocation[string, counter, int] {
	return b.Invoke("callback", func(context.Context, counter) (int, error) { panic(errXerrStart) })
}

// xerrRejectingPromise invokes a promise actor that rejects.
func xerrRejectingPromise(b *sc.StateBuilder[string, counter]) *sc.Invocation[string, counter, int] {
	return b.Invoke("promise", xerrRejects[counter](errXerrReject))
}

// xerrDropped is the OnError of a port whose XState chart declares none.
func xerrDropped(t *sc.Transition[string, counter, sc.Failed]) { t.Stay() }

// errors.test.ts: error handling > unhandled rejection of a promise actor
// should be reported to the existing error listener of its parent
//
// DIVERGES from XState: XState's machine errors and its error listener is
// told of the rejection. An sc chart must say what a failure does; here it
// drops it, and then nobody running the instance is told: no driver call
// returns the error and the instance stays in "pending". Only the hooks see
// how the service run ended.
func TestXErrErrorsUnhandledRejectionOfAPromiseActorShouldBeReportedToTheExistingErrorListenerOfItsParent(t *testing.T) {
	t.Parallel()

	synctest.Test(t, func(t *testing.T) {
		box := xerrNewBox()
		m := xerrPending(t, xerrRejectingPromise, xerrDropped)

		inst, err := m.Instance(t.Context(), counter{}, sc.WithHooks(xerrServiceEnds(box)))
		require.NoError(t, err)
		defer inst.Close()
		synctest.Wait()

		require.Equal(t, xerrSeen{name: "promise", err: errXerrReject}, box.only(t))
		require.Equal(t, []string{"pending"}, inst.Snapshot().Active())
		require.False(t, inst.Snapshot().Done())
	})
}

// errors.test.ts: error handling > unhandled rejection of a promise actor
// should be reported to the existing error listener of its grandparent
//
// DIVERGES from XState: XState's rejection errors the child machine, which
// errors the parent, whose error listener hears it. In sc an error goes no
// further than the OnError of the invocation that failed: the child drops
// it, stays in "pending", and its parent's OnError is never taken.
func TestXErrErrorsUnhandledRejectionOfAPromiseActorShouldBeReportedToTheExistingErrorListenerOfItsGrandparent(t *testing.T) {
	t.Parallel()

	synctest.Test(t, func(t *testing.T) {
		box := xerrNewBox()
		child := xerrPending(t, xerrRejectingPromise, xerrDropped)

		m, err := sc.New("parent", func(b *xerrBuilder) {
			b.Initial("pending")
			inv := b.State("pending").InvokeMachine("child", child,
				func(c counter) counter { return c },
				func(sc.Snapshot[string, counter]) (int, error) { return 0, nil },
			)
			inv.OnDone().To("success")
			inv.OnError().Do("record", xerrRecord[counter](box))
			b.Final("success")
		})
		require.NoError(t, err)

		inst, err := m.Instance(t.Context(), counter{})
		require.NoError(t, err)
		defer inst.Close()
		synctest.Wait()

		require.Empty(t, box.all())
		require.Equal(t, []string{"pending"}, inst.Snapshot().Active())

		kid, ok := inst.Snapshot().Child(child, "child")
		require.True(t, ok)
		require.Equal(t, []string{"pending"}, kid.Active())
		require.Empty(t, kid.Services())
	})
}

// errors.test.ts: error handling > handled sync errors thrown when starting
// a child actor should not be reported globally
//
// The global handler is dropped: sc has none. What is left is that the
// throw is handled: it arrives at OnError as a failure wrapping
// sc.ErrPanicked.
func TestXErrErrorsHandledSyncErrorsThrownWhenStartingAChildActorShouldNotBeReportedGlobally(t *testing.T) {
	t.Parallel()

	synctest.Test(t, func(t *testing.T) {
		box := xerrNewBox()
		m := xerrPending(t, xerrThrowingCallback, func(t *sc.Transition[string, counter, sc.Failed]) {
			t.Do("record", xerrRecord[counter](box)).To("failed")
		})

		inst, err := m.Instance(t.Context(), counter{})
		require.NoError(t, err)
		defer inst.Close()
		synctest.Wait()

		require.Equal(t, []string{"failed"}, inst.Snapshot().Active())
		require.True(t, inst.Snapshot().Done())

		failure := box.only(t).err
		require.ErrorIs(t, failure, sc.ErrPanicked)
		require.ErrorContains(t, failure, "sync_error_in_actor_start")
	})
}

// errors.test.ts: error handling > handled sync errors shouldn't notify the
// error listener
//
// The error a driver call returns stands for XState's error listener.
func TestXErrErrorsHandledSyncErrorsShouldntNotifyTheErrorListener(t *testing.T) {
	t.Parallel()

	synctest.Test(t, func(t *testing.T) {
		m := xerrPending(t, xerrThrowingCallback, func(t *sc.Transition[string, counter, sc.Failed]) { t.To("failed") })

		a := m.Actor(counter{})
		wait := xerrRun(t.Context(), a)

		out := wait()
		require.NoError(t, out.err)
		require.Nil(t, out.panicked)
		require.Equal(t, []string{"failed"}, a.Snapshot().Active())
	})
}

// errors.test.ts: error handling > unhandled sync errors should notify the
// root error listener
//
// DIVERGES from XState: XState's machine errors and its error listener is
// told of the throw. As for the unhandled rejection above, an sc chart must
// say what the failure does; dropping it leaves the instance running in
// "pending" with only the hooks told, of an error wrapping sc.ErrPanicked.
func TestXErrErrorsUnhandledSyncErrorsShouldNotifyTheRootErrorListener(t *testing.T) {
	t.Parallel()

	synctest.Test(t, func(t *testing.T) {
		box := xerrNewBox()
		m := xerrPending(t, xerrThrowingCallback, xerrDropped)

		inst, err := m.Instance(t.Context(), counter{}, sc.WithHooks(xerrServiceEnds(box)))
		require.NoError(t, err)
		defer inst.Close()
		synctest.Wait()

		ended := box.only(t)
		require.Equal(t, "callback", ended.name)
		require.ErrorIs(t, ended.err, sc.ErrPanicked)
		require.ErrorContains(t, ended.err, "sync_error_in_actor_start")
		require.Equal(t, []string{"pending"}, inst.Snapshot().Active())
	})
}

// errors.test.ts: error handling > handled sync errors thrown when starting
// an actor shouldn't crash the parent
func TestXErrErrorsHandledSyncErrorsThrownWhenStartingAnActorShouldntCrashTheParent(t *testing.T) {
	t.Parallel()

	synctest.Test(t, func(t *testing.T) {
		var spy atomic.Int32

		m, err := sc.New("machine", func(b *xerrBuilder) {
			b.Initial("pending")
			b.State("pending").Stream("callback", xerrThrows[counter](errXerrStart)).OnError().To("failed")
			b.State("failed").On[xerrDo]().Do("spy", func(context.Context, counter, xerrDo) { spy.Add(1) })
		})
		require.NoError(t, err)

		inst, err := m.Instance(t.Context(), counter{})
		require.NoError(t, err)
		defer inst.Close()
		synctest.Wait()

		require.Equal(t, []string{"failed"}, inst.Snapshot().Active())
		require.False(t, inst.Snapshot().Done())

		_, err = inst.Send(t.Context(), xerrDo{})
		require.NoError(t, err)
		require.Equal(t, int32(1), spy.Load())
	})
}

// errors.test.ts: error handling > unhandled sync errors thrown when
// starting an actor should crash the parent
//
// DIVERGES from XState: XState's machine ends in its "error" status. sc
// rejects the chart as written, and with the failure dropped the instance
// is not crashed: it stays in "pending" and goes on taking events.
func TestXErrErrorsUnhandledSyncErrorsThrownWhenStartingAnActorShouldCrashTheParent(t *testing.T) {
	t.Parallel()

	_, err := sc.New("unhandled", func(b *xerrBuilder) {
		b.Initial("pending")
		b.State("pending").Stream("callback", xerrThrows[counter](errXerrStart))
	})
	require.ErrorContains(t, err, `service "callback" has no OnError transition`)

	synctest.Test(t, func(t *testing.T) {
		m, err := sc.New("machine", func(b *xerrBuilder) {
			b.Initial("pending")
			b.State("pending").Stream("callback", xerrThrows[counter](errXerrStart)).OnError().Stay()
		})
		require.NoError(t, err)

		inst, err := m.Instance(t.Context(), counter{})
		require.NoError(t, err)
		defer inst.Close()
		synctest.Wait()

		require.Equal(t, []string{"pending"}, inst.Snapshot().Active())
		require.False(t, inst.Snapshot().Done())
		require.Empty(t, inst.Snapshot().Services())

		_, err = inst.Send(t.Context(), xerrDo{})
		require.NoError(t, err)
	})
}

// errors.test.ts: error handling > error thrown in initial custom entry
// action should error the actor
//
// An sc instance whose initial action panics is never handed out: the
// driver closes it and returns the error, which stands for XState's "error"
// status and error listener. An actor's Run returns the same error.
func TestXErrErrorsErrorThrownInInitialCustomEntryActionShouldErrorTheActor(t *testing.T) {
	t.Parallel()

	m, err := sc.New("machine", func(b *xerrBuilder) {
		b.Chart().Entry("throw", func(context.Context, counter) { panic(errXerrEntry) })
	})
	require.NoError(t, err)

	inst, err := m.Instance(t.Context(), counter{})
	require.ErrorIs(t, err, sc.ErrPanicked)
	require.ErrorContains(t, err, "error_thrown_in_entry_action")
	require.Nil(t, inst)

	err = m.Actor(counter{}).Run(t.Context())
	require.ErrorIs(t, err, sc.ErrPanicked)
	require.ErrorContains(t, err, "error_thrown_in_entry_action")
}

// errors.test.ts: error handling > error thrown by a custom entry action
// when transitioning should error the actor
//
// DIVERGES from XState: XState's actor ends in its "error" status. An sc
// action cannot fail a transition: the instance has moved to "b", Send
// returns the panic as an error wrapping sc.ErrPanicked, and the instance
// goes on taking events. An actor has no caller to return the error to: its
// hooks are told, and it goes on running in "b".
func TestXErrErrorsErrorThrownByACustomEntryActionWhenTransitioningShouldErrorTheActor(t *testing.T) {
	t.Parallel()

	m, err := sc.New("machine", func(b *xerrBuilder) {
		b.Initial("a")
		b.State("a").On[xerrNext]().To("b")
		b.State("b").Entry("throw", func(context.Context, counter) { panic(errXerrEntry) })
	})
	require.NoError(t, err)

	inst, err := m.Instance(t.Context(), counter{})
	require.NoError(t, err)
	defer inst.Close()

	step, err := inst.Send(t.Context(), xerrNext{})
	require.ErrorIs(t, err, sc.ErrPanicked)
	require.ErrorContains(t, err, "error_thrown_in_entry_action")
	require.True(t, step.Accepted())
	require.Equal(t, []string{"b"}, inst.Snapshot().Active())

	_, err = inst.Send(t.Context(), xerrNext{})
	require.NoError(t, err)

	synctest.Test(t, func(t *testing.T) {
		box := xerrNewBox()
		ctx, cancel := context.WithCancel(t.Context())
		defer cancel()

		a := m.Actor(counter{}, sc.WithHooks(xerrActionEnds(box)))
		wait := xerrRun(ctx, a)

		_, err := a.Call(t.Context(), xerrNext{})
		require.NoError(t, err)
		require.Equal(t, []string{"b"}, a.Snapshot().Active())

		failed := box.only(t)
		require.Equal(t, "throw", failed.name)
		require.ErrorIs(t, failed.err, sc.ErrPanicked)
		require.ErrorContains(t, failed.err, "error_thrown_in_entry_action")

		_, err = a.Call(t.Context(), xerrNext{})
		require.NoError(t, err)

		cancel()
		out := wait()
		require.Nil(t, out.panicked)
		require.ErrorIs(t, out.err, context.Canceled)
	})
}

// errors.test.ts: error handling > shouldn't execute deferred initial
// actions that come after an action that errors
//
// DIVERGES from XState: XState runs no action after the one that throws.
// Both sc drivers carry out every effect of the step and return the first
// error, so the later action has run by the time Actor.Run or
// Machine.Instance reports the failure.
func TestXErrErrorsShouldntExecuteDeferredInitialActionsThatComeAfterAnActionThatErrors(t *testing.T) {
	t.Parallel()

	var spy atomic.Int32

	m, err := sc.New("machine", func(b *xerrBuilder) {
		b.Chart().
			Entry("throw", func(context.Context, counter) { panic(errXerrEntry) }).
			Entry("spy", func(context.Context, counter) { spy.Add(1) })
	})
	require.NoError(t, err)

	err = m.Actor(counter{}).Run(t.Context())
	require.ErrorIs(t, err, sc.ErrPanicked)
	require.Equal(t, int32(1), spy.Load())

	_, err = m.Instance(t.Context(), counter{})
	require.ErrorIs(t, err, sc.ErrPanicked)
	require.Equal(t, int32(2), spy.Load())
}

// errors.test.ts: error handling > should error the parent on errored
// initial state of a child
//
// XState's child is custom logic whose first snapshot is an error. The port
// uses a child machine that fails as it starts, on a panicking entry action.
//
// DIVERGES from XState: XState's parent errors with the child's error. In sc
// the child's failure to start arrives at the parent's OnError, and the
// parent is running.
func TestXErrErrorsShouldErrorTheParentOnErroredInitialStateOfAChild(t *testing.T) {
	t.Parallel()

	synctest.Test(t, func(t *testing.T) {
		box := xerrNewBox()

		child, err := sc.New("child", func(b *xerrBuilder) {
			b.Chart().Entry("fail", func(context.Context, counter) { panic("immediate error!") })
		})
		require.NoError(t, err)

		m, err := sc.New("machine", func(b *xerrBuilder) {
			b.Chart().InvokeMachine("child", child,
				func(c counter) counter { return c },
				func(sc.Snapshot[string, counter]) (int, error) { return 0, nil },
			).OnError().Do("record", xerrRecord[counter](box))
		})
		require.NoError(t, err)

		inst, err := m.Instance(t.Context(), counter{})
		require.NoError(t, err)
		defer inst.Close()
		synctest.Wait()

		failure := box.only(t).err
		require.ErrorIs(t, failure, sc.ErrPanicked)
		require.ErrorContains(t, failure, "immediate error!")
		require.False(t, inst.Snapshot().Done())
		require.Empty(t, inst.Snapshot().Services())
	})
}

// errors.test.ts: error handling > actor continues to work normally after
// emit callback errors
//
// DIVERGES from XState: XState contains the listener's error and reports it
// out of band, so its send returns normally. sc calls the OnEmit listener
// inside Send and contains its panic too, but returns it from Send as an
// error wrapping sc.ErrPanicked. The instance goes on working afterwards, as
// XState's actor does.
func TestXErrErrorsActorContinuesToWorkNormallyAfterEmitCallbackErrors(t *testing.T) {
	t.Parallel()

	m, err := sc.New("machine", func(b *xerrBuilder) {
		b.On[xerrSomeEvent]().Emit("emitted", func(counter, xerrSomeEvent) xerrEmitted { return xerrEmitted{Foo: "bar"} })
	})
	require.NoError(t, err)

	var (
		calls atomic.Int32
		got   atomic.Value
	)

	inst, err := m.Instance(t.Context(), counter{}, sc.OnEmit(func(_ context.Context, ev any) {
		if calls.Add(1) == 1 {
			panic(errXerrOops)
		}

		got.Store(ev)
	}))
	require.NoError(t, err)
	defer inst.Close()

	_, err = inst.Send(t.Context(), xerrSomeEvent{})
	require.ErrorIs(t, err, sc.ErrPanicked)
	require.ErrorContains(t, err, "oops")
	require.Equal(t, int32(1), calls.Load())
	require.False(t, inst.Snapshot().Done())

	_, err = inst.Send(t.Context(), xerrSomeEvent{})
	require.NoError(t, err)
	require.Equal(t, xerrEmitted{Foo: "bar"}, got.Load())
	require.False(t, inst.Snapshot().Done())
}

// errors.test.ts: error handling > state onError catches errors thrown by
// initial entry actions
//
// DIVERGES from XState: XState's root onError catches the entry action's
// error and moves to "failed". sc has no onError for actions, so "failed"
// is a state nothing reaches: the core starts the chart in "active", and a
// driver that runs the action gives back its panic as an error instead of
// an instance.
func TestXErrErrorsStateOnErrorCatchesErrorsThrownByInitialEntryActions(t *testing.T) {
	t.Parallel()

	m, err := sc.New("machine", func(b *xerrBuilder) {
		b.Initial("active")
		b.State("active").Entry("throw", func(context.Context, counter) { panic("initial entry failed") })
		b.State("failed")
	}, sc.AllowUnreachable())
	require.NoError(t, err)

	snap, effects := m.Start(counter{}, epoch)
	require.Equal(t, []string{"active"}, snap.Active())
	require.Equal(t, []string{"active/throw"}, names(effects))

	inst, err := m.Instance(t.Context(), counter{})
	require.ErrorIs(t, err, sc.ErrPanicked)
	require.ErrorContains(t, err, "initial entry failed")
	require.Nil(t, inst)
}

// errors.test.ts: error handling > state onError catches errors thrown by
// transition actions
//
// DIVERGES from XState: XState's state onError catches the action's error
// and moves to "failed". In sc the panic comes back from Send as an error
// wrapping sc.ErrPanicked and the instance stays in "active".
func TestXErrErrorsStateOnErrorCatchesErrorsThrownByTransitionActions(t *testing.T) {
	t.Parallel()

	m, err := sc.New("machine", func(b *xerrBuilder) {
		b.Initial("active")
		b.State("active").On[xerrNext]().Do("throw", func(context.Context, counter, xerrNext) { panic("transition action failed") })
		b.State("failed")
	}, sc.AllowUnreachable())
	require.NoError(t, err)

	inst, err := m.Instance(t.Context(), counter{})
	require.NoError(t, err)
	defer inst.Close()

	_, err = inst.Send(t.Context(), xerrNext{})
	require.ErrorIs(t, err, sc.ErrPanicked)
	require.ErrorContains(t, err, "transition action failed")
	require.Equal(t, []string{"active"}, inst.Snapshot().Active())
	require.False(t, inst.Snapshot().Done())
}

// errors.test.ts: error handling > state onError accepts a cross-state
// context patch (typed against the target state schema)
//
// DIVERGES from XState: XState's state onError catches what the transition
// function throws, moves to "failed" and patches the context with the
// message. An sc assignment is a plain function: its panic propagates out
// of Machine.Transition, and nothing is returned, so the caller keeps the
// snapshot it had.
func TestXErrErrorsStateOnErrorAcceptsACrossStateContextPatch(t *testing.T) {
	t.Parallel()

	m, err := sc.New("machine", func(b *sc.Builder[string, xerrReason]) {
		b.Initial("active")
		b.State("active").On[xerrNext]().Assign("throw", func(xerrReason, xerrNext) xerrReason { panic(errXerrBoom) })
		b.State("failed")
	}, sc.AllowUnreachable())
	require.NoError(t, err)

	snap, _ := m.Start(xerrReason{Reason: ""}, epoch)
	require.PanicsWithValue(t, errXerrBoom, func() { m.Transition(snap, xerrNext{}, epoch) })
	require.Equal(t, []string{"active"}, snap.Active())
	require.Empty(t, snap.Context.Reason)
}

// errors.test.ts: error handling > state onError catches errors thrown by
// transition functions
//
// DIVERGES from XState: XState's state onError catches the throw and moves
// to "failed". The port's throw is a guard's, and an sc guard's panic
// propagates out of Machine.Transition and Machine.Can.
func TestXErrErrorsStateOnErrorCatchesErrorsThrownByTransitionFunctions(t *testing.T) {
	t.Parallel()

	m, err := sc.New("machine", func(b *xerrBuilder) {
		b.Initial("active")
		b.State("active").On[xerrNext]().When("throws", func(counter, xerrNext) bool { panic("transition function failed") }).To("done")
		b.State("done")
		b.State("failed")
	}, sc.AllowUnreachable())
	require.NoError(t, err)

	snap, _ := m.Start(counter{}, epoch)
	require.PanicsWithValue(t, "transition function failed", func() { m.Transition(snap, xerrNext{}, epoch) })
	require.PanicsWithValue(t, "transition function failed", func() { m.Can(snap, xerrNext{}, epoch) })
}

// errors.test.ts: error handling > state onError catches errors thrown by
// target entry actions
//
// DIVERGES from XState: XState's onError on the source state catches the
// target's entry error and moves to "failed". In sc the transition into
// "loading" stands, and Send returns the panic as an error.
func TestXErrErrorsStateOnErrorCatchesErrorsThrownByTargetEntryActions(t *testing.T) {
	t.Parallel()

	m, err := sc.New("machine", func(b *xerrBuilder) {
		b.Initial("active")
		b.State("active").On[xerrNext]().To("loading")
		b.State("loading").Entry("throw", func(context.Context, counter) { panic("target entry failed") })
		b.State("failed")
	}, sc.AllowUnreachable())
	require.NoError(t, err)

	inst, err := m.Instance(t.Context(), counter{})
	require.NoError(t, err)
	defer inst.Close()

	_, err = inst.Send(t.Context(), xerrNext{})
	require.ErrorIs(t, err, sc.ErrPanicked)
	require.ErrorContains(t, err, "target entry failed")
	require.Equal(t, []string{"loading"}, inst.Snapshot().Active())
}

// errors.test.ts: error handling > state onError catches invoked actor
// errors
//
// XState catches the actor's error with the onError of the state that
// invokes it. sc declares it on the invocation, which is the only place it
// has.
func TestXErrErrorsStateOnErrorCatchesInvokedActorErrors(t *testing.T) {
	t.Parallel()

	synctest.Test(t, func(t *testing.T) {
		box := xerrNewBox()

		m, err := sc.New("machine", func(b *xerrBuilder) {
			b.Initial("active")
			b.State("active").Stream("callback", func(context.Context, counter) iter.Seq2[any, error] {
				panic("invoked actor failed")
			}).OnError().Do("errorSpy", xerrRecord[counter](box)).To("failed")
			b.State("failed")
		})
		require.NoError(t, err)

		inst, err := m.Instance(t.Context(), counter{})
		require.NoError(t, err)
		defer inst.Close()
		synctest.Wait()

		require.Equal(t, []string{"failed"}, inst.Snapshot().Active())
		require.False(t, inst.Snapshot().Done())

		failure := box.only(t).err
		require.ErrorIs(t, failure, sc.ErrPanicked)
		require.ErrorContains(t, failure, "invoked actor failed")
	})
}

// errors.test.ts: error handling > state onError catches errors from
// invoked actor initialization
//
// XState's child machine throws from its context factory. A child's first
// context is what the parent's input function returns, so that is what
// panics in the port.
func TestXErrErrorsStateOnErrorCatchesErrorsFromInvokedActorInitialization(t *testing.T) {
	t.Parallel()

	synctest.Test(t, func(t *testing.T) {
		box := xerrNewBox()

		child, err := sc.New("child", func(*xerrBuilder) {})
		require.NoError(t, err)

		m, err := sc.New("machine", func(b *xerrBuilder) {
			b.Initial("active")
			b.State("active").InvokeMachine("child", child,
				func(counter) counter { panic("invoked actor initialization failed") },
				func(sc.Snapshot[string, counter]) (int, error) { return 0, nil },
			).OnError().Do("errorSpy", xerrRecord[counter](box)).To("failed")
			b.State("failed")
		})
		require.NoError(t, err)

		inst, err := m.Instance(t.Context(), counter{})
		require.NoError(t, err)
		defer inst.Close()
		synctest.Wait()

		require.Equal(t, []string{"failed"}, inst.Snapshot().Active())
		require.False(t, inst.Snapshot().Done())

		failure := box.only(t).err
		require.ErrorIs(t, failure, sc.ErrPanicked)
		require.ErrorContains(t, failure, "invoked actor initialization failed")
	})
}

// errors.test.ts: error handling > missing send targets > dead-letters a
// send to an unknown child id without erroring the actor
//
// The rejection callback and the console warning are dropped: sc has
// neither.
//
// DIVERGES from XState: XState finds out as the event is sent, and
// dead-letters it. sc rejects a chart whose SendTo names a service the
// chart does not declare. A send to a declared service that is not running
// is the nearer case: it is dropped without a trace, the core returning no
// send effect at all.
func TestXErrErrorsMissingSendTargetsDeadLettersASendToAnUnknownChildID(t *testing.T) {
	t.Parallel()

	_, err := sc.New("sender", func(b *xerrBuilder) {
		b.Initial("active")
		b.State("active").On[xerrNext]().SendTo("ping", "worker", func(counter, xerrNext) any { return xerrPing{} })
	})
	require.ErrorContains(t, err, `SendTo names "worker", which is not a service of the chart`)

	m, err := sc.New("sender", func(b *xerrBuilder) {
		b.Initial("active")
		b.State("active").On[xerrNext]().SendTo("ping", "worker", func(counter, xerrNext) any { return xerrPing{} })
		b.State("elsewhere").Invoke("worker", xerrRejects[counter](errXerrOops)).OnError().To("failed")
		b.State("failed")
	}, sc.AllowUnreachable())
	require.NoError(t, err)

	snap, _ := m.Start(counter{}, epoch)
	next, effects := m.Transition(snap, xerrNext{}, epoch)

	require.Equal(t, []string{"active"}, next.Active())
	require.False(t, next.Done())
	require.Empty(t, effects)
}

// errors.test.ts: error handling > missing send targets > dead-letters a
// send to the parent of a root actor
//
// A send to the parent is Emit. Without a listener, which is what a root
// has in place of a parent, the driver drops the event; the rejection
// callback is dropped from the port.
func TestXErrErrorsMissingSendTargetsDeadLettersASendToTheParentOfARootActor(t *testing.T) {
	t.Parallel()

	m, err := sc.New("sender", func(b *xerrBuilder) {
		b.Initial("active")
		b.State("active").On[xerrNext]().Emit("ping", func(counter, xerrNext) xerrPing { return xerrPing{} })
		b.State("failed")
	}, sc.AllowUnreachable())
	require.NoError(t, err)

	inst, err := m.Instance(t.Context(), counter{})
	require.NoError(t, err)
	defer inst.Close()

	step, err := inst.Send(t.Context(), xerrNext{})
	require.NoError(t, err)
	require.Equal(t, []sc.EffectKind{sc.EffectEmit}, kinds(step.Effects))
	require.Equal(t, []string{"active"}, inst.Snapshot().Active())
	require.False(t, inst.Snapshot().Done())
}

// errorPrecedence.test.ts: error precedence > (a) an execution error
// recovered by state-level onError does not reach subscribers
//
// DIVERGES from XState: XState's state onError recovers the transition
// function's throw by moving to "recovered", and nobody else hears of it.
// sc has no state-level onError: the assignment's panic comes out of Send
// to the sender, and the instance stays in "active", still usable.
func TestXErrPrecedenceAnExecutionErrorRecoveredByStateLevelOnErrorDoesNotReachSubscribers(t *testing.T) {
	t.Parallel()

	m, err := sc.New("machine", func(b *xerrBuilder) {
		b.Initial("active")
		b.State("active").On[xerrFail]().Assign("throw", func(counter, xerrFail) counter { panic(errXerrBoom) })
		b.State("recovered")
	}, sc.AllowUnreachable())
	require.NoError(t, err)

	inst, err := m.Instance(t.Context(), counter{})
	require.NoError(t, err)
	defer inst.Close()

	require.PanicsWithValue(t, errXerrBoom, func() { _, _ = inst.Send(t.Context(), xerrFail{}) })
	require.Equal(t, []string{"active"}, inst.Snapshot().Active())

	_, err = inst.Send(t.Context(), xerrNext{})
	require.NoError(t, err)
}

// errorPrecedence.test.ts: error precedence > (c) an unrecovered error goes
// to an error observer and is not reported globally
//
// DIVERGES from XState: XState's actor ends in its "error" status and hands
// the error to its error observer. An sc actor has no observer to hand it
// to: the assignment's panic comes out of Run on the goroutine that called
// it, and the actor has stopped.
func TestXErrPrecedenceAnUnrecoveredErrorGoesToAnErrorObserver(t *testing.T) {
	t.Parallel()

	synctest.Test(t, func(t *testing.T) {
		m, err := sc.New("machine", func(b *xerrBuilder) {
			b.On[xerrFail]().Assign("throw", func(counter, xerrFail) counter { panic(errXerrBoom) })
		})
		require.NoError(t, err)

		a := m.Actor(counter{})
		wait := xerrRun(t.Context(), a)

		require.NoError(t, a.Send(t.Context(), xerrFail{}))
		out := wait()

		require.Equal(t, errXerrBoom, out.panicked)
		require.NoError(t, out.err)
		require.ErrorIs(t, a.Send(t.Context(), xerrFail{}), sc.ErrActorStopped)
	})
}

// xerrMover is XState's machine that "NEXT" moves between a and b, emitting
// "moved" on the way from a.
func xerrMover(t *testing.T) *sc.Machine[string, counter] {
	t.Helper()

	m, err := sc.New("machine", func(b *xerrBuilder) {
		b.Initial("a")
		b.State("a").On[xerrNext]().Emit("moved", func(counter, xerrNext) xerrMoved { return xerrMoved{} }).To("b")
		b.State("b").On[xerrNext]().To("a")
	})
	require.NoError(t, err)

	return m
}

// errorPrecedence.test.ts: error precedence > (f) a throwing subscribe
// observer or on() listener does not kill the actor
//
// Hooks.Transition stands for the subscriber and OnEmit for the on()
// listener. A hook's panic would hide the listener's, so each is checked on
// an instance of its own.
//
// DIVERGES from XState: XState contains both errors and reports them out of
// band. In sc the hook's panic comes out of Send, and the listener's comes
// back from Send as an error wrapping sc.ErrPanicked. Either way the step
// stands, so the instance goes from a to b and back to a, as XState's actor
// does, and is not killed.
func TestXErrPrecedenceAThrowingSubscribeObserverOrOnListenerDoesNotKillTheActor(t *testing.T) {
	t.Parallel()

	m := xerrMover(t)

	var observed, heard atomic.Int32

	for name, opt := range map[string]sc.RunOption{
		"observer": sc.WithHooks(xerrThrowingSubscriber(&observed, 2, errXerrObserver)),
		"listener": sc.OnEmit(func(context.Context, any) {
			heard.Add(1)
			panic(errXerrListener)
		}),
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			inst, err := m.Instance(t.Context(), counter{}, opt)
			require.NoError(t, err)
			defer inst.Close()

			if name == "observer" {
				require.PanicsWithValue(t, errXerrObserver, func() { _, _ = inst.Send(t.Context(), xerrNext{}) })
				require.Equal(t, []string{"b"}, inst.Snapshot().Active())
				require.PanicsWithValue(t, errXerrObserver, func() { _, _ = inst.Send(t.Context(), xerrNext{}) })
			} else {
				_, err = inst.Send(t.Context(), xerrNext{})
				require.ErrorIs(t, err, sc.ErrPanicked)
				require.ErrorContains(t, err, "listener")
				require.Equal(t, []string{"b"}, inst.Snapshot().Active())
				_, err = inst.Send(t.Context(), xerrNext{})
				require.NoError(t, err)
			}

			require.Equal(t, []string{"a"}, inst.Snapshot().Active())
			require.False(t, inst.Snapshot().Done())
		})
	}
}

// errorPrecedence.test.ts: error precedence > (g) a parent error stops all
// of its invoked and spawned children
//
// XState spawns its child in the root's entry action. Nothing spawns when
// an sc chart starts, so an event spawns it. The hooks tell when each
// child's run has ended.
//
// DIVERGES from XState: XState's parent ends in its "error" status. The sc
// actor's Run panics with the assignment's panic; on its way out it stops
// both children, as XState does.
func TestXErrPrecedenceAParentErrorStopsAllOfItsInvokedAndSpawnedChildren(t *testing.T) {
	t.Parallel()

	synctest.Test(t, func(t *testing.T) {
		box := xerrNewBox()

		child, err := sc.New("child", func(*xerrBuilder) {})
		require.NoError(t, err)

		output := func(sc.Snapshot[string, counter]) (int, error) { return 0, nil }

		m, err := sc.New("parent", func(b *xerrBuilder) {
			b.Chart().InvokeMachine("invoked", child, func(c counter) counter { return c }, output).OnError().Stay()
			b.Chart().SpawnMachine("spawned", child, func(c counter, _ string) counter { return c }, output).OnError().Stay()
			b.On[xerrSpawn]().Spawn("spawn", "spawned", func(counter, xerrSpawn) string { return "spawned" })
			b.On[xerrFail]().Assign("throw", func(counter, xerrFail) counter { panic(errXerrBoom) })
		})
		require.NoError(t, err)

		a := m.Actor(counter{}, sc.WithHooks(xerrServiceEnds(box)))
		wait := xerrRun(t.Context(), a)

		_, err = a.Call(t.Context(), xerrSpawn{})
		require.NoError(t, err)
		synctest.Wait()

		var running []string
		for _, service := range a.Snapshot().Services() {
			running = append(running, service.Name)
		}
		require.ElementsMatch(t, []string{"invoked", "spawned"}, running)
		require.Empty(t, box.all())

		require.NoError(t, a.Send(t.Context(), xerrFail{}))
		out := wait()
		synctest.Wait()

		require.Equal(t, errXerrBoom, out.panicked)

		var ended []string
		for _, seen := range box.all() {
			ended = append(ended, seen.name)
			require.ErrorIs(t, seen.err, context.Canceled)
		}
		require.ElementsMatch(t, []string{"invoked", "spawned"}, ended)
	})
}

// errorPrecedence.test.ts: error precedence > (h) an invoke failure in one
// parallel region is recovered by that region's onError without exiting
// siblings
//
// XState recovers with the onError of the region; sc with the OnError of
// the invocation.
func TestXErrPrecedenceAnInvokeFailureInOneParallelRegionIsRecoveredWithoutExitingSiblings(t *testing.T) {
	t.Parallel()

	synctest.Test(t, func(t *testing.T) {
		var siblingExit atomic.Int32

		m, err := sc.New("machine", func(b *xerrBuilder) {
			b.Regions()
			b.State("left", func(b *xerrBuilder) {
				b.Initial("working")
				b.State("working").Stream("callback", xerrThrows[counter](errXerrBoom)).OnError().To("failed")
				b.State("failed")
			})
			b.State("right", func(b *xerrBuilder) {
				b.Initial("idle")
				b.State("idle").Exit("siblingExit", func(context.Context, counter) { siblingExit.Add(1) })
			})
		})
		require.NoError(t, err)

		inst, err := m.Instance(t.Context(), counter{})
		require.NoError(t, err)
		defer inst.Close()
		synctest.Wait()

		snap := inst.Snapshot()
		require.False(t, snap.Done())
		require.Equal(t, []string{"left", "failed", "right", "idle"}, snap.Active())
		require.Zero(t, siblingExit.Load())
	})
}

// errorPrecedence.test.ts: error precedence > (i) an invoked child failure
// is delivered as xstate.error.actor for that child id
//
// XState handles one error event for every actor and picks the child out
// by its ID. An sc OnError belongs to its invocation, and the Failed it
// receives names the run, whose ID has the service's name in it.
func TestXErrPrecedenceAnInvokedChildFailureIsDeliveredForThatChildID(t *testing.T) {
	t.Parallel()

	synctest.Test(t, func(t *testing.T) {
		box := xerrNewBox()

		m, err := sc.New("machine", func(b *xerrBuilder) {
			b.Chart().Invoke("fetcher", xerrRejects[counter](errXerrBoom)).OnError().Do("received", xerrRecord[counter](box))
		})
		require.NoError(t, err)

		inst, err := m.Instance(t.Context(), counter{})
		require.NoError(t, err)
		defer inst.Close()
		synctest.Wait()

		require.False(t, inst.Snapshot().Done())

		received := box.only(t)
		require.Equal(t, errXerrBoom, received.err)
		require.Equal(t, "machine.fetcher#1", received.name)
	})
}

// xerrMs is the time ms milliseconds after the charts start.
func xerrMs(ms int) time.Time {
	return epoch.Add(time.Duration(ms) * time.Millisecond)
}

// xerrLimited is the chart of XState's invoke-level timeout cases: working
// invokes a service under a time limit. XState's invoke timeout is the
// state's budget, declared with Within, and its onTimeout is the OnError
// that takes a failure carrying context.DeadlineExceeded.
func xerrLimited[R any](t *testing.T, limit time.Duration, run func(context.Context, counter) (R, error), onDone func(*sc.Transition[string, counter, sc.Done[R]]), box *xerrBox) *sc.Machine[string, counter] {
	t.Helper()

	m, err := sc.New("machine", func(b *xerrBuilder) {
		b.Initial("working")
		inv := b.State("working").Within(limit).Invoke("child", run)
		if onDone != nil {
			onDone(inv.OnDone())
		}
		inv.OnError().
			When("timed out", func(_ counter, f sc.Failed) bool { return errors.Is(f.Err, context.DeadlineExceeded) }).
			Do("timeoutSpy", xerrRecord[counter](box)).
			To("timedOut")
		inv.OnError().Stay()
		b.State("done")
		b.State("timedOut")
	}, sc.AllowUnreachable())
	require.NoError(t, err)

	return m
}

// xerrTakes is a service that takes d and stops early when it is cancelled.
func xerrTakes[C any](d time.Duration) func(context.Context, C) (string, error) {
	return func(ctx context.Context, _ C) (string, error) {
		select {
		case <-time.After(d):
			return "ok", nil
		case <-ctx.Done():
			return "", context.Cause(ctx)
		}
	}
}

// xerrResolves is a service that succeeds at once.
func xerrResolves(context.Context, counter) (string, error) { return "ok", nil }

// timeout.test.ts: async logic timeout > aborts and errors when
// createAsyncLogic exceeds its timeout
//
// XState starts the logic as an actor of its own. An sc service runs inside
// a state, whose budget is its timeout. XState's logic ID is dropped.
//
// As in XState, the logic never settles and ignores its abort signal, and
// the chart is told of the timeout when the time is up all the same.
func TestXErrTimeoutAbortsAndErrorsWhenCreateAsyncLogicExceedsItsTimeout(t *testing.T) {
	t.Parallel()

	synctest.Test(t, func(t *testing.T) {
		box := xerrNewBox()
		started := make(chan context.Context, 1)
		release := make(chan struct{})

		m := xerrLimited(t, 10*time.Millisecond, func(ctx context.Context, _ counter) (string, error) {
			started <- ctx
			<-release

			return "", context.Cause(ctx)
		}, nil, box)

		inst, err := m.Instance(t.Context(), counter{})
		require.NoError(t, err)
		defer inst.Close()

		signal := <-started
		require.NoError(t, signal.Err())

		// The service must be let go before the instance is closed, which
		// waits for it.
		defer close(release)

		xerrSleep(10 * time.Millisecond)
		require.ErrorIs(t, signal.Err(), context.DeadlineExceeded)
		require.Equal(t, []string{"timedOut"}, inst.Snapshot().Active())
		require.ErrorIs(t, box.only(t).err, context.DeadlineExceeded)
	})
}

// timeout.test.ts: async logic timeout > clears the timeout timer when run
// throws synchronously instead of returning a rejected promise
//
// A budget is a deadline on the service's context, not a timer of the
// chart's, so the port checks that the failure leaves no timer and that
// nothing happens when the budget would have ended.
func TestXErrTimeoutClearsTheTimeoutTimerWhenRunThrowsSynchronously(t *testing.T) {
	t.Parallel()

	synctest.Test(t, func(t *testing.T) {
		box := xerrNewBox()

		m, err := sc.New("machine", func(b *xerrBuilder) {
			b.Initial("working")
			b.State("working").Within(30*time.Second).
				Invoke("task", func(context.Context, counter) (int, error) { panic("not configured") }).
				OnError().Do("record", xerrRecord[counter](box)).To("failed")
			b.State("failed")
		})
		require.NoError(t, err)

		inst, err := m.Instance(t.Context(), counter{})
		require.NoError(t, err)
		defer inst.Close()
		synctest.Wait()

		snap := inst.Snapshot()
		require.Equal(t, []string{"failed"}, snap.Active())
		require.Empty(t, snap.Timers())
		require.Empty(t, snap.Services())

		failure := box.only(t).err
		require.ErrorIs(t, failure, sc.ErrPanicked)
		require.ErrorContains(t, failure, "not configured")

		xerrSleep(time.Minute)
		require.Equal(t, snap.Seq(), inst.Snapshot().Seq())
	})
}

// xerrEscalating is XState's machine whose waiting state escalates when
// its timeout, declared by limit, has passed.
func xerrEscalating[C any](t *testing.T, limit func(waiting *sc.StateBuilder[string, C]) *sc.Transition[string, C, struct{}]) *sc.Machine[string, C] {
	t.Helper()

	m, err := sc.New("machine", func(b *sc.Builder[string, C]) {
		b.Initial("waiting")
		limit(b.State("waiting")).To("escalated")
		b.State("escalated")
	})
	require.NoError(t, err)

	return m
}

// timeout.test.ts: state-level timeout > transitions via onTimeout when
// duration elapses
//
// DIVERGES from XState: XState hands onTimeout an event of its own,
// {type: 'xstate.timeout.state', stateId: 'waiting'}. An sc delayed
// transition has no event: its action's effect names the state it belongs
// to and carries no event.
func TestXErrTimeoutTransitionsViaOnTimeoutWhenDurationElapses(t *testing.T) {
	t.Parallel()

	m := xerrEscalating(t, func(waiting *sc.StateBuilder[string, counter]) *sc.Transition[string, counter, struct{}] {
		return waiting.After(time.Second).Do("timeoutSpy", func(context.Context, counter, struct{}) {})
	})

	snap, _ := m.Start(counter{}, epoch)
	require.Equal(t, []string{"waiting"}, snap.Active())

	snap, effects := m.Advance(snap, xerrMs(500))
	require.Equal(t, []string{"waiting"}, snap.Active())
	require.Empty(t, effects)

	snap, effects = m.Advance(snap, xerrMs(1100))
	require.Equal(t, []string{"escalated"}, snap.Active())

	spy := effect(t, effects, sc.EffectAction)
	require.Equal(t, "waiting", spy.State)
	require.Equal(t, "timeoutSpy", spy.Name)
	require.Nil(t, spy.Event)
}

// timeout.test.ts: state-level timeout > cancels the timeout when the state
// is exited by another event
func TestXErrTimeoutCancelsTheTimeoutWhenTheStateIsExitedByAnotherEvent(t *testing.T) {
	t.Parallel()

	m, err := sc.New("machine", func(b *xerrBuilder) {
		b.Initial("waiting")
		waiting := b.State("waiting")
		waiting.After(time.Second).To("escalated")
		waiting.On[xerrApprove]().To("approved")
		b.State("approved")
		b.State("escalated")
	})
	require.NoError(t, err)

	snap, _ := m.Start(counter{}, epoch)
	snap, effects := m.Transition(snap, xerrApprove{}, epoch)
	require.Equal(t, []string{"approved"}, snap.Active())
	require.Equal(t, []sc.EffectKind{sc.EffectCancelTimer}, kinds(effects))
	require.Empty(t, snap.Timers())

	snap, _ = m.Advance(snap, xerrMs(5000))
	require.Equal(t, []string{"approved"}, snap.Active())
}

// timeout.test.ts: state-level timeout > coexists with `after` on the same
// state (independent timers)
//
// Both XState's after and its timeout are sc delayed transitions.
func TestXErrTimeoutCoexistsWithAfterOnTheSameState(t *testing.T) {
	t.Parallel()

	m, err := sc.New("machine", func(b *xerrBuilder) {
		b.Initial("waiting")
		waiting := b.State("waiting")
		waiting.After(500 * time.Millisecond).To("periodic")
		waiting.After(time.Second).To("escalated")
		b.State("periodic")
		b.State("escalated")
	})
	require.NoError(t, err)

	snap, _ := m.Start(counter{}, epoch)
	require.Len(t, snap.Timers(), 2)

	snap, _ = m.Advance(snap, xerrMs(600))
	require.Equal(t, []string{"periodic"}, snap.Active())
	require.Empty(t, snap.Timers())
}

// timeout.test.ts: state-level timeout > accepts a cross-state context
// patch (typed against the target state schema)
//
// The per-state context schemas are TypeScript's; the patch is an
// assignment on the delayed transition.
func TestXErrTimeoutAcceptsACrossStateContextPatch(t *testing.T) {
	t.Parallel()

	m, err := sc.New("machine", func(b *sc.Builder[string, xerrReason]) {
		b.Initial("running")
		running := b.State("running")
		running.After(time.Second).Assign("timeout", func(c xerrReason, _ struct{}) xerrReason {
			c.Reason = "timeout"
			return c
		}).To("expired")
		running.After(2*time.Second).Assign("after", func(c xerrReason, _ struct{}) xerrReason {
			c.Reason = "after"
			return c
		}).To("elapsed")
		b.Final("expired")
		b.Final("elapsed")
	})
	require.NoError(t, err)

	snap, _ := m.Start(xerrReason{Reason: ""}, epoch)
	snap, _ = m.Advance(snap, xerrMs(1000))

	require.Equal(t, []string{"expired"}, snap.Active())
	require.Equal(t, xerrReason{Reason: "timeout"}, snap.Context)
}

// timeout.test.ts: state-level timeout > supports onTimeout with object
// form { target }
func TestXErrTimeoutSupportsOnTimeoutWithObjectForm(t *testing.T) {
	t.Parallel()

	m := xerrEscalating(t, func(waiting *sc.StateBuilder[string, counter]) *sc.Transition[string, counter, struct{}] {
		return waiting.After(500 * time.Millisecond)
	})

	snap, _ := m.Start(counter{}, epoch)
	snap, _ = m.Advance(snap, xerrMs(600))
	require.Equal(t, []string{"escalated"}, snap.Active())
}

// timeout.test.ts: state-level timeout > supports a dynamic timeout
// function
func TestXErrTimeoutSupportsADynamicTimeoutFunction(t *testing.T) {
	t.Parallel()

	m := xerrEscalating(t, func(waiting *sc.StateBuilder[string, xerrSLA]) *sc.Transition[string, xerrSLA, struct{}] {
		return waiting.Delay("sla", func(c xerrSLA) time.Duration { return time.Duration(c.Millis) * time.Millisecond })
	})

	snap, _ := m.Start(xerrSLA{Millis: 1500}, epoch)

	snap, _ = m.Advance(snap, xerrMs(1000))
	require.Equal(t, []string{"waiting"}, snap.Active())

	snap, _ = m.Advance(snap, xerrMs(1600))
	require.Equal(t, []string{"escalated"}, snap.Active())
}

// timeout.test.ts: state-level timeout > supports a referenced delay
//
// XState names a delay the machine declares in its delays map. An sc chart
// has no such map: a named delay is a Delay with a function, declared where
// it is used.
func TestXErrTimeoutSupportsAReferencedDelay(t *testing.T) {
	t.Parallel()

	m := xerrEscalating(t, func(waiting *sc.StateBuilder[string, counter]) *sc.Transition[string, counter, struct{}] {
		return waiting.Delay("approvalSla", func(counter) time.Duration { return 750 * time.Millisecond })
	})

	snap, _ := m.Start(counter{}, epoch)
	require.Equal(t, "approvalSla", snap.Timers()[0].Name)

	snap, _ = m.Advance(snap, xerrMs(700))
	require.Equal(t, []string{"waiting"}, snap.Active())

	snap, _ = m.Advance(snap, xerrMs(800))
	require.Equal(t, []string{"escalated"}, snap.Active())
}

// timeout.test.ts: state-level timeout > keeps `timeout` and `after`
// independent on the same state
func TestXErrTimeoutKeepsTimeoutAndAfterIndependentOnTheSameState(t *testing.T) {
	t.Parallel()

	m, err := sc.New("machine", func(b *xerrBuilder) {
		b.Initial("waiting")
		waiting := b.State("waiting")
		waiting.After(500*time.Millisecond).Do("afterSpy", func(context.Context, counter, struct{}) {})
		waiting.After(time.Second).To("escalated")
		b.State("escalated")
	})
	require.NoError(t, err)

	snap, _ := m.Start(counter{}, epoch)

	snap, effects := m.Advance(snap, xerrMs(600))
	require.Equal(t, []string{"waiting"}, snap.Active())
	require.Equal(t, []string{"waiting/afterSpy"}, names(effects))

	snap, effects = m.Advance(snap, xerrMs(1100))
	require.Equal(t, []string{"escalated"}, snap.Active())
	require.NotContains(t, names(effects), "waiting/afterSpy")
}

// timeout.test.ts: state-level timeout > throws at construction when
// timeout is set without onTimeout
//
// An sc time limit and what it does are one declaration, so a limit that
// does nothing is a delayed transition with neither a target nor steps,
// which New rejects. Within is not such a limit: it declares no transition
// of its own, and New accepts it on a state with no services to bound.
func TestXErrTimeoutThrowsAtConstructionWhenTimeoutIsSetWithoutOnTimeout(t *testing.T) {
	t.Parallel()

	_, err := sc.New("machine", func(b *xerrBuilder) {
		b.Initial("waiting")
		b.State("waiting").After(time.Second)
	})
	require.ErrorContains(t, err, "a transition has neither a target nor any steps")

	_, err = sc.New("machine", func(b *xerrBuilder) {
		b.Initial("waiting")
		b.State("waiting").Within(time.Second)
	})
	require.NoError(t, err)
}

// timeout.test.ts: state-level timeout > passes state input to entry, exit,
// on, timeout, onTimeout, and after
//
// An sc state has no input of its own. The transition that enters the
// state assigns the duration to the context instead, which is what the
// state's actions, its computed delay and its transitions then see.
// XState's spy inside the timeout function is replaced by the delay of the
// timer the state starts, because an sc delay function must be pure.
func TestXErrTimeoutPassesStateInputToEntryExitOnTimeoutOnTimeoutAndAfter(t *testing.T) {
	t.Parallel()

	m, err := sc.New("machine", func(b *xerrBuilder) {
		b.Initial("idle")
		b.State("idle").On[xerrActivateFor]().Assign("input", func(c counter, ev xerrActivateFor) counter {
			c.N = ev.Duration
			return c
		}).To("active")

		active := b.State("active").Entry("entry", noop).Exit("exit", noop)
		active.Delay("timeout", func(c counter) time.Duration { return time.Duration(c.N) * time.Millisecond }).
			Do("onTimeout", func(context.Context, counter, struct{}) {}).
			To("idle")
		active.On[xerrPing]().Do("onPing", func(context.Context, counter, xerrPing) {})
		active.After(time.Second).Do("after", func(context.Context, counter, struct{}) {})
	})
	require.NoError(t, err)

	// seen maps each action the effects run to the duration it sees.
	seen := func(effects []sc.Effect[counter]) map[string]int {
		out := map[string]int{}
		for _, e := range effects {
			if e.Kind == sc.EffectAction {
				out[e.Name] = e.Context.N
			}
		}

		return out
	}

	snap, _ := m.Start(counter{}, epoch)

	snap, effects := m.Transition(snap, xerrActivateFor{Duration: 500}, epoch)
	require.Equal(t, []string{"active"}, snap.Active())
	require.Equal(t, map[string]int{"entry": 500}, seen(effects))

	// The timeout is resolved as the state is entered.
	delays := map[string]time.Duration{}
	for _, timer := range snap.Timers() {
		delays[timer.Name] = timer.Delay
	}
	require.Equal(t, map[string]time.Duration{"timeout": 500 * time.Millisecond, "": time.Second}, delays)

	snap, effects = m.Transition(snap, xerrPing{}, epoch)
	require.Equal(t, map[string]int{"onPing": 500}, seen(effects))

	snap, effects = m.Advance(snap, xerrMs(500))
	require.Equal(t, []string{"idle"}, snap.Active())
	require.Equal(t, map[string]int{"onTimeout": 500, "exit": 500}, seen(effects))

	// Entered again for longer, the state's after fires first.
	snap, _ = m.Transition(snap, xerrActivateFor{Duration: 2000}, xerrMs(500))
	require.Equal(t, []string{"active"}, snap.Active())

	snap, effects = m.Advance(snap, xerrMs(1500))
	require.Equal(t, []string{"active"}, snap.Active())
	require.Equal(t, map[string]int{"after": 2000}, seen(effects))
}

// timeout.test.ts: invoke-level timeout > preserves the canonical invoke
// timeout event on the pure path
//
// DIVERGES from XState: XState has an event of its own for an invoke's
// timeout, {type: 'xstate.timeout.actor', actorId, sessionId}, which its
// pure transition function takes. sc has none: the core gives the service's
// start effect the deadline and never fails the service itself. The driver
// does, and what the chart takes is the run's Failed carrying
// context.DeadlineExceeded. That error has a code of its own, so stored and
// loaded again it is still told for a timeout by errors.Is, and the
// canonical event survives the round trip as XState's does.
func TestXErrTimeoutPreservesTheCanonicalInvokeTimeoutEventOnThePurePath(t *testing.T) {
	t.Parallel()

	box := xerrNewBox()
	m := xerrLimited(t, time.Second, xerrTakes[counter](time.Hour), nil, box)

	working, effects := m.Start(counter{}, epoch)
	start := effect(t, effects, sc.EffectStartInvocation)
	require.Equal(t, "working.child#1", start.ID)
	require.Equal(t, xerrMs(1000), start.At)

	// Time passing does not time the service out in the core.
	late, effects := m.Advance(working, xerrMs(5000))
	require.Equal(t, working.Seq(), late.Seq())
	require.Empty(t, effects)

	timeout := sc.Failed{ID: start.ID, Err: context.DeadlineExceeded}
	timedOut, effects := m.Transition(working, timeout, xerrMs(1000))
	require.Equal(t, []string{"timedOut"}, timedOut.Active())
	require.Equal(t, timeout, effect(t, effects, sc.EffectAction).Event)

	data, err := json.Marshal(timeout)
	require.NoError(t, err)

	var stored sc.Failed
	require.NoError(t, json.Unmarshal(data, &stored))
	require.ErrorIs(t, stored.Err, context.DeadlineExceeded)
	require.EqualError(t, stored.Err, context.DeadlineExceeded.Error())
	require.Equal(t, sc.CodeDeadlineExceeded, sc.ErrorCode(stored.Err))
	require.Equal(t, sc.CodeDeadlineExceeded, sc.ErrorCode(timeout.Err))

	// The stored event is taken as the original is.
	again, _ := m.Transition(working, stored, xerrMs(1000))
	require.Equal(t, []string{"timedOut"}, again.Active())
}

// timeout.test.ts: invoke-level timeout > transitions via invoke.onTimeout
// when the invoke exceeds its timeout
//
// The event XState's onTimeout receives is the Failed of the run here, as
// in the case before it.
func TestXErrTimeoutTransitionsViaInvokeOnTimeoutWhenTheInvokeExceedsItsTimeout(t *testing.T) {
	t.Parallel()

	synctest.Test(t, func(t *testing.T) {
		box := xerrNewBox()
		m := xerrLimited(t, time.Second, xerrTakes[counter](10*time.Second), func(tr *sc.Transition[string, counter, sc.Done[string]]) {
			tr.To("done")
		}, box)

		inst, err := m.Instance(t.Context(), counter{})
		require.NoError(t, err)
		defer inst.Close()
		synctest.Wait()
		require.Equal(t, []string{"working"}, inst.Snapshot().Active())

		xerrSleep(1100 * time.Millisecond)
		require.Equal(t, []string{"timedOut"}, inst.Snapshot().Active())

		timeout := box.only(t)
		require.Equal(t, "working.child#1", timeout.name)
		require.ErrorIs(t, timeout.err, context.DeadlineExceeded)
	})
}

// timeout.test.ts: invoke-level timeout > does NOT fire onTimeout if the
// invoke completes first
func TestXErrTimeoutDoesNotFireOnTimeoutIfTheInvokeCompletesFirst(t *testing.T) {
	t.Parallel()

	synctest.Test(t, func(t *testing.T) {
		box := xerrNewBox()
		m := xerrLimited(t, 5*time.Second, xerrResolves, func(tr *sc.Transition[string, counter, sc.Done[string]]) {
			tr.To("done")
		}, box)

		inst, err := m.Instance(t.Context(), counter{})
		require.NoError(t, err)
		defer inst.Close()
		synctest.Wait()
		require.Equal(t, []string{"done"}, inst.Snapshot().Active())

		xerrSleep(10 * time.Second)
		require.Equal(t, []string{"done"}, inst.Snapshot().Active())
		require.Empty(t, box.all())
	})
}

// timeout.test.ts: invoke-level timeout > cancels the timeout when the
// invoke completes and the parent state stays active
func TestXErrTimeoutCancelsTheTimeoutWhenTheInvokeCompletesAndTheParentStateStaysActive(t *testing.T) {
	t.Parallel()

	synctest.Test(t, func(t *testing.T) {
		box := xerrNewBox()
		m := xerrLimited(t, time.Second, xerrResolves, nil, box)

		inst, err := m.Instance(t.Context(), counter{})
		require.NoError(t, err)
		defer inst.Close()
		synctest.Wait()
		require.Equal(t, []string{"working"}, inst.Snapshot().Active())
		require.Empty(t, inst.Snapshot().Services())

		xerrSleep(10 * time.Second)
		require.Equal(t, []string{"working"}, inst.Snapshot().Active())
		require.Empty(t, box.all())
	})
}

// timeout.test.ts: invoke-level timeout > cancels the timeout when
// invoke.onDone only enqueues actions
func TestXErrTimeoutCancelsTheTimeoutWhenInvokeOnDoneOnlyEnqueuesActions(t *testing.T) {
	t.Parallel()

	synctest.Test(t, func(t *testing.T) {
		var emitted atomic.Int32

		box := xerrNewBox()
		m := xerrLimited(t, time.Second, xerrResolves, func(tr *sc.Transition[string, counter, sc.Done[string]]) {
			tr.Emit("invokeDone", func(counter, sc.Done[string]) xerrInvokeDone { return xerrInvokeDone{} })
		}, box)

		inst, err := m.Instance(t.Context(), counter{}, sc.OnEmit(func(_ context.Context, ev any) {
			if _, ok := ev.(xerrInvokeDone); ok {
				emitted.Add(1)
			}
		}))
		require.NoError(t, err)
		defer inst.Close()
		synctest.Wait()
		require.Equal(t, []string{"working"}, inst.Snapshot().Active())
		require.Equal(t, int32(1), emitted.Load())

		xerrSleep(10 * time.Second)
		require.Equal(t, []string{"working"}, inst.Snapshot().Active())
		require.Empty(t, box.all())
	})
}

// timeout.test.ts: invoke-level timeout > supports a dynamic invoke-level
// timeout
//
// DIVERGES from XState: XState computes the invoke's timeout from the
// context. Within takes a fixed duration only. The limit that depends on
// the context is a computed delay on the invoking state, which leaves the
// state when it is due and so stops the service rather than failing it.
func TestXErrTimeoutSupportsADynamicInvokeLevelTimeout(t *testing.T) {
	t.Parallel()

	synctest.Test(t, func(t *testing.T) {
		m, err := sc.New("machine", func(b *sc.Builder[string, xerrSLA]) {
			b.Initial("working")
			working := b.State("working")
			inv := working.Invoke("child", xerrTakes[xerrSLA](time.Minute))
			inv.OnDone().To("done")
			inv.OnError().Stay()
			working.Delay("timeout", func(c xerrSLA) time.Duration { return time.Duration(c.Millis) * time.Millisecond }).To("timedOut")
			b.State("done")
			b.State("timedOut")
		})
		require.NoError(t, err)

		inst, err := m.Instance(t.Context(), xerrSLA{Millis: 2000})
		require.NoError(t, err)
		defer inst.Close()
		synctest.Wait()

		xerrSleep(time.Second)
		require.Equal(t, []string{"working"}, inst.Snapshot().Active())

		xerrSleep(1500 * time.Millisecond)
		require.Equal(t, []string{"timedOut"}, inst.Snapshot().Active())
		require.Empty(t, inst.Snapshot().Services())
	})
}

// timeout.test.ts: invoke-level timeout > throws at construction when
// invoke.timeout is set without onTimeout
//
// A budget's expiry is the service's failure, and New rejects a service
// that does not route its failure.
func TestXErrTimeoutThrowsAtConstructionWhenInvokeTimeoutIsSetWithoutOnTimeout(t *testing.T) {
	t.Parallel()

	_, err := sc.New("machine", func(b *xerrBuilder) {
		b.Initial("working")
		b.State("working").Within(time.Second).Invoke("child", xerrResolves).OnDone().To("done")
		b.State("done")
	})
	require.ErrorContains(t, err, `service "child" has no OnError transition`)
}

// Not an XState case: a regression test for a panic in an OnEmit listener,
// which once came out of Instance.Send after the driver had carried out the
// effects before it but before the step's snapshot was published, leaving
// the instance reporting "a" with a service the driver had stopped. Here the
// step stops the service of "a" and then emits. The listener's panic is
// contained and comes back from Send as an error; the step stands, so the
// instance is in "b" and counts no service, and the service was stopped
// once.
func TestXErrRegressionAListenerPanicDoesNotLeaveTheSnapshotBehindTheDriver(t *testing.T) {
	t.Parallel()

	synctest.Test(t, func(t *testing.T) {
		var cleanup atomic.Int32

		m, err := sc.New("machine", func(b *xerrBuilder) {
			b.Initial("a")
			a := b.State("a")
			a.Stream("work", xerrCallback[counter](&cleanup)).OnError().Stay()
			a.On[xerrNext]().Emit("moved", func(counter, xerrNext) xerrMoved { return xerrMoved{} }).To("b")
			b.State("b")
		})
		require.NoError(t, err)

		inst, err := m.Instance(t.Context(), counter{}, sc.OnEmit(func(context.Context, any) { panic(errXerrListener) }))
		require.NoError(t, err)
		defer inst.Close()
		synctest.Wait()
		require.Len(t, inst.Snapshot().Services(), 1)

		_, err = inst.Send(t.Context(), xerrNext{})
		require.ErrorIs(t, err, sc.ErrPanicked)
		require.ErrorContains(t, err, "listener")
		synctest.Wait()

		snap := inst.Snapshot()
		require.Equal(t, []string{"b"}, snap.Active())
		require.Empty(t, snap.Services())
		require.Equal(t, int32(1), cleanup.Load())
	})
}

// XState cases that are not ported, by reason.
//
// The case is about custom actor logic (createStepLogic, a hand-written
// ActorLogic, or a callback actor run on its own), which sc has no
// counterpart for: an sc service always runs inside a chart's state.
//   - errors.test.ts: stops custom-logic children absent from its snapshot after an error
//   - errors.test.ts: ignores domain children and foreign actors when custom logic errors
//   - errors.test.ts: error thrown by an initial logic effect should error the actor
//   - errors.test.ts: does not report an error that a subscriber observes before the report runs
//   - errors.test.ts: reports an error that no subscriber observed
//
// The case checks what XState's global reporter (reportUnhandledError, the
// window's error event) is told, or which of several observers of an actor
// or of its child actor ref have an error listener. sc has no global
// reporter, no error observers and no actor refs; the parts of these cases
// that are about the machine are covered by the cases ported above.
//   - errors.test.ts: doesn't notify error listener when an error is thrown in subscribe
//   - errors.test.ts: unhandled sync errors thrown when starting a child actor should be reported globally
//   - errors.test.ts: unhandled rejection of a promise actor should be reported globally in absence of error listener
//   - errors.test.ts: handled sync errors thrown when starting a child actor should be reported globally when not all of its own observers come with an error listener
//   - errors.test.ts: handled sync errors thrown when starting a child actor should not be reported globally when all of its own observers come with an error listener
//   - errors.test.ts: unhandled sync errors thrown when starting a child actor should be reported twice globally when not all of its own observers come with an error listener and when the root has no error listener of its own
//   - errors.test.ts: unhandled sync errors should not notify the global listener when the root error listener is present
//   - errors.test.ts: error thrown by the error listener should be reported globally
//   - errors.test.ts: error should be reported globally if not every observer comes with an error listener
//   - errors.test.ts: uncaught error and an error thrown by the error listener should both be reported globally when not every observer comes with an error listener
//   - errorPrecedence.test.ts: (d) an unrecovered error without an error observer is reported once
//   - errorPrecedence.test.ts: (j) mixed subscribers: error observer receives it and reportUnhandledError fires once
//
// The case needs an actor that stays in an "error" status and can be
// subscribed to afterwards. An sc driver returns the error once.
//   - errorPrecedence.test.ts: (e) subscribing after the actor errored delivers the same error immediately
//
// The case ranks the onError of several enclosing states. sc has no
// state-level onError, so there is nothing to rank: an OnError belongs to
// one invocation, and a panic in a transition's own functions is caught by
// no part of the chart (see case (a)).
//   - errorPrecedence.test.ts: (b) the nearest enclosing ancestor onError wins
//
// The case is about an action that returns a rejected promise. An sc action
// returns nothing; work that can fail later is a service.
//   - errors.test.ts: state onError catches rejected transition action promises
//
// The case sends to an actor reference that is undefined. An sc send names
// a service; the unknown-name case next to it is ported.
//   - errors.test.ts: dead-letters a send to an undefined ref without erroring the actor
//
// The case only checks TypeScript types (@ts-expect-error).
//   - errors.test.ts: state onError rejects a cross-state context patch that does not match the target state schema
//   - timeout.test.ts: rejects a cross-state context patch that does not match the target state schema
//
// The case is about the input of a state, given by the transition that
// enters it and kept apart for a parent and its child state. An sc chart has
// one context and no per-state input; the timed case that uses state input
// is ported with the context standing in for it.
//   - timeout.test.ts: passes the correct state input to nested states
//
// One kind of failure has no test at all, because it cannot be caught: a
// panic in a guard or an assignment of a transition that a service's result
// or a timer takes under an Instance happens on the service's or the timer's
// goroutine, where no caller can recover it, and ends the process.
