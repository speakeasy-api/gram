package sc_test

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
	"testing/synctest"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/sc"
)

func TestSnapshotReportsTheTagsOfItsActiveStates(t *testing.T) {
	t.Parallel()

	m, err := sc.New("tagged", func(b *sc.Builder[string, counter]) {
		b.Initial("idle")
		b.State("idle").On[tick]().To("working")
		b.State("working", func(b *sc.Builder[string, counter]) {
			b.Initial("fetching")
			b.State("fetching").Tag("network").On[tick]().To("done")
		}).Tag("busy")
		b.Final("done").Tag("terminal")
	})
	require.NoError(t, err)

	snap, _ := m.Start(counter{N: 0}, epoch)
	require.Empty(t, snap.Tags())
	require.False(t, snap.HasTag("busy"))

	snap, _ = m.Transition(snap, tick{}, epoch)
	require.Equal(t, []string{"busy", "network"}, snap.Tags())
	require.True(t, snap.HasTag("busy"))

	// Tags follow from the active states, so a restored snapshot has them.
	data, err := json.Marshal(snap)
	require.NoError(t, err)
	restored, _, err := m.Restore(data, epoch)
	require.NoError(t, err)
	require.True(t, restored.HasTag("network"))

	snap, _ = m.Transition(snap, tick{}, epoch)
	require.Equal(t, []string{"terminal"}, snap.Tags())
}

func TestOnAnyTakesWhatTheStatesOtherTransitionsDoNot(t *testing.T) {
	t.Parallel()

	m, err := sc.New("catchall", func(b *sc.Builder[string, counter]) {
		b.Initial("outer")
		outer := b.State("outer", func(b *sc.Builder[string, counter]) {
			b.Initial("inner")
			inner := b.State("inner")

			// Declared first, and still tried after the state's On.
			inner.OnAny().Assign("unexpected", func(c counter, _ any) counter { c.N += 100; return c })
			inner.On[tick]().When("small", func(c counter, _ tick) bool { return c.N < 2 }).Assign("increment", increment[tick])
		})
		outer.On[ping]().To("pinged")
		b.State("pinged")
	})
	require.NoError(t, err)

	snap, _ := m.Start(counter{N: 0}, epoch)

	snap, _ = m.Transition(snap, tick{}, epoch)
	snap, _ = m.Transition(snap, tick{}, epoch)
	require.Equal(t, 2, snap.Context.N)

	// The guard on tick now fails, so the catch-all takes it; it also
	// takes ping before the outer state's own transition can.
	snap, _ = m.Transition(snap, tick{}, epoch)
	require.Equal(t, 102, snap.Context.N)

	snap, _ = m.Transition(snap, ping{}, epoch)
	require.Equal(t, 202, snap.Context.N)
	require.True(t, snap.In("inner"))

	// A timer or a service result is not an event to catch.
	ignored, effects := m.Transition(snap, sc.TimerFired{ID: "no such timer"}, epoch)
	require.Equal(t, snap.Seq(), ignored.Seq())
	require.Empty(t, effects)

	require.Contains(t, m.Definition().States["outer"].States["inner"].On, "*")
}

var errFlaky = errors.New("flaky")

// retryChart runs a service that is retried twice, half a minute and then a
// minute apart.
func retryChart(t *testing.T, work func(context.Context, counter) (int, error), policy sc.Retry) *sc.Machine[string, counter] {
	t.Helper()

	m, err := sc.New("retrying", func(b *sc.Builder[string, counter]) {
		b.Initial("working")
		working := b.State("working")
		inv := working.Invoke("work", work).Retry(policy)
		inv.OnDone().To("done")
		inv.OnError().To("failed")
		working.On[leave]().To("failed")
		b.Final("done")
		b.Final("failed")
	})
	require.NoError(t, err)

	return m
}

func TestRetryRunsAFailedServiceAgainWithGrowingWaits(t *testing.T) {
	t.Parallel()

	m := retryChart(t, nil, sc.Retry{Max: 2, First: 30 * time.Second, Cap: 10 * time.Minute, Jitter: 0, If: nil})

	snap, effects := m.Start(counter{N: 0}, epoch)
	run := effect(t, effects, sc.EffectStartInvocation)

	// The first failure starts a wait instead of reaching the chart.
	snap, effects = m.Transition(snap, sc.Failed{ID: run.ID, Err: errFlaky}, epoch)
	require.Equal(t, []string{"working"}, snap.Active())
	require.Empty(t, snap.Services())
	wait := effect(t, effects, sc.EffectStartTimer)
	require.Equal(t, 30*time.Second, wait.Delay)
	require.Equal(t, []sc.PendingTimer[string]{{
		ID: wait.ID, State: "working", Name: "work", Retry: true, Attempt: 1, Raised: nil,
		Delay: 30 * time.Second, Due: epoch.Add(30 * time.Second), Jitter: 0,
	}}, snap.Timers())

	// The count survives being stored.
	data, err := json.Marshal(snap)
	require.NoError(t, err)
	snap, _, err = m.Restore(data, epoch)
	require.NoError(t, err)

	// When the wait ends the service runs again; a second failure waits
	// twice as long.
	at := epoch.Add(30 * time.Second)
	snap, effects = m.Advance(snap, at)
	run = effect(t, effects, sc.EffectStartInvocation)
	snap, effects = m.Transition(snap, sc.Failed{ID: run.ID, Err: errFlaky}, at)
	require.Equal(t, time.Minute, effect(t, effects, sc.EffectStartTimer).Delay)

	// The third failure is past the policy and reaches the chart.
	at = at.Add(time.Minute)
	snap, effects = m.Advance(snap, at)
	run = effect(t, effects, sc.EffectStartInvocation)
	snap, _ = m.Transition(snap, sc.Failed{ID: run.ID, Err: errFlaky}, at)
	require.Equal(t, []string{"failed"}, snap.Active())
}

func TestRetryLeavesAloneWhatItsPolicyDoesNotCover(t *testing.T) {
	t.Parallel()

	fatal := errors.New("fatal")
	m := retryChart(t, nil, sc.Retry{
		Max: sc.Forever, First: time.Second, Cap: 0, Jitter: 0,
		If: func(err error) bool { return !errors.Is(err, fatal) },
	})

	snap, effects := m.Start(counter{N: 0}, epoch)
	run := effect(t, effects, sc.EffectStartInvocation)

	failed, _ := m.Transition(snap, sc.Failed{ID: run.ID, Err: fatal}, epoch)
	require.Equal(t, []string{"failed"}, failed.Active())

	// Leaving the state while a retry waits cancels the wait.
	snap, _ = m.Transition(snap, sc.Failed{ID: run.ID, Err: errFlaky}, epoch)
	require.Len(t, snap.Timers(), 1)
	snap, effects = m.Transition(snap, leave{}, epoch)
	require.Equal(t, sc.EffectCancelTimer, effects[0].Kind)
	require.Empty(t, snap.Timers())
}

func TestExploreFollowsRetriesToTheirEnd(t *testing.T) {
	t.Parallel()

	m := retryChart(t, nil, sc.Retry{Max: sc.Forever, First: 30 * time.Second, Cap: 2 * time.Minute, Jitter: 0, If: nil})

	report, err := m.Explore(counter{N: 0}, sc.Exploration[string, counter]{
		Outcomes: map[string][]sc.Outcome{"work": {sc.Succeeds(1), sc.Fails(errFlaky)}},
	})
	require.NoError(t, err)

	// Retrying forever still has finitely many states, because the count
	// of failures stops growing once the wait has.
	require.False(t, report.Truncated)
	require.Empty(t, report.Trapped)
	require.Empty(t, report.Stuck)
}

func TestARetriedServiceRunsUntilItSucceedsUnderADriver(t *testing.T) {
	t.Parallel()

	synctest.Test(t, func(t *testing.T) {
		calls := 0
		m := retryChart(t, func(context.Context, counter) (int, error) {
			calls++
			if calls < 3 {
				return 0, errFlaky
			}

			return calls, nil
		}, sc.Retry{Max: 5, First: time.Second, Cap: time.Minute, Jitter: 0.5, If: nil})

		inst, err := m.Instance(t.Context(), counter{N: 0})
		require.NoError(t, err)
		defer inst.Close()

		snap, err := inst.Await(t.Context(), func(s sc.Snapshot[string, counter]) bool { return s.Done() })
		require.NoError(t, err)
		require.Equal(t, []string{"done"}, snap.Active())
		require.Equal(t, 3, calls)
	})
}

type (
	arm     struct{}
	disarm  struct{}
	expired struct{ Reason string }
)

// reminderChart raises expired a minute after arm, wherever it is by then.
func reminderChart(t *testing.T) *sc.Machine[string, counter] {
	t.Helper()

	m, err := sc.New("reminder", func(b *sc.Builder[string, counter]) {
		b.Initial("idle")
		b.State("idle").On[arm]().
			RaiseAfter("remind", time.Minute, func(counter, arm) any { return expired{Reason: "armed"} }).
			To("armed")
		armed := b.State("armed")
		armed.On[tick]().To("elsewhere")
		armed.On[disarm]().CancelRaise("remind").To("idle")
		armed.On[expired]().To("late")
		b.State("elsewhere").On[expired]().
			Assign("note", func(c counter, ev expired) counter { c.N = len(ev.Reason); return c }).
			To("late")
		b.State("late")
	})
	require.NoError(t, err)

	return m
}

func TestRaiseAfterDeliversItsEventWhereverTheChartIsByThen(t *testing.T) {
	t.Parallel()

	m := reminderChart(t)
	snap, _ := m.Start(counter{N: 0}, epoch)

	snap, effects := m.Transition(snap, arm{}, epoch)
	timer := effect(t, effects, sc.EffectStartTimer)
	require.Equal(t, epoch.Add(time.Minute), timer.At)
	require.Equal(t, expired{Reason: "armed"}, snap.Timers()[0].Raised)

	// Leaving the state that raised it does not cancel it.
	snap, effects = m.Transition(snap, tick{}, epoch)
	require.Empty(t, effects)

	// The waiting event survives being stored.
	data, err := json.Marshal(snap)
	require.NoError(t, err)
	snap, _, err = m.Restore(data, epoch)
	require.NoError(t, err)

	snap, _ = m.Transition(snap, sc.TimerFired{ID: timer.ID}, epoch.Add(time.Minute))
	require.Equal(t, []string{"late"}, snap.Active())
	require.Equal(t, len("armed"), snap.Context.N)
}

func TestCancelRaiseDropsAWaitingEvent(t *testing.T) {
	t.Parallel()

	m := reminderChart(t)
	snap, _ := m.Start(counter{N: 0}, epoch)
	snap, _ = m.Transition(snap, arm{}, epoch)

	snap, effects := m.Transition(snap, disarm{}, epoch)
	require.Equal(t, sc.EffectCancelTimer, effects[0].Kind)
	require.Empty(t, snap.Timers())

	snap, _ = m.Advance(snap, epoch.Add(time.Hour))
	require.Equal(t, []string{"idle"}, snap.Active())

	_, err := sc.New("dangling", func(b *sc.Builder[string, counter]) {
		b.Initial("a")
		b.State("a").On[tick]().CancelRaise("nothing").To("b")
		b.State("b")
	})
	require.ErrorContains(t, err, "no RaiseAfter declares")
}

type (
	start    struct{}
	pause    struct{}
	progress struct{ Percent int }
	finish   struct{ Fail bool }
)

var errChildFailed = errors.New("child failed")

// downloadChart is a child machine: it reports progress when told to and
// ends when told to finish.
func downloadChart(t *testing.T) *sc.Machine[string, counter] {
	t.Helper()

	m, err := sc.New("download", func(b *sc.Builder[string, counter]) {
		b.Initial("running")
		running := b.State("running")
		running.On[tick]().
			Assign("advance", func(c counter, _ tick) counter { c.N += 50; return c }).
			Emit("report", func(c counter, _ tick) any { return progress{Percent: c.N} })
		running.On[finish]().
			Assign("outcome", func(c counter, ev finish) counter {
				if ev.Fail {
					c.N = -1
				}

				return c
			}).
			To("ended")
		b.Final("ended")
	})
	require.NoError(t, err)

	return m
}

// parentChart runs downloadChart as a child, forwards ticks to it, and
// records the progress it reports.
func parentChart(t *testing.T, child *sc.Machine[string, counter]) *sc.Machine[string, counter] {
	t.Helper()

	m, err := sc.New("parent", func(b *sc.Builder[string, counter]) {
		b.Initial("idle")
		b.State("idle").On[start]().To("downloading")

		downloading := b.State("downloading")
		download := downloading.InvokeMachine("download", child,
			func(parent counter) counter { return counter{N: parent.N} },
			func(snap sc.Snapshot[string, counter]) (int, error) {
				if snap.Context.N < 0 {
					return 0, errChildFailed
				}

				return snap.Context.N, nil
			},
		)
		download.OnDone().
			Assign("keep", func(c counter, d sc.Done[int]) counter { c.N = d.Result; return c }).
			To("done")
		download.OnError().To("failed")

		downloading.On[tick]().SendTo("forward", "download", func(_ counter, ev tick) any { return ev })
		downloading.On[finish]().SendTo("finish", "download", func(_ counter, ev finish) any { return ev })
		downloading.On[progress]().Assign("seen", func(c counter, ev progress) counter { c.N = ev.Percent; return c })
		downloading.On[pause]().To("idle")

		b.Final("done")
		b.Final("failed")
	})
	require.NoError(t, err)

	return m
}

func TestAParentAndItsChildMachineTalkInEvents(t *testing.T) {
	t.Parallel()

	synctest.Test(t, func(t *testing.T) {
		m := parentChart(t, downloadChart(t))

		inst, err := m.Instance(t.Context(), counter{N: 0})
		require.NoError(t, err)
		defer inst.Close()

		_, err = inst.Send(t.Context(), start{})
		require.NoError(t, err)
		synctest.Wait()

		// The parent sends ticks down, and the child's progress comes
		// back up as an event of the parent's.
		for _, want := range []int{50, 100} {
			_, err = inst.Send(t.Context(), tick{})
			require.NoError(t, err)

			snap, err := inst.Await(t.Context(), func(s sc.Snapshot[string, counter]) bool { return s.Context.N == want })
			require.NoError(t, err)
			require.True(t, snap.In("downloading"))
		}

		// The child's final context is the service's result.
		_, err = inst.Send(t.Context(), finish{Fail: false})
		require.NoError(t, err)

		snap, err := inst.Await(t.Context(), func(s sc.Snapshot[string, counter]) bool { return s.Done() })
		require.NoError(t, err)
		require.Equal(t, []string{"done"}, snap.Active())
		require.Equal(t, 100, snap.Context.N)
	})
}

func TestAChildMachineThatEndsBadlyFailsItsService(t *testing.T) {
	t.Parallel()

	synctest.Test(t, func(t *testing.T) {
		m := parentChart(t, downloadChart(t))

		a := m.Actor(counter{N: 0})
		wait := runActor(t.Context(), a)

		require.NoError(t, a.Send(t.Context(), start{}))
		synctest.Wait()
		require.NoError(t, a.Send(t.Context(), finish{Fail: true}))

		require.NoError(t, wait())
		require.Equal(t, []string{"failed"}, a.Snapshot().Active())
	})
}

func TestLeavingTheStateClosesTheChildMachine(t *testing.T) {
	t.Parallel()

	synctest.Test(t, func(t *testing.T) {
		m := parentChart(t, downloadChart(t))

		inst, err := m.Instance(t.Context(), counter{N: 0})
		require.NoError(t, err)
		defer inst.Close()

		_, err = inst.Send(t.Context(), start{})
		require.NoError(t, err)
		synctest.Wait()

		step, err := inst.Send(t.Context(), pause{})
		require.NoError(t, err)
		require.Equal(t, []string{"idle"}, step.To.Active())

		// With no child running, an event for it is dropped.
		step, err = inst.Send(t.Context(), tick{})
		require.NoError(t, err)
		require.False(t, step.Accepted())
	})
}

func TestTheCoreReturnsSendsAndEmitsAsEffects(t *testing.T) {
	t.Parallel()

	child := downloadChart(t)
	snap, _ := child.Start(counter{N: 0}, epoch)
	_, effects := child.Transition(snap, tick{}, epoch)
	require.Equal(t, []sc.EffectKind{sc.EffectEmit}, kinds(effects))
	require.Equal(t, progress{Percent: 50}, effects[0].Event)

	parent := parentChart(t, child)
	snap, _ = parent.Start(counter{N: 0}, epoch)
	snap, effects = parent.Transition(snap, start{}, epoch)
	run := effect(t, effects, sc.EffectStartInvocation)

	_, effects = parent.Transition(snap, tick{}, epoch)
	require.Equal(t, []sc.EffectKind{sc.EffectSend}, kinds(effects))
	require.Equal(t, run.ID, effects[0].ID)
	require.Equal(t, tick{}, effects[0].Event)

	require.True(t, parent.Messaging())

	_, err := sc.New("nowhere", func(b *sc.Builder[string, counter]) {
		b.Initial("a")
		b.State("a").On[tick]().SendTo("send", "nobody", func(counter, tick) any { return tick{} })
	})
	require.ErrorContains(t, err, "not a service of the chart")
}

func TestAServiceReadsWhatTheChartSendsIt(t *testing.T) {
	t.Parallel()

	synctest.Test(t, func(t *testing.T) {
		m, err := sc.New("worker", func(b *sc.Builder[string, counter]) {
			b.Initial("working")
			working := b.State("working")
			inv := working.Invoke("work", func(ctx context.Context, _ counter) (int, error) {
				total := 0
				for ev := range sc.Inbox(ctx) {
					if _, ok := ev.(finish); ok {
						break
					}
					total++
				}

				return total, nil
			})
			inv.OnDone().Assign("keep", func(c counter, d sc.Done[int]) counter { c.N = d.Result; return c }).To("done")
			inv.OnError().To("done")
			working.On[tick]().SendTo("count", "work", func(_ counter, ev tick) any { return ev })
			working.On[finish]().SendTo("stop", "work", func(_ counter, ev finish) any { return ev })
			b.Final("done")
		})
		require.NoError(t, err)

		inst, err := m.Instance(t.Context(), counter{N: 0})
		require.NoError(t, err)
		defer inst.Close()

		for range 3 {
			_, err = inst.Send(t.Context(), tick{})
			require.NoError(t, err)
		}
		_, err = inst.Send(t.Context(), finish{Fail: false})
		require.NoError(t, err)

		snap, err := inst.Await(t.Context(), func(s sc.Snapshot[string, counter]) bool { return s.Done() })
		require.NoError(t, err)
		require.Equal(t, 3, snap.Context.N)
	})
}
