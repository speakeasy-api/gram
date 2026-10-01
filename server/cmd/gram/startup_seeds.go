package gram

import (
	"context"
	"fmt"
	"log/slog"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/speakeasy-api/gram/server/internal/background/activities"
	"github.com/speakeasy-api/gram/server/internal/mcpregistry"
	"github.com/speakeasy-api/gram/server/internal/mcpregistry/oktaseed"
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
				if _, err := oktaseed.Apply(ctx, logger, mcpregistry.New(db, validator), false); err != nil {
					return fmt.Errorf("apply okta catalog seed: %w", err)
				}
				return nil
			},
		},
	}
}
