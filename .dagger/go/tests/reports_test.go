package pipeline_test

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
)

// Exercise the host wrapper's boundary: stale reports must not survive a setup
// failure, and exporting a fresh report must not hide the test exit status.
func TestGoTestReportExport(t *testing.T) {
	t.Parallel()

	script, err := filepath.Abs("../../../.mise-tasks/dagger/go-test.sh")
	require.NoError(t, err)
	for _, tc := range []struct {
		name       string
		mode       string
		exit       int
		wantReport bool
		wantOutput string
	}{
		{name: "success", mode: "success", exit: 0, wantReport: true, wantOutput: ""},
		{name: "test failure", mode: "failure", exit: 1, wantReport: true, wantOutput: "fresh test failure"},
		{name: "setup failure", mode: "setup", exit: 7, wantReport: false, wantOutput: ""},
		{name: "invalid status", mode: "invalid", exit: 1, wantReport: true, wantOutput: "Invalid test exit code"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			dir := t.TempDir()
			require.NoError(t, os.Mkdir(filepath.Join(dir, "cli"), 0o755))
			reports := []string{"junit-report.xml", "test-timings.json", "test-exit-code", "test-output.txt"}
			for _, name := range reports {
				require.NoError(t, os.WriteFile(filepath.Join(dir, "cli", name), []byte("stale"), 0o600))
			}
			require.NoError(t, os.WriteFile(filepath.Join(dir, "dagger"), []byte(`#!/usr/bin/env bash
set -eu
if [ "$PIPELINE_TEST_MODE" = setup ]; then exit 7; fi
status=0
if [ "$PIPELINE_TEST_MODE" = failure ]; then status=1; fi
if [ "$PIPELINE_TEST_MODE" = invalid ]; then status=invalid; fi
printf '%s\n' "$status" > cli/test-exit-code
printf 'fresh junit' > cli/junit-report.xml
printf 'fresh test failure' > cli/test-output.txt
`), 0o755))

			cmd := exec.CommandContext(t.Context(), "bash", script)
			cmd.Dir = dir
			cmd.Env = append(os.Environ(), "PATH="+dir+string(os.PathListSeparator)+os.Getenv("PATH"),
				"usage_component=cli", "usage_packages=./...", "PIPELINE_TEST_MODE="+tc.mode)
			output, err := cmd.CombinedOutput()
			if tc.exit == 0 {
				require.NoError(t, err, "%s", output)
			} else {
				var exitErr *exec.ExitError
				require.ErrorAs(t, err, &exitErr, "%s", output)
				require.Equal(t, tc.exit, exitErr.ExitCode(), "%s", output)
			}
			if tc.wantOutput != "" {
				require.Contains(t, string(output), tc.wantOutput)
			}
			require.NoFileExists(t, filepath.Join(dir, "cli", "test-timings.json"))
			if tc.wantReport {
				data, err := os.ReadFile(filepath.Join(dir, "cli", "junit-report.xml"))
				require.NoError(t, err)
				require.Equal(t, "fresh junit", string(data))
			} else {
				for _, name := range reports {
					require.NoFileExists(t, filepath.Join(dir, "cli", name))
				}
			}
		})
	}
}
