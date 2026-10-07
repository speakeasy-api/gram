package app

import (
	"bytes"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/urfave/cli/v2"
)

// brandedWords are old product names that must not appear in help text. The
// legacy file names (gram.config.*, gram.deploy.json, src/gram.ts) are
// lowercase and stay listed as fallbacks.
var brandedWords = []string{"Gram", "GRAM_"}

func requireUnbranded(t *testing.T, where string, text string) {
	t.Helper()
	for _, word := range brandedWords {
		require.NotContains(t, text, word, "%s mentions %q", where, word)
	}
}

func checkFlags(t *testing.T, where string, fs []cli.Flag) {
	t.Helper()
	for _, f := range fs {
		name := where + " --" + f.Names()[0]
		if df, ok := f.(cli.DocGenerationFlag); ok {
			requireUnbranded(t, name, df.GetUsage())
			requireUnbranded(t, name, strings.Join(df.GetEnvVars(), " "))
		}
		requireUnbranded(t, name, f.String())
	}
}

func checkCommands(t *testing.T, path string, cmds []*cli.Command) {
	t.Helper()
	for _, cmd := range cmds {
		where := path + " " + cmd.Name
		requireUnbranded(t, where, cmd.Usage)
		requireUnbranded(t, where, cmd.UsageText)
		requireUnbranded(t, where, cmd.Description)
		// A [1:] after a concatenated description slices only its last
		// literal, leaving a leading newline and eating a character.
		require.False(t, strings.HasPrefix(cmd.Description, "\n"), "%s description starts with a newline", where)
		requireUnbranded(t, where, cmd.ArgsUsage)
		checkFlags(t, where, cmd.Flags)
		checkCommands(t, where, cmd.Subcommands)
	}
}

func TestHelpText_HasNoGramBranding(t *testing.T) {
	t.Parallel()

	app := newApp()
	requireUnbranded(t, "speakeasy", app.Usage)
	requireUnbranded(t, "speakeasy", app.Description)
	checkFlags(t, "speakeasy", app.Flags)
	checkCommands(t, "speakeasy", app.Commands)
}

func TestHelpText_RenderedHelpHasNoGramBranding(t *testing.T) {
	t.Parallel()

	for _, args := range [][]string{
		{"--help"},
		{"functions", "--help"},
		{"auth", "--help"},
		{"install", "--help"},
		{"stage", "--help"},
		{"push", "--help"},
	} {
		var out bytes.Buffer
		app := newApp()
		app.Writer = &out
		app.ErrWriter = &out
		require.NoError(t, app.RunContext(t.Context(), append([]string{"speakeasy", "--profile-path", t.TempDir() + "/profile.json"}, args...)))
		requireUnbranded(t, strings.Join(args, " "), out.String())
	}
}
