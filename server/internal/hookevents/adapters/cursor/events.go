package cursor

import (
	"time"

	gen "github.com/speakeasy-api/gram/server/gen/hooks"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/conv"
	"github.com/speakeasy-api/gram/server/internal/hookevents"
)

func Normalize(authCtx *contextvalues.AuthContext, payload *gen.CursorPayload, identity hookevents.Identity, timestamp time.Time) (any, bool, error) {
	if payload == nil {
		return nil, false, nil
	}

	base := hookevents.Event{
		Provider:       hookevents.ProviderCursor,
		RawEventType:   payload.HookEventName,
		Timestamp:      timestamp,
		AuthContext:    authCtx,
		OrganizationID: identity.OrganizationID,
		ProjectID:      identity.ProjectID,
		UserID:         identity.UserID,
		UserEmail:      identity.UserEmail,
		ConversationID: conv.PtrValOr(payload.ConversationID, ""),
		Raw:            payload,
	}

	switch payload.HookEventName {
	case "beforeSubmitPrompt":
		return hookevents.NewUserPromptSubmit(base, conv.PtrValOr(payload.Prompt, "")), true, nil
	case "afterAgentResponse":
		return hookevents.NewAfterAgentResponse(base, conv.PtrValOr(payload.Text, "")), true, nil
	case "afterAgentThought":
		return hookevents.NewAfterAgentThought(base, conv.PtrValOr(payload.Text, ""), conv.PtrValOr(payload.DurationMs, 0)), true, nil
	case "preToolUse":
		return hookevents.NewBeforeToolUse(base, conv.PtrValOr(payload.ToolName, ""), payload.ToolInput), true, nil
	case "postToolUse":
		return hookevents.NewAfterToolUse(base, conv.PtrValOr(payload.ToolName, ""), payload.ToolResponse), true, nil
	case "postToolUseFailure":
		return hookevents.NewAfterToolUseFailure(base, conv.PtrValOr(payload.ToolName, ""), payload.Error, conv.PtrValOr(payload.IsInterrupt, false)), true, nil
	case "beforeMCPExecution":
		return hookevents.NewBeforeMCPExecution(base, conv.PtrValOr(payload.ToolName, ""), payload.ToolInput), true, nil
	case "afterMCPExecution":
		return hookevents.NewAfterMCPExecution(base, conv.PtrValOr(payload.ToolName, ""), payload.ToolResponse), true, nil
	case "stop":
		return hookevents.NewStop(base, ""), true, nil
	default:
		return nil, false, nil
	}
}
