package proxy

import (
	"context"
	"encoding/json"

	"github.com/modelcontextprotocol/go-sdk/jsonrpc"
)

// RequestServedInterceptor is told once about each forwarded POST the remote
// MCP server answered without an error, after the reply was relayed: a 2xx
// whose reply is neither a JSON-RPC error nor a tools/call result marked
// isError, or a 2xx with no reply to a request owed none. It only observes.
// Requests Speakeasy refused, and GET or DELETE requests, never reach it.
type RequestServedInterceptor interface {
	// InterceptRequestServed is called for the served request.
	InterceptRequestServed(ctx context.Context)

	// Name returns a stable identifier for this interceptor, used for log
	// correlation.
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
