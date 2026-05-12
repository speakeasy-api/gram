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

// toolsCallRequestFromUserRequest returns a ToolsCallRequest if req carries
// exactly one JSON-RPC "tools/call" request whose params decode cleanly.
// Anything else — notifications, responses, multiple messages, unrelated
// methods, malformed params — returns ok=false so the typed interceptor loop
// is skipped. Decoding failures do not abort the proxy; the request is
// forwarded to upstream unchanged so upstream's own validation surfaces.
func toolsCallRequestFromUserRequest(req *UserRequest) (*ToolsCallRequest, bool) {
	if req == nil || len(req.JSONRPCMessages) != 1 {
		return nil, false
	}
	rpcReq, ok := req.JSONRPCMessages[0].(*jsonrpc.Request)
	if !ok {
		return nil, false
	}
	if rpcReq.Method != methodToolsCall {
		return nil, false
	}

	params := &mcp.CallToolParamsRaw{
		Arguments: nil,
		Meta:      nil,
		Name:      "",
	}
	if err := json.Unmarshal(rpcReq.Params, params); err != nil {
		return nil, false
	}

	return &ToolsCallRequest{UserRequest: req, Params: params}, true
}

// SetArguments replaces the arguments payload on a tools/call request,
// marking the underlying user request dirty so the proxy forwards the
// mutated body upstream. Use this for inject and scrub patterns: stripping
// proxy-only properties before forwarding to the tool, normalizing
// argument shapes, or rewriting wholesale.
//
// arguments must be a JSON object payload that the upstream tool can
// unmarshal against its declared input schema. The replacement is
// observed by every subsequent interceptor in the same chain through the
// shared *Params pointer — no re-read of wire bytes is required. The
// outer jsonrpc.Message is re-encoded once after the chain completes (see
// [UserRequest.refreshBody]); this method does the inner payload swap up
// front so the dirty signal alone is sufficient to trigger that
// re-encode. Marshal happens before any typed-view or underlying-message
// state is touched so a marshal failure leaves everything at its
// pre-call values — the typed view and the wire remain in sync
// regardless of the failure mode.
//
// Returns a [*MutationError] when the underlying jsonrpc.Message is not
// a *jsonrpc.Request or when marshaling the mutated CallToolParamsRaw
// fails. The proxy detects [*MutationError] at the interceptor return
// path and surfaces it as an HTTP 5xx via [oops.E] with
// [oops.CodeUnexpected] rather than as a user-facing JSON-RPC rejection.
func (r *ToolsCallRequest) SetArguments(arguments json.RawMessage) error {
	rpcReq, ok := r.UserRequest.JSONRPCMessages[0].(*jsonrpc.Request)
	if !ok {
		return &MutationError{Op: "set arguments", Cause: fmt.Errorf("underlying message is %T, want *jsonrpc.Request", r.UserRequest.JSONRPCMessages[0])}
	}

	// Stage the mutation against a temporary copy of Params so a marshal
	// failure can't leave the typed view's Arguments desynced from the
	// underlying wire bytes. Only commit (assign to Params.Arguments,
	// rpcReq.Params, dirty) once marshaling has succeeded.
	staged := *r.Params
	staged.Arguments = arguments
	payload, err := json.Marshal(&staged)
	if err != nil {
		return &MutationError{Op: "set arguments", Cause: fmt.Errorf("marshal mutated CallToolParamsRaw: %w", err)}
	}

	r.Params.Arguments = arguments
	rpcReq.Params = payload
	r.UserRequest.dirty = true
	return nil
}
