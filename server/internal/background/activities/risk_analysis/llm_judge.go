package risk_analysis

import (
	"context"
	"encoding/json"
	"strings"

	"github.com/speakeasy-api/gram/server/internal/message"
	"github.com/speakeasy-api/gram/server/internal/scanners"
	"github.com/speakeasy-api/gram/server/internal/toolref"
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
	// Evaluate returns a verdict for a successful judge call. Nil means the
	// message was not judged, such as fail-open on judge error.
	Evaluate(ctx context.Context, in JudgeInput) *JudgeVerdict
}

// JudgeInput carries everything needed for one judge evaluation.
type JudgeInput struct {
	OrgID     string
	ProjectID string
	// Prompt is the policy's operator-authored guardrail.
	Prompt string
	// Message is the message under evaluation. Type drives how it's rendered
	// for the judge; Body holds the type-appropriate content.
	Message JudgeMessage
	Config  JudgeConfig
}

// JudgeMessage is the message under evaluation. Type selects the actor/role and
// body label the judge sees; Body is the type-appropriate content (user or
// assistant text, tool-call arguments, or tool-result output). For tool calls
// and results, ToolName is the raw tool name and MCPServer/MCPFunction are its
// destructured MCP components (empty for native tools and non-tool messages).
// When a single assistant message issued more than one tool call, ToolCalls
// carries each call with its own attribution and takes precedence over the
// single ToolName/Body fields. An unset/unknown Type renders as opaque content.
type JudgeMessage struct {
	Type        message.Type
	Body        string
	ToolName    string
	MCPServer   string
	MCPFunction string
	ToolCalls   []JudgeToolCall
}

// JudgeToolCall is one tool invocation within a multi-call assistant message.
// ToolName is the raw name; MCPServer/MCPFunction are its destructured MCP
// components (empty for native tools); Arguments is the call's raw input.
type JudgeToolCall struct {
	ToolName    string
	MCPServer   string
	MCPFunction string
	Arguments   string
}

// NewJudgeMessage builds the message from a message type, optional tool name,
// and the type-appropriate body. The tool name is destructured via AttributeTool
// so an MCP server/function is surfaced separately — a no-op for native tools
// and non-tool messages, where toolName is "".
func NewJudgeMessage(messageType message.Type, toolName, body string) JudgeMessage {
	server, fn, _ := toolref.AttributeTool(toolName)
	return JudgeMessage{
		Type:        messageType,
		Body:        body,
		ToolName:    toolName,
		MCPServer:   server,
		MCPFunction: fn,
		ToolCalls:   nil,
	}
}

// HasContent reports whether the message carries anything for the judge to
// evaluate: a non-empty body, tool attribution (so a tool-scoped policy can
// match a no-arg/no-output call), or one or more tool calls. An empty body
// alone is not a reason to skip a tool event.
func (m JudgeMessage) HasContent() bool {
	if strings.TrimSpace(m.Body) != "" {
		return true
	}
	if m.ToolName != "" || m.MCPServer != "" || m.MCPFunction != "" {
		return true
	}
	return len(m.ToolCalls) > 0
}

// NewJudgeToolCall destructures a tool name into its MCP components and pairs it
// with the call's raw arguments, for one entry of a multi-call message.
func NewJudgeToolCall(toolName, arguments string) JudgeToolCall {
	server, fn, _ := toolref.AttributeTool(toolName)
	return JudgeToolCall{
		ToolName:    toolName,
		MCPServer:   server,
		MCPFunction: fn,
		Arguments:   arguments,
	}
}

// NewJudgeMessageForToolCalls builds a tool-request message carrying multiple
// tool calls, each with its own attribution. Used by the batch analyzer when an
// assistant message issued more than one tool call, so per-call MCP server and
// function names reach the judge instead of an opaque tool_calls blob.
func NewJudgeMessageForToolCalls(calls []JudgeToolCall) JudgeMessage {
	return JudgeMessage{
		Type:        message.ToolRequest,
		Body:        "",
		ToolName:    "",
		MCPServer:   "",
		MCPFunction: "",
		ToolCalls:   calls,
	}
}

// JudgeVerdict is the resolved outcome of a judge evaluation.
type JudgeVerdict struct {
	Matched    bool
	Confidence float64
	// Rationale is a short, secret-free explanation of the match.
	Rationale        string
	CostUSD          float64
	PromptTokens     int
	CompletionTokens int
	TotalTokens      int
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
func JudgeFinding(verdict JudgeVerdict) scanners.Finding {
	description := verdict.Rationale
	if description == "" {
		description = "Message matched the prompt-based policy."
	}
	return scanners.Finding{
		Source:              SourceLLMJudge,
		RuleID:              RuleLLMJudge,
		Description:         description,
		Match:               "",
		StartPos:            0,
		EndPos:              0,
		Tags:                []string{},
		Confidence:          verdict.Confidence,
		DeadLetterReason:    "",
		McpLookupToolCallID: "",
		SpanGroupKey:        "",
		Field:               "",
		Path:                "",
	}
}
