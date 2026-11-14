package sc_test

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/sc"
)

// expiry is the context of the charts that compare stored times with now.
type expiry struct {
	ExpiresAt time.Time `json:"expires_at"`
	SeenAt    time.Time `json:"seen_at"`
}

type accept struct{}

// invitationChart is pending until its expiry time, then expired. Accepting
// is allowed only before the expiry.
func invitationChart(t *testing.T) *sc.Machine[string, expiry] {
	t.Helper()

	m, err := sc.New("invitation", func(b *sc.Builder[string, expiry]) {
		b.Initial("pending")
		pending := b.State("pending")
		pending.Deadline("expiresAt", func(c expiry) time.Time { return c.ExpiresAt }).To("expired")
		pending.On[accept]().
			WhenAt("notExpired", func(c expiry, _ accept, now time.Time) bool { return now.Before(c.ExpiresAt) }).
			AssignAt("recordAcceptance", func(c expiry, _ accept, now time.Time) expiry { c.SeenAt = now; return c }).
			To("accepted")
		b.Final("expired")
		b.Final("accepted")
	})
	require.NoError(t, err)

	return m
}

func TestAdvanceShowsAnExpiryNothingHasProcessed(t *testing.T) {
	t.Parallel()

	m := invitationChart(t)
	snap, _ := m.Start(expiry{ExpiresAt: epoch.Add(7 * 24 * time.Hour)}, epoch)

	early, effects := m.Advance(snap, epoch.Add(time.Hour))
	require.Equal(t, snap, early)
	require.Nil(t, effects)

	late, _ := m.Advance(snap, epoch.Add(8*24*time.Hour))
	require.Equal(t, []string{"expired"}, late.Active())
}

func TestAnEventAfterADeadlineSeesTheDeadlineFireFirst(t *testing.T) {
	t.Parallel()

	m := invitationChart(t)
	snap, _ := m.Start(expiry{ExpiresAt: epoch.Add(time.Hour)}, epoch)

	require.True(t, m.Can(snap, accept{}, epoch.Add(time.Minute)))
	require.False(t, m.Can(snap, accept{}, epoch.Add(2*time.Hour)))

	accepted, _ := m.Transition(snap, accept{}, epoch.Add(time.Minute))
	require.Equal(t, []string{"accepted"}, accepted.Active())
	require.Equal(t, epoch.Add(time.Minute), accepted.Context.SeenAt)

	expired, _ := m.Transition(snap, accept{}, epoch.Add(2*time.Hour))
	require.Equal(t, []string{"expired"}, expired.Active())
}

type obtained struct{}

// renewalChart reproduces the ordering the certificate port depended on: a
// fresh certificate's window must be read before it is renewed, and both
// deadlines are already past when the state is entered.
func renewalChart(t *testing.T) *sc.Machine[string, expiry] {
	t.Helper()

	m, err := sc.New("renewal", func(b *sc.Builder[string, expiry]) {
		b.Initial("waiting")
		b.State("waiting").On[obtained]().To("serving")
		serving := b.State("serving")
		serving.Deadline("windowCheckAt", func(expiry) time.Time { return time.Time{} }).To("reading_window")
		serving.Deadline("renewAt", func(expiry) time.Time { return time.Time{} }).To("renewing")
		b.State("reading_window")
		b.State("renewing")
	})
	require.NoError(t, err)

	return m
}

func TestTimersDueTogetherFireInTheOrderTheyWereDeclared(t *testing.T) {
	t.Parallel()

	m := renewalChart(t)

	for range 50 {
		snap, _ := m.Start(expiry{}, epoch)
		snap, effects := m.Transition(snap, obtained{}, epoch)

		require.Equal(t, []string{"reading_window"}, snap.Active())
		require.Empty(t, effects)
	}
}

func TestTimersFireInTheOrderTheyFallDueAndSeeTheirOwnTime(t *testing.T) {
	t.Parallel()

	m, err := sc.New("ordered", func(b *sc.Builder[string, expiry]) {
		b.Initial("a")
		a := b.State("a")
		a.After(2 * time.Hour).To("late")
		a.After(time.Hour).
			AssignAt("record", func(c expiry, _ struct{}, now time.Time) expiry { c.SeenAt = now; return c }).
			To("early")
		b.State("early")
		b.State("late")
	})
	require.NoError(t, err)

	snap, _ := m.Start(expiry{}, epoch)
	snap, _ = m.Advance(snap, epoch.Add(3*time.Hour))

	require.Equal(t, []string{"early"}, snap.Active())
	require.Equal(t, epoch.Add(time.Hour), snap.Context.SeenAt)
}

func TestJitteredTimerWaitsForItsDriverUntilItsWindowEnds(t *testing.T) {
	t.Parallel()

	m, err := sc.New("jittered", func(b *sc.Builder[string, expiry]) {
		b.Initial("a")
		b.State("a").After(time.Hour).Jitter(0.1).To("b")
		b.Final("b")
	})
	require.NoError(t, err)

	snap, effects := m.Start(expiry{}, epoch)
	timer := effect(t, effects, sc.EffectStartTimer)

	// Inside the window only the driver fires it; past the window the core
	// does, so a timer is never lost under a clock that has no driver.
	unchanged, _ := m.Advance(snap, epoch.Add(65*time.Minute))
	require.Equal(t, snap, unchanged)

	late, _ := m.Advance(snap, epoch.Add(2*time.Hour))
	require.True(t, late.Done())

	fired, _ := m.Transition(snap, sc.TimerFired{ID: timer.ID}, epoch.Add(62*time.Minute))
	require.True(t, fired.Done())
}

func TestResumeSchedulesTimersForTheTimeTheyAreDue(t *testing.T) {
	t.Parallel()

	m := backoffChart(t)
	snap, _ := m.Start(attempts{}, epoch)

	data, err := json.Marshal(snap)
	require.NoError(t, err)
	restored, _, err := m.Restore(data, epoch.Add(30*time.Minute))
	require.NoError(t, err)

	resumed := m.Resume(restored)
	require.Len(t, resumed, 1)
	require.Equal(t, epoch.Add(time.Second), resumed[0].At)
	require.Zero(t, resumed[0].Wait(epoch.Add(30*time.Minute), func() float64 { return 0 }))
}

func TestRestoreStartsATimerRecordedWithoutADueTimeAfresh(t *testing.T) {
	t.Parallel()

	m := backoffChart(t)
	snap, _ := m.Start(attempts{}, epoch)

	data, err := json.Marshal(snap)
	require.NoError(t, err)

	var raw sc.RawSnapshot
	require.NoError(t, json.Unmarshal(data, &raw))
	raw.Timers[0].Due = time.Time{}
	data, err = json.Marshal(raw)
	require.NoError(t, err)

	restoredAt := epoch.Add(time.Hour)
	restored, _, err := m.Restore(data, restoredAt)
	require.NoError(t, err)

	require.Equal(t, restoredAt.Add(time.Second), restored.Timers()[0].Due)
}

func TestExploreWaitsLetAnEventArriveAfterAnExpiry(t *testing.T) {
	t.Parallel()

	m := invitationChart(t)
	start := time.Date(2030, time.March, 1, 0, 0, 0, 0, time.UTC)

	report, err := m.Explore(expiry{ExpiresAt: start.Add(time.Hour)}, sc.Exploration[string, expiry]{
		Start:  start,
		Events: []any{accept{}},
		Waits:  []time.Duration{2 * time.Hour},
		Key: func(c expiry, now time.Time) any {
			return struct {
				Left time.Duration `json:"left"`
				Seen bool          `json:"seen"`
			}{max(c.ExpiresAt.Sub(now), 0), !c.SeenAt.IsZero()}
		},
		Invariants: []sc.Invariant[string, expiry]{{
			Name: "accepted only before expiry",
			Holds: func(s sc.Snapshot[string, expiry]) bool {
				return !s.In("accepted") || s.Context.SeenAt.Before(s.Context.ExpiresAt)
			},
		}},
	})
	require.NoError(t, err)

	require.Empty(t, report.Violations)
	require.True(t, report.Reached("accepted"))
	require.True(t, report.Reached("expired"))
	require.Empty(t, report.Trapped)
}

func TestTimersThatKeepFiringAtOnceAreReported(t *testing.T) {
	t.Parallel()

	m, err := sc.New("loop", func(b *sc.Builder[string, expiry]) {
		b.Initial("a")
		b.State("a").Deadline("past", func(expiry) time.Time { return time.Time{} }).To("a").Reenter()
	})
	require.NoError(t, err)

	require.PanicsWithValue(t, `sc: machine "loop" does not settle within 10000 steps: its timers keep firing at `+epoch.String()+`, in states [a]`, func() {
		m.Start(expiry{}, epoch)
	})
}

func TestCheckDeterminismCatchesAGuardThatReadsTheWallClock(t *testing.T) {
	t.Parallel()

	calls := 0
	m, err := sc.New("leaky", func(b *sc.Builder[string, expiry]) {
		b.Initial("a")
		// A guard with state of its own answers differently each time,
		// as one reading time.Now would.
		b.State("a").On[accept]().
			When("alternates", func(expiry, accept) bool { calls++; return calls%2 == 0 }).
			To("b")
		b.Final("b")
	})
	require.NoError(t, err)

	report, err := m.Explore(expiry{}, sc.Exploration[string, expiry]{Events: []any{accept{}}, CheckDeterminism: true})
	require.NoError(t, err)

	require.Len(t, report.Violations, 1)
	require.Equal(t, "deterministic", report.Violations[0].Invariant)
}

func TestStartInPlacesAnInstanceInAStateWithItsTimersAndServices(t *testing.T) {
	t.Parallel()

	m := orderChart(t, chargeOK)

	snap, effects, err := m.StartIn(order{Items: []string{"book"}, Attempts: 1}, epoch, retrying)
	require.NoError(t, err)
	require.Equal(t, []state{paying, retrying}, snap.Active())
	require.Equal(t, []sc.EffectKind{sc.EffectStartTimer}, kinds(effects))

	snap, effects, err = m.StartIn(order{}, epoch, charging)
	require.NoError(t, err)
	require.Equal(t, []sc.EffectKind{sc.EffectStartInvocation}, kinds(effects))
	require.Len(t, snap.Services(), 1)

	// A state with children stands for the states entering it leads to,
	// and the regions it does not name get their default entry.
	snap, _, err = m.StartIn(order{}, epoch, packing)
	require.NoError(t, err)
	require.True(t, snap.In(packing))

	_, _, err = m.StartIn(order{}, epoch, paying, onHold)
	require.ErrorIs(t, err, sc.ErrInvalidSnapshot)

	_, _, err = m.StartIn(order{}, epoch, picking, invoicing, drafting)
	require.NoError(t, err)
}
