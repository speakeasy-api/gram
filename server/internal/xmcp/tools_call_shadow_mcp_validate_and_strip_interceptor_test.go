package xmcp_test

import (
	"encoding/json"
	"testing"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/remotemcp/proxy"
	"github.com/speakeasy-api/gram/server/internal/testenv"
	"github.com/speakeasy-api/gram/server/internal/xmcp"
)

// newToolsCallRequestWithArguments builds a ToolsCallRequest for the
// no-op gating tests. The underlying JSONRPCMessages slice is left
// empty because the interceptor short-circuits before reaching
// SetArguments in these scenarios.
func newToolsCallRequestWithArguments(toolName string, args json.RawMessage) *proxy.ToolsCallRequest {
	return &proxy.ToolsCallRequest{
		UserRequest: &proxy.UserRequest{},
		Params: &mcp.CallToolParamsRaw{
			Name:      toolName,
			Arguments: args,
			Meta:      nil,
		},
	}
}

func TestToolsCallShadowMCPValidateAndStripInterceptor_Name(t *testing.T) {
	t.Parallel()

	interceptor := xmcp.NewToolsCallShadowMCPValidateAndStripInterceptor(newShadowMCPClientForTest(t), testServerID, testProjectID, testenv.NewLogger(t))
	require.Equal(t, "tools-call-shadow-mcp-validate-and-strip", interceptor.Name())
}

func TestToolsCallShadowMCPValidateAndStripInterceptor_NilParamsPassesThrough(t *testing.T) {
	t.Parallel()

	// Defensive: a nil Params (only reachable through direct
	// construction) must not panic.
	interceptor := xmcp.NewToolsCallShadowMCPValidateAndStripInterceptor(newShadowMCPClientForTest(t), testServerID, testProjectID, testenv.NewLogger(t))

	call := &proxy.ToolsCallRequest{
		UserRequest: &proxy.UserRequest{},
		Params:      nil,
	}
	require.NoError(t, interceptor.InterceptToolsCallRequest(t.Context(), call))
}

func TestToolsCallShadowMCPValidateAndStripInterceptor_InvalidProjectIDPassesThrough(t *testing.T) {
	t.Parallel()

	// Non-UUID project id short-circuits with a warning log — the call
	// flows through to upstream unchanged, since shadow-MCP cannot be
	// validated against an unknown project scope.
	interceptor := xmcp.NewToolsCallShadowMCPValidateAndStripInterceptor(newShadowMCPClientForTest(t), testServerID, "not-a-uuid", testenv.NewLogger(t))

	call := newToolsCallRequestWithArguments("tool_a", json.RawMessage(`{"x-gram-toolset-id":"abc"}`))
	require.NoError(t, interceptor.InterceptToolsCallRequest(t.Context(), call))
	// Arguments must be unchanged (no validation, no strip).
	require.JSONEq(t, `{"x-gram-toolset-id":"abc"}`, string(call.Params.Arguments))
}

func TestToolsCallShadowMCPValidateAndStripInterceptor_PolicyDisabledPassesThrough(t *testing.T) {
	t.Parallel()

	// With a fresh project (no enabled risk policies), the gate skips
	// validation and the arguments are forwarded verbatim — including
	// any x-gram-toolset-id property the caller happened to echo (no
	// validation, no strip).
	interceptor := xmcp.NewToolsCallShadowMCPValidateAndStripInterceptor(newShadowMCPClientForTest(t), testServerID, testProjectID, testenv.NewLogger(t))

	call := newToolsCallRequestWithArguments("tool_a", json.RawMessage(`{"x-gram-toolset-id":"abc","location":"sf"}`))
	require.NoError(t, interceptor.InterceptToolsCallRequest(t.Context(), call))
	require.JSONEq(t, `{"x-gram-toolset-id":"abc","location":"sf"}`, string(call.Params.Arguments))
}
