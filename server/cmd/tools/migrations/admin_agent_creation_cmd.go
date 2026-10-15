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

	"github.com/speakeasy-api/gram/server/internal/authz"
	"github.com/speakeasy-api/gram/server/internal/o11y"
)

type adminAgentCreationConfig struct {
	dbURL          string
	environment    string
	organizationID string
	apply          bool
}

func parseAdminAgentCreationFlags(args []string, getenv func(string) string) (adminAgentCreationConfig, error) {
	fs := flag.NewFlagSet("admin-agent-creation", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	apply := fs.Bool("apply", false, "commit the one-time initialization (default rolls back)")
	environment := fs.String("environment", "", "target environment name")
	organizationID := fs.String("organization", "", "one exact organization to initialize")
	confirmEnvironment := fs.String("confirm-environment", "", "must match environment for writes")
	confirmTarget := fs.String("confirm-target", "", "must match parsed host, port, and database for writes")
	confirmOrganization := fs.String("confirm-organization", "", "must match the organization for writes")
	confirmApply := fs.String("confirm-apply", "", "must equal admin-agent-creation for writes")
	if err := fs.Parse(args); err != nil || fs.NArg() != 0 {
		return adminAgentCreationConfig{}, errors.New("invalid admin-agent-creation flags")
	}
	if *environment == "" || *organizationID == "" || *organizationID == "*" {
		return adminAgentCreationConfig{}, errors.New("environment and one exact organization are required")
	}
	dbURL := getenv("GRAM_DATABASE_URL")
	if dbURL == "" {
		return adminAgentCreationConfig{}, errors.New("missing $GRAM_DATABASE_URL")
	}
	dbConfig, err := pgx.ParseConfig(dbURL)
	if err != nil {
		return adminAgentCreationConfig{}, errors.New("invalid $GRAM_DATABASE_URL")
	}
	target := net.JoinHostPort(dbConfig.Host, strconv.Itoa(int(dbConfig.Port))) + "/" + dbConfig.Database
	if *apply && (*confirmEnvironment != *environment || *confirmTarget != target || *confirmOrganization != *organizationID || *confirmApply != "admin-agent-creation") {
		return adminAgentCreationConfig{}, errors.New("writes require exact environment, database target, organization, and admin-agent-creation confirmations")
	}
	return adminAgentCreationConfig{dbURL: dbURL, environment: *environment, organizationID: *organizationID, apply: *apply}, nil
}

func backfillAdminAgentCreation(ctx context.Context, db *pgxpool.Pool, organizationID string, apply bool) (bool, error) {
	tx, err := db.Begin(ctx)
	if err != nil {
		return false, fmt.Errorf("begin Admin creation initialization: %w", err)
	}
	defer o11y.NoLogDefer(func() error { return tx.Rollback(ctx) })
	added, err := authz.InitializeAdminAgentCreationTx(ctx, tx, organizationID)
	if err != nil {
		return false, fmt.Errorf("initialize Admin creation eligibility: %w", err)
	}
	if !apply {
		if err := tx.Rollback(ctx); err != nil {
			return false, fmt.Errorf("roll back Admin creation dry run: %w", err)
		}
		return added, nil
	}
	if err := tx.Commit(ctx); err != nil {
		return false, fmt.Errorf("commit Admin creation initialization: %w", err)
	}
	return added, nil
}

func runAdminAgentCreation(args []string, stdout io.Writer, getenv func(string) string) int {
	cfg, err := parseAdminAgentCreationFlags(args, getenv)
	if err != nil {
		_, _ = fmt.Fprintf(stdout, "invalid arguments: %v\n", err)
		return 2
	}
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()
	pool, err := pgxpool.New(ctx, cfg.dbURL)
	if err != nil {
		_, _ = fmt.Fprintln(stdout, "initialization failed: connect postgres")
		return 1
	}
	defer pool.Close()
	added, err := backfillAdminAgentCreation(ctx, pool, cfg.organizationID, cfg.apply)
	if err != nil {
		_, _ = fmt.Fprintf(stdout, "initialization failed: %v\n", err)
		return 1
	}
	mode := "dry-run"
	if cfg.apply {
		mode = "apply"
	}
	_, _ = fmt.Fprintf(stdout, "mode=%s environment=%s organizations=1 grant_added=%t\n", mode, cfg.environment, added)
	return 0
}
