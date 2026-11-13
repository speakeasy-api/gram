package pgdriver_test

import (
	"context"
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/sc"
	"github.com/speakeasy-api/gram/sc/pgdriver"
)

// reminder is the context of the deadline chart: when to wake.
type reminder struct {
	At time.Time `json:"at"`
}

func deadlineDriver(t *testing.T) *pgdriver.Driver[string, reminder] {
	t.Helper()

	m, err := sc.New(machineID(t), func(b *sc.Builder[string, reminder]) {
		b.Initial("waiting")
		b.State("waiting").Deadline("wakeAt", func(c reminder) time.Time { return c.At }).To("woken")
		b.Final("woken")
	})
	require.NoError(t, err)

	return pgdriver.New(pool, m)
}

func TestDeadlineTimerIsDueAtItsOwnTime(t *testing.T) {
	t.Parallel()

	d := deadlineDriver(t)
	later := time.Now().Add(time.Hour).UTC().Truncate(time.Microsecond)

	_, err := d.Create(t.Context(), "later", reminder{At: later})
	require.NoError(t, err)
	_, err = d.Create(t.Context(), "overdue", reminder{At: time.Now().Add(-time.Minute)})
	require.NoError(t, err)

	drain(t, d)

	overdue, err := d.Load(t.Context(), "overdue")
	require.NoError(t, err)
	require.True(t, overdue.Done())

	waiting, err := d.Load(t.Context(), "later")
	require.NoError(t, err)
	require.False(t, waiting.Done())

	var runAfter time.Time
	err = pool.QueryRow(t.Context(),
		`SELECT run_after FROM statechart_effects WHERE machine = $1 AND instance_id = 'later'`,
		machineID(t),
	).Scan(&runAfter)
	require.NoError(t, err)
	require.True(t, later.Equal(runAfter), "run_after %s, want %s", runAfter, later)
}

// hookLog collects what the driver's hooks were told.
type hookLog struct {
	mu    sync.Mutex
	calls []string
}

func (l *hookLog) add(format string, args ...any) {
	l.mu.Lock()
	defer l.mu.Unlock()

	l.calls = append(l.calls, fmt.Sprintf(format, args...))
}

func TestDriverReportsCommittedStepsAndEffectsToItsHooks(t *testing.T) {
	t.Parallel()

	m, err := sc.New(machineID(t), func(b *sc.Builder[string, order]) {
		b.Initial("idle")
		b.State("idle").On[submit]().Do("announce", func(context.Context, order, submit) {}).To("charging")
		charge := b.State("charging").Invoke("charge", func(context.Context, order) (string, error) { return "", errCardDeclined })
		charge.OnDone().To("paid")
		charge.OnError().To("declined")
		b.Final("paid")
		b.Final("declined")
	})
	require.NoError(t, err)

	log := &hookLog{mu: sync.Mutex{}, calls: nil}
	d := pgdriver.New(pool, m, pgdriver.WithHooks(&sc.Hooks[string, order]{
		Transition: func(_ context.Context, step sc.Step[string, order]) {
			log.add("transition %s to %v", m.EventName(step.Event), step.To.Active())
		},
		Ignored: func(_ context.Context, _ sc.Snapshot[string, order], event any) {
			log.add("ignored %s", m.EventName(event))
		},
		EffectStarted: func(ctx context.Context, e sc.Effect[order]) context.Context {
			log.add("started %s", e.Name)
			return ctx
		},
		EffectFinished: func(_ context.Context, e sc.Effect[order], _ time.Duration, err error) {
			log.add("finished %s: %v", e.Name, err)
		},
	}))

	_, err = d.Create(t.Context(), "o1", order{})
	require.NoError(t, err)
	_, err = d.Send(t.Context(), "o1", submit{Items: []string{"book"}})
	require.NoError(t, err)
	_, err = d.Send(t.Context(), "o1", sc.TimerFired{ID: "no such timer"})
	require.NoError(t, err)

	drain(t, d)

	require.Equal(t, []string{
		"transition submit to [charging]",
		"ignored TimerFired",
		"started announce",
		"finished announce: <nil>",
		"started charge",
		"transition sc.failed to [declined]",
		"finished charge: card declined",
	}, log.calls)
}

func TestWithHooksForAnotherMachinePanics(t *testing.T) {
	t.Parallel()

	m, err := sc.New(machineID(t), func(b *sc.Builder[string, order]) {
		b.Initial("idle")
		b.Final("idle")
	})
	require.NoError(t, err)

	require.Panics(t, func() { pgdriver.New(pool, m, pgdriver.WithHooks(&sc.Hooks[string, reminder]{})) })
}
