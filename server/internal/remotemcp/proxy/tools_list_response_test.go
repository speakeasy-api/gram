package proxy_test

import (
	"encoding/json"
	"testing"

	"github.com/modelcontextprotocol/go-sdk/jsonrpc"
	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/remotemcp/proxy"
)

// toolsListResponseOverWire builds a typed view over a raw result payload,
// mirroring how the proxy constructs one from upstream bytes.
func toolsListResponseOverWire(t *testing.T, payload string) *proxy.ToolsListResponse {
	t.Helper()

	result := &mcp.ListToolsResult{
		Meta:       nil,
		Cacheable:  mcp.Cacheable{TTLMs: 0, CacheScope: ""},
		NextCursor: "",
		Tools:      nil,
	}
	require.NoError(t, json.Unmarshal([]byte(payload), result))
	return &proxy.ToolsListResponse{
		Error: nil,
		RemoteMessage: &proxy.RemoteMessage{
			UserHTTPRequest:    nil,
			RemoteHTTPRequest:  nil,
			RemoteHTTPResponse: nil,
			Message: &jsonrpc.Response{
				ID:     jsonrpc.ID{},
				Result: json.RawMessage(payload),
				Error:  nil,
			},
		},
		Request: nil,
		Result:  result,
	}
}

func wireMembers(t *testing.T, resp *proxy.ToolsListResponse) map[string]json.RawMessage {
	t.Helper()

	rpcResp, ok := resp.RemoteMessage.Message.(*jsonrpc.Response)
	require.True(t, ok)
	var members map[string]json.RawMessage
	require.NoError(t, json.Unmarshal(rpcResp.Result, &members))
	return members
}

func TestToolsListResponse_SetToolsLeavesUpstreamHintsAlone(t *testing.T) {
	t.Parallel()

	// SetTools stays a pure tools-member rewrite. The proxy applies the
	// caller-varying label after the whole chain, not the setter.
	resp := toolsListResponseOverWire(t, `{"ttlMs":60000,"cacheScope":"public",`+
		`"tools":[{"name":"a","inputSchema":{}},{"name":"b","inputSchema":{}}]}`)

	require.NoError(t, resp.SetTools([]*mcp.Tool{{Name: "a", InputSchema: map[string]any{}}}))

	members := wireMembers(t, resp)
	require.JSONEq(t, `"public"`, string(members["cacheScope"]))
	require.JSONEq(t, `60000`, string(members["ttlMs"]))
}
