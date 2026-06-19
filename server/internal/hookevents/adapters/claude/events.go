package claude

import (
	"time"

	gen "github.com/speakeasy-api/gram/server/gen/hooks"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/conv"
	"github.com/speakeasy-api/gram/server/internal/hookevents"
)

func Normalize(authCtx *contextvalues.AuthContext, payload *gen.ClaudePayload, identity hookevents.Identity, timestamp time.Time) (any, bool, error) {
	if payload == nil {
		return nil, false, nil
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
		return hookevents.NewSessionStart(base), true, nil
	case "ConfigChange":
		return hookevents.NewConfigChange(base), true, nil
	case "PreToolUse":
		return hookevents.NewBeforeToolUse(base, conv.PtrValOr(payload.ToolName, ""), payload.ToolInput), true, nil
	case "PostToolUse":
		return hookevents.NewAfterToolUse(base, conv.PtrValOr(payload.ToolName, ""), payload.ToolResponse), true, nil
	case "PostToolUseFailure":
		return hookevents.NewAfterToolUseFailure(base, conv.PtrValOr(payload.ToolName, ""), payload.Error, conv.PtrValOr(payload.IsInterrupt, false)), true, nil
	case "UserPromptSubmit":
		return hookevents.NewUserPromptSubmit(base, conv.PtrValOr(payload.Prompt, "")), true, nil
	case "Stop":
		return hookevents.NewStop(base, conv.PtrValOr(payload.LastAssistantMessage, "")), true, nil
	case "SessionEnd":
		return hookevents.NewSessionEnd(base, conv.PtrValOr(payload.Reason, "")), true, nil
	case "Notification":
		return hookevents.NewNotification(base, conv.PtrValOr(payload.NotificationType, ""), conv.PtrValOr(payload.Message, ""), conv.PtrValOr(payload.Title, "")), true, nil
	default:
		return nil, false, nil
	}
}
