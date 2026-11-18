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
	"strings"
	"syscall"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/speakeasy-api/gram/server/cmd/tools/migrations/remotesessionsorganization"
)

const (
	remoteSessionsOrganizationName = "remote-sessions-organization"

	// remoteSessionsOrganizationDefaultBatch is the default rows per update:
	// small enough that each statement holds its row locks only briefly
	// against concurrent sign-in and refresh.
	remoteSessionsOrganizationDefaultBatch = 500

	// remoteSessionsOrganizationMaxBatch caps -batch-size so one statement
	// cannot lock a large share of a busy table.
	remoteSessionsOrganizationMaxBatch = 10000
)

type remoteSessionsOrganizationConfig struct {
	dbURL       string
	environment string
	apply       bool
	batchSize   int32
}

func parseRemoteSessionsOrganizationFlags(args []string, getenv func(string) string) (remoteSessionsOrganizationConfig, error) {
	fs := flag.NewFlagSet(remoteSessionsOrganizationName, flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	apply := fs.Bool("apply", false, "write the backfill (default is dry run)")
	environment := fs.String("environment", "", "target environment name")
	batchSize := fs.Int("batch-size", remoteSessionsOrganizationDefaultBatch, "rows updated per statement")
	confirmEnvironment := fs.String("confirm-environment", "", "must exactly match environment for writes")
	confirmTarget := fs.String("confirm-target", "", "must exactly match the parsed host, port, and database for writes")
	confirmApply := fs.String("confirm-apply", "", "must equal "+remoteSessionsOrganizationName+" for writes")
	if err := fs.Parse(args); err != nil || fs.NArg() != 0 {
		return remoteSessionsOrganizationConfig{}, errors.New("invalid " + remoteSessionsOrganizationName + " flags")
	}
	if *environment == "" {
		return remoteSessionsOrganizationConfig{}, errors.New("environment is required")
	}
	if *batchSize < 1 || *batchSize > remoteSessionsOrganizationMaxBatch {
		return remoteSessionsOrganizationConfig{}, fmt.Errorf("batch-size must be between 1 and %d", remoteSessionsOrganizationMaxBatch)
	}
	if *apply && *confirmEnvironment != *environment {
		return remoteSessionsOrganizationConfig{}, errors.New("writes require -confirm-environment to exactly match -environment")
	}
	dbURL := getenv("GRAM_DATABASE_URL")
	if dbURL == "" {
		return remoteSessionsOrganizationConfig{}, errors.New("missing $GRAM_DATABASE_URL")
	}
	dbConfig, err := pgx.ParseConfig(dbURL)
	if err != nil {
		return remoteSessionsOrganizationConfig{}, errors.New("invalid $GRAM_DATABASE_URL")
	}
	// A fallback may only retry the same server, so an outage can never
	// redirect writes away from the confirmed target.
	for _, fallback := range dbConfig.Fallbacks {
		if *apply && (fallback.Host != dbConfig.Host || fallback.Port != dbConfig.Port) {
			return remoteSessionsOrganizationConfig{}, errors.New("writes require $GRAM_DATABASE_URL to name a single host; fallbacks to other hosts are not allowed")
		}
	}
	if *apply && !requiresTLS(dbConfig) {
		return remoteSessionsOrganizationConfig{}, errors.New("writes require a TLS connection: set sslmode=require, verify-ca or verify-full in $GRAM_DATABASE_URL")
	}
	// A preview still sends credentials, so it needs TLS too unless every
	// host it may dial is on this machine.
	if !*apply && !requiresTLS(dbConfig) && !onlyLocalHosts(dbConfig) {
		return remoteSessionsOrganizationConfig{}, errors.New("a remote database requires a TLS connection: set sslmode=require, verify-ca or verify-full in $GRAM_DATABASE_URL")
	}
	target := net.JoinHostPort(dbConfig.Host, strconv.Itoa(int(dbConfig.Port))) + "/" + dbConfig.Database
	if *apply && *confirmTarget != target {
		return remoteSessionsOrganizationConfig{}, fmt.Errorf("writes require -confirm-target=%s to match the parsed database target", target)
	}
	if *apply && *confirmApply != remoteSessionsOrganizationName {
		return remoteSessionsOrganizationConfig{}, errors.New("writes require -confirm-apply=" + remoteSessionsOrganizationName)
	}
	return remoteSessionsOrganizationConfig{dbURL: dbURL, environment: *environment, apply: *apply, batchSize: int32(*batchSize)}, nil
}

// onlyLocalHosts reports whether every host pgx would dial, fallbacks
// included, is a loopback address, localhost, or a Unix socket directory.
func onlyLocalHosts(config *pgx.ConnConfig) bool {
	isLocal := func(host string) bool {
		if strings.HasPrefix(host, "/") || strings.EqualFold(host, "localhost") {
			return true
		}
		ip := net.ParseIP(host)
		return ip != nil && ip.IsLoopback()
	}
	if !isLocal(config.Host) {
		return false
	}
	for _, fallback := range config.Fallbacks {
		if !isLocal(fallback.Host) {
			return false
		}
	}
	return true
}

func formatRemoteSessionsOrganizationReport(mode, environment string, report remotesessionsorganization.Report) string {
	return fmt.Sprintf("mode=%s environment=%s filled=%d pending=%d global_client=%d missing_organization=%d mismatched=%d\n",
		mode, environment, report.Filled, report.Counts.Pending, report.Counts.GlobalClient, report.Counts.MissingOrganization, report.Counts.Mismatched)
}

func runRemoteSessionsOrganization(args []string, stdout io.Writer, getenv func(string) string) int {
	cfg, err := parseRemoteSessionsOrganizationFlags(args, getenv)
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
	report, err := remotesessionsorganization.Run(ctx, pool, remotesessionsorganization.Options{Apply: cfg.apply, BatchSize: cfg.batchSize})
	if err != nil {
		_, _ = fmt.Fprintf(stdout, "migration failed after %d rows: %v\n", report.Filled, err)
		return 1
	}
	mode := "dry-run"
	if cfg.apply {
		mode = "apply"
	}
	_, _ = io.WriteString(stdout, formatRemoteSessionsOrganizationReport(mode, cfg.environment, report))
	return 0
}
