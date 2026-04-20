package chat

import (
	"crypto/sha256"

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
// the canonical fields (role, textual content, tool call id). parentHash is
// nil for the first message in a chain.
func chainMessageHash(parentHash []byte, role, content, toolCallID string) []byte {
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
	return h.Sum(nil)
}

func hashIncomingMessage(parentHash []byte, msg or.ChatMessages) []byte {
	var toolCallID string
	if tc := openrouter.GetToolCallID(msg); tc != nil {
		toolCallID = *tc
	}
	return chainMessageHash(parentHash, openrouter.GetRole(msg), openrouter.GetText(msg), toolCallID)
}

func hashAssistantResponse(parentHash []byte, content string) []byte {
	return chainMessageHash(parentHash, "assistant", content, "")
}
