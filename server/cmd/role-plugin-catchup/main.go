// One-off role-plugin rollout catch-up. Not registered in the server CLI.
package main

import (
	"context"
	_ "embed"
	"flag"
	"fmt"
	"os"
	"os/signal"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/speakeasy-api/gram/server/internal/roledistribution/requests"
)

//go:embed queries.sql
var organizationsSQL string

func main() {
	apply := flag.Bool("apply", false, "Queue setup; without this flag, only count eligible organizations")
	flag.Parse()
	if flag.NArg() != 0 {
		fmt.Fprintln(os.Stderr, "unexpected positional arguments")
		os.Exit(1)
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	err := run(ctx, *apply)
	stop()
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func run(ctx context.Context, apply bool) error {
	// gram-infra's gcp:db:master command supplies DB_URL and PG* settings.
	url := os.Getenv("DB_URL")
	if url == "" {
		return fmt.Errorf("DB_URL is required; use the existing gram-infra database connection task")
	}
	db, err := pgxpool.New(ctx, url)
	if err != nil {
		return fmt.Errorf("configure database connection") // Do not echo a connection URL.
	}
	defer db.Close()
	n, err := catchUp(ctx, db, apply)
	if err != nil {
		return err
	}
	if !apply {
		fmt.Printf("Would queue organization setup for %d organizations. Re-run with -apply to enqueue.\n", n)
	} else {
		fmt.Printf("Queued organization setup for %d organizations. Processing continues through the existing event pipeline.\n", n)
	}
	return nil
}

func catchUp(ctx context.Context, db *pgxpool.Pool, apply bool) (int, error) {
	tx, err := db.Begin(ctx)
	if err != nil {
		return 0, fmt.Errorf("begin catch-up: %w", err)
	}
	defer func() { _ = tx.Rollback(context.WithoutCancel(ctx)) }()
	rows, err := tx.Query(ctx, organizationsSQL)
	if err != nil {
		return 0, fmt.Errorf("select catch-up organizations: %w", err)
	}
	organizations, err := pgx.CollectRows(rows, pgx.RowTo[string])
	if err != nil {
		return 0, fmt.Errorf("read catch-up organizations: %w", err)
	}
	if !apply {
		return len(organizations), nil
	}
	for _, org := range organizations {
		if err := requests.LockOrganization(ctx, tx, org); err != nil {
			return 0, fmt.Errorf("lock organization setup: %w", err)
		}
		if err := requests.ResumeOrganization(ctx, tx, org); err != nil {
			return 0, fmt.Errorf("queue organization setup: %w", err)
		}
	}
	if err := tx.Commit(ctx); err != nil {
		return 0, fmt.Errorf("commit catch-up: %w", err)
	}
	return len(organizations), nil
}
