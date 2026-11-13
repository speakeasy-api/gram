package sc_test

import (
	"context"
	"testing"
	"testing/synctest"

	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/sc"
)

func TestGroupHoldsInstancesByKeyAndReadsAcrossThem(t *testing.T) {
	t.Parallel()

	synctest.Test(t, func(t *testing.T) {
		m := statusChart(t, func(sc.Move[string]) {})
		g := sc.NewGroup[string](m)
		defer g.Close()

		for _, slot := range []string{"a", "b", "c"} {
			_, created, err := g.Start(t.Context(), slot, counter{})
			require.NoError(t, err)
			require.True(t, created)
		}

		_, created, err := g.Start(t.Context(), "a", counter{})
		require.NoError(t, err)
		require.False(t, created)

		_, err = g.Send(t.Context(), "a", connect{})
		require.NoError(t, err)
		_, err = g.Send(t.Context(), "a", tick{})
		require.NoError(t, err)

		online := func(s sc.Snapshot[string, counter]) bool { return s.In("online") }
		require.Equal(t, 1, g.Count(online))

		_, err = g.Send(t.Context(), "missing", tick{})
		require.ErrorIs(t, err, sc.ErrNoInstance)

		evicted := g.Evict(func(s sc.Snapshot[string, counter]) bool { return s.In("offline") })
		require.ElementsMatch(t, []string{"b", "c"}, evicted)
		require.Equal(t, []string{"a"}, g.Keys())

		final, ok := g.Remove("a")
		require.True(t, ok)
		require.True(t, final.In("online"))
		require.Empty(t, g.Keys())
	})
}

func TestGroupInstancesShareTheGroupsOptions(t *testing.T) {
	t.Parallel()

	synctest.Test(t, func(t *testing.T) {
		m := workChart(t, func(context.Context, counter) (int, error) { return 1, nil })
		rec := &recorder{}
		g := sc.NewGroup[int](m, sc.WithHooks(rec.hooks(m)))
		defer g.Close()

		for key := range 2 {
			_, _, err := g.Start(t.Context(), key, counter{})
			require.NoError(t, err)
			_, err = g.Send(t.Context(), key, tick{})
			require.NoError(t, err)
		}

		for key := range 2 {
			inst, _ := g.Get(key)
			_, err := inst.Await(t.Context(), func(s sc.Snapshot[string, counter]) bool { return s.Done() })
			require.NoError(t, err)
		}

		transitions := 0
		for _, call := range rec.list() {
			if len(call) > 10 && call[:10] == "transition" {
				transitions++
			}
		}
		require.Equal(t, 4, transitions)
	})
}
