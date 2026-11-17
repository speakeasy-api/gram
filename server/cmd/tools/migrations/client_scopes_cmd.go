package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"log"
	"net"
	"os"
	"os/signal"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/speakeasy-api/gram/server/cmd/tools/migrations/clientscopes"
	"github.com/speakeasy-api/gram/server/internal/o11y"
)

type clientScopesConfig struct {
	dbURL       string
	environment string
	reportPath  string
	restore     bool
	options     clientscopes.Options
}

// Stdout carries counts only, never client, project or organization ids.
type clientScopesSummary struct {
	Mode          string                       `json:"mode"`
	Phase         string                       `json:"phase"`
	Environment   string                       `json:"environment"`
	CreatedBefore string                       `json:"created_before,omitempty"`
	ExcludedOrgs  int                          `json:"excluded_organizations"`
	Scanned       int                          `json:"scanned"`
	Outcomes      map[clientscopes.Outcome]int `json:"outcomes"`
	ReportPath    string                       `json:"report_path,omitempty"`
}

// parseClientScopesFlags returns flag.ErrHelp, with usage written to helpOut, when -h is passed.
func parseClientScopesFlags(args []string, getenv func(string) string, now time.Time, helpOut io.Writer) (clientScopesConfig, error) {
	fs := flag.NewFlagSet("client-scopes", flag.ContinueOnError)
	fs.SetOutput(helpOut)
	apply := fs.Bool("apply", false, "commit writes (default: dry run)")
	restore := fs.Bool("restore", false, "put back every scope this backfill cleared that is still unset")
	environment := fs.String("environment", "", "target environment name")
	confirmEnvironment := fs.String("confirm-environment", "", "must exactly match -environment for writes")
	confirmTarget := fs.String("confirm-target", "", "must exactly match the parsed host, port, and database for writes")
	createdBefore := fs.String("created-before", "", "RFC 3339 time #7199 reached the environment; only older clients are considered")
	excluded := organizationList{}
	fs.Var(&excluded, "exclude-organization", "organization id never written to; repeatable")
	reportPath := fs.String("report", "", "per-client JSON report path; refuses an existing file, required with -apply")
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
		return clientScopesConfig{}, errors.New("writes require -report")
	}
	cfg := clientScopesConfig{
		dbURL:       getenv("GRAM_DATABASE_URL"),
		environment: *environment,
		reportPath:  *reportPath,
		restore:     *restore,
		options:     clientscopes.Options{CreatedBefore: time.Time{}, PageSize: 0, Apply: *apply, ExcludedOrganizations: map[string]bool(excluded)},
	}
	if *restore {
		if *createdBefore != "" {
			return clientScopesConfig{}, errors.New("-restore takes no -created-before")
		}
	} else {
		cutoff, err := time.Parse(time.RFC3339, *createdBefore)
		if err != nil {
			return clientScopesConfig{}, errors.New("-created-before must be an RFC 3339 time")
		}
		if cutoff.After(now) {
			return clientScopesConfig{}, errors.New("-created-before must not be in the future")
		}
		cfg.options.CreatedBefore = cutoff
	}
	if cfg.dbURL == "" {
		return cfg, errors.New("missing $GRAM_DATABASE_URL")
	}
	dbConfig, err := pgx.ParseConfig(cfg.dbURL)
	if err != nil {
		return cfg, errors.New("invalid $GRAM_DATABASE_URL")
	}
	target := net.JoinHostPort(dbConfig.Host, strconv.Itoa(int(dbConfig.Port))) + "/" + dbConfig.Database
	if *apply && *confirmTarget != target {
		return cfg, fmt.Errorf("writes require -confirm-target=%s to match the parsed database target", target)
	}
	return cfg, nil
}

type organizationList map[string]bool

func (l organizationList) String() string { return fmt.Sprint(len(l)) }

func (l organizationList) Set(value string) error {
	if strings.TrimSpace(value) == "" {
		return errors.New("empty organization id")
	}
	l[strings.TrimSpace(value)] = true
	return nil
}

func runClientScopes(args []string, stdout io.Writer, getenv func(string) string) int {
	cfg, err := parseClientScopesFlags(args, getenv, time.Now(), stdout)
	if errors.Is(err, flag.ErrHelp) {
		return 0
	}
	if err != nil {
		log.Printf("invalid client-scopes configuration: %v", err)
		return 2
	}
	// Open the report before any write, so a bad path cannot strand changed rows without it.
	var reportFile *os.File
	if cfg.reportPath != "" {
		// #nosec G304 -- operator-supplied report path
		reportFile, err = os.OpenFile(cfg.reportPath, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
		if err != nil {
			log.Printf("open client-scopes report: %v", err)
			return 2
		}
		defer o11y.NoLogDefer(reportFile.Close)
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM, syscall.SIGHUP)
	defer stop()
	pool, err := pgxpool.New(ctx, cfg.dbURL)
	if err != nil {
		log.Printf("connect postgres for client-scopes failed")
		return 1
	}
	defer pool.Close()

	runner := clientscopes.NewRunner(pool, cfg.options)
	phase := "clear"
	var report clientscopes.Report
	var runErr error
	if cfg.restore {
		phase = "restore"
		report, runErr = runner.Restore(ctx)
	} else {
		report, runErr = runner.Run(ctx)
	}

	code := 0
	// Written even after a failure: it names every client already changed.
	if reportFile != nil {
		if err := writeClientScopesReport(reportFile, report); err != nil {
			log.Printf("write client-scopes report: %v", err)
			code = 1
		}
	}
	summary := clientScopesSummary{
		Mode:          report.Mode,
		Phase:         phase,
		Environment:   cfg.environment,
		CreatedBefore: "",
		ExcludedOrgs:  len(cfg.options.ExcludedOrganizations),
		Scanned:       report.Scanned,
		Outcomes:      report.Outcomes,
		ReportPath:    cfg.reportPath,
	}
	if !cfg.restore {
		summary.CreatedBefore = cfg.options.CreatedBefore.UTC().Format(time.RFC3339)
	}
	if err := json.NewEncoder(stdout).Encode(summary); err != nil {
		log.Printf("write client-scopes summary: %v", err)
		code = 1
	}
	if runErr != nil {
		log.Printf("client-scopes failed: %v", runErr)
		return 1
	}
	return code
}

func writeClientScopesReport(f *os.File, report clientscopes.Report) error {
	encoder := json.NewEncoder(f)
	encoder.SetIndent("", "  ")
	if err := encoder.Encode(report); err != nil {
		return fmt.Errorf("encode report: %w", err)
	}
	if err := f.Sync(); err != nil {
		return fmt.Errorf("sync report: %w", err)
	}
	return nil
}
