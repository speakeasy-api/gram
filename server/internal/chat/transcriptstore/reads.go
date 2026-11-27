package transcriptstore

import (
	"context"
	"fmt"

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

func chatMessageBody(m *chatrepo.ChatMessage) ProtectedFieldRefs {
	return ProtectedFieldRefs{MessageID: m.ID, Content: &m.Content, ContentRaw: &m.ContentRaw, ToolCalls: &m.ToolCalls, ToolOutcomeNotes: &m.ToolOutcomeNotes, ToolCallSummaries: &m.ToolCallSummaries, StorageError: &m.StorageError}
}

// unsetBody is a placeholder that chatMessages replaces before opening.
var unsetBody ProtectedFieldRefs

// chatMessages converts embedded rows to the chat repo model and opens them.
func (t *Transcripts) chatMessages(ctx context.Context, db DBTX, projectID uuid.UUID, reader string, n int, row func(int) (plainrepo.ChatMessage, joinedRow)) ([]chatrepo.ChatMessage, error) {
	messages := make([]chatrepo.ChatMessage, n)
	joined := make([]joinedRow, n)
	for i := range n {
		m, j := row(i)
		messages[i] = chatrepo.ChatMessage(m)
		j.body = chatMessageBody(&messages[i])
		j.chatID = messages[i].ChatID
		joined[i] = j
	}
	if err := t.openJoined(ctx, db, projectID, reader, joined); err != nil {
		return nil, err
	}
	return messages, nil
}

// ListLatestGenerationChatMessages returns a chat's latest-generation
// messages, oldest first.
func (t *Transcripts) ListLatestGenerationChatMessages(ctx context.Context, db DBTX, projectID, chatID uuid.UUID) ([]chatrepo.ChatMessage, error) {
	rows, err := plainrepo.New(db).ListLatestGenerationChatMessages(ctx, plainrepo.ListLatestGenerationChatMessagesParams{OpenShadows: t.Enabled(projectID), ChatID: chatID, ProjectID: projectID})
	if err != nil {
		return nil, fmt.Errorf("list latest generation chat messages: %w", err)
	}
	return t.chatMessages(ctx, db, projectID, "ListLatestGenerationChatMessages", len(rows), func(i int) (plainrepo.ChatMessage, joinedRow) {
		return rows[i].ChatMessage, joinedRow{body: unsetBody, chatID: uuid.Nil, ciphertext: rows[i].ShadowCiphertext, current: rows[i].ShadowCurrent, shape: nil}
	})
}

// ListChatMessagesByGeneration returns one pinned generation's messages,
// oldest first.
func (t *Transcripts) ListChatMessagesByGeneration(ctx context.Context, db DBTX, projectID, chatID uuid.UUID, generation int32) ([]chatrepo.ChatMessage, error) {
	rows, err := plainrepo.New(db).ListChatMessagesByGeneration(ctx, plainrepo.ListChatMessagesByGenerationParams{OpenShadows: t.Enabled(projectID), ChatID: chatID, ProjectID: projectID, Generation: generation})
	if err != nil {
		return nil, fmt.Errorf("list chat messages by generation: %w", err)
	}
	return t.chatMessages(ctx, db, projectID, "ListChatMessagesByGeneration", len(rows), func(i int) (plainrepo.ChatMessage, joinedRow) {
		return rows[i].ChatMessage, joinedRow{body: unsetBody, chatID: uuid.Nil, ciphertext: rows[i].ShadowCiphertext, current: rows[i].ShadowCurrent, shape: nil}
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
		return nil, fmt.Errorf("list chat messages after page: %w", err)
	}
	return t.chatMessages(ctx, db, page.ProjectID, "ListChatMessagesAfterPage", len(rows), func(i int) (plainrepo.ChatMessage, joinedRow) {
		return rows[i].ChatMessage, joinedRow{body: unsetBody, chatID: uuid.Nil, ciphertext: rows[i].ShadowCiphertext, current: rows[i].ShadowCurrent, shape: nil}
	})
}

// ListChatMessagesBeforePage returns messages before the cursor, newest first.
func (t *Transcripts) ListChatMessagesBeforePage(ctx context.Context, db DBTX, page ChatPage) ([]chatrepo.ChatMessage, error) {
	rows, err := plainrepo.New(db).ListChatMessagesBeforePage(ctx, plainrepo.ListChatMessagesBeforePageParams{OpenShadows: t.Enabled(page.ProjectID), ChatID: page.ChatID, ProjectID: page.ProjectID, Generation: page.Generation, BeforeSeq: page.Cursor, Lim: page.Limit})
	if err != nil {
		return nil, fmt.Errorf("list chat messages before page: %w", err)
	}
	return t.chatMessages(ctx, db, page.ProjectID, "ListChatMessagesBeforePage", len(rows), func(i int) (plainrepo.ChatMessage, joinedRow) {
		return rows[i].ChatMessage, joinedRow{body: unsetBody, chatID: uuid.Nil, ciphertext: rows[i].ShadowCiphertext, current: rows[i].ShadowCurrent, shape: nil}
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
		return nil, fmt.Errorf("list risk windowed messages: %w", err)
	}
	joined := make([]joinedRow, len(rows))
	for i := range rows {
		r := &rows[i]
		joined[i] = joinedRow{body: ProtectedFieldRefs{MessageID: r.ID, Content: &r.Content, ContentRaw: &r.ContentRaw, ToolCalls: &r.ToolCalls, ToolOutcomeNotes: &r.ToolOutcomeNotes, ToolCallSummaries: &r.ToolCallSummaries, StorageError: &r.StorageError}, chatID: r.ChatID, ciphertext: r.ShadowCiphertext, current: r.ShadowCurrent, shape: nil}
	}
	if err := t.openJoined(ctx, db, w.ProjectID, "ListRiskWindowedMessages", joined); err != nil {
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
		return nil, fmt.Errorf("list search windowed messages: %w", err)
	}
	joined := make([]joinedRow, len(rows))
	for i := range rows {
		r := &rows[i]
		joined[i] = joinedRow{body: ProtectedFieldRefs{MessageID: r.ID, Content: &r.Content, ContentRaw: &r.ContentRaw, ToolCalls: &r.ToolCalls, ToolOutcomeNotes: &r.ToolOutcomeNotes, ToolCallSummaries: &r.ToolCallSummaries, StorageError: &r.StorageError}, chatID: r.ChatID, ciphertext: r.ShadowCiphertext, current: r.ShadowCurrent, shape: nil}
	}
	if err := t.openJoined(ctx, db, w.ProjectID, "ListSearchWindowedMessages", joined); err != nil {
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
		return row, fmt.Errorf("get chat entry totals: %w", err)
	}
	return row, nil
}

// ListChatMessagesForMatchRow is one stored message the capture matcher
// compares an incoming transcript against.
type ListChatMessagesForMatchRow = plainrepo.ListChatMessagesForMatchRow

// ListChatMessagesForMatch returns a generation's messages for transcript
// matching, oldest first.
func (t *Transcripts) ListChatMessagesForMatch(ctx context.Context, db DBTX, projectID, chatID uuid.UUID, generation int32) ([]ListChatMessagesForMatchRow, error) {
	rows, err := plainrepo.New(db).ListChatMessagesForMatch(ctx, plainrepo.ListChatMessagesForMatchParams{OpenShadows: t.Enabled(projectID), ChatID: chatID, ProjectID: projectID, Generation: generation})
	if err != nil {
		return nil, fmt.Errorf("list chat messages for match: %w", err)
	}
	joined := make([]joinedRow, len(rows))
	for i := range rows {
		r := &rows[i]
		joined[i] = joinedRow{body: ProtectedFieldRefs{MessageID: r.ID, Content: &r.Content, ContentRaw: nil, ToolCalls: &r.ToolCalls, ToolOutcomeNotes: nil, ToolCallSummaries: nil, StorageError: nil}, chatID: chatID, ciphertext: r.ShadowCiphertext, current: r.ShadowCurrent, shape: nil}
	}
	if err := t.openJoined(ctx, db, projectID, "ListChatMessagesForMatch", joined); err != nil {
		return nil, err
	}
	return rows, nil
}

// GetKitProjectionMessage returns one message of a project, or
// pgx.ErrNoRows (wrapped) when it does not exist.
func (t *Transcripts) GetKitProjectionMessage(ctx context.Context, db DBTX, projectID, messageID uuid.UUID) (chatrepo.ChatMessage, error) {
	row, err := plainrepo.New(db).GetKitProjectionMessage(ctx, plainrepo.GetKitProjectionMessageParams{OpenShadows: t.Enabled(projectID), ProjectID: uuid.NullUUID{UUID: projectID, Valid: true}, ID: messageID})
	if err != nil {
		return chatrepo.ChatMessage{}, fmt.Errorf("get kit projection message: %w", err)
	}
	messages, err := t.chatMessages(ctx, db, projectID, "GetKitProjectionMessage", 1, func(int) (plainrepo.ChatMessage, joinedRow) {
		return row.ChatMessage, joinedRow{body: unsetBody, chatID: uuid.Nil, ciphertext: row.ShadowCiphertext, current: row.ShadowCurrent, shape: nil}
	})
	if err != nil {
		return chatrepo.ChatMessage{}, err
	}
	return messages[0], nil
}

// ToolCallSummaryContext selects an assistant message and one of its tool
// calls, by provider tool-call ID.
type ToolCallSummaryContext struct {
	ProjectID  uuid.UUID
	ChatID     uuid.UUID
	MessageID  uuid.UUID
	ToolCallID string
}

// GetToolCallSummaryContextRow is an assistant message's tool calls and
// summaries with the body of the first matching tool result.
type GetToolCallSummaryContextRow = plainrepo.GetToolCallSummaryContextRow

// GetToolCallSummaryContext loads what summarizing one tool call needs.
func (t *Transcripts) GetToolCallSummaryContext(ctx context.Context, db DBTX, c ToolCallSummaryContext) (GetToolCallSummaryContextRow, error) {
	row, err := plainrepo.New(db).GetToolCallSummaryContext(ctx, plainrepo.GetToolCallSummaryContextParams{OpenShadows: t.Enabled(c.ProjectID), ToolCallID: pgtype.Text{String: c.ToolCallID, Valid: true}, MessageID: c.MessageID, ChatID: c.ChatID, ProjectID: c.ProjectID})
	if err != nil {
		return GetToolCallSummaryContextRow{}, fmt.Errorf("get tool call summary context: %w", err)
	}
	joined := []joinedRow{
		{body: ProtectedFieldRefs{MessageID: c.MessageID, Content: nil, ContentRaw: nil, ToolCalls: &row.ToolCalls, ToolOutcomeNotes: nil, ToolCallSummaries: &row.ToolCallSummaries, StorageError: nil}, chatID: row.ChatID, ciphertext: row.CallsShadowCiphertext, current: row.CallsShadowCurrent, shape: nil},
		{body: ProtectedFieldRefs{MessageID: row.ResultMessageID, Content: &row.ResultContent, ContentRaw: nil, ToolCalls: nil, ToolOutcomeNotes: nil, ToolCallSummaries: nil, StorageError: nil}, chatID: row.ChatID, ciphertext: row.ResultShadowCiphertext, current: row.ResultShadowCurrent, shape: nil},
	}
	if err := t.openJoined(ctx, db, c.ProjectID, "GetToolCallSummaryContext", joined); err != nil {
		// Never hand back the plaintext that failed the check.
		return GetToolCallSummaryContextRow{}, err
	}
	return row, nil
}

// ListClaudeUserMessagesForPromptAttachmentParentRow is one non-empty user
// prompt of a chat.
type ListClaudeUserMessagesForPromptAttachmentParentRow = plainrepo.ListClaudeUserMessagesForPromptAttachmentParentRow

// ListClaudeUserMessagesForPromptAttachmentParent returns a chat's non-empty
// user prompts, newest first.
func (t *Transcripts) ListClaudeUserMessagesForPromptAttachmentParent(ctx context.Context, db DBTX, projectID, chatID uuid.UUID) ([]ListClaudeUserMessagesForPromptAttachmentParentRow, error) {
	rows, err := plainrepo.New(db).ListClaudeUserMessagesForPromptAttachmentParent(ctx, plainrepo.ListClaudeUserMessagesForPromptAttachmentParentParams{OpenShadows: t.Enabled(projectID), ChatID: chatID, ProjectID: projectID})
	if err != nil {
		return nil, fmt.Errorf("list claude user messages for prompt attachment parent: %w", err)
	}
	joined := make([]joinedRow, len(rows))
	for i := range rows {
		r := &rows[i]
		joined[i] = joinedRow{body: ProtectedFieldRefs{MessageID: r.ID, Content: &r.Content, ContentRaw: nil, ToolCalls: nil, ToolOutcomeNotes: nil, ToolCallSummaries: nil, StorageError: nil}, chatID: chatID, ciphertext: r.ShadowCiphertext, current: r.ShadowCurrent, shape: nil}
	}
	if err := t.openJoined(ctx, db, projectID, "ListClaudeUserMessagesForPromptAttachmentParent", joined); err != nil {
		return nil, err
	}
	return rows, nil
}

// TranscriptPage selects one keyset page of a chat, newest first. A NULL
// CursorCreatedAt returns the newest page.
type TranscriptPage struct {
	ProjectID       uuid.UUID
	ChatID          uuid.UUID
	CursorCreatedAt pgtype.Timestamptz
	CursorSeq       pgtype.Int8
	CursorID        uuid.NullUUID
	Limit           int32
}

// ListChatTranscriptMessagesPageRow is one message of a transcript page.
type ListChatTranscriptMessagesPageRow = plainrepo.ListChatTranscriptMessagesPageRow

// ListChatTranscriptMessagesPage returns one page of a chat, newest first.
func (t *Transcripts) ListChatTranscriptMessagesPage(ctx context.Context, db DBTX, page TranscriptPage) ([]ListChatTranscriptMessagesPageRow, error) {
	rows, err := plainrepo.New(db).ListChatTranscriptMessagesPage(ctx, plainrepo.ListChatTranscriptMessagesPageParams{OpenShadows: t.Enabled(page.ProjectID), ChatID: page.ChatID, ProjectID: page.ProjectID, CursorCreatedAt: page.CursorCreatedAt, CursorSeq: page.CursorSeq, CursorID: page.CursorID, Lim: page.Limit})
	if err != nil {
		return nil, fmt.Errorf("list chat transcript messages page: %w", err)
	}
	joined := make([]joinedRow, len(rows))
	for i := range rows {
		r := &rows[i]
		joined[i] = joinedRow{body: ProtectedFieldRefs{MessageID: r.ID, Content: &r.Content, ContentRaw: nil, ToolCalls: &r.ToolCalls, ToolOutcomeNotes: &r.ToolOutcomeNotes, ToolCallSummaries: nil, StorageError: nil}, chatID: page.ChatID, ciphertext: r.ShadowCiphertext, current: r.ShadowCurrent, shape: nil}
	}
	if err := t.openJoined(ctx, db, page.ProjectID, "ListChatTranscriptMessagesPage", joined); err != nil {
		return nil, err
	}
	return rows, nil
}

// TranscriptPages reads transcript pages through Transcripts over one
// database handle, for readers that take the page reader as an interface.
type TranscriptPages struct {
	transcripts *Transcripts
	db          DBTX
}

// Pages binds the transcript page reader to db.
func (t *Transcripts) Pages(db DBTX) TranscriptPages {
	return TranscriptPages{transcripts: t, db: db}
}

// ListChatTranscriptMessagesPage returns one page of a chat, newest first.
func (p TranscriptPages) ListChatTranscriptMessagesPage(ctx context.Context, page TranscriptPage) ([]ListChatTranscriptMessagesPageRow, error) {
	return p.transcripts.ListChatTranscriptMessagesPage(ctx, p.db, page)
}

// FindAssistantToolCallMessageID returns the newest assistant message of a
// chat that issued the tool call, or pgx.ErrNoRows (wrapped).
func (t *Transcripts) FindAssistantToolCallMessageID(ctx context.Context, db DBTX, projectID, chatID uuid.UUID, toolCallID string) (uuid.UUID, error) {
	id, err := plainrepo.New(db).FindAssistantToolCallMessageID(ctx, plainrepo.FindAssistantToolCallMessageIDParams{ProjectID: uuid.NullUUID{UUID: projectID, Valid: true}, ChatID: chatID, ToolCallID: toolCallID})
	if err != nil {
		return uuid.Nil, fmt.Errorf("find assistant tool call message: %w", err)
	}
	return id, nil
}
