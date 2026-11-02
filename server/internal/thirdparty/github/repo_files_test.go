package github

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(req *http.Request) (*http.Response, error) { return f(req) }

func jsonResponse(t *testing.T, status int, body any) *http.Response {
	t.Helper()
	raw, err := json.Marshal(body)
	require.NoError(t, err)
	return &http.Response{
		StatusCode: status,
		Header:     http.Header{"Content-Type": []string{"application/json"}},
		Body:       io.NopCloser(strings.NewReader(string(raw))),
	}
}

type fakeRepo struct {
	client    *Client
	requested sync.Map
	peak      atomic.Int32
	// failSHA, when set, makes that blob's fetch return a server error.
	failSHA string
}

// newFakeRepo serves a git tree of n blob entries ("dir-<i%2>/file-<i>") and
// their blobs, recording the blobs requested and the peak number in flight.
// With holdUntilFull, blob responses wait until repoFileFetchConcurrency are in
// flight at once (or a timeout passes), so peak shows how far fetches overlap.
func newFakeRepo(t *testing.T, n int, holdUntilFull bool) *fakeRepo {
	t.Helper()

	type entry struct {
		Path string `json:"path"`
		Type string `json:"type"`
		SHA  string `json:"sha"`
	}
	entries := []entry{{Path: "dir-0", Type: "tree", SHA: "tree-sha"}}
	for i := range n {
		entries = append(entries, entry{Path: fmt.Sprintf("dir-%d/file-%d", i%2, i), Type: "blob", SHA: fmt.Sprintf("sha-%d", i)})
	}

	repo := &fakeRepo{}
	var inFlight atomic.Int32
	full := make(chan struct{})
	var closeFull sync.Once
	transport := roundTripFunc(func(req *http.Request) (*http.Response, error) {
		switch {
		case strings.HasSuffix(req.URL.Path, "/git/trees/main"):
			return jsonResponse(t, http.StatusOK, map[string]any{"tree": entries, "truncated": false}), nil
		case strings.Contains(req.URL.Path, "/git/blobs/"):
			cur := inFlight.Add(1)
			defer inFlight.Add(-1)
			for {
				prev := repo.peak.Load()
				if cur <= prev || repo.peak.CompareAndSwap(prev, cur) {
					break
				}
			}
			if cur >= repoFileFetchConcurrency {
				closeFull.Do(func() { close(full) })
			}
			if holdUntilFull {
				select {
				case <-full:
				case <-time.After(5 * time.Second):
				}
			}
			sha := req.URL.Path[strings.LastIndex(req.URL.Path, "/")+1:]
			repo.requested.Store(sha, true)
			if sha == repo.failSHA {
				return jsonResponse(t, http.StatusInternalServerError, map[string]string{"message": "boom"}), nil
			}
			return jsonResponse(t, http.StatusOK, map[string]string{
				"content":  base64.StdEncoding.EncodeToString([]byte("content-of-" + sha)),
				"encoding": "base64",
			}), nil
		default:
			return jsonResponse(t, http.StatusNotFound, map[string]string{}), nil
		}
	})

	repo.client = &Client{
		appID:      1,
		privateKey: nil,
		httpClient: &http.Client{Transport: transport},
		mu:         sync.Mutex{},
		tokens:     map[int64]cachedToken{1: {token: "test-token", expiresAt: time.Now().Add(time.Hour)}},
	}
	return repo
}

func (r *fakeRepo) requestedSHAs(t *testing.T) []string {
	t.Helper()
	var shas []string
	r.requested.Range(func(key, _ any) bool {
		sha, ok := key.(string)
		require.True(t, ok)
		shas = append(shas, sha)
		return true
	})
	return shas
}

func TestGetRepoFiles_FetchesBlobsConcurrently(t *testing.T) {
	t.Parallel()

	const n = 5 * repoFileFetchConcurrency
	repo := newFakeRepo(t, n, true)

	files, err := repo.client.GetRepoFiles(t.Context(), 1, "owner", "repo", "main", nil)
	require.NoError(t, err)
	require.Len(t, files, n)
	for i := range n {
		require.Equal(t, fmt.Sprintf("content-of-sha-%d", i), string(files[fmt.Sprintf("dir-%d/file-%d", i%2, i)]))
	}

	require.Len(t, repo.requestedSHAs(t), n)
	require.Equal(t, int32(repoFileFetchConcurrency), repo.peak.Load(), "blobs must be fetched in parallel up to, and never beyond, the concurrency bound")
}

func TestGetRepoFiles_IncludeSkipsUnwantedBlobs(t *testing.T) {
	t.Parallel()

	repo := newFakeRepo(t, 10, false)

	files, err := repo.client.GetRepoFiles(t.Context(), 1, "owner", "repo", "main", func(path string) bool {
		return strings.HasPrefix(path, "dir-1/")
	})
	require.NoError(t, err)
	require.Len(t, files, 5)
	for path := range files {
		require.True(t, strings.HasPrefix(path, "dir-1/"), path)
	}

	requested := repo.requestedSHAs(t)
	require.Len(t, requested, 5)
	for _, sha := range requested {
		var i int
		_, err := fmt.Sscanf(sha, "sha-%d", &i)
		require.NoError(t, err)
		require.Equal(t, 1, i%2, "an excluded blob was downloaded: %s", sha)
	}
}

func TestGetRepoFiles_BlobErrorFailsWholeFetch(t *testing.T) {
	t.Parallel()

	repo := newFakeRepo(t, 3*repoFileFetchConcurrency, false)
	repo.failSHA = "sha-5"

	files, err := repo.client.GetRepoFiles(t.Context(), 1, "owner", "repo", "main", nil)
	require.ErrorContains(t, err, "get blob dir-1/file-5")
	require.ErrorContains(t, err, "status 500")
	require.Nil(t, files, "a failed fetch must not return a partial file set")
}
