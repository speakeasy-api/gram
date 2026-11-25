package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"net"
	"os/signal"
	"strconv"
	"syscall"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/speakeasy-api/gram/server/cmd/tools/migrations/functionsaccessorganization"
)

const (
	functionsAccessOrganizationName = "functions-access-organization"

	// functionsAccessOrganizationDefaultBatch is the default rows per update:
	// small enough that each statement holds its row locks only briefly.
	functionsAccessOrganizationDefaultBatch = 500

	// functionsAccessOrganizationMaxBatch caps -batch-size so one statement
	// cannot lock a large share of a table that every function tool call
	// reads.
	functionsAccessOrganizationMaxBatch = 10000
)

type functionsAccessOrganizationConfig struct {
	dbURL       string
	environment string
	apply       bool
	batchSize   int32
}

func parseFunctionsAccessOrganizationFlags(args []string, getenv func(string) string) (functionsAccessOrganizationConfig, error) {
	fs := flag.NewFlagSet(functionsAccessOrganizationName, flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	apply := fs.Bool("apply", false, "write the backfill (default is dry run)")
	environment := fs.String("environment", "", "target environment name")
	batchSize := fs.Int("batch-size", functionsAccessOrganizationDefaultBatch, "rows looked at per statement")
	confirmEnvironment := fs.String("confirm-environment", "", "must exactly match environment for writes")
	confirmTarget := fs.String("confirm-target", "", "must exactly match the parsed host, port, and database for writes")
	confirmApply := fs.String("confirm-apply", "", "must equal "+functionsAccessOrganizationName+" for writes")
	if err := fs.Parse(args); err != nil || fs.NArg() != 0 {
		return functionsAccessOrganizationConfig{}, errors.New("invalid " + functionsAccessOrganizationName + " flags")
	}
	if *environment == "" {
		return functionsAccessOrganizationConfig{}, errors.New("environment is required")
	}
	if *batchSize < 1 || *batchSize > functionsAccessOrganizationMaxBatch {
		return functionsAccessOrganizationConfig{}, fmt.Errorf("batch-size must be between 1 and %d", functionsAccessOrganizationMaxBatch)
	}
	if *apply && *confirmEnvironment != *environment {
		return functionsAccessOrganizationConfig{}, errors.New("writes require -confirm-environment to exactly match -environment")
	}
	dbURL := getenv("GRAM_DATABASE_URL")
	if dbURL == "" {
		return functionsAccessOrganizationConfig{}, errors.New("missing $GRAM_DATABASE_URL")
	}
	dbConfig, err := pgx.ParseConfig(dbURL)
	if err != nil {
		return functionsAccessOrganizationConfig{}, errors.New("invalid $GRAM_DATABASE_URL")
	}
	// A fallback may only retry the same server, so an outage can never
	// redirect writes away from the confirmed target.
	for _, fallback := range dbConfig.Fallbacks {
		if *apply && (fallback.Host != dbConfig.Host || fallback.Port != dbConfig.Port) {
			return functionsAccessOrganizationConfig{}, errors.New("writes require $GRAM_DATABASE_URL to name a single host; fallbacks to other hosts are not allowed")
		}
	}
	if *apply && !requiresTLS(dbConfig) {
		return functionsAccessOrganizationConfig{}, errors.New("writes require a TLS connection: set sslmode=require, verify-ca or verify-full in $GRAM_DATABASE_URL")
	}
	// A preview still sends credentials, so it needs TLS too unless every
	// host it may dial is on this machine.
	if !*apply && !requiresTLS(dbConfig) && !onlyLocalHosts(dbConfig) {
		return functionsAccessOrganizationConfig{}, errors.New("a remote database requires a TLS connection: set sslmode=require, verify-ca or verify-full in $GRAM_DATABASE_URL")
	}
	target := net.JoinHostPort(dbConfig.Host, strconv.Itoa(int(dbConfig.Port))) + "/" + dbConfig.Database
	if *apply && *confirmTarget != target {
		return functionsAccessOrganizationConfig{}, fmt.Errorf("writes require -confirm-target=%s to match the parsed database target", target)
	}
	if *apply && *confirmApply != functionsAccessOrganizationName {
		return functionsAccessOrganizationConfig{}, errors.New("writes require -confirm-apply=" + functionsAccessOrganizationName)
	}
	return functionsAccessOrganizationConfig{dbURL: dbURL, environment: *environment, apply: *apply, batchSize: int32(*batchSize)}, nil
}

func formatFunctionsAccessOrganizationReport(mode, environment string, report functionsaccessorganization.Report) string {
	return fmt.Sprintf("mode=%s environment=%s filled=%d pending=%d missing_organization=%d\n",
		mode, environment, report.Filled, report.Counts.Pending, report.Counts.MissingOrganization)
}

func runFunctionsAccessOrganization(args []string, stdout io.Writer, getenv func(string) string) int {
	cfg, err := parseFunctionsAccessOrganizationFlags(args, getenv)
	if err != nil {
		_, _ = fmt.Fprintf(stdout, "invalid arguments: %v\n", err)
		return 2
	}
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()
	pool, err := pgxpool.New(ctx, cfg.dbURL)
	if err != nil {
		_, _ = fmt.Fprintln(stdout, "migration failed: connect postgres")
		return 1
	}
	defer pool.Close()
	report, err := functionsaccessorganization.Run(ctx, pool, functionsaccessorganization.Options{Apply: cfg.apply, BatchSize: cfg.batchSize})
	if err != nil {
		_, _ = fmt.Fprintf(stdout, "migration failed after %d rows: %v\n", report.Filled, err)
		return 1
	}
	mode := "dry-run"
	if cfg.apply {
		mode = "apply"
	}
	_, _ = io.WriteString(stdout, formatFunctionsAccessOrganizationReport(mode, cfg.environment, report))
	return 0
}
