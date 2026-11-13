// Package pgdriver runs statechart instances with their state in PostgreSQL.
//
// Each instance is a row holding its latest snapshot. [Driver.Send] locks the
// row, applies one event with the machine's pure transition function, and
// saves the new snapshot together with the effects it produced, all in one
// transaction. The effects sit in an outbox table until a worker running
// [Driver.Work] carries them out and sends any resulting events back.
//
// Effects are delivered at least once: a worker that stops after running an
// effect but before removing it from the outbox will run it again. Actions
// and services must therefore be idempotent. Events the machine receives
// twice because of this are ignored, since the timer or service run they
// refer to is no longer outstanding.
//
// The tables are defined by [Schema]. The driver does not create them.
package pgdriver

import (
	"context"
	_ "embed"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/speakeasy-api/gram/sc"
)

// Schema is the SQL that creates the driver's tables and indexes. It is
// idempotent.
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
)

// Driver stores and runs the instances of one machine. It is safe for
// concurrent use, and several processes may run drivers for the same machine
// against the same database.
type Driver[S comparable, C any] struct {
	pool *pgxpool.Pool
	m    *sc.Machine[S, C]
	poll time.Duration
}

// config holds what the Option values passed to New configure.
type config struct {
	poll time.Duration
}

// Option configures a [Driver].
type Option func(*config)

// WithPollInterval sets how long an idle worker waits before it looks for
// due effects again.
func WithPollInterval(d time.Duration) Option {
	return func(c *config) { c.poll = d }
}

// New returns a driver for the instances of m.
func New[S comparable, C any](pool *pgxpool.Pool, m *sc.Machine[S, C], opts ...Option) *Driver[S, C] {
	cfg := config{poll: defaultPollInterval}
	for _, opt := range opts {
		opt(&cfg)
	}

	return &Driver[S, C]{pool: pool, m: m, poll: cfg.poll}
}

// Create stores a new instance with context c and returns its first
// snapshot.
func (d *Driver[S, C]) Create(ctx context.Context, id string, c C) (sc.Snapshot[S, C], error) {
	snap, effects := d.m.Start(c)

	data, err := json.Marshal(snap)
	if err != nil {
		return snap, fmt.Errorf("marshal snapshot: %w", err)
	}

	err = pgx.BeginFunc(ctx, d.pool, func(tx pgx.Tx) error {
		tag, err := tx.Exec(ctx, `
			INSERT INTO statechart_instances (machine, id, seq, snapshot)
			VALUES ($1, $2, $3, $4)
			ON CONFLICT (machine, id) DO NOTHING`,
			d.m.ID(), id, snap.Seq(), data,
		)
		if err != nil {
			return fmt.Errorf("insert instance: %w", err)
		}
		if tag.RowsAffected() == 0 {
			return ErrExists
		}

		return d.enqueue(ctx, tx, id, effects)
	})
	if err != nil {
		return snap, fmt.Errorf("create instance %q: %w", id, err)
	}

	return snap, nil
}

// Load returns the latest snapshot of an instance.
func (d *Driver[S, C]) Load(ctx context.Context, id string) (sc.Snapshot[S, C], error) {
	var zero sc.Snapshot[S, C]

	var data []byte
	err := d.pool.QueryRow(ctx, `
		SELECT snapshot FROM statechart_instances WHERE machine = $1 AND id = $2`,
		d.m.ID(), id,
	).Scan(&data)
	switch {
	case errors.Is(err, pgx.ErrNoRows):
		return zero, fmt.Errorf("load instance %q: %w", id, ErrNotFound)
	case err != nil:
		return zero, fmt.Errorf("load instance %q: %w", id, err)
	}

	snap, _, err := d.m.Restore(data)
	if err != nil {
		return zero, fmt.Errorf("load instance %q: %w", id, err)
	}

	return snap, nil
}

// Send applies one event to an instance and returns its new snapshot. The
// snapshot and the effects of the transition are saved in one transaction,
// so an effect is recorded if and only if the state change that caused it
// is. Concurrent sends to the same instance are applied one after another.
func (d *Driver[S, C]) Send(ctx context.Context, id string, ev any) (sc.Snapshot[S, C], error) {
	var next sc.Snapshot[S, C]

	err := pgx.BeginFunc(ctx, d.pool, func(tx pgx.Tx) error {
		var data []byte
		err := tx.QueryRow(ctx, `
			SELECT snapshot FROM statechart_instances WHERE machine = $1 AND id = $2 FOR UPDATE`,
			d.m.ID(), id,
		).Scan(&data)
		switch {
		case errors.Is(err, pgx.ErrNoRows):
			return ErrNotFound
		case err != nil:
			return fmt.Errorf("lock instance: %w", err)
		}

		snap, restored, err := d.m.Restore(data)
		if err != nil {
			return fmt.Errorf("restore snapshot: %w", err)
		}

		var effects []sc.Effect[C]
		next, effects = d.m.Transition(snap, ev)

		data, err = json.Marshal(next)
		if err != nil {
			return fmt.Errorf("marshal snapshot: %w", err)
		}

		_, err = tx.Exec(ctx, `
			UPDATE statechart_instances
			SET seq = $3, snapshot = $4, updated_at = clock_timestamp()
			WHERE machine = $1 AND id = $2`,
			d.m.ID(), id, next.Seq(), data,
		)
		if err != nil {
			return fmt.Errorf("update instance: %w", err)
		}

		return d.enqueue(ctx, tx, id, append(restored, effects...))
	})
	if err != nil {
		return next, fmt.Errorf("send to instance %q: %w", id, err)
	}

	return next, nil
}

// enqueue records effects in the outbox. Cancellations remove the outbox row
// they refer to instead of adding one.
func (d *Driver[S, C]) enqueue(ctx context.Context, tx pgx.Tx, id string, effects []sc.Effect[C]) error {
	for _, e := range effects {
		switch e.Kind {
		case sc.EffectCancelTimer, sc.EffectStopInvocation:
			// A row a worker has locked is being carried out right
			// now. It is skipped, not waited for: the machine
			// ignores the event it produces.
			_, err := tx.Exec(ctx, `
				DELETE FROM statechart_effects
				WHERE id IN (
					SELECT id FROM statechart_effects
					WHERE machine = $1 AND instance_id = $2 AND effect_id = $3
					FOR UPDATE SKIP LOCKED
				)`,
				d.m.ID(), id, e.ID,
			)
			if err != nil {
				return fmt.Errorf("cancel effect %q: %w", e.ID, err)
			}
		case sc.EffectAction, sc.EffectStartInvocation, sc.EffectStartTimer:
			data, err := d.m.EncodeEffect(e)
			if err != nil {
				return fmt.Errorf("encode %s effect: %w", e.Kind, err)
			}

			_, err = tx.Exec(ctx, `
				INSERT INTO statechart_effects (machine, instance_id, kind, effect_id, effect, run_after)
				VALUES ($1, $2, $3, $4, $5, now() + $6 * interval '1 microsecond')`,
				d.m.ID(), id, string(e.Kind), e.ID, data, e.Delay.Microseconds(),
			)
			if err != nil {
				return fmt.Errorf("insert %s effect: %w", e.Kind, err)
			}
		}
	}

	return nil
}

// Work carries out due effects until ctx is cancelled or an effect cannot be
// processed, and returns the reason. Run it on as many goroutines or
// processes as the workload needs; a service occupies its worker until it
// finishes.
func (d *Driver[S, C]) Work(ctx context.Context) error {
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

// Step carries out at most one due effect and reports whether it found one.
// The effect's outbox row stays locked while it runs and is removed in the
// same transaction, so no other worker picks it up, and it becomes available
// again if this worker stops.
//
// The actions of one instance run in the order the machine produced them.
// Services and timers of the same instance are not held up by each other or
// by a running action.
func (d *Driver[S, C]) Step(ctx context.Context) (bool, error) {
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
			effectID string
			data     []byte
		)

		err := tx.QueryRow(ctx, `
			SELECT e.id, e.instance_id, e.kind, e.effect_id, e.effect
			FROM statechart_effects e
			WHERE e.machine = $1
			  AND e.run_after <= now()
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
		).Scan(&rowID, &instance, &kind, &effectID, &data)
		switch {
		case errors.Is(err, pgx.ErrNoRows):
			return nil
		case err != nil:
			return fmt.Errorf("claim effect: %w", err)
		}

		worked = true

		err = d.run(ctx, instance, sc.EffectKind(kind), effectID, data)
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

// run carries out one claimed effect and sends the events it produces to the
// instance.
func (d *Driver[S, C]) run(ctx context.Context, instance string, kind sc.EffectKind, effectID string, data []byte) error {
	if kind == sc.EffectStartTimer {
		_, err := d.Send(ctx, instance, sc.TimerFired{ID: effectID})
		return err
	}

	e, err := d.m.DecodeEffect(data)
	if err != nil {
		return fmt.Errorf("decode effect: %w", err)
	}

	var sendErr error
	err = d.m.Exec(ctx, e, func(ev any) {
		if sendErr == nil {
			_, sendErr = d.Send(ctx, instance, ev)
		}
	})

	// A service the chart no longer declares fails like any other service,
	// which hands the decision to the chart's own error transition.
	if kind == sc.EffectStartInvocation && errors.Is(err, sc.ErrUnknownEffect) {
		_, err = d.Send(ctx, instance, sc.Failed{ID: effectID, Err: err})
	}
	if err != nil {
		return err
	}

	return sendErr
}
