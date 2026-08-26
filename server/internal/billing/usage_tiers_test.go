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
	require.Equal(t, billing.NewPaygTierLimits(), tiers.Payg)
}

func TestNewPaygTierLimits(t *testing.T) {
	t.Parallel()

	want := billing.NewPaygTierLimits()
	require.Zero(t, want.BasePrice)
	require.Zero(t, want.IncludedToolCalls)
	require.Zero(t, want.IncludedServers)
	require.Zero(t, want.IncludedCredits)
	require.Zero(t, want.PricePerAdditionalToolCall)
	require.Zero(t, want.PricePerAdditionalServer)
	require.NotNil(t, want.TumPricePerMillionUsd)
	require.Equal(t, billing.TUMPricePerMillionUSD, *want.TumPricePerMillionUsd)
	require.Equal(t, "0.00000035", billing.TUMUnitPriceUSD)
	require.Equal(t, []string{
		"Other inference billed at provider cost",
		"Platform-initiated inference billed at provider cost",
	}, want.IncludedBullets)
	require.Empty(t, want.AddOnBullets)
	require.Equal(t, []string{
		"Oauth 2.1 proxy support",
		"Register your own OAuth server",
		"Custom domain",
		"30 day log retention",
		"SSO",
		"Audit logs",
		"Self-hosting Gram dataplane",
	}, want.FeatureBullets)

	other := billing.NewPaygTierLimits()
	require.NotSame(t, want, other)
	want.FeatureBullets[0] = "changed"
	require.Equal(t, "Oauth 2.1 proxy support", other.FeatureBullets[0])
}
