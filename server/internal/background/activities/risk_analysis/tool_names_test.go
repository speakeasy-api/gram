package risk_analysis_test

import (
	"testing"

	"github.com/stretchr/testify/require"

	risk_analysis "github.com/speakeasy-api/gram/server/internal/background/activities/risk_analysis"
	"github.com/speakeasy-api/gram/server/internal/message"
)

func TestNewJudgeMessage(t *testing.T) {
	t.Parallel()

	// Type + body carry through; native/non-tool messages leave MCP fields empty.
	user := risk_analysis.NewJudgeMessage(message.User, "", "hi")
	require.Equal(t, message.User, user.Type)
	require.Equal(t, "hi", user.Body)
	require.Empty(t, user.ToolName)

	// MCP tool name is destructured into server + function.
	tc := risk_analysis.NewJudgeMessage(message.ToolRequest, "mcp__github__create_issue", `{"title":"x"}`)
	require.Equal(t, message.ToolRequest, tc.Type)
	require.Equal(t, "mcp__github__create_issue", tc.ToolName)
	require.Equal(t, "github", tc.MCPServer)
	require.Equal(t, "create_issue", tc.MCPFunction)
	require.JSONEq(t, `{"title":"x"}`, tc.Body)

	// Native tool: no MCP destructuring.
	native := risk_analysis.NewJudgeMessage(message.ToolResponse, "Bash", "ok")
	require.Equal(t, message.ToolResponse, native.Type)
	require.Equal(t, "Bash", native.ToolName)
	require.Empty(t, native.MCPServer)
	require.Empty(t, native.MCPFunction)
	require.Empty(t, native.ToolCalls, "single-tool message carries no ToolCalls")
}

func TestJudgeMessageHasContent(t *testing.T) {
	t.Parallel()

	// Non-empty body is content.
	require.True(t, risk_analysis.NewJudgeMessage(message.User, "", "hi").HasContent())
	// Empty body but MCP attribution: a tool-scoped policy can still match.
	require.True(t, risk_analysis.NewJudgeMessage(message.ToolRequest, "mcp__github__delete_repo", "").HasContent())
	// Empty body but a native tool name.
	require.True(t, risk_analysis.NewJudgeMessage(message.ToolRequest, "Bash", "").HasContent())
	// Multi-call message with no bodies still has content.
	require.True(t, risk_analysis.NewJudgeMessageForToolCalls([]risk_analysis.JudgeToolCall{
		risk_analysis.NewJudgeToolCall("Bash", ""),
	}).HasContent())
	// Nothing to judge: blank body, no tool, no calls.
	require.False(t, risk_analysis.NewJudgeMessage(message.User, "", "   ").HasContent())
}

func TestNewJudgeMessageForToolCalls(t *testing.T) {
	t.Parallel()

	msg := risk_analysis.NewJudgeMessageForToolCalls([]risk_analysis.JudgeToolCall{
		risk_analysis.NewJudgeToolCall("mcp__github__create_issue", `{"title":"x"}`),
		risk_analysis.NewJudgeToolCall("Bash", `{"command":"ls"}`),
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
