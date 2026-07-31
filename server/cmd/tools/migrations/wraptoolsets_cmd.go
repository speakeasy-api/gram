package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"log"
	"os"
	"os/signal"
	"sort"
	"syscall"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/speakeasy-api/gram/server/cmd/tools/migrations/wraptoolsets"
)

// wrapToolsetsConfig configures the wraptoolsets migration: wrapping toolsets
// that publish directly through toolsets.mcp_slug in toolset-backed
// mcp_servers/mcp_endpoints rows. See WRAPTOOLSETS.md.
type wrapToolsetsConfig struct {
	dbURL      string
	reportPath string
	opts       wraptoolsets.Options
}

func runWrapToolsets(args []string) int {
	cfg, err := parseWrapToolsetsFlags(args)
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

	report, runErr := wraptoolsets.Run(ctx, pool, cfg.opts)

	// The partial report is still written on failure so the operator can see
	// committed outcomes and the resume cursor.
	if cfg.reportPath != "" {
		if err := writeWrapToolsetsReport(cfg.reportPath, report); err != nil {
			log.Printf("write report: %v", err)
			if runErr == nil {
				return 1
			}
		}
	}
	printWrapToolsetsReport(cfg, report)

	if runErr != nil {
		resumeHint := ""
		if report.LastCursor != nil {
			resumeHint = fmt.Sprintf("; resume with -after %s (repeat the other flags)", *report.LastCursor)
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

func parseWrapToolsetsFlags(args []string) (wrapToolsetsConfig, error) {
	fs := flag.NewFlagSet("wraptoolsets", flag.ContinueOnError)

	// The Postgres URL is read from the environment only — never as a flag
	// value — so it does not leak through argv / ps.
	var (
		dryRun          = fs.Bool("dry-run", true, "when true (default) run every read and guard but write nothing; pass -dry-run=false to apply")
		afterStr        = fs.String("after", "", "resume the keyset scan strictly after this toolset id (uuid)")
		limit           = fs.Int64("limit", 0, "maximum candidates to process this run; 0 processes all")
		projectIDStr    = fs.String("project-id", "", "restrict the candidate set to one project (uuid, optional)")
		clearDeadDomain = fs.Bool("clear-dead-domain", false, "null a candidate's custom_domain_id when its domain row is soft-deleted and wrap it as a platform candidate")
		reportPath      = fs.String("report", "", "path to write the JSON report (optional)")
	)
	if err := fs.Parse(args); err != nil {
		return wrapToolsetsConfig{}, fmt.Errorf("parse flags: %w", err)
	}
	// A leftover positional token is almost always a mistyped flag; silently
	// ignoring it would run the migration at an unintended scope.
	if fs.NArg() > 0 {
		return wrapToolsetsConfig{}, fmt.Errorf("unexpected positional arguments: %q", fs.Args())
	}

	cfg := wrapToolsetsConfig{
		dbURL:      os.Getenv("GRAM_DATABASE_URL"),
		reportPath: *reportPath,
		opts: wraptoolsets.Options{
			DryRun:          *dryRun,
			After:           uuid.NullUUID{UUID: uuid.Nil, Valid: false},
			Limit:           *limit,
			ProjectID:       uuid.NullUUID{UUID: uuid.Nil, Valid: false},
			ClearDeadDomain: *clearDeadDomain,
		},
	}

	if cfg.dbURL == "" {
		return cfg, errors.New("missing $GRAM_DATABASE_URL")
	}
	if cfg.opts.Limit < 0 {
		return cfg, errors.New("-limit must be zero or positive")
	}
	if *afterStr != "" {
		after, err := uuid.Parse(*afterStr)
		if err != nil {
			return cfg, fmt.Errorf("invalid -after: %w", err)
		}
		cfg.opts.After = uuid.NullUUID{UUID: after, Valid: true}
	}
	if *projectIDStr != "" {
		pid, err := uuid.Parse(*projectIDStr)
		if err != nil {
			return cfg, fmt.Errorf("invalid -project-id: %w", err)
		}
		cfg.opts.ProjectID = uuid.NullUUID{UUID: pid, Valid: true}
	}

	return cfg, nil
}

func writeWrapToolsetsReport(path string, report *wraptoolsets.Report) error {
	payload, err := json.MarshalIndent(report, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal report: %w", err)
	}
	if err := os.WriteFile(path, append(payload, '\n'), 0o600); err != nil {
		return fmt.Errorf("write report file: %w", err)
	}
	return nil
}

func printWrapToolsetsReport(cfg wrapToolsetsConfig, report *wraptoolsets.Report) {
	mode := "DRY RUN (no writes)"
	if !cfg.opts.DryRun {
		mode = "APPLIED"
	}
	fmt.Println()
	fmt.Println("wraptoolsets migration summary")
	fmt.Printf("  mode:      %s\n", mode)
	fmt.Printf("  processed: %d\n", len(report.Rows))

	outcomes := make([]string, 0, len(report.Counts))
	for outcome := range report.Counts {
		outcomes = append(outcomes, string(outcome))
	}
	sort.Strings(outcomes)
	for _, outcome := range outcomes {
		fmt.Printf("  %-27s%d\n", outcome+":", report.Counts[wraptoolsets.Outcome(outcome)])
	}

	blocked := 0
	for _, row := range report.Rows {
		if row.Outcome == wraptoolsets.OutcomeCreated ||
			row.Outcome == wraptoolsets.OutcomeWouldCreate ||
			row.Outcome == wraptoolsets.OutcomeAlreadyComplete {
			continue
		}
		if blocked == 0 {
			fmt.Println()
			fmt.Println("blocked rows (ids and slugs only):")
		}
		blocked++
		fmt.Printf("  toolset %s slug %q project %s: %s (%s)\n",
			row.ToolsetID, row.Slug, row.ProjectID, row.Outcome, row.Reason)
	}

	if report.LastCursor != nil {
		fmt.Printf("\n  last cursor: %s\n", *report.LastCursor)
	}
	if cfg.opts.DryRun && report.Counts[wraptoolsets.OutcomeWouldCreate] > 0 {
		fmt.Println()
		fmt.Println("re-run with -dry-run=false to apply.")
	}
}
