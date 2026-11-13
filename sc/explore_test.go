package sc_test

import (
	"context"
	"iter"
	"slices"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/sc"
)

// orderEnvironment is everything that can happen to an order: the events its
// users send and both ways its charge can end.
func orderEnvironment() sc.Exploration[state, order] {
	return sc.Exploration[state, order]{
		Events: []any{
			submit{Items: []string{"book"}},
			cancel{},
			hold{},
			resume{},
			itemsPicked{},
			invoiceSent{},
		},
		Outcomes: map[string][]sc.Outcome{
			"charge": {sc.Succeeds("rcpt-1"), sc.Fails(errCardDeclined)},
		},
	}
}

func TestExploreChecksInvariantsInEveryReachableState(t *testing.T) {
	t.Parallel()

	x := orderEnvironment()
	x.Invariants = []sc.Invariant[state, order]{
		{
			Name: "fulfilment only after payment",
			Holds: func(s sc.Snapshot[state, order]) bool {
				paid := s.Context.Receipt != ""
				return paid || (!s.In(fulfilling) && !s.In(shipped))
			},
		},
		{
			Name: "retries are bounded",
			Holds: func(s sc.Snapshot[state, order]) bool {
				return s.Context.Attempts <= maxChargeAttempts
			},
		},
	}
	x.Steps = []sc.StepInvariant[state, order]{{
		Name: "refund only when cancelling",
		Holds: func(step sc.Step[state, order]) bool {
			refunds := slices.ContainsFunc(step.Effects, func(e sc.Effect[order]) bool { return e.Name == "refund" })
			return !refunds || step.To.In(cancelled)
		},
	}}

	report, err := orderChart(t, chargeOK).Explore(order{}, x)
	require.NoError(t, err)

	require.Empty(t, report.Violations)
	require.Empty(t, report.Stuck)
	require.False(t, report.Truncated)
	require.Greater(t, report.States, 10)

	for _, s := range []state{idle, charging, retrying, onHold, packed, sent, shipped, cancelled} {
		require.Truef(t, report.Reached(s), "state %q was never reached", s)
	}
}

func TestExploreReportsShortestTraceToAViolation(t *testing.T) {
	t.Parallel()

	x := orderEnvironment()
	x.Invariants = []sc.Invariant[state, order]{{
		Name:  "orders are never put on hold while retrying",
		Holds: func(s sc.Snapshot[state, order]) bool { return !s.In(onHold) || s.Context.Attempts == 0 },
	}}

	m := orderChart(t, chargeOK)
	report, err := m.Explore(order{}, x)
	require.NoError(t, err)

	require.Len(t, report.Violations, 1)
	violation := report.Violations[0]
	require.Equal(t, "orders are never put on hold while retrying", violation.Invariant)

	// Submit, fail the charge, wait out the retry, hold: no shorter
	// sequence puts an order with a counted attempt on hold.
	require.Len(t, violation.Trace.Events, 4)
	require.IsType(t, submit{}, violation.Trace.Events[0].Event)
	require.IsType(t, sc.Failed{}, violation.Trace.Events[1].Event)
	require.IsType(t, sc.TimerFired{}, violation.Trace.Events[2].Event)
	require.IsType(t, hold{}, violation.Trace.Events[3].Event)

	// The retry timer fires when it is due.
	require.Equal(t, retryDelay, violation.Trace.Events[2].At.Sub(violation.Trace.Start))

	// The trace replays to the reported state.
	replayed, _ := m.Start(order{}, violation.Trace.Start)
	for snap := range m.Fold(replayed, slices.Values(violation.Trace.Events)) {
		replayed = snap
	}
	require.Equal(t, violation.Trace.Snapshot, replayed)
}

func TestExploreReportsStepViolationWithTheOffendingEventLast(t *testing.T) {
	t.Parallel()

	x := orderEnvironment()
	x.Steps = []sc.StepInvariant[state, order]{{
		Name: "nothing is announced",
		Holds: func(step sc.Step[state, order]) bool {
			return !slices.ContainsFunc(step.Effects, func(e sc.Effect[order]) bool { return e.Name == "announce" })
		},
	}}

	report, err := orderChart(t, chargeOK).Explore(order{}, x)
	require.NoError(t, err)

	require.Len(t, report.Violations, 1)
	require.Len(t, report.Violations[0].Trace.Events, 1)
	require.Equal(t, submit{Items: []string{"book"}}, report.Violations[0].Trace.Events[0].Event)
	require.True(t, report.Violations[0].Trace.Snapshot.In(charging))
}

func TestExploreReportsStatesNothingMovesOutOf(t *testing.T) {
	t.Parallel()

	// A charge with no outcomes models a payment provider that never
	// answers, and without cancel or hold nothing else can move the order.
	x := sc.Exploration[state, order]{Events: []any{submit{Items: []string{"book"}}}}

	report, err := orderChart(t, chargeOK).Explore(order{}, x)
	require.NoError(t, err)

	require.Len(t, report.Stuck, 1)
	require.Equal(t, []state{paying, charging}, report.Stuck[0].Snapshot.Active())
	require.False(t, report.Reached(fulfilling))
}

func TestExploreStopsAtMaxStatesWhenTheContextIsUnbounded(t *testing.T) {
	t.Parallel()

	m, err := sc.New("unbounded", func(b *sc.Builder[string, counter]) {
		b.Initial("counting")
		b.State("counting").On[tick]().Assign("increment", increment[tick])
	})
	require.NoError(t, err)

	// limit is small so the test is fast; any value shows the cut-off.
	const limit = 50

	report, err := m.Explore(counter{}, sc.Exploration[string, counter]{Events: []any{tick{}}, MaxStates: limit})
	require.NoError(t, err)

	require.True(t, report.Truncated)
	require.Equal(t, limit, report.States)
}

func TestExploreFollowsStreamEvents(t *testing.T) {
	t.Parallel()

	m, err := sc.New("stream", func(b *sc.Builder[string, counter]) {
		b.Initial("listening")
		listening := b.State("listening")
		listening.Stream("ticks", func(context.Context, counter) iter.Seq2[any, error] {
			return func(func(any, error) bool) {}
		}).OnError().To("stopped")
		listening.On[tick]().
			When("belowThree", func(c counter, _ tick) bool { return c.N < 3 }).
			Assign("increment", increment[tick])
		listening.Always().
			When("reachedThree", func(c counter, _ struct{}) bool { return c.N >= 3 }).
			To("stopped")
		b.Final("stopped")
	})
	require.NoError(t, err)

	report, err := m.Explore(counter{}, sc.Exploration[string, counter]{
		Outcomes: map[string][]sc.Outcome{"ticks": {sc.Emits(tick{})}},
		Invariants: []sc.Invariant[string, counter]{{
			Name:  "never counts past three",
			Holds: func(s sc.Snapshot[string, counter]) bool { return s.Context.N <= 3 },
		}},
	})
	require.NoError(t, err)

	require.Empty(t, report.Violations)
	require.True(t, report.Reached("stopped"))
	require.Equal(t, 4, report.States)
}
