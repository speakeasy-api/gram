package judgemessage

import (
	"strings"

	"github.com/speakeasy-api/gram/server/internal/message"
	"github.com/speakeasy-api/gram/server/internal/toolref"
)

// Message is the message under evaluation by a judge.
type Message struct {
	// Type is the Gram chat message type that produced this judge input.
	Type message.Type
	// Body is the text content rendered for judge evaluation.
	Body string
	// ToolName is the raw tool identifier observed on a tool request, such as
	// "mcp__github__create_issue" for MCP tools or "Bash" for native tools.
	ToolName string
	// MCPServer is the server slug parsed from an MCP tool name, such as
	// "github" in "mcp__github__create_issue"; it is empty for non-MCP tools.
	MCPServer string
	// MCPFunction is the function name parsed from an MCP tool name, such as
	// "create_issue" in "mcp__github__create_issue"; it is empty for non-MCP tools.
	MCPFunction string
	// ToolCalls contains the individual tool invocations for multi-call
	// assistant messages. Single-call messages use ToolName/MCPServer/MCPFunction.
	ToolCalls []ToolCall
}

// ToolCall is one tool invocation within a multi-call assistant message.
type ToolCall struct {
	// ToolName is the raw tool identifier observed on the tool call.
	ToolName string
	// MCPServer is the server slug parsed from an MCP tool name; it is empty
	// for non-MCP tools.
	MCPServer string
	// MCPFunction is the function name parsed from an MCP tool name; it is
	// empty for non-MCP tools.
	MCPFunction string
	// Arguments is the raw serialized argument payload supplied to the tool.
	Arguments string
}

func New(messageType message.Type, toolName, body string) Message {
	server, fn, _ := toolref.AttributeTool(toolName)
	return Message{
		Type:        messageType,
		Body:        body,
		ToolName:    toolName,
		MCPServer:   server,
		MCPFunction: fn,
		ToolCalls:   nil,
	}
}

func (m Message) HasContent() bool {
	if strings.TrimSpace(m.Body) != "" {
		return true
	}
	if m.ToolName != "" || m.MCPServer != "" || m.MCPFunction != "" {
		return true
	}
	return len(m.ToolCalls) > 0
}

func NewToolCall(toolName, arguments string) ToolCall {
	server, fn, _ := toolref.AttributeTool(toolName)
	return ToolCall{
		ToolName:    toolName,
		MCPServer:   server,
		MCPFunction: fn,
		Arguments:   arguments,
	}
}

func NewForToolCalls(calls []ToolCall) Message {
	return Message{
		Type:        message.ToolRequest,
		Body:        "",
		ToolName:    "",
		MCPServer:   "",
		MCPFunction: "",
		ToolCalls:   calls,
	}
}
