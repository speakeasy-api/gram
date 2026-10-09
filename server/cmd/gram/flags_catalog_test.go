package gram

import (
	"bytes"
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
		{"local", "", true},
		{"", "", true},
		{"prod", "10.23.45.67/32", true},
		{"prod", "bad", true},
		{"production", "10.23.45.67/32", true},
		{"local", "10.23.45.67/32", true},
		{"staging", "10.23.45.67/32", true},
		{"preview", "10.23.45.67/32", true},
		{"development", "10.23.45.67/32", true},
		{"DEV", "10.23.45.67/32", true},
		{"", "10.23.45.67/32", true},
	} {
		t.Run(fmt.Sprintf("%s/%s", tc.environment, tc.cidr), func(t *testing.T) {
			t.Parallel()
			flags := flag.NewFlagSet("catalog", flag.ContinueOnError)
			flags.String("environment", tc.environment, "")
			require.NoError(t, internalCatalogFlag().Apply(flags))
			require.NoError(t, flags.Set("remote-mcp-catalog-ilb-cidr", tc.cidr))
			ctx := cli.NewContext(cli.NewApp(), flags, nil)
			var logs bytes.Buffer
			logger := slog.New(slog.NewTextHandler(&logs, nil))
			policy, err := newGuardianPolicy(ctx, logger, testenv.NewTracerProvider(t), testenv.NewMeterProvider(t), nil)
			if tc.environment != "dev" && tc.cidr != "" {
				require.Contains(t, logs.String(), "level=WARN")
				require.Contains(t, logs.String(), "ignoring remote MCP catalog CIDR allowance")
			} else {
				require.NotContains(t, logs.String(), "ignoring remote MCP catalog CIDR allowance")
			}
			if tc.valid {
				require.NoError(t, err)
				if tc.environment != "local" {
					err = policy.ValidateHost(t.Context(), "10.23.45.67", guardian.WithInternalCatalog())
					if tc.environment == "dev" && tc.cidr != "" {
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
