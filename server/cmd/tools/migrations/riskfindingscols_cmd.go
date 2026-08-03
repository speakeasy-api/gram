package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"log"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/ClickHouse/clickhouse-go/v2"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/speakeasy-api/gram/server/cmd/tools/migrations/pipeline"
	"github.com/speakeasy-api/gram/server/cmd/tools/migrations/riskfindingscols"
)

// colsConfig configures the riskfindingscols migration: backfilling the
// message_created_at and assistant_id columns onto existing ClickHouse
// risk_findings rows via mutations. See RISKFINDINGS_COLS_MIGRATION.md.
type colsConfig struct {
	dbURL      string
	ch         clickhouseConfig
	orgID      string
	projectID  uuid.NullUUID
	from       *time.Time
	to         *time.Time
	cursor     uuid.NullUUID
	batchSize  int
	bufferSize int
	dryRun     bool
}

func runRiskFindingsCols(args []string) int {
	cfg, err := parseColsFlags(args)
	if err != nil {
		log.Printf("invalid arguments: %v", err)
		return 2
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	pool, err := pgxpool.New(ctx, cfg.dbURL)
	if err != nil {
		log.Printf("connect postgres: %v", err)
		return 1
	}
	defer pool.Close()
	if err := pool.Ping(ctx); err != nil {
		log.Printf("ping postgres: %v", err)
		return 1
	}

	var chConn clickhouse.Conn
	if !cfg.dryRun {
		chConn, err = openClickhouse(ctx, cfg.ch)
		if err != nil {
			log.Printf("connect clickhouse: %v", err)
			return 1
		}
		defer func() {
			if cerr := chConn.Close(); cerr != nil {
				log.Printf("close clickhouse: %v", cerr)
			}
		}()
	}

	source := riskfindingscols.NewSource(pool)
	transformer := riskfindingscols.NewTransformer()
	sink := riskfindingscols.NewSink(chConn, cfg.bufferSize, cfg.batchSize, cfg.dryRun)

	runErr := pipeline.Run[riskfindingscols.SourceRow, riskfindingscols.UpdateRow](
		ctx, source, transformer, sink, cfg.criteria(), cfg.bufferSize,
	)

	// Resume from the sink's committed id, not the source read position: rows
	// the source read but the sink had not yet flushed on interruption were
	// never handed to ClickHouse, and resuming from the read position would
	// skip them.
	printColsReport(cfg, source.Scanned(), sink.Mutated(), sink.Batches(), sink.LastCommitted())

	if runErr != nil {
		// Both interruption and hard failure leave an incomplete backfill: exit
		// nonzero and, whenever anything was durably submitted, point the
		// operator at the cursor to resume from so shell automation never
		// treats a partial run as done.
		resumeHint := ""
		if committed := sink.LastCommitted(); committed != uuid.Nil {
			resumeHint = fmt.Sprintf("; resume with -cursor %s (repeat the same -from/-to/-org/-project)", committed)
		}
		if errors.Is(runErr, context.Canceled) {
			log.Printf("migration interrupted before completion%s", resumeHint)
			return 130
		}
		log.Printf("migration failed: %v%s", runErr, resumeHint)
		return 1
	}
	return 0
}

func (c colsConfig) criteria() pipeline.Criteria {
	crit := pipeline.Criteria{
		riskfindingscols.CriteriaBatchSize: c.batchSize,
	}
	if c.orgID != "" {
		crit[riskfindingscols.CriteriaOrgID] = c.orgID
	}
	if c.projectID.Valid {
		crit[riskfindingscols.CriteriaProjectID] = c.projectID.UUID
	}
	if c.from != nil {
		crit[riskfindingscols.CriteriaFrom] = *c.from
	}
	if c.to != nil {
		crit[riskfindingscols.CriteriaTo] = *c.to
	}
	if c.cursor.Valid {
		crit[riskfindingscols.CriteriaCursor] = c.cursor.UUID
	}
	return crit
}

func parseColsFlags(args []string) (colsConfig, error) {
	fs := flag.NewFlagSet("riskfindingscols", flag.ContinueOnError)

	// Secrets (Postgres URL, ClickHouse password) are read from the
	// environment only — never as flag values — so they do not leak through
	// argv / ps.
	var (
		chCfg      = registerClickhouseFlags(fs)
		orgID      = fs.String("org", "", "organization_id to scope the migration (optional; all orgs if empty)")
		projectID  = fs.String("project", "", "project_id (uuid) to scope (optional)")
		fromStr    = fs.String("from", "", "lower time bound, RFC3339 (optional; from the beginning if empty)")
		toStr      = fs.String("to", "", "upper time bound, RFC3339 (optional; to the end if empty)")
		cursorStr  = fs.String("cursor", "", "resume after this risk_results id (exclusive); keyset resume position only — still pass the original -from/-to/-org/-project")
		batchSize  = fs.Int("batch-size", riskfindingscols.DefaultBatchSize, "rows per source page and per mutation batch")
		bufferSize = fs.Int("buffer", riskfindingscols.DefaultBatchSize, "channel buffer between pipeline stages")
		dryRun     = fs.Bool("dry-run", true, "when true (default) read and transform but do not write; pass -dry-run=false to submit mutations")
	)
	if err := fs.Parse(args); err != nil {
		return colsConfig{}, fmt.Errorf("parse flags: %w", err)
	}
	// A leftover positional token is almost always a mistyped flag (e.g. a
	// scope value detached from its -org/-project). Silently ignoring it would
	// run the migration at its default all-org scope.
	if fs.NArg() > 0 {
		return colsConfig{}, fmt.Errorf("unexpected positional arguments: %q", fs.Args())
	}

	chCfg.password = os.Getenv("CLICKHOUSE_PASSWORD")

	cfg := colsConfig{
		dbURL:      os.Getenv("GRAM_DATABASE_URL"),
		ch:         *chCfg,
		orgID:      *orgID,
		projectID:  uuid.NullUUID{UUID: uuid.Nil, Valid: false},
		from:       nil,
		to:         nil,
		cursor:     uuid.NullUUID{UUID: uuid.Nil, Valid: false},
		batchSize:  *batchSize,
		bufferSize: *bufferSize,
		dryRun:     *dryRun,
	}

	if cfg.dbURL == "" {
		return cfg, errors.New("missing $GRAM_DATABASE_URL")
	}
	if cfg.batchSize <= 0 {
		return cfg, errors.New("-batch-size must be positive")
	}

	if *projectID != "" {
		pid, err := uuid.Parse(*projectID)
		if err != nil {
			return cfg, fmt.Errorf("invalid -project: %w", err)
		}
		cfg.projectID = uuid.NullUUID{UUID: pid, Valid: true}
	}
	if *fromStr != "" {
		from, err := time.Parse(time.RFC3339, *fromStr)
		if err != nil {
			return cfg, fmt.Errorf("invalid -from: %w", err)
		}
		cfg.from = &from
	}
	if *toStr != "" {
		to, err := time.Parse(time.RFC3339, *toStr)
		if err != nil {
			return cfg, fmt.Errorf("invalid -to: %w", err)
		}
		cfg.to = &to
	}
	if cfg.from != nil && cfg.to != nil && !cfg.from.Before(*cfg.to) {
		return cfg, errors.New("-from must be before -to")
	}
	if *cursorStr != "" {
		cur, err := uuid.Parse(*cursorStr)
		if err != nil {
			return cfg, fmt.Errorf("invalid -cursor: %w", err)
		}
		cfg.cursor = uuid.NullUUID{UUID: cur, Valid: true}
	}

	return cfg, nil
}

func printColsReport(cfg colsConfig, scanned, mutated, batches int64, lastCursor uuid.UUID) {
	mode := "DRY RUN (no writes)"
	if !cfg.dryRun {
		mode = "APPLIED"
	}
	fmt.Println()
	fmt.Println("risk_findings columns migration summary")
	fmt.Printf("  mode:          %s\n", mode)
	if cfg.orgID != "" {
		fmt.Printf("  org:           %s\n", cfg.orgID)
	} else {
		fmt.Printf("  org:           (all)\n")
	}
	fmt.Printf("  scanned:       %d\n", scanned)
	fmt.Printf("  rows targeted: %d\n", mutated)
	fmt.Printf("  mutations:     %d\n", batches)
	// The resume cursor is meaningful only for an applied run: in dry-run
	// nothing is submitted, so there is no durable checkpoint to resume from.
	if cfg.dryRun {
		fmt.Printf("  last cursor:   (dry run — no durable checkpoint)\n")
	} else {
		fmt.Printf("  last cursor:   %s\n", lastCursor)
	}
	if cfg.dryRun && scanned > 0 {
		fmt.Println()
		fmt.Println("re-run with -dry-run=false to submit ClickHouse mutations.")
	}
}
