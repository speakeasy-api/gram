package productfeatures_test

import (
	"testing"

	"github.com/stretchr/testify/require"

	gen "github.com/speakeasy-api/gram/server/gen/features"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/productfeatures"
	featurerepo "github.com/speakeasy-api/gram/server/internal/productfeatures/repo"
	"github.com/speakeasy-api/gram/server/internal/testenv"
)

func TestProductFeaturesService_CustomModelKeysAlwaysEnabled(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestProductFeaturesService(t)
	organizationID := activeOrganizationID(t, ctx)
	seedOrganization(t, ctx, ti.conn, organizationID)

	result, err := ti.service.GetProductFeatures(ctx, &gen.GetProductFeaturesPayload{SessionToken: nil})
	require.NoError(t, err)
	require.True(t, result.CustomModelKeysEnabled)

	// Disabling is a no-op: the call succeeds and the feature stays on.
	err = ti.service.SetProductFeature(ctx, &gen.SetProductFeaturePayload{
		FeatureName: string(productfeatures.FeatureCustomModelKeys),
		Enabled:     false,
	})
	require.NoError(t, err)

	result, err = ti.service.GetProductFeatures(ctx, &gen.GetProductFeaturesPayload{SessionToken: nil})
	require.NoError(t, err)
	require.True(t, result.CustomModelKeysEnabled)

	// An organization_features row left over from the entitlement era must
	// survive a disable attempt rather than being soft-deleted.
	err = ti.service.SetProductFeature(ctx, &gen.SetProductFeaturePayload{
		FeatureName: string(productfeatures.FeatureCustomModelKeys),
		Enabled:     true,
	})
	require.NoError(t, err)
	err = ti.service.SetProductFeature(ctx, &gen.SetProductFeaturePayload{
		FeatureName: string(productfeatures.FeatureCustomModelKeys),
		Enabled:     false,
	})
	require.NoError(t, err)

	rowEnabled, err := featurerepo.New(ti.conn).IsFeatureEnabled(ctx, featurerepo.IsFeatureEnabledParams{
		OrganizationID: organizationID,
		FeatureName:    string(productfeatures.FeatureCustomModelKeys),
	})
	require.NoError(t, err)
	require.True(t, rowEnabled)
}

func TestProductFeaturesClient_CustomModelKeysAlwaysEnabled(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestProductFeaturesService(t)
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	require.NotNil(t, authCtx)

	redisClient, err := infra.NewRedisClient(t, 1)
	require.NoError(t, err)
	client := productfeatures.NewClient(
		testenv.NewLogger(t),
		testenv.NewTracerProvider(t),
		ti.conn,
		redisClient,
	)
	client.UpdateFeatureCache(ctx, authCtx.ActiveOrganizationID, productfeatures.FeatureCustomModelKeys, false)

	enabled, err := client.IsFeatureEnabled(ctx, authCtx.ActiveOrganizationID, productfeatures.FeatureCustomModelKeys)
	require.NoError(t, err)
	require.True(t, enabled)
}
