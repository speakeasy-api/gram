package sc_test

// Ports of XState's packages/core/test/after.test.ts, guards.test.ts,
// stateIn.test.ts and predictableExec.test.ts (6.0.0-beta.2), one test per
// `it`, in order.
//
// Time is an argument of the pure core, so XState's fake timers become
// explicit times: vi.advanceTimersByTime(n) is Machine.Advance at epoch+n.
// XState actions are observed through the effects the core returns, named
// "<state>/<action>", the state of the chart's own actions being the
// machine's id. An XState root that is parallel is Builder.Regions, and what
// an XState root declares for itself (entry actions, delayed transitions,
// services) is declared on Builder.Chart. An XState machine that declares
// no states is an sc chart without states.
//
// Cases that are not ported, with the reason:
//
// after.test.ts
//   - "does not rely on inferred function names for built-in timer effects":
//     about the names of JavaScript functions inside XState.
//   - "should transition after an ISO8601 duration without a delay source",
//     "should error on a delay that is neither a delay name nor a duration",
//     "should prefer a delay source value over the parsed ISO8601 duration":
//     delays are time.Duration values or named Go functions, never strings.
//
// guards.test.ts
//   - "should throw on a guard reference that is not implemented": a guard is
//     a Go function, so a misspelt one does not compile.
//   - "rejects source name '%s' for every source kind": sc reserves no names.

import (
	"context"
	"encoding/json"
	"testing"
	"testing/synctest"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/sc"
)

type (
	xaftGo           struct{}
	xaftCancel       struct{}
	xaftNext         struct{}
	xaftActivate     struct{ Delay time.Duration }
	xaftAny          struct{}
	xaftTimer        struct{}
	xaftTimerCondObj struct{}
	xaftEmergency    struct{ IsEmergency bool }
	xaftElapsedTimer struct{ Elapsed int }
	xaftT1           struct{}
	xaftT2           struct{}
	xaftA            struct{}
	xaftEvent        struct{ Value int }
	xaftEv           struct{}
	xaftSubmit       struct{}
	xaftCheck        struct{}
	xaftEvent1       struct{}
	xaftEvent2       struct{}
	xaftEvent3       struct{}
	xaftEventDeep    struct{}
	xaftMyEvent      struct{}
	xaftRaised       struct{}
	xaftChildUpdated struct{}
	xaftPing         struct{}
	xaftPong         struct{}
	xaftKnock        struct{}
	xaftToggle       struct{}
)

// xaftMs is the context of the charts whose delays are computed from it.
type xaftMs struct {
	Ms     int `json:"ms"`
	Offset int `json:"offset"`
}

// xaftDurations is the context of the chart whose delay depends on the state
// it belongs to.
type xaftDurations struct {
	Waiting int `json:"waiting"`
	Active  int `json:"active"`
}

// xaftElapsed is the context of the traffic light the guard suites share.
type xaftElapsed struct {
	Elapsed int `json:"elapsed"`
}

// xaftValid is the context of the form charts.
type xaftValid struct {
	Valid bool `json:"valid"`
}

// xaftFlag is the context of the charts with one boolean.
type xaftFlag struct {
	Flag bool `json:"flag"`
}

type xaftBuilder = sc.Builder[string, counter]

// xaftAt is the time n milliseconds after the instance started.
func xaftAt(n int) time.Time {
	return epoch.Add(time.Duration(n) * time.Millisecond)
}

func xaftMillis(n int) time.Duration {
	return time.Duration(n) * time.Millisecond
}

// xaftActions names the actions among effects, leaving out timers and
// services.
func xaftActions[C any](effects []sc.Effect[C]) []string {
	out := make([]string, 0, len(effects))
	for _, e := range effects {
		if e.Kind == sc.EffectAction {
			out = append(out, e.State+"/"+e.Name)
		}
	}

	return out
}

func xaftEntry[C any](context.Context, C) {}

func xaftDo[C, Ev any](context.Context, C, Ev) {}

func xaftNoResult[S comparable, C any](sc.Snapshot[S, C]) (struct{}, error) {
	return struct{}{}, nil
}

func xaftSameContext(c counter) counter { return c }

// xaftLight is XState's lightMachine of after.test.ts.
func xaftLight(t *testing.T) *sc.Machine[string, counter] {
	t.Helper()

	m, err := sc.New("light", func(b *xaftBuilder) {
		b.Initial("green")
		b.State("green").After(time.Second).To("yellow")
		b.State("yellow").After(time.Second).To("red")
		b.State("red").After(time.Second).To("green")
	})
	require.NoError(t, err)

	return m
}

// XState: delayed transitions > resolves a named delay with context updated
// by the same state entry
func TestXAftAfterResolvesNamedDelayWithContextUpdatedBySameStateEntry(t *testing.T) {
	t.Parallel()

	m, err := sc.New("named_delay", func(b *sc.Builder[string, xaftMs]) {
		b.Initial("a")
		b.State("a").On[xaftGo]().To("b")
		sb := b.State("b").AssignOnEntry("setMs", func(c xaftMs) xaftMs {
			c.Ms = 300
			return c
		})
		sb.Delay("d", func(c xaftMs) time.Duration { return xaftMillis(c.Ms) }).To("a")
	})
	require.NoError(t, err)

	snap, _ := m.Start(xaftMs{Ms: 0, Offset: 0}, epoch)
	snap, _ = m.Transition(snap, xaftGo{}, epoch)
	require.Equal(t, 300, snap.Context.Ms)

	snap, _ = m.Advance(snap, xaftAt(299))
	require.Equal(t, []string{"b"}, snap.Active())

	snap, _ = m.Advance(snap, xaftAt(300))
	require.Equal(t, []string{"a"}, snap.Active())
}

// XState: delayed transitions > resolves all initial state delays after an
// enqueue entry context patch
func TestXAftAfterResolvesAllInitialStateDelaysAfterEntryContextPatch(t *testing.T) {
	t.Parallel()

	var order []string

	m, err := sc.New("initial_delays", func(b *sc.Builder[string, xaftMs]) {
		b.Initial("waiting")
		waiting := b.State("waiting").
			AssignOnEntry("patch", func(c xaftMs) xaftMs {
				order = append(order, "entry")
				c.Ms = 300

				return c
			}).
			Entry("effect", xaftEntry[xaftMs])
		waiting.Delay("first", func(c xaftMs) time.Duration {
			order = append(order, "first")
			return xaftMillis(c.Ms + c.Offset)
		}).To("done")
		waiting.Delay("second", func(c xaftMs) time.Duration {
			order = append(order, "second")
			return xaftMillis(c.Ms*2 + c.Offset)
		}).To("done")
		b.State("done")
	})
	require.NoError(t, err)

	snap, effects := m.Start(xaftMs{Ms: 0, Offset: 20}, epoch)

	require.Equal(t, []string{"entry", "first", "second"}, order)
	require.Equal(t, []string{"waiting/effect"}, xaftActions(effects))

	delays := make([]time.Duration, 0, 2)
	for _, timer := range snap.Timers() {
		delays = append(delays, timer.Delay)
	}
	require.Equal(t, []time.Duration{xaftMillis(320), xaftMillis(620)}, delays)

	snap, _ = m.Advance(snap, xaftAt(319))
	require.Equal(t, []string{"waiting"}, snap.Active())

	snap, _ = m.Advance(snap, xaftAt(320))
	require.Equal(t, []string{"done"}, snap.Active())
	require.Empty(t, snap.Timers())
}

// xaftCancellable is XState's chart for the two cases that follow: a state
// with a delayed transition and an event that cancels its timer without
// leaving it.
//
// XState's entry action also tries to cancel the timer, which does nothing
// because the timer is scheduled after the entry actions run; the port
// leaves that attempt out, since sc starts the timer at the same point.
func xaftCancellable(t *testing.T) *sc.Machine[string, counter] {
	t.Helper()

	m, err := sc.New("cancel", func(b *xaftBuilder) {
		b.Initial("waiting")
		waiting := b.State("waiting")
		waiting.After(xaftMillis(100)).To("done")
		waiting.On[xaftCancel]().CancelAfter("waiting")
		b.State("done")
	})
	require.NoError(t, err)

	return m
}

// XState: delayed transitions > schedules after entry cancellation and honors
// later cancellation (cancel after entry: false)
func TestXAftAfterSchedulesAfterEntryCancellationFalse(t *testing.T) {
	t.Parallel()

	m := xaftCancellable(t)

	snap, _ := m.Start(counter{N: 0}, epoch)

	timers := snap.Timers()
	require.Len(t, timers, 1)
	require.Equal(t, "waiting", timers[0].State)
	require.Equal(t, xaftMillis(100), timers[0].Delay)
	// DIVERGES from XState: XState's built-in timers have no id and are
	// cancelled by their event; every sc timer has an ID, which its
	// TimerFired event carries.
	require.NotEmpty(t, timers[0].ID)

	snap, _ = m.Advance(snap, xaftAt(50))
	require.Equal(t, []string{"waiting"}, snap.Active())

	snap, _ = m.Advance(snap, xaftAt(150))
	require.Equal(t, []string{"done"}, snap.Active())
}

// XState: delayed transitions > schedules after entry cancellation and honors
// later cancellation (cancel after entry: true)
func TestXAftAfterSchedulesAfterEntryCancellationTrue(t *testing.T) {
	t.Parallel()

	m := xaftCancellable(t)

	snap, _ := m.Start(counter{N: 0}, epoch)
	require.Len(t, snap.Timers(), 1)

	snap, _ = m.Advance(snap, xaftAt(50))
	require.Equal(t, []string{"waiting"}, snap.Active())

	// The cancel event clears the timer and the state stays.
	snap, _ = m.Transition(snap, xaftCancel{}, xaftAt(50))
	require.Empty(t, snap.Timers())

	snap, _ = m.Advance(snap, xaftAt(150))
	require.Equal(t, []string{"waiting"}, snap.Active())
}

// XState: delayed transitions > uses a canonical after event with delay and
// state identity
//
// XState's machine declares the delayed transition on itself and has no
// states, nor does the chart.
func TestXAftAfterUsesCanonicalAfterEventWithDelayAndStateIdentity(t *testing.T) {
	t.Parallel()

	m, err := sc.New("job", func(b *xaftBuilder) {
		b.Chart().After(xaftMillis(10)).Do("spy", xaftDo[counter, struct{}])
	})
	require.NoError(t, err)

	snap, effects := m.Start(counter{N: 0}, epoch)
	timer := effect(t, effects, sc.EffectStartTimer)
	require.Equal(t, "job", timer.State)
	require.Equal(t, xaftMillis(10), timer.Delay)

	// The chart's timer is the snapshot's under the zero state.
	require.Len(t, snap.Timers(), 1)
	require.Empty(t, snap.Timers()[0].State)

	_, effects = m.Advance(snap, xaftAt(10))
	require.Equal(t, []string{"job/spy"}, xaftActions(effects))

	// DIVERGES from XState: XState hands the action the event
	// {type: 'xstate.after', delay: 10, stateId: 'job'}. sc's delayed
	// transition has no event: the action's effect names the chart in
	// Effect.State, by the machine's id as XState's stateId does, and
	// carries a nil Event, and the delay is known only from the timer effect
	// that started it.
	spy := effect(t, effects, sc.EffectAction)
	require.Equal(t, "job", spy.State)
	require.Nil(t, spy.Event)
}

// XState: delayed transitions > should transition after delay
func TestXAftAfterShouldTransitionAfterDelay(t *testing.T) {
	t.Parallel()

	m := xaftLight(t)
	snap, _ := m.Start(counter{N: 0}, epoch)
	require.Equal(t, []string{"green"}, snap.Active())

	snap, _ = m.Advance(snap, xaftAt(500))
	require.Equal(t, []string{"green"}, snap.Active())

	snap, _ = m.Advance(snap, xaftAt(1010))
	require.Equal(t, []string{"yellow"}, snap.Active())
}

// XState: delayed transitions > should not try to clear an undefined timeout
// when exiting source state of a delayed transition
//
// XState's clock.clearTimeout is sc's EffectCancelTimer: a timer that fired
// must not be cancelled as its state is left.
func TestXAftAfterDoesNotCancelTheTimerThatFired(t *testing.T) {
	t.Parallel()

	m, err := sc.New("clear", func(b *xaftBuilder) {
		b.Initial("green")
		b.State("green").After(time.Millisecond).To("yellow")
		b.State("yellow")
	})
	require.NoError(t, err)

	snap, effects := m.Start(counter{N: 0}, epoch)
	timer := effect(t, effects, sc.EffectStartTimer)

	// As a driver delivers it.
	fired, effects := m.Transition(snap, sc.TimerFired{ID: timer.ID}, xaftAt(1))
	require.Equal(t, []string{"yellow"}, fired.Active())
	require.NotContains(t, kinds(effects), sc.EffectCancelTimer)

	// As the core finds it due.
	advanced, effects := m.Advance(snap, xaftAt(5))
	require.Equal(t, []string{"yellow"}, advanced.Active())
	require.NotContains(t, kinds(effects), sc.EffectCancelTimer)
}

// XState: delayed transitions > should format transitions properly
func TestXAftAfterShouldFormatTransitionsProperly(t *testing.T) {
	t.Parallel()

	green := xaftLight(t).Definition().States["green"]

	// DIVERGES from XState: XState keys the state's transitions by the one
	// event type "xstate.after" and matches on {delay: 1000, stateId:
	// 'light.green'}. sc has no such event; its export lists delayed
	// transitions under "after", keyed by the delay in milliseconds, and
	// state IDs are not qualified by the machine's.
	require.Empty(t, green.On)
	require.Len(t, green.After, 1)
	require.Len(t, green.After["1000"], 1)
	require.Equal(t, "#yellow", green.After["1000"][0].Target)
	require.Equal(t, "green", green.ID)
}

// XState: delayed transitions > should be able to transition with delay from
// nested initial state
func TestXAftAfterTransitionsWithDelayFromNestedInitialState(t *testing.T) {
	t.Parallel()

	m, err := sc.New("nested_delay", func(b *xaftBuilder) {
		b.Initial("nested")
		b.State("nested", func(b *xaftBuilder) {
			b.Initial("wait")
			b.State("wait").After(xaftMillis(10)).To("end")
		})
		b.Final("end")
	})
	require.NoError(t, err)

	snap, _ := m.Start(counter{N: 0}, epoch)
	require.False(t, snap.Done())

	snap, _ = m.Advance(snap, xaftAt(10))
	require.True(t, snap.Done())
}

// XState: delayed transitions > parent state should enter child state without
// re-entering self (relative target)
func TestXAftAfterParentEntersChildWithoutReenteringSelf(t *testing.T) {
	t.Parallel()

	m, err := sc.New("relative", func(b *xaftBuilder) {
		b.Initial("one")
		one := b.State("one", func(b *xaftBuilder) {
			b.Initial("two")
			b.State("two").Entry("entered", noop)
			b.State("three").Entry("entered", noop).Always().To("end")
		}).Entry("entered", noop)
		one.After(xaftMillis(10)).To("three")
		b.Final("end")
	})
	require.NoError(t, err)

	snap, started := m.Start(counter{N: 0}, epoch)
	snap, advanced := m.Advance(snap, xaftAt(10))

	require.True(t, snap.Done())
	require.Equal(t,
		[]string{"one/entered", "two/entered", "three/entered"},
		append(xaftActions(started), xaftActions(advanced)...),
	)
}

// XState: delayed transitions > should defer a single send event for a
// delayed conditional transition (#886)
func TestXAftAfterDefersSingleSendEventForDelayedConditionalTransition(t *testing.T) {
	t.Parallel()

	m, err := sc.New("conditional_delay", func(b *xaftBuilder) {
		b.Initial("X")
		x := b.State("X")
		x.After(time.Millisecond).When("always", func(counter, struct{}) bool { return true }).To("Y")
		x.After(time.Millisecond).To("Z")
		b.State("Y").OnAny().Do("spy", xaftDo[counter, any])
		b.State("Z")
	})
	require.NoError(t, err)

	snap, effects := m.Start(counter{N: 0}, epoch)

	// DIVERGES from XState: XState gives the candidates of one delay a
	// single timer and so a single deferred event. Each After of sc is a
	// transition with a timer of its own, so two timers start; the second is
	// dropped when the first leaves the state.
	require.Equal(t, []sc.EffectKind{sc.EffectStartTimer, sc.EffectStartTimer}, kinds(effects))

	snap, effects = m.Advance(snap, xaftAt(10))
	require.Equal(t, []string{"Y"}, snap.Active())
	require.Empty(t, xaftActions(effects))
	require.Empty(t, snap.Timers())
}

// XState: delayed transitions > should execute an after transition after
// starting from a state resolved using `.getPersistedSnapshot`
//
// XState skips this case (it.skip), leaving the behaviour undecided. sc
// stores the timer's due time in the snapshot, so it is ported.
func TestXAftAfterExecutesAfterTransitionFromGetPersistedSnapshot(t *testing.T) {
	t.Parallel()

	m, err := sc.New("machine", func(b *xaftBuilder) {
		b.Initial("a")
		b.State("a").On[xaftNext]().To("withAfter")
		b.State("withAfter").After(time.Millisecond).To("done")
		b.Final("done")
	})
	require.NoError(t, err)

	snap, _ := m.Start(counter{N: 0}, epoch)
	snap, _ = m.Transition(snap, xaftNext{}, epoch)

	data, err := json.Marshal(snap)
	require.NoError(t, err)

	restored, _, err := m.Restore(data, epoch)
	require.NoError(t, err)
	require.Equal(t, []string{"withAfter"}, restored.Active())
	require.Len(t, restored.Timers(), 1)
	require.Equal(t, xaftAt(1), restored.Timers()[0].Due)

	restored, _ = m.Advance(restored, xaftAt(1))
	require.True(t, restored.Done())
}

// XState: delayed transitions > should execute an after transition after
// starting from a persisted state
func TestXAftAfterExecutesAfterTransitionFromPersistedState(t *testing.T) {
	t.Parallel()

	declare := func() *sc.Machine[string, counter] {
		m, err := sc.New("persisted", func(b *xaftBuilder) {
			b.Initial("A")
			b.State("A").On[xaftNext]().To("B")
			b.State("B").After(time.Millisecond).To("C")
			b.Final("C")
		})
		require.NoError(t, err)

		return m
	}

	snap, _ := declare().Start(counter{N: 0}, epoch)

	data, err := json.Marshal(snap)
	require.NoError(t, err)

	m := declare()
	restored, _, err := m.Restore(data, epoch)
	require.NoError(t, err)

	restored, _ = m.Transition(restored, xaftNext{}, epoch)
	require.Equal(t, []string{"B"}, restored.Active())

	restored, _ = m.Advance(restored, xaftAt(1))
	require.True(t, restored.Done())
}

// XState: delayed transitions > delay expressions > should evaluate the
// expression (function) to determine the delay
func TestXAftAfterDelayExpressionFunction(t *testing.T) {
	t.Parallel()

	var seen []xaftMs

	m, err := sc.New("delay_function", func(b *sc.Builder[string, xaftMs]) {
		b.Initial("inactive")
		b.State("inactive").Delay("myDelay", func(c xaftMs) time.Duration {
			seen = append(seen, c)
			return xaftMillis(c.Ms)
		}).To("active")
		b.State("active")
	})
	require.NoError(t, err)

	initial := xaftMs{Ms: 500, Offset: 0}
	snap, _ := m.Start(initial, epoch)

	require.Equal(t, []xaftMs{initial}, seen)
	require.Equal(t, []string{"inactive"}, snap.Active())

	snap, _ = m.Advance(snap, xaftAt(300))
	require.Equal(t, []string{"inactive"}, snap.Active())

	snap, _ = m.Advance(snap, xaftAt(500))
	require.Equal(t, []string{"active"}, snap.Active())
}

// XState: delayed transitions > delay expressions > should evaluate the
// expression (string) to determine the delay
func TestXAftAfterDelayExpressionString(t *testing.T) {
	t.Parallel()

	var seen []xaftMs

	m, err := sc.New("delay_string", func(b *sc.Builder[string, xaftMs]) {
		b.Initial("inactive")
		// DIVERGES from XState: XState hands the delay expression the event
		// that entered the state, and the test reads the delay from it. An
		// sc delay sees only the context, so the transition has to copy the
		// event's delay into the context first.
		b.State("inactive").On[xaftActivate]().
			Assign("keepDelay", func(c xaftMs, ev xaftActivate) xaftMs {
				c.Ms = int(ev.Delay / time.Millisecond)
				return c
			}).
			To("active")
		b.State("active").Delay("someDelay", func(c xaftMs) time.Duration {
			seen = append(seen, c)
			return xaftMillis(c.Ms)
		}).To("inactive")
	})
	require.NoError(t, err)

	snap, _ := m.Start(xaftMs{Ms: 0, Offset: 0}, epoch)
	snap, _ = m.Transition(snap, xaftActivate{Delay: xaftMillis(500)}, epoch)

	require.Equal(t, []xaftMs{{Ms: 500, Offset: 0}}, seen)
	require.Equal(t, []string{"active"}, snap.Active())

	snap, _ = m.Advance(snap, xaftAt(300))
	require.Equal(t, []string{"active"}, snap.Active())

	snap, _ = m.Advance(snap, xaftAt(500))
	require.Equal(t, []string{"inactive"}, snap.Active())
}

// XState: delayed transitions > stateNode in delay functions > should pass
// stateNode to delay expression
func TestXAftAfterPassesStateNodeToDelayExpression(t *testing.T) {
	t.Parallel()

	var keys []string

	m, err := sc.New("phases", func(b *sc.Builder[string, xaftDurations]) {
		// DIVERGES from XState: XState declares one delay, phaseDuration,
		// which is told the state node it is computed for and used by both
		// states. An sc delay sees only the context, so each state declares
		// its own function, which knows its state by closing over it.
		phase := func(key string, read func(xaftDurations) int) func(xaftDurations) time.Duration {
			return func(c xaftDurations) time.Duration {
				keys = append(keys, key)
				return xaftMillis(read(c))
			}
		}

		b.Initial("waiting")
		b.State("waiting").
			Delay("phaseDuration", phase("waiting", func(c xaftDurations) int { return c.Waiting })).
			To("active")
		b.State("active").
			Delay("phaseDuration", phase("active", func(c xaftDurations) int { return c.Active })).
			To("done")
		b.Final("done")
	})
	require.NoError(t, err)

	snap, _ := m.Start(xaftDurations{Waiting: 300, Active: 500}, epoch)
	require.Equal(t, []string{"waiting"}, keys)
	require.Equal(t, []string{"waiting"}, snap.Active())

	snap, _ = m.Advance(snap, xaftAt(300))
	require.Equal(t, []string{"active"}, snap.Active())
	require.Equal(t, []string{"waiting", "active"}, keys)

	snap, _ = m.Advance(snap, xaftAt(800))
	require.Equal(t, []string{"done"}, snap.Active())
}

// XState: delayed transitions > stateNode in delay functions > should pass
// stateNode with correct id
func TestXAftAfterPassesStateNodeWithCorrectID(t *testing.T) {
	t.Parallel()

	m, err := sc.New("test", func(b *xaftBuilder) {
		b.Initial("a")
		b.State("a").Delay("myDelay", func(counter) time.Duration { return xaftMillis(100) }).To("b")
		b.Final("b")
	})
	require.NoError(t, err)

	_, effects := m.Start(counter{N: 0}, epoch)
	timer := effect(t, effects, sc.EffectStartTimer)

	// DIVERGES from XState: XState tells the delay expression its state
	// node, whose id is 'test.a'. An sc delay is not told its state; the
	// state is named by the timer effect instead, and by its own identifier
	// "a", not one qualified by the machine's.
	require.Equal(t, "a", timer.State)
	require.Equal(t, "myDelay", timer.Name)
	require.Equal(t, xaftMillis(100), timer.Delay)
}

// xaftMinTimeElapsed is XState's minTimeElapsed guard.
func xaftMinTimeElapsed(elapsed int) bool {
	return elapsed >= 100 && elapsed < 200
}

// xaftGuardLight is the lightMachine both guard suites declare.
func xaftGuardLight(t *testing.T) *sc.Machine[string, xaftElapsed] {
	t.Helper()

	m, err := sc.New("guard_light", func(b *sc.Builder[string, xaftElapsed]) {
		b.Initial("green")
		xaftGuardGreen(b.State("green"))
		yellow := b.State("yellow")
		yellow.On[xaftTimer]().
			When("minTimeElapsed", func(c xaftElapsed, _ xaftTimer) bool { return xaftMinTimeElapsed(c.Elapsed) }).
			To("red")
		yellow.On[xaftTimerCondObj]().
			When("minTimeElapsed", func(c xaftElapsed, _ xaftTimerCondObj) bool { return xaftMinTimeElapsed(c.Elapsed) }).
			To("red")
		b.State("red")
	})
	require.NoError(t, err)

	return m
}

// xaftGuardGreen declares the transitions of the light's green state.
func xaftGuardGreen(green *sc.StateBuilder[string, xaftElapsed]) {
	green.On[xaftTimer]().
		When("under100", func(c xaftElapsed, _ xaftTimer) bool { return c.Elapsed < 100 }).
		To("green")
	green.On[xaftTimer]().
		When("under200", func(c xaftElapsed, _ xaftTimer) bool { return c.Elapsed >= 100 && c.Elapsed < 200 }).
		To("yellow")
	green.On[xaftEmergency]().
		When("isEmergency", func(_ xaftElapsed, ev xaftEmergency) bool { return ev.IsEmergency }).
		To("red")
}

// xaftGuardYellow is the machine of the "(condition not met)" cases: it
// starts in yellow, which its guard does not let it leave.
func xaftGuardYellow(t *testing.T) *sc.Machine[string, xaftElapsed] {
	t.Helper()

	// Starting in yellow leaves green unreachable, which sc reports unless
	// told to allow it.
	m, err := sc.New("guard_yellow", func(b *sc.Builder[string, xaftElapsed]) {
		b.Initial("yellow")
		xaftGuardGreen(b.State("green"))
		b.State("yellow").On[xaftTimer]().
			When("minTimeElapsed", func(c xaftElapsed, _ xaftTimer) bool { return xaftMinTimeElapsed(c.Elapsed) }).
			To("red")
		b.State("red")
	}, sc.AllowUnreachable())
	require.NoError(t, err)

	return m
}

// xaftGuardNoCondition is the machine of the "no condition is met" cases.
func xaftGuardNoCondition(t *testing.T) *sc.Machine[string, counter] {
	t.Helper()

	m, err := sc.New("no_condition", func(b *xaftBuilder) {
		b.Initial("a")
		a := b.State("a")
		a.On[xaftElapsedTimer]().
			When("over200", func(_ counter, ev xaftElapsedTimer) bool { return ev.Elapsed > 200 }).
			To("b")
		a.On[xaftElapsedTimer]().
			When("over100", func(_ counter, ev xaftElapsedTimer) bool { return ev.Elapsed > 100 }).
			To("c")
		b.State("b").Entry("enter", noop)
		b.State("c").Entry("enter", noop)
	})
	require.NoError(t, err)

	return m
}

func xaftNever(counter, struct{}) bool { return false }

// xaftGuardMatching is the machine of the "should allow a matching
// transition" cases. XState's root is parallel, so the chart is one of
// regions.
func xaftGuardMatching(t *testing.T) *sc.Machine[string, counter] {
	t.Helper()

	m, err := sc.New("matching", func(b *xaftBuilder) {
		b.Regions()
		b.State("A", func(b *xaftBuilder) {
			b.Initial("A2")
			b.State("A0")
			b.State("A2")
		})
		b.State("B", func(b *xaftBuilder) {
			b.Initial("B0")
			b0 := b.State("B0")
			b0.Always().When("never", xaftNever).To("B4")
			b0.On[xaftT2]().WhenIn("A2").To("B2")
			b.State("B1")
			b.State("B2")
			b.State("B4")
		})
	}, sc.AllowUnreachable())
	require.NoError(t, err)

	return m
}

// xaftGuardInterim is the machine of the "should check guards with interim
// states" cases.
func xaftGuardInterim(t *testing.T) *sc.Machine[string, counter] {
	t.Helper()

	m, err := sc.New("interim", func(b *xaftBuilder) {
		b.Regions()
		b.State("A", func(b *xaftBuilder) {
			b.Initial("A2")
			b.State("A2").On[xaftA]().To("A3")
			b.State("A3").Always().To("A4")
			b.State("A4").Always().To("A5")
			b.State("A5")
		})
		b.State("B", func(b *xaftBuilder) {
			b.Initial("B0")
			b.State("B0").Always().WhenIn("A4").To("B4")
			b.State("B4")
		})
	})
	require.NoError(t, err)

	return m
}

// xaftGuardSuite runs the cases the suites "guard conditions" and
// "[function] guard conditions" share. In XState 6 the two differ only in
// how the transition functions are written, which sc has one way to say.
func xaftGuardSuite(t *testing.T) {
	t.Helper()

	// should transition only if condition is met
	t.Run("ShouldTransitionOnlyIfConditionIsMet", func(t *testing.T) {
		t.Parallel()

		m := xaftGuardLight(t)

		snap, _ := m.Start(xaftElapsed{Elapsed: 50}, epoch)
		snap, _ = m.Transition(snap, xaftTimer{}, epoch)
		require.Equal(t, []string{"green"}, snap.Active())

		snap, _ = m.Start(xaftElapsed{Elapsed: 120}, epoch)
		snap, _ = m.Transition(snap, xaftTimer{}, epoch)
		require.Equal(t, []string{"yellow"}, snap.Active())
	})

	// should transition if condition based on event is met
	t.Run("ShouldTransitionIfConditionBasedOnEventIsMet", func(t *testing.T) {
		t.Parallel()

		m := xaftGuardLight(t)
		snap, _ := m.Start(xaftElapsed{Elapsed: 0}, epoch)
		snap, _ = m.Transition(snap, xaftEmergency{IsEmergency: true}, epoch)

		require.Equal(t, []string{"red"}, snap.Active())
	})

	// should not transition if condition based on event is not met
	t.Run("ShouldNotTransitionIfConditionBasedOnEventIsNotMet", func(t *testing.T) {
		t.Parallel()

		m := xaftGuardLight(t)
		snap, _ := m.Start(xaftElapsed{Elapsed: 0}, epoch)
		snap, _ = m.Transition(snap, xaftEmergency{IsEmergency: false}, epoch)

		require.Equal(t, []string{"green"}, snap.Active())
	})

	// should not transition if no condition is met
	t.Run("ShouldNotTransitionIfNoConditionIsMet", func(t *testing.T) {
		t.Parallel()

		m := xaftGuardNoCondition(t)
		snap, _ := m.Start(counter{N: 0}, epoch)
		next, effects := m.Transition(snap, xaftElapsedTimer{Elapsed: 10}, epoch)

		require.Equal(t, []string{"a"}, next.Active())
		require.Empty(t, effects)
		require.Equal(t, snap, next)
	})

	// should work with defined string transitions
	t.Run("ShouldWorkWithDefinedStringTransitions", func(t *testing.T) {
		t.Parallel()

		m := xaftGuardLight(t)
		snap, _ := m.Start(xaftElapsed{Elapsed: 120}, epoch)

		snap, _ = m.Transition(snap, xaftTimer{}, epoch)
		require.Equal(t, []string{"yellow"}, snap.Active())

		snap, _ = m.Transition(snap, xaftTimer{}, epoch)
		require.Equal(t, []string{"red"}, snap.Active())
	})

	// should work with guard objects
	t.Run("ShouldWorkWithGuardObjects", func(t *testing.T) {
		t.Parallel()

		m := xaftGuardLight(t)
		snap, _ := m.Start(xaftElapsed{Elapsed: 150}, epoch)

		snap, _ = m.Transition(snap, xaftTimer{}, epoch)
		require.Equal(t, []string{"yellow"}, snap.Active())

		snap, _ = m.Transition(snap, xaftTimerCondObj{}, epoch)
		require.Equal(t, []string{"red"}, snap.Active())
	})

	// should work with defined string transitions (condition not met)
	t.Run("ShouldWorkWithDefinedStringTransitionsConditionNotMet", func(t *testing.T) {
		t.Parallel()

		m := xaftGuardYellow(t)
		snap, _ := m.Start(xaftElapsed{Elapsed: 10}, epoch)
		snap, _ = m.Transition(snap, xaftTimer{}, epoch)

		require.Equal(t, []string{"yellow"}, snap.Active())
	})

	// should allow a matching transition
	//
	// XState skips this case (it.skip) in "[function] guard conditions".
	t.Run("ShouldAllowAMatchingTransition", func(t *testing.T) {
		t.Parallel()

		m := xaftGuardMatching(t)
		snap, _ := m.Start(counter{N: 0}, epoch)
		snap, _ = m.Transition(snap, xaftT2{}, epoch)

		require.Equal(t, []string{"A", "A2", "B", "B2"}, snap.Active())
	})

	// should check guards with interim states
	//
	// XState skips this case (it.skip) in "[function] guard conditions".
	t.Run("ShouldCheckGuardsWithInterimStates", func(t *testing.T) {
		t.Parallel()

		m := xaftGuardInterim(t)
		snap, _ := m.Start(counter{N: 0}, epoch)
		snap, _ = m.Transition(snap, xaftA{}, epoch)

		require.Equal(t, []string{"A", "A5", "B", "B4"}, snap.Active())
	})
}

// XState: guard conditions > (every case but "should guard against
// transition", which follows)
func TestXAftGuardsGuardConditions(t *testing.T) {
	t.Parallel()

	xaftGuardSuite(t)
}

// XState: guard conditions > should guard against transition
func TestXAftGuardsGuardConditionsShouldGuardAgainstTransition(t *testing.T) {
	t.Parallel()

	m, err := sc.New("guard_against", func(b *xaftBuilder) {
		b.Regions()
		b.State("A", func(b *xaftBuilder) {
			b.Initial("A2")
			b.State("A0")
			b.State("A2")
		})
		b.State("B", func(b *xaftBuilder) {
			b.Initial("B0")
			b0 := b.State("B0")
			b0.Always().When("never", xaftNever).To("B4")
			b0.On[xaftT1]().When("never", func(counter, xaftT1) bool { return false }).To("B1")
			b.State("B1")
			b.State("B4")
		})
	}, sc.AllowUnreachable())
	require.NoError(t, err)

	snap, _ := m.Start(counter{N: 0}, epoch)
	snap, _ = m.Transition(snap, xaftT1{}, epoch)

	require.Equal(t, []string{"A", "A2", "B", "B0"}, snap.Active())
}

// XState: [function] guard conditions > (every case, including the two XState
// skips)
func TestXAftGuardsFunctionGuardConditions(t *testing.T) {
	t.Parallel()

	xaftGuardSuite(t)
}

// xaftCustomGuardParams are the params of XState's customGuard.
type xaftCustomGuardParams struct {
	Op      string
	Compare int
}

// XState: custom guards > should evaluate custom guards
func TestXAftGuardsCustomGuards(t *testing.T) {
	t.Parallel()

	custom := func(params xaftCustomGuardParams) func(counter, xaftEvent) bool {
		return func(c counter, ev xaftEvent) bool {
			if params.Op == "greaterThan" {
				return c.N+ev.Value > params.Compare
			}

			return false
		}
	}

	m, err := sc.New("custom", func(b *xaftBuilder) {
		b.Initial("inactive")
		b.State("inactive").On[xaftEvent]().
			When("custom", custom(xaftCustomGuardParams{Op: "greaterThan", Compare: 3})).
			To("active")
		b.State("active")
	})
	require.NoError(t, err)

	snap, _ := m.Start(counter{N: 0}, epoch)
	pass, _ := m.Transition(snap, xaftEvent{Value: 4}, epoch)
	require.Equal(t, []string{"active"}, pass.Active())

	fail, _ := m.Transition(snap, xaftEvent{Value: 3}, epoch)
	require.Equal(t, []string{"inactive"}, fail.Active())
}

// XState: guards - other > should allow for a fallback target to be a simple
// string
func TestXAftGuardsFallbackTarget(t *testing.T) {
	t.Parallel()

	m, err := sc.New("fallback", func(b *xaftBuilder) {
		b.Initial("a")
		a := b.State("a")
		a.On[xaftAny]().When("never", func(counter, xaftAny) bool { return false }).To("b")
		a.On[xaftAny]().To("c")
		b.State("b")
		b.State("c")
	})
	require.NoError(t, err)

	snap, _ := m.Start(counter{N: 0}, epoch)
	snap, _ = m.Transition(snap, xaftAny{}, epoch)

	require.Equal(t, []string{"c"}, snap.Active())
}

// XState: guards - plain function sources > passes only the caller-supplied
// params to the source
func TestXAftGuardsPlainFunctionSourcesPassOnlyCallerSuppliedParams(t *testing.T) {
	t.Parallel()

	var received [][]int
	isAbove := func(count, threshold int) bool {
		received = append(received, []int{count, threshold})
		return count > threshold
	}

	m, err := sc.New("plain", func(b *xaftBuilder) {
		b.Initial("a")
		b.State("a").On[xaftEv]().
			When("isAbove", func(c counter, _ xaftEv) bool { return isAbove(c.N, 3) }).
			To("b")
		b.State("b")
	})
	require.NoError(t, err)

	snap, _ := m.Start(counter{N: 5}, epoch)
	snap, _ = m.Transition(snap, xaftEv{}, epoch)

	require.Equal(t, []string{"b"}, snap.Active())
	require.Equal(t, [][]int{{5, 3}}, received)
}

// XState: guards - plain function sources > supports zero-param guards
func TestXAftGuardsPlainFunctionSourcesSupportZeroParamGuards(t *testing.T) {
	t.Parallel()

	isEnabled := func() bool { return true }

	m, err := sc.New("zero_param", func(b *xaftBuilder) {
		b.Initial("a")
		b.State("a").On[xaftEv]().
			When("isEnabled", func(counter, xaftEv) bool { return isEnabled() }).
			To("b")
		b.State("b")
	})
	require.NoError(t, err)

	snap, _ := m.Start(counter{N: 0}, epoch)
	snap, _ = m.Transition(snap, xaftEv{}, epoch)

	require.Equal(t, []string{"b"}, snap.Active())
}

// xaftForms are the three form charts of "guards - enqueue before return".
type xaftForms struct {
	flat         *sc.Machine[string, xaftValid]
	withWildcard *sc.Machine[string, xaftValid]
	withParent   *sc.Machine[string, xaftValid]
}

// xaftCreateForms is XState's createForms. Its submit function either
// returns before it enqueues anything, which rejects the event, or enqueues
// "track" first, which selects the transition whatever it then returns. sc
// separates guards from actions, so the second form is two candidates: the
// guarded one that leaves for "sent", and a fallback that only tracks.
func xaftCreateForms(t *testing.T, enqueueFirst bool) xaftForms {
	t.Helper()

	isValid := func(c xaftValid, _ xaftSubmit) bool { return c.Valid }
	submit := func(editing *sc.StateBuilder[string, xaftValid]) {
		if !enqueueFirst {
			editing.On[xaftSubmit]().When("valid", isValid).To("sent")
			return
		}

		editing.On[xaftSubmit]().When("valid", isValid).Do("trackAndSend", xaftDo[xaftValid, xaftSubmit]).To("sent")
		editing.On[xaftSubmit]().Do("track", xaftDo[xaftValid, xaftSubmit])
	}

	flat, err := sc.New("flat", func(b *sc.Builder[string, xaftValid]) {
		b.Initial("editing")
		submit(b.State("editing"))
		b.State("sent")
	})
	require.NoError(t, err)

	// XState's 'form.*' is a partial wildcard; the only event sent is
	// form.submit, so OnAny stands for it.
	withWildcard, err := sc.New("with_wildcard", func(b *sc.Builder[string, xaftValid]) {
		b.Initial("editing")
		editing := b.State("editing")
		submit(editing)
		editing.OnAny().Do("wildcard", xaftDo[xaftValid, any])
		b.State("sent")
	})
	require.NoError(t, err)

	withParent, err := sc.New("with_parent", func(b *sc.Builder[string, xaftValid]) {
		b.Initial("form")
		b.State("form", func(b *sc.Builder[string, xaftValid]) {
			b.Initial("editing")
			submit(b.State("editing"))
			b.State("sent")
		}).On[xaftSubmit]().Do("parent", xaftDo[xaftValid, xaftSubmit])
	})
	require.NoError(t, err)

	return xaftForms{flat: flat, withWildcard: withWildcard, withParent: withParent}
}

// XState: guards - enqueue before return > leaves the event unhandled when
// the function returns before calling enq
func TestXAftGuardsEnqueueBeforeReturnLeavesEventUnhandled(t *testing.T) {
	t.Parallel()

	forms := xaftCreateForms(t, false)
	invalid := xaftValid{Valid: false}

	snap, _ := forms.flat.Start(invalid, epoch)
	require.False(t, forms.flat.Can(snap, xaftSubmit{}, epoch))

	// XState's onUnhandledEvent is sc returning the snapshot it was given.
	next, effects := forms.flat.Transition(snap, xaftSubmit{}, epoch)
	require.Equal(t, snap, next)
	require.Equal(t, []string{"editing"}, next.Active())
	require.Empty(t, effects)

	snap, _ = forms.withWildcard.Start(invalid, epoch)
	_, effects = forms.withWildcard.Transition(snap, xaftSubmit{}, epoch)
	require.Equal(t, []string{"editing/wildcard"}, xaftActions(effects))

	snap, _ = forms.withParent.Start(invalid, epoch)
	_, effects = forms.withParent.Transition(snap, xaftSubmit{}, epoch)
	require.Equal(t, []string{"form/parent"}, xaftActions(effects))
}

// XState: guards - enqueue before return > selects the transition when the
// function calls enq, even if it then returns undefined
func TestXAftGuardsEnqueueBeforeReturnSelectsTransition(t *testing.T) {
	t.Parallel()

	forms := xaftCreateForms(t, true)
	invalid := xaftValid{Valid: false}

	snap, _ := forms.flat.Start(invalid, epoch)
	require.True(t, forms.flat.Can(snap, xaftSubmit{}, epoch))

	next, effects := forms.flat.Transition(snap, xaftSubmit{}, epoch)
	require.NotEqual(t, snap.Seq(), next.Seq())
	require.Equal(t, []string{"editing"}, next.Active())
	require.Equal(t, []string{"editing/track"}, xaftActions(effects))

	snap, _ = forms.withWildcard.Start(invalid, epoch)
	_, effects = forms.withWildcard.Transition(snap, xaftSubmit{}, epoch)
	require.Equal(t, []string{"editing/track"}, xaftActions(effects))

	snap, _ = forms.withParent.Start(invalid, epoch)
	_, effects = forms.withParent.Transition(snap, xaftSubmit{}, epoch)
	require.Equal(t, []string{"editing/track"}, xaftActions(effects))
}

// XState: self.getSnapshot() inside a transition > returns the snapshot
// committed before the macrostep
//
// An sc guard has no handle on its instance. The place a chart can read its
// own instance is an action under a driver, so the two transition functions
// become actions on the same transitions, read through Instance.Snapshot.
func TestXAftGuardsSelfGetSnapshotReturnsSnapshotCommittedBeforeMacrostep(t *testing.T) {
	t.Parallel()

	var inst *sc.Instance[string, counter]
	seen := map[string][]string{}

	m, err := sc.New("self_snapshot", func(b *xaftBuilder) {
		b.Initial("a")
		b.State("a").On[xaftGo]().To("mid")
		b.State("mid").Always().
			Do("seeAlways", func(context.Context, counter, struct{}) { seen["always"] = inst.Snapshot().Active() }).
			To("b")
		sb := b.State("b").RaiseOnEntry("raiseCheck", func(counter) any { return xaftCheck{} })
		sb.On[xaftCheck]().
			Do("seeRaised", func(context.Context, counter, xaftCheck) { seen["raised"] = inst.Snapshot().Active() })
	})
	require.NoError(t, err)

	inst, err = m.Instance(t.Context(), counter{N: 0})
	require.NoError(t, err)
	defer inst.Close()

	_, err = inst.Send(t.Context(), xaftGo{})
	require.NoError(t, err)

	require.Equal(t, map[string][]string{"always": {"a"}, "raised": {"a"}}, seen)
	require.Equal(t, []string{"b"}, inst.Snapshot().Active())
}

// xaftInChart is the machine the first four stateIn cases share: region "a"
// leaves a1 for a2 on Ev when region "b" is in b2. XState's root is
// parallel, so the chart is one of regions, and a state's identifier stands
// for its path, its id and its state value alike, since sc names every state
// once.
func xaftInChart[Ev any](t *testing.T, bInitial, fooInitial string) *sc.Machine[string, counter] {
	t.Helper()

	m, err := sc.New("in_check", func(b *xaftBuilder) {
		b.Regions()
		b.State("a", func(b *xaftBuilder) {
			b.Initial("a1")
			b.State("a1").On[Ev]().WhenIn("b2").To("a2")
			b.State("a2")
		})
		b.State("b", func(b *xaftBuilder) {
			b.Initial(bInitial)
			b.State("b1")
			b.Parallel("b2", func(b *xaftBuilder) {
				b.State("foo", func(b *xaftBuilder) {
					b.Initial(fooInitial)
					b.State("foo1")
					b.State("foo2")
				})
				b.State("bar", func(b *xaftBuilder) {
					b.Initial("bar1")
					b.State("bar1")
					b.State("bar2")
				})
			})
		})
	}, sc.AllowUnreachable())
	require.NoError(t, err)

	return m
}

// xaftDeepChart is the machine of the two "relative to grandparent" cases:
// foo1 leaves for foo2 on EVENT_DEEP when bar1 is active.
func xaftDeepChart(t *testing.T, barInitial string) *sc.Machine[string, counter] {
	t.Helper()

	m, err := sc.New("in_deep", func(b *xaftBuilder) {
		b.Regions()
		b.State("a", func(b *xaftBuilder) {
			b.Initial("a1")
			b.State("a1")
			b.State("a2")
		})
		b.State("b", func(b *xaftBuilder) {
			b.Initial("b2")
			b.State("b1")
			b.Parallel("b2", func(b *xaftBuilder) {
				b.State("foo", func(b *xaftBuilder) {
					b.Initial("foo1")
					b.State("foo1").On[xaftEventDeep]().WhenIn("bar1").To("foo2")
					b.State("foo2")
				})
				b.State("bar", func(b *xaftBuilder) {
					b.Initial(barInitial)
					b.State("bar1")
					b.State("bar2")
				})
			})
		})
	}, sc.AllowUnreachable())
	require.NoError(t, err)

	return m
}

// XState: transition "in" check > should transition if string state path
// matches current state value
func TestXAftStateInTransitionsIfStringStatePathMatches(t *testing.T) {
	t.Parallel()

	m := xaftInChart[xaftEvent2](t, "b2", "foo2")
	snap, _ := m.Start(counter{N: 0}, epoch)
	snap, _ = m.Transition(snap, xaftEvent2{}, epoch)

	require.Equal(t, []string{"a", "a2", "b", "b2", "foo", "foo2", "bar", "bar1"}, snap.Active())
}

// XState: transition "in" check > should transition if state node ID matches
// current state value
func TestXAftStateInTransitionsIfStateNodeIDMatches(t *testing.T) {
	t.Parallel()

	m := xaftInChart[xaftEvent3](t, "b2", "foo2")
	snap, _ := m.Start(counter{N: 0}, epoch)
	snap, _ = m.Transition(snap, xaftEvent3{}, epoch)

	require.Equal(t, []string{"a", "a2", "b", "b2", "foo", "foo2", "bar", "bar1"}, snap.Active())
}

// XState: transition "in" check > should not transition if string state path
// does not match current state value
func TestXAftStateInDoesNotTransitionIfStringStatePathDoesNotMatch(t *testing.T) {
	t.Parallel()

	m := xaftInChart[xaftEvent1](t, "b1", "foo1")
	snap, _ := m.Start(counter{N: 0}, epoch)
	next, _ := m.Transition(snap, xaftEvent1{}, epoch)

	require.Equal(t, []string{"a", "a1", "b", "b1"}, next.Active())
	require.Equal(t, snap, next)
}

// XState: transition "in" check > should not transition if state value
// matches current state value
//
// Despite its title, the XState case expects the transition to be taken.
func TestXAftStateInStateValueMatchesCurrentStateValue(t *testing.T) {
	t.Parallel()

	m := xaftInChart[xaftEvent2](t, "b2", "foo2")
	snap, _ := m.Start(counter{N: 0}, epoch)
	snap, _ = m.Transition(snap, xaftEvent2{}, epoch)

	require.Equal(t, []string{"a", "a2", "b", "b2", "foo", "foo2", "bar", "bar1"}, snap.Active())
}

// XState: transition "in" check > matching should be relative to grandparent
// (match)
func TestXAftStateInMatchingRelativeToGrandparentMatch(t *testing.T) {
	t.Parallel()

	m := xaftDeepChart(t, "bar1")
	snap, _ := m.Start(counter{N: 0}, epoch)
	snap, _ = m.Transition(snap, xaftEventDeep{}, epoch)

	require.Equal(t, []string{"a", "a1", "b", "b2", "foo", "foo2", "bar", "bar1"}, snap.Active())
}

// XState: transition "in" check > matching should be relative to grandparent
// (no match)
func TestXAftStateInMatchingRelativeToGrandparentNoMatch(t *testing.T) {
	t.Parallel()

	m := xaftDeepChart(t, "bar2")
	snap, _ := m.Start(counter{N: 0}, epoch)
	snap, _ = m.Transition(snap, xaftEventDeep{}, epoch)

	require.Equal(t, []string{"a", "a1", "b", "b2", "foo", "foo1", "bar", "bar2"}, snap.Active())
}

// XState: transition "in" check > should work to forbid events
func TestXAftStateInWorksToForbidEvents(t *testing.T) {
	t.Parallel()

	m, err := sc.New("forbid", func(b *xaftBuilder) {
		b.Initial("green")
		b.State("green").On[xaftTimer]().To("yellow")
		b.State("yellow").On[xaftTimer]().To("red")
		b.State("red", func(b *xaftBuilder) {
			b.Initial("walk")
			b.State("walk").On[xaftTimer]().To("wait")
			b.State("wait").On[xaftTimer]().To("stop")
			b.State("stop")
		}).On[xaftTimer]().WhenIn("stop").To("green")
	})
	require.NoError(t, err)

	snap, _ := m.Start(counter{N: 0}, epoch)

	for range 3 {
		snap, _ = m.Transition(snap, xaftTimer{}, epoch)
	}
	require.Equal(t, []string{"red", "wait"}, snap.Active())

	snap, _ = m.Transition(snap, xaftTimer{}, epoch)
	require.Equal(t, []string{"red", "stop"}, snap.Active())

	snap, _ = m.Transition(snap, xaftTimer{}, epoch)
	require.Equal(t, []string{"green"}, snap.Active())
}

// XState: transition "in" check > should be possible to use a referenced
// `stateIn` guard
//
// XState names the guard hasSelection and refers to it; WhenIn is the only
// form sc has, so the reference is a Go function that applies it.
func TestXAftStateInReferencedStateInGuard(t *testing.T) {
	t.Parallel()

	hasSelection := func(t *sc.Transition[string, counter, xaftNext]) *sc.Transition[string, counter, xaftNext] {
		return t.WhenIn("selected")
	}

	m, err := sc.New("referenced", func(b *xaftBuilder) {
		b.Regions()
		b.State("selected")
		b.State("location", func(b *xaftBuilder) {
			b.Initial("home")
			hasSelection(b.State("home").On[xaftNext]()).To("success")
			b.State("success")
		})
	})
	require.NoError(t, err)

	snap, _ := m.Start(counter{N: 0}, epoch)
	snap, _ = m.Transition(snap, xaftNext{}, epoch)

	require.Equal(t, []string{"selected", "location", "success"}, snap.Active())
}

// XState: transition "in" check > should be possible to check an ID with a
// path
//
// XState skips this case (it.skip): '#b.B1' mixes an id with a path. sc names
// B1 directly, so it is ported.
func TestXAftStateInChecksAnIDWithAPath(t *testing.T) {
	t.Parallel()

	m, err := sc.New("id_path", func(b *xaftBuilder) {
		b.Regions()
		b.State("A", func(b *xaftBuilder) {
			b.Initial("A1")
			b.State("A1").On[xaftMyEvent]().WhenIn("B1").Do("spy", xaftDo[counter, xaftMyEvent])
		})
		b.State("B", func(b *xaftBuilder) {
			b.Initial("B1")
			b.State("B1")
		})
	})
	require.NoError(t, err)

	snap, _ := m.Start(counter{N: 0}, epoch)
	_, effects := m.Transition(snap, xaftMyEvent{}, epoch)

	require.Equal(t, []string{"A1/spy"}, xaftActions(effects))
}

// XState: predictableExec > should call mixed custom and builtin actions in
// the definitions order
func TestXAftExecCallsMixedCustomAndBuiltinActionsInDefinitionOrder(t *testing.T) {
	t.Parallel()

	m, err := sc.New("mixed", func(b *xaftBuilder) {
		b.Initial("a")
		b.State("a").On[xaftNext]().To("b")
		b.State("b").Entry("custom", noop).Entry("assign", noop)
	})
	require.NoError(t, err)

	snap, _ := m.Start(counter{N: 0}, epoch)
	_, effects := m.Transition(snap, xaftNext{}, epoch)

	require.Equal(t, []string{"b/custom", "b/assign"}, xaftActions(effects))
}

// XState: predictableExec > should call initial custom actions when starting
// a service
//
// The entry action is the machine's own, as in XState, whose machine has no
// states.
func TestXAftExecCallsInitialCustomActionsWhenStarting(t *testing.T) {
	t.Parallel()

	called := false

	m, err := sc.New("initial_actions", func(b *xaftBuilder) {
		b.Chart().Entry("call", func(context.Context, counter) { called = true })
	})
	require.NoError(t, err)
	require.False(t, called)

	// The core returns the action; a driver runs it.
	_, effects := m.Start(counter{N: 0}, epoch)
	require.Equal(t, []string{"initial_actions/call"}, xaftActions(effects))
	require.False(t, called)

	require.NoError(t, m.Exec(t.Context(), effects[0], func(any) {}))
	require.True(t, called)
}

// XState: predictableExec > should resolve initial assign actions before
// starting a service
//
// XState reads the context of an actor that has not been started. An sc
// instance has no snapshot before Start, whose result already holds the
// assignment while its actions are still to be run.
func TestXAftExecResolvesInitialAssignActionsBeforeStarting(t *testing.T) {
	t.Parallel()

	m, err := sc.New("initial_assign", func(b *sc.Builder[string, xaftFlag]) {
		b.Chart().AssignOnEntry("call", func(c xaftFlag) xaftFlag {
			c.Flag = true
			return c
		})
	})
	require.NoError(t, err)

	snap, _ := m.Start(xaftFlag{Flag: false}, epoch)
	require.True(t, snap.Context.Flag)
}

// xaftRaisedChart is the machine of the two "raised transition ... actions
// with raised event" cases, which are the same in XState 6.
func xaftRaisedChart(t *testing.T) *sc.Machine[string, counter] {
	t.Helper()

	m, err := sc.New("raised", func(b *xaftBuilder) {
		b.Initial("a")
		b.State("a").On[xaftNext]().To("b")
		sb := b.State("b").RaiseOnEntry("raise", func(counter) any { return xaftRaised{} })
		sb.On[xaftRaised]().Do("capture", xaftDo[counter, xaftRaised]).To("c")
		b.State("c")
	})
	require.NoError(t, err)

	return m
}

// XState: predictableExec > should call raised transition custom actions with
// raised event
func TestXAftExecCallsRaisedTransitionCustomActionsWithRaisedEvent(t *testing.T) {
	t.Parallel()

	m := xaftRaisedChart(t)
	snap, _ := m.Start(counter{N: 0}, epoch)
	snap, effects := m.Transition(snap, xaftNext{}, epoch)

	require.Equal(t, []string{"c"}, snap.Active())
	require.Equal(t, []string{"b/capture"}, xaftActions(effects))
	require.Equal(t, xaftRaised{}, effect(t, effects, sc.EffectAction).Event)
}

// XState: predictableExec > should call raised transition builtin actions
// with raised event
func TestXAftExecCallsRaisedTransitionBuiltinActionsWithRaisedEvent(t *testing.T) {
	t.Parallel()

	m := xaftRaisedChart(t)
	snap, _ := m.Start(counter{N: 0}, epoch)
	snap, effects := m.Transition(snap, xaftNext{}, epoch)

	require.Equal(t, []string{"c"}, snap.Active())
	require.Equal(t, []string{"b/capture"}, xaftActions(effects))
	require.Equal(t, xaftRaised{}, effect(t, effects, sc.EffectAction).Event)
}

func xaftIdle(context.Context, counter) (struct{}, error) { return struct{}{}, nil }

// XState: predictableExec > should call invoke creator with raised event
func TestXAftExecCallsInvokeCreatorWithRaisedEvent(t *testing.T) {
	t.Parallel()

	m, err := sc.New("invoke_creator", func(b *xaftBuilder) {
		b.Initial("a")
		b.State("a").On[xaftNext]().To("b")
		b.State("b").RaiseOnEntry("raise", func(counter) any { return xaftRaised{} }).
			On[xaftRaised]().To("c")
		b.State("c").Invoke("creator", xaftIdle).OnError().Stay()
	})
	require.NoError(t, err)

	snap, _ := m.Start(counter{N: 0}, epoch)
	snap, effects := m.Transition(snap, xaftNext{}, epoch)
	require.Equal(t, []string{"c"}, snap.Active())

	// As in XState, the invoked service is given the event that entered
	// its state, here the raised one: the effect carries it, and the
	// service reads it with sc.Trigger.
	start := effect(t, effects, sc.EffectStartInvocation)
	require.Equal(t, "c", start.State)
	require.Equal(t, xaftRaised{}, start.Event)
}

// xaftChildChart is the machine of the two "invoked child" cases.
func xaftChildChart(t *testing.T) *sc.Machine[string, counter] {
	t.Helper()

	m, err := sc.New("invoked_child", func(b *xaftBuilder) {
		b.Initial("a")
		b.State("a").On[xaftNext]().To("b")
		sb := b.State("b")
		sb.Invoke("myChild", xaftIdle).OnError().Stay()
		sb.On[xaftNext]().To("c")
		b.State("c")
	})
	require.NoError(t, err)

	return m
}

// XState: predictableExec > invoked child should be available on the new
// state
func TestXAftExecInvokedChildIsAvailableOnTheNewState(t *testing.T) {
	t.Parallel()

	m := xaftChildChart(t)
	snap, _ := m.Start(counter{N: 0}, epoch)
	snap, _ = m.Transition(snap, xaftNext{}, epoch)

	services := snap.Services()
	require.Len(t, services, 1)
	require.Equal(t, "myChild", services[0].Name)
	require.Equal(t, "b", services[0].State)
}

// XState: predictableExec > invoked child should not be available on the
// state after leaving invoking state
func TestXAftExecInvokedChildIsNotAvailableAfterLeavingInvokingState(t *testing.T) {
	t.Parallel()

	m := xaftChildChart(t)
	snap, _ := m.Start(counter{N: 0}, epoch)
	snap, _ = m.Transition(snap, xaftNext{}, epoch)
	snap, effects := m.Transition(snap, xaftNext{}, epoch)

	require.Empty(t, snap.Services())
	require.Equal(t, []sc.EffectKind{sc.EffectStopInvocation}, kinds(effects))
}

// XState: predictableExec > should correctly provide intermediate context
// value to a custom action executed in between assign actions
func TestXAftExecProvidesIntermediateContextToActionBetweenAssigns(t *testing.T) {
	t.Parallel()

	set := func(n int) func(counter) counter {
		return func(c counter) counter {
			c.N = n
			return c
		}
	}

	m, err := sc.New("intermediate", func(b *xaftBuilder) {
		b.Initial("a")
		b.State("a").On[xaftNext]().To("b")
		b.State("b").
			AssignOnEntry("one", set(1)).
			Entry("capture", noop).
			AssignOnEntry("two", set(2))
	})
	require.NoError(t, err)

	snap, _ := m.Start(counter{N: 0}, epoch)
	snap, effects := m.Transition(snap, xaftNext{}, epoch)
	require.Equal(t, 2, snap.Context.N)

	// DIVERGES from XState: XState expects the action enqueued between the
	// two context values to be called with the first, 1. sc runs every
	// AssignOnEntry of a state before any of its entry actions, whatever
	// order they are declared in, so the action sees the last, 2.
	require.Equal(t, 2, effect(t, effects, sc.EffectAction).Context.N)

	// The steps of a transition do run in the order they are declared, so
	// the same sequence placed there gives XState's answer.
	onTransition, err := sc.New("intermediate_steps", func(b *xaftBuilder) {
		b.Initial("a")
		b.State("a").On[xaftNext]().
			Assign("one", func(c counter, _ xaftNext) counter { return set(1)(c) }).
			Do("capture", xaftDo[counter, xaftNext]).
			Assign("two", func(c counter, _ xaftNext) counter { return set(2)(c) }).
			To("b")
		b.State("b")
	})
	require.NoError(t, err)

	snap, _ = onTransition.Start(counter{N: 0}, epoch)
	snap, effects = onTransition.Transition(snap, xaftNext{}, epoch)
	require.Equal(t, 2, snap.Context.N)
	require.Equal(t, 1, effect(t, effects, sc.EffectAction).Context.N)
}

// XState: predictableExec > initial actions should receive context updated
// only by preceding assign actions
func TestXAftExecInitialActionsReceiveContextUpdatedOnlyByPrecedingAssigns(t *testing.T) {
	t.Parallel()

	inc := func(c counter) counter {
		c.N++
		return c
	}

	m, err := sc.New("preceding", func(b *xaftBuilder) {
		b.Chart().
			Entry("count0", noop).
			AssignOnEntry("inc1", inc).
			Entry("count1", noop).
			AssignOnEntry("inc2", inc).
			Entry("count2", noop)
	})
	require.NoError(t, err)

	snap, effects := m.Start(counter{N: 0}, epoch)
	require.Equal(t, 2, snap.Context.N)
	require.Equal(t, []string{"preceding/count0", "preceding/count1", "preceding/count2"}, xaftActions(effects))

	actual := make([]int, 0, len(effects))
	for _, e := range effects {
		actual = append(actual, e.Context.N)
	}

	// DIVERGES from XState: XState expects [0, 1, 2], each action seeing
	// only the assignments before it. sc runs every AssignOnEntry of a state
	// before any of its entry actions, so all three see the final context.
	require.Equal(t, []int{2, 2, 2}, actual)
}

// xaftReadingParent is the parent of the "read the updated state of a child"
// cases. As in XState, the machine itself invokes the child, and the
// transition for the child's event picks its target by where the child is.
func xaftReadingParent(t *testing.T, child *sc.Machine[string, counter]) *sc.Machine[string, counter] {
	t.Helper()

	m, err := sc.New("reading_parent", func(b *xaftBuilder) {
		b.Chart().InvokeMachine("myChild", child, xaftSameContext, xaftNoResult[string, counter]).OnError().Stay()

		b.Initial("initial")
		initial := b.State("initial")
		initial.On[xaftChildUpdated]().WhenChildIn("myChild", "b").To("success")
		initial.On[xaftChildUpdated]().To("fail")
		b.Final("success")
		b.Final("fail")
	})
	require.NoError(t, err)

	return m
}

// xaftCheckReadingParent runs the parent to its end and checks that it found
// its child in "b".
func xaftCheckReadingParent(t *testing.T, child *sc.Machine[string, counter]) {
	t.Helper()

	inst, err := xaftReadingParent(t, child).Instance(t.Context(), counter{N: 0})
	require.NoError(t, err)
	defer inst.Close()

	// Once the child has started, the parent's snapshot holds the child's,
	// read by its service's name.
	synctest.Wait()

	started, ok := inst.Snapshot().Child(child, "myChild")
	require.True(t, ok)
	require.Equal(t, []string{"a"}, started.Active())

	snap, err := inst.Await(t.Context(), func(s sc.Snapshot[string, counter]) bool { return s.Done() })
	require.NoError(t, err)

	// As in XState, the parent finds its child already in "b" when the
	// child's event arrives: the snapshot of the step that emitted it is
	// handed up first.
	require.Equal(t, []string{"success"}, snap.Active())
}

// XState: predictableExec > parent should be able to read the updated state
// of a child when receiving an event from it
func TestXAftExecParentReadsUpdatedStateOfChildOnEventFromIt(t *testing.T) {
	t.Parallel()

	synctest.Test(t, func(t *testing.T) {
		child, err := sc.New("updating_child", func(b *xaftBuilder) {
			b.Initial("a")
			b.State("a").After(time.Millisecond).To("b")
			b.State("b").EmitOnEntry("childUpdated", func(counter) xaftChildUpdated { return xaftChildUpdated{} })
		})
		require.NoError(t, err)

		xaftCheckReadingParent(t, child)
	})
}

// XState: predictableExec > should be possible to send immediate events to
// initially invoked actors
func TestXAftExecSendsImmediateEventsToInitiallyInvokedActors(t *testing.T) {
	t.Parallel()

	m := xaftPonger(t, func(waiting *sc.StateBuilder[string, counter]) {
		waiting.SendOnEntry("ping", "ponger", func(counter) any { return xaftPing{} })
	})

	// The send comes after the start of the service it is for.
	_, effects := m.Start(counter{N: 0}, epoch)
	require.Equal(t, []sc.EffectKind{sc.EffectStartInvocation, sc.EffectSend}, kinds(effects))
	require.Equal(t, xaftPing{}, effects[1].Event)

	// Under a driver the child answers and the machine finishes without
	// any time passing. XState sees "done" as start returns; an sc child
	// runs on goroutines of its own, so the test waits for them to rest.
	synctest.Test(t, func(t *testing.T) {
		inst, err := m.Instance(t.Context(), counter{N: 0})
		require.NoError(t, err)
		defer inst.Close()

		started := time.Now()
		synctest.Wait()

		snap := inst.Snapshot()
		require.Equal(t, []string{"done"}, snap.Active())
		require.True(t, snap.Done())
		require.Equal(t, started, time.Now())
	})
}

// xaftPonger is the parent of the "immediate events" cases: it invokes a
// child that answers PING with PONG, and finishes on PONG. send declares how
// "waiting" sends the PING. The child has no states, as in XState.
func xaftPonger(t *testing.T, send func(waiting *sc.StateBuilder[string, counter])) *sc.Machine[string, counter] {
	t.Helper()

	child, err := sc.New("ponger", func(b *xaftBuilder) {
		b.On[xaftPing]().Emit("pong", func(counter, xaftPing) xaftPong { return xaftPong{} })
	})
	require.NoError(t, err)

	m, err := sc.New("pinger", func(b *xaftBuilder) {
		b.Initial("waiting")
		waiting := b.State("waiting")
		waiting.InvokeMachine("ponger", child, xaftSameContext, xaftNoResult[string, counter]).OnError().Stay()
		send(waiting)
		waiting.On[xaftPong]().To("done")
		b.Final("done")
	})
	require.NoError(t, err)

	return m
}

// XState: predictableExec > should create invoke based on context updated by
// entry actions of the same state
//
// XState skips this case (it.skip). sc starts a state's services after its
// entry assignments, so it is ported.
func TestXAftExecCreatesInvokeBasedOnContextUpdatedByEntryActions(t *testing.T) {
	t.Parallel()

	m, err := sc.New("invoke_context", func(b *sc.Builder[string, xaftFlag]) {
		b.Initial("a")
		b.State("a").On[xaftNext]().To("b")
		b.State("b").
			AssignOnEntry("update", func(c xaftFlag) xaftFlag {
				c.Flag = true
				return c
			}).
			Invoke("check", func(context.Context, xaftFlag) (struct{}, error) { return struct{}{}, nil }).
			OnError().Stay()
	})
	require.NoError(t, err)

	snap, _ := m.Start(xaftFlag{Flag: false}, epoch)
	_, effects := m.Transition(snap, xaftNext{}, epoch)

	require.True(t, effect(t, effects, sc.EffectStartInvocation).Context.Flag)
}

// XState: predictableExec > should deliver events sent from the entry actions
// to a service invoked in the same state
func TestXAftExecDeliversEventsSentFromEntryActionsToServiceInvokedInSameState(t *testing.T) {
	t.Parallel()

	received := make(chan any, 1)

	child, err := sc.New("knocked", func(b *xaftBuilder) {
		b.OnAny().Do("receive", func(_ context.Context, _ counter, ev any) { received <- ev })
	})
	require.NoError(t, err)

	m, err := sc.New("knocker", func(b *xaftBuilder) {
		b.Initial("a")
		b.State("a").On[xaftNext]().To("b")
		sb := b.State("b").SendOnEntry("knockKnock", "myChild", func(counter) any { return xaftKnock{} })
		sb.InvokeMachine("myChild", child, xaftSameContext, xaftNoResult[string, counter]).OnError().Stay()
	})
	require.NoError(t, err)

	// The send comes after the start of the service it is for.
	snap, _ := m.Start(counter{N: 0}, epoch)
	_, effects := m.Transition(snap, xaftNext{}, epoch)
	require.Equal(t, []sc.EffectKind{sc.EffectStartInvocation, sc.EffectSend}, kinds(effects))
	require.Equal(t, xaftKnock{}, effects[1].Event)

	// Under a driver the child receives it.
	synctest.Test(t, func(t *testing.T) {
		inst, err := m.Instance(t.Context(), counter{N: 0})
		require.NoError(t, err)
		defer inst.Close()

		_, err = inst.Send(t.Context(), xaftNext{})
		require.NoError(t, err)
		synctest.Wait()

		select {
		case ev := <-received:
			require.Equal(t, xaftKnock{}, ev)
		default:
			require.Fail(t, "the child did not receive KNOCK_KNOCK")
		}
	})
}

// XState: predictableExec > parent should be able to read the updated state
// of a child when receiving an event from it (the second case of that name)
//
// Here XState's child tells its parent a millisecond after entering "b",
// which in sc is a delayed transition of "b" that emits.
func TestXAftExecParentReadsUpdatedStateOfChildOnDeferredEventFromIt(t *testing.T) {
	t.Parallel()

	synctest.Test(t, func(t *testing.T) {
		child, err := sc.New("deferring_child", func(b *xaftBuilder) {
			b.Initial("a")
			b.State("a").After(time.Millisecond).To("b")
			b.State("b").After(time.Millisecond).
				Emit("childUpdated", func(counter, struct{}) xaftChildUpdated { return xaftChildUpdated{} })
		})
		require.NoError(t, err)

		xaftCheckReadingParent(t, child)
	})
}

// XState: predictableExec > should be possible to send immediate events to
// initially invoked actors (the second case of that name)
//
// Here XState's entry action sends the PING a millisecond later, which in sc
// is a delayed transition that sends.
func TestXAftExecSendsDeferredEventsToInitiallyInvokedActors(t *testing.T) {
	t.Parallel()

	synctest.Test(t, func(t *testing.T) {
		m := xaftPonger(t, func(waiting *sc.StateBuilder[string, counter]) {
			waiting.After(time.Millisecond).SendTo("ping", "ponger", func(counter, struct{}) any { return xaftPing{} })
		})

		inst, err := m.Instance(t.Context(), counter{N: 0})
		require.NoError(t, err)
		defer inst.Close()

		snap, err := inst.Await(t.Context(), func(s sc.Snapshot[string, counter]) bool { return s.Done() })
		require.NoError(t, err)
		require.Equal(t, []string{"done"}, snap.Active())
	})
}

// XState: predictableExec > should deliver events sent from the exit actions
// to a service invoked in the same state
func TestXAftExecDeliversEventsSentFromExitActionsToServiceInvokedInSameState(t *testing.T) {
	t.Parallel()

	declare := func(service func(context.Context, counter) (struct{}, error)) *sc.Machine[string, counter] {
		m, err := sc.New("exit_send", func(b *xaftBuilder) {
			b.Initial("active")
			active := b.State("active").SendOnExit("myEvent", "my-service", func(counter) any { return xaftMyEvent{} })
			inv := active.Invoke("my-service", service)
			inv.OnDone().Stay()
			inv.OnError().Stay()
			active.On[xaftToggle]().To("inactive")
			b.State("inactive")
		})
		require.NoError(t, err)

		return m
	}

	m := declare(xaftIdle)
	snap, _ := m.Start(counter{N: 0}, epoch)
	snap, effects := m.Transition(snap, xaftToggle{}, epoch)
	require.Equal(t, []string{"inactive"}, snap.Active())

	// As in XState (statelyai/xstate#3617), the service the step stops
	// receives MY_EVENT first: the send goes in before the stop.
	require.Equal(t, []sc.EffectKind{sc.EffectSend, sc.EffectStopInvocation}, kinds(effects))
	require.Equal(t, xaftMyEvent{}, effects[0].Event)
	require.Equal(t, effects[1].ID, effects[0].ID)

	// Under a driver the stopped service still hears it.
	synctest.Test(t, func(t *testing.T) {
		var received []any

		m := declare(func(ctx context.Context, _ counter) (struct{}, error) {
			for ev := range sc.Inbox(ctx) {
				received = append(received, ev)
			}

			return struct{}{}, context.Cause(ctx)
		})

		inst, err := m.Instance(t.Context(), counter{N: 0})
		require.NoError(t, err)
		defer inst.Close()
		synctest.Wait()

		// Send returns once the stopped service has returned.
		_, err = inst.Send(t.Context(), xaftToggle{})
		require.NoError(t, err)
		require.Equal(t, []any{xaftMyEvent{}}, received)
	})
}
