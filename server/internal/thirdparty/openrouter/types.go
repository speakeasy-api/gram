package openrouter

import (
	"encoding/json"
	"fmt"

	or "github.com/speakeasy-api/gram/openrouter/models/components"
	"github.com/speakeasy-api/gram/server/internal/conv"
)

func GetRole(msg or.Message) string {
	switch msg.Type {
	case or.MessageTypeAssistant:
		return msg.AssistantMessage.GetRole()
	case or.MessageTypeDeveloper:
		return msg.MessageDeveloper.GetRole()
	case or.MessageTypeSystem:
		return msg.SystemMessage.GetRole()
	case or.MessageTypeTool:
		return msg.ToolResponseMessage.GetRole()
	case or.MessageTypeUser:
		return msg.UserMessage.GetRole()
	default:
		return ""
	}
}

func GetContentJSON(msg or.Message) ([]byte, error) {
	switch msg.Type {
	case or.MessageTypeAssistant:
		bs, err := json.Marshal(msg.AssistantMessage.Content)
		if err != nil {
			return nil, fmt.Errorf("marshal assistant message: %w", err)
		}
		return bs, nil
	case or.MessageTypeDeveloper:
		bs, err := json.Marshal(msg.MessageDeveloper.Content)
		if err != nil {
			return nil, fmt.Errorf("marshal developer message: %w", err)
		}
		return bs, nil
	case or.MessageTypeSystem:
		bs, err := json.Marshal(msg.SystemMessage.Content)
		if err != nil {
			return nil, fmt.Errorf("marshal system message: %w", err)
		}
		return bs, nil
	case or.MessageTypeTool:
		bs, err := json.Marshal(msg.ToolResponseMessage.Content)
		if err != nil {
			return nil, fmt.Errorf("marshal tool response message: %w", err)
		}
		return bs, nil
	case or.MessageTypeUser:
		bs, err := json.Marshal(msg.UserMessage.Content)
		if err != nil {
			return nil, fmt.Errorf("marshal user message: %w", err)
		}
		return bs, nil
	default:
		return nil, fmt.Errorf("unknown message type: %s", msg.Type)
	}
}

func GetText(msg or.Message) string {
	switch msg.Type {
	case or.MessageTypeAssistant:
		content, ok := msg.AssistantMessage.Content.GetOrZero()
		if !ok {
			return ""
		}

		switch content.Type {
		case or.AssistantMessageContentTypeArrayOfChatMessageContentItem:
			arr := content.ArrayOfChatMessageContentItem
			txt := ""
			for _, item := range arr {
				if item.Type == or.ChatMessageContentItemTypeText {
					txt = item.ChatMessageContentItemText.Text
					break
				}
			}
			return txt
		case or.AssistantMessageContentTypeStr:
			return conv.PtrValOr(content.Str, "")
		default:
			return ""
		}
	case or.MessageTypeDeveloper:
		switch msg.MessageDeveloper.Content.Type {
		case or.MessageContentTypeArrayOfChatMessageContentItemText:
			arr := msg.MessageDeveloper.Content.ArrayOfChatMessageContentItemText
			if len(arr) == 0 {
				return ""
			}
			return arr[0].Text
		case or.MessageContentTypeStr:
			return conv.PtrValOr(msg.MessageDeveloper.Content.Str, "")
		default:
			return ""
		}
	case or.MessageTypeSystem:
		switch msg.SystemMessage.Content.Type {
		case or.SystemMessageContentTypeArrayOfChatMessageContentItemText:
			arr := msg.SystemMessage.Content.ArrayOfChatMessageContentItemText
			if len(arr) == 0 {
				return ""
			}
			return arr[0].Text
		case or.SystemMessageContentTypeStr:
			return conv.PtrValOr(msg.SystemMessage.Content.Str, "")
		default:
			return ""
		}
	case or.MessageTypeTool:
		switch msg.ToolResponseMessage.Content.Type {
		case or.ToolResponseMessageContentTypeArrayOfChatMessageContentItem:
			arr := msg.ToolResponseMessage.Content.ArrayOfChatMessageContentItem
			txt := ""
			for _, item := range arr {
				if item.Type == or.ChatMessageContentItemTypeText {
					txt = item.ChatMessageContentItemText.Text
					break
				}
			}
			return txt
		case or.ToolResponseMessageContentTypeStr:
			return conv.PtrValOr(msg.ToolResponseMessage.Content.Str, "")
		default:
			return ""
		}
	case or.MessageTypeUser:
		switch msg.UserMessage.Content.Type {
		case or.UserMessageContentTypeArrayOfChatMessageContentItem:
			arr := msg.UserMessage.Content.ArrayOfChatMessageContentItem
			txt := ""
			for _, item := range arr {
				if item.Type == or.ChatMessageContentItemTypeText {
					txt = item.ChatMessageContentItemText.Text
					break
				}
			}
			return txt
		case or.UserMessageContentTypeStr:
			return conv.PtrValOr(msg.UserMessage.Content.Str, "")
		default:
			return ""
		}

	default:
		return ""
	}
}

func GetToolCallID(msg or.Message) *string {
	switch msg.Type {
	case or.MessageTypeTool:
		return &msg.ToolResponseMessage.ToolCallID
	default:
		return nil
	}
}

// OpenAIChatRequest represents the request structure for OpenAI chat completions
type OpenAIChatRequest struct {
	Model       string       `json:"model"`
	Messages    []or.Message `json:"messages"`
	Stream      bool         `json:"stream"`
	Tools       []Tool       `json:"tools,omitempty"`
	Temperature float32      `json:"temperature,omitempty"`
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
	Function ToolCallFunction `json:"function,omitempty"`
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

// OpenAIChatResponse represents the response structure from OpenAI for non-streaming responses
type OpenAIChatResponse struct {
	ID      string `json:"id"`
	Object  string `json:"object"`
	Created int64  `json:"created"`
	Model   string `json:"model"`
	Choices []struct {
		Message      or.Message `json:"message"`
		FinishReason string     `json:"finish_reason"`
	} `json:"choices"`
	Usage *Usage `json:"usage,omitempty"`
}
