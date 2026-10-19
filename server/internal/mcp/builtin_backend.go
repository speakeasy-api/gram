package mcp

import (
	"net/http"

	"github.com/speakeasy-api/gram/server/internal/oops"
)

// SetBuiltinBackend registers the handler that serves MCP servers whose
// builtin_backend is name. Call it during startup, before serving requests.
func (s *Service) SetBuiltinBackend(name string, handler http.Handler) {
	s.builtinBackends[name] = handler
}

// serveBuiltinBackend hands the request to the backend's handler. A server
// whose backend this process does not serve is not found.
func (s *Service) serveBuiltinBackend(w http.ResponseWriter, r *http.Request, name string) error {
	handler, ok := s.builtinBackends[name]
	if !ok {
		return oops.E(oops.CodeNotFound, nil, "mcp server not found")
	}
	handler.ServeHTTP(w, r)
	return nil
}
