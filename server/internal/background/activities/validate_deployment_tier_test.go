package activities

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/billing"
)

func TestDeploymentLimitForTier(t *testing.T) {
	t.Parallel()

	for _, tt := range []struct {
		tier billing.Tier
		want deploymentTierLimit
		ok   bool
	}{
		{tier: billing.TierBase, want: deploymentTierLimit{displayName: "Free", maxFunctionAssets: 5}, ok: true},
		{tier: billing.TierPro, want: deploymentTierLimit{displayName: "Pro", maxFunctionAssets: 10}, ok: true},
		{tier: billing.TierPayg, want: deploymentTierLimit{displayName: "PAYG", maxFunctionAssets: 25}, ok: true},
		{tier: billing.TierEnterprise, want: deploymentTierLimit{displayName: "Enterprise", maxFunctionAssets: 25}, ok: true},
		{tier: billing.Tier("unknown"), want: deploymentTierLimit{}, ok: false},
	} {
		t.Run(string(tt.tier), func(t *testing.T) {
			t.Parallel()

			got, ok := deploymentLimitForTier(tt.tier)
			require.Equal(t, tt.ok, ok)
			require.Equal(t, tt.want, got)
		})
	}
}
