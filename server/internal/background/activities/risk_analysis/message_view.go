package risk_analysis

import (
	"strings"

	"github.com/speakeasy-api/gram/server/internal/message"
	"github.com/speakeasy-api/gram/server/internal/toolref"
)

const SourceCustom = "custom"

// FindingSpan is one matched span attributed to a finding.
type FindingSpan struct {
	Match string `json:"match"`
	// Field is the author-facing message field the span matched.
	Field string `json:"field,omitempty"`
	// Path is the gjson sub-path within Field for `.get(...)` matches.
	Path     string `json:"path,omitempty"`
	StartPos int    `json:"start_pos"`
	EndPos   int    `json:"end_pos"`
}

// ToolView is one tool invocation surfaced from a message's recorded tool calls.
type ToolView struct {
	Name      string
	Server    string
	Function  string
	Arguments string
}

// MessageView is the structured input the CEL engine evaluates against.
type MessageView struct {
	Content string
	Type    message.Type
	Tools   []ToolView
}

// ToolCallInput is one recorded tool call as NewMessageView consumes it.
type ToolCallInput struct {
	Name      string
	Arguments string
}

// NewMessageView builds the view detection scopes evaluate for a message:
// its own text as content and, only for a tool request, one tool view per
// recorded call, skipping calls with neither a name nor arguments. Every
// path that scopes a batch message (the legacy prefilter and the LLM lane's
// post-scan filter) builds its view here so the two cannot drift.
func NewMessageView(content string, typ message.Type, calls []ToolCallInput) MessageView {
	view := MessageView{Content: content, Type: typ, Tools: []ToolView{}}
	if typ != message.ToolRequest {
		return view
	}
	for _, call := range calls {
		if call.Name == "" && strings.TrimSpace(call.Arguments) == "" {
			continue
		}
		view.Tools = append(view.Tools, NewToolView(call.Name, call.Arguments))
	}
	return view
}

// NewToolView destructures a tool-call name and pairs it with raw arguments.
func NewToolView(name, arguments string) ToolView {
	return ToolView{
		Name:      name,
		Server:    toolref.MCPServerOf(name),
		Function:  toolref.MCPFunctionOf(name),
		Arguments: arguments,
	}
}
