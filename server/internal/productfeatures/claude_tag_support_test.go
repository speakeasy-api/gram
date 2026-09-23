package productfeatures_test

import (
	"testing"

	gen "github.com/speakeasy-api/gram/server/gen/features"
	"github.com/speakeasy-api/gram/server/internal/productfeatures"
	"github.com/stretchr/testify/require"
)

func TestClaudeTagSupportDefaultsDisabled(t *testing.T) {
	t.Parallel()
	require.NotContains(t, productfeatures.OrganizationDefaultFeatures, productfeatures.FeatureClaudeTagSupport)
	require.NotContains(t, productfeatures.EnterpriseAccessBundle, productfeatures.FeatureClaudeTagSupport)
	require.NotContains(t, productfeatures.TrialRuntimeFeatures, productfeatures.FeatureClaudeTagSupport)
	ctx, ti := newTestProductFeaturesService(t)
	result, err := ti.service.GetProductFeatures(ctx, &gen.GetProductFeaturesPayload{OrganizationID: requestedOrganizationID(ctx)})
	require.NoError(t, err)
	require.False(t, result.ClaudeTagSupportEnabled)
	for _, enabled := range []bool{true, false, true} {
		require.NoError(t, ti.service.SetProductFeature(withPlatformAdmin(t, ctx), &gen.SetProductFeaturePayload{
			OrganizationID: requestedOrganizationID(ctx), FeatureName: gen.ProductFeatureName(productfeatures.FeatureClaudeTagSupport), Enabled: enabled,
		}))
		result, err = ti.service.GetProductFeatures(ctx, &gen.GetProductFeaturesPayload{OrganizationID: requestedOrganizationID(ctx)})
		require.NoError(t, err)
		require.Equal(t, enabled, result.ClaudeTagSupportEnabled)
		live, err := ti.client.IsFeatureEnabledUncached(ctx, requestedOrganizationID(ctx), productfeatures.FeatureClaudeTagSupport)
		require.NoError(t, err)
		require.Equal(t, enabled, live)
	}
}
