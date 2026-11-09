package sessionhandoff

import (
	"context"
	"errors"
	"fmt"
	"net/url"
	"time"

	"github.com/google/uuid"
)

const UploadIntentStaleAge = 15 * time.Minute

type UploadIntent struct {
	ID        uuid.UUID
	ProjectID uuid.UUID
	BlobURL   string
	CreatedAt time.Time
}

type UploadIntentCursor struct {
	CreatedAt time.Time
	ID        uuid.UUID
}

type UploadIntentRepository interface {
	ListStaleUploadIntents(context.Context, time.Time, *UploadIntentCursor, int32) ([]UploadIntent, error)
	ClaimStaleUploadIntent(context.Context, UploadIntent, time.Time) (UploadIntent, bool, error)
	BlobIsReferenced(context.Context, UploadIntent) (bool, error)
	RestoreLinkedUpload(context.Context, UploadIntent) error
	ClearUploadIntent(context.Context, UploadIntent) error
	QuarantineUploadIntent(context.Context, UploadIntent, string) error
}

type UploadIntentCleaner struct {
	repository UploadIntentRepository
	blobs      blobDeleter
}

func NewUploadIntentCleaner(repository UploadIntentRepository, blobs blobDeleter) *UploadIntentCleaner {
	return &UploadIntentCleaner{repository: repository, blobs: blobs}
}

func (c *UploadIntentCleaner) CleanAfter(ctx context.Context, staleBefore time.Time, cursor *UploadIntentCursor) (*UploadIntentCursor, bool, error) {
	rows, err := c.repository.ListStaleUploadIntents(ctx, staleBefore, cursor, expiredUnreadCleanupBatchSize)
	if err != nil {
		return cursor, false, fmt.Errorf("list stale handoff upload intents: %w", err)
	}
	next := cursor
	if len(rows) > 0 {
		last := rows[len(rows)-1]
		next = &UploadIntentCursor{CreatedAt: last.CreatedAt, ID: last.ID}
	}
	var rowErrors []error
	for _, candidate := range rows {
		intent, claimed, err := c.repository.ClaimStaleUploadIntent(ctx, candidate, staleBefore)
		if err != nil {
			rowErrors = append(rowErrors, err)
			continue
		}
		if !claimed {
			continue
		}
		if intent.BlobURL == "" {
			if err := c.repository.ClearUploadIntent(ctx, intent); err != nil {
				rowErrors = append(rowErrors, err)
			}
			continue
		}
		referenced, err := c.repository.BlobIsReferenced(ctx, intent)
		if err != nil {
			rowErrors = append(rowErrors, err)
			continue
		}
		if referenced {
			if err := c.repository.RestoreLinkedUpload(ctx, intent); err != nil {
				rowErrors = append(rowErrors, err)
			}
			continue
		}
		blobURL, err := url.ParseRequestURI(intent.BlobURL)
		if err != nil || blobURL.Scheme == "" || blobURL.Host == "" {
			if err := c.repository.QuarantineUploadIntent(ctx, intent, "malformed blob URL"); err != nil {
				rowErrors = append(rowErrors, err)
			}
			continue
		}
		if err := c.blobs.Delete(ctx, blobURL); err != nil {
			rowErrors = append(rowErrors, fmt.Errorf("delete orphan handoff upload %s: %w", intent.ID, err))
			continue
		}
		if err := c.repository.ClearUploadIntent(ctx, intent); err != nil {
			rowErrors = append(rowErrors, err)
		}
	}
	return next, len(rows) == int(expiredUnreadCleanupBatchSize), errors.Join(rowErrors...)
}
