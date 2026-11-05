package main

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"testing/fstest"

	"github.com/stretchr/testify/require"
)

func TestTestExists(t *testing.T) {
	t.Parallel()

	fsys := fstest.MapFS{
		"server/internal/mcp/serve_test.go": {Data: []byte("package mcp\n\nfunc TestServe(t *testing.T) {}\n\nfunc (s *suite) TestMethod(t *testing.T) {}\n")},
		"server/internal/mcp/helpers.go":    {Data: []byte("package mcp\n\nfunc TestHelper() {}\n")},
		"server/internal/empty/doc.go":      {Data: []byte("package empty\n")},
	}

	cases := []struct {
		name string
		key  testKey
		want bool
	}{
		{name: "declared", key: testKey{Package: "server/internal/mcp", Test: "TestServe"}, want: true},
		{name: "removed from its package", key: testKey{Package: "server/internal/mcp", Test: "TestGone"}, want: false},
		{name: "method with the name is not a test", key: testKey{Package: "server/internal/mcp", Test: "TestMethod"}, want: false},
		{name: "function outside a test file is not a test", key: testKey{Package: "server/internal/mcp", Test: "TestHelper"}, want: false},
		{name: "package without test files", key: testKey{Package: "server/internal/empty", Test: "TestServe"}, want: false},
		{name: "deleted package", key: testKey{Package: "server/internal/xmcp", Test: "TestServe"}, want: false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			got, err := testExists(fsys, tc.key)
			require.NoError(t, err)
			require.Equal(t, tc.want, got)
		})
	}
}

func TestTestExists_ParseError(t *testing.T) {
	t.Parallel()

	fsys := fstest.MapFS{
		"pkg/a_test.go": {Data: []byte("package pkg\n\nfunc TestBroken(\n")},
		"pkg/b_test.go": {Data: []byte("package pkg\n\nfunc TestFound(t *testing.T) {}\n")},
	}

	got, err := testExists(fsys, testKey{Package: "pkg", Test: "TestFound"})
	require.NoError(t, err, "another file declares the test, so the parse error does not matter")
	require.True(t, got)

	_, err = testExists(fsys, testKey{Package: "pkg", Test: "TestBroken"})
	require.ErrorContains(t, err, "parse pkg/a_test.go", "the unparseable file might declare the test")
}

func TestTestExists_InvalidKey(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name string
		key  testKey
	}{
		{name: "package escapes the root", key: testKey{Package: "../outside", Test: "TestServe"}},
		{name: "test is not an identifier", key: testKey{Package: "pkg", Test: "Test.*"}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			_, err := testExists(fstest.MapFS{}, tc.key)
			require.ErrorContains(t, err, "invalid test")
		})
	}
}

func TestCheckModuleRoot(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name    string
		gomod   string
		wantErr string
	}{
		{name: "repository module", gomod: "// comment\nmodule github.com/speakeasy-api/gram\n\ngo 1.26\n"},
		{name: "other module", gomod: "module example.com/other\n", wantErr: "not github.com/speakeasy-api/gram"},
		{name: "no module", gomod: "go 1.26\n", wantErr: "declares no module"},
		{name: "no go.mod", wantErr: "open go.mod"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			root := t.TempDir()
			if tc.gomod != "" {
				require.NoError(t, os.WriteFile(filepath.Join(root, "go.mod"), []byte(tc.gomod), 0o600))
			}

			err := checkModuleRoot(root)
			if tc.wantErr == "" {
				require.NoError(t, err)
				return
			}
			require.ErrorContains(t, err, tc.wantErr)
		})
	}
}

func TestFindRemoval(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	git := func(args ...string) string {
		t.Helper()
		cmd := exec.CommandContext(t.Context(), "git", append([]string{"-c", "user.name=test", "-c", "user.email=test@example.com", "-c", "commit.gpgsign=false"}, args...)...)
		cmd.Dir = root
		out, err := cmd.CombinedOutput()
		require.NoError(t, err, string(out))
		return strings.TrimSpace(string(out))
	}
	write := func(name, src string) {
		t.Helper()
		full := filepath.Join(root, name)
		require.NoError(t, os.MkdirAll(filepath.Dir(full), 0o700))
		require.NoError(t, os.WriteFile(full, []byte(src), 0o600))
	}

	git("init", "-q")
	write("pkg/a_test.go", "package pkg\n\nfunc TestGone(t *testing.T) {}\n\nfunc TestKept(t *testing.T) {}\n")
	git("add", ".")
	git("commit", "-q", "-m", "add tests")
	write("pkg/a_test.go", "package pkg\n\nfunc TestKept(t *testing.T) {}\n")
	git("commit", "-q", "-am", "chore: remove TestGone")
	removedSHA := git("rev-parse", "HEAD")
	write("pkg/a_test.go", "package pkg\n\nfunc TestKept(t *testing.T) { t.Parallel() }\n")
	git("commit", "-q", "-am", "unrelated edit")

	got, err := findRemoval(t.Context(), root, testKey{Package: "pkg", Test: "TestGone"})
	require.NoError(t, err)
	require.Equal(t, removal{SHA: removedSHA, Subject: "chore: remove TestGone"}, got)

	_, err = findRemoval(t.Context(), root, testKey{Package: "pkg", Test: "TestNeverExisted"})
	require.ErrorContains(t, err, "no commit in history")
}

func TestPruneComment(t *testing.T) {
	t.Parallel()

	key := testKey{Package: "server/internal/xmcp", Test: "TestServe"}
	removedBy := removal{SHA: "e25139992edd1aa2d9e610f506f695aad97a8fc6", Subject: "chore: retire xmcp (#1)"}

	cases := []struct {
		name      string
		removedBy removal
		repo      string
		want      string
	}{
		{
			name:      "linked commit",
			removedBy: removedBy,
			repo:      "speakeasy-api/gram",
			want:      "Canceling: `TestServe` no longer exists in `server/internal/xmcp` on `main`. It was removed in [`e25139992e`](https://github.com/speakeasy-api/gram/commit/e25139992edd1aa2d9e610f506f695aad97a8fc6) (chore: retire xmcp (#1)).",
		},
		{
			name:      "unlinked commit",
			removedBy: removedBy,
			want:      "Canceling: `TestServe` no longer exists in `server/internal/xmcp` on `main`. It was removed in `e25139992e` (chore: retire xmcp (#1)).",
		},
		{
			name: "no commit found",
			repo: "speakeasy-api/gram",
			want: "Canceling: `TestServe` no longer exists in `server/internal/xmcp` on `main`.",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			require.Equal(t, tc.want, pruneComment(key, tc.removedBy, tc.repo))
		})
	}
}
