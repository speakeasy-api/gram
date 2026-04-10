package githttp

import (
	"errors"
	"net/http"
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
}

// NewHandler creates a new git smart HTTP handler.
func NewHandler(resolver RepoResolver, opts ...Option) *Handler {
	h := &Handler{
		resolver: resolver,
	}
	for _, opt := range opts {
		opt(h)
	}
	return h
}

// ServeHTTP implements http.Handler but is not yet implemented.
func (h *Handler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	http.Error(w, "not implemented", http.StatusNotImplemented)
}
