package message

type Type = string

const (
	User         Type = "user_message"
	ToolRequest  Type = "tool_request"
	ToolResponse Type = "tool_response"
	Assistant    Type = "assistant_message"
)

var allTypes = []Type{
	User,
	ToolRequest,
	ToolResponse,
	Assistant,
}

var validTypes = map[Type]struct{}{
	User:         {},
	ToolRequest:  {},
	ToolResponse: {},
	Assistant:    {},
}

func AllTypes() []Type {
	return append([]Type(nil), allTypes...)
}

func IsTypeValid(messageType Type) bool {
	_, ok := validTypes[messageType]
	return ok
}
