package transcriptstore

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	chatrepo "github.com/speakeasy-api/gram/server/internal/chat/repo"
	"github.com/speakeasy-api/gram/server/internal/chat/transcriptstore/internal/plainrepo"
	"github.com/speakeasy-api/gram/server/internal/chat/transcriptstore/repo"
)

// Writes that change a message body after its insert, or that return a
// stored body, live here too. Plain inserts stay with the chat message
// writer, which captures shadows in the same transaction.

// UpsertCorrelatedChatMessageParams is one correlated message to insert or
// promote. OpenShadows is set by the method; leave it false.
type UpsertCorrelatedChatMessageParams = plainrepo.UpsertCorrelatedChatMessageParams

// UpsertCorrelatedChatMessageRow is the stored message after the upsert.
type UpsertCorrelatedChatMessageRow = plainrepo.UpsertCorrelatedChatMessageRow

// UpsertCorrelatedChatMessage inserts a correlated message or promotes the
// stored one, then captures its shadow in the caller's transaction. For a
// project that keeps shadows the returned body is read from that shadow, not
// returned from the plaintext columns.
func (t *Transcripts) UpsertCorrelatedChatMessage(ctx context.Context, db DBTX, organizationID string, params UpsertCorrelatedChatMessageParams) (UpsertCorrelatedChatMessageRow, error) {
	params.OpenShadows = t.Enabled(params.ProjectID)
	q := plainrepo.New(db)
	row, err := q.UpsertCorrelatedChatMessage(ctx, params)
	if err != nil {
		return row, fmt.Errorf("upsert correlated chat message: %w", err)
	}
	if err := t.Capture(ctx, db, organizationID, params.ProjectID, []uuid.UUID{row.ID}); err != nil {
		return row, err
	}
	if !params.OpenShadows {
		return row, nil
	}
	body, err := q.GetCorrelatedChatMessageBody(ctx, plainrepo.GetCorrelatedChatMessageBodyParams{OpenShadows: true, ProjectID: params.ProjectID, ID: row.ID})
	if err != nil {
		return row, fmt.Errorf("read correlated chat message body: %w", err)
	}
	joined := []joinedRow{{body: Body{MessageID: row.ID, Content: &body.Content, ContentRaw: nil, ToolCalls: &body.ToolCalls, ToolOutcomeNotes: nil, ToolCallSummaries: nil, StorageError: nil}, chatID: body.ChatID, ciphertext: body.ShadowCiphertext, current: body.ShadowCurrent, shape: nil}}
	if err := t.openJoined(ctx, db, params.ProjectID, joined); err != nil {
		return row, err
	}
	row.Content = body.Content
	row.ToolCalls = body.ToolCalls
	return row, nil
}

// PatchKitProjectionMessageParams is the new body of a projected message.
// BodyChanged is set by the method; leave it false.
type PatchKitProjectionMessageParams = plainrepo.PatchKitProjectionMessageParams

// PatchKitProjectionMessage rewrites a projected message when its body or
// finish reason changed, and returns how many rows changed. previous is the
// stored message the caller loaded and locked with GetKitProjectionMessage in
// the same transaction. The body is compared with it in Go, so the write never
// compares against plaintext, and risk analysis is reset only when the body
// changed. The caller captures the shadow in the same transaction.
func (t *Transcripts) PatchKitProjectionMessage(ctx context.Context, db DBTX, previous chatrepo.ChatMessage, params PatchKitProjectionMessageParams) (int64, error) {
	params.BodyChanged = previous.Content != params.Content || !sameJSON(previous.ContentRaw, params.ContentRaw) || !sameJSON(previous.ToolCalls, params.ToolCalls)
	n, err := plainrepo.New(db).PatchKitProjectionMessage(ctx, params)
	if err != nil {
		return 0, fmt.Errorf("patch projected chat message: %w", err)
	}
	return n, nil
}

// StoreToolCallSummaryParams is one tool call's summary record.
type StoreToolCallSummaryParams = plainrepo.StoreToolCallSummaryParams

// StoreToolCallSummary merges one tool call's summary into its message and
// returns the stored record, keeping the summaries of sibling calls.
// tool_call_summaries is part of the shadow, so for a project that keeps
// shadows the merge happens in Go on the opened summaries, under a row lock,
// and the write and the recapture commit together.
func (t *Transcripts) StoreToolCallSummary(ctx context.Context, db DBTX, params StoreToolCallSummaryParams) (string, error) {
	if !t.Enabled(params.ProjectID) {
		stored, err := plainrepo.New(db).StoreToolCallSummary(ctx, params)
		if err != nil {
			return "", fmt.Errorf("store tool call summary: %w", err)
		}
		return stored, nil
	}
	var stored string
	err := t.withCapture(ctx, db, params.ProjectID, params.MessageID, func(db DBTX) error {
		var err error
		stored, err = t.mergeToolCallSummary(ctx, db, params)
		return err
	})
	return stored, err
}

// mergeToolCallSummary locks the message, opens its summaries, sets one
// call's record and writes the whole value back. It runs inside withCapture's
// transaction, so a concurrent summary for a sibling call waits on the lock
// and then merges into this one instead of overwriting it.
func (t *Transcripts) mergeToolCallSummary(ctx context.Context, db DBTX, params StoreToolCallSummaryParams) (string, error) {
	if !json.Valid(params.Summary) {
		return "", fmt.Errorf("store tool call summary: %w", ErrInvalid)
	}
	if err := repo.New(db).LockChatMessage(ctx, repo.LockChatMessageParams{ProjectID: params.ProjectID, ID: params.MessageID}); err != nil {
		return "", fmt.Errorf("lock tool call summaries: %w", err)
	}
	q := plainrepo.New(db)
	row, err := q.GetToolCallSummaries(ctx, plainrepo.GetToolCallSummariesParams{OpenShadows: true, MessageID: params.MessageID, ChatID: params.ChatID, ProjectID: params.ProjectID})
	if err != nil {
		return "", fmt.Errorf("read tool call summaries: %w", err)
	}
	joined := []joinedRow{{body: Body{MessageID: params.MessageID, Content: nil, ContentRaw: nil, ToolCalls: nil, ToolOutcomeNotes: nil, ToolCallSummaries: &row.ToolCallSummaries, StorageError: nil}, chatID: params.ChatID, ciphertext: row.ShadowCiphertext, current: row.ShadowCurrent, shape: nil}}
	if err := t.openJoined(ctx, db, params.ProjectID, joined); err != nil {
		return "", err
	}
	summaries := map[string]json.RawMessage{}
	if len(bytes.TrimSpace(row.ToolCallSummaries)) > 0 {
		if err := json.Unmarshal(row.ToolCallSummaries, &summaries); err != nil {
			return "", fmt.Errorf("decode tool call summaries: %w", err)
		}
	}
	if summaries == nil {
		// A stored JSON null decodes to a nil map.
		summaries = map[string]json.RawMessage{}
	}
	summaries[params.ToolCallID] = json.RawMessage(params.Summary)
	merged, err := json.Marshal(summaries)
	if err != nil {
		return "", fmt.Errorf("encode tool call summaries: %w", err)
	}
	if _, err := q.SetToolCallSummaries(ctx, plainrepo.SetToolCallSummariesParams{ToolCallSummaries: merged, MessageID: params.MessageID, ChatID: params.ChatID, ProjectID: params.ProjectID}); err != nil {
		return "", fmt.Errorf("store tool call summaries: %w", err)
	}
	return string(params.Summary), nil
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
		return t.Capture(ctx, db, organizationID, projectID, []uuid.UUID{messageID})
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
