package claude

import (
	"time"

	gen "github.com/speakeasy-api/gram/server/gen/hooks"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/conv"
	"github.com/speakeasy-api/gram/server/internal/hookevents"
)

func Normalize(authCtx *contextvalues.AuthContext, payload *gen.ClaudePayload, identity hookevents.Identity, timestamp time.Time) (any, error) {
	if payload == nil {
		return nil, nil
	}

	base := hookevents.Event{
		Provider:       hookevents.ProviderClaude,
		RawEventType:   payload.HookEventName,
		Timestamp:      timestamp,
		AuthContext:    authCtx,
		OrganizationID: identity.OrganizationID,
		ProjectID:      identity.ProjectID,
		UserID:         identity.UserID,
		UserEmail:      identity.UserEmail,
		ConversationID: conv.PtrValOr(payload.SessionID, ""),
		Raw:            payload,
	}

	switch payload.HookEventName {
	case "SessionStart":
		return hookevents.NewSessionStart(base), nil
	case "ConfigChange":
		return hookevents.NewConfigChange(base), nil
	case "PreToolUse":
		return hookevents.NewBeforeToolUse(base, hookevents.BeforeToolUseParams{
			ToolName:  conv.PtrValOr(payload.ToolName, ""),
			ToolInput: payload.ToolInput,
		}), nil
	case "PostToolUse":
		return hookevents.NewAfterToolUse(base, hookevents.AfterToolUseParams{
			ToolName:   conv.PtrValOr(payload.ToolName, ""),
			ToolOutput: payload.ToolResponse,
		}), nil
	case "PostToolUseFailure":
		return hookevents.NewAfterToolUseFailure(base, hookevents.AfterToolUseFailureParams{
			ToolName:    conv.PtrValOr(payload.ToolName, ""),
			Error:       payload.Error,
			IsInterrupt: conv.PtrValOr(payload.IsInterrupt, false),
		}), nil
	case "UserPromptSubmit":
		return hookevents.NewUserPromptSubmit(base, hookevents.UserPromptSubmitParams{
			Prompt: conv.PtrValOr(payload.Prompt, ""),
		}), nil
	case "Stop":
		return hookevents.NewStop(base, hookevents.StopParams{
			LastAssistantMessage: conv.PtrValOr(payload.LastAssistantMessage, ""),
		}), nil
	case "SessionEnd":
		return hookevents.NewSessionEnd(base, hookevents.SessionEndParams{
			Reason: conv.PtrValOr(payload.Reason, ""),
		}), nil
	case "Notification":
		return hookevents.NewNotification(base, hookevents.NotificationParams{
			NotificationType: conv.PtrValOr(payload.NotificationType, ""),
			Message:          conv.PtrValOr(payload.Message, ""),
			Title:            conv.PtrValOr(payload.Title, ""),
		}), nil
	default:
		return nil, nil
	}
}
