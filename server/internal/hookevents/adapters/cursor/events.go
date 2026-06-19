package cursor

import (
	"time"

	gen "github.com/speakeasy-api/gram/server/gen/hooks"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/conv"
	"github.com/speakeasy-api/gram/server/internal/hookevents"
)

func Normalize(authCtx *contextvalues.AuthContext, payload *gen.CursorPayload, identity hookevents.Identity, timestamp time.Time) (any, error) {
	if payload == nil {
		return nil, nil
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
		return hookevents.NewUserPromptSubmit(base, hookevents.UserPromptSubmitParams{
			Prompt: conv.PtrValOr(payload.Prompt, ""),
		}), nil
	case "afterAgentResponse":
		return hookevents.NewAfterAgentResponse(base, hookevents.AfterAgentResponseParams{
			Text: conv.PtrValOr(payload.Text, ""),
		}), nil
	case "afterAgentThought":
		return hookevents.NewAfterAgentThought(base, hookevents.AfterAgentThoughtParams{
			Text:       conv.PtrValOr(payload.Text, ""),
			DurationMs: conv.PtrValOr(payload.DurationMs, 0),
		}), nil
	case "preToolUse":
		return hookevents.NewBeforeToolUse(base, hookevents.BeforeToolUseParams{
			ToolName:  conv.PtrValOr(payload.ToolName, ""),
			ToolInput: payload.ToolInput,
		}), nil
	case "postToolUse":
		return hookevents.NewAfterToolUse(base, hookevents.AfterToolUseParams{
			ToolName:   conv.PtrValOr(payload.ToolName, ""),
			ToolOutput: payload.ToolResponse,
		}), nil
	case "postToolUseFailure":
		return hookevents.NewAfterToolUseFailure(base, hookevents.AfterToolUseFailureParams{
			ToolName:    conv.PtrValOr(payload.ToolName, ""),
			Error:       payload.Error,
			IsInterrupt: conv.PtrValOr(payload.IsInterrupt, false),
		}), nil
	case "beforeMCPExecution":
		return hookevents.NewBeforeMCPExecution(base, hookevents.BeforeMCPExecutionParams{
			ToolName:  conv.PtrValOr(payload.ToolName, ""),
			ToolInput: payload.ToolInput,
		}), nil
	case "afterMCPExecution":
		return hookevents.NewAfterMCPExecution(base, hookevents.AfterMCPExecutionParams{
			ToolName:   conv.PtrValOr(payload.ToolName, ""),
			ToolOutput: payload.ToolResponse,
		}), nil
	case "stop":
		return hookevents.NewStop(base, hookevents.StopParams{}), nil
	default:
		return nil, nil
	}
}
