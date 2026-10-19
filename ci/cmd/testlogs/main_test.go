package main

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

// writeAttempt writes a test log in the shape `go test -v` prints and returns
// the build event that reports it.
func writeAttempt(t *testing.T, dir string, label string, attempt int, status string, duration string, log string) string {
	t.Helper()
	path := filepath.Join(dir, fmt.Sprintf("attempt_%d.log", attempt))
	if status == "PASSED" {
		path = filepath.Join(dir, "test.log")
	}
	require.NoError(t, os.WriteFile(path, []byte(log), 0o600))

	event := map[string]any{
		"id": map[string]any{"testResult": map[string]any{"label": label, "run": 1, "shard": 1, "attempt": attempt}},
		"testResult": map[string]any{
			"status":              status,
			"testAttemptDuration": duration,
			"testActionOutput":    []map[string]string{{"name": "test.log", "uri": "file://" + path}},
		},
	}
	raw, err := json.Marshal(event)
	require.NoError(t, err)
	return string(raw)
}

func TestRun_FlakyTargetReportsLastAttemptAndRerun(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	label := "//server/internal/example:example_test"
	events := []string{
		`{"id":{"started":{}},"started":{"command":"test"}}`,
		writeAttempt(t, dir, label, 1, "FAILED", "1.5s", "=== RUN   TestA\n--- FAIL: TestA (0.10s)\n=== RUN   TestB\n--- PASS: TestB (0.00s)\nFAIL\n"),
		writeAttempt(t, dir, label, 2, "PASSED", "2.25s", "=== RUN   TestA\n--- PASS: TestA (0.20s)\n=== RUN   TestB\n--- PASS: TestB (0.00s)\nPASS\n"),
	}
	bep := filepath.Join(dir, "bep.json")
	require.NoError(t, os.WriteFile(bep, []byte(strings.Join(events, "\n")+"\n"), 0o600))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "coverage.dat"), []byte(
		"mode: atomic\n"+
			"github.com/speakeasy-api/gram/server/internal/example/a.go:1.1,2.2 1 1\n"+
			"github.com/speakeasy-api/gram/server/gen/example/b.go:1.1,2.2 1 0\n"), 0o600))

	out := filepath.Join(dir, "events.json")
	rerun := filepath.Join(dir, "rerun.txt")
	cover := filepath.Join(dir, "cover.out")
	require.NoError(t, run([]string{"-bep", bep, "-o", out, "-rerun-report", rerun, "-coverprofile", cover}))

	raw, err := os.ReadFile(out)
	require.NoError(t, err)
	var final map[string]any
	for line := range strings.Lines(string(raw)) {
		var ev map[string]any
		require.NoError(t, json.Unmarshal([]byte(line), &ev))
		require.Equal(t, "github.com/speakeasy-api/gram/server/internal/example", ev["Package"])
		if _, isTest := ev["Test"]; !isTest && (ev["Action"] == "pass" || ev["Action"] == "fail") {
			final = ev
		}
	}
	require.NotNil(t, final, "package result event")
	require.Equal(t, "pass", final["Action"])
	require.InDelta(t, 2.25, final["Elapsed"], 0.0001)

	report, err := os.ReadFile(rerun)
	require.NoError(t, err)
	require.Equal(t, "github.com/speakeasy-api/gram/server/internal/example.TestA: failed attempt 1 of 2\n", string(report))

	profile, err := os.ReadFile(cover)
	require.NoError(t, err)
	require.Equal(t, "mode: atomic\ngithub.com/speakeasy-api/gram/server/internal/example/a.go:1.1,2.2 1 1\n", string(profile))
}

func TestImportPath(t *testing.T) {
	t.Parallel()

	got, err := importPath("//server/internal/oops:oops_test")
	require.NoError(t, err)
	require.Equal(t, "github.com/speakeasy-api/gram/server/internal/oops", got)

	_, err = importPath("@other//pkg:pkg_test")
	require.Error(t, err)
}

func TestAttemptDuration(t *testing.T) {
	t.Parallel()

	d, err := attemptDuration("1.5s", "")
	require.NoError(t, err)
	require.Equal(t, 1500, int(d.Milliseconds()))

	d, err = attemptDuration("", "250")
	require.NoError(t, err)
	require.Equal(t, 250, int(d.Milliseconds()))
}
