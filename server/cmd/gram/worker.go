package gram

import (
	"context"
	"fmt"
	"log/slog"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/speakeasy-api/gram/internal/control"
	"github.com/speakeasy-api/gram/internal/o11y"
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
			},
			&cli.StringFlag{
				Name:    "temporal-namespace",
				Usage:   "The temporal namespace to use",
				EnvVars: []string{"TEMPORAL_NAMESPACE"},
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
		},
		Action: func(c *cli.Context) error {
			ctx, cancel := context.WithCancel(c.Context)
			defer cancel()
			logger := PullLogger(ctx)

			if c.Bool("observe") {
				shutdown, err := o11y.SetupOTelSDK(ctx)
				if err != nil {
					return err
				}
				shutdownFuncs = append(shutdownFuncs, shutdown)
			}

			db, err := newDBClient(ctx, logger, c.String("database-url"), dbClientOptions{
				enableTracing:       c.Bool("observe"),
				enableUnsafeLogging: c.Bool("unsafe-db-log"),
			})
			if err != nil {
				return err
			}
			defer db.Close()

			assetStorage, shutdown, err := newAssetStorage(ctx, assetStorageOptions{
				assetsBackend: c.String("assets-backend"),
				assetsURI:     c.String("assets-uri"),
			})
			if err != nil {
				return err
			}
			shutdownFuncs = append(shutdownFuncs, shutdown)

			temporalClient, err := client.Dial(client.Options{
				HostPort:  c.String("temporal-address"),
				Namespace: c.String("temporal-namespace"),
				Logger:    logger.With(slog.String("component", "temporal")),
			})
			if err != nil {
				return fmt.Errorf("failed to create temporal client: %w", err)
			}
			shutdownFuncs = append(shutdownFuncs, func(context.Context) error {
				temporalClient.Close()
				return nil
			})

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

			temporalWorker := newTemporalWorker(temporalClient, logger, db, assetStorage)

			return temporalWorker.Run(worker.InterruptCh())
		},
		After: func(c *cli.Context) error {
			return runShutdown(PullLogger(c.Context), c.Context, shutdownFuncs)
		},
	}
}
