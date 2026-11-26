package transcriptstore

import (
	"context"
	"fmt"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/speakeasy-api/gram/server/internal/chat/transcriptstore/internal/plainrepo"
)

// Writes that change a message body after its insert, or that return a
// stored body, live here too. Plain inserts stay with the chat message
// writer, which captures shadows in the same transaction.

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

// PatchKitProjectionMessageParams is the new body of a projected message.
type PatchKitProjectionMessageParams = plainrepo.PatchKitProjectionMessageParams

// PatchKitProjectionMessage rewrites a projected message when its body
// changed, and returns how many rows changed. The caller captures the shadow
// in the same transaction.
func (t *Transcripts) PatchKitProjectionMessage(ctx context.Context, db DBTX, params PatchKitProjectionMessageParams) (int64, error) {
	n, err := plainrepo.New(db).PatchKitProjectionMessage(ctx, params)
	if err != nil {
		return 0, fmt.Errorf("patch projected chat message: %w", err)
	}
	return n, nil
}

// StoreToolCallSummaryParams is one tool call's summary record.
type StoreToolCallSummaryParams = plainrepo.StoreToolCallSummaryParams

// StoreToolCallSummary merges one tool call's summary into its message and
// returns the stored record. tool_call_summaries is part of the shadow, so
// for a project that keeps shadows the write and the recapture commit
// together.
func (t *Transcripts) StoreToolCallSummary(ctx context.Context, db DBTX, params StoreToolCallSummaryParams) (string, error) {
	var stored string
	err := t.withCapture(ctx, db, params.ProjectID, params.MessageID, func(db DBTX) error {
		var err error
		stored, err = plainrepo.New(db).StoreToolCallSummary(ctx, params)
		if err != nil {
			return fmt.Errorf("store tool call summary: %w", err)
		}
		return nil
	})
	return stored, err
}

// UpdateToolCallOutcomeParams is a tool call's judged outcome.
type UpdateToolCallOutcomeParams = plainrepo.UpdateToolCallOutcomeParams

// UpdateToolCallOutcome records a tool call's outcome. tool_outcome_notes is
// part of the shadow, so for a project that keeps shadows the write and the
// recapture commit together.
func (t *Transcripts) UpdateToolCallOutcome(ctx context.Context, db DBTX, params UpdateToolCallOutcomeParams) error {
	return t.withCapture(ctx, db, params.ProjectID, params.ID, func(db DBTX) error {
		if err := plainrepo.New(db).UpdateToolCallOutcome(ctx, params); err != nil {
			return fmt.Errorf("update tool call outcome: %w", err)
		}
		return nil
	})
}

type beginner interface {
	Begin(ctx context.Context) (pgx.Tx, error)
}

// withCapture runs a write that changes a shadowed field, then recaptures
// the message's shadow, as one unit. On a pool or connection it opens a
// transaction. Inside a caller's transaction Begin opens a savepoint, so a
// failed write or recapture rolls back both and leaves the caller's
// transaction usable: a caller that logs the error and continues never
// commits the write without its recapture. It adds nothing for a project
// that does not keep shadows.
func (t *Transcripts) withCapture(ctx context.Context, db DBTX, projectID, messageID uuid.UUID, write func(DBTX) error) error {
	if !t.Enabled(projectID) {
		return write(db)
	}
	capture := func(db DBTX) error {
		if err := write(db); err != nil {
			return err
		}
		organizationID, err := t.organization(ctx, db, projectID)
		if err != nil {
			return err
		}
		return t.Seal(ctx, db, organizationID, projectID, []uuid.UUID{messageID})
	}
	b, ok := db.(beginner)
	if !ok {
		return capture(db)
	}
	tx, err := b.Begin(ctx)
	if err != nil {
		return fmt.Errorf("begin transcript write: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	if err := capture(tx); err != nil {
		return err
	}
	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("commit transcript write: %w", err)
	}
	return nil
}
