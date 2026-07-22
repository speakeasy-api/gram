package message

type Type = string

const (
	User         Type = "user_message"
	ToolRequest  Type = "tool_request"
	ToolResponse Type = "tool_response"
	Assistant    Type = "assistant_message"
	// PromptAttachment is client-side context attached to a user prompt and
	// recorded with tool-like provenance after the turn.
	PromptAttachment Type = "prompt_attachment"
)

var allTypes = []Type{
	User,
	ToolRequest,
	ToolResponse,
	Assistant,
	PromptAttachment,
}

var validTypes = map[Type]struct{}{
	User:             {},
	ToolRequest:      {},
	ToolResponse:     {},
	Assistant:        {},
	PromptAttachment: {},
}

func AllTypes() []Type {
	return append([]Type(nil), allTypes...)
}

func IsTypeValid(messageType Type) bool {
	_, ok := validTypes[messageType]
	return ok
}
