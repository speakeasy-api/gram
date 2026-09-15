package openrouter

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/billing"
)

func TestCreditsAccountTypeMap(t *testing.T) {
	t.Parallel()

	for _, tt := range []struct {
		tier billing.Tier
		want int
	}{
		{tier: billing.TierBase, want: 5},
		{tier: billing.TierPro, want: 100},
		{tier: billing.TierPayg, want: 100},
		{tier: billing.TierEnterprise, want: 100},
	} {
		t.Run(string(tt.tier), func(t *testing.T) {
			t.Parallel()
			limit, ok := AccountTypeCreditLimit(tt.tier)
			require.True(t, ok)
			require.Equal(t, tt.want, limit)
		})
	}
}

func TestAccountTypeCreditLimitDoesNotFallBackForUnknownTier(t *testing.T) {
	t.Parallel()

	limit, ok := AccountTypeCreditLimit(billing.Tier("unknown"))
	require.False(t, ok)
	require.Zero(t, limit)
}
