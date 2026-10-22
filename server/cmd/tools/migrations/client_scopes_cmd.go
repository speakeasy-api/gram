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
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/speakeasy-api/gram/server/cmd/tools/migrations/clientscopes"
)

type clientScopesConfig struct {
	dbURL       string
	environment string
	reportPath  string
	options     clientscopes.Options
}

// Stdout carries counts only, never client, project or organization ids.
type clientScopesSummary struct {
	Mode          string                       `json:"mode"`
	Environment   string                       `json:"environment"`
	CreatedBefore string                       `json:"created_before"`
	Scanned       int                          `json:"scanned"`
	Outcomes      map[clientscopes.Outcome]int `json:"outcomes"`
	ReportPath    string                       `json:"report_path,omitempty"`
}

// parseClientScopesFlags returns flag.ErrHelp, with usage written to helpOut, when -h is passed.
func parseClientScopesFlags(args []string, getenv func(string) string, helpOut io.Writer) (clientScopesConfig, error) {
	fs := flag.NewFlagSet("client-scopes", flag.ContinueOnError)
	fs.SetOutput(helpOut)
	apply := fs.Bool("apply", false, "commit writes (default: dry run)")
	environment := fs.String("environment", "", "target environment name")
	confirmEnvironment := fs.String("confirm-environment", "", "must exactly match -environment for writes")
	createdBefore := fs.String("created-before", "", "RFC 3339 time #7199 reached the environment; only older clients are considered")
	reportPath := fs.String("report", "", "per-client JSON report path; -apply requires one and refuses an existing file")
	if err := fs.Parse(args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return clientScopesConfig{}, fmt.Errorf("parse flags: %w", err)
		}
		return clientScopesConfig{}, errors.New("invalid client-scopes flags")
	}
	if fs.NArg() != 0 {
		return clientScopesConfig{}, errors.New("unexpected positional arguments")
	}
	if *environment == "" {
		return clientScopesConfig{}, errors.New("-environment is required")
	}
	if *apply && *confirmEnvironment != *environment {
		return clientScopesConfig{}, errors.New("writes require -confirm-environment to exactly match -environment")
	}
	if *apply && *reportPath == "" {
		return clientScopesConfig{}, errors.New("writes require -report, the record a restore reads")
	}
	cutoff, err := time.Parse(time.RFC3339, *createdBefore)
	if err != nil {
		return clientScopesConfig{}, errors.New("-created-before must be an RFC 3339 time")
	}
	cfg := clientScopesConfig{
		dbURL:       getenv("GRAM_DATABASE_URL"),
		environment: *environment,
		reportPath:  *reportPath,
		options:     clientscopes.Options{CreatedBefore: cutoff, PageSize: 0, Apply: *apply},
	}
	if cfg.dbURL == "" {
		return cfg, errors.New("missing $GRAM_DATABASE_URL")
	}
	return cfg, nil
}

func runClientScopes(args []string, stdout io.Writer, getenv func(string) string) int {
	cfg, err := parseClientScopesFlags(args, getenv, stdout)
	if errors.Is(err, flag.ErrHelp) {
		return 0
	}
	if err != nil {
		log.Printf("invalid client-scopes configuration: %v", err)
		return 2
	}
	// Open the report before any apply, so a bad path cannot strand cleared rows without it.
	var reportFile *os.File
	if cfg.reportPath != "" {
		flags := os.O_WRONLY | os.O_CREATE | os.O_TRUNC
		if cfg.options.Apply {
			flags = os.O_WRONLY | os.O_CREATE | os.O_EXCL
		}
		reportFile, err = os.OpenFile(cfg.reportPath, flags, 0o600)
		if err != nil {
			log.Printf("open client-scopes report: %v", err)
			return 2
		}
		defer func() { _ = reportFile.Close() }()
	}

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()
	pool, err := pgxpool.New(ctx, cfg.dbURL)
	if err != nil {
		log.Printf("connect postgres for client-scopes failed")
		return 1
	}
	defer pool.Close()

	report, runErr := clientscopes.NewRunner(pool, cfg.options).Run(ctx)
	// The report is written even after a failure: it names every client already cleared.
	if reportFile != nil {
		encoder := json.NewEncoder(reportFile)
		encoder.SetIndent("", "  ")
		if err := encoder.Encode(report); err != nil {
			log.Printf("write client-scopes report: %v", err)
			return 1
		}
	}
	summary := clientScopesSummary{
		Mode:          report.Mode,
		Environment:   cfg.environment,
		CreatedBefore: cfg.options.CreatedBefore.UTC().Format(time.RFC3339),
		Scanned:       report.Scanned,
		Outcomes:      report.Outcomes,
		ReportPath:    cfg.reportPath,
	}
	if err := json.NewEncoder(stdout).Encode(summary); err != nil {
		log.Printf("write client-scopes summary: %v", err)
		return 1
	}
	if runErr != nil {
		log.Printf("client-scopes failed: %v", runErr)
		return 1
	}
	return 0
}
