package mcpregistry

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/speakeasy-api/gram/server/internal/mcpregistry/repo"
	"github.com/stretchr/testify/require"
)

// Both writers hold a conflicting relation lock before refresh acquires the
// mapping lock. A real NOWAIT failure proves that refresh reached that boundary;
// the writer must then be able to claim mappings without a lock-order deadlock.
func TestCatalogRefreshConcurrentWriters(t *testing.T) {
	t.Parallel()
	for _, composed := range []bool{false, true} {
		name := "row_lock_then_mapping"
		if composed {
			name = "unmapped_create_then_mapped_create"
		}
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			ctx, s, db := newTestService(t)
			ctx, cancel := context.WithTimeout(ctx, 15*time.Second)
			defer cancel()
			var existing Entry
			if !composed {
				var err error
				existing, err = s.Create(ctx, json.RawMessage(basicRecord))
				require.NoError(t, err)
			}
			tx, err := db.Begin(ctx) //nolint:glint // notestingrawsql: retain real writer locks across refresh admission.
			require.NoError(t, err)
			defer func() { _ = tx.Rollback(context.Background()) }()
			if composed {
				_, err = s.CreateTx(ctx, tx, json.RawMessage(basicRecord))
			} else {
				_, err = repo.New(tx).LockEntry(ctx, existing.ID)
			}
			require.NoError(t, err)
			trace := &catalogRetryTrace{retried: make(chan struct{})}
			cfg := db.Config()
			cfg.ConnConfig.Tracer = trace
			refreshDB, err := pgxpool.NewWithConfig(ctx, cfg)
			require.NoError(t, err)
			defer refreshDB.Close()
			done := make(chan error, 1)
			go func() {
				refreshTx, err := New(refreshDB, s.validator).beginCatalogRefresh(ctx)
				if err == nil {
					err = refreshTx.Rollback(ctx)
				}
				done <- err
			}()
			select {
			case <-trace.retried:
			case <-ctx.Done():
				t.Fatal(ctx.Err())
			}
			_, err = s.CreateTx(ctx, tx, json.RawMessage(oktaRecord("example.test/mapped-writer", "mapped-writer")))
			require.NoError(t, err, "refresh must release its advisory lock after failed NOWAIT")
			require.NoError(t, tx.Commit(ctx))
			select {
			case err := <-done:
				require.NoError(t, err)
			case <-ctx.Done():
				t.Fatal(ctx.Err())
			}
			_, err = s.GetByName(ctx, "example.test/mapped-writer")
			require.NoError(t, err)
		})
	}
}

func TestCatalogRefreshCancellationReleasesLocks(t *testing.T) {
	t.Parallel()
	for _, advisoryWait := range []bool{false, true} {
		name := "table_retry"
		if advisoryWait {
			name = "advisory_wait"
		}
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			ctx, s, db := newTestService(t)
			ctx, cancel := context.WithTimeout(ctx, 15*time.Second)
			defer cancel()
			blocker, err := db.Begin(ctx) //nolint:glint // notestingrawsql: schedule cancellation under actual PostgreSQL contention.
			require.NoError(t, err)
			defer func() { _ = blocker.Rollback(context.Background()) }()
			if advisoryWait {
				require.NoError(t, repo.New(blocker).LockOktaMappings(ctx))
			} else {
				_, err = s.CreateTx(ctx, blocker, json.RawMessage(basicRecord))
				require.NoError(t, err)
			}
			trace := &catalogRetryTrace{retried: make(chan struct{})}
			cfg := db.Config()
			cfg.MaxConns = 1
			cfg.ConnConfig.Tracer = trace
			refreshDB, err := pgxpool.NewWithConfig(ctx, cfg)
			require.NoError(t, err)
			defer refreshDB.Close()
			conn, err := refreshDB.Acquire(ctx)
			require.NoError(t, err)
			pid := conn.Conn().PgConn().PID()
			conn.Release()
			refreshCtx, cancelRefresh := context.WithCancel(ctx)
			defer cancelRefresh()
			done := make(chan error, 1)
			go func() {
				tx, err := New(refreshDB, s.validator).beginCatalogRefresh(refreshCtx)
				if tx != nil {
					_ = tx.Rollback(context.Background())
				}
				done <- err
			}()
			if advisoryWait {
				require.Eventually(t, func() bool {
					var waiting bool
					err := db.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM pg_locks WHERE pid = $1 AND locktype = 'advisory' AND NOT granted)`, pid).Scan(&waiting) //nolint:glint // notestingrawsql: observe the exact refresh backend before cancellation.
					return err == nil && waiting
				}, 5*time.Second, 10*time.Millisecond)
			} else {
				select {
				case <-trace.retried:
				case <-ctx.Done():
					t.Fatal(ctx.Err())
				}
			}
			cancelRefresh()
			select {
			case err := <-done:
				require.ErrorIs(t, err, context.Canceled)
			case <-time.After(2 * time.Second):
				t.Fatal("refresh cancellation did not return promptly")
			}
			require.Eventually(t, func() bool {
				var locked bool
				err := db.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM pg_locks WHERE pid = $1 AND (locktype = 'advisory' OR relation = 'mcp_registry_entries'::regclass))`, pid).Scan(&locked) //nolint:glint // notestingrawsql: verify cancellation leaves neither held nor waiting refresh locks.
				return err == nil && !locked
			}, 2*time.Second, 10*time.Millisecond)
			require.NoError(t, blocker.Rollback(ctx))
			// A single-connection pool also detects connections stranded by cancellation.
			tx, err := New(refreshDB, s.validator).beginCatalogRefresh(ctx)
			require.NoError(t, err)
			require.NoError(t, tx.Rollback(ctx))
			require.NoError(t, refreshDB.Ping(ctx))
		})
	}
}

func TestCatalogRefreshExclusiveAllowsReaders(t *testing.T) {
	t.Parallel()
	ctx, s, db := newTestService(t)
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	_, err := s.Create(ctx, json.RawMessage(basicRecord))
	require.NoError(t, err)
	tx, err := s.beginCatalogRefresh(ctx)
	require.NoError(t, err)
	defer func() { _ = tx.Rollback(context.Background()) }()
	var exclusive bool
	err = db.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM pg_locks WHERE pid = $1 AND relation = 'mcp_registry_entries'::regclass AND mode = 'ExclusiveLock' AND granted)`, tx.Conn().PgConn().PID()).Scan(&exclusive) //nolint:glint // notestingrawsql: establish that the production transaction holds EXCLUSIVE throughout the read.
	require.NoError(t, err)
	require.True(t, exclusive)
	reader, err := db.BeginTx(ctx, pgx.TxOptions{AccessMode: pgx.ReadOnly}) //nolint:glint // notestingrawsql: a separate PostgreSQL backend must read while refresh holds EXCLUSIVE.
	require.NoError(t, err)
	defer func() { _ = reader.Rollback(context.Background()) }()
	_, err = repo.New(reader).GetEntryByName(ctx, "example.test/demo")
	require.NoError(t, err, "ordinary catalog reads must not wait for refresh")
	require.NoError(t, reader.Commit(ctx))
	require.NoError(t, tx.Rollback(ctx))
}
