package chat

import (
	"context"
	"encoding/json"
	"strconv"

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
		// Tool rows reach this slice too, and skipping them left their calls
		// unresolved on the client: assistant-ui's resume check sees a step
		// holding a tool call with no output and re-sends a turn the server
		// already finished, which is what produced a second sendMessage and a
		// spinner that outlived the reply.
		if msg.Role == "tool" {
			if msg.ToolCallID.Valid && msg.ToolCallID.String != "" {
				w.publishTurnFrame(ctx, msg.ChatID, TurnFrame{
					Kind: TurnFrameToolOutput, Cursor: "", Text: "", MessageID: "",
					ToolCalls: nil, FinishReason: "", ToolCallID: msg.ToolCallID.String,
					Output: json.RawMessage(strconv.Quote(msg.Content)),
				})
			}
			continue
		}
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
	if !isTerminalRow(msg.ToolCalls, "stop") {
		return false
	}
	// length, content_filter, tool_calls mean the turn is not cleanly done, so
	// the stream stays open and the client's resync decides.
	return isTerminalRow(nil, msg.FinishReason.String)
}

// TurnInterruptedFinishReason is the finish reason carried by the terminal
// frame a user interrupt publishes. Distinct from the model's own reasons so a
// watcher can tell "the user stopped this" from "the model finished".
const TurnInterruptedFinishReason = "interrupted"

// PublishTurnInterrupted ends a chat's turn stream because the user asked the
// assistant to stop.
//
// The persisted rows cannot do this on their own: an interrupted turn's last
// assistant row is whatever the proxy managed to capture before the runner
// dropped the upstream stream, and a partial row carries no natural finish
// reason — so nothing in the write path ever looks terminal, and a watcher
// would tail the stream until its own timeout. Publishing the terminal frame
// here is what actually settles every client watching the chat, including the
// tabs that did not press the button.
func (w *ChatMessageWriter) PublishTurnInterrupted(ctx context.Context, chatID uuid.UUID) {
	if w == nil || w.turnStream == nil || chatID == uuid.Nil {
		return
	}
	w.publishTurnFrame(ctx, chatID, TurnFrame{
		Kind:         TurnFrameDone,
		Cursor:       "",
		Text:         "",
		MessageID:    "",
		ToolCalls:    nil,
		FinishReason: TurnInterruptedFinishReason,
		ToolCallID:   "",
		Output:       nil,
	})
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

// publishRowFrames emits frames for rows persisted through the row-shaped write
// paths. Assistant turns reach the database through several of them — the
// capture strategy writes pending rows, assistant rows and whole turns
// separately — so every path publishes rather than relying on one funnel.
func (w *ChatMessageWriter) publishRowFrames(ctx context.Context, rows []chatMessageRow) {
	if w == nil || w.turnStream == nil {
		return
	}
	for _, row := range rows {
		switch row.role {
		case "tool":
			if row.toolCallID == "" {
				continue
			}
			w.publishTurnFrame(ctx, row.chatID, TurnFrame{
				Kind: TurnFrameToolOutput, Cursor: "", Text: "", MessageID: "",
				ToolCalls: nil, FinishReason: "", ToolCallID: row.toolCallID,
				Output: marshalToolOutput(row),
			})
		case "assistant":
			finish := ""
			if row.finishReason != nil {
				finish = *row.finishReason
			}
			w.publishTurnFrame(ctx, row.chatID, TurnFrame{
				Kind: TurnFrameMessage, Cursor: "", Text: "", MessageID: row.messageID,
				ToolCalls: json.RawMessage(row.toolCalls), FinishReason: finish,
				ToolCallID: "", Output: nil,
			})
			if isTerminalRow(row.toolCalls, finish) {
				w.publishTurnFrame(ctx, row.chatID, TurnFrame{
					Kind: TurnFrameDone, Cursor: "", Text: "", MessageID: row.messageID,
					ToolCalls: nil, FinishReason: finish, ToolCallID: "", Output: nil,
				})
			}
		}
	}
}

// isTerminalRow reports whether a persisted assistant row ends the turn: no
// tool calls left for the runner to answer, and the model stopped of its own
// accord rather than being cut off.
func isTerminalRow(toolCalls []byte, finishReason string) bool {
	if len(toolCalls) > 0 {
		var calls []json.RawMessage
		if err := json.Unmarshal(toolCalls, &calls); err == nil && len(calls) > 0 {
			return false
		}
	}
	// Only an explicit natural stop ends the turn. An empty finish_reason is
	// ambiguous — an intermediate row persisted before the model said why it
	// stopped looks identical to a finished one — and treating it as terminal
	// ended the stream after the first tool call, leaving the rest of the turn
	// unstreamed.
	switch finishReason {
	case "stop", "end_turn":
		return true
	default:
		return false
	}
}
