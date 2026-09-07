package marketplace

import (
	"bytes"
	"compress/gzip"
	"compress/zlib"
	"context"
	"crypto/sha1" //nolint:gosec // Git's loose-object protocol requires SHA-1 object IDs.
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"maps"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/speakeasy-api/gram/server/internal/attr"
)

// RepositoryFiles reads the current main-branch files for a locally published
// marketplace. Production never uses this path; it proxies a private GitHub
// repository through Server instead.
type RepositoryFiles func(ctx context.Context, owner, repo string) (map[string][]byte, error)

// LocalServer serves the local fixture publisher as a read-only Git repository.
// It supports Smart HTTP for shallow clones used by coding-agent marketplace
// installers and retains dumb HTTP for compatibility. This lets standard local
// startup exercise the same opaque marketplace URL that production emits
// without requiring a GitHub App.
type LocalServer struct {
	resolver Resolver
	files    RepositoryFiles
	logger   *slog.Logger

	mu      sync.RWMutex
	objects map[string]map[string][]byte
}

func NewLocalServer(resolver Resolver, files RepositoryFiles, logger *slog.Logger) *LocalServer {
	return &LocalServer{
		resolver: resolver,
		files:    files,
		logger:   logger,
		mu:       sync.RWMutex{},
		objects:  make(map[string]map[string][]byte),
	}
}

func (s *LocalServer) Routes() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET "+RoutePrefix+"{slug}/info/refs", s.handleInfoRefs)
	mux.HandleFunc("POST "+RoutePrefix+"{slug}/git-upload-pack", s.handleUploadPack)
	mux.HandleFunc("GET "+RoutePrefix+"{slug}/HEAD", s.handleHEAD)
	mux.HandleFunc("GET "+RoutePrefix+"{slug}/objects/{fanout}/{object}", s.handleObject)
	mux.HandleFunc("GET "+RoutePrefix+"healthz", func(w http.ResponseWriter, _ *http.Request) {
		_, _ = io.WriteString(w, "ok")
	})
	return mux
}

func (s *LocalServer) IsMarketplaceRoute(r *http.Request) bool {
	return strings.HasPrefix(r.URL.Path, RoutePrefix)
}

func (s *LocalServer) handleInfoRefs(w http.ResponseWriter, r *http.Request) {
	service := r.URL.Query().Get("service")
	if service != "" && service != "git-upload-pack" {
		http.Error(w, "only git-upload-pack supported", http.StatusForbidden)
		return
	}

	token, snapshot, ok := s.snapshotForRequest(w, r)
	if !ok {
		return
	}

	if service == "git-upload-pack" {
		repo, cleanup, err := materializeLocalGitRepository(snapshot)
		if err != nil {
			s.localGitError(w, r, "materialize local marketplace repository", err)
			return
		}
		defer cleanup()

		advertisement, err := runLocalUploadPack(r.Context(), repo, true, nil)
		if err != nil {
			s.localGitError(w, r, "advertise local marketplace refs", err)
			return
		}
		w.Header().Set("Content-Type", "application/x-git-upload-pack-advertisement")
		w.Header().Set("Cache-Control", "no-cache")
		_, _ = io.WriteString(w, "001e# service=git-upload-pack\n0000")
		_, _ = w.Write(advertisement)
		return
	}

	s.storeObjects(token, snapshot.objects)
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	w.Header().Set("Cache-Control", "no-cache")
	_, _ = fmt.Fprintf(w, "%s\trefs/heads/main\n", snapshot.commit)
}

func (s *LocalServer) handleUploadPack(w http.ResponseWriter, r *http.Request) {
	var bodyReader io.Reader = r.Body
	switch r.Header.Get("Content-Encoding") {
	case "":
	case "gzip":
		reader, err := gzip.NewReader(r.Body)
		if err != nil {
			http.Error(w, "invalid gzip request body", http.StatusBadRequest)
			return
		}
		defer func() { _ = reader.Close() }()
		bodyReader = reader
	default:
		http.Error(w, "content encoding not supported", http.StatusUnsupportedMediaType)
		return
	}
	body, err := io.ReadAll(http.MaxBytesReader(w, io.NopCloser(bodyReader), maxUploadPackRequestBytes))
	if err != nil {
		if _, ok := errors.AsType[*http.MaxBytesError](err); ok {
			http.Error(w, "request body too large", http.StatusRequestEntityTooLarge)
			return
		}
		http.Error(w, "invalid request body", http.StatusBadRequest)
		return
	}

	_, snapshot, ok := s.snapshotForRequest(w, r)
	if !ok {
		return
	}
	repo, cleanup, err := materializeLocalGitRepository(snapshot)
	if err != nil {
		s.localGitError(w, r, "materialize local marketplace repository", err)
		return
	}
	defer cleanup()

	response, err := runLocalUploadPack(r.Context(), repo, false, body)
	if err != nil {
		s.localGitError(w, r, "serve local marketplace upload pack", err)
		return
	}
	w.Header().Set("Content-Type", "application/x-git-upload-pack-result")
	w.Header().Set("Cache-Control", "no-cache")
	_, _ = w.Write(response)
}

func (s *LocalServer) handleHEAD(w http.ResponseWriter, r *http.Request) {
	if _, _, ok := s.snapshotForRequest(w, r); !ok {
		return
	}
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	w.Header().Set("Cache-Control", "no-cache")
	_, _ = io.WriteString(w, "ref: refs/heads/main\n")
}

var localObjectPathPattern = regexp.MustCompile(`^[0-9a-f]{40}$`)

func (s *LocalServer) handleObject(w http.ResponseWriter, r *http.Request) {
	token := tokenFromSlug(r.PathValue("slug"))
	oid := r.PathValue("fanout") + r.PathValue("object")
	if token == "" || !localObjectPathPattern.MatchString(oid) {
		http.NotFound(w, r)
		return
	}
	if _, err := s.resolver.Resolve(r.Context(), token); err != nil {
		s.errorResponse(w, r, err)
		return
	}

	object, ok := s.cachedObject(token, oid)
	if !ok {
		_, snapshot, snapshotOK := s.snapshotForToken(w, r, token)
		if !snapshotOK {
			return
		}
		s.storeObjects(token, snapshot.objects)
		object, ok = snapshot.objects[oid]
	}
	if !ok {
		http.NotFound(w, r)
		return
	}

	w.Header().Set("Content-Type", "application/octet-stream")
	w.Header().Set("Cache-Control", "private, max-age=31536000, immutable")
	_, _ = w.Write(object)
}

func (s *LocalServer) snapshotForRequest(w http.ResponseWriter, r *http.Request) (string, localGitSnapshot, bool) {
	token := tokenFromSlug(r.PathValue("slug"))
	if token == "" {
		http.NotFound(w, r)
		return "", localGitSnapshot{commit: "", objects: nil}, false
	}
	_, snapshot, ok := s.snapshotForToken(w, r, token)
	return token, snapshot, ok
}

func (s *LocalServer) snapshotForToken(w http.ResponseWriter, r *http.Request, token string) (Upstream, localGitSnapshot, bool) {
	upstream, err := s.resolver.Resolve(r.Context(), token)
	if err != nil {
		s.errorResponse(w, r, err)
		return Upstream{Token: "", Owner: "", Repo: "", AccessToken: ""}, localGitSnapshot{commit: "", objects: nil}, false
	}
	files, err := s.files(r.Context(), upstream.Owner, upstream.Repo)
	if err != nil {
		if errors.Is(err, ErrNotFound) {
			http.NotFound(w, r)
		} else {
			s.logger.ErrorContext(r.Context(), "read local marketplace repository", attr.SlogError(err))
			http.Error(w, "internal error", http.StatusInternalServerError)
		}
		return Upstream{Token: "", Owner: "", Repo: "", AccessToken: ""}, localGitSnapshot{commit: "", objects: nil}, false
	}
	snapshot, err := buildLocalGitSnapshot(files)
	if err != nil {
		s.logger.ErrorContext(r.Context(), "build local marketplace git snapshot", attr.SlogError(err))
		http.Error(w, "internal error", http.StatusInternalServerError)
		return Upstream{Token: "", Owner: "", Repo: "", AccessToken: ""}, localGitSnapshot{commit: "", objects: nil}, false
	}
	return upstream, snapshot, true
}

const localGitObjectCacheLimit = 10_000

func (s *LocalServer) storeObjects(token string, objects map[string][]byte) {
	s.mu.Lock()
	defer s.mu.Unlock()
	cached := s.objects[token]
	if cached == nil {
		cached = make(map[string][]byte)
		s.objects[token] = cached
	}
	maps.Copy(cached, objects)
	if len(cached) > localGitObjectCacheLimit {
		// Loose Git objects are immutable and can be rebuilt from the persistent
		// repository snapshot on demand. Bound the local-only cache rather than
		// retaining every object from every marketplace revision indefinitely.
		clear(cached)
		maps.Copy(cached, objects)
	}
}

func (s *LocalServer) cachedObject(token, oid string) ([]byte, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	object, ok := s.objects[token][oid]
	return object, ok
}

func (s *LocalServer) errorResponse(w http.ResponseWriter, r *http.Request, err error) {
	if errors.Is(err, ErrNotFound) {
		http.NotFound(w, r)
		return
	}
	s.logger.ErrorContext(r.Context(), "resolve local marketplace token", attr.SlogError(err))
	http.Error(w, "internal error", http.StatusInternalServerError)
}

func (s *LocalServer) localGitError(w http.ResponseWriter, r *http.Request, message string, err error) {
	s.logger.ErrorContext(r.Context(), message, attr.SlogError(err))
	http.Error(w, "internal error", http.StatusInternalServerError)
}

func materializeLocalGitRepository(snapshot localGitSnapshot) (string, func(), error) {
	repo, err := os.MkdirTemp("", "gram-marketplace-git-*")
	if err != nil {
		return "", func() {}, fmt.Errorf("create temporary bare repository: %w", err)
	}
	cleanup := func() { _ = os.RemoveAll(repo) }
	fail := func(err error) (string, func(), error) {
		cleanup()
		return "", func() {}, err
	}

	for _, dir := range []string{"objects", "refs/heads"} {
		if err := os.MkdirAll(filepath.Join(repo, dir), 0o700); err != nil {
			return fail(fmt.Errorf("create bare repository directory: %w", err))
		}
	}
	if err := os.WriteFile(filepath.Join(repo, "HEAD"), []byte("ref: refs/heads/main\n"), 0o600); err != nil {
		return fail(fmt.Errorf("write bare repository HEAD: %w", err))
	}
	if err := os.WriteFile(filepath.Join(repo, "refs", "heads", "main"), []byte(snapshot.commit+"\n"), 0o600); err != nil {
		return fail(fmt.Errorf("write bare repository main ref: %w", err))
	}
	for oid, object := range snapshot.objects {
		if !localObjectPathPattern.MatchString(oid) {
			return fail(fmt.Errorf("invalid local git object id %q", oid))
		}
		objectDir := filepath.Join(repo, "objects", oid[:2])
		if err := os.MkdirAll(objectDir, 0o700); err != nil {
			return fail(fmt.Errorf("create bare repository object directory: %w", err))
		}
		if err := os.WriteFile(filepath.Join(objectDir, oid[2:]), object, 0o600); err != nil {
			return fail(fmt.Errorf("write bare repository object: %w", err))
		}
	}
	return repo, cleanup, nil
}

func runLocalUploadPack(ctx context.Context, repo string, advertise bool, request []byte) ([]byte, error) {
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()

	args := []string{"upload-pack", "--stateless-rpc"}
	if advertise {
		args = append(args, "--advertise-refs")
	}
	args = append(args, repo)
	// All arguments are fixed except repo, which is an owner-only directory just
	// created by materializeLocalGitRepository; no request value reaches exec.
	cmd := exec.CommandContext(ctx, "git", args...)
	if !advertise {
		cmd.Stdin = bytes.NewReader(request)
	}
	output, err := cmd.Output()
	if err != nil {
		if exitErr, ok := errors.AsType[*exec.ExitError](err); ok {
			return nil, fmt.Errorf("git upload-pack: %w: %s", err, strings.TrimSpace(string(exitErr.Stderr)))
		}
		return nil, fmt.Errorf("git upload-pack: %w", err)
	}
	return output, nil
}

type localGitSnapshot struct {
	commit  string
	objects map[string][]byte
}

type localTree struct {
	files map[string][]byte
	dirs  map[string]*localTree
}

func buildLocalGitSnapshot(files map[string][]byte) (localGitSnapshot, error) {
	root := &localTree{files: make(map[string][]byte), dirs: make(map[string]*localTree)}
	for path, content := range files {
		parts := strings.Split(path, "/")
		if path == "" || strings.HasPrefix(path, "/") || len(parts) == 0 {
			return localGitSnapshot{}, fmt.Errorf("invalid repository path %q", path)
		}
		tree := root
		for _, part := range parts[:len(parts)-1] {
			if part == "" || part == "." || part == ".." || part == ".git" {
				return localGitSnapshot{}, fmt.Errorf("invalid repository path %q", path)
			}
			if tree.dirs[part] == nil {
				tree.dirs[part] = &localTree{files: make(map[string][]byte), dirs: make(map[string]*localTree)}
			}
			tree = tree.dirs[part]
		}
		name := parts[len(parts)-1]
		if name == "" || name == "." || name == ".." || name == ".git" {
			return localGitSnapshot{}, fmt.Errorf("invalid repository path %q", path)
		}
		tree.files[name] = append([]byte(nil), content...)
	}

	objects := make(map[string][]byte)
	treeOID, err := writeLocalTree(root, objects)
	if err != nil {
		return localGitSnapshot{}, err
	}
	commitBody := fmt.Appendf(nil,
		"tree %s\nauthor Local Fixture <local@example.invalid> 0 +0000\ncommitter Local Fixture <local@example.invalid> 0 +0000\n\nLocal marketplace fixture\n",
		treeOID,
	)
	commitOID, err := storeLocalGitObject("commit", commitBody, objects)
	if err != nil {
		return localGitSnapshot{}, err
	}
	return localGitSnapshot{commit: commitOID, objects: objects}, nil
}

type localTreeEntry struct {
	name string
	mode string
	oid  string
	dir  bool
}

func writeLocalTree(tree *localTree, objects map[string][]byte) (string, error) {
	entries := make([]localTreeEntry, 0, len(tree.files)+len(tree.dirs))
	for name, content := range tree.files {
		oid, err := storeLocalGitObject("blob", content, objects)
		if err != nil {
			return "", err
		}
		entries = append(entries, localTreeEntry{name: name, mode: "100644", oid: oid, dir: false})
	}
	for name, child := range tree.dirs {
		oid, err := writeLocalTree(child, objects)
		if err != nil {
			return "", err
		}
		entries = append(entries, localTreeEntry{name: name, mode: "40000", oid: oid, dir: true})
	}
	sort.Slice(entries, func(i, j int) bool {
		left, right := entries[i].name, entries[j].name
		if entries[i].dir {
			left += "/"
		}
		if entries[j].dir {
			right += "/"
		}
		return left < right
	})

	var body bytes.Buffer
	for _, entry := range entries {
		oid, err := hex.DecodeString(entry.oid)
		if err != nil {
			return "", fmt.Errorf("decode git object id: %w", err)
		}
		_, _ = fmt.Fprintf(&body, "%s %s", entry.mode, entry.name)
		_ = body.WriteByte(0)
		_, _ = body.Write(oid)
	}
	return storeLocalGitObject("tree", body.Bytes(), objects)
}

func storeLocalGitObject(kind string, body []byte, objects map[string][]byte) (string, error) {
	raw := make([]byte, 0, len(kind)+32+len(body))
	raw = fmt.Appendf(raw, "%s %d%c", kind, len(body), byte(0))
	raw = append(raw, body...)
	hash := sha1.Sum(raw) //nolint:gosec // Git's object format requires SHA-1.
	oid := hex.EncodeToString(hash[:])

	var compressed bytes.Buffer
	writer := zlib.NewWriter(&compressed)
	if _, err := writer.Write(raw); err != nil {
		return "", fmt.Errorf("compress git object: %w", err)
	}
	if err := writer.Close(); err != nil {
		return "", fmt.Errorf("close git object compressor: %w", err)
	}
	objects[oid] = compressed.Bytes()
	return oid, nil
}
