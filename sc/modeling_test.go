package sc_test

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/sc"
)

// attempts is the context of the timer charts in this file.
type attempts struct {
	Failures int       `json:"failures"`
	RetryAt  time.Time `json:"retry_at"`
}

func backoff(c attempts) time.Duration { return time.Second << c.Failures }

// backoffChart waits for a backoff that doubles with each failure, and
// starts the wait again on every tick, which counts as a failure.
func backoffChart(t *testing.T) *sc.Machine[string, attempts] {
	t.Helper()

	m, err := sc.New("backoff", func(b *sc.Builder[string, attempts]) {
		b.Initial("waiting")
		waiting := b.State("waiting")
		waiting.Delay("backoff", backoff).To("done")
		waiting.On[tick]().
			Assign("countFailure", func(c attempts, _ tick) attempts { c.Failures++; return c }).
			To("waiting").
			Reenter()
		b.Final("done")
	})
	require.NoError(t, err)

	return m
}

func TestDelayIsComputedFromTheContextWhenTheStateIsEntered(t *testing.T) {
	t.Parallel()

	m := backoffChart(t)

	snap, effects := m.Start(attempts{}, epoch)
	first := effect(t, effects, sc.EffectStartTimer)
	require.Equal(t, time.Second, first.Delay)
	require.Equal(t, "backoff", first.Name)
	require.Equal(t, epoch.Add(time.Second), first.At)

	_, effects = m.Transition(snap, tick{}, epoch)

	require.Equal(t, first.ID, effect(t, effects, sc.EffectCancelTimer).ID)
	require.Equal(t, 2*time.Second, effect(t, effects, sc.EffectStartTimer).Delay)
}

func TestNegativeDelayFiresAtOnce(t *testing.T) {
	t.Parallel()

	m, err := sc.New("negative", func(b *sc.Builder[string, attempts]) {
		b.Initial("waiting")
		b.State("waiting").Delay("overdue", func(attempts) time.Duration { return -time.Minute }).To("done")
		b.Final("done")
	})
	require.NoError(t, err)

	snap, effects := m.Start(attempts{}, epoch)

	// The timer is due at once, so it fires inside Start and the driver
	// never sees it.
	require.True(t, snap.Done())
	require.Empty(t, effects)
}

// deadlineChart waits until the time stored in the context.
func deadlineChart(t *testing.T) *sc.Machine[string, attempts] {
	t.Helper()

	m, err := sc.New("deadline", func(b *sc.Builder[string, attempts]) {
		b.Initial("waiting")
		b.State("waiting").Deadline("retryAt", func(c attempts) time.Time { return c.RetryAt }).To("done")
		b.Final("done")
	})
	require.NoError(t, err)

	return m
}

func TestDeadlineTimerCarriesItsTimeThroughSnapshotsAndEffects(t *testing.T) {
	t.Parallel()

	m := deadlineChart(t)
	retryAt := time.Date(2026, 11, 12, 22, 53, 32, 0, time.UTC)

	snap, effects := m.Start(attempts{RetryAt: retryAt}, epoch)
	timer := effect(t, effects, sc.EffectStartTimer)
	require.Equal(t, retryAt, timer.At)
	require.Zero(t, timer.Delay)

	data, err := m.EncodeEffect(timer)
	require.NoError(t, err)
	decoded, err := m.DecodeEffect(data)
	require.NoError(t, err)
	require.True(t, retryAt.Equal(decoded.At))

	stored, err := json.Marshal(snap)
	require.NoError(t, err)
	restored, _, err := m.Restore(stored, epoch)
	require.NoError(t, err)

	resumed := m.Resume(restored)
	require.Len(t, resumed, 1)
	require.True(t, retryAt.Equal(resumed[0].At))
	require.Equal(t, timer.ID, resumed[0].ID)

	done, _ := m.Transition(restored, sc.TimerFired{ID: timer.ID}, epoch)
	require.True(t, done.Done())
}

func TestResumeRestartsAComputedDelayWithTheDurationItStartedWith(t *testing.T) {
	t.Parallel()

	m := backoffChart(t)
	snap, _ := m.Start(attempts{}, epoch)
	snap, _ = m.Transition(snap, tick{}, epoch)
	snap, _ = m.Transition(snap, tick{}, epoch)

	stored, err := json.Marshal(snap)
	require.NoError(t, err)
	restored, _, err := m.Restore(stored, epoch)
	require.NoError(t, err)

	require.Equal(t, 4*time.Second, m.Resume(restored)[0].Delay)
}

// coordinatedChart has two regions. The right region may only finish once
// the left one has.
func coordinatedChart(t *testing.T) *sc.Machine[string, counter] {
	t.Helper()

	m, err := sc.New("coordinated", func(b *sc.Builder[string, counter]) {
		b.Initial("both")
		b.Parallel("both", func(b *sc.Builder[string, counter]) {
			b.State("left", func(b *sc.Builder[string, counter]) {
				b.Initial("l1")
				b.State("l1").On[tick]().To("l2")
				b.State("l2")
			})
			b.State("right", func(b *sc.Builder[string, counter]) {
				b.Initial("r1")
				b.State("r1").On[pong]().
					When("counted", func(c counter, _ pong) bool { return c.N == 0 }).
					WhenIn("l2").
					To("r2")
				b.State("r2")
			})
		})
	})
	require.NoError(t, err)

	return m
}

func TestWhenInLetsOneRegionReactToAnother(t *testing.T) {
	t.Parallel()

	m := coordinatedChart(t)
	snap, _ := m.Start(counter{}, epoch)

	require.False(t, m.Can(snap, pong{}, epoch))
	blocked, _ := m.Transition(snap, pong{}, epoch)
	require.True(t, blocked.In("r1"))

	snap, _ = m.Transition(snap, tick{}, epoch)
	require.True(t, m.Can(snap, pong{}, epoch))

	snap, _ = m.Transition(snap, pong{}, epoch)
	require.True(t, snap.In("r2"))
}

func TestWhenInAndWhenBothHaveToHold(t *testing.T) {
	t.Parallel()

	m := coordinatedChart(t)
	snap, _ := m.Start(counter{N: 1}, epoch)
	snap, _ = m.Transition(snap, tick{}, epoch)

	require.False(t, m.Can(snap, pong{}, epoch))
}

func TestNewRejectsWhenInOnAnUndeclaredStateAndAnUnnamedDelay(t *testing.T) {
	t.Parallel()

	_, err := sc.New("bad", func(b *sc.Builder[string, counter]) {
		b.Initial("a")
		a := b.State("a")
		a.On[tick]().WhenIn("missing").To("b")
		a.Delay("", func(counter) time.Duration { return time.Second }).To("b")
		b.State("b")
	})

	require.ErrorContains(t, err, `state "a": WhenIn names "missing", which is not a state of the chart`)
	require.ErrorContains(t, err, `state "a": a delay has no name`)
}

func TestDefinitionNamesComputedDelaysAndStateConditions(t *testing.T) {
	t.Parallel()

	backoffDef := backoffChart(t).Definition()
	require.Equal(t, []sc.TransitionDefinition{{Target: "#done"}}, backoffDef.States["waiting"].After["backoff"])

	coordinated := coordinatedChart(t).Definition()
	right := coordinated.States["both"].States["right"].States["r1"]
	require.Equal(t, "counted && in(#l2)", right.On["pong"][0].Guard)
}

// unbounded is a context the exploration cannot use as it is: it holds a
// channel, which does not marshal to JSON, and a counter that grows forever.
type unbounded struct {
	Failures int
	Notify   chan struct{}
}

// cappedBackoffSteps is how many failures change the backoff below; more
// failures wait as long as this many.
const cappedBackoffSteps = 3

func unboundedChart(t *testing.T) *sc.Machine[string, unbounded] {
	t.Helper()

	m, err := sc.New("unbounded", func(b *sc.Builder[string, unbounded]) {
		b.Initial("waiting")
		waiting := b.State("waiting")
		waiting.Delay("backoff", func(c unbounded) time.Duration {
			return time.Second << min(c.Failures, cappedBackoffSteps)
		}).To("done")
		waiting.On[tick]().
			Assign("countFailure", func(c unbounded, _ tick) unbounded { c.Failures++; return c }).
			To("waiting").
			Reenter()
		b.Final("done")
	})
	require.NoError(t, err)

	return m
}

func TestExploreKeyProjectsAContextThatCannotBeExploredAsItIs(t *testing.T) {
	t.Parallel()

	m := unboundedChart(t)
	x := sc.Exploration[string, unbounded]{Events: []any{tick{}}}

	_, err := m.Explore(unbounded{Notify: make(chan struct{})}, x)
	require.ErrorContains(t, err, "marshal state for exploration")

	// Past the cap, more failures change nothing the chart does, so the
	// key may stop counting there.
	x.Key = func(c unbounded, _ time.Time) any { return min(c.Failures, cappedBackoffSteps) }

	report, err := m.Explore(unbounded{Notify: make(chan struct{})}, x)
	require.NoError(t, err)
	require.False(t, report.Truncated)
	// Waiting and done, each at 0 to cappedBackoffSteps failures.
	require.Equal(t, 2*(cappedBackoffSteps+1), report.States)
}

var errCardDeclinedAgain = errors.New("declined again")

// retryForeverChart charges until the charge succeeds.
func retryForeverChart(t *testing.T, final bool) *sc.Machine[string, counter] {
	t.Helper()

	m, err := sc.New("retry", func(b *sc.Builder[string, counter]) {
		b.Initial("charging")
		charge := b.State("charging").Invoke("charge", func(context.Context, *sc.Run, counter) (int, error) { return 0, nil })
		charge.OnDone().To("paid")
		charge.OnError().To("retrying")
		b.State("retrying").After(time.Second).To("charging")
		if final {
			b.Final("paid")
		} else {
			b.State("paid")
		}
	})
	require.NoError(t, err)

	return m
}

func TestExploreReportsAStateFromWhichNoFinalStateCanBeReached(t *testing.T) {
	t.Parallel()

	m := retryForeverChart(t, true)

	// Every state has a way out, so nothing is stuck, but with a charge
	// that only ever fails the loop never ends.
	failing, err := m.Explore(counter{}, sc.Exploration[string, counter]{
		Outcomes: map[string][]sc.Outcome{"charge": {sc.Fails(errCardDeclinedAgain)}},
	})
	require.NoError(t, err)
	require.Empty(t, failing.Stuck)
	require.Len(t, failing.Trapped, 2)
	require.True(t, failing.Trapped[0].Snapshot.In("charging"))
	require.True(t, failing.Trapped[1].Snapshot.In("retrying"))

	recovering, err := m.Explore(counter{}, sc.Exploration[string, counter]{
		Outcomes: map[string][]sc.Outcome{"charge": {sc.Succeeds(1), sc.Fails(errCardDeclinedAgain)}},
	})
	require.NoError(t, err)
	require.Empty(t, recovering.Trapped)
}

func TestExploreDoesNotReportTrappedStatesForAChartThatIsNotMeantToFinish(t *testing.T) {
	t.Parallel()

	report, err := retryForeverChart(t, false).Explore(counter{}, sc.Exploration[string, counter]{
		Outcomes: map[string][]sc.Outcome{"charge": {sc.Fails(errCardDeclinedAgain)}},
	})
	require.NoError(t, err)

	require.Nil(t, report.Trapped)
}

func TestSnapshotListsPendingTimersAndRunningServices(t *testing.T) {
	t.Parallel()

	m := orderChart(t, chargeOK)
	snap, chargeID := submitted(t, m)
	require.Equal(t, []sc.RunningService[state]{{ID: chargeID, State: charging, Name: "charge"}}, snap.Services())
	require.Empty(t, snap.Timers())

	snap, effects := m.Transition(snap, sc.Failed{ID: chargeID, Err: errCardDeclined}, epoch)
	timerID := effect(t, effects, sc.EffectStartTimer).ID

	require.Empty(t, snap.Services())
	require.Equal(t, []sc.PendingTimer[state]{{ID: timerID, State: retrying, Delay: retryDelay, Due: epoch.Add(retryDelay)}}, snap.Timers())
}

func TestJitterIsCarriedToTheDriverAndBoundsTheWait(t *testing.T) {
	t.Parallel()

	m, err := sc.New("jittered", func(b *sc.Builder[string, counter]) {
		b.Initial("waiting")
		b.State("waiting").After(time.Hour).Jitter(0.1).To("done")
		b.Final("done")
	})
	require.NoError(t, err)

	_, effects := m.Start(counter{}, epoch)
	timer := effect(t, effects, sc.EffectStartTimer)
	require.InDelta(t, 0.1, timer.Jitter, 0)

	now := epoch
	require.Equal(t, 54*time.Minute, timer.Wait(now, func() float64 { return 0 }))
	require.Equal(t, time.Hour, timer.Wait(now, func() float64 { return 0.5 }))
	require.Less(t, timer.Wait(now, func() float64 { return 0.999999 }), 66*time.Minute)

	deadline := sc.Effect[counter]{Kind: sc.EffectStartTimer, At: now.Add(-time.Minute)}
	require.Zero(t, deadline.Wait(now, func() float64 { return 0 }))
}

func TestNewRejectsJitterOutsideADelayedTransition(t *testing.T) {
	t.Parallel()

	_, err := sc.New("bad", func(b *sc.Builder[string, counter]) {
		b.Initial("a")
		a := b.State("a")
		a.On[tick]().Jitter(0.1).To("b")
		a.After(time.Second).Jitter(2).To("b")
		b.State("b")
	})

	require.ErrorContains(t, err, `state "a": Jitter applies only to delayed transitions`)
	require.ErrorContains(t, err, `state "a": jitter 2 is not between 0 and 1`)
}

func TestEveryGuardOnATransitionHasToHold(t *testing.T) {
	t.Parallel()

	m, err := sc.New("guards", func(b *sc.Builder[string, counter]) {
		b.Initial("a")
		b.State("a").On[tick]().
			When("positive", func(c counter, _ tick) bool { return c.N > 0 }).
			When("small", func(c counter, _ tick) bool { return c.N < 3 }).
			To("b")
		b.State("b")
	})
	require.NoError(t, err)

	for n, want := range map[int]bool{0: false, 1: true, 2: true, 3: false} {
		snap, _ := m.Start(counter{N: n}, epoch)
		require.Equalf(t, want, m.Can(snap, tick{}, epoch), "N=%d", n)
	}

	require.Equal(t, "positive && small", m.Definition().States["a"].On["tick"][0].Guard)
}

func TestWhenNotInBlocksWhileTheOtherRegionIsInAState(t *testing.T) {
	t.Parallel()

	m, err := sc.New("exclusive", func(b *sc.Builder[string, counter]) {
		b.Initial("both")
		b.Parallel("both", func(b *sc.Builder[string, counter]) {
			b.State("left", func(b *sc.Builder[string, counter]) {
				b.Initial("l1")
				b.State("l1").On[tick]().To("l2")
				b.State("l2")
			})
			b.State("right", func(b *sc.Builder[string, counter]) {
				b.Initial("r1")
				b.State("r1").On[pong]().WhenNotIn("l2").To("r2")
				b.State("r2")
			})
		})
	})
	require.NoError(t, err)

	snap, _ := m.Start(counter{}, epoch)
	require.True(t, m.Can(snap, pong{}, epoch))

	snap, _ = m.Transition(snap, tick{}, epoch)
	require.False(t, m.Can(snap, pong{}, epoch))
	require.Equal(t, "!in(#l2)", m.Definition().States["both"].States["right"].States["r1"].On["pong"][0].Guard)
}

func TestAnEventThatChangesNothingLeavesTheSnapshotAsItWas(t *testing.T) {
	t.Parallel()

	m := orderChart(t, chargeOK)
	snap, _ := m.Start(order{}, epoch)

	unhandled, effects := m.Transition(snap, resume{}, epoch)
	require.Equal(t, snap, unhandled)
	require.Nil(t, effects)

	blocked, _ := m.Transition(snap, submit{}, epoch)
	require.Equal(t, snap.Seq(), blocked.Seq())
}

func TestStayDropsAnExpectedEventOnPurpose(t *testing.T) {
	t.Parallel()

	m, err := sc.New("stay", func(b *sc.Builder[string, counter]) {
		b.Initial("closing")
		closing := b.State("closing")
		inv := closing.Invoke("close", func(context.Context, *sc.Run, counter) (int, error) { return 0, nil })
		inv.OnDone().To("closed")
		inv.OnError().Stay()
		b.Final("closed")
	})
	require.NoError(t, err)

	snap, effects := m.Start(counter{}, epoch)
	id := effect(t, effects, sc.EffectStartInvocation).ID

	snap, effects = m.Transition(snap, sc.Failed{ID: id, Err: errWorkFailed}, epoch)

	require.Equal(t, []string{"closing"}, snap.Active())
	require.Empty(t, effects)
	require.Empty(t, snap.Services())
}

func TestNewRejectsStayWithATargetOrSteps(t *testing.T) {
	t.Parallel()

	_, err := sc.New("bad", func(b *sc.Builder[string, counter]) {
		b.Initial("a")
		b.State("a").On[tick]().Stay().To("b")
		b.State("b")
	})

	require.ErrorContains(t, err, `state "a": a transition marked Stay has a target or steps`)
}

func TestReachedCountsAStateLeftWithinTheSameStep(t *testing.T) {
	t.Parallel()

	m, err := sc.New("passing", func(b *sc.Builder[string, counter]) {
		b.Initial("a")
		b.State("a").On[tick]().To("transient")
		b.State("transient").Always().To("b")
		b.State("b")
	})
	require.NoError(t, err)

	report, err := m.Explore(counter{}, sc.Exploration[string, counter]{Events: []any{tick{}}})
	require.NoError(t, err)

	require.True(t, report.Reached("transient"))
	require.Equal(t, 2, report.States)
}

func TestFormatTraceNamesEachStep(t *testing.T) {
	t.Parallel()

	m := orderChart(t, chargeOK)
	report, err := m.Explore(order{}, sc.Exploration[state, order]{
		Events:   []any{submit{Items: []string{"book"}}, hold{}},
		Outcomes: map[string][]sc.Outcome{"charge": {sc.Fails(errCardDeclined)}},
		Invariants: []sc.Invariant[state, order]{{
			Name:  "never on hold",
			Holds: func(s sc.Snapshot[state, order]) bool { return !s.In(onHold) },
		}},
	})
	require.NoError(t, err)
	require.Len(t, report.Violations, 1)

	require.Equal(t, "1. +0s submit\n2. +0s hold\nends in [on_hold]", m.FormatTrace(report.Violations[0].Trace))
}

type (
	connect    struct{}
	disconnect struct{}
)

// statusChart records its status on every move and notifies only when the
// online boundary is crossed, which is the host port's heartbeat-then-wake
// pattern written once.
func statusChart(t *testing.T, record func(mv sc.Move[string])) *sc.Machine[string, counter] {
	t.Helper()

	m, err := sc.New("status", func(b *sc.Builder[string, counter]) {
		b.Action("record", func(_ context.Context, _ counter, mv sc.Move[string]) { record(mv) })

		b.Initial("offline")
		b.State("offline").OnEntry("record").On[connect]().To("connecting")
		connecting := b.State("connecting").OnEntry("record")
		connecting.On[tick]().To("online")
		connecting.On[disconnect]().Run("record")
		b.State("online").OnEntry("record").On[disconnect]().To("offline")
	})
	require.NoError(t, err)

	return m
}

func TestChartActionsSeeTheMoveTheyRunIn(t *testing.T) {
	t.Parallel()

	var moves []string
	m := statusChart(t, func(mv sc.Move[string]) {
		moves = append(moves, fmt.Sprintf("%s: %v -> %v", mv.Source, mv.From, mv.To))
	})

	exec := func(effects []sc.Effect[counter]) {
		for _, e := range effects {
			if e.Kind == sc.EffectAction {
				// Through JSON, as a persisting driver would.
				data, err := m.EncodeEffect(e)
				require.NoError(t, err)
				decoded, err := m.DecodeEffect(data)
				require.NoError(t, err)
				require.NoError(t, m.Exec(t.Context(), decoded, func(any) {}))
			}
		}
	}

	snap, effects := m.Start(counter{}, epoch)
	exec(effects)
	snap, effects = m.Transition(snap, connect{}, epoch)
	exec(effects)
	snap, effects = m.Transition(snap, disconnect{}, epoch)
	exec(effects)
	_, effects = m.Transition(snap, tick{}, epoch)
	exec(effects)

	require.Equal(t, []string{
		"offline: [] -> [offline]",
		"connecting: [offline] -> [connecting]",
		"connecting: [connecting] -> [connecting]",
		"online: [connecting] -> [online]",
	}, moves)
}

func TestNewRejectsUndeclaredAndClashingChartActions(t *testing.T) {
	t.Parallel()

	_, err := sc.New("bad", func(b *sc.Builder[string, counter]) {
		b.Action("record", func(context.Context, counter, sc.Move[string]) {})
		b.Action("record", func(context.Context, counter, sc.Move[string]) {})
		b.Initial("a")
		b.State("a").OnEntry("missing").Entry("record", noop).On[tick]().Run("absent").To("b")
		b.State("b")
	})

	require.ErrorContains(t, err, `chart action "record" is declared twice`)
	require.ErrorContains(t, err, `state "a": runs chart action "missing", which is not declared`)
	require.ErrorContains(t, err, `state "a": runs chart action "absent", which is not declared`)
	require.ErrorContains(t, err, `state "a": action "record" has the name of a chart action`)
}

func TestDefinitionListsChartActionsWhereTheyRun(t *testing.T) {
	t.Parallel()

	def := statusChart(t, func(sc.Move[string]) {}).Definition()

	require.Equal(t, []string{"record"}, def.States["online"].Entry)
	require.Equal(t, []string{"record"}, def.States["connecting"].On["disconnect"][0].Actions)
}

func TestExploreReportsAGoalSomeStateCanNeverGetBackTo(t *testing.T) {
	t.Parallel()

	m, err := sc.New("online", func(b *sc.Builder[string, counter]) {
		b.Initial("offline")
		b.State("offline").On[connect]().To("online")
		online := b.State("online")
		online.On[disconnect]().To("offline")
		online.On[leave]().To("retired")
		b.State("retired")
	})
	require.NoError(t, err)

	online := sc.Goal[string, counter]{Name: "online", Holds: func(s sc.Snapshot[string, counter]) bool { return s.In("online") }}

	recoverable, err := m.Explore(counter{}, sc.Exploration[string, counter]{
		Events: []any{connect{}, disconnect{}},
		Goals:  []sc.Goal[string, counter]{online},
	})
	require.NoError(t, err)
	require.Empty(t, recoverable.Violations)

	retiring, err := m.Explore(counter{}, sc.Exploration[string, counter]{
		Events: []any{connect{}, disconnect{}, leave{}},
		Goals:  []sc.Goal[string, counter]{online},
	})
	require.NoError(t, err)
	require.Len(t, retiring.Violations, 1)
	require.Equal(t, "goal: online", retiring.Violations[0].Invariant)
	require.Equal(t, []string{"retired"}, retiring.Violations[0].Trace.Snapshot.Active())
}

func TestGoalIgnoringLeavesOutStatesOnlyTheIgnoredEventsReach(t *testing.T) {
	t.Parallel()

	m, err := sc.New("closable", func(b *sc.Builder[string, counter]) {
		b.Initial("running")
		b.State("running", func(b *sc.Builder[string, counter]) {
			b.Initial("offline")
			b.State("offline").On[connect]().To("online")
			b.State("online").On[disconnect]().To("offline")
		}).On[leave]().To("closed")
		b.Final("closed")
	})
	require.NoError(t, err)

	online := sc.Goal[string, counter]{Name: "online", Holds: func(s sc.Snapshot[string, counter]) bool { return s.In("online") }}
	events := []any{connect{}, disconnect{}, leave{}}

	strict, err := m.Explore(counter{}, sc.Exploration[string, counter]{Events: events, Goals: []sc.Goal[string, counter]{online}})
	require.NoError(t, err)
	require.Len(t, strict.Violations, 1)

	online.Ignoring = []any{leave{}}
	lenient, err := m.Explore(counter{}, sc.Exploration[string, counter]{Events: events, Goals: []sc.Goal[string, counter]{online}})
	require.NoError(t, err)
	require.Empty(t, lenient.Violations)
}

func TestHoldsAtSeesTheExplorationsTime(t *testing.T) {
	t.Parallel()

	m := invitationChart(t)
	start := time.Date(2030, time.March, 1, 0, 0, 0, 0, time.UTC)

	report, err := m.Explore(expiry{ExpiresAt: start.Add(time.Hour)}, sc.Exploration[string, expiry]{
		Start: start,
		Waits: []time.Duration{2 * time.Hour},
		Key:   func(c expiry, now time.Time) any { return max(c.ExpiresAt.Sub(now), 0) },
		Invariants: []sc.Invariant[string, expiry]{{
			Name: "pending only before the expiry",
			HoldsAt: func(s sc.Snapshot[string, expiry], now time.Time) bool {
				return !s.In("pending") || now.Before(s.Context.ExpiresAt)
			},
		}},
	})
	require.NoError(t, err)

	require.Empty(t, report.Violations)
	require.True(t, report.Reached("expired"))
}

func TestCheckKeyCatchesAKeyThatMergesStatesThatBehaveDifferently(t *testing.T) {
	t.Parallel()

	m, err := sc.New("threshold", func(b *sc.Builder[string, counter]) {
		b.Initial("counting")
		counting := b.State("counting")
		counting.On[tick]().When("reached", func(c counter, _ tick) bool { return c.N >= 1 }).To("full")
		counting.On[tick]().Assign("increment", increment[tick])
		b.Final("full")
	})
	require.NoError(t, err)

	x := sc.Exploration[string, counter]{Events: []any{tick{}}, CheckKey: true}

	// The guard reads N, so a key without it merges a state that a tick
	// keeps counting with one that a tick finishes.
	x.Key = func(counter, time.Time) any { return 0 }
	careless, err := m.Explore(counter{}, x)
	require.NoError(t, err)
	require.Len(t, careless.Violations, 1)
	require.Equal(t, "key", careless.Violations[0].Invariant)

	// A key that keeps what the guard reads merges nothing it should not.
	x.Key = func(c counter, _ time.Time) any { return min(c.N, 1) }
	careful, err := m.Explore(counter{}, x)
	require.NoError(t, err)
	require.Empty(t, careful.Violations)
}

func TestFormatTraceShowsServiceResults(t *testing.T) {
	t.Parallel()

	m := orderChart(t, chargeOK)
	report, err := m.Explore(order{}, sc.Exploration[state, order]{
		Events:   []any{submit{Items: []string{"book"}}},
		Outcomes: map[string][]sc.Outcome{"charge": {sc.Succeeds("rcpt-1")}},
		Invariants: []sc.Invariant[state, order]{{
			Name:  "never fulfilling",
			Holds: func(s sc.Snapshot[state, order]) bool { return !s.In(fulfilling) },
		}},
	})
	require.NoError(t, err)
	require.Len(t, report.Violations, 1)

	require.Contains(t, m.FormatTrace(report.Violations[0].Trace), "done: rcpt-1")
}
