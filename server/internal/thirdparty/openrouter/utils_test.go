package openrouter

import (
	"encoding/json"
	"slices"
	"testing"

	or "github.com/OpenRouterTeam/go-sdk/models/components"
	"github.com/OpenRouterTeam/go-sdk/optionalnullable"
	"github.com/stretchr/testify/require"
)

func combinedAssistant(text string, toolID string) or.ChatMessages {
	c := or.CreateChatAssistantMessageContentStr(text)
	return or.CreateChatMessagesAssistant(or.ChatAssistantMessage{
		Role:    or.ChatAssistantMessageRoleAssistant,
		Content: optionalnullable.From(&c),
		Name:    nil,
		ToolCalls: []or.ChatToolCall{{
			ID:       toolID,
			Type:     or.ChatToolCallTypeFunction,
			Function: or.ChatToolCallFunction{Name: "do_thing", Arguments: "{}"},
		}},
		Refusal:          nil,
		Reasoning:        nil,
		ReasoningDetails: nil,
		Images:           nil,
		Audio:            nil,
	})
}

func toolOnlyAssistant(toolID string) or.ChatMessages {
	return or.CreateChatMessagesAssistant(or.ChatAssistantMessage{
		Role:    or.ChatAssistantMessageRoleAssistant,
		Content: optionalnullable.From[or.ChatAssistantMessageContent](nil),
		Name:    nil,
		ToolCalls: []or.ChatToolCall{{
			ID:       toolID,
			Type:     or.ChatToolCallTypeFunction,
			Function: or.ChatToolCallFunction{Name: "do_thing", Arguments: "{}"},
		}},
		Refusal:          nil,
		Reasoning:        nil,
		ReasoningDetails: nil,
		Images:           nil,
		Audio:            nil,
	})
}

func TestNormalizeAssistantMessages_SplitsCombinedIntoTextThenToolOnly(t *testing.T) {
	t.Parallel()

	msgs := []or.ChatMessages{
		CreateMessageUser("hi"),
		combinedAssistant("I'll check the weather.", "toolu_bdrk_abc"),
	}

	out := slices.Collect(NormalizeAssistantMessages(msgs))
	require.Len(t, out, 3)

	require.Equal(t, or.ChatMessagesTypeUser, out[0].Type)

	require.Equal(t, or.ChatMessagesTypeAssistant, out[1].Type)
	require.Equal(t, "I'll check the weather.", GetText(out[1]))
	require.Empty(t, out[1].ChatAssistantMessage.ToolCalls)

	require.Equal(t, or.ChatMessagesTypeAssistant, out[2].Type)
	body, err := json.Marshal(out[2])
	require.NoError(t, err)
	var decoded struct {
		Role      string          `json:"role"`
		Content   json.RawMessage `json:"content"`
		ToolCalls []struct {
			ID string `json:"id"`
		} `json:"tool_calls"`
	}
	require.NoError(t, json.Unmarshal(body, &decoded))
	require.Equal(t, "assistant", decoded.Role)
	require.Equal(t, "null", string(decoded.Content))
	require.Len(t, decoded.ToolCalls, 1)
	require.Equal(t, "toolu_bdrk_abc", decoded.ToolCalls[0].ID)
}

func TestNormalizeAssistantMessages_NullsContentOnToolOnlyWhenTextBlank(t *testing.T) {
	t.Parallel()

	blank := or.CreateChatAssistantMessageContentStr("   ")
	msgs := []or.ChatMessages{
		or.CreateChatMessagesAssistant(or.ChatAssistantMessage{
			Role:    or.ChatAssistantMessageRoleAssistant,
			Content: optionalnullable.From(&blank),
			Name:    nil,
			ToolCalls: []or.ChatToolCall{{
				ID:       "t1",
				Type:     or.ChatToolCallTypeFunction,
				Function: or.ChatToolCallFunction{Name: "do_thing", Arguments: "{}"},
			}},
			Refusal:          nil,
			Reasoning:        nil,
			ReasoningDetails: nil,
			Images:           nil,
			Audio:            nil,
		}),
	}

	out := slices.Collect(NormalizeAssistantMessages(msgs))
	require.Len(t, out, 1)

	body, err := json.Marshal(out[0])
	require.NoError(t, err)
	var decoded struct {
		Content json.RawMessage `json:"content"`
	}
	require.NoError(t, json.Unmarshal(body, &decoded))
	require.Equal(t, "null", string(decoded.Content))
}

func TestNormalizeAssistantMessages_PassesThroughToolCallFreeAssistant(t *testing.T) {
	t.Parallel()

	msgs := []or.ChatMessages{CreateMessageAssistant("plain reply")}

	out := slices.Collect(NormalizeAssistantMessages(msgs))
	require.Len(t, out, 1)
	require.Equal(t, "plain reply", GetText(out[0]))
}

func TestNormalizeAssistantMessages_PassesThroughNonAssistant(t *testing.T) {
	t.Parallel()

	msgs := []or.ChatMessages{
		CreateMessageUser("user text"),
		CreateMessageSystem("sys text"),
	}

	out := slices.Collect(NormalizeAssistantMessages(msgs))
	require.Len(t, out, 2)
	require.Equal(t, "user text", GetText(out[0]))
	require.Equal(t, "sys text", GetText(out[1]))
}

func TestNormalizeAssistantMessages_Idempotent(t *testing.T) {
	t.Parallel()

	msgs := []or.ChatMessages{
		CreateMessageUser("hi"),
		combinedAssistant("text", "t1"),
	}

	first := slices.Collect(NormalizeAssistantMessages(msgs))
	second := slices.Collect(NormalizeAssistantMessages(first))

	require.Len(t, second, len(first))
	for i := range first {
		a, err := json.Marshal(first[i])
		require.NoError(t, err)
		b, err := json.Marshal(second[i])
		require.NoError(t, err)
		require.JSONEq(t, string(a), string(b))
	}
}

func TestNormalizeAssistantMessages_PreservesOrderAndPassThroughShapes(t *testing.T) {
	t.Parallel()

	msgs := []or.ChatMessages{
		CreateMessageUser("u1"),
		combinedAssistant("a1", "t1"),
		toolOnlyAssistant("t2"),
		CreateMessageAssistant("a2"),
	}

	out := slices.Collect(NormalizeAssistantMessages(msgs))
	require.Len(t, out, 5)
	require.Equal(t, or.ChatMessagesTypeUser, out[0].Type)
	require.Equal(t, "a1", GetText(out[1]))
	require.Empty(t, out[1].ChatAssistantMessage.ToolCalls)
	require.Empty(t, GetText(out[2]))
	require.Len(t, out[2].ChatAssistantMessage.ToolCalls, 1)
	require.Empty(t, GetText(out[3]))
	require.Len(t, out[3].ChatAssistantMessage.ToolCalls, 1)
	require.Equal(t, "a2", GetText(out[4]))
}
