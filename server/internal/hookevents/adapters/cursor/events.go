package cursor

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"net/url"
	"strings"
	"time"

	gen "github.com/speakeasy-api/gram/server/gen/hooks"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/conv"
	"github.com/speakeasy-api/gram/server/internal/hookevents"
)

func Normalize(authCtx *contextvalues.AuthContext, payload *gen.CursorPayload, eventContext hookevents.EventContext, timestamp time.Time) (any, error) {
	if payload == nil {
		return nil, nil
	}

	base := hookevents.Event{
		BaseEvent: hookevents.BaseEvent{
			Provider:     hookevents.ProviderCursor,
			Type:         "",
			RawEventType: payload.HookEventName,
			Timestamp:    timestamp,
			AuthContext:  authCtx,
			Context:      eventContext,
			Raw:          payload,
		},
		ConversationID: conv.PtrValOr(payload.ConversationID, ""),
		TranscriptPath: conv.PtrValOr(payload.TranscriptPath, ""),
		CWD:            "",
		PermissionMode: "",
		Model:          conv.PtrValOr(payload.Model, ""),
		HookHostname:   strings.TrimSpace(conv.PtrValOr(payload.HookHostname, "")),
		AdditionalData: payload.AdditionalData,
	}

	switch payload.HookEventName {
	case "beforeSubmitPrompt":
		return hookevents.NewUserPromptSubmit(base, hookevents.UserPromptSubmitParams{
			Prompt: conv.PtrValOr(payload.Prompt, ""),
		}), nil
	case "afterAgentResponse":
		return hookevents.NewAfterAgentResponse(base, hookevents.AfterAgentResponseParams{
			Text:         conv.PtrValOr(payload.Text, ""),
			InputTokens:  conv.PtrValOr(payload.InputTokens, 0),
			OutputTokens: conv.PtrValOr(payload.OutputTokens, 0),
		}), nil
	case "afterAgentThought":
		return hookevents.NewAfterAgentThought(base, hookevents.AfterAgentThoughtParams{
			Text:       conv.PtrValOr(payload.Text, ""),
			DurationMs: conv.PtrValOr(payload.DurationMs, 0),
		}), nil
	case "preToolUse":
		return hookevents.NewBeforeToolUse(base, hookevents.BeforeToolUseParams{
			ToolCallID: cursorToolCorrelationID(payload),
			ToolName:   conv.PtrValOr(payload.ToolName, ""),
			ToolInput:  payload.ToolInput,
		}), nil
	case "postToolUse":
		return hookevents.NewAfterToolUse(base, hookevents.AfterToolUseParams{
			ToolCallID: cursorToolCorrelationID(payload),
			ToolName:   conv.PtrValOr(payload.ToolName, ""),
			ToolOutput: payload.ToolResponse,
		}), nil
	case "postToolUseFailure":
		return hookevents.NewAfterToolUseFailure(base, hookevents.AfterToolUseFailureParams{
			ToolCallID:  cursorToolCorrelationID(payload),
			ToolName:    conv.PtrValOr(payload.ToolName, ""),
			Error:       payload.Error,
			IsInterrupt: conv.PtrValOr(payload.IsInterrupt, false),
		}), nil
	case "beforeMCPExecution":
		return hookevents.NewBeforeMCPExecution(base, hookevents.BeforeMCPExecutionParams{
			ToolCallID:   cursorToolCorrelationID(payload),
			ToolName:     conv.PtrValOr(payload.ToolName, ""),
			ToolInput:    payload.ToolInput,
			ToolSource:   cursorMCPToolSource(payload),
			MCPServerURL: conv.PtrValOr(payload.URL, ""),
		}), nil
	case "afterMCPExecution":
		toolOutput, isError := cursorMCPToolOutput(payload)
		return hookevents.NewAfterMCPExecution(base, hookevents.AfterMCPExecutionParams{
			ToolCallID:   cursorToolCorrelationID(payload),
			ToolName:     conv.PtrValOr(payload.ToolName, ""),
			ToolOutput:   toolOutput,
			ToolSource:   cursorMCPToolSource(payload),
			MCPServerURL: conv.PtrValOr(payload.URL, ""),
			IsError:      isError,
		}), nil
	case "stop":
		return hookevents.NewStop(base, hookevents.StopParams{
			LastAssistantMessage: "",
			InputTokens:          conv.PtrValOr(payload.InputTokens, 0),
			OutputTokens:         conv.PtrValOr(payload.OutputTokens, 0),
		}), nil
	default:
		return nil, nil
	}
}

func cursorToolCorrelationID(payload *gen.CursorPayload) string {
	if payload.ToolUseID != nil && *payload.ToolUseID != "" {
		return *payload.ToolUseID
	}

	convID := conv.PtrValOr(payload.ConversationID, "")
	genID := conv.PtrValOr(payload.GenerationID, "")
	toolName := strings.TrimPrefix(conv.PtrValOr(payload.ToolName, ""), "MCP:")
	if convID == "" && genID == "" && toolName == "" && payload.ToolInput == nil {
		return ""
	}

	var b strings.Builder
	b.WriteString(convID)
	b.WriteByte('|')
	b.WriteString(genID)
	b.WriteByte('|')
	b.WriteString(toolName)
	b.WriteByte('|')
	if payload.ToolInput != nil {
		if jsonBytes, err := json.Marshal(payload.ToolInput); err == nil {
			b.Write(jsonBytes)
		}
	}

	sum := sha256.Sum256([]byte(b.String()))
	return "cursor_synth_" + hex.EncodeToString(sum[:8])
}

func cursorMCPToolSource(payload *gen.CursorPayload) string {
	if payload.URL != nil && *payload.URL != "" {
		if u, err := url.Parse(*payload.URL); err == nil && u.Host != "" {
			return u.Host
		}
		return *payload.URL
	}
	return conv.PtrValOr(payload.Command, "")
}

func cursorMCPToolOutput(payload *gen.CursorPayload) (any, bool) {
	if payload.ResultJSON == nil || *payload.ResultJSON == "" {
		return payload.ToolResponse, false
	}

	var parsed struct {
		IsError bool `json:"isError"`
	}
	isError := json.Unmarshal([]byte(*payload.ResultJSON), &parsed) == nil && parsed.IsError
	return hookevents.JSONString(*payload.ResultJSON), isError
}
