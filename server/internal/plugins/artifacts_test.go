package plugins

import (
	"crypto/sha256"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"golang.org/x/sync/singleflight"

	"github.com/speakeasy-api/gram/server/internal/testenv"
)

// newTestHooksArtifactServer builds an artifact server whose pinned index
// points at a local upstream. Returns the server and the upstream hit counter.
func newTestHooksArtifactServer(t *testing.T, archive []byte, sha string) (*HooksArtifactServer, *atomic.Int64) {
	t.Helper()

	var hits atomic.Int64
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		hits.Add(1)
		_, _ = w.Write(archive)
	}))
	t.Cleanup(upstream.Close)

	return &HooksArtifactServer{
		logger:     testenv.NewLogger(t),
		httpClient: upstream.Client(),
		index: map[string]map[string]hooksBinaryTarget{
			"0.1.0": {
				"linux-arm64": {URL: upstream.URL + "/hooks.zip", SHA256: sha},
			},
		},
		group:    singleflight.Group{},
		mu:       sync.RWMutex{},
		cache:    map[string][]byte{},
		failedAt: map[string]time.Time{},
	}, &hits
}

func TestHooksArtifactServerServesVerifiedArchiveAndCaches(t *testing.T) {
	t.Parallel()
	archive := []byte("fake-zip-bytes")
	sum := sha256.Sum256(archive)
	svc, hits := newTestHooksArtifactServer(t, archive, fmt.Sprintf("%x", sum))
	front := httptest.NewServer(svc.Routes())
	t.Cleanup(front.Close)

	for range 2 {
		resp, err := front.Client().Get(front.URL + "/hooks/releases/0.1.0/speakeasy-hooks_linux_arm64.zip")
		require.NoError(t, err)
		body, err := io.ReadAll(resp.Body)
		require.NoError(t, resp.Body.Close())
		require.NoError(t, err)
		require.Equal(t, http.StatusOK, resp.StatusCode)
		require.Equal(t, archive, body)
		require.Equal(t, "application/zip", resp.Header.Get("Content-Type"))
		require.Contains(t, resp.Header.Get("Cache-Control"), "immutable")
	}
	require.Equal(t, int64(1), hits.Load(), "verified archive must be served from cache after the first fetch")
}

func TestHooksArtifactServerRejectsUpstreamChecksumMismatch(t *testing.T) {
	t.Parallel()
	svc, hits := newTestHooksArtifactServer(t, []byte("tampered-bytes"), strings.Repeat("0", 64))
	front := httptest.NewServer(svc.Routes())
	t.Cleanup(front.Close)

	for range 2 {
		resp, err := front.Client().Get(front.URL + "/hooks/releases/0.1.0/speakeasy-hooks_linux_arm64.zip")
		require.NoError(t, err)
		body, err := io.ReadAll(resp.Body)
		require.NoError(t, resp.Body.Close())
		require.NoError(t, err)
		require.Equal(t, http.StatusBadGateway, resp.StatusCode)
		require.NotContains(t, string(body), "tampered-bytes", "unverified bytes must never reach the client")
	}
	require.Equal(t, int64(1), hits.Load(), "failed fetches must be negatively cached, not retried per request")
}

func TestHooksArtifactServerUnknownArtifactsAre404(t *testing.T) {
	t.Parallel()
	archive := []byte("fake-zip-bytes")
	sum := sha256.Sum256(archive)
	svc, hits := newTestHooksArtifactServer(t, archive, fmt.Sprintf("%x", sum))
	front := httptest.NewServer(svc.Routes())
	t.Cleanup(front.Close)

	for _, path := range []string{
		"/hooks/releases/9.9.9/speakeasy-hooks_linux_arm64.zip", // unpinned version
		"/hooks/releases/0.1.0/speakeasy-hooks_plan9_arm64.zip", // unpinned target
		"/hooks/releases/0.1.0/other-binary_linux_arm64.zip",    // foreign asset name
		"/hooks/releases/0.1.0/speakeasy-hooks_linux_arm64.tar", // wrong extension
		"/hooks/releases/0.1.0",
	} {
		resp, err := front.Client().Get(front.URL + path)
		require.NoError(t, err)
		require.NoError(t, resp.Body.Close())
		require.Equal(t, http.StatusNotFound, resp.StatusCode, path)
	}
	require.Equal(t, int64(0), hits.Load(), "requests outside the pinned set must never reach upstream")
}

func TestHooksArtifactIndexServesEveryBootstrapURL(t *testing.T) {
	t.Parallel()
	// Every URL a generated bootstrap script can contain must resolve within
	// the served index — a script pointing at an unserved artifact bricks cold
	// installs.
	served := hooksServedTargets("https://app.getgram.ai")
	require.Len(t, served, 6)
	for target, asset := range served {
		prefix := "https://app.getgram.ai/hooks/releases/"
		require.True(t, strings.HasPrefix(asset.URL, prefix), asset.URL)
		rest := strings.TrimPrefix(asset.URL, prefix)
		version, assetName, ok := strings.Cut(rest, "/")
		require.True(t, ok, asset.URL)
		require.Equal(t, target, artifactTarget(assetName))
		upstream, ok := hooksArtifactIndex[version][artifactTarget(assetName)]
		require.True(t, ok, "bootstrap URL %s is not served by hooksArtifactIndex", asset.URL)
		require.Equal(t, asset.SHA256, upstream.SHA256)
	}
}
