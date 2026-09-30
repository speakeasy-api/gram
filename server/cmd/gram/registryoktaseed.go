package gram

import (
	"fmt"

	"github.com/urfave/cli/v2"
	"go.opentelemetry.io/otel"

	"github.com/speakeasy-api/gram/server/internal/attr"
	"github.com/speakeasy-api/gram/server/internal/mcpregistry"
	"github.com/speakeasy-api/gram/server/internal/mcpregistry/oktaseed"
)

func newRegistryOktaSeedCommand() *cli.Command {
	return &cli.Command{
		Name:  "registry-okta-seed",
		Usage: "Create or update the Gram-owned catalog entries that map Okta applications to MCP servers (idempotent)",
		Flags: []cli.Flag{
			&cli.StringFlag{
				Name:     "database-url",
				Usage:    "Database URL",
				EnvVars:  []string{"GRAM_DATABASE_URL"},
				Required: true,
			},
			&cli.BoolFlag{
				Name:  "dry-run",
				Usage: "Report what would be created or updated without writing",
			},
		},
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

			validator, err := mcpregistry.LoadValidator()
			if err != nil {
				return fmt.Errorf("load registry validator: %w", err)
			}
			registry := mcpregistry.New(db, validator)
			if err := registry.Ready(ctx); err != nil {
				return fmt.Errorf("registry not ready: %w", err)
			}
			result, err := oktaseed.Apply(ctx, logger, registry, c.Bool("dry-run"))
			if err != nil {
				return fmt.Errorf("apply okta catalog seed: %w", err)
			}
			logger.InfoContext(ctx, "okta catalog seed finished",
				attr.SlogRegistrySeedCreated(result.Created),
				attr.SlogRegistrySeedUpdated(result.Updated),
				attr.SlogRegistrySeedUnchanged(result.Unchanged),
				attr.SlogRegistrySeedDryRun(c.Bool("dry-run")))
			return nil
		},
	}
}
