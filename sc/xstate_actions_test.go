package sc_test

// Ports of XState's packages/core/test/actions.test.ts (6.0.0-beta.2), one
// test per `it`, in order. The cases that are not ported are listed, by
// reason, at the end of the file.
//
// XState actions are observed through the effects the core returns, named
// "<state>/<action>". States are named after their XState paths, so XState's
// 'enter: a.a1' is the effect "a.a1/enter" and 'exit: a.a1' is "a.a1/exit".
//
// Three translations recur and are not repeated at each case:
//   - XState's root is the chart itself, reached with Chart(). Its effects
//     carry the machine's id as their state, so the root's 'enter: __root__'
//     is the effect "m/enter" of a machine named m.
//   - Where an XState machine is its root alone, the sc chart declares no
//     states, so nothing is active.
//   - XState accepts states nothing can reach; sc does only with the
//     AllowUnreachable option, which every chart here is built with.

import (
	"context"
	"sync/atomic"
	"testing"
	"testing/synctest"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/sc"
)

type (
	xactTimer        struct{}
	xactPedCountdown struct{}
	xactFake         struct{}
	xactRestart      struct{}
	xactChange       struct{}
	xactNext         struct{}
	xactNextFn       struct{}
	xactToA          struct{}
	xactToB          struct{}
	xactUpdate       struct{}
	xactEvent        struct{}
	xactTack         struct{}
	xactAbsoluteTack struct{}
	xactEV           struct{}
	xactLeave        struct{}
	xactLoad         struct{}
	xactLoaded       struct{}
	xactRetry        struct{}
	xactEnterPar     struct{}
	xactFoo          struct{}
	xactBar          struct{}
	xactWhatever     struct{}
	xactFinishChild  struct{}
	xactFinish       struct{}
	xactChildDone    struct{}
	xactInit         struct{}
	xactStop         struct{}
	xactInvalid      struct{}
	xactE            struct{}
	xactSuccess      struct{}
	xactRaised       struct{}
	xactParentEvent  struct{}
	xactSend         struct{}
	xactStart        struct{}
	xactCancel       struct{}
	xactCancelFoo    struct{}
	xactCancelBar    struct{}
	xactHello        struct{}
	xactPick         struct{}
	xactClose        struct{}
	xactExit         struct{}
	xactStopped      struct{}
)

// xactSecret, xactValue, xactPing and xactCount are the events that carry a
// number, and xactNamed the one that names an actor.
type (
	xactSecret struct{ Secret int }
	xactValue  struct{ Value int }
	xactPing   struct{ Value int }
	xactCount  struct{ Count int }
	xactNamed  struct{ Value string }
)

// xactAge is the context of the "guards and delays" chart.
type xactAge struct {
	Age   int  `json:"age"`
	Adult bool `json:"adult"`
}

// xactEventType is the context of the chart that picks the event it raises.
type xactEventType struct {
	EventType string `json:"event_type"`
}

// xactClosed is the context of the "root exit function" chart.
type xactClosed struct {
	Closed bool `json:"closed"`
}

// xactCurrency is the context of the "enqueued action results" chart.
type xactCurrency struct {
	Currency string `json:"currency"`
}

type (
	xactBuilder = sc.Builder[string, counter]
	xactState   = sc.StateBuilder[string, counter]
)

// xactNew builds a chart, accepting states that nothing reaches as XState
// does.
func xactNew[C any](t *testing.T, id string, def func(b *sc.Builder[string, C])) *sc.Machine[string, C] {
	t.Helper()

	m, err := sc.New(id, def, sc.AllowUnreachable())
	require.NoError(t, err)

	return m
}

// xactTracked gives a state the entry and exit actions XState's cases track
// as 'enter: <state>' and 'exit: <state>'.
func xactTracked(s *xactState) *xactState {
	return s.Entry("enter", noop).Exit("exit", noop)
}

// xactDo is a transition action that does nothing.
func xactDo[Ev any](context.Context, counter, Ev) {}

// xactNoMove is a chart action that does nothing.
func xactNoMove(context.Context, counter, sc.Move[string]) {}

// xactExec runs the actions among effects, as a driver would.
func xactExec[C any](t *testing.T, m *sc.Machine[string, C], effects []sc.Effect[C]) {
	t.Helper()

	for _, e := range effects {
		if e.Kind == sc.EffectAction {
			require.NoError(t, m.Exec(t.Context(), e, func(any) {}))
		}
	}
}

// xactSame and xactNoOutput are the input and output of the child machines.
func xactSame(c counter) counter { return c }

func xactNoOutput(sc.Snapshot[string, counter]) (struct{}, error) { return struct{}{}, nil }

// xactRaise raises ev on entry.
func xactRaise(ev any) func(counter) any {
	return func(counter) any { return ev }
}

// xactLights is the chart of the "transition" and "deep transition" cases.
func xactLights(t *testing.T, deep bool) *sc.Machine[string, counter] {
	t.Helper()

	return xactNew(t, "lights", func(b *xactBuilder) {
		b.Initial("green")
		xactTracked(b.State("green")).On[xactTimer]().To("yellow")

		if !deep {
			xactTracked(b.State("yellow"))
			return
		}

		xactTracked(b.State("yellow", func(b *xactBuilder) {
			b.Initial("yellow.speed_up")
			xactTracked(b.State("yellow.speed_up"))
		}))
	})
}

// xactWalk is the chart of the "nested transition" cases.
func xactWalk(t *testing.T) *sc.Machine[string, counter] {
	t.Helper()

	return xactNew(t, "walk", func(b *xactBuilder) {
		b.Initial("green")
		xactTracked(b.State("green", func(b *xactBuilder) {
			b.Initial("green.walk")
			xactTracked(b.State("green.walk")).On[xactPedCountdown]().To("green.wait")
			xactTracked(b.State("green.wait"))
		}))
	})
}

// xactGreen is the chart of the "unhandled events" and "reentering
// self-transition" cases: a state green with the given tracked children, the
// first of which is its initial state.
func xactGreen(t *testing.T, greenTracked, restart bool, children ...string) *sc.Machine[string, counter] {
	t.Helper()

	return xactNew(t, "green", func(b *xactBuilder) {
		b.Initial("green")

		green := b.State("green")
		if len(children) > 0 {
			green = b.State("green", func(b *xactBuilder) {
				b.Initial("green." + children[0])
				for _, child := range children {
					xactTracked(b.State("green." + child))
				}
			})
		}

		if greenTracked {
			xactTracked(green)
		}
		if restart {
			green.On[xactRestart]().To("green").Reenter()
		}
	})
}

// XState: entry/exit actions > State.actions > should return the entry
// actions of an initial state
func TestXActStateActionsEntryActionsOfInitialState(t *testing.T) {
	t.Parallel()

	m := xactNew(t, "m", func(b *xactBuilder) {
		b.Chart().Entry("enter", noop)
		b.Initial("green")
		b.State("green").Entry("enter", noop)
	})

	_, effects := m.Start(counter{N: 0}, epoch)

	require.Equal(t, []string{"m/enter", "green/enter"}, names(effects))
}

// XState: entry/exit actions > State.actions > should return the entry
// actions of an initial state (deep)
func TestXActStateActionsEntryActionsOfInitialStateDeep(t *testing.T) {
	t.Parallel()

	m := xactNew(t, "m", func(b *xactBuilder) {
		b.Chart().Entry("enter", noop)
		b.Initial("a")
		b.State("a", func(b *xactBuilder) {
			b.Initial("a.a1")
			b.State("a.a1").Entry("enter", noop).On[xactNext]().To("a.a2")
			b.State("a.a2")
		}).Entry("enter", noop).On[xactChange]().To("b")
		b.State("b")
	})

	_, effects := m.Start(counter{N: 0}, epoch)

	require.Equal(t, []string{"m/enter", "a/enter", "a.a1/enter"}, names(effects))
}

// XState: entry/exit actions > State.actions > should return the entry
// actions of an initial state (parallel)
func TestXActStateActionsEntryActionsOfInitialStateParallel(t *testing.T) {
	t.Parallel()

	m := xactNew(t, "m", func(b *xactBuilder) {
		b.Regions()
		b.Chart().Entry("enter", noop)
		b.State("a", func(b *xactBuilder) {
			b.Initial("a.a1")
			b.State("a.a1").Entry("enter", noop)
		}).Entry("enter", noop)
		b.State("b", func(b *xactBuilder) {
			b.Initial("b.b1")
			b.State("b.b1").Entry("enter", noop)
		}).Entry("enter", noop)
	})

	_, effects := m.Start(counter{N: 0}, epoch)

	require.Equal(t, []string{"m/enter", "a/enter", "a.a1/enter", "b/enter", "b.b1/enter"}, names(effects))
}

// XState: entry/exit actions > State.actions > should return the entry and
// exit actions of a transition
func TestXActStateActionsEntryAndExitActionsOfTransition(t *testing.T) {
	t.Parallel()

	m := xactLights(t, false)
	snap, _ := m.Start(counter{N: 0}, epoch)
	_, effects := m.Transition(snap, xactTimer{}, epoch)

	require.Equal(t, []string{"green/exit", "yellow/enter"}, names(effects))
}

// XState: entry/exit actions > State.actions > should return the entry and
// exit actions of a deep transition
func TestXActStateActionsEntryAndExitActionsOfDeepTransition(t *testing.T) {
	t.Parallel()

	m := xactLights(t, true)
	snap, _ := m.Start(counter{N: 0}, epoch)
	_, effects := m.Transition(snap, xactTimer{}, epoch)

	require.Equal(t, []string{"green/exit", "yellow/enter", "yellow.speed_up/enter"}, names(effects))
}

// XState: entry/exit actions > State.actions > should return the entry and
// exit actions of a nested transition
func TestXActStateActionsEntryAndExitActionsOfNestedTransition(t *testing.T) {
	t.Parallel()

	m := xactWalk(t)
	snap, _ := m.Start(counter{N: 0}, epoch)
	_, effects := m.Transition(snap, xactPedCountdown{}, epoch)

	require.Equal(t, []string{"green.walk/exit", "green.wait/enter"}, names(effects))
}

// XState: entry/exit actions > State.actions > should not have actions for
// unhandled events (shallow)
func TestXActStateActionsNoActionsForUnhandledEventsShallow(t *testing.T) {
	t.Parallel()

	m := xactGreen(t, true, false)
	snap, _ := m.Start(counter{N: 0}, epoch)
	next, effects := m.Transition(snap, xactFake{}, epoch)

	require.Empty(t, effects)
	require.Equal(t, snap.Seq(), next.Seq())
}

// XState: entry/exit actions > State.actions > should not have actions for
// unhandled events (deep)
func TestXActStateActionsNoActionsForUnhandledEventsDeep(t *testing.T) {
	t.Parallel()

	m := xactGreen(t, false, false, "walk", "wait", "stop")
	snap, _ := m.Start(counter{N: 0}, epoch)
	next, effects := m.Transition(snap, xactFake{}, epoch)

	require.Empty(t, effects)
	require.Equal(t, snap.Seq(), next.Seq())
}

// XState: entry/exit actions > State.actions > should exit and enter the
// state for reentering self-transitions (shallow)
func TestXActStateActionsReenteringSelfTransitionShallow(t *testing.T) {
	t.Parallel()

	m := xactGreen(t, true, true)
	snap, _ := m.Start(counter{N: 0}, epoch)
	_, effects := m.Transition(snap, xactRestart{}, epoch)

	require.Equal(t, []string{"green/exit", "green/enter"}, names(effects))
}

// XState: entry/exit actions > State.actions > should exit and enter the
// state for reentering self-transitions (deep)
func TestXActStateActionsReenteringSelfTransitionDeep(t *testing.T) {
	t.Parallel()

	m := xactGreen(t, true, true, "walk", "wait", "stop")
	snap, _ := m.Start(counter{N: 0}, epoch)
	_, effects := m.Transition(snap, xactRestart{}, epoch)

	require.Equal(t, []string{"green.walk/exit", "green/exit", "green/enter", "green.walk/enter"}, names(effects))
}

// XState: entry/exit actions > State.actions > should return actions for
// parallel machines
func TestXActStateActionsActionsForParallelMachines(t *testing.T) {
	t.Parallel()

	m := xactNew(t, "m", func(b *xactBuilder) {
		b.Regions()
		xactTracked(b.State("a", func(b *xactBuilder) {
			b.Initial("a.a1")
			xactTracked(b.State("a.a1")).On[xactChange]().
				Do("do_a2", xactDo[xactChange]).
				Do("another_do_a2", xactDo[xactChange]).
				To("a.a2")
			xactTracked(b.State("a.a2"))
		}))
		xactTracked(b.State("b", func(b *xactBuilder) {
			b.Initial("b.b1")
			xactTracked(b.State("b.b1")).On[xactChange]().Do("do_b2", xactDo[xactChange]).To("b.b2")
			xactTracked(b.State("b.b2"))
		}))
	})

	snap, _ := m.Start(counter{N: 0}, epoch)
	_, effects := m.Transition(snap, xactChange{}, epoch)

	require.Equal(t, []string{
		"b.b1/exit", // reverse document order
		"a.a1/exit",
		"a.a1/do_a2",
		"a.a1/another_do_a2",
		"b.b1/do_b2",
		"a.a2/enter",
		"b.b2/enter",
	}, names(effects))
}

// XState: entry/exit actions > State.actions > should return nested actions
// in the correct (child to parent) order
func TestXActStateActionsNestedActionsChildToParentOrder(t *testing.T) {
	t.Parallel()

	m := xactNew(t, "m", func(b *xactBuilder) {
		b.Initial("a")
		xactTracked(b.State("a", func(b *xactBuilder) {
			b.Initial("a.a1")
			xactTracked(b.State("a.a1"))
		})).On[xactChange]().To("b")
		xactTracked(b.State("b", func(b *xactBuilder) {
			b.Initial("b.b1")
			xactTracked(b.State("b.b1"))
		}))
	})

	snap, _ := m.Start(counter{N: 0}, epoch)
	_, effects := m.Transition(snap, xactChange{}, epoch)

	require.Equal(t, []string{"a.a1/exit", "a/exit", "b/enter", "b.b1/enter"}, names(effects))
}

// XState: entry/exit actions > State.actions > should ignore parent state
// actions for same-parent substates
func TestXActStateActionsIgnoresParentActionsForSameParentSubstates(t *testing.T) {
	t.Parallel()

	m := xactNew(t, "m", func(b *xactBuilder) {
		b.Initial("a")
		b.State("a", func(b *xactBuilder) {
			b.Initial("a.a1")
			xactTracked(b.State("a.a1")).On[xactNext]().To("a.a2")
			xactTracked(b.State("a.a2"))
		})
	})

	snap, _ := m.Start(counter{N: 0}, epoch)
	_, effects := m.Transition(snap, xactNext{}, epoch)

	require.Equal(t, []string{"a.a1/exit", "a.a2/enter"}, names(effects))
}

// XState: entry/exit actions > State.actions > should work with function
// actions
//
// XState's spies are the actions entrySpy, exitSpy and transitionSpy.
func TestXActStateActionsWorksWithFunctionActions(t *testing.T) {
	t.Parallel()

	m := xactNew(t, "m", func(b *xactBuilder) {
		b.Initial("a")
		b.State("a", func(b *xactBuilder) {
			b.Initial("a.a1")
			xactTracked(b.State("a.a1")).On[xactNextFn]().To("a.a3")
			xactTracked(b.State("a.a2"))
			b.State("a.a3").
				Entry("enter", noop).Entry("entrySpy", noop).
				Exit("exit", noop).Exit("exitSpy", noop).
				On[xactNext]().Do("transitionSpy", xactDo[xactNext]).To("a.a2")
		})
	})

	snap, _ := m.Start(counter{N: 0}, epoch)

	snap, effects := m.Transition(snap, xactNextFn{}, epoch)
	require.Equal(t, []string{"a.a1/exit", "a.a3/enter", "a.a3/entrySpy"}, names(effects))

	_, effects = m.Transition(snap, xactNext{}, epoch)
	require.Equal(t, []string{"a.a3/exit", "a.a3/exitSpy", "a.a3/transitionSpy", "a.a2/enter"}, names(effects))
}

// XState: entry/exit actions > State.actions > should exit children of
// parallel state nodes
func TestXActStateActionsExitsChildrenOfParallelStateNodes(t *testing.T) {
	t.Parallel()

	m := xactNew(t, "m", func(b *xactBuilder) {
		b.Initial("B")
		xactTracked(b.State("A")).On[xactToB]().To("B")
		b.Parallel("B", func(b *xactBuilder) {
			xactTracked(b.State("B.C", func(b *xactBuilder) {
				b.Initial("B.C.C1")
				xactTracked(b.State("B.C.C1"))
			}))
			xactTracked(b.State("B.D", func(b *xactBuilder) {
				b.Initial("B.D.D1")
				xactTracked(b.State("B.D.D1"))
			}))
		}).Entry("enter", noop).Exit("exit", noop).On[xactToA]().To("A")
	})

	snap, _ := m.Start(counter{N: 0}, epoch)
	_, effects := m.Transition(snap, xactToA{}, epoch)

	require.Equal(t, []string{"B.D.D1/exit", "B.D/exit", "B.C.C1/exit", "B.C/exit", "B/exit", "A/enter"}, names(effects))
}

// XState: entry/exit actions > State.actions > should reenter targeted
// ancestor (as it's a descendant of the transition domain)
func TestXActStateActionsReentersTargetedAncestor(t *testing.T) {
	t.Parallel()

	m := xactNew(t, "m", func(b *xactBuilder) {
		b.Initial("loaded")
		xactTracked(b.State("loaded", func(b *xactBuilder) {
			b.Initial("loaded.idle")
			xactTracked(b.State("loaded.idle")).On[xactUpdate]().To("loaded")
		}))
	})

	snap, _ := m.Start(counter{N: 0}, epoch)
	_, effects := m.Transition(snap, xactUpdate{}, epoch)

	require.Equal(t, []string{"loaded.idle/exit", "loaded/exit", "loaded/enter", "loaded.idle/enter"}, names(effects))
}

// XState: entry/exit actions > State.actions > root entry/exit actions
// should be called on root reentering transitions
func TestXActStateActionsRootEntryExitOnRootReenteringTransitions(t *testing.T) {
	t.Parallel()

	m := xactNew(t, "root", func(b *xactBuilder) {
		xactTracked(b.Chart()).On[xactEvent]().To("two").Reenter()
		b.Initial("one")
		b.State("one")
		b.State("two")
	})

	snap, _ := m.Start(counter{N: 0}, epoch)
	snap, effects := m.Transition(snap, xactEvent{}, epoch)

	require.Equal(t, []string{"root/exit", "root/enter"}, names(effects))
	require.Equal(t, []string{"two"}, snap.Active())
}

// XState: entry/exit actions > State.actions > should ignore same-parent
// state actions (sparse) > with a relative transition
// XState: entry/exit actions > State.actions > should ignore same-parent
// state actions (sparse) > with an absolute transition
//
// An sc target is always a state's identifier, so the two cases differ only
// in the event and in the state pong that the second declares.
func TestXActStateActionsIgnoresSameParentStateActionsSparse(t *testing.T) {
	t.Parallel()

	t.Run("with a relative transition", func(t *testing.T) {
		t.Parallel()

		m := xactNew(t, "m", func(b *xactBuilder) {
			b.Initial("ping")
			xactTracked(b.State("ping", func(b *xactBuilder) {
				b.Initial("ping.foo")
				xactTracked(b.State("ping.foo")).On[xactTack]().To("ping.bar")
				xactTracked(b.State("ping.bar"))
			}))
		})

		snap, _ := m.Start(counter{N: 0}, epoch)
		_, effects := m.Transition(snap, xactTack{}, epoch)

		require.Equal(t, []string{"ping.foo/exit", "ping.bar/enter"}, names(effects))
	})

	t.Run("with an absolute transition", func(t *testing.T) {
		t.Parallel()

		m := xactNew(t, "root", func(b *xactBuilder) {
			b.Initial("ping")
			xactTracked(b.State("ping", func(b *xactBuilder) {
				b.Initial("ping.foo")
				xactTracked(b.State("ping.foo")).On[xactAbsoluteTack]().To("ping.bar")
				xactTracked(b.State("ping.bar"))
			}))
			xactTracked(b.State("pong"))
		})

		snap, _ := m.Start(counter{N: 0}, epoch)
		_, effects := m.Transition(snap, xactAbsoluteTack{}, epoch)

		require.Equal(t, []string{"ping.foo/exit", "ping.bar/enter"}, names(effects))
	})
}

// XState: entry/exit actions > entry/exit actions > should return the entry
// actions of an initial state
func TestXActEntryExitEntryActionsOfInitialState(t *testing.T) {
	t.Parallel()

	m := xactNew(t, "m", func(b *xactBuilder) {
		xactTracked(b.Chart())
		b.Initial("green")
		xactTracked(b.State("green"))
	})

	_, effects := m.Start(counter{N: 0}, epoch)

	require.Equal(t, []string{"m/enter", "green/enter"}, names(effects))
}

// XState: entry/exit actions > entry/exit actions > should return the entry
// and exit actions of a transition
func TestXActEntryExitEntryAndExitActionsOfTransition(t *testing.T) {
	t.Parallel()

	m := xactLights(t, false)
	snap, _ := m.Start(counter{N: 0}, epoch)
	snap, effects := m.Transition(snap, xactTimer{}, epoch)

	require.Equal(t, []string{"green/exit", "yellow/enter"}, names(effects))
	require.Equal(t, []string{"yellow"}, snap.Active())
}

// XState: entry/exit actions > entry/exit actions > should return the entry
// and exit actions of a deep transition
func TestXActEntryExitEntryAndExitActionsOfDeepTransition(t *testing.T) {
	t.Parallel()

	m := xactLights(t, true)
	snap, _ := m.Start(counter{N: 0}, epoch)
	snap, effects := m.Transition(snap, xactTimer{}, epoch)

	require.Equal(t, []string{"green/exit", "yellow/enter", "yellow.speed_up/enter"}, names(effects))
	require.Equal(t, []string{"yellow", "yellow.speed_up"}, snap.Active())
}

// XState: entry/exit actions > entry/exit actions > should return the entry
// and exit actions of a nested transition
func TestXActEntryExitEntryAndExitActionsOfNestedTransition(t *testing.T) {
	t.Parallel()

	m := xactWalk(t)
	snap, _ := m.Start(counter{N: 0}, epoch)
	snap, effects := m.Transition(snap, xactPedCountdown{}, epoch)

	require.Equal(t, []string{"green.walk/exit", "green.wait/enter"}, names(effects))
	require.Equal(t, []string{"green", "green.wait"}, snap.Active())
}

// XState: entry/exit actions > entry/exit actions > should keep the same
// state for unhandled events (shallow)
func TestXActEntryExitKeepsSameStateForUnhandledEventsShallow(t *testing.T) {
	t.Parallel()

	m := xactGreen(t, true, false)
	snap, _ := m.Start(counter{N: 0}, epoch)
	next, effects := m.Transition(snap, xactFake{}, epoch)

	require.Empty(t, effects)
	require.Equal(t, snap.Seq(), next.Seq())
	require.Equal(t, []string{"green"}, next.Active())
}

// XState: entry/exit actions > entry/exit actions > should keep the same
// state for unhandled events (deep)
func TestXActEntryExitKeepsSameStateForUnhandledEventsDeep(t *testing.T) {
	t.Parallel()

	m := xactGreen(t, true, false, "walk")
	snap, _ := m.Start(counter{N: 0}, epoch)
	next, effects := m.Transition(snap, xactFake{}, epoch)

	require.Empty(t, effects)
	require.Equal(t, snap.Seq(), next.Seq())
	require.Equal(t, []string{"green", "green.walk"}, next.Active())
}

// XState: entry/exit actions > entry/exit actions > should exit and enter
// the state for reentering self-transitions (shallow)
func TestXActEntryExitReenteringSelfTransitionShallow(t *testing.T) {
	t.Parallel()

	m := xactGreen(t, true, true)
	snap, _ := m.Start(counter{N: 0}, epoch)
	_, effects := m.Transition(snap, xactRestart{}, epoch)

	require.Equal(t, []string{"green/exit", "green/enter"}, names(effects))
}

// XState: entry/exit actions > entry/exit actions > should exit and enter
// the state for reentering self-transitions (deep)
func TestXActEntryExitReenteringSelfTransitionDeep(t *testing.T) {
	t.Parallel()

	m := xactGreen(t, true, true, "walk")
	snap, _ := m.Start(counter{N: 0}, epoch)
	_, effects := m.Transition(snap, xactRestart{}, epoch)

	require.Equal(t, []string{"green.walk/exit", "green/exit", "green/enter", "green.walk/enter"}, names(effects))
}

// XState: entry/exit actions > entry/exit actions > should exit current node
// and enter target node when target is not a descendent or ancestor of
// current
func TestXActEntryExitTargetNeitherDescendantNorAncestor(t *testing.T) {
	t.Parallel()

	m := xactNew(t, "m", func(b *xactBuilder) {
		b.Initial("A")
		xactTracked(b.State("A", func(b *xactBuilder) {
			b.Initial("A.A1")
			xactTracked(b.State("A.A1")).On[xactNext]().To("A.A2.A2_child")
			xactTracked(b.State("A.A2", func(b *xactBuilder) {
				b.Initial("A.A2.A2_child")
				xactTracked(b.State("A.A2.A2_child"))
			}))
		}))
	})

	snap, _ := m.Start(counter{N: 0}, epoch)
	_, effects := m.Transition(snap, xactNext{}, epoch)

	require.Equal(t, []string{"A.A1/exit", "A.A2/enter", "A.A2.A2_child/enter"}, names(effects))
}

// XState: entry/exit actions > entry/exit actions > should exit current node
// and reenter target node when target is ancestor of current
func TestXActEntryExitReentersTargetThatIsAncestorOfCurrent(t *testing.T) {
	t.Parallel()

	m := xactNew(t, "m", func(b *xactBuilder) {
		b.Initial("A")
		xactTracked(b.State("A", func(b *xactBuilder) {
			b.Initial("A.A1")
			xactTracked(b.State("A.A1")).On[xactNext]().To("A.A2")
			xactTracked(b.State("A.A2", func(b *xactBuilder) {
				b.Initial("A.A2.A2_child")
				xactTracked(b.State("A.A2.A2_child")).On[xactNext]().To("A")
			}))
		}))
	})

	snap, _ := m.Start(counter{N: 0}, epoch)
	snap, _ = m.Transition(snap, xactNext{}, epoch)
	_, effects := m.Transition(snap, xactNext{}, epoch)

	require.Equal(t, []string{"A.A2.A2_child/exit", "A.A2/exit", "A/exit", "A/enter", "A.A1/enter"}, names(effects))
}

// XState: entry/exit actions > entry/exit actions > should enter all
// descendents when target is a descendent of the source when using an
// reentering transition
func TestXActEntryExitReenteringTransitionToDescendantEntersAllDescendants(t *testing.T) {
	t.Parallel()

	m := xactNew(t, "m", func(b *xactBuilder) {
		b.Initial("A")
		xactTracked(b.State("A", func(b *xactBuilder) {
			b.Initial("A.A1")
			xactTracked(b.State("A.A1"))
			xactTracked(b.State("A.A2", func(b *xactBuilder) {
				b.Initial("A.A2.A2a")
				xactTracked(b.State("A.A2.A2a"))
			}))
		})).On[xactNext]().To("A.A2").Reenter()
	})

	snap, _ := m.Start(counter{N: 0}, epoch)
	_, effects := m.Transition(snap, xactNext{}, epoch)

	require.Equal(t, []string{"A.A1/exit", "A/exit", "A/enter", "A.A2/enter", "A.A2.A2a/enter"}, names(effects))
}

// xactDeepSelf is the chart of the two "deep descendant" cases: a holds a1,
// which holds a11, and EV targets a itself.
func xactDeepSelf(t *testing.T, reenter bool) *sc.Machine[string, counter] {
	t.Helper()

	return xactNew(t, "m", func(b *xactBuilder) {
		b.Initial("a")
		ev := xactTracked(b.State("a", func(b *xactBuilder) {
			b.Initial("a.a1")
			xactTracked(b.State("a.a1", func(b *xactBuilder) {
				b.Initial("a.a1.a11")
				xactTracked(b.State("a.a1.a11"))
			}))
		})).On[xactEV]().To("a")
		if reenter {
			ev.Reenter()
		}
	})
}

// XState: entry/exit actions > entry/exit actions > should exit deep
// descendant during a default self-transition
func TestXActEntryExitExitsDeepDescendantDuringDefaultSelfTransition(t *testing.T) {
	t.Parallel()

	m := xactDeepSelf(t, false)
	snap, _ := m.Start(counter{N: 0}, epoch)
	snap, _ = m.Transition(snap, xactEV{}, epoch)
	_, effects := m.Transition(snap, xactEV{}, epoch)

	require.Equal(t, []string{"a.a1.a11/exit", "a.a1/exit", "a.a1/enter", "a.a1.a11/enter"}, names(effects))
}

// XState: entry/exit actions > entry/exit actions > should exit deep
// descendant during a reentering self-transition
func TestXActEntryExitExitsDeepDescendantDuringReenteringSelfTransition(t *testing.T) {
	t.Parallel()

	m := xactDeepSelf(t, true)
	snap, _ := m.Start(counter{N: 0}, epoch)
	_, effects := m.Transition(snap, xactEV{}, epoch)

	require.Equal(t, []string{
		"a.a1.a11/exit", "a.a1/exit", "a/exit",
		"a/enter", "a.a1/enter", "a.a1.a11/enter",
	}, names(effects))
}

// XState: entry/exit actions > entry/exit actions > should not reenter leaf
// state during its default self-transition
//
// XState tracks the root's entry and exit actions here; the leaf has none.
// The port also tracks the leaf, which is the state the title is about.
func TestXActEntryExitDoesNotReenterLeafDuringDefaultSelfTransition(t *testing.T) {
	t.Parallel()

	m := xactNew(t, "m", func(b *xactBuilder) {
		xactTracked(b.Chart())
		b.Initial("a")
		b.State("a", func(b *xactBuilder) {
			b.Initial("a.a1")
			xactTracked(b.State("a.a1")).On[xactEV]().To("a.a1")
		})
	})

	snap, _ := m.Start(counter{N: 0}, epoch)
	next, effects := m.Transition(snap, xactEV{}, epoch)

	require.Empty(t, effects)
	require.Equal(t, snap.Active(), next.Active())
}

// XState: entry/exit actions > entry/exit actions > should reenter leaf
// state during its reentering self-transition
func TestXActEntryExitReentersLeafDuringReenteringSelfTransition(t *testing.T) {
	t.Parallel()

	m := xactNew(t, "m", func(b *xactBuilder) {
		b.Initial("a")
		xactTracked(b.State("a", func(b *xactBuilder) {
			b.Initial("a.a1")
			xactTracked(b.State("a.a1")).On[xactEV]().To("a.a1").Reenter()
		}))
	})

	snap, _ := m.Start(counter{N: 0}, epoch)
	_, effects := m.Transition(snap, xactEV{}, epoch)

	require.Equal(t, []string{"a.a1/exit", "a.a1/enter"}, names(effects))
}

// XState: entry/exit actions > entry/exit actions > should not enter exited
// state when targeting its ancestor and when its former descendant gets
// selected through initial state
func TestXActEntryExitTargetingAncestorSelectsFormerDescendantThroughInitial(t *testing.T) {
	t.Parallel()

	m := xactNew(t, "m", func(b *xactBuilder) {
		b.Initial("a")
		xactTracked(b.State("a", func(b *xactBuilder) {
			b.Initial("a.a1")
			xactTracked(b.State("a.a1")).On[xactEV]().To("a.a2")
			xactTracked(b.State("a.a2")).On[xactEV]().To("a")
		}))
	})

	snap, _ := m.Start(counter{N: 0}, epoch)
	snap, _ = m.Transition(snap, xactEV{}, epoch)
	_, effects := m.Transition(snap, xactEV{}, epoch)

	require.Equal(t, []string{"a.a2/exit", "a/exit", "a/enter", "a.a1/enter"}, names(effects))
}

// XState: entry/exit actions > entry/exit actions > should not enter exited
// state when targeting its ancestor and when its latter descendant gets
// selected through initial state
func TestXActEntryExitTargetingAncestorSelectsLatterDescendantThroughInitial(t *testing.T) {
	t.Parallel()

	m := xactNew(t, "m", func(b *xactBuilder) {
		b.Initial("a")
		xactTracked(b.State("a", func(b *xactBuilder) {
			b.Initial("a.a2")
			xactTracked(b.State("a.a1")).On[xactEV]().To("a")
			xactTracked(b.State("a.a2")).On[xactEV]().To("a.a1")
		}))
	})

	snap, _ := m.Start(counter{N: 0}, epoch)
	snap, _ = m.Transition(snap, xactEV{}, epoch)
	_, effects := m.Transition(snap, xactEV{}, epoch)

	require.Equal(t, []string{"a.a1/exit", "a/exit", "a/enter", "a.a2/enter"}, names(effects))
}

// XState: entry/exit actions > entry/exit actions > passes the entered/exited
// state node to entry and exit
//
// An sc action is not handed a state node; the effect that runs it names the
// state it belongs to, which is what the port checks.
func TestXActEntryExitPassesEnteredExitedStateToEntryAndExit(t *testing.T) {
	t.Parallel()

	m := xactNew(t, "m", func(b *xactBuilder) {
		b.Initial("m.a")
		b.On[xactLeave]().To("m.b")
		b.State("m.b")
		b.State("m.a", func(b *xactBuilder) {
			b.Initial("m.a.a1")
			b.State("m.a.a1").Entry("entry", noop).Exit("exit", noop).On[xactNext]().To("m.a.a2")
			b.State("m.a.a2")
		}).Entry("entry", noop).Exit("exit", noop)
	})

	var log []string

	snap, effects := m.Start(counter{N: 0}, epoch)
	log = append(log, names(effects)...)

	snap, effects = m.Transition(snap, xactNext{}, epoch)
	log = append(log, names(effects)...)

	snap, effects = m.Transition(snap, xactLeave{}, epoch)
	log = append(log, names(effects)...)

	require.Equal(t, []string{"m.a/entry", "m.a.a1/entry", "m.a.a1/exit", "m.a/exit"}, log)
	require.Equal(t, []string{"m.b"}, snap.Active())
}

// XState: entry/exit actions > entry/exit actions > with any number of
// parameters > passes guards and delays to entry and exit functions with one
// parameter
//
// Partial: sc has no registry of guards and delays to hand to an entry or
// exit function. What remains is that the exit action runs and that the
// entry assignment computes the context from the context.
func TestXActEntryExitEntryAssignmentComputesFromContext(t *testing.T) {
	t.Parallel()

	m := xactNew(t, "m", func(b *sc.Builder[string, xactAge]) {
		b.Initial("start")
		b.State("start").
			Exit("seen", func(context.Context, xactAge) {}).
			On[xactNext]().To("checked")
		b.State("checked").AssignOnEntry("checkAdult", func(c xactAge) xactAge {
			c.Adult = c.Age >= 18
			return c
		})
	})

	snap, _ := m.Start(xactAge{Age: 20, Adult: false}, epoch)
	snap, effects := m.Transition(snap, xactNext{}, epoch)

	require.False(t, snap.Done())
	require.True(t, snap.Context.Adult)
	require.Equal(t, []string{"start/seen"}, names(effects))
}

// XState: entry/exit actions > entry/exit actions > with any number of
// parameters > applies the context returned by a root exit function %s when
// the machine completes
// (both parametrisations, '(args)' and '(args, enq)', are one case in sc)
func TestXActEntryExitRootExitAssignmentAppliesWhenMachineCompletes(t *testing.T) {
	t.Parallel()

	m := xactNew(t, "m", func(b *sc.Builder[string, xactClosed]) {
		b.Chart().AssignOnExit("close", func(c xactClosed) xactClosed {
			c.Closed = true
			return c
		})
		b.Initial("open")
		b.State("open").On[xactClose]().To("closed")
		b.Final("closed")
	})

	snap, _ := m.Start(xactClosed{Closed: false}, epoch)
	require.False(t, snap.Context.Closed)

	snap, _ = m.Transition(snap, xactClose{}, epoch)

	require.True(t, snap.Done())
	require.Equal(t, xactClosed{Closed: true}, snap.Context)
}

// XState: entry/exit actions > entry/exit actions > with any number of
// parameters > passes the current children to an exit function %s
// (both parametrisations, '(args)' and '(args, enq)', are one case in sc)
//
// Partial: an sc exit action is not handed the running services. What the
// XState case shows with them, that the child state's invoke has already
// been stopped when the parent's exit action runs, is the order of the
// effects.
func TestXActEntryExitChildInvokeIsStoppedBeforeParentExitAction(t *testing.T) {
	t.Parallel()

	m := xactNew(t, "m", func(b *xactBuilder) {
		b.Initial("parent")
		b.State("parent", func(b *xactBuilder) {
			b.Initial("parent.child")
			b.State("parent.child").Requires[struct{}]("worker").OnError().Stay()
		}).Exit("exit", noop).On[xactLeave]().To("elsewhere")
		b.State("elsewhere")
	})

	snap, _ := m.Start(counter{N: 0}, epoch)
	_, effects := m.Transition(snap, xactLeave{}, epoch)

	require.Equal(t, []sc.EffectKind{sc.EffectStopInvocation, sc.EffectAction}, kinds(effects))
	require.Equal(t, []string{"parent.child/worker", "parent/exit"}, names(effects))
}

// XState: entry/exit actions > entry/exit actions > with any number of
// parameters > collects enqueued events from forwarding wrappers without
// warnings
//
// Partial: sc has no warnings and no wrapped entry functions; the two
// loaders are one chart. What remains is the event raised on entry.
func TestXActEntryExitEventRaisedOnEntryIsCollected(t *testing.T) {
	t.Parallel()

	m := xactNew(t, "loader", func(b *xactBuilder) {
		b.Initial("idle")
		b.State("idle").On[xactLoad]().To("loading")
		loading := b.State("loading").RaiseOnEntry("loaded", xactRaise(xactLoaded{}))
		loading.On[xactLoaded]().To("ready")
		loading.On[xactRetry]().To("idle")
		b.State("ready").On[xactRetry]().To("idle")
	})

	snap, _ := m.Start(counter{N: 0}, epoch)

	snap, _ = m.Transition(snap, xactLoad{}, epoch)
	require.Equal(t, []string{"ready"}, snap.Active())

	snap, _ = m.Transition(snap, xactRetry{}, epoch)
	snap, _ = m.Transition(snap, xactLoad{}, epoch)
	require.Equal(t, []string{"ready"}, snap.Active())
}

// XState: entry/exit actions > parallel states > should return entry action
// defined on parallel state
func TestXActParallelEntryActionDefinedOnParallelState(t *testing.T) {
	t.Parallel()

	m := xactNew(t, "m", func(b *xactBuilder) {
		b.Initial("start")
		xactTracked(b.State("start")).On[xactEnterPar]().To("p1")
		b.Parallel("p1", func(b *xactBuilder) {
			xactTracked(b.State("p1.nested", func(b *xactBuilder) {
				b.Initial("p1.nested.inner")
				xactTracked(b.State("p1.nested.inner"))
			}))
		}).Entry("enter", noop).Exit("exit", noop)
	})

	snap, _ := m.Start(counter{N: 0}, epoch)
	_, effects := m.Transition(snap, xactEnterPar{}, epoch)

	require.Equal(t, []string{"start/exit", "p1/enter", "p1.nested/enter", "p1.nested.inner/enter"}, names(effects))
}

// xactCamera is the chart of the two "reenter parallel region" cases, which
// differ in XState only in the order of a state's properties.
func xactCamera(t *testing.T) *sc.Machine[string, counter] {
	t.Helper()

	return xactNew(t, "m", func(b *xactBuilder) {
		b.Initial("ready")
		b.Parallel("ready", func(b *xactBuilder) {
			xactTracked(b.State("ready.devicesInfo"))
			xactTracked(b.State("ready.camera", func(b *xactBuilder) {
				b.Initial("ready.camera.on")
				xactTracked(b.State("ready.camera.on"))
				xactTracked(b.State("ready.camera.off"))
			}))
		}).Entry("enter", noop).Exit("exit", noop).On[xactFoo]().To("ready.camera.off").Reenter()
	})
}

// xactCameraReentered is what both cases expect of FOO.
var xactCameraReentered = []string{
	"ready.camera.on/exit",
	"ready.camera/exit",
	"ready.devicesInfo/exit",
	"ready/exit",
	"ready/enter",
	"ready.devicesInfo/enter",
	"ready.camera/enter",
	"ready.camera.off/enter",
}

// XState: entry/exit actions > parallel states > should reenter parallel
// region when a parallel state gets reentered while targeting another region
func TestXActParallelReentersRegionWhenParallelStateGetsReentered(t *testing.T) {
	t.Parallel()

	m := xactCamera(t)
	snap, _ := m.Start(counter{N: 0}, epoch)
	_, effects := m.Transition(snap, xactFoo{}, epoch)

	require.Equal(t, xactCameraReentered, names(effects))
}

// XState: entry/exit actions > parallel states > should reenter parallel
// region when a parallel state is reentered while targeting another region
func TestXActParallelReentersRegionWhenParallelStateIsReentered(t *testing.T) {
	t.Parallel()

	m := xactCamera(t)
	snap, _ := m.Start(counter{N: 0}, epoch)
	snap, effects := m.Transition(snap, xactFoo{}, epoch)

	require.Equal(t, xactCameraReentered, names(effects))
	require.Equal(t, []string{"ready", "ready.devicesInfo", "ready.camera", "ready.camera.off"}, snap.Active())
}

// XState: entry/exit actions > targetless transitions > shouldn't exit a
// state on a parent's targetless transition
//
// XState tracks the root's entry and exit actions; the port also tracks the
// state one, which is the state the title is about.
func TestXActTargetlessParentTransitionDoesNotExitState(t *testing.T) {
	t.Parallel()

	m := xactNew(t, "m", func(b *xactBuilder) {
		xactTracked(b.Chart()).On[xactWhatever]().Do("whatever", xactDo[xactWhatever])
		b.Initial("one")
		xactTracked(b.State("one"))
	})

	snap, _ := m.Start(counter{N: 0}, epoch)
	_, effects := m.Transition(snap, xactWhatever{}, epoch)

	require.Equal(t, []string{"m/whatever"}, names(effects))
}

// XState: entry/exit actions > targetless transitions > shouldn't exit (and
// reenter) state on targetless delayed transition
func TestXActTargetlessDelayedTransitionDoesNotExitState(t *testing.T) {
	t.Parallel()

	m := xactNew(t, "m", func(b *xactBuilder) {
		xactTracked(b.Chart())
		b.Initial("one")
		xactTracked(b.State("one")).After(10*time.Millisecond).Do("tick", xactDo[struct{}])
	})

	snap, _ := m.Start(counter{N: 0}, epoch)
	snap, effects := m.Advance(snap, epoch.Add(50*time.Millisecond))

	require.Equal(t, []string{"one/tick"}, names(effects))
	require.Equal(t, []string{"one"}, snap.Active())
	require.Empty(t, snap.Timers())
}

// XState: entry/exit actions > when reaching a final state > exit actions
// should be called when invoked machine reaches its final state
//
// Partial: XState also gives the final state a an exit action and expects it
// called. An sc final state has no exit actions, so only the root's is
// ported.
func TestXActFinalExitActionsCalledWhenInvokedMachineReachesFinalState(t *testing.T) {
	t.Parallel()

	synctest.Test(t, func(t *testing.T) {
		var exitCalled atomic.Bool

		child := xactNew(t, "child", func(b *xactBuilder) {
			b.Chart().Exit("exit", func(context.Context, counter) { exitCalled.Store(true) })
			b.Initial("a")
			b.Final("a")
		})

		parent := xactNew(t, "parent", func(b *xactBuilder) {
			b.Initial("active")
			inv := b.State("active").InvokeMachine("child", child, xactSame, xactNoOutput)
			inv.OnDone().To("finished")
			inv.OnError().Stay()
			b.Final("finished")
		})

		inst, err := parent.Instance(t.Context(), counter{N: 0})
		require.NoError(t, err)
		defer inst.Close()

		synctest.Wait()

		require.True(t, inst.Snapshot().Done())
		require.True(t, exitCalled.Load())
	})
}

// XState: entry/exit actions > when stopped > exit actions should not be
// called when stopping a machine
func TestXActStoppedExitActionsNotCalledWhenStoppingMachine(t *testing.T) {
	t.Parallel()

	rootCalled, childCalled := false, false

	m := xactNew(t, "m", func(b *xactBuilder) {
		b.Chart().Exit("rootSpy", func(context.Context, counter) { rootCalled = true })
		b.Initial("a")
		b.State("a").Exit("childSpy", func(context.Context, counter) { childCalled = true })
	})

	inst, err := m.Instance(t.Context(), counter{N: 0})
	require.NoError(t, err)

	inst.Close()

	require.False(t, rootCalled)
	require.False(t, childCalled)
}

// XState: entry/exit actions > when stopped > an exit action executed when
// an interpreter reaches its final state should be called with the last
// received event
//
// XState's exit action is the root's. The port gives the root one of each
// kind of sc action: its own Exit action and a chart action run with OnExit.
//
// DIVERGES from XState: XState hands every exit action the event being
// processed. In sc a state's own Exit action takes no event, and its effect's
// Event is nil, on completion as on any other exit. A chart action run with
// OnExit is told the event in its Move, and its effect carries it.
func TestXActStoppedExitActionSeesLastReceivedEvent(t *testing.T) {
	t.Parallel()

	var got any

	m := xactNew(t, "m", func(b *xactBuilder) {
		b.Action("record", func(_ context.Context, _ counter, mv sc.Move[string]) { got = mv.Event })
		b.Chart().Exit("inline", noop).OnExit("record")
		b.Initial("a")
		b.State("a").OnExit("record").On[xactNext]().To("b")
		b.State("a").On[xactLeave]().To("c")
		b.Final("b")
		b.State("c")
	})

	start, _ := m.Start(counter{N: 0}, epoch)
	snap, effects := m.Transition(start, xactNext{}, epoch)

	require.True(t, snap.Done())
	require.Equal(t, []string{"a/record", "m/inline", "m/record"}, names(effects))
	require.Equal(t, xactNext{}, effects[0].Event)
	require.Nil(t, effects[1].Event)
	require.Equal(t, xactNext{}, effects[2].Event)

	require.NoError(t, m.Exec(t.Context(), effects[2], func(any) {}))
	require.Equal(t, xactNext{}, got)
}

// XState: entry/exit actions > when stopped > stopping an interpreter that
// receives events from its children exit handlers should not throw
func TestXActStoppedClosingParentOfChildThatEmitsOnExitDoesNotPanic(t *testing.T) {
	t.Parallel()

	synctest.Test(t, func(t *testing.T) {
		child := xactNew(t, "child", func(b *xactBuilder) {
			b.Initial("idle")
			b.State("idle").EmitOnExit("exit", func(counter) xactExit { return xactExit{} })
		})

		parent := xactNew(t, "parent", func(b *xactBuilder) {
			b.Chart().InvokeMachine("child", child, xactSame, xactNoOutput).OnError().Stay()
		})

		inst, err := parent.Instance(t.Context(), counter{N: 0})
		require.NoError(t, err)

		synctest.Wait()

		require.NotPanics(t, inst.Close)
	})
}

// XState: entry/exit actions > when stopped > sent events from exit handlers
// of a done child should be received by the parent
//
// The parent's chart invokes the child, where XState spawns it while building
// the context.
func TestXActStoppedEventsFromDoneChildAreReceivedByParent(t *testing.T) {
	t.Parallel()

	synctest.Test(t, func(t *testing.T) {
		var received atomic.Bool

		child := xactNew(t, "child", func(b *xactBuilder) {
			b.Chart().EmitOnExit("childDone", func(counter) xactChildDone { return xactChildDone{} })
			b.Initial("active")
			b.State("active").On[xactFinish]().To("done")
			b.Final("done")
		})

		parent := xactNew(t, "parent", func(b *xactBuilder) {
			chart := b.Chart()
			chart.InvokeMachine("child", child, xactSame, xactNoOutput).OnError().Stay()
			chart.On[xactFinishChild]().SendTo("finish", "child", func(counter, xactFinishChild) any { return xactFinish{} })
			chart.On[xactChildDone]().Do("received", func(context.Context, counter, xactChildDone) { received.Store(true) })
		})

		inst, err := parent.Instance(t.Context(), counter{N: 0})
		require.NoError(t, err)
		defer inst.Close()

		synctest.Wait()
		require.False(t, received.Load())

		_, err = inst.Send(t.Context(), xactFinishChild{})
		require.NoError(t, err)

		synctest.Wait()

		require.True(t, received.Load())
	})
}

// XState: entry/exit actions > when stopped > sent events from exit handlers
// of a stopped child should not be received by its children
func TestXActStoppedEventsFromStoppedChildAreNotReceivedByItsChildren(t *testing.T) {
	t.Parallel()

	synctest.Test(t, func(t *testing.T) {
		var calls atomic.Int64

		grandchild := xactNew(t, "grandchild", func(b *xactBuilder) {
			b.Chart().On[xactStopped]().Do("spy", func(context.Context, counter, xactStopped) { calls.Add(1) })
		})

		child := xactNew(t, "child", func(b *xactBuilder) {
			chart := b.Chart()
			chart.InvokeMachine("myChild", grandchild, xactSame, xactNoOutput).OnError().Stay()
			chart.SendOnExit("stopped", "myChild", func(counter) any { return xactStopped{} })
		})

		parent := xactNew(t, "parent", func(b *xactBuilder) {
			b.Initial("a")
			a := b.State("a")
			a.InvokeMachine("child", child, xactSame, xactNoOutput).OnError().Stay()
			a.On[xactNext]().To("b")
			b.State("b")
		})

		inst, err := parent.Instance(t.Context(), counter{N: 0})
		require.NoError(t, err)
		defer inst.Close()

		synctest.Wait()

		step, err := inst.Send(t.Context(), xactNext{})
		require.NoError(t, err)
		require.Equal(t, []string{"b"}, step.To.Active())

		synctest.Wait()

		require.Zero(t, calls.Load())
	})
}

// XState: entry/exit actions > when stopped > should note execute referenced
// custom actions correctly when stopping an interpreter
func TestXActStoppedReferencedActionsNotExecutedWhenStopping(t *testing.T) {
	t.Parallel()

	called := false

	m := xactNew(t, "parent", func(b *xactBuilder) {
		b.Action("referencedAction", func(context.Context, counter, sc.Move[string]) { called = true })
		b.Chart().OnExit("referencedAction")
	})

	inst, err := m.Instance(t.Context(), counter{N: 0})
	require.NoError(t, err)

	inst.Close()

	require.False(t, called)
}

// XState: entry/exit actions > when stopped > should not execute builtin
// actions when stopping an interpreter
func TestXActStoppedBuiltinActionsNotExecutedWhenStopping(t *testing.T) {
	t.Parallel()

	called := false

	m := xactNew(t, "m", func(b *xactBuilder) {
		b.Chart().Exit("action", func(context.Context, counter) { called = true })
	})

	inst, err := m.Instance(t.Context(), counter{N: 0})
	require.NoError(t, err)

	inst.Close()

	require.False(t, called)
}

// XState: entry/exit actions > when stopped > should not execute exit
// actions of the settled state of the last initiated microstep after
// executing all actions from that microstep
func TestXActStoppedNoExitActionsOfSettledStateAfterStopInsideMicrostep(t *testing.T) {
	t.Parallel()

	var (
		executed []string
		inst     *sc.Instance[string, counter]
	)

	record := func(what string) func(context.Context, counter) {
		return func(context.Context, counter) { executed = append(executed, what) }
	}

	m := xactNew(t, "m", func(b *xactBuilder) {
		b.Initial("foo")
		b.State("foo").Exit("exit", record("foo exit action")).
			On[xactInit]().
			// Stops the instance while the event is still being processed.
			Do("stop", func(context.Context, counter, xactInit) { inst.Close() }).
			Do("transition", func(context.Context, counter, xactInit) {
				executed = append(executed, "foo transition action")
			}).
			To("bar")
		b.State("bar").Exit("exit", record("bar exit action"))
	})

	inst, err := m.Instance(t.Context(), counter{N: 0})
	require.NoError(t, err)

	_, err = inst.Send(t.Context(), xactInit{})
	require.NoError(t, err)

	require.Equal(t, []string{"foo exit action", "foo transition action"}, executed)
}

// XState: actions on invalid transition > should not recall previous actions
func TestXActInvalidTransitionDoesNotRecallPreviousActions(t *testing.T) {
	t.Parallel()

	m := xactNew(t, "m", func(b *xactBuilder) {
		b.Initial("idle")
		b.State("idle").On[xactStop]().Do("spy", xactDo[xactStop]).To("stop")
		b.State("stop")
	})

	snap, _ := m.Start(counter{N: 0}, epoch)

	snap, effects := m.Transition(snap, xactStop{}, epoch)
	require.Equal(t, []string{"idle/spy"}, names(effects))

	next, effects := m.Transition(snap, xactInvalid{}, epoch)
	require.Empty(t, effects)
	require.Equal(t, snap.Seq(), next.Seq())
}

// XState: actions config > should reference actions defined in actions
// parameter of machine options (entry actions)
//
// The action is declared by name with RequiresAction and given with Bind, as
// XState's provide gives it. XState also enqueues an action that is not
// defined, which sc rejects at declaration.
func TestXActConfigReferencesActionsOfMachineOptionsEntryActions(t *testing.T) {
	t.Parallel()

	chart := func(undefined bool) func(b *xactBuilder) {
		return func(b *xactBuilder) {
			b.RequiresAction("definedAction")
			b.Initial("a")
			b.On[xactE]().To("a")
			b.State("a").On[xactEvent]().To("b")

			entered := b.State("b").OnEntry("definedAction")
			if undefined {
				entered.OnEntry("undefinedAction")
			}
		}
	}

	_, err := sc.New("m", chart(true))
	require.ErrorContains(t, err, `runs chart action "undefinedAction", which is not declared`)

	calls := 0
	m, err := xactNew(t, "m", chart(false)).Bind(func(b *sc.Binder[string, counter]) {
		b.Action("definedAction", func(context.Context, counter, sc.Move[string]) { calls++ })
	})
	require.NoError(t, err)

	snap, effects := m.Start(counter{N: 0}, epoch)
	xactExec(t, m, effects)

	_, effects = m.Transition(snap, xactEvent{}, epoch)
	xactExec(t, m, effects)

	require.Equal(t, 1, calls)
}

// XState: actions config > should reference actions defined in actions
// parameter of machine options (initial state)
func TestXActConfigReferencesActionsOfMachineOptionsInitialState(t *testing.T) {
	t.Parallel()

	calls := 0

	m := xactNew(t, "m", func(b *xactBuilder) {
		b.Action("definedAction", func(context.Context, counter, sc.Move[string]) { calls++ })
		b.Chart().OnEntry("definedAction")
	})

	_, effects := m.Start(counter{N: 0}, epoch)
	xactExec(t, m, effects)

	require.Equal(t, 1, calls)
}

// XState: actions config > should be able to reference action sources from
// action objects
func TestXActConfigReferencesActionSourcesFromActionObjects(t *testing.T) {
	t.Parallel()

	m := xactNew(t, "m", func(b *xactBuilder) {
		b.Action("definedAction", xactNoMove)
		b.Initial("a")
		b.State("a").OnEntry("definedAction").
			On[xactEvent]().
			Run("definedAction").
			Assign("updateContext", func(counter, xactEvent) counter { return counter{N: 10} }).
			To("b")
		b.State("b")
	})

	snap, _ := m.Start(counter{N: 0}, epoch)
	snap, effects := m.Transition(snap, xactEvent{}, epoch)

	require.Equal(t, []string{"a/definedAction"}, names(effects))
	require.Equal(t, counter{N: 10}, snap.Context)
}

// XState: actions config > should work with anonymous functions (with
// warning)
//
// Every sc action has a name; the functions are the anonymous part.
func TestXActConfigWorksWithAnonymousFunctions(t *testing.T) {
	t.Parallel()

	entryCalled, actionCalled, exitCalled := false, false, false

	m := xactNew(t, "anon", func(b *xactBuilder) {
		b.Initial("active")
		b.State("active").
			Entry("entry", func(context.Context, counter) { entryCalled = true }).
			Exit("exit", func(context.Context, counter) { exitCalled = true }).
			On[xactEvent]().
			Do("action", func(context.Context, counter, xactEvent) { actionCalled = true }).
			To("inactive")
		b.State("inactive")
	})

	snap, effects := m.Start(counter{N: 0}, epoch)
	xactExec(t, m, effects)
	require.True(t, entryCalled)

	_, effects = m.Transition(snap, xactEvent{}, epoch)
	xactExec(t, m, effects)
	require.True(t, exitCalled)
	require.True(t, actionCalled)
}

// XState: action meta > should resolve dynamic params using context value
//
// Partial: an sc action has no params. It is handed the context, which is
// where XState's dynamic params take the value from.
func TestXActMetaActionSeesContextValue(t *testing.T) {
	t.Parallel()

	got := 0

	m := xactNew(t, "m", func(b *xactBuilder) {
		b.Chart().Entry("entryAction", func(_ context.Context, c counter) { got = c.N })
	})

	_, effects := m.Start(counter{N: 42}, epoch)
	xactExec(t, m, effects)

	require.Equal(t, 42, got)
}

// XState: action meta > should resolve dynamic params using event value
//
// Partial: an sc action has no params. A transition's action is handed the
// event, which is where XState's dynamic params take the value from.
func TestXActMetaActionSeesEventValue(t *testing.T) {
	t.Parallel()

	got := 0

	m := xactNew(t, "m", func(b *xactBuilder) {
		b.On[xactSecret]().Do("myAction", func(_ context.Context, _ counter, ev xactSecret) { got = ev.Secret })
	})

	snap, _ := m.Start(counter{N: 0}, epoch)
	_, effects := m.Transition(snap, xactSecret{Secret: 77}, epoch)
	require.Equal(t, []string{"m/myAction"}, names(effects))
	xactExec(t, m, effects)
	require.Equal(t, 77, got)

	// The same under a driver.
	got = 0

	inst, err := m.Instance(t.Context(), counter{N: 0})
	require.NoError(t, err)
	defer inst.Close()

	_, err = inst.Send(t.Context(), xactSecret{Secret: 77})
	require.NoError(t, err)
	require.Equal(t, 77, got)
}

// XState: forwardTo() > should forward an event to a service
func TestXActForwardToForwardsEventToService(t *testing.T) {
	t.Parallel()

	synctest.Test(t, func(t *testing.T) {
		child := xactNew(t, "child", func(b *xactBuilder) {
			b.Initial("active")
			b.State("active").On[xactValue]().
				When("is42", func(_ counter, ev xactValue) bool { return ev.Value == 42 }).
				Emit("success", func(counter, xactValue) xactSuccess { return xactSuccess{} })
		})

		parent := xactNew(t, "parent", func(b *xactBuilder) {
			b.Initial("first")
			first := b.State("first")
			first.InvokeMachine("myChild", child, xactSame, xactNoOutput).OnError().Stay()
			first.On[xactValue]().SendTo("forward", "myChild", func(_ counter, ev xactValue) any { return ev })
			first.On[xactSuccess]().To("last")
			b.Final("last")
		})

		inst, err := parent.Instance(t.Context(), counter{N: 0})
		require.NoError(t, err)
		defer inst.Close()

		synctest.Wait()

		_, err = inst.Send(t.Context(), xactValue{Value: 42})
		require.NoError(t, err)

		synctest.Wait()

		require.True(t, inst.Snapshot().Done())
	})
}

// xactSpyRoot is the chart of the enqueueActions cases that run actions on
// entry: the chart runs the chart action spy once for each name given.
func xactSpyRoot(t *testing.T, calls map[string]int, runs ...string) *sc.Machine[string, counter] {
	t.Helper()

	return xactNew(t, "m", func(b *xactBuilder) {
		for _, name := range runs {
			if _, ok := calls[name]; !ok {
				calls[name] = 0
				b.Action(name, func(context.Context, counter, sc.Move[string]) { calls[name]++ })
			}
		}
		b.Chart().OnEntry(runs...)
	})
}

// XState: enqueueActions > should execute a simple referenced action
func TestXActEnqueueExecutesSimpleReferencedAction(t *testing.T) {
	t.Parallel()

	calls := map[string]int{}
	m := xactSpyRoot(t, calls, "spy")

	_, effects := m.Start(counter{N: 0}, epoch)
	xactExec(t, m, effects)

	require.Equal(t, map[string]int{"spy": 1}, calls)
}

// XState: enqueueActions > should execute multiple different referenced
// actions
func TestXActEnqueueExecutesMultipleDifferentReferencedActions(t *testing.T) {
	t.Parallel()

	calls := map[string]int{}
	m := xactSpyRoot(t, calls, "spy1", "spy2")

	_, effects := m.Start(counter{N: 0}, epoch)
	require.Equal(t, []string{"m/spy1", "m/spy2"}, names(effects))
	xactExec(t, m, effects)

	require.Equal(t, map[string]int{"spy1": 1, "spy2": 1}, calls)
}

// XState: enqueueActions > should execute multiple same referenced actions
func TestXActEnqueueExecutesMultipleSameReferencedActions(t *testing.T) {
	t.Parallel()

	calls := map[string]int{}
	m := xactSpyRoot(t, calls, "spy", "spy")

	_, effects := m.Start(counter{N: 0}, epoch)
	require.Equal(t, []string{"m/spy", "m/spy"}, names(effects))
	xactExec(t, m, effects)

	require.Equal(t, map[string]int{"spy": 2}, calls)
}

// XState: enqueueActions > should execute a function
//
// The function is the chart's own entry action, where the "simple referenced
// action" case uses a chart action.
func TestXActEnqueueExecutesFunction(t *testing.T) {
	t.Parallel()

	calls := 0

	m := xactNew(t, "m", func(b *xactBuilder) {
		b.Chart().Entry("spy", func(context.Context, counter) { calls++ })
	})

	_, effects := m.Start(counter{N: 0}, epoch)
	xactExec(t, m, effects)

	require.Equal(t, 1, calls)
}

// xactRaisesRaised is the chart of the two "builtin action" cases, which are
// the same chart in XState too: FOO raises RAISED, whose action is the spy.
func xactRaisesRaised(t *testing.T) *sc.Machine[string, counter] {
	t.Helper()

	return xactNew(t, "m", func(b *xactBuilder) {
		b.On[xactFoo]().Raise("raise", func(counter, xactFoo) any { return xactRaised{} })
		b.On[xactRaised]().Do("spy", xactDo[xactRaised])
	})
}

// XState: enqueueActions > should execute a builtin action using its own
// action creator
func TestXActEnqueueExecutesBuiltinActionUsingOwnActionCreator(t *testing.T) {
	t.Parallel()

	m := xactRaisesRaised(t)
	snap, _ := m.Start(counter{N: 0}, epoch)
	_, effects := m.Transition(snap, xactFoo{}, epoch)

	require.Equal(t, []string{"m/spy"}, names(effects))
}

// XState: enqueueActions > should execute a builtin action using its bound
// action creator
func TestXActEnqueueExecutesBuiltinActionUsingBoundActionCreator(t *testing.T) {
	t.Parallel()

	m := xactRaisesRaised(t)
	snap, _ := m.Start(counter{N: 0}, epoch)
	_, effects := m.Transition(snap, xactFoo{}, epoch)

	require.Equal(t, []string{"m/spy"}, names(effects))
	require.Equal(t, xactRaised{}, effects[0].Event)
}

// XState: enqueueActions > should execute assigns when resolving the initial
// snapshot
func TestXActEnqueueExecutesAssignsWhenResolvingInitialSnapshot(t *testing.T) {
	t.Parallel()

	m := xactNew(t, "m", func(b *xactBuilder) {
		b.Chart().AssignOnEntry("setCount", func(counter) counter { return counter{N: 42} })
	})

	snap, _ := m.Start(counter{N: 0}, epoch)

	require.Equal(t, counter{N: 42}, snap.Context)
}

// XState: enqueueActions > should be able to check a simple referenced guard
//
// Partial: an sc entry action cannot call a guard. The nearest thing is the
// condition of OnEntryIf, which the core checks when the chart is entered.
func TestXActEnqueueChecksConditionOnEntry(t *testing.T) {
	t.Parallel()

	checked := false

	m := xactNew(t, "m", func(b *xactBuilder) {
		b.Action("act", xactNoMove)
		b.Chart().OnEntryIf("act", func(sc.Move[string]) bool {
			checked = true
			return true
		})
	})

	_, effects := m.Start(counter{N: 0}, epoch)

	require.True(t, checked)
	require.Equal(t, []string{"m/act"}, names(effects))
}

// XState: enqueueActions > should enqueue.sendParent
func TestXActEnqueueSendParent(t *testing.T) {
	t.Parallel()

	synctest.Test(t, func(t *testing.T) {
		var calls atomic.Int64

		child := xactNew(t, "child", func(b *xactBuilder) {
			b.Chart().EmitOnEntry("sendParent", func(counter) xactParentEvent { return xactParentEvent{} })
		})

		parent := xactNew(t, "parent", func(b *xactBuilder) {
			chart := b.Chart()
			chart.InvokeMachine("child", child, xactSame, xactNoOutput).OnError().Stay()
			chart.On[xactParentEvent]().Do("parentSpy", func(context.Context, counter, xactParentEvent) { calls.Add(1) })
		})

		inst, err := parent.Instance(t.Context(), counter{N: 0})
		require.NoError(t, err)
		defer inst.Close()

		synctest.Wait()

		require.Equal(t, int64(1), calls.Load())
	})
}

// xactSpy counts the PING events a child received and keeps the last value.
type xactSpy struct {
	calls atomic.Int64
	last  atomic.Int64
}

// xactPingChild is the child machine of the sendTo cases: its action on PING
// is the spy.
func xactPingChild(t *testing.T, spy *xactSpy) *sc.Machine[string, counter] {
	t.Helper()

	return xactNew(t, "child", func(b *xactBuilder) {
		b.On[xactPing]().Do("received", func(_ context.Context, _ counter, ev xactPing) {
			spy.calls.Add(1)
			spy.last.Store(int64(ev.Value))
		})
	})
}

// XState: sendTo > should send to an invoked child by its declared id
func TestXActSendToSendsToInvokedChildByDeclaredID(t *testing.T) {
	t.Parallel()

	synctest.Test(t, func(t *testing.T) {
		spy := new(xactSpy)
		child := xactPingChild(t, spy)

		parent := xactNew(t, "parent", func(b *xactBuilder) {
			chart := b.Chart()
			chart.InvokeMachine("worker", child, xactSame, xactNoOutput).OnError().Stay()
			chart.On[xactSend]().SendTo("ping", "worker", func(counter, xactSend) any { return xactPing{Value: 42} })
		})

		inst, err := parent.Instance(t.Context(), counter{N: 0})
		require.NoError(t, err)
		defer inst.Close()

		synctest.Wait()

		_, err = inst.Send(t.Context(), xactSend{})
		require.NoError(t, err)

		synctest.Wait()

		require.Equal(t, int64(1), spy.calls.Load())
		require.Equal(t, int64(42), spy.last.Load())
	})
}

// xactSpawner is the parent of the two "spawned child" cases: START spawns
// the child under key and, when sendAtOnce is set, sends it PING in the same
// transition; SEND sends PING to the child under key.
func xactSpawner(t *testing.T, child *sc.Machine[string, counter], key string, sendAtOnce bool) *sc.Machine[string, counter] {
	t.Helper()

	return xactNew(t, "parent", func(b *xactBuilder) {
		active := b.Chart()
		active.SpawnMachine("worker", child, func(c counter, _ string) counter { return c }, xactNoOutput).OnError().Stay()

		start := active.On[xactStart]().Spawn("spawn", "worker", func(counter, xactStart) string { return key })
		if sendAtOnce {
			start.SendToKey("ping", "worker",
				func(counter, xactStart) string { return key },
				func(counter, xactStart) any { return xactPing{Value: 0} })
		}

		active.On[xactSend]().SendToKey("ping", "worker",
			func(counter, xactSend) string { return key },
			func(counter, xactSend) any { return xactPing{Value: 0} })
	})
}

// XState: sendTo > should resolve a child spawned earlier in the same
// transition
func TestXActSendToResolvesChildSpawnedEarlierInSameTransition(t *testing.T) {
	t.Parallel()

	synctest.Test(t, func(t *testing.T) {
		spy := new(xactSpy)
		parent := xactSpawner(t, xactPingChild(t, spy), "worker", true)

		inst, err := parent.Instance(t.Context(), counter{N: 0})
		require.NoError(t, err)
		defer inst.Close()

		step, err := inst.Send(t.Context(), xactStart{})
		require.NoError(t, err)
		require.Equal(t, []sc.EffectKind{sc.EffectStartInvocation, sc.EffectSend}, kinds(step.Effects))

		synctest.Wait()

		require.Equal(t, int64(1), spy.calls.Load())
	})
}

// XState: sendTo > should send to a child spawned with an empty-string id
func TestXActSendToSendsToChildSpawnedWithEmptyStringID(t *testing.T) {
	t.Parallel()

	synctest.Test(t, func(t *testing.T) {
		spy := new(xactSpy)
		parent := xactSpawner(t, xactPingChild(t, spy), "", false)

		inst, err := parent.Instance(t.Context(), counter{N: 0})
		require.NoError(t, err)
		defer inst.Close()

		_, err = inst.Send(t.Context(), xactStart{})
		require.NoError(t, err)

		synctest.Wait()

		_, err = inst.Send(t.Context(), xactSend{})
		require.NoError(t, err)

		synctest.Wait()

		require.Equal(t, int64(1), spy.calls.Load())
	})
}

// XState: sendTo > should dead-letter a send to an unknown declared child id
//
// DIVERGES from XState: XState declares the child id only in its schema and
// reports the undelivered event to onRejectedEvent as a 'missingTarget'
// rejection. sc rejects, at declaration, a SendTo that names no service of
// the chart; and for a service that is declared but not running it drops the
// event without telling anyone: the step has no effects and nothing reports
// the drop. Both leave the chart in active without an error.
func TestXActSendToUnknownChildIsDropped(t *testing.T) {
	t.Parallel()

	chart := func(declared bool) func(b *xactBuilder) {
		return func(b *xactBuilder) {
			b.Initial("active")
			b.State("active").On[xactSend]().SendTo("ping", "worker", func(counter, xactSend) any { return xactPing{Value: 0} })
			b.State("failed")
			if declared {
				b.State("working").Requires[struct{}]("worker").OnError().Stay()
			}
		}
	}

	_, err := sc.New("parent", chart(false), sc.AllowUnreachable())
	require.ErrorContains(t, err, `SendTo names "worker", which is not a service of the chart`)

	m := xactNew(t, "parent", chart(true))
	snap, _ := m.Start(counter{N: 0}, epoch)
	snap, effects := m.Transition(snap, xactSend{}, epoch)

	require.Equal(t, []string{"active"}, snap.Active())
	require.Empty(t, effects)
}

// xactSendsOnEntry runs a parent whose chart invokes a child for as long as
// it lives, where XState spawns it while building the context, and sends it
// the event send returns from the chart's entry action. It returns what the
// child's action on the event saw.
func xactSendsOnEntry[Ev any](t *testing.T, start counter, send func(counter) any) (int64, Ev) {
	t.Helper()

	var (
		calls atomic.Int64
		last  atomic.Pointer[Ev]
	)

	synctest.Test(t, func(t *testing.T) {
		child := xactNew(t, "child", func(b *xactBuilder) {
			b.Initial("waiting")
			b.State("waiting").On[Ev]().Do("resolve", func(_ context.Context, _ counter, ev Ev) {
				calls.Add(1)
				last.Store(&ev)
			})
		})

		parent := xactNew(t, "parent", func(b *xactBuilder) {
			chart := b.Chart()
			chart.InvokeMachine("child", child, xactSame, xactNoOutput).OnError().Stay()
			chart.SendOnEntry("send", "child", send)
		})

		_, effects := parent.Start(start, epoch)
		require.Equal(t, []sc.EffectKind{sc.EffectStartInvocation, sc.EffectSend}, kinds(effects))
		require.Equal(t, effects[0].ID, effects[1].ID)

		inst, err := parent.Instance(t.Context(), start)
		require.NoError(t, err)
		defer inst.Close()

		synctest.Wait()
	})

	var ev Ev
	if got := last.Load(); got != nil {
		ev = *got
	}

	return calls.Load(), ev
}

// XState: sendTo > should be able to send an event to an actor
func TestXActSendToSendsEventToActor(t *testing.T) {
	t.Parallel()

	calls, _ := xactSendsOnEntry[xactEvent](t, counter{N: 0}, func(counter) any { return xactEvent{} })

	require.Equal(t, int64(1), calls)
}

// XState: sendTo > should be able to send an event from expression to an
// actor
func TestXActSendToSendsEventFromExpressionToActor(t *testing.T) {
	t.Parallel()

	calls, ev := xactSendsOnEntry[xactCount](t, counter{N: 42}, func(c counter) any { return xactCount{Count: c.N} })

	require.Equal(t, int64(1), calls)
	require.Equal(t, xactCount{Count: 42}, ev)
}

// XState: sendTo > should be able to read from event
//
// Partial: XState picks the actor out of the context by a key the event
// carries. In sc the key of a spawned run does that; the runs are spawned by
// an event, where XState spawns the actor while building the context.
func TestXActSendToReadsTargetFromEvent(t *testing.T) {
	t.Parallel()

	m := xactNew(t, "m", func(b *xactBuilder) {
		b.Initial("a")
		a := b.State("a")
		a.RequiresStream("actor").Spawned().OnError().Stay()
		a.On[xactStart]().
			Spawn("spawnFoo", "actor", func(counter, xactStart) string { return "foo" }).
			Spawn("spawnBar", "actor", func(counter, xactStart) string { return "bar" })
		a.On[xactNamed]().SendToKey("send", "actor",
			func(_ counter, ev xactNamed) string { return ev.Value },
			func(counter, xactNamed) any { return xactEvent{} })
	})

	snap, _ := m.Start(counter{N: 0}, epoch)
	snap, effects := m.Transition(snap, xactStart{}, epoch)
	require.Equal(t, []sc.EffectKind{sc.EffectStartInvocation, sc.EffectStartInvocation}, kinds(effects))

	_, effects = m.Transition(snap, xactNamed{Value: "foo"}, epoch)
	require.Equal(t, []sc.EffectKind{sc.EffectSend}, kinds(effects))
	require.Equal(t, xactEvent{}, effects[0].Event)

	key, spawned := sc.SpawnKey(effects[0].ID)
	require.True(t, spawned)
	require.Equal(t, "foo", key)
}

// xactCounterOne is the chart of the two "read updated snapshot of self"
// cases: entering b sets the counter to one and sends the chart EVENT, whose
// action is the spy.
func xactCounterOne(t *testing.T) *sc.Machine[string, counter] {
	t.Helper()

	return xactNew(t, "m", func(b *xactBuilder) {
		b.Initial("a")
		b.State("a").On[xactNext]().To("b")
		b.State("b").
			AssignOnEntry("setCounter", func(counter) counter { return counter{N: 1} }).
			RaiseOnEntry("event", xactRaise(xactEvent{})).
			On[xactEvent]().Do("spy", xactDo[xactEvent]).To("c")
		b.State("c")
	})
}

// XState: sendTo > a self-event "handler" of an event sent using sendTo
// should be able to read updated snapshot of self
//
// An sc chart sends itself an event by raising it, which is processed in the
// same call where XState's sendTo(self) waits for the step to end. The
// action sees the context of its effect; it has no actor to ask.
func TestXActSendToSelfEventHandlerSeesUpdatedContext(t *testing.T) {
	t.Parallel()

	m := xactCounterOne(t)
	snap, _ := m.Start(counter{N: 0}, epoch)

	snap, effects := m.Transition(snap, xactNext{}, epoch)
	require.Equal(t, []string{"b/spy"}, names(effects))
	require.Equal(t, counter{N: 1}, effects[0].Context)
	require.Equal(t, []string{"c"}, snap.Active())

	next, effects := m.Transition(snap, xactEvent{}, epoch)
	require.Empty(t, effects)
	require.Equal(t, snap.Seq(), next.Seq())
}

// xactDelayedSelf is the chart of the cases that raise a delayed event from
// the entry action of their initial state a. a goes to b on TO_B and, when
// respond is set, on EVENT; b goes to the final state c on EVENT.
func xactDelayedSelf(t *testing.T, delay time.Duration, respond bool) *sc.Machine[string, counter] {
	t.Helper()

	return xactNew(t, "m", func(b *xactBuilder) {
		b.Initial("a")
		a := b.State("a").RaiseAfterOnEntry("event", delay, func(counter) any { return xactEvent{} })
		a.On[xactToB]().To("b")
		if respond {
			a.On[xactEvent]().To("b")
		}
		b.State("b").On[xactEvent]().To("c")
		b.Final("c")
	})
}

// XState: raise > should be able to send a delayed event to itself
func TestXActRaiseSendsDelayedEventToItself(t *testing.T) {
	t.Parallel()

	m := xactDelayedSelf(t, time.Millisecond, false)
	snap, effects := m.Start(counter{N: 0}, epoch)
	require.Equal(t, []string{"a"}, snap.Active())
	require.Equal(t, []sc.EffectKind{sc.EffectStartTimer}, kinds(effects))

	// The delayed event arrives while the chart is in b.
	snap, _ = m.Transition(snap, xactToB{}, epoch)
	require.Equal(t, []string{"b"}, snap.Active())

	snap, _ = m.Advance(snap, epoch.Add(time.Millisecond))
	require.True(t, snap.Done())
}

// XState: raise > should be able to send a delayed event to itself with
// delay = 0
//
// DIVERGES from XState: XState treats a delay of 0 as setTimeout(..., 0), so
// the chart is still in a when start returns and reaches b on the next tick.
// sc fires every timer that is already due before the call returns, so a
// raise delayed by 0 is delivered inside Start and the first snapshot is
// already in b; no timer effect reaches the driver.
func TestXActRaiseSendsDelayedEventToItselfWithZeroDelay(t *testing.T) {
	t.Parallel()

	m := xactDelayedSelf(t, 0, true)
	snap, effects := m.Start(counter{N: 0}, epoch)

	require.Equal(t, []string{"b"}, snap.Active())
	require.Empty(t, effects)
	require.Empty(t, snap.Timers())
}

// XState: raise > should be able to raise an event and respond to it in the
// same state
func TestXActRaiseRaisesEventAndRespondsInSameState(t *testing.T) {
	t.Parallel()

	m := xactNew(t, "m", func(b *xactBuilder) {
		b.Initial("a")
		b.State("a").RaiseOnEntry("toB", xactRaise(xactToB{})).On[xactToB]().To("b")
		b.Final("b")
	})

	snap, _ := m.Start(counter{N: 0}, epoch)

	require.Equal(t, []string{"b"}, snap.Active())
}

// XState: raise > should be able to raise a delayed event and respond to it
// in the same state
func TestXActRaiseRaisesDelayedEventAndRespondsInSameState(t *testing.T) {
	t.Parallel()

	m := xactNew(t, "m", func(b *xactBuilder) {
		b.Initial("a")
		b.State("a").
			RaiseAfterOnEntry("toB", 100*time.Millisecond, func(counter) any { return xactToB{} }).
			On[xactToB]().To("b")
		b.Final("b")
	})

	snap, _ := m.Start(counter{N: 0}, epoch)

	// It has not transitioned yet.
	snap, _ = m.Advance(snap, epoch.Add(50*time.Millisecond))
	require.Equal(t, []string{"a"}, snap.Active())

	snap, _ = m.Advance(snap, epoch.Add(100*time.Millisecond))
	require.Equal(t, []string{"b"}, snap.Active())
	require.True(t, snap.Done())
}

// XState: raise > should accept event expression
func TestXActRaiseAcceptsEventExpression(t *testing.T) {
	t.Parallel()

	m := xactNew(t, "m", func(b *xactBuilder) {
		b.Initial("a")
		a := b.State("a")
		a.On[xactNext]().Raise("raise", func(counter, xactNext) any { return xactRaised{} })
		a.On[xactRaised]().To("b")
		b.State("b")
	})

	snap, _ := m.Start(counter{N: 0}, epoch)
	snap, _ = m.Transition(snap, xactNext{}, epoch)

	require.Equal(t, []string{"b"}, snap.Active())
}

// XState: raise > should be possible to access context in the event
// expression
func TestXActRaiseAccessesContextInEventExpression(t *testing.T) {
	t.Parallel()

	m := xactNew(t, "m", func(b *sc.Builder[string, xactEventType]) {
		b.Initial("a")
		a := b.State("a")
		a.On[xactNext]().Raise("raise", func(c xactEventType, _ xactNext) any {
			if c.EventType == "RAISED" {
				return xactRaised{}
			}

			return xactNext{}
		})
		a.On[xactRaised]().To("b")
		b.State("b")
	})

	snap, _ := m.Start(xactEventType{EventType: "RAISED"}, epoch)
	snap, _ = m.Transition(snap, xactNext{}, epoch)

	require.Equal(t, []string{"b"}, snap.Active())
}

// XState: cancel > should be possible to cancel a raised delayed event
func TestXActCancelCancelsRaisedDelayedEvent(t *testing.T) {
	t.Parallel()

	m := xactNew(t, "m", func(b *xactBuilder) {
		b.Initial("a")
		a := b.State("a")
		a.On[xactNext]().RaiseAfter("myId", time.Millisecond, func(counter, xactNext) any { return xactRaised{} })
		a.On[xactRaised]().To("b")
		a.On[xactCancel]().CancelRaise("myId")
		b.State("b")
	})

	snap, _ := m.Start(counter{N: 0}, epoch)

	snap, effects := m.Transition(snap, xactNext{}, epoch)
	require.Equal(t, []sc.EffectKind{sc.EffectStartTimer}, kinds(effects))

	snap, effects = m.Transition(snap, xactCancel{}, epoch)
	require.Equal(t, []sc.EffectKind{sc.EffectCancelTimer}, kinds(effects))
	require.Empty(t, snap.Timers())

	snap, _ = m.Advance(snap, epoch.Add(10*time.Millisecond))
	require.Equal(t, []string{"a"}, snap.Active())
}

// xactSameIDChild is a child machine of the two "same ID" cases: it raises
// "event" after 100ms under the name sameId, which every such child shares,
// counts the events it receives in spy, and cancels the raise on "cancel".
func xactSameIDChild(t *testing.T, id string, spy *atomic.Int64) *sc.Machine[string, counter] {
	t.Helper()

	return xactNew(t, id, func(b *xactBuilder) {
		run := b.Chart().RaiseAfterOnEntry("sameId", 100*time.Millisecond, func(counter) any { return xactEvent{} })
		run.On[xactEvent]().Do("spy", func(context.Context, counter, xactEvent) { spy.Add(1) })
		run.On[xactCancel]().CancelRaise("sameId")
	})
}

// xactCancelSameID runs a parent whose chart invokes the children foo and bar,
// sends it ev after 50ms, and checks 55ms later how often each child
// received its delayed event.
func xactCancelSameID(t *testing.T, ev any, wantFoo, wantBar int64) {
	t.Helper()

	synctest.Test(t, func(t *testing.T) {
		var fooSpy, barSpy atomic.Int64

		foo := xactSameIDChild(t, "foo", &fooSpy)
		bar := xactSameIDChild(t, "bar", &barSpy)

		parent := xactNew(t, "parent", func(b *xactBuilder) {
			chart := b.Chart()
			chart.InvokeMachine("foo", foo, xactSame, xactNoOutput).OnError().Stay()
			chart.InvokeMachine("bar", bar, xactSame, xactNoOutput).OnError().Stay()
			chart.On[xactCancelFoo]().SendTo("cancel", "foo", func(counter, xactCancelFoo) any { return xactCancel{} })
			chart.On[xactCancelBar]().SendTo("cancel", "bar", func(counter, xactCancelBar) any { return xactCancel{} })
		})

		inst, err := parent.Instance(t.Context(), counter{N: 0})
		require.NoError(t, err)
		defer inst.Close()

		time.Sleep(50 * time.Millisecond) //nolint:forbidigo // GG013: advances the synctest fake clock instantly (the only way to move past a timer inside a synctest.Test bubble); this exemption is valid ONLY within synctest.Test — do not copy it to a real-time time.Sleep

		_, err = inst.Send(t.Context(), ev)
		require.NoError(t, err)

		time.Sleep(55 * time.Millisecond) //nolint:forbidigo // GG013: advances the synctest fake clock instantly (the only way to move past a timer inside a synctest.Test bubble); this exemption is valid ONLY within synctest.Test — do not copy it to a real-time time.Sleep
		synctest.Wait()

		require.Equal(t, wantFoo, fooSpy.Load())
		require.Equal(t, wantBar, barSpy.Load())
	})
}

// XState: cancel > should cancel only the delayed event in the machine that
// scheduled it when canceling the event with the same ID in the machine that
// sent it first
func TestXActCancelOnlyInMachineThatScheduledItSentFirst(t *testing.T) {
	t.Parallel()

	xactCancelSameID(t, xactCancelFoo{}, 0, 1)
}

// XState: cancel > should cancel only the delayed event in the machine that
// scheduled it when canceling the event with the same ID in the machine that
// sent it second
func TestXActCancelOnlyInMachineThatScheduledItSentSecond(t *testing.T) {
	t.Parallel()

	xactCancelSameID(t, xactCancelBar{}, 1, 0)
}

// XState: cancel > should not try to clear an undefined timeout when
// canceling an unscheduled timer
//
// DIVERGES from XState: XState lets a chart cancel an id nothing ever
// schedules. sc rejects, at declaration, a CancelRaise that names no
// RaiseAfter of the chart. For a raise that is declared but not waiting,
// cancelling asks the driver to cancel nothing, as XState expects.
func TestXActCancelUnscheduledTimerCancelsNothing(t *testing.T) {
	t.Parallel()

	chart := func(declared bool) func(b *xactBuilder) {
		return func(b *xactBuilder) {
			b.On[xactFoo]().CancelRaise("foo")
			if declared {
				b.On[xactBar]().RaiseAfter("foo", time.Millisecond, func(counter, xactBar) any { return xactRaised{} })
				b.On[xactRaised]().Stay()
			}
		}
	}

	_, err := sc.New("m", chart(false))
	require.ErrorContains(t, err, `CancelRaise names "foo", which no RaiseAfter declares`)

	m := xactNew(t, "m", chart(true))
	snap, _ := m.Start(counter{N: 0}, epoch)
	_, effects := m.Transition(snap, xactFoo{}, epoch)

	require.Empty(t, effects)
}

// XState: cancel > should not be able to cancel a just scheduled non-delayed
// event to a just invoked child
//
// Partial: an sc send has no id and cannot be cancelled, so only the send
// itself is ported: the event b sends on entry is delivered to the child b
// invokes.
func TestXActCancelSendToJustInvokedChildIsDelivered(t *testing.T) {
	t.Parallel()

	synctest.Test(t, func(t *testing.T) {
		spy := new(xactSpy)
		child := xactPingChild(t, spy)

		m := xactNew(t, "m", func(b *xactBuilder) {
			b.Initial("a")
			b.State("a").On[xactStart]().To("b")
			sb := b.State("b").SendOnEntry("ping", "myChild", func(counter) any { return xactPing{Value: 0} })
			sb.InvokeMachine("myChild", child, xactSame, xactNoOutput).OnError().Stay()
		})

		inst, err := m.Instance(t.Context(), counter{N: 0})
		require.NoError(t, err)
		defer inst.Close()

		step, err := inst.Send(t.Context(), xactStart{})
		require.NoError(t, err)
		require.Equal(t, []string{"b"}, step.To.Active())
		require.Equal(t, []sc.EffectKind{sc.EffectStartInvocation, sc.EffectSend}, kinds(step.Effects))
		require.Equal(t, step.Effects[0].ID, step.Effects[1].ID)

		synctest.Wait()

		require.Equal(t, int64(1), spy.calls.Load())
	})
}

// XState: actions > should call transition actions in document order for
// same-level parallel regions
func TestXActActionsDocumentOrderForSameLevelParallelRegions(t *testing.T) {
	t.Parallel()

	m := xactNew(t, "m", func(b *xactBuilder) {
		b.Regions()
		b.State("a").On[xactFoo]().Do("push", xactDo[xactFoo])
		b.State("b").On[xactFoo]().Do("push", xactDo[xactFoo])
	})

	snap, _ := m.Start(counter{N: 0}, epoch)
	_, effects := m.Transition(snap, xactFoo{}, epoch)

	require.Equal(t, []string{"a/push", "b/push"}, names(effects))
}

// XState: actions > should call transition actions in document order for
// states at different levels of parallel regions
func TestXActActionsDocumentOrderForDifferentLevelsOfParallelRegions(t *testing.T) {
	t.Parallel()

	m := xactNew(t, "m", func(b *xactBuilder) {
		b.Regions()
		b.State("a", func(b *xactBuilder) {
			b.Initial("a1")
			b.State("a1").On[xactFoo]().Do("push", xactDo[xactFoo])
		})
		b.State("b").On[xactFoo]().Do("push", xactDo[xactFoo])
	})

	snap, _ := m.Start(counter{N: 0}, epoch)
	_, effects := m.Transition(snap, xactFoo{}, epoch)

	require.Equal(t, []string{"a1/push", "b/push"}, names(effects))
}

// xactInitialRaise is the chart of the four "initial raise" cases: the chart
// sets the count when assign is set and raises HELLO on entry, and its
// action on HELLO is inline or, when referenced is set, the chart action foo.
func xactInitialRaise(t *testing.T, assign, referenced bool) *sc.Machine[string, counter] {
	t.Helper()

	return xactNew(t, "m", func(b *xactBuilder) {
		b.Action("foo", xactNoMove)

		root := b.Chart()
		if assign {
			root.AssignOnEntry("setCount", func(counter) counter { return counter{N: 42} })
		}
		root.RaiseOnEntry("hello", xactRaise(xactHello{}))

		hello := b.On[xactHello]()
		if referenced {
			hello.Run("foo")
		} else {
			hello.Do("spy", xactDo[xactHello])
		}
	})
}

// XState: actions > should call an inline action responding to an initial
// raise with the raised event
func TestXActActionsInlineActionOnInitialRaiseSeesRaisedEvent(t *testing.T) {
	t.Parallel()

	_, effects := xactInitialRaise(t, false, false).Start(counter{N: 0}, epoch)

	require.Equal(t, []string{"m/spy"}, names(effects))
	require.Equal(t, xactHello{}, effects[0].Event)
}

// XState: actions > should call a referenced action responding to an initial
// raise with the raised event
func TestXActActionsReferencedActionOnInitialRaiseSeesRaisedEvent(t *testing.T) {
	t.Parallel()

	_, effects := xactInitialRaise(t, false, true).Start(counter{N: 0}, epoch)

	require.Equal(t, []string{"m/foo"}, names(effects))
	require.Equal(t, xactHello{}, effects[0].Event)
}

// XState: actions > should call an inline action responding to an initial
// raise with updated (non-initial) context
func TestXActActionsInlineActionOnInitialRaiseSeesUpdatedContext(t *testing.T) {
	t.Parallel()

	_, effects := xactInitialRaise(t, true, false).Start(counter{N: 0}, epoch)

	require.Equal(t, []string{"m/spy"}, names(effects))
	require.Equal(t, counter{N: 42}, effects[0].Context)
}

// XState: actions > should call a referenced action responding to an initial
// raise with updated (non-initial) context
func TestXActActionsReferencedActionOnInitialRaiseSeesUpdatedContext(t *testing.T) {
	t.Parallel()

	_, effects := xactInitialRaise(t, true, true).Start(counter{N: 0}, epoch)

	require.Equal(t, []string{"m/foo"}, names(effects))
	require.Equal(t, counter{N: 42}, effects[0].Context)
}

// XState: actions > a raised event action should be able to read updated
// snapshot of self
//
// The action sees the context of its effect; it has no actor to ask.
func TestXActActionsRaisedEventActionSeesUpdatedContext(t *testing.T) {
	t.Parallel()

	m := xactCounterOne(t)
	snap, _ := m.Start(counter{N: 0}, epoch)

	snap, effects := m.Transition(snap, xactNext{}, epoch)
	require.Equal(t, []string{"b/spy"}, names(effects))
	require.Equal(t, counter{N: 1}, effects[0].Context)

	_, effects = m.Transition(snap, xactEvent{}, epoch)
	require.Empty(t, effects)
}

// XState: enqueued action results > applies a context patch from a named
// action that the transition function returns
//
// Partial: the named action that returns a context patch is an assignment.
// The second half of the case replaces it with provide, and Bind replaces
// only services, resources and chart actions.
func TestXActResultsTransitionAppliesContextPatchOfNamedAction(t *testing.T) {
	t.Parallel()

	m := xactNew(t, "m", func(b *sc.Builder[string, xactCurrency]) {
		b.On[xactPick]().Assign("applyDefaults", func(xactCurrency, xactPick) xactCurrency {
			return xactCurrency{Currency: "EUR"}
		})
	})

	snap, _ := m.Start(xactCurrency{Currency: ""}, epoch)
	snap, _ = m.Transition(snap, xactPick{}, epoch)

	require.Equal(t, xactCurrency{Currency: "EUR"}, snap.Context)
}

// XState cases that are not ported, by reason.
//
// An sc action cannot start a service, and a chart holds no actor
// references: its services are declared on states and addressed by name:
//   - actors spawned in exit handlers of a stopped child should not be started
//   - should forward an event to a service (dynamic)
//   - should be able to communicate with the parent using params
//     (the send itself is ported as "should enqueue.sendParent")
//   - should be able to send an event to a named actor
//     (in sc the same as "should be able to send an event to an actor")
//   - should be able to send an event directly to an ActorRef
//     (in sc the same as "should be able to send an event to an actor")
//
// An sc action must not send events to its own instance, and has no actor
// reference to itself:
//   - should clear all scheduled events when the interpreter gets stopped
//   - should provide self (enqueueActions)
//   - should provide self (action meta)
//
// sc actions take no params, and guards none either:
//   - should provide the original params
//   - should provide the action with resolved params when they are dynamic
//   - should execute a parameterized action
//   - should be able to check a parameterized guard
//   - should call inline transition custom action with undefined parametrized action object
//   - should call a referenced custom action with undefined params when it has no params and it is referenced using a string
//   - should call a referenced custom action with the provided parametrized action object
//
// sc has no log action:
//   - should log a string
//   - should log an expression
//
// An sc send to a service cannot be delayed, has no id, and cannot be
// cancelled:
//   - should not attempt to deliver a delayed event to the spawned actor's ID that was stopped since the event was scheduled
//   - should be able to cancel a just scheduled delayed event to a just invoked child
//
// The case is about TypeScript's types, or about a JavaScript value that Go's
// types rule out (a string as an event, an inherited property as a name):
//   - TS: should compile for any event
//   - should report a type error for an invalid event
//   - should not resolve inherited properties as declared child ids
//   - should error if given a string (sendTo)
//   - should error if given a string (raise)
//
// An sc action returns nothing, so it has no result to ignore or to warn
// about, and sc has no warnings:
//   - ignores a context patch returned by an enqueued action, with a development warning
//   - does not warn when an enqueued function returns another value
//
// Skipped in XState itself (it.skip):
//   - sent events from exit handlers of a stopped child should not be received by the parent
//   - sent events from exit handlers of a done child should be received by its children
//   - should execute exit actions of the settled state of the last initiated microstep
//   - should not cause an infinite loop when forwarding to undefined
//   - should not attempt to deliver a delayed event to the invoked actor's ID that was stopped since the event was scheduled
