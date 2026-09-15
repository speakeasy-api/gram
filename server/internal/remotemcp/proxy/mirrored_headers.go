package proxy

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"

	"github.com/modelcontextprotocol/go-sdk/jsonrpc"
)

// MCP 2026-07-28 mirrors selected JSON-RPC fields into HTTP headers. The body
// remains the source of truth, so the final request sent upstream must reject
// any disagreement after interceptors and configured headers have run.
const (
	headerMCPMethod = "Mcp-Method"
	headerMCPName   = "Mcp-Name"

	base64SentinelPrefix = "=?base64?"
	base64SentinelSuffix = "?="

	methodPromptsGet = "prompts/get"
)

func mirroredNameSource(method string) (string, bool) {
	switch method {
	case methodToolsCall, methodPromptsGet:
		return "name", true
	case methodResourcesRead:
		return "uri", true
	default:
		return "", false
	}
}

// validateMirroredHeaders compares the headers on the fully constructed
// upstream request with the final decoded body. Absent headers remain valid for
// protocol revisions that predate request metadata.
func validateMirroredHeaders(r *http.Request, req *UserRequest) *RejectError {
	if r == nil || req == nil || len(req.JSONRPCMessages) != 1 {
		return nil
	}
	rpcReq, ok := req.JSONRPCMessages[0].(*jsonrpc.Request)
	if !ok {
		return nil
	}

	method, methodPresent, rejection := singleMirroredHeader(r, headerMCPMethod)
	if rejection != nil {
		return rejection
	}
	if methodPresent && method != rpcReq.Method {
		return headerMismatch(headerMCPMethod, method, rpcReq.Method)
	}

	name, namePresent, rejection := singleMirroredHeader(r, headerMCPName)
	if rejection != nil {
		return rejection
	}
	if !namePresent {
		return nil
	}

	source, mirrored := mirroredNameSource(rpcReq.Method)
	if !mirrored {
		return nil
	}

	var params map[string]json.RawMessage
	if err := json.Unmarshal(rpcReq.Params, &params); err != nil {
		return unverifiableHeader(headerMCPName, "request params are not a JSON object")
	}
	var want string
	if err := json.Unmarshal(params[source], &want); err != nil {
		return unverifiableHeader(headerMCPName, fmt.Sprintf("request params carry no string %q to compare against", source))
	}
	if name != want {
		return headerMismatch(headerMCPName, name, want)
	}
	return nil
}

func singleMirroredHeader(r *http.Request, name string) (string, bool, *RejectError) {
	values := r.Header.Values(name)
	switch len(values) {
	case 0:
		return "", false, nil
	case 1:
		decoded, err := decodeMirroredHeaderValue(values[0])
		if err != nil {
			return "", true, &RejectError{
				Code:    RejectCodeHeaderMismatch,
				Message: fmt.Sprintf("malformed %s header", name),
				Data:    nil,
			}
		}
		return decoded, true, nil
	default:
		return "", true, &RejectError{
			Code:    RejectCodeHeaderMismatch,
			Message: fmt.Sprintf("%s header is repeated", name),
			Data:    nil,
		}
	}
}

func unverifiableHeader(header, reason string) *RejectError {
	return &RejectError{
		Code:    RejectCodeHeaderMismatch,
		Message: fmt.Sprintf("header mismatch: %s header cannot be verified — %s", header, reason),
		Data:    nil,
	}
}

func decodeMirroredHeaderValue(value string) (string, error) {
	if !strings.HasPrefix(value, base64SentinelPrefix) || !strings.HasSuffix(value, base64SentinelSuffix) {
		return value, nil
	}
	encoded := strings.TrimSuffix(strings.TrimPrefix(value, base64SentinelPrefix), base64SentinelSuffix)
	decoded, err := base64.StdEncoding.DecodeString(encoded)
	if err != nil {
		return "", fmt.Errorf("decode base64 header value: %w", err)
	}
	return string(decoded), nil
}

func headerMismatch(header, headerValue, bodyValue string) *RejectError {
	return &RejectError{
		Code:    RejectCodeHeaderMismatch,
		Message: fmt.Sprintf("header mismatch: %s header value %q does not match body value %q", header, headerValue, bodyValue),
		Data:    nil,
	}
}
