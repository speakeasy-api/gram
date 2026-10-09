package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

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

func TestMeasuredCodeKeysTheCodeOutsideTheFixtures(t *testing.T) {
	t.Parallel()

	dir := benchRepo(t)
	first, err := measuredCode(t.Context(), dir)
	require.NoError(t, err)
	require.Len(t, first.key, codeKeyHexLen)
	require.Equal(t, runCommit{SHA: strings.TrimSpace(runGit(t, dir, "rev-parse", "HEAD")), Subject: "chore: first", Uncommitted: false}, first.commit)

	writeRepoFile(t, dir, fixtureFile, `{"id": "a", "edited": true}`+"\n")
	commitAll(t, dir, "chore: edit a fixture")
	fixtureOnly, err := measuredCode(t.Context(), dir)
	require.NoError(t, err)
	require.Equal(t, first.key, fixtureOnly.key, "a fixture-only commit keeps the key")
	require.Equal(t, "chore: edit a fixture", fixtureOnly.commit.Subject)

	writeRepoFile(t, dir, "server/main.go", "package main\n\nfunc main() {}\n")
	commitAll(t, dir, "fix: change the code")
	changed, err := measuredCode(t.Context(), dir)
	require.NoError(t, err)
	require.NotEqual(t, first.key, changed.key)
}

func TestMeasuredCodeMarksUncommittedCode(t *testing.T) {
	t.Parallel()

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
			clean, err := measuredCode(t.Context(), dir)
			require.NoError(t, err)

			writeRepoFile(t, dir, tc.path, "changed\n")
			got, err := measuredCode(t.Context(), dir)
			require.NoError(t, err)
			require.Equal(t, tc.uncommitted, got.commit.Uncommitted)
			require.Equal(t, tc.uncommitted, got.key != clean.key)
			require.True(t, strings.HasPrefix(got.key, clean.key), "uncommitted code extends the committed key")
			require.Equal(t, clean.commit.SHA, got.commit.SHA)
		})
	}
}

func TestRunCommitName(t *testing.T) {
	t.Parallel()

	require.Equal(t, "1a2b3c4d5e", runCommit{SHA: "1a2b3c4d5e6f7a8b", Subject: "", Uncommitted: false}.name())
	require.Equal(t, "1a2b3c4d5e + uncommitted", runCommit{SHA: "1a2b3c4d5e6f7a8b", Subject: "", Uncommitted: true}.name())
}
