package testmcp

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// Tool describes a single tool that the mock MCP server will expose. Fields
// mirror [mcp.Tool] so tests can exercise the full tool-definition surface,
// plus a Response field that controls what a tool call returns.
type Tool struct {
	// Annotations carries optional UI hints (title, read-only, destructive,
	// idempotent, open-world).
	Annotations *mcp.ToolAnnotations

	// Description is the human-readable tool description returned to MCP
	// clients.
	Description string

	// Icons lists optional icon references for the tool.
	Icons []mcp.Icon

	// InputSchema is the JSON Schema object describing the tool's accepted
	// arguments. Marshaled to [encoding/json.RawMessage] before being passed
	// to the MCP SDK so tests can supply arbitrary shapes.
	InputSchema map[string]any

	// Meta carries optional protocol-level metadata.
	Meta mcp.Meta

	// Name is the tool's programmatic identifier (the value clients send as
	// "name" in a tools/call request).
	Name string

	// OutputSchema is the optional JSON Schema describing structured tool
	// output. When nil the server emits no output schema.
	OutputSchema map[string]any

	// Response controls the content the mock returns when this tool is
	// invoked.
	Response ToolResponse

	// Title is the optional UI-facing tool name.
	Title string
}

// mcpTool returns the [mcp.Tool] representation of t, marshaling InputSchema
// (and OutputSchema when set) to [encoding/json.RawMessage] so any JSON-
// serializable shape can be supplied.
func (t Tool) mcpTool() (*mcp.Tool, error) {
	inputSchemaJSON, err := json.Marshal(t.InputSchema)
	if err != nil {
		return nil, fmt.Errorf("marshal input schema for tool %q: %w", t.Name, err)
	}

	var outputSchema any
	if t.OutputSchema != nil {
		outputSchemaJSON, err := json.Marshal(t.OutputSchema)
		if err != nil {
			return nil, fmt.Errorf("marshal output schema for tool %q: %w", t.Name, err)
		}
		outputSchema = json.RawMessage(outputSchemaJSON)
	}

	return &mcp.Tool{
		Annotations:  t.Annotations,
		Description:  t.Description,
		Icons:        t.Icons,
		InputSchema:  json.RawMessage(inputSchemaJSON),
		Meta:         t.Meta,
		Name:         t.Name,
		OutputSchema: outputSchema,
		Title:        t.Title,
	}, nil
}

// mcpToolHandler returns an [mcp.ToolHandler] that replies with t.Response
// on every invocation. The handler ignores the call arguments — the mock is
// canned by design.
func (t Tool) mcpToolHandler() mcp.ToolHandler {
	response := t.Response
	return func(_ context.Context, _ *mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		return &mcp.CallToolResult{
			Content:           response.mcpContent(),
			IsError:           response.IsError,
			Meta:              nil,
			StructuredContent: nil,
		}, nil
	}
}

// ToolResponse is the content returned when a Tool is called.
type ToolResponse struct {
	// Content is the list of content blocks returned to the MCP client.
	// Each entry mirrors the MCP content-object shape — currently only
	// type="text" entries (with a "text" string) are materialized by the
	// mock.
	Content []map[string]any

	// IsError, when true, causes the mock to return a CallToolResult with
	// IsError set so tests can exercise tool-side error handling without
	// producing a protocol-level error.
	IsError bool
}

// mcpContent converts r.Content into a slice of [mcp.Content] blocks.
// Entries with type="text" become [mcp.TextContent] carrying the "text"
// value. Unknown types fall back to text when a non-empty "text" field is
// present and are dropped otherwise.
func (r ToolResponse) mcpContent() []mcp.Content {
	content := make([]mcp.Content, 0, len(r.Content))
	for _, c := range r.Content {
		contentType, _ := c["type"].(string)
		text, _ := c["text"].(string)

		switch contentType {
		case "text":
			content = append(content, &mcp.TextContent{
				Annotations: nil,
				Meta:        nil,
				Text:        text,
			})
		default:
			if text != "" {
				content = append(content, &mcp.TextContent{
					Annotations: nil,
					Meta:        nil,
					Text:        text,
				})
			}
		}
	}
	return content
}
