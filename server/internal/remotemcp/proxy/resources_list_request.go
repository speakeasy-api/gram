package proxy

import (
	"encoding/json"

	"github.com/modelcontextprotocol/go-sdk/jsonrpc"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// methodResourcesList is the MCP JSON-RPC method for resource discovery. The
// official SDK keeps this constant unexported (mcp.methodListResources), so
// we repeat it here rather than depend on the SDK's internal name.
const methodResourcesList = "resources/list"

// ResourcesListRequest is a "resources/list"-specific view over a UserRequest.
// Instances are constructed by the proxy and passed to each
// [ResourcesListRequestInterceptor] after the generic UserRequestInterceptor
// chain has run.
type ResourcesListRequest struct {
	// Params is the decoded resources/list params. Per the MCP spec, params
	// may be omitted entirely for resources/list; in that case Params is a
	// zero-valued [mcp.ListResourcesParams].
	Params *mcp.ListResourcesParams

	// UserRequest is the underlying request. Other interceptors in the generic
	// chain may already have observed it. Callers should prefer Params for
	// resources/list-specific data; UserRequest is exposed for RPC-level needs
	// (JSON-RPC ID, raw messages) and for forwarding control via the
	// underlying HTTP request.
	UserRequest *UserRequest
}

// resourcesListRequestFromUserRequest returns a ResourcesListRequest if req
// carries exactly one JSON-RPC "resources/list" request whose params decode
// cleanly. Anything else — notifications, responses, multiple messages,
// unrelated methods, malformed params — returns ok=false so the typed
// interceptor loop is skipped. Decoding failures do not abort the proxy; the
// request is forwarded to upstream unchanged so upstream's own validation
// surfaces.
//
// Per MCP spec, resources/list params are optional, so a missing or empty
// params payload yields a zero-valued [mcp.ListResourcesParams] rather than a
// decode failure.
func resourcesListRequestFromUserRequest(req *UserRequest) (*ResourcesListRequest, bool) {
	if req == nil || len(req.JSONRPCMessages) != 1 {
		return nil, false
	}
	rpcReq, ok := req.JSONRPCMessages[0].(*jsonrpc.Request)
	if !ok {
		return nil, false
	}
	if rpcReq.Method != methodResourcesList {
		return nil, false
	}

	params := &mcp.ListResourcesParams{
		Cursor: "",
		Meta:   nil,
	}
	if len(rpcReq.Params) > 0 {
		if err := json.Unmarshal(rpcReq.Params, params); err != nil {
			return nil, false
		}
	}

	return &ResourcesListRequest{UserRequest: req, Params: params}, true
}
