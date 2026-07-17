// Command backfill_ai_integration_sync_schedules fills nullable primary
// schedule discriminators and creates missing Anthropic analytics schedules.
//
// Run it once per project after the Phase 3 index transition has deployed:
//
//	GRAM_DATABASE_URL=postgres://USER:PASS@127.0.0.1:5432/gram \
//	  go run ./server/cmd/tools/backfill_ai_integration_sync_schedules \
//	  -project 01900000-0000-7000-8000-000000000000
//	GRAM_DATABASE_URL=postgres://USER:PASS@127.0.0.1:5432/gram \
//	  go run ./server/cmd/tools/backfill_ai_integration_sync_schedules \
//	  -project 01900000-0000-7000-8000-000000000000 -dry-run=false
//
// The default run is read-only. Applied runs commit one idempotent batch at a
// time and print the last config ID, which can be passed back with -cursor
// after an interruption. Re-running from the beginning is also safe.
package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"log"
	"os"
	"os/signal"
	"syscall"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/speakeasy-api/gram/server/internal/aiintegrations"
)

const (
	defaultBatchSize = 500
	maxBatchSize     = 10_000
)

type config struct {
	dbURL     string
	projectID uuid.UUID
	cursor    uuid.UUID
	batchSize int32
	dryRun    bool
}

type backfiller interface {
	Status(context.Context) (aiintegrations.SyncScheduleBackfillStatus, error)
	BackfillBatch(context.Context, uuid.UUID, int32) (aiintegrations.SyncScheduleBackfillBatch, error)
}

func main() {
	os.Exit(run(os.Args[1:], os.Stdout))
}

func run(args []string, out io.Writer) int {
	cfg, err := parseFlags(args)
	if err != nil {
		log.Printf("invalid arguments: %v", err)
		return 2
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	pool, err := pgxpool.New(ctx, cfg.dbURL)
	if err != nil {
		log.Printf("connect database: %v", err)
		return 1
	}
	defer pool.Close()

	if err := pool.Ping(ctx); err != nil {
		log.Printf("ping database: %v", err)
		return 1
	}

	worker := aiintegrations.NewSyncScheduleBackfiller(pool, cfg.projectID)
	if err := execute(ctx, out, worker, cfg); err != nil {
		if !errors.Is(err, context.Canceled) {
			log.Printf("backfill failed: %v", err)
		}
		return 1
	}
	return 0
}

func parseFlags(args []string) (config, error) {
	flags := flag.NewFlagSet("backfill_ai_integration_sync_schedules", flag.ContinueOnError)
	flags.SetOutput(io.Discard)

	dbURL := flags.String("db", os.Getenv("GRAM_DATABASE_URL"), "Postgres connection string (defaults to $GRAM_DATABASE_URL)")
	projectText := flags.String("project", "", "project_id to backfill (required)")
	cursorText := flags.String("cursor", "", "last successfully processed ai_integration_config id")
	batchSize := flags.Int("batch-size", defaultBatchSize, "configs processed per committed batch")
	dryRun := flags.Bool("dry-run", true, "when true (default) only report; pass -dry-run=false to write")

	if err := flags.Parse(args); err != nil {
		return config{}, fmt.Errorf("parse flags: %w", err)
	}
	if flags.NArg() != 0 {
		return config{}, fmt.Errorf("unexpected positional arguments: %v", flags.Args())
	}
	if *dbURL == "" {
		return config{}, fmt.Errorf("missing -db / $GRAM_DATABASE_URL")
	}
	projectID, err := uuid.Parse(*projectText)
	if err != nil {
		return config{}, fmt.Errorf("invalid -project: %w", err)
	}
	if *batchSize <= 0 || *batchSize > maxBatchSize {
		return config{}, fmt.Errorf("-batch-size must be in [1, %d]", maxBatchSize)
	}

	cursor := uuid.Nil
	if *cursorText != "" {
		cursor, err = uuid.Parse(*cursorText)
		if err != nil {
			return config{}, fmt.Errorf("invalid -cursor: %w", err)
		}
	}

	return config{
		dbURL:     *dbURL,
		projectID: projectID,
		cursor:    cursor,
		batchSize: int32(*batchSize),
		dryRun:    *dryRun,
	}, nil
}

func execute(ctx context.Context, out io.Writer, worker backfiller, cfg config) error {
	before, err := worker.Status(ctx)
	if err != nil {
		return fmt.Errorf("get status before backfill: %w", err)
	}
	if err := printStatus(out, "before", before); err != nil {
		return err
	}

	if cfg.dryRun {
		if _, err := fmt.Fprintln(out, "mode: DRY RUN (no writes); re-run with -dry-run=false to apply"); err != nil {
			return fmt.Errorf("write dry-run summary: %w", err)
		}
		return nil
	}

	cursor := cfg.cursor
	var configs, primaries, analytics int
	for {
		if err := ctx.Err(); err != nil {
			return fmt.Errorf("backfill interrupted; resume with -cursor=%s: %w", cursor, err)
		}

		batch, err := worker.BackfillBatch(ctx, cursor, cfg.batchSize)
		if err != nil {
			return fmt.Errorf("resume with -cursor=%s: %w", cursor, err)
		}
		if batch.ConfigsProcessed == 0 {
			break
		}

		cursor = batch.LastConfigID
		configs += batch.ConfigsProcessed
		primaries += batch.PrimarySyncsUpdated
		analytics += batch.AnalyticsSchedulesCreated
		if _, err := fmt.Fprintf(
			out,
			"batch: configs=%d primary_updated=%d analytics_created=%d cursor=%s\n",
			batch.ConfigsProcessed,
			batch.PrimarySyncsUpdated,
			batch.AnalyticsSchedulesCreated,
			cursor,
		); err != nil {
			return fmt.Errorf("write batch progress: %w", err)
		}
	}

	after, err := worker.Status(ctx)
	if err != nil {
		return fmt.Errorf("get status after backfill: %w", err)
	}
	if _, err := fmt.Fprintf(out, "applied: configs=%d primary_updated=%d analytics_created=%d\n", configs, primaries, analytics); err != nil {
		return fmt.Errorf("write backfill summary: %w", err)
	}
	if err := printStatus(out, "after", after); err != nil {
		return err
	}
	if after.PrimarySyncsPending != 0 || after.AnalyticsSyncsPending != 0 {
		return fmt.Errorf(
			"backfill incomplete: primary_pending=%d analytics_pending=%d; rerun from the zero cursor",
			after.PrimarySyncsPending,
			after.AnalyticsSyncsPending,
		)
	}
	return nil
}

func printStatus(out io.Writer, label string, status aiintegrations.SyncScheduleBackfillStatus) error {
	if _, err := fmt.Fprintf(
		out,
		"%s: primary_pending=%d analytics_pending=%d\n",
		label,
		status.PrimarySyncsPending,
		status.AnalyticsSyncsPending,
	); err != nil {
		return fmt.Errorf("write %s backfill status: %w", label, err)
	}
	return nil
}
