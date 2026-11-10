package proxy

import (
	"encoding/json"
	"errors"
	"fmt"

	"github.com/modelcontextprotocol/go-sdk/jsonrpc"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// ToolsListResponse is a "tools/list"-specific view over the remote message
// carrying the response. The proxy constructs it and passes it to each
// [ToolsListResponseInterceptor] after the generic [RemoteMessageInterceptor]
// chain has run.
type ToolsListResponse struct {
	// Error is the JSON-RPC protocol error when upstream returned an error
	// response (e.g. "method not found"). Exactly one of Error and Result is
	// non-nil.
	Error *jsonrpc.Error

	// RemoteMessage is the underlying remote message, which the generic
	// interceptor chain may already have observed.
	RemoteMessage *RemoteMessage

	// Request is the tools/list request this response replies to, so
	// interceptors can correlate input and output without re-parsing.
	Request *ToolsListRequest

	// Result is the decoded tools/list result when upstream returned a
	// JSON-RPC success response. Exactly one of Error and Result is non-nil.
	Result *mcp.ListToolsResult
}

// toolsListResponseFromRemoteMessage returns a ToolsListResponse view over
// msg if msg carries a JSON-RPC response whose payload decodes as either a
// [mcp.ListToolsResult] or a [jsonrpc.Error]. Anything else returns ok=false,
// skipping the typed interceptor loop and relaying the response unchanged.
//
// Both the buffered JSON path and the SSE-terminal path use this. In both,
// msg.Message is already a *jsonrpc.Response decoded from the wire, so the
// helper only re-decodes its payload as a tools/list shape.
func toolsListResponseFromRemoteMessage(request *ToolsListRequest, msg *RemoteMessage) (*ToolsListResponse, bool) {
	if request == nil || msg == nil {
		return nil, false
	}
	rpcResp, ok := msg.Message.(*jsonrpc.Response)
	if !ok {
		return nil, false
	}

	resp := &ToolsListResponse{
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

	result := &mcp.ListToolsResult{
		Meta:       nil,
		Cacheable:  mcp.Cacheable{TTLMs: 0, CacheScope: ""},
		NextCursor: "",
		Tools:      nil,
	}
	if err := json.Unmarshal(rpcResp.Result, result); err != nil {
		return nil, false
	}
	resp.Result = result
	return resp, true
}

// SetTools replaces the tools array on a successful tools/list response and
// marks the underlying remote message dirty so the proxy re-emits the mutated
// payload. Use it for filter and inject patterns: dropping tools the caller is
// not authorized to see, injecting schema fields, or replacing the array
// wholesale. A nil tools argument is normalized to an empty slice, since MCP
// tools/list responses carry a JSON array and never null.
//
// Only the tools member is rewritten. The replacement array is spliced into
// the original wire payload, so _meta, nextCursor, and members not modeled by
// [mcp.ListToolsResult] (under MCP 2026-07-28, the required resultType, ttlMs,
// and cacheScope) keep their original values instead of being dropped by a
// typed re-marshal. Later interceptors in the same chain see the replacement
// through the shared *Result pointer.
//
// Returns a [*MutationError] when the response carries a JSON-RPC Error rather
// than a Result, when it carries no remote message, when the underlying
// jsonrpc.Message is not a *jsonrpc.Response, or when marshaling or splicing
// the replacement fails. Nothing is mutated on those paths, so the typed view
// and the wire stay in sync. The proxy surfaces a [*MutationError] as an HTTP
// 5xx via [oops.E] with [oops.CodeUnexpected] rather than as a user-facing
// JSON-RPC rejection.
func (r *ToolsListResponse) SetTools(tools []*mcp.Tool) error {
	if r.Result == nil {
		return &MutationError{Op: "set tools", Cause: errors.New("response carries an error, not a result")}
	}
	if r.RemoteMessage == nil {
		return &MutationError{Op: "set tools", Cause: errors.New("response carries no remote message")}
	}
	rpcResp, ok := r.RemoteMessage.Message.(*jsonrpc.Response)
	if !ok {
		return &MutationError{Op: "set tools", Cause: fmt.Errorf("underlying message is %T, want *jsonrpc.Response", r.RemoteMessage.Message)}
	}

	if tools == nil {
		tools = []*mcp.Tool{}
	}

	// Marshal and splice before touching any state, so a failure can't leave
	// the typed view's Tools desynced from the underlying wire bytes.
	payload, err := marshalJSONNoHTMLEscape(tools)
	if err != nil {
		return &MutationError{Op: "set tools", Cause: fmt.Errorf("marshal replacement tools array: %w", err)}
	}
	result, err := spliceTopLevelKey(rpcResp.Result, "tools", payload)
	if err != nil {
		return &MutationError{Op: "set tools", Cause: fmt.Errorf("splice replacement tools array: %w", err)}
	}

	r.Result.Tools = tools
	rpcResp.Result = result
	r.RemoteMessage.dirty = true
	return nil
}
