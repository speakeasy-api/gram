package app

import (
	"bytes"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/urfave/cli/v2"
)

func TestInvokedAs(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		arg0 string
		want string
	}{
		{name: "bare speakeasy", arg0: "speakeasy", want: "speakeasy"},
		{name: "absolute gram path", arg0: "/opt/homebrew/bin/gram", want: "gram"},
		{name: "relative speakeasy path", arg0: "./bin/speakeasy", want: "speakeasy"},
		{name: "windows gram exe", arg0: `C:\Program Files\gram\gram.exe`, want: "gram"},
		{name: "windows uppercase exe", arg0: `C:\tools\GRAM.EXE`, want: "gram"},
		{name: "windows speakeasy exe", arg0: `C:\tools\speakeasy.exe`, want: "speakeasy"},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			require.Equal(t, tc.want, invokedAs(tc.arg0))
		})
	}
}

func TestWriteLegacyCommandNotice_Gram(t *testing.T) {
	t.Parallel()

	var buf bytes.Buffer
	writeLegacyCommandNotice(&buf, "/usr/local/bin/gram")

	require.Equal(t, legacyCommandNotice+"\n", buf.String())
	require.Contains(t, buf.String(), "brew install speakeasy-api/tap/cli")
	require.Contains(t, buf.String(), "npm i -g @speakeasy-api/cli")
}

func TestWriteLegacyCommandNotice_Speakeasy(t *testing.T) {
	t.Parallel()

	var buf bytes.Buffer
	writeLegacyCommandNotice(&buf, "/opt/homebrew/bin/speakeasy")

	require.Equal(t, sharedCommandNotice+"\n", buf.String())
	require.Contains(t, buf.String(), "now called speakeasy-ai")
	require.Contains(t, buf.String(), "brew install speakeasy-api/tap/cli")
}

func TestWriteLegacyCommandNotice_SpeakeasyAI(t *testing.T) {
	t.Parallel()

	var buf bytes.Buffer
	writeLegacyCommandNotice(&buf, "/usr/local/bin/speakeasy-ai")

	require.Empty(t, buf.String())
}

func TestNewApp_UsesSpeakeasyAIName(t *testing.T) {
	t.Parallel()

	require.Equal(t, "speakeasy-ai", newApp(commandName).Name)
}

func TestNewApp_ControlPlaneMarker(t *testing.T) {
	t.Parallel()

	var stdout bytes.Buffer
	app := newApp(commandName)
	app.Writer = &stdout
	app.ErrWriter = &bytes.Buffer{}

	profilePath := filepath.Join(t.TempDir(), "profile.json")
	err := app.RunContext(t.Context(), []string{"speakeasy", "--profile-path", profilePath, "--control-plane-cli"})

	require.NoError(t, err)
	require.Equal(t, "speakeasy-ai-control-plane-cli\n", stdout.String())
}

func TestNewApp_NoArgsShowsHelp(t *testing.T) {
	t.Parallel()

	var stdout bytes.Buffer
	app := newApp(commandName)
	app.Writer = &stdout
	app.ErrWriter = &bytes.Buffer{}

	profilePath := filepath.Join(t.TempDir(), "profile.json")
	err := app.RunContext(t.Context(), []string{"speakeasy", "--profile-path", profilePath})

	require.NoError(t, err)
	require.NotContains(t, stdout.String(), "speakeasy-ai-control-plane-cli")
	require.Contains(t, stdout.String(), "USAGE:")
}

func TestNewApp_SDKGeneratorCommandNamesTheOtherCLI(t *testing.T) {
	t.Parallel()

	var exitErr error
	app := newApp(commandName)
	app.Writer = &bytes.Buffer{}
	app.ErrWriter = &bytes.Buffer{}
	app.ExitErrHandler = func(_ *cli.Context, err error) { exitErr = err }

	profilePath := filepath.Join(t.TempDir(), "profile.json")
	err := app.RunContext(t.Context(), []string{"speakeasy-ai", "--profile-path", profilePath, "run"})

	require.Error(t, err)
	require.Equal(t, err, exitErr)
	var exitCoder cli.ExitCoder
	require.ErrorAs(t, err, &exitCoder)
	require.Equal(t, unknownCommandExitCode, exitCoder.ExitCode())
	require.Contains(t, err.Error(), `"run" is a command of the Speakeasy SDK generator`)
	require.Contains(t, err.Error(), "Run it with the SDK generator instead: speakeasy run")
}

func TestNewApp_SDKGeneratorCommandRunAsSpeakeasyNamesTheRename(t *testing.T) {
	t.Parallel()

	app := newApp(sharedCommandName)
	app.Writer = &bytes.Buffer{}
	app.ErrWriter = &bytes.Buffer{}
	app.ExitErrHandler = func(*cli.Context, error) {}

	profilePath := filepath.Join(t.TempDir(), "profile.json")
	err := app.RunContext(t.Context(), []string{sharedCommandName, "--profile-path", profilePath, "run"})

	require.Error(t, err)
	var exitCoder cli.ExitCoder
	require.ErrorAs(t, err, &exitCoder)
	require.Equal(t, unknownCommandExitCode, exitCoder.ExitCode())
	require.Contains(t, err.Error(), "now called speakeasy-ai")
	require.NotContains(t, err.Error(), "Run it with the SDK generator instead")
}

func TestSDKGeneratorCommandMessage_RunAsSpeakeasy(t *testing.T) {
	t.Parallel()

	message, ok := sdkGeneratorCommandMessage("quickstart", "speakeasy")

	require.True(t, ok)
	require.Contains(t, message, "now called speakeasy-ai")
	require.Contains(t, message, "remove this copy, so speakeasy runs the SDK generator")
}

func TestNewApp_UnknownCommandKeepsDefaultError(t *testing.T) {
	t.Parallel()

	app := newApp(commandName)
	app.Writer = &bytes.Buffer{}
	app.ErrWriter = &bytes.Buffer{}
	app.ExitErrHandler = func(*cli.Context, error) {}

	profilePath := filepath.Join(t.TempDir(), "profile.json")
	err := app.RunContext(t.Context(), []string{"speakeasy", "--profile-path", profilePath, "nope"})

	require.Error(t, err)
	require.NotContains(t, err.Error(), "SDK generator")
}

func TestSDKGeneratorCommandsSkipSharedAndOwnCommands(t *testing.T) {
	t.Parallel()

	for _, command := range newApp(commandName).Commands {
		require.NotContains(t, sdkGeneratorCommands, command.Name, "this CLI has its own %q command", command.Name)
	}
	for _, shared := range []string{"auth", "status", "update"} {
		_, ok := sdkGeneratorCommandMessage(shared, commandName)
		require.False(t, ok, "%q exists in both CLIs", shared)
	}
}
