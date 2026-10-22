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

	"github.com/speakeasy-api/gram/server/cmd/tools/migrations/assistantrolegrants"
)

const assistantRoleGrantsCommand = "assistant-role-grants"

type assistantRoleGrantsConfig struct {
	dbURL       string
	environment string
	orgID       string
	apply       bool
}

func parseAssistantRoleGrantsFlags(args []string, getenv func(string) string) (assistantRoleGrantsConfig, error) {
	fs := flag.NewFlagSet(assistantRoleGrantsCommand, flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	apply := fs.Bool("apply", false, "write the backfill (default is dry run)")
	environment := fs.String("environment", "", "target environment name")
	orgID := fs.String("org", "", "organization_id to scope the backfill (optional)")
	confirmEnvironment := fs.String("confirm-environment", "", "must exactly match environment for writes")
	confirmTarget := fs.String("confirm-target", "", "must exactly match the parsed host, port, and database for writes")
	confirmApply := fs.String("confirm-apply", "", "must equal "+assistantRoleGrantsCommand+" for writes")
	if err := fs.Parse(args); err != nil || fs.NArg() != 0 {
		return assistantRoleGrantsConfig{}, errors.New("invalid " + assistantRoleGrantsCommand + " flags")
	}
	if *environment == "" {
		return assistantRoleGrantsConfig{}, errors.New("environment is required")
	}
	if *apply && *confirmEnvironment != *environment {
		return assistantRoleGrantsConfig{}, errors.New("writes require -confirm-environment to exactly match -environment")
	}
	dbURL := getenv("GRAM_DATABASE_URL")
	if dbURL == "" {
		return assistantRoleGrantsConfig{}, errors.New("missing $GRAM_DATABASE_URL")
	}
	dbConfig, err := pgx.ParseConfig(dbURL)
	if err != nil {
		return assistantRoleGrantsConfig{}, errors.New("invalid $GRAM_DATABASE_URL")
	}
	target := net.JoinHostPort(dbConfig.Host, strconv.Itoa(int(dbConfig.Port))) + "/" + dbConfig.Database
	if *apply && *confirmTarget != target {
		return assistantRoleGrantsConfig{}, fmt.Errorf("writes require -confirm-target=%s to match the parsed database target", target)
	}
	if *apply && *confirmApply != assistantRoleGrantsCommand {
		return assistantRoleGrantsConfig{}, errors.New("writes require -confirm-apply=" + assistantRoleGrantsCommand)
	}
	return assistantRoleGrantsConfig{dbURL: dbURL, environment: *environment, orgID: *orgID, apply: *apply}, nil
}

func runAssistantRoleGrants(args []string, stdout io.Writer, getenv func(string) string) int {
	cfg, err := parseAssistantRoleGrantsFlags(args, getenv)
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
	report, err := assistantrolegrants.Run(ctx, pool, assistantrolegrants.Options{OrganizationID: cfg.orgID, Apply: cfg.apply})
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
