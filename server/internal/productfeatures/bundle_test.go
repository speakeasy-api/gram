package productfeatures_test

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/productfeatures"
	featurerepo "github.com/speakeasy-api/gram/server/internal/productfeatures/repo"
	"github.com/speakeasy-api/gram/server/internal/testenv"
)

// enterpriseBundleFeatures is the full set SeedEnterpriseTrialBundleTx enables:
// the seven plain capability inserts plus skills (via EnableSkillsTx).
var enterpriseBundleFeatures = []productfeatures.Feature{
	productfeatures.FeatureLogs, productfeatures.FeatureToolIOLogs, productfeatures.FeatureSessionCapture,
	productfeatures.FeatureAuthzChallengeLogging, productfeatures.FeatureWebhooks, productfeatures.FeatureHooksBrowserLogin,
	productfeatures.FeatureCustomModelKeys, productfeatures.FeatureSkills,
}

func TestSeedEnterpriseTrialBundleTx_RollsBackWithCallerTransaction(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestProductFeaturesService(t)
	organizationID := activeOrganizationID(t, ctx)
	seedOrganization(t, ctx, ti.conn, organizationID)

	tx := testenv.BeginTx(t, ctx, ti.conn)
	// RBAC is the seeder's precondition (skills grants need it); Register seeds it
	// first in the same tx, mirrored here.
	require.NoError(t, productfeatures.EnableRBACTx(ctx, tx, organizationID))
	require.NoError(t, productfeatures.SeedEnterpriseTrialBundleTx(ctx, tx, organizationID))

	txRepo := featurerepo.New(tx)
	for _, f := range enterpriseBundleFeatures {
		enabled, err := txRepo.IsFeatureEnabled(ctx, featurerepo.IsFeatureEnabledParams{OrganizationID: organizationID, FeatureName: string(f)})
		require.NoError(t, err)
		require.Truef(t, enabled, "feature %s should be enabled inside the caller tx", f)
	}
	require.NoError(t, tx.Rollback(ctx))

	poolRepo := featurerepo.New(ti.conn)
	for _, f := range enterpriseBundleFeatures {
		enabled, err := poolRepo.IsFeatureEnabled(ctx, featurerepo.IsFeatureEnabledParams{OrganizationID: organizationID, FeatureName: string(f)})
		require.NoError(t, err)
		require.Falsef(t, enabled, "feature %s must roll back with the caller tx", f)
	}
	// sso/scim are deliberately withheld and never seeded.
	for _, f := range []productfeatures.Feature{productfeatures.FeatureSSO, productfeatures.FeatureSCIM} {
		enabled, err := poolRepo.IsFeatureEnabled(ctx, featurerepo.IsFeatureEnabledParams{OrganizationID: organizationID, FeatureName: string(f)})
		require.NoError(t, err)
		require.Falsef(t, enabled, "feature %s must never be seeded by the bundle", f)
	}
}

func TestSeedEnterpriseTrialBundleTx_Idempotent(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestProductFeaturesService(t)
	organizationID := activeOrganizationID(t, ctx)
	seedOrganization(t, ctx, ti.conn, organizationID)

	rbacTx := testenv.BeginTx(t, ctx, ti.conn)
	require.NoError(t, productfeatures.EnableRBACTx(ctx, rbacTx, organizationID))
	require.NoError(t, rbacTx.Commit(ctx))

	for range 2 {
		tx := testenv.BeginTx(t, ctx, ti.conn)
		require.NoError(t, productfeatures.SeedEnterpriseTrialBundleTx(ctx, tx, organizationID))
		require.NoError(t, tx.Commit(ctx))
	}

	poolRepo := featurerepo.New(ti.conn)
	for _, f := range enterpriseBundleFeatures {
		enabled, err := poolRepo.IsFeatureEnabled(ctx, featurerepo.IsFeatureEnabledParams{OrganizationID: organizationID, FeatureName: string(f)})
		require.NoError(t, err)
		require.Truef(t, enabled, "feature %s should remain enabled after re-running the seeder", f)
	}
}
