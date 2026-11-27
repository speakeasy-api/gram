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
	var changed int64
	var write MessageWrite
	err := w.InTx(ctx, projectID, func(tx pgx.Tx) error {
		var err error
		changed, write, err = w.projectMessage(ctx, tx, projectID, messageID, guard, build)
		return err
	})
	if err != nil {
		return false, err
	}
	if changed > 0 {
		w.publishTurnFrames(ctx, nil, []repo.CreateChatMessageParams{write.Params})
		w.notifyMessagesStored(ctx, projectID)
	}
	return changed > 0, nil
}

// projectMessage is ProjectMessage inside its transaction. It returns how many
// rows changed and the write build produced, which was applied only when that
// count is above zero.
func (w *ChatMessageWriter) projectMessage(ctx context.Context, tx pgx.Tx, projectID, messageID uuid.UUID,
	guard func(context.Context, pgx.Tx) error,
	build func(*repo.ChatMessage) (MessageWrite, bool, error),
) (int64, MessageWrite, error) {
	var none MessageWrite
	if err := guard(ctx, tx); err != nil {
		return 0, none, fmt.Errorf("admit message projection: %w", err)
	}
	q := repo.New(tx)
	row, err := q.GetKitProjectionMessage(ctx, repo.GetKitProjectionMessageParams{ProjectID: uuid.NullUUID{UUID: projectID, Valid: true}, ID: messageID})
	exists := err == nil
	if err != nil && !errors.Is(err, pgx.ErrNoRows) {
		return 0, none, fmt.Errorf("load projected message: %w", err)
	}
	var previous *repo.ChatMessage
	if exists {
		previous = &row
	}
	write, apply, err := build(previous)
	if err != nil {
		return 0, none, fmt.Errorf("build message projection: %w", err)
	}
	if !apply {
		return 0, write, nil
	}
	if write.Params.ProjectID != projectID || write.Params.ID != messageID || (!write.Params.Source.Valid || write.Params.Source.String != "kit") {
		return 0, none, fmt.Errorf("invalid projection ownership")
	}
	if exists && (row.Source.String != "kit" || row.ChatID != write.Params.ChatID || row.Role != write.Params.Role) {
		return 0, none, fmt.Errorf("projection cannot replace another producer's message")
	}
	if err := requireChatProject(ctx, tx, write.Params.ChatID, projectID); err != nil {
		return 0, none, fmt.Errorf("validate projection chat project: %w", err)
	}
	now := time.Now().UTC()
	writes := []MessageWrite{write}
	if err := stampMessageFields(writes, now); err != nil {
		return 0, none, fmt.Errorf("stamp projected message: %w", err)
	}
	write = writes[0]
	if exists {
		changed, err := w.patchProjection(ctx, tx, projectID, row, write)
		if err != nil {
			return 0, none, fmt.Errorf("persist projected message: %w", err)
		}
		return changed, write, nil
	}
	changed, organizationID, err := w.insertProjection(ctx, tx, projectID, write.Params)
	if err != nil {
		return 0, none, fmt.Errorf("persist projected message: %w", err)
	}
	if changed == 0 {
		return 0, write, nil
	}
	readings, err := w.meterMessages(ctx, w.logger, organizationID, projectID, writes, now)
	if err != nil {
		return 0, none, fmt.Errorf("meter projected message: %w", err)
	}
	if err := metering.Enqueue(ctx, tx, readings); err != nil {
		return 0, none, fmt.Errorf("enqueue projection metering: %w", err)
	}
	if err := w.enqueueMessageEvents(ctx, tx, organizationID, projectID, writes, now); err != nil {
		return 0, none, fmt.Errorf("enqueue projection events: %w", err)
	}
	return changed, write, nil
}

// insertProjection stores a new kit message through the project's transcript
// mode.
func (w *ChatMessageWriter) insertProjection(ctx context.Context, tx pgx.Tx, projectID uuid.UUID, params repo.CreateChatMessageParams) (int64, string, error) {
	organizationID, err := repo.New(tx).GetProjectOrganizationID(ctx, projectID)
	if err != nil {
		return 0, "", fmt.Errorf("look up projection organization: %w", err)
	}
	n, err := w.transcripts.InsertChatMessages(ctx, tx, organizationID, projectID, []repo.CreateChatMessageParams{params})
	if err != nil {
		return 0, "", fmt.Errorf("create projected message: %w", err)
	}
	return n, organizationID, nil
}

// patchProjection rewrites an existing kit message in the caller's
// transaction through UpdateChatMessage, which keeps every copy of the row in
// step. It sets the content hash and finish reason, and when content,
// content_raw or tool_calls differ from the row as UpdateChatMessage read it
// (decrypted in encrypted mode), it writes them and clears the risk mark so
// the new content is analyzed again. It returns 1 when anything changed.
func (w *ChatMessageWriter) patchProjection(ctx context.Context, tx pgx.Tx, projectID uuid.UUID, row repo.ChatMessage, write MessageWrite) (int64, error) {
	// An unchanged patch must not lock the row and open its transcript. In
	// encrypted mode the plaintext columns are empty, so this check passes
	// and apply compares against the decrypted row instead.
	if !contentDiffers(row, write) && bytes.Equal(row.ContentHash, write.Params.ContentHash) && row.FinishReason == write.Params.FinishReason {
		return 0, nil
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
