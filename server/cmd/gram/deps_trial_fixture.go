package gram

import (
	"context"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/speakeasy-api/gram/server/internal/localaccounts"
	"github.com/speakeasy-api/gram/server/internal/productfeatures"
)

// Both worker startup modes must opt in only for local development.
func newTrialFixtureHandler(environment string, db *pgxpool.Pool, features *productfeatures.Client) func(context.Context, string) (bool, error) {
	if environment != "local" {
		return nil
	}
	return func(ctx context.Context, orgID string) (bool, error) {
		return localaccounts.HandleTrialFixture(ctx, db, features, orgID)
	}
}
