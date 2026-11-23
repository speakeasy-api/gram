package hooks

// Claude Cowork capture. The org instruction forces every unified session into
// Cowork, where the plugin's hooks fire for each prompt and tool call, so the
// per-event hooks capture the session the way they do in Claude Code. The
// first message is the exception: it was typed in chat before the session
// moved into the Cowork VM, so no prompt hook saw it. The transcript the hooks
// binary ships from Stop backfills it, along with the assistant text Claude
// Code reports to no hook.
//
// Both paths can describe the same prompt or tool call, so both key those rows
// by the ids Claude reports to both: the prompt's prompt_id (promptId in the
// transcript) and the tool call's tool_use_id. The (chat_id,
// external_message_id) unique index then keeps exactly one row per message,
// whichever path writes first and however often either one is re-sent.
//
// Some Cowork sessions run Claude Code only as a tool server (`claude mcp
// serve`): the conversation lives on claude.ai and no transcript is written.
// There the binary sends each reply from Stop as an assistant.responded event
// instead, which is stored under a key of its own (claudeReplyKey). A session
// with a transcript never sends that event, so a reply is never stored twice.

import (
	"crypto/sha256"
	"encoding/hex"
	"strings"
	"time"

	gen "github.com/speakeasy-api/gram/server/gen/hooks"
	"github.com/speakeasy-api/gram/server/internal/conv"
)

const (
	claudePromptKeyPrefix     = "claude-prompt:"
	claudeToolCallKeyPrefix   = "claude-tool-call:"
	claudeToolResultKeyPrefix = "claude-tool-result:"
	claudeReplyKeyPrefix      = "claude-reply:"
)

// claudeSharedRowKey returns the external message id a row shares with the
// per-event hooks: prompts by prompt id, tool calls and results by tool call
// id. "" means only the transcript describes the row (assistant text,
// injected user text), so it keeps its per-entry key.
func claudeSharedRowKey(row claudeTranscriptRow, promptID string) string {
	switch {
	case row.role == "user" && strings.TrimSpace(promptID) != "":
		return claudePromptKeyPrefix + strings.TrimSpace(promptID)
	case row.role == "assistant" && row.toolUseID != "":
		return claudeToolCallKeyPrefix + row.toolUseID
	case row.role == "tool" && row.toolCallID != "":
		return claudeToolResultKeyPrefix + row.toolCallID
	default:
		return ""
	}
}

// coworkEventRow maps a per-event hook from a Cowork session to
// the row the transcript would store for the same message, under the shared
// key. A reply comes as an event only from a session without a transcript, so
// it is stored under its own key. ok is false for events that carry no shared
// id or no content.
func coworkEventRow(payload *gen.IngestPayload, occurredAt time.Time) (claudeTranscriptRow, bool) {
	row := claudeTranscriptRow{
		externalID:   "",
		role:         "",
		content:      "",
		toolCallID:   "",
		toolUseID:    "",
		toolCalls:    nil,
		finishReason: "",
		model:        canonicalModel(payload),
		messageID:    "",
		createdAt:    occurredAt,
	}
	switch strings.TrimSpace(payload.Event.Type) {
	case "prompt.submitted":
		promptID := strings.TrimSpace(conv.PtrValOr(payload.Session.TurnID, ""))
		content := canonicalPromptText(payload)
		if promptID == "" || content == "" {
			return row, false
		}
		row.role, row.content = "user", content
		row.externalID = claudeSharedRowKey(row, promptID)
	case "tool.requested":
		toolID := canonicalToolCallID(payload)
		if toolID == "" || canonicalToolName(payload) == "" || canonicalPermissionType(payload) != "" ||
			strings.EqualFold(strings.TrimSpace(conv.PtrValOr(payload.Source.RawEventName, "")), "PermissionRequest") {
			return row, false
		}
		toolCalls, err := canonicalToolCallsJSON(payload)
		if err != nil {
			return row, false
		}
		row.role, row.toolUseID, row.toolCalls, row.finishReason = "assistant", toolID, toolCalls, "tool_calls"
		row.externalID = claudeSharedRowKey(row, "")
	case "tool.completed", "tool.failed":
		toolID := canonicalToolCallID(payload)
		if toolID == "" {
			return row, false
		}
		content := canonicalToolResultContent(payload)
		failed := strings.TrimSpace(payload.Event.Type) == "tool.failed"
		if failed && isEmptyJSONValue(content) {
			content = marshalToJSON(toolFailedWithoutErrorContent)
		}
		if strings.TrimSpace(content) == "" {
			content = marshalToJSON(toolCompletedWithoutOutputContent)
		}
		row.role, row.content, row.toolCallID = "tool", content, toolID
		if failed {
			row.finishReason = toolResultFinishReasonError
		}
		row.externalID = claudeSharedRowKey(row, "")
	case "assistant.responded":
		content := strings.TrimSpace(conv.StripNUL(canonicalMessageText(payload)))
		if content == "" {
			return row, false
		}
		row.role, row.content = "assistant", content
		row.externalID = claudeReplyKey(payload, content)
	default:
		return row, false
	}
	return row, row.externalID != ""
}

// claudeReplyKey keys a reply a Cowork Stop hook sent as an event. Claude
// shares no id for a reply, so the key comes from the event itself: the
// prompt it answers, when the sender says it happened, and its text. A
// re-sent event (the binary's offline spool replays the same envelope) maps
// to the same row, and two replies to one prompt stay apart. The sender's
// own occurred_at is hashed, not the clamped time the row is stored at, so a
// late replay still matches.
func claudeReplyKey(payload *gen.IngestPayload, content string) string {
	sum := sha256.Sum256([]byte(strings.Join([]string{
		strings.TrimSpace(conv.PtrValOr(payload.Session.TurnID, "")),
		strings.TrimSpace(conv.PtrValOr(payload.Event.OccurredAt, "")),
		content,
	}, "\x00")))
	return claudeReplyKeyPrefix + hex.EncodeToString(sum[:16])
}
