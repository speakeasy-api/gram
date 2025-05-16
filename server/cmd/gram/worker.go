package gram

import (
	"context"
	"errors"
	"fmt"
	"log/slog"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/speakeasy-api/gram/internal/control"
	"github.com/speakeasy-api/gram/internal/encryption"
	"github.com/speakeasy-api/gram/internal/o11y"
	"github.com/speakeasy-api/gram/internal/thirdparty/slack"
	slack_client "github.com/speakeasy-api/gram/internal/thirdparty/slack/client"
	"github.com/urfave/cli/v2"
	"go.temporal.io/sdk/client"
	"go.temporal.io/sdk/worker"
)

func newWorkerCommand() *cli.Command {
	var shutdownFuncs []func(context.Context) error

	return &cli.Command{
		Name:  "worker",
		Usage: "Start the temporal worker",
		Flags: []cli.Flag{
			&cli.StringFlag{
				Name:    "temporal-address",
				Usage:   "The address of the temporal server",
				EnvVars: []string{"TEMPORAL_ADDRESS"},
				Value:   "localhost:7233",
			},
			&cli.StringFlag{
				Name:    "temporal-namespace",
				Usage:   "The temporal namespace to use",
				EnvVars: []string{"TEMPORAL_NAMESPACE"},
				Value:   "default",
			},
			&cli.StringFlag{
				Name:    "temporal-client-cert",
				Usage:   "Client cert of the Temporal server",
				EnvVars: []string{"TEMPORAL_CLIENT_CERT"},
			},
			&cli.StringFlag{
				Name:    "temporal-client-key",
				Usage:   "Client key of the Temporal server",
				EnvVars: []string{"TEMPORAL_CLIENT_KEY"},
			},
			&cli.StringFlag{
				Name:    "control-address",
				Value:   ":8081",
				Usage:   "HTTP address to listen on",
				EnvVars: []string{"GRAM_WORKER_CONTROL_ADDRESS"},
			},
			&cli.StringFlag{
				Name:     "database-url",
				Usage:    "Database URL",
				EnvVars:  []string{"GRAM_DATABASE_URL"},
				Required: true,
			},
			&cli.BoolFlag{
				Name:    "unsafe-db-log",
				Usage:   "Turn on unsafe database logging. WARNING: This will log all database queries and data to the console.",
				EnvVars: []string{"GRAM_UNSAFE_DB_LOG"},
				Value:   false,
			},
			&cli.BoolFlag{
				Name:    "observe",
				Usage:   "Enable OpenTelemetry observability",
				EnvVars: []string{"GRAM_ENABLE_OTEL"},
			},
			&cli.StringFlag{
				Name:     "assets-backend",
				Usage:    "The backend to use for managing assets",
				EnvVars:  []string{"GRAM_ASSETS_BACKEND"},
				Required: true,
				Action: func(c *cli.Context, val string) error {
					if val != "fs" && val != "gcs" {
						return fmt.Errorf("invalid assets backend: %s", val)
					}
					return nil
				},
			},
			&cli.StringFlag{
				Name:     "assets-uri",
				Usage:    "The location of the assets backend to connect to",
				EnvVars:  []string{"GRAM_ASSETS_URI"},
				Required: true,
			},
			&cli.StringFlag{
				Name:     "encryption-key",
				Usage:    "Key for App level AES encryption/decyryption",
				Required: true,
				EnvVars:  []string{"GRAM_ENCRYPTION_KEY"},
			},
			&cli.StringFlag{
				Name:     "slack-client-secret",
				Usage:    "The slack client secret",
				EnvVars:  []string{"SLACK_CLIENT_SECRET"},
				Required: false,
			},
		},
		Action: func(c *cli.Context) error {
			o11y.PullAppInfo(c.Context).Command = "worker"
			logger := PullLogger(c.Context).With(slog.String("cmd", "worker"))

			ctx, cancel := context.WithCancel(c.Context)
			defer cancel()

			temporalClient, shutdown, err := newTemporalClient(logger, temporalClientOptions{
				address:      c.String("temporal-address"),
				namespace:    c.String("temporal-namespace"),
				certPEMBlock: []byte(c.String("temporal-client-cert")),
				keyPEMBlock:  []byte(c.String("temporal-client-key")),
			})
			if err != nil {
				return err
			}
			if temporalClient == nil {
				return errors.New("insufficient options to create temporal client")
			}
			shutdownFuncs = append(shutdownFuncs, shutdown)

			shutdown, err = o11y.SetupOTelSDK(ctx, logger, o11y.SetupOTelSDKOptions{
				Discard: !c.Bool("observe"),
			})
			if err != nil {
				return err
			}
			shutdownFuncs = append(shutdownFuncs, shutdown)

			db, err := newDBClient(ctx, logger, c.String("database-url"), dbClientOptions{
				enableUnsafeLogging: c.Bool("unsafe-db-log"),
			})
			if err != nil {
				return err
			}
			// Ping the database to ensure connectivity
			if err := db.Ping(ctx); err != nil {
				logger.ErrorContext(ctx, "failed to ping database", slog.String("error", err.Error()))
				return fmt.Errorf("database ping failed: %w", err)
			}
			defer db.Close()

			encryptionClient, err := encryption.New(c.String("encryption-key"))
			if err != nil {
				return err
			}

			assetStorage, shutdown, err := newAssetStorage(ctx, assetStorageOptions{
				assetsBackend: c.String("assets-backend"),
				assetsURI:     c.String("assets-uri"),
			})
			if err != nil {
				return err
			}
			shutdownFuncs = append(shutdownFuncs, shutdown)

			{
				controlServer := control.Server{
					Address:          c.String("control-address"),
					Logger:           logger.With(slog.String("component", "control")),
					DisableProfiling: false,
				}

				shutdown, err := controlServer.Start(c.Context, o11y.NewHealthCheckHandler(
					[]*o11y.NamedResource[*pgxpool.Pool]{{Name: "default", Resource: db}},
					nil,
					[]*o11y.NamedResource[client.Client]{{Name: "default", Resource: temporalClient}},
				))
				if err != nil {
					return err
				}

				shutdownFuncs = append(shutdownFuncs, shutdown)
			}

			slackClient := slack_client.NewSlackClient(slack.SlackClientID(c.String("environment")), c.String("slack-client-secret"), db, encryptionClient)

			temporalWorker := newTemporalWorker(temporalClient, logger, db, assetStorage, slackClient)

			return temporalWorker.Run(worker.InterruptCh())
		},
		After: func(c *cli.Context) error {
			return runShutdown(PullLogger(c.Context), c.Context, shutdownFuncs)
		},
	}
}
