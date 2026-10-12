package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

const fixtureFile = fixturesPath + "/prompt_injection/deepset.jsonl"

func runGit(t *testing.T, dir string, args ...string) string {
	t.Helper()
	out, err := git(t.Context(), dir, args...)
	require.NoError(t, err)
	return out
}

func writeRepoFile(t *testing.T, dir, path, content string) {
	t.Helper()
	full := filepath.Join(dir, filepath.FromSlash(path))
	require.NoError(t, os.MkdirAll(filepath.Dir(full), 0o750))
	require.NoError(t, os.WriteFile(full, []byte(content), 0o600))
}

func commitAll(t *testing.T, dir, subject string) {
	t.Helper()
	runGit(t, dir, "add", "-A")
	runGit(t, dir, "-c", "user.name=bench", "-c", "user.email=bench@example.com", "-c", "commit.gpgsign=false", "commit", "-q", "--no-verify", "-m", subject)
}

// benchRepo is a checkout with code, an ignored path and a fixture, all
// committed.
func benchRepo(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	runGit(t, dir, "init", "-q")
	writeRepoFile(t, dir, ".gitignore", "bin/\n")
	writeRepoFile(t, dir, "server/main.go", "package main\n")
	writeRepoFile(t, dir, fixtureFile, `{"id": "a"}`+"\n")
	commitAll(t, dir, "chore: first")
	return dir
}

func TestCheckoutCommitReadsHeadAndUncommittedCode(t *testing.T) {
	t.Parallel()

	dir := benchRepo(t)
	clean, err := checkoutCommit(t.Context(), dir)
	require.NoError(t, err)
	require.Equal(t, runCommit{SHA: strings.TrimSpace(runGit(t, dir, "rev-parse", "HEAD")), Subject: "chore: first", Uncommitted: false}, clean)

	tests := []struct {
		name        string
		path        string
		uncommitted bool
	}{
		{name: "edited fixture", path: fixtureFile, uncommitted: false},
		{name: "new fixture", path: fixturesPath + "/prompt_injection/agentdojo.jsonl", uncommitted: false},
		{name: "ignored file", path: "bin/risk-pi-report", uncommitted: false},
		{name: "edited code", path: "server/main.go", uncommitted: true},
		{name: "new code", path: "server/new.go", uncommitted: true},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			dir := benchRepo(t)
			writeRepoFile(t, dir, tc.path, "changed\n")
			got, err := checkoutCommit(t.Context(), dir)
			require.NoError(t, err)
			require.Equal(t, strings.TrimSpace(runGit(t, dir, "rev-parse", "HEAD")), got.SHA)
			require.Equal(t, tc.uncommitted, got.Uncommitted)
		})
	}
}

// startRun makes a run of commit that started at started.
func startRun(t *testing.T, runs string, commit runCommit, started time.Time) string {
	t.Helper()
	dir, err := pickRunDir(runs, commit, false, started)
	require.NoError(t, err)
	require.NoError(t, claimRunDir(dir, commit, started))
	return dir
}

func TestPickRunDirStartsANewRunUnlessResuming(t *testing.T) {
	t.Parallel()

	runs := filepath.Join(t.TempDir(), "runs")
	start := time.Date(2026, 10, 12, 15, 30, 0, 0, time.UTC)
	edited := runCommit{SHA: mainCommit.SHA, Subject: mainCommit.Subject, Uncommitted: true}

	first := startRun(t, runs, mainCommit, start)
	second := startRun(t, runs, mainCommit, start)
	require.NotEqual(t, first, second, "every run gets its own directory, even at the same instant")
	require.True(t, strings.HasPrefix(filepath.Base(first), "20261012T153000.000000000Z-"))

	startRun(t, runs, mainCommit, start.Add(time.Millisecond))
	latest := startRun(t, runs, edited, start.Add(time.Hour))
	other := startRun(t, runs, fixtureCommit, start.Add(2*time.Hour))

	resumed, err := pickRunDir(runs, mainCommit, true, start.Add(3*time.Hour))
	require.NoError(t, err)
	require.Equal(t, latest, resumed, "resuming skips newer runs of other commits and ignores uncommitted changes")
	resumedOther, err := pickRunDir(runs, fixtureCommit, true, start.Add(3*time.Hour))
	require.NoError(t, err)
	require.Equal(t, other, resumedOther)

	unknown := runCommit{SHA: "0000000000aa", Subject: "chore: never run", Uncommitted: false}
	fresh, err := pickRunDir(runs, unknown, true, start.Add(4*time.Hour))
	require.NoError(t, err)
	require.True(t, strings.HasPrefix(filepath.Base(fresh), "20261012T193000.000000000Z-"), "resuming code that never ran starts a new run")
}

func TestPickRunDirStopsAtAnUnreadableManifest(t *testing.T) {
	t.Parallel()

	runs := filepath.Join(t.TempDir(), "runs")
	start := time.Date(2026, 10, 12, 15, 30, 0, 0, time.UTC)
	startRun(t, runs, mainCommit, start)
	broken := startRun(t, runs, mainCommit, start.Add(time.Minute))
	require.NoError(t, os.WriteFile(filepath.Join(broken, manifestFile), []byte(`{"commit": {`), 0o600))
	claiming := filepath.Join(runs, start.Add(time.Hour).Format(runIDLayout)+"-claiming")
	require.NoError(t, os.Mkdir(claiming, 0o750), "a run still being claimed has no manifest yet")

	_, err := pickRunDir(runs, mainCommit, true, start.Add(2*time.Hour))
	require.ErrorContains(t, err, "parse run manifest", "resuming must not skip past the broken latest run to an older one")
}

func TestRunCommitName(t *testing.T) {
	t.Parallel()

	require.Equal(t, "1a2b3c4d5e", runCommit{SHA: "1a2b3c4d5e6f7a8b", Subject: "", Uncommitted: false}.name())
	require.Equal(t, "1a2b3c4d5e + uncommitted", runCommit{SHA: "1a2b3c4d5e6f7a8b", Subject: "", Uncommitted: true}.name())
}
