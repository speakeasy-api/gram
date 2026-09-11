package proxy

import (
	"encoding/json"
	"testing"

	"github.com/modelcontextprotocol/go-sdk/jsonrpc"
	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/stretchr/testify/require"
)

// markableToolsListResponse builds a typed view over a raw result payload.
// Internal so the tests below can reach the unexported dirty flag.
func markableToolsListResponse(t *testing.T, payload string) *ToolsListResponse {
	t.Helper()

	result := &mcp.ListToolsResult{
		Meta:       nil,
		Cacheable:  mcp.Cacheable{TTLMs: 0, CacheScope: ""},
		NextCursor: "",
		Tools:      nil,
	}
	require.NoError(t, json.Unmarshal([]byte(payload), result))
	return &ToolsListResponse{
		Error: nil,
		RemoteMessage: &RemoteMessage{
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

// TestMarkCallerVaryingFlipsDirty pins the step that actually gets the label
// onto the wire. Without the dirty flag the proxy relays the upstream body
// untouched (see materializedBytes) and the spliced result is never emitted,
// yet every assertion made against rpcResp.Result alone would still pass.
func TestMarkCallerVaryingFlipsDirty(t *testing.T) {
	t.Parallel()

	resp := markableToolsListResponse(t, `{"tools":[{"name":"a","inputSchema":{}}]}`)
	require.False(t, resp.RemoteMessage.dirty, "fixture must start clean")

	require.NoError(t, resp.MarkCallerVarying())

	require.True(t, resp.RemoteMessage.dirty,
		"the label only reaches the client if the message is re-materialized")

	body, ok, err := resp.RemoteMessage.materializedBytes()
	require.NoError(t, err)
	require.True(t, ok, "a marked message must re-encode")
	require.Contains(t, string(body), `"cacheScope":"private"`)
	require.Contains(t, string(body), `"ttlMs":0`)
}

// TestMarkCallerVaryingLeavesMessageCleanOnFailure covers the atomicity
// contract: a failed mark must not leave the message flagged for re-encode,
// or the proxy would emit a body no setter successfully produced.
func TestMarkCallerVaryingLeavesMessageCleanOnFailure(t *testing.T) {
	t.Parallel()

	resp := markableToolsListResponse(t, `{"tools":[]}`)
	rpcResp, ok := resp.RemoteMessage.Message.(*jsonrpc.Response)
	require.True(t, ok)
	// A result that is not a JSON object cannot be spliced.
	rpcResp.Result = json.RawMessage(`"not an object"`)

	require.Error(t, resp.MarkCallerVarying())
	require.False(t, resp.RemoteMessage.dirty, "a failed mark must not flag a re-encode")
	require.Equal(t, `"not an object"`, string(rpcResp.Result), "a failed mark must not rewrite the result")
	require.Empty(t, resp.Result.CacheScope, "a failed mark must not update the typed view")
}

func TestMarkCallerVaryingRejectsMissingRemoteMessage(t *testing.T) {
	t.Parallel()

	resp := markableToolsListResponse(t, `{"tools":[]}`)
	resp.RemoteMessage = nil

	err := resp.MarkCallerVarying()
	require.Error(t, err)
	var mutErr *MutationError
	require.ErrorAs(t, err, &mutErr)
}

// TestCallerVaryingHintsDerivedFromCacheable guards the coupling between the
// typed stance and the bytes spliced onto the wire: the two must not be
// independently maintained literals that can drift apart.
func TestCallerVaryingHintsDerivedFromCacheable(t *testing.T) {
	t.Parallel()

	spliced, err := spliceCallerVaryingHints(json.RawMessage(`{"tools":[]}`))
	require.NoError(t, err)

	var members map[string]json.RawMessage
	require.NoError(t, json.Unmarshal(spliced, &members))

	var wire mcp.Cacheable
	require.NoError(t, json.Unmarshal(spliced, &wire))
	require.Equal(t, callerVaryingCacheable, wire,
		"spliced bytes must decode back to the declared stance")
}
