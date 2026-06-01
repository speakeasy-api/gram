package riskscope

import "slices"

const (
	InputScopeUserMessage      = "user_message"
	InputScopeToolRequest      = "tool_request"
	InputScopeToolResponse     = "tool_response"
	InputScopeAssistantMessage = "assistant_message"
)

var all = []string{
	InputScopeUserMessage,
	InputScopeToolRequest,
	InputScopeToolResponse,
	InputScopeAssistantMessage,
}

func All() []string {
	return append([]string(nil), all...)
}

func IsValid(scope string) bool {
	return slices.Contains(all, scope)
}

func Allows(scopes []string, inputScope string) bool {
	if len(scopes) == 0 {
		return true
	}
	return slices.Contains(scopes, inputScope)
}
