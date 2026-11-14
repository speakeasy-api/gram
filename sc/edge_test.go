package sc_test

import (
	"context"
	"testing"
	"testing/synctest"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/sc"
)

// gateChart enters "open" through an entry action that calls gate with the
// instance's context.
func gateChart(t *testing.T, gate func(counter)) *sc.Machine[string, counter] {
	t.Helper()

	m, err := sc.New("gate", func(b *sc.Builder[string, counter]) {
		b.Initial("open")
		b.State("open").Entry("gate", func(_ context.Context, c counter) { gate(c) })
	})
	require.NoError(t, err)

	return m
}

func TestGroupStartDoesNotHoldUpOtherKeysWhileAnInstanceIsCreated(t *testing.T) {
	t.Parallel()

	synctest.Test(t, func(t *testing.T) {
		release := make(chan struct{})
		m := gateChart(t, func(c counter) {
			if c.N == 1 {
				<-release
			}
		})
		g := sc.NewGroup[string](m)
		defer g.Close()

		type started struct {
			inst    *sc.Instance[string, counter]
			created bool
		}
		slow := make(chan started, 2)
		for range 2 {
			go func() {
				inst, created, err := g.Start(t.Context(), "slow", counter{N: 1})
				if err == nil {
					slow <- started{inst: inst, created: created}
				}
			}()
		}
		synctest.Wait()

		// The slow key's entry action is still running, and the group
		// serves other keys meanwhile.
		_, created, err := g.Start(t.Context(), "quick", counter{N: 0})
		require.NoError(t, err)
		require.True(t, created)
		require.Equal(t, []string{"quick"}, g.Keys())

		close(release)
		first, second := <-slow, <-slow

		// Both callers for the slow key share the one instance, which
		// one of them created.
		require.Same(t, first.inst, second.inst)
		require.NotEqual(t, first.created, second.created)
		require.Len(t, g.Keys(), 2)
	})
}

func TestGroupStartThatFailsCreatesNothing(t *testing.T) {
	t.Parallel()

	m := gateChart(t, func(counter) { panic("boom") })
	g := sc.NewGroup[string](m)
	defer g.Close()

	inst, created, err := g.Start(t.Context(), "a", counter{N: 0})
	require.ErrorIs(t, err, sc.ErrPanicked)
	require.Nil(t, inst)
	require.False(t, created)
	require.Empty(t, g.Keys())
}

func TestDeclaringABudgetTwiceOrAnInterfaceEventIsAnError(t *testing.T) {
	t.Parallel()

	_, err := sc.New("twice", func(b *sc.Builder[string, counter]) {
		b.Initial("a")
		b.State("a").Within(time.Minute).Within(time.Hour)
	})
	require.ErrorContains(t, err, "budget declared twice")

	_, err = sc.New("iface", func(b *sc.Builder[string, counter]) {
		b.Initial("a")
		b.State("a").On[any]().To("b")
		b.State("b")
	})
	require.ErrorContains(t, err, "is an interface")
}

func TestBindRejectsANilImplementation(t *testing.T) {
	t.Parallel()

	m, err := sc.New("unbound", func(b *sc.Builder[string, counter]) {
		b.Initial("a")
		inv := b.State("a").Requires[int]("work")
		inv.OnDone().To("b")
		inv.OnError().To("b")
		b.Final("b")
	})
	require.NoError(t, err)

	_, err = m.Bind(func(b *sc.Binder[string, counter]) {
		b.Service[int]("work", nil)
	})
	require.ErrorContains(t, err, "nil function")
}

func TestWithClockPanicsOnANilClock(t *testing.T) {
	t.Parallel()

	require.PanicsWithValue(t, "sc: WithClock: the clock is nil", func() { sc.WithClock(nil) })
}

func TestStartInTakesAnAlwaysTransitionThatHolds(t *testing.T) {
	t.Parallel()

	m, err := sc.New("through", func(b *sc.Builder[string, counter]) {
		b.Initial("a")
		b.State("a").On[tick]().To("b")
		b.State("b").Always().When("ready", func(c counter, _ struct{}) bool { return c.N > 0 }).To("c")
		b.State("c")
	})
	require.NoError(t, err)

	snap, _, err := m.StartIn(counter{N: 1}, epoch, "b")
	require.NoError(t, err)
	require.Equal(t, []string{"c"}, snap.Active())

	snap, _, err = m.StartIn(counter{N: 0}, epoch, "b")
	require.NoError(t, err)
	require.Equal(t, []string{"b"}, snap.Active())
}

func TestExecDoesNotTakeAPanicInSendForTheServices(t *testing.T) {
	t.Parallel()

	m := workChart(t, func(context.Context, *sc.Run, counter) (int, error) { return 1, nil })
	snap, _ := m.Start(counter{N: 0}, epoch)
	_, effects := m.Transition(snap, tick{}, epoch)

	require.PanicsWithValue(t, "driver bug", func() {
		_ = m.Exec(t.Context(), effect(t, effects, sc.EffectStartInvocation), func(any) { panic("driver bug") })
	})
}

func TestExploreCountsStartsAgainstMaxStates(t *testing.T) {
	t.Parallel()

	m := gateChart(t, func(counter) {})

	report, err := m.Explore(counter{N: 0}, sc.Exploration[string, counter]{
		Starts:    []counter{{N: 1}, {N: 2}},
		MaxStates: 2,
	})
	require.NoError(t, err)
	require.Equal(t, 2, report.States)
	require.True(t, report.Truncated)
}

func TestExploreTellsApartStatesWithDifferentBudgetsLeft(t *testing.T) {
	t.Parallel()

	m, err := sc.New("budgeted", func(b *sc.Builder[string, counter]) {
		b.Initial("working")
		working := b.State("working").Within(time.Minute)
		inv := working.Requires[int]("work")
		inv.OnDone().To("done")
		inv.OnError().To("done")
		b.Final("done")
	})
	require.NoError(t, err)

	report, err := m.Explore(counter{N: 0}, sc.Exploration[string, counter]{
		Waits: []time.Duration{30 * time.Second},
	})
	require.NoError(t, err)

	// Working with a minute, half a minute and nothing left, then done.
	require.Equal(t, 4, report.States)
	require.False(t, report.Truncated)
}

func TestSendAndSettleSettlesBeforeReturningAnActionsError(t *testing.T) {
	t.Parallel()

	synctest.Test(t, func(t *testing.T) {
		m, err := sc.New("settles", func(b *sc.Builder[string, counter]) {
			b.Initial("idle")
			b.State("idle").On[tick]().Do("fail", func(context.Context, counter, tick) { panic("boom") }).To("working")
			working := b.State("working")
			inv := working.Invoke("work", func(ctx context.Context, _ *sc.Run, _ counter) (int, error) {
				select {
				case <-time.After(time.Minute):
					return 1, nil
				case <-ctx.Done():
					return 0, context.Cause(ctx)
				}
			})
			inv.OnDone().To("done")
			inv.OnError().To("done")
			b.Final("done")
		})
		require.NoError(t, err)

		inst, err := m.Instance(t.Context(), counter{N: 0})
		require.NoError(t, err)
		defer inst.Close()

		step, err := inst.SendAndSettle(t.Context(), tick{})
		require.ErrorIs(t, err, sc.ErrPanicked)
		require.True(t, step.Accepted())

		// The service the step started has finished and been processed.
		require.True(t, inst.Snapshot().Done())
	})
}

func TestANameCannotBeBothAServiceAndAResource(t *testing.T) {
	t.Parallel()

	_, err := sc.New("clash", func(b *sc.Builder[string, counter]) {
		b.Initial("a")
		a := b.State("a")
		work := a.Requires[int]("thing")
		work.OnDone().To("b")
		work.OnError().To("b")

		held := b.State("b").HoldsRequired[int]("thing")
		held.OnAcquired().To("c")
		held.OnError().To("c")
		b.Final("c")
	})
	require.ErrorContains(t, err, "names both a service and a resource")
}
