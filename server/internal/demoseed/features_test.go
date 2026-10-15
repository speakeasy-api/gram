//go:build demoseed_safety

package demoseed

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/productfeatures"
	featurerepo "github.com/speakeasy-api/gram/server/internal/productfeatures/repo"
)

func TestSeedPreservesFailClosedChoice(t *testing.T) {
	t.Parallel()

	for _, spec := range []Spec{DefaultSpec(), LocalSpec()} {
		t.Run(spec.OrgSlug, func(t *testing.T) {
			t.Parallel()

			ctx := t.Context()
			db, err := infra.CloneTestDatabase(t, "testdb")
			require.NoError(t, err)
			q := featurerepo.New(db)
			params := featurerepo.IsFeatureEnabledParams{
				OrganizationID: spec.OrgID,
				FeatureName:    string(productfeatures.FeatureHooksFailOpen),
			}

			seedLocalPostgres(ctx, t, db, spec)
			seedLocalPostgres(ctx, t, db, spec)
			enabled, err := q.IsFeatureEnabled(ctx, params)
			require.NoError(t, err)
			require.True(t, enabled)

			_, err = q.DeleteFeature(ctx, featurerepo.DeleteFeatureParams(params))
			require.NoError(t, err)
			seedLocalPostgres(ctx, t, db, spec)
			enabled, err = q.IsFeatureEnabled(ctx, params)
			require.NoError(t, err)
			require.False(t, enabled, "reseed must preserve an explicit fail-closed choice")
		})
	}
}
