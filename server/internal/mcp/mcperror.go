package mcp

import (
	"context"
	"encoding/json"
	"log/slog"
	"net/http"

	"github.com/speakeasy-api/gram/server/internal/mcp/mcpversions"
	"github.com/speakeasy-api/gram/server/internal/mcpjsonrpc"
	"github.com/speakeasy-api/gram/server/internal/oops"
)

// writeMCPError serializes cause as the JSON-RPC error response to request id.
// The hosted, platform, and meta surfaces share it so that the wire shape of an
// MCP error does not depend on which of them produced it. The consent
// transport writes its own JSON-RPC errors and does not come through here.
//
// revision is the protocol revision in effect for the request. It selects both
// the wire error code and the HTTP status, so a surface that resolves a
// revision passes the resolved value rather than the client's declaration; an
// empty value is answered on the legacy mappings.
func writeMCPError(ctx context.Context, logger *slog.Logger, w http.ResponseWriter, id mcpjsonrpc.ID, revision string, cause error) error {
	mcpErr := oops.NewMCPErrorFromCause(id, revision, cause)

	bs, err := json.Marshal(mcpErr)
	if err != nil {
		return oops.E(oops.CodeUnexpected, err, "failed to serialize error response").LogError(ctx, logger)
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(mcpErrorHTTPStatus(mcpErr.Code, revision))
	if _, err := w.Write(bs); err != nil {
		return oops.E(oops.CodeUnexpected, err, "failed to write error response body").LogError(ctx, logger)
	}

	return nil
}

// mcpErrorHTTPStatus selects the HTTP status carrying a JSON-RPC error.
//
// UnsupportedProtocolVersionError is always carried by HTTP 400: the
// unsupported declaration cannot select which revision's status rules govern
// its own rejection, and the error exists to let the client inspect the body
// and retry with one of the advertised versions.
//
// Other errors under the handshake-based revisions keep the 200 they have
// always been served. Under them a JSON-RPC error travels in the body of a
// successful response, and many clients on those revisions read a non-2xx as
// a transport failure without parsing the body at all, so a status change
// there breaks working clients to fix a problem none of them have.
//
// MCP 2026-07-28 instead mandates a status per condition, and the mandate is
// load-bearing rather than cosmetic. A client that speaks both eras detects
// which one a server implements by making a modern request and inspecting the
// body of a 400 before falling back, and it separates a modern server that
// does not implement a method from a legacy server that does not host the
// modern endpoint at all by the JSON-RPC body accompanying a 404. Answering
// either with a 200 leaves such a client nothing to inspect and no way to tell
// the eras apart.
//
// Conditions the specification assigns no status keep 200 on every revision.
// Inventing one would move traffic it did not ask to move.
//
// An error that names no request — a notification, or a failure raised before
// the id could be read — is treated no differently. The mandated status is a
// property of the condition rather than of the message that provoked it, and
// the specification's own direction for input a server cannot accept is an
// HTTP error status, not a 200 carrying an error body. Splitting on the id
// here would also put this out of step with the wrapper that answers errors
// escaping a handler, which has no 200 to fall back to.
func mcpErrorHTTPStatus(code oops.MCPCode, revision string) int {
	// A protocol version outside the served set cannot govern its own error
	// response. MCP assigns this condition HTTP 400 specifically so a client
	// can inspect the supported set and retry with a mutually supported
	// version, regardless of which unsupported revision it requested.
	if code == oops.MCPCodeUnsupportedProtocolVersion {
		return http.StatusBadRequest
	}

	if !mcpversions.AtLeast(revision, mcpversions.Version20260728) {
		return http.StatusOK
	}

	if mandated, ok := code.MandatedHTTPStatus(); ok {
		return mandated
	}

	return http.StatusOK
}
