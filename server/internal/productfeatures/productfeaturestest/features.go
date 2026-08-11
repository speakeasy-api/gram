package productfeaturestest

import (
	"context"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/productfeatures"
	"github.com/speakeasy-api/gram/server/internal/productfeatures/repo"
)

// Enable turns the feature on for the organization: it writes the
// organization_features row and refreshes the client's cache so a read in the
// same test observes it.
func Enable(
	t *testing.T,
	ctx context.Context,
	conn *pgxpool.Pool,
	client *productfeatures.Client,
	organizationID string,
	feature productfeatures.Feature,
) {
	t.Helper()

	_, err := repo.New(conn).EnableFeature(ctx, repo.EnableFeatureParams{
		OrganizationID: organizationID,
		FeatureName:    string(feature),
	})
	require.NoError(t, err)

	client.UpdateFeatureCache(ctx, organizationID, feature, true)
}

// Disable turns the feature off for the organization, removing the row as well
// as refreshing the cache.
func Disable(
	t *testing.T,
	ctx context.Context,
	conn *pgxpool.Pool,
	client *productfeatures.Client,
	organizationID string,
	feature productfeatures.Feature,
) {
	t.Helper()

	_, err := repo.New(conn).DeleteFeature(ctx, repo.DeleteFeatureParams{
		OrganizationID: organizationID,
		FeatureName:    string(feature),
	})
	require.NoError(t, err)

	client.UpdateFeatureCache(ctx, organizationID, feature, false)
}
