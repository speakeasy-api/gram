package proxy

import (
	"encoding/json"
	"net/http"

	"github.com/modelcontextprotocol/go-sdk/jsonrpc"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/trace"

	"github.com/speakeasy-api/gram/server/internal/attr"
	"github.com/speakeasy-api/gram/server/internal/mcp/mcpversions"
)

// How the MCP protocol version reaches the proxy depends on the revision in
// play, and no single source covers every client:
//
//   - Under the handshake-based revisions (2025-11-25 and earlier) the version
//     is agreed by an `initialize` exchange. The request carries no
//     MCP-Protocol-Version header, because nothing is agreed yet when it is
//     sent, so the handshake is only observable from the two ends of that
//     exchange. Clients on 2025-06-18 and later then repeat the agreed version
//     in the header on every subsequent request; earlier clients never send it.
//   - Under 2026-07-28 there is no `initialize` at all. Each request declares
//     its own version, and on Streamable HTTP that declaration is carried in
//     the header, making it the only source for those clients.
//
// The proxy therefore reads both: the header wherever it appears, and the
// handshake where one happens. Gram is a pass-through on this path, so the
// values are agreed between the client and the upstream server and are recorded
// on the proxy's own spans, keeping them attributable to the upstream leg.
//
// Values reaching these functions are supplied by a client or by a server Gram
// does not control, so they are bounded before being recorded. They are not
// clamped to the known revision set: on a span an unrecognized revision is the
// diagnostic payload, and bucketing belongs on metric dimensions, whose series
// count is what needs protecting.

// appendNegotiatedProtocolVersion appends the span attribute naming the
// protocol revision in effect for r, read from its MCP-Protocol-Version header.
// Returns attrs unchanged when the request names no revision.
func appendNegotiatedProtocolVersion(attrs []attribute.KeyValue, r *http.Request) []attribute.KeyValue {
	v := mcpversions.Sanitize(r.Header.Get(mcpversions.HTTPHeader))
	if v == "" {
		return attrs
	}

	return append(attrs, attr.MCPNegotiatedProtocolVersion(v))
}

// The record functions below take the span explicitly and the proxy calls them
// against the span covering the inbound request, matching how tools/call and
// resources/read attach their own request attributes.

// recordRequestedProtocolVersion stamps the revision a client asked for in its
// initialize params onto span. A no-op when the client asked for none.
func recordRequestedProtocolVersion(span trace.Span, req *InitializeRequest) {
	if !span.IsRecording() || req == nil || req.Params == nil {
		return
	}

	if v := mcpversions.Sanitize(req.Params.ProtocolVersion); v != "" {
		span.SetAttributes(attr.MCPRequestedProtocolVersion(v))
	}
}

// recordNegotiatedProtocolVersion stamps the revision an upstream answered an
// initialize request with onto span, reading it from msg. A no-op when msg is
// not a decodable successful initialize response naming a revision, so a
// rejected or malformed handshake is not recorded as a successful one.
//
// Callers must have established that msg replies to an initialize request; the
// payload shape alone does not identify one.
func recordNegotiatedProtocolVersion(span trace.Span, msg *RemoteMessage) {
	if !span.IsRecording() || msg == nil {
		return
	}

	rpcResp, ok := msg.Message.(*jsonrpc.Response)
	if !ok || rpcResp.Error != nil || len(rpcResp.Result) == 0 {
		return
	}

	// Decoded into a minimal shape rather than mcp.InitializeResult: the
	// protocol version is the only field wanted, and a narrow target keeps this
	// immune to unrelated churn in the SDK's result type.
	var result struct {
		ProtocolVersion string `json:"protocolVersion"`
	}
	if err := json.Unmarshal(rpcResp.Result, &result); err != nil {
		return
	}

	if v := mcpversions.Sanitize(result.ProtocolVersion); v != "" {
		span.SetAttributes(attr.MCPNegotiatedProtocolVersion(v))
	}
}
