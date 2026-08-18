package mcp

import (
	"context"
	"encoding/json"
	"log/slog"
	"net/http"

	"github.com/speakeasy-api/gram/server/internal/mcp/mcpversions"
	"github.com/speakeasy-api/gram/server/internal/mcpjsonrpc"
	"github.com/speakeasy-api/gram/server/internal/oops"

	"github.com/speakeasy-api/gram/server/internal/attr"
)

// mcpCodeUnsupportedProtocolVersion is the JSON-RPC error code MCP allocates for
// a request declaring a protocol revision the server does not implement. It sits
// in the -32020..-32099 sub-range the specification reserves for itself, so it
// carries this meaning and no other.
//
// See https://modelcontextprotocol.io/specification/2026-07-28/basic/index#error-codes.
const mcpCodeUnsupportedProtocolVersion = -32022

// metaProtocolVersionKey is where a request carries its protocol revision from
// 2026-07-28 onward, mirrored into the MCP-Protocol-Version header by the
// Streamable HTTP binding.
const metaProtocolVersionKey = "io.modelcontextprotocol/protocolVersion"

// unsupportedProtocolVersionError is the error response body. `supported` is the
// field a client reads to pick a revision to retry with.
type unsupportedProtocolVersionError struct {
	JSONRPC string                            `json:"jsonrpc"`
	ID      mcpjsonrpc.ID                     `json:"id"`
	Error   unsupportedProtocolVersionPayload `json:"error"`
}

type unsupportedProtocolVersionPayload struct {
	Code    int                            `json:"code"`
	Message string                         `json:"message"`
	Data    unsupportedProtocolVersionData `json:"data"`
}

type unsupportedProtocolVersionData struct {
	Supported []string `json:"supported"`
}

// requestedProtocolVersion returns the protocol revision a request declares,
// preferring the header the Streamable HTTP binding requires and falling back to
// the `_meta` key it mirrors. Empty when the request declares none, which is
// every client on a revision predating the header.
//
// The value is client-supplied and is bounded before use, since it reaches
// telemetry and an error message.
func requestedProtocolVersion(r *http.Request, req *rawRequest) string {
	if v := mcpversions.Sanitize(r.Header.Get(mcpversions.HTTPHeader)); v != "" {
		return v
	}
	if req == nil || len(req.Params) == 0 {
		return ""
	}

	var params struct {
		Meta map[string]json.RawMessage `json:"_meta"`
	}
	if err := json.Unmarshal(req.Params, &params); err != nil {
		return ""
	}
	raw, ok := params.Meta[metaProtocolVersionKey]
	if !ok {
		return ""
	}

	var declared string
	if err := json.Unmarshal(raw, &declared); err != nil {
		return ""
	}

	return mcpversions.Sanitize(declared)
}

// rejectUnsupportedProtocolVersion writes the specified refusal for a request
// declaring a revision this surface does not implement: HTTP 400 with a JSON-RPC
// error naming the revisions it does.
//
// This is not merely a courtesy. A client on a handshake-less revision has no
// `initialize` in which to be told what the server speaks, so the refusal is the
// only signal it gets — and the specification's backward-compatibility flow is
// built on receiving it: on a 400 the client either retries with an advertised
// revision or falls back to `initialize`. Answering such a client with a result
// shaped for an older revision leaves it validating that result against the only
// revision it knows, which is how a missing `resultType` surfaces as a broken
// server rather than as a version mismatch.
func rejectUnsupportedProtocolVersion(
	ctx context.Context,
	logger *slog.Logger,
	w http.ResponseWriter,
	id mcpjsonrpc.ID,
	served, requested string,
) error {
	logger.InfoContext(ctx, "rejecting request declaring an unsupported mcp protocol revision",
		attr.SlogMCPRequestedProtocolVersion(requested),
		attr.SlogMCPNegotiatedProtocolVersion(served))

	payload := unsupportedProtocolVersionError{
		JSONRPC: "2.0",
		ID:      id,
		Error: unsupportedProtocolVersionPayload{
			Code:    mcpCodeUnsupportedProtocolVersion,
			Message: "unsupported protocol version: " + requested,
			Data:    unsupportedProtocolVersionData{Supported: []string{served}},
		},
	}

	bs, err := json.Marshal(payload)
	if err != nil {
		return oops.E(oops.CodeUnexpected, err, "failed to serialize unsupported protocol version response").LogError(ctx, logger)
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusBadRequest)
	_, _ = w.Write(bs)

	return nil
}

// enforceServedProtocolVersion refuses a request this surface cannot honour,
// reporting whether it did so. See [mcpversions.Serves] for which requests those
// are, and [rejectUnsupportedProtocolVersion] for why refusing beats serving a
// mismatched shape.
//
// Only surfaces Gram answers from its own inventory call this. The remote and
// tunneled backends relay to an upstream that answers for itself, and gating
// those would have Gram refuse a revision the upstream may well implement.
func (s *Service) enforceServedProtocolVersion(
	ctx context.Context,
	w http.ResponseWriter,
	r *http.Request,
	req *rawRequest,
	served string,
) (bool, error) {
	requested := requestedProtocolVersion(r, req)
	if mcpversions.Serves(served, requested) {
		return false, nil
	}

	var id mcpjsonrpc.ID
	if req != nil {
		id = req.ID
	}

	return true, rejectUnsupportedProtocolVersion(ctx, s.logger, w, id, served, requested)
}
