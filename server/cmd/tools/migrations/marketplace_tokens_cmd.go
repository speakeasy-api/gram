package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"log/slog"
	"net"
	"os/signal"
	"strconv"
	"syscall"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/speakeasy-api/gram/server/internal/encryption"
	"github.com/speakeasy-api/gram/server/internal/plugins/marketplacetokens"
	"github.com/speakeasy-api/gram/server/internal/secretbackfills/marketplacetokenbackfill"
)

const (
	marketplaceTokensName = "marketplace-tokens"

	// marketplaceTokensDefaultBatch is the default connections per page and
	// per attribution statement. There is at most one connection per project,
	// so a run takes a handful of pages.
	marketplaceTokensDefaultBatch = 500

	// marketplaceTokensMaxBatch caps -batch-size so one attribution statement
	// cannot lock a large share of the table.
	marketplaceTokensMaxBatch = 10000
)

type marketplaceTokensConfig struct {
	dbURL         string
	encryptionKey string
	environment   string
	apply         bool
	batchSize     int32
}

func parseMarketplaceTokensFlags(args []string, getenv func(string) string) (marketplaceTokensConfig, error) {
	fs := flag.NewFlagSet(marketplaceTokensName, flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	apply := fs.Bool("apply", false, "write the backfill (default is dry run)")
	environment := fs.String("environment", "", "target environment name")
	batchSize := fs.Int("batch-size", marketplaceTokensDefaultBatch, "connections per page and per attribution statement")
	confirmEnvironment := fs.String("confirm-environment", "", "must exactly match environment for writes")
	confirmTarget := fs.String("confirm-target", "", "must exactly match the parsed host, port, and database for writes")
	confirmKeyFingerprint := fs.String("confirm-key-fingerprint", "", "must exactly match the fingerprint of $GRAM_ENCRYPTION_KEY for writes")
	confirmApply := fs.String("confirm-apply", "", "must equal "+marketplaceTokensName+" for writes")
	if err := fs.Parse(args); err != nil || fs.NArg() != 0 {
		return marketplaceTokensConfig{}, errors.New("invalid " + marketplaceTokensName + " flags")
	}
	if *environment == "" {
		return marketplaceTokensConfig{}, errors.New("environment is required")
	}
	if *batchSize < 1 || *batchSize > marketplaceTokensMaxBatch {
		return marketplaceTokensConfig{}, fmt.Errorf("batch-size must be between 1 and %d", marketplaceTokensMaxBatch)
	}
	if *apply && *confirmEnvironment != *environment {
		return marketplaceTokensConfig{}, errors.New("writes require -confirm-environment to exactly match -environment")
	}
	dbURL := getenv("GRAM_DATABASE_URL")
	if dbURL == "" {
		return marketplaceTokensConfig{}, errors.New("missing $GRAM_DATABASE_URL")
	}
	dbConfig, err := pgx.ParseConfig(dbURL)
	if err != nil {
		return marketplaceTokensConfig{}, errors.New("invalid $GRAM_DATABASE_URL")
	}
	// A fallback may only retry the same server, so an outage can never
	// redirect writes away from the confirmed target.
	for _, fallback := range dbConfig.Fallbacks {
		if *apply && (fallback.Host != dbConfig.Host || fallback.Port != dbConfig.Port) {
			return marketplaceTokensConfig{}, errors.New("writes require $GRAM_DATABASE_URL to name a single host; fallbacks to other hosts are not allowed")
		}
	}
	if *apply && !requiresTLS(dbConfig) {
		return marketplaceTokensConfig{}, errors.New("writes require a TLS connection: set sslmode=require, verify-ca or verify-full in $GRAM_DATABASE_URL")
	}
	// A preview still sends credentials, so it needs TLS too unless every
	// host it may dial is on this machine.
	if !*apply && !requiresTLS(dbConfig) && !onlyLocalHosts(dbConfig) {
		return marketplaceTokensConfig{}, errors.New("a remote database requires a TLS connection: set sslmode=require, verify-ca or verify-full in $GRAM_DATABASE_URL")
	}
	target := net.JoinHostPort(dbConfig.Host, strconv.Itoa(int(dbConfig.Port))) + "/" + dbConfig.Database
	if *apply && *confirmTarget != target {
		return marketplaceTokensConfig{}, fmt.Errorf("writes require -confirm-target=%s to match the parsed database target", target)
	}

	// The copies a run seals must open under the key the servers run with,
	// because the run clears the plaintext token once they are written. A
	// copy sealed under another key opens in this process but nowhere else.
	encryptionKey := getenv("GRAM_ENCRYPTION_KEY")
	if *apply {
		if encryptionKey == "" {
			return marketplaceTokensConfig{}, errors.New("writes require $GRAM_ENCRYPTION_KEY, the platform key the servers run with")
		}
		client, err := encryption.New(encryptionKey)
		if err != nil {
			return marketplaceTokensConfig{}, errors.New("invalid $GRAM_ENCRYPTION_KEY")
		}
		if *confirmKeyFingerprint != client.Fingerprint() {
			return marketplaceTokensConfig{}, errors.New("writes require -confirm-key-fingerprint to match the encryption key fingerprint the servers log at startup")
		}
	}

	if *apply && *confirmApply != marketplaceTokensName {
		return marketplaceTokensConfig{}, errors.New("writes require -confirm-apply=" + marketplaceTokensName)
	}
	return marketplaceTokensConfig{dbURL: dbURL, encryptionKey: encryptionKey, environment: *environment, apply: *apply, batchSize: int32(*batchSize)}, nil
}

func formatMarketplaceTokensReport(mode, environment string, report marketplacetokenbackfill.Report) string {
	return fmt.Sprintf("mode=%s environment=%s attributed=%d moved=%d skipped=%d needs_organization_key=%d unattributed=%d legacy=%d hashed=%d\n",
		mode, environment, report.Attributed, report.Moved, report.Skipped, report.NeedsOrganizationKey, report.Counts.Unattributed, report.Counts.Legacy, report.Counts.Hashed)
}

func runMarketplaceTokens(args []string, stdout io.Writer, getenv func(string) string) int {
	cfg, err := parseMarketplaceTokensFlags(args, getenv)
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

	// A dry run moves nothing, so it needs no key.
	var tokens marketplacetokenbackfill.Mover
	if cfg.apply {
		platform, err := encryption.New(cfg.encryptionKey)
		if err != nil {
			_, _ = fmt.Fprintln(stdout, "migration failed: load encryption key")
			return 1
		}
		// Every organization is in platform_key for a new use case, which is
		// all this process can seal for. A token whose organization moved on
		// is counted and left in place.
		tokens = marketplacetokens.WithoutOrganizationEncryption(slog.New(slog.DiscardHandler), pool, platform)
	}

	report, err := marketplacetokenbackfill.Run(ctx, pool, tokens, marketplacetokenbackfill.Options{Apply: cfg.apply, BatchSize: cfg.batchSize})
	if err != nil {
		_, _ = fmt.Fprintf(stdout, "migration failed after attributing %d and moving %d connections: %v\n", report.Attributed, report.Moved, err)
		return 1
	}
	mode := "dry-run"
	if cfg.apply {
		mode = "apply"
	}
	_, _ = io.WriteString(stdout, formatMarketplaceTokensReport(mode, cfg.environment, report))
	return 0
}
