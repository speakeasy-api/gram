package sc_test

import (
	"context"
	"encoding/json"
	"testing"
	"testing/synctest"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/sc"
)

// sessionChart puts everything a state can have on the chart itself: entry
// and exit actions, an entry assignment, a service, a timer, a budget, a tag
// and a transition.
func sessionChart(t *testing.T, heartbeat func(context.Context, counter) (int, error)) *sc.Machine[string, counter] {
	t.Helper()

	m, err := sc.New("session", func(b *sc.Builder[string, counter]) {
		chart := b.Chart().
			Tag("live").
			Within(time.Hour).
			AssignOnEntry("begin", func(c counter) counter { c.N = 1; return c }).
			Entry("opened", noop).
			Exit("closed", noop)
		beat := chart.Invoke("heartbeat", heartbeat)
		beat.OnDone().Assign("beats", increment[sc.Done[int]])
		beat.OnError().To("failed")
		chart.After(10 * time.Minute).To("expired")
		chart.On[leave]().To("left")

		b.Initial("idle")
		b.State("idle").On[tick]().To("busy")
		b.State("busy").Tag("working")
		b.Final("left")
		b.Final("expired")
		b.Final("failed")
	})
	require.NoError(t, err)

	return m
}

func TestTheChartIsAStateLikeAnyOther(t *testing.T) {
	t.Parallel()

	m := sessionChart(t, nil)

	// Starting enters the chart before its states: its assignment and
	// entry action first, then its timer and its service.
	snap, effects := m.Start(counter{N: 0}, epoch)
	require.Equal(t, 1, snap.Context.N)
	require.Equal(t, []string{"session/opened"}, names(effects[:1]))
	require.Equal(t, []sc.EffectKind{sc.EffectAction, sc.EffectStartTimer, sc.EffectStartInvocation}, kinds(effects))
	require.Equal(t, epoch.Add(time.Hour), effects[2].At)
	require.True(t, snap.HasTag("live"))

	// The chart's timer and service are the snapshot's, under the zero
	// state.
	require.Len(t, snap.Timers(), 1)
	require.Len(t, snap.Services(), 1)
	require.Empty(t, snap.Services()[0].State)

	// They survive being stored.
	data, err := json.Marshal(snap)
	require.NoError(t, err)
	require.Contains(t, string(data), `"state":"session"`)

	snap, restored, err := m.Restore(data, epoch)
	require.NoError(t, err)
	require.Empty(t, restored)
	require.Equal(t, []sc.EffectKind{sc.EffectStartTimer, sc.EffectStartInvocation}, kinds(m.Resume(snap)))

	// The service reports to the chart whatever state is active.
	snap, _ = m.Transition(snap, tick{}, epoch)
	snap, _ = m.Transition(snap, sc.Done[int]{ID: snap.Services()[0].ID, Result: 1}, epoch)
	require.Equal(t, []string{"busy"}, snap.Active())
	require.Equal(t, 2, snap.Context.N)
	require.Equal(t, []string{"live", "working"}, snap.Tags())

	// The chart's timer fires in any state, and finishing runs the chart's
	// exit action after everything else is stopped being needed.
	snap, effects = m.Advance(snap, epoch.Add(10*time.Minute))
	require.True(t, snap.Done())
	require.Equal(t, []string{"expired"}, snap.Active())
	require.Equal(t, []string{"session/closed"}, names(effects[:1]))
}

func TestTheChartsServiceRunsForTheLifeOfTheInstance(t *testing.T) {
	t.Parallel()

	synctest.Test(t, func(t *testing.T) {
		stopped := make(chan struct{})
		m := sessionChart(t, func(ctx context.Context, _ counter) (int, error) {
			<-ctx.Done()
			close(stopped)

			return 0, context.Cause(ctx)
		})

		inst, err := m.Instance(t.Context(), counter{N: 0})
		require.NoError(t, err)
		defer inst.Close()

		_, err = inst.Send(t.Context(), tick{})
		require.NoError(t, err)
		synctest.Wait()

		select {
		case <-stopped:
			require.Fail(t, "the chart's service stopped while the instance was running")
		default:
		}

		// Finishing the machine stops it.
		step, err := inst.Send(t.Context(), leave{})
		require.NoError(t, err)
		require.True(t, step.To.Done())
		<-stopped
	})
}

func TestAChartOfRegionsFinishesWhenEveryRegionHas(t *testing.T) {
	t.Parallel()

	m, err := sc.New("both", func(b *sc.Builder[string, counter]) {
		b.Regions()
		b.Chart().Exit("finished", noop)

		b.State("upload", func(b *sc.Builder[string, counter]) {
			b.Initial("uploading")
			b.State("uploading").Exit("stop", noop).On[tick]().To("uploaded")
			b.Final("uploaded")
		})
		b.State("scan", func(b *sc.Builder[string, counter]) {
			b.Initial("scanning")
			b.State("scanning").On[ping]().To("scanned")
			b.Final("scanned")
		})
		b.Final("ready")
	})
	require.NoError(t, err)

	// Every region is active from the start; the third is done already.
	snap, _ := m.Start(counter{N: 0}, epoch)
	require.Equal(t, []string{"upload", "uploading", "scan", "scanning", "ready"}, snap.Active())
	require.False(t, snap.Done())

	snap, _ = m.Transition(snap, tick{}, epoch)
	require.False(t, snap.Done())

	snap, effects := m.Transition(snap, ping{}, epoch)
	require.True(t, snap.Done())
	require.Equal(t, []string{"both/finished"}, names(effects))

	report, err := m.Explore(counter{N: 0}, sc.Exploration[string, counter]{Events: []any{tick{}, ping{}}})
	require.NoError(t, err)
	require.Empty(t, report.Trapped)
	require.Empty(t, report.Stuck)

	_, err = sc.New("confused", func(b *sc.Builder[string, counter]) {
		b.Initial("a")
		b.Regions()
		b.State("a")
	})
	require.ErrorContains(t, err, "a chart of regions has no initial state")
}

type verdict struct {
	Approved bool   `json:"approved"`
	By       string `json:"by"`
}

func TestAFinalStateReportsAnOutput(t *testing.T) {
	t.Parallel()

	m, err := sc.New("review", func(b *sc.Builder[string, counter]) {
		b.Initial("reviewing")

		reviewing := b.State("reviewing", func(b *sc.Builder[string, counter]) {
			b.Initial("open")
			open := b.State("open")
			open.On[tick]().To("approved")
			open.On[leave]().To("rejected")
			b.Final("approved").
				AssignOnEntry("count", func(c counter) counter { c.N++; return c }).
				Output(func(c counter) verdict { return verdict{Approved: true, By: "reviewer"} })
			b.Final("rejected").Output(func(counter) verdict { return verdict{Approved: false, By: "reviewer"} })
		})

		// One transition tells the final states apart by what they report.
		done := reviewing.OnDoneWith[verdict]()
		done.When("approved", func(_ counter, d sc.Done[verdict]) bool { return d.Result.Approved }).
			Do("record", func(context.Context, counter, sc.Done[verdict]) {}).
			To("merged")
		reviewing.OnDone().To("closed")

		b.Final("merged").Output(func(c counter) string { return "merged after 1 review" })
		b.Final("closed")
	})
	require.NoError(t, err)

	start, _ := m.Start(counter{N: 0}, epoch)
	_, has := m.Output(start)
	require.False(t, has)

	// The approving final state's output reaches the guard and the action,
	// and the machine's own output comes from the final state it ends in.
	merged, effects := m.Transition(start, tick{}, epoch)
	require.Equal(t, []string{"merged"}, merged.Active())
	require.Equal(t, sc.Done[verdict]{ID: "approved", Result: verdict{Approved: true, By: "reviewer"}}, effects[0].Event)

	output, has := m.Output(merged)
	require.True(t, has)
	require.Equal(t, "merged after 1 review", output)

	// The action's event survives being stored with its effect.
	data, err := m.EncodeEffect(effects[0])
	require.NoError(t, err)
	decoded, err := m.DecodeEffect(data)
	require.NoError(t, err)
	require.Equal(t, effects[0].Event, decoded.Event)

	// A final state without an output is not a machine output.
	closed, _ := m.Transition(start, leave{}, epoch)
	require.Equal(t, []string{"closed"}, closed.Active())
	_, has = m.Output(closed)
	require.False(t, has)
}
