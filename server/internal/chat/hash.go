package chat

import (
	"encoding/json"
	"fmt"
	"sort"
	"strings"

	or "github.com/OpenRouterTeam/go-sdk/models/components"
	"github.com/speakeasy-api/gram/server/internal/thirdparty/openrouter"
)

// messageSlot is the durable identity of a chat-history slot. Two slots are
// interchangeable when their fields match — byte-level differences in
// content/tool_calls JSON are deliberately ignored. The matcher walks
// stored vs. incoming slots in order; the first mismatch bumps generation.
//
// Identity rules:
//   - tool result rows: role + toolCallID. Result body is opaque and
//     non-deterministic across providers/replay paths.
//   - assistant tool-call rows: role + sorted joined toolCallIDs (no text).
//     Function names/arguments are excluded — the call ID already uniquely
//     names the call.
//   - text rows (system / user / plain assistant): role + content.
type messageSlot struct {
	role        string
	content     string
	toolCallID  string
	toolCallIDs string
}

func slotFromIncoming(msg or.ChatMessages) messageSlot {
	var toolCallID string
	if tc := openrouter.GetToolCallID(msg); tc != nil {
		toolCallID = *tc
	}
	return buildSlot(openrouter.GetRole(msg), openrouter.GetText(msg), toolCallID, toolCallIDsFromIncoming(msg))
}

func slotFromStored(role, content, toolCallID string, toolCallsJSON []byte) messageSlot {
	return buildSlot(role, content, toolCallID, toolCallIDsFromStoredJSON(toolCallsJSON))
}

func buildSlot(role, content, toolCallID string, toolCallIDs []string) messageSlot {
	switch {
	case toolCallID != "":
		return messageSlot{role: role, content: "", toolCallID: toolCallID, toolCallIDs: ""}
	case len(toolCallIDs) > 0:
		sorted := append([]string(nil), toolCallIDs...)
		sort.Strings(sorted)
		return messageSlot{role: role, content: "", toolCallID: "", toolCallIDs: strings.Join(sorted, ",")}
	default:
		return messageSlot{role: role, content: content, toolCallID: "", toolCallIDs: ""}
	}
}

// isBlankAssistant reports whether the slot is a no-op assistant turn:
// role=assistant with no text, no tool_call_id, no tool_calls. The server
// can persist such a row (model returns an empty stop) while clients drop
// it from subsequent wire requests, so the matcher steps over it on either
// side rather than treating the asymmetry as divergence.
func (s messageSlot) isBlankAssistant() bool {
	return s == messageSlot{role: "assistant"}
}

func toolCallIDsFromIncoming(msg or.ChatMessages) []string {
	if msg.Type != or.ChatMessagesTypeAssistant {
		return nil
	}
	calls := msg.ChatAssistantMessage.GetToolCalls()
	if len(calls) == 0 {
		return nil
	}
	out := make([]string, 0, len(calls))
	for _, c := range calls {
		if c.ID == "" {
			continue
		}
		out = append(out, c.ID)
	}
	return out
}

func toolCallIDsFromStoredJSON(toolCallsJSON []byte) []string {
	if len(toolCallsJSON) == 0 {
		return nil
	}
	var calls []struct {
		ID string `json:"id"`
	}
	if err := json.Unmarshal(toolCallsJSON, &calls); err != nil {
		return nil
	}
	out := make([]string, 0, len(calls))
	for _, c := range calls {
		if c.ID == "" {
			continue
		}
		out = append(out, c.ID)
	}
	return out
}

// assistantToolCallsJSON returns replay JSON for an assistant message's
// tool_calls. Storage is decoupled from slot identity — slots compare by
// tool_call IDs only, so capture and replay do not need byte-identical
// JSON serialization.
func assistantToolCallsJSON(msg or.ChatMessages) ([]byte, error) {
	if msg.Type != or.ChatMessagesTypeAssistant {
		return nil, nil
	}
	calls := msg.ChatAssistantMessage.GetToolCalls()
	if len(calls) == 0 {
		return nil, nil
	}
	out := make([]openrouter.ToolCall, len(calls))
	for i, c := range calls {
		out[i] = openrouter.ToolCall{
			Index: 0,
			ID:    c.ID,
			Type:  string(c.Type),
			Function: openrouter.ToolCallFunction{
				Name:      c.Function.Name,
				Arguments: c.Function.Arguments,
			},
		}
	}
	bs, err := json.Marshal(out)
	if err != nil {
		return nil, fmt.Errorf("marshal assistant tool calls: %w", err)
	}
	return bs, nil
}
