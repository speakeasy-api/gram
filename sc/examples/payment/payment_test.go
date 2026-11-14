package payment_test

import (
	"context"
	"errors"
	"slices"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/sc"
	"github.com/speakeasy-api/gram/sc/examples/payment"
)

// unusedProvider stands in for the payment provider in tests that never
// run a service or an action.
type unusedProvider struct{}

func (unusedProvider) Charge(context.Context, string) (string, error) { return "", nil }
func (unusedProvider) Refund(context.Context, string)                 {}

var errTimeout = errors.New("timed out waiting for the provider")

// epoch is the time the tests start instances at.
var epoch = time.Date(2026, time.January, 1, 0, 0, 0, 0, time.UTC)

type (
	snapshot = sc.Snapshot[payment.State, payment.Order]
	step     = sc.Step[payment.State, payment.Order]
)

func newChart(t *testing.T) *sc.Machine[payment.State, payment.Order] {
	t.Helper()

	m, err := payment.New(unusedProvider{})
	require.NoError(t, err)

	return m
}

func refunds(effects []sc.Effect[payment.Order]) bool {
	return slices.ContainsFunc(effects, func(e sc.Effect[payment.Order]) bool { return e.Name == "refund" })
}

// chargeKey returns the idempotency key of the charge the effects start.
func chargeKey(t *testing.T, effects []sc.Effect[payment.Order]) (runID, key string) {
	t.Helper()

	for _, e := range effects {
		if e.Kind == sc.EffectStartInvocation && e.Name == "charge" {
			return e.ID, e.Context.IdempotencyKey()
		}
	}

	require.Fail(t, "no charge was started")

	return "", ""
}

// TestChartKeepsTheRulesOfTheModel explores every state the chart can reach
// and checks the chart's share of each rule in Payment.tla. The chart cannot
// see the card, so rules about the card become rules about what the chart
// asks for: which key a charge carries, and when a refund is requested.
func TestChartKeepsTheRulesOfTheModel(t *testing.T) {
	t.Parallel()

	report, err := newChart(t).Explore(payment.Order{ID: "o1"}, sc.Exploration[payment.State, payment.Order]{
		Events: []any{payment.Submit{}, payment.Cancel{}},
		Outcomes: map[string][]sc.Outcome{
			"charge": {sc.Succeeds("rcpt-1"), sc.Fails(payment.ErrDeclined), sc.Fails(errTimeout)},
		},
		Invariants: []sc.Invariant[payment.State, payment.Order]{
			{
				// PaidMeansCharged
				Name:  "a paid order has a receipt",
				Holds: func(s snapshot) bool { return !s.In(payment.Paid) || s.Context.Receipt != "" },
			},
			{
				// TypeOK: attempts \in 0..MaxAttempts
				Name:  "retries are bounded",
				Holds: func(s snapshot) bool { return s.Context.Attempts <= payment.MaxAttempts },
			},
			{
				// NoChargeKeptAfterCancel: the chart never gives up on
				// a charge whose outcome it does not know.
				Name:  "an order is never cancelled while a charge is unresolved",
				Holds: func(s snapshot) bool { return !s.In(payment.Cancelled) || !s.Context.Unresolved },
			},
		},
		Steps: []sc.StepInvariant[payment.State, payment.Order]{
			{
				// NoChargeKeptAfterCancel
				Name: "a charge that ends in cancelled is refunded",
				Holds: func(st step) bool {
					_, charged := st.Event.(sc.Done[string])
					return !charged || !st.To.In(payment.Cancelled) || refunds(st.Effects)
				},
			},
			{
				// PaidMeansCharged: the money of a paid order stays.
				Name:  "only cancelled orders are refunded",
				Holds: func(st step) bool { return !refunds(st.Effects) || st.To.In(payment.Cancelled) },
			},
			{
				// ChargedAtMostOnce: only a clear decline may lead to
				// a charge under a new key.
				Name: "the idempotency key changes only after a decline",
				Holds: func(st step) bool {
					failed, _ := st.Event.(sc.Failed)
					declined := errors.Is(failed.Err, payment.ErrDeclined)

					return declined || st.From.Context.IdempotencyKey() == st.To.Context.IdempotencyKey()
				},
			},
		},
	})
	require.NoError(t, err)

	require.Empty(t, report.Violations)
	require.False(t, report.Truncated)

	// Settles: nothing but a final state is left without a way forward.
	require.Empty(t, report.Stuck)

	for _, s := range []payment.State{payment.Idle, payment.Charging, payment.Retrying, payment.Paid, payment.Cancelled} {
		require.Truef(t, report.Reached(s), "state %q is never reached", s)
	}
}

// TestCancelDuringChargeRefundsAChargeThatStillSucceeds replays the trace
// TLC gives with CancelWaits = FALSE: Submit, Cancel, and only then
// WorkerRuns charging the card.
func TestCancelDuringChargeRefundsAChargeThatStillSucceeds(t *testing.T) {
	t.Parallel()

	m := newChart(t)

	snap, _ := m.Start(payment.Order{ID: "o1"}, epoch)
	snap, effects := m.Transition(snap, payment.Submit{}, epoch)
	runID, _ := chargeKey(t, effects)

	snap, effects = m.Transition(snap, payment.Cancel{}, epoch)
	require.Equal(t, []payment.State{payment.Charging}, snap.Active())
	require.Empty(t, effects)

	snap, effects = m.Transition(snap, sc.Done[string]{ID: runID, Result: "rcpt-1"}, epoch)

	require.Equal(t, []payment.State{payment.Cancelled}, snap.Active())
	require.True(t, refunds(effects))
	require.Empty(t, snap.Context.Receipt)
}

// TestChargeWithUnknownOutcomeIsAskedAgainUnderTheSameKey replays the trace
// TLC gives for ChargedAtMostOnce with UnknownKeepsKey = FALSE: Submit,
// WorkerRuns charging the card but losing the answer, ChargeUnknown,
// RetryTimerFired, and WorkerRuns charging again under a new key.
func TestChargeWithUnknownOutcomeIsAskedAgainUnderTheSameKey(t *testing.T) {
	t.Parallel()

	m := newChart(t)

	snap, _ := m.Start(payment.Order{ID: "o1"}, epoch)
	snap, effects := m.Transition(snap, payment.Submit{}, epoch)
	firstRun, firstKey := chargeKey(t, effects)

	snap, effects = m.Transition(snap, sc.Failed{ID: firstRun, Err: errTimeout}, epoch)
	require.Equal(t, []payment.State{payment.Retrying}, snap.Active())
	require.True(t, snap.Context.Unresolved)

	var timer sc.Effect[payment.Order]
	for _, e := range effects {
		if e.Kind == sc.EffectStartTimer {
			timer = e
		}
	}

	_, effects = m.Transition(snap, sc.TimerFired{ID: timer.ID}, epoch)
	secondRun, secondKey := chargeKey(t, effects)

	require.NotEqual(t, firstRun, secondRun)
	require.Equal(t, firstKey, secondKey)
}

// TestCancelWhileOutcomeIsUnknownWaitsThenRefunds replays the trace TLC
// gives for NoChargeKeptAfterCancel with UnknownKeepsKey = FALSE: Submit,
// Cancel, WorkerRuns charging the card but losing the answer, ChargeUnknown.
func TestCancelWhileOutcomeIsUnknownWaitsThenRefunds(t *testing.T) {
	t.Parallel()

	m := newChart(t)

	snap, _ := m.Start(payment.Order{ID: "o1"}, epoch)
	snap, effects := m.Transition(snap, payment.Submit{}, epoch)
	runID, _ := chargeKey(t, effects)

	snap, _ = m.Transition(snap, payment.Cancel{}, epoch)
	snap, effects = m.Transition(snap, sc.Failed{ID: runID, Err: errTimeout}, epoch)
	require.Equal(t, []payment.State{payment.Retrying}, snap.Active())

	var timer sc.Effect[payment.Order]
	for _, e := range effects {
		if e.Kind == sc.EffectStartTimer {
			timer = e
		}
	}

	snap, effects = m.Transition(snap, sc.TimerFired{ID: timer.ID}, epoch)
	runID, _ = chargeKey(t, effects)

	// The provider repeats the outcome it decided the first time.
	snap, effects = m.Transition(snap, sc.Done[string]{ID: runID, Result: "rcpt-1"}, epoch)

	require.Equal(t, []payment.State{payment.Cancelled}, snap.Active())
	require.True(t, refunds(effects))
}

func TestCancelBetweenAttemptsIsImmediate(t *testing.T) {
	t.Parallel()

	m := newChart(t)

	snap, _ := m.Start(payment.Order{ID: "o1"}, epoch)
	snap, effects := m.Transition(snap, payment.Submit{}, epoch)
	runID, _ := chargeKey(t, effects)
	snap, _ = m.Transition(snap, sc.Failed{ID: runID, Err: payment.ErrDeclined}, epoch)
	require.Equal(t, []payment.State{payment.Retrying}, snap.Active())

	snap, effects = m.Transition(snap, payment.Cancel{}, epoch)

	require.Equal(t, []payment.State{payment.Cancelled}, snap.Active())
	require.False(t, refunds(effects))
}

func TestOnlyADeclineStartsANewIdempotencyKey(t *testing.T) {
	t.Parallel()

	first := payment.Order{ID: "o1", Attempts: 0}
	second := payment.Order{ID: "o1", Attempts: 1}
	unresolved := payment.Order{ID: "o1", Attempts: 0, Unresolved: true}

	require.Equal(t, "o1/0", first.IdempotencyKey())
	require.NotEqual(t, first.IdempotencyKey(), second.IdempotencyKey())
	require.Equal(t, first.IdempotencyKey(), unresolved.IdempotencyKey())
}
