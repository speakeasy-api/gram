package chat

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/speakeasy-api/gram/server/internal/chat/repo"
	"github.com/speakeasy-api/gram/server/internal/chat/transcriptstore"
	"github.com/speakeasy-api/gram/server/internal/metering"
)

// ProjectMessage applies one canonical message patch under the producer's
// transaction-bound admission fence. Only first insertion emits storage usage
// and conversation-created events; updates retain the row ID and creation time.
// The guard must lock and validate producer ownership before build runs.
func (w *ChatMessageWriter) ProjectMessage(ctx context.Context, projectID, messageID uuid.UUID,
	guard func(context.Context, pgx.Tx) error,
	build func(*repo.ChatMessage) (MessageWrite, bool, error),
) (bool, error) {
	if guard == nil || build == nil || messageID == uuid.Nil {
		return false, fmt.Errorf("projection requires admission guard and message ID")
	}
	// A projection the guard refuses or build skips seals nothing, so it
	// must not fail on the key. Keep the error for a change that seals.
	prepared := w.PrepareTranscripts(ctx, projectID)
	tx, err := w.db.Begin(ctx)
	if err != nil {
		return false, fmt.Errorf("begin message projection: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	if err := guard(ctx, tx); err != nil {
		return false, fmt.Errorf("admit message projection: %w", err)
	}
	q := repo.New(tx)
	row, err := q.GetKitProjectionMessage(ctx, repo.GetKitProjectionMessageParams{ProjectID: uuid.NullUUID{UUID: projectID, Valid: true}, ID: messageID})
	exists := err == nil
	if err != nil && !errors.Is(err, pgx.ErrNoRows) {
		return false, fmt.Errorf("load projected message: %w", err)
	}
	var previous *repo.ChatMessage
	if exists {
		previous = &row
	}
	write, apply, err := build(previous)
	if err != nil {
		return false, fmt.Errorf("build message projection: %w", err)
	}
	if !apply {
		if err := tx.Commit(ctx); err != nil {
			return false, fmt.Errorf("commit unchanged message projection: %w", err)
		}
		return false, nil
	}
	if write.Params.ProjectID != projectID || write.Params.ID != messageID || (!write.Params.Source.Valid || write.Params.Source.String != "kit") {
		return false, fmt.Errorf("invalid projection ownership")
	}
	if exists && (row.Source.String != "kit" || row.ChatID != write.Params.ChatID || row.Role != write.Params.Role) {
		return false, fmt.Errorf("projection cannot replace another producer's message")
	}
	if err := requireChatProject(ctx, tx, write.Params.ChatID, projectID); err != nil {
		return false, fmt.Errorf("validate projection chat project: %w", err)
	}
	now := time.Now().UTC()
	writes := []MessageWrite{write}
	if err := stampMessageFields(writes, now); err != nil {
		return false, fmt.Errorf("stamp projected message: %w", err)
	}
	write = writes[0]
	var changed int64
	var organizationID string
	if exists {
		changed, err = w.patchProjection(ctx, tx, projectID, row, write, prepared)
	} else {
		changed, organizationID, err = w.insertProjection(ctx, tx, projectID, write.Params, prepared)
		if err == nil && changed > 0 {
			org := organizationID
			readings, meterErr := w.meterMessages(ctx, w.logger, org, projectID, writes, now)
			if meterErr != nil {
				return false, fmt.Errorf("meter projected message: %w", meterErr)
			}
			if err := metering.Enqueue(ctx, tx, readings); err != nil {
				return false, fmt.Errorf("enqueue projection metering: %w", err)
			}
			if err := w.enqueueMessageEvents(ctx, tx, org, projectID, writes, now); err != nil {
				return false, fmt.Errorf("enqueue projection events: %w", err)
			}
		}
	}
	if err != nil {
		return false, fmt.Errorf("persist projected message: %w", err)
	}
	if err := tx.Commit(ctx); err != nil {
		return false, fmt.Errorf("commit message projection: %w", err)
	}
	if changed > 0 {
		w.publishTurnFrames(ctx, nil, []repo.CreateChatMessageParams{write.Params})
		w.notifyMessagesStored(ctx, projectID)
	}
	return changed > 0, nil
}

// insertProjection stores a new kit message through the project's transcript
// mode. A projection that reaches here always writes, so a failed key load
// fails it.
func (w *ChatMessageWriter) insertProjection(ctx context.Context, tx pgx.Tx, projectID uuid.UUID, params repo.CreateChatMessageParams, prepared error) (int64, string, error) {
	q := repo.New(tx)
	organizationID, err := q.GetProjectOrganizationID(ctx, projectID)
	if err != nil {
		return 0, "", fmt.Errorf("look up projection organization: %w", err)
	}
	if prepared != nil && w.transcripts.Enabled(projectID) {
		return 0, "", prepared
	}
	var changed int64
	refs := []transcriptstore.ProtectedFieldRefs{protectedFieldRefs(params.ID, &params.Content, &params.ContentRaw, &params.ToolCalls, &params.StorageError)}
	_, err = w.insertMessages(ctx, tx, organizationID, projectID, refs, func() ([]uuid.UUID, error) {
		n, err := q.CreateChatMessage(ctx, []repo.CreateChatMessageParams{params})
		if err != nil {
			return nil, fmt.Errorf("create projected message: %w", err)
		}
		changed = n
		if n == 0 {
			return nil, nil
		}
		return []uuid.UUID{params.ID}, nil
	})
	if err != nil {
		return 0, "", err
	}
	return changed, organizationID, nil
}

// patchProjection rewrites an existing kit message in the caller's
// transaction through UpdateChatMessage, which keeps every copy of the row in
// step. It sets the content hash and finish reason, and when content,
// content_raw or tool_calls differ from the row as UpdateChatMessage read it
// (decrypted in encrypted mode), it writes them and clears the risk mark so
// the new content is analyzed again. It returns 1 when anything changed.
func (w *ChatMessageWriter) patchProjection(ctx context.Context, tx pgx.Tx, projectID uuid.UUID, row repo.ChatMessage, write MessageWrite, prepared error) (int64, error) {
	// An unchanged patch must not lock the row and open its transcript. In
	// encrypted mode the plaintext columns are empty, so this check passes
	// and apply compares against the decrypted row instead.
	if !contentDiffers(row, write) && bytes.Equal(row.ContentHash, write.Params.ContentHash) && row.FinishReason == write.Params.FinishReason {
		return 0, nil
	}
	if prepared != nil && w.transcripts.Enabled(projectID) {
		return 0, prepared
	}
	organizationID, err := repo.New(tx).GetProjectOrganizationID(ctx, projectID)
	if err != nil {
		return 0, fmt.Errorf("look up projection organization: %w", err)
	}
	changed, err := w.transcripts.UpdateChatMessage(ctx, tx, organizationID, projectID, row.ChatID, row.ID, func(m *repo.ChatMessage) error {
		m.ContentHash = write.Params.ContentHash
		m.FinishReason = write.Params.FinishReason
		if !contentDiffers(*m, write) {
			return nil
		}
		m.Content = write.Params.Content
		m.ContentRaw = write.Params.ContentRaw
		m.ToolCalls = write.Params.ToolCalls
		m.RiskAnalyzedAt = pgtype.Timestamptz{Time: time.Time{}, InfinityModifier: pgtype.Finite, Valid: false}
		return nil
	})
	if err != nil {
		return 0, fmt.Errorf("patch projected message: %w", err)
	}
	if !changed {
		return 0, nil
	}
	return 1, nil
}

// contentDiffers reports whether the write changes content, content_raw or
// tool_calls. Postgres re-serializes jsonb, so JSON is compared by value.
func contentDiffers(row repo.ChatMessage, write MessageWrite) bool {
	return row.Content != write.Params.Content || !transcriptstore.SameJSON(row.ContentRaw, write.Params.ContentRaw) || !transcriptstore.SameJSON(row.ToolCalls, write.Params.ToolCalls)
}
