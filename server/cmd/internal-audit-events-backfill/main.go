// Command internal-audit-events-backfill classifies historical internal audit rows.
package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

type options struct {
	before                time.Time
	batchSize, maxBatches int
	apply                 bool
}

func parseOptions(args []string) (options, error) {
	var o options
	fs := flag.NewFlagSet("internal-audit-events-backfill", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	before := fs.String("before", "", "Required exclusive RFC3339 cutoff")
	fs.IntVar(&o.batchSize, "batch-size", 500, "Rows per transaction (1..5000)")
	fs.IntVar(&o.maxBatches, "max-batches", 20, "Maximum batches per table")
	fs.BoolVar(&o.apply, "apply", false, "Commit updates; default counts only")
	if err := fs.Parse(args); err != nil {
		return o, errors.New("invalid flags: require --before RFC3339; optional --batch-size 1..5000, --max-batches positive, --apply")
	}
	var err error
	o.before, err = time.Parse(time.RFC3339, *before)
	if err != nil {
		return o, errors.New("--before is required and must be RFC3339")
	}
	if fs.NArg() != 0 || o.batchSize < 1 || o.batchSize > 5000 || o.maxBatches < 1 {
		return o, errors.New("unexpected arguments or invalid bounds: --batch-size 1..5000; --max-batches positive")
	}
	return o, nil
}

func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func run() error {
	o, err := parseOptions(os.Args[1:])
	if err != nil {
		return err
	}
	url := os.Getenv("GRAM_DATABASE_URL")
	if url == "" {
		return errors.New("GRAM_DATABASE_URL is required")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	pool, err := pgxpool.New(ctx, url)
	if err != nil {
		cancel()
		return errors.New("invalid database configuration (connection details withheld)")
	}
	defer pool.Close()
	err = pool.Ping(ctx)
	cancel()
	if err != nil {
		return errors.New("database connection failed (connection details withheld)")
	}
	return backfill(context.Background(), pool, o, os.Stdout)
}

// SQL is fixed for both supported tables; there is no operator-supplied SQL.
var tables = [...]struct{ name, count, update string }{
	{"audit_logs", `SELECT count(*) FROM audit_logs WHERE NOT is_internal AND created_at < $1 AND (subject_type = 'catalog_promotion' OR action = 'mcp-server:promote-catalogue')`,
		`WITH batch AS (SELECT id FROM audit_logs WHERE NOT is_internal AND created_at < $1 AND (subject_type = 'catalog_promotion' OR action = 'mcp-server:promote-catalogue') ORDER BY created_at,id LIMIT $2 FOR UPDATE) UPDATE audit_logs a SET is_internal=true FROM batch b WHERE a.id=b.id AND NOT a.is_internal`},
	{"security_audit_logs", `SELECT count(*) FROM security_audit_logs WHERE NOT is_internal AND created_at < $1 AND (subject_type = 'catalog_promotion' OR action = 'mcp-server:promote-catalogue')`,
		`WITH batch AS (SELECT id FROM security_audit_logs WHERE NOT is_internal AND created_at < $1 AND (subject_type = 'catalog_promotion' OR action = 'mcp-server:promote-catalogue') ORDER BY created_at,id LIMIT $2 FOR UPDATE) UPDATE security_audit_logs a SET is_internal=true FROM batch b WHERE a.id=b.id AND NOT a.is_internal`},
}

func backfill(ctx context.Context, pool *pgxpool.Pool, o options, out io.Writer) (err error) {
	var committed int64
	defer func() {
		if err != nil {
			err = fmt.Errorf("backfill stopped; previous batches committed rows=%d; a failed commit may have an unknown outcome; inspect counts before rerunning: %w", committed, err)
		}
	}()
	for _, table := range tables {
		before, err := countEligible(ctx, pool, table.count, o.before)
		if err != nil {
			return fmt.Errorf("%s before count: %w", table.name, err)
		}
		if _, err = fmt.Fprintf(out, "%s apply=%t before=%s eligible_before=%d\n", table.name, o.apply, o.before.Format(time.RFC3339Nano), before); err != nil {
			return err
		}
		var updated int64
		batches := 0
		if o.apply {
			for batches < o.maxBatches {
				n, err := updateBatch(ctx, pool, table.update, o)
				if err != nil {
					return fmt.Errorf("%s batch: %w", table.name, err)
				}
				if n == 0 {
					break
				}
				committed += n
				updated += n
				batches++
				if _, err = fmt.Fprintf(out, "%s batch=%d committed_rows=%d\n", table.name, batches, n); err != nil {
					return err
				}
			}
		}
		remaining, err := countEligible(ctx, pool, table.count, o.before)
		if err != nil {
			return fmt.Errorf("%s after count: %w", table.name, err)
		}
		if _, err = fmt.Fprintf(out, "%s updated=%d batches=%d remaining=%d\n", table.name, updated, batches, remaining); err != nil {
			return err
		}
		if remaining > 0 {
			if _, err = fmt.Fprintln(out, "Eligible rows remain; review counts and rerun with --apply to continue."); err != nil {
				return err
			}
		}
	}
	return nil
}

func countEligible(ctx context.Context, pool *pgxpool.Pool, sql string, before time.Time) (int64, error) {
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	var n int64
	if err := pool.QueryRow(ctx, sql, before).Scan(&n); err != nil {
		return 0, errors.New("database count failed or timed out (connection details withheld)")
	}
	return n, nil
}

func updateBatch(ctx context.Context, pool *pgxpool.Pool, sql string, o options) (int64, error) {
	ctx, cancel := context.WithTimeout(ctx, 20*time.Second)
	defer cancel()
	tx, err := pool.Begin(ctx)
	if err != nil {
		return 0, errors.New("begin transaction failed")
	}
	defer func() {
		rollbackCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = tx.Rollback(rollbackCtx)
	}()
	if _, err = tx.Exec(ctx, `SET LOCAL lock_timeout = '2s'; SET LOCAL statement_timeout = '15s'`); err != nil {
		return 0, errors.New("set transaction timeouts failed")
	}
	result, err := tx.Exec(ctx, sql, o.before, o.batchSize)
	if err != nil {
		return 0, errors.New("update failed or timed out; batch rolled back")
	}
	if err = tx.Commit(ctx); err != nil {
		return 0, errors.New("commit failed; outcome may be unknown")
	}
	return result.RowsAffected(), nil
}
