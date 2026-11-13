package sc_test

import (
	"context"
	"errors"
	"slices"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/sc"
)

type state string

const (
	idle       state = "idle"
	paying     state = "paying"
	charging   state = "charging"
	retrying   state = "retrying"
	payingHist state = "paying_history"
	onHold     state = "on_hold"
	fulfilling state = "fulfilling"
	packing    state = "packing"
	picking    state = "picking"
	packed     state = "packed"
	invoicing  state = "invoicing"
	drafting   state = "drafting"
	sent       state = "sent"
	shipped    state = "shipped"
	cancelled  state = "cancelled"
)

type order struct {
	Items    []string `json:"items"`
	Attempts int      `json:"attempts"`
	Receipt  string   `json:"receipt"`
	Notes    []string `json:"notes"`
}

type (
	submit      struct{ Items []string }
	cancel      struct{}
	hold        struct{}
	resume      struct{}
	itemsPicked struct{}
	invoiceSent struct{}
	note        struct{ Text string }
)

// retryDelay is how long the order chart waits in retrying before it charges
// again. Tests only compare against it.
const retryDelay = 2 * time.Second

// maxChargeAttempts is how many failed charges the order chart retries before
// it cancels the order.
const maxChargeAttempts = 2

var errCardDeclined = errors.New("card declined")

// epoch is the time the tests start instances at. It is before every fixed
// date the tests use, so no deadline in them is already past.
var epoch = time.Date(2026, time.January, 1, 0, 0, 0, 0, time.UTC)

// orderChart is the chart most tests drive. charge is the payment service.
func orderChart(t *testing.T, charge func(context.Context, order) (string, error), opts ...sc.Option) *sc.Machine[state, order] {
	t.Helper()

	m, err := sc.New("order", func(b *sc.Builder[state, order]) {
		b.Initial(idle)

		b.State(idle).On[submit]().
			When("hasItems", func(_ order, e submit) bool { return len(e.Items) > 0 }).
			Assign("setItems", func(o order, e submit) order { o.Items = e.Items; return o }).
			Do("announce", func(context.Context, order, submit) {}).
			To(paying)

		pay := b.State(paying, func(b *sc.Builder[state, order]) {
			b.Initial(charging)
			b.History(payingHist, sc.Deep)

			inv := b.State(charging).Invoke("charge", charge)
			inv.OnDone().
				Assign("setReceipt", func(o order, d sc.Done[string]) order { o.Receipt = d.Result; return o }).
				To(fulfilling)
			inv.OnError().
				When("canRetry", func(o order, _ sc.Failed) bool { return o.Attempts < maxChargeAttempts }).
				To(retrying)
			inv.OnError().To(cancelled)

			b.State(retrying).After(retryDelay).
				Assign("countAttempt", func(o order, _ struct{}) order { o.Attempts++; return o }).
				To(charging)
		})
		pay.On[cancel]().To(cancelled)
		pay.On[hold]().To(onHold)
		pay.On[note]().Assign("addNote", func(o order, e note) order {
			o.Notes = append(append([]string(nil), o.Notes...), e.Text)
			return o
		})

		b.State(onHold).On[resume]().To(payingHist)

		b.Parallel(fulfilling, func(b *sc.Builder[state, order]) {
			b.State(packing, func(b *sc.Builder[state, order]) {
				b.Initial(picking)
				b.State(picking).On[itemsPicked]().To(packed)
				b.Final(packed)
			})
			b.State(invoicing, func(b *sc.Builder[state, order]) {
				b.Initial(drafting)
				b.State(drafting).On[invoiceSent]().To(sent)
				b.Final(sent)
			})
		}).OnDone().To(shipped)

		b.Final(shipped)
		b.Final(cancelled).Entry("refund", func(context.Context, order) {})
	}, opts...)
	require.NoError(t, err)

	return m
}

func chargeOK(context.Context, order) (string, error) { return "rcpt-1", nil }

// effect finds the first effect of the given kind.
func effect[C any](t *testing.T, effects []sc.Effect[C], kind sc.EffectKind) sc.Effect[C] {
	t.Helper()

	for _, e := range effects {
		if e.Kind == kind {
			return e
		}
	}

	require.Failf(t, "missing effect", "no %s effect in %v", kind, effects)

	return sc.Effect[C]{}
}

func kinds[C any](effects []sc.Effect[C]) []sc.EffectKind {
	out := make([]sc.EffectKind, 0, len(effects))
	for _, e := range effects {
		out = append(out, e.Kind)
	}

	return out
}

// submitted returns the order chart after a submit, with the ID of the
// running charge.
func submitted(t *testing.T, m *sc.Machine[state, order]) (sc.Snapshot[state, order], string) {
	t.Helper()

	snap, _ := m.Start(order{}, epoch)
	snap, effects := m.Transition(snap, submit{Items: []string{"book"}}, epoch)
	require.Equal(t, []state{paying, charging}, snap.Active())

	return snap, effect(t, effects, sc.EffectStartInvocation).ID
}

func TestStartEntersInitialState(t *testing.T) {
	t.Parallel()

	snap, effects := orderChart(t, chargeOK).Start(order{}, epoch)

	require.Equal(t, []state{idle}, snap.Active())
	require.Empty(t, effects)
	require.False(t, snap.Done())
	require.Zero(t, snap.Seq())
}

func TestTransitionRunsStepsInOrderAndStartsService(t *testing.T) {
	t.Parallel()

	m := orderChart(t, chargeOK)
	snap, _ := m.Start(order{}, epoch)

	snap, effects := m.Transition(snap, submit{Items: []string{"book"}}, epoch)

	require.Equal(t, []state{paying, charging}, snap.Active())
	require.True(t, snap.In(paying))
	require.Equal(t, []string{"book"}, snap.Context.Items)
	require.Equal(t, uint64(1), snap.Seq())
	require.Equal(t, []sc.EffectKind{sc.EffectAction, sc.EffectStartInvocation}, kinds(effects))

	// The action is declared after the assignment, so it sees its result.
	require.Equal(t, "announce", effects[0].Name)
	require.Equal(t, "idle", effects[0].State)
	require.Equal(t, []string{"book"}, effects[0].Context.Items)
	require.Equal(t, submit{Items: []string{"book"}}, effects[0].Event)

	require.Equal(t, "charge", effects[1].Name)
	require.Equal(t, "charging", effects[1].State)
}

func TestGuardBlocksTransition(t *testing.T) {
	t.Parallel()

	m := orderChart(t, chargeOK)
	snap, _ := m.Start(order{}, epoch)

	require.False(t, m.Can(snap, submit{}, epoch))
	require.True(t, m.Can(snap, submit{Items: []string{"book"}}, epoch))

	next, effects := m.Transition(snap, submit{}, epoch)

	require.Equal(t, []state{idle}, next.Active())
	require.Empty(t, effects)
}

func TestUnhandledEventLeavesStateUnchanged(t *testing.T) {
	t.Parallel()

	m := orderChart(t, chargeOK)
	snap, _ := m.Start(order{}, epoch)

	next, effects := m.Transition(snap, resume{}, epoch)

	require.Equal(t, snap.Active(), next.Active())
	require.Empty(t, effects)
}

func TestParentHandlesEventForEveryChild(t *testing.T) {
	t.Parallel()

	m := orderChart(t, chargeOK)
	snap, chargeID := submitted(t, m)

	snap, effects := m.Transition(snap, cancel{}, epoch)

	require.Equal(t, []state{cancelled}, snap.Active())
	require.True(t, snap.Done())

	// Leaving charging stops its service before the final state's entry
	// action runs.
	require.Equal(t, []sc.EffectKind{sc.EffectStopInvocation, sc.EffectAction}, kinds(effects))
	require.Equal(t, chargeID, effects[0].ID)
	require.Equal(t, "refund", effects[1].Name)
}

func TestTargetlessTransitionKeepsActiveStatesAndService(t *testing.T) {
	t.Parallel()

	m := orderChart(t, chargeOK)
	snap, _ := submitted(t, m)

	snap, effects := m.Transition(snap, note{Text: "gift"}, epoch)

	require.Equal(t, []state{paying, charging}, snap.Active())
	require.Equal(t, []string{"gift"}, snap.Context.Notes)
	require.Empty(t, effects)
}

func TestServiceResultIsRouted(t *testing.T) {
	t.Parallel()

	m := orderChart(t, chargeOK)
	snap, chargeID := submitted(t, m)

	snap, effects := m.Transition(snap, sc.Done[string]{ID: chargeID, Result: "rcpt-9"}, epoch)

	require.Equal(t, "rcpt-9", snap.Context.Receipt)
	require.Equal(t, []state{fulfilling, packing, picking, invoicing, drafting}, snap.Active())
	require.Empty(t, effects)
}

func TestServiceFailureRetriesAfterDelayThenCancels(t *testing.T) {
	t.Parallel()

	m := orderChart(t, chargeOK)
	snap, chargeID := submitted(t, m)

	for attempt := range maxChargeAttempts {
		var effects []sc.Effect[order]
		snap, effects = m.Transition(snap, sc.Failed{ID: chargeID, Err: errCardDeclined}, epoch)
		require.Equal(t, []state{paying, retrying}, snap.Active())

		timer := effect(t, effects, sc.EffectStartTimer)
		require.Equal(t, retryDelay, timer.Delay)

		snap, effects = m.Transition(snap, sc.TimerFired{ID: timer.ID}, epoch)
		require.Equal(t, []state{paying, charging}, snap.Active())
		require.Equal(t, attempt+1, snap.Context.Attempts)

		chargeID = effect(t, effects, sc.EffectStartInvocation).ID
	}

	snap, _ = m.Transition(snap, sc.Failed{ID: chargeID, Err: errCardDeclined}, epoch)

	require.Equal(t, []state{cancelled}, snap.Active())
	require.True(t, snap.Done())
}

func TestResultOfStoppedServiceIsIgnored(t *testing.T) {
	t.Parallel()

	m := orderChart(t, chargeOK)
	snap, chargeID := submitted(t, m)
	snap, _ = m.Transition(snap, hold{}, epoch)
	require.Equal(t, []state{onHold}, snap.Active())

	next, effects := m.Transition(snap, sc.Done[string]{ID: chargeID, Result: "late"}, epoch)

	require.Equal(t, snap.Seq(), next.Seq())
	require.Equal(t, []state{onHold}, next.Active())
	require.Empty(t, next.Context.Receipt)
	require.Empty(t, effects)
}

func TestLeavingStateCancelsTimerAndLateFireIsIgnored(t *testing.T) {
	t.Parallel()

	m := orderChart(t, chargeOK)
	snap, chargeID := submitted(t, m)
	snap, effects := m.Transition(snap, sc.Failed{ID: chargeID, Err: errCardDeclined}, epoch)
	timerID := effect(t, effects, sc.EffectStartTimer).ID

	snap, effects = m.Transition(snap, hold{}, epoch)
	require.Equal(t, timerID, effect(t, effects, sc.EffectCancelTimer).ID)

	next, effects := m.Transition(snap, sc.TimerFired{ID: timerID}, epoch)

	require.Equal(t, []state{onHold}, next.Active())
	require.Empty(t, effects)
}

func TestDeepHistoryRestoresLeafState(t *testing.T) {
	t.Parallel()

	m := orderChart(t, chargeOK)
	snap, chargeID := submitted(t, m)
	snap, _ = m.Transition(snap, sc.Failed{ID: chargeID, Err: errCardDeclined}, epoch)
	require.Equal(t, []state{paying, retrying}, snap.Active())

	snap, _ = m.Transition(snap, hold{}, epoch)
	snap, effects := m.Transition(snap, resume{}, epoch)

	require.Equal(t, []state{paying, retrying}, snap.Active())
	require.Equal(t, []sc.EffectKind{sc.EffectStartTimer}, kinds(effects))
}

func TestParallelRegionsAdvanceIndependentlyAndCompleteTogether(t *testing.T) {
	t.Parallel()

	m := orderChart(t, chargeOK)
	snap, chargeID := submitted(t, m)
	snap, _ = m.Transition(snap, sc.Done[string]{ID: chargeID, Result: "rcpt-1"}, epoch)

	snap, _ = m.Transition(snap, invoiceSent{}, epoch)
	require.Equal(t, []state{fulfilling, packing, picking, invoicing, sent}, snap.Active())
	require.False(t, snap.Done())

	snap, _ = m.Transition(snap, itemsPicked{}, epoch)

	require.Equal(t, []state{shipped}, snap.Active())
	require.True(t, snap.Done())
}

func TestDoneMachineIgnoresEvents(t *testing.T) {
	t.Parallel()

	m := orderChart(t, chargeOK)
	snap, _ := submitted(t, m)
	snap, _ = m.Transition(snap, cancel{}, epoch)
	require.True(t, snap.Done())

	next, effects := m.Transition(snap, submit{Items: []string{"book"}}, epoch)

	require.Equal(t, snap.Seq(), next.Seq())
	require.Empty(t, effects)
	require.False(t, m.Can(snap, submit{Items: []string{"book"}}, epoch))
}

func TestTransitionDoesNotModifyItsInput(t *testing.T) {
	t.Parallel()

	m := orderChart(t, chargeOK)
	before, _ := submitted(t, m)

	_, _ = m.Transition(before, hold{}, epoch)

	require.Equal(t, []state{paying, charging}, before.Active())
	require.Equal(t, uint64(1), before.Seq())
}

func TestFoldReplaysEvents(t *testing.T) {
	t.Parallel()

	m := orderChart(t, chargeOK)
	snap, _ := m.Start(order{}, epoch)

	events := []sc.Timed{
		{At: epoch, Event: submit{Items: []string{"book"}}},
		{At: epoch.Add(time.Minute), Event: hold{}},
		{At: epoch.Add(2 * time.Minute), Event: resume{}},
	}

	var trail [][]state
	for next := range m.Fold(snap, slices.Values(events)) {
		trail = append(trail, next.Active())
	}

	require.Equal(t, [][]state{{paying, charging}, {onHold}, {paying, charging}}, trail)
}
