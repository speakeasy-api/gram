package app

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/urfave/cli/v2"
)

// These tests change the working directory, so they cannot run in parallel.

// runAppIn runs the CLI in dir and returns what it wrote to stderr.
func runAppIn(t *testing.T, dir string, args ...string) (string, error) {
	t.Helper()
	t.Chdir(dir)

	var stderr bytes.Buffer
	app := newApp()
	app.Writer = &bytes.Buffer{}
	app.ErrWriter = &stderr
	app.ExitErrHandler = func(*cli.Context, error) {}

	full := append([]string{"speakeasy", "--profile-path", filepath.Join(t.TempDir(), "profile.json")}, args...)
	err := app.RunContext(t.Context(), full)
	return stderr.String(), err
}

const legacyNote = "Note: gram.deploy.json is deprecated and still works. Rename it to speakeasy.deploy.json.\n"

func TestResolveDeployFile(t *testing.T) { //nolint:paralleltest // changes the working directory
	tests := []struct {
		name     string
		explicit string
		existing []string
		want     string
		wantNote bool
	}{
		{name: "explicit path wins", explicit: "custom.json", existing: []string{"gram.deploy.json"}, want: "custom.json", wantNote: false},
		{name: "neither file exists", explicit: "", existing: nil, want: "speakeasy.deploy.json", wantNote: false},
		{name: "only the new file", explicit: "", existing: []string{"speakeasy.deploy.json"}, want: "speakeasy.deploy.json", wantNote: false},
		{name: "only the legacy file", explicit: "", existing: []string{"gram.deploy.json"}, want: "gram.deploy.json", wantNote: true},
		{name: "both files", explicit: "", existing: []string{"speakeasy.deploy.json", "gram.deploy.json"}, want: "speakeasy.deploy.json", wantNote: false},
	}
	for _, tc := range tests { //nolint:paralleltest // changes the working directory
		t.Run(tc.name, func(t *testing.T) {
			dir := t.TempDir()
			for _, name := range tc.existing {
				require.NoError(t, os.WriteFile(filepath.Join(dir, name), []byte("{}"), 0o600))
			}
			t.Chdir(dir)

			var note bytes.Buffer
			require.Equal(t, tc.want, resolveDeployFile(tc.explicit, &note))
			if tc.wantNote {
				require.Equal(t, legacyNote, note.String())
			} else {
				require.Empty(t, note.String())
			}
		})
	}
}

func TestStage_DefaultsToSpeakeasyDeployFile(t *testing.T) { //nolint:paralleltest // changes the working directory
	dir := t.TempDir()

	stderr, err := runAppIn(t, dir, "stage", "function", "--slug", "my-fn", "--location", "dist/functions.zip")
	require.NoError(t, err)
	require.Empty(t, stderr)

	cfg := readDeployConfig(t, filepath.Join(dir, "speakeasy.deploy.json"))
	require.Len(t, cfg.Sources, 1)
	require.NoFileExists(t, filepath.Join(dir, "gram.deploy.json"))
}

func TestStage_KeepsUsingLegacyDeployFile(t *testing.T) { //nolint:paralleltest // changes the working directory
	for _, args := range [][]string{ //nolint:paralleltest // changes the working directory
		{"stage", "function", "--slug", "my-fn", "--location", "dist/functions.zip"},
		{"functions", "stage", "--slug", "my-fn", "--location", "dist/functions.zip"},
	} {
		t.Run(args[0], func(t *testing.T) {
			dir := t.TempDir()
			legacy := filepath.Join(dir, "gram.deploy.json")
			require.NoError(t, os.WriteFile(legacy, []byte(`{"schema_version":"1.0.0","type":"deployment","sources":[]}`), 0o600))

			stderr, err := runAppIn(t, dir, args...)
			require.NoError(t, err)
			require.Equal(t, legacyNote, stderr)

			require.Len(t, readDeployConfig(t, legacy).Sources, 1)
			require.NoFileExists(t, filepath.Join(dir, "speakeasy.deploy.json"))
		})
	}
}

func TestDoStageFunction_DefaultsToSpeakeasyDeployFile(t *testing.T) { //nolint:paralleltest // changes the working directory
	dir := t.TempDir()
	t.Chdir(dir)

	require.NoError(t, DoStageFunction(StageFunctionOptions{ConfigFile: "", Slug: "my-fn", Name: "", Location: "dist/functions.zip", Runtime: "", Scale: nil, MemoryMiB: nil}))
	require.FileExists(t, filepath.Join(dir, "speakeasy.deploy.json"))
}

func TestPush_ConfigDefaultsToTheDeployFile(t *testing.T) { //nolint:paralleltest // changes the working directory
	dir := t.TempDir()

	// Without a deployment file, push names the default it looked for.
	_, err := runAppIn(t, dir, "push", "--api-key", "key", "--org", "org", "--project", "proj")
	require.ErrorContains(t, err, "speakeasy.deploy.json")
}

func TestResolveDeployFile_UncheckableLegacyFileIsChosen(t *testing.T) { //nolint:paralleltest // changes the working directory
	dir := t.TempDir()
	// A symlink loop makes stat fail with an error other than "not exist".
	if err := os.Symlink("gram.deploy.json", filepath.Join(dir, "gram.deploy.json")); err != nil {
		t.Skipf("symlinks unavailable: %v", err)
	}
	t.Chdir(dir)

	// The legacy file is still chosen, so opening it reports the real error.
	var note bytes.Buffer
	require.Equal(t, "gram.deploy.json", resolveDeployFile("", &note))
	require.Equal(t, legacyNote, note.String())
}
