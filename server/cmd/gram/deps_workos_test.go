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
			// which needs no key. The endpoint is what makes it reachable.
			name:            "local falls back to the mock endpoint",
			env:             "local",
			flags:           map[string]string{"workos-endpoint": "http://127.0.0.1:35000"},
			wantUnavailable: false,
		},
		{
			name:            "local reaches the mock endpoint past the unset sentinel",
			env:             "local",
			flags:           map[string]string{"workos-api-key": "unset", "workos-endpoint": "http://127.0.0.1:35000"},
			wantUnavailable: false,
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
			// the local stack has both WORKOS_API_URL and
			// GRAM_IDP_CLIENT_SECRET exported, which silently turns the
			// refusing cases into configured ones.
			flags := map[string]string{
				"environment":     tc.env,
				"workos-api-key":  "",
				"workos-endpoint": "",
				"idp-client-id":   "",
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

// TestNewAdminWorkOSOrganizationCreator_FallsBackToTheSharedIdPSecret pins the
// second environment variable on the flag. The admin server is deployed beside
// a server and a worker that already read GRAM_IDP_CLIENT_SECRET, and reading
// it too is what keeps organization creation working without a second copy of
// the same secret being added to the deployment.
//
// Not parallel: it edits the process environment, which t.Setenv already
// refuses to do from a parallel test.
func TestNewAdminWorkOSOrganizationCreator_FallsBackToTheSharedIdPSecret(t *testing.T) {
	// WORKOS_API_KEY comes first in the flag's list, and urfave/cli stops at
	// the first variable that is merely present. Leaving it set to anything,
	// the empty string included, would decide this test before the fallback is
	// consulted, so it has to be removed rather than blanked.
	unsetEnv(t, "WORKOS_API_KEY")
	t.Setenv("GRAM_IDP_CLIENT_SECRET", "test-api-key")

	got := newAdminWorkOSOrganizationCreator(
		t.Context(),
		testenv.NewLogger(t),
		guardian.NewDefaultPolicy(noop.NewTracerProvider()),
		newAdminCLIContext(t, map[string]string{
			"environment":     "prod",
			"workos-endpoint": "",
			"idp-client-id":   "",
		}),
	)

	require.IsType(t, (*workos.Client)(nil), got,
		"a deployment that sets only the shared identity-provider secret must still be able to create organizations")
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
