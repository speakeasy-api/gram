package sc_test

// Ports of XState's packages/core/test/interpreter.test.ts, waitFor.test.ts,
// clock.test.ts and logical-timers.test.ts (6.0.0-beta.2), one test per
// `it`, in order.
//
// createActor(machine) is Machine.Actor, which does nothing until Run, and
// createActor(machine).start() is Machine.Instance where the case only sends
// events and reads the state, since Send returns once the event and its
// actions are done. actor.stop() is Instance.Close, or cancelling the
// context an Actor runs under. actor.subscribe is Hooks.Transition or
// Actor.Snapshots, and a completion observer is Run returning or
// Snapshot.Done. SimulatedClock is WithClock with ManualTimers and
// Instance.Advance, or explicit times given to the pure core. waitFor is
// Await under a context.
//
// XState raises delayed events from entry actions, and so does sc
// (StateBuilder.RaiseAfterOnEntry).
//
// The cases that are not ported are listed at the end of the file.

import (
	"context"
	"encoding/json"
	"errors"
	"iter"
	"strconv"
	"sync"
	"sync/atomic"
	"testing"
	"testing/synctest"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/sc"
)

type (
	xintTimer      struct{}
	xintKeepGoing  struct{}
	xintActivate   struct{ Wait int }
	xintFireDelay  struct{ Value int }
	xintTurnOff    struct{}
	xintToggle     struct{}
	xintSwitch     struct{}
	xintFoo        struct{}
	xintBar        struct{}
	xintNextA      struct{}
	xintNextB      struct{}
	xintSomeEvent  struct{}
	xintLog        struct{}
	xintExternal   struct{}
	xintRaised     struct{}
	xintNext       struct{ Password string }
	xintEvent      struct{ ID int }
	xintInactivate struct{}
	xintTrigger    struct{ Self *xintTrigger }
	xintStart      struct{}
	xintFinish     struct{}
	xintInc        struct{}
	xintFire       struct{}
	xintFired      struct{}
	xintTick       struct{ N int }
	xintEv         struct{}
	xintExpire     struct{}
	xintInternal   struct{}
	xintIgnored    struct{}
	xintSchedule   struct{}
	xintFirst      struct{}
	xintSecond     struct{}
	xintLate       struct{}
	xintExit       struct{}
	xintNew1       struct{}
)

// xintDelays is the context of the charts whose delays are computed.
type xintDelays struct {
	Delay int `json:"delay"`
	Wait  int `json:"wait"`
}

// xintPassword is the context of the charts that raise an event built from
// the context.
type xintPassword struct {
	Password string `json:"password"`
}

// xintTimes is the context of the charts that record when their timers
// fired, in milliseconds after the start, or which of them fired.
type xintTimes struct {
	At    []int    `json:"at"`
	Trace []string `json:"trace"`
}

type (
	xintBuilder  = sc.Builder[string, counter]
	xintMachine  = sc.Machine[string, counter]
	xintSnapshot = sc.Snapshot[string, counter]
)

func xintMs(n int) time.Duration { return time.Duration(n) * time.Millisecond }

// xintNew declares a chart with the counter context.
func xintNew(t *testing.T, id string, def func(b *xintBuilder), opts ...sc.Option) *xintMachine {
	t.Helper()

	m, err := sc.New(id, def, opts...)
	require.NoError(t, err)

	return m
}

// xintIn is an Await condition: every one of states is active.
func xintIn(states ...string) func(xintSnapshot) bool {
	return func(s xintSnapshot) bool {
		for _, state := range states {
			if !s.In(state) {
				return false
			}
		}

		return true
	}
}

func xintDone(s xintSnapshot) bool { return s.Done() }

// xintClock is SimulatedClock: a clock a test moves by hand.
type xintClock struct {
	mu  sync.Mutex
	now time.Time
}

func xintNewClock() *xintClock { return &xintClock{mu: sync.Mutex{}, now: epoch} }

func (c *xintClock) Now() time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()

	return c.now
}

func (c *xintClock) Add(d time.Duration) {
	c.mu.Lock()
	defer c.mu.Unlock()

	c.now = c.now.Add(d)
}

// xintIncrement is clock.increment(ms) for an instance with manual timers:
// it moves the clock and lets the instance fire what fell due.
func xintIncrement[C any](t *testing.T, clock *xintClock, inst *sc.Instance[string, C], ms int) {
	t.Helper()

	clock.Add(xintMs(ms))
	_, err := inst.Advance(t.Context())
	require.NoError(t, err)
}

// xintSleep moves the fake clock of a synctest bubble and lets the
// goroutines it woke settle.
func xintSleep(d time.Duration) {
	time.Sleep(d) //nolint:forbidigo // GG013: advances the synctest fake clock instantly (the only way to move past a timer inside a synctest.Test bubble); this exemption is valid ONLY within synctest.Test — do not copy it to a real-time time.Sleep
	synctest.Wait()
}

// xintRecorder collects what a chart's actions record.
type xintRecorder struct {
	mu      sync.Mutex
	entries []string
}

func xintNewLog() *xintRecorder { return &xintRecorder{mu: sync.Mutex{}, entries: nil} }

func (l *xintRecorder) add(entry string) {
	l.mu.Lock()
	defer l.mu.Unlock()

	l.entries = append(l.entries, entry)
}

func (l *xintRecorder) all() []string {
	l.mu.Lock()
	defer l.mu.Unlock()

	return append([]string{}, l.entries...)
}

// xintLight is XState's lightMachine. green and yellow raise TIMER 10ms
// after they are entered; green's raise is named TIMER1, which KEEP_GOING
// cancels.
func xintLight(t *testing.T) *xintMachine {
	t.Helper()

	timer := func(counter) any { return xintTimer{} }

	return xintNew(t, "light", func(b *xintBuilder) {
		b.Initial("green")
		green := b.State("green").RaiseAfterOnEntry("TIMER1", xintMs(10), timer)
		green.On[xintTimer]().To("yellow")
		green.On[xintKeepGoing]().CancelRaise("TIMER1")
		b.State("yellow").RaiseAfterOnEntry("yellowTimer", xintMs(10), timer).On[xintTimer]().To("red")
		b.State("red").After(xintMs(10)).To("green")
	})
}

// XState: interpreter > initial state > .getSnapshot returns the initial
// state
func TestXIntInterpreterGetSnapshotReturnsInitialState(t *testing.T) {
	t.Parallel()

	m := xintNew(t, "m", func(b *xintBuilder) {
		b.Initial("foo")
		b.State("bar")
		b.State("foo")
	}, sc.AllowUnreachable())

	a := m.Actor(counter{N: 0})

	require.Equal(t, []string{"foo"}, a.Snapshot().Active())
}

// XState: interpreter > initial state > initially spawned actors should not
// be spawned when reading initial state
//
// The promise XState spawns from the entry action is a service the state
// invokes.
func TestXIntInterpreterInitialServicesNotStartedWhenReadingInitialState(t *testing.T) {
	t.Parallel()

	synctest.Test(t, func(t *testing.T) {
		var spawned atomic.Int32

		m := xintNew(t, "m", func(b *xintBuilder) {
			b.Initial("idle")
			b.State("idle").Invoke("promise", func(ctx context.Context, _ counter) (int, error) {
				spawned.Add(1)
				<-ctx.Done()

				return 0, ctx.Err()
			}).OnError().Stay()
		})

		a := m.Actor(counter{N: 0})
		require.Zero(t, spawned.Load())

		a.Snapshot()
		a.Snapshot()
		a.Snapshot()
		synctest.Wait()
		require.Zero(t, spawned.Load())

		ctx, cancel := context.WithCancel(t.Context())
		wait := runActor(ctx, a)

		xintSleep(xintMs(100))
		require.Equal(t, int32(1), spawned.Load())

		cancel()
		require.ErrorIs(t, wait(), context.Canceled)
	})
}

// xintRestored stores an instance's snapshot and continues another instance
// from it, as getPersistedSnapshot and restoreActor do.
func xintRestored(t *testing.T, m *xintMachine, inst *sc.Instance[string, counter]) *sc.Instance[string, counter] {
	t.Helper()

	data, err := json.Marshal(inst.Snapshot())
	require.NoError(t, err)

	snap, _, err := m.Restore(data, epoch)
	require.NoError(t, err)

	restored, err := m.InstanceFrom(t.Context(), snap)
	require.NoError(t, err)
	t.Cleanup(restored.Close)

	return restored
}

// XState: interpreter > initial state > does not execute actions from a
// restored state
func TestXIntInterpreterDoesNotExecuteActionsFromRestoredState(t *testing.T) {
	t.Parallel()

	var called atomic.Bool

	m := xintNew(t, "m", func(b *xintBuilder) {
		b.Initial("green")
		b.State("green").On[xintTimer]().
			Do("call", func(context.Context, counter, xintTimer) { called.Store(true) }).
			To("yellow")
		b.State("yellow").On[xintTimer]().To("red")
		b.State("red").On[xintTimer]().To("green")
	})

	inst, err := m.Instance(t.Context(), counter{N: 0})
	require.NoError(t, err)
	defer inst.Close()

	_, err = inst.Send(t.Context(), xintTimer{})
	require.NoError(t, err)
	require.True(t, called.Load())
	called.Store(false)

	restored := xintRestored(t, m, inst)

	require.Equal(t, []string{"yellow"}, restored.Snapshot().Active())
	require.False(t, called.Load())
}

// XState: interpreter > initial state > should not execute actions that are
// not part of the actual persisted state
func TestXIntInterpreterDoesNotExecuteActionsOutsidePersistedState(t *testing.T) {
	t.Parallel()

	var called atomic.Bool

	m := xintNew(t, "m", func(b *xintBuilder) {
		b.Initial("a")
		a := b.State("a")
		a.Entry("call", func(context.Context, counter) { called.Store(true) })
		a.Always().To("b")
		b.State("b")
	})

	inst, err := m.Instance(t.Context(), counter{N: 0})
	require.NoError(t, err)
	defer inst.Close()

	require.True(t, called.Load())
	called.Store(false)
	require.Equal(t, []string{"b"}, inst.Snapshot().Active())

	xintRestored(t, m, inst)

	require.False(t, called.Load())
}

// XState: interpreter > subscribing > should not notify subscribers of the
// current state upon subscription (subscribe)
func TestXIntInterpreterSubscribingDoesNotNotifyOfCurrentState(t *testing.T) {
	t.Parallel()

	synctest.Test(t, func(t *testing.T) {
		m := xintNew(t, "m", func(b *xintBuilder) {
			b.Initial("active")
			b.State("active")
		})

		// The hooks, which are given when the driver is built, hear
		// nothing of the state the instance starts in.
		var transitions, ignored atomic.Int32
		inst, err := m.Instance(t.Context(), counter{N: 0}, sc.WithHooks(&sc.Hooks[string, counter]{
			Transition:     func(context.Context, sc.Step[string, counter]) { transitions.Add(1) },
			Ignored:        func(context.Context, xintSnapshot, any) { ignored.Add(1) },
			EffectStarted:  nil,
			EffectFinished: nil,
		}))
		require.NoError(t, err)
		defer inst.Close()

		require.Zero(t, transitions.Load())
		require.Zero(t, ignored.Load())

		// DIVERGES from XState: XState's subscribe does not call the
		// observer with the state the actor is in. Actor.Snapshots, the
		// only subscription that can begin after the actor has started,
		// yields the current snapshot first.
		ctx, cancel := context.WithCancel(t.Context())
		a := m.Actor(counter{N: 0})
		wait := runActor(ctx, a)
		synctest.Wait()

		var seen atomic.Int32
		watched := make(chan struct{})
		go func() {
			defer close(watched)
			for range a.Snapshots(ctx) {
				seen.Add(1)
			}
		}()
		synctest.Wait()
		require.Equal(t, int32(1), seen.Load())

		cancel()
		require.ErrorIs(t, wait(), context.Canceled)
		<-watched
	})
}

// XState: interpreter > send with delay > can send an event after a delay
func TestXIntInterpreterSendWithDelayCanSendEventAfterDelay(t *testing.T) {
	t.Parallel()

	m := xintNew(t, "m", func(b *xintBuilder) {
		b.Initial("foo")
		b.State("foo").
			RaiseAfterOnEntry("timer", xintMs(10), func(counter) any { return xintTimer{} }).
			On[xintTimer]().To("bar")
		b.State("bar")
	})

	// An actor that was never run does nothing, whatever its clock says.
	idleClock := xintNewClock()
	idle := m.Actor(counter{N: 0}, sc.WithClock(idleClock.Now))
	require.Equal(t, []string{"foo"}, idle.Snapshot().Active())

	idleClock.Add(xintMs(10))
	require.Equal(t, []string{"foo"}, idle.Snapshot().Active())

	clock := xintNewClock()
	inst, err := m.Instance(t.Context(), counter{N: 0}, sc.WithClock(clock.Now), sc.ManualTimers())
	require.NoError(t, err)
	defer inst.Close()
	require.Equal(t, []string{"foo"}, inst.Snapshot().Active())

	xintIncrement(t, clock, inst, 5)
	require.Equal(t, []string{"foo"}, inst.Snapshot().Active())

	xintIncrement(t, clock, inst, 5)
	require.Equal(t, []string{"bar"}, inst.Snapshot().Active())
}

// xintDelayExpr is XState's delayExprMachine, and xintDelayExprStops runs the
// case both delay-expression tests share.
//
// DIVERGES from XState: XState's entry action raises FINISH after a delay it
// computes from the context and from the event that entered the state. sc's
// RaiseAfterOnEntry takes a fixed duration, and its computed delays (Delay)
// belong to delayed transitions and see only the context. So ACTIVATE copies its
// wait into the context, and pending leaves by a delayed transition instead
// of raising FINISH for itself.
func xintDelayExpr(t *testing.T) *sc.Machine[string, xintDelays] {
	t.Helper()

	m, err := sc.New("delayExpr", func(b *sc.Builder[string, xintDelays]) {
		b.Initial("idle")
		b.State("idle").On[xintActivate]().
			Assign("keepWait", func(c xintDelays, ev xintActivate) xintDelays {
				c.Wait = ev.Wait
				return c
			}).
			To("pending")
		b.State("pending").
			Delay("finishDelay", func(c xintDelays) time.Duration { return xintMs(c.Delay + c.Wait) }).
			To("finished")
		b.Final("finished")
	})
	require.NoError(t, err)

	return m
}

func xintDelayExprStops(t *testing.T) {
	t.Helper()

	m := xintDelayExpr(t)
	clock := xintNewClock()

	inst, err := m.Instance(t.Context(), xintDelays{Delay: 100, Wait: 0}, sc.WithClock(clock.Now), sc.ManualTimers())
	require.NoError(t, err)
	defer inst.Close()

	_, err = inst.Send(t.Context(), xintActivate{Wait: 50})
	require.NoError(t, err)

	xintIncrement(t, clock, inst, 101)
	require.False(t, inst.Snapshot().Done())

	xintIncrement(t, clock, inst, 50)
	require.True(t, inst.Snapshot().Done())
}

// XState: interpreter > send with delay > can send an event after a delay
// (expression)
func TestXIntInterpreterSendWithDelayExpression(t *testing.T) {
	t.Parallel()

	xintDelayExprStops(t)
}

// XState: interpreter > send with delay > can send an event after a delay
// (expression using _event)
//
// XState's two cases differ only in how the expression narrows the event's
// type, so here they are the same test.
func TestXIntInterpreterSendWithDelayExpressionUsingEvent(t *testing.T) {
	t.Parallel()

	xintDelayExprStops(t)
}

// XState: interpreter > send with delay > can send an event after a delay
// (delayed transitions)
func TestXIntInterpreterSendWithDelayDelayedTransitions(t *testing.T) {
	t.Parallel()

	someDelay := func(c xintDelays) time.Duration { return xintMs(c.Delay + 50) }

	m, err := sc.New("letter", func(b *sc.Builder[string, xintDelays]) {
		b.Initial("a")
		b.State("a").Delay("delayA", func(c xintDelays) time.Duration { return xintMs(c.Delay) }).To("b")
		b.State("b").Delay("someDelay", someDelay).To("c")
		// DIVERGES from XState: XState hands delayD the event that
		// entered d. An sc delay sees only the context, so the transition
		// copies the event's value into it first.
		b.State("c").
			RaiseAfterOnEntry("fireDelay", xintMs(20), func(xintDelays) any { return xintFireDelay{Value: 200} }).
			On[xintFireDelay]().
			Assign("keepValue", func(c xintDelays, ev xintFireDelay) xintDelays {
				c.Wait = ev.Value
				return c
			}).
			To("d")
		b.State("d").Delay("delayD", func(c xintDelays) time.Duration { return xintMs(c.Delay + c.Wait) }).To("e")
		b.State("e").Delay("someDelay", someDelay).To("f")
		b.Final("f")
	})
	require.NoError(t, err)

	clock := xintNewClock()
	inst, err := m.Instance(t.Context(), xintDelays{Delay: 100, Wait: 0}, sc.WithClock(clock.Now), sc.ManualTimers())
	require.NoError(t, err)
	defer inst.Close()

	require.Equal(t, []string{"a"}, inst.Snapshot().Active())
	xintIncrement(t, clock, inst, 100)
	require.Equal(t, []string{"b"}, inst.Snapshot().Active())
	xintIncrement(t, clock, inst, 100+50)
	require.Equal(t, []string{"c"}, inst.Snapshot().Active())
	xintIncrement(t, clock, inst, 20)
	require.Equal(t, []string{"d"}, inst.Snapshot().Active())
	xintIncrement(t, clock, inst, 100+200)
	require.Equal(t, []string{"e"}, inst.Snapshot().Active())
	xintIncrement(t, clock, inst, 100+50)

	require.True(t, inst.Snapshot().Done())
}

// xintActivity is the chart of the "activities" cases: on invokes a callback
// that counts its starts and its cleanups, until TURN_OFF.
func xintActivity(t *testing.T, started, cleaned *atomic.Int32) *xintMachine {
	t.Helper()

	return xintNew(t, "activity", func(b *xintBuilder) {
		b.Initial("on")
		on := b.State("on")
		on.Stream("myActivity", func(ctx context.Context, _ counter) iter.Seq2[any, error] {
			return func(func(any, error) bool) {
				started.Add(1)
				defer cleaned.Add(1)

				<-ctx.Done()
			}
		}).OnError().Stay()
		on.On[xintTurnOff]().To("off")
		b.State("off")
	})
}

// XState: interpreter > activities (deprecated) > should start activities
func TestXIntInterpreterActivitiesStart(t *testing.T) {
	t.Parallel()

	synctest.Test(t, func(t *testing.T) {
		var started, cleaned atomic.Int32
		m := xintActivity(t, &started, &cleaned)

		inst, err := m.Instance(t.Context(), counter{N: 0})
		require.NoError(t, err)
		defer inst.Close()

		synctest.Wait()
		require.Equal(t, int32(1), started.Load())
	})
}

// XState: interpreter > activities (deprecated) > should stop activities
func TestXIntInterpreterActivitiesStop(t *testing.T) {
	t.Parallel()

	synctest.Test(t, func(t *testing.T) {
		var started, cleaned atomic.Int32
		m := xintActivity(t, &started, &cleaned)

		inst, err := m.Instance(t.Context(), counter{N: 0})
		require.NoError(t, err)
		defer inst.Close()

		synctest.Wait()
		require.Zero(t, cleaned.Load())

		// The service's goroutine has returned by the time Send does.
		_, err = inst.Send(t.Context(), xintTurnOff{})
		require.NoError(t, err)

		require.Equal(t, int32(1), cleaned.Load())
	})
}

// XState: interpreter > activities (deprecated) > should stop activities
// upon stopping the service
func TestXIntInterpreterActivitiesStopWhenServiceStops(t *testing.T) {
	t.Parallel()

	synctest.Test(t, func(t *testing.T) {
		var started, cleaned atomic.Int32
		m := xintActivity(t, &started, &cleaned)

		inst, err := m.Instance(t.Context(), counter{N: 0})
		require.NoError(t, err)

		synctest.Wait()
		require.Zero(t, cleaned.Load())

		inst.Close()

		require.Equal(t, int32(1), cleaned.Load())
	})
}

// XState: interpreter > activities (deprecated) > should restart activities
// from a compound state
//
// XState skips this case (it.skip); sc passes it.
func TestXIntInterpreterActivitiesRestartFromCompoundState(t *testing.T) {
	t.Parallel()

	synctest.Test(t, func(t *testing.T) {
		var active atomic.Bool

		m := xintNew(t, "m", func(b *xintBuilder) {
			b.Initial("inactive")
			b.State("inactive").On[xintToggle]().To("active")
			on := b.State("active", func(b *xintBuilder) {
				b.Initial("A")
				b.State("A").On[xintSwitch]().To("B")
				b.State("B").On[xintSwitch]().To("A")
			})
			on.Stream("blink", func(ctx context.Context, _ counter) iter.Seq2[any, error] {
				return func(func(any, error) bool) {
					active.Store(true)
					defer active.Store(false)

					<-ctx.Done()
				}
			}).OnError().Stay()
			on.On[xintToggle]().To("inactive")
		})

		inst, err := m.Instance(t.Context(), counter{N: 0})
		require.NoError(t, err)

		_, err = inst.Send(t.Context(), xintToggle{})
		require.NoError(t, err)
		_, err = inst.Send(t.Context(), xintSwitch{})
		require.NoError(t, err)
		synctest.Wait()
		require.True(t, active.Load())

		data, err := json.Marshal(inst.Snapshot())
		require.NoError(t, err)
		inst.Close()
		require.False(t, active.Load())

		snap, _, err := m.Restore(data, epoch)
		require.NoError(t, err)
		restored, err := m.InstanceFrom(t.Context(), snap)
		require.NoError(t, err)
		defer restored.Close()

		synctest.Wait()
		require.Equal(t, []string{"active", "B"}, restored.Snapshot().Active())
		require.True(t, active.Load())
	})
}

// XState: interpreter > can cancel a delayed event
func TestXIntInterpreterCanCancelDelayedEvent(t *testing.T) {
	t.Parallel()

	m := xintLight(t)
	clock := xintNewClock()

	inst, err := m.Instance(t.Context(), counter{N: 0}, sc.WithClock(clock.Now), sc.ManualTimers())
	require.NoError(t, err)
	defer inst.Close()

	xintIncrement(t, clock, inst, 5)
	_, err = inst.Send(t.Context(), xintKeepGoing{})
	require.NoError(t, err)

	require.Equal(t, []string{"green"}, inst.Snapshot().Active())
	xintIncrement(t, clock, inst, 10)
	require.Equal(t, []string{"green"}, inst.Snapshot().Active())
}

// XState: interpreter > can cancel a delayed event using expression to
// resolve send id
//
// A raise's name is fixed when the chart is declared, so there is no
// expression to resolve. XState's entry action raises FOO and BAR and then
// cancels FOO. An sc state can raise after a delay on entry but cannot
// cancel there, so an eventless transition out of "boot" does all three on
// the way into first.
func TestXIntInterpreterCanCancelDelayedEventByID(t *testing.T) {
	t.Parallel()

	m := xintNew(t, "m", func(b *xintBuilder) {
		b.Initial("boot")
		b.State("boot").Always().
			RaiseAfter("foo", xintMs(100), func(counter, struct{}) any { return xintFoo{} }).
			RaiseAfter("bar", xintMs(200), func(counter, struct{}) any { return xintBar{} }).
			CancelRaise("foo").
			To("first")
		first := b.State("first")
		first.On[xintFoo]().To("fail")
		first.On[xintBar]().To("pass")
		b.Final("fail")
		b.Final("pass")
	})

	snap, _ := m.Start(counter{N: 0}, epoch)
	require.Len(t, snap.Timers(), 1)

	snap, _ = m.Advance(snap, epoch.Add(xintMs(100)))
	require.Equal(t, []string{"first"}, snap.Active())

	snap, _ = m.Advance(snap, epoch.Add(xintMs(200)))
	require.Equal(t, []string{"pass"}, snap.Active())
	require.True(t, snap.Done())
}

// XState: interpreter > should not throw an error if an event is sent to an
// uninitialized interpreter
func TestXIntInterpreterSendToUninitializedInterpreter(t *testing.T) {
	t.Parallel()

	synctest.Test(t, func(t *testing.T) {
		a := xintLight(t).Actor(counter{N: 0})

		// DIVERGES from XState: XState takes the event at once and keeps
		// it until the actor starts. An sc actor has no buffer: Send
		// blocks until Run takes the event, and gives up with the
		// context's error when the context ends first.
		ctx, cancel := context.WithTimeout(t.Context(), time.Second)
		defer cancel()

		err := a.Send(ctx, xintSomeEvent{})
		require.ErrorIs(t, err, context.DeadlineExceeded)
	})
}

// XState: interpreter > should defer events sent to an uninitialized service
func TestXIntInterpreterDefersEventsSentToUninitializedService(t *testing.T) {
	t.Parallel()

	synctest.Test(t, func(t *testing.T) {
		m := xintNew(t, "defer", func(b *xintBuilder) {
			b.Initial("a")
			b.State("a").On[xintNextA]().To("b")
			b.State("b").On[xintNextB]().To("c")
			b.Final("c")
		})

		a := m.Actor(counter{N: 0})

		// DIVERGES from XState: XState queues the two events and send
		// returns. In sc the sender waits: the events are deferred by
		// holding their sender up until the actor runs.
		sent := make(chan error, 1)
		go func() {
			if err := a.Send(t.Context(), xintNextA{}); err != nil {
				sent <- err
				return
			}
			sent <- a.Send(t.Context(), xintNextB{})
		}()

		synctest.Wait()
		require.Equal(t, []string{"a"}, a.Snapshot().Active())
		require.Empty(t, sent)

		require.NoError(t, a.Run(t.Context()))
		require.NoError(t, <-sent)
		require.Equal(t, []string{"c"}, a.Snapshot().Active())
	})
}

// XState: interpreter > should throw an error if initial state sent to
// interpreter is invalid
func TestXIntInterpreterInvalidInitialState(t *testing.T) {
	t.Parallel()

	// DIVERGES from XState: XState builds the machine and the actor, whose
	// snapshot then has the status 'error'. sc rejects the chart when it is
	// declared, so there is never an actor.
	_, err := sc.New("fetchMachine", func(b *xintBuilder) {
		b.Initial("create")
		b.State("edit", func(b *xintBuilder) {
			b.Initial("idle")
			b.State("idle").On[xintNext]().To("pending")
			b.State("pending")
		})
	})

	require.ErrorContains(t, err, "create")
}

// XState: interpreter > should not update when stopped
//
// XState warns that the event was not delivered; sc returns
// ErrInstanceClosed to the sender.
func TestXIntInterpreterDoesNotUpdateWhenStopped(t *testing.T) {
	t.Parallel()

	m := xintLight(t)
	clock := xintNewClock()

	inst, err := m.Instance(t.Context(), counter{N: 0}, sc.WithClock(clock.Now), sc.ManualTimers())
	require.NoError(t, err)

	_, err = inst.Send(t.Context(), xintTimer{})
	require.NoError(t, err)
	require.Equal(t, []string{"yellow"}, inst.Snapshot().Active())

	inst.Close()

	_, err = inst.Send(t.Context(), xintTimer{})
	require.ErrorIs(t, err, sc.ErrInstanceClosed)
	require.Equal(t, []string{"yellow"}, inst.Snapshot().Active())
}

// XState: interpreter > should be able to log (log action)
//
// sc has no log action and its drivers no logger; the chart's own action
// records what XState logs, the context the transition has just assigned.
func TestXIntInterpreterLogAction(t *testing.T) {
	t.Parallel()

	logs := xintNewLog()

	m := xintNew(t, "log", func(b *xintBuilder) {
		b.Initial("x")
		b.State("x").On[xintLog]().
			Assign("increment", increment[xintLog]).
			Do("log", func(_ context.Context, c counter, _ xintLog) { logs.add(strconv.Itoa(c.N)) })
	})

	inst, err := m.Instance(t.Context(), counter{N: 0})
	require.NoError(t, err)
	defer inst.Close()

	_, err = inst.Send(t.Context(), xintLog{})
	require.NoError(t, err)
	_, err = inst.Send(t.Context(), xintLog{})
	require.NoError(t, err)

	require.Equal(t, []string{"1", "2"}, logs.all())
}

// XState: interpreter > should receive correct event (log action)
func TestXIntInterpreterLogActionReceivesCorrectEvent(t *testing.T) {
	t.Parallel()

	logs := xintNewLog()
	name := func(ev any) string {
		switch ev.(type) {
		case xintExternal:
			return "EXTERNAL_EVENT"
		case xintRaised:
			return "RAISED_EVENT"
		default:
			return "?"
		}
	}

	m := xintNew(t, "m", func(b *xintBuilder) {
		b.Initial("foo")
		b.State("foo").On[xintExternal]().
			Raise("raise", func(counter, xintExternal) any { return xintRaised{} }).
			Do("log", func(_ context.Context, _ counter, ev xintExternal) { logs.add(name(ev)) })
		b.OnAny().Do("logAny", func(_ context.Context, _ counter, ev any) { logs.add(name(ev)) })
	})

	inst, err := m.Instance(t.Context(), counter{N: 0})
	require.NoError(t, err)
	defer inst.Close()

	_, err = inst.Send(t.Context(), xintExternal{})
	require.NoError(t, err)

	require.Equal(t, []string{"EXTERNAL_EVENT", "RAISED_EVENT"}, logs.all())
}

// XState: interpreter > send() event expressions > should resolve send event
// expressions
func TestXIntInterpreterSendEventExpressions(t *testing.T) {
	t.Parallel()

	m, err := sc.New("sendexpr", func(b *sc.Builder[string, xintPassword]) {
		b.Initial("start")
		start := b.State("start")
		start.RaiseOnEntry("next", func(c xintPassword) any { return xintNext(c) })
		start.On[xintNext]().
			When("isFoo", func(_ xintPassword, ev xintNext) bool { return ev.Password == "foo" }).
			To("finish")
		b.Final("finish")
	})
	require.NoError(t, err)

	snap, _ := m.Start(xintPassword{Password: "foo"}, epoch)

	require.True(t, snap.Done())
	require.Equal(t, []string{"finish"}, snap.Active())
}

// XState: interpreter > sendParent() event expressions > should resolve
// sendParent event expressions
//
// The observer's check that the child's actor ref has a send function is
// dropped: a parent reaches its child by the service's name, not by a ref.
func TestXIntInterpreterSendParentEventExpressions(t *testing.T) {
	t.Parallel()

	synctest.Test(t, func(t *testing.T) {
		child, err := sc.New("child", func(b *sc.Builder[string, xintPassword]) {
			b.Initial("start")
			b.State("start").EmitOnEntry("next", func(c xintPassword) xintNext { return xintNext(c) })
		})
		require.NoError(t, err)

		parent := xintNew(t, "parent", func(b *xintBuilder) {
			b.Initial("start")
			start := b.State("start")
			start.InvokeMachine("child", child,
				func(counter) xintPassword { return xintPassword{Password: "foo"} },
				func(sc.Snapshot[string, xintPassword]) (struct{}, error) { return struct{}{}, nil },
			).OnError().Stay()
			start.On[xintNext]().
				When("isFoo", func(_ counter, ev xintNext) bool { return ev.Password == "foo" }).
				To("finish")
			b.Final("finish")
		})

		inst, err := parent.Instance(t.Context(), counter{N: 0})
		require.NoError(t, err)
		defer inst.Close()

		done, err := inst.Await(t.Context(), xintDone)
		require.NoError(t, err)
		require.Equal(t, []string{"finish"}, done.Active())
	})
}

// xintSend is XState's sendMachine.
func xintSend(t *testing.T) *xintMachine {
	t.Helper()

	return xintNew(t, "send", func(b *xintBuilder) {
		b.Initial("inactive")
		inactive := b.State("inactive")
		inactive.On[xintEvent]().
			When("is42", func(_ counter, ev xintEvent) bool { return ev.ID == 42 }).
			To("active")
		inactive.On[xintActivate]().To("active")
		b.Final("active")
	})
}

// xintCompletes runs an actor of m, sends it events, and checks that it
// completes: Run returns nil once the chart is done.
func xintCompletes(t *testing.T, m *xintMachine, events ...any) *sc.Actor[string, counter] {
	t.Helper()

	a := m.Actor(counter{N: 0})
	wait := runActor(t.Context(), a)

	for _, ev := range events {
		require.NoError(t, a.Send(t.Context(), ev))
	}

	require.NoError(t, wait())
	require.True(t, a.Snapshot().Done())

	return a
}

// XState: interpreter > .send() > can send events with a string
//
// An sc event is a value of a Go type, never a string, so this is the same
// test as the next one.
func TestXIntInterpreterSendCanSendEventsWithString(t *testing.T) {
	t.Parallel()

	synctest.Test(t, func(t *testing.T) {
		xintCompletes(t, xintSend(t), xintActivate{Wait: 0})
	})
}

// XState: interpreter > .send() > can send events with an object
func TestXIntInterpreterSendCanSendEventsWithObject(t *testing.T) {
	t.Parallel()

	synctest.Test(t, func(t *testing.T) {
		xintCompletes(t, xintSend(t), xintActivate{Wait: 0})
	})
}

// XState: interpreter > .send() > can send events with an object with
// payload
func TestXIntInterpreterSendCanSendEventsWithPayload(t *testing.T) {
	t.Parallel()

	synctest.Test(t, func(t *testing.T) {
		m := xintSend(t)

		// The guard reads the payload: another id is not accepted.
		snap, _ := m.Start(counter{N: 0}, epoch)
		ignored, _ := m.Transition(snap, xintEvent{ID: 41}, epoch)
		require.Equal(t, snap.Seq(), ignored.Seq())

		xintCompletes(t, m, xintEvent{ID: 42})
	})
}

// XState: interpreter > .send() > should receive and process all events sent
// simultaneously
func TestXIntInterpreterSendProcessesAllEventsSentSimultaneously(t *testing.T) {
	t.Parallel()

	synctest.Test(t, func(t *testing.T) {
		m := xintNew(t, "toggle", func(b *xintBuilder) {
			b.Initial("inactive")
			b.State("fail")
			inactive := b.State("inactive")
			inactive.On[xintInactivate]().To("fail")
			inactive.On[xintActivate]().To("active")
			b.State("active").On[xintInactivate]().To("success")
			b.Final("success")
		})

		a := xintCompletes(t, m, xintActivate{Wait: 0}, xintInactivate{})

		require.Equal(t, []string{"success"}, a.Snapshot().Active())
	})
}

// XState: interpreter > .start() > should initialize the service
//
// The spy on the context factory is dropped: an sc context is a value the
// caller passes in, not a function the chart calls.
func TestXIntInterpreterStartInitializesService(t *testing.T) {
	t.Parallel()

	var entered atomic.Int32

	m := xintNew(t, "m", func(b *xintBuilder) {
		b.Chart().Entry("entry", func(context.Context, counter) { entered.Add(1) })
		b.Initial("foo")
		b.State("foo")
	})

	inst, err := m.Instance(t.Context(), counter{N: 0})
	require.NoError(t, err)
	defer inst.Close()

	require.Equal(t, int32(1), entered.Load())
	require.True(t, inst.Snapshot().In("foo"))
}

// XState: interpreter > .start() > should not reinitialize a started service
func TestXIntInterpreterStartDoesNotReinitializeStartedService(t *testing.T) {
	t.Parallel()

	synctest.Test(t, func(t *testing.T) {
		var entered atomic.Int32

		m := xintNew(t, "m", func(b *xintBuilder) {
			b.Chart().Entry("entry", func(context.Context, counter) { entered.Add(1) })
		})

		ctx, cancel := context.WithCancel(t.Context())
		a := m.Actor(counter{N: 0})
		wait := runActor(ctx, a)
		synctest.Wait()

		// DIVERGES from XState: XState's second start() does nothing and
		// says nothing. A second Run does nothing either, but returns
		// ErrActorStarted.
		require.ErrorIs(t, a.Run(ctx), sc.ErrActorStarted)
		require.Equal(t, int32(1), entered.Load())

		cancel()
		require.ErrorIs(t, wait(), context.Canceled)
	})
}

// xintStartsIn places an instance of m in states, as restoreActor does with
// machine.resolveState({ value }), and returns what is active before and
// after the instance is started.
func xintStartsIn(t *testing.T, m *xintMachine, states ...string) (before, after []string) {
	t.Helper()

	snap, _, err := m.StartIn(counter{N: 0}, epoch, states...)
	require.NoError(t, err)

	inst, err := m.InstanceFrom(t.Context(), snap)
	require.NoError(t, err)
	defer inst.Close()

	return snap.Active(), inst.Snapshot().Active()
}

// XState: interpreter > .start() > should be able to be initialized at a
// custom state
func TestXIntInterpreterStartAtCustomState(t *testing.T) {
	t.Parallel()

	m := xintNew(t, "m", func(b *xintBuilder) {
		b.Initial("foo")
		b.State("foo")
		b.State("bar")
	}, sc.AllowUnreachable())

	before, after := xintStartsIn(t, m, "bar")

	require.Equal(t, []string{"bar"}, before)
	require.Equal(t, []string{"bar"}, after)
}

// XState: interpreter > .start() > should be able to be initialized at a
// custom state value
//
// sc has one way to name a state, so this is the same test as the last.
func TestXIntInterpreterStartAtCustomStateValue(t *testing.T) {
	t.Parallel()

	m := xintNew(t, "m", func(b *xintBuilder) {
		b.Initial("foo")
		b.State("foo")
		b.State("bar")
	}, sc.AllowUnreachable())

	before, after := xintStartsIn(t, m, "bar")

	require.Equal(t, []string{"bar"}, before)
	require.Equal(t, []string{"bar"}, after)
}

// XState: interpreter > .start() > should be able to resolve a custom
// initialized state
func TestXIntInterpreterStartResolvesCustomInitializedState(t *testing.T) {
	t.Parallel()

	m := xintNew(t, "start", func(b *xintBuilder) {
		b.Initial("foo")
		b.State("foo", func(b *xintBuilder) {
			b.Initial("one")
			b.State("one")
		})
		b.State("bar")
	}, sc.AllowUnreachable())

	// Like resolveState({ value: 'foo' }), StartIn fills in foo's initial
	// child, giving { foo: 'one' }.
	before, after := xintStartsIn(t, m, "foo")

	require.Equal(t, []string{"foo", "one"}, before)
	require.Equal(t, []string{"foo", "one"}, after)
}

// XState: interpreter > .stop() > should cancel delayed events
func TestXIntInterpreterStopCancelsDelayedEvents(t *testing.T) {
	t.Parallel()

	synctest.Test(t, func(t *testing.T) {
		var called atomic.Bool

		m := xintNew(t, "delayed", func(b *xintBuilder) {
			b.Initial("foo")
			b.State("foo").After(xintMs(50)).
				Do("call", func(context.Context, counter, struct{}) { called.Store(true) }).
				To("bar")
			b.State("bar")
		})

		inst, err := m.Instance(t.Context(), counter{N: 0})
		require.NoError(t, err)

		inst.Close()

		xintSleep(xintMs(60))
		require.False(t, called.Load())
		require.Equal(t, []string{"foo"}, inst.Snapshot().Active())
	})
}

// xintWaiting is the chart of the next two cases: TRIGGER moves it from
// waiting to active, whose entry action sets called.
func xintWaiting(t *testing.T, called *atomic.Bool) *xintMachine {
	t.Helper()

	return xintNew(t, "m", func(b *xintBuilder) {
		b.Initial("waiting")
		b.State("waiting").On[xintTrigger]().To("active")
		b.State("active").Entry("call", func(context.Context, counter) { called.Store(true) })
	})
}

// XState: interpreter > .stop() > should not execute transitions after being
// stopped
//
// XState warns that the event was not delivered; sc returns
// ErrInstanceClosed to the sender.
func TestXIntInterpreterStopDoesNotExecuteTransitionsAfterStopped(t *testing.T) {
	t.Parallel()

	synctest.Test(t, func(t *testing.T) {
		var called atomic.Bool
		m := xintWaiting(t, &called)

		inst, err := m.Instance(t.Context(), counter{N: 0})
		require.NoError(t, err)

		inst.Close()

		step, err := inst.Send(t.Context(), xintTrigger{Self: nil})
		require.ErrorIs(t, err, sc.ErrInstanceClosed)
		require.Equal(t, step.From.Seq(), step.To.Seq())

		xintSleep(xintMs(10))
		require.False(t, called.Load())
		require.Equal(t, []string{"waiting"}, inst.Snapshot().Active())
	})
}

// XState: interpreter > .stop() > should not throw when sending an
// unserializable event to a stopped actor
//
// The event points at itself, as XState's does, so it cannot be marshalled
// to JSON. The warning XState counts is the error Send returns.
func TestXIntInterpreterStopUnserializableEventToStoppedActor(t *testing.T) {
	t.Parallel()

	var called atomic.Bool
	m := xintWaiting(t, &called)

	inst, err := m.Instance(t.Context(), counter{N: 0})
	require.NoError(t, err)

	inst.Close()

	circular := &xintTrigger{Self: nil}
	circular.Self = circular

	require.NotPanics(t, func() {
		_, err = inst.Send(t.Context(), *circular)
	})
	require.ErrorIs(t, err, sc.ErrInstanceClosed)
}

// XState: interpreter > .stop() > stopping a not-started interpreter should
// not crash
//
// An actor is stopped by ending the context it runs under, so one that was
// never run has nothing to stop. The nearest case is ported: an actor whose
// context has already ended when it is run stops at once and says why.
func TestXIntInterpreterStopNotStartedInterpreter(t *testing.T) {
	t.Parallel()

	synctest.Test(t, func(t *testing.T) {
		m := xintNew(t, "m", func(b *xintBuilder) {
			b.Initial("a")
			b.State("a")
		})

		ctx, cancel := context.WithCancel(t.Context())
		cancel()

		a := m.Actor(counter{N: 0})

		require.NotPanics(t, func() {
			require.ErrorIs(t, a.Run(ctx), context.Canceled)
		})
		require.ErrorIs(t, a.Send(t.Context(), xintToggle{}), sc.ErrActorStopped)
	})
}

// xintWatch ranges over an actor's snapshots on a goroutine of its own,
// calling seen with each, and returns a channel closed when the sequence
// ends.
func xintWatch(ctx context.Context, a *sc.Actor[string, counter], seen func(xintSnapshot)) <-chan struct{} {
	ended := make(chan struct{})
	go func() {
		defer close(ended)
		for snap := range a.Snapshots(ctx) {
			seen(snap)
		}
	}()

	return ended
}

// xintCall sends ev to a running actor, waits until it is processed, and
// lets the goroutines watching the actor catch up.
func xintCall(t *testing.T, a *sc.Actor[string, counter], ev any) {
	t.Helper()

	_, err := a.Call(t.Context(), ev)
	require.NoError(t, err)
	synctest.Wait()
}

// XState: interpreter > .unsubscribe() > should remove transition listeners
func TestXIntInterpreterUnsubscribeRemovesTransitionListeners(t *testing.T) {
	t.Parallel()

	synctest.Test(t, func(t *testing.T) {
		m := xintNew(t, "toggle", func(b *xintBuilder) {
			b.Initial("inactive")
			b.State("inactive").On[xintToggle]().To("active")
			b.State("active").On[xintToggle]().To("inactive")
		})

		ctx, cancel := context.WithCancel(t.Context())
		a := m.Actor(counter{N: 0})
		wait := runActor(ctx, a)

		var stateCount atomic.Int32
		subCtx, unsubscribe := context.WithCancel(ctx)
		ended := xintWatch(subCtx, a, func(xintSnapshot) { stateCount.Add(1) })
		synctest.Wait()

		// DIVERGES from XState: XState's listener has heard nothing yet
		// (0, then 1 and 2). Actor.Snapshots yields the current snapshot
		// first, so every count is one higher.
		require.Equal(t, int32(1), stateCount.Load())

		xintCall(t, a, xintToggle{})
		require.Equal(t, int32(2), stateCount.Load())

		xintCall(t, a, xintToggle{})
		require.Equal(t, int32(3), stateCount.Load())

		unsubscribe()
		<-ended
		xintCall(t, a, xintToggle{})
		require.Equal(t, int32(3), stateCount.Load())

		cancel()
		require.ErrorIs(t, wait(), context.Canceled)
	})
}

// xintTransientOrder subscribes to an actor of m before it starts, sends it
// START, and returns the states the subscriber saw.
func xintTransientOrder(t *testing.T, m *xintMachine) []string {
	t.Helper()

	ctx, cancel := context.WithCancel(t.Context())
	a := m.Actor(counter{N: 0})

	values := xintNewLog()
	ended := xintWatch(ctx, a, func(s xintSnapshot) { values.add(s.Active()[0]) })
	synctest.Wait()

	wait := runActor(ctx, a)
	xintCall(t, a, xintStart{})

	cancel()
	require.ErrorIs(t, wait(), context.Canceled)
	<-ended

	return values.all()
}

// XState: interpreter > transient states > should transition in correct
// order
func TestXIntInterpreterTransientStatesTransitionInCorrectOrder(t *testing.T) {
	t.Parallel()

	synctest.Test(t, func(t *testing.T) {
		m := xintNew(t, "transient", func(b *xintBuilder) {
			b.Initial("idle")
			b.State("idle").On[xintStart]().To("transient")
			b.State("transient").Always().To("next")
			b.State("next").On[xintFinish]().To("end")
			b.Final("end")
		})

		require.Equal(t, []string{"idle", "next"}, xintTransientOrder(t, m))
	})
}

// XState: interpreter > transient states > should transition in correct
// order when there is a condition
func TestXIntInterpreterTransientStatesTransitionInCorrectOrderWithCondition(t *testing.T) {
	t.Parallel()

	synctest.Test(t, func(t *testing.T) {
		m := xintNew(t, "transient", func(b *xintBuilder) {
			b.Initial("idle")
			b.State("idle").On[xintStart]().To("transient")
			transient := b.State("transient")
			transient.Always().When("alwaysFalse", func(counter, struct{}) bool { return false }).To("end")
			transient.Always().To("next")
			b.State("next").On[xintFinish]().To("end")
			b.Final("end")
		})

		require.Equal(t, []string{"idle", "next"}, xintTransientOrder(t, m))
	})
}

// xintInterval is XState's intervalMachine: active re-enters itself every
// 10ms, counting, and is finished at five.
func xintInterval(t *testing.T) *xintMachine {
	t.Helper()

	return xintNew(t, "interval", func(b *xintBuilder) {
		b.Initial("active")
		active := b.State("active")
		active.After(xintMs(10)).Assign("increment", increment[struct{}]).To("active").Reenter()
		active.Always().When("reachedFive", func(c counter, _ struct{}) bool { return c.N >= 5 }).To("finished")
		b.Final("finished")
	})
}

// XState: interpreter > observable > should be subscribable
func TestXIntInterpreterObservableIsSubscribable(t *testing.T) {
	t.Parallel()

	synctest.Test(t, func(t *testing.T) {
		a := xintInterval(t).Actor(counter{N: 0})
		wait := runActor(t.Context(), a)

		var count atomic.Int32
		ended := xintWatch(t.Context(), a, func(s xintSnapshot) { count.Store(int32(s.Context.N)) }) //nolint:gosec // the count is at most five

		// The sequence ends when the chart is done: that is complete().
		<-ended
		require.NoError(t, wait())
		require.Equal(t, int32(5), count.Load())
	})
}

// XState: interpreter > observable > should be interoperable with RxJS, etc.
// via Symbol.observable
//
// There is nothing like Symbol.observable to interoperate through. What the
// case counts is ported: an observer is notified five times, once for each
// tick, the last of which also finishes the chart.
func TestXIntInterpreterObservableNotifiesOncePerStep(t *testing.T) {
	t.Parallel()

	synctest.Test(t, func(t *testing.T) {
		var count atomic.Int32

		a := xintInterval(t).Actor(counter{N: 0}, sc.WithHooks(&sc.Hooks[string, counter]{
			Transition:     func(context.Context, sc.Step[string, counter]) { count.Add(1) },
			Ignored:        nil,
			EffectStarted:  nil,
			EffectFinished: nil,
		}))

		require.NoError(t, a.Run(t.Context()))
		require.Equal(t, int32(5), count.Load())
	})
}

// XState: interpreter > observable > should be unsubscribable
func TestXIntInterpreterObservableIsUnsubscribable(t *testing.T) {
	t.Parallel()

	synctest.Test(t, func(t *testing.T) {
		m := xintNew(t, "m", func(b *xintBuilder) {
			b.Initial("active")
			active := b.State("active")
			active.Always().When("reachedFive", func(c counter, _ struct{}) bool { return c.N >= 5 }).To("finished")
			active.On[xintInc]().Assign("increment", increment[xintInc])
			b.Final("finished")
		})

		a := m.Actor(counter{N: 0})
		wait := runActor(t.Context(), a)

		var count atomic.Int32
		subCtx, unsubscribe := context.WithCancel(t.Context())
		ended := xintWatch(subCtx, a, func(s xintSnapshot) { count.Store(int32(s.Context.N)) }) //nolint:gosec // the count is at most five

		xintCall(t, a, xintInc{})
		xintCall(t, a, xintInc{})
		unsubscribe()
		<-ended
		xintCall(t, a, xintInc{})
		xintCall(t, a, xintInc{})
		require.NoError(t, a.Send(t.Context(), xintInc{}))

		require.NoError(t, wait())
		require.Equal(t, 5, a.Snapshot().Context.N)
		require.Equal(t, int32(2), count.Load())
	})
}

// XState: interpreter > observable > should call complete() once a final
// state is reached
func TestXIntInterpreterObservableCompletesOnceFinalStateReached(t *testing.T) {
	t.Parallel()

	synctest.Test(t, func(t *testing.T) {
		m := xintNew(t, "m", func(b *xintBuilder) {
			b.Initial("idle")
			b.State("idle").On[xintNext]().To("done")
			b.Final("done")
		})

		a := m.Actor(counter{N: 0})
		wait := runActor(t.Context(), a)

		var completed atomic.Int32
		ended := xintWatch(t.Context(), a, func(xintSnapshot) {})
		go func() {
			<-ended
			completed.Add(1)
		}()

		synctest.Wait()
		require.Zero(t, completed.Load())

		require.NoError(t, a.Send(t.Context(), xintNext{Password: ""}))
		require.NoError(t, wait())
		synctest.Wait()

		require.Equal(t, int32(1), completed.Load())
	})
}

// XState: interpreter > observable > should call complete() once the
// interpreter is stopped
func TestXIntInterpreterObservableCompletesOnceInterpreterStopped(t *testing.T) {
	t.Parallel()

	synctest.Test(t, func(t *testing.T) {
		m := xintNew(t, "m", func(*xintBuilder) {})

		ctx, cancel := context.WithCancel(t.Context())
		a := m.Actor(counter{N: 0})
		wait := runActor(ctx, a)

		// The subscription's own context outlives the actor's.
		var completed atomic.Int32
		ended := xintWatch(t.Context(), a, func(xintSnapshot) {})
		go func() {
			<-ended
			completed.Add(1)
		}()

		synctest.Wait()
		require.Zero(t, completed.Load())

		cancel()
		require.ErrorIs(t, wait(), context.Canceled)
		synctest.Wait()

		require.Equal(t, int32(1), completed.Load())
	})
}

// XState: interpreter > actors > doesn't crash cryptically on undefined
// return from the actor creator
//
// A callback that returns no cleanup is a stream that ends without yielding.
func TestXIntInterpreterActorsCallbackReturningNothing(t *testing.T) {
	t.Parallel()

	synctest.Test(t, func(t *testing.T) {
		m := xintNew(t, "m", func(b *xintBuilder) {
			b.Initial("initial")
			b.State("initial").Stream("testService", func(context.Context, counter) iter.Seq2[any, error] {
				return func(func(any, error) bool) {}
			}).OnError().Stay()
		})

		inst, err := m.Instance(t.Context(), counter{N: 0})
		require.NoError(t, err)
		defer inst.Close()

		synctest.Wait()
		require.Equal(t, []string{"initial"}, inst.Snapshot().Active())
	})
}

// XState: interpreter > children > state.children should reference invoked
// child actors (machine)
//
// XState's test sends FIRE to the child through the ref in state.children.
// A snapshot holds no refs: the parent's own FIRE transition forwards the
// event to the service by name. What stands for state.children is
// Snapshot.Services.
func TestXIntInterpreterChildrenReferenceInvokedMachine(t *testing.T) {
	t.Parallel()

	synctest.Test(t, func(t *testing.T) {
		child := xintNew(t, "child", func(b *xintBuilder) {
			b.Initial("active")
			b.State("active").On[xintFire]().Emit("fired", func(counter, xintFire) xintFired { return xintFired{} })
		})

		parent := xintNew(t, "parent", func(b *xintBuilder) {
			b.Initial("active")
			active := b.State("active")
			active.InvokeMachine("childActor", child,
				func(c counter) counter { return c },
				func(xintSnapshot) (struct{}, error) { return struct{}{}, nil },
			).OnError().Stay()
			active.On[xintFire]().SendTo("fire", "childActor", func(counter, xintFire) any { return xintFire{} })
			active.On[xintFired]().To("success")
			b.Final("success")
		})

		inst, err := parent.Instance(t.Context(), counter{N: 0})
		require.NoError(t, err)
		defer inst.Close()

		synctest.Wait()
		require.Len(t, inst.Snapshot().Services(), 1)
		require.Equal(t, "childActor", inst.Snapshot().Services()[0].Name)

		_, err = inst.Send(t.Context(), xintFire{})
		require.NoError(t, err)

		done, err := inst.Await(t.Context(), xintDone)
		require.NoError(t, err)
		require.Equal(t, []string{"success"}, done.Active())
		require.Empty(t, done.Services())
	})
}

// XState: interpreter > children > state.children should reference invoked
// child actors (promise)
func TestXIntInterpreterChildrenReferenceInvokedPromise(t *testing.T) {
	t.Parallel()

	synctest.Test(t, func(t *testing.T) {
		m := xintNew(t, "parent", func(b *xintBuilder) {
			b.Initial("active")
			num := b.State("active").Invoke("childActor", func(ctx context.Context, _ counter) (int, error) {
				select {
				case <-time.After(xintMs(100)):
					return 42, nil
				case <-ctx.Done():
					return 0, ctx.Err()
				}
			})
			num.OnDone().When("is42", func(_ counter, ev sc.Done[int]) bool { return ev.Result == 42 }).To("success")
			num.OnDone().To("failure")
			num.OnError().Stay()
			b.Final("success")
			b.Final("failure")
		})

		inst, err := m.Instance(t.Context(), counter{N: 0})
		require.NoError(t, err)
		defer inst.Close()

		require.True(t, inst.Snapshot().In("active"))
		require.Len(t, inst.Snapshot().Services(), 1)
		require.Equal(t, "childActor", inst.Snapshot().Services()[0].Name)

		done, err := inst.Await(t.Context(), xintDone)
		require.NoError(t, err)
		require.Equal(t, []string{"success"}, done.Active())
		require.Empty(t, done.Services())
	})
}

// XState: interpreter > children > state.children should reference invoked
// child actors (observable)
//
// An observable's values reach the chart as events, where XState delivers
// them as snapshot events of the child.
func TestXIntInterpreterChildrenReferenceInvokedObservable(t *testing.T) {
	t.Parallel()

	synctest.Test(t, func(t *testing.T) {
		m := xintNew(t, "parent", func(b *xintBuilder) {
			b.Initial("active")
			active := b.State("active")
			active.Stream("childActor", func(ctx context.Context, _ counter) iter.Seq2[any, error] {
				return func(yield func(any, error) bool) {
					for n := 0; ; n++ {
						select {
						case <-time.After(xintMs(10)):
						case <-ctx.Done():
							return
						}

						if !yield(xintTick{N: n}, nil) {
							return
						}
					}
				}
			}).OnError().Stay()
			active.On[xintTick]().When("isThree", func(_ counter, ev xintTick) bool { return ev.N == 3 }).To("success")
			b.Final("success")
		})

		var missing atomic.Int32
		inst, err := m.Instance(t.Context(), counter{N: 0}, sc.WithHooks(&sc.Hooks[string, counter]{
			Transition: func(_ context.Context, step sc.Step[string, counter]) {
				if step.To.In("active") && len(step.To.Services()) != 1 {
					missing.Add(1)
				}
			},
			Ignored: func(_ context.Context, snap xintSnapshot, _ any) {
				if snap.In("active") && len(snap.Services()) != 1 {
					missing.Add(1)
				}
			},
			EffectStarted:  nil,
			EffectFinished: nil,
		}))
		require.NoError(t, err)
		defer inst.Close()

		done, err := inst.Await(t.Context(), xintDone)
		require.NoError(t, err)
		require.Empty(t, done.Services())
		require.Zero(t, missing.Load())
	})
}

// xintRootEntry is a chart whose only behaviour is an entry action on the
// chart itself, as in XState: the chart has no states.
func xintRootEntry(t *testing.T, entered *atomic.Int32) *xintMachine {
	t.Helper()

	return xintNew(t, "m", func(b *xintBuilder) {
		b.Chart().Entry("entry", func(context.Context, counter) { entered.Add(1) })
	})
}

// XState: interpreter > shouldn't execute actions when reading a snapshot of
// not started actor
func TestXIntInterpreterReadingSnapshotOfNotStartedActorRunsNoActions(t *testing.T) {
	t.Parallel()

	var entered atomic.Int32
	a := xintRootEntry(t, &entered).Actor(counter{N: 0})

	a.Snapshot()

	require.Zero(t, entered.Load())
}

// XState: interpreter > should execute entry actions when starting the actor
// after reading its snapshot first
func TestXIntInterpreterEntryActionsRunWhenStartingAfterReadingSnapshot(t *testing.T) {
	t.Parallel()

	synctest.Test(t, func(t *testing.T) {
		var entered atomic.Int32
		a := xintRootEntry(t, &entered).Actor(counter{N: 0})

		a.Snapshot()
		require.Zero(t, entered.Load())

		ctx, cancel := context.WithCancel(t.Context())
		wait := runActor(ctx, a)
		synctest.Wait()

		require.Equal(t, int32(1), entered.Load())

		cancel()
		require.ErrorIs(t, wait(), context.Canceled)
	})
}

// XState: interpreter > the first state of an actor should be its initial
// state
func TestXIntInterpreterFirstStateIsInitialState(t *testing.T) {
	t.Parallel()

	synctest.Test(t, func(t *testing.T) {
		m := xintNew(t, "m", func(*xintBuilder) {})

		a := m.Actor(counter{N: 0})
		initial := a.Snapshot()

		ctx, cancel := context.WithCancel(t.Context())
		wait := runActor(ctx, a)
		synctest.Wait()

		require.Equal(t, initial, a.Snapshot())

		cancel()
		require.ErrorIs(t, wait(), context.Canceled)
	})
}

// XState: interpreter > should call an onDone callback immediately if the
// service is already done
func TestXIntInterpreterCompletesImmediatelyWhenAlreadyDone(t *testing.T) {
	t.Parallel()

	synctest.Test(t, func(t *testing.T) {
		m := xintNew(t, "m", func(b *xintBuilder) {
			b.Initial("a")
			b.Final("a")
		})

		a := m.Actor(counter{N: 0})
		require.NoError(t, a.Run(t.Context()))
		require.True(t, a.Snapshot().Done())

		// A subscription that begins now ends at once.
		seen := 0
		for range a.Snapshots(t.Context()) {
			seen++
		}
		require.Equal(t, 1, seen)

		done, err := a.Await(t.Context(), xintDone)
		require.NoError(t, err)
		require.Equal(t, []string{"a"}, done.Active())
	})
}

// XState: should throw if an event is received
func TestXIntInterpreterStringEvent(t *testing.T) {
	t.Parallel()

	m := xintNew(t, "m", func(*xintBuilder) {})

	inst, err := m.Instance(t.Context(), counter{N: 0})
	require.NoError(t, err)
	defer inst.Close()

	// DIVERGES from XState: XState throws when it is sent a bare string
	// instead of an event object. To sc a string is an event like any
	// other, of a type the chart does not handle, so it is ignored.
	step, err := inst.Send(t.Context(), "EVENT")
	require.NoError(t, err)
	require.Equal(t, step.From.Seq(), step.To.Seq())
}

// XState: should not process events sent directly to own actor ref before
// initial entry actions are processed
func TestXIntInterpreterEventsRaisedOnEntryWaitForInitialEntryActions(t *testing.T) {
	t.Parallel()

	m := xintNew(t, "m", func(b *xintBuilder) {
		root := b.Chart()
		root.Entry("initial root entry start", noop)
		root.RaiseOnEntry("raise", func(counter) any { return xintEv{} })
		root.Entry("initial root entry end", noop)
		b.On[xintEv]().Do("EV transition", func(context.Context, counter, xintEv) {})
		b.Initial("a")
		b.State("a").Entry("initial nested entry", noop)
	})

	_, effects := m.Start(counter{N: 0}, epoch)

	require.Equal(t, []string{
		"m/initial root entry start",
		"m/initial root entry end",
		"a/initial nested entry",
		"m/EV transition",
	}, names(effects))
}

// XState: should not notify the completion observer for an active logic when
// it gets subscribed before starting
func TestXIntInterpreterNoCompletionForActiveLogicSubscribedBeforeStarting(t *testing.T) {
	t.Parallel()

	synctest.Test(t, func(t *testing.T) {
		m := xintNew(t, "m", func(*xintBuilder) {})

		ctx, cancel := context.WithCancel(t.Context())
		a := m.Actor(counter{N: 0})

		var completed atomic.Bool
		ended := xintWatch(ctx, a, func(xintSnapshot) {})
		go func() {
			<-ended
			completed.Store(true)
		}()

		synctest.Wait()
		require.False(t, completed.Load())

		cancel()
		<-ended
	})
}

// XState: should notify the error observer for an errored logic when it gets
// subscribed after it errors
//
// A panic in an action is the error. sc has no error observer: the error
// goes to whoever started the instance or ran the actor, and someone who
// comes later learns only that the actor has stopped.
func TestXIntInterpreterErroredLogicReportsItsError(t *testing.T) {
	t.Parallel()

	synctest.Test(t, func(t *testing.T) {
		m := xintNew(t, "m", func(b *xintBuilder) {
			b.Chart().Entry("entry", func(context.Context, counter) { panic(errors.New("error")) })
		})

		_, err := m.Instance(t.Context(), counter{N: 0})
		require.ErrorIs(t, err, sc.ErrPanicked)
		require.ErrorContains(t, err, "error")

		a := m.Actor(counter{N: 0})
		require.ErrorIs(t, a.Run(t.Context()), sc.ErrPanicked)

		_, err = a.Await(t.Context(), xintDone)
		require.ErrorIs(t, err, sc.ErrActorStopped)
		require.ErrorIs(t, a.Send(t.Context(), xintEv{}), sc.ErrActorStopped)
	})
}

// xintAB is the chart most waitFor cases use: NEXT moves a to b, which is
// final when finalB is set.
func xintAB(t *testing.T, finalB bool) *xintMachine {
	t.Helper()

	return xintNew(t, "m", func(b *xintBuilder) {
		b.Initial("a")
		b.State("a").On[xintNext]().To("b")
		if finalB {
			b.Final("b")
		} else {
			b.State("b")
		}
	})
}

// xintABC is a to b to c on NEXT, with nothing final.
func xintABC(t *testing.T) *xintMachine {
	t.Helper()

	return xintNew(t, "m", func(b *xintBuilder) {
		b.Initial("a")
		b.State("a").On[xintNext]().To("b")
		b.State("b").On[xintNext]().To("c")
		b.State("c")
	})
}

// xintFinished runs an actor of the a-to-final-b chart and sends it NEXT,
// so that it has finished and stopped.
func xintFinished(t *testing.T) *sc.Actor[string, counter] {
	t.Helper()

	return xintCompletes(t, xintAB(t, true), xintNext{Password: ""})
}

var errXintAborted = errors.New("aborted")

// XState: waitFor > should wait for a condition to be true and return the
// emitted value
func TestXIntWaitForWaitsForConditionAndReturnsValue(t *testing.T) {
	t.Parallel()

	synctest.Test(t, func(t *testing.T) {
		inst, err := xintAB(t, false).Instance(t.Context(), counter{N: 0})
		require.NoError(t, err)
		defer inst.Close()

		time.AfterFunc(xintMs(10), func() { _, _ = inst.Send(t.Context(), xintNext{Password: ""}) })

		state, err := inst.Await(t.Context(), xintIn("b"))
		require.NoError(t, err)
		require.Equal(t, []string{"b"}, state.Active())
	})
}

// XState: waitFor > should throw an error after a timeout
func TestXIntWaitForFailsAfterTimeout(t *testing.T) {
	t.Parallel()

	synctest.Test(t, func(t *testing.T) {
		inst, err := xintABC(t).Instance(t.Context(), counter{N: 0})
		require.NoError(t, err)
		defer inst.Close()

		ctx, cancel := context.WithTimeout(t.Context(), xintMs(10))
		defer cancel()

		last, err := inst.Await(ctx, xintIn("c"))
		require.ErrorIs(t, err, context.DeadlineExceeded)
		require.Equal(t, []string{"a"}, last.Active())
	})
}

// XState: waitFor > should not reject immediately when passing Infinity as
// timeout
//
// No timeout is a context without a deadline.
func TestXIntWaitForWithoutTimeoutDoesNotRejectImmediately(t *testing.T) {
	t.Parallel()

	synctest.Test(t, func(t *testing.T) {
		inst, err := xintABC(t).Instance(t.Context(), counter{N: 0})
		require.NoError(t, err)
		defer inst.Close()

		ctx, cancel := context.WithCancel(t.Context())
		result := make(chan error, 1)
		go func() {
			_, err := inst.Await(ctx, xintIn("c"))
			result <- err
		}()

		xintSleep(xintMs(10))
		require.Empty(t, result)

		cancel()
		require.ErrorIs(t, <-result, context.Canceled)
	})
}

// XState: waitFor > should throw an error when reaching a final state that
// does not match the predicate
func TestXIntWaitForFailsWhenFinalStateDoesNotMatch(t *testing.T) {
	t.Parallel()

	synctest.Test(t, func(t *testing.T) {
		a := xintAB(t, true).Actor(counter{N: 0})
		wait := runActor(t.Context(), a)

		time.AfterFunc(xintMs(10), func() { _ = a.Send(t.Context(), xintNext{Password: ""}) })

		last, err := a.Await(t.Context(), xintIn("never"))
		require.ErrorIs(t, err, sc.ErrActorStopped)
		require.Equal(t, []string{"b"}, last.Active())
		require.NoError(t, wait())
	})
}

// XState: waitFor > should resolve correctly when the predicate immediately
// matches the current state
func TestXIntWaitForResolvesWhenPredicateImmediatelyMatches(t *testing.T) {
	t.Parallel()

	m := xintNew(t, "m", func(b *xintBuilder) {
		b.Initial("a")
		b.State("a")
	})

	inst, err := m.Instance(t.Context(), counter{N: 0})
	require.NoError(t, err)
	defer inst.Close()

	state, err := inst.Await(t.Context(), xintIn("a"))
	require.NoError(t, err)
	require.Equal(t, []string{"a"}, state.Active())
}

// XState: waitFor > should internally unsubscribe when the predicate
// immediately matches the current state
func TestXIntWaitForStopsCheckingOncePredicateMatched(t *testing.T) {
	t.Parallel()

	inst, err := xintAB(t, false).Instance(t.Context(), counter{N: 0})
	require.NoError(t, err)
	defer inst.Close()

	var count atomic.Int32
	_, err = inst.Await(t.Context(), func(s xintSnapshot) bool {
		count.Add(1)
		return s.In("a")
	})
	require.NoError(t, err)

	_, err = inst.Send(t.Context(), xintNext{Password: ""})
	require.NoError(t, err)

	require.Equal(t, int32(1), count.Load())
}

// XState: waitFor > should immediately resolve for an actor in its final
// state that matches the predicate
func TestXIntWaitForResolvesForFinishedActorThatMatches(t *testing.T) {
	t.Parallel()

	synctest.Test(t, func(t *testing.T) {
		a := xintFinished(t)

		state, err := a.Await(t.Context(), xintIn("b"))
		require.NoError(t, err)
		require.Equal(t, []string{"b"}, state.Active())

		// The same holds for an instance, which is never stopped by
		// finishing.
		inst, err := xintAB(t, true).Instance(t.Context(), counter{N: 0})
		require.NoError(t, err)
		defer inst.Close()

		_, err = inst.Send(t.Context(), xintNext{Password: ""})
		require.NoError(t, err)

		state, err = inst.Await(t.Context(), xintIn("b"))
		require.NoError(t, err)
		require.Equal(t, []string{"b"}, state.Active())
	})
}

// XState: waitFor > should immediately reject for an actor in its final
// state that does not match the predicate
func TestXIntWaitForRejectsForFinishedActorThatDoesNotMatch(t *testing.T) {
	t.Parallel()

	synctest.Test(t, func(t *testing.T) {
		a := xintFinished(t)

		_, err := a.Await(t.Context(), xintIn("a"))
		require.ErrorIs(t, err, sc.ErrActorStopped)

		inst, err := xintAB(t, true).Instance(t.Context(), counter{N: 0})
		require.NoError(t, err)
		defer inst.Close()

		_, err = inst.Send(t.Context(), xintNext{Password: ""})
		require.NoError(t, err)
		require.True(t, inst.Snapshot().Done())

		// Like waitFor, which rejects at once with "Actor terminated
		// without satisfying predicate", Instance.Await returns at once:
		// a finished instance can no longer change.
		ctx, cancel := context.WithTimeout(t.Context(), time.Hour)
		defer cancel()

		began := time.Now()
		_, err = inst.Await(ctx, xintIn("a"))
		require.ErrorIs(t, err, sc.ErrInstanceDone)
		require.Zero(t, time.Since(began))
	})
}

// XState: waitFor > should immediately reject when it receives an aborted
// signal
//
// An AbortSignal is a context, and the reason it was aborted with is the
// context's cause.
func TestXIntWaitForWithAbortedSignal(t *testing.T) {
	t.Parallel()

	synctest.Test(t, func(t *testing.T) {
		ctx, abort := context.WithCancelCause(t.Context())
		abort(errXintAborted)

		// DIVERGES from XState: waitFor rejects with the abort reason
		// although the actor is already in a state that satisfies the
		// predicate. Await looks at the snapshot before it looks at the
		// context, so it returns the matching snapshot and no error.
		a := xintFinished(t)
		state, err := a.Await(ctx, xintIn("b"))
		require.NoError(t, err)
		require.Equal(t, []string{"b"}, state.Active())

		inst, err := xintAB(t, true).Instance(t.Context(), counter{N: 0})
		require.NoError(t, err)
		defer inst.Close()

		state, err = inst.Await(ctx, xintIn("a"))
		require.NoError(t, err)
		require.Equal(t, []string{"a"}, state.Active())

		// When the predicate does not hold, the aborted context rejects
		// at once, with the reason.
		_, err = inst.Await(ctx, xintIn("b"))
		require.ErrorIs(t, err, errXintAborted)
	})
}

// XState: waitFor > should reject when the signal is aborted while waiting
func TestXIntWaitForRejectsWhenSignalAbortedWhileWaiting(t *testing.T) {
	t.Parallel()

	synctest.Test(t, func(t *testing.T) {
		inst, err := xintAB(t, false).Instance(t.Context(), counter{N: 0})
		require.NoError(t, err)
		defer inst.Close()

		ctx, abort := context.WithCancelCause(t.Context())
		time.AfterFunc(xintMs(10), func() { abort(errXintAborted) })

		began := time.Now()
		_, err = inst.Await(ctx, xintIn("b"))
		require.ErrorIs(t, err, errXintAborted)
		require.Equal(t, xintMs(10), time.Since(began))

		// The same for an actor.
		actorCtx, cancel := context.WithCancel(t.Context())
		a := xintAB(t, false).Actor(counter{N: 0})
		wait := runActor(actorCtx, a)

		ctx, abort = context.WithCancelCause(t.Context())
		time.AfterFunc(xintMs(10), func() { abort(errXintAborted) })

		_, err = a.Await(ctx, xintIn("b"))
		require.ErrorIs(t, err, errXintAborted)

		cancel()
		require.ErrorIs(t, wait(), context.Canceled)
	})
}

// xintRecordAt appends the time a timer fired, in milliseconds after the
// epoch.
func xintRecordAt(c xintTimes, _ struct{}, now time.Time) xintTimes {
	c.At = append(append([]int{}, c.At...), int(now.Sub(epoch)/time.Millisecond))
	return c
}

// xintTrace returns an assignment that appends entry to the trace.
func xintTrace[Ev any](entry string) func(xintTimes, Ev) xintTimes {
	return func(c xintTimes, _ Ev) xintTimes {
		c.Trace = append(append([]string{}, c.Trace...), entry)
		return c
	}
}

// XState: clock > runs callbacks at their deadlines and includes newly
// scheduled intermediate timers
//
// sc has no clock object with callbacks. The case is ported to what stands
// in its place, the core being given a time well past several timers: each
// fires at its own time, and so does one that an earlier firing started.
func TestXIntClockFiresTimersAtTheirDeadlinesIncludingNewOnes(t *testing.T) {
	t.Parallel()

	m, err := sc.New("clock", func(b *sc.Builder[string, xintTimes]) {
		b.Regions()
		b.State("chain", func(b *sc.Builder[string, xintTimes]) {
			b.Initial("a")
			b.State("a").After(xintMs(2000)).AssignAt("record", xintRecordAt).To("b")
			b.State("b").After(xintMs(1000)).AssignAt("record", xintRecordAt).To("c")
			b.State("c")
		})
		b.State("single", func(b *sc.Builder[string, xintTimes]) {
			b.Initial("x")
			b.State("x").After(xintMs(6000)).AssignAt("record", xintRecordAt).To("y")
			b.State("y")
		})
	})
	require.NoError(t, err)

	snap, _ := m.Start(xintTimes{At: nil, Trace: nil}, epoch)
	snap, _ = m.Advance(snap, epoch.Add(xintMs(6000)))

	require.Equal(t, []int{2000, 3000, 6000}, snap.Context.At)
	require.Empty(t, snap.Timers())
}

// XState: clock > reconsiders cancellation and equal-deadline ordering after
// each callback
//
// Ported the same way: "first" and "second" are due together, and "first"
// cancels a later timer and starts one that is due at once. The clock's
// refusal to go back in time or to a time that is not finite is dropped: a
// time.Time is always finite, and the core is not a clock.
func TestXIntClockReconsidersCancellationAndOrderAfterEachTimer(t *testing.T) {
	t.Parallel()

	m, err := sc.New("clock", func(b *sc.Builder[string, xintTimes]) {
		b.Initial("running")
		running := b.Parallel("running", func(b *sc.Builder[string, xintTimes]) {
			b.State("one", func(b *sc.Builder[string, xintTimes]) {
				b.Initial("a1")
				b.State("a1").After(xintMs(2000)).
					Assign("first", xintTrace[struct{}]("first")).
					CancelRaise("late").
					RaiseAfter("new", 0, func(xintTimes, struct{}) any { return xintNew1{} }).
					To("a2")
				b.State("a2")
			})
			b.State("two", func(b *sc.Builder[string, xintTimes]) {
				b.Initial("b1")
				b.State("b1").After(xintMs(2000)).Assign("second", xintTrace[struct{}]("second")).To("b2")
				b.State("b2")
			})
		})
		running.RaiseAfterOnEntry("late", xintMs(6000), func(xintTimes) any { return xintLate{} })
		running.On[xintLate]().Assign("canceled", xintTrace[xintLate]("canceled"))
		running.On[xintNew1]().Assign("new", xintTrace[xintNew1]("new"))
	})
	require.NoError(t, err)

	snap, _ := m.Start(xintTimes{At: nil, Trace: nil}, epoch)
	snap, _ = m.Advance(snap, epoch.Add(xintMs(6000)))

	require.Equal(t, []string{"first", "second", "new"}, snap.Context.Trace)
	require.Empty(t, snap.Timers())
}

// xintWaitingDone is waiting, which goes to done after 100ms.
func xintWaitingDone(t *testing.T) *xintMachine {
	t.Helper()

	return xintNew(t, "m", func(b *xintBuilder) {
		b.Initial("waiting")
		b.State("waiting").After(xintMs(100)).To("done")
		b.State("done")
	})
}

// XState: clock > uses the injected clock time for scheduled timer metadata
func TestXIntClockUsesInjectedClockTimeForTimerMetadata(t *testing.T) {
	t.Parallel()

	clock := xintNewClock()
	clock.Add(xintMs(1000))

	inst, err := xintWaitingDone(t).Instance(t.Context(), counter{N: 0}, sc.WithClock(clock.Now), sc.ManualTimers())
	require.NoError(t, err)
	defer inst.Close()

	timers := inst.Snapshot().Timers()
	require.Len(t, timers, 1)
	require.Equal(t, xintMs(100), timers[0].Delay)
	require.Equal(t, epoch.Add(xintMs(1100)), timers[0].Due)
}

// XState: clock > uses the injected clock time when restoring scheduled
// timers
func TestXIntClockUsesInjectedClockTimeWhenRestoringTimers(t *testing.T) {
	t.Parallel()

	m := xintWaitingDone(t)

	// Scheduled at 950 and due at 1050, the timer is restored at 1000.
	snap, _ := m.Start(counter{N: 0}, epoch.Add(xintMs(950)))
	data, err := json.Marshal(snap)
	require.NoError(t, err)

	restoredAt := epoch.Add(xintMs(1000))
	restored, _, err := m.Restore(data, restoredAt)
	require.NoError(t, err)

	resumed := m.Resume(restored)
	require.Equal(t, []sc.EffectKind{sc.EffectStartTimer}, kinds(resumed))
	require.Equal(t, epoch.Add(xintMs(1050)), resumed[0].At)
	require.Equal(t, xintMs(50), resumed[0].Wait(restoredAt, func() float64 { return 0 }))
}

// XState: clock > system clock should be default clock for actors (invoked
// from machine)
func TestXIntClockIsDefaultClockForInvokedMachine(t *testing.T) {
	t.Parallel()

	synctest.Test(t, func(t *testing.T) {
		child := xintNew(t, "child", func(b *xintBuilder) {
			b.Initial("a")
			b.State("a").After(10 * time.Second).To("b")
			b.State("b")
		})

		parent := xintNew(t, "parent", func(b *xintBuilder) {
			b.Chart().InvokeMachine("child", child,
				func(c counter) counter { return c },
				func(xintSnapshot) (struct{}, error) { return struct{}{}, nil },
			).OnError().Stay()
		})

		clock := xintNewClock()
		inst, err := parent.Instance(t.Context(), counter{N: 0}, sc.WithClock(clock.Now), sc.ManualTimers())
		require.NoError(t, err)
		defer inst.Close()

		childState := func() []string {
			synctest.Wait()

			snap, ok := inst.Snapshot().Child(child, "child")
			require.True(t, ok)

			return snap.Active()
		}

		require.Equal(t, []string{"a"}, childState())

		// Like XState's, incrementing the parent's clock moves the child,
		// which shares it, to b: the child follows the parent's manual
		// timers, and the parent's Advance advances it.
		clock.Add(9 * time.Second)
		_, err = inst.Advance(t.Context())
		require.NoError(t, err)
		require.Equal(t, []string{"a"}, childState())

		clock.Add(time.Second)
		_, err = inst.Advance(t.Context())
		require.NoError(t, err)
		require.Equal(t, []string{"b"}, childState())
	})
}

// xintRedGreen is red, which goes to green after 100ms.
func xintRedGreen(t *testing.T) *xintMachine {
	t.Helper()

	return xintNew(t, "m", func(b *xintBuilder) {
		b.Initial("red")
		b.State("red").After(xintMs(100)).To("green")
		b.State("green")
	})
}

// XState: logical snapshot timers > declares a pending timer in the pure
// initial snapshot
func TestXIntTimersPendingTimerInPureInitialSnapshot(t *testing.T) {
	t.Parallel()

	m := xintRedGreen(t)

	snap, effects := m.Start(counter{N: 0}, epoch)
	schedule := effect(t, effects, sc.EffectStartTimer)
	timers := snap.Timers()
	require.Len(t, timers, 1)

	require.Equal(t, "red", schedule.State)
	require.Equal(t, xintMs(100), schedule.Delay)

	// DIVERGES from XState: XState's built-in timer has no id, being
	// identified by its event, and the snapshot's entry has no scheduledAt,
	// dueAt or elapsed: the pure core knows no time. sc's core is given the
	// time, so the timer has an ID, which the effect shares, and the time
	// it is due.
	require.Equal(t, sc.PendingTimer[string]{
		ID:      schedule.ID,
		State:   "red",
		Name:    "",
		Retry:   false,
		Attempt: 0,
		Raised:  nil,
		Delay:   xintMs(100),
		Due:     epoch.Add(xintMs(100)),
		Jitter:  0,
	}, timers[0])
	require.NotEmpty(t, schedule.ID)
	require.Equal(t, timers[0].Due, schedule.At)
}

// XState: logical snapshot timers > keeps wall-clock bookkeeping in the
// runtime
func TestXIntTimersWallClockBookkeeping(t *testing.T) {
	t.Parallel()

	synctest.Test(t, func(t *testing.T) {
		ctx, cancel := context.WithCancel(t.Context())
		began := time.Now()
		a := xintWaitingDone(t).Actor(counter{N: 0})
		wait := runActor(ctx, a)
		synctest.Wait()

		// The runtime's bookkeeping: when the actor scheduled the timer.
		scheduled := a.Timers()
		require.Len(t, scheduled, 1)
		require.Equal(t, "waiting", scheduled[0].State)
		require.Equal(t, xintMs(100), scheduled[0].Delay)
		require.Equal(t, began.Add(xintMs(100)), scheduled[0].Due)

		// DIVERGES from XState: the timer in XState's snapshot has no
		// scheduledAt or dueAt, which only the runtime keeps. The timer in
		// sc's snapshot carries the time it is due.
		require.Equal(t, began.Add(xintMs(100)), a.Snapshot().Timers()[0].Due)

		cancel()
		require.ErrorIs(t, wait(), context.Canceled)
	})
}

// XState: logical snapshot timers > consumes a delayed raise in the same
// macrostep
func TestXIntTimersConsumesTimerInSameMacrostep(t *testing.T) {
	t.Parallel()

	m := xintRedGreen(t)
	red, _ := m.Start(counter{N: 0}, epoch)

	consumed, effects := m.Transition(red, sc.TimerFired{ID: red.Timers()[0].ID}, epoch.Add(xintMs(100)))

	require.Equal(t, []string{"green"}, consumed.Active())
	require.Empty(t, consumed.Timers())

	// DIVERGES from XState: XState returns a cancel effect for the timer
	// it has just consumed. sc returns none: a timer that fired needs no
	// cancelling.
	require.Empty(t, effects)

	// Like XState's, the timer input is taken at its word: the core does
	// not check that the time it is given has reached the timer's.
	early, _ := m.Transition(red, sc.TimerFired{ID: red.Timers()[0].ID}, epoch)
	require.Equal(t, []string{"green"}, early.Active())
}

// XState: logical snapshot timers > removes cancelled timers and ignores
// stale timer inputs
func TestXIntTimersRemovesCancelledTimersAndIgnoresStaleInputs(t *testing.T) {
	t.Parallel()

	m := xintNew(t, "m", func(b *xintBuilder) {
		b.Initial("waiting")
		waiting := b.State("waiting")
		waiting.After(xintMs(100)).To("late")
		waiting.On[xintExit]().To("done")
		b.State("late")
		b.State("done")
	})

	waiting, _ := m.Start(counter{N: 0}, epoch)
	timer := waiting.Timers()[0]

	done, exitEffects := m.Transition(waiting, xintExit{}, epoch)
	require.Empty(t, done.Timers())
	require.Equal(t, []sc.EffectKind{sc.EffectCancelTimer}, kinds(exitEffects))
	require.Equal(t, timer.ID, exitEffects[0].ID)

	unchanged, staleEffects := m.Transition(done, sc.TimerFired{ID: timer.ID}, epoch.Add(xintMs(100)))
	require.Equal(t, done, unchanged)
	require.Empty(t, staleEffects)
}

// XState: logical snapshot timers > takes an after transition before
// reevaluating an %s always guard (outside, event-dependent)
func TestXIntTimersAfterTransitionBeforeReevaluatingAlwaysGuard(t *testing.T) {
	t.Parallel()

	for _, guard := range []string{"outside", "event-dependent"} {
		t.Run(guard, func(t *testing.T) {
			t.Parallel()

			var ready atomic.Bool

			m := xintNew(t, "m", func(b *xintBuilder) {
				b.Initial("waiting")
				waiting := b.State("waiting")
				if guard == "outside" {
					waiting.Always().When("ready", func(counter, struct{}) bool { return ready.Load() }).To("early")
				} else {
					// The event of an Advance that fires no timer, like
					// that of the step that starts the instance, is nil,
					// so this guard cannot tell the two apart as XState's
					// does with event.type !== 'xstate.init'.
					waiting.Always().WhenEvent("notInit", func(_ counter, ev any) bool { return ev != nil }).To("early")
				}
				waiting.After(xintMs(10)).To("elapsed")
				b.State("early")
				b.State("elapsed")
			}, sc.AllowUnreachable())

			waiting, _ := m.Start(counter{N: 0}, epoch)
			require.Equal(t, []string{"waiting"}, waiting.Active())

			ready.Store(true)
			early, _ := m.Advance(waiting, epoch.Add(xintMs(9)))
			if guard == "outside" {
				// DIVERGES from XState: incrementing the clock by 9ms
				// fires nothing, so XState does not look at the always
				// guard and stays in waiting. Advance evaluates the
				// eventless transitions even when no timer is due, so a
				// guard that reads something outside the chart is seen
				// to have changed.
				require.Equal(t, []string{"early"}, early.Active())
			} else {
				require.Equal(t, []string{"waiting"}, early.Active())
			}

			// What the case is about holds: when the timer is due, its
			// transition is taken before the always guard is looked at
			// again.
			elapsed, _ := m.Advance(waiting, epoch.Add(xintMs(10)))
			require.Equal(t, []string{"elapsed"}, elapsed.Active())
		})
	}
}

// XState: logical snapshot timers > handles a delayed %s before always and
// settles before raised events (raise)
func TestXIntTimersDelayedRaiseBeforeAlwaysAndSettlesBeforeRaisedEvents(t *testing.T) {
	t.Parallel()

	var ready atomic.Bool

	m := xintNew(t, "m", func(b *xintBuilder) {
		b.Initial("waiting")
		waiting := b.State("waiting").RaiseAfterOnEntry("expire", xintMs(10), func(counter) any { return xintExpire{} })
		waiting.Always().When("ready", func(counter, struct{}) bool { return ready.Load() }).To("early")
		waiting.On[xintExpire]().To("elapsed")
		b.State("early")
		elapsed := b.State("elapsed")
		elapsed.RaiseOnEntry("internal", func(counter) any { return xintInternal{} })
		elapsed.Always().
			WhenEvent("isExpire", func(_ counter, ev any) bool { _, ok := ev.(xintExpire); return ok }).
			To("settled")
		elapsed.On[xintInternal]().To("early")
		b.State("settled")
	})

	snap, _ := m.Start(counter{N: 0}, epoch)
	require.Equal(t, []string{"waiting"}, snap.Active())

	ready.Store(true)
	snap, _ = m.Advance(snap, epoch.Add(xintMs(10)))

	// Like XState's, the delayed EXPIRE is handled before waiting's always,
	// and elapsed's always guard, which asks whether the event being
	// processed is EXPIRE, sees it, so the chart settles before INTERNAL is
	// handled.
	require.Equal(t, []string{"settled"}, snap.Active())

	// The same chart sent EXPIRE from outside settles the same way.
	ready.Store(false)
	snap, _ = m.Start(counter{N: 0}, epoch)
	snap, _ = m.Transition(snap, xintExpire{}, epoch)
	require.Equal(t, []string{"settled"}, snap.Active())
}

// XState: logical snapshot timers > handles a state timeout before
// reevaluating always
//
// XState's timeout and onTimeout are a delayed transition here: Within, sc's
// state timeout, bounds a state's services and not the state.
func TestXIntTimersStateTimeoutBeforeReevaluatingAlways(t *testing.T) {
	t.Parallel()

	var ready atomic.Bool

	m := xintNew(t, "m", func(b *xintBuilder) {
		b.Initial("waiting")
		waiting := b.State("waiting")
		waiting.Always().When("ready", func(counter, struct{}) bool { return ready.Load() }).To("early")
		waiting.After(xintMs(10)).To("elapsed")
		b.State("early")
		b.State("elapsed")
	})

	snap, _ := m.Start(counter{N: 0}, epoch)

	ready.Store(true)
	snap, _ = m.Advance(snap, epoch.Add(xintMs(10)))

	require.Equal(t, []string{"elapsed"}, snap.Active())
}

// XState: logical snapshot timers > reevaluates always after an unhandled
// delayed raise
func TestXIntTimersReevaluatesAlwaysAfterUnhandledDelayedRaise(t *testing.T) {
	t.Parallel()

	m := xintNew(t, "m", func(b *xintBuilder) {
		b.Initial("waiting")
		b.State("waiting").
			RaiseAfterOnEntry("ignored", xintMs(10), func(counter) any { return xintIgnored{} }).
			Always().
			WhenEvent("isIgnored", func(_ counter, ev any) bool { _, ok := ev.(xintIgnored); return ok }).
			To("settled")
		// A delayed event must be of a type the chart names in an On
		// somewhere; nothing handles it where it arrives.
		b.State("settled").On[xintIgnored]().Stay()
	})

	snap, _ := m.Start(counter{N: 0}, epoch)
	require.Equal(t, []string{"waiting"}, snap.Active())

	// Like XState, sc reevaluates always with the delayed event that
	// nothing handled, and settles.
	delayed, _ := m.Advance(snap, epoch.Add(xintMs(10)))
	require.Equal(t, []string{"settled"}, delayed.Active())
	require.Empty(t, delayed.Timers())

	// So does the same event sent from outside, still handled by nothing.
	sent, _ := m.Transition(snap, xintIgnored{}, epoch)
	require.Equal(t, []string{"settled"}, sent.Active())
}

// XState: logical snapshot timers > numbers anonymous delayed effects
// deterministically
//
// An sc raise always has a name, and its timer an ID made from it, so there
// is no sequence number to check; that the IDs are the same each time is.
func TestXIntTimersDelayedEffectsAreIdentifiedDeterministically(t *testing.T) {
	t.Parallel()

	m := xintNew(t, "m", func(b *xintBuilder) {
		b.On[xintSchedule]().
			RaiseAfter("first", xintMs(10), func(counter, xintSchedule) any { return xintFirst{} }).
			RaiseAfter("second", xintMs(20), func(counter, xintSchedule) any { return xintSecond{} })
		b.On[xintFirst]().Stay()
		b.On[xintSecond]().Stay()
	})

	initial, _ := m.Start(counter{N: 0}, epoch)

	left, leftEffects := m.Transition(initial, xintSchedule{}, epoch)
	right, rightEffects := m.Transition(initial, xintSchedule{}, epoch)

	require.Len(t, left.Timers(), 2)
	require.Equal(t, xintFirst{}, left.Timers()[0].Raised)
	require.Equal(t, xintSecond{}, left.Timers()[1].Raised)
	require.Equal(t, left.Timers(), right.Timers())
	require.Equal(t, leftEffects, rightEffects)
	require.Equal(t, []sc.EffectKind{sc.EffectStartTimer, sc.EffectStartTimer}, kinds(leftEffects))
	require.Equal(t, left.Timers()[0].ID, leftEffects[0].ID)
	require.Equal(t, left.Timers()[1].ID, leftEffects[1].ID)
	require.NotEqual(t, left.Timers()[0].ID, left.Timers()[1].ID)

	fired, _ := m.Transition(left, sc.TimerFired{ID: left.Timers()[1].ID}, epoch)
	require.Equal(t, []sc.PendingTimer[string]{left.Timers()[0]}, fired.Timers())
}

// XState: logical snapshot timers > cancels remaining timers after child
// stops when reaching final
func TestXIntTimersCancelsRemainingTimersWhenReachingFinal(t *testing.T) {
	t.Parallel()

	m := xintNew(t, "m", func(b *xintBuilder) {
		b.Initial("active")
		active := b.State("active")
		active.Stream("child", func(ctx context.Context, _ counter) iter.Seq2[any, error] {
			return func(func(any, error) bool) { <-ctx.Done() }
		}).OnError().Stay()
		active.On[xintFinish]().
			RaiseAfter("late", xintMs(100), func(counter, xintFinish) any { return xintLate{} }).
			To("done")
		active.On[xintLate]().Stay()
		b.Final("done")
	})

	active, _ := m.Start(counter{N: 0}, epoch)
	require.Len(t, active.Services(), 1)

	done, effects := m.Transition(active, xintFinish{}, epoch)

	require.True(t, done.Done())
	require.Empty(t, done.Services())
	require.Empty(t, done.Timers())

	// DIVERGES from XState: XState's effects are stop, raise, cancel and
	// terminate: the delayed event is scheduled and then cancelled, and
	// the actor told to terminate. sc never starts a timer that the same
	// step cancels, and has no terminate effect: the snapshot is done.
	require.Equal(t, []sc.EffectKind{sc.EffectStopInvocation}, kinds(effects))
}

// Cases that are not ported, with the reason:
//
// interpreter.test.ts
//   - "state.children should reference spawned actors" and "stopped spawned
//     actors should be cleaned up in parent": XState skips both (it.skip).
//     They spawn actors from the root's entry action and keep them in the
//     snapshot's children; sc spawns only from a transition, and a snapshot
//     holds no actor refs.
//
// waitFor.test.ts
//   - "should not subscribe when the predicate immediately matches", "should
//     not subscribe to the actor when it receives an aborted signal": they
//     replace actor.subscribe with a spy. Await is not built on a
//     subscription that could be observed.
//   - "should not listen for the "abort" event when it receives an aborted
//     signal", "should not listen for the "abort" event for actor in its
//     final state that matches the predicate", "should stop listening for
//     the "abort" event upon successful completion", "should stop listening
//     for the "abort" event upon failure": they spy on the AbortSignal's
//     addEventListener and removeEventListener. A context has no listeners
//     to add or remove.
//
// clock.test.ts
//   - "orders large mixed batches while removing cancelled entries and
//     inserting callback timers", "continues flushing future timers after a
//     callback throws": about SimulatedClock's own queue of callbacks. sc
//     has no clock object: a clock is a func() time.Time, and timers belong
//     to the snapshot.
//
// logical-timers.test.ts
//   - "handles a delayed sendTo before always and settles before raised
//     events": the sendTo half of an it.each. A chart sends itself an event
//     only by raising it, which is the half that is ported.
//   - "does not reevaluate the sender always transitions for a delayed send
//     to a child", "uses the same timer mechanism for delayed sends to
//     children": SendTo has no delay.
//   - "exposes timer consumption as its own microstep": there is no
//     getMicrosteps; Transition returns the result of the whole step.
//   - "cancels remaining timers after child stops when explicitly stopped":
//     the pure core has no stop event. Stopping is the driver's, and leaves
//     the snapshot as it was so that it can be resumed.
