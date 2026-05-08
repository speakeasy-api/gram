package marketplace

import (
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
// proxy. All proxy routes live under this prefix so the main mux only carves
// out one namespace, and so the dispatch logic stays here rather than leaking
// into the server bootstrap.
const RoutePrefix = "/marketplace/"

// Server hosts the git Smart HTTP proxy for plugin source clones. Streams
// directly from GitHub against an installation token minted by the resolver —
// no local mirror state.
type Server struct {
	resolver   Resolver
	httpClient *guardian.HTTPClient
	logger     *slog.Logger
}

func NewServer(
	resolver Resolver,
	httpClient *guardian.HTTPClient,
	logger *slog.Logger,
) *Server {
	return &Server{
		resolver:   resolver,
		httpClient: httpClient,
		logger:     logger,
	}
}

func (s *Server) Routes() http.Handler {
	mux := http.NewServeMux()
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
