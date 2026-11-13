package payment_test

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"slices"
	"sync"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/speakeasy-api/gram/sc"
	"github.com/speakeasy-api/gram/sc/examples/payment"
	"github.com/speakeasy-api/gram/sc/pgdriver"
)

// fakeProvider is a payment provider that keeps the contract of
// [payment.Provider]: it decides once per idempotency key and repeats that
// outcome for every later request with the key.
type fakeProvider struct {
	mu sync.Mutex

	// declines is how many new keys are still to be declined.
	declines int

	// hold, when set, makes every charge wait until it is closed.
	hold chan struct{}

	receipts map[string]string
	declined map[string]bool
}

func (p *fakeProvider) Charge(ctx context.Context, key string) (string, error) {
	if p.hold != nil {
		select {
		case <-p.hold:
		case <-ctx.Done():
			return "", fmt.Errorf("charge %s: %w", key, ctx.Err())
		}
	}

	p.mu.Lock()
	defer p.mu.Unlock()

	if p.declined[key] {
		return "", payment.ErrDeclined
	}

	if receipt, ok := p.receipts[key]; ok {
		return receipt, nil
	}

	if p.declines > 0 {
		p.declines--
		p.declined[key] = true

		fmt.Printf("  provider: declined %s\n", key)

		return "", payment.ErrDeclined
	}

	p.receipts[key] = "rcpt-" + key

	fmt.Printf("  provider: charged %s\n", key)

	return p.receipts[key], nil
}

func (p *fakeProvider) Refund(_ context.Context, receipt string) {
	fmt.Printf("  provider: refunded %s\n", receipt)
}

func newProvider(declines int) *fakeProvider {
	return &fakeProvider{
		declines: declines,
		receipts: map[string]string{},
		declined: map[string]bool{},
	}
}

// fakeClock is a clock the example moves by hand, so that waiting out the
// retry delay takes no real time.
type fakeClock struct {
	mu  sync.Mutex
	now time.Time
}

func (c *fakeClock) Now() time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()

	return c.now
}

func (c *fakeClock) Add(d time.Duration) {
	c.mu.Lock()
	defer c.mu.Unlock()

	c.now = c.now.Add(d)
}

// logTransitions returns hooks that print each event an instance accepts
// with the states it moved between. Production code passes the hooks built
// by sco11y.New instead, or both joined with [sc.JoinHooks].
func logTransitions(m *sc.Machine[payment.State, payment.Order]) *sc.Hooks[payment.State, payment.Order] {
	return &sc.Hooks[payment.State, payment.Order]{
		Transition: func(_ context.Context, st step) {
			fmt.Printf("%v -> %v on %s\n", st.From.Active(), st.To.Active(), describe(m, st.Event))
		},
	}
}

// describe names an event for the log. Events the chart declares have the
// name of their type; the rest come from a driver.
func describe(m *sc.Machine[payment.State, payment.Order], ev any) string {
	switch ev := ev.(type) {
	case nil:
		// A step with no event is time passing: a timer fell due.
		return "timer"
	case sc.Done[string]:
		return "charge done"
	case sc.Failed:
		return "charge failed: " + ev.Err.Error()
	default:
		return m.EventName(ev)
	}
}

// An order is paid on the second attempt. The instance lives in memory and
// is driven from the caller's goroutine, which is how a request handler or
// a poll loop runs a chart: Send returns once the event and its actions are
// done, and the step says whether the event changed anything.
func Example() {
	ctx := context.Background()
	clock := &fakeClock{now: epoch}

	m, err := payment.New(newProvider(1))
	if err != nil {
		panic(err)
	}

	order, err := m.Instance(ctx, payment.Order{ID: "o1"},
		sc.WithHooks(logTransitions(m)),
		sc.WithClock(clock.Now),
		sc.ManualTimers(),
	)
	if err != nil {
		panic(err)
	}
	defer order.Close()

	// SendAndSettle also waits for the charge the event starts, and for
	// the chart to process its answer.
	if _, err := order.SendAndSettle(ctx, payment.Submit{}); err != nil {
		panic(err)
	}

	// An event no active state handles is ignored, not an error.
	st, err := order.Send(ctx, payment.Submit{})
	if err != nil {
		panic(err)
	}

	fmt.Println("second submit accepted:", st.Accepted())

	// Let the retry delay pass. With ManualTimers the timer fires when the
	// caller advances the instance; without it the instance wakes itself.
	clock.Add(payment.RetryDelay)

	if _, err := order.Advance(ctx); err != nil {
		panic(err)
	}

	paid, err := order.Await(ctx, func(s snapshot) bool { return s.Done() })
	if err != nil {
		panic(err)
	}

	fmt.Println("paid:", paid.In(payment.Paid), "receipt:", paid.Context.Receipt, "declines:", paid.Context.Attempts)

	// Output:
	// [idle] -> [charging] on Submit
	//   provider: declined o1/0
	// [charging] -> [retrying] on charge failed: card declined
	// second submit accepted: false
	// [retrying] -> [charging] on timer
	//   provider: charged o1/1
	// [charging] -> [paid] on charge done
	// paid: true receipt: rcpt-o1/1 declines: 1
}

// The customer cancels while the charge is on its way to the provider. The
// chart stays in charging until it knows what happened to the card, and
// refunds the charge when it turns out to have succeeded.
//
// The instance runs as an actor: a goroutine of its own with a mailbox,
// which suits a long-lived instance that several goroutines talk to.
func Example_actor() {
	ctx := context.Background()

	provider := newProvider(0)
	provider.hold = make(chan struct{})

	m, err := payment.New(provider)
	if err != nil {
		panic(err)
	}

	order := m.Actor(payment.Order{ID: "o2"}, sc.WithHooks(logTransitions(m)))

	// Run returns nil once the chart reaches a final state, after the
	// actions of its last step have run.
	stopped := make(chan error, 1)
	go func() { stopped <- order.Run(ctx) }()

	// Call waits until the actor has processed the event.
	if _, err := order.Call(ctx, payment.Submit{}); err != nil {
		panic(err)
	}

	if _, err := order.Call(ctx, payment.Cancel{}); err != nil {
		panic(err)
	}

	// The provider answers only now, after the cancellation.
	close(provider.hold)

	if err := <-stopped; err != nil {
		panic(err)
	}

	final := order.Snapshot()
	fmt.Println("cancelled:", final.In(payment.Cancelled), "receipt kept:", final.Context.Receipt != "")

	// Output:
	// [idle] -> [charging] on Submit
	// [charging] -> [charging] on Cancel
	//   provider: charged o2/0
	// [charging] -> [cancelled] on charge done
	//   provider: refunded rcpt-o2/0
	// cancelled: true receipt kept: false
}

// The core of a chart is a pure function. Transition takes a snapshot, an
// event and the time, and returns the next snapshot with the effects a
// driver must carry out. Nothing runs, which is what makes a chart easy to
// test, replay and store.
func Example_core() {
	m, err := payment.New(newProvider(0))
	if err != nil {
		panic(err)
	}

	printEffects := func(effects []sc.Effect[payment.Order]) {
		for _, e := range effects {
			fmt.Printf("  effect: %s %s (state %s)\n", e.Kind, e.Name, e.State)
		}
	}

	snap, _ := m.Start(payment.Order{ID: "o3"}, epoch)

	snap, effects := m.Transition(snap, payment.Submit{}, epoch)
	fmt.Println(snap.Active())
	printEffects(effects)

	// A driver would run the charge and deliver its result. Here the
	// result is written by hand, addressed to the run the chart started.
	run := snap.Services()[0]

	snap, effects = m.Transition(snap, sc.Failed{ID: run.ID, Err: payment.ErrDeclined}, epoch)
	fmt.Println(snap.Active())
	printEffects(effects)

	// The same result again is about a run that is no longer outstanding,
	// so it changes nothing. This is what makes at-least-once delivery safe.
	again, _ := m.Transition(snap, sc.Failed{ID: run.ID, Err: payment.ErrDeclined}, epoch)
	fmt.Println("duplicate ignored:", again.Seq() == snap.Seq())

	// A snapshot is a value that marshals to JSON and comes back with
	// Restore, timers included. Advance fires the ones that are due.
	data, err := json.Marshal(snap)
	if err != nil {
		panic(err)
	}

	later := epoch.Add(payment.RetryDelay)

	restored, _, err := m.Restore(data, later)
	if err != nil {
		panic(err)
	}

	restored, effects = m.Advance(restored, later)
	fmt.Println(restored.Active(), "key:", restored.Context.IdempotencyKey())
	printEffects(effects)

	// Output:
	// [charging]
	//   effect: invocation.start charge (state charging)
	// [retrying]
	//   effect: timer.start  (state retrying)
	// duplicate ignored: true
	// [charging] key: o3/1
	//   effect: invocation.start charge (state charging)
}

// Explore walks every state the chart can reach, trying each event and
// each outcome of each service in each one. The package's tests use it to
// check the chart's rules. It also answers questions: a rule that is meant
// to fail comes back with the shortest sequence of events that breaks it.
func Example_explore() {
	m, err := payment.New(newProvider(0))
	if err != nil {
		panic(err)
	}

	report, err := m.Explore(payment.Order{ID: "o4"}, sc.Exploration[payment.State, payment.Order]{
		Events: []any{payment.Submit{}, payment.Cancel{}},
		Outcomes: map[string][]sc.Outcome{
			"charge": {sc.Succeeds("rcpt-1"), sc.Fails(payment.ErrDeclined), sc.Fails(errTimeout)},
		},
		Invariants: []sc.Invariant[payment.State, payment.Order]{{
			Name:  "a paid order has a receipt",
			Holds: func(s snapshot) bool { return !s.In(payment.Paid) || s.Context.Receipt != "" },
		}},
		Steps: []sc.StepInvariant[payment.State, payment.Order]{{
			// Not a rule of the chart: it asks how a refund comes about.
			Name:  "nothing is ever refunded",
			Holds: func(st step) bool { return !refunds(st.Effects) },
		}},
	})
	if err != nil {
		panic(err)
	}

	fmt.Println("explored every state:", !report.Truncated)
	fmt.Println("states that cannot move:", len(report.Stuck), "states that can no longer finish:", len(report.Trapped))

	for _, v := range report.Violations {
		fmt.Printf("broken: %s\n%s", v.Invariant, m.FormatTrace(v.Trace))
	}

	// Output:
	// explored every state: true
	// states that cannot move: 0 states that can no longer finish: 0
	// broken: nothing is ever refunded
	// 1. +0s Submit
	// 2. +0s Cancel
	// 3. +0s service charging.charge#1 done: rcpt-1
	// ends in [cancelled]
}

// Definition exports the chart in the XState machine configuration format,
// for documentation and for tools that draw that format.
func Example_definition() {
	m, err := payment.New(newProvider(0))
	if err != nil {
		panic(err)
	}

	def := m.Definition()

	states := make([]string, 0, len(def.States))
	for name := range def.States {
		states = append(states, name)
	}

	slices.Sort(states)
	fmt.Println(def.ID, "starts in", def.Initial, "and has states", states)

	retrying, err := json.MarshalIndent(def.States[string(payment.Retrying)], "", "  ")
	if err != nil {
		panic(err)
	}

	fmt.Println(string(retrying))

	// Output:
	// payment starts in idle and has states [cancelled charging idle paid retrying]
	// {
	//   "id": "retrying",
	//   "on": {
	//     "Cancel": [
	//       {
	//         "guard": "chargeMayHaveLanded",
	//         "actions": [
	//           "requestCancel"
	//         ]
	//       },
	//       {
	//         "target": "#cancelled"
	//       }
	//     ]
	//   },
	//   "after": {
	//     "60000": [
	//       {
	//         "target": "#charging"
	//       }
	//     ]
	//   }
	// }
}

// The same chart with its instances in PostgreSQL. An event is applied
// inside the caller's transaction, so the order's new state and the effects
// it asks for commit together with whatever else the caller writes. Workers
// carry the effects out afterwards, at least once, which is why the charge
// carries an idempotency key and the refund must be idempotent.
//
// This example needs a database, so it is compiled but not run.
func Example_postgres() {
	ctx, stop := context.WithCancel(context.Background())
	defer stop()

	pool, err := pgxpool.New(ctx, os.Getenv("DATABASE_URL"))
	if err != nil {
		panic(err)
	}
	defer pool.Close()

	// The driver does not create its tables. In this repository they
	// belong in a migration; Schema is the SQL to start from.
	if _, err := pool.Exec(ctx, pgdriver.Schema); err != nil {
		panic(err)
	}

	m, err := payment.New(newProvider(0))
	if err != nil {
		panic(err)
	}

	orders, err := pgdriver.New(pool, m, pgdriver.WithHooks(logTransitions(m)))
	if err != nil {
		panic(err)
	}

	// A worker runs charges, refunds and timers from the outbox. Run as
	// many as the workload needs, in any process.
	go func() { _ = orders.Work(ctx) }()

	tx, err := pool.Begin(ctx)
	if err != nil {
		panic(err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	if _, err := orders.CreateTx(ctx, tx, "o5", payment.Order{ID: "o5"}); err != nil {
		panic(err)
	}

	// The caller's own writes go here, in the same transaction.

	st, err := orders.SendTx(ctx, tx, "o5", payment.Submit{})
	if err != nil {
		panic(err)
	}

	if err := tx.Commit(ctx); err != nil {
		panic(err)
	}

	// SendTx cannot know when the caller commits, so the caller reports
	// the step to the hooks once it has.
	orders.Report(ctx, st)

	// Load reads the instance as of now, with any timer that is already
	// due fired, and writes nothing.
	snap, err := orders.Load(ctx, "o5")
	if err != nil {
		panic(err)
	}

	fmt.Println(snap.Active())
}
