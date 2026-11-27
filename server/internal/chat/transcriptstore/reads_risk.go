package transcriptstore

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"

	"github.com/google/uuid"

	"github.com/speakeasy-api/gram/server/internal/chat/transcriptcrypto"
	"github.com/speakeasy-api/gram/server/internal/chat/transcriptstore/internal/plainrepo"
)

// Readers for risk analysis and its judges.

// riskContextRunes caps the prior user request and recent tool result that
// risk analysis reads around each message, like LEFT(content, 4000) in SQL.
const riskContextRunes = 4000

// leftRunes keeps the first n characters, like PostgreSQL LEFT on UTF-8 text.
func leftRunes(s string, n int) string {
	count := 0
	for i := range s {
		if count == n {
			return s[:i]
		}
		count++
	}
	return s
}

func capContent(n int) func(*transcriptcrypto.ProtectedFields) {
	return func(m *transcriptcrypto.ProtectedFields) { m.Content = leftRunes(m.Content, n) }
}

// GetMessageContentBatchRow is one message risk analysis scans, with the
// prior user request and recent tool result around it.
type GetMessageContentBatchRow = plainrepo.GetMessageContentBatchRow

// GetMessageContentBatch loads a batch of messages for risk analysis.
func (t *Transcripts) GetMessageContentBatch(ctx context.Context, db DBTX, projectID uuid.UUID, ids []uuid.UUID) ([]GetMessageContentBatchRow, error) {
	rows, err := plainrepo.New(db).GetMessageContentBatch(ctx, plainrepo.GetMessageContentBatchParams{OpenShadows: t.Enabled(projectID), Ids: ids, ProjectID: uuid.NullUUID{UUID: projectID, Valid: true}})
	if err != nil {
		return nil, fmt.Errorf("get message content batch: %w", err)
	}
	joined := make([]joinedRow, 0, len(rows)*3)
	for i := range rows {
		r := &rows[i]
		joined = append(joined, joinedRow{body: bodyRefs{MessageID: r.ID, Content: &r.Content, ContentRaw: nil, ToolCalls: &r.ToolCalls, ToolOutcomeNotes: nil, ToolCallSummaries: nil, StorageError: nil}, chatID: r.ChatID, ciphertext: r.ShadowCiphertext, current: r.ShadowCurrent, shape: nil})
		if r.PriorUserRequestID != uuid.Nil {
			joined = append(joined, joinedRow{body: bodyRefs{MessageID: r.PriorUserRequestID, Content: &r.PriorUserRequest, ContentRaw: nil, ToolCalls: nil, ToolOutcomeNotes: nil, ToolCallSummaries: nil, StorageError: nil}, chatID: r.ChatID, ciphertext: r.PriorShadowCiphertext, current: r.PriorShadowCurrent, shape: capContent(riskContextRunes)})
		}
		if r.RecentUntrustedContentID != uuid.Nil {
			joined = append(joined, joinedRow{body: bodyRefs{MessageID: r.RecentUntrustedContentID, Content: &r.RecentUntrustedContent, ContentRaw: nil, ToolCalls: nil, ToolOutcomeNotes: nil, ToolCallSummaries: nil, StorageError: nil}, chatID: r.ChatID, ciphertext: r.RecentShadowCiphertext, current: r.RecentShadowCurrent, shape: capContent(riskContextRunes)})
		}
	}
	if err := t.openJoined(ctx, db, projectID, joined); err != nil {
		return nil, err
	}
	return rows, nil
}

// GetChatMessageForUnmaskRow is the message a risk finding's reveal reads.
type GetChatMessageForUnmaskRow = plainrepo.GetChatMessageForUnmaskRow

// GetChatMessageForUnmask loads the message a risk finding points at.
func (t *Transcripts) GetChatMessageForUnmask(ctx context.Context, db DBTX, projectID, messageID uuid.UUID) (GetChatMessageForUnmaskRow, error) {
	row, err := plainrepo.New(db).GetChatMessageForUnmask(ctx, plainrepo.GetChatMessageForUnmaskParams{OpenShadows: t.Enabled(projectID), ID: messageID, ProjectID: uuid.NullUUID{UUID: projectID, Valid: true}})
	if err != nil {
		return GetChatMessageForUnmaskRow{}, fmt.Errorf("get chat message for unmask: %w", err)
	}
	joined := []joinedRow{{body: bodyRefs{MessageID: row.ID, Content: &row.Content, ContentRaw: nil, ToolCalls: &row.ToolCalls, ToolOutcomeNotes: nil, ToolCallSummaries: nil, StorageError: nil}, chatID: row.ChatID, ciphertext: row.ShadowCiphertext, current: row.ShadowCurrent, shape: nil}}
	if err := t.openJoined(ctx, db, projectID, joined); err != nil {
		// Never hand back the plaintext that failed the check.
		return GetChatMessageForUnmaskRow{}, err
	}
	return row, nil
}

// JudgeWindowParams selects the evidence around a judged message: the
// anchor and two neighbors on each side, or the latest four messages of the
// chat when AnchorID is uuid.Nil.
type JudgeWindowParams struct {
	AnchorID       uuid.UUID
	ChatID         uuid.UUID
	ProjectID      uuid.UUID
	OrganizationID string
}

// JudgeWindowMessage is one capped message of a judge's evidence window.
// Content keeps at most judgeContentRunes characters, and ToolCalls is a JSON
// array of at most judgeToolCalls calls with capped names and arguments.
type JudgeWindowMessage struct {
	ID        uuid.UUID
	Role      string
	Content   string
	ToolCalls string
}

// The caps keep one overflow rune or call so the renderer can show that
// evidence was truncated.
const (
	judgeContentRunes   = 4001
	judgeToolCalls      = 9
	judgeToolNameRunes  = 257
	judgeArgumentsRunes = 4001
)

// GetJudgeMessageWindow loads a judge's evidence window, oldest first.
func (t *Transcripts) GetJudgeMessageWindow(ctx context.Context, db DBTX, p JudgeWindowParams) ([]JudgeWindowMessage, error) {
	rows, err := plainrepo.New(db).GetJudgeMessageWindow(ctx, plainrepo.GetJudgeMessageWindowParams{OpenShadows: t.Enabled(p.ProjectID), AnchorID: p.AnchorID, ChatID: p.ChatID, ProjectID: p.ProjectID, OrganizationID: p.OrganizationID})
	if err != nil {
		return nil, fmt.Errorf("get judge message window: %w", err)
	}
	joined := make([]joinedRow, len(rows))
	for i := range rows {
		r := &rows[i]
		joined[i] = joinedRow{body: bodyRefs{MessageID: r.ID, Content: &r.Content, ContentRaw: nil, ToolCalls: &r.ToolCalls, ToolOutcomeNotes: nil, ToolCallSummaries: nil, StorageError: nil}, chatID: r.ChatID, ciphertext: r.ShadowCiphertext, current: r.ShadowCurrent, shape: nil}
	}
	if err := t.openJoined(ctx, db, p.ProjectID, joined); err != nil {
		return nil, err
	}
	out := make([]JudgeWindowMessage, len(rows))
	for i, r := range rows {
		out[i] = JudgeWindowMessage{ID: r.ID, Role: r.Role, Content: leftRunes(r.Content, judgeContentRunes), ToolCalls: judgeToolCallsOf(r.ToolCalls)}
	}
	return out, nil
}

type judgeFunction struct {
	Name      *string `json:"name"`
	Arguments *string `json:"arguments"`
}

type judgeCall struct {
	Function judgeFunction `json:"function"`
}

// judgeToolCallsOf keeps the first judgeToolCalls calls, each reduced to a
// capped function name and arguments. A double-encoded array is read too.
// Anything unreadable yields no calls.
func judgeToolCallsOf(raw []byte) string {
	value := bytes.TrimSpace(raw)
	var inner string
	if json.Unmarshal(value, &inner) == nil {
		value = []byte(inner)
	}
	var calls []json.RawMessage
	if err := json.Unmarshal(value, &calls); err != nil {
		var single map[string]json.RawMessage
		if json.Unmarshal(value, &single) != nil {
			return "[]"
		}
		calls = []json.RawMessage{value}
	}
	if len(calls) > judgeToolCalls {
		calls = calls[:judgeToolCalls]
	}
	out := make([]judgeCall, 0, len(calls))
	for _, raw := range calls {
		var call struct {
			Function map[string]json.RawMessage `json:"function"`
		}
		_ = json.Unmarshal(raw, &call)
		out = append(out, judgeCall{Function: judgeFunction{
			Name:      capText(call.Function["name"], judgeToolNameRunes),
			Arguments: capText(call.Function["arguments"], judgeArgumentsRunes),
		}})
	}
	if len(out) == 0 {
		return "[]"
	}
	encoded, err := json.Marshal(out)
	if err != nil {
		return "[]"
	}
	return string(encoded)
}

// capText reads a JSON value as text the way ->> does (a string's value, any
// other value's JSON) and keeps its first n characters. Missing or null
// yields nil.
func capText(raw json.RawMessage, n int) *string {
	if len(raw) == 0 || string(raw) == "null" {
		return nil
	}
	var text string
	if json.Unmarshal(raw, &text) != nil {
		text = string(raw)
	}
	text = leftRunes(text, n)
	return &text
}
