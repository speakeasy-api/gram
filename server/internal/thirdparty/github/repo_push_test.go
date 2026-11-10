package github

import (
	"encoding/base64"
	"encoding/json"
	"net/http"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

// A plugin icon is binary, and the Trees API only carries UTF-8 text inline,
// so PushFiles must upload it as a base64 blob and reference it by SHA.
func TestPushFiles_UploadsBinaryContentAsBlob(t *testing.T) {
	t.Parallel()

	binary := []byte{0x89, 'P', 'N', 'G', 0x0d, 0x0a, 0x1a, 0x0a, 0xff, 0x00}
	require.False(t, json.Valid(binary))

	var (
		mu        sync.Mutex
		blobBody  map[string]string
		treeEntry = map[string]map[string]string{}
	)
	transport := roundTripFunc(func(req *http.Request) (*http.Response, error) {
		mu.Lock()
		defer mu.Unlock()
		switch {
		case req.Method == http.MethodGet && strings.HasSuffix(req.URL.Path, "/git/ref/heads/main"):
			return jsonResponse(t, http.StatusOK, map[string]any{"object": map[string]string{"sha": "head-sha"}}), nil
		case req.Method == http.MethodPost && strings.HasSuffix(req.URL.Path, "/git/blobs"):
			require.NoError(t, json.NewDecoder(req.Body).Decode(&blobBody))
			return jsonResponse(t, http.StatusCreated, map[string]string{"sha": "icon-blob-sha"}), nil
		case req.Method == http.MethodPost && strings.HasSuffix(req.URL.Path, "/git/trees"):
			var payload struct {
				Tree []map[string]string `json:"tree"`
			}
			require.NoError(t, json.NewDecoder(req.Body).Decode(&payload))
			for _, entry := range payload.Tree {
				treeEntry[entry["path"]] = entry
			}
			return jsonResponse(t, http.StatusCreated, map[string]string{"sha": "tree-sha"}), nil
		case req.Method == http.MethodPost && strings.HasSuffix(req.URL.Path, "/git/commits"):
			return jsonResponse(t, http.StatusCreated, map[string]string{"sha": "commit-sha"}), nil
		case req.Method == http.MethodPatch && strings.HasSuffix(req.URL.Path, "/git/refs/heads/main"):
			return jsonResponse(t, http.StatusOK, map[string]string{}), nil
		default:
			return jsonResponse(t, http.StatusNotFound, map[string]string{"message": req.Method + " " + req.URL.Path}), nil
		}
	})
	client := &Client{
		appID:      1,
		privateKey: nil,
		httpClient: &http.Client{Transport: transport},
		mu:         sync.Mutex{},
		tokens:     map[int64]cachedToken{1: {token: "test-token", expiresAt: time.Now().Add(time.Hour)}},
	}

	sha, err := client.PushFiles(t.Context(), 1, "owner", "repo", "main", "publish", map[string][]byte{
		"plugin/.claude-plugin/icon.png": binary,
		"plugin/README.md":               []byte("# Plugin\n"),
	})
	require.NoError(t, err)
	require.Equal(t, "commit-sha", sha)

	require.Equal(t, "base64", blobBody["encoding"])
	decoded, err := base64.StdEncoding.DecodeString(blobBody["content"])
	require.NoError(t, err)
	require.Equal(t, binary, decoded)

	icon := treeEntry["plugin/.claude-plugin/icon.png"]
	require.Equal(t, "icon-blob-sha", icon["sha"])
	require.NotContains(t, icon, "content")

	readme := treeEntry["plugin/README.md"]
	require.Equal(t, "# Plugin\n", readme["content"])
	require.NotContains(t, readme, "sha")
}
