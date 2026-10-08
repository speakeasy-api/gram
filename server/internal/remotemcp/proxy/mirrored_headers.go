package proxy

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"

	"github.com/modelcontextprotocol/go-sdk/jsonrpc"

	"github.com/speakeasy-api/gram/server/internal/mcp/httpheaders"
)

// validateMirroredHeaders compares the headers on the fully constructed
// upstream request with the final decoded body. MCP 2026-07-28 mirrors
// selected JSON-RPC fields into HTTP headers and the body remains the source
// of truth, so the request sent upstream must reject any disagreement after
// interceptors and configured headers have run.
//
// The wire format (header names, Base64 sentinel, which params field Mcp-Name
// mirrors) is shared with Speakeasy's terminating surfaces through httpheaders.
// Treating an absent header as valid is this intermediary's own policy: the
// proxy relays protocol revisions that predate request metadata, and it never
// answers a protocol version itself.
func validateMirroredHeaders(r *http.Request, req *UserRequest) *RejectError {
	if r == nil || req == nil || len(req.JSONRPCMessages) != 1 {
		return nil
	}
	rpcReq, ok := req.JSONRPCMessages[0].(*jsonrpc.Request)
	if !ok {
		return nil
	}

	method, methodPresent, rejection := singleMirroredHeader(r, httpheaders.MethodHeader)
	if rejection != nil {
		return rejection
	}
	if methodPresent && method != rpcReq.Method {
		return headerMismatch(httpheaders.MethodHeader, method, rpcReq.Method)
	}

	name, namePresent, rejection := singleMirroredHeader(r, httpheaders.NameHeader)
	if rejection != nil {
		return rejection
	}
	if !namePresent {
		return nil
	}

	source, mirrored := httpheaders.MirroredNameField(rpcReq.Method)
	if !mirrored {
		return nil
	}

	var params map[string]json.RawMessage
	if err := json.Unmarshal(rpcReq.Params, &params); err != nil {
		return unverifiableHeader(httpheaders.NameHeader, "request params are not a JSON object")
	}
	var want string
	if err := json.Unmarshal(params[source], &want); err != nil {
		return unverifiableHeader(httpheaders.NameHeader, fmt.Sprintf("request params carry no string %q to compare against", source))
	}
	if name != want {
		return headerMismatch(httpheaders.NameHeader, name, want)
	}
	return nil
}

func singleMirroredHeader(r *http.Request, name string) (string, bool, *RejectError) {
	value, present, err := httpheaders.MirroredValue(r.Header, name)
	switch {
	case errors.Is(err, httpheaders.ErrRepeatedHeader):
		return "", true, &RejectError{
			Code:    RejectCodeHeaderMismatch,
			Message: fmt.Sprintf("%s header is repeated", name),
			Data:    nil,
		}
	case err != nil:
		return "", true, &RejectError{
			Code:    RejectCodeHeaderMismatch,
			Message: fmt.Sprintf("malformed %s header", name),
			Data:    nil,
		}
	default:
		return value, present, nil
	}
}

func unverifiableHeader(header, reason string) *RejectError {
	return &RejectError{
		Code:    RejectCodeHeaderMismatch,
		Message: fmt.Sprintf("header mismatch: %s header cannot be verified — %s", header, reason),
		Data:    nil,
	}
}

func headerMismatch(header, headerValue, bodyValue string) *RejectError {
	return &RejectError{
		Code:    RejectCodeHeaderMismatch,
		Message: fmt.Sprintf("header mismatch: %s header value %q does not match body value %q", header, headerValue, bodyValue),
		Data:    nil,
	}
}
