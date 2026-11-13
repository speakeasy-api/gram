// Package pgdriver runs statechart instances with their state in PostgreSQL.
//
// [Driver.SendTx] applies one event inside the caller's transaction: it
// locks the instance through its [Store], applies the event with the
// machine's pure transition function at the database's time, saves the new
// snapshot, and records the step's effects through its [Sink]. The state
// change and its effects therefore commit together with whatever else the
// caller writes, after whatever locks the caller has taken. [Driver.Send] is
// the same in a transaction of its own.
//
// By default instances live in the driver's own table ([TableStore]) and
// effects in its own outbox ([Outbox]), which [Driver.Work] carries out. A
// store can instead keep the snapshot on the domain's row and copy the state
// into indexed columns; a sink can hand effects to an existing outbox, whose
// consumer calls [Driver.Perform].
//
// Effects are delivered at least once: a worker that stops after running an
// effect but before removing it will run it again. Actions and services must
// therefore be idempotent. Events the machine receives twice because of this
// are ignored, since the timer or service run they refer to is no longer
// outstanding.
//
// The default tables are defined by [Schema]. The driver does not create
// them.
package pgdriver

import (
	"context"
	_ "embed"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/speakeasy-api/gram/sc"
)

// Schema is the SQL that creates the default store's and outbox's tables
// and indexes. It is idempotent.
//
//go:embed schema.sql
var Schema string

// defaultPollInterval is how long an idle worker waits before it looks for
// due effects again. It bounds how late a timer can fire and how long an
// effect waits when every worker was idle, so it trades latency for load on
// an empty outbox.
const defaultPollInterval = time.Second

var (
	// ErrNotFound is returned when no instance has the given ID.
	ErrNotFound = errors.New("statechart instance not found")

	// ErrExists is returned by [Driver.Create] when the ID is taken.
	ErrExists = errors.New("statechart instance already exists")

	// ErrNoOutbox is returned by [Driver.Work] and [Driver.Step] for a
	// driver whose sink is not the built-in [Outbox].
	ErrNoOutbox = errors.New("driver does not use the built-in outbox")

	// ErrResources is returned by [New] for a chart that declares
	// resources with Holds. A resource lives in one process, but any
	// worker process may carry out an instance's effects, so this driver
	// cannot hold one. Run such a chart with sc.Instance or sc.Actor.
	ErrResources = errors.New("chart declares resources, which the Postgres driver cannot hold")

	// ErrMessaging is returned by [New] for a chart that sends events to
	// its services or emits them to whoever runs it. Both reach a
	// goroutine in one process, and any worker process may carry out an
	// instance's effects. Run such a chart with sc.Instance or sc.Actor.
	ErrMessaging = errors.New("chart sends or emits events, which the Postgres driver cannot deliver")
)

// Driver stores and runs the instances of one machine. It is safe for
// concurrent use, and several processes may run drivers for the same machine
// against the same database.
type Driver[S comparable, C any] struct {
	pool   *pgxpool.Pool
	m      *sc.Machine[S, C]
	poll   time.Duration
	hooks  *sc.Hooks[S, C]
	store  Store[S, C]
	sink   Sink[C]
	outbox bool
}

// config holds what the Option values passed to New configure.
type config struct {
	poll time.Duration

	// hooks, store and sink hold values typed for the driver's S and C,
	// checked by New.
	hooks any
	store any
	sink  any
}

// Option configures a [Driver].
type Option func(*config)

// WithPollInterval sets how long an idle worker waits before it looks for
// due effects again. A duration that is not positive keeps the default, so
// a worker never polls without a pause.
func WithPollInterval(d time.Duration) Option {
	return func(c *config) {
		if d > 0 {
			c.poll = d
		}
	}
}

// WithHooks makes the driver report to h. [Driver.Send] reports a step
// after the transaction that saved it commits, so a step that was rolled
// back is never reported. [Driver.SendTx] cannot know when the caller
// commits; call [Driver.Report] after the commit. Creating an instance is
// not a step and is not reported.
// New panics when h is for another state or context type.
func WithHooks[S comparable, C any](h *sc.Hooks[S, C]) Option {
	return func(c *config) { c.hooks = h }
}

// WithStore makes the driver keep snapshots in store instead of its own
// table. New panics when store is for another state or context type.
func WithStore[S comparable, C any](store Store[S, C]) Option {
	return func(c *config) { c.store = store }
}

// WithSink makes the driver record effects in sink instead of its own
// outbox. [Driver.Work] is then unavailable; the sink's consumer carries
// effects out with [Driver.Perform]. New panics when sink is for another
// context type.
func WithSink[C any](sink Sink[C]) Option {
	return func(c *config) { c.sink = sink }
}

// New returns a driver for the instances of m. It fails with an error
// wrapping [ErrResources] when the chart declares resources, and with
// [ErrMessaging] when it sends or emits events.
func New[S comparable, C any](pool *pgxpool.Pool, m *sc.Machine[S, C], opts ...Option) (*Driver[S, C], error) {
	if resources := m.Resources(); len(resources) > 0 {
		return nil, fmt.Errorf("%w: %s", ErrResources, strings.Join(resources, ", "))
	}

	if m.Messaging() {
		return nil, ErrMessaging
	}

	cfg := config{poll: defaultPollInterval, hooks: nil, store: nil, sink: nil}
	for _, opt := range opts {
		opt(&cfg)
	}

	d := &Driver[S, C]{
		pool:   pool,
		m:      m,
		poll:   cfg.poll,
		hooks:  typed[*sc.Hooks[S, C]](cfg.hooks, "WithHooks", m.ID()),
		store:  typed[Store[S, C]](cfg.store, "WithStore", m.ID()),
		sink:   typed[Sink[C]](cfg.sink, "WithSink", m.ID()),
		outbox: false,
	}

	if d.store == nil {
		d.store = TableStore[S, C]{Machine: m.ID()}
	}
	if d.sink == nil {
		d.sink = Outbox[S, C]{Machine: m}
		d.outbox = true
	}

	return d, nil
}

// typed asserts an option's value to the type the driver needs, panicking
// with the option's name when it was built for another machine.
func typed[T any](v any, option, machine string) T {
	var zero T
	if v == nil {
		return zero
	}

	t, ok := v.(T)
	if !ok {
		panic(fmt.Sprintf("pgdriver: %s was given %T, which is not for machine %q", option, v, machine))
	}

	return t
}

// Machine returns the machine the driver runs.
func (d *Driver[S, C]) Machine() *sc.Machine[S, C] { return d.m }

// Create stores a new instance with context c and returns its first
// snapshot, in a transaction of its own.
func (d *Driver[S, C]) Create(ctx context.Context, id string, c C) (sc.Snapshot[S, C], error) {
	var snap sc.Snapshot[S, C]

	err := pgx.BeginFunc(ctx, d.pool, func(tx pgx.Tx) error {
		var err error
		snap, err = d.CreateTx(ctx, tx, id, c)

		return err
	})
	if err != nil {
		return snap, fmt.Errorf("create transaction: %w", err)
	}

	return snap, nil
}

// CreateTx stores a new instance with context c inside tx, started at the
// database's time, and records the effects of entering its initial states.
func (d *Driver[S, C]) CreateTx(ctx context.Context, tx pgx.Tx, id string, c C) (sc.Snapshot[S, C], error) {
	var zero sc.Snapshot[S, C]

	now, err := dbNow(ctx, tx)
	if err != nil {
		return zero, fmt.Errorf("create instance %q: %w", id, err)
	}

	snap, effects := d.m.Start(c, now, sc.RandomSeed())

	data, err := json.Marshal(snap)
	if err != nil {
		return snap, fmt.Errorf("create instance %q: marshal snapshot: %w", id, err)
	}

	if err := d.store.Insert(ctx, tx, id, snap, data); err != nil {
		return snap, fmt.Errorf("create instance %q: %w", id, err)
	}

	if err := d.sink.Enqueue(ctx, tx, id, now, effects); err != nil {
		return snap, fmt.Errorf("create instance %q: %w", id, err)
	}

	return snap, nil
}

// Load returns an instance as of now by the database's clock: its stored
// snapshot with every timer that is already due fired, as
// [sc.Machine.Advance] fires them. It writes nothing, so a timer that has
// passed shows its effect on reads before a worker gets to it, such as an
// expiry.
func (d *Driver[S, C]) Load(ctx context.Context, id string) (sc.Snapshot[S, C], error) {
	var zero sc.Snapshot[S, C]

	data, err := d.store.Read(ctx, d.pool, id)
	if err != nil {
		return zero, fmt.Errorf("load instance %q: %w", id, err)
	}

	now, err := dbNow(ctx, d.pool)
	if err != nil {
		return zero, fmt.Errorf("load instance %q: %w", id, err)
	}

	snap, _, err := d.m.Restore(data, now)
	if err != nil {
		return zero, fmt.Errorf("load instance %q: %w", id, err)
	}

	current, _ := d.m.Advance(snap, now)

	return current, nil
}

// Send applies one event to an instance in a transaction of its own, and
// reports the step to the driver's hooks once it has committed. Concurrent
// sends to the same instance are applied one after another.
func (d *Driver[S, C]) Send(ctx context.Context, id string, ev any) (sc.Step[S, C], error) {
	var step sc.Step[S, C]

	err := pgx.BeginFunc(ctx, d.pool, func(tx pgx.Tx) error {
		var err error
		step, err = d.SendTx(ctx, tx, id, ev)

		return err
	})
	if err != nil {
		return step, fmt.Errorf("send transaction: %w", err)
	}

	d.Report(ctx, step)

	return step, nil
}

// SendTx applies one event to an instance inside tx, at the database's
// time. It locks the instance through the store, so take any locks that
// must come first before calling it. An accepted step's snapshot and effects
// are written in tx and commit with it; an ignored one writes nothing,
// unless restoring the stored snapshot itself produced effects, as after a
// migration, in which case the restored snapshot and those effects are
// written. Use
// [sc.Step.Accepted] to tell them apart, and call [Driver.Report] after the
// commit to reach the hooks.
func (d *Driver[S, C]) SendTx(ctx context.Context, tx pgx.Tx, id string, ev any) (sc.Step[S, C], error) {
	var step sc.Step[S, C]

	data, err := d.store.Lock(ctx, tx, id)
	if err != nil {
		return step, fmt.Errorf("send to instance %q: %w", id, err)
	}

	now, err := dbNow(ctx, tx)
	if err != nil {
		return step, fmt.Errorf("send to instance %q: %w", id, err)
	}

	snap, restored, err := d.m.Restore(data, now)
	if err != nil {
		return step, fmt.Errorf("send to instance %q: restore snapshot: %w", id, err)
	}

	next, effects := d.m.Transition(snap, ev, now)
	step = sc.Step[S, C]{At: now, From: snap, Event: ev, To: next, Effects: effects}

	if !step.Accepted() && len(restored) == 0 {
		return step, nil
	}

	encoded, err := json.Marshal(next)
	if err != nil {
		return step, fmt.Errorf("send to instance %q: marshal snapshot: %w", id, err)
	}

	if err := d.store.Update(ctx, tx, id, step, encoded); err != nil {
		return step, fmt.Errorf("send to instance %q: %w", id, err)
	}

	if err := d.sink.Enqueue(ctx, tx, id, now, append(restored, effects...)); err != nil {
		return step, fmt.Errorf("send to instance %q: %w", id, err)
	}

	return step, nil
}

// Report passes a committed step to the driver's hooks. [Driver.Send] calls
// it itself; callers of [Driver.SendTx] call it after their commit.
func (d *Driver[S, C]) Report(ctx context.Context, step sc.Step[S, C]) {
	d.hooks.Observe(ctx, step)
}

// dbNow reads the database's clock, which every decision the driver makes
// is measured by, so the chart and the queries that read its rows agree.
func dbNow(ctx context.Context, q Querier) (time.Time, error) {
	var now time.Time
	if err := q.QueryRow(ctx, `SELECT clock_timestamp()`).Scan(&now); err != nil {
		return time.Time{}, fmt.Errorf("read database clock: %w", err)
	}

	return now, nil
}

// Perform carries out one effect a sink recorded, encoded as
// [sc.Machine.EncodeEffect] encodes it, and sends the events it produces to
// the instance: a timer's TimerFired, or a service's results. It is how the
// consumer of a custom [Sink] runs effects; [Driver.Work] uses it for the
// built-in outbox. It returns an error wrapping [sc.ErrUnknownEffect] for an
// action the chart no longer declares, which can never succeed; a service
// the chart no longer declares, or one with no implementation bound, is
// reported to the instance as failed.
func (d *Driver[S, C]) Perform(ctx context.Context, instance string, data []byte) error {
	e, err := d.m.DecodeEffect(data)
	if err != nil {
		return fmt.Errorf("decode effect: %w", err)
	}

	if e.Kind == sc.EffectStartTimer {
		_, err := d.Send(ctx, instance, sc.TimerFired{ID: e.ID})
		return err
	}

	now, err := dbNow(ctx, d.pool)
	if err != nil {
		return fmt.Errorf("perform effect: %w", err)
	}

	// A service that is run again starts from the progress it has recorded
	// since the effect was written.
	if e.Kind == sc.EffectStartInvocation {
		snap, err := d.Load(ctx, instance)
		if err != nil {
			return fmt.Errorf("perform effect: %w", err)
		}
		if saved := snap.Checkpoint(e.ID); saved != nil {
			e.Saved = saved
		}
	}

	var sendErr error
	err = d.hooks.Exec(ctx, d.m, e, now, func(evCtx context.Context, ev any) {
		if sendErr == nil {
			_, sendErr = d.Send(evCtx, instance, ev)
		}
	})

	// A service the chart no longer declares fails like any other service,
	// which hands the decision to the chart's own error transition.
	if e.Kind == sc.EffectStartInvocation && (errors.Is(err, sc.ErrUnknownEffect) || errors.Is(err, sc.ErrNotBound)) {
		_, err = d.Send(ctx, instance, sc.Failed{ID: e.ID, Err: err})
	}
	if err != nil {
		return err
	}

	return sendErr
}

// Work carries out the built-in outbox's due effects until ctx is cancelled
// or an effect cannot be processed, and returns the reason. Run it on as
// many goroutines or processes as the workload needs; a service occupies its
// worker until it finishes.
func (d *Driver[S, C]) Work(ctx context.Context) error {
	if !d.outbox {
		return ErrNoOutbox
	}

	for {
		worked, err := d.Step(ctx)
		if err != nil {
			return err
		}
		if worked {
			continue
		}

		idle := time.NewTimer(d.poll)
		select {
		case <-ctx.Done():
			idle.Stop()
			return fmt.Errorf("stop worker: %w", context.Cause(ctx))
		case <-idle.C:
		}
	}
}

// Step carries out at most one of the built-in outbox's due effects and
// reports whether it found one. The effect's row stays locked while it runs
// and is removed in the same transaction, so no other worker picks it up,
// and it becomes available again if this worker stops.
//
// The actions of one instance run in the order the machine produced them.
// Services and timers of the same instance are not held up by each other or
// by a running action.
//
// Step keeps one pool connection for the claim while the effect runs, and
// delivering a timer or a service's result takes a second one for its own
// transaction. Size the pool above the number of workers calling Step, or
// they can hold every connection while each waits for another.
func (d *Driver[S, C]) Step(ctx context.Context) (bool, error) {
	if !d.outbox {
		return false, ErrNoOutbox
	}

	worked := false

	// dropped is set when the claimed effect no longer matches the chart.
	// Such an effect can never run, so its row is removed like a completed
	// one and the error is reported after the removal commits.
	var dropped error

	err := pgx.BeginFunc(ctx, d.pool, func(tx pgx.Tx) error {
		var (
			rowID    int64
			instance string
			kind     string
			data     []byte
		)

		err := tx.QueryRow(ctx, `
			SELECT e.id, e.instance_id, e.kind, e.effect
			FROM statechart_effects e
			WHERE e.machine = $1
			  AND e.run_after <= clock_timestamp()
			  AND (e.kind <> $2 OR NOT EXISTS (
				SELECT 1 FROM statechart_effects earlier
				WHERE earlier.machine = e.machine
				  AND earlier.instance_id = e.instance_id
				  AND earlier.kind = $2
				  AND earlier.id < e.id
			  ))
			ORDER BY e.id
			LIMIT 1
			FOR UPDATE OF e SKIP LOCKED`,
			d.m.ID(), string(sc.EffectAction),
		).Scan(&rowID, &instance, &kind, &data)
		switch {
		case errors.Is(err, pgx.ErrNoRows):
			return nil
		case err != nil:
			return fmt.Errorf("claim effect: %w", err)
		}

		worked = true

		err = d.Perform(ctx, instance, data)
		switch {
		case errors.Is(err, sc.ErrUnknownEffect):
			dropped = fmt.Errorf("drop %s effect of instance %q: %w", kind, instance, err)
		case err != nil:
			return fmt.Errorf("run %s effect of instance %q: %w", kind, instance, err)
		}

		if _, err := tx.Exec(ctx, `DELETE FROM statechart_effects WHERE id = $1`, rowID); err != nil {
			return fmt.Errorf("remove effect: %w", err)
		}

		return nil
	})
	if err != nil {
		return worked, fmt.Errorf("process effect: %w", err)
	}

	return worked, dropped
}
