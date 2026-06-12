package risk_analysis

import (
	"context"
	"encoding/json"
	"strings"

	"github.com/speakeasy-api/gram/server/internal/message"
)

const (
	// SourceLLMJudge is the policy/finding source for prompt_based (LLM-judge)
	// policy evaluations.
	SourceLLMJudge = "llm_judge"
	// RuleLLMJudge is the canonical rule id emitted for every llm_judge
	// finding. The policy that produced it carries the human-meaningful prompt;
	// the rule_id just buckets the finding by detection mechanism.
	RuleLLMJudge = "llm_judge"
)

// PromptJudge evaluates a single message against a natural-language guardrail
// prompt and returns a verdict. The concrete OpenRouter-backed implementation
// lives in the internal/riskjudge package; this package only depends on the
// interface so it stays free of the LLM-client dependency chain (which would
// otherwise pull authz in through testenv and create an import cycle).
type PromptJudge interface {
	// Evaluate returns a non-nil verdict when the message violates the policy
	// prompt, or nil when it does not (including fail-open on judge error).
	Evaluate(ctx context.Context, in JudgeInput) *JudgeVerdict
}

// JudgeInput carries everything needed for one judge evaluation.
type JudgeInput struct {
	OrgID     string
	ProjectID string
	// Prompt is the policy's operator-authored guardrail.
	Prompt string
	// Message is the polymorphic message under evaluation. Its concrete type
	// (user/assistant content, tool call with arguments, tool result with
	// output) drives how it is rendered for the judge.
	Message JudgeMessage
	Config  JudgeConfig
}

// JudgeMessage is the polymorphic body of the message under evaluation. Each
// message type has a distinct shape so the judge sees structured fields (tool
// name, arguments, output) rather than one ambiguous text blob. Implemented by
// UserMessage, AssistantMessage, ToolCallMessage, ToolResultMessage, and the
// OpaqueMessage fallback.
type JudgeMessage interface {
	// Type reports the message type this payload represents.
	Type() message.Type
	// Body returns the primary evaluable content (used for the empty-input
	// guard). Empty Body means there is nothing to judge.
	Body() string
}

// UserMessage is a message authored by the end user.
type UserMessage struct{ Content string }

// AssistantMessage is a message authored by the AI assistant.
type AssistantMessage struct{ Content string }

// ToolCallMessage is a tool call issued by the assistant. Name is the raw
// tool name; MCPServer/MCPFunction are its destructured components for
// MCP-routed tools (empty for native). Arguments is the raw JSON input.
type ToolCallMessage struct {
	Name        string
	MCPServer   string
	MCPFunction string
	Arguments   string
}

// ToolResultMessage is a tool result returned to the assistant. Name/MCP*
// identify the originating tool when known; Output is the raw result.
type ToolResultMessage struct {
	Name        string
	MCPServer   string
	MCPFunction string
	Output      string
}

// OpaqueMessage is the fallback for an unknown/unset message type: the body is
// rendered without actor or tool framing.
type OpaqueMessage struct{ Content string }

func (UserMessage) Type() message.Type       { return message.User }
func (m UserMessage) Body() string           { return m.Content }
func (AssistantMessage) Type() message.Type  { return message.Assistant }
func (m AssistantMessage) Body() string      { return m.Content }
func (ToolCallMessage) Type() message.Type   { return message.ToolRequest }
func (m ToolCallMessage) Body() string       { return m.Arguments }
func (ToolResultMessage) Type() message.Type { return message.ToolResponse }
func (m ToolResultMessage) Body() string     { return m.Output }
func (OpaqueMessage) Type() message.Type     { return "" }
func (m OpaqueMessage) Body() string         { return m.Content }

// NewJudgeMessage builds the polymorphic message for a message type, optional
// tool name, and the type-appropriate body: user/assistant content, tool-call
// arguments JSON, or tool-result output. Tool names are destructured via
// AttributeTool so MCP server/function are surfaced separately.
func NewJudgeMessage(messageType message.Type, toolName, body string) JudgeMessage {
	switch messageType {
	case message.ToolRequest:
		server, fn, _ := AttributeTool(toolName)
		return ToolCallMessage{Name: toolName, MCPServer: server, MCPFunction: fn, Arguments: body}
	case message.ToolResponse:
		server, fn, _ := AttributeTool(toolName)
		return ToolResultMessage{Name: toolName, MCPServer: server, MCPFunction: fn, Output: body}
	case message.Assistant:
		return AssistantMessage{Content: body}
	case message.User:
		return UserMessage{Content: body}
	default:
		return OpaqueMessage{Content: body}
	}
}

// JudgeVerdict is the resolved outcome of a judge evaluation.
type JudgeVerdict struct {
	Confidence float64
	// Rationale is a short, secret-free explanation of the match.
	Rationale string
}

// JudgeConfig is the per-policy judge model configuration parsed from a
// prompt_based policy's model_config JSONB column.
type JudgeConfig struct {
	// Model is the OpenRouter model id; empty selects the default judge model.
	Model string
	// Temperature overrides the default judge temperature when non-nil.
	Temperature *float64
	// FailOpen decides the verdict when the judge call fails: true => allow
	// (no finding), false => treat as matched. Defaults to true.
	FailOpen bool
}

// ParseJudgeConfig decodes a prompt_based policy's model_config JSONB into a
// JudgeConfig. Missing or unparseable config defaults to fail-open with the
// default model and temperature.
func ParseJudgeConfig(raw []byte) JudgeConfig {
	cfg := JudgeConfig{Model: "", Temperature: nil, FailOpen: true}
	if len(raw) == 0 {
		return cfg
	}
	var parsed struct {
		Model       *string  `json:"model"`
		Temperature *float64 `json:"temperature"`
		FailOpen    *bool    `json:"fail_open"`
	}
	if err := json.Unmarshal(raw, &parsed); err != nil {
		return cfg
	}
	if parsed.Model != nil {
		cfg.Model = strings.TrimSpace(*parsed.Model)
	}
	cfg.Temperature = parsed.Temperature
	if parsed.FailOpen != nil {
		cfg.FailOpen = *parsed.FailOpen
	}
	return cfg
}

// JudgeFinding builds a canonical llm_judge Finding from a verdict. Shared by
// the batch analyzer so the (source, rule_id) identity stays consistent with
// the realtime scanner.
func JudgeFinding(verdict JudgeVerdict) Finding {
	description := verdict.Rationale
	if description == "" {
		description = "Message matched the prompt-based policy."
	}
	return Finding{
		Source:           SourceLLMJudge,
		RuleID:           RuleLLMJudge,
		Description:      description,
		Match:            "",
		StartPos:         0,
		EndPos:           0,
		Tags:             nil,
		Confidence:       verdict.Confidence,
		DeadLetterReason: "",
		toolCallID:       "",
	}
}
