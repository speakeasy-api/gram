package judgemessage_test

import (
	"fmt"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/judgemessage"
	"github.com/speakeasy-api/gram/server/internal/message"
)

func TestNewJudgeMessage(t *testing.T) {
	t.Parallel()

	// Type + body carry through; native/non-tool messages leave MCP fields empty.
	user := judgemessage.New(message.User, "", "hi")
	require.Equal(t, message.User, user.Type)
	require.Equal(t, "hi", user.Body)
	require.Empty(t, user.ToolName)

	// MCP tool name is destructured into server + function.
	tc := judgemessage.New(message.ToolRequest, "mcp__github__create_issue", `{"title":"x"}`)
	require.Equal(t, message.ToolRequest, tc.Type)
	require.Equal(t, "mcp__github__create_issue", tc.ToolName)
	require.Equal(t, "github", tc.MCPServer)
	require.Equal(t, "create_issue", tc.MCPFunction)
	require.JSONEq(t, `{"title":"x"}`, tc.Body)

	// Native tool: no MCP destructuring.
	native := judgemessage.New(message.ToolResponse, "Bash", "ok")
	require.Equal(t, message.ToolResponse, native.Type)
	require.Equal(t, "Bash", native.ToolName)
	require.Empty(t, native.MCPServer)
	require.Empty(t, native.MCPFunction)
	require.Empty(t, native.ToolCalls, "single-tool message carries no ToolCalls")
}

func TestJudgeMessageHasContent(t *testing.T) {
	t.Parallel()

	// Non-empty body is content.
	require.True(t, judgemessage.New(message.User, "", "hi").HasContent())
	// Empty body but MCP attribution: a tool-scoped policy can still match.
	require.True(t, judgemessage.New(message.ToolRequest, "mcp__github__delete_repo", "").HasContent())
	// Empty body but a native tool name.
	require.True(t, judgemessage.New(message.ToolRequest, "Bash", "").HasContent())
	// Multi-call message with no bodies still has content.
	require.True(t, judgemessage.NewForToolCalls([]judgemessage.ToolCall{
		judgemessage.NewToolCall("Bash", ""),
	}).HasContent())
	// Nothing to judge: blank body, no tool, no calls.
	require.False(t, judgemessage.New(message.User, "", "   ").HasContent())
}

func TestNewJudgeMessageForToolCalls(t *testing.T) {
	t.Parallel()

	msg := judgemessage.NewForToolCalls([]judgemessage.ToolCall{
		judgemessage.NewToolCall("mcp__github__create_issue", `{"title":"x"}`),
		judgemessage.NewToolCall("Bash", `{"command":"ls"}`),
	})

	require.Equal(t, message.ToolRequest, msg.Type)
	require.Empty(t, msg.ToolName, "multi-call message has no single tool name")
	require.Len(t, msg.ToolCalls, 2)

	// MCP call is destructured per-call.
	require.Equal(t, "github", msg.ToolCalls[0].MCPServer)
	require.Equal(t, "create_issue", msg.ToolCalls[0].MCPFunction)
	require.JSONEq(t, `{"title":"x"}`, msg.ToolCalls[0].Arguments)

	// Native call keeps its raw name, no MCP fields.
	require.Equal(t, "Bash", msg.ToolCalls[1].ToolName)
	require.Empty(t, msg.ToolCalls[1].MCPServer)
	require.Empty(t, msg.ToolCalls[1].MCPFunction)
}

func TestRenderPayloadTruncatedToolCallsPreservesTail(t *testing.T) {
	t.Parallel()

	calls := make([]judgemessage.ToolCall, 0, 60)
	for i := range 60 {
		calls = append(calls, judgemessage.NewToolCall("Bash", fmt.Sprintf(`{"index":%d}`, i)))
	}
	calls[59] = judgemessage.NewToolCall("Bash", `{"command":"rm -rf /tail"}`)

	payload := judgemessage.RenderPayload(judgemessage.NewForToolCalls(calls))

	require.True(t, payload.ToolCallsTruncated)
	require.Len(t, payload.ToolCalls, 50)
	require.JSONEq(t, `{"index":0}`, payload.ToolCalls[0].Arguments)
	require.JSONEq(t, `{"index":24}`, payload.ToolCalls[24].Arguments)
	require.JSONEq(t, `{"index":35}`, payload.ToolCalls[25].Arguments)
	require.JSONEq(t, `{"command":"rm -rf /tail"}`, payload.ToolCalls[49].Arguments)
}
