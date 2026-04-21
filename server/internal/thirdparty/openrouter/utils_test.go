package openrouter

import (
	"encoding/json"
	"testing"

	or "github.com/OpenRouterTeam/go-sdk/models/components"
	"github.com/OpenRouterTeam/go-sdk/optionalnullable"
	"github.com/stretchr/testify/require"
)

func TestSanitizeAssistantContent_NullsContentWhenToolCallsPresent(t *testing.T) {
	t.Parallel()

	c := or.CreateChatAssistantMessageContentStr("narrative text")
	msgs := []or.ChatMessages{
		CreateMessageUser("hi"),
		or.CreateChatMessagesAssistant(or.ChatAssistantMessage{
			Role:    or.ChatAssistantMessageRoleAssistant,
			Content: optionalnullable.From(&c),
			ToolCalls: []or.ChatToolCall{{
				ID:       "toolu_bdrk_abc",
				Type:     or.ChatToolCallTypeFunction,
				Function: or.ChatToolCallFunction{Name: "do_thing", Arguments: "{}"},
			}},
		}),
	}

	SanitizeAssistantContent(msgs)

	body, err := json.Marshal(msgs[1])
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

func TestSanitizeAssistantContent_LeavesToolCallFreeAssistantAlone(t *testing.T) {
	t.Parallel()

	msgs := []or.ChatMessages{CreateMessageAssistant("plain reply")}
	SanitizeAssistantContent(msgs)
	require.Equal(t, "plain reply", GetText(msgs[0]))
}

func TestSanitizeAssistantContent_LeavesNonAssistantAlone(t *testing.T) {
	t.Parallel()

	msgs := []or.ChatMessages{
		CreateMessageUser("user text"),
		CreateMessageSystem("sys text"),
	}
	SanitizeAssistantContent(msgs)
	require.Equal(t, "user text", GetText(msgs[0]))
	require.Equal(t, "sys text", GetText(msgs[1]))
}
