package codex

import (
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
			ToolCallID: "",
			ToolName:   conv.PtrValOr(payload.ToolName, ""),
			ToolInput:  payload.ToolInput,
		}), nil
	case "PostToolUse":
		return hookevents.NewAfterToolUse(base, hookevents.AfterToolUseParams{
			ToolCallID: "",
			ToolName:   conv.PtrValOr(payload.ToolName, ""),
			ToolOutput: payload.ToolOutput,
		}), nil
	case "PermissionRequest":
		return hookevents.NewPermissionRequest(base, hookevents.PermissionRequestParams{
			ToolCallID:     "",
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
		}), nil
	default:
		return nil, nil
	}
}
