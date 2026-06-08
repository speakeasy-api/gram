package eventsink

import (
	"encoding/json"
	"fmt"

	"github.com/google/uuid"

	"github.com/speakeasy-api/gram/server/internal/agentevents"
	"github.com/speakeasy-api/gram/server/internal/agentevents/types"
	chatRepo "github.com/speakeasy-api/gram/server/internal/chat/repo"
	"github.com/speakeasy-api/gram/server/internal/conv"
)

// ChatMessage is a chat message to persist plus the write-side instruction of
// whether a chat title should be scheduled for it.
type ChatMessage struct {
	Params        chatRepo.CreateChatMessageParams
	ScheduleTitle bool
}

// BuildChatMessages converts an agent event into chat messages to persist.
// chatID and source (the provider display name) are resolved by the caller
// because they depend on service-side identity derivation.
func BuildChatMessages[T any](e agentevents.Event[T], chatID uuid.UUID, source string) ([]ChatMessage, error) {
	eventType, ok, err := e.EventType()
	if err != nil || !ok {
		return nil, err
	}
	switch eventType {
	case types.UserPromptSubmit:
		return buildPromptChatMessage(e, chatID, source)
	case types.AssistantResponseComplete:
		return buildAssistantChatMessage(e, chatID, source)
	case types.ToolCallStarted, types.MCPToolCallStarted:
		return buildToolCallChatMessage(e, chatID, source)
	case types.ToolCallCompleted, types.ToolCallFailed, types.MCPToolCallCompleted:
		return buildToolResultChatMessage(e, chatID, source, eventType)
	default:
		return nil, nil
	}
}

func baseChatParams[T any](e agentevents.Event[T], chatID uuid.UUID, source string) (chatRepo.CreateChatMessageParams, error) {
	projectID, err := uuid.Parse(e.Context.ProjectID)
	if err != nil {
		return chatRepo.CreateChatMessageParams{}, fmt.Errorf("invalid project ID for agent conversation: %w", err)
	}
	return chatRepo.CreateChatMessageParams{
		ChatID:           chatID,
		ProjectID:        projectID,
		Role:             "",
		Content:          "",
		Model:            conv.ToPGTextEmpty(""),
		UserID:           conv.ToPGTextEmpty(e.Context.UserID),
		Source:           conv.ToPGText(source),
		PromptTokens:     0,
		CompletionTokens: 0,
		TotalTokens:      0,
		ContentRaw:       nil,
		ContentAssetUrl:  conv.ToPGTextEmpty(""),
		StorageError:     conv.ToPGTextEmpty(""),
		MessageID:        conv.ToPGTextEmpty(""),
		ToolCallID:       conv.ToPGTextEmpty(""),
		ExternalUserID:   conv.ToPGTextEmpty(e.Context.UserEmail),
		FinishReason:     conv.ToPGTextEmpty(""),
		ToolCalls:        nil,
		Origin:           conv.ToPGTextEmpty(""),
		UserAgent:        conv.ToPGTextEmpty(""),
		IpAddress:        conv.ToPGTextEmpty(""),
		ContentHash:      nil,
		Generation:       0,
	}, nil
}

func buildPromptChatMessage[T any](e agentevents.Event[T], chatID uuid.UUID, source string) ([]ChatMessage, error) {
	content, ok, err := e.String(types.FieldPrompt)
	if err != nil || !ok || content == "" {
		return nil, err
	}
	params, err := baseChatParams(e, chatID, source)
	if err != nil {
		return nil, err
	}
	params.Role = "user"
	params.Content = content
	return []ChatMessage{{Params: params}}, nil
}

func buildAssistantChatMessage[T any](e agentevents.Event[T], chatID uuid.UUID, source string) ([]ChatMessage, error) {
	content, ok, err := e.String(types.FieldAssistantText)
	if err != nil || !ok || content == "" {
		return nil, err
	}
	model := optionalString(e, types.FieldModel)
	params, err := baseChatParams(e, chatID, source)
	if err != nil {
		return nil, err
	}
	params.Role = "assistant"
	params.Content = content
	params.Model = conv.ToPGTextEmpty(model)
	return []ChatMessage{{Params: params, ScheduleTitle: true}}, nil
}

func buildToolCallChatMessage[T any](e agentevents.Event[T], chatID uuid.UUID, source string) ([]ChatMessage, error) {
	toolName, ok, err := e.String(types.FieldToolName)
	if err != nil || !ok || toolName == "" {
		return nil, err
	}
	model := optionalString(e, types.FieldModel)
	toolCallID := optionalString(e, types.FieldToolCallID)
	toolInput, _, err := e.Any(types.FieldToolInput)
	if err != nil {
		return nil, err
	}
	toolCallsJSON, err := json.Marshal(buildToolCalls(toolCallID, toolName, toolInput))
	if err != nil {
		return nil, fmt.Errorf("marshal tool calls: %w", err)
	}

	params, err := baseChatParams(e, chatID, source)
	if err != nil {
		return nil, err
	}
	params.Role = "assistant"
	params.Content = ""
	params.FinishReason = conv.ToPGTextEmpty("tool_calls")
	params.Model = conv.ToPGTextEmpty(model)
	params.ToolCalls = toolCallsJSON
	return []ChatMessage{{Params: params}}, nil
}

func buildToolResultChatMessage[T any](e agentevents.Event[T], chatID uuid.UUID, source string, eventType types.EventType) ([]ChatMessage, error) {
	content, ok, err := toolResultContent(e, eventType)
	if err != nil || !ok || content == "" {
		return nil, err
	}
	toolCallID := optionalString(e, types.FieldToolCallID)
	params, err := baseChatParams(e, chatID, source)
	if err != nil {
		return nil, err
	}
	params.Role = "tool"
	params.Content = content
	params.ToolCallID = conv.ToPGTextEmpty(toolCallID)
	return []ChatMessage{{Params: params}}, nil
}

func toolResultContent[T any](e agentevents.Event[T], eventType types.EventType) (string, bool, error) {
	if eventType == types.ToolCallFailed {
		if value, ok, err := e.Any(types.FieldError); err != nil || ok {
			return marshalToJSON(value), ok, err
		}
	}
	value, ok, err := e.Any(types.FieldToolOutput)
	if err != nil || !ok {
		return "", ok, err
	}
	return marshalToJSON(value), true, nil
}
