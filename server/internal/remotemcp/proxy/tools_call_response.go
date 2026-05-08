package proxy

import (
	"encoding/json"
	"errors"

	"github.com/modelcontextprotocol/go-sdk/jsonrpc"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// ToolsCallResponse is a "tools/call"-specific view over the remote message
// carrying the response. Instances are constructed by the proxy and passed
// to each [ToolsCallResponseInterceptor] after the generic
// [RemoteMessageInterceptor] chain has run.
type ToolsCallResponse struct {
	// Error is the JSON-RPC protocol error when upstream returned an error
	// response (e.g. "tool not found", "method not found"). Mutually
	// exclusive with Result.
	Error *jsonrpc.Error

	// RemoteMessage is the underlying remote message. Other interceptors in the
	// generic chain may have observed it already.
	RemoteMessage *RemoteMessage

	// Request is the tools/call request this response is replying to.
	// Available so interceptors can correlate input and output without
	// re-parsing.
	Request *ToolsCallRequest

	// Result is the decoded tools/call result when upstream returned a
	// JSON-RPC success response. Check Result.IsError to distinguish
	// tool-level failures (the tool ran and reported an error) from tool-
	// level successes. Mutually exclusive with Error — exactly one of Result
	// and Error is non-nil.
	Result *mcp.CallToolResult
}

// toolsCallResponseFromRemoteMessage returns a ToolsCallResponse view over
// msg if msg carries a JSON-RPC response whose payload decodes cleanly as
// either a [mcp.CallToolResult] or a [jsonrpc.Error]. Anything else
// returns ok=false so the typed interceptor loop is skipped. Decoding
// failures do not abort the proxy; the response is relayed to the user
// unchanged.
//
// Used by both the buffered JSON path and the SSE-terminal path. In both
// cases msg.Message is already a *jsonrpc.Response decoded from the wire;
// the helper just re-decodes its payload as a tools/call shape.
func toolsCallResponseFromRemoteMessage(request *ToolsCallRequest, msg *RemoteMessage) (*ToolsCallResponse, bool) {
	if request == nil || msg == nil {
		return nil, false
	}
	rpcResp, ok := msg.Message.(*jsonrpc.Response)
	if !ok {
		return nil, false
	}

	resp := &ToolsCallResponse{
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

	result := &mcp.CallToolResult{
		Content:           nil,
		IsError:           false,
		Meta:              nil,
		StructuredContent: nil,
	}
	if err := json.Unmarshal(rpcResp.Result, result); err != nil {
		return nil, false
	}
	resp.Result = result
	return resp, true
}
