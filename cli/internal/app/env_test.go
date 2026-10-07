package app

import (
	"bytes"
	"os"
	"slices"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/urfave/cli/v2"

	"github.com/speakeasy-api/gram/cli/internal/flags"
)

// envVarsOf returns the environment variables a flag reads, if any.
func envVarsOf(f cli.Flag) []string {
	if ef, ok := f.(interface{ GetEnvVars() []string }); ok {
		return ef.GetEnvVars()
	}
	return nil
}

// walkFlags calls fn for every flag of the app and its commands.
func walkFlags(cmds []*cli.Command, fn func(cli.Flag)) {
	for _, cmd := range cmds {
		for _, f := range cmd.Flags {
			fn(f)
		}
		walkFlags(cmd.Subcommands, fn)
	}
}

func TestNewApp_EveryEnvBoundFlagReadsBothNames(t *testing.T) {
	t.Parallel()

	app := newApp()
	var seen []string
	check := func(f cli.Flag) {
		vars := envVarsOf(f)
		if len(vars) == 0 {
			return
		}
		setting := strings.TrimPrefix(vars[0], "SPEAKEASY_AI_")
		require.Equal(t, flags.EnvVars(setting), vars, "flag %v", f.Names())
		require.Contains(t, flags.EnvSettings, setting, "flag %v", f.Names())
		seen = append(seen, setting)
	}
	for _, f := range app.Flags {
		check(f)
	}
	walkFlags(app.Commands, check)

	// Every setting except the SDK version override, which init reads from
	// the environment directly, is bound to a flag.
	for _, setting := range flags.EnvSettings {
		if setting == "FUNCTIONS_SDK_VERSION" {
			continue
		}
		require.True(t, slices.Contains(seen, setting), "no flag reads %s", setting)
	}
}

// resolveFlag runs a one-flag app and returns the value the flag resolved to.
func resolveFlag(t *testing.T, flag cli.Flag) string {
	t.Helper()

	var got string
	app := &cli.App{
		Name:      "speakeasy",
		Flags:     []cli.Flag{flag},
		Writer:    &bytes.Buffer{},
		ErrWriter: &bytes.Buffer{},
		Action: func(c *cli.Context) error {
			got = c.String(flag.Names()[0])
			return nil
		},
	}
	require.NoError(t, app.RunContext(t.Context(), []string{"speakeasy"}))
	return got
}

// The env precedence tests set process environment variables, so they cannot
// run in parallel.

// unsetEnv unsets both names of each setting for the rest of the test, so
// values from the developer's shell do not leak in.
func unsetEnv(t *testing.T, settings ...string) {
	t.Helper()
	for _, setting := range settings {
		for _, name := range flags.EnvVars(setting) {
			t.Setenv(name, "")
			require.NoError(t, os.Unsetenv(name))
		}
	}
}

func TestEnvPrecedence_NewNameWins(t *testing.T) {
	unsetEnv(t, "API_KEY", "PROJECT", "ORG")
	t.Setenv("SPEAKEASY_AI_API_KEY", "new-key")
	t.Setenv("GRAM_API_KEY", "old-key")

	require.Equal(t, "new-key", resolveFlag(t, flags.APIKey()))
}

func TestEnvPrecedence_LegacyNameStillWorks(t *testing.T) {
	unsetEnv(t, "API_KEY", "PROJECT", "ORG")
	t.Setenv("GRAM_PROJECT", "old-project")

	require.Equal(t, "old-project", resolveFlag(t, flags.Project()))
}

func TestEnvPrecedence_NewNameAlone(t *testing.T) {
	unsetEnv(t, "API_KEY", "PROJECT", "ORG")
	t.Setenv("SPEAKEASY_AI_ORG", "new-org")

	require.Equal(t, "new-org", resolveFlag(t, flags.Org()))
}

func TestEnvPrecedence_IgnoresSDKGeneratorNames(t *testing.T) {
	unsetEnv(t, "API_KEY", "PROJECT", "ORG")
	// The Speakeasy SDK generator CLI reads SPEAKEASY_API_KEY for its own key,
	// so this CLI must never read the plain SPEAKEASY_* names.
	t.Setenv("SPEAKEASY_API_KEY", "generator-key")
	t.Setenv("SPEAKEASY_PROJECT", "generator-project")

	require.Empty(t, resolveFlag(t, flags.APIKey()))
	require.Empty(t, resolveFlag(t, flags.Project()))

	t.Setenv("GRAM_API_KEY", "gram-key")
	require.Equal(t, "gram-key", resolveFlag(t, flags.APIKey()))
}
