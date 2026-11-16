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

	"github.com/speakeasy-api/gram/server/cmd/tools/migrations/mcpgrantlimits"
)

const mcpGrantLimitsCommand = "mcp-grant-limits"

type mcpGrantLimitsConfig struct {
	dbURL       string
	environment string
	orgID       string
	apply       bool
}

func parseMCPGrantLimitsFlags(args []string, getenv func(string) string) (mcpGrantLimitsConfig, error) {
	fs := flag.NewFlagSet(mcpGrantLimitsCommand, flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	apply := fs.Bool("apply", false, "write the rewrite (default is dry run)")
	environment := fs.String("environment", "", "target environment name")
	orgID := fs.String("org", "", "organization_id to scope the rewrite (optional)")
	confirmEnvironment := fs.String("confirm-environment", "", "must exactly match environment for writes")
	confirmTarget := fs.String("confirm-target", "", "must exactly match the parsed host, port, and database for writes")
	confirmApply := fs.String("confirm-apply", "", "must equal "+mcpGrantLimitsCommand+" for writes")
	if err := fs.Parse(args); err != nil || fs.NArg() != 0 {
		return mcpGrantLimitsConfig{}, errors.New("invalid " + mcpGrantLimitsCommand + " flags")
	}
	if *environment == "" {
		return mcpGrantLimitsConfig{}, errors.New("environment is required")
	}
	// An explicitly empty -org (for example an unset shell variable) must not
	// silently widen the run to every organization.
	orgSet := false
	fs.Visit(func(f *flag.Flag) { orgSet = orgSet || f.Name == "org" })
	if orgSet && *orgID == "" {
		return mcpGrantLimitsConfig{}, errors.New("-org must not be empty; omit it to cover every organization")
	}
	if *apply && *confirmEnvironment != *environment {
		return mcpGrantLimitsConfig{}, errors.New("writes require -confirm-environment to exactly match -environment")
	}
	dbURL := getenv("GRAM_DATABASE_URL")
	if dbURL == "" {
		return mcpGrantLimitsConfig{}, errors.New("missing $GRAM_DATABASE_URL")
	}
	dbConfig, err := pgx.ParseConfig(dbURL)
	if err != nil {
		return mcpGrantLimitsConfig{}, errors.New("invalid $GRAM_DATABASE_URL")
	}
	// pgx dials fallbacks when the primary fails, so a fallback on another host
	// would send confirmed writes to an unconfirmed target.
	for _, fallback := range dbConfig.Fallbacks {
		if fallback.Host != dbConfig.Host || fallback.Port != dbConfig.Port {
			return mcpGrantLimitsConfig{}, errors.New("$GRAM_DATABASE_URL must name a single host; fallbacks to other hosts are not allowed")
		}
	}
	if *apply && !requiresTLS(dbConfig) {
		return mcpGrantLimitsConfig{}, errors.New("writes require a TLS connection: set sslmode=require, verify-ca or verify-full in $GRAM_DATABASE_URL")
	}
	target := net.JoinHostPort(dbConfig.Host, strconv.Itoa(int(dbConfig.Port))) + "/" + dbConfig.Database
	if *apply && *confirmTarget != target {
		return mcpGrantLimitsConfig{}, fmt.Errorf("writes require -confirm-target=%s to match the parsed database target", target)
	}
	if *apply && *confirmApply != mcpGrantLimitsCommand {
		return mcpGrantLimitsConfig{}, errors.New("writes require -confirm-apply=" + mcpGrantLimitsCommand)
	}
	return mcpGrantLimitsConfig{dbURL: dbURL, environment: *environment, orgID: *orgID, apply: *apply}, nil
}

func runMCPGrantLimits(args []string, stdout io.Writer, getenv func(string) string) int {
	cfg, err := parseMCPGrantLimitsFlags(args, getenv)
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
	report, err := mcpgrantlimits.Run(ctx, pool, mcpgrantlimits.Options{OrganizationID: cfg.orgID, Apply: cfg.apply})
	mode := "dry-run"
	if cfg.apply {
		mode = "apply"
	}
	if err != nil {
		// Each principal commits on its own, so the counts so far are what an
		// apply has already written.
		_, _ = fmt.Fprintf(stdout, "migration failed: %v\n", err)
		_, _ = fmt.Fprintf(stdout, "before the failure: mode=%s environment=%s principals=%d limits_removed=%d merged=%d blocks_removed=%d\n", mode, cfg.environment, report.Principals, report.LimitsRemoved, report.Merged, report.BlocksRemoved)
		return 1
	}
	_, _ = fmt.Fprintf(stdout, "mode=%s environment=%s principals=%d limits_removed=%d merged=%d blocks_removed=%d\n", mode, cfg.environment, report.Principals, report.LimitsRemoved, report.Merged, report.BlocksRemoved)
	return 0
}
