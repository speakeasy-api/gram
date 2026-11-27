package transcriptstore

import (
	"context"
	"fmt"

	"github.com/google/uuid"

	"github.com/speakeasy-api/gram/server/internal/chat/transcriptstore/internal/plainrepo"
)

// UpsertCorrelatedChatMessageParams is one correlated message to insert or
// promote with UpsertCorrelatedChatMessage. Its shape comes from the
// internal/plainrepo query of the same name.
type UpsertCorrelatedChatMessageParams = plainrepo.UpsertCorrelatedChatMessageParams

// UpsertCorrelatedChatMessageRow is the stored message after the upsert.
type UpsertCorrelatedChatMessageRow = plainrepo.UpsertCorrelatedChatMessageRow

// openCorrelatedBody sets a promoted message's content and tool calls from its
// transcript, for a project that keeps transcripts, whose upsert returns no
// body from the plaintext columns.
func (t *Transcripts) openCorrelatedBody(ctx context.Context, db DBTX, projectID uuid.UUID, row *UpsertCorrelatedChatMessageRow) error {
	body, err := plainrepo.New(db).GetCorrelatedChatMessageBody(ctx, plainrepo.GetCorrelatedChatMessageBodyParams{OpenShadows: true, ProjectID: projectID, ID: row.ID})
	if err != nil {
		return fmt.Errorf("read correlated chat message body: %w", err)
	}
	joined := []joinedRow{{body: bodyRefs{MessageID: row.ID, Content: &body.Content, ContentRaw: nil, ToolCalls: &body.ToolCalls, ToolOutcomeNotes: nil, ToolCallSummaries: nil, StorageError: nil}, chatID: body.ChatID, ciphertext: body.ShadowCiphertext, current: body.ShadowCurrent, shape: nil}}
	if err := t.openJoined(ctx, db, projectID, joined); err != nil {
		return err
	}
	row.Content = body.Content
	row.ToolCalls = body.ToolCalls
	return nil
}
