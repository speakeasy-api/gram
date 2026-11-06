package productfeatures_test

import (
	"testing"

	gen "github.com/speakeasy-api/gram/server/gen/features"
	"github.com/speakeasy-api/gram/server/internal/oops"
	"github.com/speakeasy-api/gram/server/internal/productfeatures"
	"github.com/stretchr/testify/require"
)

func TestProviderSetupRolloutIsStaffControlledAndDefaultsOff(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestProductFeaturesService(t)
	orgID := requestedOrganizationID(ctx)
	features, err := ti.service.GetProductFeatures(ctx, &gen.GetProductFeaturesPayload{OrganizationID: orgID})
	require.NoError(t, err)
	require.NotNil(t, features.ProviderSetupEnabled)
	require.False(t, *features.ProviderSetupEnabled)
	require.NotContains(t, productfeatures.OrganizationDefaultFeatures, productfeatures.FeatureProviderSetup)
	require.NotContains(t, productfeatures.EnterpriseAccessBundle, productfeatures.FeatureProviderSetup)
	require.NotContains(t, productfeatures.TrialRuntimeFeatures, productfeatures.FeatureProviderSetup)

	for _, enabled := range []bool{true, false} {
		err = ti.service.SetProductFeature(ctx, &gen.SetProductFeaturePayload{OrganizationID: orgID, FeatureName: gen.ProductFeatureName(productfeatures.FeatureProviderSetup), Enabled: enabled})
		requireOopsCode(t, err, oops.CodeForbidden)
		err = ti.service.SetProductFeature(withPlatformAdmin(t, ctx), &gen.SetProductFeaturePayload{OrganizationID: orgID, FeatureName: gen.ProductFeatureName(productfeatures.FeatureProviderSetup), Enabled: enabled})
		requireOopsCode(t, err, oops.CodeForbidden)
	}
	for _, enabled := range []bool{true, false} {
		err = ti.client.SetFeatureEnabled(ctx, orgID, productfeatures.FeatureProviderSetup, enabled)
		require.NoError(t, err)
		features, err = ti.service.GetProductFeatures(ctx, &gen.GetProductFeaturesPayload{OrganizationID: orgID})
		require.NoError(t, err)
		require.NotNil(t, features.ProviderSetupEnabled)
		require.Equal(t, enabled, *features.ProviderSetupEnabled)
		snapshot, err := ti.client.SnapshotStrict(ctx, orgID)
		require.NoError(t, err)
		require.Equal(t, enabled, snapshot.ProviderSetupEnabled)
		other, err := ti.client.IsFeatureEnabledUncached(ctx, "other-organization", productfeatures.FeatureProviderSetup)
		require.NoError(t, err)
		require.False(t, other)
	}
}
