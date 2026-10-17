package mcp

import (
	"encoding/base64"
	"errors"
	"fmt"
	"net/http"
	"strings"

	"github.com/speakeasy-api/gram/server/internal/oops"
	"github.com/speakeasy-api/gram/server/internal/requestorigin"
)

// mcpRegistryPublicKeySizes is the public key length, in bytes, of each key
// type the MCP registry accepts: a raw Ed25519 key and a compressed P-384
// point.
var mcpRegistryPublicKeySizes = map[string]int{
	"ed25519":   32,
	"ecdsap384": 49,
}

// SetMCPRegistryAuth records the proof record served at
// /.well-known/mcp-registry-auth on the platform hosts, for example
// "v=MCPv1; k=ed25519; p=<base64 public key>". Empty serves not found. A
// value that is not a well-formed record is refused, so a private key pasted
// by mistake is never published.
func (s *Service) SetMCPRegistryAuth(record string) error {
	record = strings.TrimSpace(record)
	if record != "" {
		if err := validateMCPRegistryAuth(record); err != nil {
			return err
		}
	}
	s.mcpRegistryAuth = record
	return nil
}

func validateMCPRegistryAuth(record string) error {
	invalid := errors.New(`MCP registry auth record must be "v=MCPv1; k=<ed25519|ecdsap384>; p=<base64 public key>"`)
	parts := strings.Split(record, "; ")
	if len(parts) != 3 || parts[0] != "v=MCPv1" {
		return invalid
	}
	keyType, ok := strings.CutPrefix(parts[1], "k=")
	if !ok {
		return invalid
	}
	size, ok := mcpRegistryPublicKeySizes[keyType]
	if !ok {
		return invalid
	}
	encoded, ok := strings.CutPrefix(parts[2], "p=")
	if !ok {
		return invalid
	}
	key, err := base64.StdEncoding.DecodeString(encoded)
	if err != nil || len(key) != size {
		return invalid
	}
	return nil
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
