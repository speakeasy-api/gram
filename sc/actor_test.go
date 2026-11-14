package sc_test

import (
	"context"
	"encoding/json"
	"iter"
	"sync/atomic"
	"testing"
	"testing/synctest"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/sc"
)

// runActor runs the actor on its own goroutine and returns a function that
// waits for Run to return.
func runActor[S comparable, C any](ctx context.Context, a *sc.Actor[S, C]) (wait func() error) {
	result := make(chan error, 1)
	go func() { result <- a.Run(ctx) }()

	return func() error { return <-result }
}

func TestActorRunsServicesAndTimersUntilTheMachineIsDone(t *testing.T) {
	t.Parallel()

	synctest.Test(t, func(t *testing.T) {
		var charges atomic.Int32
		m := orderChart(t, func(context.Context, *sc.Run, order) (string, error) {
			if charges.Add(1) == 1 {
				return "", errCardDeclined
			}

			return "rcpt-2", nil
		})

		a := m.Actor(order{})
		wait := runActor(t.Context(), a)

		require.NoError(t, a.Send(t.Context(), submit{Items: []string{"book"}}))

		// The first charge fails, so the actor waits out the retry
		// delay on the fake clock and charges again.
		synctest.Wait()
		require.Equal(t, []state{paying, retrying}, a.Snapshot().Active())

		time.Sleep(retryDelay) //nolint:forbidigo // GG013: advances the synctest fake clock instantly (the only way to move past a timer inside a synctest.Test bubble); this exemption is valid ONLY within synctest.Test — do not copy it to a real-time time.Sleep
		synctest.Wait()
		require.Equal(t, "rcpt-2", a.Snapshot().Context.Receipt)
		require.True(t, a.Snapshot().In(fulfilling))

		require.NoError(t, a.Send(t.Context(), itemsPicked{}))
		require.NoError(t, a.Send(t.Context(), invoiceSent{}))

		require.NoError(t, wait())
		require.True(t, a.Snapshot().Done())
		require.Equal(t, []state{shipped}, a.Snapshot().Active())
		require.ErrorIs(t, a.Send(t.Context(), cancel{}), sc.ErrActorStopped)
	})
}

func TestActorSnapshotsYieldsUntilTheMachineIsDone(t *testing.T) {
	t.Parallel()

	synctest.Test(t, func(t *testing.T) {
		// The charge never finishes, so the order stays in paying,
		// where cancel is handled.
		m := orderChart(t, func(ctx context.Context, _ *sc.Run, _ order) (string, error) {
			<-ctx.Done()
			return "", ctx.Err()
		})
		a := m.Actor(order{})
		wait := runActor(t.Context(), a)

		var last sc.Snapshot[state, order]
		watched := make(chan struct{})
		go func() {
			defer close(watched)
			for snap := range a.Snapshots(t.Context()) {
				last = snap
			}
		}()

		require.NoError(t, a.Send(t.Context(), submit{Items: []string{"book"}}))
		synctest.Wait()
		require.NoError(t, a.Send(t.Context(), cancel{}))

		require.NoError(t, wait())
		<-watched
		require.True(t, last.Done())
		require.Equal(t, []state{cancelled}, last.Active())
	})
}

func TestActorStopsStreamServiceWhenItsStateIsExited(t *testing.T) {
	t.Parallel()

	synctest.Test(t, func(t *testing.T) {
		cleanedUp := make(chan struct{})

		m, err := sc.New("ticker", func(b *sc.Builder[string, counter]) {
			b.Initial("listening")
			listening := b.State("listening")
			listening.Stream("ticks", func(ctx context.Context, _ *sc.Run, _ counter) iter.Seq2[any, error] {
				return func(yield func(any, error) bool) {
					defer close(cleanedUp)

					for {
						select {
						case <-ctx.Done():
							return
						case <-time.After(time.Second):
						}

						if !yield(tick{}, nil) {
							return
						}
					}
				}
			}).OnError().To("stopped")
			listening.On[tick]().Assign("increment", increment[tick])
			listening.Always().
				When("reachedThree", func(c counter, _ struct{}) bool { return c.N >= 3 }).
				To("stopped")
			b.Final("stopped")
		})
		require.NoError(t, err)

		a := m.Actor(counter{})

		require.NoError(t, a.Run(t.Context()))
		<-cleanedUp
		require.Equal(t, 3, a.Snapshot().Context.N)
	})
}

func TestActorReturnsWhenItsContextIsCancelled(t *testing.T) {
	t.Parallel()

	synctest.Test(t, func(t *testing.T) {
		blocked := make(chan struct{})
		m := orderChart(t, func(ctx context.Context, _ *sc.Run, _ order) (string, error) {
			close(blocked)
			<-ctx.Done()

			return "", ctx.Err()
		})

		ctx, cancelRun := context.WithCancel(t.Context())
		a := m.Actor(order{})
		wait := runActor(ctx, a)

		require.NoError(t, a.Send(t.Context(), submit{Items: []string{"book"}}))
		<-blocked
		cancelRun()

		require.ErrorIs(t, wait(), context.Canceled)
		require.ErrorIs(t, a.Run(t.Context()), sc.ErrActorStarted)
	})
}

func TestActorFromContinuesAPersistedInstance(t *testing.T) {
	t.Parallel()

	synctest.Test(t, func(t *testing.T) {
		m := orderChart(t, chargeOK)

		// Take the snapshot while the charge is outstanding, as a
		// process that stopped mid-payment would have stored it.
		snap, _ := submitted(t, m)
		data, err := json.Marshal(snap)
		require.NoError(t, err)

		restored, _, err := m.Restore(data, epoch)
		require.NoError(t, err)

		a := m.ActorFrom(restored)
		wait := runActor(t.Context(), a)

		synctest.Wait()
		require.Equal(t, "rcpt-1", a.Snapshot().Context.Receipt)

		require.NoError(t, a.Send(t.Context(), itemsPicked{}))
		require.NoError(t, a.Send(t.Context(), invoiceSent{}))
		require.NoError(t, wait())
	})
}
