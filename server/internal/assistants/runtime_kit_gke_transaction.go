package assistants

import (
	"context"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// kitLifecycleTransaction keeps the same connection and session advisory fence
// while publishing recovery phases in successive transactions. Existing writers
// use the identical transaction-lock key and therefore remain excluded across
// commits. The explicit pgx.Tx handle stays stable for query/provisioning helpers.
// No caller may retain a nested transaction across Publish.
type kitLifecycleTransaction struct {
	pgx.Tx
	conn *pgxpool.Conn
}

func (t *kitLifecycleTransaction) Publish(ctx context.Context) error {
	if err := t.Commit(ctx); err != nil {
		return fmt.Errorf("publish Kit lifecycle phase: %w", err)
	}
	next, err := t.conn.Begin(ctx)
	if err != nil {
		return fmt.Errorf("resume Kit lifecycle transaction: %w", err)
	}
	t.Tx = next
	return nil
}

func beginKitLifecycleTransaction(ctx context.Context, pool *pgxpool.Pool, assistantID string) (*kitLifecycleTransaction, func(), error) {
	conn, err := pool.Acquire(ctx)
	if err != nil {
		return nil, nil, fmt.Errorf("acquire Kit lifecycle connection: %w", err)
	}
	var lifecycle *kitLifecycleTransaction
	cleanup := func() {
		cleanupCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 10*time.Second)
		defer cancel()
		if lifecycle != nil {
			_ = lifecycle.Rollback(cleanupCtx)
		}
		// Always attempt unlock, including a canceled acquisition with uncertain
		// completion. Never return a connection with session locks to the pool.
		var unlocked bool
		err := conn.QueryRow(cleanupCtx, "SELECT pg_advisory_unlock(hashtext('asst:' || $1::text))", assistantID).Scan(&unlocked)
		if err != nil {
			raw := conn.Hijack()
			_ = raw.Close(cleanupCtx)
			return
		}
		conn.Release()
	}
	if _, err = conn.Exec(ctx, "SELECT pg_advisory_lock(hashtext('asst:' || $1::text))", assistantID); err != nil {
		cleanup()
		return nil, nil, fmt.Errorf("lock Kit lifecycle: %w", err)
	}
	tx, err := conn.Begin(ctx)
	if err != nil {
		cleanup()
		return nil, nil, fmt.Errorf("begin Kit lifecycle transaction: %w", err)
	}
	lifecycle = &kitLifecycleTransaction{Tx: tx, conn: conn}
	return lifecycle, cleanup, nil
}
