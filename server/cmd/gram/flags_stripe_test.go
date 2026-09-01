package gram

import (
	"slices"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/urfave/cli/v2"
	"github.com/urfave/cli/v2/altsrc"
)

func TestStripeFlagsAreAvailableInEveryServerProcess(t *testing.T) {
	t.Parallel()

	commands := map[string]*cli.Command{
		"admin":   newAdminCommand(),
		"server":  newStartCommand(),
		"worker":  newWorkerCommand(),
		"streams": newStreamsCommand(),
	}

	for process, command := range commands {
		t.Run(process, func(t *testing.T) {
			t.Parallel()

			apiKey := requireFlag(t, command.Flags, "stripe-api-key")
			_, tomlSourceable := apiKey.(altsrc.FlagInputSourceExtension)
			require.False(t, tomlSourceable, "Stripe API key must be env-only")

			webhookSecret := requireFlag(t, command.Flags, "stripe-webhook-secret")
			_, tomlSourceable = webhookSecret.(altsrc.FlagInputSourceExtension)
			require.False(t, tomlSourceable, "Stripe webhook secret must be env-only")

			priceID := requireFlag(t, command.Flags, "stripe-price-id-tum")
			_, tomlSourceable = priceID.(altsrc.FlagInputSourceExtension)
			require.True(t, tomlSourceable, "Stripe catalog must be TOML-sourceable")
			require.Contains(t, priceID.Names(), "stripe.price_id_tum")

			meterID := requireFlag(t, command.Flags, "stripe-meter-id-tum")
			_, tomlSourceable = meterID.(altsrc.FlagInputSourceExtension)
			require.True(t, tomlSourceable, "Stripe catalog must be TOML-sourceable")
			require.Contains(t, meterID.Names(), "stripe.meter_id_tum")

			meterEventName := requireFlag(t, command.Flags, "stripe-meter-event-name")
			_, tomlSourceable = meterEventName.(altsrc.FlagInputSourceExtension)
			require.True(t, tomlSourceable, "Stripe catalog must be TOML-sourceable")
			require.Contains(t, meterEventName.Names(), "stripe.meter_event_name")

			portalConfigurationID := requireFlag(t, command.Flags, "stripe-portal-configuration-id")
			_, tomlSourceable = portalConfigurationID.(altsrc.FlagInputSourceExtension)
			require.True(t, tomlSourceable, "Stripe catalog must be TOML-sourceable")
			require.Contains(t, portalConfigurationID.Names(), "stripe.portal_configuration_id")

			streaming, ok := requireFlag(t, command.Flags, stripeTUMMeterStreamingFlagName).(*cli.BoolFlag)
			require.True(t, ok)
			require.False(t, streaming.Value)
			require.Equal(t, []string{"GRAM_STRIPE_TUM_METER_STREAMING"}, streaming.EnvVars)

			exportEnabled, ok := requireFlag(t, command.Flags, stripeMeterEventExportFlagName).(*cli.BoolFlag)
			require.True(t, ok)
			require.False(t, exportEnabled.Value)
			require.Equal(t, []string{"GRAM_STRIPE_METER_EVENT_EXPORT_ENABLED"}, exportEnabled.EnvVars)
		})
	}
}

func requireFlag(t *testing.T, flags []cli.Flag, name string) cli.Flag {
	t.Helper()

	for _, candidate := range flags {
		if slices.Contains(candidate.Names(), name) {
			return candidate
		}
	}
	require.FailNow(t, "flag not found", name)
	return nil
}
