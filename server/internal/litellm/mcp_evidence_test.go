package litellm

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestMCPInventoryFromTools(t *testing.T) {
	t.Parallel()
	tools := []any{
		map[string]any{"type": "function", "function": map[string]any{"name": "mcp__GitHub__create_issue"}},
		map[string]any{"type": "function", "function": map[string]any{"name": "mcp__github__list_issues"}},
		map[string]any{"type": "function", "function": map[string]any{"name": "bash"}},
		map[string]any{"type": "function", "function": map[string]any{"name": "MCP:search"}},
		map[string]any{"type": "function", "function": map[string]any{"name": "mcp____broken"}},
		map[string]any{"type": "function", "function": "not-an-object"},
		map[string]any{"type": "mcp", "server_label": "linear", "server_url": "https://mcp.linear.app/mcp"},
		map[string]any{"type": "mcp", "server_url": "https://mcp.example.com/sse"},
		map[string]any{"type": "mcp", "server_label": "no-url"},
		map[string]any{"type": "code_interpreter"},
		map[string]any{"type": 42},
		"not-an-object",
	}
	got := mcpInventoryFromTools(tools)
	require.Len(t, got, 3)
	// sorted by URL
	require.Equal(t, "https://mcp.example.com/sse", *got[0].URL)
	require.Equal(t, "mcp.example.com", *got[0].ServerName)
	require.Equal(t, "https://mcp.linear.app/mcp", *got[1].URL)
	require.Equal(t, "linear", *got[1].ServerName)
	require.Equal(t, "mcp-tool://github", *got[2].URL)
	require.Equal(t, "github", *got[2].ServerName)
	for _, entry := range got {
		require.Nil(t, entry.ServerIdentity)
		require.Nil(t, entry.Command)
		require.Nil(t, entry.ResultJSON)
	}
}

func TestMCPInventoryFromToolsEmpty(t *testing.T) {
	t.Parallel()
	require.Nil(t, mcpInventoryFromTools(nil))
	require.Nil(t, mcpInventoryFromTools([]any{}))
	require.Nil(t, mcpInventoryFromTools([]any{map[string]any{"type": "function", "function": map[string]any{"name": "bash"}}}))
	require.Nil(t, mcpInventoryFromTools([]any{map[string]any{"type": "function", "function": map[string]any{"name": "MCP:search"}}}))
}
