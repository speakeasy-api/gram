package openrouter

import (
	"encoding/json"
	"fmt"

	or "github.com/OpenRouterTeam/go-sdk/models/components"
	"github.com/speakeasy-api/gram/server/internal/conv"
)

func GetRole(msg or.ChatMessages) string {
	switch msg.Type {
	case or.ChatMessagesTypeAssistant:
		return string(msg.ChatAssistantMessage.GetRole())
	case or.ChatMessagesTypeDeveloper:
		return string(msg.ChatDeveloperMessage.GetRole())
	case or.ChatMessagesTypeSystem:
		return string(msg.ChatSystemMessage.GetRole())
	case or.ChatMessagesTypeTool:
		return string(msg.ChatToolMessage.GetRole())
	case or.ChatMessagesTypeUser:
		return string(msg.ChatUserMessage.GetRole())
	default:
		return ""
	}
}

func GetContentJSON(msg or.ChatMessages) ([]byte, error) {
	switch msg.Type {
	case or.ChatMessagesTypeAssistant:
		bs, err := json.Marshal(msg.ChatAssistantMessage.Content)
		if err != nil {
			return nil, fmt.Errorf("marshal assistant message: %w", err)
		}
		return bs, nil
	case or.ChatMessagesTypeDeveloper:
		bs, err := json.Marshal(msg.ChatDeveloperMessage.Content)
		if err != nil {
			return nil, fmt.Errorf("marshal developer message: %w", err)
		}
		return bs, nil
	case or.ChatMessagesTypeSystem:
		bs, err := json.Marshal(msg.ChatSystemMessage.Content)
		if err != nil {
			return nil, fmt.Errorf("marshal system message: %w", err)
		}
		return bs, nil
	case or.ChatMessagesTypeTool:
		bs, err := json.Marshal(msg.ChatToolMessage.Content)
		if err != nil {
			return nil, fmt.Errorf("marshal tool response message: %w", err)
		}
		return bs, nil
	case or.ChatMessagesTypeUser:
		bs, err := json.Marshal(msg.ChatUserMessage.Content)
		if err != nil {
			return nil, fmt.Errorf("marshal user message: %w", err)
		}
		return bs, nil
	default:
		return nil, fmt.Errorf("unknown message type: %s", msg.Type)
	}
}

func GetText(msg or.ChatMessages) string {
	switch msg.Type {
	case or.ChatMessagesTypeAssistant:
		content, ok := msg.ChatAssistantMessage.Content.GetOrZero()
		if !ok {
			return ""
		}

		switch content.Type {
		case or.ChatAssistantMessageContentTypeArrayOfChatContentItems:
			arr := content.ArrayOfChatContentItems
			txt := ""
			for _, item := range arr {
				if item.Type == or.ChatContentItemsTypeText {
					txt = item.ChatContentText.Text
					break
				}
			}
			return txt
		case or.ChatAssistantMessageContentTypeStr:
			return conv.PtrValOr(content.Str, "")
		default:
			return ""
		}
	case or.ChatMessagesTypeDeveloper:
		switch msg.ChatDeveloperMessage.Content.Type {
		case or.ChatDeveloperMessageContentTypeArrayOfChatContentText:
			arr := msg.ChatDeveloperMessage.Content.ArrayOfChatContentText
			if len(arr) == 0 {
				return ""
			}
			return arr[0].Text
		case or.ChatDeveloperMessageContentTypeStr:
			return conv.PtrValOr(msg.ChatDeveloperMessage.Content.Str, "")
		default:
			return ""
		}
	case or.ChatMessagesTypeSystem:
		switch msg.ChatSystemMessage.Content.Type {
		case or.ChatSystemMessageContentTypeArrayOfChatContentText:
			arr := msg.ChatSystemMessage.Content.ArrayOfChatContentText
			if len(arr) == 0 {
				return ""
			}
			return arr[0].Text
		case or.ChatSystemMessageContentTypeStr:
			return conv.PtrValOr(msg.ChatSystemMessage.Content.Str, "")
		default:
			return ""
		}
	case or.ChatMessagesTypeTool:
		switch msg.ChatToolMessage.Content.Type {
		case or.ChatToolMessageContentTypeArrayOfChatContentItems:
			arr := msg.ChatToolMessage.Content.ArrayOfChatContentItems
			txt := ""
			for _, item := range arr {
				if item.Type == or.ChatContentItemsTypeText {
					txt = item.ChatContentText.Text
					break
				}
			}
			return txt
		case or.ChatToolMessageContentTypeStr:
			return conv.PtrValOr(msg.ChatToolMessage.Content.Str, "")
		default:
			return ""
		}
	case or.ChatMessagesTypeUser:
		switch msg.ChatUserMessage.Content.Type {
		case or.ChatUserMessageContentTypeArrayOfChatContentItems:
			arr := msg.ChatUserMessage.Content.ArrayOfChatContentItems
			txt := ""
			for _, item := range arr {
				if item.Type == or.ChatContentItemsTypeText {
					txt = item.ChatContentText.Text
					break
				}
			}
			return txt
		case or.ChatUserMessageContentTypeStr:
			return conv.PtrValOr(msg.ChatUserMessage.Content.Str, "")
		default:
			return ""
		}

	default:
		return ""
	}
}

func GetToolCallID(msg or.ChatMessages) *string {
	switch msg.Type {
	case or.ChatMessagesTypeTool:
		return &msg.ChatToolMessage.ToolCallID
	default:
		return nil
	}
}

// OpenAIChatRequest represents the request structure for OpenAI chat completions
type OpenAIChatRequest struct {
	Model          string             `json:"model"`
	Messages       []or.ChatMessages  `json:"messages"`
	Stream         bool               `json:"stream"`
	Tools          []Tool             `json:"tools,omitempty"`
	Temperature    float32            `json:"temperature,omitempty"`
	ResponseFormat *or.ResponseFormat `json:"response_format,omitempty"`
}

// ToolCallFunction represents the function part of a tool call
type ToolCallFunction struct {
	Name      string `json:"name,omitempty"`
	Arguments string `json:"arguments,omitempty"`
}

// ToolCall represents an individual tool call in a response
type ToolCall struct {
	Index    int              `json:"index,omitempty"`
	ID       string           `json:"id,omitempty"`
	Type     string           `json:"type,omitempty"` // always "function"
	Function ToolCallFunction `json:"function"`
}

// Tool defines a function tool available to the model
type Tool struct {
	Type     string              `json:"type"` // always "function"
	Function *FunctionDefinition `json:"function"`
}

// FunctionDefinition defines a callable function's name and input schema
type FunctionDefinition struct {
	Name        string          `json:"name"`
	Description string          `json:"description,omitempty"`
	Parameters  json.RawMessage `json:"parameters"`
}

// ChunkDelta represents the delta content in a streaming response chunk
type ChunkDelta struct {
	Content   string     `json:"content,omitempty"`
	ToolCalls []ToolCall `json:"tool_calls,omitempty"`
}

// ChunkChoice represents a choice in a streaming response chunk
type ChunkChoice struct {
	Index        int        `json:"index"`
	Delta        ChunkDelta `json:"delta"`
	FinishReason *string    `json:"finish_reason"`
}

// StreamingChunk represents a streaming response chunk from OpenAI
type StreamingChunk struct {
	ID                string        `json:"id"`
	Object            string        `json:"object"`
	Created           int64         `json:"created"`
	Model             string        `json:"model"`
	SystemFingerprint string        `json:"system_fingerprint"`
	Choices           []ChunkChoice `json:"choices"`
	Usage             *Usage        `json:"usage"`
}

// Tokens used in the completion
type Usage struct {
	PromptTokens     int `json:"prompt_tokens"`
	CompletionTokens int `json:"completion_tokens"`
	TotalTokens      int `json:"total_tokens"`
}

// GramMetadata is a Gram-specific extension attached to chat completion
// responses to surface upstream model attributes resolved by the proxy.
type GramMetadata struct {
	ContextWindow int `json:"context_window"`
}

// OpenAIChatResponse represents the response structure from OpenAI for non-streaming responses
type OpenAIChatResponse struct {
	ID      string `json:"id"`
	Object  string `json:"object"`
	Created int64  `json:"created"`
	Model   string `json:"model"`
	Choices []struct {
		Message      or.ChatMessages `json:"message"`
		FinishReason string          `json:"finish_reason"`
	} `json:"choices"`
	Usage        *Usage        `json:"usage,omitempty"`
	GramMetadata *GramMetadata `json:"gram_metadata,omitempty"`
}
