package remotemcp_test

import (
	"encoding/json"
	"fmt"
	"testing"

	"github.com/modelcontextprotocol/go-sdk/jsonrpc"
	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/remotemcp"
	"github.com/speakeasy-api/gram/server/internal/remotemcp/proxy"
	"github.com/speakeasy-api/gram/server/internal/shadowmcp"
	"github.com/speakeasy-api/gram/server/internal/testenv"
)

// newToolsCallRequestForSetArguments builds a ToolsCallRequest whose
// underlying JSONRPCMessages[0] is a real *jsonrpc.Request, which
// SetArguments needs in order to commit the scrubbed payload back onto the
// wire shape.
func newToolsCallRequestForSetArguments(toolName string, args json.RawMessage) *proxy.ToolsCallRequest {
	rpcReq := &jsonrpc.Request{
		ID:     jsonrpc.ID{},
		Method: "tools/call",
		Params: json.RawMessage(`{}`),
		Extra:  nil,
	}
	return &proxy.ToolsCallRequest{
		UserRequest: &proxy.UserRequest{
			UserHTTPRequest: nil,
			JSONRPCMessages: []jsonrpc.Message{rpcReq},
		},
		Params: &mcp.CallToolParamsRaw{
			Name:      toolName,
			Arguments: args,
			Meta:      nil,
		},
	}
}

// requireRequestNotRewritten asserts the interceptor left the underlying
// JSON-RPC message alone. newToolsCallRequestForSetArguments seeds Params
// with the "{}" sentinel, which only SetArguments overwrites, so an
// unchanged sentinel proves the request was never marked dirty — and
// therefore that the proxy will not needlessly re-encode the body before
// forwarding it upstream.
func requireRequestNotRewritten(t *testing.T, call *proxy.ToolsCallRequest) {
	t.Helper()

	rpcReq, ok := call.UserRequest.JSONRPCMessages[0].(*jsonrpc.Request)
	require.True(t, ok, "test helper must seed a *jsonrpc.Request")
	require.JSONEq(t, `{}`, string(rpcReq.Params), "interceptor must not commit an unchanged payload")
}

// requireRequestRewritten is the inverse of [requireRequestNotRewritten]:
// a genuine strip must reach the wire, not just the typed view.
func requireRequestRewritten(t *testing.T, call *proxy.ToolsCallRequest) {
	t.Helper()

	rpcReq, ok := call.UserRequest.JSONRPCMessages[0].(*jsonrpc.Request)
	require.True(t, ok, "test helper must seed a *jsonrpc.Request")
	require.NotContains(t, string(rpcReq.Params), shadowmcp.XGramToolsetIDField, "stripped payload must reach the underlying message")
}

func TestToolsCallStripToolsetIDInterceptor_Name(t *testing.T) {
	t.Parallel()

	interceptor := remotemcp.NewToolsCallStripToolsetIDInterceptor(testenv.NewLogger(t))
	require.Equal(t, "tools-call-strip-toolset-id", interceptor.Name())
}

func TestToolsCallStripToolsetIDInterceptor_NilParamsPassesThrough(t *testing.T) {
	t.Parallel()

	// Defensive: a nil Params (only reachable through direct construction)
	// must not panic.
	interceptor := remotemcp.NewToolsCallStripToolsetIDInterceptor(testenv.NewLogger(t))

	call := &proxy.ToolsCallRequest{
		UserRequest: &proxy.UserRequest{},
		Params:      nil,
	}
	require.NoError(t, interceptor.InterceptToolsCallRequest(t.Context(), call))
}

func TestToolsCallStripToolsetIDInterceptor_NoEchoedPropertySucceeds(t *testing.T) {
	t.Parallel()

	// The regression this change exists to fix: a caller that does not echo
	// the property must be forwarded upstream, not rejected. The interceptor
	// holds no policy client, so this holds regardless of the project's risk
	// policies.
	interceptor := remotemcp.NewToolsCallStripToolsetIDInterceptor(testenv.NewLogger(t))

	call := newToolsCallRequestForSetArguments("tool_a", json.RawMessage(`{"location":"sf"}`))

	require.NoError(t, interceptor.InterceptToolsCallRequest(t.Context(), call))
	require.JSONEq(t, `{"location":"sf"}`, string(call.Params.Arguments))
	requireRequestNotRewritten(t, call)
}

func TestToolsCallStripToolsetIDInterceptor_EmptyArgumentsSucceed(t *testing.T) {
	t.Parallel()

	interceptor := remotemcp.NewToolsCallStripToolsetIDInterceptor(testenv.NewLogger(t))

	call := newToolsCallRequestForSetArguments("tool_a", nil)

	require.NoError(t, interceptor.InterceptToolsCallRequest(t.Context(), call))
	require.Empty(t, call.Params.Arguments)
	requireRequestNotRewritten(t, call)
}

func TestToolsCallStripToolsetIDInterceptor_StaleEchoedPropertyStripped(t *testing.T) {
	t.Parallel()

	// Simulates a client whose cached tools/list schema still carries the
	// injected const: the echo is a well-formed UUID that no longer
	// corresponds to anything. It must be stripped, not validated.
	interceptor := remotemcp.NewToolsCallStripToolsetIDInterceptor(testenv.NewLogger(t))

	args := json.RawMessage(fmt.Sprintf(`{%q:%q,"location":"sf"}`, shadowmcp.XGramToolsetIDField, "018f0000-0000-7000-8000-000000000000"))
	call := newToolsCallRequestForSetArguments("tool_a", args)

	require.NoError(t, interceptor.InterceptToolsCallRequest(t.Context(), call))
	require.JSONEq(t, `{"location":"sf"}`, string(call.Params.Arguments))
	requireRequestRewritten(t, call)
}

func TestToolsCallStripToolsetIDInterceptor_GarbageEchoedValueStripped(t *testing.T) {
	t.Parallel()

	// Models were observed inventing values rather than echoing the const.
	// A non-UUID value is no longer a rejection, just something to strip.
	interceptor := remotemcp.NewToolsCallStripToolsetIDInterceptor(testenv.NewLogger(t))

	args := json.RawMessage(fmt.Sprintf(`{%q:"not-a-uuid","location":"sf"}`, shadowmcp.XGramToolsetIDField))
	call := newToolsCallRequestForSetArguments("tool_a", args)

	require.NoError(t, interceptor.InterceptToolsCallRequest(t.Context(), call))
	require.JSONEq(t, `{"location":"sf"}`, string(call.Params.Arguments))
}

func TestToolsCallStripToolsetIDInterceptor_AllZerosEchoedValueStripped(t *testing.T) {
	t.Parallel()

	interceptor := remotemcp.NewToolsCallStripToolsetIDInterceptor(testenv.NewLogger(t))

	args := json.RawMessage(fmt.Sprintf(`{%q:"00000000-0000-0000-0000-000000000000"}`, shadowmcp.XGramToolsetIDField))
	call := newToolsCallRequestForSetArguments("tool_a", args)

	require.NoError(t, interceptor.InterceptToolsCallRequest(t.Context(), call))
	require.JSONEq(t, `{}`, string(call.Params.Arguments))
}

func TestToolsCallStripToolsetIDInterceptor_NonStringEchoedValueStripped(t *testing.T) {
	t.Parallel()

	// The old validator rejected a non-string value. The strip works on the
	// raw property regardless of its JSON type.
	interceptor := remotemcp.NewToolsCallStripToolsetIDInterceptor(testenv.NewLogger(t))

	args := json.RawMessage(fmt.Sprintf(`{%q:123,"location":"sf"}`, shadowmcp.XGramToolsetIDField))
	call := newToolsCallRequestForSetArguments("tool_a", args)

	require.NoError(t, interceptor.InterceptToolsCallRequest(t.Context(), call))
	require.JSONEq(t, `{"location":"sf"}`, string(call.Params.Arguments))
}

func TestToolsCallStripToolsetIDInterceptor_NonObjectArgumentsPassThrough(t *testing.T) {
	t.Parallel()

	// A JSON array is not something the proxy should have an opinion about:
	// forward it and let the upstream server's own validation decide. The
	// previous interceptor rejected this outright. The array carries the
	// property name so the byte-scan does not short-circuit, which puts the
	// non-object guard inside StripToolsetIDProperty under test.
	interceptor := remotemcp.NewToolsCallStripToolsetIDInterceptor(testenv.NewLogger(t))

	args := json.RawMessage(fmt.Sprintf(`[%q]`, shadowmcp.XGramToolsetIDField))
	call := newToolsCallRequestForSetArguments("tool_a", args)

	require.NoError(t, interceptor.InterceptToolsCallRequest(t.Context(), call))
	require.JSONEq(t, string(args), string(call.Params.Arguments))
	requireRequestNotRewritten(t, call)
}

func TestToolsCallStripToolsetIDInterceptor_PropertyNameInsideValueNotRewritten(t *testing.T) {
	t.Parallel()

	// The byte-scan matches the property name anywhere in the payload, so a
	// caller merely mentioning it in a value gets past the short-circuit.
	// Nothing is removed, so the request must not be marked dirty — a
	// needless re-encode drops params members CallToolParamsRaw doesn't
	// model and logs a mutation that never happened.
	interceptor := remotemcp.NewToolsCallStripToolsetIDInterceptor(testenv.NewLogger(t))

	args := json.RawMessage(fmt.Sprintf(`{"query":"how do I set %s?"}`, shadowmcp.XGramToolsetIDField))
	call := newToolsCallRequestForSetArguments("tool_a", args)

	require.NoError(t, interceptor.InterceptToolsCallRequest(t.Context(), call))
	require.JSONEq(t, string(args), string(call.Params.Arguments))
	requireRequestNotRewritten(t, call)
}

func TestToolsCallStripToolsetIDInterceptor_NestedPropertyNotRewritten(t *testing.T) {
	t.Parallel()

	// Only a top-level property is Gram's envelope. A nested occurrence
	// belongs to the upstream tool's own argument shape and must survive
	// untouched, without dirtying the request.
	interceptor := remotemcp.NewToolsCallStripToolsetIDInterceptor(testenv.NewLogger(t))

	args := json.RawMessage(fmt.Sprintf(`{"filter":{%q:"v"}}`, shadowmcp.XGramToolsetIDField))
	call := newToolsCallRequestForSetArguments("tool_a", args)

	require.NoError(t, interceptor.InterceptToolsCallRequest(t.Context(), call))
	require.JSONEq(t, string(args), string(call.Params.Arguments))
	requireRequestNotRewritten(t, call)
}

func TestToolsCallStripToolsetIDInterceptor_MalformedObjectRejectsAsParseError(t *testing.T) {
	t.Parallel()

	// Arguments that open with '{' but don't parse are the one surviving
	// rejection. It must carry a parse-error code so the caller sees a
	// client-side error rather than a Gram internal error.
	interceptor := remotemcp.NewToolsCallStripToolsetIDInterceptor(testenv.NewLogger(t))

	args := json.RawMessage(fmt.Sprintf(`{%q:"abc",`, shadowmcp.XGramToolsetIDField))
	call := newToolsCallRequestForSetArguments("tool_a", args)

	err := interceptor.InterceptToolsCallRequest(t.Context(), call)
	var rejErr *proxy.RejectError
	require.ErrorAs(t, err, &rejErr, "malformed arguments must surface as a JSON-RPC rejection")
	require.Equal(t, proxy.RejectCodeParseError, rejErr.Code)
}

func TestToolsCallStripToolsetIDInterceptor_MalformedObjectWithoutPropertyPassesThrough(t *testing.T) {
	t.Parallel()

	// The byte-scan short-circuit runs before any parsing, so malformed
	// arguments that never carried the property are forwarded untouched
	// rather than being rejected on Gram's behalf.
	interceptor := remotemcp.NewToolsCallStripToolsetIDInterceptor(testenv.NewLogger(t))

	call := newToolsCallRequestForSetArguments("tool_a", json.RawMessage(`{"location":`))

	require.NoError(t, interceptor.InterceptToolsCallRequest(t.Context(), call))
	require.Equal(t, `{"location":`, string(call.Params.Arguments))
}
