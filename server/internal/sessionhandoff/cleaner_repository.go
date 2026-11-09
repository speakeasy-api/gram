package sessionhandoff

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/speakeasy-api/gram/server/internal/agent/repo"
	"github.com/speakeasy-api/gram/server/internal/conv"
)

type PgxExpiredUnreadRepository struct {
	queries *repo.Queries
}

func NewPgxExpiredUnreadRepository(db *pgxpool.Pool) *PgxExpiredUnreadRepository {
	return &PgxExpiredUnreadRepository{queries: repo.New(db)}
}

func (r *PgxExpiredUnreadRepository) ListExpiredUnreadProjects(ctx context.Context, cursor *uuid.UUID, batchSize int32) ([]uuid.UUID, error) {
	params := repo.ListProjectsWithExpiredSessionHandoffsParams{
		ConsumerLeaseBefore: conv.ToPGTimestamptz(time.Now().Add(-ConsumerClaimLease)),
		AfterProjectID:      uuid.NullUUID{UUID: uuid.Nil, Valid: false},
		BatchSize:           batchSize,
	}
	if cursor != nil {
		params.AfterProjectID = uuid.NullUUID{UUID: *cursor, Valid: true}
	}
	rows, err := r.queries.ListProjectsWithExpiredSessionHandoffs(ctx, params)
	if err != nil {
		return nil, fmt.Errorf("list projects with expired unread session handoffs: %w", err)
	}
	result := make([]uuid.UUID, 0, len(rows))
	for _, row := range rows {
		if row.Valid {
			result = append(result, row.UUID)
		}
	}
	return result, nil
}

func (r *PgxExpiredUnreadRepository) ListExpiredUnread(ctx context.Context, projectID uuid.UUID, cursor *ExpiredUnreadCursor, batchSize int32) ([]ExpiredUnread, error) {
	var params repo.ListExpiredUnreadSessionHandoffLinksParams
	params.ProjectID = projectID
	params.ConsumerLeaseBefore = conv.ToPGTimestamptz(time.Now().Add(-ConsumerClaimLease))
	params.BatchSize = batchSize
	if cursor != nil {
		params.AfterExpiresAt = conv.ToPGTimestamptz(cursor.ExpiresAt)
		params.AfterID = cursor.ID
	}
	rows, err := r.queries.ListExpiredUnreadSessionHandoffLinks(ctx, params)
	if err != nil {
		return nil, fmt.Errorf("list expired unread session handoffs: %w", err)
	}
	result := make([]ExpiredUnread, 0, len(rows))
	for _, row := range rows {
		result = append(result, ExpiredUnread{
			ID: row.ID, ProjectID: row.ProjectID.UUID, BlobURL: row.BlobUrl,
			ExpiresAt: row.ExpiresAt.Time,
		})
	}
	return result, nil
}

func (r *PgxExpiredUnreadRepository) ClaimExpiredUnread(ctx context.Context, row ExpiredUnread) (ExpiredUnread, bool, error) {
	claimed, err := r.queries.ClaimExpiredUnreadSessionHandoffLink(ctx, repo.ClaimExpiredUnreadSessionHandoffLinkParams{
		ID: row.ID, ProjectID: row.ProjectID, BlobUrl: row.BlobURL, ExpiresAt: conv.ToPGTimestamptz(row.ExpiresAt),
		ConsumerLeaseBefore: conv.ToPGTimestamptz(time.Now().Add(-ConsumerClaimLease)),
	})
	if err != nil {
		var empty ExpiredUnread
		if errors.Is(err, pgx.ErrNoRows) {
			return empty, false, nil
		}
		return empty, false, fmt.Errorf("claim expired handoff: %w", err)
	}
	return ExpiredUnread{ID: claimed.ID, ProjectID: claimed.ProjectID, BlobURL: claimed.BlobUrl, ExpiresAt: claimed.ExpiresAt.Time}, true, nil
}

func (r *PgxExpiredUnreadRepository) FinalizeExpiredUnread(ctx context.Context, row ExpiredUnread) (bool, error) {
	count, err := r.queries.FinalizeExpiredUnreadSessionHandoffLink(ctx, repo.FinalizeExpiredUnreadSessionHandoffLinkParams{
		ID: row.ID, ProjectID: row.ProjectID, BlobUrl: row.BlobURL, ExpiresAt: conv.ToPGTimestamptz(row.ExpiresAt),
	})
	if err != nil {
		return false, fmt.Errorf("finalize expired handoff: %w", err)
	}
	if count == 1 {
		if _, err := r.queries.DeleteSessionHandoffUploadRegistryByBlob(ctx, repo.DeleteSessionHandoffUploadRegistryByBlobParams{ProjectID: row.ProjectID, BlobUrl: row.BlobURL}); err != nil {
			return true, fmt.Errorf("clear expired handoff blob registry: %w", err)
		}
	}
	return count == 1, nil
}

func (r *PgxExpiredUnreadRepository) QuarantineExpiredUnread(ctx context.Context, row ExpiredUnread, reason string) error {
	_, err := r.queries.QuarantineSessionHandoffBlobCleanup(ctx, repo.QuarantineSessionHandoffBlobCleanupParams{
		BlobCleanupError: conv.ToPGText(reason), ID: row.ID, ProjectID: row.ProjectID, BlobUrl: row.BlobURL,
	})
	if err != nil {
		return fmt.Errorf("quarantine expired handoff: %w", err)
	}
	return nil
}
