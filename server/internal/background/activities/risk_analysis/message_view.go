package risk_analysis

import (
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

// NewToolView destructures a tool-call name and pairs it with raw arguments.
func NewToolView(name, arguments string) ToolView {
	return ToolView{
		Name:      name,
		Server:    toolref.MCPServerOf(name),
		Function:  toolref.MCPFunctionOf(name),
		Arguments: arguments,
	}
}
