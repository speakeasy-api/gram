package mcp

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"

	gen "github.com/speakeasy-api/gram/server/gen/usage"
	"github.com/speakeasy-api/gram/server/internal/billing"
	"github.com/speakeasy-api/gram/server/internal/testenv"
)

type unexpectedUsageLimitRepo struct {
	billing.Repository
}

func (*unexpectedUsageLimitRepo) GetStoredPeriodUsage(context.Context, string) (*gen.PeriodUsage, error) {
	panic("GetStoredPeriodUsage must not be called for non-free tiers")
}

func TestCheckToolUsageLimitsBypassesPaidTiers(t *testing.T) {
	t.Parallel()

	for _, tier := range []billing.Tier{billing.TierPro, billing.TierPayg, billing.TierEnterprise} {
		t.Run(string(tier), func(t *testing.T) {
			t.Parallel()
			require.NoError(t, checkToolUsageLimits(
				t.Context(),
				testenv.NewLogger(t),
				"org-paid",
				string(tier),
				&unexpectedUsageLimitRepo{},
			))
		})
	}
}
