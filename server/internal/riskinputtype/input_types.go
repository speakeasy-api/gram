package riskinputtype

import "slices"

const (
	InputTypeUserMessage      = "user_message"
	InputTypeToolRequest      = "tool_request"
	InputTypeToolResponse     = "tool_response"
	InputTypeAssistantMessage = "assistant_message"
)

var all = []string{
	InputTypeUserMessage,
	InputTypeToolRequest,
	InputTypeToolResponse,
	InputTypeAssistantMessage,
}

func All() []string {
	return append([]string(nil), all...)
}

func IsValid(inputType string) bool {
	return slices.Contains(all, inputType)
}

func Allows(inputTypes []string, inputType string) bool {
	if len(inputTypes) == 0 {
		return true
	}
	return slices.Contains(inputTypes, inputType)
}
