package gram

import (
	"flag"
	"fmt"
	"log/slog"
	"testing"

	"github.com/speakeasy-api/gram/server/internal/guardian"
	"github.com/speakeasy-api/gram/server/internal/testenv"
	"github.com/stretchr/testify/require"
	"github.com/urfave/cli/v2"
)

func TestInternalCatalogStartupConfiguration(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		environment string
		cidr        string
		valid       bool
	}{
		{"dev", "", true},
		{"dev", "10.23.45.67/32", true},
		{"dev", "10.0.0.0/8", false},
		{"dev", "bad", false},
		{"prod", "", true},
		{"prod", "10.23.45.67/32", true},
		{"prod", "10.0.0.0/8", false},
		{"prod", "bad", false},
		{"local", "", true},
		{"local", "10.23.45.67/32", true},
		{"staging", "10.23.45.67/32", true},
		{"", "", true},
		{"", "10.23.45.67/32", true},
	} {
		t.Run(fmt.Sprintf("%s/%s", tc.environment, tc.cidr), func(t *testing.T) {
			t.Parallel()
			flags := flag.NewFlagSet("catalog", flag.ContinueOnError)
			flags.String("environment", tc.environment, "")
			require.NoError(t, internalCatalogFlag().Apply(flags))
			require.NoError(t, flags.Set("remote-mcp-catalog-ilb-cidr", tc.cidr))
			ctx := cli.NewContext(cli.NewApp(), flags, nil)
			logger := slog.New(slog.DiscardHandler)
			policy, err := newGuardianPolicy(ctx, logger, testenv.NewTracerProvider(t), testenv.NewMeterProvider(t), nil)
			if !tc.valid {
				require.ErrorContains(t, err, "configure remote MCP catalog")
				return
			}
			require.NoError(t, err)
			if tc.environment == "local" {
				// Local development allows all destinations regardless of the catalog setting.
				return
			}

			err = policy.ValidateHost(t.Context(), "10.23.45.67", guardian.WithInternalCatalog())
			if tc.cidr != "" {
				require.NoError(t, err)
			} else {
				require.ErrorIs(t, err, guardian.ErrBlockedIP)
			}
			require.ErrorIs(t, policy.ValidateHost(t.Context(), "10.23.45.67"), guardian.ErrBlockedIP, "clients without the catalog opt-in stay blocked")
			require.ErrorIs(t, policy.ValidateHost(t.Context(), "10.23.45.68", guardian.WithInternalCatalog()), guardian.ErrBlockedIP, "other private addresses stay blocked")
		})
	}
}
