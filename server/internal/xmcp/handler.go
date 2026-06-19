package xmcp

import (
	"fmt"
	"net/http"

	"github.com/go-chi/chi/v5"

	"github.com/speakeasy-api/gram/server/internal/o11y"
	"github.com/speakeasy-api/gram/server/internal/oops"
)

// ServeMCP handles DELETE, GET, and POST on /x/mcp/{slug} by forwarding
// to the unified mcp.Service.ServeMCPEndpoint dispatcher with the
// "x/mcp" route base. The dispatcher resolves the slug, runs the issuer
// gate when applicable, and dispatches to the remote-MCP proxy or the
// toolset-backed handler.
func (s *Service) ServeMCP(w http.ResponseWriter, r *http.Request) error {
	ctx := r.Context()
	defer o11y.LogDefer(ctx, s.logger, func() error {
		return r.Body.Close()
	})

	slug := chi.URLParam(r, "slug")
	if slug == "" {
		return oops.E(oops.CodeBadRequest, nil, "an mcp slug must be provided")
	}

	if err := s.mcpService.ServeMCPEndpoint(w, r, slug, "x/mcp"); err != nil {
		return fmt.Errorf("serve mcp endpoint: %w", err)
	}
	return nil
}
