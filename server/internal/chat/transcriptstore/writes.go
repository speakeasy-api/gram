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
// promote.
type UpsertCorrelatedChatMessageParams = plainrepo.UpsertCorrelatedChatMessageParams

// UpsertCorrelatedChatMessageRow is the stored message after the upsert.
type UpsertCorrelatedChatMessageRow = plainrepo.UpsertCorrelatedChatMessageRow

// UpsertCorrelatedChatMessage inserts a correlated message or promotes the
// stored one. The caller captures the shadow in the same transaction.
func (t *Transcripts) UpsertCorrelatedChatMessage(ctx context.Context, db DBTX, params UpsertCorrelatedChatMessageParams) (UpsertCorrelatedChatMessageRow, error) {
	row, err := plainrepo.New(db).UpsertCorrelatedChatMessage(ctx, params)
	if err != nil {
		return row, fmt.Errorf("upsert correlated chat message: %w", err)
	}
	return row, nil
}
