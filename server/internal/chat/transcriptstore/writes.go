package transcriptstore

import (
	"context"
	"fmt"

	"github.com/speakeasy-api/gram/server/internal/chat/transcriptstore/internal/plainrepo"
)

// Writes that return a stored body live here too. Every change to the body of
// an existing message goes through UpdateChatMessage. Plain inserts stay with the
// chat message writer, which seals in the same transaction.

// UpsertCorrelatedChatMessageParams is one correlated message to insert or
// promote. OpenShadows is set by the method; leave it false.
type UpsertCorrelatedChatMessageParams = plainrepo.UpsertCorrelatedChatMessageParams

// UpsertCorrelatedChatMessageRow is the stored message after the upsert.
type UpsertCorrelatedChatMessageRow = plainrepo.UpsertCorrelatedChatMessageRow

// UpsertCorrelatedChatMessage inserts a correlated message or promotes the
// stored one. The chat message writer calls it inside its insert path, which
// seals an inserted message in the same transaction. For a project that keeps
// transcripts the body is never returned from the plaintext columns: a
// promoted message's body is read from its transcript, and an inserted one
// returns no body, since the caller already holds what it wrote.
func (t *Transcripts) UpsertCorrelatedChatMessage(ctx context.Context, db DBTX, params UpsertCorrelatedChatMessageParams) (UpsertCorrelatedChatMessageRow, error) {
	params.OpenShadows = t.Enabled(params.ProjectID)
	q := plainrepo.New(db)
	row, err := q.UpsertCorrelatedChatMessage(ctx, params)
	if err != nil {
		return row, fmt.Errorf("upsert correlated chat message: %w", err)
	}
	if !params.OpenShadows || row.Inserted {
		return row, nil
	}
	body, err := q.GetCorrelatedChatMessageBody(ctx, plainrepo.GetCorrelatedChatMessageBodyParams{OpenShadows: true, ProjectID: params.ProjectID, ID: row.ID})
	if err != nil {
		return row, fmt.Errorf("read correlated chat message body: %w", err)
	}
	joined := []joinedRow{{body: ProtectedFieldRefs{MessageID: row.ID, Content: &body.Content, ContentRaw: nil, ToolCalls: &body.ToolCalls, ToolOutcomeNotes: nil, ToolCallSummaries: nil, StorageError: nil}, chatID: body.ChatID, ciphertext: body.ShadowCiphertext, current: body.ShadowCurrent, shape: nil}}
	if err := t.openJoined(ctx, db, params.ProjectID, joined); err != nil {
		return row, err
	}
	row.Content = body.Content
	row.ToolCalls = body.ToolCalls
	return row, nil
}
