package cursor

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"net/url"
	"strings"

	gen "github.com/speakeasy-api/gram/server/gen/hooks"
	"github.com/speakeasy-api/gram/server/internal/agentevents"
	"github.com/speakeasy-api/gram/server/internal/agentevents/types"
	"github.com/speakeasy-api/gram/server/internal/conv"
	"github.com/speakeasy-api/gram/server/internal/message"
)

const Agent types.Provider = "cursor"

type EventType string

const (
	EventBeforeSubmitPrompt EventType = "beforeSubmitPrompt"
	EventAfterAgentResponse EventType = "afterAgentResponse"
	EventAfterAgentThought  EventType = "afterAgentThought"
	EventPreToolUse         EventType = "preToolUse"
	EventPostToolUse        EventType = "postToolUse"
	EventPostToolUseFailure EventType = "postToolUseFailure"
	EventBeforeMCPExecution EventType = "beforeMCPExecution"
	EventAfterMCPExecution  EventType = "afterMCPExecution"
	EventStop               EventType = "stop"
)

var GetEventType agentevents.FieldResolver[*gen.CursorPayload] = getEventType
var GetHookSource agentevents.FieldResolver[*gen.CursorPayload] = getHookSource
var GetHookHostname agentevents.FieldResolver[*gen.CursorPayload] = agentevents.StringField[*gen.CursorPayload]("HookHostname")
var GetBlockReason agentevents.FieldResolver[*gen.CursorPayload] = getBlockReason
var GetError agentevents.FieldResolver[*gen.CursorPayload] = getError
var GetPrompt agentevents.FieldResolver[*gen.CursorPayload] = agentevents.StringField[*gen.CursorPayload]("Prompt")
var GetAssistantText agentevents.FieldResolver[*gen.CursorPayload] = agentevents.StringField[*gen.CursorPayload]("Text")
var GetModel agentevents.FieldResolver[*gen.CursorPayload] = agentevents.StringField[*gen.CursorPayload]("Model")
var GetScannableText agentevents.FieldResolver[*gen.CursorPayload] = getScannableText
var GetScanMessageType agentevents.FieldResolver[*gen.CursorPayload] = getScanMessageType
var GetToolName agentevents.FieldResolver[*gen.CursorPayload] = getToolName
var GetToolDisplayName agentevents.FieldResolver[*gen.CursorPayload] = getToolName
var GetToolSource agentevents.FieldResolver[*gen.CursorPayload] = getToolSource
var GetToolInput agentevents.FieldResolver[*gen.CursorPayload] = agentevents.AnyField[*gen.CursorPayload]("ToolInput")
var GetToolOutput agentevents.FieldResolver[*gen.CursorPayload] = getToolOutput
var GetToolCallID agentevents.FieldResolver[*gen.CursorPayload] = getToolCallID
var GetUsageInputTokens agentevents.FieldResolver[*gen.CursorPayload] = agentevents.IntField[*gen.CursorPayload]("InputTokens")
var GetUsageOutputTokens agentevents.FieldResolver[*gen.CursorPayload] = agentevents.IntField[*gen.CursorPayload]("OutputTokens")
var GetUsageCacheReadTokens agentevents.FieldResolver[*gen.CursorPayload] = agentevents.IntField[*gen.CursorPayload]("CacheReadTokens")
var GetUsageCacheWriteTokens agentevents.FieldResolver[*gen.CursorPayload] = agentevents.IntField[*gen.CursorPayload]("CacheWriteTokens")

func getEventType(ev agentevents.Event[*gen.CursorPayload]) (any, bool, error) {
	if ev.Raw == nil {
		return nil, false, nil
	}
	eventType, ok := ParseEventType(ev.Raw.HookEventName)
	if !ok {
		return nil, false, nil
	}
	return eventType, true, nil
}

func getHookSource(_ agentevents.Event[*gen.CursorPayload]) (any, bool, error) {
	return string(Agent), true, nil
}

func getBlockReason(ev agentevents.Event[*gen.CursorPayload]) (any, bool, error) {
	return ev.BlockReason, ev.BlockReason != "", nil
}

func getError(ev agentevents.Event[*gen.CursorPayload]) (any, bool, error) {
	payload := ev.Raw
	if payload == nil {
		return nil, false, nil
	}
	if payload.Error != nil {
		return payload.Error, true, nil
	}
	if payload.ResultJSON != nil && *payload.ResultJSON != "" && cursorResultJSONIsError(*payload.ResultJSON) {
		return *payload.ResultJSON, true, nil
	}
	return nil, false, nil
}

func getScannableText(ev agentevents.Event[*gen.CursorPayload]) (any, bool, error) {
	payload := ev.Raw
	if payload == nil {
		return nil, false, nil
	}
	eventType, ok := ParseEventType(payload.HookEventName)
	if !ok {
		return nil, false, nil
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
	return nil, false, nil
}

func getScanMessageType(ev agentevents.Event[*gen.CursorPayload]) (any, bool, error) {
	if ev.Raw == nil {
		return nil, false, nil
	}
	eventType, ok := ParseEventType(ev.Raw.HookEventName)
	if !ok {
		return nil, false, nil
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

func getToolName(ev agentevents.Event[*gen.CursorPayload]) (any, bool, error) {
	if ev.Raw == nil || ev.Raw.ToolName == nil || *ev.Raw.ToolName == "" {
		return nil, false, nil
	}
	name := normalizeToolName(*ev.Raw.ToolName)
	return name, name != "", nil
}

func getToolSource(ev agentevents.Event[*gen.CursorPayload]) (any, bool, error) {
	payload := ev.Raw
	if payload == nil {
		return nil, false, nil
	}
	eventType, _ := ParseEventType(payload.HookEventName)
	if eventType == types.MCPToolCallStarted || eventType == types.MCPToolCallCompleted {
		source := cursorMCPToolSource(payload)
		return source, source != "", nil
	}
	if payload.ToolName != nil && strings.HasPrefix(*payload.ToolName, "mcp__") {
		parts := strings.SplitN(*payload.ToolName, "__", 3)
		if len(parts) == 3 && parts[1] != "" {
			return parts[1], true, nil
		}
	}
	return nil, false, nil
}

func getToolOutput(ev agentevents.Event[*gen.CursorPayload]) (any, bool, error) {
	payload := ev.Raw
	if payload == nil {
		return nil, false, nil
	}
	if payload.ResultJSON != nil && *payload.ResultJSON != "" {
		return *payload.ResultJSON, true, nil
	}
	if payload.ToolResponse != nil {
		return payload.ToolResponse, true, nil
	}
	return nil, false, nil
}

func getToolCallID(ev agentevents.Event[*gen.CursorPayload]) (any, bool, error) {
	id := cursorToolCorrelationID(ev.Raw)
	return id, id != "", nil
}

func ParseEventType(raw string) (types.EventType, bool) {
	switch EventType(raw) {
	case EventBeforeSubmitPrompt:
		return types.UserPromptSubmit, true
	case EventAfterAgentResponse:
		return types.AssistantResponseComplete, true
	case EventPreToolUse:
		return types.ToolCallStarted, true
	case EventBeforeMCPExecution:
		return types.MCPToolCallStarted, true
	case EventPostToolUse:
		return types.ToolCallCompleted, true
	case EventAfterMCPExecution:
		return types.MCPToolCallCompleted, true
	case EventPostToolUseFailure:
		return types.ToolCallFailed, true
	case EventStop:
		return types.SessionEnded, true
	default:
		return "", false
	}
}

func normalizeToolName(name string) string {
	if stripped, ok := strings.CutPrefix(name, "MCP:"); ok {
		return stripped
	}
	if strings.HasPrefix(name, "mcp__") {
		parts := strings.SplitN(name, "__", 3)
		if len(parts) == 3 {
			return parts[2]
		}
	}
	return name
}

func cursorMCPToolSource(payload *gen.CursorPayload) string {
	if payload.URL != nil && *payload.URL != "" {
		if u, err := url.Parse(*payload.URL); err == nil && u.Host != "" {
			return u.Host
		}
		return *payload.URL
	}
	if payload.Command != nil && *payload.Command != "" {
		return *payload.Command
	}
	return ""
}

func cursorToolCorrelationID(payload *gen.CursorPayload) string {
	if payload == nil {
		return ""
	}
	if payload.ToolUseID != nil && *payload.ToolUseID != "" {
		return *payload.ToolUseID
	}

	convID := conv.PtrValOr(payload.ConversationID, "")
	genID := conv.PtrValOr(payload.GenerationID, "")
	toolName := normalizeToolName(conv.PtrValOr(payload.ToolName, ""))
	if convID == "" && genID == "" && toolName == "" && payload.ToolInput == nil {
		return ""
	}

	var b strings.Builder
	b.WriteString(convID)
	b.WriteByte('|')
	b.WriteString(genID)
	b.WriteByte('|')
	b.WriteString(toolName)
	b.WriteByte('|')
	if payload.ToolInput != nil {
		if jsonBytes, err := json.Marshal(payload.ToolInput); err == nil {
			b.Write(jsonBytes)
		}
	}

	sum := sha256.Sum256([]byte(b.String()))
	return "cursor_synth_" + hex.EncodeToString(sum[:8])
}

func cursorResultJSONIsError(raw string) bool {
	var parsed struct {
		IsError bool `json:"isError"`
	}
	return json.Unmarshal([]byte(raw), &parsed) == nil && parsed.IsError
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
		return ""
	}
	return string(b)
}
