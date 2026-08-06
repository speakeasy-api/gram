// Package pgwal provides helpers for coordinating read-your-writes consistency
// across a Postgres primary and its read replicas using WAL log sequence
// numbers (LSNs).
//
// Typical usage: after committing a write on the primary, capture the current
// LSN, then block until a read replica has replayed up to that LSN before
// reading from it:
//
//	lsn, err := pgwal.CurrentLSN(ctx, primaryPool)
//	if err != nil { ... }
//	// ... hand lsn to the read path ...
//	if err := pgwal.WaitForLSN(ctx, replicaPool, lsn); err != nil { ... }
package pgwal

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"

	"github.com/speakeasy-api/gram/server/internal/pgwal/repo"
)

// defaultPollInterval is how often WaitForLSN re-checks replay progress when no
// override is supplied. Kept small so read-your-writes latency stays low.
const defaultPollInterval = 50 * time.Millisecond

// ErrWaitTimeout is returned by WaitForLSN when the WithTimeout deadline elapses
// before the replica catches up. Distinguished from parent-context cancellation
// (which returns the context error); callers can test it with errors.Is.
var ErrWaitTimeout = errors.New("pgwal: timed out waiting for replica to catch up")

// DBTX is the minimal query surface these helpers need. Both *pgxpool.Pool and
// pgx.Tx satisfy it, so callers can pass a pool or an in-flight transaction.
type DBTX interface {
	Exec(context.Context, string, ...any) (pgconn.CommandTag, error)
	Query(context.Context, string, ...any) (pgx.Rows, error)
	QueryRow(context.Context, string, ...any) pgx.Row
}

// LSN is a Postgres WAL log sequence number (e.g. "0/16B3748"). Its field is
// unexported so an LSN can only be produced by CurrentLSN within this package —
// callers cannot fabricate one. Use String to read its text form for logging.
type LSN struct {
	value string
}

// String returns the Postgres text form of the LSN.
func (l LSN) String() string {
	return l.value
}

// CurrentLSN returns pg_current_wal_lsn(), the current WAL insert position.
// Call this on the PRIMARY after a write to capture a point the read path can
// later wait for via WaitForLSN.
func CurrentLSN(ctx context.Context, db DBTX) (LSN, error) {
	lsn, err := repo.New(db).CurrentWALLSN(ctx)
	if err != nil {
		return LSN{}, fmt.Errorf("query current wal lsn: %w", err)
	}

	return LSN{value: lsn}, nil
}

// waitOptions holds the resolved configuration for WaitForLSN.
type waitOptions struct {
	pollInterval time.Duration
	timeout      time.Duration
}

// Option configures WaitForLSN.
type Option func(*waitOptions)

// WithPollInterval overrides how often WaitForLSN re-checks replay progress.
// Non-positive values are ignored and the default (50ms) is used.
func WithPollInterval(d time.Duration) Option {
	return func(o *waitOptions) {
		if d > 0 {
			o.pollInterval = d
		}
	}
}

// WithTimeout bounds the total wait. When the timeout elapses before the
// replica catches up, WaitForLSN returns ErrWaitTimeout. A non-positive value
// leaves the wait unbounded (governed only by the caller's context).
func WithTimeout(d time.Duration) Option {
	return func(o *waitOptions) {
		o.timeout = d
	}
}

// WaitForLSN blocks until db has replayed WAL up to target. It returns:
//
//   - nil once the replica has caught up. Against a primary this happens on the
//     first check, since pg_last_wal_replay_lsn() is NULL when not in recovery.
//   - ErrWaitTimeout if a WithTimeout deadline elapses first.
//   - the context error (context.Canceled / context.DeadlineExceeded) if the
//     caller's ctx is cancelled or expires first.
func WaitForLSN(ctx context.Context, db DBTX, target LSN, opts ...Option) error {
	cfg := waitOptions{pollInterval: defaultPollInterval, timeout: 0}
	for _, opt := range opts {
		opt(&cfg)
	}

	// When a timeout is configured, derive a child context so both the poll
	// cadence and any in-flight query are bounded by it.
	waitCtx := ctx
	if cfg.timeout > 0 {
		var cancel context.CancelFunc
		waitCtx, cancel = context.WithTimeout(ctx, cfg.timeout)
		defer cancel()
	}

	queries := repo.New(db)
	ticker := time.NewTicker(cfg.pollInterval)
	defer ticker.Stop()

	for {
		caughtUp, err := queries.WALReplayCaughtUp(waitCtx, target.String())
		switch {
		case err == nil:
			if caughtUp {
				return nil
			}
		case errors.Is(err, context.DeadlineExceeded), errors.Is(err, context.Canceled):
			// The query was cut off by waitCtx; classify the same way as when
			// the select below observes waitCtx.Done.
			return classifyWaitErr(ctx)
		default:
			return fmt.Errorf("query wal replay progress: %w", err)
		}

		select {
		case <-ticker.C:
		case <-waitCtx.Done():
			return classifyWaitErr(ctx)
		}
	}
}

// classifyWaitErr maps a finished waitCtx to the caller-facing error. When the
// parent context is the cause, its error is returned (wrapped); otherwise only
// our own WithTimeout deadline could have tripped, so the sentinel is returned.
func classifyWaitErr(parent context.Context) error {
	if err := parent.Err(); err != nil {
		return fmt.Errorf("wait for wal replay: %w", err)
	}

	return ErrWaitTimeout
}
