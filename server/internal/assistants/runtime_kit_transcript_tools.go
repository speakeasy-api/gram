package assistants

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/speakeasy-api/gram/server/internal/chat"
	chatrepo "github.com/speakeasy-api/gram/server/internal/chat/repo"
	"github.com/speakeasy-api/gram/server/internal/metering"
)

type kitProjectedToolCall struct {
	ID       string                   `json:"id"`
	Type     string                   `json:"type"`
	Function kitProjectedToolFunction `json:"function"`
}

type kitProjectedToolFunction struct {
	Name      string `json:"name"`
	Arguments string `json:"arguments"`
}

func (p *KitTranscriptProjector) handleKitToolUpdate(ctx context.Context, b KitTranscriptBinding, u kitTranscriptUpdate, raw json.RawMessage) (KitTranscriptResult, error) {
	var out KitTranscriptResult
	out.MessageID, out.Role, out.Replacement = u.ToolCallID, "tool", true
	if u.ToolCallID == "" {
		return out, fmt.Errorf("kit transcript tool update missing stable ID")
	}
	if p.writer == nil {
		return out, fmt.Errorf("kit transcript writer unavailable")
	}
	guard := func(ctx context.Context, tx pgx.Tx) error { return p.guard(ctx, tx, b) }
	callID := KitTranscriptMessageID(b, "tool_call", u.ToolCallID)
	changed, err := p.writer.ProjectMessage(ctx, b.ProjectID, callID, guard, func(old *chatrepo.ChatMessage) (chat.MessageWrite, bool, error) {
		return translateKitToolCall(b, callID, u.ToolCallID, raw, old)
	})
	out.Changed = changed
	if err != nil {
		return out, fmt.Errorf("project kit transcript tool call: %w", err)
	}
	// Retain the existing sparse patch row from pending onward. This preserves
	// unknown ACP fields and explicit nulls without a second state store.
	resultID := KitTranscriptMessageID(b, "tool_call_update", u.ToolCallID)
	changed, err = p.writer.ProjectMessage(ctx, b.ProjectID, resultID, guard, func(old *chatrepo.ChatMessage) (chat.MessageWrite, bool, error) {
		write, apply, text, generic, err := translateKitMessage(b, resultID, u, raw, old)
		out.SourceText, out.GenericContentTypes = text, generic
		return write, apply, err
	})
	out.Changed = out.Changed || changed
	if err != nil {
		return out, fmt.Errorf("project kit transcript tool result: %w", err)
	}
	return out, nil
}

func translateKitToolCall(b KitTranscriptBinding, id uuid.UUID, toolCallID string, raw json.RawMessage, old *chatrepo.ChatMessage) (chat.MessageWrite, bool, error) {
	var write chat.MessageWrite
	var patch struct {
		Title    string          `json:"title"`
		Status   string          `json:"status"`
		RawInput json.RawMessage `json:"rawInput"`
	}
	if err := json.Unmarshal(raw, &patch); err != nil {
		return write, false, fmt.Errorf("decode kit tool call: %w", err)
	}
	var call kitProjectedToolCall
	if old != nil {
		var calls []kitProjectedToolCall
		if err := json.Unmarshal(old.ToolCalls, &calls); err != nil {
			return write, false, fmt.Errorf("decode stored kit tool call: %w", err)
		}
		if len(calls) != 1 || calls[0].ID != toolCallID {
			return write, false, fmt.Errorf("invalid stored kit tool call identity")
		}
		call = calls[0]
	} else {
		// Only the pending title is the function name. Later titles describe
		// human intent and must never replace (or invent) that identity.
		if patch.Status != "pending" || patch.Title == "" {
			return write, false, nil
		}
		call.ID, call.Type = toolCallID, "function"
		call.Function.Name, call.Function.Arguments = patch.Title, "{}"
	}
	if len(patch.RawInput) != 0 {
		call.Function.Arguments = string(patch.RawInput)
	}
	calls, err := json.Marshal([]kitProjectedToolCall{call})
	if err != nil {
		return write, false, fmt.Errorf("encode kit tool call: %w", err)
	}
	write.Params.ID, write.Params.ProjectID, write.Params.ChatID = id, b.ProjectID, b.ChatID
	write.Params.Role = "assistant"
	write.Params.Source = pgtype.Text{String: "kit", Valid: true}
	write.Params.MessageID = pgtype.Text{String: id.String(), Valid: true}
	write.Params.ToolCalls = calls
	write.Params.ContentRaw = []byte("[]")
	digest := sha256.Sum256(write.Params.ContentRaw)
	write.Params.ContentHash = digest[:]
	write.AssistantID, write.WorkloadSource = b.AssistantID, metering.WorkloadSourceAssistant
	if old != nil {
		write.Params.Generation, write.Params.CreatedAt = old.Generation, old.CreatedAt
	}
	return write, true, nil
}
