package sc_test

import (
	"context"
	"encoding/json"
	"testing"
	"testing/synctest"

	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/sc"
)

func TestAParallelStateReportsWhatItsRegionsDo(t *testing.T) {
	t.Parallel()

	m, err := sc.New("checks", func(b *sc.Builder[string, counter]) {
		b.Initial("checking")
		checking := b.Parallel("checking", func(b *sc.Builder[string, counter]) {
			b.State("credit", func(b *sc.Builder[string, counter]) {
				b.Initial("scoring")
				b.State("scoring").On[tick]().To("scored")
				b.Final("scored").Output(func(c counter) int { return 700 + c.N })
			})
			b.State("identity", func(b *sc.Builder[string, counter]) {
				b.Initial("verifying")
				b.State("verifying").On[ping]().To("verified")
				b.Final("verified").Output(func(counter) string { return "passport" })
			})
			b.Final("consent")
		})
		checking.OnDoneWith[map[string]any]().
			When("goodScore", func(_ counter, d sc.Done[map[string]any]) bool {
				score, _ := d.Result["credit"].(int)
				return score >= 700
			}).
			To("approved")
		checking.OnDone().To("declined")
		b.Final("approved")
		b.Final("declined")
	})
	require.NoError(t, err)

	snap, _ := m.Start(counter{N: 5}, epoch)
	snap, _ = m.Transition(snap, tick{}, epoch)
	snap, _ = m.Transition(snap, ping{}, epoch)

	// Each region's output arrived under its name; the region without one
	// reports nil.
	require.Equal(t, []string{"approved"}, snap.Active())
}

type charge struct {
	Amount int `json:"amount"`
}

func TestAServiceReadsTheEventItsStateWasEnteredWith(t *testing.T) {
	t.Parallel()

	chart := func(t *testing.T, work func(context.Context, counter) (int, error)) *sc.Machine[string, counter] {
		t.Helper()

		m, err := sc.New("charging", func(b *sc.Builder[string, counter]) {
			b.Initial("idle")
			b.State("idle").On[charge]().To("charging")
			inv := b.State("charging").Invoke("charge", work).Retry(sc.Retry{Max: 1, First: 0, Cap: 0, Jitter: 0, If: nil})
			inv.OnDone().Assign("keep", func(c counter, d sc.Done[int]) counter { c.N = d.Result; return c }).To("done")
			inv.OnError().To("done")
			b.Final("done")
		})
		require.NoError(t, err)

		return m
	}

	// The service acts on the amount without the chart copying it into
	// the context.
	synctest.Test(t, func(t *testing.T) {
		m := chart(t, func(ctx context.Context, _ counter) (int, error) {
			ev, _ := sc.Trigger(ctx).(charge)
			return ev.Amount, nil
		})

		inst, err := m.Instance(t.Context(), counter{N: 0})
		require.NoError(t, err)
		defer inst.Close()

		_, err = inst.SendAndSettle(t.Context(), charge{Amount: 42})
		require.NoError(t, err)
		require.Equal(t, 42, inst.Snapshot().Context.N)
	})

	// The event stays with the run across a restart and a retry.
	m := chart(t, nil)
	snap, _ := m.Start(counter{N: 0}, epoch)
	snap, effects := m.Transition(snap, charge{Amount: 7}, epoch)
	run := effect(t, effects, sc.EffectStartInvocation)
	require.Equal(t, charge{Amount: 7}, run.Event)

	data, err := json.Marshal(snap)
	require.NoError(t, err)
	snap, _, err = m.Restore(data, epoch)
	require.NoError(t, err)
	require.Equal(t, charge{Amount: 7}, effect(t, m.Resume(snap), sc.EffectStartInvocation).Event)

	_, effects = m.Transition(snap, sc.Failed{ID: run.ID, Err: errFlaky}, epoch)
	require.Equal(t, charge{Amount: 7}, effect(t, effects, sc.EffectStartInvocation).Event)
}

func TestAParentDecidesOnWhereItsChildIs(t *testing.T) {
	t.Parallel()

	synctest.Test(t, func(t *testing.T) {
		child, err := sc.New("door", func(b *sc.Builder[string, counter]) {
			b.Initial("closed")
			b.State("closed").On[tick]().To("open")
			b.State("open")
		})
		require.NoError(t, err)

		m, err := sc.New("house", func(b *sc.Builder[string, counter]) {
			b.Initial("home")
			home := b.State("home")
			door := home.InvokeMachine("door", child,
				func(counter) counter { return counter{N: 0} },
				func(sc.Snapshot[string, counter]) (int, error) { return 0, nil },
			)
			door.OnError().Stay()
			home.On[tick]().SendTo("push", "door", func(_ counter, ev tick) any { return ev })
			home.On[leave]().WhenChildIn("door", "open").To("away")
			b.State("away")
		})
		require.NoError(t, err)

		inst, err := m.Instance(t.Context(), counter{N: 0})
		require.NoError(t, err)
		defer inst.Close()
		synctest.Wait()

		// The door is closed, so leaving is refused.
		step, err := inst.Send(t.Context(), leave{})
		require.NoError(t, err)
		require.False(t, step.Accepted())

		door, ok := inst.Snapshot().Child(child, "door")
		require.True(t, ok)
		require.Equal(t, []string{"closed"}, door.Active())

		// Once the child has moved, the parent sees it.
		_, err = inst.Send(t.Context(), tick{})
		require.NoError(t, err)
		synctest.Wait()

		door, ok = inst.Snapshot().Child(child, "door")
		require.True(t, ok)
		require.Equal(t, []string{"open"}, door.Active())

		step, err = inst.Send(t.Context(), leave{})
		require.NoError(t, err)
		require.Equal(t, []string{"away"}, step.To.Active())
	})

	_, err := sc.New("nobody", func(b *sc.Builder[string, counter]) {
		b.Initial("a")
		b.State("a").On[tick]().WhenChildIn("ghost", "open").To("b")
		b.State("b")
	})
	require.ErrorContains(t, err, "not a service of the chart")
}

func TestAServiceHearsWhatItIsToldAsItIsStopped(t *testing.T) {
	t.Parallel()

	synctest.Test(t, func(t *testing.T) {
		var last []any

		m, err := sc.New("worker", func(b *sc.Builder[string, counter]) {
			b.Initial("working")
			working := b.State("working").SendOnExit("flush", "work", func(counter) any { return ping{} })
			inv := working.Invoke("work", func(ctx context.Context, _ counter) (int, error) {
				// The context ends when the state is left; what was sent
				// by that step is still handed over.
				for ev := range sc.Inbox(ctx) {
					last = append(last, ev)
				}

				return 0, context.Cause(ctx)
			})
			inv.OnDone().Stay()
			inv.OnError().Stay()
			working.On[leave]().
				SendTo("bye", "work", func(_ counter, ev leave) any { return ev }).
				To("stopped")
			b.State("stopped")
		})
		require.NoError(t, err)

		inst, err := m.Instance(t.Context(), counter{N: 0})
		require.NoError(t, err)
		defer inst.Close()
		synctest.Wait()

		step, err := inst.Send(t.Context(), leave{})
		require.NoError(t, err)
		require.Equal(t, []sc.EffectKind{sc.EffectSend, sc.EffectSend, sc.EffectStopInvocation}, kinds(step.Effects))

		// Send returns once the stopped service has returned, by which
		// time it has heard both events.
		require.ElementsMatch(t, []any{ping{}, leave{}}, last)
	})
}
