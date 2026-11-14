package sc_test

// Ports of XState's packages/core/test/transition.test.ts (6.0.0-beta.2), one
// test per `it`, in order. The cases that cannot be expressed are listed at
// the end of the file.
//
// XState's initialTransition(machine) is Machine.Start and transition(machine,
// snapshot, event) is Machine.Transition. Both return the next snapshot and
// the effects to carry out. An XState action is an sc EffectAction named
// "<state>/<action>"; '@xstate.start' and '@xstate.spawn' are one
// EffectStartInvocation, '@xstate.stop' is EffectStopInvocation, a delayed
// '@xstate.raise' is EffectStartTimer, '@xstate.cancel' is EffectCancelTimer,
// '@xstate.sendTo' is EffectSend and an emitted event is EffectEmit.
//
// A child that XState spawns from an entry action (enq.spawn) is a service
// declared Spawned on the chart, and started by a transition: an sc state
// cannot spawn as it is entered. XState's session id of a child is the ID of
// the sc service run.

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
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
	xtrnGo           struct{}
	xtrnNext         struct{}
	xtrnInc          struct{}
	xtrnUnknown      struct{}
	xtrnUpdate       struct{}
	xtrnCheck        struct{}
	xtrnChildEv      struct{}
	xtrnPoke         struct{}
	xtrnGoValue      struct{ Value int }
	xtrnMsg          struct{ Msg string }
	xtrnStringAction struct{}
	xtrnObjectAction struct{}
	xtrnSomeEvent    struct{}
	xtrnSpawn        struct{}
	xtrnPing         struct{}
	xtrnLater        struct{}
	xtrnToggle       struct{}
	xtrnKeyEscape    struct{}
	xtrnExit         struct{}
	xtrnEnter        struct{}
	xtrnFinish       struct{}
	xtrnStop         struct{}
	xtrnRestart      struct{}
	xtrnSend         struct{}
	xtrnCancel       struct{}
	xtrnLeft         struct{}
	xtrnRight        struct{}
	xtrnReplace      struct{}
	xtrnReset        struct{}
	xtrnCounted      struct{ Count int }
	xtrnEvent        struct{}
	xtrnSentEv       struct{}
)

// xtrnPair is the context of the chart with two regions that each assign.
type xtrnPair struct {
	A int `json:"a"`
	B int `json:"b"`
}

// xtrnSent is what the welcome email service of the workflow example
// returns.
type xtrnSent struct {
	Status string `json:"status"`
}

type xtrnBuilder = sc.Builder[string, counter]

// xtrnLog records what ran, in order, from any goroutine.
type xtrnLog struct {
	mu    sync.Mutex
	items []string
}

func (l *xtrnLog) add(item string) {
	l.mu.Lock()
	defer l.mu.Unlock()

	l.items = append(l.items, item)
}

func (l *xtrnLog) list() []string {
	l.mu.Lock()
	defer l.mu.Unlock()

	return append([]string{}, l.items...)
}

func xtrnNew(t *testing.T, id string, def func(b *xtrnBuilder), opts ...sc.Option) *sc.Machine[string, counter] {
	t.Helper()

	m, err := sc.New(id, def, opts...)
	require.NoError(t, err)

	return m
}

// xtrnDo is a transition action that does nothing.
func xtrnDo[Ev any](context.Context, counter, Ev) {}

// xtrnListen is XState's createCallbackLogic(() => {}): a service that runs
// until it is stopped and reports nothing.
func xtrnListen(ctx context.Context, _ counter) iter.Seq2[any, error] {
	return func(func(any, error) bool) {
		<-ctx.Done()
	}
}

// xtrnPending is a promise that never settles.
func xtrnPending(ctx context.Context, _ counter) (int, error) {
	<-ctx.Done()

	return 0, fmt.Errorf("stopped: %w", context.Cause(ctx))
}

func xtrnNoOutput(sc.Snapshot[string, counter]) (struct{}, error) { return struct{}{}, nil }

func xtrnSame(c counter) counter { return c }

// xtrnEmpty is XState's createMachine({}): a machine that does nothing.
func xtrnEmpty(t *testing.T) *sc.Machine[string, counter] {
	t.Helper()

	return xtrnNew(t, "empty", func(*xtrnBuilder) {})
}

// xtrnOf returns the effects of one kind.
func xtrnOf(effects []sc.Effect[counter], kind sc.EffectKind) []sc.Effect[counter] {
	var out []sc.Effect[counter]
	for _, e := range effects {
		if e.Kind == kind {
			out = append(out, e)
		}
	}

	return out
}

// xtrnStops returns the names of the services the effects stop, in order.
func xtrnStops(effects []sc.Effect[counter]) []string {
	out := []string{}
	for _, e := range xtrnOf(effects, sc.EffectStopInvocation) {
		out = append(out, e.Name)
	}

	return out
}

// xtrnIDs returns the IDs of the effects of one kind, in order.
func xtrnIDs(effects []sc.Effect[counter], kind sc.EffectKind) []string {
	out := []string{}
	for _, e := range xtrnOf(effects, kind) {
		out = append(out, e.ID)
	}

	return out
}

// xtrnServiceIDs returns the IDs of the snapshot's running services.
func xtrnServiceIDs(snap sc.Snapshot[string, counter]) []string {
	out := []string{}
	for _, s := range snap.Services() {
		out = append(out, s.ID)
	}

	return out
}

// xtrnKey is the key every spawned child of this file runs under.
func xtrnKey[Ev any](counter, Ev) string { return "child" }

// xtrnRejectsTargetlessReenter asserts that sc refuses a chart with a
// transition that re-enters without a target.
func xtrnRejectsTargetlessReenter(t *testing.T, def func(b *xtrnBuilder, reenter bool)) {
	t.Helper()

	_, err := sc.New("m", func(b *xtrnBuilder) { def(b, true) })
	require.ErrorContains(t, err, `state "active": a transition without a target cannot re-enter`)
}

// XState: transition function > targetless reentry > does not return
// lifecycle effects with target %j and reenter: true
//
// XState runs the case for target undefined and for target []; both are an
// sc transition without To.
func TestXTrnTargetlessReentryReturnsNoLifecycleEffects(t *testing.T) {
	t.Parallel()

	def := func(b *xtrnBuilder, reenter bool) {
		b.Initial("active")
		g := b.State("active").Entry("entry", noop).Exit("exit", noop).On[xtrnGo]()
		if reenter {
			g.Reenter()
		} else {
			g.Stay()
		}
	}

	// DIVERGES from XState: XState accepts reenter: true on a transition
	// without a target and ignores it. sc rejects the declaration. The
	// same transition without Reenter, which must then be marked Stay
	// because it has no steps either, does what XState expects.
	xtrnRejectsTargetlessReenter(t, def)

	m := xtrnNew(t, "m", func(b *xtrnBuilder) { def(b, false) })

	initial, _ := m.Start(counter{N: 0}, epoch)
	next, effects := m.Transition(initial, xtrnGo{}, epoch)

	require.Equal(t, []string{"active"}, next.Active())
	require.Empty(t, effects)
}

// XState: transition function > targetless reentry > preserves transition
// effects and context updates with dynamic target %j
func TestXTrnTargetlessReentryPreservesTransitionEffectsAndContext(t *testing.T) {
	t.Parallel()

	var calls atomic.Int64

	def := func(b *xtrnBuilder, reenter bool) {
		b.Initial("active")
		g := b.State("active").Entry("entry", noop).Exit("exit", noop).
			On[xtrnGo]().
			Do("transition", func(context.Context, counter, xtrnGo) { calls.Add(1) }).
			Assign("count", increment[xtrnGo])
		if reenter {
			g.Reenter()
		}
	}

	// DIVERGES from XState: XState accepts reenter: true on a transition
	// without a target and ignores it. sc rejects the declaration; without
	// Reenter the transition does what XState expects.
	xtrnRejectsTargetlessReenter(t, def)

	m := xtrnNew(t, "m", func(b *xtrnBuilder) { def(b, false) })

	initial, _ := m.Start(counter{N: 0}, epoch)
	next, effects := m.Transition(initial, xtrnGo{}, epoch)

	require.Equal(t, []string{"active"}, next.Active())
	require.Equal(t, counter{N: 1}, next.Context)
	require.Equal(t, []string{"active/transition"}, names(effects))
	require.Equal(t, counter{N: 0}, effects[0].Context)
	require.Zero(t, calls.Load())
}

// XState: transition function > targetless reentry > still returns exit and
// entry effects for explicit self-reentry
func TestXTrnExplicitSelfReentryReturnsExitAndEntryEffects(t *testing.T) {
	t.Parallel()

	m := xtrnNew(t, "m", func(b *xtrnBuilder) {
		b.Initial("active")
		b.State("active").Entry("entry", noop).Exit("exit", noop).
			On[xtrnGo]().To("active").Reenter()
	})

	initial, _ := m.Start(counter{N: 0}, epoch)
	next, effects := m.Transition(initial, xtrnGo{}, epoch)

	require.Equal(t, []string{"active"}, next.Active())
	require.Equal(t, []string{"active/exit", "active/entry"}, names(effects))
}

// XState: transition function > targetless reentry > does not reenter a
// targetless source when another parallel region changes state
func TestXTrnTargetlessSourceNotReenteredWhenAnotherRegionChanges(t *testing.T) {
	t.Parallel()

	def := func(b *xtrnBuilder, reenter bool) {
		b.Regions()
		b.State("left", func(b *xtrnBuilder) {
			b.Initial("active")
			g := b.State("active").Entry("entry left", noop).Exit("exit left", noop).On[xtrnGo]()
			if reenter {
				g.Reenter()
			} else {
				g.Stay()
			}
		})
		b.State("right", func(b *xtrnBuilder) {
			b.Initial("before")
			b.State("before").Exit("exit right", noop).On[xtrnGo]().To("after")
			b.State("after").Entry("entry right", noop)
		})
	}

	// DIVERGES from XState: XState accepts reenter: true on a transition
	// without a target. sc rejects the declaration; with the transition
	// marked Stay instead, the step does what XState expects.
	xtrnRejectsTargetlessReenter(t, def)

	m := xtrnNew(t, "m", func(b *xtrnBuilder) { def(b, false) })

	initial, _ := m.Start(counter{N: 0}, epoch)
	next, effects := m.Transition(initial, xtrnGo{}, epoch)

	require.True(t, next.In("active"))
	require.True(t, next.In("after"))
	require.Equal(t, []string{"before/exit right", "after/entry right"}, names(effects))
}

// XState: transition function > targetless reentry > reenters the region of
// a targetless source when another region reenters the parallel parent
func TestXTrnTargetlessSourceReenteredWhenParallelParentReenters(t *testing.T) {
	t.Parallel()

	def := func(b *xtrnBuilder, reenter bool) {
		b.Initial("parent")
		b.Parallel("parent", func(b *xtrnBuilder) {
			b.State("left", func(b *xtrnBuilder) {
				b.Initial("active")
				g := b.State("active").Entry("entry", noop).Exit("exit", noop).On[xtrnGo]()
				if reenter {
					g.Reenter()
				} else {
					g.Stay()
				}
			}).Entry("entry", noop).Exit("exit", noop)
			b.State("right").Entry("entry", noop).Exit("exit", noop).
				On[xtrnGo]().To("parent").Reenter()
		}).Entry("entry", noop).Exit("exit", noop)
	}

	// DIVERGES from XState: XState accepts reenter: true on a transition
	// without a target. sc rejects the declaration; with the transition
	// marked Stay instead, the step does what XState expects.
	xtrnRejectsTargetlessReenter(t, def)

	m := xtrnNew(t, "m", func(b *xtrnBuilder) { def(b, false) })

	initial, _ := m.Start(counter{N: 0}, epoch)
	next, effects := m.Transition(initial, xtrnGo{}, epoch)

	require.Equal(t, []string{"parent", "left", "active", "right"}, next.Active())
	require.Equal(t, []string{
		"right/exit",
		"active/exit",
		"left/exit",
		"parent/exit",
		"parent/entry",
		"left/entry",
		"active/entry",
		"right/entry",
	}, names(effects))
}

// XState: transition function > targetless reentry > preserves an active
// descendant without rerunning parent entry in a running actor
//
// XState runs the actor; the actions that would run are the effects the
// pure transition returns.
func TestXTrnTargetlessReentryPreservesActiveDescendant(t *testing.T) {
	t.Parallel()

	def := func(b *xtrnBuilder, reenter bool) {
		b.Initial("active")
		g := b.State("active", func(b *xtrnBuilder) {
			b.Initial("first")
			b.State("first").On[xtrnNext]().To("second")
			b.State("second").Entry("entry second", noop).Exit("exit second", noop)
		}).Entry("entry active", noop).Exit("exit active", noop).
			On[xtrnGo]().
			Do("transition", xtrnDo[xtrnGo]).
			Assign("count", increment[xtrnGo])
		if reenter {
			g.Reenter()
		}
	}

	// DIVERGES from XState: XState accepts reenter: true on a transition
	// without a target and ignores it. sc rejects the declaration; without
	// Reenter the transition does what XState expects.
	xtrnRejectsTargetlessReenter(t, def)

	m := xtrnNew(t, "m", func(b *xtrnBuilder) { def(b, false) })

	snap, _ := m.Start(counter{N: 0}, epoch)
	snap, _ = m.Transition(snap, xtrnNext{}, epoch)
	snap, effects := m.Transition(snap, xtrnGo{}, epoch)

	require.Equal(t, []string{"active", "second"}, snap.Active())
	require.Equal(t, counter{N: 1}, snap.Context)
	require.Equal(t, []string{"active/transition"}, names(effects))
}

// XState: transition function > does not materialize actors or systems for
// context-only planning
//
// Ported: a hundred context-only transitions on the chart itself, and a
// hundred unknown events that change nothing. Dropped: the count of actor
// and system materializations, which sc has no notion of.
func TestXTrnContextOnlyPlanning(t *testing.T) {
	t.Parallel()

	m := xtrnNew(t, "m", func(b *xtrnBuilder) {
		b.On[xtrnInc]().Assign("count", increment[xtrnInc])
	})

	snap, effects := m.Start(counter{N: 0}, epoch)
	require.Empty(t, effects)

	for range 100 {
		snap, effects = m.Transition(snap, xtrnInc{}, epoch)
		require.Empty(t, effects)
	}
	require.Equal(t, 100, snap.Context.N)

	seq := snap.Seq()
	for range 100 {
		snap, effects = m.Transition(snap, xtrnUnknown{}, epoch)
		require.Empty(t, effects)
	}
	require.Equal(t, seq, snap.Seq())
}

// XState: transition function > does not repeatedly resolve a selected
// transition during a microstep
func TestXTrnSelectedTransitionResolvedOncePerMicrostep(t *testing.T) {
	t.Parallel()

	var calls atomic.Int64

	m := xtrnNew(t, "m", func(b *xtrnBuilder) {
		b.On[xtrnUpdate]().Assign("update", func(c counter, _ xtrnUpdate) counter {
			calls.Add(1)
			c.N++

			return c
		})
	})

	snap, _ := m.Start(counter{N: 0}, epoch)
	snap, _ = m.Transition(snap, xtrnUpdate{}, epoch)

	require.Equal(t, int64(1), calls.Load())
	require.Equal(t, counter{N: 1}, snap.Context)
}

// XState: transition function > does not repeatedly resolve object
// transition mappers during a microstep
//
// Dropped: the input mapper of the transition, which gives the target state
// an input; sc states take none.
func TestXTrnObjectTransitionMappersResolvedOncePerMicrostep(t *testing.T) {
	t.Parallel()

	var calls atomic.Int64

	mapper := func(c counter) counter {
		calls.Add(1)
		c.N++

		return c
	}

	m := xtrnNew(t, "m", func(b *xtrnBuilder) {
		b.Initial("idle")
		idle := b.State("idle")
		idle.On[xtrnUpdate]().Assign("context", func(c counter, _ xtrnUpdate) counter { return mapper(c) })
		idle.On[xtrnGo]().Assign("context", func(c counter, _ xtrnGo) counter { return mapper(c) }).To("active")
		b.State("active")
	})

	snap, _ := m.Start(counter{N: 0}, epoch)

	snap, _ = m.Transition(snap, xtrnUpdate{}, epoch)
	require.Equal(t, int64(1), calls.Load())

	snap, _ = m.Transition(snap, xtrnGo{}, epoch)
	require.Equal(t, int64(2), calls.Load())
	require.Equal(t, counter{N: 2}, snap.Context)
}

// XState: transition function > does not resolve object transition mappers
// while removing conflicting transitions
func TestXTrnMappersNotResolvedWhileRemovingConflictingTransitions(t *testing.T) {
	t.Parallel()

	var callsA, callsB atomic.Int64

	m, err := sc.New("m", func(b *sc.Builder[string, xtrnPair]) {
		b.Regions()
		b.State("a", func(b *sc.Builder[string, xtrnPair]) {
			b.Initial("a.idle")
			b.State("a.idle").On[xtrnGo]().
				Assign("contextA", func(c xtrnPair, _ xtrnGo) xtrnPair {
					callsA.Add(1)
					c.A = 1

					return c
				}).
				To("a.done")
			b.State("a.done")
		})
		b.State("b", func(b *sc.Builder[string, xtrnPair]) {
			b.Initial("b.idle")
			b.State("b.idle").On[xtrnGo]().
				Assign("contextB", func(c xtrnPair, _ xtrnGo) xtrnPair {
					callsB.Add(1)
					c.B = 1

					return c
				}).
				To("b.done")
			b.State("b.done")
		})
	})
	require.NoError(t, err)

	snap, _ := m.Start(xtrnPair{A: 0, B: 0}, epoch)
	snap, _ = m.Transition(snap, xtrnGo{}, epoch)

	require.Equal(t, int64(1), callsA.Load())
	require.Equal(t, int64(1), callsB.Load())
	require.Equal(t, xtrnPair{A: 1, B: 1}, snap.Context)
}

// XState: transition function > does not send to the parent during
// transition selection
//
// XState sends CHECK to the child through its actor ref and the child sends
// CHILD to its parent ref; here the parent passes POKE on to the child as
// CHECK and the child emits CHILD. The parent must hear it once.
func TestXTrnDoesNotSendToParentDuringTransitionSelection(t *testing.T) {
	t.Parallel()

	synctest.Test(t, func(t *testing.T) {
		child := xtrnNew(t, "child", func(b *xtrnBuilder) {
			b.On[xtrnCheck]().Emit("tell parent", func(counter, xtrnCheck) xtrnChildEv { return xtrnChildEv{} })
		})

		parent := xtrnNew(t, "parent", func(b *xtrnBuilder) {
			b.Chart().InvokeMachine("child", child, xtrnSame, xtrnNoOutput).OnError().Stay()
			b.On[xtrnPoke]().SendTo("check", "child", func(counter, xtrnPoke) any { return xtrnCheck{} })
			b.On[xtrnChildEv]().Assign("received", increment[xtrnChildEv])
		})

		inst, err := parent.Instance(t.Context(), counter{N: 0})
		require.NoError(t, err)
		defer inst.Close()

		synctest.Wait()

		_, err = inst.Send(t.Context(), xtrnPoke{})
		require.NoError(t, err)

		synctest.Wait()

		require.Equal(t, 1, inst.Snapshot().Context.N)
	})
}

// XState: transition function > resolves mapper context on object
// transitions
func TestXTrnResolvesMapperContextOnObjectTransitions(t *testing.T) {
	t.Parallel()

	m := xtrnNew(t, "m", func(b *xtrnBuilder) {
		b.Initial("idle")
		b.State("idle").On[xtrnGoValue]().
			Assign("context", func(_ counter, ev xtrnGoValue) counter { return counter{N: ev.Value} }).
			To("done")
		b.Final("done").Output(func(c counter) int { return c.N })
	})

	snap, _ := m.Start(counter{N: 0}, epoch)
	snap, _ = m.Transition(snap, xtrnGoValue{Value: 42}, epoch)

	require.Equal(t, 42, snap.Context.N)

	output, ok := m.Output(snap)
	require.True(t, ok)
	require.Equal(t, 42, output)
}

// XState: transition function > resolves mapper context on invoke onDone
// object transitions
//
// The promise is run by handing the start effect to Machine.Exec, and its
// result is given back to the pure core.
func TestXTrnResolvesMapperContextOnInvokeOnDone(t *testing.T) {
	t.Parallel()

	m := xtrnNew(t, "m", func(b *xtrnBuilder) {
		b.Initial("pending")
		inv := b.State("pending").Invoke("promise", func(context.Context, counter) (int, error) { return 42, nil })
		inv.OnDone().
			Assign("context", func(_ counter, d sc.Done[int]) counter { return counter{N: d.Result} }).
			To("done")
		inv.OnError().Stay()
		b.Final("done").Output(func(c counter) int { return c.N })
	})

	snap, effects := m.Start(counter{N: 0}, epoch)
	require.Equal(t, []sc.EffectKind{sc.EffectStartInvocation}, kinds(effects))

	var results []any
	require.NoError(t, m.Exec(t.Context(), effects[0], func(ev any) { results = append(results, ev) }))
	require.Equal(t, []any{sc.Done[int]{ID: effects[0].ID, Result: 42}}, results)

	snap, _ = m.Transition(snap, results[0], epoch)

	require.True(t, snap.Done())
	require.Equal(t, 42, snap.Context.N)
}

// XState: transition function > should capture actions
func TestXTrnCapturesActions(t *testing.T) {
	t.Parallel()

	var calls atomic.Int64

	m := xtrnNew(t, "m", func(b *xtrnBuilder) {
		b.Chart().
			AssignOnEntry("count", func(counter) counter { return counter{N: 100} }).
			Entry("actionWithParams", func(context.Context, counter) { calls.Add(1) }).
			Entry("stringAction", func(context.Context, counter) { calls.Add(1) }).
			On[xtrnMsg]().Do("actionWithDynamicParams", func(context.Context, counter, xtrnMsg) { calls.Add(1) })
	})

	state0, actions0 := m.Start(counter{N: 0}, epoch)

	require.Equal(t, 100, state0.Context.N)
	require.Equal(t, []sc.EffectKind{sc.EffectAction, sc.EffectAction}, kinds(actions0))
	require.Equal(t, []string{"m/actionWithParams", "m/stringAction"}, names(actions0))
	require.Zero(t, calls.Load())

	state1, actions1 := m.Transition(state0, xtrnMsg{Msg: "hello"}, epoch)

	require.Equal(t, 100, state1.Context.N)
	require.Equal(t, []string{"m/actionWithDynamicParams"}, names(actions1))
	require.Equal(t, xtrnMsg{Msg: "hello"}, actions1[0].Event)
	require.Equal(t, counter{N: 100}, actions1[0].Context)
	require.Zero(t, calls.Load())
}

// XState: transition function > should not execute a referenced serialized
// action
//
// XState's referenced action is an sc chart action, declared once with
// Builder.Action and run by name.
func TestXTrnDoesNotExecuteReferencedAction(t *testing.T) {
	t.Parallel()

	var calls atomic.Int64

	m := xtrnNew(t, "m", func(b *xtrnBuilder) {
		b.Action("foo", func(context.Context, counter, sc.Move[string]) { calls.Add(1) })
		b.Chart().OnEntry("foo")
	})

	_, actions := m.Start(counter{N: 0}, epoch)

	require.Equal(t, []string{"m/foo"}, names(actions))
	require.Zero(t, calls.Load())
}

// XState: transition function > should capture enqueued actions
func TestXTrnCapturesEnqueuedActions(t *testing.T) {
	t.Parallel()

	m := xtrnNew(t, "m", func(b *xtrnBuilder) {
		b.Chart().
			EmitOnEntry("stringAction", func(counter) xtrnStringAction { return xtrnStringAction{} }).
			EmitOnEntry("objectAction", func(counter) xtrnObjectAction { return xtrnObjectAction{} })
	})

	_, actions := m.Start(counter{N: 0}, epoch)

	require.Equal(t, []sc.EffectKind{sc.EffectEmit, sc.EffectEmit}, kinds(actions))
	require.Equal(t, xtrnStringAction{}, actions[0].Event)
	require.Equal(t, xtrnObjectAction{}, actions[1].Event)
}

// xtrnDelayedRaise is the chart of the two cases whose state a raises NEXT
// after 10ms from its entry action.
func xtrnDelayedRaise(t *testing.T, cancel bool) *sc.Machine[string, counter] {
	t.Helper()

	return xtrnNew(t, "m", func(b *xtrnBuilder) {
		b.Initial("a")
		next := b.State("a").
			RaiseAfterOnEntry("myRaise", 10*time.Millisecond, func(counter) any { return xtrnNext{} }).
			On[xtrnNext]()
		if cancel {
			next.CancelRaise("myRaise")
		}
		next.To("b")
		b.State("b")
	})
}

// XState: transition function > delayed raise actions should be returned
//
// XState marks this case it.todo; sc returns the delayed raise as a timer.
func TestXTrnDelayedRaiseActionsAreReturned(t *testing.T) {
	t.Parallel()

	m := xtrnDelayedRaise(t, false)
	state, actions := m.Start(counter{N: 0}, epoch)

	require.Equal(t, []string{"a"}, state.Active())
	require.Equal(t, []sc.EffectKind{sc.EffectStartTimer}, kinds(actions))
	require.Equal(t, "myRaise", actions[0].Name)
	require.Equal(t, 10*time.Millisecond, actions[0].Delay)
	require.Equal(t, xtrnNext{}, state.Timers()[0].Raised)
}

// XState: transition function > raise actions related to delayed
// transitions should be returned
func TestXTrnRaiseActionsOfDelayedTransitionsAreReturned(t *testing.T) {
	t.Parallel()

	m := xtrnNew(t, "m", func(b *xtrnBuilder) {
		b.Initial("a")
		b.State("a").After(10 * time.Millisecond).To("b")
		b.State("b")
	})

	state, actions := m.Start(counter{N: 0}, epoch)

	require.Equal(t, []string{"a"}, state.Active())
	require.Equal(t, sc.EffectStartTimer, actions[0].Kind)
	require.Equal(t, "a", actions[0].State)
	require.Equal(t, 10*time.Millisecond, actions[0].Delay)
	require.Equal(t, epoch.Add(10*time.Millisecond), actions[0].At)

	// XState's raise has no id; an sc timer for a fixed delay has no name.
	require.Empty(t, actions[0].Name)
}

// XState: transition function > cancel action should be returned
func TestXTrnCancelActionIsReturned(t *testing.T) {
	t.Parallel()

	m := xtrnDelayedRaise(t, true)
	state, started := m.Start(counter{N: 0}, epoch)

	require.Equal(t, []string{"a"}, state.Active())

	next, actions := m.Transition(state, xtrnNext{}, epoch)

	require.Equal(t, []string{"b"}, next.Active())
	require.Equal(t, []sc.EffectKind{sc.EffectCancelTimer}, kinds(actions))
	require.Equal(t, started[0].ID, actions[0].ID)
	require.Equal(t, "myRaise", actions[0].Name)
	require.Empty(t, next.Timers())
}

// XState: transition function > sendTo action should be returned
func TestXTrnSendToActionIsReturned(t *testing.T) {
	t.Parallel()

	m := xtrnNew(t, "m", func(b *xtrnBuilder) {
		b.Chart().InvokeMachine("someActor", xtrnEmpty(t), xtrnSame, xtrnNoOutput).OnError().Stay()
		b.Initial("a")
		b.State("a").On[xtrnNext]().SendTo("send", "someActor", func(counter, xtrnNext) any { return xtrnSomeEvent{} })
	})

	state, actions0 := m.Start(counter{N: 0}, epoch)

	require.Equal(t, []string{"a"}, state.Active())

	start := effect(t, actions0, sc.EffectStartInvocation)
	require.Equal(t, "someActor", start.Name)
	require.Equal(t, state.Services()[0].ID, start.ID)

	_, actions := m.Transition(state, xtrnNext{}, epoch)

	send := effect(t, actions, sc.EffectSend)
	require.Equal(t, start.ID, send.ID)
	require.Equal(t, xtrnSomeEvent{}, send.Event)
}

// XState: transition function > enq.sendTo with an undefined actor does not
// return a sendTo action from a transition function
func TestXTrnSendToMissingActorReturnsNoSendToAction(t *testing.T) {
	t.Parallel()

	// DIVERGES from XState: XState resolves children.missing when the
	// transition runs and quietly returns no sendTo. sc names the service
	// when the chart is declared, and a name no state invokes is a
	// declaration error.
	_, err := sc.New("m", func(b *xtrnBuilder) {
		b.Initial("a")
		b.State("a").On[xtrnNext]().SendTo("send", "missing", func(counter, xtrnNext) any { return xtrnSomeEvent{} })
	})
	require.ErrorContains(t, err, "missing")

	// The nearest case sc can run: the service exists but is not running,
	// because its state is not active. No send is returned.
	m := xtrnNew(t, "m", func(b *xtrnBuilder) {
		b.Initial("a")
		a := b.State("a")
		a.On[xtrnNext]().SendTo("send", "missing", func(counter, xtrnNext) any { return xtrnSomeEvent{} })
		a.On[xtrnGo]().To("b")
		b.State("b").Stream("missing", xtrnListen).OnError().Stay()
	})

	state, _ := m.Start(counter{N: 0}, epoch)
	next, actions := m.Transition(state, xtrnNext{}, epoch)

	require.NotContains(t, kinds(actions), sc.EffectSend)
	require.False(t, next.Done())
}

// XState: transition function > enq.spawn creates and starts a child once
// from a transition function
func TestXTrnSpawnCreatesAndStartsChildOnce(t *testing.T) {
	t.Parallel()

	synctest.Test(t, func(t *testing.T) {
		var constructions atomic.Int64

		child := xtrnNew(t, "child", func(b *xtrnBuilder) {
			b.On[xtrnPing]().Assign("n", increment[xtrnPing])
		})

		parent := xtrnNew(t, "parent", func(b *xtrnBuilder) {
			b.Chart().SpawnMachine("child", child, func(counter, string) counter {
				constructions.Add(1)

				return counter{N: 0}
			}, xtrnNoOutput).OnError().Stay()
			b.On[xtrnSpawn]().Spawn("spawn", "child", xtrnKey[xtrnSpawn])
			b.On[xtrnPing]().SendToKey("ping", "child", xtrnKey[xtrnPing], func(counter, xtrnPing) any { return xtrnPing{} })
		})

		inst, err := parent.Instance(t.Context(), counter{N: 0})
		require.NoError(t, err)
		defer inst.Close()

		_, err = inst.Send(t.Context(), xtrnSpawn{})
		require.NoError(t, err)

		synctest.Wait()
		require.Equal(t, int64(1), constructions.Load())

		_, err = inst.Send(t.Context(), xtrnPing{})
		require.NoError(t, err)

		synctest.Wait()

		got, ok := inst.Snapshot().Child(child, "child")
		require.True(t, ok)
		require.Equal(t, counter{N: 1}, got.Context)
		require.Equal(t, int64(1), constructions.Load())
	})
}

// XState: transition function > built-in action effects expose public
// metadata fields
//
// Ported: the start effect of the invoked child, and the spawn, the delayed
// raise, the send and the cancel of one transition. Dropped: the delay and
// id of the send, which sc sends have none of, and the stop of the invoked
// child, which only leaving its state does in sc.
func TestXTrnBuiltInActionEffectsExposeMetadata(t *testing.T) {
	t.Parallel()

	chart := func(cancel bool) *sc.Machine[string, counter] {
		return xtrnNew(t, "m", func(b *xtrnBuilder) {
			chart := b.Chart()
			chart.InvokeMachine("child", xtrnEmpty(t), func(counter) counter { return counter{N: 7} }, xtrnNoOutput).OnError().Stay()
			chart.Invoke("spawned", xtrnPending).Spawned().OnError().Stay()
			chart.On[xtrnLater]().Stay()

			b.Initial("a")
			a := b.State("a")
			next := a.On[xtrnNext]().
				Spawn("spawn", "spawned", xtrnKey[xtrnNext]).
				RaiseAfter("raise-id", 10*time.Millisecond, func(counter, xtrnNext) any { return xtrnLater{} }).
				SendTo("send-id", "child", func(counter, xtrnNext) any { return xtrnPing{} })
			if cancel {
				next.CancelRaise("raise-id")
			} else {
				a.On[xtrnCancel]().CancelRaise("raise-id")
			}
		})
	}

	m := chart(false)
	state, initial := m.Start(counter{N: 1}, epoch)

	invoke := effect(t, initial, sc.EffectStartInvocation)
	require.Equal(t, "child", invoke.Name)
	require.Equal(t, "m", invoke.State)
	require.Equal(t, state.Services()[0].ID, invoke.ID)

	// The effect carries the parent's context; the input function turns it
	// into the child's when the effect is carried out.
	require.Equal(t, counter{N: 1}, invoke.Context)

	next, actions := m.Transition(state, xtrnNext{}, epoch)

	require.Equal(t, []sc.EffectKind{sc.EffectStartInvocation, sc.EffectStartTimer, sc.EffectSend}, kinds(actions))

	spawned := actions[0]
	require.Equal(t, "spawned", spawned.Name)
	key, ok := sc.SpawnKey(spawned.ID)
	require.True(t, ok)
	require.Equal(t, "child", key)

	raise := actions[1]
	require.Equal(t, "raise-id", raise.Name)
	require.Equal(t, 10*time.Millisecond, raise.Delay)
	require.Equal(t, xtrnLater{}, next.Timers()[0].Raised)

	send := actions[2]
	require.Equal(t, xtrnPing{}, send.Event)
	require.Equal(t, invoke.ID, send.ID)

	require.Len(t, next.Services(), 2)

	after, cancelled := m.Transition(next, xtrnCancel{}, epoch)
	require.Equal(t, []sc.EffectKind{sc.EffectCancelTimer}, kinds(cancelled))
	require.Equal(t, "raise-id", cancelled[0].Name)
	require.Equal(t, raise.ID, cancelled[0].ID)
	require.Empty(t, after.Timers())

	// DIVERGES from XState: XState returns both the raise and the cancel
	// of a transition that schedules an event and cancels it at once. sc
	// returns neither: the timer is never started.
	m = chart(true)
	state, _ = m.Start(counter{N: 1}, epoch)
	next, actions = m.Transition(state, xtrnNext{}, epoch)

	require.Equal(t, []sc.EffectKind{sc.EffectStartInvocation, sc.EffectSend}, kinds(actions))
	require.Empty(t, next.Timers())
}

// xtrnInvoking is the chart most of the "invoke stop effects" cases use:
// "active" invokes a listener called child and leaves for "inactive" on
// EXIT.
func xtrnInvoking(t *testing.T, more func(b *xtrnBuilder, active *sc.StateBuilder[string, counter])) *sc.Machine[string, counter] {
	t.Helper()

	return xtrnNew(t, "m", func(b *xtrnBuilder) {
		b.Initial("active")
		active := b.State("active")
		active.Stream("child", xtrnListen).OnError().Stay()
		active.On[xtrnExit]().To("inactive")
		b.State("inactive")
		if more != nil {
			more(b, active)
		}
	})
}

// XState: transition function > invoke stop effects > returns an
// @xstate.stop effect when an invoking state exits
func TestXTrnStopEffectWhenInvokingStateExits(t *testing.T) {
	t.Parallel()

	m := xtrnNew(t, "player", func(b *xtrnBuilder) {
		b.Initial("mini")
		b.State("mini").On[xtrnToggle]().To("full")
		full := b.State("full")
		full.Stream("keyEscape", xtrnListen).OnError().Stay()
		full.On[xtrnKeyEscape]().To("mini")
	})

	initial, _ := m.Start(counter{N: 0}, epoch)
	full, _ := m.Transition(initial, xtrnToggle{}, epoch)
	child := full.Services()[0]
	mini, effects := m.Transition(full, xtrnKeyEscape{}, epoch)

	require.Empty(t, mini.Services())

	stops := xtrnOf(effects, sc.EffectStopInvocation)
	require.Len(t, stops, 1)
	require.Equal(t, child.ID, stops[0].ID)
	require.Equal(t, "keyEscape", stops[0].Name)
	require.Equal(t, "full", stops[0].State)
}

// XState: transition function > invoke stop effects > orders an invoke stop
// after its exit action
func TestXTrnInvokeStopOrderedAfterExitAction(t *testing.T) {
	t.Parallel()

	m := xtrnInvoking(t, func(_ *xtrnBuilder, active *sc.StateBuilder[string, counter]) {
		active.Exit("exitAction", noop)
	})

	active, _ := m.Start(counter{N: 0}, epoch)
	_, effects := m.Transition(active, xtrnExit{}, epoch)

	require.Equal(t, []sc.EffectKind{sc.EffectAction, sc.EffectStopInvocation}, kinds(effects))
	require.Equal(t, []string{"active/exitAction", "active/child"}, names(effects))
}

// XState: transition function > invoke stop effects > does not add a
// placeholder effect for a one-argument exit function
//
// XState runs a one-argument exit function inline and returns no effect for
// it. What sc runs inline on exit is an assignment.
func TestXTrnNoPlaceholderEffectForInlineExitFunction(t *testing.T) {
	t.Parallel()

	var calls atomic.Int64

	m := xtrnInvoking(t, func(_ *xtrnBuilder, active *sc.StateBuilder[string, counter]) {
		active.AssignOnExit("exitAction", func(c counter) counter {
			calls.Add(1)

			return c
		})
	})

	active, _ := m.Start(counter{N: 0}, epoch)
	_, effects := m.Transition(active, xtrnExit{}, epoch)

	require.Equal(t, int64(1), calls.Load())
	require.Len(t, effects, 1)
	require.Equal(t, sc.EffectStopInvocation, effects[0].Kind)
	require.Equal(t, "child", effects[0].Name)
}

// XState: transition function > invoke stop effects > orders after
// cancellation before the invoke stop
func TestXTrnAfterCancellationOrderedBeforeInvokeStop(t *testing.T) {
	t.Parallel()

	m := xtrnInvoking(t, func(_ *xtrnBuilder, active *sc.StateBuilder[string, counter]) {
		active.After(time.Second).To("inactive")
	})

	active, _ := m.Start(counter{N: 0}, epoch)
	_, effects := m.Transition(active, xtrnExit{}, epoch)

	require.Equal(t, []sc.EffectKind{sc.EffectCancelTimer, sc.EffectStopInvocation}, kinds(effects))
	require.Equal(t, "active", effects[0].State)
	require.Equal(t, active.Timers()[0].ID, effects[0].ID)
	require.Equal(t, time.Second, active.Timers()[0].Delay)
	require.Equal(t, "child", effects[1].Name)
}

// XState: transition function > invoke stop effects > stops invokes in
// reverse state document order on parallel exit
func TestXTrnStopsInvokesInReverseDocumentOrderOnParallelExit(t *testing.T) {
	t.Parallel()

	m := xtrnNew(t, "m", func(b *xtrnBuilder) {
		b.Initial("active")
		b.Parallel("active", func(b *xtrnBuilder) {
			b.State("left").Stream("left", xtrnListen).OnError().Stay()
			b.State("right").Stream("right", xtrnListen).OnError().Stay()
		}).On[xtrnExit]().To("inactive")
		b.State("inactive")
	})

	active, _ := m.Start(counter{N: 0}, epoch)
	_, effects := m.Transition(active, xtrnExit{}, epoch)

	require.Equal(t, []string{"right", "left"}, xtrnStops(effects))
}

// XState: transition function > invoke stop effects > stops multiple
// invokes in their declaration order
func TestXTrnStopsMultipleInvokesInDeclarationOrder(t *testing.T) {
	t.Parallel()

	m := xtrnNew(t, "m", func(b *xtrnBuilder) {
		b.Initial("active")
		active := b.State("active")
		active.Stream("first", xtrnListen).OnError().Stay()
		active.Stream("second", xtrnListen).OnError().Stay()
		active.On[xtrnExit]().To("inactive")
		b.State("inactive")
	})

	active, _ := m.Start(counter{N: 0}, epoch)
	_, effects := m.Transition(active, xtrnExit{}, epoch)

	require.Equal(t, []string{"first", "second"}, xtrnStops(effects))
}

// XState: transition function > invoke stop effects > stops nested invokes
// from child state to parent state
func TestXTrnStopsNestedInvokesFromChildToParent(t *testing.T) {
	t.Parallel()

	m := xtrnNew(t, "m", func(b *xtrnBuilder) {
		b.Initial("active")
		active := b.State("active", func(b *xtrnBuilder) {
			b.Initial("child")
			b.State("child").Stream("child", xtrnListen).OnError().Stay()
		})
		active.Stream("parent", xtrnListen).OnError().Stay()
		active.On[xtrnExit]().To("inactive")
		b.State("inactive")
	})

	active, _ := m.Start(counter{N: 0}, epoch)
	_, effects := m.Transition(active, xtrnExit{}, epoch)

	require.Equal(t, []string{"child", "parent"}, xtrnStops(effects))
}

// XState: transition function > invoke stop effects > stops remaining
// children when the machine reaches a final state
func TestXTrnStopsRemainingChildrenOnFinalState(t *testing.T) {
	t.Parallel()

	m := xtrnNew(t, "m", func(b *xtrnBuilder) {
		b.Chart().Stream("rootChild", xtrnListen).OnError().Stay()
		b.Initial("active")
		b.State("active").On[xtrnFinish]().To("done")
		b.Final("done")
	})

	active, _ := m.Start(counter{N: 0}, epoch)
	done, effects := m.Transition(active, xtrnFinish{}, epoch)

	require.True(t, done.Done())
	require.Empty(t, done.Services())
	require.Equal(t, []string{"rootChild"}, xtrnStops(effects))
}

// XState: transition function > invoke stop effects > stops spawned
// children after root exit actions on machine completion
//
// XState runs the actor and records the order the exit action and the
// child's cleanup ran in; the order of the returned effects decides it.
// The child is spawned by a transition, not by the root's entry action.
func TestXTrnStopsSpawnedChildrenAfterRootExitActions(t *testing.T) {
	t.Parallel()

	m := xtrnNew(t, "m", func(b *xtrnBuilder) {
		chart := b.Chart().Exit("exit", noop)
		chart.Invoke("child", xtrnPending).Spawned().OnError().Stay()
		chart.On[xtrnSpawn]().Spawn("spawn", "child", xtrnKey[xtrnSpawn])
		b.Initial("active")
		b.State("active").On[xtrnFinish]().To("done")
		b.Final("done")
	})

	snap, _ := m.Start(counter{N: 0}, epoch)
	snap, _ = m.Transition(snap, xtrnSpawn{}, epoch)
	require.Len(t, snap.Services(), 1)

	done, effects := m.Transition(snap, xtrnFinish{}, epoch)

	require.True(t, done.Done())
	require.Equal(t, []sc.EffectKind{sc.EffectAction, sc.EffectStopInvocation}, kinds(effects))
	require.Equal(t, []string{"m/exit", "m/child"}, names(effects))
}

// XState: transition function > invoke stop effects > reports every removed
// child id with a stop effect
func TestXTrnReportsEveryRemovedChildWithStopEffect(t *testing.T) {
	t.Parallel()

	m := xtrnNew(t, "m", func(b *xtrnBuilder) {
		b.Initial("first")
		first := b.State("first")
		first.Stream("firstChild", xtrnListen).OnError().Stay()
		first.On[xtrnNext]().To("second")
		second := b.State("second")
		second.Stream("secondChild", xtrnListen).OnError().Stay()
		second.On[xtrnFinish]().To("done")
		b.Final("done")
	})

	first, _ := m.Start(counter{N: 0}, epoch)
	second, nextEffects := m.Transition(first, xtrnNext{}, epoch)
	done, finishEffects := m.Transition(second, xtrnFinish{}, epoch)

	steps := []struct {
		previous, next sc.Snapshot[string, counter]
		effects        []sc.Effect[counter]
	}{
		{previous: first, next: second, effects: nextEffects},
		{previous: second, next: done, effects: finishEffects},
	}
	for _, step := range steps {
		removed := []string{}
		for _, id := range xtrnServiceIDs(step.previous) {
			if !slices.Contains(xtrnServiceIDs(step.next), id) {
				removed = append(removed, id)
			}
		}

		require.Len(t, removed, 1)
		require.Equal(t, removed, xtrnIDs(step.effects, sc.EffectStopInvocation))
	}
}

// XState: transition function > invoke stop effects > exposes stops in
// getMicrosteps and getInitialMicrosteps
//
// Ported: the stop effects of the transitions themselves, among them the
// start of a chart whose initial state invokes and is left at once.
// Dropped: the microsteps, which sc does not expose.
func TestXTrnStopsOfTransientInitialState(t *testing.T) {
	t.Parallel()

	m := xtrnInvoking(t, nil)
	active, _ := m.Start(counter{N: 0}, epoch)
	_, effects := m.Transition(active, xtrnExit{}, epoch)
	require.Equal(t, []string{"child"}, xtrnStops(effects))

	initial := xtrnNew(t, "m", func(b *xtrnBuilder) {
		b.Initial("transient")
		transient := b.State("transient")
		transient.Stream("initialChild", xtrnListen).OnError().Stay()
		transient.Always().To("settled")
		b.State("settled")
	})

	settled, initialEffects := initial.Start(counter{N: 0}, epoch)

	require.Equal(t, []string{"settled"}, settled.Active())
	require.Empty(t, settled.Services())

	// DIVERGES from XState: XState returns the start and the stop of
	// initialChild. sc returns neither: the core drops a run that is
	// started and stopped within one call, so the service never runs.
	require.Empty(t, initialEffects)
}

// XState: transition function > invoke stop effects > createActor executes
// an invoke stop exactly once after exit actions
func TestXTrnActorExecutesInvokeStopOnceAfterExitActions(t *testing.T) {
	t.Parallel()

	synctest.Test(t, func(t *testing.T) {
		order := new(xtrnLog)

		m := xtrnNew(t, "m", func(b *xtrnBuilder) {
			b.Initial("active")
			active := b.State("active").Exit("exit", func(context.Context, counter) { order.add("exit") })
			active.Stream("child", func(ctx context.Context, _ counter) iter.Seq2[any, error] {
				return func(func(any, error) bool) {
					<-ctx.Done()
					order.add("stop")
				}
			}).OnError().Stay()
			active.On[xtrnExit]().To("inactive")
			b.State("inactive")
		})

		inst, err := m.Instance(t.Context(), counter{N: 0})
		require.NoError(t, err)
		defer inst.Close()

		synctest.Wait()

		_, err = inst.Send(t.Context(), xtrnExit{})
		require.NoError(t, err)

		synctest.Wait()

		require.Equal(t, []string{"exit", "stop"}, order.list())
	})
}

// xtrnSpawner is the chart of the cases whose root spawns a child from its
// entry action: here SPAWN spawns it, under the key "child". The child is a
// promise that does not settle on its own.
func xtrnSpawner(t *testing.T, more func(b *xtrnBuilder, child *sc.Invocation[string, counter, int])) *sc.Machine[string, counter] {
	t.Helper()

	return xtrnNew(t, "m", func(b *xtrnBuilder) {
		child := b.Chart().Invoke("child", xtrnPending).Spawned()
		b.On[xtrnSpawn]().Spawn("spawn", "child", xtrnKey[xtrnSpawn])
		if more != nil {
			more(b, child)
		} else {
			child.OnError().Stay()
		}
	})
}

// xtrnSpawned starts the chart and spawns its child.
func xtrnSpawned(t *testing.T, m *sc.Machine[string, counter]) sc.Snapshot[string, counter] {
	t.Helper()

	snap, _ := m.Start(counter{N: 0}, epoch)
	snap, effects := m.Transition(snap, xtrnSpawn{}, epoch)
	require.Equal(t, []sc.EffectKind{sc.EffectStartInvocation}, kinds(effects))
	require.Len(t, snap.Services(), 1)

	return snap
}

// XState: transition function > invoke stop effects > explicit enq.stop
// removes and stops a child exactly once
//
// Dropped: carrying the effects out and counting the child's cleanups.
func TestXTrnExplicitStopRemovesAndStopsChildOnce(t *testing.T) {
	t.Parallel()

	m := xtrnSpawner(t, func(b *xtrnBuilder, child *sc.Invocation[string, counter, int]) {
		child.OnError().Stay()
		b.On[xtrnStop]().StopSpawned("stop", "child", xtrnKey[xtrnStop])
	})

	active := xtrnSpawned(t, m)
	child := active.Services()[0]

	stopped, effects := m.Transition(active, xtrnStop{}, epoch)

	require.Empty(t, stopped.Services())
	require.Equal(t, []sc.EffectKind{sc.EffectStopInvocation}, kinds(effects))
	require.Equal(t, child.ID, effects[0].ID)
	require.Equal(t, "child", effects[0].Name)
}

// XState: transition function > invoke stop effects > supports naive
// sequential execution of invoke lifecycle effects
//
// Dropped: carrying the effects out. The order they come back in is what
// lets a driver do so one after another.
func TestXTrnSequentialExecutionOfInvokeLifecycleEffects(t *testing.T) {
	t.Parallel()

	m := xtrnNew(t, "m", func(b *xtrnBuilder) {
		b.Initial("active")
		active := b.State("active")
		active.Stream("child", xtrnListen).OnError().Stay()
		active.On[xtrnRestart]().To("active").Reenter()
	})

	active, initial := m.Start(counter{N: 0}, epoch)
	require.Equal(t, []sc.EffectKind{sc.EffectStartInvocation}, kinds(initial))

	child := active.Services()[0]

	reentered, restart := m.Transition(active, xtrnRestart{}, epoch)

	require.Equal(t, []sc.EffectKind{sc.EffectStopInvocation, sc.EffectStartInvocation}, kinds(restart))
	require.Equal(t, child.ID, restart[0].ID)
	require.NotEqual(t, child.ID, restart[1].ID)
	require.Equal(t, []string{restart[1].ID}, xtrnServiceIDs(reentered))
}

// XState: transition function > invoke stop effects > executes immediate
// sends in a sequential effect loop
func TestXTrnExecutesImmediateSends(t *testing.T) {
	t.Parallel()

	synctest.Test(t, func(t *testing.T) {
		var received atomic.Int64

		m := xtrnNew(t, "m", func(b *xtrnBuilder) {
			b.Chart().Stream("child", func(ctx context.Context, _ counter) iter.Seq2[any, error] {
				return func(func(any, error) bool) {
					for ev := range sc.Inbox(ctx) {
						if _, ok := ev.(xtrnPing); ok {
							received.Add(1)
						}
					}
				}
			}).OnError().Stay()
			b.On[xtrnSend]().SendTo("ping", "child", func(counter, xtrnSend) any { return xtrnPing{} })
		})

		inst, err := m.Instance(t.Context(), counter{N: 0})
		require.NoError(t, err)
		defer inst.Close()

		step, err := inst.Send(t.Context(), xtrnSend{})
		require.NoError(t, err)
		require.Equal(t, []sc.EffectKind{sc.EffectSend}, kinds(step.Effects))
		require.Equal(t, xtrnPing{}, step.Effects[0].Event)

		synctest.Wait()

		require.Equal(t, int64(1), received.Load())
	})
}

// XState: transition function > invoke stop effects > cancels a previously
// scheduled effect in a sequential effect loop
//
// XState reads the system's scheduled timers; the snapshot lists them here.
func TestXTrnCancelsPreviouslyScheduledEffect(t *testing.T) {
	t.Parallel()

	m := xtrnNew(t, "m", func(b *xtrnBuilder) {
		b.Initial("waiting")
		waiting := b.State("waiting")
		waiting.After(10 * time.Second).To("done")
		waiting.On[xtrnCancel]().To("done")
		b.State("done")
	})

	waiting, initial := m.Start(counter{N: 0}, epoch)

	require.Equal(t, []sc.EffectKind{sc.EffectStartTimer}, kinds(initial))
	require.Len(t, waiting.Timers(), 1)

	done, effects := m.Transition(waiting, xtrnCancel{}, epoch)

	require.Equal(t, []sc.EffectKind{sc.EffectCancelTimer}, kinds(effects))
	require.Equal(t, initial[0].ID, effects[0].ID)
	require.Empty(t, done.Timers())
}

// xtrnAB is the chart with states a and b, where NEXT moves from a to b.
func xtrnAB(t *testing.T) *sc.Machine[string, counter] {
	t.Helper()

	return xtrnNew(t, "m", func(b *xtrnBuilder) {
		b.Initial("a")
		b.State("a").On[xtrnNext]().To("b")
		b.State("b")
	})
}

// XState: transition function > invoke stop effects > machine transition
// methods do not require an actor scope
func TestXTrnMachineTransitionMethodsNeedNoActorScope(t *testing.T) {
	t.Parallel()

	m := xtrnAB(t)
	a, _ := m.Start(counter{N: 0}, epoch)
	b, _ := m.Transition(a, xtrnNext{}, epoch)

	require.Equal(t, []string{"b"}, b.Active())
}

// XState: transition function > invoke stop effects > keeps the inert self
// snapshot in sync for executable effects
//
// XState's action reads self.getSnapshot().value when it is carried out. An
// sc action has no self; its effect names the states active after the step.
func TestXTrnEffectsSeeTheStatesAfterTheStep(t *testing.T) {
	t.Parallel()

	m := xtrnNew(t, "m", func(b *xtrnBuilder) {
		b.Initial("a")
		b.State("a").On[xtrnNext]().Do("read", xtrnDo[xtrnNext]).To("b")
		b.State("b")
	})

	a, _ := m.Start(counter{N: 0}, epoch)
	_, effects := m.Transition(a, xtrnNext{}, epoch)

	require.Equal(t, []string{"a/read"}, names(effects))
	require.Equal(t, []string{"a"}, effects[0].From)
	require.Equal(t, []string{"b"}, effects[0].To)
}

// XState: transition function > invoke stop effects > keeps executable
// effect self snapshots isolated between branches
func TestXTrnEffectsIsolatedBetweenBranches(t *testing.T) {
	t.Parallel()

	m := xtrnNew(t, "m", func(b *xtrnBuilder) {
		b.Initial("a")
		a := b.State("a")
		a.On[xtrnLeft]().Do("read", xtrnDo[xtrnLeft]).To("left")
		a.On[xtrnRight]().To("right")
		b.State("left")
		b.State("right")
	})

	a, _ := m.Start(counter{N: 0}, epoch)
	_, leftEffects := m.Transition(a, xtrnLeft{}, epoch)
	right, _ := m.Transition(a, xtrnRight{}, epoch)

	require.Equal(t, []string{"right"}, right.Active())
	require.Equal(t, []string{"left"}, leftEffects[0].To)
	require.Equal(t, []string{"a"}, a.Active())
}

// XState: transition function > invoke stop effects > keeps pure system
// registries isolated between branches
//
// XState looks the spawned child up in the system registry from another
// branch; here the snapshot the branches start from must not list it.
func TestXTrnRunningServicesIsolatedBetweenBranches(t *testing.T) {
	t.Parallel()

	m := xtrnSpawner(t, func(b *xtrnBuilder, child *sc.Invocation[string, counter, int]) {
		child.OnError().Stay()
		b.On[xtrnCheck]().Do("check", xtrnDo[xtrnCheck])
	})

	initial, _ := m.Start(counter{N: 0}, epoch)

	unknown, _ := m.Transition(initial, xtrnUnknown{}, epoch)
	spawned, _ := m.Transition(initial, xtrnSpawn{}, epoch)
	checked, _ := m.Transition(initial, xtrnCheck{}, epoch)

	require.Len(t, spawned.Services(), 1)
	require.Empty(t, unknown.Services())
	require.Empty(t, checked.Services())
	require.Empty(t, initial.Services())
}

// XState: transition function > invoke stop effects > does not expose
// future runtime registry entries to old snapshots
func TestXTrnOldSnapshotsDoNotSeeLaterChildren(t *testing.T) {
	t.Parallel()

	synctest.Test(t, func(t *testing.T) {
		m := xtrnSpawner(t, func(b *xtrnBuilder, child *sc.Invocation[string, counter, int]) {
			child.OnError().Stay()
			b.On[xtrnCheck]().Do("check", xtrnDo[xtrnCheck])
		})

		inst, err := m.Instance(t.Context(), counter{N: 0})
		require.NoError(t, err)
		defer inst.Close()

		old := inst.Snapshot()

		_, err = inst.Send(t.Context(), xtrnSpawn{})
		require.NoError(t, err)

		checked, _ := m.Transition(old, xtrnCheck{}, epoch)

		require.Len(t, inst.Snapshot().Services(), 1)
		require.Empty(t, old.Services())
		require.Empty(t, checked.Services())
	})
}

// XState: transition function > invoke stop effects > removes stopped
// children from later pure system views
func TestXTrnStoppedChildrenRemovedFromLaterSnapshots(t *testing.T) {
	t.Parallel()

	m := xtrnInvoking(t, func(b *xtrnBuilder, _ *sc.StateBuilder[string, counter]) {
		b.State("inactive").On[xtrnCheck]().Do("check", xtrnDo[xtrnCheck])
	})

	active, _ := m.Start(counter{N: 0}, epoch)
	require.Len(t, active.Services(), 1)

	inactive, _ := m.Transition(active, xtrnExit{}, epoch)
	checked, _ := m.Transition(inactive, xtrnCheck{}, epoch)

	require.Empty(t, inactive.Services())
	require.Empty(t, checked.Services())
}

// XState: transition function > invoke stop effects > uses a new actor
// session ID across pure stop and reentry
func TestXTrnNewRunIDAcrossStopAndReentry(t *testing.T) {
	t.Parallel()

	m := xtrnInvoking(t, func(b *xtrnBuilder, _ *sc.StateBuilder[string, counter]) {
		b.State("inactive").On[xtrnEnter]().To("active")
	})

	active, _ := m.Start(counter{N: 0}, epoch)
	first := active.Services()[0].ID
	inactive, _ := m.Transition(active, xtrnExit{}, epoch)
	reentered, _ := m.Transition(inactive, xtrnEnter{}, epoch)

	require.NotEqual(t, first, reentered.Services()[0].ID)
}

// XState: transition function > invoke stop effects > assigns distinct
// opaque session IDs across pure initial transitions
func TestXTrnRunIDsAcrossInitialTransitions(t *testing.T) {
	t.Parallel()

	m := xtrnNew(t, "m", func(b *xtrnBuilder) {
		b.Chart().Stream("child", xtrnListen).OnError().Stay()
	})

	first, _ := m.Start(counter{N: 0}, epoch)
	second, _ := m.Start(counter{N: 0}, epoch)

	// DIVERGES from XState: XState gives the children of two initial
	// transitions distinct session ids. sc's core is a pure function, so
	// starting the same chart twice gives the same run ID.
	require.Equal(t, first.Services()[0].ID, second.Services()[0].ID)
}

// XState: transition function > invoke stop effects > assigns distinct
// session IDs to branches from the same snapshot
func TestXTrnRunIDsOfBranchesFromSameSnapshot(t *testing.T) {
	t.Parallel()

	m := xtrnNew(t, "m", func(b *xtrnBuilder) {
		b.Initial("idle")
		b.State("idle").On[xtrnEnter]().To("active")
		b.State("active").Stream("child", xtrnListen).OnError().Stay()
	})

	idle, _ := m.Start(counter{N: 0}, epoch)

	first, _ := m.Transition(idle, xtrnEnter{}, epoch)
	second, _ := m.Transition(idle, xtrnEnter{}, epoch)

	// DIVERGES from XState: XState gives the child of each branch its own
	// session id. sc's core is a pure function, so the same event on the
	// same snapshot gives the same run ID.
	require.Equal(t, first.Services()[0].ID, second.Services()[0].ID)
}

// XState: transition function > invoke stop effects > does not reuse a
// running actor scope for a pure transition
func TestXTrnPureTransitionLeavesRunningInstanceAlone(t *testing.T) {
	t.Parallel()

	synctest.Test(t, func(t *testing.T) {
		m := xtrnAB(t)

		inst, err := m.Instance(t.Context(), counter{N: 0})
		require.NoError(t, err)
		defer inst.Close()

		next, _ := m.Transition(inst.Snapshot(), xtrnNext{}, epoch)

		require.Equal(t, []string{"b"}, next.Active())
		require.Equal(t, []string{"a"}, inst.Snapshot().Active())
	})
}

// XState: transition function > terminal child cleanup > removes a
// dynamically spawned child after its matching done event
func TestXTrnRemovesSpawnedChildAfterItsDoneEvent(t *testing.T) {
	t.Parallel()

	m := xtrnSpawner(t, nil)
	active := xtrnSpawned(t, m)

	completed, _ := m.Transition(active, sc.Done[int]{ID: active.Services()[0].ID, Result: 42}, epoch)

	require.Empty(t, completed.Services())
}

// XState: transition function > terminal child cleanup > removes a
// dynamically spawned child after its matching error event
func TestXTrnRemovesSpawnedChildAfterItsErrorEvent(t *testing.T) {
	t.Parallel()

	m := xtrnSpawner(t, nil)
	active := xtrnSpawned(t, m)

	failed, _ := m.Transition(active, sc.Failed{ID: active.Services()[0].ID, Err: errors.New("failed")}, epoch)

	require.Empty(t, failed.Services())
	require.False(t, failed.Done())
}

// XState: transition function > terminal child cleanup > keeps the child
// visible while handling its terminal event
//
// Ported: the transition taken for the child's done event sees the event.
// Dropped: reading the child's ref from that transition.
func TestXTrnTerminalEventIsSeenWhileHandlingIt(t *testing.T) {
	t.Parallel()

	m := xtrnSpawner(t, func(_ *xtrnBuilder, child *sc.Invocation[string, counter, int]) {
		child.OnDone().Do("observe", xtrnDo[sc.Done[int]])
		child.OnError().Stay()
	})
	active := xtrnSpawned(t, m)
	run := active.Services()[0]

	completed, effects := m.Transition(active, sc.Done[int]{ID: run.ID, Result: 42}, epoch)

	require.Equal(t, []string{"m/observe"}, names(effects))
	require.Equal(t, sc.Done[int]{ID: run.ID, Result: 42}, effects[0].Event)
	require.Empty(t, completed.Services())
}

// XState: transition function > terminal child cleanup > does not remove a
// replacement child for a stale terminal event
func TestXTrnStaleTerminalEventDoesNotRemoveReplacementChild(t *testing.T) {
	t.Parallel()

	m := xtrnSpawner(t, func(b *xtrnBuilder, child *sc.Invocation[string, counter, int]) {
		child.OnError().Stay()
		b.On[xtrnReplace]().
			StopSpawned("stop", "child", xtrnKey[xtrnReplace]).
			Spawn("spawn", "child", xtrnKey[xtrnReplace])
	})

	first := xtrnSpawned(t, m)
	firstChild := first.Services()[0]

	second, effects := m.Transition(first, xtrnReplace{}, epoch)
	require.Equal(t, []sc.EffectKind{sc.EffectStopInvocation, sc.EffectStartInvocation}, kinds(effects))
	require.Len(t, second.Services(), 1)

	secondChild := second.Services()[0]
	require.NotEqual(t, firstChild.ID, secondChild.ID)

	afterStaleDone, effects := m.Transition(second, sc.Done[int]{ID: firstChild.ID, Result: 0}, epoch)

	require.Empty(t, effects)
	require.Equal(t, second.Seq(), afterStaleDone.Seq())
	require.Equal(t, []sc.RunningService[string]{secondChild}, afterStaleDone.Services())
}

// xtrnResettable is the chart of the two stale-child cases: "active"
// invokes a promise that never settles, goes to "done" when it does, and
// re-enters itself on RESET.
func xtrnResettable(t *testing.T) *sc.Machine[string, counter] {
	t.Helper()

	return xtrnNew(t, "m", func(b *xtrnBuilder) {
		b.Initial("active")
		active := b.State("active")
		inv := active.Invoke("child", xtrnPending)
		inv.OnDone().To("done")
		inv.OnError().Stay()
		active.On[xtrnReset]().To("active").Reenter()
		b.State("done")
	})
}

// XState: transition function > terminal child cleanup > does not take an
// invoke completion transition for a stale child
func TestXTrnNoCompletionTransitionForStaleChild(t *testing.T) {
	t.Parallel()

	m := xtrnResettable(t)

	first, _ := m.Start(counter{N: 0}, epoch)
	firstChild := first.Services()[0]
	second, _ := m.Transition(first, xtrnReset{}, epoch)
	secondChild := second.Services()[0]

	afterStaleDone, _ := m.Transition(second, sc.Done[int]{ID: firstChild.ID, Result: 0}, epoch)

	require.Equal(t, []string{"active"}, afterStaleDone.Active())
	require.Equal(t, []sc.RunningService[string]{secondChild}, afterStaleDone.Services())

	// The replacement's own completion is taken.
	done, _ := m.Transition(second, sc.Done[int]{ID: secondChild.ID, Result: 0}, epoch)
	require.Equal(t, []string{"done"}, done.Active())
}

// XState: transition function > terminal child cleanup > ignores an
// unhandled error from a stale child
//
// XState's chart has no onError; an sc invocation must declare one, and
// here it leaves for "done" so that taking it for the stale child would
// show.
func TestXTrnIgnoresErrorFromStaleChild(t *testing.T) {
	t.Parallel()

	m := xtrnNew(t, "m", func(b *xtrnBuilder) {
		b.Initial("active")
		active := b.State("active")
		active.Invoke("child", xtrnPending).OnError().To("done")
		active.On[xtrnReset]().To("active").Reenter()
		b.State("done")
	})

	first, _ := m.Start(counter{N: 0}, epoch)
	second, _ := m.Transition(first, xtrnReset{}, epoch)

	afterStaleError, effects := m.Transition(second, sc.Failed{ID: first.Services()[0].ID, Err: errors.New("stale")}, epoch)

	require.Empty(t, effects)
	require.False(t, afterStaleError.Done())
	require.Equal(t, []string{"active"}, afterStaleError.Active())
	require.Equal(t, second.Services(), afterStaleError.Services())
}

// XState: transition function > terminal child cleanup > removes a
// completed dynamically spawned child on the live path
//
// The child is spawned by a transition, not by the root's entry action.
func TestXTrnRemovesCompletedSpawnedChildOnLivePath(t *testing.T) {
	t.Parallel()

	synctest.Test(t, func(t *testing.T) {
		m := xtrnNew(t, "m", func(b *xtrnBuilder) {
			b.Chart().Invoke("completingChild", func(context.Context, counter) (int, error) { return 42, nil }).
				Spawned().OnError().Stay()
			b.On[xtrnSpawn]().Spawn("spawn", "completingChild", xtrnKey[xtrnSpawn])
		})

		inst, err := m.Instance(t.Context(), counter{N: 0})
		require.NoError(t, err)
		defer inst.Close()

		step, err := inst.Send(t.Context(), xtrnSpawn{})
		require.NoError(t, err)
		require.Equal(t, []sc.EffectKind{sc.EffectStartInvocation}, kinds(step.Effects))

		synctest.Wait()

		require.Empty(t, inst.Snapshot().Services())
	})
}

// XState: transition function > suffixed internal events > does not match
// delayed transitions
//
// XState sends an event whose type is the internal 'xstate.after' with a
// suffix. An sc timer fires on a TimerFired event that carries the timer's
// ID, so the case is one whose ID is the timer's with a suffix.
func TestXTrnSuffixedTimerEventDoesNotMatchDelayedTransition(t *testing.T) {
	t.Parallel()

	m := xtrnNew(t, "m", func(b *xtrnBuilder) {
		b.Initial("waiting")
		b.State("waiting").After(10 * time.Millisecond).To("done")
		b.State("done")
	})

	waiting, _ := m.Start(counter{N: 0}, epoch)
	timer := waiting.Timers()[0]

	next, effects := m.Transition(waiting, sc.TimerFired{ID: timer.ID + ":10"}, epoch)

	require.Empty(t, effects)
	require.Equal(t, []string{"waiting"}, next.Active())
	require.Equal(t, waiting.Seq(), next.Seq())

	fired, _ := m.Transition(waiting, sc.TimerFired{ID: timer.ID}, epoch.Add(10*time.Millisecond))
	require.Equal(t, []string{"done"}, fired.Active())
}

// XState: transition function > initialTransition: invoke start is deferred
// to the end, after entry actions
//
// Ported: the root's invoke starts after the root's entry actions. Dropped:
// the listener the entry action attaches to the child.
func TestXTrnInvokeStartComesAfterEntryActions(t *testing.T) {
	t.Parallel()

	m := xtrnNew(t, "m", func(b *xtrnBuilder) {
		chart := b.Chart()
		chart.Stream("child", xtrnListen).OnError().Stay()
		chart.Entry("listen", noop)
	})

	_, effects := m.Start(counter{N: 0}, epoch)

	require.Equal(t, []string{"m/listen", "m/child"}, names(effects))
	require.Equal(t, []sc.EffectKind{sc.EffectAction, sc.EffectStartInvocation}, kinds(effects))
}

// XState: transition function > emit actions should be returned
func TestXTrnEmitActionsAreReturned(t *testing.T) {
	t.Parallel()

	m := xtrnNew(t, "m", func(b *xtrnBuilder) {
		b.Initial("a")
		b.State("a").On[xtrnNext]().Emit("counted", func(c counter, _ xtrnNext) xtrnCounted { return xtrnCounted{Count: c.N} })
	})

	state, _ := m.Start(counter{N: 10}, epoch)

	require.Equal(t, []string{"a"}, state.Active())

	_, nextActions := m.Transition(state, xtrnNext{}, epoch)

	require.Equal(t, []sc.EffectKind{sc.EffectEmit}, kinds(nextActions))
	require.Equal(t, "counted", nextActions[0].Name)
	require.Equal(t, xtrnCounted{Count: 10}, nextActions[0].Event)
}

// XState: transition function > should calculate the next snapshot for
// machine logic
func TestXTrnCalculatesNextSnapshotForMachineLogic(t *testing.T) {
	t.Parallel()

	m := xtrnNew(t, "m", func(b *xtrnBuilder) {
		b.Initial("a")
		b.State("a").On[xtrnNext]().To("b")
		b.State("b").On[xtrnNext]().To("c")
		b.State("c")
	})

	init, _ := m.Start(counter{N: 0}, epoch)
	s1, _ := m.Transition(init, xtrnNext{}, epoch)

	require.Equal(t, []string{"b"}, s1.Active())

	s2, _ := m.Transition(s1, xtrnNext{}, epoch)

	require.Equal(t, []string{"c"}, s2.Active())
}

// XState: transition function > should not execute entry actions
func TestXTrnDoesNotExecuteEntryActions(t *testing.T) {
	t.Parallel()

	var calls atomic.Int64

	m := xtrnNew(t, "m", func(b *xtrnBuilder) {
		b.Chart().Entry("fn", func(context.Context, counter) { calls.Add(1) })
		b.Initial("a")
		b.State("a")
		b.State("b")
	}, sc.AllowUnreachable())

	_, effects := m.Start(counter{N: 0}, epoch)

	require.Equal(t, []string{"m/fn"}, names(effects))
	require.Zero(t, calls.Load())
}

// XState: transition function > should not execute transition actions
func TestXTrnDoesNotExecuteTransitionActions(t *testing.T) {
	t.Parallel()

	var calls atomic.Int64

	m := xtrnNew(t, "m", func(b *xtrnBuilder) {
		b.Initial("a")
		b.State("a").On[xtrnEvent]().Do("fn", func(context.Context, counter, xtrnEvent) { calls.Add(1) }).To("b")
		b.State("b")
	})

	init, _ := m.Start(counter{N: 0}, epoch)
	next, effects := m.Transition(init, xtrnEvent{}, epoch)

	require.Equal(t, []string{"a/fn"}, names(effects))
	require.Zero(t, calls.Load())
	require.Equal(t, []string{"b"}, next.Active())
}

// XState: transition function > delayed events example (experimental)
//
// The example keeps the snapshot as JSON between requests and carries the
// delayed raise out by sleeping. Here the snapshot is stored and restored
// the same way, and the timer's event is delivered at the time it is due.
func TestXTrnDelayedEventsExample(t *testing.T) {
	t.Parallel()

	m := xtrnNew(t, "m", func(b *xtrnBuilder) {
		b.Initial("start")
		b.State("start").On[xtrnNext]().To("waiting")
		b.State("waiting").After(10 * time.Millisecond).To("done")
		b.Final("done")
	})

	now := epoch

	var db []byte

	store := func(snap sc.Snapshot[string, counter]) {
		data, err := json.Marshal(snap)
		require.NoError(t, err)

		db = data
	}

	// postEvent handles one request, and reports the timers it must set.
	postEvent := func(ev any) []sc.Effect[counter] {
		snap, _, err := m.Restore(db, now)
		require.NoError(t, err)

		next, effects := m.Transition(snap, ev, now)
		store(next)

		return xtrnOf(effects, sc.EffectStartTimer)
	}

	state, actions := m.Start(counter{N: 0}, now)
	store(state)
	require.Empty(t, actions)

	timers := postEvent(xtrnNext{})
	require.Len(t, timers, 1)
	require.Equal(t, 10*time.Millisecond, timers[0].Delay)

	now = timers[0].At
	require.Empty(t, postEvent(sc.TimerFired{ID: timers[0].ID}))

	done, _, err := m.Restore(db, now)
	require.NoError(t, err)
	require.True(t, done.Done())
}

// XState: transition function > serverless workflow example (experimental)
//
// The example keeps the snapshot as JSON between requests, runs each
// started actor and posts its done event. Machine.Exec runs a started
// service and hands back the event to post.
func TestXTrnServerlessWorkflowExample(t *testing.T) {
	t.Parallel()

	calls := new(xtrnLog)

	m := xtrnNew(t, "m", func(b *xtrnBuilder) {
		b.Initial("sendingWelcomeEmail")
		email := b.State("sendingWelcomeEmail").Invoke("sendWelcomeEmail", func(context.Context, counter) (xtrnSent, error) {
			calls.add("sendWelcomeEmail")

			return xtrnSent{Status: "sent"}, nil
		})
		email.OnDone().To("logSent")
		email.OnError().Stay()
		log := b.State("logSent").Invoke("log", func(context.Context, counter) (struct{}, error) { return struct{}{}, nil })
		log.OnDone().To("finish")
		log.OnError().Stay()
		b.State("finish")
	})

	var db []byte

	store := func(snap sc.Snapshot[string, counter]) {
		data, err := json.Marshal(snap)
		require.NoError(t, err)

		db = data
	}

	var postEvent func(ev any)

	// execute runs each started service and posts what it reports.
	execute := func(effects []sc.Effect[counter]) {
		for _, e := range xtrnOf(effects, sc.EffectStartInvocation) {
			var events []any
			require.NoError(t, m.Exec(t.Context(), e, func(ev any) { events = append(events, ev) }))

			for _, ev := range events {
				postEvent(ev)
			}
		}
	}

	postEvent = func(ev any) {
		snap, _, err := m.Restore(db, epoch)
		require.NoError(t, err)

		next, effects := m.Transition(snap, ev, epoch)
		store(next)
		execute(effects)
	}

	state, actions := m.Start(counter{N: 0}, epoch)
	store(state)
	require.Equal(t, []sc.EffectKind{sc.EffectStartInvocation}, kinds(actions))
	require.Empty(t, calls.list())

	execute(actions)

	// The example posts an event nothing handles, which changes nothing.
	postEvent(xtrnSentEv{})

	require.Equal(t, []string{"sendWelcomeEmail"}, calls.list())

	finished, _, err := m.Restore(db, epoch)
	require.NoError(t, err)
	require.Equal(t, []string{"finish"}, finished.Active())
}

// XState: transition function > should support transition functions
func TestXTrnSupportsTransitionFunctions(t *testing.T) {
	t.Parallel()

	m := xtrnNew(t, "m", func(b *xtrnBuilder) {
		b.Initial("a")
		b.State("a").On[xtrnNext]().Do("fn", xtrnDo[xtrnNext]).To("b")
		b.State("b")
	})

	init, _ := m.Start(counter{N: 0}, epoch)
	s1, actions := m.Transition(init, xtrnNext{}, epoch)

	require.Equal(t, []string{"b"}, s1.Active())
	require.Len(t, actions, 1)
}

// XState: transition function > fast-paths flat static target/context
// transitions
//
// Dropped: the spy that checks XState's internal fast path was taken.
func TestXTrnFlatStaticTargetAndContextTransition(t *testing.T) {
	t.Parallel()

	m := xtrnNew(t, "m", func(b *xtrnBuilder) {
		b.Initial("a")
		b.State("a").On[xtrnNext]().Assign("context", func(counter, xtrnNext) counter { return counter{N: 1} }).To("b")
		b.State("b")
	})

	init, _ := m.Start(counter{N: 0}, epoch)
	next, actions := m.Transition(init, xtrnNext{}, epoch)

	require.Equal(t, []string{"b"}, next.Active())
	require.Equal(t, counter{N: 1}, next.Context)
	require.Empty(t, actions)
}

// XState: transition function > fast-paths flat static targetless context
// transitions
//
// Dropped: the spy that checks XState's internal fast path was taken.
func TestXTrnFlatStaticTargetlessContextTransition(t *testing.T) {
	t.Parallel()

	m := xtrnNew(t, "m", func(b *xtrnBuilder) {
		b.Initial("a")
		b.State("a").On[xtrnInc]().Assign("context", func(counter, xtrnInc) counter { return counter{N: 1} })
	})

	init, _ := m.Start(counter{N: 0}, epoch)
	next, actions := m.Transition(init, xtrnInc{}, epoch)

	require.Equal(t, []string{"a"}, next.Active())
	require.Equal(t, counter{N: 1}, next.Context)
	require.Empty(t, actions)
}

// XState: getNextTransitions > should return no transitions for a completed
// snapshot
//
// sc has no getNextTransitions; Machine.Can answers, for one event, whether
// a snapshot has a transition to take.
func TestXTrnNoTransitionsForCompletedSnapshot(t *testing.T) {
	t.Parallel()

	m := xtrnNew(t, "m", func(b *xtrnBuilder) {
		b.On[xtrnReset]().To("done")
		b.Initial("done")
		b.Final("done")
	})

	done, _ := m.Start(counter{N: 0}, epoch)

	require.True(t, done.Done())
	require.False(t, m.Can(done, xtrnReset{}, epoch))

	next, effects := m.Transition(done, xtrnReset{}, epoch)
	require.Empty(t, effects)
	require.Equal(t, done.Seq(), next.Seq())
}

// Not ported, in the order of the XState file.
//
// Actor refs, self, parent and the actor system, which sc has none of: a
// service is known by its name and run ID, and nothing in a chart holds a
// reference to a running actor.
//   - resolves a selected transition with the real parent
//   - resolves a root transition with an undefined parent
//   - keeps one child ref before and after its spawn effect executes
//   - uses the snapshot child ref as self after start
//   - uses the same system on child self during initialization
//   - uses the same system on a runtime self
//   - invoke stop effects > does not let an actor stop itself through
//     enq.stop()
//   - invoke stop effects > reuses captured live system state until topology
//     changes
//   - invoke stop effects > does not discover future nested actors through
//     old child refs
//   - invoke stop effects > projects nested system registries from the input
//     snapshot
//   - invoke stop effects > preserves parent refs when purely transitioning
//     a child snapshot
//   - invoke stop effects > replaces inherited live identity after
//     materializing a pure branch
//   - invoke stop effects > gives every planned snapshot its own current
//     owner snapshot
//   - invoke stop effects > keeps one session identity across a plan started
//     from scratch
//   - invoke stop effects > represents context initializer spawns as
//     executable effects
//
// Microsteps (getMicrosteps, getInitialMicrosteps), which sc does not
// expose: a step comes back whole.
//   - invoke stop effects > keeps one session identity across initial
//     microsteps
//   - invoke stop effects > gives every microstep its own current owner
//     snapshot
//   - invoke stop effects > appends deferred starts to the final microstep
//   - terminal child cleanup > reports the pruned child in the final
//     microstep
//
// Listener and subscription actors (enq.listen, enq.subscribeTo), which sc
// has none of: a parent hears a child through the events the child emits.
//   - initialTransition: defers listener/child starts to the end of the
//     effects
//   - initialTransition: defers subscription/child starts to the end of the
//     effects
//   - transition: cross-phase spawns with listeners from exit and entry
//     defer starts to the end of the effects
//   - transition: a same-transition spawn+stop keeps its appended start
//     (which no-ops at runtime)
//   - transition: interleaved spawns and listeners in the same phase defer
//     starts to the end (attached before child, each in authored order)
//   - transition: listening to a pre-existing actor spawns+starts only the
//     listener actor (no new target start)
//   - initialTransition: spawn+listen+subscribeTo on the same child defers
//     starts to the end
//   - initialTransition: effects can be executed by a manual executor loop
//     with listener starting before child
//
// Custom actor logic and XState internals (createDurable, createStepLogic,
// hand-written ActorLogic, isBuiltInExecutableEffect, the keys of the
// argument objects a callback receives).
//   - does not materialize an unchanged durable transition scope to wire its
//     runtime
//   - preserves callback argument surfaces while planning lazily
//   - keeps materialized pure scopes effect-free
//   - does not classify inherited object keys as built-in actions
//   - does not classify emitted reserved event names as built-in actions
//   - does not classify user-created reserved action shapes as built-in
//     actions
//   - should calculate the next snapshot for custom logic
//
// Events named by strings. sc events are Go values matched by their type,
// so there is no internal event name to put a suffix on, and no string to
// raise by mistake; a state's completion is not an event a caller can send
// at all.
//   - enq.raise with a string event throws in a transition function
//   - terminal child cleanup > ignores suffixed actor terminal events
//   - suffixed internal events > does not match state completion transitions
//
// API sc does not have.
//   - does not repeatedly resolve an initial transition input mapper: sc
//     states take no input.
//   - invoke stop effects > does not duplicate invoke auto-stop after an
//     explicit exit stop: only leaving its state stops an invoked service;
//     StopSpawned stops a spawned one, from a transition.
//   - log actions should be returned: sc has no log action; it would be an
//     ordinary action.
//
// getNextTransitions, which sc has no counterpart of: Machine.Can answers
// for one event, and Machine.Definition exports the whole chart.
//   - should return no transitions for an error snapshot (sc has no error
//     snapshot either: Restore returns the error)
//   - should return all transitions from current state
//   - should include guarded transitions regardless of guard result
//   - should include always (eventless) transitions
//   - should include after (delayed) transitions
//   - should include transitions from parent states in depth-first order
//   - should include all guarded transitions from different state nodes with
//     same event type
//   - should return transitions from parallel states in document order
//   - should return transitions from deeply nested compound states in
//     depth-first order
//   - should return transitions from parallel states with nested compound
//     states
