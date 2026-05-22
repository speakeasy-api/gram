package proxy

import (
	"encoding/json"
	"errors"
	"fmt"

	"github.com/modelcontextprotocol/go-sdk/jsonrpc"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// ResourcesListResponse is a "resources/list"-specific view over the remote
// message carrying the response. Instances are constructed by the proxy and
// passed to each [ResourcesListResponseInterceptor] after the generic
// [RemoteMessageInterceptor] chain has run.
type ResourcesListResponse struct {
	// Error is the JSON-RPC protocol error when upstream returned an error
	// response (e.g. "method not found"). Mutually exclusive with Result.
	Error *jsonrpc.Error

	// RemoteMessage is the underlying remote message. Other interceptors in the
	// generic chain may have observed it already.
	RemoteMessage *RemoteMessage

	// Request is the resources/list request this response is replying to.
	// Available so interceptors can correlate input and output without
	// re-parsing.
	Request *ResourcesListRequest

	// Result is the decoded resources/list result when upstream returned a
	// JSON-RPC success response. Mutually exclusive with Error — exactly one
	// of Result and Error is non-nil.
	Result *mcp.ListResourcesResult
}

// resourcesListResponseFromRemoteMessage returns a ResourcesListResponse view
// over msg if msg carries a JSON-RPC response whose payload decodes cleanly
// as either a [mcp.ListResourcesResult] or a [jsonrpc.Error]. Anything else
// returns ok=false so the typed interceptor loop is skipped. Decoding
// failures do not abort the proxy; the response is relayed to the user
// unchanged.
//
// Used by both the buffered JSON path and the SSE-terminal path. In both
// cases msg.Message is already a *jsonrpc.Response decoded from the wire;
// the helper just re-decodes its payload as a resources/list shape.
func resourcesListResponseFromRemoteMessage(request *ResourcesListRequest, msg *RemoteMessage) (*ResourcesListResponse, bool) {
	if request == nil || msg == nil {
		return nil, false
	}
	rpcResp, ok := msg.Message.(*jsonrpc.Response)
	if !ok {
		return nil, false
	}

	resp := &ResourcesListResponse{
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

	result := &mcp.ListResourcesResult{
		Meta:       nil,
		NextCursor: "",
		Resources:  nil,
	}
	if err := json.Unmarshal(rpcResp.Result, result); err != nil {
		return nil, false
	}
	resp.Result = result
	return resp, true
}

// SetResources replaces the resources array on a successful resources/list
// response, marking the underlying remote message dirty so the proxy
// re-emits the mutated payload to the user. Use this for filter and inject
// patterns: dropping resources that the caller is not authorized to see,
// rewriting URIs, or replacing the array wholesale.
//
// A nil resources argument is normalized to an empty slice so the wire shape
// stays spec-compliant — MCP resources/list responses carry a JSON array,
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
// rather than a Result (mutually exclusive per the typed-view contract),
// when marshaling the mutated ListResourcesResult fails, or when the
// underlying jsonrpc.Message is not a *jsonrpc.Response. The proxy detects
// [*MutationError] at the interceptor return path and surfaces it as an
// HTTP 5xx via [oops.E] with [oops.CodeUnexpected] rather than as a
// user-facing JSON-RPC rejection.
func (r *ResourcesListResponse) SetResources(resources []*mcp.Resource) error {
	if r.Result == nil {
		return &MutationError{Op: "set resources", Cause: errors.New("response carries an error, not a result")}
	}
	rpcResp, ok := r.RemoteMessage.Message.(*jsonrpc.Response)
	if !ok {
		return &MutationError{Op: "set resources", Cause: fmt.Errorf("underlying message is %T, want *jsonrpc.Response", r.RemoteMessage.Message)}
	}

	if resources == nil {
		resources = []*mcp.Resource{}
	}

	// Stage the mutation against a temporary copy of Result so a marshal
	// failure can't leave the typed view's Resources desynced from the
	// underlying wire bytes. Only commit (assign to Result.Resources,
	// rpcResp.Result, dirty) once marshaling has succeeded.
	staged := *r.Result
	staged.Resources = resources
	payload, err := json.Marshal(&staged)
	if err != nil {
		return &MutationError{Op: "set resources", Cause: fmt.Errorf("marshal mutated ListResourcesResult: %w", err)}
	}

	r.Result.Resources = resources
	rpcResp.Result = payload
	r.RemoteMessage.dirty = true
	return nil
}
