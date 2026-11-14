package sc_test

import (
	"context"
	"encoding/json"
	"fmt"
	"testing"
	"testing/synctest"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/sc"
	"github.com/speakeasy-api/gram/sc/pgdriver"
)

// typedCharge reads the event its state was entered with.
func typedCharge(context.Context, *sc.Run, counter, charge) (int, error) { return 0, nil }

func TestTypedInvokeOnAcceptsAStateEnteredOnlyByItsEvent(t *testing.T) {
	t.Parallel()

	_, err := sc.New("typed", func(b *sc.Builder[string, counter]) {
		b.Initial("idle")
		b.State("idle").On[charge]().To("charging")
		b.State("retrying").On[charge]().To("charging")
		b.State("idle").On[tick]().To("retrying")
		inv := b.State("charging").InvokeOn[charge]("charge", typedCharge)
		inv.OnDone().To("done")
		inv.OnError().To("retrying")
		b.Final("done")
	})
	require.NoError(t, err)
}

func TestTypedInvokeOnRejectsAStateEnteredAnyOtherWay(t *testing.T) {
	t.Parallel()

	for name, tc := range map[string]struct {
		def  func(b *sc.Builder[string, counter])
		want string
	}{
		"another event type": {
			def: func(b *sc.Builder[string, counter]) {
				b.Initial("idle")
				b.State("idle").On[charge]().To("charging")
				b.State("idle").On[tick]().To("charging")
			},
			want: "entered by sc_test.tick, from idle",
		},
		"as the initial state": {
			def: func(b *sc.Builder[string, counter]) {
				b.Initial("charging")
			},
			want: "entered as the initial state of typed",
		},
		"as its parent's initial state": {
			def: func(b *sc.Builder[string, counter]) {
				b.Initial("idle")
				b.State("idle").On[charge]().To("outer")
				b.State("outer", func(b *sc.Builder[string, counter]) {
					b.Initial("charging")
					inv := b.State("charging").InvokeOn[charge]("charge", typedCharge)
					inv.OnDone().To("done")
					inv.OnError().To("done")
				})
			},
			want: "as the initial state of outer",
		},
		"by a timer": {
			def: func(b *sc.Builder[string, counter]) {
				b.Initial("idle")
				b.State("idle").On[charge]().To("charging")
				b.State("idle").After(time.Minute).To("charging")
			},
			want: "by a timer, from idle",
		},
		"without an event": {
			def: func(b *sc.Builder[string, counter]) {
				b.Initial("idle")
				b.State("idle").On[charge]().To("waiting")
				b.State("waiting").Always().To("charging")
			},
			want: "without an event, from waiting",
		},
		"through a history state": {
			def: func(b *sc.Builder[string, counter]) {
				b.Initial("idle")
				b.State("idle").On[charge]().To("charging")
				b.State("charging").On[leave]().To("away")
				b.State("away").On[back]().To("hist")
				b.History("hist", sc.Deep)
			},
			want: "by sc_test.back, from away",
		},
		"by any event": {
			def: func(b *sc.Builder[string, counter]) {
				b.Initial("idle")
				b.State("idle").OnAny().To("charging")
			},
			want: "by any event, from idle",
		},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			_, err := sc.New("typed", func(b *sc.Builder[string, counter]) {
				tc.def(b)
				if name != "as its parent's initial state" {
					inv := b.State("charging").InvokeOn[charge]("charge", typedCharge)
					inv.OnDone().To("done")
					inv.OnError().To("done")
				}
				b.Final("done")
			})
			require.ErrorContains(t, err, `service "charge`)
			require.ErrorContains(t, err, "started with sc_test.charge")
			require.ErrorContains(t, err, tc.want)
		})
	}

	// The chart itself is entered as the instance starts.
	_, err := sc.New("typed", func(b *sc.Builder[string, counter]) {
		b.Initial("idle")
		b.State("idle")
		inv := b.Chart().InvokeOn[charge]("charge", typedCharge)
		inv.OnDone().Stay()
		inv.OnError().Stay()
	})
	require.ErrorContains(t, err, "entered as the instance starts")
}

// emitsGreeting emits a greeting on every tick.
func emitsGreeting(t *testing.T) *sc.Machine[string, counter] {
	t.Helper()

	m, err := sc.New("greeter", func(b *sc.Builder[string, counter]) {
		b.Initial("idle")
		b.State("idle").On[tick]().Emit("greet", func(counter, tick) greeting { return greeting{Text: "hi"} })
	})
	require.NoError(t, err)

	return m
}

func TestTypedListenForANeverEmittedTypeFailsTheDriver(t *testing.T) {
	t.Parallel()

	m := emitsGreeting(t)
	never := sc.Listen(func(context.Context, ping) {})

	t.Run("instance", func(t *testing.T) {
		t.Parallel()

		_, err := m.Instance(t.Context(), counter{N: 0}, never)
		require.ErrorIs(t, err, sc.ErrNoSuchEmit)
		require.ErrorContains(t, err, "sc_test.ping")

		// A pointer to the emitted type is another type.
		_, err = m.Instance(t.Context(), counter{N: 0}, sc.Listen(func(context.Context, *greeting) {}))
		require.ErrorIs(t, err, sc.ErrNoSuchEmit)

		inst, err := m.Instance(t.Context(), counter{N: 0}, sc.Listen(func(context.Context, greeting) {}))
		require.NoError(t, err)
		inst.Close()
	})

	t.Run("actor", func(t *testing.T) {
		t.Parallel()

		synctest.Test(t, func(t *testing.T) {
			err := m.Actor(counter{N: 0}, never).Run(t.Context())
			require.ErrorIs(t, err, sc.ErrNoSuchEmit)
			require.ErrorContains(t, err, "sc_test.ping")
		})
	})

	t.Run("pgdriver", func(t *testing.T) {
		t.Parallel()

		// The listeners are checked before the pool is touched.
		_, err := pgdriver.New(nil, m, pgdriver.Listen(func(context.Context, string, ping) error { return nil }))
		require.ErrorIs(t, err, sc.ErrNoSuchEmit)
		require.ErrorContains(t, err, "sc_test.ping")

		_, err = pgdriver.New(nil, m, pgdriver.Listen(func(context.Context, string, greeting) error { return nil }))
		require.NoError(t, err)
	})
}

func TestTypedListenHearsOnlyItsType(t *testing.T) {
	t.Parallel()

	synctest.Test(t, func(t *testing.T) {
		m, err := sc.New("chatty", func(b *sc.Builder[string, counter]) {
			b.Initial("idle")
			b.State("idle").On[tick]().
				Emit("greet", func(counter, tick) greeting { return greeting{Text: "hi"} }).
				Emit("count", func(c counter, _ tick) progress { return progress{Percent: c.N} })
		})
		require.NoError(t, err)

		var greetings []greeting
		var progresses []progress
		inst, err := m.Instance(t.Context(), counter{N: 3},
			sc.Listen(func(_ context.Context, ev greeting) { greetings = append(greetings, ev) }),
			sc.Listen(func(_ context.Context, ev progress) { progresses = append(progresses, ev) }),
		)
		require.NoError(t, err)
		defer inst.Close()

		_, err = inst.Send(t.Context(), tick{})
		require.NoError(t, err)
		require.Equal(t, []greeting{{Text: "hi"}}, greetings)
		require.Equal(t, []progress{{Percent: 3}}, progresses)
	})
}

func TestTypedOnDoneWithOfAnotherOutputTypeIsADeclarationError(t *testing.T) {
	t.Parallel()

	compound := func(t *testing.T, done func(s *sc.StateBuilder[string, counter])) error {
		t.Helper()

		_, err := sc.New("outputs", func(b *sc.Builder[string, counter]) {
			b.Initial("working")
			working := b.State("working", func(b *sc.Builder[string, counter]) {
				b.Initial("busy")
				b.State("busy").On[tick]().To("finished")
				b.State("busy").On[leave]().To("quiet")
				b.Final("finished").Output(func(c counter) int { return c.N })
				b.Final("quiet")
			})
			done(working)
			b.Final("done")
		})
		if err != nil {
			return fmt.Errorf("declare the chart: %w", err)
		}

		return nil
	}

	err := compound(t, func(s *sc.StateBuilder[string, counter]) { s.OnDoneWith[string]().To("done") })
	require.ErrorContains(t, err, `OnDoneWith[string], but final state "finished" reports int`)

	// The right type, and a final state with no output, are fine.
	require.NoError(t, compound(t, func(s *sc.StateBuilder[string, counter]) { s.OnDoneWith[int]().To("done") }))

	_, err = sc.New("regions", func(b *sc.Builder[string, counter]) {
		b.Initial("both")
		b.Parallel("both", func(b *sc.Builder[string, counter]) {
			b.State("left", func(b *sc.Builder[string, counter]) {
				b.Initial("l")
				b.State("l").On[tick]().To("lDone")
				b.Final("lDone").Output(func(counter) int { return 1 })
			})
		}).OnDoneWith[int]().To("done")
		b.Final("done")
	})
	require.ErrorContains(t, err, "on a parallel state, which reports map[string]any")
}

func TestTypedOnDoneWithGivesTheZeroValueForAFinalStateWithoutOutput(t *testing.T) {
	t.Parallel()

	m, err := sc.New("zero", func(b *sc.Builder[string, counter]) {
		b.Initial("working")
		b.State("working", func(b *sc.Builder[string, counter]) {
			b.Initial("busy")
			b.State("busy").On[tick]().To("finished")
			b.State("busy").On[leave]().To("quiet")
			b.Final("finished").Output(func(counter) int { return 7 })
			b.Final("quiet")
		}).OnDoneWith[int]().
			Assign("keep", func(c counter, d sc.Done[int]) counter { c.N = d.Result + 100; return c }).
			To("done")
		b.Final("done")
	})
	require.NoError(t, err)

	snap, _ := m.Start(counter{N: 0}, epoch)
	snap, _ = m.Transition(snap, leave{}, epoch)
	require.True(t, snap.Done())
	require.Equal(t, 100, snap.Context.N)

	snap, _ = m.Start(counter{N: 0}, epoch)
	snap, _ = m.Transition(snap, tick{}, epoch)
	require.Equal(t, 107, snap.Context.N)
}

func TestTypedSendRefusesAPointerWhereTheChartNamesTheValue(t *testing.T) {
	t.Parallel()

	m := emitsGreeting(t)

	checkPointer := func(t *testing.T, err error) {
		t.Helper()

		require.ErrorIs(t, err, sc.ErrUnknownEvent)
		require.ErrorContains(t, err, "*sc_test.tick")
		require.ErrorContains(t, err, "the chart handles sc_test.tick, a value, not a pointer")
	}

	t.Run("instance", func(t *testing.T) {
		t.Parallel()

		synctest.Test(t, func(t *testing.T) {
			inst, err := m.Instance(t.Context(), counter{N: 0})
			require.NoError(t, err)
			defer inst.Close()

			before := inst.Snapshot()
			_, err = inst.Send(t.Context(), &tick{})
			checkPointer(t, err)
			require.Equal(t, before.Seq(), inst.Snapshot().Seq())

			// A type the chart never names at all is refused too.
			_, err = inst.Send(t.Context(), pong{})
			require.ErrorIs(t, err, sc.ErrUnknownEvent)
			require.ErrorContains(t, err, "sc_test.pong")

			step, err := inst.Send(t.Context(), tick{})
			require.NoError(t, err)
			require.True(t, step.Accepted())
		})
	})

	t.Run("actor", func(t *testing.T) {
		t.Parallel()

		synctest.Test(t, func(t *testing.T) {
			a := m.Actor(counter{N: 0})
			ctx, cancel := context.WithCancel(t.Context())
			wait := runActor(ctx, a)

			checkPointer(t, a.Send(t.Context(), &tick{}))

			_, err := a.Call(t.Context(), &tick{})
			checkPointer(t, err)

			step, err := a.Call(t.Context(), tick{})
			require.NoError(t, err)
			require.True(t, step.Accepted())

			cancel()
			require.ErrorIs(t, wait(), context.Canceled)
		})
	})

	t.Run("group", func(t *testing.T) {
		t.Parallel()

		synctest.Test(t, func(t *testing.T) {
			g := sc.NewGroup[string](m)
			defer g.Close()

			_, _, err := g.Start(t.Context(), "a", counter{N: 0})
			require.NoError(t, err)

			_, err = g.Send(t.Context(), "a", &tick{})
			checkPointer(t, err)

			_, err = g.Send(t.Context(), "a", tick{})
			require.NoError(t, err)
		})
	})

	t.Run("a chart that names the pointer", func(t *testing.T) {
		t.Parallel()

		pointers, err := sc.New("pointers", func(b *sc.Builder[string, counter]) {
			b.Initial("idle")
			b.State("idle").On[*tick]().To("done")
			b.Final("done")
		})
		require.NoError(t, err)

		synctest.Test(t, func(t *testing.T) {
			inst, err := pointers.Instance(t.Context(), counter{N: 0})
			require.NoError(t, err)
			defer inst.Close()

			_, err = inst.Send(t.Context(), tick{})
			require.ErrorIs(t, err, sc.ErrUnknownEvent)
			require.ErrorContains(t, err, "the chart handles *sc_test.tick, a pointer")

			step, err := inst.Send(t.Context(), &tick{})
			require.NoError(t, err)
			require.True(t, step.Accepted())
		})
	})
}

func TestTypedOnAnyChartAcceptsAnyEvent(t *testing.T) {
	t.Parallel()

	m, err := sc.New("open", func(b *sc.Builder[string, counter]) {
		b.Initial("idle")
		b.State("idle").On[tick]().Assign("count", func(c counter, _ tick) counter { c.N++; return c })
		b.State("idle").OnAny().Assign("other", func(c counter, _ any) counter { c.N += 10; return c })
	})
	require.NoError(t, err)

	require.NoError(t, m.CheckEvent(&tick{}))
	require.NoError(t, m.CheckEvent(pong{}))

	synctest.Test(t, func(t *testing.T) {
		inst, err := m.Instance(t.Context(), counter{N: 0})
		require.NoError(t, err)
		defer inst.Close()

		for _, ev := range []any{&tick{}, pong{}, tick{}} {
			_, err = inst.Send(t.Context(), ev)
			require.NoError(t, err)
		}
		require.Equal(t, 21, inst.Snapshot().Context.N)

		a := m.Actor(counter{N: 0})
		ctx, cancel := context.WithCancel(t.Context())
		wait := runActor(ctx, a)
		_, err = a.Call(t.Context(), &pong{})
		require.NoError(t, err)
		require.Equal(t, 10, a.Snapshot().Context.N)
		cancel()
		require.ErrorIs(t, wait(), context.Canceled)
	})
}

func TestTypedTheCoreStillIgnoresAnUnknownEvent(t *testing.T) {
	t.Parallel()

	m := emitsGreeting(t)
	snap, _ := m.Start(counter{N: 0}, epoch)

	next, effects := m.Transition(snap, &tick{}, epoch)
	require.Empty(t, effects)
	require.Equal(t, snap.Seq(), next.Seq())
}

// chargeChart charges the amount of the charge event that entered
// "charging", retrying once, and keeps what each run saw.
func chargeChart(t *testing.T, seen chan<- charge, fail func(ctx context.Context) bool) *sc.Machine[string, counter] {
	t.Helper()

	m, err := sc.New("charging", func(b *sc.Builder[string, counter]) {
		b.Initial("idle")
		b.State("idle").On[charge]().To("charging")
		inv := b.State("charging").InvokeOn[charge]("charge", func(ctx context.Context, _ *sc.Run, _ counter, ev charge) (int, error) {
			seen <- ev
			if fail(ctx) {
				return 0, errFlaky
			}

			return ev.Amount, nil
		})
		inv.Retry(sc.Retry{Max: 1, First: time.Second, Cap: 0, Jitter: 0, If: nil})
		inv.OnDone().Assign("keep", func(c counter, d sc.Done[int]) counter { c.N = d.Result; return c }).To("done")
		inv.OnError().To("failed")
		b.Final("done")
		b.Final("failed")
	})
	require.NoError(t, err)

	return m
}

func TestTypedInvokeOnsEventSurvivesARetry(t *testing.T) {
	t.Parallel()

	synctest.Test(t, func(t *testing.T) {
		seen := make(chan charge, 2)
		calls := 0
		m := chargeChart(t, seen, func(context.Context) bool { calls++; return calls == 1 })

		inst, err := m.Instance(t.Context(), counter{N: 0})
		require.NoError(t, err)
		defer inst.Close()

		_, err = inst.Send(t.Context(), charge{Amount: 42})
		require.NoError(t, err)

		done, err := inst.Await(t.Context(), func(s sc.Snapshot[string, counter]) bool { return s.Done() })
		require.NoError(t, err)
		require.Equal(t, []string{"done"}, done.Active())
		require.Equal(t, 42, done.Context.N)

		close(seen)
		var got []charge
		for ev := range seen {
			got = append(got, ev)
		}
		require.Equal(t, []charge{{Amount: 42}, {Amount: 42}}, got)
	})
}

func TestTypedInvokeOnsEventSurvivesAStoreAndRestore(t *testing.T) {
	t.Parallel()

	synctest.Test(t, func(t *testing.T) {
		gate := make(chan struct{})
		seen := make(chan charge, 2)
		m := chargeChart(t, seen, func(ctx context.Context) bool {
			select {
			case <-gate:
				return false
			case <-ctx.Done():
				return true
			}
		})

		inst, err := m.Instance(t.Context(), counter{N: 0})
		require.NoError(t, err)

		_, err = inst.Send(t.Context(), charge{Amount: 7})
		require.NoError(t, err)
		require.Equal(t, charge{Amount: 7}, <-seen)

		// The process stops while the charge is in flight; the stopped
		// run's result is discarded.
		inst.Close()
		data, err := json.Marshal(inst.Snapshot())
		require.NoError(t, err)

		snap, _, err := m.Restore(data, time.Now())
		require.NoError(t, err)
		inst, err = m.InstanceFrom(t.Context(), snap)
		require.NoError(t, err)
		defer inst.Close()

		// The new run is handed the same event, decoded from the snapshot.
		require.Equal(t, charge{Amount: 7}, <-seen)
		gate <- struct{}{}

		done, err := inst.Await(t.Context(), func(s sc.Snapshot[string, counter]) bool { return s.Done() })
		require.NoError(t, err)
		require.Equal(t, 7, done.Context.N)
	})
}
