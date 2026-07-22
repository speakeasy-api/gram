package judgemessage

import (
	"encoding/json"
	"fmt"
	"strings"
	"unicode/utf8"

	"github.com/speakeasy-api/gram/server/internal/message"
)

const (
	// maxPayloadBodyLen caps each body/arguments string in runes. A body is
	// head+tail truncated past this. This is a security control, not just a cost
	// one: without it an oversized payload can blow the judge model's context
	// window, producing an error that a fail-open policy turns into an allow.
	maxPayloadBodyLen = 16000
	// maxPayloadRenderedToolCall caps how many tool calls a single multi-call
	// message renders. Oversized call lists keep head and tail calls, and set
	// tool_calls_truncated.
	maxPayloadRenderedToolCall = 50
	// maxTrajectoryBodyLen bounds each contextual field independently. Context
	// is supporting evidence, not a second copy of the full conversation.
	maxTrajectoryBodyLen = 4000
)

type TrajectoryPayload struct {
	PriorUserRequest                string `json:"prior_user_request,omitempty"`
	PriorUserRequestTruncated       bool   `json:"prior_user_request_truncated,omitempty"`
	RecentUntrustedContent          string `json:"recent_untrusted_content,omitempty"`
	RecentUntrustedContentTruncated bool   `json:"recent_untrusted_content_truncated,omitempty"`
}

func RenderTrajectory(t Trajectory) TrajectoryPayload {
	priorUserRequest, priorUserRequestTruncated := truncatePayloadBody(t.PriorUserRequest, maxTrajectoryBodyLen)
	recent, recentTruncated := truncatePayloadBody(t.RecentUntrustedContent, maxTrajectoryBodyLen)
	return TrajectoryPayload{
		PriorUserRequest:                priorUserRequest,
		PriorUserRequestTruncated:       priorUserRequestTruncated,
		RecentUntrustedContent:          recent,
		RecentUntrustedContentTruncated: recentTruncated,
	}
}

type Payload struct {
	ProducedBy         string            `json:"produced_by"`
	Tool               *ToolPayload      `json:"tool,omitempty"`
	BodyKind           string            `json:"body_kind"`
	Body               string            `json:"body,omitempty"`
	BodyTruncated      bool              `json:"body_truncated,omitempty"`
	ToolCalls          []ToolCallPayload `json:"tool_calls,omitempty"`
	ToolCallsTruncated bool              `json:"tool_calls_truncated,omitempty"`
}

type ToolPayload struct {
	MCPServer   string `json:"mcp_server,omitempty"`
	MCPFunction string `json:"mcp_function,omitempty"`
	Name        string `json:"name,omitempty"`
}

type ToolCallPayload struct {
	Tool               *ToolPayload `json:"tool,omitempty"`
	Arguments          string       `json:"arguments"`
	ArgumentsTruncated bool         `json:"arguments_truncated,omitempty"`
}

// Render returns the judge-visible payload as a compact JSON string. It is what
// gets stored as a finding's Match for llm_judge / prompt_injection detections,
// which have no literal offending substring — the "match" is the entire event
// the judge saw. Best-effort: falls back to the raw body if marshaling fails.
// RenderPayload already truncates body/args, so the result stays bounded.
func Render(m Message) string {
	b, err := json.Marshal(RenderPayload(m))
	if err != nil {
		return m.Body
	}
	return string(b)
}

// RenderPayload maps a judge message onto the JSON payload both prompt judges read.
func RenderPayload(m Message) Payload {
	if len(m.ToolCalls) > 0 {
		calls, truncatedCalls := payloadToolCalls(m.ToolCalls)
		rendered := make([]ToolCallPayload, 0, len(calls))
		for _, c := range calls {
			args, argsTruncated := truncatePayloadBody(c.Arguments, maxPayloadBodyLen)
			rendered = append(rendered, ToolCallPayload{
				Tool:               payloadTool(c.ToolName, c.MCPServer, c.MCPFunction),
				Arguments:          args,
				ArgumentsTruncated: argsTruncated,
			})
		}
		return Payload{
			ProducedBy:         "ai_assistant_tool_call",
			Tool:               nil,
			BodyKind:           "tool_calls",
			Body:               "",
			BodyTruncated:      false,
			ToolCalls:          rendered,
			ToolCallsTruncated: truncatedCalls,
		}
	}

	producedBy, bodyKind := payloadDescriptors(m.Type)
	body, truncated := truncatePayloadBody(m.Body, maxPayloadBodyLen)
	return Payload{
		ProducedBy:         producedBy,
		Tool:               payloadTool(m.ToolName, m.MCPServer, m.MCPFunction),
		BodyKind:           bodyKind,
		Body:               body,
		BodyTruncated:      truncated,
		ToolCalls:          nil,
		ToolCallsTruncated: false,
	}
}

func payloadToolCalls(calls []ToolCall) ([]ToolCall, bool) {
	if len(calls) <= maxPayloadRenderedToolCall {
		return calls, false
	}
	head := maxPayloadRenderedToolCall / 2
	tail := maxPayloadRenderedToolCall - head
	rendered := make([]ToolCall, 0, maxPayloadRenderedToolCall)
	rendered = append(rendered, calls[:head]...)
	rendered = append(rendered, calls[len(calls)-tail:]...)
	return rendered, true
}

func payloadDescriptors(messageType message.Type) (producedBy, bodyKind string) {
	switch messageType {
	case message.User:
		return "end_user", "content"
	case message.Assistant:
		return "ai_assistant", "content"
	case message.ToolRequest:
		return "ai_assistant_tool_call", "arguments"
	case message.ToolResponse:
		return "tool_result", "output"
	default:
		return "unknown", "content"
	}
}

func payloadTool(name, mcpServer, mcpFunction string) *ToolPayload {
	if mcpServer != "" || mcpFunction != "" {
		return &ToolPayload{MCPServer: mcpServer, MCPFunction: mcpFunction, Name: ""}
	}
	if name != "" {
		return &ToolPayload{MCPServer: "", MCPFunction: "", Name: name}
	}
	return nil
}

func truncatePayloadBody(s string, maxLen int) (string, bool) {
	if maxLen <= 0 || utf8.RuneCountInString(s) <= maxLen {
		return s, false
	}
	runes := []rune(s)
	const markerBudget = 40
	budget := max(maxLen-markerBudget, 0)
	dropped := len(runes) - budget
	head := budget * 3 / 5
	tail := budget - head
	var b strings.Builder
	b.WriteString(string(runes[:head]))
	fmt.Fprintf(&b, "\n…[%d characters truncated]…\n", dropped)
	b.WriteString(string(runes[len(runes)-tail:]))
	return b.String(), true
}
