package openrouter

import (
	"iter"

	or "github.com/OpenRouterTeam/go-sdk/models/components"
	"github.com/OpenRouterTeam/go-sdk/optionalnullable"
)

// NormalizeAssistantMessages yields assistant messages in a canonical shape for
// OpenRouter where content and tool_calls never coexist on the same message.
// An assistant message with both text content and tool_calls is emitted as a
// single tool-calls message with empty content. All other messages pass through
// unchanged. Idempotent.
//
// Rationale: some OpenAI->Anthropic converters (Azure path) reject a single
// assistant message that carries both content and tool_calls. Other converters
// (Vertex path) reject or mishandle a split text-only assistant message followed
// by a tool-calls-only assistant message. Dropping pre-tool narrative text at
// the OpenRouter boundary preserves the required tool_call/tool_result
// adjacency across dynamically routed providers.
func NormalizeAssistantMessages(msgs []or.ChatMessages) iter.Seq[or.ChatMessages] {
	return func(yield func(or.ChatMessages) bool) {
		for _, msg := range msgs {
			if msg.Type != or.ChatMessagesTypeAssistant || msg.ChatAssistantMessage == nil {
				if !yield(msg) {
					return
				}
				continue
			}

			asst := msg.ChatAssistantMessage
			if len(asst.ToolCalls) == 0 {
				if !yield(msg) {
					return
				}
				continue
			}

			empty := or.CreateChatAssistantMessageContentStr("")

			normalized := *asst
			normalized.Content = optionalnullable.From(&empty)
			if !yield(or.CreateChatMessagesAssistant(normalized)) {
				return
			}
		}
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
