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

	"github.com/speakeasy-api/gram/server/cmd/tools/migrations/memoryrolegrants"
)

const memoryRoleGrantsCommand = "memory-role-grants"

type memoryRoleGrantsConfig struct {
	dbURL       string
	environment string
	orgID       string
	apply       bool
}

func parseMemoryRoleGrantsFlags(args []string, getenv func(string) string) (memoryRoleGrantsConfig, error) {
	fs := flag.NewFlagSet(memoryRoleGrantsCommand, flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	apply := fs.Bool("apply", false, "write the backfill (default is dry run)")
	environment := fs.String("environment", "", "target environment name")
	orgID := fs.String("org", "", "organization_id to scope the backfill (optional)")
	confirmEnvironment := fs.String("confirm-environment", "", "must exactly match environment for writes")
	confirmTarget := fs.String("confirm-target", "", "must exactly match the parsed host, port, and database for writes")
	confirmApply := fs.String("confirm-apply", "", "must equal "+memoryRoleGrantsCommand+" for writes")
	if err := fs.Parse(args); err != nil || fs.NArg() != 0 {
		return memoryRoleGrantsConfig{}, errors.New("invalid " + memoryRoleGrantsCommand + " flags")
	}
	if *environment == "" {
		return memoryRoleGrantsConfig{}, errors.New("environment is required")
	}
	// An explicitly empty -org (for example an unset shell variable) must not
	// silently widen the run to every organization.
	orgSet := false
	fs.Visit(func(f *flag.Flag) { orgSet = orgSet || f.Name == "org" })
	if orgSet && *orgID == "" {
		return memoryRoleGrantsConfig{}, errors.New("-org must not be empty; omit it to cover every organization")
	}
	if *apply && *confirmEnvironment != *environment {
		return memoryRoleGrantsConfig{}, errors.New("writes require -confirm-environment to exactly match -environment")
	}
	dbURL := getenv("GRAM_DATABASE_URL")
	if dbURL == "" {
		return memoryRoleGrantsConfig{}, errors.New("missing $GRAM_DATABASE_URL")
	}
	dbConfig, err := pgx.ParseConfig(dbURL)
	if err != nil {
		return memoryRoleGrantsConfig{}, errors.New("invalid $GRAM_DATABASE_URL")
	}
	// pgx dials fallbacks when the primary fails, so a fallback on another host
	// would send confirmed writes to an unconfirmed target.
	for _, fallback := range dbConfig.Fallbacks {
		if fallback.Host != dbConfig.Host || fallback.Port != dbConfig.Port {
			return memoryRoleGrantsConfig{}, errors.New("$GRAM_DATABASE_URL must name a single host; fallbacks to other hosts are not allowed")
		}
	}
	if *apply && !requiresTLS(dbConfig) {
		return memoryRoleGrantsConfig{}, errors.New("writes require a TLS connection: set sslmode=require, verify-ca or verify-full in $GRAM_DATABASE_URL")
	}
	target := net.JoinHostPort(dbConfig.Host, strconv.Itoa(int(dbConfig.Port))) + "/" + dbConfig.Database
	if *apply && *confirmTarget != target {
		return memoryRoleGrantsConfig{}, fmt.Errorf("writes require -confirm-target=%s to match the parsed database target", target)
	}
	if *apply && *confirmApply != memoryRoleGrantsCommand {
		return memoryRoleGrantsConfig{}, errors.New("writes require -confirm-apply=" + memoryRoleGrantsCommand)
	}
	return memoryRoleGrantsConfig{dbURL: dbURL, environment: *environment, orgID: *orgID, apply: *apply}, nil
}

func runMemoryRoleGrants(args []string, stdout io.Writer, getenv func(string) string) int {
	cfg, err := parseMemoryRoleGrantsFlags(args, getenv)
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
	report, err := memoryrolegrants.Run(ctx, pool, memoryrolegrants.Options{OrganizationID: cfg.orgID, Apply: cfg.apply})
	if err != nil {
		_, _ = fmt.Fprintf(stdout, "migration failed: %v\n", err)
		return 1
	}
	mode := "dry-run"
	if cfg.apply {
		mode = "apply"
	}
	_, _ = fmt.Fprintf(stdout, "mode=%s environment=%s roles=%d grants_added=%d\n", mode, cfg.environment, report.Roles, report.GrantsAdded)
	return 0
}
