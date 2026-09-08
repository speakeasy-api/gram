package main

import (
	"context"
	"flag"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"syscall"

	"github.com/speakeasy-api/gram/dev-idp/internal/bootstrap"
	"github.com/speakeasy-api/gram/dev-idp/internal/config"
	"github.com/speakeasy-api/gram/plog"
)

func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, "reconcile dev-idp schema:", err)
		os.Exit(1)
	}
}

func run() error {
	dbSpec := flag.String("db", os.Getenv("GRAM_DEVIDP_DB"), "SQLite location: 'memory' or 'file:<path>'")
	flag.Parse()

	dbCfg, err := config.ParseDB(*dbSpec)
	if err != nil {
		return fmt.Errorf("parse database config: %w", err)
	}

	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer cancel()
	logger := plog.NewLogger(os.Stderr).With(slog.String("component", "dev-idp-schema-reconcile"))

	if err := bootstrap.Reconcile(ctx, dbCfg, logger); err != nil {
		return fmt.Errorf("reconcile schema: %w", err)
	}
	return nil
}
