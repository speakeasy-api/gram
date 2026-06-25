package codex

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"strings"
	"time"

	gen "github.com/speakeasy-api/gram/server/gen/hooks"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/conv"
	"github.com/speakeasy-api/gram/server/internal/hookevents"
)

func Normalize(authCtx *contextvalues.AuthContext, payload *gen.CodexPayload, eventContext hookevents.EventContext, timestamp time.Time) (any, error) {
	if payload == nil {
		return nil, nil
	}

	base := hookevents.Event{
		BaseEvent: hookevents.BaseEvent{
			Provider:     hookevents.ProviderCodex,
			Type:         "",
			RawEventType: payload.HookEventName,
			Timestamp:    timestamp,
			AuthContext:  authCtx,
			Context:      eventContext,
			Raw:          payload,
		},
		ConversationID: conv.PtrValOr(payload.SessionID, ""),
		TranscriptPath: conv.PtrValOr(payload.TranscriptPath, ""),
		CWD:            conv.PtrValOr(payload.Cwd, ""),
		PermissionMode: "",
		Model:          conv.PtrValOr(payload.Model, ""),
		HookHostname:   strings.TrimSpace(conv.PtrValOr(payload.HookHostname, "")),
		AdditionalData: payload.AdditionalData,
	}

	switch payload.HookEventName {
	case "SessionStart":
		return hookevents.NewSessionStart(base), nil
	case "PreToolUse":
		return hookevents.NewBeforeToolUse(base, hookevents.BeforeToolUseParams{
			ToolCallID: codexToolCorrelationID(payload),
			ToolName:   conv.PtrValOr(payload.ToolName, ""),
			ToolInput:  payload.ToolInput,
		}), nil
	case "PostToolUse":
		return hookevents.NewAfterToolUse(base, hookevents.AfterToolUseParams{
			ToolCallID: codexToolCorrelationID(payload),
			ToolName:   conv.PtrValOr(payload.ToolName, ""),
			ToolOutput: payload.ToolOutput,
		}), nil
	case "PermissionRequest":
		return hookevents.NewPermissionRequest(base, hookevents.PermissionRequestParams{
			ToolCallID:     codexToolCorrelationID(payload),
			ToolName:       conv.PtrValOr(payload.ToolName, ""),
			ToolInput:      payload.ToolInput,
			PermissionType: conv.PtrValOr(payload.PermissionType, ""),
		}), nil
	case "UserPromptSubmit":
		return hookevents.NewUserPromptSubmit(base, hookevents.UserPromptSubmitParams{
			Prompt: conv.PtrValOr(payload.Prompt, ""),
		}), nil
	case "Stop":
		return hookevents.NewStop(base, hookevents.StopParams{
			LastAssistantMessage: conv.PtrValOr(payload.LastAssistantMessage, ""),
			InputTokens:          0,
			OutputTokens:         0,
		}), nil
	default:
		return nil, nil
	}
}

func codexToolCorrelationID(payload *gen.CodexPayload) string {
	if payload.IdempotencyKey != nil && *payload.IdempotencyKey != "" {
		return *payload.IdempotencyKey
	}

	sessionID := conv.PtrValOr(payload.SessionID, "")
	toolName := conv.PtrValOr(payload.ToolName, "")
	if sessionID == "" && toolName == "" && payload.ToolInput == nil && payload.ToolOutput == nil {
		return ""
	}

	var b strings.Builder
	b.WriteString(sessionID)
	b.WriteByte('|')
	b.WriteString(toolName)
	b.WriteByte('|')
	if payload.ToolInput != nil {
		if jsonBytes, err := json.Marshal(payload.ToolInput); err == nil {
			b.Write(jsonBytes)
		}
	}
	b.WriteByte('|')
	if payload.ToolOutput != nil {
		if jsonBytes, err := json.Marshal(payload.ToolOutput); err == nil {
			b.Write(jsonBytes)
		}
	}

	sum := sha256.Sum256([]byte(b.String()))
	return "codex_synth_" + hex.EncodeToString(sum[:8])
}
