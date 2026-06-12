package risk_analysis_test

import (
	"testing"

	"github.com/stretchr/testify/require"

	risk_analysis "github.com/speakeasy-api/gram/server/internal/background/activities/risk_analysis"
	"github.com/speakeasy-api/gram/server/internal/message"
)

func TestAttributeTool(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name     string
		in       string
		server   string
		function string
		isMCP    bool
	}{
		{"claude code mcp", "mcp__github__create_issue", "github", "create_issue", true},
		{"nested server name", "mcp__claude_ai_Linear__list_issues", "claude_ai_Linear", "list_issues", true},
		{"cursor MCP prefix", "MCP:slack:send_message", "slack:send_message", "slack:send_message", true},
		{"native tool", "Bash", "", "", false},
		{"malformed mcp without function", "mcp__github__", "", "", false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			server, function, isMCP := risk_analysis.AttributeTool(tc.in)
			require.Equal(t, tc.isMCP, isMCP)
			require.Equal(t, tc.server, server)
			require.Equal(t, tc.function, function)
		})
	}
}

func TestNewJudgeMessage(t *testing.T) {
	t.Parallel()

	require.IsType(t, risk_analysis.UserMessage{}, risk_analysis.NewJudgeMessage(message.User, "", "hi"))
	require.IsType(t, risk_analysis.AssistantMessage{}, risk_analysis.NewJudgeMessage(message.Assistant, "", "hi"))
	require.IsType(t, risk_analysis.ToolResultMessage{}, risk_analysis.NewJudgeMessage(message.ToolResponse, "", "ok"))
	require.IsType(t, risk_analysis.OpaqueMessage{}, risk_analysis.NewJudgeMessage("", "", "x"))

	m := risk_analysis.NewJudgeMessage(message.ToolRequest, "mcp__github__create_issue", `{"title":"x"}`)
	tc, ok := m.(risk_analysis.ToolCallMessage)
	require.True(t, ok)
	require.Equal(t, message.ToolRequest, tc.Type())
	require.Equal(t, "github", tc.MCPServer)
	require.Equal(t, "create_issue", tc.MCPFunction)
	require.JSONEq(t, `{"title":"x"}`, tc.Arguments)
	require.JSONEq(t, `{"title":"x"}`, tc.Body())
}
