package messagetype

import "slices"

type MessageType = string

const (
	UserMessage      MessageType = "user_message"
	ToolRequest      MessageType = "tool_request"
	ToolResponse     MessageType = "tool_response"
	AssistantMessage MessageType = "assistant_message"
)

var all = []MessageType{
	UserMessage,
	ToolRequest,
	ToolResponse,
	AssistantMessage,
}

func All() []MessageType {
	return append([]MessageType(nil), all...)
}

func IsValid(messageType MessageType) bool {
	return slices.Contains(all, messageType)
}

func Allows(messageTypes []string, messageType MessageType) bool {
	if len(messageTypes) == 0 {
		return true
	}
	return slices.Contains(messageTypes, messageType)
}
