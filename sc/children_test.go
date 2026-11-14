package sc_test

import (
	"testing"
	"testing/synctest"

	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/sc"
)

// TestChildHandlesTellApartChildrenOfTheSameName checks that a child handle
// reads the child its own declaration started, when two regions each invoke
// a child under the same name.
func TestChildHandlesTellApartChildrenOfTheSameName(t *testing.T) {
	t.Parallel()

	synctest.Test(t, func(t *testing.T) {
		door := func(initial string) *sc.Machine[string, counter] {
			m, err := sc.New("door-"+initial, func(b *sc.Builder[string, counter]) {
				b.Initial(initial)
				b.State(initial)
			})
			require.NoError(t, err)

			return m
		}
		start := func(counter) counter { return counter{N: 0} }
		output := func(sc.Snapshot[string, counter]) (int, error) { return 0, nil }

		var front, back *sc.Child[string, counter, string, counter, int]
		m, err := sc.New("house", func(b *sc.Builder[string, counter]) {
			b.Regions()
			left := b.State("left")
			front = left.InvokeMachine("door", door("closed"), start, output)
			front.OnError().Stay()
			right := b.State("right")
			back = right.InvokeMachine("door", door("open"), start, output)
			back.OnError().Stay()

			left.On[tick]().WhenChildIn(front, "open").Assign("front", func(c counter, _ tick) counter { return counter{N: c.N + 1} })
			left.On[leave]().WhenChildIn(back, "open").Assign("back", func(c counter, _ leave) counter { return counter{N: c.N + 10} })
		})
		require.NoError(t, err)

		inst, err := m.Instance(t.Context(), counter{N: 0})
		require.NoError(t, err)
		defer inst.Close()
		synctest.Wait()

		frontSnap, ok := front.Snapshot(inst.Snapshot())
		require.True(t, ok)
		require.Equal(t, []string{"closed"}, frontSnap.Active())

		backSnap, ok := back.Snapshot(inst.Snapshot())
		require.True(t, ok)
		require.Equal(t, []string{"open"}, backSnap.Active())

		// The front door is closed, so tick is refused; the back door is
		// open, so leave is taken.
		step, err := inst.Send(t.Context(), tick{})
		require.NoError(t, err)
		require.False(t, step.Accepted())

		step, err = inst.Send(t.Context(), leave{})
		require.NoError(t, err)
		require.True(t, step.Accepted())
		require.Equal(t, counter{N: 10}, inst.Snapshot().Context)
	})
}

func TestAnEmptyEntryOrExitListDeclaresNothing(t *testing.T) {
	t.Parallel()

	_, err := sc.New("empty", func(b *sc.Builder[string, counter]) {
		b.Initial("a")
		a := b.State("a")
		a.Entry()
		a.Exit()
		b.Chart().Entry()
		b.Final("f").Exit()
		a.On[tick]().To("f")
	})
	require.NoError(t, err)
}
