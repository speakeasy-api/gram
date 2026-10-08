package testenv_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/testenv"
)

func TestFindRepoRootCurrentCheckout(t *testing.T) {
	t.Parallel()
	root, err := testenv.FindRepoRoot(t.Context())
	require.NoError(t, err)
	require.FileExists(t, filepath.Join(root, "server", "database", "schema.sql"))
	require.FileExists(t, filepath.Join(root, "server", "clickhouse", "schema.sql"))
	require.DirExists(t, filepath.Join(root, "functions", "cmd", "runner"))
}

func TestFindRepoRootNestedModuleInWorktree(t *testing.T) { //nolint:paralleltest // changes the process working directory
	root := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(root, "go.mod"), []byte("module github.com/speakeasy-api/gram\n"), 0600))
	require.NoError(t, os.WriteFile(filepath.Join(root, ".git"), []byte("gitdir: /unused/worktree\n"), 0600))
	nested := filepath.Join(root, "nested")
	require.NoError(t, os.Mkdir(nested, 0700))
	require.NoError(t, os.WriteFile(filepath.Join(nested, "go.mod"), []byte("module example.com/other\n"), 0600))
	t.Chdir(nested)
	got, err := testenv.FindRepoRoot(t.Context())
	require.NoError(t, err)
	want, err := filepath.EvalSymlinks(root)
	require.NoError(t, err)
	require.Equal(t, want, got)
}

func TestFindRepoRootOutsideCheckout(t *testing.T) { //nolint:paralleltest // changes the process working directory
	t.Chdir(t.TempDir())
	_, err := testenv.FindRepoRoot(t.Context())
	require.ErrorContains(t, err, "no matching go.mod")
}
