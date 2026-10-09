package main

import (
	"context"
	"encoding/json"
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

	"github.com/speakeasy-api/gram/server/cmd/tools/migrations/hostedmcpbackfill"
	"github.com/speakeasy-api/gram/server/internal/o11y"
)

type hostedMCPWrappersConfig struct {
	dbURL      string
	reportPath string
	options    hostedmcpbackfill.Options
}

// Stdout carries counts only, so the transcript never holds tenant ids.
type hostedMCPWrappersSummary struct {
	Mode                  string                            `json:"mode"`
	Scanned               int                               `json:"scanned"`
	Writes                int                               `json:"writes"`
	Outcomes              map[hostedmcpbackfill.Outcome]int `json:"outcomes"`
	FreshIDServersPresent int                               `json:"fresh_id_servers_present"`
	LastCursor            uuid.UUID                         `json:"last_cursor"`
	ReportPath            string                            `json:"report_path,omitempty"`
}

// parseHostedMCPWrappersFlags returns flag.ErrHelp, with usage written to helpOut, when -h is passed.
func parseHostedMCPWrappersFlags(args []string, getenv func(string) string, helpOut io.Writer) (hostedMCPWrappersConfig, error) {
	fs := flag.NewFlagSet("hosted-mcp-wrappers", flag.ContinueOnError)
	fs.SetOutput(helpOut)
	apply := fs.Bool("apply", false, "commit writes (default: dry run)")
	project := fs.String("project", "", "project_id (uuid) to scope the run")
	cursor := fs.String("cursor", "", "resume after this toolset id")
	limit := fs.Int("limit", 0, "max toolsets to process; 0 means all")
	reportPath := fs.String("report", "", "per-row JSON report path")
	if err := fs.Parse(args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return hostedMCPWrappersConfig{}, fmt.Errorf("parse flags: %w", err)
		}
		return hostedMCPWrappersConfig{}, errors.New("invalid hosted-mcp-wrappers flags")
	}
	if fs.NArg() != 0 {
		return hostedMCPWrappersConfig{}, errors.New("unexpected positional arguments")
	}
	if *limit < 0 {
		return hostedMCPWrappersConfig{}, errors.New("limit must be nonnegative")
	}

	cfg := hostedMCPWrappersConfig{
		dbURL:      getenv("GRAM_DATABASE_URL"),
		reportPath: *reportPath,
		options: hostedmcpbackfill.Options{
			ProjectID: uuid.NullUUID{UUID: uuid.Nil, Valid: false},
			Cursor:    uuid.Nil,
			Limit:     *limit,
			PageSize:  0,
			Apply:     *apply,
		},
	}
	if cfg.dbURL == "" {
		return cfg, errors.New("missing $GRAM_DATABASE_URL")
	}
	if *project != "" {
		id, err := uuid.Parse(*project)
		if err != nil {
			return cfg, fmt.Errorf("invalid -project: %w", err)
		}
		cfg.options.ProjectID = uuid.NullUUID{UUID: id, Valid: true}
	}
	if *cursor != "" {
		id, err := uuid.Parse(*cursor)
		if err != nil {
			return cfg, fmt.Errorf("invalid -cursor: %w", err)
		}
		cfg.options.Cursor = id
	}
	return cfg, nil
}

func runHostedMCPWrappers(args []string, stdout io.Writer, getenv func(string) string) int {
	cfg, err := parseHostedMCPWrappersFlags(args, getenv, stdout)
	if errors.Is(err, flag.ErrHelp) {
		return 0
	}
	if err != nil {
		log.Printf("invalid hosted-mcp-wrappers configuration: %v", err)
		return 2
	}
	// Open the report before any apply, so a bad path cannot strand committed rows without one.
	// Truncate only when writing, so a failed run keeps the previous report.
	var reportFile *os.File
	if cfg.reportPath != "" {
		reportFile, err = os.OpenFile(cfg.reportPath, os.O_CREATE|os.O_WRONLY, 0o600) // #nosec G304 -- operator-supplied report path
		if err != nil {
			log.Printf("open hosted-mcp-wrappers report: %v", err)
			return 2
		}
		defer o11y.NoLogDefer(reportFile.Close)
	}
	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer cancel()
	pool, err := pgxpool.New(ctx, cfg.dbURL)
	if err != nil {
		log.Printf("connect postgres for hosted-mcp-wrappers failed")
		return 1
	}
	defer pool.Close()

	report, runErr := hostedmcpbackfill.NewRunner(pool, cfg.options).Run(ctx)
	summary := hostedMCPWrappersSummary{
		Mode: report.Mode, Scanned: report.Scanned, Writes: report.Writes, Outcomes: report.Outcomes,
		FreshIDServersPresent: report.FreshIDServersPresent, LastCursor: report.LastCursor, ReportPath: cfg.reportPath,
	}
	code := 0
	// The report is the record of committed rows, so write it before anything that can fail.
	if cfg.reportPath != "" {
		if err := writeHostedMCPWrappersReport(reportFile, report); err != nil {
			log.Printf("write hosted-mcp-wrappers report: %v", err)
			code = 1
		}
	}
	if err := json.NewEncoder(stdout).Encode(summary); err != nil {
		log.Printf("write hosted-mcp-wrappers summary: %v", err)
		code = 1
	}
	if runErr != nil {
		code = 1
		if cfg.options.Apply {
			log.Printf("hosted-mcp-wrappers stopped early: %v; resume with -cursor %s", runErr, report.LastCursor)
		} else {
			log.Printf("hosted-mcp-wrappers stopped early: %v; dry-run cursors commit nothing, so rerun without -cursor", runErr)
		}
	}
	return code
}

func writeHostedMCPWrappersReport(f *os.File, report hostedmcpbackfill.Report) error {
	if err := f.Truncate(0); err != nil {
		return fmt.Errorf("truncate report: %w", err)
	}
	if _, err := f.Seek(0, io.SeekStart); err != nil {
		return fmt.Errorf("rewind report: %w", err)
	}
	enc := json.NewEncoder(f)
	enc.SetIndent("", "  ")
	if err := enc.Encode(report); err != nil {
		return fmt.Errorf("encode report: %w", err)
	}
	if err := f.Sync(); err != nil {
		return fmt.Errorf("flush report: %w", err)
	}
	return nil
}
