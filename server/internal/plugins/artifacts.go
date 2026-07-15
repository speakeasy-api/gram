package plugins

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"strings"
	"sync"
	"time"

	"golang.org/x/sync/singleflight"

	"github.com/speakeasy-api/gram/server/internal/attr"
	"github.com/speakeasy-api/gram/server/internal/guardian"
	"github.com/speakeasy-api/gram/server/internal/o11y"
)

// HooksReleaseRoutePrefix is the top-level path namespace reserved for hooks
// binary artifacts. Generated bootstrap scripts download from this prefix on
// the org's Gram server domain, so customers only ever need to allow egress to
// the one domain their hooks already ingest to — GitHub never has to be
// reachable from the machines running hooks. Sandboxed harnesses (Claude
// Cowork) gate GitHub per-session with no way to grant artifact access, which
// is what makes serving from our own domain a hard requirement rather than a
// convenience.
const HooksReleaseRoutePrefix = "/hooks/releases/"

// hooksArtifactIndex enumerates every artifact set this server is willing to
// serve, keyed by release version. When bumping hooksBinaryVersion, KEEP the
// prior versions here: bootstrap scripts already installed on user machines
// and published repos that have not regenerated yet keep requesting the old
// version's path, and dropping it bricks their cold installs.
var hooksArtifactIndex = map[string]map[string]hooksBinaryTarget{
	hooksBinaryVersion: hooksBinaryTargets,
}

// Release archives are ~15MB; anything past this limit means the upstream
// response is not the artifact we pinned.
const maxHooksArtifactBytes = 128 << 20

// hooksArtifactFetchTimeout caps the server-side upstream fetch. It must sit
// well under the bootstrap scripts' 45s download budget: a cold request pays
// upstream fetch + verification before the first byte streams out, so the gap
// between the two budgets is the client's headroom to actually receive the
// archive. A fetch slower than this fails the request quickly instead of
// burning the client's whole window — the fetch that does complete is cached,
// so the next hook invocation installs from warm bytes.
const hooksArtifactFetchTimeout = 30 * time.Second

// HooksArtifactServer serves the pinned speakeasy-hooks release archives from
// the Gram server domain. It is a verifying proxy in front of the GitHub
// release: only the exact version × target set in hooksArtifactIndex is
// served, every archive is checked against its pinned SHA-256 before the
// first byte goes out, and verified archives are cached in memory (bounded by
// the index: currently six ~15MB targets per version). The host therefore
// needs no trust from clients — bootstrap scripts re-verify the same pinned
// checksum on their side.
type HooksArtifactServer struct {
	logger     *slog.Logger
	httpClient *guardian.HTTPClient
	index      map[string]map[string]hooksBinaryTarget

	group singleflight.Group
	mu    sync.RWMutex
	cache map[string][]byte
}

func NewHooksArtifactServer(logger *slog.Logger, httpClient *guardian.HTTPClient) *HooksArtifactServer {
	return &HooksArtifactServer{
		logger:     logger,
		httpClient: httpClient,
		index:      hooksArtifactIndex,
		group:      singleflight.Group{},
		mu:         sync.RWMutex{},
		cache:      make(map[string][]byte),
	}
}

func (s *HooksArtifactServer) Routes() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET "+HooksReleaseRoutePrefix+"{version}/{asset}", s.handleArtifact)
	return mux
}

// IsHooksReleaseRoute reports whether the request targets the hooks artifact
// namespace. The main server mux uses this to short-circuit the Goa muxer.
func (s *HooksArtifactServer) IsHooksReleaseRoute(r *http.Request) bool {
	return strings.HasPrefix(r.URL.Path, HooksReleaseRoutePrefix)
}

// artifactTarget maps an asset filename ("speakeasy-hooks_linux_arm64.zip")
// back to its target key ("linux-arm64"). Returns "" for anything that does
// not match the exact release naming scheme.
func artifactTarget(asset string) string {
	name, ok := strings.CutPrefix(asset, "speakeasy-hooks_")
	if !ok {
		return ""
	}
	name, ok = strings.CutSuffix(name, ".zip")
	if !ok {
		return ""
	}
	osName, arch, ok := strings.Cut(name, "_")
	if !ok || osName == "" || arch == "" || strings.Contains(arch, "_") {
		return ""
	}
	return osName + "-" + arch
}

func (s *HooksArtifactServer) handleArtifact(w http.ResponseWriter, r *http.Request) {
	version := r.PathValue("version")
	target := artifactTarget(r.PathValue("asset"))
	upstream, ok := s.index[version][target]
	if !ok || target == "" {
		http.NotFound(w, r)
		return
	}

	data, err := s.artifact(r.Context(), version, target, upstream)
	if err != nil {
		s.logger.ErrorContext(r.Context(), "fetch hooks artifact",
			attr.SlogError(err),
			attr.SlogURLFull(upstream.URL),
		)
		http.Error(w, "upstream unavailable", http.StatusBadGateway)
		return
	}

	// The URL pins version and target and the bytes are checksum-verified, so
	// the response is immutable by construction.
	w.Header().Set("Content-Type", "application/zip")
	w.Header().Set("Cache-Control", "public, max-age=31536000, immutable")
	w.Header().Set("ETag", `"`+upstream.SHA256+`"`)
	http.ServeContent(w, r, r.PathValue("asset"), time.Time{}, bytes.NewReader(data))
}

// artifact returns the verified archive bytes for one pinned target, fetching
// and caching them on first use. Concurrent cold requests for the same target
// share a single upstream fetch.
func (s *HooksArtifactServer) artifact(ctx context.Context, version, target string, upstream hooksBinaryTarget) ([]byte, error) {
	key := version + "/" + target

	s.mu.RLock()
	data, ok := s.cache[key]
	s.mu.RUnlock()
	if ok {
		return data, nil
	}

	out, err, _ := s.group.Do(key, func() (any, error) {
		s.mu.RLock()
		data, ok := s.cache[key]
		s.mu.RUnlock()
		if ok {
			return data, nil
		}

		// The fetch outlives any single requester: a canceled download would
		// waste the work for every waiter sharing this flight, so detach from
		// the request context and rely on the timeout alone.
		fetchCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), hooksArtifactFetchTimeout)
		defer cancel()

		data, err := s.fetchVerified(fetchCtx, upstream)
		if err != nil {
			return nil, err
		}

		s.mu.Lock()
		s.cache[key] = data
		s.mu.Unlock()
		return data, nil
	})
	if err != nil {
		return nil, err
	}

	data, ok = out.([]byte)
	if !ok {
		return nil, fmt.Errorf("unexpected artifact cache entry type %T", out)
	}
	return data, nil
}

// fetchVerified downloads the upstream release archive and verifies it against
// the pinned SHA-256. Bytes are never returned (or cached) on a mismatch.
func (s *HooksArtifactServer) fetchVerified(ctx context.Context, upstream hooksBinaryTarget) ([]byte, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, upstream.URL, nil)
	if err != nil {
		return nil, fmt.Errorf("build hooks artifact request: %w", err)
	}

	resp, err := s.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("fetch hooks artifact: %w", err)
	}
	defer o11y.NoLogDefer(func() error { return resp.Body.Close() })

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("fetch hooks artifact: upstream status %d", resp.StatusCode)
	}

	data, err := io.ReadAll(io.LimitReader(resp.Body, maxHooksArtifactBytes+1))
	if err != nil {
		return nil, fmt.Errorf("read hooks artifact: %w", err)
	}
	if len(data) > maxHooksArtifactBytes {
		return nil, fmt.Errorf("hooks artifact exceeds %d bytes", maxHooksArtifactBytes)
	}

	sum := sha256.Sum256(data)
	if actual := hex.EncodeToString(sum[:]); actual != upstream.SHA256 {
		return nil, fmt.Errorf("hooks artifact checksum mismatch: got %s, pinned %s", actual, upstream.SHA256)
	}
	return data, nil
}
