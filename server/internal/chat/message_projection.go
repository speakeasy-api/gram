package chat

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/speakeasy-api/gram/server/internal/chat/repo"
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
	if write.Params.ProjectID != projectID || write.Params.ID != messageID || write.Params.Source.String != "kit" {
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
	if exists {
		changed, err = q.PatchKitProjectionMessage(ctx, repo.PatchKitProjectionMessageParams{ProjectID: uuid.NullUUID{UUID: projectID, Valid: true}, ChatID: write.Params.ChatID, ID: messageID, Content: write.Params.Content, ContentRaw: write.Params.ContentRaw, ToolCalls: write.Params.ToolCalls, ContentHash: write.Params.ContentHash})
	} else {
		changed, err = q.CreateChatMessage(ctx, []repo.CreateChatMessageParams{write.Params})
		if err == nil && changed > 0 {
			org, lookupErr := q.GetProjectOrganizationID(ctx, projectID)
			if lookupErr != nil {
				return false, fmt.Errorf("look up projection organization: %w", lookupErr)
			}
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
