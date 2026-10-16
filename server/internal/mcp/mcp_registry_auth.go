package mcp

import (
	"fmt"
	"net/http"

	"github.com/speakeasy-api/gram/server/internal/oops"
	"github.com/speakeasy-api/gram/server/internal/requestorigin"
)

// SetMCPRegistryAuth records the proof record served at
// /.well-known/mcp-registry-auth on the platform hosts, for example
// "v=MCPv1; k=ed25519; p=<base64 public key>". Empty serves not found.
func (s *Service) SetMCPRegistryAuth(record string) {
	s.mcpRegistryAuth = record
}

// HandleMCPRegistryAuth serves the official MCP registry's HTTP ownership
// proof. The registry fetches it from the platform host when the publish
// workflow logs in, and checks that the workflow's signature matches the
// public key in it. Only platform hosts serve it: a custom domain belongs to
// a customer, not to Speakeasy.
func (s *Service) HandleMCPRegistryAuth(w http.ResponseWriter, r *http.Request) error {
	ctx := r.Context()
	origin, ok := requestorigin.FromContext(ctx)
	if !ok || origin.Surface != requestorigin.SurfacePlatform || s.mcpRegistryAuth == "" {
		return oops.E(oops.CodeNotFound, nil, "MCP registry auth record not found").LogInfo(ctx, s.logger)
	}

	w.Header().Set("Content-Type", "text/plain")
	if _, err := w.Write([]byte(s.mcpRegistryAuth)); err != nil {
		return fmt.Errorf("write MCP registry auth record: %w", err)
	}
	return nil
}
