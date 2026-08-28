package proxy

import (
	"encoding/json"
	"fmt"

	"github.com/modelcontextprotocol/go-sdk/jsonrpc"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// methodToolsCall is the MCP JSON-RPC method for tool invocation. The
// official SDK keeps this constant unexported (mcp.methodCallTool), so we
// repeat it here rather than depend on the SDK's internal name.
const methodToolsCall = "tools/call"

// ToolsCallRequest is a "tools/call"-specific view over a UserRequest.
// Instances are constructed by the proxy and passed to each
// [ToolsCallRequestInterceptor] after the generic UserRequestInterceptor
// chain has run.
type ToolsCallRequest struct {
	// Params is the decoded tools/call params. Arguments is retained as a
	// [json.RawMessage] so implementations can Unmarshal into tool-specific
	// argument schemas without a double-decode round-trip.
	Params *mcp.CallToolParamsRaw

	// UserRequest is the underlying request. Other interceptors in the generic
	// chain may already have observed it. Callers should prefer Params for
	// tools/call-specific data; UserRequest is exposed for RPC-level needs (JSON-RPC
	// ID, raw messages) and for forwarding control via the underlying HTTP
	// request.
	UserRequest *UserRequest
}

// userRequestMethod returns the JSON-RPC method of a single-message user
// request, or "" for anything else (responses, batch shapes). Used by the
// strict-tool-selection path to distinguish "not a tools/call" from "a
// tools/call whose params failed to decode".
func userRequestMethod(req *UserRequest) string {
	if req == nil || len(req.JSONRPCMessages) != 1 {
		return ""
	}
	if rpcReq, ok := req.JSONRPCMessages[0].(*jsonrpc.Request); ok {
		return rpcReq.Method
	}
	return ""
}

// toolsCallRequestFromUserRequest returns a ToolsCallRequest if req carries
// exactly one JSON-RPC "tools/call" request. A recognized call retains its
// underlying request even when params fail to decode so method-level preflight
// enforcement can run before the proxy rejects the malformed call. Unrelated
// methods and non-request messages return nil, nil.
func toolsCallRequestFromUserRequest(req *UserRequest) (*ToolsCallRequest, error) {
	if req == nil || len(req.JSONRPCMessages) != 1 {
		return nil, nil
	}
	rpcReq, ok := req.JSONRPCMessages[0].(*jsonrpc.Request)
	if !ok {
		return nil, nil
	}
	if rpcReq.Method != methodToolsCall {
		return nil, nil
	}

	call := &ToolsCallRequest{Params: nil, UserRequest: req}
	if firstNonWhitespaceByte(rpcReq.Params) != '{' {
		return call, fmt.Errorf("tools/call params must be a JSON object")
	}

	params := &mcp.CallToolParamsRaw{
		Arguments:      nil,
		Meta:           nil,
		Name:           "",
		InputResponses: nil,
		RequestState:   "",
	}
	if err := json.Unmarshal(rpcReq.Params, params); err != nil {
		return call, fmt.Errorf("decode tools/call params: %w", err)
	}

	call.Params = params
	return call, nil
}

// SetArguments replaces the arguments payload on a tools/call request,
// marking the underlying user request dirty so the proxy forwards the
// mutated body upstream. Use this for inject and scrub patterns: stripping
// proxy-only properties before forwarding to the tool, normalizing
// argument shapes, or rewriting wholesale.
//
// arguments must be a JSON object payload that the upstream tool can
// unmarshal against its declared input schema; a nil or empty arguments
// removes the member from the forwarded params, mirroring the omitempty
// encoding of the field. The replacement is observed by every subsequent
// interceptor in the same chain through the shared *Params pointer — no
// re-read of wire bytes is required. The outer jsonrpc.Message is
// re-encoded once after the chain completes (see
// [UserRequest.refreshBody]); this method does the inner payload swap up
// front so the dirty signal alone is sufficient to trigger that
// re-encode. Only the params' arguments member is rewritten: the
// replacement is spliced into the original wire payload, so _meta, name,
// and members not modeled by [mcp.CallToolParamsRaw] retain their
// original values instead of being dropped by a typed re-marshal. The
// splice happens before any typed-view or underlying-message state is
// touched so a failure leaves everything at its pre-call values — the
// typed view and the wire remain in sync regardless of the failure mode.
//
// Returns a [*MutationError] when the underlying jsonrpc.Message is not
// a *jsonrpc.Request or when splicing the replacement arguments — which
// also validates that arguments is well-formed JSON — fails. The proxy
// detects [*MutationError] at the interceptor return path and surfaces
// it as an HTTP 5xx via [oops.E] with [oops.CodeUnexpected] rather than
// as a user-facing JSON-RPC rejection.
func (r *ToolsCallRequest) SetArguments(arguments json.RawMessage) error {
	rpcReq, ok := r.UserRequest.JSONRPCMessages[0].(*jsonrpc.Request)
	if !ok {
		return &MutationError{Op: "set arguments", Cause: fmt.Errorf("underlying message is %T, want *jsonrpc.Request", r.UserRequest.JSONRPCMessages[0])}
	}

	// Splice the replacement into the original wire payload so a failure
	// can't leave the typed view's Arguments desynced from the underlying
	// wire bytes. Only commit (assign to Params.Arguments, rpcReq.Params,
	// dirty) once splicing has succeeded.
	payload, err := spliceTopLevelKey(rpcReq.Params, "arguments", arguments)
	if err != nil {
		return &MutationError{Op: "set arguments", Cause: fmt.Errorf("splice replacement arguments: %w", err)}
	}

	r.Params.Arguments = arguments
	rpcReq.Params = payload
	r.UserRequest.dirty = true
	return nil
}
