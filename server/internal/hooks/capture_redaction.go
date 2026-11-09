package hooks

import (
	"bytes"
	"context"
	"encoding/json"

	gen "github.com/speakeasy-api/gram/server/gen/hooks"
	"github.com/speakeasy-api/gram/server/internal/attr"
	"github.com/speakeasy-api/gram/server/internal/captureredact"
)

const pendingCaptureRedactionsField = "_speakeasy_pending_capture_redactions"

type pendingHookContent struct {
	// ToolName is the observed tool label.
	ToolName *string `json:"tool_name"`
	// Cwd and TranscriptPath are descriptive paths retained with the capture.
	Cwd            *string `json:"cwd"`
	TranscriptPath *string `json:"transcript_path"`
	// Model identifies the reported model without retaining a credential in its label.
	Model *string `json:"model"`
	// ToolInput is the observed tool invocation.
	ToolInput any `json:"tool_input"`
	// ToolResponse is the observed tool output.
	ToolResponse any `json:"tool_response"`
	// Error is the provider's tool failure payload.
	Error any `json:"error"`
	// AdditionalData carries provider-specific content.
	AdditionalData map[string]any `json:"additional_data"`
	// Prompt is the user's submitted text.
	Prompt *string `json:"prompt"`
	// LastAssistantMessage is the final assistant text.
	LastAssistantMessage *string `json:"last_assistant_message"`
	// Message is notification text.
	Message *string `json:"message"`
	// Title is the notification label.
	Title *string `json:"title"`
	// Reason is the provider's explanatory text.
	Reason *string `json:"reason"`
}

// redactPendingHook protects the legacy buffer before attribution is known.
// Its safe locations cannot claim a tenant fingerprint or policy decision.
func redactPendingHook(ctx context.Context, payload *gen.ClaudePayload) *gen.ClaudePayload {
	if payload == nil {
		return nil
	}
	captured := *payload
	captured.ApikeyToken, captured.DeviceAgentToken = nil, nil
	content := pendingHookContent{
		ToolName: payload.ToolName, Cwd: payload.Cwd, TranscriptPath: payload.TranscriptPath, Model: payload.Model,
		ToolInput: payload.ToolInput, ToolResponse: payload.ToolResponse, Error: payload.Error,
		AdditionalData: payload.AdditionalData, Prompt: payload.Prompt,
		LastAssistantMessage: payload.LastAssistantMessage, Message: payload.Message, Title: payload.Title, Reason: payload.Reason,
	}
	data, err := json.Marshal(content)
	if err != nil {
		return nil
	}
	redacted, report := captureredact.RedactJSONWithReport(ctx, data)
	var clean pendingHookContent
	if json.Unmarshal(redacted, &clean) != nil {
		return nil
	}
	captured.ToolInput, captured.ToolResponse, captured.Error = clean.ToolInput, clean.ToolResponse, clean.Error
	captured.ToolName, captured.Cwd, captured.TranscriptPath, captured.Model = clean.ToolName, clean.Cwd, clean.TranscriptPath, clean.Model
	captured.AdditionalData, captured.Prompt = clean.AdditionalData, clean.Prompt
	captured.LastAssistantMessage, captured.Message, captured.Title, captured.Reason = clean.LastAssistantMessage, clean.Message, clean.Title, clean.Reason
	if len(report.Findings) > 0 || report.Incomplete {
		if captured.AdditionalData == nil {
			captured.AdditionalData = make(map[string]any)
		}
		encoded, err := json.Marshal(report)
		if err != nil {
			return nil
		}
		var value any
		if json.Unmarshal(encoded, &value) != nil {
			return nil
		}
		captured.AdditionalData[pendingCaptureRedactionsField] = value
	}
	return &captured
}

func cloneCaptureAttributes(ctx context.Context, attrs map[attr.Key]any) map[attr.Key]any {
	if attrs == nil {
		return nil
	}
	raw := make(map[string]json.RawMessage, len(attrs))
	for key, value := range attrs {
		encoded, err := json.Marshal(value)
		if err != nil {
			encoded = json.RawMessage(`"[redacted]"`)
		}
		raw[string(key)] = encoded
	}
	encoded, err := json.Marshal(raw)
	if err != nil {
		return nil
	}
	var redacted map[string]json.RawMessage
	if json.Unmarshal(captureredact.RedactJSON(ctx, encoded), &redacted) != nil {
		return nil
	}
	result := make(map[attr.Key]any, len(redacted))
	for key, value := range redacted {
		if bytes.Equal(raw[key], value) {
			result[attr.Key(key)] = attrs[attr.Key(key)]
			continue
		}
		var text string
		if json.Unmarshal(value, &text) == nil {
			result[attr.Key(key)] = text
		} else {
			result[attr.Key(key)] = value
		}
	}
	return result
}
