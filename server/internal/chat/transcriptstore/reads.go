package transcriptstore

import (
	"context"

	"github.com/google/uuid"

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
