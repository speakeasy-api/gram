package productfeatures_test

import (
	"slices"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/productfeatures"
	featurerepo "github.com/speakeasy-api/gram/server/internal/productfeatures/repo"
	"github.com/speakeasy-api/gram/server/internal/testenv"
)

func TestSeedEnterpriseTrialBundleTx_Idempotent(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestProductFeaturesService(t)
	organizationID := activeOrganizationID(t, ctx)
	seedOrganization(t, ctx, ti.conn, organizationID)

	for range 2 {
		tx := testenv.BeginTx(t, ctx, ti.conn)
		require.NoError(t, productfeatures.SeedEnterpriseTrialBundleTx(ctx, tx, organizationID))
		require.NoError(t, tx.Commit(ctx))
	}

	// Skills sits outside the bundle: EnableSkillsTx enables it alongside the
	// role grants it needs. slices.Concat rather than append, so this never
	// writes into spare capacity the exported bundle might grow.
	entitlements := slices.Concat(productfeatures.EnterpriseAccessBundle, []productfeatures.Feature{productfeatures.FeatureSkills})

	poolRepo := featurerepo.New(ti.conn)
	for _, feature := range entitlements {
		enabled, err := poolRepo.IsFeatureEnabled(ctx, featurerepo.IsFeatureEnabledParams{
			OrganizationID: organizationID,
			FeatureName:    string(feature),
		})
		require.NoError(t, err)
		require.Truef(t, enabled, "feature %s should remain enabled after a replayed seed", feature)
	}
}

func TestSetTrialRuntimeFeaturesTx_TogglesOnlyRuntimeFeatures(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestProductFeaturesService(t)
	organizationID := activeOrganizationID(t, ctx)
	seedOrganization(t, ctx, ti.conn, organizationID)

	tx := testenv.BeginTx(t, ctx, ti.conn)
	require.NoError(t, productfeatures.SeedOrganizationDefaultsTx(ctx, tx, organizationID))
	require.NoError(t, productfeatures.SeedEnterpriseTrialBundleTx(ctx, tx, organizationID))
	require.NoError(t, tx.Commit(ctx))

	assertEnabled := func(want bool) {
		t.Helper()
		q := featurerepo.New(ti.conn)
		for _, feature := range productfeatures.TrialRuntimeFeatures {
			enabled, err := q.IsFeatureEnabled(ctx, featurerepo.IsFeatureEnabledParams{
				OrganizationID: organizationID,
				FeatureName:    string(feature),
			})
			require.NoError(t, err)
			require.Equalf(t, want, enabled, "feature %s", feature)
		}
		ssoEnabled, err := q.IsFeatureEnabled(ctx, featurerepo.IsFeatureEnabledParams{
			OrganizationID: organizationID,
			FeatureName:    string(productfeatures.FeatureSSO),
		})
		require.NoError(t, err)
		require.True(t, ssoEnabled)
	}

	for range 2 {
		for _, enabled := range []bool{false, true} {
			tx := testenv.BeginTx(t, ctx, ti.conn)
			require.NoError(t, productfeatures.SetTrialRuntimeFeaturesTx(ctx, tx, organizationID, enabled))
			require.NoError(t, tx.Commit(ctx))
			assertEnabled(enabled)
		}
	}
}

func TestSeedEnterpriseAccessEntitlementsTxPreservesExplicitDisable(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestProductFeaturesService(t)
	organizationID := activeOrganizationID(t, ctx)
	seedOrganization(t, ctx, ti.conn, organizationID)

	q := featurerepo.New(ti.conn)
	_, err := q.EnableFeature(ctx, featurerepo.EnableFeatureParams{
		OrganizationID: organizationID,
		FeatureName:    string(productfeatures.FeatureSSO),
	})
	require.NoError(t, err)
	_, err = q.DeleteFeature(ctx, featurerepo.DeleteFeatureParams{
		OrganizationID: organizationID,
		FeatureName:    string(productfeatures.FeatureSSO),
	})
	require.NoError(t, err)

	tx := testenv.BeginTx(t, ctx, ti.conn)
	enabled, err := productfeatures.SeedEnterpriseAccessEntitlementsTx(ctx, tx, organizationID)
	require.NoError(t, err)
	require.NoError(t, tx.Commit(ctx))
	require.NotContains(t, enabled, productfeatures.FeatureSSO)

	ssoEnabled, err := q.IsFeatureEnabled(ctx, featurerepo.IsFeatureEnabledParams{
		OrganizationID: organizationID,
		FeatureName:    string(productfeatures.FeatureSSO),
	})
	require.NoError(t, err)
	require.False(t, ssoEnabled)

	for _, feature := range slices.Concat(
		[]productfeatures.Feature{productfeatures.FeaturePlatformMCP},
		productfeatures.EnterpriseAccessBundle,
		[]productfeatures.Feature{productfeatures.FeatureSkills},
	) {
		if feature == productfeatures.FeatureSSO {
			continue
		}
		featureEnabled, err := q.IsFeatureEnabled(ctx, featurerepo.IsFeatureEnabledParams{
			OrganizationID: organizationID,
			FeatureName:    string(feature),
		})
		require.NoError(t, err)
		require.Truef(t, featureEnabled, "feature %s should be enabled", feature)
	}
}
