package sc_test

// This file ports XState's packages/core/test/history.test.ts (6.0.0-beta.2)
// to check sc's history semantics against XState's. Each test names the
// XState case it ports. A comment starting "DIVERGES from XState:" marks a
// place where sc behaves differently and the test asserts what sc does.
//
// Two things are translated the same way throughout:
//
//   - sc rejects a chart with a state no transition can reach unless it is
//     built with sc.AllowUnreachable, and several of XState's charts have
//     one, so every chart here is built with that option.
//   - sc state identifiers are unique across the chart, so XState's nested
//     keys ("on.hist", "A.hist") become distinct identifiers.
//
// Cases that are not ported, and why:
//
//   - "should enter a legal multi-target default for deep parallel history":
//     a history state's Default takes one target, so a default naming a state
//     in each region cannot be declared.
//   - "should not execute actions of the initial transition when a history
//     state with a default target is targeted and its parent state was never
//     visited yet", "should execute parent entry actions when recorded
//     history is restored", "should not execute actions of the initial
//     transition when a history state with a default target is targeted and
//     its parent state was already visited": it.skip in XState itself. They
//     are about actions on an initial transition, which sc does not have.

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/sc"
)

// xhistCtx is the context of the charts in this file. None of them uses it.
type xhistCtx struct{}

type (
	xhistSwitch    struct{}
	xhistPower     struct{}
	xhistNext      struct{}
	xhistDeploy    struct{}
	xhistSuccess   struct{}
	xhistFailure   struct{}
	xhistDestroy   struct{}
	xhistReenter   struct{}
	xhistPing      struct{}
	xhistClick     struct{}
	xhistClose     struct{}
	xhistInner     struct{}
	xhistInnerA    struct{}
	xhistInnerK    struct{}
	xhistDeepPower struct{}
	xhistGo        struct{}

	xhistParallelHistory     struct{}
	xhistParallelSomeHistory struct{}
	xhistParallelDeepHistory struct{}
)

type (
	xhistBuilder = sc.Builder[string, xhistCtx]
	xhistMachine = sc.Machine[string, xhistCtx]
	xhistSnap    = sc.Snapshot[string, xhistCtx]
)

func xhistNoop(context.Context, xhistCtx) {}

func xhistNew(t *testing.T, def func(b *xhistBuilder)) *xhistMachine {
	t.Helper()

	m, err := sc.New("xhist", def, sc.AllowUnreachable())
	require.NoError(t, err)

	return m
}

// xhistSend sends events one after another and returns the last snapshot and
// the effects of the last event.
func xhistSend(m *xhistMachine, snap xhistSnap, events ...any) (xhistSnap, []sc.Effect[xhistCtx]) {
	var effects []sc.Effect[xhistCtx]
	for _, ev := range events {
		snap, effects = m.Transition(snap, ev, epoch)
	}

	return snap, effects
}

// xhistRun starts the machine and sends it events.
func xhistRun(m *xhistMachine, events ...any) (xhistSnap, []sc.Effect[xhistCtx]) {
	snap, effects := m.Start(xhistCtx{}, epoch)
	if len(events) == 0 {
		return snap, effects
	}

	return xhistSend(m, snap, events...)
}

// xhistTrack gives every named state an "enter" and an "exit" action, as
// XState's trackEntries test helper does.
func xhistTrack(b *xhistBuilder, states ...string) {
	for _, s := range states {
		b.State(s).Entry("enter", xhistNoop).Exit("exit", xhistNoop)
	}
}

// xhistCount counts the effects named name.
func xhistCount(effects []sc.Effect[xhistCtx], name string) int {
	n := 0
	for _, got := range names(effects) {
		if got == name {
			n++
		}
	}

	return n
}

// XState: history states > rejects a history state without a non-empty
// default target at runtime
func TestXHistRejectsAHistoryStateWithoutADefaultTarget(t *testing.T) {
	t.Parallel()

	m, err := sc.New("xhist", func(b *xhistBuilder) {
		b.Initial("on")
		b.State("on", func(b *xhistBuilder) {
			b.Initial("active")
			b.State("active").On[xhistNext]().To("history")
			b.History("history", sc.Shallow)
		})
	}, sc.AllowUnreachable())

	// DIVERGES from XState: XState refuses to create a machine whose history
	// state has no default target. sc accepts it: a history state without a
	// Default falls back to its parent's initial state.
	require.NoError(t, err)

	snap, _ := xhistRun(m, xhistNext{})
	require.Equal(t, []string{"on", "active"}, snap.Active())
}

// xhistPowerChart is the on/off chart of the first XState cases: on has two
// children and a shallow history state, and off returns to that history.
// sc has no implicit history kind, so XState's "no explicit history type"
// cases use sc.Shallow, which is XState's default.
func xhistPowerChart(t *testing.T, initial string) *xhistMachine {
	t.Helper()

	return xhistNew(t, func(b *xhistBuilder) {
		b.Initial(initial)
		b.State("on", func(b *xhistBuilder) {
			b.Initial("first")
			// The cases that start in off have no SWITCH transition in
			// XState; they never send SWITCH.
			b.State("first").On[xhistSwitch]().To("second")
			b.State("second")
			b.History("hist", sc.Shallow).Default("first")
		}).On[xhistPower]().To("off")
		b.State("off").On[xhistPower]().To("hist")
	})
}

// XState: history states > should go to the most recently visited state
// (explicit shallow history type)
func TestXHistGoesToTheMostRecentlyVisitedStateExplicitShallow(t *testing.T) {
	t.Parallel()

	snap, _ := xhistRun(xhistPowerChart(t, "on"), xhistSwitch{}, xhistPower{}, xhistPower{})

	require.Equal(t, []string{"on", "second"}, snap.Active())
}

// XState: history states > should go to the most recently visited state (no
// explicit history type)
func TestXHistGoesToTheMostRecentlyVisitedStateNoExplicitType(t *testing.T) {
	t.Parallel()

	// sc requires a history kind, so this is the same chart as the explicit
	// shallow case.
	snap, _ := xhistRun(xhistPowerChart(t, "on"), xhistSwitch{}, xhistPower{}, xhistPower{})

	require.Equal(t, []string{"on", "second"}, snap.Active())
}

// XState: history states > should go to the initial state when no history
// present (explicit shallow history type)
func TestXHistGoesToTheInitialStateWhenNoHistoryPresentExplicitShallow(t *testing.T) {
	t.Parallel()

	snap, _ := xhistRun(xhistPowerChart(t, "off"), xhistPower{})

	require.Equal(t, []string{"on", "first"}, snap.Active())
}

// XState: history states > should go to the initial state when no history
// present (no explicit history type)
func TestXHistGoesToTheInitialStateWhenNoHistoryPresentNoExplicitType(t *testing.T) {
	t.Parallel()

	snap, _ := xhistRun(xhistPowerChart(t, "off"), xhistPower{})

	require.Equal(t, []string{"on", "first"}, snap.Active())
}

// XState: history states > should go to the most recently visited state by a
// transient transition
func TestXHistGoesToTheMostRecentlyVisitedStateByATransientTransition(t *testing.T) {
	t.Parallel()

	m := xhistNew(t, func(b *xhistBuilder) {
		b.Initial("idle")
		b.State("idle", func(b *xhistBuilder) {
			b.Initial("absent")
			b.State("absent").On[xhistDeploy]().To("deploy")
			present := b.State("present")
			present.On[xhistDeploy]().To("deploy")
			present.On[xhistDestroy]().To("destroy")
			b.History("hist", sc.Shallow).Default("absent")
		})
		deploy := b.State("deploy")
		deploy.On[xhistSuccess]().To("present")
		deploy.On[xhistFailure]().To("hist")
		b.State("destroy").Always().To("absent")
	})

	snap, _ := xhistRun(m, xhistDeploy{}, xhistSuccess{}, xhistDestroy{}, xhistDeploy{}, xhistFailure{})

	require.Equal(t, []string{"idle", "absent"}, snap.Active())
}

// XState: history states > should reenter persisted state during reentering
// transition targeting a history state
func TestXHistReentersPersistedStateDuringReenteringTransitionTargetingAHistoryState(t *testing.T) {
	t.Parallel()

	m := xhistNew(t, func(b *xhistBuilder) {
		b.Initial("a")
		b.State("a", func(b *xhistBuilder) {
			b.Initial("a1")
			b.State("a1").On[xhistNext]().To("a2")
			b.State("a2").Entry("entered", xhistNoop).Exit("exited", xhistNoop)
			b.History("a3", sc.Shallow).Default("a1")
		}).On[xhistReenter]().To("a3").Reenter()
	})

	snap, _ := xhistRun(m, xhistNext{})
	snap, effects := xhistSend(m, snap, xhistReenter{})

	require.Equal(t, []string{"a2/exited", "a2/entered"}, names(effects))
	require.Equal(t, []string{"a", "a2"}, snap.Active())
}

// XState: history states > should go to the configured default target when a
// history state is the initial state of the machine
func TestXHistGoesToTheDefaultTargetWhenAHistoryStateIsTheInitialStateOfTheMachine(t *testing.T) {
	t.Parallel()

	m := xhistNew(t, func(b *xhistBuilder) {
		b.Initial("foo")
		b.History("foo", sc.Shallow).Default("bar")
		b.State("bar")
	})

	snap, _ := xhistRun(m)

	require.Equal(t, []string{"bar"}, snap.Active())
}

// XState: history states > should go to the configured default target when a
// history state is the initial state of the transition's target
func TestXHistGoesToTheDefaultTargetWhenAHistoryStateIsTheInitialStateOfTheTransitionsTarget(t *testing.T) {
	t.Parallel()

	m := xhistNew(t, func(b *xhistBuilder) {
		b.Initial("foo")
		b.State("foo").On[xhistNext]().To("bar")
		b.State("bar", func(b *xhistBuilder) {
			b.Initial("baz")
			b.History("baz", sc.Shallow).Default("qwe")
			b.State("qwe")
		})
	})

	snap, _ := xhistRun(m, xhistNext{})

	require.Equal(t, []string{"bar", "qwe"}, snap.Active())
}

// XState: history states > should execute parent entry actions when a history
// default is used before its parent was visited
func TestXHistExecutesParentEntryActionsWhenAHistoryDefaultIsUsedBeforeItsParentWasVisited(t *testing.T) {
	t.Parallel()

	m := xhistNew(t, func(b *xhistBuilder) {
		b.Initial("a")
		b.State("a").On[xhistNext]().To("hist")
		b.State("b", func(b *xhistBuilder) {
			b.Initial("b1")
			b.State("b1")
			b.History("hist", sc.Shallow).Default("b1")
		}).Entry("spy", xhistNoop)
	})

	snap, effects := xhistRun(m, xhistNext{})

	require.Equal(t, 1, xhistCount(effects, "b/spy"))
	require.Equal(t, []string{"b", "b1"}, snap.Active())
}

// xhistRegionsChart is a parallel state with two regions and a history state
// of the given kind, entered from off through that history state.
//
// XState gives the history state a default with one target per region, which
// sc cannot declare: Default takes a single target. Both XState cases name
// each region's initial state (or the region itself), which is what sc enters
// for a history state of a parallel state that has no Default, so the
// history state is declared without one.
func xhistRegionsChart(t *testing.T, kind sc.HistoryKind) *xhistMachine {
	t.Helper()

	return xhistNew(t, func(b *xhistBuilder) {
		b.Initial("off")
		b.State("off").On[xhistGo]().To("hist")
		b.Parallel("on", func(b *xhistBuilder) {
			b.State("regA", func(b *xhistBuilder) {
				b.Initial("a1")
				b.State("a1")
				b.State("a2")
			})
			b.State("regB", func(b *xhistBuilder) {
				b.Initial("b1")
				b.State("b1")
				b.State("b2")
			})
			b.History("hist", kind)
		})
	})
}

// XState: history states > should enter a deep parallel history default
// before its parent was visited
func TestXHistEntersADeepParallelHistoryDefaultBeforeItsParentWasVisited(t *testing.T) {
	t.Parallel()

	snap, _ := xhistRun(xhistRegionsChart(t, sc.Deep), xhistGo{})

	require.Equal(t, []string{"on", "regA", "a1", "regB", "b1"}, snap.Active())
}

// XState: history states > should enter a shallow parallel history default
// before its parent was visited
func TestXHistEntersAShallowParallelHistoryDefaultBeforeItsParentWasVisited(t *testing.T) {
	t.Parallel()

	snap, _ := xhistRun(xhistRegionsChart(t, sc.Shallow), xhistGo{})

	require.Equal(t, []string{"on", "regA", "a1", "regB", "b1"}, snap.Active())
}

// xhistSpyChart has a compound state b with an entry action "spy" and a
// history state whose default, b3, is not b's initial state.
func xhistSpyChart(t *testing.T, leave bool) *xhistMachine {
	t.Helper()

	return xhistNew(t, func(b *xhistBuilder) {
		b.Initial("a")
		b.State("a").On[xhistNext]().To("hist")
		parent := b.State("b", func(b *xhistBuilder) {
			b.Initial("b1")
			b.State("b1")
			b.History("hist", sc.Shallow).Default("b3")
			b.State("b3")
		}).Entry("spy", xhistNoop)
		if leave {
			parent.On[xhistNext]().To("a")
		}
	})
}

// XState: history states > should execute entry actions of a parent of the
// targeted history state when its parent state was never visited yet
func TestXHistExecutesEntryActionsOfTheParentOfTheTargetedHistoryStateWhenNeverVisited(t *testing.T) {
	t.Parallel()

	snap, effects := xhistRun(xhistSpyChart(t, false), xhistNext{})

	require.Equal(t, 1, xhistCount(effects, "b/spy"))
	require.Equal(t, []string{"b", "b3"}, snap.Active())
}

// XState: history states > should execute actions of the initial transition
// when it select a history state as the initial state of its parent
func TestXHistExecutesActionsOfTheInitialTransitionWhenItSelectsAHistoryStateAsTheInitialState(t *testing.T) {
	t.Parallel()

	m := xhistNew(t, func(b *xhistBuilder) {
		b.Initial("a")
		b.State("a").On[xhistNext]().To("b")
		b.State("b", func(b *xhistBuilder) {
			b.Initial("b1")
			b.History("b1", sc.Shallow).Default("b2")
			b.State("b2")
		}).Entry("spy", xhistNoop)
	})

	snap, effects := xhistRun(m, xhistNext{})

	require.Equal(t, 1, xhistCount(effects, "b/spy"))
	require.Equal(t, []string{"b", "b2"}, snap.Active())
}

// XState: history states > should execute entry actions of a parent of the
// targeted history state when its parent state was already visited
func TestXHistExecutesEntryActionsOfTheParentOfTheTargetedHistoryStateWhenAlreadyVisited(t *testing.T) {
	t.Parallel()

	m := xhistSpyChart(t, true)
	snap, _ := xhistRun(m, xhistNext{})

	// XState clears the spy here and counts the calls of both later events.
	snap, leaving := xhistSend(m, snap, xhistNext{})
	snap, returning := xhistSend(m, snap, xhistNext{})

	require.Equal(t, 1, xhistCount(leaving, "b/spy")+xhistCount(returning, "b/spy"))
	require.Equal(t, []string{"b", "b3"}, snap.Active())
}

// XState: history states > should invoke an actor when reentering the stored
// configuration through the history state
func TestXHistInvokesAnActorWhenReenteringTheStoredConfigurationThroughTheHistoryState(t *testing.T) {
	t.Parallel()

	m := xhistNew(t, func(b *xhistBuilder) {
		b.Initial("running")
		running := b.State("running")
		running.On[xhistPing]().To("refresh")
		running.Invoke("callback", func(context.Context, xhistCtx) (struct{}, error) {
			return struct{}{}, nil
		}).OnError().Stay()
		b.History("refresh", sc.Shallow).Default("running")
	})

	snap, started := m.Start(xhistCtx{}, epoch)
	require.Equal(t, []sc.EffectKind{sc.EffectStartInvocation}, kinds(started))

	snap, effects := xhistSend(m, snap, xhistPing{})

	require.Equal(t, []sc.EffectKind{sc.EffectStopInvocation, sc.EffectStartInvocation}, kinds(effects))
	require.Equal(t, []string{"running"}, snap.Active())
}

// xhistDoorChart is the closed/open chart of the two transition-domain
// cases, with every state tracked. The first case's chart has no NEXT and
// CLOSE transitions in XState, and never sends those events.
func xhistDoorChart(t *testing.T) *xhistMachine {
	t.Helper()

	return xhistNew(t, func(b *xhistBuilder) {
		b.Initial("closed")
		b.State("closed").On[xhistClick]().To("hist")
		b.State("open", func(b *xhistBuilder) {
			b.Initial("first")
			b.History("hist", sc.Shallow).Default("first")
			b.State("first").On[xhistNext]().To("second")
			b.State("second").On[xhistClose]().To("closed")
			xhistTrack(b, "first", "second")
		}).On[xhistClick]().To("closed")
		xhistTrack(b, "closed", "open")
	})
}

// XState: history states > should not enter ancestors of the entered history
// state that lie outside of the transition domain when entering the default
// history configuration
func TestXHistDoesNotEnterAncestorsOutsideTheTransitionDomainWhenEnteringTheDefaultHistory(t *testing.T) {
	t.Parallel()

	_, effects := xhistRun(xhistDoorChart(t), xhistClick{})

	require.Equal(t, []string{"closed/exit", "open/enter", "first/enter"}, names(effects))
}

// XState: history states > should not enter ancestors of the entered history
// state that lie outside of the transition domain when restoring the stored
// history configuration
func TestXHistDoesNotEnterAncestorsOutsideTheTransitionDomainWhenRestoringTheStoredHistory(t *testing.T) {
	t.Parallel()

	_, effects := xhistRun(xhistDoorChart(t), xhistClick{}, xhistNext{}, xhistClose{}, xhistClick{})

	require.Equal(t, []string{"closed/exit", "open/enter", "second/enter"}, names(effects))
}

// xhistDeepChart is the chart of the "deep history states" cases: on holds
// first and a three-level second, and a history state of the given kind.
// pToQ adds the P -> Q transition only the last case has.
func xhistDeepChart(t *testing.T, kind sc.HistoryKind, pToQ bool) *xhistMachine {
	t.Helper()

	return xhistNew(t, func(b *xhistBuilder) {
		b.Initial("on")
		b.State("off").On[xhistPower]().To("history")
		b.State("on", func(b *xhistBuilder) {
			b.Initial("first")
			b.State("first").On[xhistSwitch]().To("second")
			b.State("second", func(b *xhistBuilder) {
				b.Initial("A")
				b.State("A").On[xhistInner]().To("B")
				b.State("B", func(b *xhistBuilder) {
					b.Initial("P")
					p := b.State("P")
					if pToQ {
						p.On[xhistInner]().To("Q")
					}
					b.State("Q")
				})
			})
			b.History("history", kind).Default("first")
		}).On[xhistPower]().To("off")
	})
}

// XState: deep history states > should go to the shallow history
func TestXHistDeepGoesToTheShallowHistory(t *testing.T) {
	t.Parallel()

	snap, _ := xhistRun(xhistDeepChart(t, sc.Shallow, false), xhistSwitch{}, xhistInner{}, xhistPower{}, xhistPower{})

	require.Equal(t, []string{"on", "second", "A"}, snap.Active())
}

// XState: deep history states > should go to the deep history (explicit)
func TestXHistDeepGoesToTheDeepHistoryExplicit(t *testing.T) {
	t.Parallel()

	snap, _ := xhistRun(xhistDeepChart(t, sc.Deep, false), xhistSwitch{}, xhistInner{}, xhistPower{}, xhistPower{})

	require.Equal(t, []string{"on", "second", "B", "P"}, snap.Active())
}

// XState: deep history states > should go to the deepest history
func TestXHistDeepGoesToTheDeepestHistory(t *testing.T) {
	t.Parallel()

	snap, _ := xhistRun(xhistDeepChart(t, sc.Deep, true), xhistSwitch{}, xhistInner{}, xhistInner{}, xhistPower{}, xhistPower{})

	require.Equal(t, []string{"on", "second", "B", "Q"}, snap.Active())
}

// xhistParallelChart is the chart of the "parallel history states" cases: a
// parallel state on with regions A and K, each with its own shallow and deep
// history states, and shallow and deep history states of on itself.
//
// XState declares a slightly smaller chart for each case; this is the
// largest of them, from "should re-enter each regions of parallel state
// correctly". The cases that use a smaller one never send the events the
// extra transitions take. As in xhistRegionsChart, on's history states have
// no Default, because XState's name one target per region and those targets
// are the regions' initial states.
func xhistParallelChart(t *testing.T) *xhistMachine {
	t.Helper()

	return xhistNew(t, func(b *xhistBuilder) {
		b.Initial("off")
		off := b.State("off")
		off.On[xhistSwitch]().To("on")
		off.On[xhistPower]().To("on_hist")
		off.On[xhistDeepPower]().To("on_deep")
		off.On[xhistParallelHistory]().To("A_hist", "K_hist")
		off.On[xhistParallelSomeHistory]().To("C", "K_hist")
		off.On[xhistParallelDeepHistory]().To("A_deep", "K_deep")
		b.Parallel("on", func(b *xhistBuilder) {
			b.State("A", func(b *xhistBuilder) {
				b.Initial("B")
				b.State("B").On[xhistInnerA]().To("C")
				b.State("C", func(b *xhistBuilder) {
					b.Initial("D")
					b.State("D").On[xhistInnerA]().To("E")
					b.State("E")
				})
				b.History("A_hist", sc.Shallow).Default("B")
				b.History("A_deep", sc.Deep).Default("B")
			})
			b.State("K", func(b *xhistBuilder) {
				b.Initial("L")
				b.State("L").On[xhistInnerK]().To("M")
				b.State("M", func(b *xhistBuilder) {
					b.Initial("N")
					b.State("N").On[xhistInnerK]().To("O")
					b.State("O")
				})
				b.History("K_hist", sc.Shallow).Default("L")
				b.History("K_deep", sc.Deep).Default("L")
			})
			b.History("on_hist", sc.Shallow)
			b.History("on_deep", sc.Deep)
		}).On[xhistPower]().To("off")
	})
}

// XState: parallel history states > should ignore parallel state history
func TestXHistParallelIgnoresParallelStateHistory(t *testing.T) {
	t.Parallel()

	snap, _ := xhistRun(xhistParallelChart(t), xhistSwitch{}, xhistInnerA{}, xhistPower{}, xhistPower{})

	require.Equal(t, []string{"on", "A", "B", "K", "L"}, snap.Active())
}

// XState: parallel history states > should remember first level state history
func TestXHistParallelRemembersFirstLevelStateHistory(t *testing.T) {
	t.Parallel()

	snap, _ := xhistRun(xhistParallelChart(t), xhistSwitch{}, xhistInnerA{}, xhistPower{}, xhistDeepPower{})

	require.Equal(t, []string{"on", "A", "C", "D", "K", "L"}, snap.Active())
}

// XState: parallel history states > should re-enter each regions of parallel
// state correctly
func TestXHistParallelReentersEachRegionOfParallelStateCorrectly(t *testing.T) {
	t.Parallel()

	snap, _ := xhistRun(xhistParallelChart(t),
		xhistSwitch{}, xhistInnerA{}, xhistInnerA{}, xhistInnerK{}, xhistInnerK{}, xhistPower{}, xhistDeepPower{})

	require.Equal(t, []string{"on", "A", "C", "E", "K", "M", "O"}, snap.Active())
}

// xhistParallelVisited sends the events the last three "parallel history
// states" cases share: both regions are driven to their innermost states and
// the parallel state is left.
func xhistParallelVisited(m *xhistMachine) xhistSnap {
	snap, _ := xhistRun(m,
		xhistSwitch{}, xhistInnerA{}, xhistInnerA{}, xhistInnerK{}, xhistInnerK{}, xhistPower{})

	return snap
}

// XState: parallel history states > should re-enter multiple history states
func TestXHistParallelReentersMultipleHistoryStates(t *testing.T) {
	t.Parallel()

	m := xhistParallelChart(t)
	snap, _ := xhistSend(m, xhistParallelVisited(m), xhistParallelHistory{})

	require.Equal(t, []string{"on", "A", "C", "D", "K", "M", "N"}, snap.Active())
}

// XState: parallel history states > should re-enter a parallel with partial
// history
func TestXHistParallelReentersAParallelWithPartialHistory(t *testing.T) {
	t.Parallel()

	m := xhistParallelChart(t)
	snap, _ := xhistSend(m, xhistParallelVisited(m), xhistParallelSomeHistory{})

	require.Equal(t, []string{"on", "A", "C", "D", "K", "M", "N"}, snap.Active())
}

// XState: parallel history states > should re-enter a parallel with full
// history
func TestXHistParallelReentersAParallelWithFullHistory(t *testing.T) {
	t.Parallel()

	m := xhistParallelChart(t)
	snap, _ := xhistSend(m, xhistParallelVisited(m), xhistParallelDeepHistory{})

	require.Equal(t, []string{"on", "A", "C", "E", "K", "M", "O"}, snap.Active())
}

// XState: internal transition to a history state should enter default history
// state configuration if the containing state has never been exited yet
func TestXHistInternalTransitionToAHistoryStateEntersTheDefaultIfTheContainingStateWasNeverExited(t *testing.T) {
	t.Parallel()

	m := xhistNew(t, func(b *xhistBuilder) {
		b.Initial("first")
		b.State("first").On[xhistNext]().To("other")
		b.State("second", func(b *xhistBuilder) {
			b.Initial("nested")
			b.State("nested")
			b.State("other")
			b.History("hist", sc.Shallow).Default("nested")
		}).On[xhistNext]().To("hist")
	})

	snap, _ := xhistRun(m, xhistNext{})
	require.Equal(t, []string{"second", "other"}, snap.Active())

	snap, _ = xhistSend(m, snap, xhistNext{})

	require.Equal(t, []string{"second", "nested"}, snap.Active())
}

// XState: multistage history states > should go to the most recently visited
// state
func TestXHistMultistageGoesToTheMostRecentlyVisitedState(t *testing.T) {
	t.Parallel()

	m := xhistNew(t, func(b *xhistBuilder) {
		b.Initial("running")
		b.State("running", func(b *xhistBuilder) {
			b.Initial("normal")
			b.State("normal").On[xhistSwitch]().To("turbo")
			b.State("turbo").On[xhistSwitch]().To("normal")
			b.History("H", sc.Shallow).Default("normal")
		}).On[xhistPower]().To("off")
		b.State("starting").On[xhistGo]().To("H")
		b.State("off").On[xhistPower]().To("starting")
	})

	snap, _ := xhistRun(m, xhistSwitch{}, xhistPower{}, xhistPower{}, xhistGo{})

	require.Equal(t, []string{"running", "turbo"}, snap.Active())
}

// xhistPersisted returns the power chart, the snapshot it has after SWITCH
// and POWER, and that snapshot's stored form, as the "revive history states"
// cases share.
func xhistPersisted(t *testing.T) (*xhistMachine, xhistSnap, sc.RawSnapshot) {
	t.Helper()

	m := xhistPowerChart(t, "on")
	snap, _ := xhistRun(m, xhistSwitch{}, xhistPower{})

	data, err := json.Marshal(snap)
	require.NoError(t, err)

	var raw sc.RawSnapshot
	require.NoError(t, json.Unmarshal(data, &raw))
	require.Equal(t, []string{"off"}, raw.Active)

	return m, snap, raw
}

// xhistStoredHistory returns the history a snapshot stores.
func xhistStoredHistory(t *testing.T, snap xhistSnap) map[string][]string {
	t.Helper()

	data, err := json.Marshal(snap)
	require.NoError(t, err)

	var raw sc.RawSnapshot
	require.NoError(t, json.Unmarshal(data, &raw))

	return raw.History
}

// XState: revive history states > should restore from stringified snapshot
func TestXHistReviveRestoresFromStringifiedSnapshot(t *testing.T) {
	t.Parallel()

	m, _, raw := xhistPersisted(t)

	data, err := json.Marshal(raw)
	require.NoError(t, err)

	snap, effects, err := m.Restore(data, epoch)
	require.NoError(t, err)
	require.Empty(t, effects)

	snap, _ = xhistSend(m, snap, xhistPower{})

	require.Equal(t, []string{"on", "second"}, snap.Active())
}

// XState: revive history states > should ignore unresolved ids as-is and log
// a warning
func TestXHistReviveIgnoresUnresolvedIDs(t *testing.T) {
	t.Parallel()

	m, _, raw := xhistPersisted(t)
	raw.History = map[string][]string{"hist": {"nonexistent"}}

	data, err := json.Marshal(raw)
	require.NoError(t, err)

	_, _, err = m.Restore(data, epoch)

	// DIVERGES from XState: XState logs a warning, drops the state it cannot
	// resolve, and carries on, so POWER then enters the history default,
	// on.first, and the persisted history is empty. sc refuses to restore
	// the snapshot at all.
	require.ErrorIs(t, err, sc.ErrInvalidSnapshot)
	require.ErrorContains(t, err, `history state "hist" remembers "nonexistent"`)
}

// XState: revive history states > should not re-resolve already-instantiated
// StateNode
func TestXHistReviveContinuesFromALiveSnapshot(t *testing.T) {
	t.Parallel()

	// XState restores an actor from a live snapshot, whose history already
	// holds StateNode instances, and checks they are used as they are. An sc
	// snapshot is a value: there is nothing to restore, and the machine
	// carries on from it. Only the outcome is ported.
	m, snap, _ := xhistPersisted(t)
	require.Equal(t, []string{"off"}, snap.Active())

	snap, _ = xhistSend(m, snap, xhistPower{})

	require.Equal(t, []string{"on", "second"}, snap.Active())
}

// XState: revive history states > should handle null, undefined, and
// primitive values
func TestXHistReviveHandlesNullUndefinedAndPrimitiveHistory(t *testing.T) {
	t.Parallel()

	m, _, raw := xhistPersisted(t)

	stored, err := json.Marshal(raw)
	require.NoError(t, err)

	// with returns the stored snapshot with its history replaced by value,
	// or removed when value is empty.
	with := func(value string) []byte {
		var fields map[string]json.RawMessage
		require.NoError(t, json.Unmarshal(stored, &fields))
		require.Contains(t, fields, "history")

		if value == "" {
			delete(fields, "history")
		} else {
			fields["history"] = json.RawMessage(value)
		}

		data, err := json.Marshal(fields)
		require.NoError(t, err)

		return data
	}

	// null and undefined: as in XState, the snapshot is restored with no
	// history, so POWER enters the history default.
	for _, value := range []string{"null", ""} {
		snap, _, err := m.Restore(with(value), epoch)
		require.NoError(t, err, "history %q", value)

		snap, _ = xhistSend(m, snap, xhistPower{})

		require.Equal(t, []string{"on", "first"}, snap.Active(), "history %q", value)
		require.Empty(t, xhistStoredHistory(t, snap), "history %q", value)
	}

	// DIVERGES from XState: XState treats a history value that is a number,
	// a string, or a boolean as no history, and restores as above. sc fails
	// to decode the snapshot.
	for _, value := range []string{"42", `"foo"`, "true", "false"} {
		_, _, err := m.Restore(with(value), epoch)
		require.ErrorContains(t, err, "unmarshal snapshot", "history %q", value)
	}
}
