package proxy

import (
	"encoding/json"

	"github.com/modelcontextprotocol/go-sdk/jsonrpc"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// methodResourcesRead is the MCP JSON-RPC method for resource reads. The
// official SDK keeps this constant unexported (mcp.methodReadResource), so we
// repeat it here rather than depend on the SDK's internal name.
const methodResourcesRead = "resources/read"

// ResourcesReadRequest is a "resources/read"-specific view over a UserRequest.
// Instances are constructed by the proxy and passed to each
// [ResourcesReadRequestInterceptor] after the generic UserRequestInterceptor
// chain has run.
type ResourcesReadRequest struct {
	// Params is the decoded resources/read params. URI is the only
	// spec-defined field and is required; per-protocol validation lives
	// upstream so a malformed payload that nonetheless decodes here is still
	// forwarded.
	Params *mcp.ReadResourceParams

	// UserRequest is the underlying request. Other interceptors in the generic
	// chain may already have observed it. Callers should prefer Params for
	// resources/read-specific data; UserRequest is exposed for RPC-level needs
	// (JSON-RPC ID, raw messages) and for forwarding control via the
	// underlying HTTP request.
	UserRequest *UserRequest
}

// resourcesReadRequestFromUserRequest returns a ResourcesReadRequest if req
// carries exactly one JSON-RPC "resources/read" request whose params decode
// cleanly. Anything else — notifications, responses, multiple messages,
// unrelated methods, malformed params — returns ok=false so the typed
// interceptor loop is skipped. Decoding failures do not abort the proxy; the
// request is forwarded to upstream unchanged so upstream's own validation
// surfaces.
func resourcesReadRequestFromUserRequest(req *UserRequest) (*ResourcesReadRequest, bool) {
	if req == nil || len(req.JSONRPCMessages) != 1 {
		return nil, false
	}
	rpcReq, ok := req.JSONRPCMessages[0].(*jsonrpc.Request)
	if !ok {
		return nil, false
	}
	if rpcReq.Method != methodResourcesRead {
		return nil, false
	}

	params := &mcp.ReadResourceParams{
		Meta: nil,
		URI:  "",
	}
	if err := json.Unmarshal(rpcReq.Params, params); err != nil {
		return nil, false
	}

	return &ResourcesReadRequest{UserRequest: req, Params: params}, true
}
