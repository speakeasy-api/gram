package sc_test

// Ports of XState's packages/core/test/emit.test.ts, spawnChild.test.ts,
// spawn.test.ts and input.test.ts (6.0.0-beta.2), one test per `it`, in
// source order. Each test names the XState case it ports. Cases that test an
// XState-only API are listed at the end of the file with the reason they were
// not ported.
//
// Several differences in what sc can declare shape every port:
//
//   - An emitted event is a Go value of a concrete struct type. A driver has
//     one listener, given with sc.OnEmit, that hears every emitted event;
//     there is no subscription per event type, so the ports that listen for
//     one type pick it out of everything the listener heard. The pure core
//     reports the same events as EffectEmit effects.
//   - XState's promise, callback, observable and step logics can be actors of
//     their own, created with createActor, with listeners of their own. In sc
//     only a machine runs under a driver, and a service has no way to emit:
//     what a stream yields is an event for its own machine. The ports that
//     emit from such logic run it as a service of a chart that passes on what
//     it reports.
//   - XState spawns from an entry action, with enq.spawn. In sc only a
//     transition spawns (Transition.Spawn), and the spawned service is
//     declared on a state and lives while that state is active. A port of
//     "spawn on entry" raises an event on entry and spawns on the transition
//     that handles it, which the same Start or Transition call processes.
//   - There are no actor references: a spawned run is found in
//     Snapshot.Services, told apart by sc.SpawnKey of its ID, addressed with
//     SendToKey and stopped with StopSpawned.
//   - A machine's input is the context value given to Start or Instance; sc
//     has no separate input, context factory, or init event. An invoked child
//     machine's input is the mapper given to InvokeMachine or SpawnMachine. A
//     plain service sees the context of its machine.
//   - A service must declare an OnError transition in sc. Where XState
//     declares none, the port declares OnError().Stay().
//   - What an XState root declares for itself is declared on Builder.Chart.
//     sc asks a chart for at least one state, so an XState machine that
//     declares none has one inert state.

import (
	"context"
	"iter"
	"slices"
	"sync"
	"sync/atomic"
	"testing"
	"testing/synctest"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/sc"
)

type (
	xmsgSomeEvent struct{}
	xmsgEmitted   struct{ Foo string }
	xmsgUnknown   struct{}
	xmsgCounted   struct{ Count int }
	xmsgEv        struct{}
	xmsgNotice    struct{}
	xmsgEvent     struct{}
	xmsgHello     struct{ Msg string }
	xmsgEmitCmd   struct{}
	xmsgOther     struct{}
	xmsgSpawn     struct{}
	xmsgSpawnNum  struct{ Input int }
	xmsgTick      struct{ N int }
	xmsgFoo       struct{}
	xmsgGo        struct{}
	xmsgStop      struct{}
	xmsgOpen      struct{}
	xmsgSave      struct{}
	xmsgPing      struct{}
	xmsgClose     struct{}
	xmsgCount     struct{ Count int }
)

type xmsgBuilder = sc.Builder[string, counter]

// xmsgGreeting is the context of the child machines that are given a
// greeting as their input.
type xmsgGreeting struct {
	Greeting string `json:"greeting"`
}

// xmsgChildID is the context of XState's "dynamic id" parent.
type xmsgChildID struct {
	ChildID string `json:"child_id"`
}

// xmsgHeard collects what a driver's OnEmit listener is given.
type xmsgHeard struct {
	mu     sync.Mutex
	events []any
}

func xmsgListener() *xmsgHeard {
	return &xmsgHeard{mu: sync.Mutex{}, events: nil}
}

func (h *xmsgHeard) listen(_ context.Context, ev any) {
	h.mu.Lock()
	defer h.mu.Unlock()

	h.events = append(h.events, ev)
}

func (h *xmsgHeard) all() []any {
	h.mu.Lock()
	defer h.mu.Unlock()

	return slices.Clone(h.events)
}

func xmsgNew[C any](t *testing.T, id string, def func(b *sc.Builder[string, C]), opts ...sc.Option) *sc.Machine[string, C] {
	t.Helper()

	m, err := sc.New(id, def, opts...)
	require.NoError(t, err)

	return m
}

// xmsgInstance starts an instance that is closed when the test ends.
func xmsgInstance[C any](t *testing.T, m *sc.Machine[string, C], c C, opts ...sc.RunOption) *sc.Instance[string, C] {
	t.Helper()

	inst, err := m.Instance(t.Context(), c, opts...)
	require.NoError(t, err)
	t.Cleanup(inst.Close)

	return inst
}

func xmsgSend[C any](t *testing.T, inst *sc.Instance[string, C], ev any) sc.Step[string, C] {
	t.Helper()

	step, err := inst.Send(t.Context(), ev)
	require.NoError(t, err)

	return step
}

// xmsgEmits returns the events of the emit effects, in order.
func xmsgEmits[C any](effects []sc.Effect[C]) []any {
	var out []any
	for _, e := range effects {
		if e.Kind == sc.EffectEmit {
			out = append(out, e.Event)
		}
	}

	return out
}

// xmsgRuns names the running services of a snapshot: a spawned run by its
// service name and key, any other by its service name.
func xmsgRuns[C any](snap sc.Snapshot[string, C]) []string {
	out := []string{}
	for _, run := range snap.Services() {
		if key, spawned := sc.SpawnKey(run.ID); spawned {
			out = append(out, run.Name+":"+key)
			continue
		}

		out = append(out, run.Name)
	}

	return out
}

// xmsgNoOutput is the output of a child machine whose result is not used.
func xmsgNoOutput[C any](sc.Snapshot[string, C]) (struct{}, error) {
	return struct{}{}, nil
}

func xmsgSame(c counter) counter { return c }

func xmsgSameKeyed(c counter, _ string) counter { return c }

// xmsgInert is XState's createMachine({}): a machine that does nothing.
func xmsgInert(t *testing.T, id string) *sc.Machine[string, counter] {
	t.Helper()

	return xmsgNew(t, id, func(b *xmsgBuilder) {
		b.Initial("idle")
		b.State("idle")
	})
}

// xmsgCallback is a stream standing for an XState callback actor: it calls
// started, sends events back and then stays alive until it is stopped, when
// it calls stopped.
func xmsgCallback[C any](started, stopped func(), events ...any) func(context.Context, C) iter.Seq2[any, error] {
	return func(ctx context.Context, _ C) iter.Seq2[any, error] {
		return func(yield func(any, error) bool) {
			started()
			defer stopped()

			for _, ev := range events {
				if !yield(ev, nil) {
					return
				}
			}

			<-ctx.Done()
		}
	}
}

func xmsgNothing() {}

// xmsgEmitter is the chart most of the emit cases use: someEvent runs an
// action and emits what emit returns for the context.
func xmsgEmitter[T any](t *testing.T, emit func(counter, xmsgSomeEvent) T) *sc.Machine[string, counter] {
	t.Helper()

	return xmsgNew(t, "emitter", func(b *xmsgBuilder) {
		b.Initial("active")
		b.State("active")
		b.On[xmsgSomeEvent]().
			Do("noop", func(context.Context, counter, xmsgSomeEvent) {}).
			Emit("emitted", emit)
	})
}

func xmsgFooBar(counter, xmsgSomeEvent) xmsgEmitted { return xmsgEmitted{Foo: "bar"} }

// ---------------------------------------------------------------------------
// emit.test.ts
// ---------------------------------------------------------------------------

// XState: event emitter > emits events that can be listened to on
// actorRef.on(…)
func TestXMsgEmitEventsCanBeListenedTo(t *testing.T) {
	t.Parallel()

	m := xmsgEmitter(t, xmsgFooBar)

	// The pure core reports the emitted event as an effect, after the
	// action enqueued before it.
	snap, _ := m.Start(counter{N: 0}, epoch)
	_, effects := m.Transition(snap, xmsgSomeEvent{}, epoch)
	require.Equal(t, []sc.EffectKind{sc.EffectAction, sc.EffectEmit}, kinds(effects))
	require.Equal(t, []any{xmsgEmitted{Foo: "bar"}}, xmsgEmits(effects))

	synctest.Test(t, func(t *testing.T) {
		heard := xmsgListener()
		inst := xmsgInstance(t, m, counter{N: 0}, sc.OnEmit(heard.listen))

		xmsgSend(t, inst, xmsgSomeEvent{})

		require.Equal(t, []any{xmsgEmitted{Foo: "bar"}}, heard.all())
	})
}

// XState: event emitter > enqueue.emit(…) emits events that can be listened
// to on actorRef.on(…)
//
// XState's listener is registered for 'emitted' alone, and so never hears the
// 'unknown' event the transition also emits. sc's one listener hears both, in
// the order they were emitted.
func TestXMsgEmitEnqueueEmitEventsCanBeListenedTo(t *testing.T) {
	t.Parallel()

	m := xmsgNew(t, "emitter", func(b *xmsgBuilder) {
		b.Initial("active")
		b.State("active")
		b.On[xmsgSomeEvent]().
			Emit("emitted", xmsgFooBar).
			Emit("unknown", func(counter, xmsgSomeEvent) xmsgUnknown { return xmsgUnknown{} })
	})

	synctest.Test(t, func(t *testing.T) {
		heard := xmsgListener()
		inst := xmsgInstance(t, m, counter{N: 0}, sc.OnEmit(heard.listen))

		xmsgSend(t, inst, xmsgSomeEvent{})

		require.Equal(t, []any{xmsgEmitted{Foo: "bar"}, xmsgUnknown{}}, heard.all())
	})
}

// XState: event emitter > handles errors
//
// The chart also counts, runs an action after the emit, and moves on the
// event, which XState's does not, so that the test can tell what became of
// the step the listener failed in.
//
// As in XState, the listener's failure does not undo the step: the
// transition stands and the instance stays active. XState reports the error
// out of band; sc returns it from Send, wrapping sc.ErrPanicked, after it has
// carried out the rest of the step's effects and published the snapshot.
func TestXMsgEmitHandlesErrors(t *testing.T) {
	t.Parallel()

	after := 0
	m := xmsgNew(t, "emitter", func(b *xmsgBuilder) {
		b.Initial("a")
		b.State("a").On[xmsgSomeEvent]().
			Assign("count", increment[xmsgSomeEvent]).
			Emit("emitted", xmsgFooBar).
			Do("after", func(context.Context, counter, xmsgSomeEvent) { after++ }).
			To("b")
		b.State("b")
	})

	synctest.Test(t, func(t *testing.T) {
		calls := 0
		inst := xmsgInstance(t, m, counter{N: 0}, sc.OnEmit(func(context.Context, any) {
			calls++
			panic("oops")
		}))

		step, err := inst.Send(t.Context(), xmsgSomeEvent{})
		require.ErrorIs(t, err, sc.ErrPanicked)
		require.ErrorContains(t, err, "oops")
		require.True(t, step.Accepted())
		require.Equal(t, 1, calls)
		require.Equal(t, 1, after, "the effects after the emit still run")

		// The step stands: the instance is active, in b, with the count taken.
		require.False(t, inst.Snapshot().Done())
		require.Equal(t, []string{"b"}, inst.Snapshot().Active())
		require.Equal(t, 1, inst.Snapshot().Context.N)
	})
}

// XState: event emitter > dynamically emits events that can be listened to
// on actorRef.on(…)
func TestXMsgEmitDynamicallyEmitsEvents(t *testing.T) {
	t.Parallel()

	m := xmsgEmitter(t, func(c counter, _ xmsgSomeEvent) xmsgCounted { return xmsgCounted{Count: c.N} })

	snap, _ := m.Start(counter{N: 10}, epoch)
	_, effects := m.Transition(snap, xmsgSomeEvent{}, epoch)
	require.Equal(t, []any{xmsgCounted{Count: 10}}, xmsgEmits(effects))

	synctest.Test(t, func(t *testing.T) {
		heard := xmsgListener()
		inst := xmsgInstance(t, m, counter{N: 10}, sc.OnEmit(heard.listen))

		xmsgSend(t, inst, xmsgSomeEvent{})

		require.Equal(t, []any{xmsgCounted{Count: 10}}, heard.all())
	})
}

// XState: event emitter > listener should be able to read the updated
// snapshot of the emitting actor
//
// DIVERGES from XState: XState's listener is called once, after the actor's
// snapshot is updated, and reads 'b'. sc calls the listener while the step's
// effects are being carried out, before the step's snapshot is published, so
// Instance.Snapshot read from the listener is still the state before the
// step: 'a'. The new state is in the step Send returns, and in the snapshot
// once Send has returned.
func TestXMsgEmitListenerReadsSnapshotOfEmittingActor(t *testing.T) {
	t.Parallel()

	m := xmsgNew(t, "emitter", func(b *xmsgBuilder) {
		b.Initial("a")
		b.State("a").On[xmsgEv]().
			Emit("someEvent", func(counter, xmsgEv) xmsgNotice { return xmsgNotice{} }).
			To("b")
		b.State("b")
	})

	synctest.Test(t, func(t *testing.T) {
		var (
			inst *sc.Instance[string, counter]
			seen [][]string
		)
		inst = xmsgInstance(t, m, counter{N: 0}, sc.OnEmit(func(context.Context, any) {
			seen = append(seen, inst.Snapshot().Active())
		}))

		step := xmsgSend(t, inst, xmsgEv{})

		require.Equal(t, [][]string{{"a"}}, seen)
		require.Equal(t, []string{"b"}, step.To.Active())
		require.Equal(t, []string{"b"}, inst.Snapshot().Active())
	})
}

// XState: event emitter > wildcard listeners should be able to receive all
// emitted events
//
// sc's listener is always XState's wildcard: it is given every emitted event.
func TestXMsgEmitWildcardListenersReceiveAllEmittedEvents(t *testing.T) {
	t.Parallel()

	m := xmsgNew(t, "emitter", func(b *xmsgBuilder) {
		b.Initial("active")
		b.State("active")
		b.On[xmsgEvent]().Emit("emitted", func(counter, xmsgEvent) xmsgEmitted { return xmsgEmitted{Foo: ""} })
	})

	synctest.Test(t, func(t *testing.T) {
		heard := xmsgListener()
		inst := xmsgInstance(t, m, counter{N: 0}, sc.OnEmit(heard.listen))

		xmsgSend(t, inst, xmsgEvent{})

		require.Len(t, heard.all(), 1)
	})
}

// XState: event emitter > events can be emitted from async logic
//
// Partial: XState's promise logic is an actor of its own that emits while it
// runs. An sc service has no way to emit, so the promise is a service of a
// chart that emits what the service resolved with.
func TestXMsgEmitFromAsyncLogic(t *testing.T) {
	t.Parallel()

	m := xmsgNew(t, "logic", func(b *xmsgBuilder) {
		run := b.Chart().Invoke("run", func(context.Context, counter) (string, error) { return "hello", nil })
		run.OnDone().Emit("emitted", func(_ counter, d sc.Done[string]) xmsgHello { return xmsgHello{Msg: d.Result} })
		run.OnError().Stay()
		b.Initial("active")
		b.State("active")
	})

	synctest.Test(t, func(t *testing.T) {
		heard := xmsgListener()
		xmsgInstance(t, m, counter{N: 0}, sc.OnEmit(heard.listen))

		synctest.Wait()

		require.Equal(t, []any{xmsgHello{Msg: "hello"}}, heard.all())
	})
}

// XState: event emitter > events can be emitted from custom logic
//
// XState's step logic is a transition function that emits when it is sent
// 'emit'; in sc such logic is a chart with one transition.
func TestXMsgEmitFromCustomLogic(t *testing.T) {
	t.Parallel()

	m := xmsgNew(t, "logic", func(b *xmsgBuilder) {
		b.Initial("active")
		b.State("active")
		b.On[xmsgEmitCmd]().Emit("emitted", func(counter, xmsgEmitCmd) xmsgHello { return xmsgHello{Msg: "hello"} })
		b.On[xmsgOther]().Stay()
	})

	snap, effects := m.Start(counter{N: 0}, epoch)
	require.Empty(t, xmsgEmits(effects))

	_, effects = m.Transition(snap, xmsgOther{}, epoch)
	require.Empty(t, xmsgEmits(effects))

	synctest.Test(t, func(t *testing.T) {
		heard := xmsgListener()
		inst := xmsgInstance(t, m, counter{N: 0}, sc.OnEmit(heard.listen))
		require.Empty(t, heard.all())

		xmsgSend(t, inst, xmsgEmitCmd{})

		require.Equal(t, []any{xmsgHello{Msg: "hello"}}, heard.all())
	})
}

// xmsgRelayed runs the body shared by the three cases that emit from an
// observable, an event observable and a callback: logic that emits as it
// starts and then stays alive without producing anything.
//
// Partial: in XState each logic is an actor of its own with an emit function.
// An sc stream can only yield events to its own machine, so the stream is a
// service of a chart that emits each such event again.
func xmsgRelayed(t *testing.T) {
	t.Helper()

	m := xmsgNew(t, "logic", func(b *xmsgBuilder) {
		b.Chart().Stream("logic", xmsgCallback[counter](xmsgNothing, xmsgNothing, xmsgHello{Msg: "hello"})).OnError().Stay()
		b.On[xmsgHello]().Emit("emitted", func(_ counter, ev xmsgHello) xmsgHello { return ev })
		b.Initial("active")
		b.State("active")
	})

	synctest.Test(t, func(t *testing.T) {
		heard := xmsgListener()
		xmsgInstance(t, m, counter{N: 0}, sc.OnEmit(heard.listen))

		synctest.Wait()

		require.Equal(t, []any{xmsgHello{Msg: "hello"}}, heard.all())
	})
}

// XState: event emitter > events can be emitted from observable logic
func TestXMsgEmitFromObservableLogic(t *testing.T) {
	t.Parallel()

	xmsgRelayed(t)
}

// XState: event emitter > events can be emitted from event observable logic
func TestXMsgEmitFromEventObservableLogic(t *testing.T) {
	t.Parallel()

	xmsgRelayed(t)
}

// XState: event emitter > events can be emitted from callback logic
func TestXMsgEmitFromCallbackLogic(t *testing.T) {
	t.Parallel()

	xmsgRelayed(t)
}

// ---------------------------------------------------------------------------
// spawnChild.test.ts
// ---------------------------------------------------------------------------

// XState: spawnChild action > can spawn
func TestXMsgSpawnChildCanSpawn(t *testing.T) {
	t.Parallel()

	m := xmsgNew(t, "parent", func(b *xmsgBuilder) {
		chart := b.Chart()
		chart.RaiseOnEntry("spawn", func(counter) any { return xmsgSpawn{} })
		chart.Invoke("promise", func(context.Context, counter) (int, error) { return 42, nil }).Spawned().OnError().Stay()
		chart.On[xmsgSpawn]().Spawn("spawnChild", "promise", func(counter, xmsgSpawn) string { return "child" })
		b.Initial("idle")
		b.State("idle")
	})

	snap, effects := m.Start(counter{N: 0}, epoch)

	require.Equal(t, []sc.EffectKind{sc.EffectStartInvocation}, kinds(effects))
	require.Equal(t, []string{"promise:child"}, xmsgRuns(snap))
}

// XState: spawnChild action > can spawn from named actor
//
// XState provides the logic by name with .provide and gives it the input 21.
// In sc the service is declared with Requires and bound with Bind; a plain
// service has no input mapper, so the number travels in the event that
// spawns it, which the service reads with sc.Trigger. XState only checks that
// the child exists; the port also checks what it resolves with.
func TestXMsgSpawnChildCanSpawnFromNamedActor(t *testing.T) {
	t.Parallel()

	declared := xmsgNew(t, "parent", func(b *xmsgBuilder) {
		chart := b.Chart()
		chart.RaiseOnEntry("spawn", func(counter) any { return xmsgSpawnNum{Input: 21} })
		fetch := chart.Requires[int]("fetchNum").Spawned()
		fetch.OnDone().Assign("keep", func(c counter, d sc.Done[int]) counter { c.N = d.Result; return c })
		fetch.OnError().Stay()
		chart.On[xmsgSpawnNum]().Spawn("spawnChild", "fetchNum", func(counter, xmsgSpawnNum) string { return "child" })
		b.Initial("idle")
		b.State("idle")
	})

	m, err := declared.Bind(func(b *sc.Binder[string, counter]) {
		b.Service[int]("fetchNum", func(ctx context.Context, _ counter) (int, error) {
			spawned, _ := sc.Trigger(ctx).(xmsgSpawnNum)
			return spawned.Input * 2, nil
		})
	})
	require.NoError(t, err)

	snap, effects := m.Start(counter{N: 0}, epoch)
	require.Equal(t, []string{"fetchNum:child"}, xmsgRuns(snap))

	var events []any
	require.NoError(t, m.Exec(t.Context(), effect(t, effects, sc.EffectStartInvocation), func(ev any) { events = append(events, ev) }))
	require.Len(t, events, 1)

	snap, _ = m.Transition(snap, events[0], epoch)
	require.Equal(t, 42, snap.Context.N)
	require.Empty(t, xmsgRuns(snap))
}

// XState: spawnChild action > should accept `syncSnapshot` option
//
// Partial: there is no syncSnapshot option. XState's observable is spawned
// with it so that each value reaches the parent as a snapshot event; an sc
// stream's values always reach its machine as events, so the spawned stream
// yields the counts itself.
func TestXMsgSpawnChildSyncSnapshot(t *testing.T) {
	t.Parallel()

	interval := func(ctx context.Context, _ counter) iter.Seq2[any, error] {
		return func(yield func(any, error) bool) {
			for n := 0; ; n++ {
				select {
				case <-ctx.Done():
					return
				case <-time.After(10 * time.Millisecond):
				}

				if !yield(xmsgTick{N: n}, nil) {
					return
				}
			}
		}
	}

	m := xmsgNew(t, "observable", func(b *xmsgBuilder) {
		b.Initial("idle")
		idle := b.State("idle")
		idle.RaiseOnEntry("spawn", func(counter) any { return xmsgSpawn{} })
		idle.Stream("interval", interval).Spawned().OnError().Stay()
		idle.On[xmsgSpawn]().Spawn("spawnInt", "interval", func(counter, xmsgSpawn) string { return "int" })
		idle.On[xmsgTick]().
			When("is five", func(_ counter, ev xmsgTick) bool { return ev.N == 5 }).
			To("success")
		b.Final("success")
	})

	synctest.Test(t, func(t *testing.T) {
		inst := xmsgInstance(t, m, counter{N: 0})
		require.Equal(t, []string{"interval:int"}, xmsgRuns(inst.Snapshot()))

		snap, err := inst.Await(t.Context(), func(s sc.Snapshot[string, counter]) bool { return s.Done() })
		require.NoError(t, err)
		require.Equal(t, []string{"success"}, snap.Active())
		require.Empty(t, xmsgRuns(snap))
	})
}

// XState: spawnChild action > should handle a dynamic id
func TestXMsgSpawnChildHandlesDynamicID(t *testing.T) {
	t.Parallel()

	var spy atomic.Int64

	child := xmsgNew(t, "child", func(b *xmsgBuilder) {
		b.Initial("idle")
		b.State("idle")
		b.On[xmsgFoo]().Do("spy", func(context.Context, counter, xmsgFoo) { spy.Add(1) })
	})

	childID := func(c xmsgChildID, _ xmsgSpawn) string { return c.ChildID }

	m := xmsgNew(t, "parent", func(b *sc.Builder[string, xmsgChildID]) {
		chart := b.Chart()
		chart.RaiseOnEntry("spawn", func(xmsgChildID) any { return xmsgSpawn{} })
		chart.SpawnMachine("child", child,
			func(xmsgChildID, string) counter { return counter{N: 0} },
			xmsgNoOutput[counter],
		).OnError().Stay()
		chart.On[xmsgSpawn]().
			Spawn("spawnChild", "child", childID).
			SendToKey("foo", "child", childID, func(xmsgChildID, xmsgSpawn) any { return xmsgFoo{} })
		b.Initial("idle")
		b.State("idle")
	})

	snap, effects := m.Start(xmsgChildID{ChildID: "myChild"}, epoch)
	require.Equal(t, []sc.EffectKind{sc.EffectStartInvocation, sc.EffectSend}, kinds(effects))
	require.Equal(t, []string{"child:myChild"}, xmsgRuns(snap))

	synctest.Test(t, func(t *testing.T) {
		xmsgInstance(t, m, xmsgChildID{ChildID: "myChild"})

		synctest.Wait()

		require.Equal(t, int64(1), spy.Load())
	})
}

// XState: spawnChild action > does not start a child that is spawned and
// stopped in the same entry
//
// As in XState, the child's logic never runs: sc's core drops the start and
// the stop of a run that is started and stopped within one call.
func TestXMsgSpawnChildSpawnedAndStoppedInSameEntryIsNotStarted(t *testing.T) {
	t.Parallel()

	var started, stopped atomic.Int64

	key := func(counter, xmsgSpawn) string { return "child" }

	m := xmsgNew(t, "parent", func(b *xmsgBuilder) {
		chart := b.Chart()
		chart.RaiseOnEntry("spawn", func(counter) any { return xmsgSpawn{} })
		chart.Stream("callback", xmsgCallback[counter](func() { started.Add(1) }, func() { stopped.Add(1) })).Spawned().OnError().Stay()
		chart.On[xmsgSpawn]().
			Spawn("spawnChild", "callback", key).
			StopSpawned("stopChild", "callback", key)
		b.Initial("idle")
		b.State("idle")
	})

	snap, effects := m.Start(counter{N: 0}, epoch)
	require.Empty(t, kinds(effects))
	require.Empty(t, xmsgRuns(snap))

	synctest.Test(t, func(t *testing.T) {
		inst := xmsgInstance(t, m, counter{N: 0})

		synctest.Wait()

		require.Equal(t, int64(0), started.Load())
		require.Equal(t, int64(0), stopped.Load())
		require.Empty(t, xmsgRuns(inst.Snapshot()))
	})
}

// XState: spawnChild action > does not start a child that is spawned in one
// microstep and stopped in a later microstep of the same macrostep
//
// XState spawns in a's entry, leaves a at once on an eventless transition,
// and stops the child in b's entry, all as the machine starts. In sc a
// transition spawns and stops, so GO spawns on its way into a, and b raises
// on entry the event whose transition stops the child: still one call to
// Transition, so, as in XState, the child's logic never runs.
func TestXMsgSpawnChildSpawnedAndStoppedInSameMacrostepIsNotStarted(t *testing.T) {
	t.Parallel()

	var started, stopped atomic.Int64

	m := xmsgNew(t, "parent", func(b *xmsgBuilder) {
		chart := b.Chart()
		chart.Stream("callback", xmsgCallback[counter](func() { started.Add(1) }, func() { stopped.Add(1) })).Spawned().OnError().Stay()
		chart.On[xmsgStop]().StopSpawned("stopChild", "callback", func(counter, xmsgStop) string { return "child" })
		b.Initial("idle")
		b.State("idle").On[xmsgGo]().
			Spawn("spawnChild", "callback", func(counter, xmsgGo) string { return "child" }).
			To("a")
		b.State("a").Always().To("b")
		b.State("b").RaiseOnEntry("stop", func(counter) any { return xmsgStop{} })
	})

	snap, _ := m.Start(counter{N: 0}, epoch)
	snap, effects := m.Transition(snap, xmsgGo{}, epoch)
	require.Empty(t, kinds(effects))
	require.Equal(t, []string{"b"}, snap.Active())
	require.Empty(t, xmsgRuns(snap))

	synctest.Test(t, func(t *testing.T) {
		inst := xmsgInstance(t, m, counter{N: 0})
		xmsgSend(t, inst, xmsgGo{})

		synctest.Wait()

		require.Equal(t, int64(0), started.Load())
		require.Equal(t, int64(0), stopped.Load())
		require.Equal(t, []string{"b"}, inst.Snapshot().Active())
		require.Empty(t, xmsgRuns(inst.Snapshot()))
	})
}

// XState: spawnChild action > keeps a child spawned on entry when a child
// state with an invoke is entered in the same microstep
//
// The autosave child also runs a stream of its own, which XState's does not,
// so that the test can see it stopped.
// The children are listed as XState lists them, autosave and then loader,
// although sc spawns on the event editor raises on entry: a state's
// invocations start once the whole call has settled, after the raised event
// was processed.
func TestXMsgSpawnChildKeepsChildSpawnedOnEntryBesideInvoke(t *testing.T) {
	t.Parallel()

	var pinged, stopped atomic.Int64

	autosave := xmsgNew(t, "autosave", func(b *xmsgBuilder) {
		b.Chart().Stream("alive", xmsgCallback[counter](xmsgNothing, func() { stopped.Add(1) })).OnError().Stay()
		b.On[xmsgPing]().Do("pinged", func(context.Context, counter, xmsgPing) { pinged.Add(1) })
		b.Initial("idle")
		b.State("idle")
	})
	loader := xmsgInert(t, "loader")

	key := func(counter, xmsgSpawn) string { return "autosave" }

	m := xmsgNew(t, "parent", func(b *xmsgBuilder) {
		b.Initial("idle")
		b.State("idle").On[xmsgOpen]().To("editor")

		editor := b.State("editor", func(b *xmsgBuilder) {
			b.Initial("loading")
			b.State("loading").InvokeMachine("loader", loader, xmsgSame, xmsgNoOutput[counter]).OnError().Stay()
		})
		editor.RaiseOnEntry("spawn", func(counter) any { return xmsgSpawn{} })
		editor.SpawnMachine("autosave", autosave, xmsgSameKeyed, xmsgNoOutput[counter]).OnError().Stay()
		editor.On[xmsgSpawn]().Spawn("spawnAutosave", "autosave", key)
		editor.On[xmsgSave]().SendToKey("ping", "autosave",
			func(counter, xmsgSave) string { return "autosave" },
			func(counter, xmsgSave) any { return xmsgPing{} })
	})

	synctest.Test(t, func(t *testing.T) {
		inst := xmsgInstance(t, m, counter{N: 0})

		step := xmsgSend(t, inst, xmsgOpen{})
		require.Equal(t, []string{"autosave:autosave", "loader"}, xmsgRuns(step.To))

		synctest.Wait()

		xmsgSend(t, inst, xmsgSave{})
		synctest.Wait()
		require.Equal(t, int64(1), pinged.Load())
		require.Equal(t, int64(0), stopped.Load())

		inst.Close()
		require.Equal(t, int64(1), stopped.Load())
	})
}

// XState: spawnChild action > does not bring back a child stopped on entry
// when a child state with an invoke is entered in the same microstep
//
// XState spawns autosave while building the context and stops it in the
// entry action of closed. In sc the chart spawns it on an event raised as it
// starts, and the transition into closed stops it, so the stop and the start
// of the archiver belong to one microstep as they do in XState.
func TestXMsgSpawnChildDoesNotBringBackChildStoppedBesideInvoke(t *testing.T) {
	t.Parallel()

	var stopped atomic.Int64

	autosave := xmsgNew(t, "autosave", func(b *xmsgBuilder) {
		b.Chart().Stream("alive", xmsgCallback[counter](xmsgNothing, func() { stopped.Add(1) })).OnError().Stay()
		b.Initial("idle")
		b.State("idle")
	})
	archiver := xmsgInert(t, "archiver")

	m := xmsgNew(t, "parent", func(b *xmsgBuilder) {
		chart := b.Chart()
		chart.RaiseOnEntry("spawn", func(counter) any { return xmsgSpawn{} })
		chart.SpawnMachine("autosave", autosave, xmsgSameKeyed, xmsgNoOutput[counter]).OnError().Stay()
		chart.On[xmsgSpawn]().Spawn("spawnAutosave", "autosave", func(counter, xmsgSpawn) string { return "autosave" })

		b.Initial("editing")
		b.State("editing").On[xmsgClose]().
			StopSpawned("stopAutosave", "autosave", func(counter, xmsgClose) string { return "autosave" }).
			To("closed")
		b.State("closed", func(b *xmsgBuilder) {
			b.Initial("archiving")
			b.State("archiving").InvokeMachine("archiver", archiver, xmsgSame, xmsgNoOutput[counter]).OnError().Stay()
		})
	})

	snap, _ := m.Start(counter{N: 0}, epoch)
	require.Equal(t, []string{"autosave:autosave"}, xmsgRuns(snap))

	snap, effects := m.Transition(snap, xmsgClose{}, epoch)
	require.Equal(t, []sc.EffectKind{sc.EffectStopInvocation, sc.EffectStartInvocation}, kinds(effects))
	require.Equal(t, []string{"archiver"}, xmsgRuns(snap))

	synctest.Test(t, func(t *testing.T) {
		inst := xmsgInstance(t, m, counter{N: 0})
		synctest.Wait()
		require.Equal(t, int64(0), stopped.Load())

		step := xmsgSend(t, inst, xmsgClose{})
		synctest.Wait()

		require.Equal(t, int64(1), stopped.Load())
		require.Equal(t, []string{"archiver"}, xmsgRuns(step.To))
		require.Equal(t, []string{"archiver"}, xmsgRuns(inst.Snapshot()))
	})
}

// ---------------------------------------------------------------------------
// spawn.test.ts
// ---------------------------------------------------------------------------

// XState: spawn during speculative evaluation > returns an inert ref when
// `can()` evaluates a spawning transition
//
// Partial: sc has no actor references, so everything XState checks about the
// inert reference is dropped. What is left is that asking whether the event
// can be taken says yes and spawns nothing.
func TestXMsgSpawnCanEvaluatesSpawningTransitionWithoutSpawning(t *testing.T) {
	t.Parallel()

	m := xmsgNew(t, "parent", func(b *xmsgBuilder) {
		chart := b.Chart()
		chart.SpawnMachine("worker", xmsgInert(t, "worker"), xmsgSameKeyed, xmsgNoOutput[counter]).OnError().Stay()
		chart.On[xmsgSpawn]().Spawn("spawnWorker", "worker", func(counter, xmsgSpawn) string { return "w" })
		b.Initial("idle")
		b.State("idle")
	})

	snap, _ := m.Start(counter{N: 0}, epoch)

	require.True(t, m.Can(snap, xmsgSpawn{}, epoch))
	require.Empty(t, xmsgRuns(snap))

	// Taking the transition is what spawns.
	next, effects := m.Transition(snap, xmsgSpawn{}, epoch)
	require.Equal(t, []sc.EffectKind{sc.EffectStartInvocation}, kinds(effects))
	require.Equal(t, []string{"worker:w"}, xmsgRuns(next))
	require.Empty(t, xmsgRuns(snap))
}

// XState: spawn during speculative evaluation > gives the inert ref the id
// and address a real spawn would get
//
// Partial: XState evaluates region a's transition twice, once speculatively
// while it resolves the two enabled transitions, and checks that the inert
// reference of the first evaluation has the generated id and address of the
// real one. sc has no references, no addresses and no generated ids: the key
// is given. What is left is that an event two regions take spawns once.
func TestXMsgSpawnInOneOfTwoEnabledRegionsSpawnsOnce(t *testing.T) {
	t.Parallel()

	m := xmsgNew(t, "parent", func(b *xmsgBuilder) {
		b.Regions()
		b.Chart().SpawnMachine("worker", xmsgInert(t, "worker"), xmsgSameKeyed, xmsgNoOutput[counter]).OnError().Stay()

		b.State("a", func(b *xmsgBuilder) {
			b.Initial("a.idle")
			b.State("a.idle").On[xmsgSpawn]().
				Spawn("spawnWorker", "worker", func(counter, xmsgSpawn) string { return "0" }).
				To("a.done")
			b.State("a.done")
		})
		b.State("b", func(b *xmsgBuilder) {
			b.Initial("b.idle")
			b.State("b.idle").On[xmsgSpawn]().To("b.done")
			b.State("b.done")
		})
	})

	snap, _ := m.Start(counter{N: 0}, epoch)
	snap, effects := m.Transition(snap, xmsgSpawn{}, epoch)

	require.Equal(t, []sc.EffectKind{sc.EffectStartInvocation}, kinds(effects))
	require.Equal(t, []string{"worker:0"}, xmsgRuns(snap))
	require.True(t, snap.In("a.done"))
	require.True(t, snap.In("b.done"))
}

// ---------------------------------------------------------------------------
// input.test.ts
// ---------------------------------------------------------------------------

// XState: input > should create a machine with input
//
// XState's context factory maps the input { startCount } to the context
// { count }. In sc the caller builds the context it starts the machine with.
func TestXMsgInputCreatesMachineWithInput(t *testing.T) {
	t.Parallel()

	var spy []int

	m := xmsgNew(t, "machine", func(b *xmsgBuilder) {
		b.Chart().Entry("spy", func(_ context.Context, c counter) { spy = append(spy, c.N) })
		b.Initial("idle")
		b.State("idle")
	})

	fromInput := func(startCount int) counter { return counter{N: startCount} }

	_, effects := m.Start(fromInput(42), epoch)
	require.Equal(t, []string{"machine/spy"}, names(effects))
	require.Equal(t, 42, effects[0].Context.N)

	require.NoError(t, m.Exec(t.Context(), effects[0], func(any) {}))
	require.Equal(t, []int{42}, spy)
}

// XState: input > initial event should have input property
//
// DIVERGES from XState: XState's entry action receives the init event, which
// carries the input. sc has no init event: an entry action is given the
// context only, and its effect carries no event. The input is the context.
func TestXMsgInputInitialEventHasNoInput(t *testing.T) {
	t.Parallel()

	var greeted []string

	m := xmsgNew(t, "machine", func(b *sc.Builder[string, xmsgGreeting]) {
		b.Chart().Entry("check", func(_ context.Context, c xmsgGreeting) { greeted = append(greeted, c.Greeting) })
		b.Initial("idle")
		b.State("idle")
	})

	_, effects := m.Start(xmsgGreeting{Greeting: "hello"}, epoch)
	require.Len(t, effects, 1)
	require.Nil(t, effects[0].Event)
	require.Equal(t, "hello", effects[0].Context.Greeting)

	require.NoError(t, m.Exec(t.Context(), effects[0], func(any) {}))
	require.Equal(t, []string{"hello"}, greeted)
}

// xmsgGreeted is the child of the two cases that give a greeting to a child
// machine: its entry action records the greeting in its context.
func xmsgGreeted(t *testing.T, record func(string)) *sc.Machine[string, xmsgGreeting] {
	t.Helper()

	return xmsgNew(t, "greeted", func(b *sc.Builder[string, xmsgGreeting]) {
		b.Chart().Entry("check", func(_ context.Context, c xmsgGreeting) { record(c.Greeting) })
		b.Initial("idle")
		b.State("idle")
	})
}

// XState: input > should provide input data to invoked machines
//
// Partial: XState also checks the input on the child's init event, which sc
// does not have; the port checks the child's context.
func TestXMsgInputProvidesInputToInvokedMachines(t *testing.T) {
	t.Parallel()

	heard := xmsgListener()
	child := xmsgGreeted(t, func(greeting string) { heard.listen(t.Context(), greeting) })

	m := xmsgNew(t, "machine", func(b *xmsgBuilder) {
		b.Chart().InvokeMachine("child", child,
			func(counter) xmsgGreeting { return xmsgGreeting{Greeting: "hello"} },
			xmsgNoOutput[xmsgGreeting],
		).OnError().Stay()
		b.Initial("idle")
		b.State("idle")
	})

	synctest.Test(t, func(t *testing.T) {
		inst := xmsgInstance(t, m, counter{N: 0})

		synctest.Wait()

		require.Equal(t, []any{"hello"}, heard.all())

		started, ok := sc.Snapshot[string, counter].Child(inst.Snapshot(), child, "child")
		require.True(t, ok)
		require.Equal(t, "hello", started.Context.Greeting)
	})
}

// XState: input > should provide input data to spawned machines
//
// Partial: as in the case before, the check of the init event is dropped.
// XState keeps the spawned child's reference in the context; sc has none to
// keep.
func TestXMsgInputProvidesInputToSpawnedMachines(t *testing.T) {
	t.Parallel()

	heard := xmsgListener()
	child := xmsgGreeted(t, func(greeting string) { heard.listen(t.Context(), greeting) })

	m := xmsgNew(t, "machine", func(b *xmsgBuilder) {
		chart := b.Chart()
		chart.RaiseOnEntry("spawn", func(counter) any { return xmsgSpawn{} })
		chart.SpawnMachine("child", child,
			func(counter, string) xmsgGreeting { return xmsgGreeting{Greeting: "hello"} },
			xmsgNoOutput[xmsgGreeting],
		).OnError().Stay()
		chart.On[xmsgSpawn]().Spawn("spawnChild", "child", func(counter, xmsgSpawn) string { return "ref" })
		b.Initial("idle")
		b.State("idle")
	})

	synctest.Test(t, func(t *testing.T) {
		inst := xmsgInstance(t, m, counter{N: 0})

		synctest.Wait()

		require.Equal(t, []any{"hello"}, heard.all())
		require.Equal(t, []string{"child:ref"}, xmsgRuns(inst.Snapshot()))
	})
}

// xmsgPromise is the chart of the two promise cases: its one service
// resolves with its input, which in sc is the context of its machine.
func xmsgPromise(t *testing.T) *sc.Machine[string, counter] {
	t.Helper()

	return xmsgNew(t, "promise", func(b *xmsgBuilder) {
		b.Initial("running")
		run := b.State("running").Invoke("run", func(_ context.Context, c counter) (xmsgCount, error) {
			return xmsgCount{Count: c.N}, nil
		})
		run.OnDone().To("done")
		run.OnError().Stay()
		b.Final("done")
	})
}

// xmsgResolves runs the promise chart's service with the input 42 and checks
// what it resolves with.
func xmsgResolves(t *testing.T) {
	t.Helper()

	m := xmsgPromise(t)

	snap, effects := m.Start(counter{N: 42}, epoch)
	start := effect(t, effects, sc.EffectStartInvocation)
	require.Equal(t, 42, start.Context.N)

	var events []any
	require.NoError(t, m.Exec(t.Context(), start, func(ev any) { events = append(events, ev) }))
	require.Equal(t, []any{sc.Done[xmsgCount]{ID: start.ID, Result: xmsgCount{Count: 42}}}, events)

	snap, _ = m.Transition(snap, events[0], epoch)
	require.True(t, snap.Done())
}

// XState: input > should create a promise with input
//
// Partial: XState's promise logic is an actor of its own with an input and
// an output. In sc it is a service of a chart: its input is the context the
// chart was started with, and its output the result its Done event carries.
func TestXMsgInputCreatesPromiseWithInput(t *testing.T) {
	t.Parallel()

	xmsgResolves(t)
}

// XState: input > should infer async logic input from schemas
//
// Partial: the case differs from the one before only in what TypeScript
// infers from the schema, which has no counterpart at run time. The input's
// type is the chart's context type, which the Go compiler checks.
func TestXMsgInputInfersAsyncLogicInputFromSchemas(t *testing.T) {
	t.Parallel()

	xmsgResolves(t)
}

// XState: input > should create a transition function actor with input
//
// XState's step logic builds its context from the input; an sc machine is
// started with its context.
func TestXMsgInputCreatesTransitionFunctionActorWithInput(t *testing.T) {
	t.Parallel()

	m := xmsgNew(t, "transition", func(b *xmsgBuilder) {
		b.Initial("active")
		b.State("active")
	})

	snap, effects := m.Start(counter{N: 42}, epoch)

	require.Empty(t, effects)
	require.Equal(t, counter{N: 42}, snap.Context)
}

// XState: input > should create an observable actor with input
//
// Partial: XState's observable is an actor of its own whose snapshot context
// is the value it last produced. In sc it is a stream of a chart, which sees
// the chart's context as its input and yields events to the chart.
func TestXMsgInputCreatesObservableActorWithInput(t *testing.T) {
	t.Parallel()

	m := xmsgNew(t, "observable", func(b *xmsgBuilder) {
		b.Chart().Stream("of", func(_ context.Context, c counter) iter.Seq2[any, error] {
			return func(yield func(any, error) bool) {
				yield(xmsgCount{Count: c.N}, nil)
			}
		}).OnError().Stay()
		b.On[xmsgCount]().Assign("keep", func(c counter, ev xmsgCount) counter { c.N = ev.Count * 2; return c })
		b.Initial("active")
		b.State("active")
	})

	snap, effects := m.Start(counter{N: 42}, epoch)
	start := effect(t, effects, sc.EffectStartInvocation)

	var events []any
	require.NoError(t, m.Exec(t.Context(), start, func(ev any) { events = append(events, ev) }))
	require.Equal(t, []any{
		sc.Emitted{ID: start.ID, Event: xmsgCount{Count: 42}},
		sc.Done[struct{}]{ID: start.ID, Result: struct{}{}},
	}, events)

	snap, _ = m.Transition(snap, events[0], epoch)
	require.Equal(t, 84, snap.Context.N)
}

// XState: input > should create a callback actor with input
//
// Partial: the callback is a stream of a chart, and its input the chart's
// context.
func TestXMsgInputCreatesCallbackActorWithInput(t *testing.T) {
	t.Parallel()

	var inputs []counter

	m := xmsgNew(t, "callback", func(b *xmsgBuilder) {
		b.Chart().Stream("callback", func(_ context.Context, c counter) iter.Seq2[any, error] {
			return func(func(any, error) bool) {
				inputs = append(inputs, c)
			}
		}).OnError().Stay()
		b.Initial("active")
		b.State("active")
	})

	_, effects := m.Start(counter{N: 42}, epoch)
	require.NoError(t, m.Exec(t.Context(), effect(t, effects, sc.EffectStartInvocation), func(any) {}))

	require.Equal(t, []counter{{N: 42}}, inputs)
}

// xmsgInputTo starts a parent with the context start that invokes a child
// with the input the mapper returns, and returns the context the child
// started with.
func xmsgInputTo(t *testing.T, start counter, input func(counter) counter) counter {
	t.Helper()

	child := xmsgInert(t, "child")

	m := xmsgNew(t, "machine", func(b *xmsgBuilder) {
		b.Chart().InvokeMachine("child", child, input, xmsgNoOutput[counter]).OnError().Stay()
		b.Initial("idle")
		b.State("idle")
	})

	var got counter

	synctest.Test(t, func(t *testing.T) {
		inst := xmsgInstance(t, m, start)

		synctest.Wait()

		started, ok := sc.Snapshot[string, counter].Child(inst.Snapshot(), child, "child")
		require.True(t, ok)

		got = started.Context
	})

	return got
}

// XState: input > should provide a static inline input to the referenced
// actor
//
// XState spies on the input the child's context factory is given; in sc the
// input is the context the child starts with.
func TestXMsgInputProvidesStaticInlineInputToReferencedActor(t *testing.T) {
	t.Parallel()

	calls := 0
	got := xmsgInputTo(t, counter{N: 0}, func(counter) counter {
		calls++
		return counter{N: 42}
	})

	require.Equal(t, counter{N: 42}, got)
	require.Equal(t, 1, calls)
}

// XState: input > should provide a dynamic inline input to the referenced
// actor
func TestXMsgInputProvidesDynamicInlineInputToReferencedActor(t *testing.T) {
	t.Parallel()

	got := xmsgInputTo(t, counter{N: 42}, func(c counter) counter { return counter{N: c.N + 100} })

	require.Equal(t, counter{N: 142}, got)
}

// Not ported.
//
// emit.test.ts:
//
//   - event emitter > only emits expected events if specified in schemas
//   - event emitter > emits any events if not specified in schemas (unsafe)
//     Both only create a machine and assert nothing at run time: they check
//     what TypeScript accepts as an emitted event against the schemas. In sc
//     an emitted event is the return value of a typed Go function, which the
//     compiler checks.
//   - event emitter > events can be emitted from callback logic (restored
//     root)
//     Skipped by XState itself (it.skip), and it listens on the reference of
//     an invoked child, which sc does not have.
//
// spawnChild.test.ts:
//
//   - spawnChild action > passes a child spawned on entry to the entry
//     functions of the states entered after it
//     The case is about the children an entry function is given. An sc action
//     is given the context only: it cannot see the running services.
//
// spawn.test.ts:
//
//   - spawn inside machine > input is required when defined in actor
//     It spawns while building the context and looks the child up in the
//     actor system by its registry key. sc has no context factory, no actor
//     system and no registry.
//   - speculative spawn ids > skip ids held by live children, like committed
//     spawns
//     It is about the ids XState generates for children that are given none
//     ('worker:0', 'worker:1') and the references of a speculative
//     evaluation. In sc a spawned run always has the key its Spawn step
//     gives, and there are no references.
//
// input.test.ts:
//
//   - input > should error if input is expected but not provided
//   - input > should retain the machine snapshot interface when resolving
//     input throws
//   - input > should retain the machine snapshot interface and factory error
//     when resolving input throws with result validation
//     All three are about a context factory that throws, for want of an input
//     or on purpose, which leaves the actor in an error status. sc has no
//     context factory and no input apart from the context: Start and Instance
//     take the context as a required argument, and cannot fail on it.
//   - input > should be a type error if input is not expected yet provided
//     A TypeScript check; at run time it only starts a machine without an
//     input, which sc cannot express as different from starting with one.
//   - input > should call the input factory with self when invoking
//     The input mapper of InvokeMachine is given the parent's context only;
//     there is no reference to the parent actor to pass.
