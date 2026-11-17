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

	"github.com/speakeasy-api/gram/server/cmd/tools/migrations/mcpunannotatedgrants"
)

const mcpUnannotatedGrantsCommand = "mcp-unannotated-grants"

type mcpUnannotatedGrantsConfig struct {
	dbURL       string
	environment string
	orgID       string
	apply       bool
}

func parseMCPUnannotatedGrantsFlags(args []string, getenv func(string) string) (mcpUnannotatedGrantsConfig, error) {
	fs := flag.NewFlagSet(mcpUnannotatedGrantsCommand, flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	apply := fs.Bool("apply", false, "write the backfill (default is dry run)")
	environment := fs.String("environment", "", "target environment name")
	orgID := fs.String("org", "", "organization_id to scope the backfill (optional)")
	confirmEnvironment := fs.String("confirm-environment", "", "must exactly match environment for writes")
	confirmTarget := fs.String("confirm-target", "", "must exactly match the parsed host, port, and database for writes")
	confirmApply := fs.String("confirm-apply", "", "must equal "+mcpUnannotatedGrantsCommand+" for writes")
	if err := fs.Parse(args); err != nil || fs.NArg() != 0 {
		return mcpUnannotatedGrantsConfig{}, errors.New("invalid " + mcpUnannotatedGrantsCommand + " flags")
	}
	if *environment == "" {
		return mcpUnannotatedGrantsConfig{}, errors.New("environment is required")
	}
	// An explicitly empty -org (for example an unset shell variable) must not
	// silently widen the run to every organization.
	orgSet := false
	fs.Visit(func(f *flag.Flag) { orgSet = orgSet || f.Name == "org" })
	if orgSet && *orgID == "" {
		return mcpUnannotatedGrantsConfig{}, errors.New("-org must not be empty; omit it to cover every organization")
	}
	if *apply && *confirmEnvironment != *environment {
		return mcpUnannotatedGrantsConfig{}, errors.New("writes require -confirm-environment to exactly match -environment")
	}
	dbURL := getenv("GRAM_DATABASE_URL")
	if dbURL == "" {
		return mcpUnannotatedGrantsConfig{}, errors.New("missing $GRAM_DATABASE_URL")
	}
	dbConfig, err := pgx.ParseConfig(dbURL)
	if err != nil {
		return mcpUnannotatedGrantsConfig{}, errors.New("invalid $GRAM_DATABASE_URL")
	}
	// pgx dials fallbacks when the primary fails, so a fallback on another host
	// would send confirmed writes to an unconfirmed target.
	for _, fallback := range dbConfig.Fallbacks {
		if fallback.Host != dbConfig.Host || fallback.Port != dbConfig.Port {
			return mcpUnannotatedGrantsConfig{}, errors.New("$GRAM_DATABASE_URL must name a single host; fallbacks to other hosts are not allowed")
		}
	}
	if *apply && !requiresTLS(dbConfig) {
		return mcpUnannotatedGrantsConfig{}, errors.New("writes require a TLS connection: set sslmode=require, verify-ca or verify-full in $GRAM_DATABASE_URL")
	}
	target := net.JoinHostPort(dbConfig.Host, strconv.Itoa(int(dbConfig.Port))) + "/" + dbConfig.Database
	if *apply && *confirmTarget != target {
		return mcpUnannotatedGrantsConfig{}, fmt.Errorf("writes require -confirm-target=%s to match the parsed database target", target)
	}
	if *apply && *confirmApply != mcpUnannotatedGrantsCommand {
		return mcpUnannotatedGrantsConfig{}, errors.New("writes require -confirm-apply=" + mcpUnannotatedGrantsCommand)
	}
	return mcpUnannotatedGrantsConfig{dbURL: dbURL, environment: *environment, orgID: *orgID, apply: *apply}, nil
}

func runMCPUnannotatedGrants(args []string, stdout io.Writer, getenv func(string) string) int {
	cfg, err := parseMCPUnannotatedGrantsFlags(args, getenv)
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
	report, err := mcpunannotatedgrants.Run(ctx, pool, mcpunannotatedgrants.Options{OrganizationID: cfg.orgID, Apply: cfg.apply})
	mode := "dry-run"
	if cfg.apply {
		mode = "apply"
	}
	if err != nil {
		// Each principal commits on its own, so the counts so far are what an
		// apply has already written.
		_, _ = fmt.Fprintf(stdout, "migration failed: %v\n", err)
		_, _ = fmt.Fprintf(stdout, "before the failure: mode=%s environment=%s principals=%d added=%d extended=%d\n", mode, cfg.environment, report.Principals, report.Added, report.Extended)
		return 1
	}
	_, _ = fmt.Fprintf(stdout, "mode=%s environment=%s principals=%d added=%d extended=%d\n", mode, cfg.environment, report.Principals, report.Added, report.Extended)
	return 0
}
