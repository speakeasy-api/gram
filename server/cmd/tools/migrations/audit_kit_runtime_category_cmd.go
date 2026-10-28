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

	auditrepo "github.com/speakeasy-api/gram/server/internal/audit/repo"
)

const (
	auditKitRuntimeCategoryName = "audit-kit-runtime-category"

	auditKitRuntimeCategoryDefaultBatch = 1000
)

type auditKitRuntimeCategoryConfig struct {
	dbURL       string
	environment string
	apply       bool
	batchSize   int32
}

func parseAuditKitRuntimeCategoryFlags(args []string, getenv func(string) string) (auditKitRuntimeCategoryConfig, error) {
	fs := flag.NewFlagSet(auditKitRuntimeCategoryName, flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	apply := fs.Bool("apply", false, "write the backfill (default is dry run)")
	environment := fs.String("environment", "", "target environment name")
	batchSize := fs.Int("batch-size", auditKitRuntimeCategoryDefaultBatch, "rows updated per statement")
	confirmEnvironment := fs.String("confirm-environment", "", "must exactly match environment for writes")
	confirmTarget := fs.String("confirm-target", "", "must exactly match the parsed host, port, and database for writes")
	confirmApply := fs.String("confirm-apply", "", "must equal "+auditKitRuntimeCategoryName+" for writes")
	if err := fs.Parse(args); err != nil || fs.NArg() != 0 {
		return auditKitRuntimeCategoryConfig{}, errors.New("invalid " + auditKitRuntimeCategoryName + " flags")
	}
	if *environment == "" {
		return auditKitRuntimeCategoryConfig{}, errors.New("environment is required")
	}
	if *batchSize < 1 || *batchSize > 10000 {
		return auditKitRuntimeCategoryConfig{}, errors.New("batch-size must be between 1 and 10000")
	}
	if *apply && *confirmEnvironment != *environment {
		return auditKitRuntimeCategoryConfig{}, errors.New("writes require -confirm-environment to exactly match -environment")
	}
	dbURL := getenv("GRAM_DATABASE_URL")
	if dbURL == "" {
		return auditKitRuntimeCategoryConfig{}, errors.New("missing $GRAM_DATABASE_URL")
	}
	dbConfig, err := pgx.ParseConfig(dbURL)
	if err != nil {
		return auditKitRuntimeCategoryConfig{}, errors.New("invalid $GRAM_DATABASE_URL")
	}
	// A fallback may only retry the same server, so an outage can never
	// redirect writes away from the confirmed target.
	for _, fallback := range dbConfig.Fallbacks {
		if *apply && (fallback.Host != dbConfig.Host || fallback.Port != dbConfig.Port) {
			return auditKitRuntimeCategoryConfig{}, errors.New("writes require $GRAM_DATABASE_URL to name a single host; fallbacks to other hosts are not allowed")
		}
	}
	if *apply && !requiresTLS(dbConfig) {
		return auditKitRuntimeCategoryConfig{}, errors.New("writes require a TLS connection: set sslmode=require, verify-ca or verify-full in $GRAM_DATABASE_URL")
	}
	target := net.JoinHostPort(dbConfig.Host, strconv.Itoa(int(dbConfig.Port))) + "/" + dbConfig.Database
	if *apply && *confirmTarget != target {
		return auditKitRuntimeCategoryConfig{}, fmt.Errorf("writes require -confirm-target=%s to match the parsed database target", target)
	}
	if *apply && *confirmApply != auditKitRuntimeCategoryName {
		return auditKitRuntimeCategoryConfig{}, errors.New("writes require -confirm-apply=" + auditKitRuntimeCategoryName)
	}
	return auditKitRuntimeCategoryConfig{dbURL: dbURL, environment: *environment, apply: *apply, batchSize: int32(*batchSize)}, nil
}

// requiresTLS reports whether every connection attempt pgx would make uses
// TLS. sslmode=prefer and allow add a plaintext fallback, and disable has no
// TLS at all.
func requiresTLS(config *pgx.ConnConfig) bool {
	if config.TLSConfig == nil {
		return false
	}
	for _, fallback := range config.Fallbacks {
		if fallback.TLSConfig == nil {
			return false
		}
	}
	return true
}

// backfillAuditKitRuntimeCategory moves the Kit runtime's own workload
// admission entries into the security category. A dry run only counts them. Each batch
// commits on its own, so an interrupted run resumes where it stopped.
func backfillAuditKitRuntimeCategory(ctx context.Context, db auditrepo.DBTX, apply bool, batchSize int32) (int64, error) {
	q := auditrepo.New(db)
	if !apply {
		pending, err := q.CountConfigurationKitRuntimeWorkloadAdmissions(ctx)
		if err != nil {
			return 0, fmt.Errorf("count kit runtime admission entries: %w", err)
		}
		return pending, nil
	}

	var updated int64
	for {
		n, err := q.MarkKitRuntimeWorkloadAdmissionsSecurity(ctx, batchSize)
		if err != nil {
			return updated, fmt.Errorf("mark kit runtime admission entries security: %w", err)
		}
		updated += n
		if n == 0 {
			return updated, nil
		}
		if err := ctx.Err(); err != nil {
			return updated, fmt.Errorf("mark kit runtime admission entries security: %w", err)
		}
	}
}

func runAuditKitRuntimeCategory(args []string, stdout io.Writer, getenv func(string) string) int {
	cfg, err := parseAuditKitRuntimeCategoryFlags(args, getenv)
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
	rows, err := backfillAuditKitRuntimeCategory(ctx, pool, cfg.apply, cfg.batchSize)
	mode := "dry-run"
	if cfg.apply {
		mode = "apply"
	}
	if err != nil {
		_, _ = fmt.Fprintf(stdout, "migration failed after %d rows: %v\n", rows, err)
		return 1
	}
	_, _ = fmt.Fprintf(stdout, "mode=%s environment=%s rows=%d\n", mode, cfg.environment, rows)
	return 0
}
