package productfeatures_test

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"

	gen "github.com/speakeasy-api/gram/server/gen/features"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/oops"
	"github.com/speakeasy-api/gram/server/internal/productfeatures"
	"github.com/speakeasy-api/gram/server/internal/productfeatures/repo"
)

// stubPluginPublisher records the eligibility answer to give and how many times a
// republish was requested, so the observability-mode gating can be exercised
// without a real plugins service.
type stubPluginPublisher struct {
	eligible       bool
	republishErr   error
	republishCalls int
	lastOrgID      string
}

func (s *stubPluginPublisher) HooksRolloutEligible(_ context.Context, _, _ string) bool {
	return s.eligible
}

func (s *stubPluginPublisher) RepublishOrganizationProjects(_ context.Context, orgID string) error {
	s.republishCalls++
	s.lastOrgID = orgID
	return s.republishErr
}

var _ productfeatures.PluginPublisher = (*stubPluginPublisher)(nil)

func TestProductFeaturesService_SetObservabilityMode(t *testing.T) {
	t.Parallel()

	t.Run("blocks toggle and does not persist when org is not eligible", func(t *testing.T) {
		t.Parallel()
		pub := &stubPluginPublisher{eligible: false}
		ctx, ti := newTestProductFeaturesServiceWithPublisher(t, pub)

		authCtx, ok := contextvalues.GetAuthContext(ctx)
		require.True(t, ok)

		err := ti.service.SetProductFeature(ctx, &gen.SetProductFeaturePayload{
			FeatureName: string(productfeatures.FeatureObservabilityMode),
			Enabled:     true,
		})
		require.Error(t, err)

		var oopsErr *oops.ShareableError
		require.ErrorAs(t, err, &oopsErr)
		require.Equal(t, oops.CodeConflict, oopsErr.Code)

		require.Equal(t, 0, pub.republishCalls, "a blocked toggle must not republish")

		enabled, err := repo.New(ti.conn).IsFeatureEnabled(ctx, repo.IsFeatureEnabledParams{
			OrganizationID: authCtx.ActiveOrganizationID,
			FeatureName:    string(productfeatures.FeatureObservabilityMode),
		})
		require.NoError(t, err)
		require.False(t, enabled, "a blocked toggle must not persist the feature")
	})

	t.Run("persists and republishes when org is eligible", func(t *testing.T) {
		t.Parallel()
		pub := &stubPluginPublisher{eligible: true}
		ctx, ti := newTestProductFeaturesServiceWithPublisher(t, pub)

		authCtx, ok := contextvalues.GetAuthContext(ctx)
		require.True(t, ok)

		err := ti.service.SetProductFeature(ctx, &gen.SetProductFeaturePayload{
			FeatureName: string(productfeatures.FeatureObservabilityMode),
			Enabled:     true,
		})
		require.NoError(t, err)

		require.Equal(t, 1, pub.republishCalls, "an eligible toggle must republish the org")
		require.Equal(t, authCtx.ActiveOrganizationID, pub.lastOrgID)

		enabled, err := repo.New(ti.conn).IsFeatureEnabled(ctx, repo.IsFeatureEnabledParams{
			OrganizationID: authCtx.ActiveOrganizationID,
			FeatureName:    string(productfeatures.FeatureObservabilityMode),
		})
		require.NoError(t, err)
		require.True(t, enabled)
	})

	t.Run("no-op toggle is not gated but still republishes", func(t *testing.T) {
		t.Parallel()
		pub := &stubPluginPublisher{eligible: true}
		ctx, ti := newTestProductFeaturesServiceWithPublisher(t, pub)

		// First enable is a real change: gated (passes, eligible) and republishes.
		require.NoError(t, ti.service.SetProductFeature(ctx, &gen.SetProductFeaturePayload{
			FeatureName: string(productfeatures.FeatureObservabilityMode),
			Enabled:     true,
		}))
		require.Equal(t, 1, pub.republishCalls)

		// Setting the same value again is unchanged, so even after the org becomes
		// ineligible it must NOT be gated (no error). It still requests a
		// republish: change detection is a read-then-write race under concurrent
		// opposite toggles, so every write propagates and the publish path
		// dedupes via SkipIfUnchanged.
		pub.eligible = false
		err := ti.service.SetProductFeature(ctx, &gen.SetProductFeaturePayload{
			FeatureName: string(productfeatures.FeatureObservabilityMode),
			Enabled:     true,
		})
		require.NoError(t, err, "re-setting the current value is a no-op, not a gated change")
		require.Equal(t, 2, pub.republishCalls, "every hook-output write requests a republish; SkipIfUnchanged dedupes downstream")
	})

	t.Run("republish failure does not fail the toggle", func(t *testing.T) {
		t.Parallel()
		pub := &stubPluginPublisher{eligible: true, republishErr: context.DeadlineExceeded}
		ctx, ti := newTestProductFeaturesServiceWithPublisher(t, pub)

		authCtx, ok := contextvalues.GetAuthContext(ctx)
		require.True(t, ok)

		err := ti.service.SetProductFeature(ctx, &gen.SetProductFeaturePayload{
			FeatureName: string(productfeatures.FeatureObservabilityMode),
			Enabled:     true,
		})
		require.NoError(t, err, "republish is best-effort; the automated rollout retries")
		require.Equal(t, 1, pub.republishCalls)

		enabled, err := repo.New(ti.conn).IsFeatureEnabled(ctx, repo.IsFeatureEnabledParams{
			OrganizationID: authCtx.ActiveOrganizationID,
			FeatureName:    string(productfeatures.FeatureObservabilityMode),
		})
		require.NoError(t, err)
		require.True(t, enabled, "the feature is still written even if the immediate republish fails")
	})
}
