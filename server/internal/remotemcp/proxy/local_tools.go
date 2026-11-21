package proxy

import (
	"context"
	"encoding/json"
	"fmt"
	"maps"
	"net/http"

	"github.com/modelcontextprotocol/go-sdk/jsonrpc"
	"github.com/modelcontextprotocol/go-sdk/mcp"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/trace"

	"github.com/speakeasy-api/gram/server/internal/attr"
	"github.com/speakeasy-api/gram/server/internal/oops"
)

// LocalTool is a tool the proxy serves itself instead of forwarding to the
// remote MCP server. It is added to a tools/list result after every tools/list
// interceptor has run, and a tools/call naming it is answered instead of the
// typed tools/call interceptors running. The policies those interceptors apply
// to the remote server's tools therefore do not apply to it: it is neither
// filtered by per-tool access or a session's tool selection, nor billed, nor
// logged as a call of the remote server. The policies that apply to every call
// whatever answers it, such as request risk policies, run as
// [Proxy.LocalToolCallInterceptors].
type LocalTool struct {
	// Definition is the tool as clients see it. Its name is taken on this
	// proxy: a remote tool with the same name is left out of tools/list, and a
	// call to that name is never forwarded.
	Definition *mcp.Tool

	// Call answers one tools/call. An error rejects the call the way a
	// tools/call request interceptor's error does.
	Call func(ctx context.Context, call *ToolsCallRequest) (*mcp.CallToolResult, error)
}

// LocalToolsResolver loads the tools a proxy serves itself. The proxy calls it
// for tools/list and tools/call requests only, at most once per request, so
// every other request costs nothing.
type LocalToolsResolver func(ctx context.Context) ([]LocalTool, error)

// resolveLocalTools loads the local tools. A failure serves the request
// without them: local tools are extras, and the remote server's own tools
// must stay listed and callable when they cannot be loaded.
func (p *Proxy) resolveLocalTools(ctx context.Context) []LocalTool {
	if p.LocalTools == nil {
		return nil
	}

	tools, err := p.LocalTools(ctx)
	if err != nil {
		p.Logger.ErrorContext(ctx, "load local tools; serving without them",
			attr.SlogError(err),
			attr.SlogComponent("remotemcp.proxy"))
		return nil
	}

	return tools
}

// localToolNamed returns the local tool a tools/call names, if there is one.
func (p *Proxy) localToolNamed(ctx context.Context, name string) (LocalTool, bool) {
	for _, tool := range p.resolveLocalTools(ctx) {
		if tool.Definition != nil && tool.Call != nil && tool.Definition.Name == name {
			return tool, true
		}
	}

	return LocalTool{Definition: nil, Call: nil}, false
}

// listLocalTools gives the local tools their names on a tools/list result: it
// leaves out remote tools that share a name with one and, on the first page,
// appends the local definitions. Later pages only lose the shadowed names, so
// a paginating client sees each local tool once.
func (p *Proxy) listLocalTools(ctx context.Context, list *ToolsListResponse) error {
	if list.Result == nil {
		return nil
	}

	tools := p.resolveLocalTools(ctx)
	if len(tools) == 0 {
		return nil
	}

	taken := make(map[string]struct{}, len(tools))
	definitions := make([]*mcp.Tool, 0, len(tools))
	for _, tool := range tools {
		if tool.Definition == nil || tool.Call == nil {
			continue
		}
		// Calls go to the first tool with a name, so only that one is listed.
		if _, listed := taken[tool.Definition.Name]; listed {
			continue
		}
		taken[tool.Definition.Name] = struct{}{}
		definitions = append(definitions, tool.Definition)
	}

	firstPage := list.Request == nil || list.Request.Params == nil || list.Request.Params.Cursor == ""
	if !firstPage {
		definitions = nil
	}

	return list.replaceTakenTools(taken, definitions)
}

// answerLocalToolCall answers a tools/call for a tool the proxy serves itself.
// The remote MCP server is never contacted. The local tool call interceptors
// run first, and a refusal from one answers the call instead.
func (p *Proxy) answerLocalToolCall(
	ctx context.Context,
	w http.ResponseWriter,
	span trace.Span,
	id jsonrpc.ID,
	call *ToolsCallRequest,
	tool LocalTool,
	responseBytes *int64,
) error {
	if err := p.runToolsCallRequestInterceptorChain(ctx, call, p.LocalToolCallInterceptors); err != nil {
		return p.dispatchInterceptorError(ctx, w, span, id, err, responseBytes)
	}

	callCtx, callSpan := p.Tracer.Start(ctx, "remotemcp.proxy.LocalTool", trace.WithAttributes(attr.ToolName(tool.Definition.Name)))
	result, err := tool.Call(callCtx, call)
	if err != nil {
		callSpan.SetStatus(codes.Error, err.Error())
		callSpan.RecordError(err, trace.WithStackTrace(true))
		callSpan.End()
		return p.dispatchInterceptorError(ctx, w, span, id, err, responseBytes)
	}
	callSpan.End()

	payload, response, err := marshalLocalToolResult(id, result)
	if err != nil {
		return oops.E(oops.CodeUnexpected, err, "encode local tool result").LogError(ctx, p.Logger)
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	n, writeErr := w.Write(payload)
	if writeErr != nil {
		p.Logger.WarnContext(ctx, "failed to write local tool result to client",
			attr.SlogError(writeErr),
			attr.SlogComponent("remotemcp.proxy"))
	}
	*responseBytes = int64(n)
	// The request metric counts the answer; it is not the remote server
	// serving the request, so the connection is not marked as served.
	observeReply(ctx, response, id, methodToolsCall, false)

	return nil
}

// localToolResultType is the resultType of a local tool's result: MCP
// 2026-07-28 requires one on every result, and a local tool answers in full.
const localToolResultType = `"complete"`

// localToolServerInfoKey is where MCP 2026-07-28 puts the identity of the
// server that produced a result, under the result's _meta.
const localToolServerInfoKey = "io.modelcontextprotocol/serverInfo"

// localToolServerInfo names Speakeasy as the server that produced a local
// tool's result, the way Speakeasy names itself on the results it produces
// for hosted servers.
var localToolServerInfo = map[string]any{"name": "Speakeasy", "version": "0.0.0"}

// marshalLocalToolResult produces the tools/call response carrying result,
// both as wire bytes and as the decoded message the request observer reads.
// It adds the members MCP 2026-07-28 requires on every result, which earlier
// revisions permit, so they are always sent.
func marshalLocalToolResult(id jsonrpc.ID, result *mcp.CallToolResult) ([]byte, *jsonrpc.Response, error) {
	if result == nil {
		return nil, nil, fmt.Errorf("local tool returned no result")
	}

	stamped := *result
	stamped.Meta = make(mcp.Meta, len(result.Meta)+1)
	maps.Copy(stamped.Meta, result.Meta)
	if _, ok := stamped.Meta[localToolServerInfoKey]; !ok {
		stamped.Meta[localToolServerInfoKey] = localToolServerInfo
	}

	marshaled, err := json.Marshal(&stamped)
	if err != nil {
		return nil, nil, fmt.Errorf("marshal local tool result: %w", err)
	}
	encoded, err := spliceTopLevelKey(marshaled, "resultType", json.RawMessage(localToolResultType))
	if err != nil {
		return nil, nil, fmt.Errorf("add local tool result type: %w", err)
	}

	response := &jsonrpc.Response{ID: id, Result: encoded, Error: nil, Extra: nil}
	payload, err := jsonrpc.EncodeMessage(response)
	if err != nil {
		return nil, nil, fmt.Errorf("encode local tool response: %w", err)
	}

	return payload, response, nil
}
