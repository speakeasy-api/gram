package testmcp

import (
	"encoding/json"
	"testing"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/stretchr/testify/require"
)

func TestTool_mcpTool_PropagatesFields(t *testing.T) {
	t.Parallel()

	annotations := &mcp.ToolAnnotations{
		Title:           "My Tool",
		ReadOnlyHint:    true,
		DestructiveHint: nil,
		IdempotentHint:  false,
		OpenWorldHint:   nil,
	}
	icons := []mcp.Icon{{Source: "https://example.com/icon.png", MIMEType: "image/png", Sizes: nil, Theme: ""}}
	meta := mcp.Meta{"custom": "value"}

	tool := Tool{
		Annotations:  annotations,
		Description:  "a tool",
		Icons:        icons,
		InputSchema:  map[string]any{"type": "object"},
		Meta:         meta,
		Name:         "my_tool",
		OutputSchema: nil,
		Response:     ToolResponse{Content: nil, IsError: false},
		Title:        "Display Title",
	}

	got, err := tool.mcpTool()
	require.NoError(t, err)

	require.Equal(t, "my_tool", got.Name)
	require.Equal(t, "Display Title", got.Title)
	require.Equal(t, "a tool", got.Description)
	require.Same(t, annotations, got.Annotations)
	require.Equal(t, icons, got.Icons)
	require.Equal(t, meta, got.Meta)
}

func TestTool_mcpTool_MarshalsInputSchema(t *testing.T) {
	t.Parallel()

	tool := Tool{
		Annotations:  nil,
		Description:  "",
		Icons:        nil,
		InputSchema:  map[string]any{"type": "object", "properties": map[string]any{"x": map[string]any{"type": "string"}}},
		Meta:         nil,
		Name:         "t",
		OutputSchema: nil,
		Response:     ToolResponse{Content: nil, IsError: false},
		Title:        "",
	}

	got, err := tool.mcpTool()
	require.NoError(t, err)

	raw, ok := got.InputSchema.(json.RawMessage)
	require.True(t, ok, "InputSchema must be a json.RawMessage")

	var decoded map[string]any
	require.NoError(t, json.Unmarshal(raw, &decoded))
	require.Equal(t, "object", decoded["type"])
	require.Contains(t, decoded, "properties")
}

func TestTool_mcpTool_OutputSchemaNilWhenNotSet(t *testing.T) {
	t.Parallel()

	tool := Tool{
		Annotations:  nil,
		Description:  "",
		Icons:        nil,
		InputSchema:  map[string]any{"type": "object"},
		Meta:         nil,
		Name:         "t",
		OutputSchema: nil,
		Response:     ToolResponse{Content: nil, IsError: false},
		Title:        "",
	}

	got, err := tool.mcpTool()
	require.NoError(t, err)
	require.Nil(t, got.OutputSchema)
}

func TestTool_mcpTool_MarshalsOutputSchemaWhenSet(t *testing.T) {
	t.Parallel()

	tool := Tool{
		Annotations:  nil,
		Description:  "",
		Icons:        nil,
		InputSchema:  map[string]any{"type": "object"},
		Meta:         nil,
		Name:         "t",
		OutputSchema: map[string]any{"type": "object", "properties": map[string]any{"result": map[string]any{"type": "string"}}},
		Response:     ToolResponse{Content: nil, IsError: false},
		Title:        "",
	}

	got, err := tool.mcpTool()
	require.NoError(t, err)

	raw, ok := got.OutputSchema.(json.RawMessage)
	require.True(t, ok, "OutputSchema must be a json.RawMessage when set")

	var decoded map[string]any
	require.NoError(t, json.Unmarshal(raw, &decoded))
	require.Equal(t, "object", decoded["type"])
	require.Contains(t, decoded, "properties")
}

func TestTool_mcpTool_ReturnsErrorOnUnmarshalableInputSchema(t *testing.T) {
	t.Parallel()

	tool := Tool{
		Annotations: nil,
		Description: "",
		Icons:       nil,
		// channels are not JSON-marshalable, so json.Marshal returns an error.
		InputSchema:  map[string]any{"bad": make(chan int)},
		Meta:         nil,
		Name:         "broken",
		OutputSchema: nil,
		Response:     ToolResponse{Content: nil, IsError: false},
		Title:        "",
	}

	_, err := tool.mcpTool()
	require.ErrorContains(t, err, "marshal input schema for tool \"broken\"")
}

func TestTool_mcpTool_ReturnsErrorOnUnmarshalableOutputSchema(t *testing.T) {
	t.Parallel()

	tool := Tool{
		Annotations:  nil,
		Description:  "",
		Icons:        nil,
		InputSchema:  map[string]any{"type": "object"},
		Meta:         nil,
		Name:         "broken",
		OutputSchema: map[string]any{"bad": make(chan int)},
		Response:     ToolResponse{Content: nil, IsError: false},
		Title:        "",
	}

	_, err := tool.mcpTool()
	require.ErrorContains(t, err, "marshal output schema for tool \"broken\"")
}

func TestTool_mcpToolHandler_WrapsResponseIntoCallToolResult(t *testing.T) {
	t.Parallel()

	tool := Tool{
		Annotations:  nil,
		Description:  "",
		Icons:        nil,
		InputSchema:  nil,
		Meta:         nil,
		Name:         "t",
		OutputSchema: nil,
		Response: ToolResponse{
			Content: []map[string]any{{"type": "text", "text": "hello"}},
			IsError: true,
		},
		Title: "",
	}

	result, err := tool.mcpToolHandler()(t.Context(), nil)
	require.NoError(t, err)
	require.True(t, result.IsError, "IsError must propagate into the CallToolResult")
	require.Equal(t, tool.Response.mcpContent(), result.Content, "Content must come from ToolResponse.mcpContent")
	require.Nil(t, result.Meta)
	require.Nil(t, result.StructuredContent)
}

func TestToolResponse_mcpContent_ConvertsTextEntries(t *testing.T) {
	t.Parallel()

	r := ToolResponse{
		Content: []map[string]any{
			{"type": "text", "text": "hello"},
			{"type": "text", "text": "world"},
		},
		IsError: false,
	}

	content := r.mcpContent()
	require.Len(t, content, 2)

	first, ok := content[0].(*mcp.TextContent)
	require.True(t, ok)
	require.Equal(t, "hello", first.Text)

	second, ok := content[1].(*mcp.TextContent)
	require.True(t, ok)
	require.Equal(t, "world", second.Text)
}

func TestToolResponse_mcpContent_EmptyProducesEmptyNonNilSlice(t *testing.T) {
	t.Parallel()

	r := ToolResponse{Content: nil, IsError: false}

	content := r.mcpContent()
	require.NotNil(t, content, "content slice must be non-nil so JSON encodes as [] not null")
	require.Empty(t, content)
}

func TestToolResponse_mcpContent_UnknownTypeFallsBackToTextWhenTextPresent(t *testing.T) {
	t.Parallel()

	r := ToolResponse{
		Content: []map[string]any{{"type": "image", "text": "fallback"}},
		IsError: false,
	}

	content := r.mcpContent()
	require.Len(t, content, 1)

	got, ok := content[0].(*mcp.TextContent)
	require.True(t, ok)
	require.Equal(t, "fallback", got.Text)
}

func TestToolResponse_mcpContent_UnknownTypeWithoutTextIsDropped(t *testing.T) {
	t.Parallel()

	r := ToolResponse{
		Content: []map[string]any{{"type": "image", "url": "https://example.com/x.png"}},
		IsError: false,
	}

	content := r.mcpContent()
	require.Empty(t, content)
}
