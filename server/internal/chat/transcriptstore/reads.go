package transcriptstore

import (
	"context"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"

	chatrepo "github.com/speakeasy-api/gram/server/internal/chat/repo"
	"github.com/speakeasy-api/gram/server/internal/chat/transcriptstore/internal/plainrepo"
)

// DBTX is what every read accepts: a pool, a connection or a transaction.
type DBTX = plainrepo.DBTX

// The methods below are the only way to read the plaintext transcript columns
// of chat_messages. Each one runs its query from internal/plainrepo, then opens
// the shadows that came back with it for projects that keep them. A nil
// *Transcripts reads plaintext for every project.

func chatMessageBody(m *chatrepo.ChatMessage) Body {
	return Body{MessageID: m.ID, Content: &m.Content, ContentRaw: &m.ContentRaw, ToolCalls: &m.ToolCalls, ToolOutcomeNotes: &m.ToolOutcomeNotes, ToolCallSummaries: &m.ToolCallSummaries, StorageError: &m.StorageError}
}

// chatMessages converts embedded rows to the chat repo model and opens them.
func (t *Transcripts) chatMessages(ctx context.Context, db DBTX, projectID uuid.UUID, n int, row func(int) (plainrepo.ChatMessage, joinedRow)) ([]chatrepo.ChatMessage, error) {
	messages := make([]chatrepo.ChatMessage, n)
	joined := make([]joinedRow, n)
	for i := range n {
		m, j := row(i)
		messages[i] = chatrepo.ChatMessage(m)
		j.body = chatMessageBody(&messages[i])
		j.chatID = messages[i].ChatID
		joined[i] = j
	}
	if err := t.openJoined(ctx, db, projectID, joined); err != nil {
		return nil, err
	}
	return messages, nil
}

// ListLatestGenerationChatMessages returns a chat's latest-generation
// messages, oldest first.
func (t *Transcripts) ListLatestGenerationChatMessages(ctx context.Context, db DBTX, projectID, chatID uuid.UUID) ([]chatrepo.ChatMessage, error) {
	rows, err := plainrepo.New(db).ListLatestGenerationChatMessages(ctx, plainrepo.ListLatestGenerationChatMessagesParams{OpenShadows: t.Enabled(projectID), ChatID: chatID, ProjectID: projectID})
	if err != nil {
		return nil, safe(err)
	}
	return t.chatMessages(ctx, db, projectID, len(rows), func(i int) (plainrepo.ChatMessage, joinedRow) {
		return rows[i].ChatMessage, joinedRow{ciphertext: rows[i].ShadowCiphertext, current: rows[i].ShadowCurrent}
	})
}

// ListChatMessagesByGeneration returns one pinned generation's messages,
// oldest first.
func (t *Transcripts) ListChatMessagesByGeneration(ctx context.Context, db DBTX, projectID, chatID uuid.UUID, generation int32) ([]chatrepo.ChatMessage, error) {
	rows, err := plainrepo.New(db).ListChatMessagesByGeneration(ctx, plainrepo.ListChatMessagesByGenerationParams{OpenShadows: t.Enabled(projectID), ChatID: chatID, ProjectID: projectID, Generation: generation})
	if err != nil {
		return nil, safe(err)
	}
	return t.chatMessages(ctx, db, projectID, len(rows), func(i int) (plainrepo.ChatMessage, joinedRow) {
		return rows[i].ChatMessage, joinedRow{ciphertext: rows[i].ShadowCiphertext, current: rows[i].ShadowCurrent}
	})
}

// ChatPage selects one keyset page of a chat generation for
// ListChatMessagesAfterPage and ListChatMessagesBeforePage. A NULL Cursor
// returns the oldest (after) or newest (before) page.
type ChatPage struct {
	ProjectID  uuid.UUID
	ChatID     uuid.UUID
	Generation int32
	Cursor     pgtype.Int8
	Limit      int32
}

// ListChatMessagesAfterPage returns messages after the cursor, oldest first.
func (t *Transcripts) ListChatMessagesAfterPage(ctx context.Context, db DBTX, page ChatPage) ([]chatrepo.ChatMessage, error) {
	rows, err := plainrepo.New(db).ListChatMessagesAfterPage(ctx, plainrepo.ListChatMessagesAfterPageParams{OpenShadows: t.Enabled(page.ProjectID), ChatID: page.ChatID, ProjectID: page.ProjectID, Generation: page.Generation, AfterSeq: page.Cursor, Lim: page.Limit})
	if err != nil {
		return nil, safe(err)
	}
	return t.chatMessages(ctx, db, page.ProjectID, len(rows), func(i int) (plainrepo.ChatMessage, joinedRow) {
		return rows[i].ChatMessage, joinedRow{ciphertext: rows[i].ShadowCiphertext, current: rows[i].ShadowCurrent}
	})
}

// ListChatMessagesBeforePage returns messages before the cursor, newest first.
func (t *Transcripts) ListChatMessagesBeforePage(ctx context.Context, db DBTX, page ChatPage) ([]chatrepo.ChatMessage, error) {
	rows, err := plainrepo.New(db).ListChatMessagesBeforePage(ctx, plainrepo.ListChatMessagesBeforePageParams{OpenShadows: t.Enabled(page.ProjectID), ChatID: page.ChatID, ProjectID: page.ProjectID, Generation: page.Generation, BeforeSeq: page.Cursor, Lim: page.Limit})
	if err != nil {
		return nil, safe(err)
	}
	return t.chatMessages(ctx, db, page.ProjectID, len(rows), func(i int) (plainrepo.ChatMessage, joinedRow) {
		return rows[i].ChatMessage, joinedRow{ciphertext: rows[i].ShadowCiphertext, current: rows[i].ShadowCurrent}
	})
}

// ListRiskWindowedMessagesRow is one message of the risk-only chat view.
type ListRiskWindowedMessagesRow = plainrepo.ListRiskWindowedMessagesRow

// RiskWindow selects the risk-only view: every message within ContextSize
// positions of a flagged message or of a prompt whose attachment is flagged.
type RiskWindow struct {
	ProjectID          uuid.UUID
	ChatID             uuid.UUID
	Generation         int32
	ContextSize        int64
	RiskMessageIDs     []uuid.UUID
	RiskContentPartIDs []uuid.UUID
}

// ListRiskWindowedMessages returns the risk-only view, oldest first.
func (t *Transcripts) ListRiskWindowedMessages(ctx context.Context, db DBTX, w RiskWindow) ([]ListRiskWindowedMessagesRow, error) {
	rows, err := plainrepo.New(db).ListRiskWindowedMessages(ctx, plainrepo.ListRiskWindowedMessagesParams{OpenShadows: t.Enabled(w.ProjectID), ContextSize: w.ContextSize, ChatID: w.ChatID, ProjectID: w.ProjectID, Generation: w.Generation, RiskContentPartIds: w.RiskContentPartIDs, RiskMessageIds: w.RiskMessageIDs})
	if err != nil {
		return nil, safe(err)
	}
	joined := make([]joinedRow, len(rows))
	for i := range rows {
		r := &rows[i]
		joined[i] = joinedRow{body: Body{MessageID: r.ID, Content: &r.Content, ContentRaw: &r.ContentRaw, ToolCalls: &r.ToolCalls, ToolOutcomeNotes: &r.ToolOutcomeNotes, ToolCallSummaries: &r.ToolCallSummaries, StorageError: &r.StorageError}, chatID: r.ChatID, ciphertext: r.ShadowCiphertext, current: r.ShadowCurrent, shape: nil}
	}
	if err := t.openJoined(ctx, db, w.ProjectID, joined); err != nil {
		return nil, err
	}
	return rows, nil
}

// ListSearchWindowedMessagesRow is one message of the chat search view.
type ListSearchWindowedMessagesRow = plainrepo.ListSearchWindowedMessagesRow

// SearchWindow selects the search view: messages whose text matches Query,
// up to MatchLimit matches, each with ContextSize messages around it.
type SearchWindow struct {
	ProjectID   uuid.UUID
	ChatID      uuid.UUID
	Generation  int32
	Query       string
	MatchLimit  int32
	ContextSize int64
}

// ListSearchWindowedMessages returns the search view, oldest first.
func (t *Transcripts) ListSearchWindowedMessages(ctx context.Context, db DBTX, w SearchWindow) ([]ListSearchWindowedMessagesRow, error) {
	rows, err := plainrepo.New(db).ListSearchWindowedMessages(ctx, plainrepo.ListSearchWindowedMessagesParams{OpenShadows: t.Enabled(w.ProjectID), ContextSize: w.ContextSize, ChatID: w.ChatID, ProjectID: w.ProjectID, Generation: w.Generation, Query: w.Query, MatchLimit: w.MatchLimit})
	if err != nil {
		return nil, safe(err)
	}
	joined := make([]joinedRow, len(rows))
	for i := range rows {
		r := &rows[i]
		joined[i] = joinedRow{body: Body{MessageID: r.ID, Content: &r.Content, ContentRaw: &r.ContentRaw, ToolCalls: &r.ToolCalls, ToolOutcomeNotes: &r.ToolOutcomeNotes, ToolCallSummaries: &r.ToolCallSummaries, StorageError: &r.StorageError}, chatID: r.ChatID, ciphertext: r.ShadowCiphertext, current: r.ShadowCurrent, shape: nil}
	}
	if err := t.openJoined(ctx, db, w.ProjectID, joined); err != nil {
		return nil, err
	}
	return rows, nil
}

// GetChatEntryTotalsParams selects a generation and its risk anchors.
type GetChatEntryTotalsParams = plainrepo.GetChatEntryTotalsParams

// GetChatEntryTotalsRow counts a generation's trace entries by kind.
type GetChatEntryTotalsRow = plainrepo.GetChatEntryTotalsRow

// GetChatEntryTotals counts a generation's trace entries. It returns no
// message body.
func (t *Transcripts) GetChatEntryTotals(ctx context.Context, db DBTX, params GetChatEntryTotalsParams) (GetChatEntryTotalsRow, error) {
	row, err := plainrepo.New(db).GetChatEntryTotals(ctx, params)
	if err != nil {
		return row, safe(err)
	}
	return row, nil
}
