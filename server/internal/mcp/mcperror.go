package mcp

import (
	"context"
	"encoding/json"
	"log/slog"
	"net/http"

	"github.com/speakeasy-api/gram/server/internal/mcpjsonrpc"
	"github.com/speakeasy-api/gram/server/internal/oops"
)

// writeMCPError serializes cause as the JSON-RPC error response to request id.
// It is the single write site for errors a Gram-served MCP dispatcher returns,
// shared by the hosted, platform, and meta surfaces so that the wire shape of
// an MCP error does not depend on which of them produced it.
//
// revision is the protocol revision in effect for the request. It selects the
// wire error code, so a surface that resolves a revision passes the resolved
// value rather than the client's declaration; an empty value is answered on
// the legacy mappings.
func writeMCPError(ctx context.Context, logger *slog.Logger, w http.ResponseWriter, id mcpjsonrpc.ID, revision string, cause error) error {
	bs, err := json.Marshal(oops.NewMCPErrorFromCause(id, revision, cause))
	if err != nil {
		return oops.E(oops.CodeUnexpected, err, "failed to serialize error response").LogError(ctx, logger)
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	if _, err := w.Write(bs); err != nil {
		return oops.E(oops.CodeUnexpected, err, "failed to write MCP error response")
	}

	return nil
}
