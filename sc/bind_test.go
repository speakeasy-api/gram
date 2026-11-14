package sc_test

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/sc"
)

// declaredChart declares its service and chart action without implementing
// them, the way a package declares a chart before it has dependencies.
func declaredChart(t *testing.T) *sc.Machine[string, counter] {
	t.Helper()

	m, err := sc.New("declared", func(b *sc.Builder[string, counter]) {
		b.RequiresAction("notify")
		b.Initial("working")
		inv := b.State("working").Requires[int]("compute")
		inv.OnDone().
			Assign("store", func(c counter, d sc.Done[int]) counter { c.N = d.Result; return c }).
			Run("notify").
			To("done")
		inv.OnError().To("failed")
		b.Final("done")
		b.Final("failed")
	})
	require.NoError(t, err)

	return m
}

func TestBindGivesImplementationsToADeclaredChart(t *testing.T) {
	t.Parallel()

	m := declaredChart(t)
	require.Equal(t, []string{"action notify", "service compute"}, m.Unbound())

	var notified []string
	bound, err := m.Bind(func(b *sc.Binder[string, counter]) {
		b.Service[int]("compute", func(context.Context, *sc.Run, counter) (int, error) { return 42, nil })
		b.Action("notify", func(_ context.Context, c counter, mv sc.Move[string]) {
			notified = append(notified, mv.Source)
		})
	})
	require.NoError(t, err)
	require.Empty(t, bound.Unbound())

	snap, effects := bound.Start(counter{}, epoch)
	var events []any
	require.NoError(t, bound.Exec(t.Context(), effect(t, effects, sc.EffectStartInvocation), func(ev any) { events = append(events, ev) }))
	require.Len(t, events, 1)

	snap, effects = bound.Transition(snap, events[0], epoch)
	require.Equal(t, 42, snap.Context.N)
	require.NoError(t, bound.Exec(t.Context(), effect(t, effects, sc.EffectAction), func(any) {}))
	require.Equal(t, []string{"working"}, notified)

	// The unbound machine and the bound copy share the chart, so their
	// snapshots are interchangeable.
	unboundSnap, _ := m.Start(counter{}, epoch)
	require.True(t, bound.Can(unboundSnap, events[0], epoch))
}

func TestAnUnboundServiceFailsWhenItRuns(t *testing.T) {
	t.Parallel()

	m := declaredChart(t)
	_, effects := m.Start(counter{}, epoch)

	err := m.Exec(t.Context(), effect(t, effects, sc.EffectStartInvocation), func(any) {})
	require.ErrorIs(t, err, sc.ErrNotBound)
}

func TestBindReportsUndeclaredNamesAndMismatchedResultTypes(t *testing.T) {
	t.Parallel()

	_, err := declaredChart(t).Bind(func(b *sc.Binder[string, counter]) {
		b.Service[string]("compute", func(context.Context, *sc.Run, counter) (string, error) { return "", nil })
		b.Service[int]("missing", func(context.Context, *sc.Run, counter) (int, error) { return 0, nil })
		b.Action("absent", func(context.Context, counter, sc.Move[string]) {})
	})

	require.ErrorContains(t, err, `service "compute" in state "working" reports sc.Done[int], not sc.Done[string]`)
	require.ErrorContains(t, err, `service "missing" is not declared`)
	require.ErrorContains(t, err, `chart action "absent" is not declared`)
}

func TestBindReplacesAnImplementationForATest(t *testing.T) {
	t.Parallel()

	m := orderChart(t, func(context.Context, *sc.Run, order) (string, error) { return "real", nil })
	fake, err := m.Bind(func(b *sc.Binder[state, order]) {
		b.Service[string]("charge", func(context.Context, *sc.Run, order) (string, error) { return "fake", nil })
	})
	require.NoError(t, err)

	_, effects := fake.Transition(func() sc.Snapshot[state, order] { s, _ := fake.Start(order{}, epoch); return s }(), submit{Items: []string{"book"}}, epoch)

	var done sc.Done[string]
	require.NoError(t, fake.Exec(t.Context(), effect(t, effects, sc.EffectStartInvocation), func(ev any) { done, _ = ev.(sc.Done[string]) }))
	require.Equal(t, "fake", done.Result)
}

func TestExploreNeedsNoImplementations(t *testing.T) {
	t.Parallel()

	report, err := declaredChart(t).Explore(counter{}, sc.Exploration[string, counter]{
		Outcomes: map[string][]sc.Outcome{"compute": {sc.Succeeds(1), sc.Fails(errWorkFailed)}},
	})
	require.NoError(t, err)

	require.True(t, report.Reached("done"))
	require.True(t, report.Reached("failed"))
}
