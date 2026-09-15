package gram

import (
	"flag"
	"maps"
	"os"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/urfave/cli/v2"
	"go.opentelemetry.io/otel/trace/noop"

	"github.com/speakeasy-api/gram/server/internal/guardian"
	"github.com/speakeasy-api/gram/server/internal/organizations/orgprovision"
	"github.com/speakeasy-api/gram/server/internal/testenv"
	"github.com/speakeasy-api/gram/server/internal/thirdparty/workos"
)

// TestNewAdminWorkOSOrganizationCreator covers every branch of the wiring the
// create-organization endpoint depends on. The one the admin tests lean on
// hardest is the last: they all assume a deployment with nothing configured
// still gets a usable client, because the handler calls it without a nil check.
func TestNewAdminWorkOSOrganizationCreator(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name string
		env  string
		// flags are set on the real admin command's flag set, so the test is
		// bound to the flag names the deployment actually configures.
		flags map[string]string
		// wantUnavailable is the whole point of the switch: refusing is a
		// deliberate outcome, not an error path.
		wantUnavailable bool
	}{
		{
			name:            "a real key is used wherever it is set",
			env:             "prod",
			flags:           map[string]string{"workos-api-key": "test-api-key"},
			wantUnavailable: false,
		},
		{
			// "unset" is what the deployment templates write when they have no
			// secret to supply, so treating it as a key would send that string
			// to WorkOS as a bearer token on every create.
			name:            "the unset sentinel is not a key",
			env:             "prod",
			flags:           map[string]string{"workos-api-key": "unset"},
			wantUnavailable: true,
		},
		{
			name:            "no key outside local refuses",
			env:             "prod",
			flags:           map[string]string{},
			wantUnavailable: true,
		},
		{
			// Local development points at the dev-idp mock-workos emulator,
			// which authenticates callers with its own client secret.
			name: "local uses the dev-idp client secret",
			env:  "local",
			flags: map[string]string{
				"idp-client-secret": "test-client-secret",
				"workos-endpoint":   "http://127.0.0.1:35000",
			},
			wantUnavailable: false,
		},
		{
			name: "local rejects the unset client-secret sentinel",
			env:  "local",
			flags: map[string]string{
				"idp-client-secret": "unset",
				"workos-endpoint":   "http://127.0.0.1:35000",
			},
			wantUnavailable: true,
		},
		{
			name:            "local with nothing configured refuses",
			env:             "local",
			flags:           map[string]string{},
			wantUnavailable: true,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			// Every flag the function reads is set explicitly, including the
			// ones this case wants empty. urfave/cli resolves an unset flag
			// from its environment variables, and a developer machine running
			// the local stack exports WORKOS_API_URL and a real per-checkout
			// GRAM_IDP_CLIENT_SECRET — together exactly the pair that turns a
			// refusing case into the configured local branch. WORKOS_API_KEY
			// is exported too, though mise defaults it to the "unset"
			// sentinel the function rejects.
			flags := map[string]string{
				"environment":       tc.env,
				"idp-client-secret": "",
				"workos-api-key":    "",
				"workos-endpoint":   "",
				"idp-client-id":     "",
			}
			maps.Copy(flags, tc.flags)

			got := newAdminWorkOSOrganizationCreator(
				t.Context(),
				testenv.NewLogger(t),
				guardian.NewDefaultPolicy(noop.NewTracerProvider()),
				newAdminCLIContext(t, flags),
			)

			// Never nil, whatever the configuration. The admin service stores
			// this on a struct field and the handler calls it directly.
			require.NotNil(t, got)

			if tc.wantUnavailable {
				require.IsType(t, orgprovision.Unavailable{}, got,
					"an unconfigured deployment must refuse rather than mint organizations only Gram knows about")
				return
			}

			require.IsType(t, (*workos.Client)(nil), got)
		})
	}
}

// unsetEnv removes a variable for the duration of a test and puts it back
// afterwards. testing.T can set a variable but not remove one, and an empty
// value is not the same thing to urfave/cli.
func unsetEnv(t *testing.T, name string) {
	t.Helper()

	original, wasSet := os.LookupEnv(name)
	if !wasSet {
		return
	}

	require.NoError(t, os.Unsetenv(name))
	t.Cleanup(func() {
		require.NoError(t, os.Setenv(name, original)) //nolint:usetesting // t.Setenv cannot run from a cleanup function
	})
}

// newAdminCLIContext builds a context from the admin command's own flags rather
// than a copy of them, so a flag renamed or stripped of an environment variable
// in admin.go breaks these tests instead of silently passing.
func newAdminCLIContext(t *testing.T, values map[string]string) *cli.Context {
	t.Helper()

	set := flag.NewFlagSet("admin", flag.ContinueOnError)
	for _, f := range newAdminCommand().Flags {
		require.NoError(t, f.Apply(set))
	}

	for name, value := range values {
		require.NoError(t, set.Set(name, value))
	}

	return cli.NewContext(cli.NewApp(), set, nil)
}
