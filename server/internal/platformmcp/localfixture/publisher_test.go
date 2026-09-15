package localfixture

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestInMemoryGitHubPublisherCreatesEmptyMainBranch(t *testing.T) {
	t.Parallel()

	publisher := NewInMemoryGitHubPublisher()
	require.NoError(t, publisher.CreateRepo(t.Context(), 1, "local", "fixture", true))

	files, err := publisher.GetRepoFiles(t.Context(), 1, "local", "fixture", "main")
	require.NoError(t, err)
	require.Empty(t, files)
}

func TestInMemoryGitHubPublisherStoresDefensiveCopies(t *testing.T) {
	t.Parallel()

	publisher := NewInMemoryGitHubPublisher()
	files := map[string][]byte{"platform-mcp/.mcp.json": []byte(`{"mcpServers":{}}`)}
	_, err := publisher.PushFiles(t.Context(), 1, "local", "fixture", "main", "publish", files)
	require.NoError(t, err)

	files["platform-mcp/.mcp.json"][0] = 'x'
	stored, err := publisher.GetRepoFiles(t.Context(), 1, "local", "fixture", "main")
	require.NoError(t, err)
	require.JSONEq(t, `{"mcpServers":{}}`, string(stored["platform-mcp/.mcp.json"]))

	stored["platform-mcp/.mcp.json"][0] = 'x'
	content, err := publisher.GetFileContent(t.Context(), 1, "local", "fixture", "main", "platform-mcp/.mcp.json")
	require.NoError(t, err)
	require.JSONEq(t, `{"mcpServers":{}}`, string(content))
}

func TestPersistentGitHubPublisherSharesRepositoriesAcrossInstances(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	writer, err := NewPersistentGitHubPublisher(root)
	require.NoError(t, err)
	reader, err := NewPersistentGitHubPublisher(root)
	require.NoError(t, err)

	files := map[string][]byte{"platform-mcp/.mcp.json": []byte(`{"mcpServers":{}}`)}
	_, err = writer.PushFiles(t.Context(), 1, "local", "fixture", "main", "publish", files)
	require.NoError(t, err)
	require.NoError(t, writer.AddCollaborator(t.Context(), 1, "local", "fixture", "admin", "push"))

	stored, err := reader.MainBranchFiles(t.Context(), "local", "fixture")
	require.NoError(t, err)
	require.JSONEq(t, `{"mcpServers":{}}`, string(stored["platform-mcp/.mcp.json"]))
	hasCollaborator, err := reader.HasDirectCollaborator(t.Context(), 1, "local", "fixture")
	require.NoError(t, err)
	require.True(t, hasCollaborator)

	entries, err := os.ReadDir(root)
	require.NoError(t, err)
	require.Len(t, entries, 2)
	for _, entry := range entries {
		info, err := entry.Info()
		require.NoError(t, err)
		require.Equal(t, os.FileMode(0o600), info.Mode().Perm())
	}
}

func TestPersistentGitHubPublisherCreateRepoPreservesPublishedFiles(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	publisher, err := NewPersistentGitHubPublisher(root)
	require.NoError(t, err)
	_, err = publisher.PushFiles(t.Context(), 1, "local", "fixture", "main", "publish", map[string][]byte{
		"platform-mcp/.mcp.json": []byte(`{"mcpServers":{}}`),
	})
	require.NoError(t, err)

	require.NoError(t, publisher.CreateRepo(t.Context(), 1, "local", "fixture", true))
	stored, err := publisher.MainBranchFiles(t.Context(), "local", "fixture")
	require.NoError(t, err)
	require.Contains(t, stored, "platform-mcp/.mcp.json")
}

func TestPersistentGitHubPublisherRemovesOnlyStaleTemporaryFiles(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	stale := filepath.Join(root, ".publisher-stale.tmp")
	recent := filepath.Join(root, ".publisher-recent.tmp")
	require.NoError(t, os.WriteFile(stale, []byte("stale"), 0o600))
	require.NoError(t, os.WriteFile(recent, []byte("recent"), 0o600))
	staleTime := time.Now().Add(-2 * time.Hour)
	require.NoError(t, os.Chtimes(stale, staleTime, staleTime))

	_, err := NewPersistentGitHubPublisher(root)
	require.NoError(t, err)
	require.NoFileExists(t, stale)
	require.FileExists(t, recent)
}
