package chat

import (
	"context"
	"encoding/json"

	"github.com/google/uuid"

	"github.com/speakeasy-api/gram/server/internal/attr"
	"github.com/speakeasy-api/gram/server/internal/chat/repo"
)

// Turn frames are published from the write path rather than from the
// completions proxy. The proxy sees one model call, but a turn is often
// several completions with tool calls between them, and "the turn is over" is
// a property of the persisted rows — so only the writer knows enough to drive
// a dashboard that has stopped polling.
//
// Publishing happens after commit, never inside the transaction: a frame
// describes a row that exists, and a rolled-back turn must not have announced
// itself. Failures are logged and swallowed, because a watcher missing a frame
// is a responsiveness problem while a failed write is a correctness one.

// publishTurnFrames emits one frame per row just persisted, then a terminal
// frame when the turn has finished.
//
// Ordering matters to the client: tool outputs answer calls announced by an
// earlier assistant row, so pending (user/tool) rows are emitted before the
// assistant rows written in the same commit.
func (w *ChatMessageWriter) publishTurnFrames(
	ctx context.Context,
	pending []chatMessageRow,
	assistants []repo.CreateChatMessageParams,
) {
	if w == nil || w.turnStream == nil {
		return
	}

	for _, row := range pending {
		if row.role != "tool" || row.toolCallID == "" {
			continue
		}
		w.publishTurnFrame(ctx, row.chatID, TurnFrame{
			Kind:         TurnFrameToolOutput,
			Cursor:       "",
			Text:         "",
			MessageID:    "",
			ToolCalls:    nil,
			FinishReason: "",
			ToolCallID:   row.toolCallID,
			Output:       marshalToolOutput(row),
		})
	}

	for _, msg := range assistants {
		if msg.Role != "assistant" {
			continue
		}
		w.publishTurnFrame(ctx, msg.ChatID, TurnFrame{
			Kind:         TurnFrameMessage,
			Cursor:       "",
			Text:         "",
			MessageID:    msg.MessageID.String,
			ToolCalls:    json.RawMessage(msg.ToolCalls),
			FinishReason: msg.FinishReason.String,
			ToolCallID:   "",
			Output:       nil,
		})

		// A turn ends on an assistant row that neither stops to call a tool
		// nor was cut short mid-stream. This mirrors the terminal check the
		// dashboard's poll used to make for itself.
		if isTerminalAssistantRow(msg) {
			w.publishTurnFrame(ctx, msg.ChatID, TurnFrame{
				Kind:         TurnFrameDone,
				Cursor:       "",
				Text:         "",
				MessageID:    msg.MessageID.String,
				ToolCalls:    nil,
				FinishReason: msg.FinishReason.String,
				ToolCallID:   "",
				Output:       nil,
			})
		}
	}
}

// isTerminalAssistantRow reports whether this row ends the turn: it carries no
// tool calls the runner still has to answer, and the model stopped of its own
// accord rather than being cut off.
func isTerminalAssistantRow(msg repo.CreateChatMessageParams) bool {
	if len(msg.ToolCalls) > 0 {
		var calls []json.RawMessage
		if err := json.Unmarshal(msg.ToolCalls, &calls); err == nil && len(calls) > 0 {
			return false
		}
	}
	switch msg.FinishReason.String {
	case "stop", "end_turn", "":
		return true
	default:
		// length, content_filter, tool_calls — the turn is not cleanly done,
		// so leave the stream open and let the client's resync decide.
		return false
	}
}

func (w *ChatMessageWriter) publishTurnFrame(ctx context.Context, chatID uuid.UUID, frame TurnFrame) {
	if _, err := w.turnStream.Publish(ctx, chatID, frame); err != nil {
		w.logger.WarnContext(ctx, "publish turn frame",
			attr.SlogChatID(chatID.String()),
			attr.SlogError(err),
		)
	}
}

// marshalToolOutput renders a tool row's content for the frame. A row whose
// content will not marshal yields a nil output rather than dropping the frame:
// the client still learns the call was answered and can resync for the body,
// which beats leaving a tool call rendered as forever-pending.
func marshalToolOutput(row chatMessageRow) json.RawMessage {
	encoded, err := json.Marshal(row.content)
	if err != nil {
		return nil
	}
	return encoded
}
