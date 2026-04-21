package openrouter

import (
	or "github.com/OpenRouterTeam/go-sdk/models/components"
	"github.com/OpenRouterTeam/go-sdk/optionalnullable"
)

// SanitizeAssistantContent nulls out the content string on any assistant
// message that also carries tool_calls. OpenRouter's OpenAI→Anthropic
// converter on the Azure path drops tool_calls when content is a non-null
// string, producing a tool_result with no matching tool_use on the next turn.
// The known-good shape across providers is content:null + tool_calls populated.
func SanitizeAssistantContent(msgs []or.ChatMessages) {
	for i := range msgs {
		if msgs[i].Type != or.ChatMessagesTypeAssistant {
			continue
		}
		asst := msgs[i].ChatAssistantMessage
		if asst == nil || len(asst.ToolCalls) == 0 {
			continue
		}
		asst.Content = optionalnullable.From[or.ChatAssistantMessageContent](nil)
	}
}

func CreateMessageUser(content string) or.ChatMessages {
	return or.CreateChatMessagesUser(or.ChatUserMessage{
		Role:    or.ChatUserMessageRoleUser,
		Content: or.CreateChatUserMessageContentStr(content),
		Name:    nil,
	})
}

func CreateMessageAssistant(content string) or.ChatMessages {
	c := or.CreateChatAssistantMessageContentStr(content)
	return or.CreateChatMessagesAssistant(or.ChatAssistantMessage{
		Role:             or.ChatAssistantMessageRoleAssistant,
		Content:          optionalnullable.From(&c),
		Name:             nil,
		ToolCalls:        nil,
		Refusal:          nil,
		Reasoning:        nil,
		ReasoningDetails: nil,
		Images:           nil,
		Audio:            nil,
	})
}

func CreateMessageSystem(content string) or.ChatMessages {
	return or.CreateChatMessagesSystem(or.ChatSystemMessage{
		Role:    or.ChatSystemMessageRoleSystem,
		Content: or.CreateChatSystemMessageContentStr(content),
		Name:    nil,
	})
}
