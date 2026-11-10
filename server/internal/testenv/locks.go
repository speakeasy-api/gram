package testenv

import (
	"context"
	"fmt"
	"strconv"
	"testing"
	"time"

	"github.com/jackc/pgerrcode"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/conv"
	"github.com/speakeasy-api/gram/server/internal/testenv/testrepo"
)

const (
	// lockWaitTimeout bounds a missing-contention failure even without a caller deadline.
	lockWaitTimeout = 30 * time.Second
	// lockPollInterval avoids busy polling while observing actual database contention.
	lockPollInterval = 10 * time.Millisecond
)

// BackendPID identifies the connection holding tx's locks, without another query.
func BackendPID(tx pgx.Tx) uint32 { return tx.Conn().PgConn().PID() }

// WaitForBackendsBlockedBy waits for at least want distinct backends blocked by
// holderPID, including waiters behind other waiters. The holder must stay alive
// and locked until this returns. db needs a free connection for observation.
func WaitForBackendsBlockedBy(t *testing.T, ctx context.Context, db *pgxpool.Pool, holderPID uint32, want int) {
	t.Helper()
	require.Positive(t, want)
	pid, clamped := conv.ClampedUintToInt32(uint(holderPID))
	require.False(t, clamped, "holder PID exceeds PostgreSQL integer range")
	err := waitForLock(ctx, func(ctx context.Context) (bool, error) {
		count, err := testrepo.New(db).CountBackendsBlockedByFixture(ctx, pid)
		if err != nil {
			return false, fmt.Errorf("count blocked backends: %w", err)
		}
		return count >= int64(want), nil
	})
	require.NoError(t, err, "waiting for %d backends blocked by PID %d", want, holderPID)
}

// WaitForQueryBlockedBy waits for an active query matching a SQL LIKE pattern
// in holderPID's blocker chain. Keep the holder locked and reserve observer
// capacity in db; a matching query blocked by an unrelated holder is ignored.
func WaitForQueryBlockedBy(t *testing.T, ctx context.Context, db *pgxpool.Pool, holderPID uint32, queryPattern string) {
	t.Helper()
	pid, clamped := conv.ClampedUintToInt32(uint(holderPID))
	require.False(t, clamped, "holder PID exceeds PostgreSQL integer range")
	err := waitForLock(ctx, func(ctx context.Context) (bool, error) {
		return testrepo.New(db).IsQueryBlockedByFixture(ctx, testrepo.IsQueryBlockedByFixtureParams{
			HolderPid: pid, QueryPattern: queryPattern,
		})
	})
	require.NoError(t, err, "waiting for query %q blocked by PID %d", queryPattern, holderPID)
}

func waitForLock(ctx context.Context, check func(context.Context) (bool, error)) error {
	ctx, cancel := context.WithTimeout(ctx, lockWaitTimeout)
	defer cancel()
	ticker := time.NewTicker(lockPollInterval)
	defer ticker.Stop()
	for {
		if err := ctx.Err(); err != nil {
			return fmt.Errorf("wait for database lock: %w", err)
		}
		ready, err := check(ctx)
		if err != nil {
			return fmt.Errorf("observe lock contention: %w", err)
		}
		if ready {
			return nil
		}
		select {
		case <-ctx.Done():
			return fmt.Errorf("wait for database lock: %w", ctx.Err())
		case <-ticker.C:
		}
	}
}

// SetLockTimeout bounds lock acquisition in tx. Use a narrow probe and roll
// back after a timeout, since PostgreSQL aborts the failed transaction.
func SetLockTimeout(t *testing.T, ctx context.Context, tx pgx.Tx, d time.Duration) {
	t.Helper()
	require.GreaterOrEqual(t, d, time.Millisecond, "lock_timeout must not round to zero (disabled)")
	_, err := testrepo.New(tx).SetLocalLockTimeoutFixture(ctx, strconv.FormatInt(d.Milliseconds(), 10))
	require.NoError(t, err)
}

// NewLockTimeoutPool copies db's configuration and bounds lock acquisition on
// every new connection. Inject it into services that acquire their own sessions.
// The test must release its holders and finish contenders before pool cleanup.
func NewLockTimeoutPool(t *testing.T, db *pgxpool.Pool, d time.Duration) *pgxpool.Pool {
	t.Helper()
	require.GreaterOrEqual(t, d, time.Millisecond, "lock_timeout must not round to zero (disabled)")
	config := db.Config()
	config.ConnConfig.RuntimeParams["lock_timeout"] = strconv.FormatInt(d.Milliseconds(), 10)
	pool, err := pgxpool.NewWithConfig(t.Context(), config)
	require.NoError(t, err)
	t.Cleanup(pool.Close)
	return pool
}

// RequireLockNotAvailable asserts PostgreSQL's lock_not_available SQLSTATE,
// including errors wrapped by service handlers. A client deadline is not proof
// that PostgreSQL reached a lock.
func RequireLockNotAvailable(t *testing.T, err error) {
	t.Helper()
	var pgerr *pgconn.PgError
	require.ErrorAs(t, err, &pgerr)
	require.Equal(t, pgerrcode.LockNotAvailable, pgerr.Code)
}
