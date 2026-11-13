package pgdriver_test

import (
	"context"
	"errors"
	"fmt"
	"log"
	"os"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/testcontainers/testcontainers-go"
	"github.com/testcontainers/testcontainers-go/modules/postgres"

	"github.com/speakeasy-api/gram/sc"
	"github.com/speakeasy-api/gram/sc/pgdriver"
)

// pool connects to the Postgres container shared by every test. Tests stay
// independent by giving each machine its own ID, which scopes every row the
// driver reads or writes.
var pool *pgxpool.Pool

func TestMain(m *testing.M) {
	os.Exit(run(m))
}

func run(m *testing.M) int {
	ctx := context.Background()

	container, err := postgres.Run(ctx,
		"pgvector/pgvector:0.8.7-pg18-bookworm@sha256:2358fcba361ed2233a5ed81b5fe4ca779ccb304120ce531a3bf51c0ed7e2bc11",
		postgres.WithUsername("gotest"),
		postgres.WithPassword("gotest"),
		postgres.WithDatabase("gotestdb"),
		postgres.BasicWaitStrategies(),
	)
	defer func() {
		if container != nil {
			_ = testcontainers.TerminateContainer(container)
		}
	}()
	if err != nil {
		log.Printf("start postgres container: %v", err)
		return 1
	}

	uri, err := container.ConnectionString(ctx, "sslmode=disable")
	if err != nil {
		log.Printf("read connection string: %v", err)
		return 1
	}

	pool, err = pgxpool.New(ctx, uri)
	if err != nil {
		log.Printf("connect to postgres: %v", err)
		return 1
	}
	defer pool.Close()

	if _, err := pool.Exec(ctx, pgdriver.Schema); err != nil {
		log.Printf("apply schema: %v", err)
		return 1
	}

	return m.Run()
}

// machineIDs holds the machine ID chosen for each running test.
var machineIDs sync.Map

// machineSeq numbers the machine IDs handed out in this process.
var machineSeq atomic.Int64

// machineID returns an ID that is stable within one run of a test and
// differs between runs, so that repeating a test with -count never meets the
// rows of an earlier run.
func machineID(t *testing.T) string {
	t.Helper()

	id, _ := machineIDs.LoadOrStore(t, fmt.Sprintf("%s-%d", t.Name(), machineSeq.Add(1)))

	name, ok := id.(string)
	require.True(t, ok)

	return name
}

type (
	submit struct{ Items []string }
	cancel struct{}
	tick   struct{}
)

type order struct {
	Items    []string `json:"items"`
	Attempts int      `json:"attempts"`
	Receipt  string   `json:"receipt"`
	Count    int      `json:"count"`
}

// retryDelay is how long the test chart waits before charging again. It is
// short so tests that wait for the timer stay fast, and long enough that a
// test on a slow machine can observe the waiting state first.
const retryDelay = time.Second

var errCardDeclined = errors.New("card declined")

// recorder collects what the chart's actions and services were called with.
type recorder struct {
	mu    sync.Mutex
	calls []string
}

func (r *recorder) add(call string) {
	r.mu.Lock()
	defer r.mu.Unlock()

	r.calls = append(r.calls, call)
}

func (r *recorder) list() []string {
	r.mu.Lock()
	defer r.mu.Unlock()

	return append([]string(nil), r.calls...)
}

// newDriver builds an order chart with its own machine ID and a driver for
// it. charge is the payment service.
func newDriver(t *testing.T, rec *recorder, charge func(context.Context, order) (string, error)) *pgdriver.Driver[string, order] {
	t.Helper()

	m, err := sc.New(machineID(t), func(b *sc.Builder[string, order]) {
		b.Initial("idle")

		idle := b.State("idle")
		idle.On[submit]().
			Assign("setItems", func(o order, e submit) order { o.Items = e.Items; return o }).
			Do("announce", func(_ context.Context, o order, e submit) {
				rec.add(fmt.Sprintf("announce %v %v", o.Items, e.Items))
			}).
			Do("audit", func(context.Context, order, submit) { rec.add("audit") }).
			To("charging")
		idle.On[tick]().Assign("count", func(o order, _ tick) order { o.Count++; return o })

		inv := b.State("charging").Invoke("charge", charge)
		inv.OnDone().
			Assign("setReceipt", func(o order, d sc.Done[string]) order { o.Receipt = d.Result; return o }).
			To("paid")
		inv.OnError().
			Do("reportFailure", func(_ context.Context, _ order, f sc.Failed) { rec.add("failed: " + f.Err.Error()) }).
			To("retrying")

		retrying := b.State("retrying")
		retrying.After(retryDelay).
			Assign("countAttempt", func(o order, _ struct{}) order { o.Attempts++; return o }).
			To("charging")
		retrying.On[cancel]().To("cancelled")

		b.Final("paid").Entry("ship", func(_ context.Context, o order) { rec.add("ship " + o.Receipt) })
		b.Final("cancelled")
	})
	require.NoError(t, err)

	return mustNew(t, m)
}

// mustNew builds a driver on the shared pool.
func mustNew[S comparable, C any](t *testing.T, m *sc.Machine[S, C], opts ...pgdriver.Option) *pgdriver.Driver[S, C] {
	t.Helper()

	d, err := pgdriver.New(pool, m, opts...)
	require.NoError(t, err)

	return d
}

// drain carries out effects until none is due.
func drain[S comparable, C any](t *testing.T, d *pgdriver.Driver[S, C]) {
	t.Helper()

	for {
		worked, err := d.Step(t.Context())
		require.NoError(t, err)

		if !worked {
			return
		}
	}
}

func outboxSize(t *testing.T, instance string) int {
	t.Helper()

	var n int
	err := pool.QueryRow(t.Context(),
		`SELECT count(*) FROM statechart_effects WHERE machine = $1 AND instance_id = $2`,
		machineID(t), instance,
	).Scan(&n)
	require.NoError(t, err)

	return n
}

func TestSendPersistsStateAndWorkerCarriesOutEffects(t *testing.T) {
	t.Parallel()

	rec := &recorder{mu: sync.Mutex{}, calls: nil}
	d := newDriver(t, rec, func(_ context.Context, o order) (string, error) {
		rec.add("charge")
		return "rcpt-" + o.Items[0], nil
	})

	created, err := d.Create(t.Context(), "o1", order{})
	require.NoError(t, err)
	require.Equal(t, []string{"idle"}, created.Active())

	sent, err := d.Send(t.Context(), "o1", submit{Items: []string{"book"}})
	require.NoError(t, err)
	require.Equal(t, []string{"charging"}, sent.To.Active())

	// Nothing has run yet: the effects are only recorded.
	require.Empty(t, rec.list())
	require.Equal(t, 3, outboxSize(t, "o1"))

	drain(t, d)

	loaded, err := d.Load(t.Context(), "o1")
	require.NoError(t, err)
	require.True(t, loaded.Done())
	require.Equal(t, []string{"paid"}, loaded.Active())
	require.Equal(t, "rcpt-book", loaded.Context.Receipt)

	require.Equal(t, []string{"announce [book] [book]", "audit", "charge", "ship rcpt-book"}, rec.list())
	require.Zero(t, outboxSize(t, "o1"))
}

func TestSendTxEffectsRollBackWithTheCallersTransaction(t *testing.T) {
	t.Parallel()

	rec := &recorder{mu: sync.Mutex{}, calls: nil}
	d := newDriver(t, rec, func(context.Context, order) (string, error) { return "rcpt", nil })

	_, err := d.Create(t.Context(), "o1", order{})
	require.NoError(t, err)
	before := outboxSize(t, "o1")

	errAborted := errors.New("caller aborted")
	err = pgx.BeginFunc(t.Context(), pool, func(tx pgx.Tx) error {
		step, err := d.SendTx(t.Context(), tx, "o1", submit{Items: []string{"book"}})
		require.NoError(t, err)
		require.True(t, step.Accepted())

		return errAborted
	})
	require.ErrorIs(t, err, errAborted)
	require.Equal(t, before, outboxSize(t, "o1"))

	err = pgx.BeginFunc(t.Context(), pool, func(tx pgx.Tx) error {
		if _, err := d.SendTx(t.Context(), tx, "o1", submit{Items: []string{"book"}}); err != nil {
			return fmt.Errorf("submit: %w", err)
		}

		return nil
	})
	require.NoError(t, err)
	require.Greater(t, outboxSize(t, "o1"), before)
}

func TestFailedServiceIsRetriedWhenItsTimerIsDue(t *testing.T) {
	t.Parallel()

	rec := &recorder{mu: sync.Mutex{}, calls: nil}
	d := newDriver(t, rec, func(_ context.Context, o order) (string, error) {
		if o.Attempts == 0 {
			return "", errCardDeclined
		}

		return "rcpt-retry", nil
	})

	_, err := d.Create(t.Context(), "o1", order{})
	require.NoError(t, err)
	_, err = d.Send(t.Context(), "o1", submit{Items: []string{"book"}})
	require.NoError(t, err)

	drain(t, d)

	waiting, err := d.Load(t.Context(), "o1")
	require.NoError(t, err)
	require.Equal(t, []string{"retrying"}, waiting.Active())
	require.Contains(t, rec.list(), "failed: card declined")

	// The timer is in the outbox but not yet due.
	require.Equal(t, 1, outboxSize(t, "o1"))

	require.EventuallyWithT(t, func(c *assert.CollectT) {
		for {
			worked, err := d.Step(t.Context())
			if !assert.NoError(c, err) || !worked {
				break
			}
		}

		snap, err := d.Load(t.Context(), "o1")
		if assert.NoError(c, err) {
			assert.True(c, snap.Done())
			assert.Equal(c, "rcpt-retry", snap.Context.Receipt)
			assert.Equal(c, 1, snap.Context.Attempts)
		}
	}, 10*time.Second, 50*time.Millisecond)
}

func TestLeavingAStateRemovesItsTimerFromTheOutbox(t *testing.T) {
	t.Parallel()

	rec := &recorder{mu: sync.Mutex{}, calls: nil}
	d := newDriver(t, rec, func(context.Context, order) (string, error) { return "", errCardDeclined })

	_, err := d.Create(t.Context(), "o1", order{})
	require.NoError(t, err)
	_, err = d.Send(t.Context(), "o1", submit{Items: []string{"book"}})
	require.NoError(t, err)
	drain(t, d)
	require.Equal(t, 1, outboxSize(t, "o1"))

	step, err := d.Send(t.Context(), "o1", cancel{})
	require.NoError(t, err)

	require.True(t, step.To.Done())
	require.Zero(t, outboxSize(t, "o1"))
}

func TestConcurrentSendsAreAppliedOneAfterAnother(t *testing.T) {
	t.Parallel()

	// senders is the number of concurrent sends. It only needs to be large
	// enough that lost updates would show up.
	const senders = 20

	d := newDriver(t, &recorder{mu: sync.Mutex{}, calls: nil}, func(context.Context, order) (string, error) { return "", nil })

	_, err := d.Create(t.Context(), "o1", order{})
	require.NoError(t, err)

	errs := make(chan error, senders)
	var wg sync.WaitGroup
	for range senders {
		wg.Go(func() {
			_, err := d.Send(t.Context(), "o1", tick{})
			errs <- err
		})
	}
	wg.Wait()
	close(errs)

	for err := range errs {
		require.NoError(t, err)
	}

	snap, err := d.Load(t.Context(), "o1")
	require.NoError(t, err)
	require.Equal(t, senders, snap.Context.Count)
	require.Equal(t, uint64(senders), snap.Seq())
}

func TestWorkRunsEffectsUntilCancelled(t *testing.T) {
	t.Parallel()

	rec := &recorder{mu: sync.Mutex{}, calls: nil}
	d := newDriver(t, rec, func(context.Context, order) (string, error) { return "rcpt-work", nil })

	ctx, stop := context.WithCancel(t.Context())
	stopped := make(chan error, 1)
	go func() { stopped <- d.Work(ctx) }()

	_, err := d.Create(t.Context(), "o1", order{})
	require.NoError(t, err)
	_, err = d.Send(t.Context(), "o1", submit{Items: []string{"book"}})
	require.NoError(t, err)

	require.EventuallyWithT(t, func(c *assert.CollectT) {
		snap, err := d.Load(t.Context(), "o1")
		if assert.NoError(c, err) {
			assert.True(c, snap.Done())
		}
	}, 10*time.Second, 50*time.Millisecond)

	stop()
	require.ErrorIs(t, <-stopped, context.Canceled)
}

func TestCreateRejectsDuplicateAndSendRejectsUnknownInstance(t *testing.T) {
	t.Parallel()

	d := newDriver(t, &recorder{mu: sync.Mutex{}, calls: nil}, func(context.Context, order) (string, error) { return "", nil })

	_, err := d.Create(t.Context(), "o1", order{})
	require.NoError(t, err)

	_, err = d.Create(t.Context(), "o1", order{})
	require.ErrorIs(t, err, pgdriver.ErrExists)

	_, err = d.Send(t.Context(), "missing", tick{})
	require.ErrorIs(t, err, pgdriver.ErrNotFound)

	_, err = d.Load(t.Context(), "missing")
	require.ErrorIs(t, err, pgdriver.ErrNotFound)
}

func TestEffectTheChartNoLongerDeclaresIsDroppedAndReported(t *testing.T) {
	t.Parallel()

	chart := func(action string) *sc.Machine[string, order] {
		m, err := sc.New(machineID(t), func(b *sc.Builder[string, order]) {
			b.Initial("idle")
			b.State("idle").On[submit]().Do(action, func(context.Context, order, submit) {}).To("done")
			b.Final("done")
		})
		require.NoError(t, err)

		return m
	}

	before := mustNew(t, chart("announce"))
	_, err := before.Create(t.Context(), "o1", order{})
	require.NoError(t, err)
	_, err = before.Send(t.Context(), "o1", submit{Items: nil})
	require.NoError(t, err)
	require.Equal(t, 1, outboxSize(t, "o1"))

	after := mustNew(t, chart("notify"))
	worked, err := after.Step(t.Context())

	require.True(t, worked)
	require.ErrorIs(t, err, sc.ErrUnknownEffect)
	require.Zero(t, outboxSize(t, "o1"))
}

func TestNewRejectsAChartThatHoldsResources(t *testing.T) {
	t.Parallel()

	m, err := sc.New(machineID(t), func(b *sc.Builder[string, order]) {
		b.Initial("open")
		conn := b.State("open").Holds("conn",
			func(context.Context, order) (int, error) { return 1, nil },
			func(context.Context, int, sc.Leave[string]) {},
		)
		conn.OnError().To("closed")
		b.Final("closed")
	})
	require.NoError(t, err)

	_, err = pgdriver.New(pool, m)

	require.ErrorIs(t, err, pgdriver.ErrResources)
	require.ErrorContains(t, err, "open.conn")
}

func TestNewRejectsAChartThatSendsOrEmitsEvents(t *testing.T) {
	t.Parallel()

	m, err := sc.New(machineID(t), func(b *sc.Builder[string, order]) {
		b.Initial("open")
		b.State("open").On[tick]().Emit("report", func(order, tick) any { return tick{} }).To("closed")
		b.Final("closed")
	})
	require.NoError(t, err)

	_, err = pgdriver.New(pool, m)

	require.ErrorIs(t, err, pgdriver.ErrMessaging)
}

func TestARetriedServiceRunsAgainFromTheOutbox(t *testing.T) {
	t.Parallel()

	var calls atomic.Int32
	m, err := sc.New(machineID(t), func(b *sc.Builder[string, order]) {
		b.Initial("charging")
		inv := b.State("charging").Invoke("charge", func(context.Context, order) (string, error) {
			if calls.Add(1) < 3 {
				return "", errCardDeclined
			}

			return "rcpt", nil
		}).Retry(sc.Retry{Max: 5, First: 100 * time.Millisecond, Cap: 0, Jitter: 0, If: nil})
		inv.OnDone().
			Assign("setReceipt", func(o order, d sc.Done[string]) order { o.Receipt = d.Result; return o }).
			To("paid")
		inv.OnError().To("declined")
		b.Final("paid")
		b.Final("declined")
	})
	require.NoError(t, err)

	d := mustNew(t, m)
	_, err = d.Create(t.Context(), "o1", order{})
	require.NoError(t, err)

	// Each failure is stored as a wait, and the worker runs the service
	// again when the wait is over, without the chart leaving its state.
	require.EventuallyWithT(t, func(c *assert.CollectT) {
		drain(t, d)

		snap, err := d.Load(t.Context(), "o1")
		if assert.NoError(c, err) {
			assert.Equal(c, []string{"paid"}, snap.Active())
			assert.Equal(c, "rcpt", snap.Context.Receipt)
		}
	}, 10*time.Second, 50*time.Millisecond)
	require.Equal(t, int32(3), calls.Load())
}
