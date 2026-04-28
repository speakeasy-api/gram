package chat

import (
	"crypto/sha256"
	"encoding/json"
	"fmt"

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
// the canonical fields (role, textual content, tool call id, tool calls JSON).
// parentHash is nil for the first message in a chain. toolCallsJSON is the
// canonical JSONB blob persisted on the row (nil for messages without tool
// calls). It must be byte-identical to whatever buildAssistantRows /
// buildPendingRows wrote to the row's tool_calls column, otherwise replay vs.
// initial-capture hashing will diverge spuriously.
func chainMessageHash(parentHash []byte, role, content, toolCallID string, toolCallsJSON []byte) []byte {
	h := sha256.New()
	if len(parentHash) > 0 {
		h.Write(parentHash)
		h.Write([]byte(chainSep))
	}
	h.Write([]byte(role))
	h.Write([]byte(fieldSep))
	h.Write([]byte(content))
	h.Write([]byte(fieldSep))
	h.Write([]byte(toolCallID))
	h.Write([]byte(fieldSep))
	h.Write(toolCallsJSON)
	return h.Sum(nil)
}

func hashIncomingMessage(parentHash []byte, msg or.ChatMessages) []byte {
	var toolCallID string
	if tc := openrouter.GetToolCallID(msg); tc != nil {
		toolCallID = *tc
	}
	tcJSON, _ := assistantToolCallsJSON(msg)
	return chainMessageHash(parentHash, openrouter.GetRole(msg), openrouter.GetText(msg), toolCallID, tcJSON)
}

func hashAssistantResponse(parentHash []byte, content string, toolCallsJSON []byte) []byte {
	return chainMessageHash(parentHash, "assistant", content, "", toolCallsJSON)
}

// assistantToolCallsJSON returns the canonical JSON bytes for an assistant
// message's tool_calls (the same shape persisted to chat_messages.tool_calls
// and replayed to runners by loadChatHistory). For non-assistant messages or
// assistant messages without tool calls it returns nil.
//
// Both initial capture (via /chat/completions request bodies passing through
// buildPendingRows) and the model-response path (buildAssistantRows) must
// store identical bytes here, otherwise the chain hash diverges between
// turns. We round-trip through openrouter.ToolCall to share serialization
// with the response path.
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
