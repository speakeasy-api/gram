package remotemcp_test

import (
	"encoding/json"
	"errors"
	"testing"

	"github.com/modelcontextprotocol/go-sdk/jsonrpc"
	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/authz"
	"github.com/speakeasy-api/gram/server/internal/authztest"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/remotemcp"
	"github.com/speakeasy-api/gram/server/internal/remotemcp/proxy"
	"github.com/speakeasy-api/gram/server/internal/testenv"
)

// newToolsListResponse constructs a typed view with the given tools and
// a fresh RemoteMessage backing the SetTools setter. The RemoteMessage
// carries a *jsonrpc.Response whose Result is set to a marshaled
// ListToolsResult so SetTools can splice its mutation into a real wire
// payload, matching the production invariant that the typed view only
// exists when those bytes decoded successfully.
func newToolsListResponse(t *testing.T, tools []*mcp.Tool) *proxy.ToolsListResponse {
	t.Helper()

	result := &mcp.ListToolsResult{
		Meta:       nil,
		NextCursor: "",
		Tools:      tools,
	}
	payload, err := json.Marshal(result)
	require.NoError(t, err)
	rpcResp := &jsonrpc.Response{
		ID:     jsonrpc.ID{},
		Result: payload,
		Error:  nil,
	}
	return &proxy.ToolsListResponse{
		Error: nil,
		RemoteMessage: &proxy.RemoteMessage{
			UserHTTPRequest:    nil,
			RemoteHTTPRequest:  nil,
			RemoteHTTPResponse: nil,
			Message:            rpcResp,
		},
		Request: nil,
		Result:  result,
	}
}

// newToolsListResponseFromWire constructs a typed view over a raw result
// payload, mirroring how the proxy builds one from upstream bytes. Use it
// when the fixture needs members mcp.ListToolsResult does not model.
func newToolsListResponseFromWire(t *testing.T, payload string) *proxy.ToolsListResponse {
	t.Helper()

	result := &mcp.ListToolsResult{
		Meta:       nil,
		Cacheable:  mcp.Cacheable{TTLMs: 0, CacheScope: ""},
		NextCursor: "",
		Tools:      nil,
	}
	require.NoError(t, json.Unmarshal([]byte(payload), result))
	rpcResp := &jsonrpc.Response{
		ID:     jsonrpc.ID{},
		Result: json.RawMessage(payload),
		Error:  nil,
	}
	return &proxy.ToolsListResponse{
		Error: nil,
		RemoteMessage: &proxy.RemoteMessage{
			UserHTTPRequest:    nil,
			RemoteHTTPRequest:  nil,
			RemoteHTTPResponse: nil,
			Message:            rpcResp,
		},
		Request: nil,
		Result:  result,
	}
}

// requireCallerVarying asserts the wire payload carries the caller-varying
// cache stance: private scope and no ttl. Both members are checked because
// a private result with an inherited upstream ttl still lets the requesting
// user's own client serve a filtered inventory past a grant revocation.
func requireCallerVarying(t *testing.T, resp *proxy.ToolsListResponse) {
	t.Helper()

	rpcResp, ok := resp.RemoteMessage.Message.(*jsonrpc.Response)
	require.True(t, ok)
	var wire map[string]json.RawMessage
	require.NoError(t, json.Unmarshal(rpcResp.Result, &wire))
	require.JSONEq(t, `"private"`, string(wire["cacheScope"]),
		"an RBAC-scoped catalog must never use the public cache default")
	require.JSONEq(t, `0`, string(wire["ttlMs"]),
		"an RBAC-scoped catalog must not inherit an upstream ttl")
	require.Equal(t, "private", resp.Result.CacheScope,
		"the typed view must agree with the wire")
	require.Equal(t, 0, resp.Result.TTLMs,
		"the typed view must agree with the wire")
}

func TestToolsListMCPConnectFilterInterceptor_Name(t *testing.T) {
	t.Parallel()

	interceptor := remotemcp.NewToolsListMCPConnectFilterInterceptor(newAuthzEngineForTest(t), emptyResolver(), testServerID, testProjectID, testenv.NewLogger(t))
	require.Equal(t, "tools-list-mcp-connect-filter", interceptor.Name())
}

func TestToolsListMCPConnectFilterInterceptor_NilEngineKeepsToolsButLabels(t *testing.T) {
	t.Parallel()

	// A nil engine must not panic and must not drop tools. It is still
	// labelled: the interceptor is only attached to private-visibility
	// servers, so the catalog describes what this caller may reach even
	// when no grants could be evaluated.
	interceptor := remotemcp.NewToolsListMCPConnectFilterInterceptor(nil, emptyResolver(), testServerID, testProjectID, testenv.NewLogger(t))

	resp := newToolsListResponse(t, []*mcp.Tool{
		{Name: "tool_a", InputSchema: map[string]any{}},
		{Name: "tool_b", InputSchema: map[string]any{}},
	})
	require.NoError(t, interceptor.InterceptToolsListResponse(t.Context(), resp))
	require.Len(t, resp.Result.Tools, 2, "nil engine must leave the tools array unchanged")
	requireCallerVarying(t, resp)
}

func TestToolsListMCPConnectFilterInterceptor_ErrorResponseUntouched(t *testing.T) {
	t.Parallel()

	// A JSON-RPC error carries no inventory, so there is nothing to label
	// and no result to splice into.
	interceptor := remotemcp.NewToolsListMCPConnectFilterInterceptor(newAuthzEngineForTest(t), emptyResolver(), testServerID, testProjectID, testenv.NewLogger(t))

	resp := newToolsListResponse(t, nil)
	resp.Result = nil
	resp.Error = &jsonrpc.Error{Code: -32601, Message: "method not found", Data: nil}
	rpcResp, ok := resp.RemoteMessage.Message.(*jsonrpc.Response)
	require.True(t, ok)
	original := string(rpcResp.Result)

	require.NoError(t, interceptor.InterceptToolsListResponse(t.Context(), resp))
	require.Equal(t, original, string(rpcResp.Result),
		"an error response must not be spliced")
}

func TestToolsListMCPConnectFilterInterceptor_KeepsOnlyGrantedTools(t *testing.T) {
	t.Parallel()

	engine := newAuthzEngineForTest(t)
	ctx := contextvalues.SetAuthContext(t.Context(), authzAuthContext(t))
	ctx = authztest.WithExactGrants(t, ctx,
		authz.NewGrantWithSelector(authz.ScopeMCPConnect, authz.Selector{
			"resource_kind": "mcp",
			"resource_id":   testServerID,
			"tool":          "search_tickets",
		}),
	)

	interceptor := remotemcp.NewToolsListMCPConnectFilterInterceptor(engine, emptyResolver(), testServerID, testProjectID, testenv.NewLogger(t))

	resp := newToolsListResponse(t, []*mcp.Tool{
		{Name: "search_tickets", InputSchema: map[string]any{}},
		{Name: "delete_ticket", InputSchema: map[string]any{}},
		{Name: "update_ticket", InputSchema: map[string]any{}},
	})
	require.NoError(t, interceptor.InterceptToolsListResponse(ctx, resp))

	require.Len(t, resp.Result.Tools, 1)
	require.Equal(t, "search_tickets", resp.Result.Tools[0].Name)
	requireCallerVarying(t, resp)
}

func TestToolsListMCPConnectFilterInterceptor_AllGrantedPreservesToolBytes(t *testing.T) {
	t.Parallel()

	// When every tool is authorized there is nothing to replace, so the
	// interceptor must label the result without rewriting the tools
	// member: replacing it would re-marshal each kept tool through
	// mcp.Tool, dropping per-tool members the SDK does not model.
	engine := newAuthzEngineForTest(t)
	ctx := contextvalues.SetAuthContext(t.Context(), authzAuthContext(t))
	ctx = authztest.WithExactGrants(t, ctx,
		authz.NewGrantWithSelector(authz.ScopeMCPConnect, authz.Selector{
			"resource_kind": "mcp",
			"resource_id":   testServerID,
			"tool":          "tool_a",
		}),
		authz.NewGrantWithSelector(authz.ScopeMCPConnect, authz.Selector{
			"resource_kind": "mcp",
			"resource_id":   testServerID,
			"tool":          "tool_b",
		}),
	)

	interceptor := remotemcp.NewToolsListMCPConnectFilterInterceptor(engine, emptyResolver(), testServerID, testProjectID, testenv.NewLogger(t))

	// An upstream declaring its own catalog public and long-lived, plus a
	// per-tool member mcp.Tool does not model.
	resp := newToolsListResponseFromWire(t, `{"ttlMs":60000,"cacheScope":"public","tools":[`+
		`{"name":"tool_a","inputSchema":{},"x-vendor-hint":"survives"},`+
		`{"name":"tool_b","inputSchema":{}}]}`)
	rpcResp, ok := resp.RemoteMessage.Message.(*jsonrpc.Response)
	require.True(t, ok)
	var before map[string]json.RawMessage
	require.NoError(t, json.Unmarshal(rpcResp.Result, &before))

	require.NoError(t, interceptor.InterceptToolsListResponse(ctx, resp))

	require.Len(t, resp.Result.Tools, 2, "no tool may be filtered when all are granted")

	var after map[string]json.RawMessage
	require.NoError(t, json.Unmarshal(rpcResp.Result, &after))
	require.Equal(t, string(before["tools"]), string(after["tools"]),
		"an unfiltered catalog's tools member must relay as it arrived, not be re-marshaled")
	require.Contains(t, string(after["tools"]), `"x-vendor-hint":"survives"`,
		"per-tool members the SDK does not model must survive an unfiltered relay")

	// Both caching members overwrite the upstream's own stance.
	requireCallerVarying(t, resp)
}

func TestToolsListMCPConnectFilterInterceptor_EmptyArrayWhenNoGrantsMatch(t *testing.T) {
	t.Parallel()

	// All tools are filtered out — the response carries an empty array,
	// not a rejection. The caller has access to nothing in this server
	// but the call itself succeeded.
	engine := newAuthzEngineForTest(t)
	ctx := contextvalues.SetAuthContext(t.Context(), authzAuthContext(t))
	ctx = authztest.WithExactGrants(t, ctx)

	interceptor := remotemcp.NewToolsListMCPConnectFilterInterceptor(engine, emptyResolver(), testServerID, testProjectID, testenv.NewLogger(t))

	resp := newToolsListResponse(t, []*mcp.Tool{
		{Name: "tool_a", InputSchema: map[string]any{}},
		{Name: "tool_b", InputSchema: map[string]any{}},
	})
	require.NoError(t, interceptor.InterceptToolsListResponse(ctx, resp))
	require.Empty(t, resp.Result.Tools)
}

func TestToolsListMCPConnectFilterInterceptor_PreservesInputOrderInFilteredResult(t *testing.T) {
	t.Parallel()

	// Grants allow tool_b and tool_d. The filtered tools must come back
	// in their input order — index 1, index 3 — not reordered by the
	// authz check ordering or by deduplication.
	engine := newAuthzEngineForTest(t)
	ctx := contextvalues.SetAuthContext(t.Context(), authzAuthContext(t))
	ctx = authztest.WithExactGrants(t, ctx,
		authz.NewGrantWithSelector(authz.ScopeMCPConnect, authz.Selector{
			"resource_kind": "mcp",
			"resource_id":   testServerID,
			"tool":          "tool_b",
		}),
		authz.NewGrantWithSelector(authz.ScopeMCPConnect, authz.Selector{
			"resource_kind": "mcp",
			"resource_id":   testServerID,
			"tool":          "tool_d",
		}),
	)

	interceptor := remotemcp.NewToolsListMCPConnectFilterInterceptor(engine, emptyResolver(), testServerID, testProjectID, testenv.NewLogger(t))

	resp := newToolsListResponse(t, []*mcp.Tool{
		{Name: "tool_a", InputSchema: map[string]any{}},
		{Name: "tool_b", InputSchema: map[string]any{}},
		{Name: "tool_c", InputSchema: map[string]any{}},
		{Name: "tool_d", InputSchema: map[string]any{}},
	})
	require.NoError(t, interceptor.InterceptToolsListResponse(ctx, resp))

	require.Len(t, resp.Result.Tools, 2)
	require.Equal(t, "tool_b", resp.Result.Tools[0].Name)
	require.Equal(t, "tool_d", resp.Result.Tools[1].Name)
}

func TestToolsListMCPConnectFilterInterceptor_NilResultPassesThrough(t *testing.T) {
	t.Parallel()

	// An error-shaped response (no Result) must short-circuit without
	// touching the typed view. The downstream relay surfaces the
	// upstream's JSON-RPC error envelope to the user unchanged.
	interceptor := remotemcp.NewToolsListMCPConnectFilterInterceptor(newAuthzEngineForTest(t), emptyResolver(), testServerID, testProjectID, testenv.NewLogger(t))

	resp := &proxy.ToolsListResponse{
		Error:         &jsonrpc.Error{Code: -32601, Message: "method not found", Data: nil},
		RemoteMessage: nil,
		Request:       nil,
		Result:        nil,
	}
	require.NoError(t, interceptor.InterceptToolsListResponse(t.Context(), resp))
}

func TestToolsListMCPConnectFilterInterceptor_EmptyToolsListIsStillLabelled(t *testing.T) {
	t.Parallel()

	// Upstream returned a successful response with zero tools, so no
	// checks fire and the tools member is left alone. An empty catalog on
	// a private-visibility server is still per-principal information, so
	// it is labelled like any other.
	engine := newAuthzEngineForTest(t)
	ctx := contextvalues.SetAuthContext(t.Context(), authzAuthContext(t))
	ctx = authztest.WithExactGrants(t, ctx)

	interceptor := remotemcp.NewToolsListMCPConnectFilterInterceptor(engine, emptyResolver(), testServerID, testProjectID, testenv.NewLogger(t))

	resp := newToolsListResponse(t, nil)
	require.NoError(t, interceptor.InterceptToolsListResponse(ctx, resp))
	require.Empty(t, resp.Result.Tools)
	requireCallerVarying(t, resp)
}

func TestToolsListMCPConnectFilterInterceptor_FiltersByDisposition(t *testing.T) {
	t.Parallel()

	engine := newAuthzEngineForTest(t)
	ctx := contextvalues.SetAuthContext(t.Context(), authzAuthContext(t))
	// Grant covers any read_only tool on the server (no tool key).
	ctx = authztest.WithExactGrants(t, ctx,
		authz.NewGrantWithSelector(authz.ScopeMCPConnect, authz.Selector{
			"resource_kind": "mcp",
			"resource_id":   testServerID,
			"disposition":   "read_only",
		}),
	)

	resolver := fakeToolDispositionResolver{dispositions: map[string]string{
		"list_items":  "read_only",
		"delete_item": "destructive",
	}}
	interceptor := remotemcp.NewToolsListMCPConnectFilterInterceptor(engine, resolver, testServerID, testProjectID, testenv.NewLogger(t))

	resp := newToolsListResponse(t, []*mcp.Tool{
		{Name: "list_items", InputSchema: map[string]any{}},
		{Name: "delete_item", InputSchema: map[string]any{}},
	})
	require.NoError(t, interceptor.InterceptToolsListResponse(ctx, resp))

	require.Len(t, resp.Result.Tools, 1)
	require.Equal(t, "list_items", resp.Result.Tools[0].Name)
}

func TestToolsListMCPConnectFilterInterceptor_ResolverErrorFailsClosed(t *testing.T) {
	t.Parallel()

	engine := newAuthzEngineForTest(t)
	ctx := contextvalues.SetAuthContext(t.Context(), authzAuthContext(t))
	// A grant that would otherwise keep the tool — the resolver error must
	// short-circuit the whole filter rather than fall back to filtering on the
	// empty disposition.
	ctx = authztest.WithExactGrants(t, ctx,
		authz.NewGrantWithSelector(authz.ScopeMCPConnect, authz.Selector{
			"resource_kind": "mcp",
			"resource_id":   testServerID,
			"tool":          "list_items",
		}),
	)

	resolver := fakeToolDispositionResolver{err: errors.New("metadata store unavailable")}
	interceptor := remotemcp.NewToolsListMCPConnectFilterInterceptor(engine, resolver, testServerID, testProjectID, testenv.NewLogger(t))

	resp := newToolsListResponse(t, []*mcp.Tool{
		{Name: "list_items", InputSchema: map[string]any{}},
	})
	err := interceptor.InterceptToolsListResponse(ctx, resp)
	require.Error(t, err)
	// The tools slice is left untouched: the error propagates for the proxy to
	// surface, rather than a silently emptied or unfiltered list.
	require.Len(t, resp.Result.Tools, 1)
}

func TestToolsListMCPConnectFilterInterceptor_KeepsUnclassifiedGrantedTool(t *testing.T) {
	t.Parallel()

	// tool_a has no cached disposition; tool_b is classified but ungranted.
	// Filtering keys off the tool-name grant regardless of disposition state,
	// so the unclassified-but-granted tool survives and the classified-but-
	// ungranted one is dropped — disposition classification of a sibling does
	// not change how an un-annotated tool is authorized.
	engine := newAuthzEngineForTest(t)
	ctx := contextvalues.SetAuthContext(t.Context(), authzAuthContext(t))
	ctx = authztest.WithExactGrants(t, ctx,
		authz.NewGrantWithSelector(authz.ScopeMCPConnect, authz.Selector{
			"resource_kind": "mcp",
			"resource_id":   testServerID,
			"tool":          "tool_a",
		}),
	)

	resolver := fakeToolDispositionResolver{dispositions: map[string]string{"tool_b": "destructive"}}
	interceptor := remotemcp.NewToolsListMCPConnectFilterInterceptor(engine, resolver, testServerID, testProjectID, testenv.NewLogger(t))

	resp := newToolsListResponse(t, []*mcp.Tool{
		{Name: "tool_a", InputSchema: map[string]any{}},
		{Name: "tool_b", InputSchema: map[string]any{}},
	})
	require.NoError(t, interceptor.InterceptToolsListResponse(ctx, resp))

	require.Len(t, resp.Result.Tools, 1)
	require.Equal(t, "tool_a", resp.Result.Tools[0].Name)
}
