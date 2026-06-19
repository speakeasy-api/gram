package proxy

import (
	"encoding/json"
	"errors"
	"fmt"

	"github.com/modelcontextprotocol/go-sdk/jsonrpc"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// ToolsListResponse is a "tools/list"-specific view over the remote message
// carrying the response. Instances are constructed by the proxy and passed
// to each [ToolsListResponseInterceptor] after the generic
// [RemoteMessageInterceptor] chain has run.
type ToolsListResponse struct {
	// Error is the JSON-RPC protocol error when upstream returned an error
	// response (e.g. "method not found"). Mutually exclusive with Result.
	Error *jsonrpc.Error

	// RemoteMessage is the underlying remote message. Other interceptors in the
	// generic chain may have observed it already.
	RemoteMessage *RemoteMessage

	// Request is the tools/list request this response is replying to.
	// Available so interceptors can correlate input and output without
	// re-parsing.
	Request *ToolsListRequest

	// Result is the decoded tools/list result when upstream returned a
	// JSON-RPC success response. Mutually exclusive with Error — exactly one
	// of Result and Error is non-nil.
	Result *mcp.ListToolsResult
}

// toolsListResponseFromRemoteMessage returns a ToolsListResponse view over
// msg if msg carries a JSON-RPC response whose payload decodes cleanly as
// either a [mcp.ListToolsResult] or a [jsonrpc.Error]. Anything else
// returns ok=false so the typed interceptor loop is skipped. Decoding
// failures do not abort the proxy; the response is relayed to the user
// unchanged.
//
// Used by both the buffered JSON path and the SSE-terminal path. In both
// cases msg.Message is already a *jsonrpc.Response decoded from the wire;
// the helper just re-decodes its payload as a tools/list shape.
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
		NextCursor: "",
		Tools:      nil,
	}
	if err := json.Unmarshal(rpcResp.Result, result); err != nil {
		return nil, false
	}
	resp.Result = result
	return resp, true
}

// SetTools replaces the tools array on a successful tools/list response,
// marking the underlying remote message dirty so the proxy re-emits the
// mutated payload to the user. Use this for filter and inject patterns:
// dropping tools that the caller is not authorized to see, injecting
// additional schema fields, or replacing the array wholesale.
//
// A nil tools argument is normalized to an empty slice so the wire shape
// stays spec-compliant — MCP tools/list responses carry a JSON array,
// never null. Use an explicit empty slice or nil interchangeably when a
// filter removes every entry.
//
// The replacement is observed by every subsequent interceptor in the same
// chain through the shared *Result pointer — no re-read of wire bytes is
// required. The outer jsonrpc.Message is re-encoded once after the chain
// completes (see [RemoteMessage.materializedBytes]); this method does the
// inner payload swap up front so the dirty signal alone is sufficient to
// trigger that re-encode. Marshal happens before any typed-view or
// underlying-message state is touched so a marshal failure leaves
// everything at its pre-call values — the typed view and the wire
// remain in sync regardless of the failure mode.
//
// Returns a [*MutationError] when the response carries a JSON-RPC Error
// rather than a Result (mutually exclusive per the typed-view
// contract), when marshaling the mutated ListToolsResult fails, or when
// the underlying jsonrpc.Message is not a *jsonrpc.Response. The proxy
// detects [*MutationError] at the interceptor return path and surfaces
// it as an HTTP 5xx via [oops.E] with [oops.CodeUnexpected] rather than
// as a user-facing JSON-RPC rejection.
func (r *ToolsListResponse) SetTools(tools []*mcp.Tool) error {
	if r.Result == nil {
		return &MutationError{Op: "set tools", Cause: errors.New("response carries an error, not a result")}
	}
	rpcResp, ok := r.RemoteMessage.Message.(*jsonrpc.Response)
	if !ok {
		return &MutationError{Op: "set tools", Cause: fmt.Errorf("underlying message is %T, want *jsonrpc.Response", r.RemoteMessage.Message)}
	}

	if tools == nil {
		tools = []*mcp.Tool{}
	}

	// Stage the mutation against a temporary copy of Result so a marshal
	// failure can't leave the typed view's Tools desynced from the
	// underlying wire bytes. Only commit (assign to Result.Tools,
	// rpcResp.Result, dirty) once marshaling has succeeded.
	staged := *r.Result
	staged.Tools = tools
	payload, err := json.Marshal(&staged)
	if err != nil {
		return &MutationError{Op: "set tools", Cause: fmt.Errorf("marshal mutated ListToolsResult: %w", err)}
	}

	r.Result.Tools = tools
	rpcResp.Result = payload
	r.RemoteMessage.dirty = true
	return nil
}
