package mcp

import (
	"encoding/json"
	"io"
	"log/slog"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/gen/types"
)

func testLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(io.Discard, nil))
}

func TestBuildDynamicSearchToolsSchema(t *testing.T) {
	t.Parallel()

	t.Run("builds_schema_with_empty_tags", func(t *testing.T) {
		t.Parallel()
		schema := buildDynamicSearchToolsSchema([]string{})

		var parsed map[string]any
		err := json.Unmarshal(schema, &parsed)
		require.NoError(t, err)
		require.Equal(t, "object", parsed["type"])

		props, ok := parsed["properties"].(map[string]any)
		require.True(t, ok)
		require.Contains(t, props, "query")
		require.Contains(t, props, "tags")
		require.Contains(t, props, "match_mode")
		require.Contains(t, props, "num_results")
	})

	t.Run("builds_schema_with_available_tags", func(t *testing.T) {
		t.Parallel()
		tags := []string{"api", "auth", "database"}
		schema := buildDynamicSearchToolsSchema(tags)

		// Schema should contain the tags in the description
		require.Contains(t, string(schema), "api")
		require.Contains(t, string(schema), "auth")
		require.Contains(t, string(schema), "database")
	})

	t.Run("schema_has_required_query_field", func(t *testing.T) {
		t.Parallel()
		schema := buildDynamicSearchToolsSchema([]string{})

		var parsed map[string]any
		err := json.Unmarshal(schema, &parsed)
		require.NoError(t, err)

		required, ok := parsed["required"].([]any)
		require.True(t, ok)
		require.Contains(t, required, "query")
	})
}

func TestBuildDescribeToolsTool(t *testing.T) {
	t.Parallel()

	t.Run("builds_tool_with_tool_names", func(t *testing.T) {
		t.Parallel()
		tools := []*types.Tool{
			{
				HTTPToolDefinition: &types.HTTPToolDefinition{
					Name:        "test-tool-1",
					Description: "First test tool",
				},
			},
			{
				HTTPToolDefinition: &types.HTTPToolDefinition{
					Name:        "test-tool-2",
					Description: "Second test tool",
				},
			},
		}

		entry, err := buildDescribeToolsTool(tools)
		require.NoError(t, err)
		require.NotNil(t, entry)
		require.Equal(t, describeToolsToolName, entry.Name)
		require.Contains(t, entry.Description, "Describe a set of tools by name")
	})

	t.Run("builds_tool_with_empty_tools_list", func(t *testing.T) {
		t.Parallel()
		tools := []*types.Tool{}

		entry, err := buildDescribeToolsTool(tools)
		require.NoError(t, err)
		require.NotNil(t, entry)
		require.Equal(t, describeToolsToolName, entry.Name)
	})

	t.Run("returns_error_for_proxy_tool", func(t *testing.T) {
		t.Parallel()
		proxyType := "proxy"
		tools := []*types.Tool{
			{
				ExternalMcpToolDefinition: &types.ExternalMCPToolDefinition{
					Name: "proxy-tool",
					Type: &proxyType,
				},
			},
		}

		_, err := buildDescribeToolsTool(tools)
		require.Error(t, err)
		require.Contains(t, err.Error(), "external mcp proxy")
	})

	t.Run("schema_includes_tool_name_examples", func(t *testing.T) {
		t.Parallel()
		tools := []*types.Tool{
			{
				HTTPToolDefinition: &types.HTTPToolDefinition{
					Name:        "get-users",
					Description: "Get users",
				},
			},
			{
				HTTPToolDefinition: &types.HTTPToolDefinition{
					Name:        "create-user",
					Description: "Create user",
				},
			},
		}

		entry, err := buildDescribeToolsTool(tools)
		require.NoError(t, err)

		// Schema should include tool names as examples
		require.Contains(t, string(entry.InputSchema), "get-users")
		require.Contains(t, string(entry.InputSchema), "create-user")
	})

	t.Run("limits_examples_to_three_tools", func(t *testing.T) {
		t.Parallel()
		tools := []*types.Tool{
			{HTTPToolDefinition: &types.HTTPToolDefinition{Name: "tool-1", Description: "d1"}},
			{HTTPToolDefinition: &types.HTTPToolDefinition{Name: "tool-2", Description: "d2"}},
			{HTTPToolDefinition: &types.HTTPToolDefinition{Name: "tool-3", Description: "d3"}},
			{HTTPToolDefinition: &types.HTTPToolDefinition{Name: "tool-4", Description: "d4"}},
			{HTTPToolDefinition: &types.HTTPToolDefinition{Name: "tool-5", Description: "d5"}},
		}

		entry, err := buildDescribeToolsTool(tools)
		require.NoError(t, err)

		// Only first 3 tools should be in examples
		require.Contains(t, string(entry.InputSchema), "tool-1")
		require.Contains(t, string(entry.InputSchema), "tool-2")
		require.Contains(t, string(entry.InputSchema), "tool-3")
		require.NotContains(t, string(entry.InputSchema), "tool-4")
		require.NotContains(t, string(entry.InputSchema), "tool-5")
	})
}

func TestHandleDescribeToolsCall(t *testing.T) {
	t.Parallel()

	t.Run("returns_error_for_empty_tool_names", func(t *testing.T) {
		t.Parallel()
		ctx := t.Context()
		logger := testLogger()

		toolset := &types.Toolset{
			Tools: []*types.Tool{
				{HTTPToolDefinition: &types.HTTPToolDefinition{Name: "test-tool", Description: "test"}},
			},
		}

		argsRaw := json.RawMessage(`{"tool_names": []}`)
		reqID := msgID{format: 1, Number: 1}

		_, err := handleDescribeToolsCall(ctx, logger, reqID, argsRaw, toolset)
		require.Error(t, err)
		require.Contains(t, err.Error(), "tool_names are required")
	})

	t.Run("returns_error_for_missing_tool_names", func(t *testing.T) {
		t.Parallel()
		ctx := t.Context()
		logger := testLogger()

		toolset := &types.Toolset{
			Tools: []*types.Tool{
				{HTTPToolDefinition: &types.HTTPToolDefinition{Name: "test-tool", Description: "test"}},
			},
		}

		argsRaw := json.RawMessage(`{}`)
		reqID := msgID{format: 1, Number: 1}

		_, err := handleDescribeToolsCall(ctx, logger, reqID, argsRaw, toolset)
		require.Error(t, err)
	})

	t.Run("returns_error_for_invalid_json_args", func(t *testing.T) {
		t.Parallel()
		ctx := t.Context()
		logger := testLogger()

		toolset := &types.Toolset{}
		argsRaw := json.RawMessage(`{invalid}`)
		reqID := msgID{format: 1, Number: 1}

		_, err := handleDescribeToolsCall(ctx, logger, reqID, argsRaw, toolset)
		require.Error(t, err)
		require.Contains(t, err.Error(), "failed to parse")
	})

	t.Run("describes_existing_tools", func(t *testing.T) {
		t.Parallel()
		ctx := t.Context()
		logger := testLogger()

		toolset := &types.Toolset{
			Tools: []*types.Tool{
				{
					HTTPToolDefinition: &types.HTTPToolDefinition{
						Name:        "get-user",
						Description: "Get a user by ID",
						Schema:      `{"type": "object", "properties": {"id": {"type": "string"}}}`,
					},
				},
				{
					HTTPToolDefinition: &types.HTTPToolDefinition{
						Name:        "create-user",
						Description: "Create a new user",
					},
				},
			},
		}

		argsRaw := json.RawMessage(`{"tool_names": ["get-user"]}`)
		reqID := msgID{format: 1, Number: 1}

		response, err := handleDescribeToolsCall(ctx, logger, reqID, argsRaw, toolset)
		require.NoError(t, err)
		require.NotNil(t, response)

		// Verify response contains the tool description
		require.Contains(t, string(response), "get-user")
		require.Contains(t, string(response), "Get a user by ID")
	})

	t.Run("handles_nonexistent_tool_names_gracefully", func(t *testing.T) {
		t.Parallel()
		ctx := t.Context()
		logger := testLogger()

		toolset := &types.Toolset{
			Tools: []*types.Tool{
				{HTTPToolDefinition: &types.HTTPToolDefinition{Name: "existing-tool", Description: "exists"}},
			},
		}

		argsRaw := json.RawMessage(`{"tool_names": ["nonexistent-tool"]}`)
		reqID := msgID{format: 1, Number: 1}

		response, err := handleDescribeToolsCall(ctx, logger, reqID, argsRaw, toolset)
		require.NoError(t, err)
		require.NotNil(t, response)
		// Should return empty tools list (JSON escaped in response)
		require.Contains(t, string(response), `\"tools\":[]`)
	})

	t.Run("returns_error_for_proxy_tools", func(t *testing.T) {
		t.Parallel()
		ctx := t.Context()
		logger := testLogger()

		proxyType := "proxy"
		toolset := &types.Toolset{
			Tools: []*types.Tool{
				{ExternalMcpToolDefinition: &types.ExternalMCPToolDefinition{Name: "proxy-tool", Type: &proxyType}},
			},
		}

		argsRaw := json.RawMessage(`{"tool_names": ["proxy-tool"]}`)
		reqID := msgID{format: 1, Number: 1}

		_, err := handleDescribeToolsCall(ctx, logger, reqID, argsRaw, toolset)
		require.Error(t, err)
		require.Contains(t, err.Error(), "external mcp proxy")
	})

	t.Run("handles_multiple_tool_names", func(t *testing.T) {
		t.Parallel()
		ctx := t.Context()
		logger := testLogger()

		toolset := &types.Toolset{
			Tools: []*types.Tool{
				{HTTPToolDefinition: &types.HTTPToolDefinition{Name: "tool-a", Description: "Tool A"}},
				{HTTPToolDefinition: &types.HTTPToolDefinition{Name: "tool-b", Description: "Tool B"}},
				{HTTPToolDefinition: &types.HTTPToolDefinition{Name: "tool-c", Description: "Tool C"}},
			},
		}

		argsRaw := json.RawMessage(`{"tool_names": ["tool-a", "tool-c"]}`)
		reqID := msgID{format: 1, Number: 1}

		response, err := handleDescribeToolsCall(ctx, logger, reqID, argsRaw, toolset)
		require.NoError(t, err)
		require.NotNil(t, response)
		require.Contains(t, string(response), "tool-a")
		require.Contains(t, string(response), "tool-c")
	})

	t.Run("trims_whitespace_from_tool_names", func(t *testing.T) {
		t.Parallel()
		ctx := t.Context()
		logger := testLogger()

		toolset := &types.Toolset{
			Tools: []*types.Tool{
				{HTTPToolDefinition: &types.HTTPToolDefinition{Name: "my-tool", Description: "My Tool"}},
			},
		}

		argsRaw := json.RawMessage(`{"tool_names": ["  my-tool  "]}`)
		reqID := msgID{format: 1, Number: 1}

		response, err := handleDescribeToolsCall(ctx, logger, reqID, argsRaw, toolset)
		require.NoError(t, err)
		require.NotNil(t, response)
		require.Contains(t, string(response), "my-tool")
	})
}
