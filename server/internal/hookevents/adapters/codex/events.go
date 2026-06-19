package codex

import (
	"time"

	gen "github.com/speakeasy-api/gram/server/gen/hooks"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/conv"
	"github.com/speakeasy-api/gram/server/internal/hookevents"
)

func Normalize(authCtx *contextvalues.AuthContext, payload *gen.CodexPayload, identity hookevents.Identity, timestamp time.Time) (any, bool, error) {
	if payload == nil {
		return nil, false, nil
	}

	base := hookevents.Event{
		Provider:       hookevents.ProviderCodex,
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
	case "PreToolUse":
		return hookevents.NewBeforeToolUse(base, conv.PtrValOr(payload.ToolName, ""), payload.ToolInput), true, nil
	case "PostToolUse":
		return hookevents.NewAfterToolUse(base, conv.PtrValOr(payload.ToolName, ""), payload.ToolOutput), true, nil
	case "PermissionRequest":
		return hookevents.NewPermissionRequest(base, conv.PtrValOr(payload.ToolName, ""), payload.ToolInput, conv.PtrValOr(payload.PermissionType, "")), true, nil
	case "UserPromptSubmit":
		return hookevents.NewUserPromptSubmit(base, conv.PtrValOr(payload.Prompt, "")), true, nil
	case "Stop":
		return hookevents.NewStop(base, conv.PtrValOr(payload.LastAssistantMessage, "")), true, nil
	default:
		return nil, false, nil
	}
}
