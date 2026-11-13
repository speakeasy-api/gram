package sc_test

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"sync/atomic"
	"testing"
	"testing/synctest"
	"time"

	"github.com/stretchr/testify/require"
	"golang.org/x/sync/semaphore"

	"github.com/speakeasy-api/gram/sc"
)

// originKey marks where a context came from, so a test can check which
// context a hook was given.
type originKey struct{}

func origin(ctx context.Context) string {
	o, _ := ctx.Value(originKey{}).(string)
	return o
}

var errWorkFailed = errors.New("work failed")

// recorder collects what the hooks were told.
type recorder struct {
	mu    sync.Mutex
	calls []string
}

func (r *recorder) add(format string, args ...any) {
	r.mu.Lock()
	defer r.mu.Unlock()

	r.calls = append(r.calls, fmt.Sprintf(format, args...))
}

func (r *recorder) list() []string {
	r.mu.Lock()
	defer r.mu.Unlock()

	return append([]string(nil), r.calls...)
}

func (r *recorder) hooks(m *sc.Machine[string, counter]) *sc.Hooks[string, counter] {
	return &sc.Hooks[string, counter]{
		Transition: func(ctx context.Context, step sc.Step[string, counter]) {
			r.add("transition %s from %s, to %v", m.EventName(step.Event), origin(ctx), step.To.Active())
		},
		Ignored: func(ctx context.Context, _ sc.Snapshot[string, counter], event any) {
			r.add("ignored %s from %s", m.EventName(event), origin(ctx))
		},
		EffectStarted: func(ctx context.Context, e sc.Effect[counter]) context.Context {
			r.add("started %s", e.Name)
			return context.WithValue(ctx, originKey{}, e.Name)
		},
		EffectFinished: func(ctx context.Context, e sc.Effect[counter], _ time.Duration, err error) {
			r.add("finished %s in %s: %v", e.Name, origin(ctx), err)
		},
	}
}

func workChart(t *testing.T, work func(context.Context, counter) (int, error)) *sc.Machine[string, counter] {
	t.Helper()

	m, err := sc.New("work", func(b *sc.Builder[string, counter]) {
		b.Initial("idle")
		b.State("idle").On[tick]().Do("announce", func(context.Context, counter, tick) {}).To("working")
		working := b.State("working")
		inv := working.Invoke("work", work)
		inv.OnDone().To("done")
		inv.OnError().To("done")
		working.On[leave]().To("done")
		b.Final("done")
	})
	require.NoError(t, err)

	return m
}

func TestActorReportsTransitionsEffectsAndIgnoredEventsToItsHooks(t *testing.T) {
	t.Parallel()

	synctest.Test(t, func(t *testing.T) {
		m := workChart(t, func(context.Context, counter) (int, error) { return 0, errWorkFailed })
		rec := &recorder{mu: sync.Mutex{}, calls: nil}
		a := m.Actor(counter{}, sc.WithHooks(rec.hooks(m)))
		wait := runActor(t.Context(), a)

		sender := context.WithValue(t.Context(), originKey{}, "sender")
		require.NoError(t, a.Send(sender, sc.TimerFired{ID: "no such timer"}))
		require.NoError(t, a.Send(sender, tick{}))
		require.NoError(t, wait())

		calls := rec.list()
		require.Len(t, calls, 7)
		require.Equal(t, []string{
			"ignored TimerFired from sender",
			"transition tick from sender, to [working]",
			"started announce",
			"finished announce in announce: <nil>",
			"started work",
		}, calls[:5])

		// The service's end and the transition its result causes are
		// reported from different goroutines, in either order. The failure
		// reaches the chart with the context the service ran with.
		require.ElementsMatch(t, []string{
			"finished work in work: work failed",
			"transition sc.failed from work, to [done]",
		}, calls[5:])
	})
}

func TestActorReportsAServiceStoppedByLeavingItsState(t *testing.T) {
	t.Parallel()

	synctest.Test(t, func(t *testing.T) {
		m := workChart(t, func(ctx context.Context, _ counter) (int, error) {
			<-ctx.Done()
			return 0, ctx.Err()
		})
		rec := &recorder{mu: sync.Mutex{}, calls: nil}
		a := m.Actor(counter{}, sc.WithHooks(rec.hooks(m)))
		wait := runActor(t.Context(), a)

		require.NoError(t, a.Send(t.Context(), tick{}))
		synctest.Wait()
		require.NoError(t, a.Send(t.Context(), leave{}))
		require.NoError(t, wait())
		synctest.Wait()

		require.Contains(t, rec.list(), "finished work in work: context canceled")
	})
}

func TestWithHooksForAnotherMachinePanics(t *testing.T) {
	t.Parallel()

	m := workChart(t, func(context.Context, counter) (int, error) { return 0, nil })

	require.PanicsWithValue(t, `sc: WithHooks was given *sc.Hooks[github.com/speakeasy-api/gram/sc_test.state,github.com/speakeasy-api/gram/sc_test.order], which is not for machine "work"`, func() {
		m.Actor(counter{}, sc.WithHooks(&sc.Hooks[state, order]{}))
	})
}

func TestLimitServicesBoundsServicesAcrossActors(t *testing.T) {
	t.Parallel()

	synctest.Test(t, func(t *testing.T) {
		release := make(chan struct{})
		var running, peak atomic.Int32

		m := workChart(t, func(ctx context.Context, _ counter) (int, error) {
			now := running.Add(1)
			defer running.Add(-1)
			if now > peak.Load() {
				peak.Store(now)
			}

			select {
			case <-release:
			case <-ctx.Done():
			}

			return 1, nil
		})

		sem := semaphore.NewWeighted(1)
		first := m.Actor(counter{}, sc.LimitServices(sem))
		second := m.Actor(counter{}, sc.LimitServices(sem))
		waitFirst := runActor(t.Context(), first)
		waitSecond := runActor(t.Context(), second)

		require.NoError(t, first.Send(t.Context(), tick{}))
		require.NoError(t, second.Send(t.Context(), tick{}))
		synctest.Wait()
		require.Equal(t, int32(1), running.Load())

		close(release)
		require.NoError(t, waitFirst())
		require.NoError(t, waitSecond())
		require.Equal(t, int32(1), peak.Load())
	})
}

func TestActorFiresADeadlineAtItsTime(t *testing.T) {
	t.Parallel()

	synctest.Test(t, func(t *testing.T) {
		// retryIn is when the deadline falls, measured on the bubble's
		// fake clock.
		const retryIn = 10 * time.Minute

		m := deadlineChart(t)
		a := m.Actor(attempts{Failures: 0, RetryAt: time.Now().Add(retryIn)})
		wait := runActor(t.Context(), a)

		time.Sleep(retryIn - time.Second) //nolint:forbidigo // GG013: advances the synctest fake clock instantly (the only way to move past a timer inside a synctest.Test bubble); this exemption is valid ONLY within synctest.Test — do not copy it to a real-time time.Sleep
		synctest.Wait()
		require.False(t, a.Snapshot().Done())

		time.Sleep(2 * time.Second) //nolint:forbidigo // GG013: advances the synctest fake clock instantly (the only way to move past a timer inside a synctest.Test bubble); this exemption is valid ONLY within synctest.Test — do not copy it to a real-time time.Sleep
		require.NoError(t, wait())
		require.True(t, a.Snapshot().Done())
	})
}

func TestActorTimersReportWhenEachTimerFires(t *testing.T) {
	t.Parallel()

	synctest.Test(t, func(t *testing.T) {
		m := backoffChart(t)
		a := m.Actor(attempts{})
		wait := runActor(t.Context(), a)

		synctest.Wait()
		timers := a.Timers()
		require.Len(t, timers, 1)
		require.Equal(t, "backoff", timers[0].Name)
		require.Equal(t, time.Now().Add(time.Second), timers[0].Due)

		time.Sleep(2 * time.Second) //nolint:forbidigo // GG013: advances the synctest fake clock instantly (the only way to move past a timer inside a synctest.Test bubble); this exemption is valid ONLY within synctest.Test — do not copy it to a real-time time.Sleep
		require.NoError(t, wait())
		require.Empty(t, a.Timers())
	})
}

func TestJoinHooksGivesEachSetItsOwnEffectContext(t *testing.T) {
	t.Parallel()

	synctest.Test(t, func(t *testing.T) {
		m := workChart(t, func(context.Context, counter) (int, error) { return 1, nil })
		first := &recorder{mu: sync.Mutex{}, calls: nil}
		second := &recorder{mu: sync.Mutex{}, calls: nil}

		a := m.Actor(counter{}, sc.WithHooks(sc.JoinHooks(first.hooks(m), nil, second.hooks(m))))
		wait := runActor(t.Context(), a)
		require.NoError(t, a.Send(t.Context(), tick{}))
		require.NoError(t, wait())

		// Each set's EffectFinished sees the context its own EffectStarted
		// returned; both recorders tag it with the effect's name.
		// The service's end and the transition its result causes come from
		// different goroutines, so the two sets may record them in
		// different orders.
		require.ElementsMatch(t, first.list(), second.list())
		require.Contains(t, first.list(), "finished work in work: <nil>")
	})
}

func TestStepServiceNamesTheRunAResultCameFrom(t *testing.T) {
	t.Parallel()

	m := orderChart(t, chargeOK)
	snap, chargeID := submitted(t, m)
	next, effects := m.Transition(snap, sc.Done[string]{ID: chargeID, Result: "rcpt-1"}, epoch)

	step := sc.Step[state, order]{From: snap, Event: sc.Done[string]{ID: chargeID, Result: "rcpt-1"}, To: next, Effects: effects}
	run, ok := step.Service()
	require.True(t, ok)
	require.Equal(t, sc.RunningService[state]{ID: chargeID, State: charging, Name: "charge"}, run)

	_, ok = sc.Step[state, order]{From: snap, Event: hold{}, To: next, Effects: nil}.Service()
	require.False(t, ok)
}

func TestAPanickingServiceFailsInsteadOfCrashing(t *testing.T) {
	t.Parallel()

	synctest.Test(t, func(t *testing.T) {
		m := workChart(t, func(context.Context, counter) (int, error) { panic("boom") })

		inst, err := m.Instance(t.Context(), counter{})
		require.NoError(t, err)
		defer inst.Close()

		_, err = inst.Send(t.Context(), tick{})
		require.NoError(t, err)

		done, err := inst.Await(t.Context(), func(s sc.Snapshot[string, counter]) bool { return s.Done() })
		require.NoError(t, err)
		require.True(t, done.Done())
	})
}

func TestExecReturnsAPanicAsAnError(t *testing.T) {
	t.Parallel()

	m := workChart(t, func(context.Context, counter) (int, error) { panic("boom") })
	snap, _ := m.Start(counter{}, epoch)
	_, effects := m.Transition(snap, tick{}, epoch)

	err := m.Exec(t.Context(), effect(t, effects, sc.EffectStartInvocation), func(any) {})
	require.ErrorIs(t, err, sc.ErrPanicked)
	require.ErrorContains(t, err, "boom")
}
