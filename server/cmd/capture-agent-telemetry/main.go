// Command capture-agent-telemetry is a local-dev tool that polls an agent
// provider's admin APIs into the local telemetry_logs bronze table and dumps
// the captured window as an anonymized NDJSON fixture. It is deliberately a
// standalone binary, not a gram CLI subcommand: it exists for generating
// local test data and never ships to production.
//
// The mise task `capture:agent-telemetry` wraps this command, running the
// poll leg in parallel with an interactive agent session (mise hooks:test)
// and dumping once the session ends.
package main

import (
	"context"
	"crypto/tls"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"time"

	"github.com/ClickHouse/clickhouse-go/v2"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/urfave/cli/v2"
	"go.opentelemetry.io/otel"

	"github.com/speakeasy-api/gram/server/internal/agentcapture"
	"github.com/speakeasy-api/gram/server/internal/aiintegrations"
	"github.com/speakeasy-api/gram/server/internal/cache"
	"github.com/speakeasy-api/gram/server/internal/encryption"
	"github.com/speakeasy-api/gram/server/internal/guardian"
	"github.com/speakeasy-api/gram/server/internal/o11y"
	"github.com/speakeasy-api/gram/server/internal/telemetry"
)

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)

	app := &cli.App{
		Name:  "capture-agent-telemetry",
		Usage: "Poll an agent provider's admin APIs into local telemetry_logs and dump the window as an anonymized NDJSON fixture (local dev only)",
		Flags: []cli.Flag{
			&cli.StringFlag{
				Name:  "agent",
				Usage: "Agent provider to capture (supported: claude)",
				Value: agentcapture.AgentClaude,
			},
			&cli.StringFlag{
				Name:    "api-key",
				Usage:   "Provider admin API key (for claude: an Anthropic admin key with Admin Analytics access); when empty the poll phase is skipped",
				EnvVars: []string{"GRAM_CAPTURE_API_KEY"},
			},
			&cli.StringFlag{
				Name:    "external-org-id",
				Usage:   "Provider-side organization ID (for claude: the Anthropic organization UUID); optional for Admin Analytics, stamped on rows as gram.external_org_id when set",
				EnvVars: []string{"GRAM_CAPTURE_EXTERNAL_ORG_ID"},
			},
			&cli.StringFlag{
				Name:  "project",
				Usage: "Local project slug that captured rows are attributed to",
				Value: "default",
			},
			&cli.StringFlag{
				Name:  "out",
				Usage: "Directory for the NDJSON dump, manifest, and anonymization salt",
				Value: "local/agent-telemetry",
			},
			&cli.DurationFlag{
				Name:  "lookback",
				Usage: "How far back to poll and dump (default 7 days)",
				Value: 7 * 24 * time.Hour,
			},
			&cli.BoolFlag{
				Name:  "anonymize",
				Usage: "Pseudonymize provider identities and scrub free-text content in the dump (pass --anonymize=false for a raw dump)",
				Value: true,
			},
			&cli.BoolFlag{
				Name:  "dump",
				Usage: "Export the captured window as NDJSON (pass --dump=false for a poll-only run, e.g. alongside a live agent session)",
				Value: true,
			},
			&cli.StringFlag{
				Name:     "database-url",
				Usage:    "Database URL",
				EnvVars:  []string{"GRAM_DATABASE_URL"},
				Required: true,
			},
			&cli.StringFlag{
				Name:     "encryption-key",
				Usage:    "Key for App level AES encryption/decryption",
				EnvVars:  []string{"GRAM_ENCRYPTION_KEY"},
				Required: true,
			},
			&cli.StringFlag{
				Name:    "clickhouse-host",
				EnvVars: []string{"CLICKHOUSE_HOST"},
				Value:   "localhost",
			},
			&cli.StringFlag{
				Name:    "clickhouse-database",
				EnvVars: []string{"CLICKHOUSE_DATABASE"},
				Value:   "default",
			},
			&cli.StringFlag{
				Name:    "clickhouse-username",
				EnvVars: []string{"CLICKHOUSE_USERNAME"},
				Value:   "gram",
			},
			&cli.StringFlag{
				Name:    "clickhouse-password",
				EnvVars: []string{"CLICKHOUSE_PASSWORD"},
				Value:   "gram",
			},
			&cli.StringFlag{
				Name:    "clickhouse-native-port",
				EnvVars: []string{"CLICKHOUSE_NATIVE_PORT"},
				Value:   "9440",
			},
			&cli.BoolFlag{
				Name:    "clickhouse-insecure",
				EnvVars: []string{"CLICKHOUSE_INSECURE"},
				Value:   false,
			},
		},
		Action: run,
	}

	err := app.RunContext(ctx, os.Args)
	stop()
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func run(c *cli.Context) error {
	ctx := c.Context
	logger := slog.New(o11y.NewLogHandler(&o11y.LogHandlerOptions{
		RawLevel:    "info",
		Pretty:      true,
		DataDogAttr: false,
	}))
	tracerProvider := otel.GetTracerProvider()
	meterProvider := otel.GetMeterProvider()

	db, err := pgxpool.New(ctx, c.String("database-url"))
	if err != nil {
		return fmt.Errorf("connect to postgres: %w", err)
	}
	defer db.Close()

	ch, err := clickhouse.Open(&clickhouse.Options{
		Protocol: clickhouse.Native,
		Addr:     []string{fmt.Sprintf("%s:%s", c.String("clickhouse-host"), c.String("clickhouse-native-port"))},
		Auth: clickhouse.Auth{
			Database: c.String("clickhouse-database"),
			Username: c.String("clickhouse-username"),
			Password: c.String("clickhouse-password"),
		},
		Settings: clickhouse.Settings{
			"max_execution_time": 60,
		},
		TLS: &tls.Config{
			// #nosec G402 -- local-dev tool; mirrors the server's flag-driven setting.
			InsecureSkipVerify: c.Bool("clickhouse-insecure"),
		},
	})
	if err != nil {
		return fmt.Errorf("open clickhouse connection: %w", err)
	}
	defer o11y.NoLogDefer(ch.Close)
	if err := ch.Ping(ctx); err != nil {
		return fmt.Errorf("ping clickhouse: %w", err)
	}

	encryptionClient, err := encryption.New(c.String("encryption-key"))
	if err != nil {
		return fmt.Errorf("create encryption client: %w", err)
	}

	guardianPolicy := guardian.NewDefaultPolicy(tracerProvider)
	store := aiintegrations.NewStore(logger, db, encryptionClient)

	// A local capture must always land rows: feature gates are hard-wired to
	// enabled instead of consulting per-org product features, and toolIOLogs
	// enabled keeps tool IO content in bronze (the dump's anonymizer scrubs
	// it on export).
	alwaysEnabled := telemetry.FeatureChecker(func(context.Context, string) (bool, error) {
		return true, nil
	})
	users := telemetry.NewUserInfoResolver(logger, db, cache.NoopCache)
	telemetryLogger := telemetry.NewLogger(
		ctx,
		logger,
		tracerProvider,
		meterProvider,
		ch,
		alwaysEnabled,
		alwaysEnabled,
		users,
		telemetry.NewNoopLogPublisher(logger),
	)

	svc := agentcapture.NewService(logger, db, ch, store, guardianPolicy, telemetryLogger)
	if err := svc.Run(ctx, agentcapture.Options{
		Agent:         c.String("agent"),
		APIKey:        c.String("api-key"),
		ExternalOrgID: c.String("external-org-id"),
		ProjectSlug:   c.String("project"),
		OutDir:        c.String("out"),
		Lookback:      c.Duration("lookback"),
		Anonymize:     c.Bool("anonymize"),
		Dump:          c.Bool("dump"),
	}); err != nil {
		return fmt.Errorf("capture agent telemetry: %w", err)
	}
	return nil
}
