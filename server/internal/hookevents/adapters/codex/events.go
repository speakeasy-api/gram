package codex

import (
	"time"

	gen "github.com/speakeasy-api/gram/server/gen/hooks"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/conv"
	"github.com/speakeasy-api/gram/server/internal/hookevents"
)

func Normalize(authCtx *contextvalues.AuthContext, payload *gen.CodexPayload, identity hookevents.Identity, timestamp time.Time) (any, error) {
	if payload == nil {
		return nil, nil
	}

	base := hookevents.Event{
		Provider:       hookevents.ProviderCodex,
		RawEventType:   payload.HookEventName,
		Timestamp:      timestamp,
		AuthContext:    authCtx,
		Identity:       identity,
		ConversationID: conv.PtrValOr(payload.SessionID, ""),
		Raw:            payload,
	}

	switch payload.HookEventName {
	case "SessionStart":
		return hookevents.NewSessionStart(base), nil
	case "PreToolUse":
		return hookevents.NewBeforeToolUse(base, hookevents.BeforeToolUseParams{
			ToolName:  conv.PtrValOr(payload.ToolName, ""),
			ToolInput: payload.ToolInput,
		}), nil
	case "PostToolUse":
		return hookevents.NewAfterToolUse(base, hookevents.AfterToolUseParams{
			ToolName:   conv.PtrValOr(payload.ToolName, ""),
			ToolOutput: payload.ToolOutput,
		}), nil
	case "PermissionRequest":
		return hookevents.NewPermissionRequest(base, hookevents.PermissionRequestParams{
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
