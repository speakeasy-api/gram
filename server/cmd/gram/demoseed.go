package gram

import (
	"fmt"

	"github.com/urfave/cli/v2"
	"go.opentelemetry.io/otel"

	"github.com/speakeasy-api/gram/server/internal/demoseed"
	"github.com/speakeasy-api/gram/server/internal/o11y"
)

func newDemoSeedCommand() *cli.Command {
	return &cli.Command{
		Name:  "demo-seed",
		Usage: "Provision or refresh the shared read-only demo organization (idempotent; intended to run daily)",
		Flags: append([]cli.Flag{
			&cli.StringFlag{
				Name:     "database-url",
				Usage:    "Database URL",
				EnvVars:  []string{"GRAM_DATABASE_URL"},
				Required: true,
			},
			&cli.StringFlag{
				Name:     "assets-backend",
				Usage:    "The backend to use for managing assets",
				EnvVars:  []string{"GRAM_ASSETS_BACKEND"},
				Required: true,
			},
			&cli.StringFlag{
				Name:     "assets-uri",
				Usage:    "The location of the assets backend to connect to",
				EnvVars:  []string{"GRAM_ASSETS_URI"},
				Required: true,
			},
		}, clickHouseFlags()...),
		Action: func(c *cli.Context) error {
			ctx := c.Context
			logger := PullLogger(ctx)

			db, err := newDBClient(ctx, logger, otel.GetMeterProvider(), c.String("database-url"), dbClientOptions{
				enableUnsafeLogging: false,
			})
			if err != nil {
				return fmt.Errorf("connect to postgres: %w", err)
			}
			defer db.Close()

			ch, chShutdown, err := newClickhouseClient(ctx, logger, c)
			if err != nil {
				return fmt.Errorf("connect to clickhouse: %w", err)
			}
			defer o11y.NoLogDefer(func() error { return chShutdown(ctx) })

			blob, blobShutdown, err := newAssetStorage(ctx, logger, assetStorageOptions{
				assetsBackend: c.String("assets-backend"),
				assetsURI:     c.String("assets-uri"),
			})
			if err != nil {
				return fmt.Errorf("connect to asset storage: %w", err)
			}
			defer o11y.NoLogDefer(func() error { return blobShutdown(ctx) })

			return demoseed.Run(ctx, logger, db, ch, blob)
		},
	}
}
