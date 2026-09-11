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

func TestToolsListResponse_MarkCallerVaryingOverwritesUpstreamHints(t *testing.T) {
	t.Parallel()

	// An upstream declaring itself publicly cacheable for a minute cannot
	// have accounted for the RBAC layer in front of it, so both members
	// overwrite rather than fill.
	resp := toolsListResponseOverWire(t, `{"ttlMs":60000,"cacheScope":"public","tools":[{"name":"a","inputSchema":{}}]}`)

	require.NoError(t, resp.MarkCallerVarying())

	members := wireMembers(t, resp)
	require.JSONEq(t, `"private"`, string(members["cacheScope"]))
	require.JSONEq(t, `0`, string(members["ttlMs"]))
	require.Equal(t, "private", resp.Result.CacheScope, "typed view must agree with the wire")
	require.Equal(t, 0, resp.Result.TTLMs, "typed view must agree with the wire")
}

func TestToolsListResponse_MarkCallerVaryingLeavesToolsAndUnknownMembers(t *testing.T) {
	t.Parallel()

	// Only the two caching members are rewritten: the tools member keeps
	// its original bytes (no re-marshal through mcp.Tool) and members the
	// SDK does not model at the result level relay untouched.
	resp := toolsListResponseOverWire(t, `{"resultType":"complete","nextCursor":"c1",`+
		`"futureUnknownField":{"deep":[1,2,3]},`+
		`"tools":[{"name":"a","inputSchema":{},"x-vendor":"keep"}]}`)
	before := wireMembers(t, resp)

	require.NoError(t, resp.MarkCallerVarying())

	after := wireMembers(t, resp)
	require.Equal(t, string(before["tools"]), string(after["tools"]))
	require.Contains(t, string(after["tools"]), `"x-vendor":"keep"`)
	require.JSONEq(t, `"complete"`, string(after["resultType"]))
	require.JSONEq(t, `"c1"`, string(after["nextCursor"]))
	require.JSONEq(t, `{"deep":[1,2,3]}`, string(after["futureUnknownField"]))
	require.Len(t, resp.Result.Tools, 1, "the typed tools view must be left alone")
}

func TestToolsListResponse_MarkCallerVaryingRejectsErrorResponse(t *testing.T) {
	t.Parallel()

	resp := toolsListResponseOverWire(t, `{"tools":[]}`)
	resp.Result = nil
	resp.Error = &jsonrpc.Error{Code: -32601, Message: "method not found", Data: nil}

	err := resp.MarkCallerVarying()
	require.Error(t, err)
	var mutErr *proxy.MutationError
	require.ErrorAs(t, err, &mutErr)
}

func TestToolsListResponse_SetPrivateToolsClearsUpstreamTTL(t *testing.T) {
	t.Parallel()

	// A private scope alone is not enough: an inherited ttl would let the
	// requesting user's own client keep serving a filtered inventory after
	// the grants that shaped it were revoked.
	resp := toolsListResponseOverWire(t, `{"ttlMs":60000,"cacheScope":"public",`+
		`"tools":[{"name":"a","inputSchema":{}},{"name":"b","inputSchema":{}}]}`)

	require.NoError(t, resp.SetPrivateTools([]*mcp.Tool{{Name: "a", InputSchema: map[string]any{}}}))

	members := wireMembers(t, resp)
	require.JSONEq(t, `"private"`, string(members["cacheScope"]))
	require.JSONEq(t, `0`, string(members["ttlMs"]))
	require.Equal(t, "private", resp.Result.CacheScope)
	require.Equal(t, 0, resp.Result.TTLMs)
	require.Len(t, resp.Result.Tools, 1)
}

func TestToolsListResponse_SetToolsLeavesUpstreamHintsAlone(t *testing.T) {
	t.Parallel()

	// SetTools stays a pure tools-member rewrite; only the caller-varying
	// setters take a stance on caching.
	resp := toolsListResponseOverWire(t, `{"ttlMs":60000,"cacheScope":"public",`+
		`"tools":[{"name":"a","inputSchema":{}},{"name":"b","inputSchema":{}}]}`)

	require.NoError(t, resp.SetTools([]*mcp.Tool{{Name: "a", InputSchema: map[string]any{}}}))

	members := wireMembers(t, resp)
	require.JSONEq(t, `"public"`, string(members["cacheScope"]))
	require.JSONEq(t, `60000`, string(members["ttlMs"]))
}
