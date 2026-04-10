package githttp

import (
	"errors"
	"net/http"

	gitbackend "github.com/go-git/go-git/v6/backend/http"
	"github.com/go-git/go-git/v6/plumbing/transport"
	"github.com/go-git/go-git/v6/storage"

	"github.com/go-git/go-billy/v6/osfs"
	"github.com/go-git/go-git/v6/plumbing/cache"
	"github.com/go-git/go-git/v6/storage/filesystem"
)

// ErrUnauthorized is returned by auth functions to indicate a 401 response.
var ErrUnauthorized = errors.New("unauthorized")

// RepoResolver maps a project ID to a bare repo path on disk.
type RepoResolver func(projectID string) (string, error)

// AuthFunc validates an incoming HTTP request. Return ErrUnauthorized for 401.
type AuthFunc func(r *http.Request) error

// Option configures a Handler.
type Option func(*Handler)

// WithAuth adds an authentication check to the handler.
func WithAuth(fn AuthFunc) Option {
	return func(h *Handler) {
		h.authFn = fn
	}
}

// Handler serves the git smart HTTP protocol for corpus repositories.
type Handler struct {
	resolver RepoResolver
	authFn   AuthFunc
	backend  *gitbackend.Backend
}

// NewHandler creates a new git smart HTTP handler.
func NewHandler(resolver RepoResolver, opts ...Option) *Handler {
	h := &Handler{
		resolver: resolver,
		authFn:   nil,
		backend:  nil,
	}
	for _, opt := range opts {
		opt(h)
	}

	loader := &repoLoader{resolver: resolver}
	h.backend = gitbackend.NewBackend(loader)

	return h
}

// ServeHTTP routes git smart HTTP requests with optional auth.
func (h *Handler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if h.authFn != nil {
		if err := h.authFn(r); err != nil {
			if errors.Is(err, ErrUnauthorized) {
				w.Header().Set("WWW-Authenticate", `Basic realm="corpus"`)
				http.Error(w, "Unauthorized", http.StatusUnauthorized)
				return
			}
			http.Error(w, "Forbidden", http.StatusForbidden)
			return
		}
	}

	h.backend.ServeHTTP(w, r)
}

// repoLoader implements transport.Loader for resolving corpus repos.
type repoLoader struct {
	resolver RepoResolver
}

func (l *repoLoader) Load(ep *transport.Endpoint) (storage.Storer, error) {
	repoPath, err := l.resolver(ep.Path)
	if err != nil {
		return nil, transport.ErrRepositoryNotFound
	}

	fs := osfs.New(repoPath)
	if _, err := fs.Stat("config"); err != nil {
		return nil, transport.ErrRepositoryNotFound
	}

	return filesystem.NewStorageWithOptions(fs, cache.NewObjectLRUDefault(), filesystem.Options{
		ExclusiveAccess:      false,
		KeepDescriptors:      false,
		MaxOpenDescriptors:   0,
		LargeObjectThreshold: 0,
		AlternatesFS:         nil,
		HighMemoryMode:       false,
		ObjectFormat:         "",
		UseInMemoryIdx:       false,
		IndexCache:           nil,
	}), nil
}
