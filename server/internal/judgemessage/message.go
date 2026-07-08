package judgemessage

import (
	"strings"

	"github.com/speakeasy-api/gram/server/internal/message"
	"github.com/speakeasy-api/gram/server/internal/toolref"
)

// Message is the message under evaluation by a judge.
type Message struct {
	Type        message.Type
	Body        string
	ToolName    string
	MCPServer   string
	MCPFunction string
	ToolCalls   []ToolCall
}

// ToolCall is one tool invocation within a multi-call assistant message.
type ToolCall struct {
	ToolName    string
	MCPServer   string
	MCPFunction string
	Arguments   string
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
