package admin

import (
	"testing"

	"github.com/speakeasy-api/gram/server/internal/productfeatures/repo"
	"github.com/stretchr/testify/require"

	gen "github.com/speakeasy-api/gram/server/gen/admin"
	"github.com/speakeasy-api/gram/server/internal/productfeatures"
)

func TestGetOrganizationFeatures(t *testing.T) {
	t.Parallel()

	ctx, svc, conn := newTestAdminService(t)

	orgID := "org_admin_features"
	_, err := conn.Exec(ctx, `
    insert into organization_metadata (id, name, slug, gram_account_type, whitelisted)
    values ($1, 'Admin Features Org', 'admin-features-org', 'enterprise', false)
  `, orgID)
	require.NoError(t, err)

	result, err := svc.GetOrganizationFeatures(ctx, &gen.GetOrganizationFeaturesPayload{
		OrganizationID: orgID,
	})
	require.NoError(t, err)
	require.False(t, result.ConsentToolFilteringEnabled)
	require.False(t, result.HooksBrowserLoginEnabled)
	require.False(t, result.HooksFailOpenEnabled)
	require.False(t, result.PlatformMcpEnabled)
	require.False(t, result.RemoteSessionAutoRefreshEnabled)
	require.False(t, result.SessionCaptureEnabled)
	require.False(t, result.SkillCaptureMetadataOnly)
	require.True(t, result.SkillsEnabled)

	queries := repo.New(conn)
	for _, feature := range []productfeatures.Feature{
		productfeatures.FeatureConsentToolFiltering,
		productfeatures.FeatureHooksBrowserLogin,
		productfeatures.FeatureHooksFailOpen,
		productfeatures.FeaturePlatformMCP,
		productfeatures.FeatureRemoteSessionAutoRefresh,
		productfeatures.FeatureSessionCapture,
		productfeatures.FeatureSkillCaptureMetadataOnly,
	} {
		_, err = queries.EnableFeature(ctx, repo.EnableFeatureParams{
			OrganizationID: orgID,
			FeatureName:    string(feature),
		})
		require.NoError(t, err)
		svc.productFeatures.UpdateFeatureCache(ctx, orgID, feature, true)
	}

	result, err = svc.GetOrganizationFeatures(ctx, &gen.GetOrganizationFeaturesPayload{
		OrganizationID: orgID,
	})
	require.NoError(t, err)
	require.True(t, result.ConsentToolFilteringEnabled)
	require.True(t, result.HooksBrowserLoginEnabled)
	require.True(t, result.HooksFailOpenEnabled)
	require.True(t, result.PlatformMcpEnabled)
	require.True(t, result.RemoteSessionAutoRefreshEnabled)
	require.True(t, result.SessionCaptureEnabled)
	require.True(t, result.SkillCaptureMetadataOnly)
	require.True(t, result.SkillsEnabled)
}
