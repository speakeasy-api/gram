package sc_test

// Ports of XState's packages/core/test/transient.test.ts and
// internalTransitions.test.ts (6.0.0-beta.2), one test per `it`, in order.
//
// XState actions are observed through the effects the core returns, named
// "<state>/<action>"; an action declared on the chart itself is named after
// the chart's id.
//
// A root with type: 'parallel' is declared with Builder.Regions, and the
// root's own transitions on the chart itself.

import (
	"context"
	"testing"

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
func xtraCycle(id, last string) string {
	return `sc: machine "` + id + `" does not settle within 10000 steps: its Always transitions or raised events form a cycle, last processing ` + last + ` in states [c]`
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

// xtraGreeting is XState's greetingMachine. RECHECK targets the chart itself
// ('#greeting').
func xtraGreeting(t *testing.T) *sc.Machine[string, xtraHour] {
	t.Helper()

	m, err := sc.New("greeting", func(b *sc.Builder[string, xtraHour]) {
		b.Initial("pending")
		pending := b.State("pending")
		pending.Always().When("beforeNoon", func(c xtraHour, _ struct{}) bool { return c.Hour < 12 }).To("morning")
		pending.Always().When("beforeSix", func(c xtraHour, _ struct{}) bool { return c.Hour < 18 }).To("afternoon")
		pending.Always().To("evening")
		b.State("morning")
		b.State("afternoon")
		b.State("evening")

		root := b.Chart()
		root.On[xtraChange]().Assign("setHour", func(c xtraHour, _ xtraChange) xtraHour {
			c.Hour = 20
			return c
		})
		root.On[xtraRecheck]().ToChart()
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
func TestXTraTransientExecutesAllInternalEventsInOrder(t *testing.T) {
	t.Parallel()

	m, err := sc.New("internal", func(b *xtraBuilder) {
		b.Regions()
		b.State("A", func(b *xtraBuilder) {
			b.Initial("A1")
			b.State("A1").On[xtraE]().To("A2")
			b.State("A2").RaiseOnEntry("raiseInt1", func(counter) any { return xtraInt1{} })
		})
		b.State("B", func(b *xtraBuilder) {
			b.Initial("B1")
			b.State("B1").On[xtraE]().To("B2")
			b.State("B2").RaiseOnEntry("raiseInt2", func(counter) any { return xtraInt2{} })
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
	require.NoError(t, err)

	snap, _ := m.Start(counter{}, epoch)
	snap, _ = m.Transition(snap, xtraE{}, epoch)

	require.Equal(t, []string{"A", "A2", "B", "B2", "C", "C4"}, snap.Active())
}

// XState: transient states (eventless transitions) > should execute all
// eventless transitions in the same microstep
func TestXTraTransientExecutesAllEventlessTransitionsInSameMicrostep(t *testing.T) {
	t.Parallel()

	m, err := sc.New("microstep", func(b *xtraBuilder) {
		b.Regions()
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
	require.NoError(t, err)

	snap, _ := m.Start(counter{}, epoch)
	snap, _ = m.Transition(snap, xtraE{}, epoch)

	require.Equal(t, []string{"A", "A4", "B", "B4"}, snap.Active())
}

// XState: transient states (eventless transitions) > should check for
// automatic transitions even after microsteps are done
func TestXTraTransientChecksAutomaticTransitionsAfterMicrosteps(t *testing.T) {
	t.Parallel()

	m, err := sc.New("automatic", func(b *xtraBuilder) {
		b.Regions()
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
	require.NoError(t, err)

	snap, _ := m.Start(counter{}, epoch)
	snap, _ = m.Transition(snap, xtraA{}, epoch)

	require.Equal(t, []string{"A", "A2", "B", "B2", "C", "C2"}, snap.Active())
}

// XState: transient states (eventless transitions) > should determine the
// resolved initial state from the transient state
func TestXTraTransientResolvesInitialStateFromTransientState(t *testing.T) {
	t.Parallel()

	snap, _ := xtraGreeting(t).Start(xtraHour{Hour: 10}, epoch)

	require.Equal(t, []string{"morning"}, snap.Active())
}

// XState: transient states (eventless transitions) > should determine the
// resolved state from an initial transient state
func TestXTraTransientResolvesStateFromInitialTransientState(t *testing.T) {
	t.Parallel()

	m := xtraGreeting(t)
	snap, _ := m.Start(xtraHour{Hour: 10}, epoch)

	snap, _ = m.Transition(snap, xtraChange{}, epoch)
	require.Equal(t, []string{"morning"}, snap.Active())

	snap, _ = m.Transition(snap, xtraRecheck{}, epoch)
	require.Equal(t, []string{"evening"}, snap.Active())
}

// XState: transient states (eventless transitions) > should select eventless
// transition before processing raised events
func TestXTraTransientSelectsEventlessTransitionBeforeRaisedEvents(t *testing.T) {
	t.Parallel()

	m, err := sc.New("before_raised", func(b *xtraBuilder) {
		b.Initial("a")
		b.State("a").On[xtraFoo]().To("b")
		sb := b.State("b").RaiseOnEntry("raiseBar", func(counter) any { return xtraBar{} })
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
func TestXTraTransientWorksWithTransientTransitionOnRoot(t *testing.T) {
	t.Parallel()

	m, err := sc.New("root_always", func(b *xtraBuilder) {
		b.Initial("first")
		b.State("first").On[xtraAdd]().Assign("increment", increment[xtraAdd])
		b.Final("success")
		b.Always().When("counted", func(c counter, _ struct{}) bool { return c.N > 0 }).To("success")
	})
	require.NoError(t, err)

	snap, _ := m.Start(counter{}, epoch)
	require.False(t, snap.Done())

	snap, _ = m.Transition(snap, xtraAdd{}, epoch)

	require.True(t, snap.Done())
	require.Equal(t, []string{"success"}, snap.Active())
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

// xtraIsWhatever is XState's guard event.type === 'WHATEVER'.
func xtraIsWhatever(_ counter, ev any) bool {
	_, ok := ev.(xtraWhatever)
	return ok
}

// XState: transient states (eventless transitions) > should be taken even in
// absence of other transitions
func TestXTraTransientTakenInAbsenceOfOtherTransitions(t *testing.T) {
	t.Parallel()

	declare := func(id string, named bool) *sc.Machine[string, counter] {
		m, err := sc.New(id, func(b *xtraBuilder) {
			b.Initial("a")
			b.State("a").Always().WhenEvent("isWhatever", xtraIsWhatever).To("b")
			sb := b.State("b")
			if named {
				sb.On[xtraWhatever]().Stay()
			}
		})
		require.NoError(t, err)

		return m
	}

	// As in XState, the chart names WHATEVER nowhere but in the guard, and
	// the eventless transition is still taken when it arrives.
	faithful := declare("absence", false)
	snap, _ := faithful.Start(counter{}, epoch)
	require.Equal(t, []string{"a"}, snap.Active())

	snap, _ = faithful.Transition(snap, xtraWhatever{}, epoch)
	require.Equal(t, []string{"b"}, snap.Active())

	// It makes no difference when a state that is not active names the
	// type.
	named := declare("absence_named", true)
	snap, _ = named.Start(counter{}, epoch)
	snap, _ = named.Transition(snap, xtraWhatever{}, epoch)
	require.Equal(t, []string{"b"}, snap.Active())
}

// XState: transient states (eventless transitions) > should select
// subsequent transient transitions even in absence of other transitions
func TestXTraTransientSelectsSubsequentTransientTransitions(t *testing.T) {
	t.Parallel()

	declare := func(id string, named bool) *sc.Machine[string, counter] {
		m, err := sc.New(id, func(b *xtraBuilder) {
			b.Initial("a")
			b.State("a").Always().WhenEvent("isWhatever", xtraIsWhatever).To("b")
			b.State("b").Always().When("always", func(counter, struct{}) bool { return true }).To("c")
			last := b.State("c")
			if named {
				last.On[xtraWhatever]().Stay()
			}
		})
		require.NoError(t, err)

		return m
	}

	// As in XState, the chart names WHATEVER nowhere but in the guard.
	faithful := declare("subsequent", false)
	snap, _ := faithful.Start(counter{}, epoch)
	snap, _ = faithful.Transition(snap, xtraWhatever{}, epoch)
	require.Equal(t, []string{"c"}, snap.Active())

	// It makes no difference when a state that is not active names the
	// type.
	named := declare("subsequent_named", true)
	snap, _ = named.Start(counter{}, epoch)
	snap, _ = named.Transition(snap, xtraWhatever{}, epoch)
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
			WhenEvent("isEvent", func(_ counter, ev any) bool {
				seen = append(seen, ev)
				_, ok := ev.(xtraEvent)

				return ok
			}).
			To("d")
		b.Final("d")
	})
	require.NoError(t, err)

	snap, _ := m.Start(counter{}, epoch)
	snap, _ = m.Transition(snap, xtraEvent{Value: 0}, epoch)

	require.True(t, snap.Done())
	require.NotEmpty(t, seen)
	for _, ev := range seen {
		require.Equal(t, xtraEvent{Value: 0}, ev)
	}
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

	// As in XState, the action of the eventless transition is given the
	// event that started the step.
	require.Equal(t, xtraEvent{Value: 42}, effects[0].Event)

	// DIVERGES from XState: XState also gives the entry action of the state
	// the eventless transition enters that event, {type: 'EVENT', value: 42}.
	// In sc a state's own entry actions never receive an event, and their
	// effect's Event is nil.
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

	require.PanicsWithValue(t, xtraCycle("loop_always", "<nil>"), func() { m.Start(counter{}, epoch) })
}

// XState: transient states (eventless transitions) > should avoid infinite
// loops with raised events
//
// As in the case before, sc panics where XState reports an error.
func TestXTraTransientAvoidsInfiniteLoopsWithRaisedEvents(t *testing.T) {
	t.Parallel()

	m, err := sc.New("loop_raised", func(b *xtraBuilder) {
		b.Initial("a")
		b.State("a").Always().To("b")
		b.State("b").RaiseOnEntry("raiseEvent", func(counter) any { return xtraEvent{Value: 0} }).
			On[xtraEvent]().To("c")
		b.State("c").Always().To("a")
	})
	require.NoError(t, err)

	require.PanicsWithValue(t, xtraCycle("loop_raised", "sc_test.xtraEvent"), func() { m.Start(counter{}, epoch) })
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
// As in XState, the machine has no states.
func TestXTraTransientExecutesAlwaysAfterRaisedTransition(t *testing.T) {
	t.Parallel()

	m, err := sc.New("always_action", func(b *xtraBuilder) {
		b.Always().Do("spy", func(context.Context, counter, struct{}) {})
		b.On[xtraEv]().Raise("raise", func(counter, xtraEv) any { return xtraRaised{} })
		b.On[xtraRaised]().Do("count", func(context.Context, counter, xtraRaised) {})
	})
	require.NoError(t, err)

	// As in XState, the action of an unguarded, targetless eventless
	// transition runs once per event processed: once on start, once for EV
	// and once more for the RAISED event it raises.
	snap, effects := m.Start(counter{}, epoch)
	require.Equal(t, []string{"always_action/spy"}, names(effects))

	snap, effects = m.Transition(snap, xtraEv{}, epoch)
	require.Empty(t, snap.Active())
	require.Equal(t, []string{"always_action/spy", "always_action/count", "always_action/spy"}, names(effects))
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
func TestXTraInternalListensToEventsDeclaredAtTopState(t *testing.T) {
	t.Parallel()

	m, err := sc.New("top_state", func(b *xtraBuilder) {
		b.Initial("foo")
		b.On[xtraClicked]().To("bar")
		b.State("foo")
		b.State("bar")
	})
	require.NoError(t, err)

	snap, _ := m.Start(counter{}, epoch)
	snap, _ = m.Transition(snap, xtraClicked{}, epoch)

	require.Equal(t, []string{"bar"}, snap.Active())
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
// transitions" cases.
func xtraTargetlessOnParent(t *testing.T) {
	t.Helper()

	m, err := sc.New("targetless_parent", func(b *xtraBuilder) {
		b.On[xtraTargetless]().Do("spy", func(context.Context, counter, xtraTargetless) {})
		b.Initial("foo")
		b.State("foo")
	})
	require.NoError(t, err)

	snap, _ := m.Start(counter{}, epoch)
	snap, effects := m.Transition(snap, xtraTargetless{}, epoch)

	require.Equal(t, []string{"foo"}, snap.Active())
	require.Equal(t, []string{"targetless_parent/spy"}, names(effects))
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
func TestXTraInternalMaintainsChildStateOnParentTargetlessTransition(t *testing.T) {
	t.Parallel()

	m, err := sc.New("maintain_child", func(b *xtraBuilder) {
		b.Initial("foo")
		b.On[xtraParentEvent]().Do("noop", func(context.Context, counter, xtraParentEvent) {})
		xtraTracked(b.State("foo"))
	})
	require.NoError(t, err)

	snap, _ := m.Start(counter{}, epoch)
	snap, effects := m.Transition(snap, xtraParentEvent{}, epoch)

	require.Equal(t, []string{"foo"}, snap.Active())
	require.Equal(t, []string{"maintain_child/noop"}, names(effects))
}

// xtraVisits is the context of the last two cases: how often the source
// state of the transition, its child and its grandchild were entered, or
// exited.
type xtraVisits struct {
	Source int `json:"source"`
	Direct int `json:"direct"`
	Deep   int `json:"deep"`
}

func xtraCountSource(v xtraVisits) xtraVisits { v.Source++; return v }
func xtraCountDirect(v xtraVisits) xtraVisits { v.Direct++; return v }
func xtraCountDeep(v xtraVisits) xtraVisits   { v.Deep++; return v }

// xtraNested is the chart of the last two cases: a1 > a11 > a111, with a
// transition on a1 that targets a111. Each state counts its entries, or its
// exits, in the context.
func xtraNested(t *testing.T, exits bool) *sc.Machine[string, xtraVisits] {
	t.Helper()

	count := func(s *sc.StateBuilder[string, xtraVisits], f func(xtraVisits) xtraVisits) *sc.StateBuilder[string, xtraVisits] {
		if exits {
			return s.AssignOnExit("count", f)
		}

		return s.AssignOnEntry("count", f)
	}

	m, err := sc.New("nested", func(b *sc.Builder[string, xtraVisits]) {
		b.Initial("a1")
		count(b.State("a1", func(b *sc.Builder[string, xtraVisits]) {
			b.Initial("a11")
			count(b.State("a11", func(b *sc.Builder[string, xtraVisits]) {
				b.Initial("a111")
				count(b.State("a111"), xtraCountDeep)
			}), xtraCountDirect)
		}), xtraCountSource).On[xtraReenter]().To("a111")
	})
	require.NoError(t, err)

	return m
}

// XState: internal transitions > should reenter proper descendants of a
// source state of an internal transition
func TestXTraInternalReentersProperDescendantsOfSource(t *testing.T) {
	t.Parallel()

	m := xtraNested(t, false)

	snap, _ := m.Start(xtraVisits{Source: 0, Direct: 0, Deep: 0}, epoch)
	require.Equal(t, xtraVisits{Source: 1, Direct: 1, Deep: 1}, snap.Context)

	snap, _ = m.Transition(snap, xtraReenter{}, epoch)

	require.Equal(t, xtraVisits{Source: 1, Direct: 2, Deep: 2}, snap.Context)
}

// XState: internal transitions > should exit proper descendants of a source
// state of an internal transition
func TestXTraInternalExitsProperDescendantsOfSource(t *testing.T) {
	t.Parallel()

	m := xtraNested(t, true)

	snap, _ := m.Start(xtraVisits{Source: 0, Direct: 0, Deep: 0}, epoch)
	snap, _ = m.Transition(snap, xtraReenter{}, epoch)

	require.Equal(t, xtraVisits{Source: 0, Direct: 1, Deep: 1}, snap.Context)
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
