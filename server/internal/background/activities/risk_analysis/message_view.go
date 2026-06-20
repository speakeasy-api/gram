package risk_analysis

import (
	"github.com/speakeasy-api/gram/server/internal/message"
	"github.com/speakeasy-api/gram/server/internal/toolref"
)

const SourceCustom = "custom"

// FindingSpan is one matched span attributed to a finding, persisted as an
// element of risk_results.spans (a JSON array). A finding may carry several
// correlated spans — e.g. a custom rule matching a tool's function name and its
// arguments on the same call.
type FindingSpan struct {
	Match string `json:"match"`
	// Field is the message field the span matched, in author-facing form
	// (content/prompt/assistant/output or tool.name/tool.server/tool.function/
	// tool.args). Empty for detectors that don't attribute a field (gitleaks,
	// presidio, legacy rows).
	Field string `json:"field,omitempty"`
	// Path is the gjson sub-path within Field for `.get(...)` matches (e.g.
	// "command", "payload.sql"). Empty when the whole field value matched.
	Path     string `json:"path,omitempty"`
	StartPos int    `json:"start_pos"`
	EndPos   int    `json:"end_pos"`
}

// CustomDetectionRule is a policy-selected custom rule as loaded from the
// database. DetectionCel is the rule's CEL detection predicate; when empty the
// rule falls back to its legacy Regex column (evaluated as content.match(regex)).
// Custom rules are pure detectors — their polarity within a policy is no longer
// part of the rule (message exemptions are expressed via the policy's
// scope_exempt_cel). Evaluation goes through CompileCELRules / ScanCELRules.
type CustomDetectionRule struct {
	RuleID       string
	Title        string
	Description  string
	DetectionCel string
	Regex        string
}

// ToolView is one tool invocation surfaced from a message's recorded tool
// calls. Server/Function are the destructured MCP components (Server is "" for
// native/harness tools); Arguments is the raw arguments JSON.
type ToolView struct {
	Name      string
	Server    string
	Function  string
	Arguments string
}

// MessageView is the structured input the CEL engine evaluates against. Both
// scan paths (batch analyzer and realtime scanner) build an identical view so a
// rule behaves the same in either path. Content is the message's raw text body;
// Tools is populated for tool-request messages.
type MessageView struct {
	Content string
	Type    message.Type
	Tools   []ToolView
}

// NewToolView destructures a tool-call name into its MCP server/function
// components (Server is "" for native/harness tools) and pairs them with the
// raw arguments JSON, for one entry of a MessageView's Tools.
func NewToolView(name, arguments string) ToolView {
	return ToolView{
		Name:      name,
		Server:    toolref.MCPServerOf(name),
		Function:  toolref.MCPFunctionOf(name),
		Arguments: arguments,
	}
}
