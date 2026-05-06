package marketplace

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"os/exec"
	"time"
)

// Server hosts the URL-based marketplace.json endpoint and the git Smart HTTP
// proxy for plugin sources. Both are scoped to a single opaque URL token that
// the resolver maps to an upstream private repo.
type Server struct {
	resolver      Resolver
	mirror        *Mirror
	publicBaseURL string // e.g. https://marketplaces.gram.dev
	fetchInterval time.Duration
	logger        *slog.Logger
}

func NewServer(
	resolver Resolver,
	mirror *Mirror,
	publicBaseURL string,
	fetchInterval time.Duration,
	logger *slog.Logger,
) *Server {
	return &Server{
		resolver:      resolver,
		mirror:        mirror,
		publicBaseURL: publicBaseURL,
		fetchInterval: fetchInterval,
		logger:        logger,
	}
}

func (s *Server) Routes() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /m/{token}/marketplace.json", s.handleManifest)
	// The {slug} segment captures "<token>.git"; the handler strips the suffix.
	// Go 1.22 ServeMux disallows mixing literals with wildcards inside one segment.
	mux.HandleFunc("GET /p/{slug}/info/refs", s.handleInfoRefs)
	mux.HandleFunc("POST /p/{slug}/git-upload-pack", s.handleUploadPack)
	mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, _ *http.Request) {
		_, _ = io.WriteString(w, "ok")
	})
	return mux
}

// tokenFromSlug extracts the URL token from a "{token}.git" path segment.
// Returns "" if the segment is missing the .git suffix.
func tokenFromSlug(slug string) string {
	const suffix = ".git"
	if len(slug) <= len(suffix) || slug[len(slug)-len(suffix):] != suffix {
		return ""
	}
	return slug[:len(slug)-len(suffix)]
}

// handleManifest serves a URL-based Claude Code marketplace.json. It reads the
// existing .claude-plugin/marketplace.json the publish flow already wrote to
// the upstream and rewrites each plugin's source to an absolute git URL on
// this proxy with a `path` selector.
func (s *Server) handleManifest(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	token := r.PathValue("token")

	up, err := s.resolver.Resolve(ctx, token)
	if err != nil {
		s.errorResponse(w, r, err)
		return
	}

	mirrorPath, err := s.mirror.Ensure(ctx, up, s.fetchInterval)
	if err != nil {
		s.logger.ErrorContext(ctx, "ensure mirror", slog.Any("error", err))
		http.Error(w, "mirror unavailable", http.StatusBadGateway)
		return
	}

	raw, err := s.mirror.ReadFileAtHead(ctx, mirrorPath, ".claude-plugin/marketplace.json")
	if err != nil {
		s.logger.ErrorContext(ctx, "read marketplace.json", slog.Any("error", err))
		http.Error(w, "marketplace.json missing in upstream", http.StatusNotFound)
		return
	}

	rewritten, err := s.rewriteManifest(raw, token)
	if err != nil {
		s.logger.ErrorContext(ctx, "rewrite manifest", slog.Any("error", err))
		http.Error(w, "manifest rewrite failed", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-cache")
	_, _ = w.Write(rewritten)
}

// rewriteManifest transforms a git-based manifest (string-typed plugin sources
// like "./foo") into a URL-based one (object-typed sources pointing at the
// proxy). String sources are interpreted as relative paths into the upstream
// repo; object sources are passed through unmodified.
func (s *Server) rewriteManifest(raw []byte, token string) ([]byte, error) {
	var m map[string]any
	if err := json.Unmarshal(raw, &m); err != nil {
		return nil, fmt.Errorf("unmarshal manifest: %w", err)
	}
	pluginsRaw, ok := m["plugins"].([]any)
	if !ok {
		return nil, errors.New("manifest missing plugins array")
	}
	gitURL := fmt.Sprintf("%s/p/%s.git", s.publicBaseURL, token)
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
			entry["source"] = map[string]any{
				"source": "git",
				"url":    gitURL,
				"path":   trimRelPrefix(pathStr),
			}
		}
	}
	return json.MarshalIndent(m, "", "  ")
}

func trimRelPrefix(s string) string {
	for _, prefix := range []string{"./", "/"} {
		if len(s) >= len(prefix) && s[:len(prefix)] == prefix {
			return s[len(prefix):]
		}
	}
	return s
}

// handleInfoRefs serves the Smart HTTP ref-advertisement.
func (s *Server) handleInfoRefs(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	if r.URL.Query().Get("service") != "git-upload-pack" {
		http.Error(w, "only git-upload-pack supported", http.StatusForbidden)
		return
	}
	token := tokenFromSlug(r.PathValue("slug"))
	if token == "" {
		http.NotFound(w, r)
		return
	}

	up, err := s.resolver.Resolve(ctx, token)
	if err != nil {
		s.errorResponse(w, r, err)
		return
	}

	mirrorPath, err := s.mirror.Ensure(ctx, up, s.fetchInterval)
	if err != nil {
		s.logger.ErrorContext(ctx, "ensure mirror", slog.Any("error", err))
		http.Error(w, "mirror unavailable", http.StatusBadGateway)
		return
	}

	w.Header().Set("Content-Type", "application/x-git-upload-pack-advertisement")
	w.Header().Set("Cache-Control", "no-cache")
	w.WriteHeader(http.StatusOK)

	if _, err := w.Write(pktLine("# service=git-upload-pack\n")); err != nil {
		return
	}
	if _, err := w.Write([]byte("0000")); err != nil {
		return
	}

	cmd := exec.CommandContext(ctx, "git", "upload-pack", "--stateless-rpc", "--advertise-refs", mirrorPath)
	cmd.Stdout = w
	if err := cmd.Run(); err != nil {
		s.logger.ErrorContext(ctx, "upload-pack advertise", slog.Any("error", err))
	}
}

// handleUploadPack streams the packfile for a fetch/clone request.
func (s *Server) handleUploadPack(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	token := tokenFromSlug(r.PathValue("slug"))
	if token == "" {
		http.NotFound(w, r)
		return
	}

	up, err := s.resolver.Resolve(ctx, token)
	if err != nil {
		s.errorResponse(w, r, err)
		return
	}

	mirrorPath, err := s.mirror.Ensure(ctx, up, s.fetchInterval)
	if err != nil {
		s.logger.ErrorContext(ctx, "ensure mirror", slog.Any("error", err))
		http.Error(w, "mirror unavailable", http.StatusBadGateway)
		return
	}

	w.Header().Set("Content-Type", "application/x-git-upload-pack-result")
	w.Header().Set("Cache-Control", "no-cache")

	cmd := exec.CommandContext(ctx, "git", "upload-pack", "--stateless-rpc", mirrorPath)
	cmd.Stdin = r.Body
	cmd.Stdout = w
	if err := cmd.Run(); err != nil {
		s.logger.ErrorContext(ctx, "upload-pack stream", slog.Any("error", err))
	}
}

func (s *Server) errorResponse(w http.ResponseWriter, r *http.Request, err error) {
	if errors.Is(err, ErrNotFound) {
		http.NotFound(w, r)
		return
	}
	s.logger.ErrorContext(r.Context(), "resolve token", slog.Any("error", err))
	http.Error(w, "internal error", http.StatusInternalServerError)
}

// pktLine wraps a payload in git's pkt-line framing: 4-hex-digit length prefix
// (length includes the 4 prefix bytes) followed by the payload bytes.
func pktLine(payload string) []byte {
	n := len(payload) + 4
	return fmt.Appendf(nil, "%04x%s", n, payload)
}
