package openrouter

import or "github.com/OpenRouterTeam/go-sdk/models/components"

func CreateMessageUser(content string) or.Message {
	return or.Message{
		Type: or.MessageTypeUser,
		UserMessage: &or.UserMessage{
			Content: or.CreateUserMessageContentStr(content),
		},
	}
}

func CreateMessageSystem(content string) or.Message {
	return or.Message{
		Type: or.MessageTypeSystem,
		SystemMessage: &or.SystemMessage{
			Content: or.CreateSystemMessageContentStr(content),
		},
	}
}
