package sc_test

// Ports of XState's packages/core/test/transient.test.ts and
// internalTransitions.test.ts (6.0.0-beta.2), one test per `it`, in order.
//
// Three things about sc shape every port in this file:
//
//   - sc has no transitions on the chart root, so an XState machine with a
//     root-level `on` or `always` is wrapped in one compound state that stands
//     for the root.
//   - sc entry and exit actions cannot raise events or assign the context, so
//     an XState entry action that raises is declared as a Raise step on the
//     transition that enters the state, and entry counters kept in the context
//     are counted from the returned effects instead.
//   - XState actions are observed through the effects the core returns.

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/sc"
)

type (
	xtraUpdate      struct{}
	xtraTimer       struct{}
	xtraE           struct{}
	xtraInt1        struct{}
	xtraInt2        struct{}
	xtraA           struct{}
	xtraChange      struct{}
	xtraRecheck     struct{}
	xtraFoo         struct{}
	xtraBar         struct{}
	xtraAdd         struct{}
	xtraWhatever    struct{}
	xtraEvent       struct{ Value int }
	xtraGo          struct{}
	xtraEv          struct{}
	xtraRaised      struct{}
	xtraClick       struct{}
	xtraNext        struct{}
	xtraReset       struct{}
	xtraResetToB    struct{}
	xtraClicked     struct{}
	xtraTargetless  struct{}
	xtraParentEvent struct{}
	xtraReenter     struct{}
)

// xtraData is the context of the "first candidate" charts.
type xtraData struct {
	Data bool `json:"data"`
}

// xtraHour is the context of the greeting chart.
type xtraHour struct {
	Hour int `json:"hour"`
}

// xtraDuration and xtraCustom are the contexts of the invoked timer chart
// and of its parent.
type xtraDuration struct {
	Duration int `json:"duration"`
}

type xtraCustom struct {
	CustomDuration int `json:"custom_duration"`
}

type xtraBuilder = sc.Builder[string, counter]

// xtraCycle is what the core panics with when a chart with the given id
// never settles.
func xtraCycle(id string) string {
	return `sc: machine "` + id + `" does not settle: its Always transitions or raised events form a cycle`
}

// xtraCandidates is the chart of the three "candidate" cases: E leaves for D
// when the context has no data and for F otherwise.
func xtraCandidates(t *testing.T) *sc.Machine[string, xtraData] {
	t.Helper()

	m, err := sc.New("candidates", func(b *sc.Builder[string, xtraData]) {
		b.Initial("G")
		b.State("G").On[xtraUpdate]().To("E")
		e := b.State("E")
		e.Always().When("noData", func(c xtraData, _ struct{}) bool { return !c.Data }).To("D")
		e.Always().To("F")
		b.State("D")
		b.State("F")
	})
	require.NoError(t, err)

	return m
}

// xtraGreeting is XState's greetingMachine. The state "greeting" stands for
// the root, which carries the CHANGE and RECHECK transitions.
func xtraGreeting(t *testing.T) *sc.Machine[string, xtraHour] {
	t.Helper()

	m, err := sc.New("greeting", func(b *sc.Builder[string, xtraHour]) {
		b.Initial("greeting")
		root := b.State("greeting", func(b *sc.Builder[string, xtraHour]) {
			b.Initial("pending")
			pending := b.State("pending")
			pending.Always().When("beforeNoon", func(c xtraHour, _ struct{}) bool { return c.Hour < 12 }).To("morning")
			pending.Always().When("beforeSix", func(c xtraHour, _ struct{}) bool { return c.Hour < 18 }).To("afternoon")
			pending.Always().To("evening")
			b.State("morning")
			b.State("afternoon")
			b.State("evening")
		})
		root.On[xtraChange]().Assign("setHour", func(c xtraHour, _ xtraChange) xtraHour {
			c.Hour = 20
			return c
		})
		root.On[xtraRecheck]().To("greeting")
	})
	require.NoError(t, err)

	return m
}

// XState: transient states (eventless transitions) > should choose the first
// candidate target that matches the guard 1
func TestXTraTransientChoosesFirstCandidateMatchingGuard1(t *testing.T) {
	t.Parallel()

	m := xtraCandidates(t)
	snap, _ := m.Start(xtraData{Data: false}, epoch)
	snap, _ = m.Transition(snap, xtraUpdate{}, epoch)

	require.Equal(t, []string{"D"}, snap.Active())
}

// XState: transient states (eventless transitions) > should choose the first
// candidate target that matches the guard 2
//
// The XState case differs from the first only in the context's schema.
func TestXTraTransientChoosesFirstCandidateMatchingGuard2(t *testing.T) {
	t.Parallel()

	m := xtraCandidates(t)
	snap, _ := m.Start(xtraData{Data: false}, epoch)
	snap, _ = m.Transition(snap, xtraUpdate{}, epoch)

	require.Equal(t, []string{"D"}, snap.Active())
}

// XState: transient states (eventless transitions) > should choose the final
// candidate without a guard if none others match
func TestXTraTransientChoosesFinalCandidateWithoutGuard(t *testing.T) {
	t.Parallel()

	m := xtraCandidates(t)
	snap, _ := m.Start(xtraData{Data: true}, epoch)
	snap, _ = m.Transition(snap, xtraUpdate{}, epoch)

	require.Equal(t, []string{"F"}, snap.Active())
}

// XState: transient states (eventless transitions) > should carry actions
// from previous transitions within same step
func TestXTraTransientCarriesActionsFromPreviousTransitions(t *testing.T) {
	t.Parallel()

	m, err := sc.New("carry", func(b *xtraBuilder) {
		b.Initial("A")
		b.State("A").Exit("exit_A", noop).
			On[xtraTimer]().Do("timer", func(context.Context, counter, xtraTimer) {}).To("T")
		b.State("T").Always().To("B")
		b.State("B").Entry("enter_B", noop)
	})
	require.NoError(t, err)

	snap, _ := m.Start(counter{}, epoch)
	snap, effects := m.Transition(snap, xtraTimer{}, epoch)

	require.Equal(t, []string{"B"}, snap.Active())
	require.Equal(t, []string{"A/exit_A", "A/timer", "B/enter_B"}, names(effects))
}

// XState: transient states (eventless transitions) > should execute all
// internal events one after the other
//
// A2 and B2 raise from their entry actions in XState; here the transitions
// that enter them raise instead.
func TestXTraTransientExecutesAllInternalEventsInOrder(t *testing.T) {
	t.Parallel()

	m, err := sc.New("internal", func(b *xtraBuilder) {
		b.Initial("P")
		b.Parallel("P", func(b *xtraBuilder) {
			b.State("A", func(b *xtraBuilder) {
				b.Initial("A1")
				b.State("A1").On[xtraE]().Raise("raiseInt1", func(counter, xtraE) any { return xtraInt1{} }).To("A2")
				b.State("A2")
			})
			b.State("B", func(b *xtraBuilder) {
				b.Initial("B1")
				b.State("B1").On[xtraE]().Raise("raiseInt2", func(counter, xtraE) any { return xtraInt2{} }).To("B2")
				b.State("B2")
			})
			b.State("C", func(b *xtraBuilder) {
				b.Initial("C1")
				c1 := b.State("C1")
				c1.On[xtraInt1]().To("C2")
				c1.On[xtraInt2]().To("C3")
				b.State("C2").On[xtraInt2]().To("C4")
				b.State("C3").On[xtraInt1]().To("C4")
				b.State("C4")
			})
		})
	})
	require.NoError(t, err)

	snap, _ := m.Start(counter{}, epoch)
	snap, _ = m.Transition(snap, xtraE{}, epoch)

	require.Equal(t, []string{"P", "A", "A2", "B", "B2", "C", "C4"}, snap.Active())
}

// XState: transient states (eventless transitions) > should execute all
// eventless transitions in the same microstep
func TestXTraTransientExecutesAllEventlessTransitionsInSameMicrostep(t *testing.T) {
	t.Parallel()

	m, err := sc.New("microstep", func(b *xtraBuilder) {
		b.Initial("P")
		b.Parallel("P", func(b *xtraBuilder) {
			b.State("A", func(b *xtraBuilder) {
				b.Initial("A1")
				b.State("A1").On[xtraE]().To("A2")
				b.State("A2").Always().To("A3")
				b.State("A3").Always().WhenIn("B3").To("A4")
				b.State("A4")
			})
			b.State("B", func(b *xtraBuilder) {
				b.Initial("B1")
				b.State("B1").On[xtraE]().To("B2")
				b.State("B2").Always().WhenIn("A2").To("B3")
				b.State("B3").Always().WhenIn("A3").To("B4")
				b.State("B4")
			})
		})
	})
	require.NoError(t, err)

	snap, _ := m.Start(counter{}, epoch)
	snap, _ = m.Transition(snap, xtraE{}, epoch)

	require.Equal(t, []string{"P", "A", "A4", "B", "B4"}, snap.Active())
}

// XState: transient states (eventless transitions) > should check for
// automatic transitions even after microsteps are done
func TestXTraTransientChecksAutomaticTransitionsAfterMicrosteps(t *testing.T) {
	t.Parallel()

	m, err := sc.New("automatic", func(b *xtraBuilder) {
		b.Initial("P")
		b.Parallel("P", func(b *xtraBuilder) {
			b.State("A", func(b *xtraBuilder) {
				b.Initial("A1")
				b.State("A1").On[xtraA]().To("A2")
				b.State("A2")
			})
			b.State("B", func(b *xtraBuilder) {
				b.Initial("B1")
				b.State("B1").Always().WhenIn("A2").To("B2")
				b.State("B2")
			})
			b.State("C", func(b *xtraBuilder) {
				b.Initial("C1")
				b.State("C1").Always().WhenIn("A2").To("C2")
				b.State("C2")
			})
		})
	})
	require.NoError(t, err)

	snap, _ := m.Start(counter{}, epoch)
	snap, _ = m.Transition(snap, xtraA{}, epoch)

	require.Equal(t, []string{"P", "A", "A2", "B", "B2", "C", "C2"}, snap.Active())
}

// XState: transient states (eventless transitions) > should determine the
// resolved initial state from the transient state
func TestXTraTransientResolvesInitialStateFromTransientState(t *testing.T) {
	t.Parallel()

	snap, _ := xtraGreeting(t).Start(xtraHour{Hour: 10}, epoch)

	require.Equal(t, []string{"greeting", "morning"}, snap.Active())
}

// XState: transient states (eventless transitions) > should determine the
// resolved state from an initial transient state
func TestXTraTransientResolvesStateFromInitialTransientState(t *testing.T) {
	t.Parallel()

	m := xtraGreeting(t)
	snap, _ := m.Start(xtraHour{Hour: 10}, epoch)

	snap, _ = m.Transition(snap, xtraChange{}, epoch)
	require.Equal(t, []string{"greeting", "morning"}, snap.Active())

	snap, _ = m.Transition(snap, xtraRecheck{}, epoch)
	require.Equal(t, []string{"greeting", "evening"}, snap.Active())
}

// XState: transient states (eventless transitions) > should select eventless
// transition before processing raised events
//
// b raises BAR from its entry action in XState; here the transition that
// enters b raises it.
func TestXTraTransientSelectsEventlessTransitionBeforeRaisedEvents(t *testing.T) {
	t.Parallel()

	m, err := sc.New("before_raised", func(b *xtraBuilder) {
		b.Initial("a")
		b.State("a").On[xtraFoo]().Raise("raiseBar", func(counter, xtraFoo) any { return xtraBar{} }).To("b")
		sb := b.State("b")
		sb.Always().To("c")
		sb.On[xtraBar]().To("d")
		b.State("c").On[xtraBar]().To("e")
		b.State("d")
		b.State("e")
	})
	require.NoError(t, err)

	snap, _ := m.Start(counter{}, epoch)
	snap, _ = m.Transition(snap, xtraFoo{}, epoch)

	require.Equal(t, []string{"e"}, snap.Active())
}

// XState: transient states (eventless transitions) > should not select
// wildcard for eventless transition
func TestXTraTransientDoesNotSelectWildcardForEventlessTransition(t *testing.T) {
	t.Parallel()

	m, err := sc.New("wildcard", func(b *xtraBuilder) {
		b.Initial("a")
		b.State("a").On[xtraFoo]().To("b")
		sb := b.State("b")
		sb.Always().To("pass")
		sb.OnAny().To("fail")
		b.State("fail")
		b.State("pass")
	})
	require.NoError(t, err)

	snap, _ := m.Start(counter{}, epoch)
	snap, _ = m.Transition(snap, xtraFoo{}, epoch)

	require.Equal(t, []string{"pass"}, snap.Active())
}

// XState: transient states (eventless transitions) > should work with
// transient transition on root
//
// The state "machine" stands for the root. Its final child completes it
// rather than the chart, so its OnDone leads to a top-level final state.
func TestXTraTransientWorksWithTransientTransitionOnRoot(t *testing.T) {
	t.Parallel()

	declare := func(id string, guardAgainstRepeat bool) *sc.Machine[string, counter] {
		m, err := sc.New(id, func(b *xtraBuilder) {
			b.Initial("machine")
			root := b.State("machine", func(b *xtraBuilder) {
				b.Initial("first")
				b.State("first").On[xtraAdd]().Assign("increment", increment[xtraAdd])
				b.Final("success")
			})
			always := root.Always().When("counted", func(c counter, _ struct{}) bool { return c.N > 0 })
			if guardAgainstRepeat {
				always.WhenNotIn("success")
			}
			always.To("success")
			root.OnDone().To("done")
			b.Final("done")
		})
		require.NoError(t, err)

		return m
	}

	// As in XState, the eventless transition is taken once: taking it
	// again would change nothing.
	faithful := declare("root_always", false)
	snap, _ := faithful.Start(counter{}, epoch)
	snap, _ = faithful.Transition(snap, xtraAdd{}, epoch)
	require.True(t, snap.Done())

	// With the transition told not to repeat, sc gives XState's result.
	guarded := declare("root_always_guarded", true)
	snap, _ = guarded.Start(counter{}, epoch)
	snap, _ = guarded.Transition(snap, xtraAdd{}, epoch)

	require.True(t, snap.Done())
	require.Equal(t, []string{"done"}, snap.Active())
}

// XState: transient states (eventless transitions) > shouldn't crash when
// invoking a machine with initial transient transition depending on custom
// data
//
// The pure core does not run services, so this checks the two halves a
// driver would join: the parent starts and asks for the child, and the child
// started with the input the parent gives it takes its transient transition.
func TestXTraTransientInvokedMachineWithInitialTransientTransition(t *testing.T) {
	t.Parallel()

	timer, err := sc.New("timer", func(b *sc.Builder[string, xtraDuration]) {
		b.Initial("initial")
		initial := b.State("initial")
		initial.Always().When("short", func(c xtraDuration, _ struct{}) bool { return c.Duration < 1000 }).To("finished")
		initial.Always().To("active")
		b.State("active")
		b.Final("finished")
	})
	require.NoError(t, err)

	input := func(c xtraCustom) xtraDuration { return xtraDuration{Duration: c.CustomDuration} }

	m, err := sc.New("invoker", func(b *sc.Builder[string, xtraCustom]) {
		b.Initial("active")
		b.State("active").
			InvokeMachine("timer", timer, input, func(sc.Snapshot[string, xtraDuration]) (struct{}, error) {
				return struct{}{}, nil
			}).
			OnError().Stay()
	})
	require.NoError(t, err)

	parent := xtraCustom{CustomDuration: 3000}

	var effects []sc.Effect[xtraCustom]
	require.NotPanics(t, func() { _, effects = m.Start(parent, epoch) })
	require.Equal(t, "active/timer", names(effects)[0])
	require.Equal(t, sc.EffectStartInvocation, effects[0].Kind)

	var child sc.Snapshot[string, xtraDuration]
	require.NotPanics(t, func() { child, _ = timer.Start(input(parent), epoch) })
	require.Equal(t, []string{"active"}, child.Active())
}

// XState: transient states (eventless transitions) > should be taken even in
// absence of other transitions
//
// DIVERGES from XState: XState's `always` guard reads the event being
// processed (event.type === 'WHATEVER'). An sc Always guard is never given
// the event, so the case cannot be written as it is. The nearest thing an sc
// guard can read that changes without a transition is the time, so the guard
// here holds once the clock has moved. sc agrees with XState on what the
// case is about: an event no state handles still makes the eventless
// transitions be looked at again.
func TestXTraTransientTakenInAbsenceOfOtherTransitions(t *testing.T) {
	t.Parallel()

	m, err := sc.New("absence", func(b *xtraBuilder) {
		b.Initial("a")
		b.State("a").Always().
			WhenAt("later", func(_ counter, _ struct{}, now time.Time) bool { return now.After(epoch) }).
			To("b")
		b.State("b")
	})
	require.NoError(t, err)

	snap, _ := m.Start(counter{}, epoch)
	require.Equal(t, []string{"a"}, snap.Active())

	snap, _ = m.Transition(snap, xtraWhatever{}, epoch.Add(time.Second))

	require.Equal(t, []string{"b"}, snap.Active())
}

// XState: transient states (eventless transitions) > should select
// subsequent transient transitions even in absence of other transitions
//
// DIVERGES from XState: as in the case before, the guard on the event's type
// is replaced by one on the time, because an sc Always guard is not given
// the event.
func TestXTraTransientSelectsSubsequentTransientTransitions(t *testing.T) {
	t.Parallel()

	m, err := sc.New("subsequent", func(b *xtraBuilder) {
		b.Initial("a")
		b.State("a").Always().
			WhenAt("later", func(_ counter, _ struct{}, now time.Time) bool { return now.After(epoch) }).
			To("b")
		b.State("b").Always().When("always", func(counter, struct{}) bool { return true }).To("c")
		b.State("c")
	})
	require.NoError(t, err)

	snap, _ := m.Start(counter{}, epoch)
	snap, _ = m.Transition(snap, xtraWhatever{}, epoch.Add(time.Second))

	require.Equal(t, []string{"c"}, snap.Active())
}

// XState: transient states (eventless transitions) > events that trigger
// eventless transitions should be preserved in guards
func TestXTraTransientEventsPreservedInGuards(t *testing.T) {
	t.Parallel()

	var seen []any

	m, err := sc.New("guard_event", func(b *xtraBuilder) {
		b.Initial("a")
		b.State("a").On[xtraEvent]().To("b")
		b.State("b").Always().To("c")
		b.State("c").Always().
			When("sawEvent", func(_ counter, ev struct{}) bool {
				seen = append(seen, ev)
				return true
			}).
			To("d")
		b.Final("d")
	})
	require.NoError(t, err)

	snap, _ := m.Start(counter{}, epoch)
	snap, _ = m.Transition(snap, xtraEvent{Value: 42}, epoch)

	require.True(t, snap.Done())

	// DIVERGES from XState: XState hands the guard of an eventless
	// transition the event that started the step ({type: 'EVENT'}), and the
	// guard takes the transition only for that event. An sc Always guard has
	// the type func(C, struct{}) bool and is given an empty struct, so it
	// cannot tell which event led to it.
	require.Equal(t, []any{struct{}{}}, seen)
}

// XState: transient states (eventless transitions) > events that trigger
// eventless transitions should be preserved in actions
func TestXTraTransientEventsPreservedInActions(t *testing.T) {
	t.Parallel()

	m, err := sc.New("action_event", func(b *xtraBuilder) {
		b.Initial("a")
		b.State("a").On[xtraEvent]().To("b")
		b.State("b").Always().Do("check", func(context.Context, counter, struct{}) {}).To("c")
		b.State("c").Entry("check", noop)
	})
	require.NoError(t, err)

	snap, _ := m.Start(counter{}, epoch)
	snap, effects := m.Transition(snap, xtraEvent{Value: 42}, epoch)

	require.Equal(t, []string{"c"}, snap.Active())
	require.Equal(t, []string{"b/check", "c/check"}, names(effects))

	// DIVERGES from XState: XState gives both the action of the eventless
	// transition and the entry action of the state it enters the event that
	// started the step, {type: 'EVENT', value: 42}. In sc neither effect
	// carries it: an Always action is declared for struct{} and its effect's
	// Event is nil, and entry actions never receive an event.
	require.Nil(t, effects[0].Event)
	require.Nil(t, effects[1].Event)
}

// XState: transient states (eventless transitions) > should avoid infinite
// loops with eventless transitions
//
// XState reports an "infinite loop" error to the actor's subscribers when it
// is started. The pure core has no error to return, so it panics, with a
// message of its own.
func TestXTraTransientAvoidsInfiniteLoopsWithEventlessTransitions(t *testing.T) {
	t.Parallel()

	m, err := sc.New("loop_always", func(b *xtraBuilder) {
		b.Initial("a")
		b.State("a").Always().To("b")
		b.State("b").Always().To("c")
		b.State("c").Always().To("a")
	})
	require.NoError(t, err)

	require.PanicsWithValue(t, xtraCycle("loop_always"), func() { m.Start(counter{}, epoch) })
}

// XState: transient states (eventless transitions) > should avoid infinite
// loops with raised events
//
// b raises EVENT from its entry action in XState; here the transition that
// enters b raises it. As in the case before, sc panics where XState reports
// an error.
func TestXTraTransientAvoidsInfiniteLoopsWithRaisedEvents(t *testing.T) {
	t.Parallel()

	m, err := sc.New("loop_raised", func(b *xtraBuilder) {
		b.Initial("a")
		b.State("a").Always().Raise("raiseEvent", func(counter, struct{}) any { return xtraEvent{Value: 0} }).To("b")
		b.State("b").On[xtraEvent]().To("c")
		b.State("c").Always().To("a")
	})
	require.NoError(t, err)

	require.PanicsWithValue(t, xtraCycle("loop_raised"), func() { m.Start(counter{}, epoch) })
}

// XState: transient states (eventless transitions) > shouldn't end up in an
// infinite loop when selecting the fallback target
func TestXTraTransientNoInfiniteLoopSelectingFallbackTarget(t *testing.T) {
	t.Parallel()

	declare := func(id string, guardAgainstRepeat bool) *sc.Machine[string, counter] {
		m, err := sc.New(id, func(b *xtraBuilder) {
			b.Initial("idle")
			b.State("idle").On[xtraGo]().To("active")
			active := b.State("active", func(b *xtraBuilder) {
				b.Initial("a")
				b.State("a")
				b.State("b")
			})
			active.Always().When("never", func(counter, struct{}) bool { return false }).To("a")
			fallback := active.Always()
			if guardAgainstRepeat {
				fallback.WhenNotIn("b")
			}
			fallback.To("b")
		})
		require.NoError(t, err)

		return m
	}

	// As in XState, an eventless transition that leaves everything as it
	// was is not taken again.
	faithful := declare("fallback", false)
	snap, _ := faithful.Start(counter{}, epoch)
	snap, _ = faithful.Transition(snap, xtraGo{}, epoch)
	require.Equal(t, []string{"active", "b"}, snap.Active())

	// With the transition told not to repeat, sc gives XState's result.
	guarded := declare("fallback_guarded", true)
	snap, _ = guarded.Start(counter{}, epoch)
	snap, _ = guarded.Transition(snap, xtraGo{}, epoch)

	require.Equal(t, []string{"active", "b"}, snap.Active())
}

// XState: transient states (eventless transitions) > shouldn't end up in an
// infinite loop when selecting a guarded target
func TestXTraTransientNoInfiniteLoopSelectingGuardedTarget(t *testing.T) {
	t.Parallel()

	m, err := sc.New("guarded_target", func(b *xtraBuilder) {
		b.Initial("idle")
		b.State("idle").On[xtraGo]().To("active")
		active := b.State("active", func(b *xtraBuilder) {
			b.Initial("a")
			b.State("a")
			b.State("b")
		})
		active.Always().When("holds", func(counter, struct{}) bool { return true }).To("a")
		active.Always().To("b")
	})
	require.NoError(t, err)

	snap, _ := m.Start(counter{}, epoch)

	snap, _ = m.Transition(snap, xtraGo{}, epoch)
	require.Equal(t, []string{"active", "a"}, snap.Active())
}

// XState: transient states (eventless transitions) > shouldn't end up in an
// infinite loop when executing a fire-and-forget action that doesn't change
// state
func TestXTraTransientNoInfiniteLoopExecutingFireAndForgetAction(t *testing.T) {
	t.Parallel()

	m, err := sc.New("fire_and_forget", func(b *xtraBuilder) {
		b.Initial("idle")
		b.State("idle").On[xtraGo]().To("active")
		b.State("active", func(b *xtraBuilder) {
			b.Initial("a")
			b.State("a")
		}).Always().Do("count", func(context.Context, counter, struct{}) {}).To("a")
	})
	require.NoError(t, err)

	snap, _ := m.Start(counter{}, epoch)

	// As in XState, the action runs exactly once.
	snap, effects := m.Transition(snap, xtraGo{}, epoch)
	require.Equal(t, []string{"active", "a"}, snap.Active())
	require.Equal(t, 1, xtraCount(names(effects), "active/count"))
}

// XState: transient states (eventless transitions) > should loop (but not
// infinitely) for assign actions
func TestXTraTransientLoopsForAssignActions(t *testing.T) {
	t.Parallel()

	m, err := sc.New("counting", func(b *xtraBuilder) {
		b.Initial("counting")
		b.State("counting").Always().
			When("belowFive", func(c counter, _ struct{}) bool { return c.N < 5 }).
			Assign("increment", increment[struct{}])
	})
	require.NoError(t, err)

	snap, _ := m.Start(counter{}, epoch)

	require.Equal(t, 5, snap.Context.N)
}

// XState: transient states (eventless transitions) > should execute an
// always transition after a raised transition even if that raised transition
// doesn't change the state
//
// The XState machine is a root with no states; here it is the single state
// "root".
func TestXTraTransientExecutesAlwaysAfterRaisedTransition(t *testing.T) {
	t.Parallel()

	declare := func(id string, withAlways bool) *sc.Machine[string, counter] {
		m, err := sc.New(id, func(b *xtraBuilder) {
			b.Initial("root")
			root := b.State("root")
			if withAlways {
				root.Always().Do("spy", func(context.Context, counter, struct{}) {})
			}
			root.On[xtraEv]().Raise("raise", func(counter, xtraEv) any { return xtraRaised{} })
			root.On[xtraRaised]().Do("count", func(context.Context, counter, xtraRaised) {})
		})
		require.NoError(t, err)

		return m
	}

	// As in XState, the action of an unguarded, targetless eventless
	// transition runs once per event processed: once on start, once for EV
	// and once more for the RAISED event it raises.
	m := declare("always_action", true)
	snap, effects := m.Start(counter{}, epoch)
	require.Equal(t, []string{"root/spy"}, names(effects))

	snap, effects = m.Transition(snap, xtraEv{}, epoch)
	require.Equal(t, []string{"root"}, snap.Active())
	require.Equal(t, []string{"root/spy", "root/count", "root/spy"}, names(effects))
}

// xtraTracked declares entry and exit actions named "enter" and "exit" on a
// state, the way XState's cases track them.
func xtraTracked(s *sc.StateBuilder[string, counter]) *sc.StateBuilder[string, counter] {
	return s.Entry("enter", noop).Exit("exit", noop)
}

// XState: internal transitions > parent state should enter child state
// without re-entering self
func TestXTraInternalParentEntersChildWithoutReenteringSelf(t *testing.T) {
	t.Parallel()

	m, err := sc.New("enter_child", func(b *xtraBuilder) {
		b.Initial("foo")
		b.State("foo", func(b *xtraBuilder) {
			b.Initial("a")
			xtraTracked(b.State("a"))
			xtraTracked(b.State("b"))
		}).On[xtraClick]().To("b")
	})
	require.NoError(t, err)

	snap, _ := m.Start(counter{}, epoch)
	snap, effects := m.Transition(snap, xtraClick{}, epoch)

	require.Equal(t, []string{"foo", "b"}, snap.Active())
	require.Equal(t, []string{"a/exit", "b/enter"}, names(effects))
}

// XState: internal transitions > parent state should re-enter self upon
// transitioning to child state if transition is reentering
func TestXTraInternalParentReentersSelfWhenTransitionIsReentering(t *testing.T) {
	t.Parallel()

	m, err := sc.New("reenter_child", func(b *xtraBuilder) {
		b.Initial("foo")
		xtraTracked(b.State("foo", func(b *xtraBuilder) {
			b.Initial("left")
			xtraTracked(b.State("left"))
			xtraTracked(b.State("right"))
		})).On[xtraNext]().To("right").Reenter()
	})
	require.NoError(t, err)

	snap, _ := m.Start(counter{}, epoch)
	snap, effects := m.Transition(snap, xtraNext{}, epoch)

	require.Equal(t, []string{"foo", "right"}, snap.Active())
	require.Equal(t, []string{"left/exit", "foo/exit", "foo/enter", "right/enter"}, names(effects))
}

// XState: internal transitions > parent state should only exit/reenter if
// there is an explicit self-transition
func TestXTraInternalParentExitsAndReentersOnExplicitSelfTransition(t *testing.T) {
	t.Parallel()

	m, err := sc.New("self_transition", func(b *xtraBuilder) {
		b.Initial("foo")
		xtraTracked(b.State("foo", func(b *xtraBuilder) {
			b.Initial("a")
			xtraTracked(b.State("a")).On[xtraNext]().To("b")
			xtraTracked(b.State("b"))
		})).On[xtraReset]().To("foo").Reenter()
	})
	require.NoError(t, err)

	snap, _ := m.Start(counter{}, epoch)
	snap, _ = m.Transition(snap, xtraNext{}, epoch)
	snap, effects := m.Transition(snap, xtraReset{}, epoch)

	require.Equal(t, []string{"foo", "a"}, snap.Active())
	require.Equal(t, []string{"b/exit", "foo/exit", "foo/enter", "a/enter"}, names(effects))
}

// XState: internal transitions > parent state should only exit/reenter if
// there is an explicit self-transition (to child)
func TestXTraInternalParentExitsAndReentersOnExplicitSelfTransitionToChild(t *testing.T) {
	t.Parallel()

	m, err := sc.New("self_transition_child", func(b *xtraBuilder) {
		b.Initial("foo")
		xtraTracked(b.State("foo", func(b *xtraBuilder) {
			b.Initial("a")
			xtraTracked(b.State("a"))
			xtraTracked(b.State("b"))
		})).On[xtraResetToB]().To("b").Reenter()
	})
	require.NoError(t, err)

	snap, _ := m.Start(counter{}, epoch)
	snap, effects := m.Transition(snap, xtraResetToB{}, epoch)

	require.Equal(t, []string{"foo", "b"}, snap.Active())
	require.Equal(t, []string{"a/exit", "foo/exit", "foo/enter", "b/enter"}, names(effects))
}

// XState: internal transitions > should listen to events declared at top
// state
//
// The state "top" stands for the root.
func TestXTraInternalListensToEventsDeclaredAtTopState(t *testing.T) {
	t.Parallel()

	m, err := sc.New("top_state", func(b *xtraBuilder) {
		b.Initial("top")
		b.State("top", func(b *xtraBuilder) {
			b.Initial("foo")
			b.State("foo")
			b.State("bar")
		}).On[xtraClicked]().To("bar")
	})
	require.NoError(t, err)

	snap, _ := m.Start(counter{}, epoch)
	snap, _ = m.Transition(snap, xtraClicked{}, epoch)

	require.Equal(t, []string{"top", "bar"}, snap.Active())
}

// xtraTargetlessOnState is the chart of the two "targetless transitions"
// cases, which are the same machine in XState 6.
func xtraTargetlessOnState(t *testing.T) {
	t.Helper()

	m, err := sc.New("targetless", func(b *xtraBuilder) {
		b.Initial("foo")
		b.State("foo").On[xtraTargetless]().Do("spy", func(context.Context, counter, xtraTargetless) {})
	})
	require.NoError(t, err)

	snap, _ := m.Start(counter{}, epoch)
	snap, effects := m.Transition(snap, xtraTargetless{}, epoch)

	require.Equal(t, []string{"foo"}, snap.Active())
	require.Equal(t, []string{"foo/spy"}, names(effects))
}

// xtraTargetlessOnParent is the chart of the two "on parent with targetless
// transitions" cases. The state "top" stands for the root.
func xtraTargetlessOnParent(t *testing.T) {
	t.Helper()

	m, err := sc.New("targetless_parent", func(b *xtraBuilder) {
		b.Initial("top")
		b.State("top", func(b *xtraBuilder) {
			b.Initial("foo")
			b.State("foo")
		}).On[xtraTargetless]().Do("spy", func(context.Context, counter, xtraTargetless) {})
	})
	require.NoError(t, err)

	snap, _ := m.Start(counter{}, epoch)
	snap, effects := m.Transition(snap, xtraTargetless{}, epoch)

	require.Equal(t, []string{"top", "foo"}, snap.Active())
	require.Equal(t, []string{"top/spy"}, names(effects))
}

// XState: internal transitions > should work with targetless transitions (in
// conditional array)
func TestXTraInternalTargetlessTransitionsInConditionalArray(t *testing.T) {
	t.Parallel()

	xtraTargetlessOnState(t)
}

// XState: internal transitions > should work with targetless transitions (in
// object)
func TestXTraInternalTargetlessTransitionsInObject(t *testing.T) {
	t.Parallel()

	xtraTargetlessOnState(t)
}

// XState: internal transitions > should work on parent with targetless
// transitions (in conditional array)
func TestXTraInternalParentTargetlessTransitionsInConditionalArray(t *testing.T) {
	t.Parallel()

	xtraTargetlessOnParent(t)
}

// XState: internal transitions > should work on parent with targetless
// transitions (in object)
func TestXTraInternalParentTargetlessTransitionsInObject(t *testing.T) {
	t.Parallel()

	xtraTargetlessOnParent(t)
}

// XState: internal transitions > should maintain the child state when
// targetless transition is handled by parent
//
// The state "top" stands for the root.
func TestXTraInternalMaintainsChildStateOnParentTargetlessTransition(t *testing.T) {
	t.Parallel()

	m, err := sc.New("maintain_child", func(b *xtraBuilder) {
		b.Initial("top")
		b.State("top", func(b *xtraBuilder) {
			b.Initial("foo")
			xtraTracked(b.State("foo"))
		}).On[xtraParentEvent]().Do("noop", func(context.Context, counter, xtraParentEvent) {})
	})
	require.NoError(t, err)

	snap, _ := m.Start(counter{}, epoch)
	snap, effects := m.Transition(snap, xtraParentEvent{}, epoch)

	require.Equal(t, []string{"top", "foo"}, snap.Active())
	require.Equal(t, []string{"top/noop"}, names(effects))
}

// xtraNested is the chart of the last two cases: a1 > a11 > a111, with a
// transition on a1 that targets a111.
func xtraNested(t *testing.T) *sc.Machine[string, counter] {
	t.Helper()

	m, err := sc.New("nested", func(b *xtraBuilder) {
		b.Initial("a1")
		xtraTracked(b.State("a1", func(b *xtraBuilder) {
			b.Initial("a11")
			xtraTracked(b.State("a11", func(b *xtraBuilder) {
				b.Initial("a111")
				xtraTracked(b.State("a111"))
			}))
		})).On[xtraReenter]().To("a111")
	})
	require.NoError(t, err)

	return m
}

// XState: internal transitions > should reenter proper descendants of a
// source state of an internal transition
//
// XState counts entries in the context from entry actions; here they are
// read from the entry effects.
func TestXTraInternalReentersProperDescendantsOfSource(t *testing.T) {
	t.Parallel()

	m := xtraNested(t)

	snap, effects := m.Start(counter{}, epoch)
	require.Equal(t, []string{"a1/enter", "a11/enter", "a111/enter"}, names(effects))

	snap, effects = m.Transition(snap, xtraReenter{}, epoch)

	require.Equal(t, []string{"a1", "a11", "a111"}, snap.Active())
	require.NotContains(t, names(effects), "a1/enter")
	require.Contains(t, names(effects), "a11/enter")
	require.Contains(t, names(effects), "a111/enter")
}

// XState: internal transitions > should exit proper descendants of a source
// state of an internal transition
func TestXTraInternalExitsProperDescendantsOfSource(t *testing.T) {
	t.Parallel()

	m := xtraNested(t)

	snap, _ := m.Start(counter{}, epoch)
	_, effects := m.Transition(snap, xtraReenter{}, epoch)

	require.Equal(t, []string{"a111/exit", "a11/exit", "a11/enter", "a111/enter"}, names(effects))
}

// xtraCount counts how often name occurs among names.
func xtraCount(all []string, name string) int {
	n := 0
	for _, each := range all {
		if each == name {
			n++
		}
	}

	return n
}
