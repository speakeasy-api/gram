package risk_analysis

import (
	"context"
	"encoding/json"
	"log/slog"
	"slices"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/speakeasy-api/gram/server/internal/attr"
	"github.com/speakeasy-api/gram/server/internal/judgemessage"
	"github.com/speakeasy-api/gram/server/internal/message"
	"github.com/speakeasy-api/gram/server/internal/risk/repo"
)

type batchMessage struct {
	ID           uuid.UUID
	Type         message.Type
	Content      string
	RawToolCalls []byte
	ToolCalls    []recordedToolCall
	// UserID is the scanned chat's owner (empty for unattributed sessions),
	// carried onto judge completions for scanning-volume attribution and into
	// Shadow MCP bypass checks. GetMessageContentBatch must return the same
	// WorkOS user-id space that authz.ResolveUserPrincipals expects.
	UserID string
	// CreatedAt is when the message was recorded. The shadow-MCP scanner uses
	// the batch's oldest value to bound its ClickHouse provenance lookup.
	CreatedAt time.Time
	// Source is the agent that recorded the message (Codex, Cursor, ...). The
	// shadow-MCP scanner attributes unresolved provenance to it.
	Source string
}

// scanSurface is the text content scanners (gitleaks, presidio) evaluate:
// message content plus, for tool requests, each call's arguments. Realtime
// scans the same argument text; composing it here keeps args-only secrets and
// PII visible to batch. Positions in an appended region index into this
// composed surface, mirroring realtime's args-as-text anchoring.
func (m batchMessage) scanSurface() string {
	if m.Type != message.ToolRequest || len(m.ToolCalls) == 0 {
		return m.Content
	}
	var b strings.Builder
	b.WriteString(m.Content)
	for _, call := range m.ToolCalls {
		if call.Function.Arguments == "" {
			continue
		}
		if b.Len() > 0 {
			b.WriteString("\n")
		}
		b.WriteString(call.Function.Arguments)
	}
	return b.String()
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
		msg, ok := newBatchMessage(ctx, logger, row.ID, row.Role, row.Content, row.ToolCalls)
		if !ok {
			continue
		}
		msg.UserID = row.ChatUserID
		if row.CreatedAt.Valid {
			msg.CreatedAt = row.CreatedAt.Time
		}
		msg.Source = row.Source.String
		messages = append(messages, msg)
	}
	return messages
}

// newBatchMessage builds a single batchMessage from the recorded columns,
// applying the same role→type mapping and tool-call parsing every batch scanner
// and the eval-guardrail replay share. ok is false for roles the analyzer does
// not evaluate.
func newBatchMessage(ctx context.Context, logger *slog.Logger, id uuid.UUID, role, content string, toolCalls []byte) (batchMessage, bool) {
	messageType, ok := messageTypeForRole(role, toolCalls)
	if !ok {
		var zero batchMessage
		return zero, false
	}

	msg := batchMessage{
		ID:           id,
		Type:         messageType,
		Content:      content,
		RawToolCalls: toolCalls,
		ToolCalls:    []recordedToolCall{},
		UserID:       "",
		CreatedAt:    time.Time{},
		Source:       "",
	}
	if messageType == message.ToolRequest && len(toolCalls) > 0 {
		msg.ToolCalls = parseRecordedToolCalls(ctx, logger, toolCalls)
	}
	return msg, true
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
	return messageTypeForRole(msg.Role, msg.ToolCalls)
}

func messageTypeForRole(role string, toolCalls []byte) (message.Type, bool) {
	switch role {
	case "user":
		return message.User, true
	case "tool":
		return message.ToolResponse, true
	case "assistant":
		if len(toolCalls) > 0 {
			return message.ToolRequest, true
		}
		return message.Assistant, true
	default:
		return "", false
	}
}

func batchJudgeMessage(msg batchMessage) judgemessage.Message {
	if msg.Type != message.ToolRequest {
		return judgemessage.New(msg.Type, "", msg.Content)
	}

	switch len(msg.ToolCalls) {
	case 0:
		return judgemessage.New(msg.Type, "", string(msg.RawToolCalls))
	case 1:
		return judgemessage.New(msg.Type, msg.ToolCalls[0].Function.Name, msg.ToolCalls[0].Function.Arguments)
	default:
		judgeCalls := make([]judgemessage.ToolCall, 0, len(msg.ToolCalls))
		for _, c := range msg.ToolCalls {
			if c.Function.Name == "" && strings.TrimSpace(c.Function.Arguments) == "" {
				continue
			}
			judgeCalls = append(judgeCalls, judgemessage.NewToolCall(c.Function.Name, c.Function.Arguments))
		}
		if len(judgeCalls) == 0 {
			return judgemessage.New(msg.Type, "", string(msg.RawToolCalls))
		}
		return judgemessage.NewForToolCalls(judgeCalls)
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
