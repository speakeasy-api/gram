package localfixture

import (
	"testing"

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
