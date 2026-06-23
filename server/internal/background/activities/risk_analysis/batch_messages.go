package risk_analysis

import (
	"context"
	"encoding/json"
	"log/slog"
	"slices"
	"strings"

	"github.com/google/uuid"

	"github.com/speakeasy-api/gram/server/internal/attr"
	"github.com/speakeasy-api/gram/server/internal/message"
	"github.com/speakeasy-api/gram/server/internal/risk/repo"
)

type batchMessage struct {
	ID           uuid.UUID
	Type         message.Type
	Content      string
	RawToolCalls []byte
	ToolCalls    []recordedToolCall
}

type recordedToolCall struct {
	ID       string `json:"id"`
	Function struct {
		Name      string `json:"name"`
		Arguments string `json:"arguments"`
	} `json:"function"`
}

const malformedToolCallsName = "tool_calls"

func newBatchMessages(ctx context.Context, logger *slog.Logger, rows []repo.GetMessageContentBatchRow) []batchMessage {
	messages := make([]batchMessage, 0, len(rows))
	for _, row := range rows {
		messageType, ok := messageRowMessageType(row)
		if !ok {
			continue
		}

		msg := batchMessage{
			ID:           row.ID,
			Type:         messageType,
			Content:      row.Content,
			RawToolCalls: row.ToolCalls,
			ToolCalls:    []recordedToolCall{},
		}
		if messageType == message.ToolRequest && len(row.ToolCalls) > 0 {
			msg.ToolCalls = parseRecordedToolCalls(ctx, logger, row.ToolCalls)
		}
		messages = append(messages, msg)
	}
	return messages
}

func parseRecordedToolCalls(ctx context.Context, logger *slog.Logger, raw []byte) []recordedToolCall {
	var calls []recordedToolCall
	if err := json.Unmarshal(raw, &calls); err != nil {
		logger.WarnContext(ctx, "risk analysis: failed to parse tool_calls", attr.SlogError(err))
		var fallback recordedToolCall
		fallback.Function.Name = malformedToolCallsName
		fallback.Function.Arguments = string(raw)
		return []recordedToolCall{fallback}
	}
	return calls
}

func filterMessagesByMessageTypes(messages []repo.GetMessageContentBatchRow, messageTypes []string) []repo.GetMessageContentBatchRow {
	filtered := make([]repo.GetMessageContentBatchRow, 0, len(messages))
	for _, msg := range messages {
		messageType, ok := messageRowMessageType(msg)
		if !ok {
			continue
		}
		if len(messageTypes) > 0 && !slices.Contains(messageTypes, messageType) {
			continue
		}
		filtered = append(filtered, msg)
	}
	return filtered
}

func messageRowMessageType(msg repo.GetMessageContentBatchRow) (message.Type, bool) {
	switch msg.Role {
	case "user":
		return message.User, true
	case "tool":
		return message.ToolResponse, true
	case "assistant":
		if len(msg.ToolCalls) > 0 {
			return message.ToolRequest, true
		}
		return message.Assistant, true
	default:
		return "", false
	}
}

func batchJudgeMessage(msg batchMessage) JudgeMessage {
	if msg.Type != message.ToolRequest {
		return NewJudgeMessage(msg.Type, "", msg.Content)
	}

	switch len(msg.ToolCalls) {
	case 0:
		return NewJudgeMessage(msg.Type, "", string(msg.RawToolCalls))
	case 1:
		return NewJudgeMessage(msg.Type, msg.ToolCalls[0].Function.Name, msg.ToolCalls[0].Function.Arguments)
	default:
		judgeCalls := make([]JudgeToolCall, 0, len(msg.ToolCalls))
		for _, c := range msg.ToolCalls {
			if c.Function.Name == "" && strings.TrimSpace(c.Function.Arguments) == "" {
				continue
			}
			judgeCalls = append(judgeCalls, NewJudgeToolCall(c.Function.Name, c.Function.Arguments))
		}
		if len(judgeCalls) == 0 {
			return NewJudgeMessage(msg.Type, "", string(msg.RawToolCalls))
		}
		return NewJudgeMessageForToolCalls(judgeCalls)
	}
}

func batchMessageView(msg batchMessage) MessageView {
	view := MessageView{Content: msg.Content, Type: msg.Type, Tools: []ToolView{}}
	if msg.Type != message.ToolRequest {
		return view
	}
	for _, c := range msg.ToolCalls {
		if c.Function.Name == "" && strings.TrimSpace(c.Function.Arguments) == "" {
			continue
		}
		view.Tools = append(view.Tools, NewToolView(c.Function.Name, c.Function.Arguments))
	}
	return view
}
