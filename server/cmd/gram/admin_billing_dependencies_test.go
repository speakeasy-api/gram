package gram

import (
	"flag"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/urfave/cli/v2"
)

// Not parallel: it temporarily removes the flag's environment variable.
func TestAdminBillingTelemetryRequiresExplicitEnablement(t *testing.T) { //nolint:paralleltest // temporarily modifies process environment
	unsetEnv(t, "GRAM_ADMIN_BILLING_TELEMETRY_ENABLED")

	telemetryFlag := requireFlag(t, newAdminCommand().Flags, adminBillingTelemetryEnabledFlag)
	boolFlag, ok := telemetryFlag.(*cli.BoolFlag)
	require.True(t, ok, "billing telemetry flag must remain a bool flag")
	require.False(t, boolFlag.Value, "billing telemetry must remain disabled by default")
	require.Equal(t, []string{"GRAM_ADMIN_BILLING_TELEMETRY_ENABLED"}, boolFlag.EnvVars)

	set := flag.NewFlagSet("admin", flag.ContinueOnError)
	require.NoError(t, telemetryFlag.Apply(set))
	ctx := cli.NewContext(cli.NewApp(), set, nil)

	require.False(t, ctx.Bool(adminBillingTelemetryEnabledFlag))
	require.NoError(t, set.Set(adminBillingTelemetryEnabledFlag, "true"))
	require.True(t, ctx.Bool(adminBillingTelemetryEnabledFlag))
}
