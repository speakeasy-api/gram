package gram

import (
	"flag"
	"testing"

	"github.com/speakeasy-api/gram/server/internal/testenv"
	"github.com/stretchr/testify/require"
	"github.com/urfave/cli/v2"
)

func TestInternalCatalogStartupConfiguration(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		cidr  string
		valid bool
	}{
		{"", true}, {"10.23.45.67/32", true}, {"10.0.0.0/8", false}, {"bad", false},
	} {
		t.Run(tc.cidr, func(t *testing.T) {
			t.Parallel()
			flags := flag.NewFlagSet("catalog", flag.ContinueOnError)
			require.NoError(t, internalCatalogFlag().Apply(flags))
			require.NoError(t, flags.Set("remote-mcp-catalog-ilb-cidr", tc.cidr))
			ctx := cli.NewContext(cli.NewApp(), flags, nil)
			_, err := newGuardianPolicy(ctx, testenv.NewLogger(t), testenv.NewTracerProvider(t), testenv.NewMeterProvider(t), nil)
			if tc.valid {
				require.NoError(t, err)
			} else {
				require.ErrorContains(t, err, "configure remote MCP catalog")
			}
		})
	}
}
