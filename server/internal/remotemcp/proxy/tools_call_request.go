package proxy

import (
	"encoding/json"

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
