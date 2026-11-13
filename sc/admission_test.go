package sc_test

import (
	"context"
	"encoding/json"
	"errors"
	"math/rand/v2"
	"slices"
	"testing"
	"testing/synctest"
	"time"

	"github.com/stretchr/testify/require"
	"golang.org/x/sync/semaphore"

	"github.com/speakeasy-api/gram/sc"
)

type (
	poll     struct{}
	finished struct{}
)

// pollingChart is the certificate tracker's request shape: a poll starts a
// request only if a pod-wide slot is free, the slot is held for the whole
// request, and it is given back however the request ends.
func pollingChart(t *testing.T) *sc.Machine[string, counter] {
	t.Helper()

	m, err := sc.New("polling", func(b *sc.Builder[string, counter]) {
		b.Initial("idle")
		b.State("idle").On[poll]().To("requesting")

		requesting := b.State("requesting", func(b *sc.Builder[string, counter]) {
			b.Initial("admitting")
			b.State("admitting")
			b.State("working").On[finished]().To("idle")
		})
		slot := requesting.HoldsRequired[sc.Permit]("slot")
		slot.OnAcquired().To("working")
		slot.OnError().
			When("noSlot", func(_ counter, f sc.Failed) bool { return errors.Is(f.Err, sc.ErrNoPermit) }).
			Assign("countSkip", increment[sc.Failed]).
			To("idle")
		slot.OnError().To("idle")
	})
	require.NoError(t, err)

	return m
}

func TestAPermitIsHeldForTheWholeStateAndSkippedWhenNoneIsFree(t *testing.T) {
	t.Parallel()

	synctest.Test(t, func(t *testing.T) {
		// Two trackers share a pod-wide budget of one request.
		sem := semaphore.NewWeighted(1)
		m, err := pollingChart(t).Bind(func(b *sc.Binder[string, counter]) { b.Permit("slot", sem) })
		require.NoError(t, err)

		first, err := m.Instance(t.Context(), counter{})
		require.NoError(t, err)
		defer first.Close()
		second, err := m.Instance(t.Context(), counter{})
		require.NoError(t, err)
		defer second.Close()

		_, err = first.SendAndSettle(t.Context(), poll{})
		require.NoError(t, err)
		require.Equal(t, []string{"requesting", "working"}, first.Snapshot().Active())

		// No slot is free: the second tracker's poll is skipped, not
		// queued, and it is back in idle to try at its next poll.
		_, err = second.SendAndSettle(t.Context(), poll{})
		require.NoError(t, err)
		require.Equal(t, []string{"idle"}, second.Snapshot().Active())
		require.Equal(t, 1, second.Snapshot().Context.N)

		// Ending the request gives the slot back.
		_, err = first.Send(t.Context(), finished{})
		require.NoError(t, err)
		_, err = second.SendAndSettle(t.Context(), poll{})
		require.NoError(t, err)
		require.Equal(t, []string{"requesting", "working"}, second.Snapshot().Active())

		// So does closing an instance that holds one.
		second.Close()
		require.True(t, sem.TryAcquire(1))
	})
}

func TestExploreCoversBothOutcomesOfAPermit(t *testing.T) {
	t.Parallel()

	report, err := pollingChart(t).Explore(counter{}, sc.Exploration[string, counter]{
		Events:   []any{poll{}, finished{}},
		Outcomes: map[string][]sc.Outcome{"slot": {sc.Acquires(sc.Permit{}), sc.Fails(sc.ErrNoPermit)}},
		Key:      func(c counter, _ time.Time) any { return min(c.N, 1) },
		Invariants: []sc.Invariant[string, counter]{{
			Name: "work happens only while the slot is held",
			Holds: func(s sc.Snapshot[string, counter]) bool {
				return !s.In("working") || (len(s.Resources()) == 1 && s.Resources()[0].Acquired)
			},
		}},
	})
	require.NoError(t, err)

	require.Empty(t, report.Violations)
	require.True(t, report.Reached("working"))
}

// hostChart is the gateway host's shape: a slot runs a node only while the
// pod is under its cap, the node is a live value held while hosting, and the
// one-shot key used to start it never leaves the function that starts it.
func hostChart(t *testing.T) (*sc.Machine[string, counter], *sc.Resource[string, counter, *line]) {
	t.Helper()

	var node *sc.Resource[string, counter, *line]

	m, err := sc.New("host", func(b *sc.Builder[string, counter]) {
		b.Initial("idle")
		b.State("idle").On[poll]().To("hosting")

		hosting := b.State("hosting", func(b *sc.Builder[string, counter]) {
			b.Initial("starting")
			b.State("starting")

			online := b.State("online")
			probe := online.Requires[string]("probe")
			probe.OnDone().Stay()
			probe.OnError().Stay()
		})

		limit := hosting.HoldsRequired[sc.Permit]("cap")
		limit.OnError().Assign("countOverCap", increment[sc.Failed]).To("idle")

		node = hosting.HoldsRequired[*line]("node")
		node.OnAcquired().To("online")
		node.OnError().To("idle")

		hosting.On[finished]().To("idle")
	})
	require.NoError(t, err)

	return m, node
}

func TestHostsShareACapAndEachHoldsItsNode(t *testing.T) {
	t.Parallel()

	synctest.Test(t, func(t *testing.T) {
		log := &journal{}
		chart, node := hostChart(t)

		// hostCap is how many hosts the pod runs at once.
		const hostCap = 2
		sem := semaphore.NewWeighted(hostCap)

		started := 0
		m, err := chart.Bind(func(b *sc.Binder[string, counter]) {
			b.Permit("cap", sem)
			b.Resource[*line]("node",
				func(context.Context, counter) (*line, error) {
					// The one-shot key is read and used here, and
					// is gone when this returns.
					key := "one-shot-key"
					started++

					return &line{name: "node-" + key[:3], done: make(chan struct{})}, nil
				},
				func(_ context.Context, n *line, why sc.Leave[string]) {
					close(n.done)
					log.add("closed node, shutting down=%t", why.Closed)
				},
			)
			b.Service[string]("probe", func(ctx context.Context, _ counter) (string, error) {
				n, ok := node.From(ctx)
				if !ok {
					return "", errWorkFailed
				}

				return n.name, nil
			})
		})
		require.NoError(t, err)

		g := sc.NewGroup[int](m)
		defer g.Close()

		for slot := range 3 {
			inst, _, err := g.Start(t.Context(), slot, counter{})
			require.NoError(t, err)
			_, err = inst.SendAndSettle(t.Context(), poll{})
			require.NoError(t, err)
		}

		online := func(s sc.Snapshot[string, counter]) bool { return s.In("online") }
		require.Equal(t, hostCap, g.Count(online))
		require.Equal(t, hostCap, started)

		// The third slot was turned away without starting a node, and
		// tries again at its next poll once a host has stopped.
		third, _ := g.Get(2)
		require.Equal(t, []string{"idle"}, third.Snapshot().Active())
		require.Equal(t, 1, third.Snapshot().Context.N)

		_, err = g.Send(t.Context(), 0, finished{})
		require.NoError(t, err)
		_, err = third.SendAndSettle(t.Context(), poll{})
		require.NoError(t, err)
		require.Equal(t, hostCap, g.Count(online))

		// A restart: the slot's snapshot is all that survives, the node is
		// started again, and the cap is taken again.
		second, _ := g.Get(1)
		snap := second.Snapshot()
		data, err := json.Marshal(snap)
		require.NoError(t, err)
		require.NotContains(t, string(data), "one-shot")

		g.Remove(1)
		require.Contains(t, log.list(), "closed node, shutting down=true")

		restored, _, err := m.Restore(data, time.Now())
		require.NoError(t, err)
		resumed, err := g.Resume(t.Context(), 1, restored)
		require.NoError(t, err)
		synctest.Wait()

		require.True(t, resumed.Snapshot().In("online"))
		require.Equal(t, hostCap, g.Count(online))
		require.False(t, sem.TryAcquire(1))
	})
}

type jittered struct {
	RefreshIn time.Duration `json:"refresh_in"`
}

func refreshChart(t *testing.T) *sc.Machine[string, jittered] {
	t.Helper()

	m, err := sc.New("refresh", func(b *sc.Builder[string, jittered]) {
		b.Initial("serving")
		b.State("serving").On[poll]().AssignRand("jitterRefresh", func(c jittered, _ poll, _ time.Time, rnd *rand.Rand) jittered {
			// An hour, give or take a tenth.
			c.RefreshIn = time.Duration(float64(time.Hour) * (0.9 + 0.2*rnd.Float64()))
			return c
		})
	})
	require.NoError(t, err)

	return m
}

func TestAssignRandIsRepeatableForAnInstanceAndDiffersBetweenSeeds(t *testing.T) {
	t.Parallel()

	m := refreshChart(t)

	draws := func(seed uint64) []time.Duration {
		snap, _ := m.Start(jittered{}, epoch, sc.Seed(seed))

		var out []time.Duration
		for range 3 {
			snap, _ = m.Transition(snap, poll{}, epoch)
			out = append(out, snap.Context.RefreshIn)
		}

		return out
	}

	first := draws(1)
	require.Equal(t, first, draws(1))
	require.NotEqual(t, first, draws(2))

	// Each step draws afresh, within the range asked for.
	require.Len(t, slices.Compact(slices.Clone(first)), 3)
	for _, d := range first {
		require.InDelta(t, float64(time.Hour), float64(d), float64(6*time.Minute))
	}

	// The seed is part of the snapshot, so the draws continue after a
	// restart exactly as they would have.
	snap, _ := m.Start(jittered{}, epoch, sc.Seed(1))
	snap, _ = m.Transition(snap, poll{}, epoch)
	data, err := json.Marshal(snap)
	require.NoError(t, err)
	restored, _, err := m.Restore(data, epoch)
	require.NoError(t, err)
	restored, _ = m.Transition(restored, poll{}, epoch)
	require.Equal(t, first[1], restored.Context.RefreshIn)

	report, err := m.Explore(jittered{}, sc.Exploration[string, jittered]{
		Events:           []any{poll{}},
		Key:              func(jittered, time.Time) any { return 0 },
		CheckDeterminism: true,
	})
	require.NoError(t, err)
	require.Empty(t, report.Violations)
}

func TestWithCodeKeepsTheErrorAndItsCodeAcrossStorage(t *testing.T) {
	t.Parallel()

	failed := sc.Failed{ID: "x", Err: sc.WithCode(errWorkFailed, "work_failed")}
	require.ErrorIs(t, failed.Err, errWorkFailed)

	data, err := json.Marshal(failed)
	require.NoError(t, err)

	var loaded sc.Failed
	require.NoError(t, json.Unmarshal(data, &loaded))
	require.Equal(t, "work_failed", sc.ErrorCode(loaded.Err))
	require.NoError(t, sc.WithCode(nil, "x"))
}
