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

// isStoredEmptyAsst reports whether a stored row collapses to an
// "empty assistant" — role=assistant, no text, no tool_calls, no
// tool_call_id. These rows arrive when the upstream model returns
// finish_reason=stop with nothing to say (Anthropic Sonnet
// extended-thinking blank stops, observed on prod chat
// 6d5bee05-9809-5678-8512-b3b2fb390120). The runner-side transcript
// (and other clients) treat such turns as no-ops and don't include
// them on the next request, so the stored row has no counterpart.
// The matcher steps over these so the asymmetry doesn't trip a
// false-positive divergence.
func isStoredEmptyAsst(role, content, toolCallID string, toolCallsJSON []byte) bool {
	if role != "assistant" {
		return false
	}
	if content != "" {
		return false
	}
	if toolCallID != "" {
		return false
	}
	return len(toolCallIDsFromStoredJSON(toolCallsJSON)) == 0
}

// isIncomingEmptyAsst is the wire-side counterpart to isStoredEmptyAsst:
// an assistant message with no text, no tool_calls, no tool_call_id.
// Defensive — the endpoint serves clients beyond the agentkit runner,
// any of which may send the wire shape `{role:asst, content:null,
// tool_calls:[]}` for a no-op turn.
func isIncomingEmptyAsst(msg or.ChatMessages) bool {
	if msg.Type != or.ChatMessagesTypeAssistant {
		return false
	}
	if openrouter.GetText(msg) != "" {
		return false
	}
	return len(toolCallIDsFromIncoming(msg)) == 0
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
