package proxy

import (
	"context"
	"encoding/json"

	"github.com/modelcontextprotocol/go-sdk/jsonrpc"
)

// RequestServed describes a forwarded POST the remote MCP server answered
// without an error: a 2xx whose reply, if any, is neither a JSON-RPC error nor
// a tools/call result marked isError.
type RequestServed struct {
	// Method is the JSON-RPC method, empty for a forwarded JSON-RPC response.
	Method string
}

// RequestServedInterceptor is told about each [RequestServed] once its reply
// was relayed. It only observes. Requests Speakeasy refused, and GET or DELETE
// requests, never reach it.
type RequestServedInterceptor interface {
	// InterceptRequestServed is called with the served request.
	InterceptRequestServed(ctx context.Context, served *RequestServed)

	// Name returns a stable identifier for this interceptor, used for tracing
	// span attributes and log correlation.
	Name() string
}

// replyFailed reports whether a reply is a JSON-RPC error, or a tools/call
// result that is marked isError or does not decode.
func replyFailed(response *jsonrpc.Response, method string) bool {
	if response.Error != nil {
		return true
	}
	if method != methodToolsCall {
		return false
	}
	var result struct {
		IsError bool `json:"isError"`
	}
	if json.Unmarshal(response.Result, &result) != nil {
		return true
	}
	return result.IsError
}

// servedBy reports whether msg, relayed after a 2xx, is a reply to id without
// an error. A request answered with no reply message counts as served.
func servedBy(msg jsonrpc.Message, id jsonrpc.ID, method string) bool {
	if msg == nil {
		return true
	}
	response, ok := msg.(*jsonrpc.Response)
	return ok && id.IsValid() && jsonrpcIDsEqual(response.ID, id) && !replyFailed(response, method)
}
