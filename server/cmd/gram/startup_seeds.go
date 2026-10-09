package gram

import (
	"context"
	"fmt"
	"log/slog"
	"slices"
	"strings"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/urfave/cli/v2"
	"go.opentelemetry.io/otel"

	"github.com/speakeasy-api/gram/server/internal/audit"
	"github.com/speakeasy-api/gram/server/internal/background/activities"
	"github.com/speakeasy-api/gram/server/internal/mcpregistry"
	"github.com/speakeasy-api/gram/server/internal/mcpregistry/oktaseed"
	"github.com/speakeasy-api/gram/server/internal/risk/policylifecycle"
)

// startupSeeds lists the reference data every worker keeps applied. To ship
// new data, change the seed's source in code: its Version moves and the next
// deploy applies it. To add a kind of seed, append one here.
func startupSeeds(logger *slog.Logger, db *pgxpool.Pool) []activities.StartupSeed {
	return []activities.StartupSeed{
		{
			Name:    "registry-okta",
			Version: oktaseed.Version(),
			Apply: func(ctx context.Context) error {
				validator, err := mcpregistry.LoadValidator()
				if err != nil {
					return fmt.Errorf("load registry validator: %w", err)
				}
				if _, err := oktaseed.Apply(ctx, logger, mcpregistry.New(db, validator)); err != nil {
					return fmt.Errorf("apply okta catalog seed: %w", err)
				}
				return nil
			},
		},
		{
			Name:    policylifecycle.OrphanRepairSeedName,
			Version: policylifecycle.OrphanRepairSeedVersion,
			Apply: func(ctx context.Context) error {
				if _, err := policylifecycle.NewCleaner(otel.GetTracerProvider(), audit.NewLogger()).RepairOrphans(ctx, db); err != nil {
					return fmt.Errorf("repair orphaned MCP risk policies: %w", err)
				}
				return nil
			},
		},
	}
}

// newAppSeedCommand applies the startup seeds directly, for a database
// no worker has seeded: a fresh local stack, or one restored from a backup.
func newAppSeedCommand() *cli.Command {
	return &cli.Command{
		Name:  "app-seed",
		Usage: "Apply the reference data that workers keep applied when they start (idempotent)",
		Flags: []cli.Flag{
			&cli.StringFlag{
				Name:     "database-url",
				Usage:    "Database URL",
				EnvVars:  []string{"GRAM_DATABASE_URL"},
				Required: true,
			},
			&cli.StringSliceFlag{
				Name:  "name",
				Usage: "Apply only the named seed; repeat for several. Applies every seed when omitted",
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

			seeds, err := selectStartupSeeds(startupSeeds(logger, db), c.StringSlice("name"))
			if err != nil {
				return err
			}
			for _, seed := range seeds {
				if err := seed.Apply(ctx); err != nil {
					return fmt.Errorf("apply startup seed %s: %w", seed.Name, err)
				}
			}
			return nil
		},
	}
}

// selectStartupSeeds returns every seed when names is empty, and otherwise
// the named ones in the order names gives them.
func selectStartupSeeds(seeds []activities.StartupSeed, names []string) ([]activities.StartupSeed, error) {
	if len(names) == 0 {
		return seeds, nil
	}
	known := make([]string, 0, len(seeds))
	for _, seed := range seeds {
		known = append(known, seed.Name)
	}
	selected := make([]activities.StartupSeed, 0, len(names))
	for _, name := range names {
		i := slices.Index(known, name)
		if i < 0 {
			return nil, fmt.Errorf("unknown startup seed %q; known seeds: %s", name, strings.Join(known, ", "))
		}
		selected = append(selected, seeds[i])
	}
	return selected, nil
}
