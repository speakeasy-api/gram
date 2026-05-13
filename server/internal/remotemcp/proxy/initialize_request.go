package proxy

import (
	"encoding/json"

	"github.com/modelcontextprotocol/go-sdk/jsonrpc"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// methodInitialize is the MCP JSON-RPC method that opens a session. The
// official SDK keeps this constant unexported (mcp.methodInitialize), so we
// repeat it here rather than depend on the SDK's internal name.
const methodInitialize = "initialize"

// InitializeRequest is an "initialize"-specific view over a UserRequest.
// Instances are constructed by the proxy and passed to each
// [InitializeRequestInterceptor] after the generic UserRequestInterceptor
// chain has run.
type InitializeRequest struct {
	// Params is the decoded initialize params, exposing the negotiated
	// protocol version, client capabilities, and client info. Per the MCP
	// spec, params is required on initialize, so a missing or malformed
	// payload causes the typed dispatch to be skipped (see
	// [initializeRequestFromUserRequest]).
	Params *mcp.InitializeParams

	// UserRequest is the underlying request. Other interceptors in the generic
	// chain may already have observed it. Callers should prefer Params for
	// initialize-specific data; UserRequest is exposed for RPC-level needs
	// (JSON-RPC ID, raw messages) and for forwarding control via the
	// underlying HTTP request.
	UserRequest *UserRequest
}

// initializeRequestFromUserRequest returns an InitializeRequest if req
// carries exactly one JSON-RPC "initialize" request whose params decode
// cleanly. Anything else — notifications, responses, multiple messages,
// unrelated methods, malformed params — returns ok=false so the typed
// interceptor loop is skipped. Decoding failures do not abort the proxy; the
// request is forwarded to upstream unchanged so upstream's own validation
// surfaces.
func initializeRequestFromUserRequest(req *UserRequest) (*InitializeRequest, bool) {
	if req == nil || len(req.JSONRPCMessages) != 1 {
		return nil, false
	}
	rpcReq, ok := req.JSONRPCMessages[0].(*jsonrpc.Request)
	if !ok {
		return nil, false
	}
	if rpcReq.Method != methodInitialize {
		return nil, false
	}

	params := &mcp.InitializeParams{
		Capabilities:    nil,
		ClientInfo:      nil,
		Meta:            nil,
		ProtocolVersion: "",
	}
	if len(rpcReq.Params) > 0 {
		if err := json.Unmarshal(rpcReq.Params, params); err != nil {
			return nil, false
		}
	}

	return &InitializeRequest{UserRequest: req, Params: params}, true
}
