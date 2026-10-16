package codemode

import (
	"bytes"
	"encoding/json"
	"fmt"
	"strings"
)

// NormalizeResult preserves structured and content results without exposing _meta.
func NormalizeResult(raw json.RawMessage) (*ToolResult, error) {
	if len(raw) > MaxResultBytes {
		return nil, fmt.Errorf("callback result exceeds limit")
	}
	var result struct {
		StructuredContent json.RawMessage              `json:"structuredContent"`
		Content           []map[string]json.RawMessage `json:"content"`
		IsError           bool                         `json:"isError"`
	}
	if err := json.Unmarshal(raw, &result); err != nil || bytes.Equal(bytes.TrimSpace(raw), []byte("null")) {
		return nil, fmt.Errorf("invalid MCP tool result")
	}
	for _, block := range result.Content {
		delete(block, "_meta")
		if rawResource, ok := block["resource"]; ok {
			var resource map[string]json.RawMessage
			if err := json.Unmarshal(rawResource, &resource); err != nil || resource == nil {
				return nil, fmt.Errorf("invalid embedded MCP resource")
			}
			delete(resource, "_meta")
			publicResource, err := json.Marshal(resource)
			if err != nil {
				return nil, fmt.Errorf("encode embedded resource: %w", err)
			}
			block["resource"] = publicResource
		}
	}
	if result.Content == nil {
		result.Content = []map[string]json.RawMessage{}
	}
	content, err := json.Marshal(result.Content)
	if err != nil {
		return nil, fmt.Errorf("encode MCP content: %w", err)
	}
	data := result.StructuredContent
	if len(data) == 0 {
		data = json.RawMessage("null")
	}
	message := ""
	if result.IsError {
		message = "upstream tool returned an error; inspect content"
	}
	return &ToolResult{Outcome: "completed", Warnings: nil, OK: !result.IsError, Data: data, Content: content, Error: message}, nil
}

// DispatchError preserves uncertainty when a transport fails after admission.
// BeforeInvoke is set only when the backend knows that dispatch never began.
type DispatchError struct {
	// BeforeInvoke distinguishes a guard refusal from an unknown external outcome.
	BeforeInvoke bool
	// Code is a public error code; private causes stay in Speakeasy logs.
	Code string
	// Rejection is the member's own refusal of the call, shown to the program.
	Rejection string
}

func (e *DispatchError) Error() string { return e.Code }

func failedResult(outcome, code string) *ToolResult {
	return &ToolResult{Outcome: outcome, Warnings: nil, OK: false, Data: json.RawMessage("null"), Content: json.RawMessage("[]"), Error: code}
}

// validateArgumentShape bounds arguments before dispatch; input schemas are
// the member's to enforce.
func validateArgumentShape(arguments json.RawMessage) error {
	var object map[string]json.RawMessage
	if len(arguments) > MaxResultBytes || json.Unmarshal(arguments, &object) != nil || object == nil {
		return fmt.Errorf("arguments must be a bounded JSON object")
	}
	return nil
}

// maxRejectionBytes bounds a member's refusal message shown to the program.
const maxRejectionBytes = 1 << 10

// rejectedResult reports a member's definitive refusal, such as an unknown
// tool or invalid arguments, as a completed failed call the program can fix.
func rejectedResult(message string) *ToolResult {
	if len(message) > maxRejectionBytes {
		message = strings.ToValidUTF8(message[:maxRejectionBytes], "")
	}
	content, err := json.Marshal([]map[string]string{{"type": "text", "text": message}})
	if err != nil {
		content = json.RawMessage("[]")
	}
	return &ToolResult{Outcome: "completed", Warnings: nil, OK: false, Data: json.RawMessage("null"), Content: content, Error: "tool_call_rejected"}
}
