package githttp_test

import (
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"

	gogit "github.com/go-git/go-git/v6"
	"github.com/go-git/go-git/v6/plumbing"
	"github.com/go-git/go-git/v6/plumbing/object"
	githttp "github.com/go-git/go-git/v6/plumbing/transport/http"
	"github.com/speakeasy-api/gram/server/internal/corpus/git"
	corpusgithttp "github.com/speakeasy-api/gram/server/internal/corpus/githttp"
	"github.com/stretchr/testify/require"
)

func seedRepo(t *testing.T, dir string) *git.Repo {
	t.Helper()
	repo, err := git.InitBareRepo(dir)
	require.NoError(t, err)

	_, err = repo.CommitFiles(map[string][]byte{
		"README.md":     []byte("# Test Repo"),
		"docs/guide.md": []byte("Guide content"),
	}, "initial commit")
	require.NoError(t, err)
	return repo
}

func TestSmartHTTPClone(t *testing.T) {
	t.Parallel()
	repoDir := t.TempDir()
	seedRepo(t, repoDir)

	handler := corpusgithttp.NewHandler(func(projectID string) (string, error) {
		return repoDir, nil
	})

	server := httptest.NewServer(http.StripPrefix("/test-project/corpus.git", handler))
	defer server.Close()

	cloneDir := t.TempDir()
	clonedRepo, err := gogit.PlainClone(cloneDir, &gogit.CloneOptions{
		URL: server.URL + "/test-project/corpus.git",
	})
	require.NoError(t, err)

	wt, err := clonedRepo.Worktree()
	require.NoError(t, err)

	f, err := wt.Filesystem.Open("README.md")
	require.NoError(t, err)
	content, err := io.ReadAll(f)
	require.NoError(t, err)
	require.Equal(t, "# Test Repo", string(content))

	f2, err := wt.Filesystem.Open("docs/guide.md")
	require.NoError(t, err)
	content2, err := io.ReadAll(f2)
	require.NoError(t, err)
	require.Equal(t, "Guide content", string(content2))
}

func TestSmartHTTPFetchAfterClone(t *testing.T) {
	t.Parallel()
	repoDir := t.TempDir()
	repo := seedRepo(t, repoDir)

	handler := corpusgithttp.NewHandler(func(projectID string) (string, error) {
		return repoDir, nil
	})

	server := httptest.NewServer(http.StripPrefix("/test-project/corpus.git", handler))
	defer server.Close()

	cloneDir := t.TempDir()
	clonedRepo, err := gogit.PlainClone(cloneDir, &gogit.CloneOptions{
		URL: server.URL + "/test-project/corpus.git",
	})
	require.NoError(t, err)

	_, err = repo.CommitFiles(map[string][]byte{
		"README.md": []byte("# Updated"),
	}, "second commit")
	require.NoError(t, err)

	err = clonedRepo.Fetch(&gogit.FetchOptions{
		RemoteName: "origin",
	})
	require.NoError(t, err)

	logIter, err := clonedRepo.Log(&gogit.LogOptions{
		From: func() plumbing.Hash {
			ref, _ := clonedRepo.Reference(plumbing.NewRemoteReferenceName("origin", "master"), true)
			return ref.Hash()
		}(),
	})
	require.NoError(t, err)

	count := 0
	_ = logIter.ForEach(func(_ *object.Commit) error {
		count++
		return nil
	})
	require.Equal(t, 2, count)
}

func TestSmartHTTPAuthRequired(t *testing.T) {
	t.Parallel()
	repoDir := t.TempDir()
	seedRepo(t, repoDir)

	handler := corpusgithttp.NewHandler(
		func(projectID string) (string, error) {
			return repoDir, nil
		},
		corpusgithttp.WithAuth(func(r *http.Request) error {
			if r.Header.Get("Authorization") == "" {
				return corpusgithttp.ErrUnauthorized
			}
			return nil
		}),
	)

	server := httptest.NewServer(http.StripPrefix("/test-project/corpus.git", handler))
	defer server.Close()

	// Clone without auth should fail
	cloneDir := t.TempDir()
	_, err := gogit.PlainClone(cloneDir, &gogit.CloneOptions{
		URL: server.URL + "/test-project/corpus.git",
	})
	require.Error(t, err)

	// Verify it's an auth error (401)
	var httpErr *githttp.Err
	if errors.As(err, &httpErr) {
		require.Equal(t, 401, httpErr.Status)
	}

	// Clone with auth should succeed
	cloneDir2 := t.TempDir()
	_, err = gogit.PlainClone(cloneDir2, &gogit.CloneOptions{
		URL: server.URL + "/test-project/corpus.git",
		Auth: &githttp.BasicAuth{
			Username: "token",
			Password: "valid-token",
		},
	})
	require.NoError(t, err)
}

func TestSmartHTTPPush(t *testing.T) {
	t.Parallel()

	repoDir := t.TempDir()
	seedRepo(t, repoDir)

	handler := corpusgithttp.NewHandler(func(projectID string) (string, error) {
		return repoDir, nil
	})

	server := httptest.NewServer(http.StripPrefix("/test-project/corpus.git", handler))
	defer server.Close()

	// Clone the repo via HTTP.
	cloneDir := t.TempDir()
	clonedRepo, err := gogit.PlainClone(cloneDir, &gogit.CloneOptions{
		URL: server.URL + "/test-project/corpus.git",
	})
	require.NoError(t, err)

	// Create a new file in the worktree, stage it, and commit.
	err = os.WriteFile(filepath.Join(cloneDir, "new-file.txt"), []byte("pushed content"), 0o644)
	require.NoError(t, err)

	wt, err := clonedRepo.Worktree()
	require.NoError(t, err)

	_, err = wt.Add("new-file.txt")
	require.NoError(t, err)

	_, err = wt.Commit("add new file via push", &gogit.CommitOptions{
		Author: &object.Signature{
			Name:  "Test",
			Email: "test@example.com",
			When:  time.Now(),
		},
	})
	require.NoError(t, err)

	// Push back to the server.
	err = clonedRepo.Push(&gogit.PushOptions{
		RemoteName: "origin",
	})
	require.NoError(t, err)

	// Open the bare repo directly and verify the pushed file exists at HEAD.
	bareRepo, err := git.OpenRepo(repoDir)
	require.NoError(t, err)

	content, err := bareRepo.ReadBlob("HEAD", "new-file.txt")
	require.NoError(t, err)
	require.Equal(t, "pushed content", string(content))

	// Verify the commit history has 2 commits (initial + push).
	entries, err := bareRepo.ReadTree("HEAD")
	require.NoError(t, err)
	require.Len(t, entries, 3) // README.md, docs/guide.md, new-file.txt
}
