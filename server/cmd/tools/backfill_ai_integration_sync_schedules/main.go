// Command backfill_ai_integration_sync_schedules is a one-off production
// backfill for ai_integration_syncs schedules.
//
// Run manually after deploying the analytics application code:
//
//	GRAM_DATABASE_URL=postgres://USER:PASS@HOST:5432/gram \
//	  go run ./server/cmd/tools/backfill_ai_integration_sync_schedules
//
// The statement labels existing primary rows and creates the two Anthropic
// analytics schedules. It is idempotent, so a successful rerun reports zeros.
package main

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/signal"
	"syscall"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/speakeasy-api/gram/server/internal/aiintegrations/repo"
)

func main() {
	if err := run(); err != nil {
		_, _ = fmt.Fprintf(os.Stderr, "backfill failed: %v\n", err)
		os.Exit(1)
	}
}

func run() error {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	databaseURL := os.Getenv("GRAM_DATABASE_URL")
	if databaseURL == "" {
		return errors.New("GRAM_DATABASE_URL is required")
	}

	pool, err := pgxpool.New(ctx, databaseURL)
	if err != nil {
		return fmt.Errorf("connect database: %w", err)
	}
	defer pool.Close()

	result, err := repo.New(pool).BackfillSyncSchedules(ctx)
	if err != nil {
		return fmt.Errorf("backfill sync schedules: %w", err)
	}

	if _, err := fmt.Fprintf(
		os.Stdout,
		"updated primary sync rows: %d\ncreated analytics schedules: %d\n",
		result.PrimarySyncsUpdated,
		result.AnalyticsSchedulesCreated,
	); err != nil {
		return fmt.Errorf("write backfill result: %w", err)
	}
	return nil
}
