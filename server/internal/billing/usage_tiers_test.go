package billing_test

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/billing"
	"github.com/speakeasy-api/gram/server/internal/testenv"
)

func TestStubUsageTiersIncludesPayg(t *testing.T) {
	t.Parallel()

	client := billing.NewStubClient(testenv.NewLogger(t), testenv.NewTracerProvider(t))
	tiers, err := client.GetUsageTiers(t.Context())
	require.NoError(t, err)
	require.NotNil(t, tiers.Payg)
	require.Zero(t, tiers.Payg.BasePrice)
	require.Zero(t, tiers.Payg.IncludedToolCalls)
	require.Zero(t, tiers.Payg.IncludedServers)
	require.Zero(t, tiers.Payg.IncludedCredits)
	require.Zero(t, tiers.Payg.PricePerAdditionalToolCall)
	require.Zero(t, tiers.Payg.PricePerAdditionalServer)
	require.Empty(t, tiers.Payg.IncludedBullets)
	require.Empty(t, tiers.Payg.AddOnBullets)
	require.ElementsMatch(t, []string{
		"Oauth 2.1 proxy support",
		"Register your own OAuth server",
		"Custom domain",
		"30 day log retention",
		"SSO",
		"Audit logs",
		"Self-hosting Gram dataplane",
	}, tiers.Payg.FeatureBullets)
}
