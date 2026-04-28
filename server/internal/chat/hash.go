package chat

import (
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"hash"
	"sort"

	or "github.com/OpenRouterTeam/go-sdk/models/components"
	"github.com/speakeasy-api/gram/server/internal/thirdparty/openrouter"
)

// Merkle-style chain hash for chat messages. Each hash links to its parent so
// any divergence in the middle of a conversation propagates forward. Used to
// detect client-side compaction or edits against the stored history.
const (
	chainSep = "\x1e"
	fieldSep = "\x1f"
)

// chainMessageHash derives a message's content hash from its parent hash and
// the durable identity of the message. Identity differs by role:
//
//   - tool result rows: role + tool_call_id only. The result body is opaque
//     and non-deterministic across providers/replay paths (JSON
//     re-serialization, key ordering, whitespace), so hashing it forces a new
//     generation every time the client round-trips a tool result through
//     parse → re-stringify.
//   - assistant rows that issue tool calls: role + the sorted set of
//     tool_call IDs. Per the storage rule, these rows carry no text content.
//   - user / system / assistant text rows: role + content. No tool IDs apply.
//
// parentHash is nil for the first message in a chain. Function names and
// arguments are deliberately excluded — tool_call IDs are already unique per
// call, so any later mutation of args/name still refers to the same call.
func chainMessageHash(parentHash []byte, role, content, toolCallID string, toolCallIDs []string) []byte {
	h := sha256.New()
	if len(parentHash) > 0 {
		h.Write(parentHash)
		h.Write([]byte(chainSep))
	}
	h.Write([]byte(role))
	h.Write([]byte(fieldSep))
	if toolCallID == "" && len(toolCallIDs) == 0 {
		h.Write([]byte(content))
	}
	h.Write([]byte(fieldSep))
	h.Write([]byte(toolCallID))
	h.Write([]byte(fieldSep))
	writeToolCallIDs(h, toolCallIDs)
	return h.Sum(nil)
}

func hashIncomingMessage(parentHash []byte, msg or.ChatMessages) []byte {
	var toolCallID string
	if tc := openrouter.GetToolCallID(msg); tc != nil {
		toolCallID = *tc
	}
	return chainMessageHash(parentHash, openrouter.GetRole(msg), openrouter.GetText(msg), toolCallID, toolCallIDsFromIncoming(msg))
}

func hashStoredMessage(parentHash []byte, role, content, toolCallID string, toolCallsJSON []byte) []byte {
	return chainMessageHash(parentHash, role, content, toolCallID, toolCallIDsFromStoredJSON(toolCallsJSON))
}

func hashAssistantResponse(parentHash []byte, content string, toolCalls []openrouter.ToolCall) []byte {
	return chainMessageHash(parentHash, "assistant", content, "", toolCallIDsFromResponse(toolCalls))
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

func toolCallIDsFromResponse(calls []openrouter.ToolCall) []string {
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

// writeToolCallIDs writes a sorted, length-delimited rendering of the IDs into
// h. Sorting makes the hash invariant to map-iteration order from streaming
// capture; length-prefixing each ID prevents `["ab","c"]` colliding with
// `["a","bc"]`.
func writeToolCallIDs(h hash.Hash, ids []string) {
	if len(ids) == 0 {
		return
	}
	sorted := append([]string(nil), ids...)
	sort.Strings(sorted)
	for _, id := range sorted {
		_, _ = fmt.Fprintf(h, "%d:%s%s", len(id), id, fieldSep)
	}
}

// assistantToolCallsJSON returns replay JSON for an assistant message's
// tool_calls. Storage bytes are deliberately decoupled from the chain hash —
// hashing uses tool_call IDs only, so capture and replay do not need
// byte-identical JSON serialization.
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
