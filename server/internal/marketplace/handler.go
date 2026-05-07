package marketplace

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"regexp"
	"strings"

	"github.com/speakeasy-api/gram/server/internal/attr"
	"github.com/speakeasy-api/gram/server/internal/guardian"
	"github.com/speakeasy-api/gram/server/internal/o11y"
)

// RoutePrefix is the single top-level path segment reserved by the marketplace
// proxy. All proxy routes (manifest + git Smart HTTP) live under this prefix
// so the main mux only carves out one namespace, and so the dispatch logic
// stays here rather than leaking into the server bootstrap.
const RoutePrefix = "/marketplace/"

// publishedManifestRef pins manifest fetches to the same branch the publish
// flow writes to (see plugins/impl.go). If the publish flow ever stops
// hardcoding "main", this needs to track that.
const publishedManifestRef = "main"

// Server hosts the URL-based marketplace.json endpoint and the git Smart HTTP
// proxy for plugin sources. Both stream directly from GitHub against an
// installation token minted by the resolver — no local mirror state.
type Server struct {
	resolver      Resolver
	httpClient    *guardian.HTTPClient
	publicBaseURL string
	logger        *slog.Logger
}

func NewServer(
	resolver Resolver,
	httpClient *guardian.HTTPClient,
	publicBaseURL string,
	logger *slog.Logger,
) *Server {
	return &Server{
		resolver:      resolver,
		httpClient:    httpClient,
		publicBaseURL: publicBaseURL,
		logger:        logger,
	}
}

func (s *Server) Routes() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET "+RoutePrefix+"m/{token}/marketplace.json", s.handleManifest)
	// {slug} captures "<token>.git"; the handler strips the suffix. Go 1.22's
	// ServeMux disallows mixing literals with wildcards inside one segment.
	mux.HandleFunc("GET "+RoutePrefix+"p/{slug}/info/refs", s.handleInfoRefs)
	mux.HandleFunc("POST "+RoutePrefix+"p/{slug}/git-upload-pack", s.handleUploadPack)
	mux.HandleFunc("GET "+RoutePrefix+"healthz", func(w http.ResponseWriter, _ *http.Request) {
		_, _ = io.WriteString(w, "ok")
	})
	return mux
}

// IsMarketplaceRoute reports whether the request targets a path owned by the
// marketplace proxy. The main server mux uses this to short-circuit the Goa
// muxer; centralizing the check here keeps the prefix definition in one
// place.
func (s *Server) IsMarketplaceRoute(r *http.Request) bool {
	return strings.HasPrefix(r.URL.Path, RoutePrefix)
}

// validTokenPattern matches the shape of a marketplace_token minted by
// generateMarketplaceToken — 32 random bytes encoded as base64url-without-
// padding, which lands at exactly 43 characters from the [A-Za-z0-9_-] set.
// Rejecting malformed tokens at the handler boundary keeps the resolver's DB
// lookup off the path for anyone hammering the proxy with random URLs.
var validTokenPattern = regexp.MustCompile(`^[A-Za-z0-9_-]{43}$`)

// isValidToken returns true when s could plausibly be a token we minted. The
// caller is still responsible for the actual existence check via the
// resolver; this is a cheap pre-filter.
func isValidToken(s string) bool {
	return validTokenPattern.MatchString(s)
}

// tokenFromSlug extracts the URL token from a "<token>.git" path segment and
// returns "" if the segment doesn't end in .git or the extracted token isn't
// well-formed. Caller treats "" as 404 without ever reaching the resolver.
func tokenFromSlug(slug string) string {
	const suffix = ".git"
	if len(slug) <= len(suffix) || slug[len(slug)-len(suffix):] != suffix {
		return ""
	}
	token := slug[:len(slug)-len(suffix)]
	if !isValidToken(token) {
		return ""
	}
	return token
}

// handleManifest fetches the upstream's published .claude-plugin/marketplace.json
// via the GitHub Contents API and rewrites each plugin's source to an absolute
// git URL on this proxy.
func (s *Server) handleManifest(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	token := r.PathValue("token")
	if !isValidToken(token) {
		http.NotFound(w, r)
		return
	}

	up, err := s.resolver.Resolve(ctx, token)
	if err != nil {
		s.errorResponse(w, r, err)
		return
	}

	raw, err := s.fetchPublishedManifest(ctx, up)
	if err != nil {
		var nf *upstreamNotFoundError
		if errors.As(err, &nf) {
			http.Error(w, "marketplace.json missing in upstream", http.StatusNotFound)
			return
		}
		s.logger.ErrorContext(ctx, "fetch published manifest", attr.SlogError(err))
		http.Error(w, "manifest unavailable", http.StatusBadGateway)
		return
	}

	rewritten, err := s.rewriteManifest(raw, token)
	if err != nil {
		s.logger.ErrorContext(ctx, "rewrite manifest", attr.SlogError(err))
		http.Error(w, "manifest rewrite failed", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-cache")
	_, _ = w.Write(rewritten)
}

// upstreamNotFoundError signals that the upstream returned 404 for the
// resource being fetched (file missing in the repo or repo not found).
type upstreamNotFoundError struct{ resource string }

func (e *upstreamNotFoundError) Error() string {
	return "upstream not found: " + e.resource
}

// fetchPublishedManifest returns the raw bytes of the marketplace.json file at
// the published ref via the GitHub Contents API. Using the raw media type
// avoids a base64 round-trip through the metadata response.
func (s *Server) fetchPublishedManifest(ctx context.Context, up Upstream) ([]byte, error) {
	apiURL := fmt.Sprintf(
		"https://api.github.com/repos/%s/%s/contents/.claude-plugin/marketplace.json?ref=%s",
		url.PathEscape(up.Owner), url.PathEscape(up.Repo), url.QueryEscape(publishedManifestRef),
	)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, apiURL, nil)
	if err != nil {
		return nil, fmt.Errorf("build manifest request: %w", err)
	}
	req.Header.Set("Accept", "application/vnd.github.raw+json")
	req.Header.Set("Authorization", "Bearer "+up.AccessToken)
	req.Header.Set("X-GitHub-Api-Version", "2022-11-28")

	resp, err := s.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("contents api request: %w", err)
	}
	defer o11y.NoLogDefer(func() error { return resp.Body.Close() })

	if resp.StatusCode == http.StatusNotFound {
		return nil, &upstreamNotFoundError{resource: ".claude-plugin/marketplace.json"}
	}
	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 512))
		return nil, fmt.Errorf("contents api: status %d: %s", resp.StatusCode, body)
	}

	// Bound the read so a misbehaving upstream can't blow our memory.
	const maxManifestBytes = 4 << 20 // 4 MiB
	body, err := io.ReadAll(io.LimitReader(resp.Body, maxManifestBytes+1))
	if err != nil {
		return nil, fmt.Errorf("read manifest body: %w", err)
	}
	if len(body) > maxManifestBytes {
		return nil, fmt.Errorf("manifest exceeds %d bytes", maxManifestBytes)
	}
	return body, nil
}

// rewriteManifest transforms a git-based manifest (string-typed plugin sources
// like "./foo") into a URL-based one (object-typed sources pointing at the
// proxy). Object sources are passed through unmodified.
func (s *Server) rewriteManifest(raw []byte, token string) ([]byte, error) {
	var m map[string]any
	if err := json.Unmarshal(raw, &m); err != nil {
		return nil, fmt.Errorf("unmarshal manifest: %w", err)
	}
	pluginsRaw, ok := m["plugins"].([]any)
	if !ok {
		return nil, errors.New("manifest missing plugins array")
	}
	gitURL := fmt.Sprintf("%s%sp/%s.git", s.publicBaseURL, RoutePrefix, token)
	for _, p := range pluginsRaw {
		entry, ok := p.(map[string]any)
		if !ok {
			continue
		}
		src, hasSource := entry["source"]
		if !hasSource {
			continue
		}
		if pathStr, isString := src.(string); isString {
			// Per the official Claude Code marketplace schema (schemastore.org/
			// claude-code-marketplace.json), `git-subdir` is the source type for
			// "clone this URL, the plugin lives at the given subdirectory". The
			// plain "git" source type doesn't exist; the four supported types
			// are npm | url | github | git-subdir.
			entry["source"] = map[string]any{
				"source": "git-subdir",
				"url":    gitURL,
				"path":   trimRelPrefix(pathStr),
			}
		}
	}
	out, err := json.MarshalIndent(m, "", "  ")
	if err != nil {
		return nil, fmt.Errorf("marshal rewritten manifest: %w", err)
	}
	return out, nil
}

func trimRelPrefix(s string) string {
	for _, prefix := range []string{"./", "/"} {
		if len(s) >= len(prefix) && s[:len(prefix)] == prefix {
			return s[len(prefix):]
		}
	}
	return s
}

// handleInfoRefs streams the Smart HTTP ref-advertisement straight from
// github.com.
func (s *Server) handleInfoRefs(w http.ResponseWriter, r *http.Request) {
	if r.URL.Query().Get("service") != "git-upload-pack" {
		http.Error(w, "only git-upload-pack supported", http.StatusForbidden)
		return
	}
	token := tokenFromSlug(r.PathValue("slug"))
	if token == "" {
		http.NotFound(w, r)
		return
	}

	up, err := s.resolver.Resolve(r.Context(), token)
	if err != nil {
		s.errorResponse(w, r, err)
		return
	}

	upstreamURL := fmt.Sprintf(
		"https://github.com/%s/%s.git/info/refs?service=git-upload-pack",
		url.PathEscape(up.Owner), url.PathEscape(up.Repo),
	)
	s.proxyToGitHub(w, r, http.MethodGet, upstreamURL, up.AccessToken, nil)
}

// handleUploadPack streams the packfile by piping the client's wants/haves to
// github.com and the resulting packfile back.
func (s *Server) handleUploadPack(w http.ResponseWriter, r *http.Request) {
	token := tokenFromSlug(r.PathValue("slug"))
	if token == "" {
		http.NotFound(w, r)
		return
	}

	up, err := s.resolver.Resolve(r.Context(), token)
	if err != nil {
		s.errorResponse(w, r, err)
		return
	}

	upstreamURL := fmt.Sprintf(
		"https://github.com/%s/%s.git/git-upload-pack",
		url.PathEscape(up.Owner), url.PathEscape(up.Repo),
	)
	s.proxyToGitHub(w, r, http.MethodPost, upstreamURL, up.AccessToken, r.Body)
}

// proxyToGitHub forwards a Smart HTTP request to github.com with installation-
// token basic auth and streams the response back. Body and headers are
// streamed without buffering so packfiles flow through in chunks.
func (s *Server) proxyToGitHub(
	w http.ResponseWriter,
	r *http.Request,
	method string,
	upstreamURL string,
	accessToken string,
	body io.Reader,
) {
	ctx := r.Context()

	upstreamReq, err := http.NewRequestWithContext(ctx, method, upstreamURL, body)
	if err != nil {
		s.logger.ErrorContext(ctx, "build upstream request", attr.SlogError(err))
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}
	upstreamReq.SetBasicAuth("x-access-token", accessToken)
	if ct := r.Header.Get("Content-Type"); ct != "" {
		upstreamReq.Header.Set("Content-Type", ct)
	}
	// Forward git-protocol negotiation hints. Git advertises protocol v2 via
	// this header; without it GitHub falls back to v0/v1 and clients may see
	// degraded behavior.
	if gp := r.Header.Get("Git-Protocol"); gp != "" {
		upstreamReq.Header.Set("Git-Protocol", gp)
	}

	resp, err := s.httpClient.Do(upstreamReq)
	if err != nil {
		s.logger.ErrorContext(ctx, "upstream request", attr.SlogError(err))
		http.Error(w, "upstream unavailable", http.StatusBadGateway)
		return
	}
	defer o11y.NoLogDefer(func() error { return resp.Body.Close() })

	// Mirror the response shape the git client expects. We pass through
	// Content-Type (carries the application/x-git-* media type), Cache-Control,
	// and Content-Encoding; everything else (cookies, GitHub-specific
	// headers) is dropped.
	for _, h := range []string{"Content-Type", "Cache-Control", "Content-Encoding"} {
		if v := resp.Header.Get(h); v != "" {
			w.Header().Set(h, v)
		}
	}
	w.WriteHeader(resp.StatusCode)
	if _, err := io.Copy(w, resp.Body); err != nil {
		s.logger.WarnContext(ctx, "stream upstream body", attr.SlogError(err))
	}
}

func (s *Server) errorResponse(w http.ResponseWriter, r *http.Request, err error) {
	if errors.Is(err, ErrNotFound) {
		http.NotFound(w, r)
		return
	}
	s.logger.ErrorContext(r.Context(), "resolve token", attr.SlogError(err))
	http.Error(w, "internal error", http.StatusInternalServerError)
}
