package gram

import (
	"flag"
	"io"
	"slices"
	"testing"

	"github.com/speakeasy-api/gram/server/internal/guardian"
	"github.com/speakeasy-api/gram/server/internal/testenv"
	delivery "github.com/speakeasy-api/gram/server/internal/thirdparty/catalogpromotion"
	"github.com/stretchr/testify/require"
	"github.com/urfave/cli/v2"
)

func TestCatalogPromotionFlagsOnlyOnDeliveryProcesses(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		command *cli.Command
		deliver bool
	}{
		{newStartCommand(), true},
		{newWorkerCommand(), true},
		{newAdminCommand(), false},
		{newMCPCommand(), false},
		{newNetworkIngressServerCommand(), false},
	} {
		t.Run(tc.command.Name, func(t *testing.T) {
			t.Parallel()
			for _, name := range []string{"catalog-promotion-delivery-paused", "catalog-guides-github-repository"} {
				found := slices.ContainsFunc(tc.command.Flags, func(f cli.Flag) bool { return slices.Contains(f.Names(), name) })
				require.Equal(t, tc.deliver, found, name)
			}
			for _, f := range tc.command.Flags {
				require.NotContains(t, f.Names(), "catalog-guides-github-token")
			}
		})
	}
}

func TestCatalogPromotionTokenRejectsCommandLineValue(t *testing.T) {
	t.Parallel()
	flags := flag.NewFlagSet("catalog-promotion", flag.ContinueOnError)
	flags.SetOutput(io.Discard)
	for _, f := range catalogPromotionFlags() {
		require.NoError(t, f.Apply(flags))
	}
	require.ErrorContains(t, flags.Parse([]string{"--catalog-guides-github-token=synthetic-token"}), "flag provided but not defined")
}

func TestCatalogPromotionTokenUsesEnvironment(t *testing.T) {
	for _, token := range []string{"", "synthetic-token"} {
		t.Run(token, func(t *testing.T) {
			t.Setenv("GRAM_CATALOG_GUIDES_GITHUB_TOKEN", token)
			flags := flag.NewFlagSet("catalog-promotion", flag.ContinueOnError)
			for _, f := range catalogPromotionFlags() {
				require.NoError(t, f.Apply(flags))
			}
			ctx := cli.NewContext(cli.NewApp(), flags, nil)
			policy := guardian.NewDefaultPolicy(testenv.NewTracerProvider(t))
			client := catalogPromotionDelivery(ctx, policy)
			// Invalid metadata fails before HTTP and distinguishes configured from disabled.
			_, err := client.EnsureGuide(t.Context(), delivery.Guide{})
			if token == "" {
				require.ErrorIs(t, err, delivery.ErrNotConfigured)
			} else {
				kind, _ := delivery.Classify(err)
				require.Equal(t, delivery.ErrorPermanent, kind)
			}
		})
	}
}
