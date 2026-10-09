package app

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/urfave/cli/v2"

	"github.com/speakeasy-api/gram/cli/internal/flags"
)

// TestLegacyEnv_ReachesParsedFlags sets only the deprecated GRAM_* names,
// applies them the way Execute does, and checks the CLI parses every value
// from them. It guards the promise that existing GRAM_* setups keep working.
func TestLegacyEnv_ReachesParsedFlags(t *testing.T) { //nolint:paralleltest // sets process environment
	for _, name := range flags.EnvSettings {
		t.Setenv(flags.EnvVar(name), "")
	}

	profilePath := filepath.Join(t.TempDir(), "profile.json")
	legacy := map[string]string{
		"GRAM_API_KEY":               "legacy-key",
		"GRAM_API_URL":               "https://legacy.example.test",
		"GRAM_SITE_URL":              "https://legacy-site.example.test",
		"GRAM_ORG":                   "legacy-org",
		"GRAM_PROJECT":               "legacy-project",
		"GRAM_PROFILE":               "legacy-profile",
		"GRAM_PROFILE_PATH":          profilePath,
		"GRAM_LOG_LEVEL":             "debug",
		"GRAM_LOG_PRETTY":            "false",
		"GRAM_FUNCTIONS_SDK_VERSION": "^9.9.9",
	}
	require.Len(t, legacy, len(flags.EnvSettings), "every setting needs a legacy case")
	for k, v := range legacy {
		t.Setenv(k, v)
	}

	notice, err := flags.ApplyLegacyEnv(os.LookupEnv, func(k, v string) error {
		t.Setenv(k, v)
		return nil
	})
	require.NoError(t, err)
	for k := range legacy {
		require.Contains(t, notice, k+" to SPEAKEASY_AI_"+strings.TrimPrefix(k, "GRAM_"))
	}

	// Settings bound to subcommand flags or read directly are checked through
	// the variable the CLI actually reads.
	for k, v := range legacy {
		require.Equal(t, v, os.Getenv("SPEAKEASY_AI_"+strings.TrimPrefix(k, "GRAM_")), k)
	}

	got := map[string]string{}
	app := newApp()
	app.Commands = append(app.Commands, &cli.Command{
		Name:   "legacy-env-probe",
		Hidden: true,
		Flags:  []cli.Flag{flags.APIEndpoint()},
		Action: func(c *cli.Context) error {
			got["api-key"] = c.String("api-key")
			got["project"] = c.String("project")
			got["org"] = c.String("org")
			got["profile"] = c.String("profile")
			got["log-level"] = c.String("log-level")
			got["api-url"] = c.String("api-url")
			if c.Bool("log-pretty") {
				got["log-pretty"] = "true"
			} else {
				got["log-pretty"] = "false"
			}
			return nil
		},
	})
	require.NoError(t, app.RunContext(t.Context(), []string{"speakeasy", "legacy-env-probe"}))

	require.Equal(t, map[string]string{
		"api-key":    "legacy-key",
		"project":    "legacy-project",
		"org":        "legacy-org",
		"profile":    "legacy-profile",
		"log-level":  "debug",
		"api-url":    "https://legacy.example.test",
		"log-pretty": "false",
	}, got)
}

// TestLegacyEnv_NewNameWins checks that no SPEAKEASY_AI_* value is ever
// overwritten by a conflicting deprecated GRAM_* value, for every setting, and
// that no notice is printed.
func TestLegacyEnv_NewNameWins(t *testing.T) { //nolint:paralleltest // sets process environment
	for _, name := range flags.EnvSettings {
		t.Setenv(flags.EnvVar(name), "new-"+name)
		t.Setenv("GRAM_"+name, "legacy-"+name)
	}

	notice, err := flags.ApplyLegacyEnv(os.LookupEnv, func(k, v string) error {
		t.Setenv(k, v)
		return nil
	})
	require.NoError(t, err)
	require.Empty(t, notice)
	for _, name := range flags.EnvSettings {
		require.Equal(t, "new-"+name, os.Getenv(flags.EnvVar(name)), name)
	}
}
