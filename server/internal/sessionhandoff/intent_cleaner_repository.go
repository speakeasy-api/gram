package sessionhandoff

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/speakeasy-api/gram/server/internal/agent/repo"
	"github.com/speakeasy-api/gram/server/internal/conv"
)

type PgxUploadIntentRepository struct{ queries *repo.Queries }

func NewPgxUploadIntentRepository(db *pgxpool.Pool) *PgxUploadIntentRepository {
	return &PgxUploadIntentRepository{queries: repo.New(db)}
}

func (r *PgxUploadIntentRepository) ListStaleUploadIntents(ctx context.Context, staleBefore time.Time, cursor *UploadIntentCursor, batchSize int32) ([]UploadIntent, error) {
	var params repo.ListStaleSessionHandoffUploadIntentsParams
	params.StaleBefore = conv.ToPGTimestamptz(staleBefore)
	params.BatchSize = batchSize
	if cursor != nil {
		params.AfterCreatedAt = conv.ToPGTimestamptz(cursor.CreatedAt)
		params.AfterID = cursor.ID
	}
	rows, err := r.queries.ListStaleSessionHandoffUploadIntents(ctx, params)
	if err != nil {
		return nil, fmt.Errorf("list stale upload intents: %w", err)
	}
	result := make([]UploadIntent, 0, len(rows))
	for _, row := range rows {
		result = append(result, UploadIntent{ID: row.ID, ProjectID: row.ProjectID, BlobURL: row.BlobUrl, CreatedAt: row.CreatedAt.Time})
	}
	return result, nil
}

func (r *PgxUploadIntentRepository) ClaimStaleUploadIntent(ctx context.Context, intent UploadIntent, staleBefore time.Time) (UploadIntent, bool, error) {
	row, err := r.queries.ClaimStaleSessionHandoffUploadIntent(ctx, repo.ClaimStaleSessionHandoffUploadIntentParams{ID: intent.ID, ProjectID: intent.ProjectID, StaleBefore: conv.ToPGTimestamptz(staleBefore)})
	if err != nil {
		var empty UploadIntent
		if errors.Is(err, pgx.ErrNoRows) {
			return empty, false, nil
		}
		return empty, false, fmt.Errorf("claim stale upload intent: %w", err)
	}
	return UploadIntent{ID: row.ID, ProjectID: row.ProjectID, BlobURL: row.BlobUrl, CreatedAt: row.CreatedAt.Time}, true, nil
}

func (r *PgxUploadIntentRepository) BlobIsReferenced(ctx context.Context, intent UploadIntent) (bool, error) {
	referenced, err := r.queries.SessionHandoffBlobIsReferenced(ctx, repo.SessionHandoffBlobIsReferencedParams{ProjectID: intent.ProjectID, BlobUrl: intent.BlobURL})
	if err != nil {
		return false, fmt.Errorf("check handoff blob reference: %w", err)
	}
	return referenced, nil
}

func (r *PgxUploadIntentRepository) RestoreLinkedUpload(ctx context.Context, intent UploadIntent) error {
	_, err := r.queries.RestoreSessionHandoffUploadRegistryLinked(ctx, repo.RestoreSessionHandoffUploadRegistryLinkedParams{ID: intent.ID, ProjectID: intent.ProjectID})
	if err != nil {
		return fmt.Errorf("restore linked handoff upload registry: %w", err)
	}
	return nil
}

func (r *PgxUploadIntentRepository) ClearUploadIntent(ctx context.Context, intent UploadIntent) error {
	_, err := r.queries.DeleteSessionHandoffUploadRegistry(ctx, repo.DeleteSessionHandoffUploadRegistryParams{ID: intent.ID, ProjectID: intent.ProjectID})
	if err != nil {
		return fmt.Errorf("clear handoff upload intent: %w", err)
	}
	return nil
}

func (r *PgxUploadIntentRepository) QuarantineUploadIntent(ctx context.Context, intent UploadIntent, reason string) error {
	_, err := r.queries.QuarantineSessionHandoffUploadIntent(ctx, repo.QuarantineSessionHandoffUploadIntentParams{LastError: conv.ToPGText(reason), ID: intent.ID, ProjectID: intent.ProjectID})
	if err != nil {
		return fmt.Errorf("quarantine handoff upload intent: %w", err)
	}
	return nil
}
