package sc_test

// Ports of XState's packages/core/test/final.test.ts (6.0.0-beta.2), to check
// sc's semantics of final states against XState's. Each test names the XState
// case it ports. Cases that test an XState-only API are listed at the end of
// the file with the reason they were not ported.
//
// Three differences in what sc can declare shape every port:
//
//   - The root of an sc chart is always a compound state, so an XState
//     machine whose root is parallel is declared as one top-level parallel
//     state whose OnDone leads to a top-level final state.
//   - State identifiers are unique in an sc chart, so XState's repeated
//     local names ("start", "finish") are qualified by their region.
//   - sc rejects a state nothing can reach, so a state XState leaves
//     unreachable is given a transition that reaches it.

import (
	"context"
	"encoding/json"
	"testing"
	"testing/synctest"

	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/sc"
)

type (
	xfinNext1         struct{}
	xfinNext2         struct{}
	xfinNext3         struct{}
	xfinNext4         struct{}
	xfinPass          struct{}
	xfinFail          struct{}
	xfinCancel        struct{}
	xfinChildCanceled struct{}
)

type xfinBuilder = sc.Builder[string, counter]

// xfinSpy is an action of a completion transition whose effect a test looks
// for.
func xfinSpy(context.Context, counter, struct{}) {}

// xfinActions returns the actions among effects, as "state/name".
func xfinActions(effects []sc.Effect[counter]) []string {
	out := make([]string, 0, len(effects))
	for _, e := range effects {
		if e.Kind == sc.EffectAction {
			out = append(out, e.State+"/"+e.Name)
		}
	}

	return out
}

// xfinCount reports how many times name is in actions.
func xfinCount(actions []string, name string) int {
	n := 0
	for _, a := range actions {
		if a == name {
			n++
		}
	}

	return n
}

// xfinSend processes events in order and returns the last snapshot with the
// actions of every step.
func xfinSend(m *sc.Machine[string, counter], snap sc.Snapshot[string, counter], events ...any) (sc.Snapshot[string, counter], []string) {
	var actions []string
	for _, ev := range events {
		var effects []sc.Effect[counter]
		snap, effects = m.Transition(snap, ev, epoch)
		actions = append(actions, xfinActions(effects)...)
	}

	return snap, actions
}

// xfinRegion declares a compound region name whose state name.start moves to
// the final state name.finish on Ev.
func xfinRegion[Ev any](b *xfinBuilder, name string) {
	b.State(name, func(b *xfinBuilder) {
		b.Initial(name + ".start")
		b.State(name + ".start").On[Ev]().To(name + ".finish")
		b.Final(name + ".finish")
	})
}

// xfinFinalRegion declares a compound region name that starts in its final
// state. It stands for an XState region that is itself a final state, which
// sc does not allow directly inside a parallel state.
func xfinFinalRegion(b *xfinBuilder, name string) {
	b.State(name, func(b *xfinBuilder) {
		b.Initial(name + ".final")
		b.Final(name + ".final")
	})
}

// XState: should emit the done state event when all nested states are final
func TestXFinEmitsDoneStateEventWhenAllNestedStatesAreFinal(t *testing.T) {
	t.Parallel()

	m, err := sc.New("m", func(b *xfinBuilder) {
		b.Initial("foo")
		b.Parallel("foo", func(b *xfinBuilder) {
			xfinRegion[xfinNext1](b, "first")
			xfinRegion[xfinNext2](b, "second")
		}).OnDone().Do("onDoneSpy", xfinSpy).To("bar")
		b.State("bar")
	})
	require.NoError(t, err)

	snap, _ := m.Start(counter{N: 0}, epoch)

	snap, actions := xfinSend(m, snap, xfinNext1{})
	require.True(t, snap.In("foo"))
	require.Empty(t, actions)

	snap, actions = xfinSend(m, snap, xfinNext2{})
	require.Equal(t, []string{"bar"}, snap.Active())
	require.Equal(t, []string{"foo/onDoneSpy"}, actions)
}

// XState: should execute final child state actions first
func TestXFinExecutesFinalChildStateActionsFirst(t *testing.T) {
	t.Parallel()

	m, err := sc.New("m", func(b *xfinBuilder) {
		b.Initial("foo")
		b.State("foo", func(b *xfinBuilder) {
			b.Initial("bar")
			b.State("bar", func(b *xfinBuilder) {
				b.Initial("baz")
				b.Final("baz").Entry("bazAction", noop)
			}).OnDone().To("barFinal")
			b.Final("barFinal").Entry("barAction", noop)
		}).OnDone().Do("fooAction", xfinSpy)
	})
	require.NoError(t, err)

	_, effects := m.Start(counter{N: 0}, epoch)

	require.Equal(t, []string{"baz/bazAction", "barFinal/barAction", "foo/fooAction"}, xfinActions(effects))
}

// XState: should call output expressions on nested final nodes
//
// Partial port: sc's final states have no output and OnDone carries no
// payload, so the secret cannot travel from the final state to the context.
// What is ported is that reaching the nested final state completes its
// parent, whose OnDone leads to the top-level final state.
func TestXFinNestedFinalNodeCompletesItsParent(t *testing.T) {
	t.Parallel()

	m, err := sc.New("m", func(b *xfinBuilder) {
		b.Initial("secret")
		b.State("secret", func(b *xfinBuilder) {
			b.Initial("wait")
			b.State("wait").On[xfinNext1]().To("reveal")
			b.Final("reveal")
		}).OnDone().To("success")
		b.Final("success")
	})
	require.NoError(t, err)

	snap, _ := m.Start(counter{N: 0}, epoch)
	require.False(t, snap.Done())

	snap, _ = xfinSend(m, snap, xfinNext1{})
	require.Equal(t, []string{"success"}, snap.Active())
	require.True(t, snap.Done())
}

// xfinStartEndChart is the chart most of XState's output cases use: start
// moves to the top-level final state end on an event.
func xfinStartEndChart(t *testing.T) *sc.Machine[string, counter] {
	t.Helper()

	m, err := sc.New("m", func(b *xfinBuilder) {
		b.Initial("start")
		b.State("start").On[xfinNext1]().To("end")
		b.Final("end")
	})
	require.NoError(t, err)

	return m
}

// XState: should use the reached top-level final state output
//
// Partial port: the output value is not expressible; which of the two
// top-level final states was reached is.
func TestXFinReachesTheTargetedTopLevelFinalState(t *testing.T) {
	t.Parallel()

	m, err := sc.New("m", func(b *xfinBuilder) {
		b.Initial("start")
		start := b.State("start")
		start.On[xfinPass]().To("passed")
		start.On[xfinFail]().To("failed")
		b.Final("passed")
		b.Final("failed")
	})
	require.NoError(t, err)

	snap, _ := m.Start(counter{N: 0}, epoch)
	snap, _ = xfinSend(m, snap, xfinFail{})

	require.Equal(t, []string{"failed"}, snap.Active())
	require.True(t, snap.Done())
}

// XState: should populate top-level final state output through pure transition
//
// Partial port: the output value is not expressible; that the pure
// transition function reports the machine done is.
func TestXFinPureTransitionReportsTheMachineDone(t *testing.T) {
	t.Parallel()

	m := xfinStartEndChart(t)

	snap, _ := m.Start(counter{N: 0}, epoch)
	require.False(t, snap.Done())

	next, _ := m.Transition(snap, xfinNext1{}, epoch)
	require.Equal(t, []string{"end"}, next.Active())
	require.True(t, next.Done())
}

// XState: should populate top-level final state output through pure initialTransition
//
// Partial port: the output value is not expressible; that a machine whose
// initial state is final is done from the start is.
func TestXFinPureStartReportsTheMachineDone(t *testing.T) {
	t.Parallel()

	m, err := sc.New("m", func(b *xfinBuilder) {
		b.Initial("end")
		b.Final("end")
	})
	require.NoError(t, err)

	snap, _ := m.Start(counter{N: 0}, epoch)

	require.Equal(t, []string{"end"}, snap.Active())
	require.True(t, snap.Done())
}

// XState: should persist and restore top-level final state output
//
// Partial port: the output value is not expressible; that a done machine is
// still done after being persisted and restored is.
func TestXFinPersistsAndRestoresADoneMachine(t *testing.T) {
	t.Parallel()

	m := xfinStartEndChart(t)

	snap, _ := m.Start(counter{N: 0}, epoch)
	snap, _ = xfinSend(m, snap, xfinNext1{})
	require.True(t, snap.Done())

	data, err := json.Marshal(snap)
	require.NoError(t, err)

	restored, effects, err := m.Restore(data, epoch)
	require.NoError(t, err)
	require.Empty(t, effects)
	require.Equal(t, []string{"end"}, restored.Active())
	require.True(t, restored.Done())
}

// xfinNestedParallelChart is a parallel state whose two regions are
// themselves parallel states with two compound regions each.
func xfinNestedParallelChart(t *testing.T) *sc.Machine[string, counter] {
	t.Helper()

	m, err := sc.New("m", func(b *xfinBuilder) {
		b.Initial("first")
		b.Parallel("first", func(b *xfinBuilder) {
			b.Parallel("alpha", func(b *xfinBuilder) {
				xfinRegion[xfinNext1](b, "one")
				xfinRegion[xfinNext2](b, "two")
			})
			b.Parallel("beta", func(b *xfinBuilder) {
				xfinRegion[xfinNext3](b, "third")
				xfinRegion[xfinNext4](b, "fourth")
			})
		}).OnDone().To("done")
		b.Final("done")
	})
	require.NoError(t, err)

	return m
}

// XState: should emit a done state event for a parallel state when its parallel children reach their final states
func TestXFinParallelStateCompletesWhenItsParallelChildrenReachTheirFinalStates(t *testing.T) {
	t.Parallel()

	m := xfinNestedParallelChart(t)

	snap, _ := m.Start(counter{N: 0}, epoch)
	snap, _ = xfinSend(m, snap, xfinNext1{}, xfinNext2{}, xfinNext3{}, xfinNext4{})

	require.True(t, snap.Done())
	require.Equal(t, []string{"done"}, snap.Active())
}

// xfinMixedParallelChart is a parallel state with one region that is a
// parallel state and one that is a compound state.
func xfinMixedParallelChart(t *testing.T) *sc.Machine[string, counter] {
	t.Helper()

	m, err := sc.New("m", func(b *xfinBuilder) {
		b.Initial("first")
		b.Parallel("first", func(b *xfinBuilder) {
			b.Parallel("alpha", func(b *xfinBuilder) {
				xfinRegion[xfinNext1](b, "one")
				xfinRegion[xfinNext2](b, "two")
			})
			xfinRegion[xfinNext3](b, "beta")
		}).OnDone().To("done")
		b.Final("done")
	})
	require.NoError(t, err)

	return m
}

// XState: should emit a done state event for a parallel state when its compound child reaches its final state when the other parallel child region is already in its final state
func TestXFinParallelStateCompletesWhenCompoundChildFinishesAfterParallelChild(t *testing.T) {
	t.Parallel()

	m := xfinMixedParallelChart(t)

	snap, _ := m.Start(counter{N: 0}, epoch)
	snap, _ = xfinSend(m, snap, xfinNext1{}, xfinNext2{}, xfinNext3{})

	require.True(t, snap.Done())
	require.Equal(t, []string{"done"}, snap.Active())
}

// XState: should emit a done state event for a parallel state when its parallel child reaches its final state when the other compound child region is already in its final state
func TestXFinParallelStateCompletesWhenParallelChildFinishesAfterCompoundChild(t *testing.T) {
	t.Parallel()

	m := xfinMixedParallelChart(t)

	snap, _ := m.Start(counter{N: 0}, epoch)
	snap, _ = xfinSend(m, snap, xfinNext3{}, xfinNext1{}, xfinNext2{})

	require.True(t, snap.Done())
	require.Equal(t, []string{"done"}, snap.Active())
}

// xfinImmediatelyFinalChart is XState's chart of a parallel state a whose
// regions are all final from the start: a1, a parallel state of two final
// regions, and a2, a compound state starting in a final state. direct
// declares a1's regions as final states directly inside the parallel state,
// as XState does; otherwise each is a compound region starting in a final
// state.
func xfinImmediatelyFinalChart(direct bool) func(*xfinBuilder) {
	return func(b *xfinBuilder) {
		b.Initial("a")
		b.Parallel("a", func(b *xfinBuilder) {
			b.Parallel("a1", func(b *xfinBuilder) {
				if direct {
					b.Final("a1a")
					b.Final("a1b")
				} else {
					xfinFinalRegion(b, "a1a")
					xfinFinalRegion(b, "a1b")
				}
			})
			b.State("a2", func(b *xfinBuilder) {
				b.Initial("a2a")
				b.Final("a2a")
			})
		}).OnDone().To("b")
		b.Final("b")
	}
}

// XState: should reach a final state when a parallel state reaches its final state and transitions to a top-level final state in response to that
//
// DIVERGES from XState: XState allows a final state directly inside a
// parallel state, as a region that is final from the start. sc rejects that
// as a declaration error, so each such region is declared as a compound
// state that starts in a final state.
func TestXFinReachesFinalStateWhenParallelStateCompletesAtStart(t *testing.T) {
	t.Parallel()

	_, err := sc.New("m", xfinImmediatelyFinalChart(true))
	require.ErrorContains(t, err, `final state "a1a": declared directly inside parallel state "a1"`)
	require.ErrorContains(t, err, `final state "a1b": declared directly inside parallel state "a1"`)

	m, err := sc.New("m", xfinImmediatelyFinalChart(false))
	require.NoError(t, err)

	snap, _ := m.Start(counter{N: 0}, epoch)

	require.True(t, snap.Done())
	require.Equal(t, []string{"b"}, snap.Active())
}

// XState: should reach a final state when a parallel state nested in a parallel state reaches its final state and transitions to a top-level final state in response to that
//
// The XState case declares the same machine as the one before it, and the
// port declares its final regions the same way.
func TestXFinReachesFinalStateWhenNestedParallelStateCompletesAtStart(t *testing.T) {
	t.Parallel()

	m, err := sc.New("m", xfinImmediatelyFinalChart(false))
	require.NoError(t, err)

	snap, _ := m.Start(counter{N: 0}, epoch)

	require.True(t, snap.Done())
	require.Equal(t, []string{"b"}, snap.Active())
}

// XState: onDone of an outer parallel state should be called with its own "xstate.done.state.*" event when its direct parallel child completes
//
// Partial port: the done event and its output are not expressible; that the
// outer parallel state's OnDone is taken once is.
func TestXFinOuterParallelOnDoneIsTakenWhenItsDirectParallelChildCompletes(t *testing.T) {
	t.Parallel()

	m, err := sc.New("m", func(b *xfinBuilder) {
		b.Initial("a")
		b.Parallel("a", func(b *xfinBuilder) {
			b.Parallel("b", func(b *xfinBuilder) {
				b.State("c", func(b *xfinBuilder) {
					b.Initial("d")
					b.Final("d")
				})
			})
		}).OnDone().Do("spy", xfinSpy)
	})
	require.NoError(t, err)

	snap, effects := m.Start(counter{N: 0}, epoch)

	require.Equal(t, 1, xfinCount(xfinActions(effects), "a/spy"))
	require.Equal(t, []string{"a", "b", "c", "d"}, snap.Active())
}

// XState: machine should not complete when a parallel child of a compound state completes
func TestXFinMachineDoesNotCompleteWhenAParallelChildOfACompoundStateCompletes(t *testing.T) {
	t.Parallel()

	m, err := sc.New("m", func(b *xfinBuilder) {
		b.Initial("a")
		b.Parallel("a", func(b *xfinBuilder) {
			b.State("b", func(b *xfinBuilder) {
				b.Initial("c")
				b.Final("c")
			})
		})
	})
	require.NoError(t, err)

	snap, _ := m.Start(counter{N: 0}, epoch)

	require.False(t, snap.Done())
	require.Equal(t, []string{"a", "b", "c"}, snap.Active())
}

// XState: onDone of a parallel state should only be called once when multiple parallel regions complete at once
func TestXFinParallelOnDoneIsTakenOnceWhenSeveralRegionsCompleteAtOnce(t *testing.T) {
	t.Parallel()

	m, err := sc.New("m", func(b *xfinBuilder) {
		b.Initial("a")
		b.Parallel("a", func(b *xfinBuilder) {
			xfinFinalRegion(b, "b")
			xfinFinalRegion(b, "c")
		}).OnDone().Do("spy", xfinSpy)
	})
	require.NoError(t, err)

	_, effects := m.Start(counter{N: 0}, epoch)

	require.Equal(t, 1, xfinCount(xfinActions(effects), "a/spy"))
}

// XState: should call exit actions in reversed document order when the machines reaches its final state
//
// Partial port: XState exits every active state, the root last, when the
// machine completes, and expects "exit: b" and "exit: __root__" after the
// transition. sc has no exit actions on a final state or on the root, so only
// the transition's own actions can be observed.
func TestXFinActionsWhenTheMachineReachesItsFinalState(t *testing.T) {
	t.Parallel()

	m, err := sc.New("m", func(b *xfinBuilder) {
		b.Initial("a")
		b.State("a").Entry("enter", noop).Exit("exit", noop).On[xfinNext1]().To("b")
		b.Final("b").Entry("enter", noop)
	})
	require.NoError(t, err)

	snap, _ := m.Start(counter{N: 0}, epoch)
	snap, actions := xfinSend(m, snap, xfinNext1{})

	require.True(t, snap.Done())
	require.Equal(t, []string{"a/exit", "b/enter"}, actions)
}

// xfinTrackedRegionsChart stands for XState's parallel root with regions a
// and b, each moving from its first child to a final one. Every state
// records its entry and, where sc allows one, its exit. wire declares the
// transitions out of child_a1 and child_b1.
func xfinTrackedRegionsChart(t *testing.T, wire func(a1, b1 *sc.StateBuilder[string, counter])) *sc.Machine[string, counter] {
	t.Helper()

	m, err := sc.New("m", func(b *xfinBuilder) {
		b.Initial("root")
		b.Parallel("root", func(b *xfinBuilder) {
			var a1, b1 *sc.StateBuilder[string, counter]

			b.State("a", func(b *xfinBuilder) {
				b.Initial("child_a1")
				a1 = b.State("child_a1").Entry("enter", noop).Exit("exit", noop)
				b.Final("child_a2").Entry("enter", noop)
			}).Entry("enter", noop).Exit("exit", noop)
			b.State("b", func(b *xfinBuilder) {
				b.Initial("child_b1")
				b1 = b.State("child_b1").Entry("enter", noop).Exit("exit", noop)
				b.Final("child_b2").Entry("enter", noop)
			}).Entry("enter", noop).Exit("exit", noop)

			wire(a1, b1)
		}).Entry("enter", noop).Exit("exit", noop).OnDone().To("end")
		b.Final("end").Entry("enter", noop)
	})
	require.NoError(t, err)

	return m
}

// xfinWireSeparateEvents moves region a on xfinNext2 and region b on
// xfinNext1, as XState's EV2 and EV1 do.
func xfinWireSeparateEvents(a1, b1 *sc.StateBuilder[string, counter]) {
	a1.On[xfinNext2]().To("child_a2")
	b1.On[xfinNext1]().To("child_b2")
}

// XState: should call exit actions of parallel states in reversed document order when the machines reaches its final state after earlier region transition
//
// Partial port: XState's root is parallel and the exits happen because the
// machine completes. Here they happen because the parallel state "root"
// completes and its OnDone leaves it for the top-level final state. XState
// expects, after the transition, "exit: b.child_b2", "exit: b",
// "exit: a.child_a2", "exit: a", "exit: __root__"; sc's final states have no
// exit actions, so those of child_b2 and child_a2 cannot be observed.
func TestXFinExitOrderOfParallelStatesAfterEarlierRegionTransition(t *testing.T) {
	t.Parallel()

	m := xfinTrackedRegionsChart(t, xfinWireSeparateEvents)

	snap, _ := m.Start(counter{N: 0}, epoch)
	snap, _ = xfinSend(m, snap, xfinNext1{})
	snap, actions := xfinSend(m, snap, xfinNext2{})

	require.True(t, snap.Done())
	require.Equal(t, []string{
		// result of the transition
		"child_a1/exit",
		"child_a2/enter",
		// result of reaching final states
		"b/exit",
		"a/exit",
		"root/exit",
		"end/enter",
	}, actions)
}

// XState: should call exit actions of parallel states in reversed document order when the machines reaches its final state after later region transition
//
// The XState case declares the same machine and sends the same events as the
// one before it; see there for what is ported.
func TestXFinExitOrderOfParallelStatesAfterLaterRegionTransition(t *testing.T) {
	t.Parallel()

	m := xfinTrackedRegionsChart(t, xfinWireSeparateEvents)

	snap, _ := m.Start(counter{N: 0}, epoch)
	snap, _ = xfinSend(m, snap, xfinNext1{})
	snap, actions := xfinSend(m, snap, xfinNext2{})

	require.True(t, snap.Done())
	require.Equal(t, []string{
		"child_a1/exit",
		"child_a2/enter",
		"b/exit",
		"a/exit",
		"root/exit",
		"end/enter",
	}, actions)
}

// XState: should call exit actions of parallel states in reversed document order when the machines reaches its final state after multiple regions transition
//
// Partial port, as the two cases before it.
func TestXFinExitOrderOfParallelStatesAfterMultipleRegionsTransition(t *testing.T) {
	t.Parallel()

	m := xfinTrackedRegionsChart(t, func(a1, b1 *sc.StateBuilder[string, counter]) {
		a1.On[xfinNext1]().To("child_a2")
		b1.On[xfinNext1]().To("child_b2")
	})

	snap, _ := m.Start(counter{N: 0}, epoch)
	snap, actions := xfinSend(m, snap, xfinNext1{})

	require.True(t, snap.Done())
	require.Equal(t, []string{
		// result of the transition
		"child_b1/exit",
		"child_a1/exit",
		"child_a2/enter",
		"child_b2/enter",
		// result of reaching final states
		"b/exit",
		"a/exit",
		"root/exit",
		"end/enter",
	}, actions)
}

// xfinPartlyFinalChart stands for XState's parallel root with region A in
// its final state from the start and region B not. XState leaves B2
// unreachable, which sc rejects, so B1 moves to it on an event.
func xfinPartlyFinalChart(direct bool) func(*xfinBuilder) {
	return func(b *xfinBuilder) {
		b.Initial("root")
		b.Parallel("root", func(b *xfinBuilder) {
			if direct {
				b.Final("A")
			} else {
				b.State("A", func(b *xfinBuilder) {
					b.Initial("A1")
					b.Final("A1")
				})
			}
			b.State("B", func(b *xfinBuilder) {
				b.Initial("B1")
				b.State("B1").On[xfinNext1]().To("B2")
				b.Final("B2")
			})
		}).OnDone().To("end")
		b.Final("end")
	}
}

// XState: should not complete a parallel root immediately when only some of its regions are in their final states (final state reached in a compound region)
func TestXFinParallelStateDoesNotCompleteWhenOnlySomeRegionsAreFinalCompoundRegion(t *testing.T) {
	t.Parallel()

	m, err := sc.New("m", xfinPartlyFinalChart(false))
	require.NoError(t, err)

	snap, _ := m.Start(counter{N: 0}, epoch)

	require.False(t, snap.Done())
	require.Equal(t, []string{"root", "A", "A1", "B", "B1"}, snap.Active())
}

// XState: should not complete a parallel root immediately when only some of its regions are in their final states (a direct final child state reached)
//
// DIVERGES from XState: XState allows a final state directly inside a
// parallel state, as a region that is final from the start. sc rejects that
// as a declaration error, so each such region is declared as a compound
// state that starts in a final state.
// Declared that way the chart is the one of the case before this one, and
// behaves as XState expects.
func TestXFinParallelStateDoesNotCompleteWhenOnlySomeRegionsAreFinalDirectFinalChild(t *testing.T) {
	t.Parallel()

	_, err := sc.New("m", xfinPartlyFinalChart(true))
	require.ErrorContains(t, err, `final state "A": declared directly inside parallel state "root"`)

	m, err := sc.New("m", xfinPartlyFinalChart(false))
	require.NoError(t, err)

	snap, _ := m.Start(counter{N: 0}, epoch)

	require.False(t, snap.Done())
	require.Equal(t, []string{"root", "A", "A1", "B", "B1"}, snap.Active())
}

// XState: should deliver final outgoing events (from final entry action) to the parent before delivering the `xstate.done.actor.*` event
//
// Two things are translated. An sc entry action cannot send, so the child
// emits from the transition that enters its final state. And XState sends
// CANCEL to the child's actor directly, where the sc parent forwards it.
func TestXFinDeliversFinalOutgoingEventsToTheParentBeforeTheDoneEvent(t *testing.T) {
	t.Parallel()

	synctest.Test(t, func(t *testing.T) {
		child, err := sc.New("child", func(b *xfinBuilder) {
			b.Initial("start")
			b.State("start").On[xfinCancel]().
				Emit("notify", func(counter, xfinCancel) xfinChildCanceled { return xfinChildCanceled{} }).
				To("canceled")
			b.Final("canceled")
		})
		require.NoError(t, err)

		parent, err := sc.New("parent", func(b *xfinBuilder) {
			b.Initial("start")
			start := b.State("start")
			invoked := start.InvokeMachine("child", child,
				func(c counter) counter { return c },
				func(sc.Snapshot[string, counter]) (struct{}, error) { return struct{}{}, nil },
			)
			invoked.OnDone().To("completed")
			invoked.OnError().To("failed")
			start.On[xfinChildCanceled]().To("canceled")
			start.On[xfinCancel]().SendTo("forward", "child", func(_ counter, ev xfinCancel) any { return ev })
			b.State("canceled")
			b.State("completed")
			b.State("failed")
		})
		require.NoError(t, err)

		inst, err := parent.Instance(t.Context(), counter{N: 0})
		require.NoError(t, err)
		defer inst.Close()

		synctest.Wait()

		_, err = inst.Send(t.Context(), xfinCancel{})
		require.NoError(t, err)

		synctest.Wait()

		// If the child's completion were delivered first, the parent
		// would be in completed.
		require.Equal(t, []string{"canceled"}, inst.Snapshot().Active())
	})
}

// XState: does not warn for plain final states
//
// sc has no warnings; a chart it objects to is a declaration error. The port
// declares XState's chart and checks whether sc accepts it.
//
// DIVERGES from XState: XState accepts the chart without complaint although
// nothing reaches its top-level final state "done". sc rejects a state
// nothing can reach, so the chart as XState declares it is an error unless
// it is declared with the AllowUnreachable option.
func TestXFinPlainFinalStatesAreAccepted(t *testing.T) {
	t.Parallel()

	chart := func(b *xfinBuilder) {
		b.Initial("a")
		b.State("a", func(b *xfinBuilder) {
			b.Initial("inner")
			b.Final("inner")
		})
		b.Final("done")
	}

	_, err := sc.New("m", chart)
	require.ErrorContains(t, err, `state "done": unreachable`)

	m, err := sc.New("m", chart, sc.AllowUnreachable())
	require.NoError(t, err)

	snap, _ := m.Start(counter{N: 0}, epoch)
	require.Equal(t, []string{"a", "inner"}, snap.Active())
	require.False(t, snap.Done())
}

// XState cases that are not ported, by reason.
//
// The root of an sc chart cannot be a final state:
//   - status of a machine with a root state being final should be done
//   - output of a machine with a root state being final should receive its state ID
//
// sc has no output on final states or on the machine, and a state's
// completion carries no payload:
//   - should only call data expression once when entering root's final state
//   - should use top-level final state output as machine output without root output
//   - should pass top-level final state output to root output mapper
//   - should keep root output-only behavior unchanged
//   - should leave machine output undefined without final or root output
//   - should resolve top-level final state output once on completion
//   - output mapper should receive self
//   - state output should be able to use context updated by the entry action of the reached final state
//     (an sc entry action cannot change the context either)
//   - root output should be called with a "xstate.done.state.*" event of the parallel root when a direct final child of that parallel root is reached
//   - root output should be called with a "xstate.done.state.*" event of the parallel root when a final child of its compound child is reached
//   - root output should be called with a "xstate.done.state.*" event of the parallel root when a final descendant is reached 2 parallel levels deep
//   - root output should only be called once when multiple parallel regions complete at once
//   - should require root output to produce output for a parallel root
//   - should not resolve output of a final state if its parent is a parallel state
//   - should be possible to complete with a null output (directly on root)
//   - should be possible to complete with a null output (resolving with final state's output)
//
// The root of an sc chart cannot be parallel and has no OnDone, so a machine
// cannot complete in the step that completes its inner states:
//   - onDone should not be called when the machine reaches its final state
//
// The root of an sc chart has no exit actions, and sc runs no exit actions
// when a machine stops:
//   - should only call exit actions once when a child machine reaches its final state and sends an event to its parent that ends up stopping that child
//   - should deliver final outgoing events (from root exit action) to the parent before delivering the `xstate.done.actor.*` event
//
// An sc final state can declare only tags and entry actions, so it cannot
// have the transitions or services these cases are about, and sc has no
// warnings:
//   - warns when a top-level final state declares invoke, on or after
//   - does not start actors invoked by a top-level final state
//   - warns when a nested or parallel-region final state declares on
//   - final regions under a parallel state take no transitions and start no actors
