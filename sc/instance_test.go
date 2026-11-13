package sc_test

import (
	"context"
	"iter"
	"sync"
	"sync/atomic"
	"testing"
	"testing/synctest"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/sc"
)

func TestInstanceSendRunsTheActionsBeforeReturningAndSaysWhetherItWasAccepted(t *testing.T) {
	t.Parallel()

	synctest.Test(t, func(t *testing.T) {
		var announced atomic.Bool
		m, err := sc.New("instant", func(b *sc.Builder[string, counter]) {
			b.Initial("idle")
			b.State("idle").On[tick]().Do("announce", func(context.Context, counter, tick) { announced.Store(true) }).To("done")
			b.Final("done")
		})
		require.NoError(t, err)

		inst, err := m.Instance(t.Context(), counter{})
		require.NoError(t, err)
		defer inst.Close()

		ignored, err := inst.Send(t.Context(), pong{})
		require.NoError(t, err)
		require.Equal(t, ignored.From.Seq(), ignored.To.Seq())

		accepted, err := inst.Send(t.Context(), tick{})
		require.NoError(t, err)
		require.NotEqual(t, accepted.From.Seq(), accepted.To.Seq())
		require.True(t, announced.Load())
		require.True(t, inst.Snapshot().Done())
	})
}

func TestInstanceRunsServicesInTheBackgroundAndAwaitSeesTheResult(t *testing.T) {
	t.Parallel()

	synctest.Test(t, func(t *testing.T) {
		m := workChart(t, func(context.Context, counter) (int, error) { return 1, nil })

		inst, err := m.Instance(t.Context(), counter{})
		require.NoError(t, err)
		defer inst.Close()

		step, err := inst.Send(t.Context(), tick{})
		require.NoError(t, err)
		require.Equal(t, []string{"working"}, step.To.Active())

		done, err := inst.Await(t.Context(), func(s sc.Snapshot[string, counter]) bool { return s.Done() })
		require.NoError(t, err)
		require.Equal(t, []string{"done"}, done.Active())
	})
}

func TestInstanceStopsAServiceBeforeTheNextEffectsRun(t *testing.T) {
	t.Parallel()

	synctest.Test(t, func(t *testing.T) {
		var exited atomic.Bool
		var exitedAtEntry atomic.Bool

		m, err := sc.New("ordered stop", func(b *sc.Builder[string, counter]) {
			b.Initial("working")
			working := b.State("working")
			working.Invoke("work", func(ctx context.Context, _ counter) (int, error) {
				defer exited.Store(true)
				<-ctx.Done()

				return 0, ctx.Err()
			}).OnError().Stay()
			working.On[leave]().To("next")
			b.State("next").Entry("check", func(context.Context, counter) { exitedAtEntry.Store(exited.Load()) })
		})
		require.NoError(t, err)

		inst, err := m.Instance(t.Context(), counter{})
		require.NoError(t, err)
		defer inst.Close()

		_, err = inst.Send(t.Context(), leave{})
		require.NoError(t, err)

		require.True(t, exitedAtEntry.Load())
	})
}

func TestInstanceStreamCanLeaveItsOwnState(t *testing.T) {
	t.Parallel()

	synctest.Test(t, func(t *testing.T) {
		m, err := sc.New("self stop", func(b *sc.Builder[string, counter]) {
			b.Initial("listening")
			listening := b.State("listening")
			listening.Stream("events", func(ctx context.Context, _ counter) iter.Seq2[any, error] {
				return func(yield func(any, error) bool) {
					if !yield(leave{}, nil) {
						return
					}
					<-ctx.Done()
				}
			}).OnError().Stay()
			listening.On[leave]().To("stopped")
			b.Final("stopped")
		})
		require.NoError(t, err)

		inst, err := m.Instance(t.Context(), counter{})
		require.NoError(t, err)
		defer inst.Close()

		_, err = inst.Await(t.Context(), func(s sc.Snapshot[string, counter]) bool { return s.Done() })
		require.NoError(t, err)
	})
}

func TestInstanceTimersWakeItOnTheirOwn(t *testing.T) {
	t.Parallel()

	synctest.Test(t, func(t *testing.T) {
		m := backoffChart(t)

		inst, err := m.Instance(t.Context(), attempts{})
		require.NoError(t, err)
		defer inst.Close()

		time.Sleep(2 * time.Second) //nolint:forbidigo // GG013: advances the synctest fake clock instantly (the only way to move past a timer inside a synctest.Test bubble); this exemption is valid ONLY within synctest.Test — do not copy it to a real-time time.Sleep
		synctest.Wait()

		require.True(t, inst.Snapshot().Done())
	})
}

// fakeClock is a clock a test moves by hand.
type fakeClock struct {
	mu  sync.Mutex
	now time.Time
}

func (c *fakeClock) Now() time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()

	return c.now
}

func (c *fakeClock) Add(d time.Duration) {
	c.mu.Lock()
	defer c.mu.Unlock()

	c.now = c.now.Add(d)
}

func TestInstanceWithManualTimersFiresThemWhenAdvanced(t *testing.T) {
	t.Parallel()

	clock := &fakeClock{mu: sync.Mutex{}, now: epoch}
	m := backoffChart(t)

	inst, err := m.Instance(t.Context(), attempts{}, sc.WithClock(clock.Now), sc.ManualTimers())
	require.NoError(t, err)
	defer inst.Close()

	step, err := inst.Advance(t.Context())
	require.NoError(t, err)
	require.Equal(t, step.From.Seq(), step.To.Seq())

	clock.Add(time.Second)
	_, err = inst.Advance(t.Context())
	require.NoError(t, err)

	require.True(t, inst.Snapshot().Done())
}

func TestClosedInstanceRefusesEventsAndStopsItsServices(t *testing.T) {
	t.Parallel()

	synctest.Test(t, func(t *testing.T) {
		var stopped atomic.Bool
		m := workChart(t, func(ctx context.Context, _ counter) (int, error) {
			<-ctx.Done()
			stopped.Store(true)

			return 0, ctx.Err()
		})

		inst, err := m.Instance(t.Context(), counter{})
		require.NoError(t, err)
		_, err = inst.Send(t.Context(), tick{})
		require.NoError(t, err)

		inst.Close()

		require.True(t, stopped.Load())
		_, err = inst.Send(t.Context(), leave{})
		require.ErrorIs(t, err, sc.ErrInstanceClosed)
	})
}

func TestActorCallWaitsForTheStepAndAwaitForAState(t *testing.T) {
	t.Parallel()

	synctest.Test(t, func(t *testing.T) {
		m := workChart(t, func(context.Context, counter) (int, error) { return 1, nil })
		a := m.Actor(counter{})
		wait := runActor(t.Context(), a)

		ignored, err := a.Call(t.Context(), pong{})
		require.NoError(t, err)
		require.Equal(t, ignored.From.Seq(), ignored.To.Seq())

		step, err := a.Call(t.Context(), tick{})
		require.NoError(t, err)
		require.Equal(t, []string{"working"}, step.To.Active())

		done, err := a.Await(t.Context(), func(s sc.Snapshot[string, counter]) bool { return s.Done() })
		require.NoError(t, err)
		require.True(t, done.Done())
		require.NoError(t, wait())
	})
}
