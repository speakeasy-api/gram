package mcp

import (
	"context"
	"encoding/json"
	"testing"

	sdkmcp "github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/mcp/mcprequests"
	"github.com/speakeasy-api/gram/server/internal/mcpfeedback"
	"github.com/speakeasy-api/gram/server/internal/mcpjsonrpc"
	"github.com/speakeasy-api/gram/server/internal/testenv"
)

// A feedback tool may not take a name the gateway answers itself in dynamic
// mode, so the feedback package's list must name every one of them.
func TestFeedbackToolCannotTakeTheGatewaysOwnToolNames(t *testing.T) {
	t.Parallel()

	for _, name := range []string{searchToolsToolName, listToolsToolName, describeToolsToolName, executeToolToolName} {
		require.Contains(t, mcpfeedback.ReservedToolNames, name)
	}
}

// A local tool that answers with no result is an unexpected error, not a panic.
func TestHostedLocalToolWithNoResultIsAnError(t *testing.T) {
	t.Parallel()

	tool := localTool{
		definition: &sdkmcp.Tool{Name: "quiet"},
		call: func(context.Context, localToolCall) (*sdkmcp.CallToolResult, error) {
			return nil, nil
		},
	}
	_, err := answerHostedLocalToolCall(t.Context(), testenv.NewLogger(t), nil, &mcpInputs{}, mcpjsonrpc.ID{Number: 1}, tool, json.RawMessage(`{}`), &mcprequests.SanitizedClientInfo{Name: "client", Version: "1"})
	require.ErrorContains(t, err, "answer local tool call")
}

// Only a proxied caller that presented a credential counts as authenticated.
func TestProxiedCallerAuthenticated(t *testing.T) {
	t.Parallel()

	require.False(t, proxiedCallerAuthenticated(t.Context()), "an anonymous caller of a public server")
	signedIn := contextvalues.SetAuthContext(t.Context(), &contextvalues.AuthContext{UserID: "user-1"})
	require.True(t, proxiedCallerAuthenticated(signedIn))
}
