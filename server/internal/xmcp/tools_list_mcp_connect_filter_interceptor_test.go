package xmcp_test

import (
	"testing"

	"github.com/modelcontextprotocol/go-sdk/jsonrpc"
	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/authz"
	"github.com/speakeasy-api/gram/server/internal/authztest"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/remotemcp/proxy"
	"github.com/speakeasy-api/gram/server/internal/testenv"
	"github.com/speakeasy-api/gram/server/internal/xmcp"
)

// newToolsListResponse constructs a typed view with the given tools and
// a fresh RemoteMessage backing the SetTools setter. The RemoteMessage
// carries a *jsonrpc.Response whose Result is set to a marshaled
// ListToolsResult so SetTools can re-marshal cleanly.
func newToolsListResponse(t *testing.T, tools []*mcp.Tool) *proxy.ToolsListResponse {
	t.Helper()

	result := &mcp.ListToolsResult{
		Meta:       nil,
		NextCursor: "",
		Tools:      tools,
	}
	rpcResp := &jsonrpc.Response{
		ID:     jsonrpc.ID{},
		Result: nil,
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

func TestToolsListMCPConnectFilterInterceptor_Name(t *testing.T) {
	t.Parallel()

	interceptor := xmcp.NewToolsListMCPConnectFilterInterceptor(newAuthzEngineForTest(t), testServerID, testProjectID, testenv.NewLogger(t))
	require.Equal(t, "tools-list-mcp-connect-filter", interceptor.Name())
}

func TestToolsListMCPConnectFilterInterceptor_NilEnginePassesThrough(t *testing.T) {
	t.Parallel()

	// A nil engine must not panic; pass the response through unchanged.
	interceptor := xmcp.NewToolsListMCPConnectFilterInterceptor(nil, testServerID, testProjectID, testenv.NewLogger(t))

	resp := newToolsListResponse(t, []*mcp.Tool{
		{Name: "tool_a", InputSchema: map[string]any{}},
		{Name: "tool_b", InputSchema: map[string]any{}},
	})
	require.NoError(t, interceptor.InterceptToolsListResponse(t.Context(), resp))
	require.Len(t, resp.Result.Tools, 2, "nil engine must leave the tools array unchanged")
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

	interceptor := xmcp.NewToolsListMCPConnectFilterInterceptor(engine, testServerID, testProjectID, testenv.NewLogger(t))

	resp := newToolsListResponse(t, []*mcp.Tool{
		{Name: "search_tickets", InputSchema: map[string]any{}},
		{Name: "delete_ticket", InputSchema: map[string]any{}},
		{Name: "update_ticket", InputSchema: map[string]any{}},
	})
	require.NoError(t, interceptor.InterceptToolsListResponse(ctx, resp))

	require.Len(t, resp.Result.Tools, 1)
	require.Equal(t, "search_tickets", resp.Result.Tools[0].Name)
}

func TestToolsListMCPConnectFilterInterceptor_EmptyArrayWhenNoGrantsMatch(t *testing.T) {
	t.Parallel()

	// All tools are filtered out — the response carries an empty array,
	// not a rejection. The caller has access to nothing in this server
	// but the call itself succeeded.
	engine := newAuthzEngineForTest(t)
	ctx := contextvalues.SetAuthContext(t.Context(), authzAuthContext(t))
	ctx = authztest.WithExactGrants(t, ctx)

	interceptor := xmcp.NewToolsListMCPConnectFilterInterceptor(engine, testServerID, testProjectID, testenv.NewLogger(t))

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

	interceptor := xmcp.NewToolsListMCPConnectFilterInterceptor(engine, testServerID, testProjectID, testenv.NewLogger(t))

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
	interceptor := xmcp.NewToolsListMCPConnectFilterInterceptor(newAuthzEngineForTest(t), testServerID, testProjectID, testenv.NewLogger(t))

	resp := &proxy.ToolsListResponse{
		Error:         &jsonrpc.Error{Code: -32601, Message: "method not found", Data: nil},
		RemoteMessage: nil,
		Request:       nil,
		Result:        nil,
	}
	require.NoError(t, interceptor.InterceptToolsListResponse(t.Context(), resp))
}

func TestToolsListMCPConnectFilterInterceptor_EmptyToolsListShortCircuits(t *testing.T) {
	t.Parallel()

	// Upstream returned a successful response with zero tools — no
	// checks fire, no SetTools is called, and the response passes
	// through.
	engine := newAuthzEngineForTest(t)
	ctx := contextvalues.SetAuthContext(t.Context(), authzAuthContext(t))
	ctx = authztest.WithExactGrants(t, ctx)

	interceptor := xmcp.NewToolsListMCPConnectFilterInterceptor(engine, testServerID, testProjectID, testenv.NewLogger(t))

	resp := newToolsListResponse(t, nil)
	require.NoError(t, interceptor.InterceptToolsListResponse(ctx, resp))
	require.Empty(t, resp.Result.Tools)
}
