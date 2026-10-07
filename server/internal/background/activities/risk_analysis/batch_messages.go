package risk_analysis

import (
	"context"
	"encoding/json"
	"log/slog"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/google/uuid"

	"github.com/speakeasy-api/gram/server/internal/attr"
	"github.com/speakeasy-api/gram/server/internal/judgemessage"
	"github.com/speakeasy-api/gram/server/internal/message"
	"github.com/speakeasy-api/gram/server/internal/risk/repo"
)

// batchScanMaxContentBytes bounds every batch scan input when it is loaded, so
// no scanner or Pub/Sub publish sees an unbounded message. Same value as the
// enforcement dispatch default, which caps each field separately instead.
const batchScanMaxContentBytes = 50 * 1024

type batchMessage struct {
	ID                     uuid.UUID
	ChatID                 uuid.UUID
	ParentChatMessageID    uuid.UUID
	ContentPart            bool
	Type                   message.Type
	Content                string
	RawToolCalls           []byte
	ToolCalls              []recordedToolCall
	PriorUserRequest       string
	RecentUntrustedContent string
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
	// Truncated reports that bound cut content, arguments, raw tool-call JSON,
	// a tool-call name or id, or the number of calls.
	Truncated bool
}

const (
	// Tool names and ids identify calls for name-based scanners, so they are
	// capped on their own rather than cut by the text budget.
	batchToolIdentityMaxBytes = 512
	batchMaxToolCalls         = 512
)

// bound cuts Content and tool-call arguments to one shared
// batchScanMaxContentBytes budget, caps each call's name and id and the number
// of calls separately, and cuts RawToolCalls to the text budget on its own.
func (m *batchMessage) bound() {
	remaining := batchScanMaxContentBytes
	m.Content = m.boundText(m.Content, &remaining)
	if len(m.ToolCalls) > batchMaxToolCalls {
		m.ToolCalls = m.ToolCalls[:batchMaxToolCalls]
		m.Truncated = true
	}
	for i := range m.ToolCalls {
		call := &m.ToolCalls[i]
		call.Function.Name = m.boundIdentity(call.Function.Name)
		call.ID = m.boundIdentity(call.ID)
		call.Function.Arguments = m.boundText(call.Function.Arguments, &remaining)
	}
	if len(m.RawToolCalls) > batchScanMaxContentBytes {
		m.RawToolCalls = []byte(truncateAtRuneBoundary(string(m.RawToolCalls), batchScanMaxContentBytes))
		// Raw JSON is only scanned when no call carries a name or arguments.
		if !m.hasUsableToolCall() {
			m.Truncated = true
		}
	}
}

func (m batchMessage) hasUsableToolCall() bool {
	for _, c := range m.ToolCalls {
		if c.Function.Name != "" || strings.TrimSpace(c.Function.Arguments) != "" {
			return true
		}
	}
	return false
}

func (m *batchMessage) boundIdentity(s string) string {
	if len(s) > batchToolIdentityMaxBytes {
		m.Truncated = true
		return truncateAtRuneBoundary(s, batchToolIdentityMaxBytes)
	}
	return s
}

func (m *batchMessage) boundText(s string, remaining *int) string {
	if len(s) > *remaining {
		s = truncateAtRuneBoundary(s, *remaining)
		m.Truncated = true
	}
	*remaining -= len(s)
	return s
}

// truncateAtRuneBoundary returns the longest prefix of s whose byte length is
// <= n and that does not split a UTF-8 rune. Returns s unchanged when it
// already fits.
func truncateAtRuneBoundary(s string, n int) string {
	if len(s) <= n {
		return s
	}
	for n > 0 && !utf8.RuneStart(s[n]) {
		n--
	}
	return s[:n]
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
		msg.ChatID = row.ChatID
		msg.UserID = row.ChatUserID
		msg.PriorUserRequest = row.PriorUserRequest
		msg.RecentUntrustedContent = row.RecentUntrustedContent
		if row.CreatedAt.Valid {
			msg.CreatedAt = row.CreatedAt.Time
		}
		msg.Source = row.Source.String
		messages = append(messages, msg)
	}
	return messages
}

func newContentPartBatchMessages(rows []repo.GetContentPartBatchRow, contents []string) []batchMessage {
	messages := make([]batchMessage, 0, len(rows))
	for i, row := range rows {
		messageType := strings.TrimSpace(row.MessageType)
		if !message.IsTypeValid(messageType) {
			continue
		}
		// Content parts carry no chat position, so they scan without trajectory
		// context.
		msg := batchMessage{
			ID:                     row.ID,
			ChatID:                 row.ChatID,
			ParentChatMessageID:    row.ParentChatMessageID.UUID,
			ContentPart:            true,
			Type:                   messageType,
			Content:                contents[i],
			RawToolCalls:           nil,
			ToolCalls:              []recordedToolCall{},
			PriorUserRequest:       "",
			RecentUntrustedContent: "",
			UserID:                 row.ChatUserID,
			CreatedAt:              time.Time{},
			Source:                 row.Source.String,
			Truncated:              false,
		}
		if row.CreatedAt.Valid {
			msg.CreatedAt = row.CreatedAt.Time
		}
		msg.bound()
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
		ID:                     id,
		ChatID:                 uuid.Nil,
		ParentChatMessageID:    uuid.Nil,
		ContentPart:            false,
		Type:                   messageType,
		Content:                content,
		RawToolCalls:           toolCalls,
		ToolCalls:              []recordedToolCall{},
		PriorUserRequest:       "",
		RecentUntrustedContent: "",
		UserID:                 "",
		CreatedAt:              time.Time{},
		Source:                 "",
		Truncated:              false,
	}
	if messageType == message.ToolRequest && len(toolCalls) > 0 {
		msg.ToolCalls = parseRecordedToolCalls(ctx, logger, toolCalls)
	}
	msg.bound()
	return msg, true
}

func (m batchMessage) chatMessageID() uuid.NullUUID {
	if m.ContentPart {
		return uuid.NullUUID{UUID: uuid.Nil, Valid: false}
	}
	return uuid.NullUUID{UUID: m.ID, Valid: true}
}

func (m batchMessage) chatContentPartID() uuid.NullUUID {
	if !m.ContentPart {
		return uuid.NullUUID{UUID: uuid.Nil, Valid: false}
	}
	return uuid.NullUUID{UUID: m.ID, Valid: true}
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

// anchorIDStrings returns the scanned unit's anchor as proto-ready pointers:
// exactly one of chat message id or content part id is non-nil.
func (m batchMessage) anchorIDStrings() (*string, *string) {
	chatMessageID := m.chatMessageID()
	if chatMessageID.Valid {
		id := chatMessageID.UUID.String()
		return &id, nil
	}

	chatContentPartID := m.chatContentPartID()
	if chatContentPartID.Valid {
		id := chatContentPartID.UUID.String()
		return nil, &id
	}

	return nil, nil
}
