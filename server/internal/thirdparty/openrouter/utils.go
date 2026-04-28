package openrouter

import (
	"iter"
	"strings"

	or "github.com/OpenRouterTeam/go-sdk/models/components"
	"github.com/OpenRouterTeam/go-sdk/optionalnullable"
)

// NormalizeAssistantMessages yields assistant messages in a canonical shape
// where content and tool_calls never coexist on the same message. An assistant
// message with both text content and tool_calls is emitted as two sequential
// messages: text-only, then tool-calls-only. All other messages pass through
// unchanged. Idempotent.
//
// Rationale: some OpenAI->Anthropic converters (Azure path) reject a single
// assistant message that carries both content and tool_calls. Splitting into
// two sequential assistant messages maps 1:1 to Anthropic's native multi-block
// content shape (text block followed by tool_use block) without losing the
// narrative text.
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

			if !assistantHasContent(asst) {
				normalized := *asst
				normalized.Content = optionalnullable.From[or.ChatAssistantMessageContent](nil)
				if !yield(or.CreateChatMessagesAssistant(normalized)) {
					return
				}
				continue
			}

			textOnly := *asst
			textOnly.ToolCalls = nil

			toolOnly := or.ChatAssistantMessage{
				Role:             asst.Role,
				Content:          optionalnullable.From[or.ChatAssistantMessageContent](nil),
				Name:             nil,
				ToolCalls:        asst.ToolCalls,
				Refusal:          nil,
				Reasoning:        nil,
				ReasoningDetails: nil,
				Images:           nil,
				Audio:            nil,
			}

			if !yield(or.CreateChatMessagesAssistant(textOnly)) {
				return
			}
			if !yield(or.CreateChatMessagesAssistant(toolOnly)) {
				return
			}
		}
	}
}

func assistantHasContent(asst *or.ChatAssistantMessage) bool {
	content, ok := asst.Content.GetOrZero()
	if !ok {
		return false
	}
	switch content.Type {
	case or.ChatAssistantMessageContentTypeStr:
		if content.Str == nil {
			return false
		}
		return strings.TrimSpace(*content.Str) != ""
	case or.ChatAssistantMessageContentTypeArrayOfChatContentItems:
		return len(content.ArrayOfChatContentItems) > 0
	default:
		return false
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
