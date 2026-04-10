package git_test

import (
	"testing"

	corpusgit "github.com/speakeasy-api/gram/server/internal/corpus/git"
	"github.com/stretchr/testify/require"
)

func TestInitBareRepo(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()

	repo, err := corpusgit.InitBareRepo(dir)
	require.NoError(t, err)
	require.NotNil(t, repo)

	// Verify it's a bare repo by reopening
	repo2, err := corpusgit.OpenRepo(dir)
	require.NoError(t, err)
	require.NotNil(t, repo2)
}

func TestCommitAndReadTree(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	repo, err := corpusgit.InitBareRepo(dir)
	require.NoError(t, err)

	files := map[string][]byte{
		"README.md":           []byte("# Hello"),
		"docs/guide.md":       []byte("Guide content"),
		"docs/nested/deep.md": []byte("Deep content"),
	}

	_, err = repo.CommitFiles(files, "initial commit")
	require.NoError(t, err)

	tree, err := repo.ReadTree("HEAD")
	require.NoError(t, err)
	require.Len(t, tree, 3)

	paths := make(map[string]bool)
	for _, entry := range tree {
		paths[entry.Path] = true
	}
	require.True(t, paths["README.md"])
	require.True(t, paths["docs/guide.md"])
	require.True(t, paths["docs/nested/deep.md"])
}

func TestReadBlob(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	repo, err := corpusgit.InitBareRepo(dir)
	require.NoError(t, err)

	content := []byte("# My Document\n\nSome content here.")
	_, err = repo.CommitFiles(map[string][]byte{
		"doc.md": content,
	}, "add doc")
	require.NoError(t, err)

	blob, err := repo.ReadBlob("HEAD", "doc.md")
	require.NoError(t, err)
	require.Equal(t, content, blob)
}

func TestReadBlob_NotFound(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	repo, err := corpusgit.InitBareRepo(dir)
	require.NoError(t, err)

	_, err = repo.CommitFiles(map[string][]byte{
		"exists.md": []byte("hi"),
	}, "add file")
	require.NoError(t, err)

	_, err = repo.ReadBlob("HEAD", "nonexistent.md")
	require.Error(t, err)
}

func TestFileLog(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	repo, err := corpusgit.InitBareRepo(dir)
	require.NoError(t, err)

	_, err = repo.CommitFiles(map[string][]byte{"doc.md": []byte("v1")}, "version 1")
	require.NoError(t, err)

	_, err = repo.CommitFiles(map[string][]byte{"doc.md": []byte("v2")}, "version 2")
	require.NoError(t, err)

	_, err = repo.CommitFiles(map[string][]byte{"doc.md": []byte("v3")}, "version 3")
	require.NoError(t, err)

	entries, err := repo.FileLog("doc.md")
	require.NoError(t, err)
	require.Len(t, entries, 3)

	// Most recent first
	require.Equal(t, "version 3", entries[0].Message)
	require.Equal(t, "version 2", entries[1].Message)
	require.Equal(t, "version 1", entries[2].Message)
}

func TestDiff(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	repo, err := corpusgit.InitBareRepo(dir)
	require.NoError(t, err)

	sha1, err := repo.CommitFiles(map[string][]byte{
		"keep.md":   []byte("keep"),
		"modify.md": []byte("original"),
		"delete.md": []byte("to delete"),
	}, "commit A")
	require.NoError(t, err)

	sha2, err := repo.CommitFiles(map[string][]byte{
		"keep.md":   []byte("keep"),
		"modify.md": []byte("modified"),
		"added.md":  []byte("new file"),
		// delete.md is absent = deleted
	}, "commit B")
	require.NoError(t, err)

	diff, err := repo.Diff(sha1, sha2)
	require.NoError(t, err)

	added := make(map[string]bool)
	modified := make(map[string]bool)
	deleted := make(map[string]bool)
	for _, d := range diff {
		switch d.Action {
		case corpusgit.DiffAdded:
			added[d.Path] = true
		case corpusgit.DiffModified:
			modified[d.Path] = true
		case corpusgit.DiffDeleted:
			deleted[d.Path] = true
		}
	}

	require.True(t, added["added.md"], "added.md should be added")
	require.True(t, modified["modify.md"], "modify.md should be modified")
	require.True(t, deleted["delete.md"], "delete.md should be deleted")
	require.False(t, added["keep.md"], "keep.md should not appear as changed")
	require.False(t, modified["keep.md"], "keep.md should not appear as changed")
}
