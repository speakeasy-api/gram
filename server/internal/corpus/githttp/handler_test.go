package githttp_test

import (
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	gogit "github.com/go-git/go-git/v5"
	"github.com/go-git/go-git/v5/plumbing/object"
	githttp "github.com/go-git/go-git/v5/plumbing/transport/http"
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
	repoDir := t.TempDir()
	seedRepo(t, repoDir)

	handler := corpusgithttp.NewHandler(func(projectID string) (string, error) {
		return repoDir, nil
	})

	server := httptest.NewServer(http.StripPrefix("/test-project/corpus.git", handler))
	defer server.Close()

	cloneDir := t.TempDir()
	clonedRepo, err := gogit.PlainClone(cloneDir, false, &gogit.CloneOptions{
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

func TestSmartHTTPShallowClone(t *testing.T) {
	repoDir := t.TempDir()
	repo := seedRepo(t, repoDir)

	_, err := repo.CommitFiles(map[string][]byte{
		"README.md": []byte("# Updated"),
	}, "second commit")
	require.NoError(t, err)

	_, err = repo.CommitFiles(map[string][]byte{
		"README.md": []byte("# Updated Again"),
	}, "third commit")
	require.NoError(t, err)

	handler := corpusgithttp.NewHandler(func(projectID string) (string, error) {
		return repoDir, nil
	})

	server := httptest.NewServer(http.StripPrefix("/test-project/corpus.git", handler))
	defer server.Close()

	cloneDir := t.TempDir()
	clonedRepo, err := gogit.PlainClone(cloneDir, false, &gogit.CloneOptions{
		URL:   server.URL + "/test-project/corpus.git",
		Depth: 1,
	})
	require.NoError(t, err)

	logIter, err := clonedRepo.Log(&gogit.LogOptions{})
	require.NoError(t, err)

	count := 0
	_ = logIter.ForEach(func(_ *object.Commit) error {
		count++
		return nil
	})
	require.Equal(t, 1, count)
}

func TestSmartHTTPAuthRequired(t *testing.T) {
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
	_, err := gogit.PlainClone(cloneDir, false, &gogit.CloneOptions{
		URL: server.URL + "/test-project/corpus.git",
	})
	require.Error(t, err)

	// Verify it's an auth error (401)
	var httpErr *githttp.Err
	if errors.As(err, &httpErr) {
		require.Equal(t, 401, httpErr.StatusCode)
	}

	// Clone with auth should succeed
	cloneDir2 := t.TempDir()
	_, err = gogit.PlainClone(cloneDir2, false, &gogit.CloneOptions{
		URL: server.URL + "/test-project/corpus.git",
		Auth: &githttp.BasicAuth{
			Username: "token",
			Password: "valid-token",
		},
	})
	require.NoError(t, err)
}
