package gram

import (
	"flag"
	"maps"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/urfave/cli/v2"

	"github.com/speakeasy-api/gram/server/internal/testenv"
)

// newStreamsCLIContext builds a context over the real streams command's flags,
// so the test is bound to the flag names the deployment configures rather than
// to a copy of them.
func newStreamsCLIContext(t *testing.T, values map[string]string) *cli.Context {
	t.Helper()

	set := flag.NewFlagSet("streams", flag.ContinueOnError)
	for _, f := range newStreamsCommand().Flags {
		require.NoError(t, f.Apply(set))
	}
	for name, value := range values {
		require.NoError(t, set.Set(name, value))
	}
	return cli.NewContext(cli.NewApp(), set, nil)
}

// The temporal flags must carry no default value. This is the half of the
// contract a behavioural test cannot reach: newTranscriptWriter only ever sees
// the resolved string, so a test that sets the flag to "" passes whether or not
// a default exists. The default is what made "unset disables wakes"
// unreachable — an operator who configured nothing got a client dialling
// localhost instead of the announced no-op — so it is asserted on the flag
// definition itself.
func TestStreamsCommand_TemporalFlagsHaveNoDefault(t *testing.T) {
	t.Parallel()

	defaults := map[string]string{}
	for _, f := range newStreamsCommand().Flags {
		sf, ok := f.(*cli.StringFlag)
		if !ok {
			continue
		}
		if sf.Name == "temporal-address" || sf.Name == "temporal-namespace" {
			defaults[sf.Name] = sf.Value
		}
	}

	require.Len(t, defaults, 2, "both temporal flags must exist on the streams command")
	for name, value := range defaults {
		require.Empty(t, value,
			"%s must have no default: a default makes unset mean 'dial localhost' "+
				"rather than the documented no-op, so wakes fail against a dead address", name)
	}
}

// With temporal resolving to empty, wakes are disabled rather than misdirected.
//
// The nil has to be a true nil interface, not a typed-nil *ChatMessageWriter.
// ChatPersister decides whether to wake with `notifier == nil`, and a typed nil
// makes that check false — routing every wake into a nil receiver.
func TestNewTranscriptWriter_UnconfiguredTemporalDisablesWakes(t *testing.T) {
	t.Parallel()

	for _, tt := range []struct {
		name  string
		flags map[string]string
	}{
		{name: "no address", flags: map[string]string{"temporal-address": ""}},
		{name: "no namespace", flags: map[string]string{"temporal-namespace": ""}},
		{name: "neither", flags: map[string]string{"temporal-address": "", "temporal-namespace": ""}},
	} {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			// Every temporal flag is set explicitly, including the ones this
			// case wants empty: urfave/cli resolves an unset flag from its
			// environment, and a machine running the local stack exports
			// TEMPORAL_ADDRESS and TEMPORAL_NAMESPACE — which would quietly
			// turn the unconfigured cases into configured ones.
			flags := map[string]string{
				"temporal-address":   "127.0.0.1:7233",
				"temporal-namespace": "default",
			}
			maps.Copy(flags, tt.flags)

			notifier, shutdown, err := newTranscriptWriter(
				newStreamsCLIContext(t, flags),
				testenv.NewLogger(t),
				testenv.NewTracerProvider(t),
				testenv.NewMeterProvider(t),
				nil,
			)
			require.NoError(t, err, "an unconfigured temporal is a deliberate no-op, not an error")
			require.Nil(t, notifier, "unset temporal must disable wakes")
			require.NotNil(t, shutdown, "callers register this unconditionally")
			require.NoError(t, shutdown(t.Context()))
		})
	}
}
