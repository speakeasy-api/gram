package sc_test

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/sc"
)

func TestATransitionEntersSeveralRegionsAtOnce(t *testing.T) {
	t.Parallel()

	m, err := sc.New("several", func(b *sc.Builder[string, counter]) {
		b.Initial("off")
		b.State("off").On[tick]().To("loud", "fast")
		b.Parallel("on", func(b *sc.Builder[string, counter]) {
			b.State("volume", func(b *sc.Builder[string, counter]) {
				b.Initial("quiet")
				b.State("quiet")
				b.State("loud")
			})
			b.State("speed", func(b *sc.Builder[string, counter]) {
				b.Initial("slow")
				b.State("slow")
				b.State("fast")
			})
			b.State("light", func(b *sc.Builder[string, counter]) {
				b.Initial("dim")
				b.State("dim")
			})
		})
	})
	require.NoError(t, err)

	snap, _ := m.Start(counter{N: 0}, epoch)
	snap, _ = m.Transition(snap, tick{}, epoch)

	// The named regions go where they were told, and the third gets its
	// default.
	require.Equal(t, []string{"on", "volume", "loud", "speed", "fast", "light", "dim"}, snap.Active())

	_, err = sc.New("together", func(b *sc.Builder[string, counter]) {
		b.Initial("a")
		b.State("a").On[tick]().To("b", "c")
		b.State("b")
		b.State("c")
	})
	require.ErrorContains(t, err, "not in different regions of a parallel state")
}

func TestAStateRaisesEventsOnEntryAndExit(t *testing.T) {
	t.Parallel()

	m, err := sc.New("raising", func(b *sc.Builder[string, counter]) {
		b.Initial("a")
		b.State("a").
			RaiseOnExit("leaving", func(counter) any { return ping{} }).
			On[tick]().To("b")
		b.State("b").
			RaiseOnEntry("arrived", func(counter) any { return back{} }).
			On[ping]().Assign("count", increment[ping])
		b.State("b").On[back]().Assign("count", increment[back])
	})
	require.NoError(t, err)

	snap, _ := m.Start(counter{N: 0}, epoch)
	snap, _ = m.Transition(snap, tick{}, epoch)

	// Both raised events were processed, in the order they were raised,
	// once the step that raised them was complete.
	require.Equal(t, []string{"b"}, snap.Active())
	require.Equal(t, 2, snap.Context.N)
}

func TestAHistoryStateCanBeTheInitialState(t *testing.T) {
	t.Parallel()

	m, err := sc.New("resuming", func(b *sc.Builder[string, counter]) {
		b.Initial("work")
		b.State("work", func(b *sc.Builder[string, counter]) {
			b.Initial("last")
			b.History("last", sc.Shallow).Default("first")
			b.State("first").On[tick]().To("second")
			b.State("second")
		}).On[leave]().To("away")
		b.State("away").On[back]().To("work")
	})
	require.NoError(t, err)

	// The first time, the default; after that, where it was.
	snap, _ := m.Start(counter{N: 0}, epoch)
	require.Equal(t, []string{"work", "first"}, snap.Active())

	for _, ev := range []any{tick{}, leave{}, back{}} {
		snap, _ = m.Transition(snap, ev, epoch)
	}
	require.Equal(t, []string{"work", "second"}, snap.Active())

	_, err = sc.New("bare", func(b *sc.Builder[string, counter]) {
		b.Initial("work")
		b.State("work", func(b *sc.Builder[string, counter]) {
			b.Initial("last")
			b.History("last", sc.Shallow)
			b.State("first")
		})
	})
	require.ErrorContains(t, err, "history state without a default")
}

func TestUnreachableStatesAreAllowedOnRequest(t *testing.T) {
	t.Parallel()

	declare := func(b *sc.Builder[string, counter]) {
		b.Initial("a")
		b.State("a")
		b.State("orphan")
	}

	_, err := sc.New("strict", declare)
	require.ErrorContains(t, err, "unreachable")

	_, err = sc.New("lenient", declare, sc.AllowUnreachable())
	require.NoError(t, err)
}

func TestTheChartItselfTakesTransitions(t *testing.T) {
	t.Parallel()

	m, err := sc.New("everywhere", func(b *sc.Builder[string, counter]) {
		b.Initial("a")
		b.On[leave]().To("stopped")
		b.OnAny().Assign("unexpected", func(c counter, _ any) counter { c.N += 100; return c })

		b.State("a").On[tick]().To("b")
		b.State("b").On[leave]().Assign("stay", increment[leave])
		b.Final("stopped")
	})
	require.NoError(t, err)

	// The chart's transition applies in a state that does not handle the
	// event itself.
	snap, _ := m.Start(counter{N: 0}, epoch)
	stopped, _ := m.Transition(snap, leave{}, epoch)
	require.True(t, stopped.Done())

	// A state that handles the event comes first, and the chart catches
	// what nothing else takes.
	snap, _ = m.Transition(snap, tick{}, epoch)
	snap, _ = m.Transition(snap, leave{}, epoch)
	require.Equal(t, []string{"b"}, snap.Active())
	require.Equal(t, 1, snap.Context.N)

	snap, _ = m.Transition(snap, ping{}, epoch)
	require.Equal(t, 101, snap.Context.N)
}

func TestAStateAssignsOnEntryAndExit(t *testing.T) {
	t.Parallel()

	m, err := sc.New("visits", func(b *sc.Builder[string, counter]) {
		b.Initial("idle")
		b.State("idle").On[tick]().To("trying")
		b.State("trying").
			AssignOnEntry("reset", func(counter) counter { return counter{N: 0} }).
			AssignOnExit("mark", func(c counter) counter { c.N += 1000; return c }).
			Entry("announce", noop).
			On[tick]().Assign("count", increment[tick])
		b.State("trying").On[leave]().To("idle")
	})
	require.NoError(t, err)

	snap, _ := m.Start(counter{N: 7}, epoch)

	// Entering resets the count, and the entry action sees the new
	// context.
	snap, effects := m.Transition(snap, tick{}, epoch)
	require.Equal(t, 0, snap.Context.N)
	require.Equal(t, 0, effects[0].Context.N)

	snap, _ = m.Transition(snap, tick{}, epoch)
	snap, _ = m.Transition(snap, leave{}, epoch)
	require.Equal(t, 1001, snap.Context.N)
}

func TestAFinalStateAssignsBeforeItsParentCompletes(t *testing.T) {
	t.Parallel()

	m, err := sc.New("outcome", func(b *sc.Builder[string, counter]) {
		b.Initial("work")
		b.State("work", func(b *sc.Builder[string, counter]) {
			b.Initial("doing")
			b.State("doing").On[tick]().To("finished")
			b.Final("finished").AssignOnEntry("result", func(c counter) counter { c.N = 42; return c })
		}).OnDone().
			When("hasResult", func(c counter, _ struct{}) bool { return c.N == 42 }).
			To("done")
		b.Final("done")
	})
	require.NoError(t, err)

	snap, _ := m.Start(counter{N: 0}, epoch)
	snap, _ = m.Transition(snap, tick{}, epoch)

	require.True(t, snap.Done())
	require.Equal(t, 42, snap.Context.N)
}
