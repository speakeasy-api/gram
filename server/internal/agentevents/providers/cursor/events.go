package cursor

import (
	gen "github.com/speakeasy-api/gram/server/gen/hooks"
	"github.com/speakeasy-api/gram/server/internal/agentevents"
	"github.com/speakeasy-api/gram/server/internal/agentevents/types"
)

const (
	HookEventBeforeSubmitPrompt types.HookEventType = "beforeSubmitPrompt"
	HookEventAfterAgentResponse types.HookEventType = "afterAgentResponse"
	HookEventPreToolUse         types.HookEventType = "preToolUse"
	HookEventPostToolUse        types.HookEventType = "postToolUse"
	HookEventPostToolUseFailure types.HookEventType = "postToolUseFailure"
	HookEventBeforeMCPExecution types.HookEventType = "beforeMCPExecution"
	HookEventAfterMCPExecution  types.HookEventType = "afterMCPExecution"
	HookEventStop               types.HookEventType = "stop"
)

var (
	GetEventType  agentevents.FieldResolver[*gen.CursorPayload, types.EventType] = getEventType
	GetPrompt     agentevents.FieldResolver[*gen.CursorPayload, string]          = agentevents.GetField[*gen.CursorPayload, string]("Prompt")
	GetToolName   agentevents.FieldResolver[*gen.CursorPayload, string]          = agentevents.GetField[*gen.CursorPayload, string]("ToolName")
	GetToolInput  agentevents.FieldResolver[*gen.CursorPayload, any]             = agentevents.GetField[*gen.CursorPayload, any]("ToolInput")
	GetToolOutput agentevents.FieldResolver[*gen.CursorPayload, any]             = agentevents.GetField[*gen.CursorPayload, any]("ToolResponse")
)

func getEventType(ev agentevents.Event[*gen.CursorPayload]) (types.EventType, bool, error) {
	payload := ev.Raw()
	if payload == nil {
		return "", false, nil
	}
	hookEventType, ok := ParseHookEventType(payload.HookEventName)
	if !ok {
		return "", false, nil
	}
	eventType, ok := eventTypeFromHookEventType(hookEventType)
	if !ok {
		return "", false, nil
	}
	return eventType, true, nil
}

func ParseHookEventType(raw string) (types.HookEventType, bool) {
	switch types.HookEventType(raw) {
	case HookEventBeforeSubmitPrompt:
		return HookEventBeforeSubmitPrompt, true
	case HookEventAfterAgentResponse:
		return HookEventAfterAgentResponse, true
	case HookEventPreToolUse:
		return HookEventPreToolUse, true
	case HookEventPostToolUse:
		return HookEventPostToolUse, true
	case HookEventPostToolUseFailure:
		return HookEventPostToolUseFailure, true
	case HookEventBeforeMCPExecution:
		return HookEventBeforeMCPExecution, true
	case HookEventAfterMCPExecution:
		return HookEventAfterMCPExecution, true
	case HookEventStop:
		return HookEventStop, true
	default:
		return "", false
	}
}

func eventTypeFromHookEventType(hookEventType types.HookEventType) (types.EventType, bool) {
	switch hookEventType {
	case HookEventBeforeSubmitPrompt:
		return types.UserPromptSubmit, true
	case HookEventAfterAgentResponse:
		return types.AfterAgentResponse, true
	case HookEventPreToolUse:
		return types.BeforeToolUse, true
	case HookEventPostToolUse:
		return types.AfterToolUse, true
	case HookEventPostToolUseFailure:
		return types.AfterToolUseFailure, true
	case HookEventBeforeMCPExecution:
		return types.BeforeMCPExecution, true
	case HookEventAfterMCPExecution:
		return types.AfterMCPExecution, true
	case HookEventStop:
		return types.Stop, true
	default:
		return "", false
	}
}
