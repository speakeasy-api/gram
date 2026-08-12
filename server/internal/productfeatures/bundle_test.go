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
	entitlements := slices.Concat(productfeatures.EnterpriseTrialBundle, []productfeatures.Feature{productfeatures.FeatureSkills})

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
