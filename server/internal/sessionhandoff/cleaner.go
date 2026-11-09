package sessionhandoff

import (
	"context"
	"errors"
	"fmt"
	"net/url"
	"time"

	"github.com/google/uuid"
)

const expiredUnreadCleanupBatchSize int32 = 100
const ConsumerClaimLease = 10 * time.Minute

// ExpiredUnread is an expired handoff whose blob was never claimed by a reader.
type ExpiredUnread struct {
	ID        uuid.UUID
	ProjectID uuid.UUID
	BlobURL   string
	ExpiresAt time.Time
}

type ExpiredUnreadRepository interface {
	ListExpiredUnreadProjects(context.Context, *uuid.UUID, int32) ([]uuid.UUID, error)
	ListExpiredUnread(context.Context, uuid.UUID, *ExpiredUnreadCursor, int32) ([]ExpiredUnread, error)
	ClaimExpiredUnread(context.Context, ExpiredUnread) (ExpiredUnread, bool, error)
	FinalizeExpiredUnread(context.Context, ExpiredUnread) (bool, error)
	QuarantineExpiredUnread(context.Context, ExpiredUnread, string) error
}

type ExpiredUnreadCursor struct {
	ExpiresAt time.Time
	ID        uuid.UUID
}

type blobDeleter interface {
	Delete(context.Context, *url.URL) error
}

type ExpiredUnreadCleaner struct {
	repository ExpiredUnreadRepository
	blobs      blobDeleter
}

func NewExpiredUnreadCleaner(repository ExpiredUnreadRepository, blobs blobDeleter) *ExpiredUnreadCleaner {
	return &ExpiredUnreadCleaner{repository: repository, blobs: blobs}
}

// ListProjects returns one ordered page of project IDs after cursor. More is
// true when the page is full and another page may exist.
func (c *ExpiredUnreadCleaner) ListProjects(ctx context.Context, cursor *uuid.UUID, batchSize int32) ([]uuid.UUID, bool, error) {
	if c == nil || c.repository == nil {
		return nil, false, fmt.Errorf("expired unread handoff cleaner is not configured")
	}
	projects, err := c.repository.ListExpiredUnreadProjects(ctx, cursor, batchSize)
	if err != nil {
		return nil, false, fmt.Errorf("list projects with expired unread handoffs: %w", err)
	}
	return projects, len(projects) == int(batchSize), nil
}

// CleanProjectAfter removes one ordered project batch after cursor. The returned
// cursor advances past every selected row, including malformed or failed rows,
// so a permanent leading failure cannot poison later batches in the activity.
func (c *ExpiredUnreadCleaner) CleanProjectAfter(ctx context.Context, projectID uuid.UUID, cursor *ExpiredUnreadCursor) (*ExpiredUnreadCursor, bool, error) {
	if c == nil || c.repository == nil || c.blobs == nil {
		return cursor, false, fmt.Errorf("expired unread handoff cleaner is not configured")
	}
	rows, err := c.repository.ListExpiredUnread(ctx, projectID, cursor, expiredUnreadCleanupBatchSize)
	if err != nil {
		return cursor, false, fmt.Errorf("list expired unread handoffs: %w", err)
	}

	next := cursor
	if len(rows) > 0 {
		last := rows[len(rows)-1]
		next = &ExpiredUnreadCursor{ExpiresAt: last.ExpiresAt, ID: last.ID}
	}
	var rowErrors []error
	for _, candidate := range rows {
		row, claimed, err := c.repository.ClaimExpiredUnread(ctx, candidate)
		if err != nil {
			rowErrors = append(rowErrors, fmt.Errorf("claim expired handoff %s: %w", candidate.ID, err))
			continue
		}
		if !claimed {
			continue
		}
		blobURL, err := url.ParseRequestURI(row.BlobURL)
		if err != nil || blobURL.Scheme == "" || blobURL.Host == "" {
			if quarantineErr := c.repository.QuarantineExpiredUnread(ctx, row, "malformed blob URL"); quarantineErr != nil {
				rowErrors = append(rowErrors, fmt.Errorf("quarantine expired handoff %s: %w", row.ID, quarantineErr))
			}
			continue
		}
		if err := c.blobs.Delete(ctx, blobURL); err != nil {
			rowErrors = append(rowErrors, fmt.Errorf("delete expired handoff %s blob: %w", row.ID, err))
			continue
		}
		if _, err := c.repository.FinalizeExpiredUnread(ctx, row); err != nil {
			rowErrors = append(rowErrors, fmt.Errorf("finalize expired handoff %s blob: %w", row.ID, err))
		}
	}
	return next, len(rows) == int(expiredUnreadCleanupBatchSize), errors.Join(rowErrors...)
}
