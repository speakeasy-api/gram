package proxy

import (
	"encoding/json"
	"errors"
	"fmt"
	"strconv"

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
	return r.setTools(tools, false)
}

// SetPrivateTools replaces the tools array and labels the rewritten result
// caller-varying. Use it for per-principal filters: MCP defaults an absent
// cacheScope to public, so an unlabelled result could let a shared
// intermediary serve one caller's filtered inventory to another.
//
// [ToolsListResponse.MarkCallerVarying] applies the same label when the tools
// array itself needs no rewrite.
func (r *ToolsListResponse) SetPrivateTools(tools []*mcp.Tool) error {
	return r.setTools(tools, true)
}

// callerVaryingCacheable is the cache stance for a tools/list result whose
// content depends on who asked, matching the hosted surface's
// cacheHintsCallerVarying so both paths label the same property identically.
// The zero ttlMs is part of the stance: an upstream's own ttl would let the
// requesting client keep serving a filtered inventory from cache after the
// grants that shaped it were revoked.
var callerVaryingCacheable = mcp.Cacheable{TTLMs: 0, CacheScope: "private"}

// spliceCallerVaryingHints overwrites both caching members of a tools/list
// result payload with [callerVaryingCacheable]. Overwrite rather than fill: an
// upstream declaring its own result public and long-lived is describing its
// own caller-uniformity and cannot account for the RBAC layer Gram puts in
// front of it.
//
// Both members go in on one splice, since a chained pair would re-decode the
// whole result, tools array included, a second time. A result payload of
// literal null becomes an object carrying only these two members, per
// [spliceTopLevelKeys]; upstream was already non-conformant there, because
// tools is a required member.
func spliceCallerVaryingHints(result json.RawMessage) (json.RawMessage, error) {
	spliced, err := spliceTopLevelKeys(result, map[string]json.RawMessage{
		"cacheScope": json.RawMessage(strconv.Quote(callerVaryingCacheable.CacheScope)),
		"ttlMs":      json.RawMessage(strconv.Itoa(callerVaryingCacheable.TTLMs)),
	})
	if err != nil {
		return nil, fmt.Errorf("mark result caller-varying: %w", err)
	}
	return spliced, nil
}

// MarkCallerVarying labels the result caller-varying without rewriting the
// tools array. Use it when a per-principal filter is in force but left the
// catalog intact: that result is still shaped by the caller's grants, and it
// is the widest such result, so a shared cache populated from it would serve a
// complete inventory to a caller whose grants cover less.
//
// Only the two caching members are spliced, so the tools member keeps its
// original wire bytes and per-tool members the SDK does not model survive. The
// rewrite still flips the message dirty, re-encoding the JSON-RPC envelope, so
// envelope member order, insignificant whitespace, and unknown envelope
// members are lost. That is the same cost the filtered path already accepts.
//
// Returns a [*MutationError] when the response carries a JSON-RPC Error rather
// than a Result, when it carries no remote message, when the underlying
// jsonrpc.Message is not a *jsonrpc.Response, or when the splice fails.
// Nothing is mutated on those paths, so the typed view and the wire stay in
// sync.
func (r *ToolsListResponse) MarkCallerVarying() error {
	if r.Result == nil {
		return &MutationError{Op: "mark caller-varying", Cause: errors.New("response carries an error, not a result")}
	}
	if r.RemoteMessage == nil {
		return &MutationError{Op: "mark caller-varying", Cause: errors.New("response carries no remote message")}
	}
	rpcResp, ok := r.RemoteMessage.Message.(*jsonrpc.Response)
	if !ok {
		return &MutationError{Op: "mark caller-varying", Cause: fmt.Errorf("underlying message is %T, want *jsonrpc.Response", r.RemoteMessage.Message)}
	}

	result, err := spliceCallerVaryingHints(rpcResp.Result)
	if err != nil {
		return &MutationError{Op: "mark caller-varying", Cause: err}
	}

	r.Result.Cacheable = callerVaryingCacheable
	rpcResp.Result = result
	r.RemoteMessage.dirty = true
	return nil
}

func (r *ToolsListResponse) setTools(tools []*mcp.Tool, private bool) error {
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
	if private {
		result, err = spliceCallerVaryingHints(result)
		if err != nil {
			return &MutationError{Op: "set tools", Cause: err}
		}
	}

	r.Result.Tools = tools
	if private {
		r.Result.Cacheable = callerVaryingCacheable
	}
	rpcResp.Result = result
	r.RemoteMessage.dirty = true
	return nil
}
