package proxy

import (
	"encoding/json"
	"errors"

	"github.com/modelcontextprotocol/go-sdk/jsonrpc"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// ResourcesReadResponse is a "resources/read"-specific view over the remote
// message carrying the response. Instances are constructed by the proxy and
// passed to each [ResourcesReadResponseInterceptor] after the generic
// [RemoteMessageInterceptor] chain has run.
type ResourcesReadResponse struct {
	// Error is the JSON-RPC protocol error when upstream returned an error
	// response (e.g. "resource not found", "method not found"). Mutually
	// exclusive with Result.
	Error *jsonrpc.Error

	// RemoteMessage is the underlying remote message. Other interceptors in the
	// generic chain may have observed it already.
	RemoteMessage *RemoteMessage

	// Request is the resources/read request this response is replying to.
	// Available so interceptors can correlate input and output without
	// re-parsing.
	Request *ResourcesReadRequest

	// Result is the decoded resources/read result when upstream returned a
	// JSON-RPC success response. Mutually exclusive with Error — exactly one
	// of Result and Error is non-nil.
	Result *mcp.ReadResourceResult
}

// resourcesReadResponseFromRemoteMessage returns a ResourcesReadResponse view
// over msg if msg carries a JSON-RPC response whose payload decodes cleanly
// as either a [mcp.ReadResourceResult] or a [jsonrpc.Error]. Anything else
// returns ok=false so the typed interceptor loop is skipped. Decoding
// failures do not abort the proxy; the response is relayed to the user
// unchanged.
//
// Used by both the buffered JSON path and the SSE-terminal path. In both
// cases msg.Message is already a *jsonrpc.Response decoded from the wire;
// the helper just re-decodes its payload as a resources/read shape.
func resourcesReadResponseFromRemoteMessage(request *ResourcesReadRequest, msg *RemoteMessage) (*ResourcesReadResponse, bool) {
	if request == nil || msg == nil {
		return nil, false
	}
	rpcResp, ok := msg.Message.(*jsonrpc.Response)
	if !ok {
		return nil, false
	}

	resp := &ResourcesReadResponse{
		Error:         nil,
		RemoteMessage: msg,
		Request:       request,
		Result:        nil,
	}

	if rpcResp.Error != nil {
		var wireErr *jsonrpc.Error
		if !errors.As(rpcResp.Error, &wireErr) {
			return nil, false
		}
		resp.Error = wireErr
		return resp, true
	}

	result := &mcp.ReadResourceResult{
		Meta:     nil,
		Contents: nil,
	}
	if err := json.Unmarshal(rpcResp.Result, result); err != nil {
		return nil, false
	}
	resp.Result = result
	return resp, true
}
