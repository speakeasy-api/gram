package cursor

import (
	"encoding/json"
	"fmt"

	gen "github.com/speakeasy-api/gram/server/gen/hooks"
	"github.com/speakeasy-api/gram/server/internal/agentevents"
	"github.com/speakeasy-api/gram/server/internal/agentevents/types"
	"github.com/speakeasy-api/gram/server/internal/message"
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
	GetEventType       agentevents.FieldResolver[*gen.CursorPayload, types.EventType] = getEventType
	GetScannableText   agentevents.FieldResolver[*gen.CursorPayload, string]          = getScannableText
	GetScanMessageType agentevents.FieldResolver[*gen.CursorPayload, any]             = getScanMessageType
	GetToolName        agentevents.FieldResolver[*gen.CursorPayload, string]          = agentevents.GetField[*gen.CursorPayload, string]("ToolName")
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

func getScannableText(ev agentevents.Event[*gen.CursorPayload]) (string, bool, error) {
	payload := ev.Raw()
	if payload == nil {
		return "", false, nil
	}

	eventType, ok, err := getEventType(ev)
	if err != nil || !ok {
		return "", false, err
	}

	switch eventType {
	case types.UserPromptSubmit:
		if payload.Prompt != nil && *payload.Prompt != "" {
			return *payload.Prompt, true, nil
		}
	case types.ToolCallStarted, types.MCPToolCallStarted:
		if payload.ToolInput != nil {
			return marshalToJSON(payload.ToolInput), true, nil
		}
	}
	return "", false, nil
}

func getScanMessageType(ev agentevents.Event[*gen.CursorPayload]) (any, bool, error) {
	eventType, ok, err := getEventType(ev)
	if err != nil || !ok {
		return nil, false, err
	}

	switch eventType {
	case types.UserPromptSubmit:
		return message.User, true, nil
	case types.ToolCallStarted, types.MCPToolCallStarted:
		return message.ToolRequest, true, nil
	default:
		return nil, false, nil
	}
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
		return types.AssistantResponseComplete, true
	case HookEventPreToolUse:
		return types.ToolCallStarted, true
	case HookEventPostToolUse:
		return types.ToolCallCompleted, true
	case HookEventPostToolUseFailure:
		return types.ToolCallFailed, true
	case HookEventBeforeMCPExecution:
		return types.MCPToolCallStarted, true
	case HookEventAfterMCPExecution:
		return types.MCPToolCallCompleted, true
	case HookEventStop:
		return types.SessionEnded, true
	default:
		return "", false
	}
}

func marshalToJSON(v any) string {
	if v == nil {
		return ""
	}
	switch t := v.(type) {
	case string:
		return t
	case []byte:
		return string(t)
	}
	b, err := json.Marshal(v)
	if err != nil {
		return fmt.Sprintf("%v", v)
	}
	return string(b)
}
