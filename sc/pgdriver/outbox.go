package pgdriver

import (
	"context"
	"fmt"
	"math/rand/v2"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/speakeasy-api/gram/sc"
)

// Sink records the effects of a step inside the transaction that saves the
// step, so an effect is recorded if and only if the state change that caused
// it is. Whatever consumes the sink carries each effect out with
// [Driver.Perform].
//
// Effects arrive in the order they must take effect. A sink may ignore
// cancellations (timer.cancel and invocation.stop effects) if it cannot
// honour them: the machine ignores the events a cancelled timer or stopped
// service produces later.
//
// The default sink, [Outbox], keeps effects in the driver's own table and is
// consumed by [Driver.Work]. A sink can instead hand effects to an existing
// outbox, such as one that publishes to a message bus.
type Sink[C any] interface {
	// Enqueue records effects for the instance with the given ID. now is
	// the database time the step was taken at, which timers are measured
	// from.
	Enqueue(ctx context.Context, tx pgx.Tx, instance string, now time.Time, effects []sc.Effect[C]) error
}

// Outbox keeps effects in the statechart_effects table that [Schema]
// creates. [Driver.Work] carries them out.
type Outbox[S comparable, C any] struct {
	// Machine encodes the effects and identifies whose they are.
	Machine *sc.Machine[S, C]
}

// Enqueue inserts one row per effect to carry out, due at once for actions
// and services and at its time for a timer, and deletes the rows that a
// cancellation refers to.
func (o Outbox[S, C]) Enqueue(ctx context.Context, tx pgx.Tx, instance string, now time.Time, effects []sc.Effect[C]) error {
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
				o.Machine.ID(), instance, e.ID,
			)
			if err != nil {
				return fmt.Errorf("cancel effect %q: %w", e.ID, err)
			}
		case sc.EffectAction, sc.EffectStartInvocation, sc.EffectStartTimer:
			data, err := o.Machine.EncodeEffect(e)
			if err != nil {
				return fmt.Errorf("encode %s effect: %w", e.Kind, err)
			}

			// A timer is due at the time the core gave it, measured
			// by the database's clock like now, varied by its jitter.
			runAfter := now
			if e.Kind == sc.EffectStartTimer {
				runAfter = now.Add(e.Wait(now, rand.Float64))
			}

			_, err = tx.Exec(ctx, `
				INSERT INTO statechart_effects (machine, instance_id, kind, effect_id, effect, run_after)
				VALUES ($1, $2, $3, $4, $5, $6)`,
				o.Machine.ID(), instance, string(e.Kind), e.ID, data, runAfter,
			)
			if err != nil {
				return fmt.Errorf("insert %s effect: %w", e.Kind, err)
			}
		}
	}

	return nil
}
