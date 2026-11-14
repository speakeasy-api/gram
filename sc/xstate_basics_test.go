package sc_test

// Ports of XState's smaller suites under packages/core/test (6.0.0-beta.2):
// deterministic, eventDescriptors, unhandledEvents, assign, internalEvents,
// deep, initial, multiple, order and macrostepBound, one test per `it`, in
// order. The cases that have no form in sc are listed at the end of the file.
//
// XState names a state by its path ("A.B.C.D") and lets two states share a
// key under different parents. sc identifies every state by one value, so
// the charts here give each state an identifier of its own, and a state
// value such as { red: 'walk' } is checked as the list of active states,
// ["red", "walk"], which sc reports in document order.
//
// resolveState({ value }) is Machine.StartIn, which takes the leaf states or
// a compound state that stands for its default entry.

import (
	"context"
	"fmt"
	"iter"
	"sync"
	"testing"
	"testing/synctest"

	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/sc"
)

type (
	xbasTimer        struct{}
	xbasPowerOutage  struct{}
	xbasPedCountdown struct{}
	xbasT            struct{}
	xbasF            struct{}
	xbasNext         struct{}
	xbasFake         struct{}
	xbasNothing      struct{}
	xbasFoo          struct{}
	xbasBar          struct{}
	xbasFooBar       struct{}
	xbasUnknown      struct{}
	xbasNoop         struct{}
	xbasPay          struct{}
	xbasReopen       struct{}
	xbasFail         struct{}
	xbasWildCard     struct{}
	xbasInc          struct{}
	xbasDec          struct{}
	xbasWinProp      struct{}
	xbasWinStatic    struct{}
	xbasWinMix       struct{}
	xbasWin          struct{}
	xbasSetMaybe     struct{}
	xbasStart        struct{}
	xbasGo           struct{}
	xbasAEvent       struct{}
	xbasBEvent       struct{}
	xbasCEvent       struct{}
	xbasDEvent       struct{}
	xbasMachineEvent struct{}
	xbasAS           struct{}
	xbasDP           struct{}
	xbasAP           struct{}
	xbasDS           struct{}
	xbasDeepM        struct{}
	xbasDeepCM       struct{}
	xbasDeepMR       struct{}
	xbasDeepCMR      struct{}
	xbasInitial      struct{}
)

// xbasResult is the `result` event of the payload-matching case.
type xbasResult struct {
	ActorID string
}

// xbasFeedbackMessage and xbasFeedbackRate are the two `FEEDBACK.*` events.
type xbasFeedbackMessage struct {
	Message string
}

type xbasFeedbackRate struct {
	Rate int
}

// xbasIncBy is the INC event that carries a value.
type xbasIncBy struct {
	Value int
}

// xbasTick and xbasChangeValue are the internal events `tick` and
// `change.value`.
type xbasTick struct {
	Count int
}

type xbasChangeValue struct {
	Value string
}

// xbasCount is the context of the counter chart in assign.test.ts.
type xbasCount struct {
	Count int    `json:"count"`
	Foo   string `json:"foo"`
	Maybe string `json:"maybe,omitempty"`
}

type (
	xbasBuilder = sc.Builder[string, counter]
	xbasState   = sc.StateBuilder[string, counter]
	xbasMachine = sc.Machine[string, counter]
)

// xbasCycle is what the core panics with when the chart with the given id
// does not settle within steps steps, last processing an event of type
// event in the given active states.
func xbasCycle(id string, steps int, event string, active string) string {
	return fmt.Sprintf("sc: machine %q does not settle within %d steps: its Always transitions or raised events form a cycle, last processing %s in states [%s]", id, steps, event, active)
}

// xbasActions returns the names of the actions among effects, in order.
func xbasActions[C any](effects []sc.Effect[C]) []string {
	out := []string{}
	for _, e := range effects {
		if e.Kind == sc.EffectAction {
			out = append(out, e.State+"/"+e.Name)
		}
	}

	return out
}

// xbasLog records what a driver's hooks were told.
type xbasLog struct {
	mu         sync.Mutex
	ignored    []any
	ignoredSeq []uint64
	taken      []any
}

func (l *xbasLog) read() (ignored []any, ignoredSeq []uint64, taken []any) {
	l.mu.Lock()
	defer l.mu.Unlock()

	return append([]any{}, l.ignored...), append([]uint64{}, l.ignoredSeq...), append([]any{}, l.taken...)
}

// xbasWatch returns hooks that record ignored and accepted events.
func xbasWatch[C any]() (*xbasLog, sc.RunOption) {
	log := new(xbasLog)

	return log, sc.WithHooks(&sc.Hooks[string, C]{
		Transition: func(_ context.Context, step sc.Step[string, C]) {
			log.mu.Lock()
			defer log.mu.Unlock()

			log.taken = append(log.taken, step.Event)
		},
		Ignored: func(_ context.Context, snap sc.Snapshot[string, C], event any) {
			log.mu.Lock()
			defer log.mu.Unlock()

			log.ignored = append(log.ignored, event)
			log.ignoredSeq = append(log.ignoredSeq, snap.Seq())
		},
		EffectStarted:  nil,
		EffectFinished: nil,
	})
}

// ---------------------------------------------------------------------------
// deterministic.test.ts
// ---------------------------------------------------------------------------

// xbasLight is XState's lightMachine. `TIMER: undefined`, a forbidden event,
// is a transition that consumes the event and does nothing: Stay.
func xbasLight(t *testing.T) *xbasMachine {
	t.Helper()

	m, err := sc.New("light", func(b *xbasBuilder) {
		b.Initial("green")
		green := b.State("green")
		green.On[xbasTimer]().To("yellow")
		green.On[xbasPowerOutage]().To("red")
		yellow := b.State("yellow")
		yellow.On[xbasTimer]().To("red")
		yellow.On[xbasPowerOutage]().To("red")
		red := b.State("red", func(b *xbasBuilder) {
			b.Initial("walk")
			walk := b.State("walk")
			walk.On[xbasPedCountdown]().To("wait")
			walk.On[xbasTimer]().Stay()
			wait := b.State("wait")
			wait.On[xbasPedCountdown]().To("stop")
			wait.On[xbasTimer]().Stay()
			b.State("stop")
		})
		red.On[xbasTimer]().To("green")
		red.On[xbasPowerOutage]().To("red")
	})
	require.NoError(t, err)

	return m
}

// xbasTest is XState's testMachine.
func xbasTest(t *testing.T) *xbasMachine {
	t.Helper()

	m, err := sc.New("test", func(b *xbasBuilder) {
		b.Initial("a")
		a := b.State("a")
		a.On[xbasT]().To("b1")
		a.On[xbasF]().To("c")
		b.State("b", func(b *xbasBuilder) {
			b.Initial("b1")
			b.State("b1")
		})
		b.State("c")
	})
	require.NoError(t, err)

	return m
}

// xbasIn places an instance of m in the given states, as Machine.StartIn
// resolves them.
func xbasIn(t *testing.T, m *xbasMachine, states ...string) sc.Snapshot[string, counter] {
	t.Helper()

	snap, _, err := m.StartIn(counter{N: 0}, epoch, states...)
	require.NoError(t, err)

	return snap
}

// XState: deterministic machine > machine transitions > should properly
// transition states based on event-like object
func TestXBasDeterministicTransitionsOnEventObject(t *testing.T) {
	t.Parallel()

	m := xbasLight(t)
	snap, _ := m.Transition(xbasIn(t, m, "green"), xbasTimer{}, epoch)

	require.Equal(t, []string{"yellow"}, snap.Active())
}

// XState: deterministic machine > machine transitions > should not
// transition states for illegal transitions
func TestXBasDeterministicIgnoresIllegalTransitions(t *testing.T) {
	t.Parallel()

	m, err := sc.New("illegal", func(b *xbasBuilder) {
		b.Initial("a")
		b.State("a").On[xbasNext]().To("b")
		b.State("b")
	})
	require.NoError(t, err)

	before, _ := m.Start(counter{N: 0}, epoch)
	after, effects := m.Transition(before, xbasFake{}, epoch)

	require.Equal(t, []string{"a"}, after.Active())
	require.Equal(t, before.Seq(), after.Seq())
	require.Empty(t, effects)
}

// XState: deterministic machine > machine transitions > should throw an
// error if not given an event
func TestXBasDeterministicWithoutAnEvent(t *testing.T) {
	t.Parallel()

	m := xbasLight(t)
	before := xbasIn(t, m, "walk")

	// DIVERGES from XState: transition() throws when the event is undefined.
	// In sc a nil event only moves time on, as Machine.Advance does, so with
	// no timer due the snapshot comes back unchanged.
	var after sc.Snapshot[string, counter]
	require.NotPanics(t, func() { after, _ = m.Transition(before, nil, epoch) })
	require.Equal(t, before.Seq(), after.Seq())
	require.Equal(t, []string{"red", "walk"}, after.Active())
}

// XState: deterministic machine > machine transitions > should transition to
// nested states as target
func TestXBasDeterministicTransitionsToNestedTarget(t *testing.T) {
	t.Parallel()

	m := xbasTest(t)
	snap, _ := m.Transition(xbasIn(t, m, "a"), xbasT{}, epoch)

	require.Equal(t, []string{"b", "b1"}, snap.Active())
}

// XState: deterministic machine > machine transitions > should throw an
// error for transitions from invalid states
//
// sc reports the state that is not in the chart as an error, where XState's
// resolveState throws.
func TestXBasDeterministicRejectsInvalidStates(t *testing.T) {
	t.Parallel()

	_, _, err := xbasTest(t).StartIn(counter{N: 0}, epoch, "fake")

	require.ErrorIs(t, err, sc.ErrInvalidSnapshot)
}

// XState: deterministic machine > machine transitions > should throw an
// error for transitions from invalid substates
//
// XState's value is the path 'a.fake'. sc has no paths, so the port names
// the missing state next to its would-be parent.
func TestXBasDeterministicRejectsInvalidSubstates(t *testing.T) {
	t.Parallel()

	_, _, err := xbasTest(t).StartIn(counter{N: 0}, epoch, "a", "fake")

	require.ErrorIs(t, err, sc.ErrInvalidSnapshot)
}

// XState: deterministic machine > machine transitions > should use the
// machine.initialState when an undefined state is given
func TestXBasDeterministicUsesInitialState(t *testing.T) {
	t.Parallel()

	m := xbasLight(t)
	snap, _ := m.Start(counter{N: 0}, epoch)
	snap, _ = m.Transition(snap, xbasTimer{}, epoch)

	require.Equal(t, []string{"yellow"}, snap.Active())
}

// XState: deterministic machine > machine transitions > should use the
// machine.initialState when an undefined state is given (unhandled event)
//
// The XState case is the same as the one before it, whatever its title.
func TestXBasDeterministicUsesInitialStateUnhandledEvent(t *testing.T) {
	t.Parallel()

	m := xbasLight(t)
	snap, _ := m.Start(counter{N: 0}, epoch)
	snap, _ = m.Transition(snap, xbasTimer{}, epoch)

	require.Equal(t, []string{"yellow"}, snap.Active())
}

// XState: deterministic machine > machine transition with nested states >
// should properly transition a nested state
func TestXBasDeterministicTransitionsNestedState(t *testing.T) {
	t.Parallel()

	m := xbasLight(t)
	snap, _ := m.Transition(xbasIn(t, m, "walk"), xbasPedCountdown{}, epoch)

	require.Equal(t, []string{"red", "wait"}, snap.Active())
}

// XState: deterministic machine > machine transition with nested states >
// should transition from initial nested states
func TestXBasDeterministicTransitionsFromInitialNestedStates(t *testing.T) {
	t.Parallel()

	m := xbasLight(t)

	// As resolveState({ value: 'red' }) does, StartIn fills in red's initial
	// child.
	from := xbasIn(t, m, "red")
	require.Equal(t, []string{"red", "walk"}, from.Active())

	snap, _ := m.Transition(from, xbasPedCountdown{}, epoch)

	require.Equal(t, []string{"red", "wait"}, snap.Active())
}

// XState: deterministic machine > machine transition with nested states >
// should transition from deep initial nested states
//
// The XState case is the same as the one before it.
func TestXBasDeterministicTransitionsFromDeepInitialNestedStates(t *testing.T) {
	t.Parallel()

	m := xbasLight(t)
	snap, _ := m.Transition(xbasIn(t, m, "red"), xbasPedCountdown{}, epoch)

	require.Equal(t, []string{"red", "wait"}, snap.Active())
}

// XState: deterministic machine > machine transition with nested states >
// should bubble up events that nested states cannot handle
func TestXBasDeterministicBubblesUpEvents(t *testing.T) {
	t.Parallel()

	m := xbasLight(t)
	snap, _ := m.Transition(xbasIn(t, m, "stop"), xbasTimer{}, epoch)

	require.Equal(t, []string{"green"}, snap.Active())
}

// XState: deterministic machine > machine transition with nested states >
// should not transition from illegal events
func TestXBasDeterministicIgnoresIllegalEventsWhenNested(t *testing.T) {
	t.Parallel()

	m, err := sc.New("nested_illegal", func(b *xbasBuilder) {
		b.Initial("a")
		b.State("a", func(b *xbasBuilder) {
			b.Initial("b")
			b.State("b").On[xbasNext]().To("c")
			b.State("c")
		})
	})
	require.NoError(t, err)

	before, _ := m.Start(counter{N: 0}, epoch)
	after, effects := m.Transition(before, xbasFake{}, epoch)

	require.Equal(t, []string{"a", "b"}, after.Active())
	require.Equal(t, before.Seq(), after.Seq())
	require.Empty(t, effects)
}

// XState: deterministic machine > machine transition with nested states >
// should transition to the deepest initial state
func TestXBasDeterministicTransitionsToDeepestInitialState(t *testing.T) {
	t.Parallel()

	m := xbasLight(t)
	snap, _ := m.Transition(xbasIn(t, m, "yellow"), xbasTimer{}, epoch)

	require.Equal(t, []string{"red", "walk"}, snap.Active())
}

// XState: deterministic machine > machine transition with nested states >
// should return the same state if no transition occurs
func TestXBasDeterministicReturnsSameStateWithoutTransition(t *testing.T) {
	t.Parallel()

	m := xbasLight(t)
	start, _ := m.Start(counter{N: 0}, epoch)
	initial, _ := m.Transition(start, xbasNothing{}, epoch)
	next, effects := m.Transition(initial, xbasNothing{}, epoch)

	require.Equal(t, initial.Active(), next.Active())
	require.Equal(t, initial.Seq(), next.Seq())
	require.Equal(t, start.Seq(), next.Seq())
	require.Empty(t, effects)
}

// XState: deterministic machine > state key names > should work with
// substate nodes that have the same key
func TestXBasDeterministicStateKeyNames(t *testing.T) {
	t.Parallel()

	m, err := sc.New("keys", func(b *xbasBuilder) {
		b.Initial("test")
		test := b.State("test").Entry("entry", noop).Exit("exit", noop)
		test.Stream("activity", func(context.Context, *sc.Run, counter) iter.Seq2[any, error] {
			return func(func(any, error) bool) {}
		}).OnError().Stay()
		test.On[xbasNext]().To("test")
	})
	require.NoError(t, err)

	snap, _ := m.Start(counter{N: 0}, epoch)
	snap, _ = m.Transition(snap, xbasNext{}, epoch)

	require.Equal(t, []string{"test"}, snap.Active())
}

// XState: deterministic machine > forbidden events > undefined transitions
// should forbid events
func TestXBasDeterministicForbiddenEvents(t *testing.T) {
	t.Parallel()

	m := xbasLight(t)
	snap, _ := m.Transition(xbasIn(t, m, "walk"), xbasTimer{}, epoch)

	require.Equal(t, []string{"red", "walk"}, snap.Active())
}

// ---------------------------------------------------------------------------
// eventDescriptors.test.ts
// ---------------------------------------------------------------------------

// XState: event descriptors > selects serialized transition arrays by
// shallow event payload matches
//
// `matches` is a guard on the event's fields.
func TestXBasDescriptorsSelectsByEventPayload(t *testing.T) {
	t.Parallel()

	m, err := sc.New("payload", func(b *xbasBuilder) {
		b.Initial("pending")
		pending := b.State("pending")
		pending.On[xbasResult]().
			When("isFirst", func(_ counter, ev xbasResult) bool { return ev.ActorID == "first" }).
			To("first")
		pending.On[xbasResult]().
			When("isSecond", func(_ counter, ev xbasResult) bool { return ev.ActorID == "second" }).
			To("second")
		b.State("first")
		b.State("second")
	})
	require.NoError(t, err)

	snap, _ := m.Start(counter{N: 0}, epoch)
	snap, _ = m.Transition(snap, xbasResult{ActorID: "second"}, epoch)

	require.Equal(t, []string{"second"}, snap.Active())
}

// XState: event descriptors > selects canonical actor events by actor ID
//
// Partial: XState sends a hand-made 'xstate.done.actor' event that names the
// actor. In sc a completion is routed to the service it belongs to with
// Invocation.OnDone, and the event names one run of it, so the port invokes
// a service called "job" and delivers the Done of its run.
func TestXBasDescriptorsSelectsActorEventsByActorID(t *testing.T) {
	t.Parallel()

	m, err := sc.New("job", func(b *xbasBuilder) {
		b.Initial("pending")
		job := b.State("pending").Invoke("job", func(context.Context, *sc.Run, counter) (struct{}, error) {
			return struct{}{}, nil
		})
		job.OnDone().To("complete")
		job.OnError().Stay()
		b.State("complete")
	})
	require.NoError(t, err)

	start, effects := m.Start(counter{N: 0}, epoch)
	run := effect(t, effects, sc.EffectStartInvocation)

	// DIVERGES from XState: an event of the done type that the test makes up
	// is taken like any other event. sc ignores a Done whose ID is not that
	// of a running service.
	forged, _ := m.Transition(start, sc.Done[struct{}]{ID: "x:1", Result: struct{}{}}, epoch)
	require.Equal(t, start.Seq(), forged.Seq())
	require.Equal(t, []string{"pending"}, forged.Active())

	snap, _ := m.Transition(start, sc.Done[struct{}]{ID: run.ID, Result: struct{}{}}, epoch)

	require.Equal(t, []string{"complete"}, snap.Active())
}

// XState: event descriptors > should fallback to using wildcard transition
// definition (if specified)
func TestXBasDescriptorsFallsBackToWildcard(t *testing.T) {
	t.Parallel()

	m, err := sc.New("wildcard", func(b *xbasBuilder) {
		b.Initial("A")
		a := b.State("A")
		a.On[xbasFoo]().To("B")
		a.OnAny().To("C")
		b.State("B")
		b.State("C")
	})
	require.NoError(t, err)

	snap, _ := m.Start(counter{N: 0}, epoch)
	snap, _ = m.Transition(snap, xbasBar{}, epoch)

	require.Equal(t, []string{"C"}, snap.Active())
}

// XState: event descriptors > should prioritize explicit descriptor even if
// wildcard comes first
func TestXBasDescriptorsPrioritizesExplicitOverWildcard(t *testing.T) {
	t.Parallel()

	m, err := sc.New("explicit", func(b *xbasBuilder) {
		b.Initial("A")
		a := b.State("A")
		a.OnAny().To("fail")
		a.On[xbasNext]().To("pass")
		b.State("fail")
		b.State("pass")
	})
	require.NoError(t, err)

	snap, _ := m.Start(counter{N: 0}, epoch)
	snap, _ = m.Transition(snap, xbasNext{}, epoch)

	require.Equal(t, []string{"pass"}, snap.Active())
}

// XState: event descriptors > should fall back to wildcard descriptor when
// exact descriptor guard fails
//
// Partial: the fallback in XState is the partial descriptor 'foo.*'. sc's
// only wildcard is OnAny, which stands in for it.
func TestXBasDescriptorsFallsBackToWildcardWhenGuardFails(t *testing.T) {
	t.Parallel()

	m, err := sc.New("guard_fallback", func(b *xbasBuilder) {
		b.Initial("A")
		a := b.State("A")
		a.On[xbasFooBar]().When("never", func(counter, xbasFooBar) bool { return false }).To("fail")
		a.OnAny().To("pass")
		b.State("fail")
		b.State("pass")
	})
	require.NoError(t, err)

	snap, _ := m.Start(counter{N: 0}, epoch)
	snap, _ = m.Transition(snap, xbasFooBar{}, epoch)

	require.Equal(t, []string{"pass"}, snap.Active())
}

// XState: event descriptors > should allow assertEvent to use partial
// descriptors
//
// Partial: assertEvent and its type narrowing are TypeScript. What is left
// is one handler that receives every FEEDBACK event as it arrived; OnAny
// stands in for 'FEEDBACK.*'.
func TestXBasDescriptorsHandlerReceivesWildcardMatchedEvents(t *testing.T) {
	t.Parallel()

	m, err := sc.New("feedback", func(b *xbasBuilder) {
		b.Initial("listening")
		b.State("listening").OnAny().Do("handleEvent", func(context.Context, counter, any) {})
	})
	require.NoError(t, err)

	snap, _ := m.Start(counter{N: 0}, epoch)

	snap, effects := m.Transition(snap, xbasFeedbackMessage{Message: "hello"}, epoch)
	require.Equal(t, []string{"listening/handleEvent"}, xbasActions(effects))
	require.Equal(t, xbasFeedbackMessage{Message: "hello"}, effects[0].Event)

	_, effects = m.Transition(snap, xbasFeedbackRate{Rate: 5}, epoch)
	require.Equal(t, []string{"listening/handleEvent"}, xbasActions(effects))
	require.Equal(t, xbasFeedbackRate{Rate: 5}, effects[0].Event)
}

// ---------------------------------------------------------------------------
// unhandledEvents.test.ts
// ---------------------------------------------------------------------------

// xbasToggle is the suite's shared machine. `NOOP: () => ({})`, an event
// that is handled and changes nothing, is Stay.
func xbasToggle(t *testing.T) *xbasMachine {
	t.Helper()

	m, err := sc.New("toggle", func(b *xbasBuilder) {
		b.Initial("a")
		a := b.State("a")
		a.On[xbasNoop]().Stay()
		a.On[xbasNext]().To("b")
		b.State("b")
	})
	require.NoError(t, err)

	return m
}

// xbasOrder is the order machine of the "status" cases.
func xbasOrder(t *testing.T) *xbasMachine {
	t.Helper()

	m, err := sc.New("order", func(b *xbasBuilder) {
		b.Initial("pending")
		b.On[xbasReopen]().To("pending")
		b.On[xbasFail]().Do("fail", func(context.Context, counter, xbasFail) {
			panic("payment provider down")
		})
		b.State("pending").Entry("entry", noop).On[xbasPay]().To("paid")
		b.Final("paid")
	})
	require.NoError(t, err)

	return m
}

// XState: unhandled events > transition() returns the same snapshot
// reference and no effects for an unhandled event
func TestXBasUnhandledReturnsSameSnapshotAndNoEffects(t *testing.T) {
	t.Parallel()

	m := xbasToggle(t)
	before, _ := m.Start(counter{N: 0}, epoch)
	after, effects := m.Transition(before, xbasUnknown{}, epoch)

	require.Equal(t, before.Seq(), after.Seq())
	require.Empty(t, effects)
	require.False(t, m.Can(before, xbasUnknown{}, epoch))
}

// XState: unhandled events > transition() treats every event as unhandled
// when the status is %s, like the actor (done, error, stopped)
//
// Partial: getMicrosteps has no counterpart, and 'xstate.stop' is not an
// event in sc.
func TestXBasUnhandledWhenNotActive(t *testing.T) {
	t.Parallel()

	t.Run("done", func(t *testing.T) {
		t.Parallel()

		synctest.Test(t, func(t *testing.T) {
			m := xbasOrder(t)
			log, hooks := xbasWatch[counter]()

			inst, err := m.Instance(t.Context(), counter{N: 0}, hooks)
			require.NoError(t, err)
			defer inst.Close()

			_, err = inst.Send(t.Context(), xbasPay{})
			require.NoError(t, err)

			snap := inst.Snapshot()
			require.True(t, snap.Done())

			for _, ev := range []any{xbasPay{}, xbasReopen{}} {
				next, effects := m.Transition(snap, ev, epoch)
				require.Equal(t, snap.Seq(), next.Seq())
				require.Empty(t, effects)
				require.False(t, m.Can(snap, ev, epoch))
			}

			step, err := inst.Send(t.Context(), xbasReopen{})
			require.NoError(t, err)
			require.False(t, step.Accepted())
			require.Equal(t, snap.Seq(), inst.Snapshot().Seq())

			ignored, _, _ := log.read()
			require.Equal(t, []any{xbasReopen{}}, ignored)
		})
	})

	t.Run("error", func(t *testing.T) {
		t.Parallel()

		synctest.Test(t, func(t *testing.T) {
			m := xbasOrder(t)

			inst, err := m.Instance(t.Context(), counter{N: 0})
			require.NoError(t, err)
			defer inst.Close()

			// DIVERGES from XState: a transition that throws puts the actor
			// in the 'error' status, after which every event is unhandled.
			// sc has no such status. A failing action is reported to the
			// sender as an error, the transition has still been taken, and
			// the instance goes on handling events.
			step, err := inst.Send(t.Context(), xbasFail{})
			require.Error(t, err)
			require.True(t, step.Accepted())
			require.False(t, inst.Snapshot().Done())

			step, err = inst.Send(t.Context(), xbasPay{})
			require.NoError(t, err)
			require.True(t, step.Accepted())
			require.Equal(t, []string{"paid"}, inst.Snapshot().Active())
		})
	})

	t.Run("stopped", func(t *testing.T) {
		t.Parallel()

		synctest.Test(t, func(t *testing.T) {
			m := xbasOrder(t)

			inst, err := m.Instance(t.Context(), counter{N: 0})
			require.NoError(t, err)

			inst.Close()
			snap := inst.Snapshot()

			// The driver refuses events once it is stopped.
			_, err = inst.Send(t.Context(), xbasReopen{})
			require.ErrorIs(t, err, sc.ErrInstanceClosed)
			require.Equal(t, snap.Seq(), inst.Snapshot().Seq())

			// DIVERGES from XState: the snapshot of a stopped actor has the
			// status 'stopped', and transition() treats every event as
			// unhandled for it. An sc snapshot does not record that its
			// driver was closed, so that it can be resumed, and the pure
			// core still takes events for it.
			next, _ := m.Transition(snap, xbasPay{}, epoch)
			require.Equal(t, []string{"paid"}, next.Active())
		})
	})
}

// XState: unhandled events > transition() returns a new snapshot object for
// a handled event that changes nothing
func TestXBasUnhandledHandledEventThatChangesNothing(t *testing.T) {
	t.Parallel()

	m := xbasToggle(t)
	before, _ := m.Start(counter{N: 0}, epoch)
	after, effects := m.Transition(before, xbasNoop{}, epoch)

	require.Equal(t, []string{"a"}, after.Active())
	require.True(t, m.Can(before, xbasNoop{}, epoch))

	// A new snapshot is one with a later Seq.
	require.Equal(t, before.Seq()+1, after.Seq())
	require.Empty(t, effects)
}

// XState: unhandled events > calls onUnhandledEvent with the event and
// unchanged snapshot
func TestXBasUnhandledCallsIgnoredHook(t *testing.T) {
	t.Parallel()

	synctest.Test(t, func(t *testing.T) {
		m := xbasToggle(t)
		log, hooks := xbasWatch[counter]()

		inst, err := m.Instance(t.Context(), counter{N: 0}, hooks)
		require.NoError(t, err)
		defer inst.Close()

		// DIVERGES from XState: XState calls onUnhandledEvent for an event
		// of a type no transition names. sc's Send rejects such an event
		// with ErrUnknownEvent, changes nothing and tells no hook. An event
		// of a type the chart names, which the current state does not
		// handle, is the one that reaches Hooks.Ignored.
		before := inst.Snapshot()
		_, err = inst.Send(t.Context(), xbasUnknown{})
		require.ErrorIs(t, err, sc.ErrUnknownEvent)
		require.Equal(t, before.Seq(), inst.Snapshot().Seq())

		ignored, _, _ := log.read()
		require.Empty(t, ignored)

		_, err = inst.Send(t.Context(), xbasNext{})
		require.NoError(t, err)

		before = inst.Snapshot()
		_, err = inst.Send(t.Context(), xbasNoop{})
		require.NoError(t, err)

		ignored, seqs, _ := log.read()
		require.Equal(t, []any{xbasNoop{}}, ignored)
		require.Equal(t, []uint64{before.Seq()}, seqs)
		require.Equal(t, before.Seq(), inst.Snapshot().Seq())
	})
}

// XState: unhandled events > reports unhandled framework names with
// validation %s and type %s
//
// Partial: event validation and the console warning have no counterpart, so
// the four cases are two. 'xstate.done.actor.save' is a Done for a service
// that is not running, and 'xstate.snapshot.actor' an Emitted for one. As in
// XState, the machine is empty.
func TestXBasUnhandledReportsFrameworkEvents(t *testing.T) {
	t.Parallel()

	events := map[string]any{
		"done":     sc.Done[string]{ID: "save", Result: "legacy"},
		"snapshot": sc.Emitted{ID: "actor", Event: "legacy"},
	}

	for name, ev := range events {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			synctest.Test(t, func(t *testing.T) {
				m, err := sc.New("empty", func(*xbasBuilder) {})
				require.NoError(t, err)

				log, hooks := xbasWatch[counter]()
				inst, err := m.Instance(t.Context(), counter{N: 0}, hooks)
				require.NoError(t, err)
				defer inst.Close()

				before := inst.Snapshot()
				_, err = inst.Send(t.Context(), ev)
				require.NoError(t, err)

				ignored, seqs, _ := log.read()
				require.Equal(t, []any{ev}, ignored)
				require.Equal(t, []uint64{before.Seq()}, seqs)
				require.Equal(t, before.Seq(), inst.Snapshot().Seq())
			})
		})
	}
}

// XState: unhandled events > does not report unhandled child completion or
// actor stop events
func TestXBasUnhandledDoesNotReportChildCompletion(t *testing.T) {
	t.Parallel()

	synctest.Test(t, func(t *testing.T) {
		m, err := sc.New("parent", func(b *xbasBuilder) {
			b.Chart().Invoke("child", func(context.Context, *sc.Run, counter) (string, error) {
				return "done", nil
			}).OnError().Stay()
		})
		require.NoError(t, err)

		log, hooks := xbasWatch[counter]()
		inst, err := m.Instance(t.Context(), counter{N: 0}, hooks)
		require.NoError(t, err)

		synctest.Wait()
		inst.Close()

		ignored, _, taken := log.read()
		require.Empty(t, ignored)
		require.Len(t, taken, 1)
		require.IsType(t, sc.Done[string]{ID: "", Result: ""}, taken[0])
		require.Empty(t, inst.Snapshot().Services())
	})
}

// XState: unhandled events > does not report a stale scheduler timer input
func TestXBasUnhandledStaleTimer(t *testing.T) {
	t.Parallel()

	synctest.Test(t, func(t *testing.T) {
		m, err := sc.New("empty", func(*xbasBuilder) {})
		require.NoError(t, err)

		log, hooks := xbasWatch[counter]()
		inst, err := m.Instance(t.Context(), counter{N: 0}, hooks)
		require.NoError(t, err)
		defer inst.Close()

		before := inst.Snapshot()
		_, err = inst.Send(t.Context(), sc.TimerFired{ID: "cancelled"})
		require.NoError(t, err)
		require.Equal(t, before.Seq(), inst.Snapshot().Seq())

		// DIVERGES from XState: a timer event for a timer that is no longer
		// scheduled is dropped without calling onUnhandledEvent. sc reports
		// it to Hooks.Ignored like any other event that changed nothing.
		ignored, _, _ := log.read()
		require.Equal(t, []any{sc.TimerFired{ID: "cancelled"}}, ignored)
	})
}

// XState: unhandled events > shows an unhandled event in the inspection
// stream as a transition with the same snapshot
func TestXBasUnhandledInTheHookStream(t *testing.T) {
	t.Parallel()

	synctest.Test(t, func(t *testing.T) {
		m := xbasToggle(t)
		log, hooks := xbasWatch[counter]()

		inst, err := m.Instance(t.Context(), counter{N: 0}, hooks)
		require.NoError(t, err)
		defer inst.Close()

		// An event of a type no transition names is rejected by Send, as
		// TestXBasUnhandledCallsIgnoredHook shows, so the unhandled event
		// here is one of a type the chart names: NOOP, once in b.
		_, err = inst.Send(t.Context(), xbasNext{})
		require.NoError(t, err)

		before := inst.Snapshot()
		step, err := inst.Send(t.Context(), xbasNoop{})
		require.NoError(t, err)
		require.Equal(t, before.Seq(), step.To.Seq())

		// DIVERGES from XState: the inspection stream shows the unhandled
		// event as an '@xstate.transition' with the same snapshot, and has
		// no event of its own for it. sc's hooks keep the two apart: the
		// event goes to Hooks.Ignored, with the unchanged snapshot, and not
		// to Hooks.Transition.
		ignored, seqs, taken := log.read()
		require.Equal(t, []any{xbasNext{}}, taken)
		require.Equal(t, []any{xbasNoop{}}, ignored)
		require.Equal(t, []uint64{before.Seq()}, seqs)
	})
}

// XState: unhandled events > does not report handled or wildcard-matched
// events
func TestXBasUnhandledHandledAndWildcardEvents(t *testing.T) {
	t.Parallel()

	synctest.Test(t, func(t *testing.T) {
		m, err := sc.New("handled", func(b *xbasBuilder) {
			b.Initial("a")
			a := b.State("a")
			a.On[xbasNoop]().Stay()
			a.OnAny().Stay()
		})
		require.NoError(t, err)

		log, hooks := xbasWatch[counter]()
		inst, err := m.Instance(t.Context(), counter{N: 0}, hooks)
		require.NoError(t, err)
		defer inst.Close()

		_, err = inst.Send(t.Context(), xbasNoop{})
		require.NoError(t, err)
		_, err = inst.Send(t.Context(), xbasWildCard{})
		require.NoError(t, err)

		ignored, _, taken := log.read()
		require.Empty(t, ignored)
		require.Equal(t, []any{xbasNoop{}, xbasWildCard{}}, taken)
	})
}

// ---------------------------------------------------------------------------
// assign.test.ts
// ---------------------------------------------------------------------------

// xbasWins is the assignment the four WIN events share.
func xbasWins[Ev any](c xbasCount, _ Ev) xbasCount {
	c.Count = 100
	c.Foo = "win"

	return c
}

// xbasCounter is XState's counter machine. Its initial context is given to
// Start, as XState's createCounterMachine takes it.
func xbasCounter(t *testing.T) *sc.Machine[string, xbasCount] {
	t.Helper()

	m, err := sc.New("counter", func(b *sc.Builder[string, xbasCount]) {
		b.Initial("counting")
		counting := b.State("counting")
		counting.On[xbasInc]().
			Assign("inc", func(c xbasCount, _ xbasInc) xbasCount {
				c.Count++
				return c
			}).
			To("counting")
		counting.On[xbasDec]().
			Assign("dec", func(c xbasCount, _ xbasDec) xbasCount {
				c.Count--
				return c
			}).
			To("counting")
		counting.On[xbasWinProp]().Assign("win", xbasWins[xbasWinProp]).To("counting")
		counting.On[xbasWinStatic]().Assign("win", xbasWins[xbasWinStatic]).To("counting")
		counting.On[xbasWinMix]().Assign("win", xbasWins[xbasWinMix]).To("counting")
		counting.On[xbasWin]().Assign("win", xbasWins[xbasWin]).To("counting")
		counting.On[xbasSetMaybe]().Assign("setMaybe", func(c xbasCount, _ xbasSetMaybe) xbasCount {
			c.Maybe = "defined"
			return c
		})
	})
	require.NoError(t, err)

	return m
}

// xbasCounting sends events to a new counter, checking after each that the
// chart is still counting, and returns the context after the last.
func xbasCounting(t *testing.T, initial xbasCount, events ...any) xbasCount {
	t.Helper()

	m := xbasCounter(t)
	snap, _ := m.Start(initial, epoch)
	for _, ev := range events {
		snap, _ = m.Transition(snap, ev, epoch)
		require.Equal(t, []string{"counting"}, snap.Active())
	}

	return snap.Context
}

// XState: assigning to context > applies the assignment to context (property
// assignment)
func TestXBasAssignPropertyAssignment(t *testing.T) {
	t.Parallel()

	initial := xbasCount{Count: 0, Foo: "bar", Maybe: ""}

	require.Equal(t, xbasCount{Count: -1, Foo: "bar", Maybe: ""}, xbasCounting(t, initial, xbasDec{}))
	require.Equal(t, xbasCount{Count: -2, Foo: "bar", Maybe: ""}, xbasCounting(t, initial, xbasDec{}, xbasDec{}))
}

// XState: assigning to context > applies the assignment to context
func TestXBasAssignAppliesAssignment(t *testing.T) {
	t.Parallel()

	initial := xbasCount{Count: 0, Foo: "bar", Maybe: ""}

	require.Equal(t, xbasCount{Count: 1, Foo: "bar", Maybe: ""}, xbasCounting(t, initial, xbasInc{}))
	require.Equal(t, xbasCount{Count: 2, Foo: "bar", Maybe: ""}, xbasCounting(t, initial, xbasInc{}, xbasInc{}))
}

// XState: assigning to context > applies the assignment to multiple
// properties (property assignment)
func TestXBasAssignMultiplePropertiesPropertyAssignment(t *testing.T) {
	t.Parallel()

	got := xbasCounting(t, xbasCount{Count: 0, Foo: "bar", Maybe: ""}, xbasWinProp{})

	require.Equal(t, xbasCount{Count: 100, Foo: "win", Maybe: ""}, got)
}

// XState: assigning to context > applies the assignment to multiple
// properties (static)
func TestXBasAssignMultiplePropertiesStatic(t *testing.T) {
	t.Parallel()

	got := xbasCounting(t, xbasCount{Count: 0, Foo: "bar", Maybe: ""}, xbasWinStatic{})

	require.Equal(t, xbasCount{Count: 100, Foo: "win", Maybe: ""}, got)
}

// XState: assigning to context > applies the assignment to multiple
// properties (static + prop assignment)
func TestXBasAssignMultiplePropertiesMixed(t *testing.T) {
	t.Parallel()

	got := xbasCounting(t, xbasCount{Count: 0, Foo: "bar", Maybe: ""}, xbasWinMix{})

	require.Equal(t, xbasCount{Count: 100, Foo: "win", Maybe: ""}, got)
}

// XState: assigning to context > applies the assignment to multiple
// properties
func TestXBasAssignMultipleProperties(t *testing.T) {
	t.Parallel()

	got := xbasCounting(t, xbasCount{Count: 0, Foo: "bar", Maybe: ""}, xbasWin{})

	require.Equal(t, xbasCount{Count: 100, Foo: "win", Maybe: ""}, got)
}

// XState: assigning to context > applies the assignment to the explicit
// external state (property assignment)
func TestXBasAssignExplicitExternalStatePropertyAssignment(t *testing.T) {
	t.Parallel()

	fifty := xbasCount{Count: 50, Foo: "bar", Maybe: ""}

	require.Equal(t, xbasCount{Count: 49, Foo: "bar", Maybe: ""}, xbasCounting(t, fifty, xbasDec{}))
	require.Equal(t, xbasCount{Count: 48, Foo: "bar", Maybe: ""}, xbasCounting(t, fifty, xbasDec{}, xbasDec{}))

	hundred := xbasCount{Count: 100, Foo: "bar", Maybe: ""}

	require.Equal(t, xbasCount{Count: 99, Foo: "bar", Maybe: ""}, xbasCounting(t, hundred, xbasDec{}))
}

// XState: assigning to context > applies the assignment to the explicit
// external state
func TestXBasAssignExplicitExternalState(t *testing.T) {
	t.Parallel()

	fifty := xbasCount{Count: 50, Foo: "bar", Maybe: ""}

	require.Equal(t, xbasCount{Count: 51, Foo: "bar", Maybe: ""}, xbasCounting(t, fifty, xbasInc{}))
	require.Equal(t, xbasCount{Count: 52, Foo: "bar", Maybe: ""}, xbasCounting(t, fifty, xbasInc{}, xbasInc{}))

	other := xbasCount{Count: 102, Foo: "bar", Maybe: ""}

	require.Equal(t, xbasCount{Count: 103, Foo: "bar", Maybe: ""}, xbasCounting(t, other, xbasInc{}))
}

// XState: assigning to context > should maintain state after unhandled event
func TestXBasAssignMaintainsStateAfterUnhandledEvent(t *testing.T) {
	t.Parallel()

	got := xbasCounting(t, xbasCount{Count: 0, Foo: "bar", Maybe: ""}, xbasFake{})

	require.Equal(t, xbasCount{Count: 0, Foo: "bar", Maybe: ""}, got)
}

// XState: assigning to context > sets undefined properties
func TestXBasAssignSetsUndefinedProperties(t *testing.T) {
	t.Parallel()

	got := xbasCounting(t, xbasCount{Count: 0, Foo: "bar", Maybe: ""}, xbasSetMaybe{})

	require.Equal(t, xbasCount{Count: 0, Foo: "bar", Maybe: "defined"}, got)
}

// XState: assigning to context > can assign from event
func TestXBasAssignFromEvent(t *testing.T) {
	t.Parallel()

	m, err := sc.New("from_event", func(b *xbasBuilder) {
		b.Initial("active")
		b.State("active").On[xbasIncBy]().Assign("set", func(c counter, ev xbasIncBy) counter {
			c.N = ev.Value
			return c
		})
	})
	require.NoError(t, err)

	snap, _ := m.Start(counter{N: 0}, epoch)
	snap, _ = m.Transition(snap, xbasIncBy{Value: 30}, epoch)

	require.Equal(t, 30, snap.Context.N)
}

// ---------------------------------------------------------------------------
// internalEvents.test.ts
// ---------------------------------------------------------------------------

// XState: internalEvents > supports separately declared internal event
// schemas
//
// Partial: the schemas, their validation and the dead-letter report for an
// internal event sent from outside are dropped; see the cases below for what
// sc does with such an event.
func TestXBasInternalRaisesDeclaredInternalEvents(t *testing.T) {
	t.Parallel()

	m, err := sc.New("schemas", func(b *xbasBuilder) {
		b.Initial("idle")
		idle := b.State("idle")
		idle.On[xbasStart]().
			Raise("raiseTick", func(counter, xbasStart) any { return xbasTick{Count: 1} }).
			Raise("raiseChange", func(counter, xbasStart) any { return xbasChangeValue{Value: "ready"} })
		idle.On[xbasTick]().Stay()
		idle.On[xbasChangeValue]().To("done")
		b.State("done")
	})
	require.NoError(t, err)

	snap, _ := m.Start(counter{N: 0}, epoch)
	snap, _ = m.Transition(snap, xbasStart{}, epoch)

	require.Equal(t, []string{"done"}, snap.Active())
}

// XState: internalEvents > allows raising internal events
func TestXBasInternalAllowsRaisingInternalEvents(t *testing.T) {
	t.Parallel()

	m, err := sc.New("raising", func(b *xbasBuilder) {
		b.Initial("idle")
		idle := b.State("idle")
		idle.On[xbasFoo]().Raise("raiseTick", func(counter, xbasFoo) any { return xbasTick{Count: 0} })
		idle.On[xbasTick]().To("done")
		b.State("done")
	})
	require.NoError(t, err)

	snap, _ := m.Start(counter{N: 0}, epoch)
	snap, _ = m.Transition(snap, xbasFoo{}, epoch)

	require.Equal(t, []string{"done"}, snap.Active())
}

// XState: internalEvents > rejects sending internal events from outside
func TestXBasInternalEventSentFromOutside(t *testing.T) {
	t.Parallel()

	m, err := sc.New("outside", func(b *xbasBuilder) {
		b.Initial("idle")
		idle := b.State("idle")
		idle.On[xbasFoo]().To("done")
		idle.On[xbasTick]().To("done")
		b.State("done")
	})
	require.NoError(t, err)

	snap, _ := m.Start(counter{N: 0}, epoch)
	snap, _ = m.Transition(snap, xbasTick{Count: 0}, epoch)

	// DIVERGES from XState: an event declared internal can only be raised;
	// sent from outside it is rejected and the machine stays in 'idle'. sc
	// does not tell internal events from public ones, so the event is taken
	// whoever sends it.
	require.Equal(t, []string{"done"}, snap.Active())
}

// XState: internalEvents > rejects sending wildcard-matched internal events
// from outside
func TestXBasInternalWildcardEventSentFromOutside(t *testing.T) {
	t.Parallel()

	m, err := sc.New("outside_wildcard", func(b *xbasBuilder) {
		b.Initial("idle")
		b.State("idle").On[xbasChangeValue]().To("done")
		b.State("done")
	})
	require.NoError(t, err)

	snap, _ := m.Start(counter{N: 0}, epoch)
	snap, _ = m.Transition(snap, xbasChangeValue{Value: "x"}, epoch)

	// DIVERGES from XState: 'change.value' matches the internal descriptor
	// 'change.*', so it is rejected when sent from outside and the machine
	// stays in 'idle'. sc has no internal events and takes it.
	require.Equal(t, []string{"done"}, snap.Active())
}

// XState: an untyped machine keeps its sendable events (type-level)
//
// Partial: the case is about TypeScript types. What runs is a machine taking
// NEXT twice.
func TestXBasInternalUntypedMachineTakesItsEvents(t *testing.T) {
	t.Parallel()

	m, err := sc.New("untyped", func(b *xbasBuilder) {
		b.Initial("a")
		b.State("a").On[xbasNext]().To("a")
	})
	require.NoError(t, err)

	snap, _ := m.Start(counter{N: 0}, epoch)
	require.True(t, m.Can(snap, xbasNext{}, epoch))

	snap, _ = m.Transition(snap, xbasNext{}, epoch)
	snap, _ = m.Transition(snap, xbasNext{}, epoch)

	require.Equal(t, []string{"a"}, snap.Active())
}

// ---------------------------------------------------------------------------
// deep.test.ts
// ---------------------------------------------------------------------------

// xbasNest declares ids as a chain of states, each the initial child of the
// one before it, and returns their builders in the same order. Every state
// gets the entry and exit actions XState's trackEntries adds, so 'exit:
// A.B.C.D' is the effect "D/exit" and 'enter: DONE' is "DONE/enter".
func xbasNest(b *xbasBuilder, ids ...string) []*xbasState {
	out := make([]*xbasState, len(ids))

	var declare func(b *xbasBuilder, i int)
	declare = func(b *xbasBuilder, i int) {
		if i+1 < len(ids) {
			out[i] = b.State(ids[i], func(b *xbasBuilder) {
				b.Initial(ids[i+1])
				declare(b, i+1)
			})
		} else {
			out[i] = b.State(ids[i])
		}

		out[i].Entry("enter", noop).Exit("exit", noop)
	}
	declare(b, 0)

	return out
}

// xbasDeepToDone is the chart of the five cases that leave A's subtree for
// DONE. wire declares the transition under test.
func xbasDeepToDone(t *testing.T, wire func(b *xbasBuilder, chain []*xbasState)) *xbasMachine {
	t.Helper()

	m, err := sc.New("deep", func(b *xbasBuilder) {
		b.Initial("A")
		xbasNest(b, "DONE")
		wire(b, xbasNest(b, "A", "B", "C", "D"))
	})
	require.NoError(t, err)

	return m
}

// xbasDeepToP is the chart of the four cases that leave A's subtree for P's.
func xbasDeepToP(t *testing.T, wire func(chain []*xbasState)) *xbasMachine {
	t.Helper()

	m, err := sc.New("deep", func(b *xbasBuilder) {
		b.Initial("A")
		wire(xbasNest(b, "A", "B", "C", "D"))
		xbasNest(b, "P", "Q", "R", "S")
	})
	require.NoError(t, err)

	return m
}

// xbasDeepActions starts m, sends ev and returns the actions that caused.
func xbasDeepActions(m *xbasMachine, ev any) []string {
	snap, _ := m.Start(counter{N: 0}, epoch)
	_, effects := m.Transition(snap, ev, epoch)

	return xbasActions(effects)
}

var (
	xbasExitToDone = []string{"D/exit", "C/exit", "B/exit", "A/exit", "DONE/enter"}
	xbasExitToS    = []string{"D/exit", "C/exit", "B/exit", "A/exit", "P/enter", "Q/enter", "R/enter", "S/enter"}
)

// XState: deep transitions > exiting super/substates > should exit all
// substates when superstates exits
func TestXBasDeepExitsAllSubstatesWhenSuperstateExits(t *testing.T) {
	t.Parallel()

	// The XState chart also has a state FAIL that nothing targets.
	m, err := sc.New("deep", func(b *xbasBuilder) {
		b.Initial("A")
		xbasNest(b, "DONE")
		xbasNest(b, "FAIL")
		xbasNest(b, "A", "B", "C", "D")[0].On[xbasAEvent]().To("DONE")
	}, sc.AllowUnreachable())
	require.NoError(t, err)

	require.Equal(t, xbasExitToDone, xbasDeepActions(m, xbasAEvent{}))
}

// XState: deep transitions > exiting super/substates > should exit substates
// and superstates when exiting (B_EVENT)
func TestXBasDeepExitsOnBEvent(t *testing.T) {
	t.Parallel()

	m := xbasDeepToDone(t, func(_ *xbasBuilder, chain []*xbasState) {
		chain[1].On[xbasBEvent]().To("DONE")
	})

	require.Equal(t, xbasExitToDone, xbasDeepActions(m, xbasBEvent{}))
}

// XState: deep transitions > exiting super/substates > should exit substates
// and superstates when exiting (C_EVENT)
func TestXBasDeepExitsOnCEvent(t *testing.T) {
	t.Parallel()

	m := xbasDeepToDone(t, func(_ *xbasBuilder, chain []*xbasState) {
		chain[2].On[xbasCEvent]().To("DONE")
	})

	require.Equal(t, xbasExitToDone, xbasDeepActions(m, xbasCEvent{}))
}

// XState: deep transitions > exiting super/substates > should exit
// superstates when exiting (D_EVENT)
func TestXBasDeepExitsOnDEvent(t *testing.T) {
	t.Parallel()

	m := xbasDeepToDone(t, func(_ *xbasBuilder, chain []*xbasState) {
		chain[3].On[xbasDEvent]().To("DONE")
	})

	require.Equal(t, xbasExitToDone, xbasDeepActions(m, xbasDEvent{}))
}

// XState: deep transitions > exiting super/substates > should exit substate
// when machine handles event (MACHINE_EVENT)
func TestXBasDeepExitsOnMachineEvent(t *testing.T) {
	t.Parallel()

	m := xbasDeepToDone(t, func(b *xbasBuilder, _ []*xbasState) {
		b.On[xbasMachineEvent]().To("DONE")
	})

	require.Equal(t, xbasExitToDone, xbasDeepActions(m, xbasMachineEvent{}))
}

// XState: deep transitions > exiting super/substates > should exit deep and
// enter deep (A_S)
func TestXBasDeepExitsDeepAndEntersDeepAS(t *testing.T) {
	t.Parallel()

	m := xbasDeepToP(t, func(chain []*xbasState) {
		chain[0].On[xbasAS]().To("S")
	})

	require.Equal(t, xbasExitToS, xbasDeepActions(m, xbasAS{}))
}

// XState: deep transitions > exiting super/substates > should exit deep and
// enter deep (D_P)
func TestXBasDeepExitsDeepAndEntersDeepDP(t *testing.T) {
	t.Parallel()

	m := xbasDeepToP(t, func(chain []*xbasState) {
		chain[3].On[xbasDP]().To("P")
	})

	require.Equal(t, xbasExitToS, xbasDeepActions(m, xbasDP{}))
}

// XState: deep transitions > exiting super/substates > should exit deep and
// enter deep when targeting an ancestor of the final resolved deep target
func TestXBasDeepExitsDeepAndEntersDeepTargetingAncestor(t *testing.T) {
	t.Parallel()

	m := xbasDeepToP(t, func(chain []*xbasState) {
		chain[0].On[xbasAP]().To("P")
	})

	require.Equal(t, xbasExitToS, xbasDeepActions(m, xbasAP{}))
}

// XState: deep transitions > exiting super/substates > should exit deep and
// enter deep when targeting a deep state
func TestXBasDeepExitsDeepAndEntersDeepTargetingDeepState(t *testing.T) {
	t.Parallel()

	m := xbasDeepToP(t, func(chain []*xbasState) {
		chain[3].On[xbasDS]().To("S")
	})

	require.Equal(t, xbasExitToS, xbasDeepActions(m, xbasDS{}))
}

// ---------------------------------------------------------------------------
// initial.test.ts
// ---------------------------------------------------------------------------

// xbasRegion declares the state p of the parallel cases: p > a > b > c, with
// a second child, leaf. The identifiers carry p as a prefix because XState
// repeats the keys a, b, c and leaf under every parent.
func xbasRegion(b *xbasBuilder, p string) {
	b.State(p, func(b *xbasBuilder) {
		b.Initial(p + ".a")
		b.State(p+".a", func(b *xbasBuilder) {
			b.Initial(p + ".a.b")
			b.State(p+".a.b", func(b *xbasBuilder) {
				b.Initial(p + ".a.b.c")
				b.State(p + ".a.b.c")
			})
		})
		b.State(p + ".leaf")
	})
}

// XState: Initial states > should support object syntax for initial
//
// sc has one form for the initial state, so this and the next case only
// differ from the third in the chart.
func TestXBasInitialObjectSyntax(t *testing.T) {
	t.Parallel()

	m, err := sc.New("initial", func(b *xbasBuilder) {
		b.Initial("a")
		b.State("a")
		b.State("b")
	}, sc.AllowUnreachable())
	require.NoError(t, err)

	snap, _ := m.Start(counter{N: 0}, epoch)

	require.Equal(t, []string{"a"}, snap.Active())
}

// XState: Initial states > should support nested object syntax for initial
func TestXBasInitialNestedObjectSyntax(t *testing.T) {
	t.Parallel()

	m, err := sc.New("initial", func(b *xbasBuilder) {
		b.Initial("a")
		b.State("a", func(b *xbasBuilder) {
			b.Initial("a1")
			b.State("a1")
			b.State("a2")
		})
		b.State("b")
	}, sc.AllowUnreachable())
	require.NoError(t, err)

	snap, _ := m.Start(counter{N: 0}, epoch)

	require.Equal(t, []string{"a", "a1"}, snap.Active())
}

// XState: Initial states > should return the correct initial state
func TestXBasInitialReturnsCorrectInitialState(t *testing.T) {
	t.Parallel()

	m, err := sc.New("initial", func(b *xbasBuilder) {
		b.Initial("a")
		b.State("a", func(b *xbasBuilder) {
			b.Initial("b")
			b.State("b", func(b *xbasBuilder) {
				b.Initial("c")
				b.State("c")
			})
		})
		b.State("leaf")
	}, sc.AllowUnreachable())
	require.NoError(t, err)

	snap, _ := m.Start(counter{N: 0}, epoch)

	require.Equal(t, []string{"a", "b", "c"}, snap.Active())
}

// XState: Initial states > should return the correct initial state
// (parallel)
func TestXBasInitialReturnsCorrectInitialStateParallel(t *testing.T) {
	t.Parallel()

	m, err := sc.New("initial", func(b *xbasBuilder) {
		b.Regions()
		xbasRegion(b, "foo")
		xbasRegion(b, "bar")
	}, sc.AllowUnreachable())
	require.NoError(t, err)

	snap, _ := m.Start(counter{N: 0}, epoch)

	require.Equal(t, []string{
		"foo", "foo.a", "foo.a.b", "foo.a.b.c",
		"bar", "bar.a", "bar.a.b", "bar.a.b.c",
	}, snap.Active())
}

// XState: Initial states > should return the correct initial state (deep
// parallel)
func TestXBasInitialReturnsCorrectInitialStateDeepParallel(t *testing.T) {
	t.Parallel()

	m, err := sc.New("initial", func(b *xbasBuilder) {
		b.Initial("one")
		b.Parallel("one", func(b *xbasBuilder) {
			xbasRegion(b, "one.foo")
			xbasRegion(b, "one.bar")
		})
		b.Parallel("two", func(b *xbasBuilder) {
			xbasRegion(b, "two.foo")
			xbasRegion(b, "two.bar")
		})
	}, sc.AllowUnreachable())
	require.NoError(t, err)

	snap, _ := m.Start(counter{N: 0}, epoch)

	require.Equal(t, []string{
		"one",
		"one.foo", "one.foo.a", "one.foo.a.b", "one.foo.a.b.c",
		"one.bar", "one.bar.a", "one.bar.a.b", "one.bar.a.b.c",
	}, snap.Active())
}

// ---------------------------------------------------------------------------
// multiple.test.ts
// ---------------------------------------------------------------------------

// xbasPair declares a region with two atomic states, the first its initial
// state.
func xbasPair(b *xbasBuilder, region, initial, other string) {
	b.State(region, func(b *xbasBuilder) {
		b.Initial(initial)
		b.State(initial)
		b.State(other)
	})
}

// xbasMultiple is the suite's machine. A transition with several targets is
// To(target, also...).
func xbasMultiple(t *testing.T) *xbasMachine {
	t.Helper()

	m, err := sc.New("multiple", func(b *xbasBuilder) {
		b.Initial("simple")
		simple := b.State("simple")
		simple.On[xbasDeepM]().To("M")
		simple.On[xbasDeepCM]().To("C", "M")
		simple.On[xbasDeepMR]().To("M", "R")
		simple.On[xbasDeepCMR]().To("C", "M", "R")
		simple.On[xbasInitial]().To("para")
		b.State("other", func(b *xbasBuilder) {
			b.Initial("X")
			b.State("X")
		})
		b.Parallel("para", func(b *xbasBuilder) {
			xbasPair(b, "A", "B", "C")
			xbasPair(b, "K", "L", "M")
			xbasPair(b, "P", "Q", "R")
		})
		b.Parallel("para2", func(b *xbasBuilder) {
			xbasPair(b, "A2", "B2", "C2")
			b.State("K2", func(b *xbasBuilder) {
				b.Initial("L2")
				b.Parallel("L2", func(b *xbasBuilder) {
					xbasPair(b, "L2A", "L2B", "L2C")
					xbasPair(b, "L2K", "L2L", "L2M")
					xbasPair(b, "L2P", "L2Q", "L2R")
				})
				b.Parallel("M2", func(b *xbasBuilder) {
					xbasPair(b, "M2A", "M2B", "M2C")
					xbasPair(b, "M2K", "M2L", "M2M")
					xbasPair(b, "M2P", "M2Q", "M2R")
				})
			})
			xbasPair(b, "P2", "Q2", "R2")
		})
	}, sc.AllowUnreachable())
	require.NoError(t, err)

	return m
}

// XState: multiple > transitions to parallel states > should enter initial
// states of parallel states
func TestXBasMultipleEntersInitialStatesOfParallelStates(t *testing.T) {
	t.Parallel()

	m := xbasMultiple(t)
	snap, _ := m.Start(counter{N: 0}, epoch)
	snap, _ = m.Transition(snap, xbasInitial{}, epoch)

	require.Equal(t, []string{"para", "A", "B", "K", "L", "P", "Q"}, snap.Active())
}

// XState: multiple > transitions to parallel states > should enter specific
// states in one region
func TestXBasMultipleEntersSpecificStatesInOneRegion(t *testing.T) {
	t.Parallel()

	m := xbasMultiple(t)
	snap, _ := m.Start(counter{N: 0}, epoch)
	snap, _ = m.Transition(snap, xbasDeepM{}, epoch)

	require.Equal(t, []string{"para", "A", "B", "K", "M", "P", "Q"}, snap.Active())
}

// XState: multiple > transitions to parallel states > should enter specific
// states in all regions
func TestXBasMultipleEntersSpecificStatesInAllRegions(t *testing.T) {
	t.Parallel()

	m := xbasMultiple(t)
	snap, _ := m.Start(counter{N: 0}, epoch)
	snap, _ = m.Transition(snap, xbasDeepCMR{}, epoch)

	require.Equal(t, []string{"para", "A", "C", "K", "M", "P", "R"}, snap.Active())
}

// XState: multiple > transitions to parallel states > should enter specific
// states in some regions
func TestXBasMultipleEntersSpecificStatesInSomeRegions(t *testing.T) {
	t.Parallel()

	m := xbasMultiple(t)
	snap, _ := m.Start(counter{N: 0}, epoch)
	snap, _ = m.Transition(snap, xbasDeepMR{}, epoch)

	require.Equal(t, []string{"para", "A", "B", "K", "M", "P", "R"}, snap.Active())
}

// ---------------------------------------------------------------------------
// order.test.ts
// ---------------------------------------------------------------------------

// XState: document order > should specify the correct document order for
// each state node
//
// Partial: sc does not expose a state's order as a number. What it does
// expose is Snapshot.Active, which lists the active states in document
// order, so the port places the chart in every configuration it has, naming
// the leaves out of order, and reads the order off the snapshots. The chart
// itself, which XState numbers 0, is not listed among the active states.
func TestXBasOrderDocumentOrder(t *testing.T) {
	t.Parallel()

	m, err := sc.New("order", func(b *xbasBuilder) {
		b.Initial("one")
		b.State("one", func(b *xbasBuilder) {
			b.Initial("two")
			b.State("two")
			b.State("three", func(b *xbasBuilder) {
				b.Initial("four")
				b.State("four")
				b.State("five", func(b *xbasBuilder) {
					b.Initial("six")
					b.State("six")
				})
			})
		})
		b.Parallel("seven", func(b *xbasBuilder) {
			b.State("eight", func(b *xbasBuilder) {
				b.Initial("nine")
				b.State("nine")
				b.State("ten", func(b *xbasBuilder) {
					b.Initial("eleven")
					b.State("eleven")
					b.State("twelve")
				})
			})
			b.Parallel("thirteen", func(b *xbasBuilder) {
				b.State("fourteen")
				b.State("fifteen")
			})
		})
	}, sc.AllowUnreachable())
	require.NoError(t, err)

	order := []string{
		"one", "two", "three", "four", "five", "six", "seven", "eight", "nine",
		"ten", "eleven", "twelve", "thirteen", "fourteen", "fifteen",
	}
	position := map[string]int{}
	for i, s := range order {
		position[s] = i + 1
	}

	configurations := []struct {
		leaves []string
		active []string
	}{
		{leaves: []string{"two"}, active: []string{"one", "two"}},
		{leaves: []string{"four"}, active: []string{"one", "three", "four"}},
		{leaves: []string{"six"}, active: []string{"one", "three", "five", "six"}},
		{
			leaves: []string{"fifteen", "fourteen", "nine"},
			active: []string{"seven", "eight", "nine", "thirteen", "fourteen", "fifteen"},
		},
		{
			leaves: []string{"fifteen", "eleven", "fourteen"},
			active: []string{"seven", "eight", "ten", "eleven", "thirteen", "fourteen", "fifteen"},
		},
		{
			leaves: []string{"fourteen", "fifteen", "twelve"},
			active: []string{"seven", "eight", "ten", "twelve", "thirteen", "fourteen", "fifteen"},
		},
	}

	seen := map[string]bool{}
	for _, cfg := range configurations {
		active := xbasIn(t, m, cfg.leaves...).Active()
		require.Equal(t, cfg.active, active)

		for i, s := range active {
			seen[s] = true
			if i > 0 {
				require.Less(t, position[active[i-1]], position[s])
			}
		}
	}
	require.Len(t, seen, len(order))
}

// ---------------------------------------------------------------------------
// macrostepBound.test.ts
// ---------------------------------------------------------------------------

// xbasCountingTo is XState's countingMachine: an eventless transition that
// counts the context up to target. XState's maxIterations is sc.MaxSteps,
// passed in opts.
func xbasCountingTo(t *testing.T, target int, opts ...sc.Option) *xbasMachine {
	t.Helper()

	m, err := sc.New("counter", func(b *xbasBuilder) {
		b.Initial("idle")
		b.State("idle").On[xbasStart]().To("counting")
		b.State("counting").Always().
			When("belowTarget", func(c counter, _ struct{}) bool { return c.N < target }).
			Assign("inc", increment[struct{}])
	}, opts...)
	require.NoError(t, err)

	return m
}

// XState: macrostep bound > throws InfiniteTransitionError at maxIterations
// for an always loop
//
// DIVERGES from XState: the actor goes to its error status and hands its
// error subscribers an InfiniteTransitionError with maxIterations, the
// actor's id and the event as fields. sc panics in Machine.Transition, and
// the bound, the machine's id and the event's type are in the message only.
func TestXBasBoundAlwaysLoop(t *testing.T) {
	t.Parallel()

	m, err := sc.New("loop", func(b *xbasBuilder) {
		b.Initial("idle")
		b.State("idle").On[xbasGo]().To("a")
		b.State("a").Always().To("b")
		b.State("b").Always().To("a")
	}, sc.MaxSteps(1000))
	require.NoError(t, err)

	snap, _ := m.Start(counter{N: 0}, epoch)

	require.PanicsWithValue(t, xbasCycle("loop", 1000, "sc_test.xbasGo", "b"), func() { m.Transition(snap, xbasGo{}, epoch) })
}

// XState: macrostep bound > names the actor, the event and the last 5 states
// in the message
//
// DIVERGES from XState: the message names the actor, the event, the bound
// and the last five states the cycle went through. sc's panic names the
// machine, the bound and the event's Go type, but only the states active
// when it gave up, not the ones before them.
func TestXBasBoundMessage(t *testing.T) {
	t.Parallel()

	m, err := sc.New("looper", func(b *xbasBuilder) {
		b.Initial("idle")
		b.State("idle").On[xbasGo]().To("a")
		b.State("a").Always().To("b")
		b.State("b").Always().To("c")
		b.State("c").Always().To("a")
	}, sc.MaxSteps(10))
	require.NoError(t, err)

	snap, _ := m.Start(counter{N: 0}, epoch)

	require.PanicsWithValue(t, xbasCycle("looper", 10, "sc_test.xbasGo", "c"), func() { m.Transition(snap, xbasGo{}, epoch) })
}

// XState: macrostep bound > is unbounded by default and warns once per
// machine after 1000 microsteps
//
// Partial: the console warning is dropped. sc is not unbounded, but 1400
// steps are well inside its default bound of 10,000.
func TestXBasBoundLongRunsSettle(t *testing.T) {
	t.Parallel()

	m := xbasCountingTo(t, 1400)

	for range 2 {
		snap, _ := m.Start(counter{N: 0}, epoch)
		snap, _ = m.Transition(snap, xbasStart{}, epoch)

		require.False(t, snap.Done())
		require.Equal(t, 1400, snap.Context.N)
	}
}

// XState: macrostep bound > does not warn for 1000 microsteps or when
// maxIterations is set
//
// Partial: the case is about the warning; what is left is that both runs
// settle.
func TestXBasBoundShortRunsSettle(t *testing.T) {
	t.Parallel()

	short := xbasCountingTo(t, 999)
	snap, _ := short.Start(counter{N: 0}, epoch)
	snap, _ = short.Transition(snap, xbasStart{}, epoch)
	require.Equal(t, 999, snap.Context.N)

	bounded := xbasCountingTo(t, 1400, sc.MaxSteps(5000))
	snap, _ = bounded.Start(counter{N: 0}, epoch)
	snap, _ = bounded.Transition(snap, xbasStart{}, epoch)
	require.Equal(t, 1400, snap.Context.N)
}

// XState: macrostep bound > allows 3000 microsteps with maxIterations: 5000
//
// As in XState, the same 3000 steps fail with a bound of 1000 and pass with
// 5000. Failing is a panic in sc (see TestXBasBoundAlwaysLoop).
func TestXBasBoundAllowsThreeThousandSteps(t *testing.T) {
	t.Parallel()

	failing := xbasCountingTo(t, 3000, sc.MaxSteps(1000))
	start, _ := failing.Start(counter{N: 0}, epoch)
	require.PanicsWithValue(t, xbasCycle("counter", 1000, "sc_test.xbasStart", "counting"), func() { failing.Transition(start, xbasStart{}, epoch) })

	m := xbasCountingTo(t, 3000, sc.MaxSteps(5000))
	snap, _ := m.Start(counter{N: 0}, epoch)
	snap, _ = m.Transition(snap, xbasStart{}, epoch)

	require.False(t, snap.Done())
	require.Equal(t, 3000, snap.Context.N)
}

// Not ported.
//
// Partial event descriptors. sc dispatches on the event's Go type, and its
// only wildcard is OnAny, so a descriptor such as 'foo.*' has no form:
//   - eventDescriptors: should prioritize explicit descriptor even if a
//     partial one comes first (with OnAny for 'foo.*' it is the wildcard
//     case above)
//   - eventDescriptors: should prioritize a longer descriptor even if the
//     shorter one comes first
//   - eventDescriptors: should use a shorter descriptor if the longer one
//     doesn't match
//   - eventDescriptors: should NOT support non-tokenized wildcards
//   - eventDescriptors: should support prefix matching with wildcards (+0)
//   - eventDescriptors: should support prefix matching with wildcards (+1)
//   - eventDescriptors: should support prefix matching with wildcards (+n)
//   - eventDescriptors: should support prefix matching with wildcards (+n,
//     multi-prefix)
//   - eventDescriptors: should not match infix wildcards (also asserts
//     console warnings)
//   - eventDescriptors: should not match wildcards as part of tokens (also
//     asserts console warnings)
//
// TypeScript types and helpers:
//   - eventDescriptors: should throw if assertEvent partial descriptor does
//     not match (assertEvent is a TypeScript helper)
//   - internalEvents: keeps setup-level internal events out of the public
//     protocol (type-level only)
//   - internalEvents: keeps registered child internal events private for
//     both spawn forms (type-level only)
//
// Configuration keys XState removed. sc has no configuration object:
//   - internalEvents: throws in development for the removed top-level
//     internalEvents key
//   - internalEvents: throws in development for an internalEvents key passed
//     to setup()
//
// Actor references. One actor cannot be handed another to send to; a chart
// sends only to the services it invokes:
//   - unhandledEvents: reports an unhandled actor-sent %s event (xstate.custom
//     and xstate.done.actor.save)
//
// Console warnings:
//   - unhandledEvents: warns once per event type per actor in development
//
// Routes. sc has no 'xstate.route' event or routable states:
//   - unhandledEvents: reports xstate.route events that match no routable
//     state
