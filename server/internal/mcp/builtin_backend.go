package mcp

import (
	"fmt"
	"log/slog"
	"net/http"
	"strings"

	"github.com/google/uuid"
	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/speakeasy-api/gram/server/internal/auth/principalcredential"
	"github.com/speakeasy-api/gram/server/internal/mcp/httpheaders"
	"github.com/speakeasy-api/gram/server/internal/mcp/mcproute"
	mcpendpointsrepo "github.com/speakeasy-api/gram/server/internal/mcpendpoints/repo"
	mcpserversrepo "github.com/speakeasy-api/gram/server/internal/mcpservers/repo"
	"github.com/speakeasy-api/gram/server/internal/oops"
)

// BuiltinBackend serves a first-party MCP server.
type BuiltinBackend struct {
	// Handler serves requests the gateway already authenticated.
	Handler http.Handler

	// Tools are the server's tools. They are the same for every caller, so
	// the consent screen lists them before the user signs in.
	Tools []*mcp.Tool
}

// SetBuiltinBackend registers the backend that serves MCP servers whose
// builtin_backend is name. Call it during startup, before serving requests.
func (s *Service) SetBuiltinBackend(name string, backend BuiltinBackend) {
	s.builtinBackends[name] = backend
}

// serveBuiltinBackend authenticates the caller and hands the request to the
// backend's handler. A key (an API key, an agent key, or a principal
// credential) authenticates as on any private server: the backend authorizes
// each call itself, so it needs no mcp:connect grant. Any other caller signs in
// through the server's issuer, which answers a request without a credential
// with the OAuth challenge. A server whose backend this process does not serve
// is not found.
func (s *Service) serveBuiltinBackend(
	w http.ResponseWriter,
	r *http.Request,
	logger *slog.Logger,
	mcpEndpoint *mcpendpointsrepo.McpEndpoint,
	mcpServer *mcpserversrepo.McpServer,
	mcpRouteBase mcproute.Base,
) error {
	backend, ok := s.builtinBackends[mcpServer.BuiltinBackend.String]
	if !ok {
		return oops.E(oops.CodeNotFound, nil, "mcp server not found")
	}
	ctx := r.Context()

	token := httpheaders.AuthorizationBearerToken(r)
	isKey := strings.HasPrefix(token, "gram_") || principalcredential.IsToken(token)
	if !mcpServer.UserSessionIssuerID.Valid || isKey {
		authCtx, err := s.RequirePrivateIdentityAuth(ctx, w, r, false, uuid.Nil, "")
		if err != nil {
			return err
		}
		backend.Handler.ServeHTTP(w, r.WithContext(authCtx))
		return nil
	}

	resolved, err := s.BuildResolvedMcpEndpointForServer(ctx, logger, mcpEndpoint, mcpServer, mcpRouteBase)
	if err != nil {
		return err
	}
	signedIn, _, _, err := s.authenticateIssuerGate(ctx, w, token, s.BaseURLForRequest(r), resolved)
	if err != nil {
		return fmt.Errorf("apply issuer gate: %w", err)
	}
	backend.Handler.ServeHTTP(w, r.WithContext(signedIn))
	return nil
}
