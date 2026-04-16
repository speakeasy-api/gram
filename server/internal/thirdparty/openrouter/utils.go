package openrouter

import (
	or "github.com/OpenRouterTeam/go-sdk/models/components"
	"github.com/OpenRouterTeam/go-sdk/optionalnullable"
)

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
