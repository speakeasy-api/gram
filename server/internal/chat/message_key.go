package chat

import (
	"crypto/sha256"
	"encoding/hex"
	"strings"

	"github.com/google/uuid"

	"github.com/speakeasy-api/gram/server/internal/claudetag"
)

// inferenceChatPrefix marks an external_chat_id the Anthropic inference hook
// minted itself; it names no agent session.
const inferenceChatPrefix = "anthropic-inference:"

// AgentSessionID is the raw agent session id every capture lane agrees on for
// a chat: the id the inference hook asserted, else the id the hook or import
// stored, else the chat id. Analytics joins on it, so each lane must land on
// the same value for the same session.
func AgentSessionID(chatID uuid.UUID, externalChatID, inferenceSessionID string) string {
	if inferenceSessionID != "" {
		return inferenceSessionID
	}
	if externalChatID != "" && !strings.HasPrefix(externalChatID, inferenceChatPrefix) {
		return externalChatID
	}
	return chatID.String()
}

// MessageTextKey is the content-derived identity of a message: no two lanes
// share a native message id, so the same message captured twice collapses on
// its session, role and normalized text. Returns "" when nothing remains of
// the text once harness framing and whitespace are stripped.
func MessageTextKey(sessionID, role, text string) string {
	normalized := normalizeMessageText(text)
	if normalized == "" {
		return ""
	}
	sum := sha256.Sum256([]byte(sessionID + "\x00" + role + "\x00" + normalized))
	return hex.EncodeToString(sum[:])
}

func normalizeMessageText(text string) string {
	body, _ := claudetag.Envelope(text)
	return strings.TrimSpace(strings.ReplaceAll(body, "\r\n", "\n"))
}
