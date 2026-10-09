package gram

import (
	"flag"
	"fmt"
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
		{"local", "", true},
		{"", "", true},
		{"prod", "10.23.45.67/32", false},
		{"production", "10.23.45.67/32", false},
		{"local", "10.23.45.67/32", false},
		{"staging", "10.23.45.67/32", false},
		{"preview", "10.23.45.67/32", false},
		{"development", "10.23.45.67/32", false},
		{"DEV", "10.23.45.67/32", false},
		{"", "10.23.45.67/32", false},
	} {
		t.Run(fmt.Sprintf("%s/%s", tc.environment, tc.cidr), func(t *testing.T) {
			t.Parallel()
			flags := flag.NewFlagSet("catalog", flag.ContinueOnError)
			flags.String("environment", tc.environment, "")
			require.NoError(t, internalCatalogFlag().Apply(flags))
			require.NoError(t, flags.Set("remote-mcp-catalog-ilb-cidr", tc.cidr))
			ctx := cli.NewContext(cli.NewApp(), flags, nil)
			policy, err := newGuardianPolicy(ctx, testenv.NewLogger(t), testenv.NewTracerProvider(t), testenv.NewMeterProvider(t), nil)
			if tc.valid {
				require.NoError(t, err)
				if tc.environment != "local" {
					err = policy.ValidateHost(t.Context(), "10.23.45.67", guardian.WithInternalCatalog())
					if tc.cidr != "" {
						require.NoError(t, err)
					} else {
						require.ErrorIs(t, err, guardian.ErrBlockedIP)
					}
				}
			} else {
				require.ErrorContains(t, err, "configure remote MCP catalog")
			}
		})
	}
}
