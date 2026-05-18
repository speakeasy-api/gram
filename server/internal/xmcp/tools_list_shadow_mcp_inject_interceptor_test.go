package xmcp_test

import (
	"encoding/json"
	"testing"

	"github.com/modelcontextprotocol/go-sdk/jsonrpc"
	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/cache"
	"github.com/speakeasy-api/gram/server/internal/remotemcp/proxy"
	"github.com/speakeasy-api/gram/server/internal/shadowmcp"
	"github.com/speakeasy-api/gram/server/internal/testenv"
	"github.com/speakeasy-api/gram/server/internal/xmcp"
)

// newShadowMCPClientForTest constructs a shadowmcp.Client backed by the
// shared test infra (Postgres + Redis from TestMain). The client is
// real-DB but tests that exercise IsEnabledForProject against a fresh
// project see the default "disabled" state because no risk policies
// have been created.
func newShadowMCPClientForTest(t *testing.T) *shadowmcp.Client {
	t.Helper()
	conn, err := infra.CloneTestDatabase(t, "xmcpshadow")
	require.NoError(t, err)
	redisClient, err := infra.NewRedisClient(t, 0)
	require.NoError(t, err)
	return shadowmcp.NewClient(testenv.NewLogger(t), conn, cache.NewRedisCacheAdapter(redisClient))
}

func TestToolsListShadowMCPInjectInterceptor_Name(t *testing.T) {
	t.Parallel()

	interceptor := xmcp.NewToolsListShadowMCPInjectInterceptor(newShadowMCPClientForTest(t), testServerID, testProjectID, testenv.NewLogger(t))
	require.Equal(t, "tools-list-shadow-mcp-inject", interceptor.Name())
}

func TestToolsListShadowMCPInjectInterceptor_InvalidProjectIDPassesThrough(t *testing.T) {
	t.Parallel()

	// A non-UUID project id short-circuits with a warning log; the
	// tools array is left unchanged so the upstream's response reaches
	// the client verbatim.
	interceptor := xmcp.NewToolsListShadowMCPInjectInterceptor(newShadowMCPClientForTest(t), testServerID, "not-a-uuid", testenv.NewLogger(t))

	resp := newToolsListResponse(t, []*mcp.Tool{
		{Name: "tool_a", InputSchema: map[string]any{"type": "object"}},
	})
	require.NoError(t, interceptor.InterceptToolsListResponse(t.Context(), resp))
	require.Len(t, resp.Result.Tools, 1)

	// The original schema must be preserved (no injection occurred).
	raw, err := json.Marshal(resp.Result.Tools[0].InputSchema)
	require.NoError(t, err)
	require.NotContains(t, string(raw), shadowmcp.XGramToolsetIDField)
}

func TestToolsListShadowMCPInjectInterceptor_PolicyDisabledPassesThrough(t *testing.T) {
	t.Parallel()

	// With a real client and a fresh project, IsEnabledForProject
	// returns false (no risk policies). The interceptor is a no-op
	// against the response, and the original schemas reach the client
	// unmodified.
	interceptor := xmcp.NewToolsListShadowMCPInjectInterceptor(newShadowMCPClientForTest(t), testServerID, testProjectID, testenv.NewLogger(t))

	resp := newToolsListResponse(t, []*mcp.Tool{
		{Name: "tool_a", InputSchema: map[string]any{"type": "object"}},
	})
	require.NoError(t, interceptor.InterceptToolsListResponse(t.Context(), resp))

	raw, err := json.Marshal(resp.Result.Tools[0].InputSchema)
	require.NoError(t, err)
	require.NotContains(t, string(raw), shadowmcp.XGramToolsetIDField)
}

func TestToolsListShadowMCPInjectInterceptor_NilResultPassesThrough(t *testing.T) {
	t.Parallel()

	interceptor := xmcp.NewToolsListShadowMCPInjectInterceptor(newShadowMCPClientForTest(t), testServerID, testProjectID, testenv.NewLogger(t))

	resp := &proxy.ToolsListResponse{
		Error:         &jsonrpc.Error{Code: -32601, Message: "method not found", Data: nil},
		RemoteMessage: nil,
		Request:       nil,
		Result:        nil,
	}
	require.NoError(t, interceptor.InterceptToolsListResponse(t.Context(), resp))
}

func TestToolsListShadowMCPInjectInterceptor_EmptyToolsListShortCircuits(t *testing.T) {
	t.Parallel()

	interceptor := xmcp.NewToolsListShadowMCPInjectInterceptor(newShadowMCPClientForTest(t), testServerID, testProjectID, testenv.NewLogger(t))

	resp := newToolsListResponse(t, nil)
	require.NoError(t, interceptor.InterceptToolsListResponse(t.Context(), resp))
}
